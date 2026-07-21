package msgin_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeExchange is a lightweight RequestReplyExchange double for the one-method
// SPI: no mockgen needed.
type fakeExchange struct {
	fn func(ctx context.Context, req msgin.Message[any]) (msgin.Message[any], error)
}

func (f fakeExchange) Exchange(ctx context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
	return f.fn(ctx, req)
}

func TestNewGateway_nilExchange(t *testing.T) {
	gw, err := msgin.NewGateway[string, string](nil)

	assert.Nil(t, gw)
	assert.ErrorIs(t, err, msgin.ErrNilExchange)
}

func TestGateway_Request(t *testing.T) {
	tests := []struct {
		name   string
		fn     func(ctx context.Context, req msgin.Message[any]) (msgin.Message[any], error)
		assert func(t *testing.T, rep string, err error)
	}{
		{
			name: "happy path",
			fn: func(_ context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
				payload, ok := req.Payload().(string)
				if !ok || payload != "hello" {
					t.Fatalf("want request payload %q, got %v (ok=%v)", "hello", req.Payload(), ok)
				}
				corrID, ok := req.Headers().String(msgin.HeaderCorrelationID)
				if !ok || corrID == "" {
					t.Fatalf("want a non-empty HeaderCorrelationID on the request, got %q (ok=%v)", corrID, ok)
				}
				return msgin.New[any]("world"), nil
			},
			assert: func(t *testing.T, rep string, err error) {
				require.NoError(t, err)
				assert.Equal(t, "world", rep)
			},
		},
		{
			name: "wrong reply type",
			fn: func(context.Context, msgin.Message[any]) (msgin.Message[any], error) {
				return msgin.New[any](42), nil
			},
			assert: func(t *testing.T, rep string, err error) {
				assert.ErrorIs(t, err, msgin.ErrPayloadType)
				assert.Empty(t, rep)
			},
		},
		{
			name: "exchange error propagates",
			fn: func(context.Context, msgin.Message[any]) (msgin.Message[any], error) {
				return msgin.Message[any]{}, context.DeadlineExceeded
			},
			assert: func(t *testing.T, rep string, err error) {
				assert.ErrorIs(t, err, context.DeadlineExceeded)
				assert.Empty(t, rep)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw, err := msgin.NewGateway[string, string](fakeExchange{fn: tt.fn})
			require.NoError(t, err)

			rep, err := gw.Request(t.Context(), "hello")

			tt.assert(t, rep, err)
		})
	}
}

func TestGateway_Request_freshCorrelationIDPerRequest(t *testing.T) {
	var gotIDs []string
	ex := fakeExchange{fn: func(_ context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
		id, _ := req.Headers().String(msgin.HeaderCorrelationID)
		gotIDs = append(gotIDs, id)
		return msgin.New[any]("ok"), nil
	}}
	gw, err := msgin.NewGateway[string, string](ex)
	require.NoError(t, err)

	_, err = gw.Request(t.Context(), "first")
	require.NoError(t, err)
	_, err = gw.Request(t.Context(), "second")
	require.NoError(t, err)

	require.Len(t, gotIDs, 2)
	assert.NotEmpty(t, gotIDs[0])
	assert.NotEmpty(t, gotIDs[1])
	assert.NotEqual(t, gotIDs[0], gotIDs[1])
}

// TestGateway_channelExchangeRoundTrip is the integration test: a real
// ChannelExchange over an echo flow that uppercases the payload, wired through
// Gateway.Request end to end.
func TestGateway_channelExchangeRoundTrip(t *testing.T) {
	request := msgin.NewDirectChannel()
	reply := msgin.NewDirectChannel()
	require.NoError(t, request.Subscribe(msgin.Chain(
		msgin.Activate(func(_ context.Context, m msgin.Message[string]) (msgin.Message[string], error) {
			return msgin.WithPayload(m, strings.ToUpper(m.Payload())), nil
		}),
		msgin.To(reply),
	)))
	ex, err := msgin.NewChannelExchange(request, reply)
	require.NoError(t, err)
	gw, err := msgin.NewGateway[string, string](ex)
	require.NoError(t, err)

	rep, err := gw.Request(t.Context(), "hello")

	require.NoError(t, err)
	assert.Equal(t, "HELLO", rep)
}

