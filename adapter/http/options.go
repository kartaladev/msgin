package msghttp

import (
	"io"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/jonboulle/clockwork"
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

// defaultMaxResponseBytes bounds the number of response-body bytes an Exchange
// reads into the reply payload when WithMaxResponseBytes is unset, and also
// caps O1's post-Send connection-reuse drain (round-2 audit F6). 1 MiB mirrors
// defaultMaxBodyBytes: the remote endpoint is untrusted, so the default errs
// conservative rather than permissive. Override it via WithMaxResponseBytes.
const defaultMaxResponseBytes int64 = 1 << 20 // 1 MiB

// defaultMaxEventBytes bounds the number of bytes NewSSEParser's SSEParser
// buffers for a single Server-Sent Event — the max of the current
// in-progress line's byte count and the accumulated "data" buffer's byte
// count, the only two things that actually buffer while parsing a
// text/event-stream — when WithMaxEventBytes is unset. 1 MiB mirrors
// defaultMaxBodyBytes/defaultMaxResponseBytes: the remote SSE stream is
// untrusted input (CLAUDE.md "Production robustness"), generous enough for a
// typical event payload while bounding the memory an endless unterminated
// line or a pathologically multi-"data:"-line event can force the parser to
// hold. Override it explicitly via WithMaxEventBytes for legitimately larger
// events — the library cannot guess a caller's legitimate event size, so the
// default errs conservative rather than permissive.
const defaultMaxEventBytes int64 = 1 << 20 // 1 MiB

// defaultMaxConnections bounds the number of concurrently registered SSE
// connections NewSSEServer's http.Handler accepts, applied when
// WithMaxConnections is unset. It is the connection-exhaustion guard: past
// this many open connections the handler responds 503 rather than
// registering an unbounded number of long-lived streaming responses. The
// cap is PROCESS-GLOBAL, not per-IP — a caller who needs per-IP/per-tenant
// limiting applies it at their own reverse proxy or load balancer, not
// through this option. Override it explicitly via WithMaxConnections.
const defaultMaxConnections = 1024

// defaultConnectionBuffer sizes the per-connection buffered event channel
// each registered SSE connection's writer goroutine drains, applied when
// WithConnectionBuffer is unset. 16 is the slow-client isolation bound: a
// connection whose reader/network cannot keep up fills only ITS OWN buffer,
// never blocking Send for any other connection or for the flow that calls
// it. Override it explicitly via WithConnectionBuffer.
const defaultConnectionBuffer = 16

// defaultWriteTimeout is the per-write OS deadline NewSSEServer's writer
// goroutine applies (via http.ResponseController.SetWriteDeadline) before
// every event Write/Flush, applied when WithWriteTimeout is unset. SSE
// forces the underlying http.Server's WriteTimeout to 0 (a streaming
// response has no fixed total duration), so this PER-WRITE deadline is what
// actually reaps a writer goroutine wedged inside Write on a stalled
// reader's socket — without it, neither Close's join nor
// WithSlowClientPolicy's disconnect could ever interrupt a blocked Write.
// It is a REAL, physical-time OS deadline, distinct from the logical
// WithHeartbeat interval: heartbeat decides how often an otherwise-idle
// connection speaks, this decides how long any single write may take
// before being aborted. 30s is generous enough that a healthy connection's
// frame write never trips it, while still bounding how long a truly
// stalled connection can wedge its writer goroutine. Override it explicitly
// via WithWriteTimeout.
const defaultWriteTimeout = 30 * time.Second

// defaultHTTPClientTimeout is the timeout on the default outbound *http.Client.
// It is finite ON PURPOSE: http.DefaultClient has NO timeout at all, so a
// stalled endpoint would otherwise block a producer goroutine indefinitely.
//
// This is the per-attempt I/O bound — it caps a SINGLE client.Do, the one thing
// ADR 0025 §1.1 could not bound from the producer side (the producer treats one
// adapter call as opaque and unbounded). It is therefore orthogonal to, and
// well below, the producer's retry BUDGET (2m) and Retry-After CAP (60s), which
// govern the sum and spacing of retries ACROSS attempts, not the duration of
// any one attempt. 30s bounds one hop; 2m/60s bound the retry envelope around
// many hops — no interaction, no ordering constraint between them.
const defaultHTTPClientTimeout = 30 * time.Second

// errorBodyExcerptMax caps the number of remote response-body bytes embedded in
// StatusError.Excerpt when WithErrorBodyExcerpt() is set (decision 5). It is a
// DoS-amplification bound (the read is capped before reading), not merely an
// output-length bound.
const errorBodyExcerptMax = 256

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

	// Outbound (O1 NewOutbound / O2 NewExchange) settings — see the WithX
	// options below. NewConfig back-fills httpClient (a client with a 30s
	// timeout) and clock (a real clock), and validates maxResponseBytes via the
	// set-flag pattern. A hand-built &Config{} reaching an outbound call reads
	// httpClient/maxResponseBytes directly (there is intentionally no
	// nil-fallback accessor for them — an unsupported hand-built &Outbound{}
	// nil-derefs, exactly like a hand-built &msgin.ChannelExchange{}), as does the
	// reply-header allow-list (reached only through Exchange). The outbound-request
	// header allow-list and clock keep their nil-safe accessors, because they are
	// reachable through the exported EncodeRequest/ClassifyResponse with a nil cfg.
	httpClient          *http.Client
	followRedirects     bool
	outboundHeaders     []string
	replyHeaders        []string
	maxResponseBytes    int64
	maxResponseBytesSet bool // distinguishes explicit WithMaxResponseBytes(n<=0) (rejected) from unset (default)
	clock               clockwork.Clock
	errorBodyExcerpt    bool

	// eventName is the SSE default "event:" field name SSEEventFromMessage
	// falls back to when a message carries no HeaderSSEEventName header.
	// eventNameSet distinguishes an explicit WithEventName call (validated by
	// NewConfig) from unset (default "").
	eventName    string
	eventNameSet bool

	// maxEventBytes is the SSEParser per-event byte cap (WithMaxEventBytes).
	// maxEventBytesSet distinguishes an explicit WithMaxEventBytes(n<=0)
	// (rejected) from unset (default defaultMaxEventBytes), mirroring
	// maxBodyBytesSet/maxResponseBytesSet.
	maxEventBytes    int64
	maxEventBytesSet bool

	// maxConnections through sseClock are the SSE server construction-time
	// settings NewSSEServer reads — see each WithX option's godoc below for
	// its default and rationale. Every *Set flag mirrors the
	// maxBodyBytesSet/maxResponseBytesSet pattern: it distinguishes an
	// explicit-but-invalid value (a construction error) from unset (the
	// documented default).
	maxConnections      int
	maxConnectionsSet   bool
	connectionBuffer    int
	connectionBufferSet bool
	slowClientPolicy    SlowClientPolicy
	slowClientPolicySet bool
	replayBuffer        int
	replayBufferSet     bool
	heartbeat           time.Duration
	heartbeatSet        bool
	writeTimeout        time.Duration
	writeTimeoutSet     bool
	sseClock            clockwork.Clock
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

// allowedOutboundHeaders is the outbound request-header allow-list, empty for a
// nil Config. A plain nil-guard mirroring allowedRequestHeaders: the
// reserved-msgin.* strip is applied by EncodeRequest at use (decision 4
// symmetry), exactly as DecodeRequest strips the inbound allow-list — never in
// the accessor, so the strip lives in one place and is covered once.
func (c *Config) allowedOutboundHeaders() []string {
	if c == nil {
		return nil
	}
	return c.outboundHeaders
}

// allowedReplyHeaders is the outbound reply-header allow-list an Exchange copies
// from the remote response onto the reply message. The reserved-msgin.* strip
// (INV-2) is applied where the reply message is built (buildReply), so the remote
// server can never forge a core header.
//
// Unlike allowedOutboundHeaders/clockOrDefault, this field is read DIRECTLY with
// no nil-Config fallback: its only caller is buildReply, reached only through
// Exchange, whose cfg is always the one NewConfig produced. A nil-guard here
// would be blackbox-unreachable dead code (no exported function passes a nil
// *Config to it), so it is omitted — a hand-built &Exchange{} nil-derefs, exactly
// as it does on maxResponseBytes and the no-follow client.
func (c *Config) allowedReplyHeaders() []string {
	return c.replyHeaders
}

// clockOrDefault is the clock the outbound adapters use for time-dependent
// parsing — currently only the HTTP-date form of a Retry-After response header.
// It back-fills a real clock for a nil or hand-built Config, so ClassifyResponse
// never nil-calls it (the Config nil-safety contract).
func (c *Config) clockOrDefault() clockwork.Clock {
	if c == nil || c.clock == nil {
		return clockwork.NewRealClock()
	}
	return c.clock
}

// errorBodyExcerptEnabled reports whether WithErrorBodyExcerpt() was set. A nil
// or hand-built Config returns false, so the default posture stays INV-4's "code
// only" — no remote-controlled bytes reach the error string unless the caller
// opted in.
func (c *Config) errorBodyExcerptEnabled() bool {
	return c != nil && c.errorBodyExcerpt
}

// eventNameOrDefault is the fallback SSE "event:" field name
// SSEEventFromMessage uses when the message carries no HeaderSSEEventName
// header. It back-fills "" for a nil or hand-built Config (the Config
// nil-safety contract) — an empty name means no event: line is emitted and
// the client dispatches the frame as the SSE default event "message".
func (c *Config) eventNameOrDefault() string {
	if c == nil {
		return ""
	}
	return c.eventName
}

// The accessors below expose the resolved SSE-server settings NewSSEServer
// (adapter/http/sse_server.go) reads. Unlike the exported-consumer accessors
// above, they read cfg.<field> DIRECTLY with no nil/zero back-fill: their only
// caller is a *SSEServer whose cfg always came from NewConfig (NewSSEServer's
// constructor), which resolved every default and rejected every explicit
// invalid value, so a zero/nil field can never reach them on any supported
// path. They are named apart from the identically-named struct fields
// (maxConnections, connectionBuffer, heartbeat, writeTimeout, sseClock,
// slowClientPolicy, replayBuffer) only because Go forbids a method and a field
// sharing a name on one type — the accessor is the field, resolved.

// maxConns is the resolved WithMaxConnections cap the SSE server enforces
// before registering a new connection (default 1024).
func (c *Config) maxConns() int { return c.maxConnections }

// connBuffer is the resolved WithConnectionBuffer size of each connection's
// buffered frame channel (default 16).
func (c *Config) connBuffer() int { return c.connectionBuffer }

// heartbeatInterval is the resolved WithHeartbeat interval; 0 means heartbeat
// is off (the default), and the writer's heartbeat select arm is a nil channel.
func (c *Config) heartbeatInterval() time.Duration { return c.heartbeat }

// perWriteTimeout is the resolved WithWriteTimeout per-write OS deadline the
// writer applies before every Write/Flush (default 30s). It is a real,
// physical-time bound — the socket deadline cannot be faked — distinct from the
// logical heartbeat interval driven by streamClock.
func (c *Config) perWriteTimeout() time.Duration { return c.writeTimeout }

// streamClock is the resolved WithSSEClock the writer uses for heartbeat
// interval timing (default a real clock). It drives the LOGICAL heartbeat
// cadence only; the per-write OS deadline uses real wall-clock time regardless.
func (c *Config) streamClock() clockwork.Clock { return c.sseClock }

// slowPolicy is the resolved WithSlowClientPolicy the SSE server's Send applies
// to a connection whose per-connection buffer is full (default SlowClientDrop).
func (c *Config) slowPolicy() SlowClientPolicy { return c.slowClientPolicy }

// replaySize is the resolved WithReplayBuffer ring depth: the number of most
// recent id-bearing events the SSE server retains for reconnect replay. 0 means
// replay is OFF (the default) — no ring is kept and Last-Event-ID is ignored.
func (c *Config) replaySize() int { return c.replayBuffer }

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
// A waiter is released early — freeing its id for reuse, and with it the
// hijack window above — by a reply timeout, a ctx cancel, a request-channel
// send error, OR A PANICKING FLOW HANDLER (Spec 012 / ADR 0022 Addendum A4).
// The panic arm is the newest of the four: it used to leak the slot and so
// fail closed on reuse.
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
// msgin.message-id, msgin.delivery-count or similar therefore DOES leak internal flow
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

// WithHTTPClient sets the *http.Client the outbound adapters (NewOutbound,
// NewExchange) use for their single per-attempt request. Default: a client with
// a 30s timeout (defaultHTTPClientTimeout) — http.DefaultClient has NO timeout
// at all, so relying on it would let a stalled endpoint block a producer
// goroutine indefinitely.
//
// The caller's client is never mutated. When it leaves CheckRedirect nil
// (net/http's follow-up-to-10-redirects default), a SHALLOW COPY is taken and a
// no-follow CheckRedirect is installed on the copy; its Transport and Jar stay
// shared by pointer so connection pooling and cookies are preserved. A client
// that sets its own CheckRedirect is used verbatim — the caller's explicit
// choice wins.
//
// SECURITY (INV-1 escape): supplying a client whose CheckRedirect FOLLOWS
// redirects re-opens the SSRF the no-follow default closes — a 302 from the
// configured host can steer the request to an internal address (e.g. a cloud
// metadata endpoint) and return its bytes into the flow, and a 307/308 replays
// the POST body and every allow-listed header to the new host. The caller owns
// that risk; it is documented here at the option, not only in an open-decision
// list. WithFollowRedirects opts out the same way via a flag.
//
// A nil client is a no-op: the default stays in place (mirrors WithLogger).
func WithHTTPClient(c *http.Client) Option {
	return func(cfg *Config) {
		if c != nil {
			cfg.httpClient = c
		}
	}
}

// WithFollowRedirects opts OUT of the outbound no-follow redirect policy,
// letting the client follow up to 10 redirects as net/http does by default.
//
// SECURITY WARNING: by default msgin installs a no-follow CheckRedirect so a
// response cannot steer a request to a host other than the one validated at
// construction. Enabling this re-opens SSRF: a 302 can redirect an outbound
// request to http://169.254.169.254/… and return cloud instance-metadata
// credentials into the flow as the reply payload, and a 307/308 replays the
// POST body and every allow-listed header to the attacker-chosen host. Only
// enable this when every host the endpoint can redirect to is as trusted as the
// endpoint itself.
func WithFollowRedirects() Option {
	return func(c *Config) { c.followRedirects = true }
}

// WithOutboundHeaders sets the allow-list of MESSAGE header names EncodeRequest
// copies onto the outbound HTTP request (values CRLF-sanitized before being
// written). Default: empty — no message header is sent unless allow-listed
// here, matching WithRequestHeaders' opt-in-only default.
//
// A reserved msgin.* entry is defensively DROPPED (decision 4 — symmetry with
// WithOutboundReplyHeaders and DecodeRequest): an entry whose name, lowercased,
// carries the msgin. prefix is silently ignored, so internal flow metadata
// (msgin.correlation-id, msgin.message-id, …) is never published to the remote
// endpoint through this option. The reserved-name GUARD is case-INSENSITIVE — a
// security filter must not be bypassable by casing, so "MSGIN.Foo" is dropped
// too — while the message-header VALUE lookup stays case-SENSITIVE (an exact
// msg.Headers().String map lookup, since a functional key must match the header
// exactly as the flow stored it).
//
// Content-Type is written by EncodeRequest AFTER this allow-list, so an entry
// naming it can never override the resolved request Content-Type.
//
// The names are cloned: a later mutation of the caller's own slice cannot
// silently rewrite this security allow-list.
func WithOutboundHeaders(names ...string) Option {
	return func(c *Config) { c.outboundHeaders = slices.Clone(names) }
}

// WithOutboundReplyHeaders sets the allow-list of HTTP RESPONSE header names an
// Exchange copies from the remote reply onto the reply message (matched
// case-insensitively via http.Header.Get, first value only). Default: empty.
//
// This governs the OUTBOUND request-reply direction — do not confuse it with
// WithResponseHeaders, which allow-lists MESSAGE headers emitted on an INBOUND
// server's response. A reserved msgin.* entry is defensively dropped: the
// remote server is untrusted input exactly as an inbound client is (INV-2), so
// it can never be made to forge a core header such as msgin.content-type.
//
// The names are cloned: a later mutation of the caller's own slice cannot
// silently rewrite this allow-list.
func WithOutboundReplyHeaders(names ...string) Option {
	return func(c *Config) { c.replyHeaders = slices.Clone(names) }
}

// WithMaxResponseBytes caps the number of response-body bytes an Exchange reads
// into the reply payload. Default: defaultMaxResponseBytes (1 MiB). Name it
// apart from WithMaxBodyBytes: WithMaxBodyBytes caps an inbound REQUEST body,
// WithMaxResponseBytes an outbound RESPONSE body.
//
// CAVEAT: it bounds the BODY only. Response HEADERS are bounded instead by
// http.Transport.MaxResponseHeaderBytes (~10 MiB by default) — lower it
// alongside this option if the reply-header surface matters to your threat
// model. The same cap also bounds O1's post-Send connection-reuse drain, not
// only the O2 reply body: raising it for large O2 replies also raises how much
// of an oversized O1 webhook response is drained before the connection returns
// to the pool. With WithErrorBodyExcerpt() enabled, a non-2xx O1 response
// additionally consumes up to errorBodyExcerptMax (256) bytes for the excerpt —
// a separate, bounded budget on top of this drain.
//
// n MUST be > 0: NewConfig returns ErrInvalidMaxResponseBytes for an explicit
// n <= 0 (the set-flag pattern distinguishes "unset" → default from "explicit
// invalid"). Leaving this option unset is how a caller asks for the default.
func WithMaxResponseBytes(n int64) Option {
	return func(c *Config) {
		c.maxResponseBytes = n
		c.maxResponseBytesSet = true
	}
}

// WithOutboundClock injects the clock the outbound adapters use for any
// time-dependent parsing — currently only the HTTP-date form of a Retry-After
// response header. Default: clockwork.NewRealClock().
//
// Named apart from msgin.WithClock (a MessageOption) and the per-component
// WithProducerClock/WithExchangeClock, per the repo's distinct-clock-name-per-
// component convention.
//
// A nil clock is a no-op: the real-clock default stays in place (mirrors
// WithLogger).
func WithOutboundClock(clk clockwork.Clock) Option {
	return func(c *Config) {
		if clk != nil {
			c.clock = clk
		}
	}
}

// WithErrorBodyExcerpt makes a non-2xx StatusError carry a bounded, sanitized
// excerpt of the remote response body. Default: OFF — a StatusError carries the
// status code ONLY, so no remote-controlled bytes reach the error string
// (INV-4's fail-safe default: no URL, no body, no resp.Status text).
//
// When enabled, up to errorBodyExcerptMax (256) bytes are read from the remote,
// ATTACKER-CONTROLLED response body — capped BEFORE reading, so an oversized
// error body cannot amplify into unbounded read/allocate work — then fully
// sanitized before being embedded in the error string: every non-printable rune
// is escaped, invalid UTF-8 is escaped, printable accented/CJK runes are
// preserved, and the whole is delimited in quotes.
//
// This DELIBERATELY surfaces remote-influenced bytes into caller logs. They are
// safe to render, but the caller should still treat them as untrusted when
// parsing.
func WithErrorBodyExcerpt() Option {
	return func(c *Config) { c.errorBodyExcerpt = true }
}

// WithEventName sets the default SSE "event:" field name SSEEventFromMessage
// falls back to when the message carries no HeaderSSEEventName header.
// Default: "" — no event: line is written, and the client dispatches the
// frame as the SSE default event "message" (WHATWG semantics).
//
// The HeaderSSEEventName message header, when present and non-empty, ALWAYS
// wins over this construction-time default — a per-message flow-set name
// overrides the adapter-wide one.
//
// name MUST NOT contain CR, LF, or NUL: NewConfig returns
// ErrInvalidEventField for an explicit name containing any of those bytes —
// an SSE "event:" field is framed as a single line, so an embedded newline
// would let name inject additional, unintended SSE fields into the frame.
// Leaving this option unset (rather than calling it with a clean empty
// string) is how a caller asks for the default; either way an empty name
// never errors.
func WithEventName(name string) Option {
	return func(c *Config) {
		c.eventName = name
		c.eventNameSet = true
	}
}

// WithMaxEventBytes caps the number of bytes NewSSEParser's SSEParser
// buffers for a single Server-Sent Event: the max of the current
// in-progress line's byte count and the accumulated "data" buffer's byte
// count — the only two things that actually buffer while parsing a
// text/event-stream — never the sum of bytes read across the whole stream.
// A comment line, an ignored field, or a blank line never accumulates
// toward this cap, so a long-lived idle stream of small "ping" comments, or
// a run of small blank-line-separated events, never trips it regardless of
// its cumulative length. Default: defaultMaxEventBytes (1 MiB).
//
// Exceeding the cap on either counter returns ErrEventTooLarge from Next: an
// endless unterminated line trips the line counter; a single event built
// from too many "data:" lines trips the data-buffer counter. Both counters
// reset independently of one another — the line counter at every line
// ending, the data buffer at every dispatch — so the cap bounds a single
// line or a single event, not the stream's lifetime.
//
// n MUST be > 0: NewSSEParser (via NewConfig) returns ErrInvalidMaxEventBytes
// for an explicit n <= 0, so a caller mistake (e.g. an uninitialized zero
// value passed through) is a construction error rather than a
// silently-disabled cap. Leaving this option unset (rather than calling it
// with 0) is how a caller asks for the default.
func WithMaxEventBytes(n int64) Option {
	return func(c *Config) {
		c.maxEventBytes = n
		c.maxEventBytesSet = true
	}
}

// SlowClientPolicy selects what NewSSEServer's Send does for a single
// registered connection whose per-connection event buffer
// (WithConnectionBuffer) is already full when a new event arrives.
type SlowClientPolicy int

const (
	// SlowClientDrop drops the event for that ONE connection only — every
	// other connection still receives it, and Send itself never blocks or
	// errors because of one slow subscriber. It is iota 0, the safe
	// default: SSE fan-out is already an at-most-once delivery guarantee,
	// so losing one already-lossy event for one already-degraded
	// connection stays within the documented contract, whereas
	// disconnecting by default would drop that subscriber's WHOLE stream
	// over what may be a transient burst.
	SlowClientDrop SlowClientPolicy = iota

	// SlowClientDisconnect cancels the slow connection instead of
	// dropping individual events for it: the connection is unregistered
	// and its writer goroutine joined, so a client that cannot keep up is
	// told (its stream closes, triggering its own reconnect) rather than
	// silently missing events indefinitely.
	SlowClientDisconnect
)

// WithMaxConnections caps the number of concurrently registered SSE
// connections NewSSEServer's http.Handler accepts. Default:
// defaultMaxConnections (1024) — the connection-exhaustion guard: once the
// cap is reached, the handler responds 503 rather than registering an
// unbounded number of long-lived streaming connections. The cap is
// PROCESS-GLOBAL, not per-IP: it budgets the whole process's open SSE
// connections, so per-IP or per-tenant limiting, where needed, is the
// caller's own reverse-proxy/load-balancer concern, not something this
// option attempts.
//
// n MUST be > 0: NewConfig returns ErrInvalidMaxConnections for an explicit
// n <= 0, so a caller mistake (e.g. an uninitialized zero value passed
// through) is a construction error rather than a silently-disabled cap.
// Leaving this option unset (rather than calling it with 0) is how a
// caller asks for the default.
func WithMaxConnections(n int) Option {
	return func(c *Config) {
		c.maxConnections = n
		c.maxConnectionsSet = true
	}
}

// WithConnectionBuffer sets the size of the per-connection buffered event
// channel each registered SSE connection's writer goroutine drains.
// Default: defaultConnectionBuffer (16 events) — the slow-client isolation
// bound: a connection whose reader or network cannot keep up fills only ITS
// OWN buffer without blocking Send for any other connection or for the flow
// calling it; once that buffer is full, WithSlowClientPolicy decides what
// happens next for that one connection.
//
// n MUST be > 0: NewConfig returns ErrInvalidConnectionBuffer for an
// explicit n <= 0. Leaving this option unset is how a caller asks for the
// default.
func WithConnectionBuffer(n int) Option {
	return func(c *Config) {
		c.connectionBuffer = n
		c.connectionBufferSet = true
	}
}

// WithSlowClientPolicy selects the SlowClientPolicy NewSSEServer's Send
// applies to a connection whose per-connection buffer is already full when
// a new event needs to be enqueued. Default: SlowClientDrop — "a slow
// subscriber loses events, never the flow": Send always returns promptly
// and no other connection is ever affected, matching SSE's inherently
// at-most-once delivery contract.
//
// p MUST be a recognized SlowClientPolicy (SlowClientDrop or
// SlowClientDisconnect): NewConfig returns ErrInvalidSlowClientPolicy for
// any other value. Leaving this option unset is how a caller asks for the
// default.
func WithSlowClientPolicy(p SlowClientPolicy) Option {
	return func(c *Config) {
		c.slowClientPolicy = p
		c.slowClientPolicySet = true
	}
}

// WithReplayBuffer enables a per-connection-set REPLAY RING of the last n
// events NewSSEServer sends, used to resume a reconnecting client that
// supplies a Last-Event-ID request header. Default: OFF — no ring is kept,
// so Last-Event-ID is ignored and a reconnecting client sees only events
// sent after it re-registers.
//
// MULTI-INSTANCE NOTE (CLAUDE.md "Production robustness — multi-instance
// awareness"): the ring is per-process, best-effort, in-memory state. A
// reconnect that lands on a DIFFERENT instance behind a load balancer finds
// an empty ring regardless of this setting, so enabling it improves a
// same-instance reconnect blip only — it does not make replay gap-free
// across a horizontally-scaled deployment. A gap-free cross-instance resume
// needs a durable/shared backbone feeding every instance; this option does
// not attempt that.
//
// n MUST be > 0 when set explicitly: NewConfig returns
// ErrInvalidReplayBuffer for an explicit n <= 0 — there is no explicit
// value that means "off" through this option; only leaving it unset does.
func WithReplayBuffer(n int) Option {
	return func(c *Config) {
		c.replayBuffer = n
		c.replayBufferSet = true
	}
}

// WithHeartbeat enables periodic ": ping" SSE comment frames, written every
// d from the connection's own writer goroutine (no extra goroutine per
// connection), to keep an otherwise-idle stream's underlying socket active.
// Default: OFF.
//
// WARNING — read before leaving this unset: an idle stream behind a
// reverse proxy or load balancer with a 30-60s idle timeout is SILENTLY
// DROPPED unless heartbeat is enabled. If your deployment sits behind such
// a proxy (most production deployments do), enable this with a d
// comfortably below the shortest idle timeout anywhere in the path.
//
// d MUST be > 0 when set explicitly: NewConfig returns ErrInvalidHeartbeat
// for an explicit d <= 0 — there is no explicit value that means "off"
// through this option; only leaving it unset does.
func WithHeartbeat(d time.Duration) Option {
	return func(c *Config) {
		c.heartbeat = d
		c.heartbeatSet = true
	}
}

// WithWriteTimeout sets the per-write OS deadline NewSSEServer's writer
// goroutine applies (via http.ResponseController.SetWriteDeadline) before
// every event Write/Flush. Default: defaultWriteTimeout (30s).
//
// This is the mechanism that lets Close and WithSlowClientPolicy's
// disconnect actually reap a writer goroutine wedged inside Write on a
// stalled reader's socket: SSE forces the underlying http.Server's
// WriteTimeout to 0 (a streaming response has no fixed total duration), so
// without a PER-WRITE deadline a stalled reader would block that
// connection's writer — and Close's join — forever. It is a REAL,
// physical-time OS deadline, distinct from the logical WithHeartbeat
// interval: heartbeat decides how often an otherwise-idle connection
// SPEAKS, this decides how long any single WRITE may take before being
// aborted. 30s is generous enough that a healthy connection's frame write
// never trips it, while still bounding how long a truly stalled connection
// can wedge its writer goroutine.
//
// d MUST be > 0: NewConfig returns ErrInvalidWriteTimeout for an explicit
// d <= 0. Leaving this option unset is how a caller asks for the default.
func WithWriteTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.writeTimeout = d
		c.writeTimeoutSet = true
	}
}

