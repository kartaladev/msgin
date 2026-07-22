package msghttp_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
)

// --- Plan 024 Task 5: O2 NewExchange / Exchange -----------------------------
//
// The INV-7 panic-unwind test (branch 24) is written FIRST: it is the contract
// this task exists to honour — O2 is the first external RequestReplyExchange, so
// it is the first bound by the no-leak-on-unwind contract (ADR 0022 Addendum A3
// / Spec 012 §7). Every other branch (1-29, incl. 20b) follows with a
// mutation-sensitive case. HTTP is fully hermetic: httptest.Server, hand-built
// responses via a roundTripperFunc, no real network.

// TestExchange_panicUnwind covers branch 24 / INV-7: when the response body's
// Read panics, Exchange does NOT recover it (the ORIGINAL panic value
// propagates, un-converted), and the single deferred Close still runs on the
// unwind. Status 200, so ClassifyResponse returns nil without touching the body
// and the panic surfaces from the io.ReadAll of the reply body — the deferred
// Close registered immediately after a nil-error Do covers it.
//
// Honest scope: this proves the DEFERRED cleanup covers the body reads. It does
// NOT (and cannot) cover a window between Do returning and the defer registering
// — there is none in the control flow.
func TestExchange_panicUnwind(t *testing.T) {
	t.Parallel()

	panicVal := "read-boom"
	body := &panickingBody{panicVal: panicVal}
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}, nil
	})}

	x, err := msghttp.NewExchange("https://example.test/rpc", msghttp.WithHTTPClient(client))
	require.NoError(t, err)

	recovered := func() (rec any) {
		defer func() { rec = recover() }()
		_, _ = x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		return nil
	}()

	require.NotNil(t, recovered, "the panic must propagate, not be converted to an error")
	assert.Equal(t, panicVal, recovered, "the ORIGINAL panic value must be preserved")
	assert.True(t, body.closed.Load(), "the single deferred Close must run on the panic unwind")
}

// exchangeReturning builds an Exchange whose client returns a hand-built response
// (status, body, header) with no network — giving the provenance/INV-2 branches
// full control over the response headers a real server would sniff or rewrite.
func exchangeReturning(t *testing.T, code int, body string, header http.Header, opts ...msghttp.Option) *msghttp.Exchange {
	t.Helper()
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		h := http.Header{}
		if header != nil {
			h = header.Clone()
		}
		return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	all := append([]msghttp.Option{msghttp.WithHTTPClient(client)}, opts...)
	x, err := msghttp.NewExchange("https://example.test/rpc", all...)
	require.NoError(t, err)
	return x
}

// TestNewExchange_construction covers branches 1-2: URL validation via the four
// sentinels (reached through NewExchange — Task 4 branch 1 for O1's twin) and a
// bad Option propagating NewConfig's error.
func TestNewExchange_construction(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		url    string
		opts   []msghttp.Option
		assert func(t *testing.T, x *msghttp.Exchange, err error)
	}

	cases := []testCase{
		{
			name: "empty URL -> ErrEmptyURL",
			url:  "",
			assert: func(t *testing.T, x *msghttp.Exchange, err error) {
				assert.Nil(t, x)
				assert.ErrorIs(t, err, msghttp.ErrEmptyURL)
			},
		},
		{
			name: "whitespace URL -> ErrEmptyURL",
			url:  "   ",
			assert: func(t *testing.T, x *msghttp.Exchange, err error) {
				assert.Nil(t, x)
				assert.ErrorIs(t, err, msghttp.ErrEmptyURL)
			},
		},
		{
			name: "unparseable URL -> ErrInvalidURL",
			url:  "://bad",
			assert: func(t *testing.T, x *msghttp.Exchange, err error) {
				assert.Nil(t, x)
				assert.ErrorIs(t, err, msghttp.ErrInvalidURL)
			},
		},
		{
			name: "non-http scheme -> ErrInvalidURL",
			url:  "file:///etc/passwd",
			assert: func(t *testing.T, x *msghttp.Exchange, err error) {
				assert.Nil(t, x)
				assert.ErrorIs(t, err, msghttp.ErrInvalidURL)
			},
		},
		{
			name: "empty host -> ErrInvalidURL",
			url:  "http:///path",
			assert: func(t *testing.T, x *msghttp.Exchange, err error) {
				assert.Nil(t, x)
				assert.ErrorIs(t, err, msghttp.ErrInvalidURL)
			},
		},
		{
			name: "a bad Option propagates NewConfig's error",
			url:  "https://example.test/rpc",
			opts: []msghttp.Option{msghttp.WithMaxResponseBytes(0)},
			assert: func(t *testing.T, x *msghttp.Exchange, err error) {
				assert.Nil(t, x)
				assert.ErrorIs(t, err, msghttp.ErrInvalidMaxResponseBytes)
			},
		},
		{
			name: "a valid URL and options construct an Exchange",
			url:  "https://example.test/rpc",
			assert: func(t *testing.T, x *msghttp.Exchange, err error) {
				require.NoError(t, err)
				assert.NotNil(t, x)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			x, err := msghttp.NewExchange(tc.url, tc.opts...)
			tc.assert(t, x, err)
		})
	}
}

