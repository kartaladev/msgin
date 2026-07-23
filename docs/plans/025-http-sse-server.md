# Plan 025 — HTTP SSE Phase 3: shared SSE core + S-out SSE server

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development` (the project default) or
> `superpowers:executing-plans`. Steps use checkbox (`- [ ]`) syntax.
>
> **Go-skills hard rule (CLAUDE.md, restated because `writing-plans` omits it):** every task starts from
> **`cc-skills-golang:golang-how-to`**, which routes to the applicable `golang-*` skills (here: `golang-concurrency`
> — the hub is the heart of this plan — `golang-security`, `golang-error-handling`, `golang-context`, `golang-safety`,
> `golang-design-patterns`, `golang-testing`). **`superpowers:test-driven-development`** governs every task
> (red → green → refactor). **`gopls`** (via the `LSP` tool) is mandatory for navigation, symbol lookup and
> refactoring — not `grep`. The project-local **`table-test`** override governs every table (assert-closure form,
> `t.Context()`). `use-mockgen` does not apply (doubles here are hand-written `io.Writer`/`http.ResponseWriter`
> fakes and `httptest.Server`); `use-testcontainers` does **not** apply — Spec 011 §7 mandates fully hermetic HTTP
> tests.
>
> **This plan is deliberately thin** (the Plan 024 precedent): it fixes signatures, names, invariants, branch
> coverage and commit boundaries; it does **not** embed finished implementations or test bodies. Write the code
> TDD-first from the invariants and branch tables below.

**Goal.** Ship Spec 011 Phase 3: the **shared SSE core** in `adapter/http` (`SSEEvent`, `EncodeSSEEvent`,
`SSEEventFromMessage`, the WHATWG `SSEParser` — Phase 4 consumes the parser) and the **S-out SSE server** in
`adapter/http/stdlib` — `NewSSEServer`, both an `http.Handler` and an `msgin.OutboundAdapter`, fanning flow messages
out to connected `text/event-stream` subscribers.

**Architecture.** Decisions C1–C6 of [ADR 0023 Addendum C](../adrs/0023-http-channel-adapter.md#addendum-c--sse-design-decisions-phases-34):
mutex-registry hub, **zero spawned goroutines** (the per-connection writer *is* the net/http handler goroutine),
non-blocking fan-out with a bounded per-connection buffer and a drop/disconnect slow-client policy, opt-in bounded
replay ring with atomic snapshot+register, opt-in clockwork-driven heartbeat, injection-rejecting encoder. Delivery
is **at-most-once, per-process** (multi-instance obligations below). **A per-write OS deadline
(`WithWriteTimeout`, default 30 s) bounds every `Write`/`Flush`** so a stalled reader cannot wedge the writer
goroutine — the mechanism that makes `Close`'s join and the disconnect policy actually reap connections under the
mandatory `WriteTimeout: 0` server config (audit BLOCKER-1; amends C4). No new dependency.

**Tech stack.** Go 1.25 (`GOTOOLCHAIN=go1.25.12`), root module: package `msghttp` in `adapter/http` (core) +
package `stdlib` in `adapter/http/stdlib` (server binding).

**Traceability.** Implements [Spec 011](../specs/011-http-adapter.md) §3.5 (Phase 3 rows), §3.6, §4, §7; decided by
[ADR 0023](../adrs/0023-http-channel-adapter.md) Addendum C (C1–C6). Follows Plan 024 (Phase 2 — merged @ `51330e9`).
Spec/ADR delta for this plan's refinements lands in **Task 0**. Plan 026 (Phase 4, SSE client) depends on this
plan's `SSEParser`.

---

## Baseline — verified against the tree while writing this plan (`main` @ `2b4ee1d`)

| Claim | Status | Citation |
|---|---|---|
| `adapter/http` and `adapter/http/stdlib` both at **100.0%** coverage | VERIFIED | `go test ./adapter/http/... -cover` |
| `goleak.VerifyTestMain` in both packages | VERIFIED | `encode_test.go:21`, `stdlib/inbound_test.go:22` |
| `payloadBytes(msg) ([]byte, error)` — `[]byte`/`string`/`ErrUnsupportedPayload` — **unexported** | VERIFIED | `adapter/http/encode.go:205` |
| `msg.ID() string` (reads `HeaderMessageID`), `msg.Headers().String(k)` | VERIFIED | `message.go:206`, `message.go:50` |
| `msgin.Permanent(err)` | VERIFIED | `reliability.go:26` |
| Non-reserved `http.*` header namespace precedent (`headerHTTPContentType` …) | VERIFIED | `adapter/http/encode.go:21-27` |
| `Config` + set-flag option pattern + nil-safe accessor contract | VERIFIED | `adapter/http/options.go:50-94` |
| `Register(mux, pattern, h)` helper | VERIFIED | `stdlib/inbound.go:101` |
| `httptest.ResponseRecorder` implements `http.Flusher` (the 500 arm needs a **non**-flusher wrapper fake) | VERIFIED | net/http/httptest docs |
| `httptest.ResponseRecorder` does **not** support `SetWriteDeadline` (`http.ResponseController.SetWriteDeadline` → `errors.ErrUnsupported`); a real net/http `ResponseWriter` does — so the write-deadline arm needs a **deadline-supporting blocking fake** to test in-process | VERIFY at Task 4 | net/http `ResponseController` docs |
| `clockwork.Clock.NewTicker` available (heartbeat), `NewFakeClock().BlockUntilContext` | VERIFIED | go.mod clockwork v0.5.x |

Capture before Task 1: `GOTOOLCHAIN=go1.25.12 go test ./... -race >/dev/null && echo GREEN` — must print GREEN.

---

## Global Constraints

- **Go 1.25 only.** `GOTOOLCHAIN=go1.25.12` on every invocation.
- **No new dependency.** `go.mod`/`go.sum` byte-identical at the end (`go mod tidy && git diff --exit-code go.mod go.sum`).
- **Blackbox only** — `package msghttp_test` / `package stdlib_test`, exported API only. No whitebox test, ever.
- **Assert-closure tables always** (`table-test`); `t.Context()`, never `context.Background()`, outside `Example`s.
- **Hermetic HTTP only** (Spec 011 §7): `httptest.Server`, `httptest.NewRecorder`, hand-built writer fakes. No network.
- **Do NOT add `defer goleak.VerifyNone(t)`** — both packages already run `goleak.VerifyTestMain`; per-test
  `VerifyNone` around `httptest` servers is flake-prone. `httptest.Server` cleanup via `t.Cleanup(srv.Close)`.
- **Fake clock for every time-dependent assertion** — heartbeat via `WithSSEClock(clockwork.NewFakeClock())`,
  `BlockUntilContext` (never the deprecated `BlockUntil`). **No real sleeps** in assertions; a test may use
  channels/sync for ordering, never `time.Sleep` as a synchronization primitive.
- **Never call `require`/`t.Fatal`/`t.FailNow` from a spawned goroutine** — buffer and assert on the test goroutine.
- **Every task ends in a green commit** — `GOTOOLCHAIN=go1.25.12 go test ./... -race` passes — with
  `Spec: 011` / `Plan: 025` / `ADR: 0023` trailers.
- **Coverage gate: both packages stay at 100.0%** (`go tool cover -func` per function) — a **delivery** gate
  (Task 6), not a literal per-task gate where a helper is first reachable in a later task (the Plan 024 Task-1
  lesson). If an arm proves blackbox-unreachable during implementation, **DELETE it** (Plan 023 Task 1 precedent);
  never `//nolint`, never relax the gate.
