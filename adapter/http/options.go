package msghttp

import (
	"io"
	"log/slog"
	"net/http"
	"slices"
)

// defaultMaxBodyBytes bounds the number of request-body bytes DecodeRequest
// reads before converting the body into a message payload, applied when
// WithMaxBodyBytes is unset. 1 MiB is a fail-safe default for an UNTRUSTED
// HTTP boundary (CLAUDE.md "Production robustness" / "Sensible defaults"):
// generous enough for typical JSON/text request bodies while bounding the
// memory cost of a single request. Override it explicitly via
// WithMaxBodyBytes for legitimately larger bodies — the library cannot guess
// a caller's legitimate payload size, so the default errs conservative
// rather than permissive.
const defaultMaxBodyBytes int64 = 1 << 20 // 1 MiB

// Config holds the resolved settings DecodeRequest and EncodeResponse (and
// the inbound handler cores built on top of them) use to convert between an
// *http.Request/http.ResponseWriter and a msgin.Message[any]. Build one with
// NewConfig: only NewConfig validates the options and reports a bad value as a
// typed construction error.
//
// Config is exported with unexported fields, so a hand-built &Config{} (or a
// nil *Config) is nonetheless constructible from any package and can reach
// every exported consumer here. That is NOT a supported way to configure the
// adapter — no validation runs — but it must never panic on caller input
// (CLAUDE.md), so each setting is read through an internal accessor that
// back-fills the documented default for a zero/nil field: a 1 MiB body cap, a
// 202 async success status, DefaultErrorStatus, a discard logger, and empty
// header allow-lists.
type Config struct {
	maxBodyBytes       int64
	maxBodyBytesSet    bool // distinguishes explicit WithMaxBodyBytes(n<=0) (rejected) from unset (default)
	successStatus      int
	successStatusSet   bool // distinguishes explicit WithSuccessStatus(out-of-range) (rejected) from unset (default)
	correlationID      func(*http.Request) string
	trustedCorrelation func(*http.Request) string
	requestHeaders     []string
	responseHeaders    []string
	errorStatus        func(error) int
	logger             *slog.Logger
}

// maxBody is the request-body cap to apply, back-filling defaultMaxBodyBytes
// for a nil or hand-built Config whose cap is unset (0) or nonsensical (<0).
// NewConfig rejects an explicit non-positive WithMaxBodyBytes outright, so
// this fallback only ever fires for a Config that skipped NewConfig.
func (c *Config) maxBody() int64 {
	if c == nil || c.maxBodyBytes <= 0 {
		return defaultMaxBodyBytes
	}
	return c.maxBodyBytes
}

// successStatusOrDefault is the async (I1) success status, back-filling
// http.StatusAccepted for a nil or hand-built Config — writing a zero status
// would panic inside net/http.
func (c *Config) successStatusOrDefault() int {
	if c == nil || c.successStatus < 100 || c.successStatus > 599 {
		return http.StatusAccepted
	}
	return c.successStatus
}

// log is the logger the handler cores emit diagnostics through, back-filling
// the discard logger for a nil or hand-built Config — never slog.Default(),
// so the adapter never logs to a package global.
func (c *Config) log() *slog.Logger {
	if c == nil || c.logger == nil {
		return discardLogger()
	}
	return c.logger
}

// errStatus is the error->status mapper, back-filling DefaultErrorStatus for a
// nil or hand-built Config.
func (c *Config) errStatus() func(error) int {
	if c == nil || c.errorStatus == nil {
		return DefaultErrorStatus
	}
	return c.errorStatus
}

// advisoryCorrelationID is the WithAdvisoryCorrelationID resolver, or nil.
func (c *Config) advisoryCorrelationID() func(*http.Request) string {
	if c == nil {
		return nil
	}
	return c.correlationID
}

// trustedCorrelationID is the WithTrustedCorrelationID resolver, or nil.
func (c *Config) trustedCorrelationID() func(*http.Request) string {
	if c == nil {
		return nil
	}
	return c.trustedCorrelation
}

// allowedRequestHeaders is the request-header allow-list, empty for a nil
// Config.
func (c *Config) allowedRequestHeaders() []string {
	if c == nil {
		return nil
	}
	return c.requestHeaders
}

// allowedResponseHeaders is the response-header allow-list, empty for a nil
// Config.
func (c *Config) allowedResponseHeaders() []string {
	if c == nil {
		return nil
	}
	return c.responseHeaders
}

// Option configures a Config built by NewConfig.
type Option func(*Config)