// TestExchange_replyProvenance covers branches 3-8: the reply payload is the
// response bytes (3); the request's correlation id (4) and sequence-number/size
// (6) survive the hop; a request with no correlation id materialises none (5);
// the reply gets a FRESH message-id and timestamp, not the request's (7); and
// the request's own content-type does NOT leak onto the reply (8).
func TestExchange_replyProvenance(t *testing.T) {
	t.Parallel()

	t.Run("branch 3: the reply payload is the response body bytes", func(t *testing.T) {
		t.Parallel()
		x := exchangeReturning(t, http.StatusOK, "reply-payload", nil)
		reply, err := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		require.NoError(t, err)
		assert.Equal(t, []byte("reply-payload"), reply.Payload())
	})

	t.Run("branch 4: the request correlation id is propagated onto the reply", func(t *testing.T) {
		t.Parallel()
		x := exchangeReturning(t, http.StatusOK, "r", nil)
		req := msgin.New[any]([]byte("req")).WithHeader(msgin.HeaderCorrelationID, "corr-1")
		reply, err := x.Exchange(t.Context(), req)
		require.NoError(t, err)
		got, ok := reply.Headers().String(msgin.HeaderCorrelationID)
		require.True(t, ok, "the correlation id must survive the hop")
		assert.Equal(t, "corr-1", got)
	})

	t.Run("branch 5: no correlation id materialises none on the reply", func(t *testing.T) {
		t.Parallel()
		x := exchangeReturning(t, http.StatusOK, "r", nil)
		reply, err := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		require.NoError(t, err)
		_, ok := reply.Header(msgin.HeaderCorrelationID)
		assert.False(t, ok, "no empty correlation header may be materialised")
	})

	t.Run("branch 6: sequence-number/size survive the hop (Aggregator correctness)", func(t *testing.T) {
		t.Parallel()
		x := exchangeReturning(t, http.StatusOK, "r", nil)
		req := msgin.New[any]([]byte("req")).
			WithHeader(msgin.HeaderSequenceNumber, 3).
			WithHeader(msgin.HeaderSequenceSize, 5)
		reply, err := x.Exchange(t.Context(), req)
		require.NoError(t, err)
		num, okNum := reply.Headers().Int(msgin.HeaderSequenceNumber)
		size, okSize := reply.Headers().Int(msgin.HeaderSequenceSize)
		require.True(t, okNum)
		require.True(t, okSize)
		assert.Equal(t, 3, num)
		assert.Equal(t, 5, size)
	})

	t.Run("branch 7: the reply gets a fresh message-id and timestamp", func(t *testing.T) {
		t.Parallel()
		x := exchangeReturning(t, http.StatusOK, "r", nil)
		req := msgin.New[any]([]byte("req"))
		reply, err := x.Exchange(t.Context(), req)
		require.NoError(t, err)
		assert.NotEmpty(t, reply.ID(), "the reply must carry a fresh, non-empty message-id")
		assert.NotEqual(t, req.ID(), reply.ID(), "the reply id must differ from the request's")
		_, ok := reply.Headers().Time(msgin.HeaderTimestamp)
		assert.True(t, ok, "the reply must carry a fresh timestamp")
	})

	t.Run("branch 8: the request's content-type does not leak onto the reply", func(t *testing.T) {
		t.Parallel()
		x := exchangeReturning(t, http.StatusOK, "r", nil) // response carries no Content-Type
		req := msgin.New[any]([]byte("req")).WithHeader(msgin.HeaderContentType, "application/json")
		reply, err := x.Exchange(t.Context(), req)
		require.NoError(t, err)
		_, ok := reply.Header(msgin.HeaderContentType)
		assert.False(t, ok, "the request's msgin.content-type describes the request, not the reply")
	})
}