// WithSSEClock injects the clockwork.Clock NewSSEServer's per-connection
// writer goroutines use for WithHeartbeat's interval timing. Default:
// clockwork.NewRealClock(). Named apart from WithOutboundClock and
// msgin.WithClock per the repo's distinct-clock-name-per-component
// convention.
//
// A nil clock is a no-op: the real-clock default stays in place (mirrors
// WithLogger/WithOutboundClock).
func WithSSEClock(clk clockwork.Clock) Option {
	return func(c *Config) {
		if clk != nil {
			c.sseClock = clk
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

	if !cfg.maxResponseBytesSet {
		cfg.maxResponseBytes = defaultMaxResponseBytes
	} else if cfg.maxResponseBytes <= 0 {
		return nil, ErrInvalidMaxResponseBytes
	}

	if cfg.eventNameSet && !validSSEField(cfg.eventName) {
		return nil, ErrInvalidEventField
	}

	if !cfg.maxEventBytesSet {
		cfg.maxEventBytes = defaultMaxEventBytes
	} else if cfg.maxEventBytes <= 0 {
		return nil, ErrInvalidMaxEventBytes
	}

	if !cfg.maxConnectionsSet {
		cfg.maxConnections = defaultMaxConnections
	} else if cfg.maxConnections <= 0 {
		return nil, ErrInvalidMaxConnections
	}

	if !cfg.connectionBufferSet {
		cfg.connectionBuffer = defaultConnectionBuffer
	} else if cfg.connectionBuffer <= 0 {
		return nil, ErrInvalidConnectionBuffer
	}

	if !cfg.slowClientPolicySet {
		cfg.slowClientPolicy = SlowClientDrop
	} else if cfg.slowClientPolicy != SlowClientDrop && cfg.slowClientPolicy != SlowClientDisconnect {
		return nil, ErrInvalidSlowClientPolicy
	}

	if cfg.replayBufferSet && cfg.replayBuffer <= 0 {
		return nil, ErrInvalidReplayBuffer
	}

	if cfg.heartbeatSet && cfg.heartbeat <= 0 {
		return nil, ErrInvalidHeartbeat
	}

	if !cfg.writeTimeoutSet {
		cfg.writeTimeout = defaultWriteTimeout
	} else if cfg.writeTimeout <= 0 {
		return nil, ErrInvalidWriteTimeout
	}

	if cfg.sseClock == nil {
		cfg.sseClock = clockwork.NewRealClock()
	}

	if cfg.httpClient == nil {
		cfg.httpClient = &http.Client{Timeout: defaultHTTPClientTimeout}
	}
	if cfg.clock == nil {
		cfg.clock = clockwork.NewRealClock()
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