- **Force, don't hope, for interleaving-sensitive arms** (project memory — a race test that never exercises its
  target arm passes anyway). Two distinct techniques, per arm:
  - **Drop / disconnect arms:** make "the per-connection buffer is full" *deterministic* by driving the connection
    with a **blocking `http.ResponseWriter`/`http.Flusher` fake** whose `Write` blocks until the test releases it —
    NOT a real `httptest.Server` socket (small events drain into the ~200 KB loopback send buffer faster than
    `frames` (cap 16) fills, so the full-buffer branch silently never fires — audit MAJOR-3). The test still asserts
    the arm executed (WARN drop record; disconnect observed). **This fake must NOT enforce the write deadline**
    (return `errors.ErrUnsupported` from `SetWriteDeadline`, or run with `writeTimeout` ≫ the test window) so the
    writer stays wedged and `frames` fills — the *opposite* of the INV-S8 fake, which honors the deadline so the
    writer *exits* (audit F4: two distinct fakes, opposite deadline behavior — keep them separate).
  - **Replay-snapshot/register arm (INV-S5):** the "registration window" is the hub's internal `mu` critical
    section — **not blackbox-observable**, so neither a "count iterations that interleaved" assertion nor a mutation
    test can exercise the snapshot↔register gap (audit MAJOR-2/F2). Split the guarantee: **contiguity** by a *forced*
    ordering (a `Send` provably appends to the ring *before* the reconnect's snapshot — the reconnect is admitted
    only after a prior `Send` has *returned*; then assert the post-reconnect stream is exactly contiguous), which
    kills gross breakage; **atomicity** (snapshot+register in one `mu` section) as a **structural guarantee verified
    by code inspection at the Task-6 gate, NOT a mutation test**. Never substitute a probabilistic stress test.

### Coverage realism — nil-safety scope (the Plan 024 rule, applied here)

The `Config` nil-safety contract covers **exported `*Config`-taking functions** only:

- **KEEP + TEST** nil-safe accessors reachable through exported surface: `SSEEventFromMessage(msg, nil)` and
  `NewSSEParser(r)` with default options must work — so `eventNameOrDefault()` and the parser's cap resolution get
  nil/zero-Config fallbacks, provable blackbox.
- **DO NOT WRITE** nil-fallback accessors whose only caller sits behind `NewSSEServer` (its cfg always came from
  `NewConfig`): read `cfg.maxConnections`, `cfg.connectionBuffer`, `cfg.slowClientPolicy`, `cfg.replayBuffer`,
  `cfg.heartbeat`, `cfg.writeTimeout`, `cfg.sseClock` directly. A hand-built `&stdlib.SSEServer{}` is unsupported and nil-derefs —
  say so in the type godoc (the `&msghttp.Outbound{}` precedent).

---

## Security invariants — the gate; each needs a test that fails if the invariant is removed

**INV-S1 — No byte influenced by a message header may reach a subscriber's stream except through
`EncodeSSEEvent`'s validated `id:`/`event:` fields or `data:` line framing.** An `ID`/`Name` containing CR, LF or
NUL forges fields or whole events into every subscriber's stream (the SSE analog of header injection; C5).
*Realized by:* `EncodeSSEEvent` rejects with `ErrInvalidEventField` (reject, not sanitize — a silently altered id
breaks C1's identity-matched replay); `Data` is split on `\n` into one `data:` line each; `Send` wraps the
rejection in `msgin.Permanent`. *Verify:* encode table over the class (`"a\nb"`, `"a\rb"`, `"a\x00b"`, in both ID
and Name); an end-to-end `Send` with a hostile `HeaderSSEEventName` asserting the error is `msgin.Permanent` +
`errors.Is(ErrInvalidEventField)` **and zero bytes reached a connected subscriber**.

**INV-S2 — `Send` never blocks on, and never allocates unboundedly for, a slow or dead subscriber.** *Realized
by:* non-blocking enqueue into a bounded per-connection buffer; on full → `SlowClientDrop` (default; drop the
event for that connection only) or `SlowClientDisconnect` (cancel that connection). *Verify (deterministic):* drive
the stalled subscriber with a **blocking-writer fake** (its `Write` blocks until the test releases it) so `frames`
provably fills — NOT a real loopback socket (audit MAJOR-3: small events drain into the kernel send buffer faster
than cap-16 `frames` fills, so the full-buffer arm silently never executes). Then `Send` `buffer+K` times: every
`Send` returns nil promptly (the enqueue is non-blocking by construction), the drop arm is **proven executed** via
the WARN log record captured with a test `slog.Handler`, and a healthy concurrent subscriber received all
`buffer+K` events. Repeat with `SlowClientDisconnect`: the stalled connection's handler returns and its response
body closes.

**INV-S3 — A connection beyond `WithMaxConnections` is refused with 503 before any registration or hub state
exists.** *Verify:* cap-sized fleet of connected subscribers + one more request → 503; after one disconnects, a
new request succeeds (the slot is actually released — refusal is not sticky).

**INV-S4 — After `Close(ctx)` returns nil, no connection handler remains (goleak), no frame is written, new
requests get 503, and `Send` returns `ErrSSEServerClosed` wrapped in `msgin.Permanent`.** A `Close` whose ctx
expires returns `ctx.Err()` with remaining connections still cancelled (join is best-effort beyond the deadline).
**The join is only *real* because of INV-S8's per-write deadline** — cancelling a connection's derived ctx cannot
interrupt a writer blocked inside `Write` on a stalled socket (audit BLOCKER-1); the write deadline is what unblocks
it so `done` actually closes. *Verify:* connect N subscribers, `Close(t.Context())`, assert all handler bodies
ended and each arm above; goleak is the package TestMain. **Add a stalled-reader arm** (blocking-writer fake, or a
real subscriber that stops reading with a short `WithWriteTimeout`): `Close` still joins — proving the deadline, not
the ctx, is what reaps the wedged writer.

**INV-S5 — Two distinct guarantees, verified two different ways (audit MAJOR-2/F2):**
*(a) Contiguity* — a reconnecting connection whose `Last-Event-ID` names a ring entry receives exactly
`k+1, k+2, …` (no gap, no duplicate) **provided it keeps up** (absent a slow-client drop). The contiguity guarantee
is **best-effort, not absolute**: a subscriber slow enough that the replay-then-live handoff overflows its buffer is
gapped by `SlowClientDrop` despite the id being in the ring (consistent with at-most-once; audit MINOR-3 — state
this caveat in the `NewSSEServer` godoc). *Verify (forced ordering):* subscriber receives `1..k`, disconnects; the
test **deterministically forces** a `Send` to append to the ring *before* the reconnect's snapshot (a controllable
barrier — a `Send` that has *returned* is a real blackbox observable), then asserts the post-reconnect stream is
exactly contiguous across several event counts (a promptly-draining subscriber holds the keep-up precondition). This
kills gross breakage (omitted register → no live events; omitted snapshot → missing replay). *(b) Atomicity* —
snapshot and register occur in **one `mu` critical section**, so no `Send` can land in a snapshot↔register gap. This
is a **structural guarantee verified by inspection, NOT a mutation test** — the gap is by construction not
blackbox-observable (a forced-ordering test runs the `Send` fully before the reconnect, so it can never place a
`Send` inside the gap; a probabilistic stress test is the "un-exercised interleaving passes anyway" trap the project
memory forbids). The Task-6 gate item for atomicity is therefore an **inspection** check (confirm the single `mu`
section), not "a mutation turns a test RED".