// TestExchange_replyHeaders covers branches 9, 11-14: INV-2's non-reserved
// landing key for the response Content-Type (9), the un-forgeable reserved key
// (11), an empty response Content-Type setting nothing (12), an allow-listed
// response header copied CRLF-sanitized (13), and a non-allow-listed one dropped
// (14). Hand-built responses give exact header control (raw CRLF, empty CT).
func TestExchange_replyHeaders(t *testing.T) {
	t.Parallel()

	t.Run("branch 9 (INV-2): response text/html lands on http.content-type, never msgin.content-type", func(t *testing.T) {
		t.Parallel()
		h := http.Header{}
		h.Set("Content-Type", "text/html")
		x := exchangeReturning(t, http.StatusOK, "<b>hi</b>", h)
		reply, err := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		require.NoError(t, err)

		got, ok := reply.Headers().String("http.content-type")
		require.True(t, ok)
		assert.Equal(t, "text/html", got, "the remote media type lands on the NON-reserved key")
		_, forged := reply.Header(msgin.HeaderContentType)
		assert.False(t, forged, "the remote must never set the reserved msgin.content-type")
	})

	t.Run("branch 11 (INV-2): WithOutboundReplyHeaders cannot forge msgin.content-type", func(t *testing.T) {
		t.Parallel()
		// The remote replies with a header literally named msgin.content-type; if the
		// reserved-name guard were removed, allow-listing it (in either casing) would
		// forge the reserved key onto the reply — so both entries carry a value.
		h := http.Header{}
		h.Set("msgin.content-type", "text/html")
		x := exchangeReturning(t, http.StatusOK, "x", h,
			msghttp.WithOutboundReplyHeaders("msgin.content-type", "MSGIN.Content-Type"))
		reply, err := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		require.NoError(t, err)

		_, forgedLower := reply.Header("msgin.content-type")
		_, forgedUpper := reply.Header("MSGIN.Content-Type")
		assert.False(t, forgedLower, "lowercase reserved name must not forge a core header")
		assert.False(t, forgedUpper, "upper-case reserved name must not either — the guard is case-insensitive")
	})

	t.Run("branch 12: an empty response Content-Type sets no http.content-type", func(t *testing.T) {
		t.Parallel()
		x := exchangeReturning(t, http.StatusOK, "x", nil) // no Content-Type in the response
		reply, err := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		require.NoError(t, err)
		_, ok := reply.Header("http.content-type")
		assert.False(t, ok, "no http.content-type is set when the response carries none")
	})

	t.Run("branch 13: an allow-listed response header is copied CRLF-sanitized", func(t *testing.T) {
		t.Parallel()
		h := http.Header{}
		h.Set("X-Reply", "line1\r\nInjected: evil")
		x := exchangeReturning(t, http.StatusOK, "x", h, msghttp.WithOutboundReplyHeaders("X-Reply"))
		reply, err := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		require.NoError(t, err)

		got, ok := reply.Headers().String("X-Reply")
		require.True(t, ok)
		assert.Equal(t, "line1Injected: evil", got)
		assert.NotContains(t, got, "\r")
		assert.NotContains(t, got, "\n")
	})

	t.Run("branch 14: a non-allow-listed response header is dropped", func(t *testing.T) {
		t.Parallel()
		h := http.Header{}
		h.Set("X-Other", "value")
		x := exchangeReturning(t, http.StatusOK, "x", h, msghttp.WithOutboundReplyHeaders("X-Reply"))
		reply, err := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		require.NoError(t, err)
		_, ok := reply.Header("X-Other")
		assert.False(t, ok, "a header not on the allow-list must not reach the reply")
	})
}