// WithMaxBodyBytes caps the number of request-body bytes DecodeRequest reads
// (via http.MaxBytesReader) before it is converted into a message payload.
// Default: defaultMaxBodyBytes (1 MiB) — the flood defense at the untrusted
// HTTP boundary (CLAUDE.md "Production robustness").
//
// CAVEAT: it bounds the BODY only. The request line copied onto every decoded
// message (http.path, http.query) and any allow-listed request header are
// bounded instead by the server's http.Server.MaxHeaderBytes (1 MiB by
// default) — and on the async path (ServeAsync) that memory is retained for as
// long as the queued message lives. Lower MaxHeaderBytes alongside this option
// if the header surface matters to your threat model.
//
// n MUST be > 0: NewConfig returns ErrInvalidMaxBodyBytes for an explicit
// n <= 0, so a caller mistake (e.g. an uninitialized zero value passed
// through) is a construction error rather than a silently-disabled cap.
// Leaving this option unset (rather than calling it with 0) is how a caller
// asks for the default.
func WithMaxBodyBytes(n int64) Option {
	return func(c *Config) {
		c.maxBodyBytes = n
		c.maxBodyBytesSet = true
	}
}

// WithAdvisoryCorrelationID sets a resolver for the client's ADVISORY
// correlation id: f is called with the inbound *http.Request and, when it
// returns a non-empty value, DecodeRequest records that value on the
// message's non-reserved "http.correlation-id" header for the flow to read,
// log, or echo back. Default: nil — no advisory header is added.
//
// It does NOT influence msgin.HeaderCorrelationID, the key a
// msgin.RequestReplyExchange correlates the reply on: that key is always the
// message's own server-minted, 128-bit-CSPRNG ID(). Routing a client-supplied
// string into the exchange key would let one client read another's reply (see
// WithTrustedCorrelationID, which is the explicit, deliberately-awkward opt-in
// for callers who genuinely need client-keyed correlation).
//
// A nil f is a no-op: it never clobbers a resolver set by an earlier
// WithAdvisoryCorrelationID in the same Option list (mirrors WithLogger's
// nil-guard).
func WithAdvisoryCorrelationID(f func(*http.Request) string) Option {
	return func(c *Config) {
		if f != nil {
			c.correlationID = f
		}
	}
}

// WithTrustedCorrelationID makes a CLIENT-SUPPLIED value the exchange
// correlation key: f is called with the inbound *http.Request and, when it
// returns a non-empty value, DecodeRequest uses it verbatim as
// msgin.HeaderCorrelationID — the key msgin.RequestReplyExchange matches the
// reply on. Default: nil, i.e. the key is always the server-minted msg.ID().
//
// # SECURITY WARNING — read before using this
//
// The correlation key is the ONLY thing that binds a reply to the request
// waiting for it, and it must be unique across the exchange's whole lifetime,
// not merely across concurrent requests. Trusting a client value therefore
// hands clients control of that binding:
//
//   - REPLY HIJACK: if a peer can GUESS or REPLAY another client's value, its
//     request registers the waiter that the victim's reply is delivered to —
//     it reads the victim's response body.
//   - TARGETED DENIAL: a peer that pre-registers a victim's value first makes
//     every victim request fail with msgin.ErrDuplicateCorrelation (409).
//
// Only enable this when the resolved values are UNGUESSABLE (high-entropy,
// not a sequential or user-derived id such as "user-42-req-1") and SINGLE-USE
// per client, and when the endpoint is authenticated so a value can be scoped
// to its owner. If you merely want the client's id visible to the flow, use
// WithAdvisoryCorrelationID instead — it carries the value without giving it
// any authority.
//
// The two options are orthogonal and may both be set:
// WithTrustedCorrelationID decides the exchange key (msgin.HeaderCorrelationID),
// WithAdvisoryCorrelationID decides the advisory "http.correlation-id" header,
// and neither overrides the other. An empty value from f falls back to the
// server-minted id rather than producing an empty correlation key.
//
// A nil f is a no-op: it never clobbers a resolver set by an earlier
// WithTrustedCorrelationID in the same Option list.
func WithTrustedCorrelationID(f func(*http.Request) string) Option {
	return func(c *Config) {
		if f != nil {
			c.trustedCorrelation = f
		}
	}
}

// WithRequestHeaders sets the allow-list of request header names
// DecodeRequest copies onto the decoded message (matched case-insensitively
// via http.Header.Get, and only the FIRST value of a multi-valued header is
// copied). Default: empty — NO request header is copied unless explicitly
// allow-listed here, because an unbounded header allow-list is itself an
// untrusted-input surface (CLAUDE.md untrusted-input boundary); the safe
// default is opt-in, not implicit passthrough.
//
// An allow-list entry naming a reserved msgin.* header is defensively ignored
// by DecodeRequest — listing one here can never let a client forge a core
// header. That strip is case-INSENSITIVE, so "MSGIN.delivery-count" is
// stripped exactly like "msgin.delivery-count".
//
// The names are cloned: a later mutation of the caller's own slice cannot
// silently rewrite this security allow-list.
func WithRequestHeaders(headers ...string) Option {
	return func(c *Config) { c.requestHeaders = slices.Clone(headers) }
}

