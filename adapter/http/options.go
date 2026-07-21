package msghttp

import (
	"io"
	"log/slog"
	"net/http"
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
// NewConfig; its fields are unexported, so a Config only ever comes from
// NewConfig plus the Option surface below — there is no zero-value
// construction path that skips validation/defaulting.
type Config struct {
	maxBodyBytes     int64
	maxBodyBytesSet  bool // distinguishes explicit WithMaxBodyBytes(n<=0) (rejected) from unset (default)
	successStatus    int
	successStatusSet bool // distinguishes explicit WithSuccessStatus(out-of-range) (rejected) from unset (default)
	correlationID    func(*http.Request) string
	requestHeaders   []string
	responseHeaders  []string
	errorStatus      func(error) int
	logger           *slog.Logger
}

// Option configures a Config built by NewConfig.
type Option func(*Config)

// WithMaxBodyBytes caps the number of request-body bytes DecodeRequest reads
// (via http.MaxBytesReader) before it is converted into a message payload.
// Default: defaultMaxBodyBytes (1 MiB) — the flood defense at the untrusted
// HTTP boundary (CLAUDE.md "Production robustness").
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

// WithCorrelationID overrides how DecodeRequest resolves the message's
// msgin.HeaderCorrelationID: f is called with the inbound *http.Request, and
// its return value is used verbatim when non-empty. Default: nil, meaning the
// correlation id is always the message's own server-minted ID() —
// DecodeRequest never trusts a client-supplied value unless the caller
// explicitly opts in here (CLAUDE.md untrusted-input boundary: correlation id
// defaults to a server-minted id, never a client value unless opted in). Even
// with f set, an empty string it returns falls back to the server-minted id
// rather than producing an empty correlation key.
//
// A nil f is a no-op: the default (server-minted) resolution stays in place,
// even if WithCorrelationID(nil) is applied after an earlier WithCorrelationID
// call in the same Option list — a nil f never clobbers an already-set
// resolver (mirrors WithLogger's nil-guard).
func WithCorrelationID(f func(*http.Request) string) Option {
	return func(c *Config) {
		if f != nil {
			c.correlationID = f
		}
	}
}

// WithRequestHeaders sets the allow-list of request header names
// DecodeRequest copies onto the decoded message (matched case-insensitively
// via http.Header.Get). Default: empty — NO request header is copied unless
// explicitly allow-listed here, because an unbounded header allow-list is
// itself an untrusted-input surface (CLAUDE.md untrusted-input boundary); the
// safe default is opt-in, not implicit passthrough. An allow-list entry
// naming a reserved msgin.* header is defensively ignored by DecodeRequest —
// listing one here can never let a client forge a core header. That strip
// matches case-SENSITIVELY against the reserved constant's exact lowercase
// spelling (e.g. "msgin.delivery-count"); this is not attacker-exploitable
// (the allow-list is trusted operator config, not client input, and the
// incoming header lookup itself is case-insensitive via http.Header.Get), but
// an allow-list entry must match a reserved constant's exact lowercase
// casing to be stripped.
func WithRequestHeaders(headers ...string) Option {
	return func(c *Config) { c.requestHeaders = headers }
}

// WithResponseHeaders sets the allow-list of message header names
// EncodeResponse emits as HTTP response headers (values CRLF-sanitized before
// being written). Default: empty — no message header is echoed as a response
// header unless explicitly allow-listed here, matching WithRequestHeaders'
// opt-in-only default.
func WithResponseHeaders(headers ...string) Option {
	return func(c *Config) { c.responseHeaders = headers }
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
// Default: nil, resolved by NewConfig to the package's defaultErrorStatus — a
// mapping over the msgin.*/msghttp.* sentinels and an oversize
// *http.MaxBytesError, with any unclassified error mapped to 500.
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
		cfg.errorStatus = defaultErrorStatus
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