// TestExchange_reflectedXSS_e2e covers branch 10 (INV-2 end-to-end): a reply
// whose remote Content-Type was text/html, driven through OutboundGateway into
// EncodeResponse, is served application/octet-stream + nosniff — the remote's
// media type landed on the NON-reserved http.content-type, so it can never
// choose how its own bytes are served back (the reflected-XSS regression).
func TestExchange_reflectedXSS_e2e(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	h.Set("Content-Type", "text/html")
	x := exchangeReturning(t, http.StatusOK, "<script>alert(1)</script>", h)

	encCfg, err := msghttp.NewConfig()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	next := msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		return msghttp.EncodeResponse(rec, m, encCfg)
	})
	handler := msgin.OutboundGateway(x)(next)
	require.NoError(t, handler.Handle(t.Context(), msgin.New[any]([]byte("req"))))

	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"),
		"the remote text/html must not become the served media type")
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
}

// TestExchange_errors covers branches 15-17, 25-26: a non-2xx yields the
// classified error and a ZERO reply (15); a transport error is redacted, INV-5
// (16); an encode failure is Permanent with ZERO server hits (17); ctx
// cancellation propagates (25); and a 302 is NOT followed — the redirect target
// is never hit (26, the O2 SSRF regression).
func TestExchange_errors(t *testing.T) {
	t.Parallel()

	t.Run("branch 15: a non-2xx yields the classified error and a zero reply", func(t *testing.T) {
		t.Parallel()
		x := exchangeReturning(t, http.StatusNotFound, "ignored error body", nil)
		reply, err := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))

		var se *msghttp.StatusError
		require.ErrorAs(t, err, &se)
		assert.Equal(t, http.StatusNotFound, se.Code)
		assert.Nil(t, reply.Payload(), "a failed exchange returns a zero-value reply")
		assert.Empty(t, reply.ID())
	})

	t.Run("branch 16: a transport error is redacted (INV-5)", func(t *testing.T) {
		t.Parallel()
		const secretURL = "https://user:pw@example.invalid/rpc?token=SECRET"
		client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: lookup failed")
		})}
		x, err := msghttp.NewExchange(secretURL, msghttp.WithHTTPClient(client))
		require.NoError(t, err)

		_, exErr := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		require.Error(t, exErr)
		assert.ErrorIs(t, exErr, msghttp.ErrOutboundTransport)
		s := exErr.Error()
		assert.NotContains(t, s, "SECRET", "the query token must not leak")
		assert.NotContains(t, s, "user", "the userinfo must not leak")
		assert.NotContains(t, s, "/rpc", "the URL path must not leak")
	})

	t.Run("branch 17: an encode failure is Permanent with zero server hits", func(t *testing.T) {
		t.Parallel()
		var hits atomic.Int64
		client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			hits.Add(1)
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
		})}
		x, err := msghttp.NewExchange("https://example.test/rpc", msghttp.WithHTTPClient(client))
		require.NoError(t, err)

		_, exErr := x.Exchange(t.Context(), msgin.New[any](42)) // int payload: unsupported
		require.Error(t, exErr)
		assert.ErrorIs(t, exErr, msghttp.ErrUnsupportedPayload)
		assert.Equal(t, int64(0), hits.Load(), "no request may be sent when encoding fails")
		assertPermanent(t, exErr)
	})

	t.Run("branch 25: ctx cancellation propagates", func(t *testing.T) {
		t.Parallel()
		client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, context.Canceled
		})}
		x, err := msghttp.NewExchange("https://example.test/rpc", msghttp.WithHTTPClient(client))
		require.NoError(t, err)

		_, exErr := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		assert.ErrorIs(t, exErr, context.Canceled, "the %w on ue.Err must preserve context.Canceled")
		assert.ErrorIs(t, exErr, msghttp.ErrOutboundTransport)
	})

	t.Run("branch 26 (INV-1): a 302 is not followed for O2", func(t *testing.T) {
		t.Parallel()
		var secondHits atomic.Int64
		second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			secondHits.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(second.Close)
		first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, second.URL, http.StatusFound)
		}))
		t.Cleanup(first.Close)

		x, err := msghttp.NewExchange(first.URL)
		require.NoError(t, err)

		reply, exErr := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		var se *msghttp.StatusError
		require.ErrorAs(t, exErr, &se)
		assert.Equal(t, http.StatusFound, se.Code, "the 302 itself is returned, not the target's 200")
		assert.Equal(t, int64(0), secondHits.Load(), "the redirect target MUST NOT be reached (SSRF)")
		assert.Nil(t, reply.Payload())
	})
}

