# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md`, then — since the Messaging Gateway (Spec 010) is now shipped — the governing artifacts for whatever
> the next increment is (see "Next actions"). For the just-shipped increment: `docs/specs/010-messaging-gateway.md`,
> `docs/adrs/0022-messaging-gateway.md`, `docs/plans/019-messaging-gateway.md`. The SDD ledger
> `.superpowers/sdd/progress.md` (gitignored, local) holds per-task history — trust it + `git log` over memory.

## LATEST: Messaging Gateway (Spec 010 / Plan 019 / ADR 0022) SHIPPED, MERGED to `main` (`9ae2275`, `--no-ff`), pushed to origin; `feat/messaging-gateway` deleted.

The EIP **Messaging Gateway** (in-process request-reply). Additive to the core `msgin` package, **no new dependency**
(stdlib + clockwork), additive API → **minor SemVer**. What shipped:
- **`RequestReplyExchange` SPI** — `Exchange(ctx, Message[any]) (Message[any], error)`; the seam a future external
  (HTTP/NATS) request-reply adapter implements without a core change.
- **`ChannelExchange`** (in-process impl) — a **zero-goroutine reply correlator** (`sync.Mutex` map `HeaderCorrelationID`
  → cap-1 slot; a receiver `Subscribe`d to the reply channel demuxes replies to blocked waiters). 30s default reply
  timeout (`min(ctx, timeout)`, `WithReplyTimeout`); graceful `Close`; unmatched replies warn-log+drop or
  `WithUnmatchedReplySink`. Guards: empty corr-id → `ErrNoCorrelation`, duplicate in-flight → `ErrDuplicateCorrelation`.
  `giveUp` drains a delivered-but-abandoned reply (timeout/close race) to the unmatched path — never silently lost.
- **Inbound `Gateway[Req,Rep]`** — `NewGateway(exchange)` + `Request(ctx, Req) (Rep, error)`: mint fresh id, box, unbox
  reply → `ErrPayloadType` on mismatch.
- **In-flow `OutboundGateway(x) Step`** — runs the exchange mid-flow, forwards reply to `next`; **saves/restores the
  incoming `HeaderCorrelationID`** (raw `Header` presence, not `String` — audit G5) so an upstream splitter/aggregator
  key survives and split-children get unique registry keys.
- **Additive `Message.WithoutHeader` / `Headers.without`** (copy-on-write header removal, needed by the outbound strip).
- New sentinels: `ErrGatewayClosed`, `ErrReplyTimeout`, `ErrNilExchange`, `ErrNilChannel`, `ErrInvalidReplyTimeout`,
  `ErrDuplicateCorrelation` (+ reuse `ErrPayloadType`/`ErrNoCorrelation`).

**Quality gate PASSED.** Design **2-round Opus-audited** (R1 NEEDS-REVISION: G1 dup/empty-id guard, G2 async+concurrent
`-race` tests, G3 reply-channel is `DirectChannel`-only wording, G4 giveUp drain, G5 raw-presence, G6–G8; R2
SOUND-WITH-NITS, no must-fix: N1 lifetime-uniqueness doc, N2 close-races-giveUp test, N3 direct-caller-guard godoc —
all folded). Whole-branch **Opus code review = Ready-to-merge YES** (0 Critical/0 Important; correlator proven
correct/leak-free every interleaving; 99.1% cov, 100% exchange hot paths) + **security review = no blocker** (0 HIGH/0
MEDIUM; 1 LOW = documented N1 sequential-reuse, façade-unreachable, crypto/rand fresh ids). `-race`/goleak/vet/gofmt/
golangci-lint0/govulncheck/CGO0/`go mod tidy` no-diff all green.

## Where we are (2026-07-21)

`main` @ **`9ae2275`** (pushed to origin). Branch commits (all reviewed clean):
`f54069f` (exchange core) → `dda93cd` (inbound Gateway) → `33d9785` (OutboundGateway + WithoutHeader; 1 review fix:
vacuous strip test → echoes fresh id, mutation-verified) → `c66182d` (Examples + gate) → `a477e8a` (review polish:
WithUnmatchedReplySink nil-guard + non-blocking-sink godoc).

**Working tree:** `.claude/settings.json` is modified — pre-existing, UNRELATED, intentionally never committed.

**`git status --short`:** ` M .claude/settings.json`  ·  **last commit:** `9ae2275 Merge Plan 019 (Spec 010): Messaging Gateway …`

## Traceability
Spec [`010`](specs/010-messaging-gateway.md) → Plan [`019`](plans/019-messaging-gateway.md) → ADR
[`0022`](adrs/0022-messaging-gateway.md). Realizes Spec 001 §1 (deferred Messaging Gateway) + un-defers Spec 003 §2.
Builds on ADR 0013 (composition backbone) / 0001 (payload typing) / 0004 (clockwork).

## Backlog (non-blocking, triaged from reviews)
- **External request-reply adapter** (HTTP/NATS implementing `RequestReplyExchange`) — the untrusted-input boundary;
  will need its own dedicated security review when built.
- **N1 sequential correlation-id reuse** — direct `ChannelExchange` callers reusing an id after a give-up can get a
  stale reply; documented, façade-unreachable. Revisit only if a direct-`ChannelExchange` public pattern emerges.
- One-way / async-future gateway, header-carrying `RequestMessage`, `NewChannelGateway` convenience ctor — all deferred
  (Spec 010 §2/§8), non-breaking to add later.
- `GatewayOption`/`gatewayConfig` are empty scaffolding (kept for SemVer variadic-stability) — first real `WithX`
  option closes the ~83% `NewGateway` opts-loop coverage.

## Next actions
Start a **fresh design cycle for a NEW increment** (user's choice): candidates — **Resequencer**, the deferred external
request-reply **HTTP/NATS adapter** (now unblocked by the `RequestReplyExchange` SPI), redis/pgx/nats group stores, or
aggregate-by-expr. Follow CLAUDE.md: brainstorm → spec → ADR → plan → **2-round adversarial Opus audit** → **ask before
implementation** → SDD (fresh implementer per task, coordinator commits, adversarial review). Fresh branch off `main`.

## Gotchas
- **Go 1.25 pin:** `GOTOOLCHAIN=go1.25.12` on every go command (local default is newer).
- Reply channel must be a `Subscribe`-based `MessageChannel` = **`DirectChannel` only** in core (`QueueChannel`/pubsub
  do NOT satisfy `MessageChannel`); "async" = who calls `reply.Send`, not the channel type.
- SDD ledger `.superpowers/sdd/progress.md` is gitignored/local — per-task history + Minors triage live there.