// WithResponseHeaders sets the allow-list of message header names
// EncodeResponse emits as HTTP response headers (values CRLF-sanitized before
// being written). Default: empty — no message header is echoed as a response
// header unless explicitly allow-listed here, matching WithRequestHeaders'
// opt-in-only default.
//
// CAUTION — this list is NOT guarded against the reserved msgin.* namespace,
// asymmetrically with WithRequestHeaders: the request side strips reserved
// names because they come from an untrusted client, whereas this list is
// trusted operator config and an operator may legitimately want to expose,
// say, a correlation id to their own clients. Listing msgin.correlation-id,
// msgin.id, msgin.delivery-count or similar therefore DOES leak internal flow
// metadata to every caller of the endpoint — list only what you mean to
// publish.
//
// Matching is case-SENSITIVE (an exact message-header map lookup), unlike
// WithRequestHeaders' case-insensitive http.Header.Get matching: name the
// message header with the exact casing the flow set it with.
//
// Content-Type and X-Content-Type-Options are set by EncodeResponse after this
// allow-list is applied, so an entry naming either cannot override them.
//
// The names are cloned: a later mutation of the caller's own slice cannot
// silently rewrite this allow-list.
func WithResponseHeaders(headers ...string) Option {
	return func(c *Config) { c.responseHeaders = slices.Clone(headers) }
}

// WithSuccessStatus overrides the HTTP status the async inbound handler (I1,
// ServeAsync) writes on a successful decode+send. Default: http.StatusAccepted
// (202) — "accepted for asynchronous processing" is the honest status for a
// fire-and-forget inbound adapter that has handed the message to a channel
// but not yet processed it.
//
// This option governs the ASYNC inbound handler ONLY. It does NOT apply to
// the sync request-reply gateway handler (I2, ServeGateway): a gateway
// response carries a synchronous reply body, so EncodeResponse always writes
// 200 regardless of this setting.
//
// code MUST be in [100,599]: NewConfig returns ErrInvalidStatusCode for an
// explicit value outside that range. Leaving this option unset (rather than
// calling it with an invalid code) is how a caller asks for the default.
func WithSuccessStatus(code int) Option {
	return func(c *Config) {
		c.successStatus = code
		c.successStatusSet = true
	}
}

// WithErrorStatus overrides how a decode/send/exchange error maps to an HTTP
// status for both inbound handler cores (I1 ServeAsync and I2 ServeGateway).
// Default: nil, resolved to the package's DefaultErrorStatus — a mapping over
// the msgin.*/msghttp.* sentinels and an oversize *http.MaxBytesError, with
// any unclassified error mapped to 500.
//
// A custom mapper should handle only the cases it cares about and DELEGATE the
// rest to the exported DefaultErrorStatus, so it keeps the 413-vs-400
// discrimination (via ErrDecodeRequest / *http.MaxBytesError) and the gateway
// sentinel mapping it would otherwise have to reimplement:
//
//	msghttp.WithErrorStatus(func(err error) int {
//		if errors.Is(err, myapp.ErrForbidden) {
//			return http.StatusForbidden
//		}
//		return msghttp.DefaultErrorStatus(err)
//	})
//
// A nil f is a no-op: the default mapping stays in place, even if
// WithErrorStatus(nil) is applied after an earlier WithErrorStatus call in
// the same Option list — a nil f never clobbers an already-set mapper
// (mirrors WithLogger's nil-guard).
func WithErrorStatus(f func(error) int) Option {
	return func(c *Config) {
		if f != nil {
			c.errorStatus = f
		}
	}
}

// WithLogger injects the structured logger the inbound handler cores use for
// their WARN/ERROR diagnostics. Default: a discard logger — the adapter never
// logs to a package global (CLAUDE.md "Observability hooks, not global
// state").
//
// A nil logger is a no-op: the discard default stays in place.
func WithLogger(l *slog.Logger) Option {
	return func(c *Config) {
		if l != nil {
			c.logger = l
		}
	}
}

// NewConfig validates opts and builds a Config, resolving the documented
// default for any option left unset. WithMaxBodyBytes and WithSuccessStatus
// use the set-flag pattern (mirrors msgin.WithMaxInFlight/WithAttemptTTL): an
// unset option resolves to its default, while an explicitly-set-but-invalid
// value is a construction error (ErrInvalidMaxBodyBytes /
// ErrInvalidStatusCode) rather than a silently-substituted default.
func NewConfig(opts ...Option) (*Config, error) {
	cfg := &Config{}
	for _, opt := range opts {
		opt(cfg)
	}

	if !cfg.maxBodyBytesSet {
		cfg.maxBodyBytes = defaultMaxBodyBytes
	} else if cfg.maxBodyBytes <= 0 {
		return nil, ErrInvalidMaxBodyBytes
	}

	if !cfg.successStatusSet {
		cfg.successStatus = http.StatusAccepted
	} else if cfg.successStatus < 100 || cfg.successStatus > 599 {
		return nil, ErrInvalidStatusCode
	}

	if cfg.errorStatus == nil {
		cfg.errorStatus = DefaultErrorStatus
	}
	if cfg.logger == nil {
		cfg.logger = discardLogger()
	}

	return cfg, nil
}

// discardLogger is the default logger: it drops every record (mirrors
// adapter/database/sql and adapter/cron's own discardLogger helper — never
// slog.Default(), so the adapter never logs to a package global).
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