// TestOutboundGateway drives OutboundGateway through a tiny
// Chain(OutboundGateway(fake), To(collector)) so the reply forwarding and
// correlation id save/restore behavior are exercised end to end. reqCorr
// records the raw HeaderCorrelationID value the fake exchange saw on each
// request, for cases that assert on the fresh id minted per exchange.
func TestOutboundGateway(t *testing.T) {
	tests := []struct {
		name   string
		msgs   func() []msgin.Message[any]
		fn     func(reqCorr *[]any) func(ctx context.Context, req msgin.Message[any]) (msgin.Message[any], error)
		assert func(t *testing.T, col *collector, err error, reqCorr []any)
	}{
		{
			name: "forwards reply downstream",
			msgs: func() []msgin.Message[any] { return []msgin.Message[any]{msgin.New[any]("req")} },
			fn: func(reqCorr *[]any) func(context.Context, msgin.Message[any]) (msgin.Message[any], error) {
				return func(_ context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
					v, _ := req.Header(msgin.HeaderCorrelationID)
					*reqCorr = append(*reqCorr, v)
					return msgin.New[any]("reply-payload"), nil
				}
			},
			assert: func(t *testing.T, col *collector, err error, reqCorr []any) {
				require.NoError(t, err)
				require.Len(t, col.got, 1)
				assert.Equal(t, "reply-payload", col.got[0].Payload())
			},
		},
		{
			name: "restores a pre-existing correlation id",
			msgs: func() []msgin.Message[any] {
				return []msgin.Message[any]{msgin.New[any]("req").WithHeader(msgin.HeaderCorrelationID, "G")}
			},
			fn: func(reqCorr *[]any) func(context.Context, msgin.Message[any]) (msgin.Message[any], error) {
				return func(_ context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
					v, _ := req.Header(msgin.HeaderCorrelationID)
					*reqCorr = append(*reqCorr, v)
					return msgin.New[any]("reply"), nil
				}
			},
			assert: func(t *testing.T, col *collector, err error, reqCorr []any) {
				require.NoError(t, err)
				require.Len(t, reqCorr, 1)
				assert.NotEqual(t, "G", reqCorr[0], "want a fresh id on the request, not the incoming correlation id")
				require.Len(t, col.got, 1)
				gotCorr, ok := col.got[0].Header(msgin.HeaderCorrelationID)
				require.True(t, ok)
				assert.Equal(t, "G", gotCorr)
			},
		},
		{
			name: "strips correlation id when the incoming had none",
			msgs: func() []msgin.Message[any] { return []msgin.Message[any]{msgin.New[any]("req")} },
			fn: func(reqCorr *[]any) func(context.Context, msgin.Message[any]) (msgin.Message[any], error) {
				return func(_ context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
					v, _ := req.Header(msgin.HeaderCorrelationID)
					*reqCorr = append(*reqCorr, v)
					// Echo the fresh exchange id back on the reply, as a real
					// responder would (ChannelExchange demuxes on it) — this is
					// the realistic leak scenario the strip must guard against.
					return msgin.New[any]("reply").WithHeader(msgin.HeaderCorrelationID, v), nil
				}
			},
			assert: func(t *testing.T, col *collector, err error, reqCorr []any) {
				require.NoError(t, err)
				require.Len(t, reqCorr, 1)
				idStr, ok := reqCorr[0].(string)
				require.True(t, ok)
				assert.NotEmpty(t, idStr)
				require.Len(t, col.got, 1)
				_, hasCorr := col.got[0].Header(msgin.HeaderCorrelationID)
				assert.False(t, hasCorr)
			},
		},
		{
			// Audit G5: presence must be read via the raw Header accessor, not
			// Headers().String, which would conflate "absent" with "present but
			// non-string" and wrongly strip a present non-string id.
			name: "restores a present non-string correlation id",
			msgs: func() []msgin.Message[any] {
				return []msgin.Message[any]{msgin.New[any]("req").WithHeader(msgin.HeaderCorrelationID, 42)}
			},
			fn: func(reqCorr *[]any) func(context.Context, msgin.Message[any]) (msgin.Message[any], error) {
				return func(_ context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
					v, _ := req.Header(msgin.HeaderCorrelationID)
					*reqCorr = append(*reqCorr, v)
					return msgin.New[any]("reply"), nil
				}
			},
			assert: func(t *testing.T, col *collector, err error, reqCorr []any) {
				require.NoError(t, err)
				require.Len(t, reqCorr, 1)
				idStr, ok := reqCorr[0].(string)
				require.True(t, ok, "want the fake to see a STRING fresh id, got %T", reqCorr[0])
				assert.NotEmpty(t, idStr)
				require.Len(t, col.got, 1)
				gotCorr, ok := col.got[0].Header(msgin.HeaderCorrelationID)
				require.True(t, ok)
				assert.Equal(t, 42, gotCorr)
			},
		},
		{
			name: "unique keys for split-children sharing G",
			msgs: func() []msgin.Message[any] {
				return []msgin.Message[any]{
					msgin.New[any]("child1").WithHeader(msgin.HeaderCorrelationID, "G"),
					msgin.New[any]("child2").WithHeader(msgin.HeaderCorrelationID, "G"),
				}
			},
			fn: func(reqCorr *[]any) func(context.Context, msgin.Message[any]) (msgin.Message[any], error) {
				return func(_ context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
					v, _ := req.Header(msgin.HeaderCorrelationID)
					*reqCorr = append(*reqCorr, v)
					return msgin.New[any]("reply"), nil
				}
			},
			assert: func(t *testing.T, col *collector, err error, reqCorr []any) {
				require.NoError(t, err)
				require.Len(t, reqCorr, 2)
				assert.NotEqual(t, reqCorr[0], reqCorr[1], "want distinct fresh ids for children sharing a correlation id")
				require.Len(t, col.got, 2)
			},
		},
		{
			name: "exchange error propagates",
			msgs: func() []msgin.Message[any] { return []msgin.Message[any]{msgin.New[any]("req")} },
			fn: func(reqCorr *[]any) func(context.Context, msgin.Message[any]) (msgin.Message[any], error) {
				return func(context.Context, msgin.Message[any]) (msgin.Message[any], error) {
					return msgin.Message[any]{}, context.DeadlineExceeded
				}
			},
			assert: func(t *testing.T, col *collector, err error, reqCorr []any) {
				assert.ErrorIs(t, err, context.DeadlineExceeded)
				assert.Empty(t, col.got)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reqCorr []any
			ex := fakeExchange{fn: tt.fn(&reqCorr)}
			col := &collector{}
			handler := msgin.Chain(msgin.OutboundGateway(ex), msgin.To(col))

			var err error
			for _, m := range tt.msgs() {
				if e := handler.Handle(t.Context(), m); e != nil {
					err = e
				}
			}

			tt.assert(t, col, err, reqCorr)
		})
	}
}