// nilReadAtBoundary serves head in full (consumed by the ReadAll of the reply),
// then on the NEXT read returns (0, nil) EXACTLY ONCE before delivering one more
// byte — modelling an io.Reader that legally returns (0, nil) at the cap
// boundary. It proves the io.ReadFull probe loops past (0, nil) instead of
// misreading it as end-of-body (branch 20b / INV-6, round-2 audit F4). A lone
// Read+`n>0` would serve a truncated body as success here.
type nilReadAtBoundary struct {
	head       []byte
	emittedNil bool
	tailSent   bool
}

func (b *nilReadAtBoundary) Read(p []byte) (int, error) {
	if len(b.head) > 0 {
		n := copy(p, b.head)
		b.head = b.head[n:]
		return n, nil
	}
	if !b.emittedNil {
		b.emittedNil = true
		return 0, nil // the LEGAL (0, nil) a lone Read would misread as EOF
	}
	if !b.tailSent {
		b.tailSent = true
		return copy(p, []byte("B")), nil
	}
	return 0, io.EOF
}

// TestExchange_bodyBounds covers branches 18-23, 20b (INV-6 and INV-7 body
// lifecycle): a body exactly at cap succeeds intact (18); cap+1 -> ErrReplyTooLarge
// (19); WithMaxResponseBytes(MaxInt64) returns a non-empty body intact, the
// overflow regression (20); an over-cap body whose boundary read returns (0, nil)
// still -> ErrReplyTooLarge (20b); a mid-body read failure errors with the body
// closed (21); and the body is closed on the success (22) and non-2xx (23) paths.
func TestExchange_bodyBounds(t *testing.T) {
	t.Parallel()

	// clientReturning hands Do a hand-built response with the given status and body.
	clientReturning := func(code int, body io.ReadCloser) *http.Client {
		return &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: code, Header: http.Header{}, Body: body}, nil
		})}
	}
	newExchange := func(t *testing.T, code int, body io.ReadCloser, opts ...msghttp.Option) *msghttp.Exchange {
		t.Helper()
		all := append([]msghttp.Option{msghttp.WithHTTPClient(clientReturning(code, body))}, opts...)
		x, err := msghttp.NewExchange("https://example.test/rpc", all...)
		require.NoError(t, err)
		return x
	}

	t.Run("branch 18: a body exactly at cap succeeds with the full body", func(t *testing.T) {
		t.Parallel()
		x := newExchange(t, http.StatusOK, io.NopCloser(strings.NewReader("AAAA")), msghttp.WithMaxResponseBytes(4))
		reply, err := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		require.NoError(t, err)
		assert.Equal(t, []byte("AAAA"), reply.Payload())
	})

	t.Run("branch 19: a body at cap+1 -> ErrReplyTooLarge", func(t *testing.T) {
		t.Parallel()
		x := newExchange(t, http.StatusOK, io.NopCloser(strings.NewReader("AAAAB")), msghttp.WithMaxResponseBytes(4))
		_, err := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		assert.ErrorIs(t, err, msghttp.ErrReplyTooLarge)
	})

	t.Run("branch 20: WithMaxResponseBytes(MaxInt64) returns a non-empty body intact", func(t *testing.T) {
		t.Parallel()
		x := newExchange(t, http.StatusOK, io.NopCloser(strings.NewReader("hello")), msghttp.WithMaxResponseBytes(math.MaxInt64))
		reply, err := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		require.NoError(t, err, "MaxInt64 must not overflow the cap arithmetic into a silent empty body")
		assert.Equal(t, []byte("hello"), reply.Payload())
	})

	t.Run("branch 20b: an over-cap body with a (0,nil) boundary read still -> ErrReplyTooLarge", func(t *testing.T) {
		t.Parallel()
		body := io.NopCloser(&nilReadAtBoundary{head: []byte("AAAA")}) // 4 = cap, then (0,nil), then "B"
		x := newExchange(t, http.StatusOK, body, msghttp.WithMaxResponseBytes(4))
		_, err := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		assert.ErrorIs(t, err, msghttp.ErrReplyTooLarge,
			"the io.ReadFull probe must loop past (0,nil), not serve a truncated success")
	})

	t.Run("branch 21: a mid-body read failure errors with the body closed", func(t *testing.T) {
		t.Parallel()
		body := &trackingBody{r: io.MultiReader(strings.NewReader("partial"), &erroringReader{})}
		x := newExchange(t, http.StatusOK, body)
		_, err := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		require.Error(t, err)
		assert.NotErrorIs(t, err, msghttp.ErrReplyTooLarge, "a read failure is not an over-cap")
		assert.True(t, body.closed.Load(), "the deferred Close must run on a read failure")
	})

	t.Run("branch 22 (INV-7): the body is closed on the success path", func(t *testing.T) {
		t.Parallel()
		body := &trackingBody{r: strings.NewReader("ok")}
		x := newExchange(t, http.StatusOK, body)
		_, err := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		require.NoError(t, err)
		assert.True(t, body.closed.Load(), "the deferred Close must run on the success path")
	})

	t.Run("branch 23 (INV-7): the body is closed on the non-2xx path", func(t *testing.T) {
		t.Parallel()
		body := &trackingBody{r: strings.NewReader("error-body")}
		x := newExchange(t, http.StatusInternalServerError, body)
		_, err := x.Exchange(t.Context(), msgin.New[any]([]byte("req")))
		require.Error(t, err)
		assert.True(t, body.closed.Load(), "the deferred Close must run on the non-2xx path")
	})
}