**INV-S6 — The parser never buffers more than the per-event cap, and never panics or hangs on any input.**
*Realized by:* the cap bounds the **maximum of (the current in-progress line's bytes, the accumulated `data` buffer's
bytes)** — the only two things that actually buffer (audit MAJOR-4/F4). A line resets its counter at each line
ending; the `data` buffer resets at each dispatch. So a single unterminated `data:` line trips the cap (line
counter), a single event with too many `data:` lines trips it (data-buffer counter), while **comment lines,
ignored fields, and blank lines never accumulate** — a long-lived idle stream of `: ping\n` keep-alives (with or
without blank lines) or blank-line-separated small events never false-trips. (This supersedes the earlier
"bytes-consumed-since-last-blank-line" rule, which over-counted non-buffering comment bytes and still false-tripped
bare `: ping\n` heartbeats — F4.) *Verify:* at-cap
event parses; cap+1 as a single unterminated line → `ErrEventTooLarge` (line counter); a single event with many
small `data:` lines whose accumulated buffer exceeds the cap → `ErrEventTooLarge` (data-buffer counter); a long run
of small complete events whose *cumulative* bytes exceed the cap but no single line and no single event's data
buffer does → **no error**; a run of bare comment pings (`: ping\n`, **no blank line between them**) whose
cumulative bytes exceed the cap → **no error** (F4 — comments never buffer); `FuzzSSEParser` seeded with the WHATWG
table cases runs clean locally (`-fuzztime 30s` in the gate).