// ExampleGateway_Request wires the inbound Gateway over a real ChannelExchange
// and a flow that doubles the payload, showing the whole request-reply
// round-trip: Request sends 21 in, the flow doubles it, and the correlated
// reply comes back as 42. The activator uses WithPayload (not New) so the
// reply keeps the request's HeaderCorrelationID — required for the exchange
// to demux the reply back to this waiter.
func ExampleGateway_Request() {
	request := msgin.NewDirectChannel()
	reply := msgin.NewDirectChannel()
	flow := msgin.Chain(
		msgin.Activate(func(_ context.Context, m msgin.Message[int]) (msgin.Message[int], error) {
			return msgin.WithPayload(m, m.Payload()*2), nil
		}),
		msgin.To(reply),
	)
	if err := request.Subscribe(flow); err != nil {
		panic(err)
	}

	ex, err := msgin.NewChannelExchange(request, reply)
	if err != nil {
		panic(err)
	}
	gw, err := msgin.NewGateway[int, int](ex)
	if err != nil {
		panic(err)
	}

	rep, err := gw.Request(context.Background(), 21)
	if err != nil {
		panic(err)
	}
	fmt.Println(rep)
	// Output: 42
}

// ExampleOutboundGateway drives a flow whose middle step is an OutboundGateway
// over a small in-process echo exchange, showing the reply forwarded
// downstream to the next step.
func ExampleOutboundGateway() {
	echo := fakeExchange{fn: func(_ context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
		return msgin.New[any](fmt.Sprintf("echo:%v", req.Payload())), nil
	}}

	flow := msgin.Chain(
		msgin.OutboundGateway(echo),
		msgin.Consume(func(_ context.Context, m msgin.Message[string]) error {
			fmt.Println(m.Payload())
			return nil
		}),
	)

	if err := flow.Handle(context.Background(), msgin.New[any]("hello")); err != nil {
		panic(err)
	}
	// Output: echo:hello
}