// TestExchange_gateway covers branches 27-29: through msgin.NewGateway[[]byte,
// []byte] the reply bytes come back (27); through NewGateway[[]byte, string] the
// recorded codec limitation surfaces as msgin.ErrPayloadType (28); and through
// msgin.OutboundGateway an incoming correlation id is restored on the forwarded
// reply (29).
func TestExchange_gateway(t *testing.T) {
	t.Parallel()

	t.Run("branch 27: NewGateway[[]byte,[]byte].Request returns the reply bytes", func(t *testing.T) {
		t.Parallel()
		x := exchangeReturning(t, http.StatusOK, "reply-bytes", nil)
		g, err := msgin.NewGateway[[]byte, []byte](x)
		require.NoError(t, err)

		reply, err := g.Request(t.Context(), []byte("hi"))
		require.NoError(t, err)
		assert.Equal(t, []byte("reply-bytes"), reply)
	})

	t.Run("branch 28: NewGateway[[]byte,string] hits the codec limitation (ErrPayloadType)", func(t *testing.T) {
		t.Parallel()
		x := exchangeReturning(t, http.StatusOK, "reply-bytes", nil)
		g, err := msgin.NewGateway[[]byte, string](x)
		require.NoError(t, err)

		_, err = g.Request(t.Context(), []byte("hi"))
		assert.ErrorIs(t, err, msgin.ErrPayloadType,
			"a []byte reply cannot satisfy a string Rep — Gateway carries no codec")
	})

	t.Run("branch 29: OutboundGateway restores the incoming correlation id on the reply", func(t *testing.T) {
		t.Parallel()
		x := exchangeReturning(t, http.StatusOK, "reply", nil)
		var captured msgin.Message[any]
		next := msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
			captured = m
			return nil
		})
		handler := msgin.OutboundGateway(x)(next)

		in := msgin.New[any]([]byte("req")).WithHeader(msgin.HeaderCorrelationID, "incoming-corr")
		require.NoError(t, handler.Handle(t.Context(), in))

		got, ok := captured.Headers().String(msgin.HeaderCorrelationID)
		require.True(t, ok, "the incoming correlation id must be restored on the forwarded reply")
		assert.Equal(t, "incoming-corr", got)
	})
}

// ExampleNewExchange drives the O2 synchronous request-reply adapter through a
// msgin.NewGateway against an httptest.Server that answers every request with a
// fixed body.
//
// The gateway must be NewGateway[[]byte, []byte]: msgin.Gateway carries no codec
// and type-asserts the reply, so a []byte request payload (what EncodeRequest
// accepts) and a []byte reply (what Exchange produces) are the only types that
// round-trip. The call is g.Request(ctx, []byte(...)) — it takes a raw value and
// returns a raw []byte reply, not a message.
//
// Only the fixed reply bytes are printed; the server's random port never is.
func ExampleNewExchange() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte("pong"))
	}))
	defer srv.Close()

	exchange, err := msghttp.NewExchange(srv.URL)
	if err != nil {
		panic(err)
	}

	gateway, err := msgin.NewGateway[[]byte, []byte](exchange)
	if err != nil {
		panic(err)
	}

	reply, err := gateway.Request(context.Background(), []byte("ping"))
	if err != nil {
		panic(err)
	}

	fmt.Printf("reply: %s\n", reply)
	// Output: reply: pong
}