**INV-S8 — A stalled or malicious slow *reader* cannot wedge a writer goroutine or leak a connection slot.** SSE
forces `http.Server.WriteTimeout: 0` (Task 6 doc.go), so a reader that fills its TCP receive window blocks the
writer inside `Write`/`Flush` — where `conn.cancel()` and `Close` cannot reach it (a derived-ctx cancel does not
interrupt an in-flight socket write; audit BLOCKER-1). *Realized by:* before every `Write`/`Flush` the writer sets
a per-write OS deadline via `http.NewResponseController(w).SetWriteDeadline(time.Now().Add(cfg.writeTimeout))`
(`WithWriteTimeout`, default 30 s); a deadline-exceeded write errors like any write error → the writer loop ends
and unregisters. This is a **real OS/physical-time deadline**, distinct from the clockwork logical heartbeat — the
kernel socket deadline cannot be faked (document the split). If `SetWriteDeadline` returns `errors.ErrUnsupported`
(an in-memory test recorder, or a production `ResponseWriter` behind non-`Unwrap` middleware), the writer proceeds
best-effort (deadline unenforceable) **but emits a one-time latched WARN on a live writer** so the voided INV-S8 is
observable, not silent (audit F5). *Verify (deterministic):* a **deadline-supporting blocking-writer fake** whose
`Write` blocks past `writeTimeout` → the writer loop exits with a deadline error and unregisters, and `Close` joins
it (feeds INV-S4's stalled-reader arm); an `ErrUnsupported`-returning fake → the latched WARN record is observed
(F5) and the writer proceeds; plus a real-loopback e2e with a tiny `WithWriteTimeout` and a never-reading client
confirming the handler returns without leaking (goleak TestMain is the backstop).

**INV-S7 — Nothing in this increment weakens Phase 1/2 invariants.** The new options/sentinels touch shared
`options.go`/`errors.go`; the existing suites are the regression net — the whole-tree `-race` run per task commit.

## Multi-instance topology (CLAUDE.md rule — must be in godoc, not implied)

`NewSSEServer` godoc **must state**: connected subscribers **and the replay ring are per-process** — a subscriber
on instance A sees only messages that reach instance A, and a reconnect landing on instance B finds an empty ring,
so replay is best-effort even when enabled. Cross-instance fan-out requires a shared pub/sub backbone (redis/nats)
feeding every instance; the SSE server is the **last hop**, not the fan-out fabric. Delivery is **at-most-once**
(no subscriber ack; events are dropped on slow/disconnect) — and even a resuming subscriber whose `Last-Event-ID`
names a live ring entry can gap if it is slow enough to trip the drop policy during replay (INV-S5's best-effort
caveat). Heartbeat is **off by default** and idle streams behind proxies/LBs with 30–60 s idle timeouts are
silently dropped without `WithHeartbeat` (C2's documented-off warning). `WithMaxConnections` is a **process-global**
cap (not per-IP) — one client can hold all slots; per-IP limiting is the caller's proxy/middleware concern
(audit NIT-2).

---

## Final exported surface (the API contract this plan commits to)

```go
// adapter/http (package msghttp) — sse.go
const HeaderSSEEventName = "http.sse-event"     // flow-set: names the event: field on Send (header wins over WithEventName)
const HeaderSSEEventID   = "http.sse-event-id"  // Phase 4 (client): the remote event id — non-reserved (INV-2 class)

type SSEEvent struct {
    ID   string // "" = no id: line
    Name string // "" = no event: line (a nameless event dispatches as "message")
    Data []byte
}
func EncodeSSEEvent(w io.Writer, ev SSEEvent) error                             // ErrInvalidEventField; write errors raw
func SSEEventFromMessage(msg msgin.Message[any], cfg *Config) (SSEEvent, error) // nil-safe cfg; ErrUnsupportedPayload / ErrInvalidEventField

type SSEParser struct{ /* unexported */ }
func NewSSEParser(r io.Reader, opts ...Option) (*SSEParser, error) // ErrInvalidMaxEventBytes
func (p *SSEParser) Next() (SSEEvent, error)                       // io.EOF = clean end; ErrEventTooLarge; read errors raw
func (p *SSEParser) RetryDelay() (time.Duration, bool)             // most recent valid retry: field, if any
func (p *SSEParser) LastEventID() string                           // current id buffer (persists; empty id: clears)

// adapter/http (package msghttp) — options.go additions
type SlowClientPolicy int
const (
    SlowClientDrop       SlowClientPolicy = iota // default: drop the event for that connection only
    SlowClientDisconnect                         // cancel the slow connection instead
)
func WithMaxEventBytes(n int64) Option          // default 1 MiB; explicit n<=0 → ErrInvalidMaxEventBytes
func WithMaxConnections(n int) Option           // default 1024; explicit n<=0 → ErrInvalidMaxConnections
func WithConnectionBuffer(n int) Option         // default 16;   explicit n<=0 → ErrInvalidConnectionBuffer
func WithSlowClientPolicy(p SlowClientPolicy) Option // default Drop; unknown value → ErrInvalidSlowClientPolicy
func WithReplayBuffer(n int) Option             // default off;  explicit n<=0 → ErrInvalidReplayBuffer
func WithHeartbeat(d time.Duration) Option      // default off;  explicit d<=0 → ErrInvalidHeartbeat
func WithWriteTimeout(d time.Duration) Option   // default 30s;  explicit d<=0 → ErrInvalidWriteTimeout (per-write OS deadline; INV-S8)
func WithEventName(name string) Option          // default "";   CR/LF/NUL → ErrInvalidEventField at construction
func WithSSEClock(clk clockwork.Clock) Option   // default real clock; nil = no-op (repo per-component clock convention)

// adapter/http (package msghttp) — errors.go additions (10)
ErrInvalidEventField, ErrEventTooLarge, ErrInvalidMaxEventBytes, ErrInvalidMaxConnections,
ErrInvalidConnectionBuffer, ErrInvalidSlowClientPolicy, ErrInvalidReplayBuffer, ErrInvalidHeartbeat,
ErrInvalidWriteTimeout, ErrSSEServerClosed

// adapter/http/stdlib — sse.go
type SSEServer struct{ /* unexported */ }
func NewSSEServer(opts ...msghttp.Option) (*SSEServer, error)
func (s *SSEServer) ServeHTTP(w http.ResponseWriter, r *http.Request)
func (s *SSEServer) Send(ctx context.Context, msg msgin.Message[any]) error
func (s *SSEServer) Close(ctx context.Context) error
var _ http.Handler        = (*SSEServer)(nil)
var _ msgin.OutboundAdapter = (*SSEServer)(nil)
```

Deviations from the committed Spec 011 §3.5 sketch, folded back in **Task 0** (flagged to the user at plan review):
1. **`Send` accepts `[]byte` OR `string` payloads** — it reuses `payloadBytes` (via `SSEEventFromMessage`), whose
   contract and `ErrUnsupportedPayload` text already say "[]byte or string". The spec's "must be `[]byte`" was
   stricter than the shipped shared helper for no reason.
2. **`SSEEventFromMessage` is exported in `msghttp`** — the message→event conversion is real logic and Phase 5's
   gin S-out binding must reuse it without importing `stdlib`; blackbox tests need it exported anyway.
3. **`ErrInvalidSlowClientPolicy` and `ErrSSEServerClosed` added** to §3.6's set (enum validation; post-`Close`
   `Send` needs a typed, `Permanent`-wrapped answer — silently returning nil would hide a lifecycle bug).
4. **`HeaderSSEEventID`** is minted now (constant only; first written by Phase 4) so both SSE header names are
   defined in one place.

Audit-driven amendments (round 1, 2026-07-23 — user-approved; also folded in **Task 0**, and they **amend ADR
Addendum C** as flagged):
5. **`WithWriteTimeout` (default 30 s) + `ErrInvalidWriteTimeout`, and a per-write OS deadline in the writer**
   (BLOCKER-1) — **amends C4**: without it, a stalled reader wedges the writer inside `Write` under the mandatory
   `WriteTimeout: 0`, leaking the goroutine + connection slot past `Close` (INV-S8). Tenth server sentinel.
6. **Parser cap bounds `max(current-line-bytes, data-buffer-bytes)`, not "bytes since a boundary"** (MAJOR-4 as
   refined by F4) — **amends C6**: the cap tracks only the two things that actually buffer (the in-progress line;
   the accumulated `data` buffer), so a single unterminated line *and* a too-large multi-`data:` event both trip it,
   while comments / ignored fields / blank lines never accumulate — bare `: ping\n` keep-alives (no blank line) and
   blank-line-separated small events never false-trip (INV-S6).

---

## Hub design (normative for Tasks 4–5)

- `SSEServer` holds `mu sync.Mutex`, `conns map[*sseConn]struct{}`, `closed bool`, `ring []ringEntry`
  (`{id string, frame []byte}`, len ≤ replayBuffer), and the resolved `cfg`.
- `sseConn` holds `frames chan []byte` (cap = connectionBuffer), `replay [][]byte` (snapshot, set at register),
  `cancel context.CancelFunc` (cancels the connection's derived ctx), `done chan struct{}` (closed by the handler
  on exit), `warned bool` (first-drop WARN latch, guarded by `mu`).
- **The writer goroutine IS the handler goroutine.** `ServeHTTP` (after the guard arms) registers, writes the SSE
  response headers, flushes, writes `replay` frames, then loops: `select` on `frames`, the heartbeat ticker channel
  (a nil channel when heartbeat is off — blocks forever, zero cost), and the connection ctx (`r.Context()` derived,
  cancellable by `Close`/disconnect-policy). **Before every `Write`/`Flush`** (headers, replay, live, heartbeat) the
  writer sets a per-write OS deadline: `http.NewResponseController(w).SetWriteDeadline(time.Now().Add(cfg.writeTimeout))`
  — a real physical-time deadline (INV-S8; the socket deadline cannot use the logical `cfg.sseClock`). A
  write/flush error — including a deadline-exceeded error from a stalled reader — ends the loop (client gone or
  wedged). `SetWriteDeadline` returning `errors.ErrUnsupported` is tolerated (proceed without an enforced deadline)
  but **emits a one-time latched WARN on a live writer** (audit F5 — a `ResponseWriter` wrapped by non-`Unwrap`
  middleware voids INV-S8 silently in production; the WARN makes the lost protection observable). On exit:
  unregister under `mu`, `close(done)`.
- **`Send`:** build the event (`SSEEventFromMessage`), encode **once** into a `[]byte` frame; under `mu`: `closed` →
  `msgin.Permanent(ErrSSEServerClosed)`; append `{id, frame}` to the ring (evicting the oldest past cap) when replay
  is on **and the event has a non-empty ID** (an id-less frame can never be resumed *to*, but still fans out live —
  document); then for each conn, non-blocking enqueue; full → policy (drop: latch-WARN + skip; disconnect:
  `conn.cancel()`). Return nil (also for zero connections — fire-and-forget).
- **Replay+register atomicity (INV-S5):** in `ServeHTTP`, under the same `mu` section: check `closed`/cap, scan the
  ring for `Last-Event-ID` (exact string match — the **last** match if a flow bug produced duplicate ids, so the
  snapshot resumes from the most recent occurrence; document that resume assumes unique `HeaderMessageID`s — audit
  NIT-1), snapshot `ring[i+1:]` frames into `conn.replay`, insert into `conns`. Events `Send` appends after this
  section land in `frames`; the writer writes `replay` first, then `frames` — contiguous, no gap, no duplicate.
- **`Close(ctx)`:** under `mu`: set `closed`, collect conns, cancel each. Outside `mu`: wait each `done` or
  `ctx.Done()` → `ctx.Err()`. Idempotent (second `Close` finds no conns, returns nil).

---

## Tasks

### Task 0 — artifact delta, committed ahead of the code

**Files:** `docs/specs/011-http-adapter.md`, `docs/adrs/0023-http-channel-adapter.md`,
`docs/plans/025-http-sse-server.md` (this file), `docs/plans/026-http-sse-client.md` (authored together).

- [ ] Spec 011 §3.5: fold in deviations 1–4 above (payload `[]byte`-or-`string`; `SSEEventFromMessage`; the two
      added sentinels; `HeaderSSEEventID`), and correct the S-in row: the remote `id`/`event` land on the
      **non-reserved** `http.sse-event-id` / `http.sse-event` headers, never on `msgin.message-id` — the INV-2
      class ("no value read from an HTTP response may be written to a reserved `msgin.*` key") applies to SSE
      streams exactly as to response headers.
- [ ] Spec 011 §3.5 (S-out): fold in the **audit amendments** — the per-write `WithWriteTimeout` (default 30 s)
      reaping stalled readers under `WriteTimeout: 0` (BLOCKER-1); the parser cap bounding
      `max(current-line-bytes, data-buffer-bytes)` so comments/keep-alives never false-trip (MAJOR-4/F4); the
      process-global (not per-IP) `WithMaxConnections` note (NIT-2); the INV-S5 best-effort/drop caveat (MINOR-3).
      §3.6: sentinel list updated (**13 total**: this plan's **10** — the prior 9 + `ErrInvalidWriteTimeout` — plus
      Phase 4's **3** (Plan 026: `ErrInvalidReconnectBackoff`, `ErrNotEventStream`, `ErrInvalidReadTimeout`));
      correct the "must be `[]byte`" S-out `Send` line to `[]byte`-or-`string`.
- [ ] ADR 0023 Addendum C: append short "settled at plan time / audit rounds 1–2" notes — under **C4** the per-write
      write-deadline (`WithWriteTimeout`) that makes the writer-join real (BLOCKER-1); under **C6** the
      `max(line, data-buffer)` cap semantics (MAJOR-4/F4); a one-line note under C5 for the `[]byte`-or-`string`
      payload and the exported `SSEEventFromMessage`. Do not rewrite C1–C6's Decision text; append as dated
      amendment notes.
- [ ] Commit: `docs(http): Phase 3+4 plan-time refinements, audit-rounds-1–3 fixes, and the audited Plans 025/026`
      Trailers: `Spec: 011` / `Plan: 025` / `ADR: 0023`. (The Plan-024 Task-0 precedent: artifacts land ahead of
      code so every later `feat` trailer resolves.)

### Task 1 — SSE core: `SSEEvent`, `EncodeSSEEvent`, `SSEEventFromMessage`, `WithEventName`

**Files:** create `adapter/http/sse.go`, `adapter/http/sse_test.go`; modify `adapter/http/errors.go`
(`ErrInvalidEventField`), `adapter/http/options.go` (`eventName` field, `WithEventName`, `eventNameOrDefault()`
nil-safe accessor, construction-time CR/LF/NUL validation in `NewConfig`).

**Interfaces — Produces:** everything in the "Final exported surface" block above except the parser and the
server; Task 5 consumes `SSEEventFromMessage` + `EncodeSSEEvent`; Plan 026 consumes `HeaderSSEEventID`.

Framing rules (WHATWG, encode side): write `id: <ID>\n` when `ID != ""`; `event: <Name>\n` when `Name != ""`;
`Data` split on `\n` — one line per element, **uniformly emitting `data:` + colon + exactly one space + the line
(no special-case)** (the parser strips exactly one leading space of a value, so this single space is the round-trip
convention — a payload whose first byte is itself a space then survives; audit MINOR-1). **An empty `Data` still
emits one line, `data: \n`** (colon-space-newline — the same uniform rule; an SSE event with no data line is never
dispatched by any conforming parser, so silently emitting nothing would turn `Send` into a no-op, and the parser
strips the one space back to `""` — F5: keep the rule uniform, no branch); terminating `\n`. Field validation first, before any byte is
written: CR, LF or NUL in `ID` or `Name` → `ErrInvalidEventField` (INV-S1). `SSEEventFromMessage`: `Data` ← `payloadBytes(msg)` (existing
helper — same file access, same package); `ID` ← `msg.ID()`; `Name` ← `HeaderSSEEventName` header if present and
non-empty, else `cfg.eventNameOrDefault()`; then the same field validation (the header is flow-supplied but a
`NewMessage`-built header map can carry anything).

**Hot-path branches (Task 1):**

| # | Branch | Test |
|---|---|---|
| 1 | encode: full event (id+event+3-line data) → exact frame bytes, `data: ` = colon+one-space each | golden table |
| 2 | encode: `ID==""` / `Name==""` → line omitted | table |
| 3 | encode: empty `Data` → single `data: \n` line (colon-space-newline, uniform rule), event still framed | table |
| 3b | encode golden (F5, uniform rule): payload beginning with a space → `data:  hello\n` (colon+space+the payload's own space); payload `""` → `data: \n` (MINOR-1; the encode-side byte proof — the round-trip is Task 2 branch RT) | table |
| 4 | encode: CR / LF / NUL in ID and in Name → `ErrInvalidEventField`, zero bytes written | table over the class |
| 5 | encode: writer error → returned raw | failing-writer fake |
| 6 | `SSEEventFromMessage`: `[]byte` payload / `string` payload / other → `ErrUnsupportedPayload` | table |
| 7 | `SSEEventFromMessage`: header name wins over `WithEventName`; absent header falls back; both absent → `""` | table |
| 8 | `SSEEventFromMessage`: `cfg == nil` and `&msghttp.Config{}` → works, name `""` | table (nil-safety contract) |
| 9 | `SSEEventFromMessage`: hostile header value → `ErrInvalidEventField` | table |
| 10 | `NewConfig(WithEventName("a\nb"))` → `ErrInvalidEventField`; valid name accepted | construction table |

- [ ] Red → green per the table; then commit:
      `feat(http): SSE event framing — validated, injection-rejecting encode core`
      Trailers: `Spec: 011` / `Plan: 025` / `ADR: 0023`.

### Task 2 — SSE core: the WHATWG `SSEParser` + `WithMaxEventBytes` + fuzz

**Files:** modify `adapter/http/sse.go` (+`sse_test.go`); modify `adapter/http/errors.go` (`ErrEventTooLarge`,
`ErrInvalidMaxEventBytes`), `adapter/http/options.go` (`maxEventBytes`/set-flag, `WithMaxEventBytes`, `NewConfig`
validation, default constant `defaultMaxEventBytes int64 = 1 << 20` with a rationale comment mirroring
`defaultMaxBodyBytes`).

**Interfaces — Produces:** `NewSSEParser` / `Next` / `RetryDelay` / `LastEventID` (Plan 026's core dependency).

Parser rules (WHATWG `text/event-stream` processing model — each is a covered branch): a single leading BOM
(`U+FEFF`) stripped once; `CR`, `LF`, `CRLF` all end a line (a bare `CR` not followed by `LF` ends a line too);
line starting `:` → comment, ignored; `field:value` with **one** leading space of value stripped; no colon → whole
line is the field name, value `""`; fields — `data` appends value + `\n` to the data buffer; `event` sets the type
buffer; `id` sets the id buffer **unless the value contains NUL** (ignored); *empty* `id:` **clears** the buffer;
`retry` = ASCII digits only → milliseconds, retained for `RetryDelay` (non-digit → ignored); any other field
ignored. Blank line → dispatch: if the data buffer is empty, **no event** (type buffer resets, id buffer
persists); else strip the single trailing `\n` from data, emit `SSEEvent{ID: idBuffer, Name: typeBuffer, Data:
data}`, reset data+type buffers (id persists — WHATWG: the id survives until overwritten or cleared). The single
leading BOM is `U+FEFF`, stripped only at stream start. `Next`
returns `io.EOF` only at a clean end-of-stream between events; EOF mid-event surfaces `io.ErrUnexpectedEOF`
**discarding the partial event** (WHATWG: an event not followed by a blank line is not dispatched — the Plan 024
"truncation honesty" precedent). Cap (INV-S6): `ErrEventTooLarge` when **either** the current in-progress line's
byte count **or** the accumulated `data` buffer's byte count exceeds `maxEventBytes` — i.e. the cap bounds
`max(line-bytes, data-buffer-bytes)`, the only two things that buffer (audit MAJOR-4 as refined by F4). The line
counter resets at each line ending; the `data` buffer resets at each dispatch. So an endless single line trips it
(line counter), a single event with too many `data:` lines trips it (data-buffer counter), while comment lines,
ignored fields, and blank lines — which never buffer — never accumulate, so bare `: ping\n` keep-alives and
blank-line-separated small events never false-trip. Implementation over `bufio.Reader` with explicit byte
accounting — **not** `bufio.Scanner` (token cap + line-splitting fit neither bare-CR endings nor the cap).

**Hot-path branches (Task 2):** one table row per rule above (≈20 rows — enumerate them in the test table
verbatim: BOM, CR/LF/CRLF/bare-CR, comment, space-strip, no-colon, data-append, multi-data join, event, id, id+NUL
ignored, empty-id clears, retry digits, retry non-digit, unknown field, empty-data no-dispatch, trailing-`\n`
strip, id persistence across events, clean EOF, mid-event EOF, at-cap, **over-cap single unterminated line** →
`ErrEventTooLarge` (line counter), **over-cap single event via many `data:` lines** → `ErrEventTooLarge`
(data-buffer counter), **cap does NOT false-trip** — a run of small complete events whose cumulative bytes exceed
the cap but no single line and no single event's data buffer does → no error, and a run of bare `: ping\n` comments
(no blank line between them) whose cumulative bytes exceed the cap → no error (MAJOR-4/F4), **RT** — round-trip: a
payload beginning with a space and a `""` payload survive `EncodeSSEEvent`→`NewSSEParser` byte-exact (discharges
Task 1 branch 3b; MINOR-1)) **plus** construction: `WithMaxEventBytes(0)/(-1)` → `ErrInvalidMaxEventBytes`, unset →
1 MiB default proven behaviorally (an event of exactly `defaultMaxEventBytes` data-buffer bytes parses; one byte
more errors — the Plan 024 "prove the default, don't read the field" rule).

- [ ] Red → green per the table.
- [ ] `FuzzSSEParser`: seed with ≥10 table inputs; the fuzz body asserts only invariants (no panic; `Next` never
      returns an event whose accounted size exceeds the cap; parser terminates — the reader is finite).
      Run: `GOTOOLCHAIN=go1.25.12 go test ./adapter/http -run FuzzSSEParser -fuzz FuzzSSEParser -fuzztime 30s`.
- [ ] Commit: `feat(http): WHATWG SSE stream parser, bounded per event`
      Trailers: `Spec: 011` / `Plan: 025` / `ADR: 0023`.

### Task 3 — server option surface + sentinels

**Files:** modify `adapter/http/options.go` (fields `maxConnections`/`connectionBuffer`/`slowClientPolicy`/
`replayBuffer`/`heartbeat`/`writeTimeout` each with a set-flag, `sseClock`; the seven `WithX` options —
`WithMaxConnections`, `WithConnectionBuffer`, `WithSlowClientPolicy`, `WithReplayBuffer`, `WithHeartbeat`,
`WithWriteTimeout`, `WithSSEClock` (`WithEventName` shipped in Task 1, `WithMaxEventBytes` in Task 2); `NewConfig`
validation with the `defaultWriteTimeout = 30 * time.Second` constant), `adapter/http/errors.go`
(`ErrInvalidMaxConnections`, `ErrInvalidConnectionBuffer`, `ErrInvalidSlowClientPolicy`, `ErrInvalidReplayBuffer`,
`ErrInvalidHeartbeat`, `ErrInvalidWriteTimeout`, `ErrSSEServerClosed`), `adapter/http/options_sse_test.go` (or
fold into the existing construction tables).

**Godoc obligations (each default + rationale, per the defaults policy):** `WithMaxConnections` — 1024, the
connection-exhaustion guard (§4), **process-global not per-IP** (NIT-2); `WithConnectionBuffer` — 16 events per
connection, the slow-client isolation bound; `WithSlowClientPolicy` — Drop, "a slow subscriber loses events, never
the flow" (at-most-once already); `WithReplayBuffer` — **off**, per-process best-effort (C1's multi-instance text);
`WithHeartbeat` — **off**, with C2's proxy-idle-timeout warning verbatim; `WithWriteTimeout` — **30 s**, the
per-write OS deadline that reaps a stalled reader under the mandatory `WriteTimeout: 0` (INV-S8; a real
physical-time deadline distinct from the logical heartbeat — say so, and that a generous 30 s never trips a healthy
frame write); `WithSSEClock` — real clock, nil no-op; `WithEventName` shipped in Task 1.

**Hot-path branches (Task 3):** construction table — each option's explicit-invalid arm returns its sentinel
(`0` and `-1` for the numeric/duration ones incl. `WithWriteTimeout`; `SlowClientPolicy(99)` and
`SlowClientPolicy(-1)` for the enum); each valid boundary value (`1`) accepted. **Behavioral-deferral note (the
Plan 024 Task-1 rule):** the *defaults* (1024/16/Drop/off/off/30 s) are line-covered here but behaviorally proven
only where observable — cap default is
impractical to prove with 1024 live connections, so the *set* value is proven at Task 4 (`WithMaxConnections(2)`)
and the default is proven as "construction succeeds + the resolved value is not re-validated" — state this in the
test file comment; buffer/policy/replay/heartbeat behavior lands in Tasks 4–5.

- [ ] Red → green; commit: `feat(http): SSE server option surface and typed sentinels`
      Trailers: `Spec: 011` / `Plan: 025` / `ADR: 0023`.

### Task 4 — `SSEServer` lifecycle: constructor, `ServeHTTP`, heartbeat, `Close`

**Files:** create `adapter/http/stdlib/sse.go`, `adapter/http/stdlib/sse_test.go`.

**Interfaces — Consumes:** Task 3's resolved `Config` (direct field reads — see coverage realism);
`msghttp.EncodeSSEEvent` (heartbeat writes the raw `: ping\n\n` comment bytes directly — comments are not events;
document the constant). **Produces:** `NewSSEServer`/`ServeHTTP`/`Close` per the surface block; `Send` lands in
Task 5 (until then `Send` is **not stubbed** — the type simply doesn't implement `OutboundAdapter` yet; the
`var _ msgin.OutboundAdapter` assertion is added in Task 5).

Normative behavior: the Hub design section above. Guard order in `ServeHTTP`: method ≠ GET → 405 (with `Allow:
GET`); under `mu`: `closed` → 503, `len(conns) >= maxConnections` → 503; `http.Flusher` type-assertion failure →
500 + WARN log (no sentinel — nothing to `errors.Is` in a handler; the log record is the observable). Headers
written after the guards: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`,
then flush (the client must see the open stream before any event). **Per-write deadline (INV-S8):** the writer sets
`http.NewResponseController(w).SetWriteDeadline(time.Now().Add(cfg.writeTimeout))` before every `Write`/`Flush`
(headers included); a deadline-exceeded/`errors.Is(os.ErrDeadlineExceeded)` write ends the loop and unregisters
(the stalled-reader reap); `errors.ErrUnsupported` from `SetWriteDeadline` is tolerated best-effort **with a
one-time latched WARN on a live writer** (F5 — makes a voided INV-S8 observable). Heartbeat:
`cfg.sseClock.NewTicker(d)` when enabled, `defer ticker.Stop()`; the `: ping\n\n` write flows through the same
deadline+write+flush+error-exit path as frames.

**Hot-path branches (Task 4):**

| # | Branch | Test |
|---|---|---|
| 1 | POST/PUT/DELETE → 405 + `Allow: GET` | recorder table |
| 2 | `WithMaxConnections(2)`: 3rd concurrent GET → 503; after one leaves, a new GET → 200 stream (INV-S3, both halves) | `httptest.Server` |
| 3 | non-Flusher `ResponseWriter` → 500 + WARN record | wrapper fake + capturing `slog.Handler` |
| 4 | happy path: headers exact, stream stays open until client disconnects | `httptest.Server` + streaming client |
| 5 | client disconnect (close request body / cancel request ctx) → handler returns, conn unregistered | e2e |
| 6 | heartbeat on (fake clock): advance → `: ping\n\n` observed; no events in between | fake clock + `BlockUntilContext` |
| 7 | heartbeat off: fake clock advance produces zero bytes | fake clock |
| 8 | `Close(ctx)`: all handlers end, second `Close` idempotent-nil, post-`Close` GET → 503 (INV-S4 arms minus `Send`) | e2e |
| 9 | `Close` with an already-expired ctx while a connection is up → `ctx.Err()`, connection still cancelled | e2e |
| 10 | **stalled reader** (deadline-supporting blocking-writer fake, or real loopback client that stops reading with a tiny `WithWriteTimeout`) → writer exits on the write deadline and unregisters; `Close` joins it, no goleak (INV-S8, INV-S4 stalled-reader arm) | fake + e2e |
| 11 | `SetWriteDeadline` → `errors.ErrUnsupported` (a fake returning it) → one-time latched WARN record observed, writer proceeds without an enforced deadline (F5) | wrapper fake + capturing `slog.Handler` |

- [ ] Red → green per the table; commit: `feat(http): SSE server lifecycle — handler-goroutine writer, heartbeat, drain-free Close`
      Trailers: `Spec: 011` / `Plan: 025` / `ADR: 0023`.

### Task 5 — `Send` fan-out, slow-client policies, replay ring

**Files:** modify `adapter/http/stdlib/sse.go` (+`sse_test.go`).

**Interfaces — Consumes:** `msghttp.SSEEventFromMessage`, `msghttp.EncodeSSEEvent` (Task 1). **Produces:**
`Send` + the `var _ msgin.OutboundAdapter = (*SSEServer)(nil)` assertion; the full INV-S1/S2/S4/S5 behavior.

Normative behavior: the Hub design section. Drop-arm observability: WARN once per connection (the `warned` latch)
with the connection's remote addr and a running drop count at DEBUG per further drop — the capturing-handler test
asserts the WARN record (INV-S2's instrumentation).

**Hot-path branches (Task 5):**

| # | Branch | Test |
|---|---|---|
| 1 | `Send` to zero connections → nil, no error | unit |
| 2 | `Send` fan-out to N healthy subscribers → each receives the identical frame | e2e, parse with `NewSSEParser` |
| 3 | payload `[]byte` / `string` / other → frames vs `msgin.Permanent(ErrUnsupportedPayload)` | table |
| 4 | hostile `HeaderSSEEventName` → `Permanent` + `ErrInvalidEventField`, zero subscriber bytes (INV-S1 e2e) | e2e |
| 5 | full buffer + Drop policy → WARN latch observed, healthy peer unaffected (INV-S2) — **blocking-writer fake** so `frames` deterministically fills, NOT loopback (MAJOR-3) | blocking fake |
| 6 | full buffer + Disconnect policy → stalled conn ends, healthy peer unaffected — same blocking-writer fake | blocking fake |
| 7 | post-`Close` `Send` → `msgin.Permanent(ErrSSEServerClosed)` (INV-S4's last arm) | unit |
| 8 | replay off + `Last-Event-ID` sent → live-only, no replay bytes | e2e |
| 9 | replay on, id in ring → tail replayed then live, contiguous (INV-S5) — **forced ordering** (a `Send` provably appends to the ring before the reconnect snapshot, via a controllable barrier; assert contiguity, not a measured interleave count — MAJOR-2) | e2e forced-barrier |
| 10 | replay on, unknown/evicted id → live-only (documented best-effort) | e2e |
| 11 | replay on, ring past cap → oldest evicted (resume from evicted id = live-only) | e2e |
| 12 | id-less event (empty `msg.ID()` via `NewMessage` with empty headers) → fans out live, never enters the ring | e2e |

- [ ] Red → green per the table; the INV-S5 test **forces** the `Send`-before-snapshot ordering with a
      deterministic barrier and asserts contiguity (do not assert a measured interleave count — it is not
      blackbox-observable; MAJOR-2). The drop/disconnect arms use the blocking-writer fake (MAJOR-3).
- [ ] Commit: `feat(http): SSE Send — bounded fan-out, slow-client policy, atomic replay ring`
      Trailers: `Spec: 011` / `Plan: 025` / `ADR: 0023`.

### Task 6 — examples, package docs, delivery gate

**Files:** modify `adapter/http/stdlib/doc.go` (SSE deployment section), `adapter/http/doc.go` (SSE core
paragraph); create `adapter/http/stdlib/example_sse_test.go` (`ExampleNewSSEServer` — subscribe-and-send over
`httptest`, deterministic `// Output:`).

- [ ] **doc.go obligation (Spec §4's "SSE timeout shape"):** the existing "Deploying these handlers safely"
      `http.Server` snippet mandates `WriteTimeout` — an SSE stream **dies at WriteTimeout**. Add the SSE
      paragraph: serve SSE from a server (or route) with `WriteTimeout: 0` (or ≫ stream lifetime), keep
      `ReadHeaderTimeout`/`IdleTimeout`, **and explain that stalled-writer protection is handled per-write by the
      handler itself via `WithWriteTimeout` (default 30 s), NOT by `http.Server.WriteTimeout` — the two are
      complementary: `WriteTimeout: 0` keeps the long stream alive, the per-write deadline reaps a stalled reader
      (BLOCKER-1/INV-S8)**; and note the heartbeat-vs-proxy warning + the multi-instance last-hop invariant +
      the process-global connection cap (godoc obligations above).
- [ ] Gate, in order (CLAUDE.md §5): `/code-review` on the branch diff → fix; **dedicated `/security-review`**
      (the Spec §4 Phase-3 mandate — SSE server is an untrusted-input boundary; attack surfaces: field injection,
      connection exhaustion, slow-client memory, replay-ring memory) → fix; coverage `go tool cover -func` —
      both packages 100.0%, every branch-table row checked off; fuzz 30s clean; **mutation spot-checks** on
      INV-S1 (drop the validation), INV-S2 (make enqueue blocking), INV-S6 (remove one of the two cap counters →
      a named INV-S6 test RED), INV-S8 (remove the per-write `SetWriteDeadline` → INV-S8 test RED) — each must turn
      a named test RED (the whole-branch-review memory lesson); **plus an INV-S5 *inspection* check (F2): confirm
      the snapshot and register execute in one `mu` critical section — atomicity is structural, NOT a mutation test
      (the snapshot↔register gap is not blackbox-observable, so a mutation there cannot be caught by any test; the
      forced-ordering contiguity test catches gross breakage only)**; then
      `GOTOOLCHAIN=go1.25.12 go test ./... -race` green.
- [ ] Commit: `docs(http): SSE server examples, deployment notes, and the Phase 3 gate record`
      Trailers: `Spec: 011` / `Plan: 025` / `ADR: 0023`. Then the whole-branch review + merge per CLAUDE.md
      (approval-gated; branch `feat/http-sse-server`, deleted after merge).

---

## Open decisions — none. C1–C6 settled with the user (2026-07-22); plan-time refinements 1–4 and audit-rounds-1–3
findings (BLOCKER-1, MAJOR-2/3/4, MINOR-1/3, NIT-1/2, F2, F4, F5) folded via Task 0 and the invariant/task tables
above; the two design forks (30 s `WithWriteTimeout`; blocking-writer-fake test strategy) user-approved 2026-07-23.
Three adversarial audit rounds complete (APPROVE-WITH-FIXES → all folded).
