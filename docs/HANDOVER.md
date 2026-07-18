# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md`, then — before starting the next increment — the governing artifacts for whatever you pick up.
> Phase 3 (Publish-Subscribe) is **DONE and merged to `main`**; there is no in-flight work.

_Updated 2026-07-19: **composition Phase 3 (Publish-Subscribe) is COMPLETE, gate-clean, and MERGED to `main`.**
Spec 004 / ADR 0014 / Plan 009 delivered via SDD (3 tasks + 1 review-fix), each task adversarially reviewed
(Approved), whole-branch `/code-review` + `/security-review` clean. The `feat/pubsub` branch was deleted after
merge. The next increment is the **scheduled/delayed-send API** (design not yet started)._

## 1. Objective & roadmap position

`msgin` (`github.com/kartaladev/msgin`) — Go 1.25 EIP library, minimal deps, multi-module monorepo. `main` now
carries Plans 001–007 (core + reliability + resilience + `sql`/`memory`/dialects incl. SQLite), **Plan 008
composition Phase 1** (in-process composition layer + 4 linear endpoints), **and Plan 009 composition Phase 3
(Publish-Subscribe)** — all merged.

**No active increment.** Phase 3 shipped: an in-process `PublishSubscribeChannel` (one message → every
subscriber), a topic pub/sub SPI (`TopicPublisher`/`TopicSubscriber`), and an EIP-native `PubSub` topic registry
(lazy-create, drop-on-empty). This un-deferred pub-sub (ADR 0002 §4) and completed Spec 003 §3 D7 Phase 3.

**Roadmap (reprioritized 2026-07-19):** **Next = a scheduled/delayed-send API** over the `sql` adapter's
*existing* `visible_after` mechanism (storage primitive already built — `dialect.go` `Insert(delay)`/`Nack(delay)`;
the gap is only a public scheduled-send surface, since `OutboundAdapter.Send` is delay-0). Start it with the full
deliberate-design loop: brainstorm → spec → ADR → plan → **2-round adversarial Opus audit** → SDD. **Deprioritized:**
Phase 2 `QueueChannel` (at-most-once buffered case is already `memory.Broker`; at-least-once needs settlement-runtime
work) and Splitter/Aggregator. **Still deferred:** Wire Tap / Recipient List, Messaging Gateway, the pended adapters
(pgx/redis/nats/http), Plan 005 Task 11 examples, the Phase-4 fluent DSL (gated).

## 2. Exact state (safepoint — Phase 3 merged, tree clean)

- **Branch:** `main` (Phase 3 merged; `feat/pubsub` deleted local + remote).
- **`git status --short`:** only `.claude/settings.json` (the user's own file — leave untouched). Everything else is committed.
- **Build/tests:** `go build ./...` and `go test ./... -race` green; coverage on the root package 98.9%; `go vet`,
  `gofmt`, `golangci-lint` (0 issues), `govulncheck` (no called vulns) all clean; `CGO_ENABLED=0` builds; `go mod tidy`
  leaves go.mod/go.sum unchanged (no new dependency — stdlib-only, as designed).

### Plan 009 commits (on `main` via the merge)
- `1a48316` feat(core): add PublishSubscribeChannel (single-topic fan-out) — Task 1 (+ CLAUDE.md writing-plans override).
- `cc4af14` feat(core): add topic pub/sub SPI + PubSub registry — Task 2 (F1 TOCTOU fix: Subscribe holds `p.mu` across `ch.Subscribe`).
- `f9a91c6` test(core): pub-sub end-to-end via NewConsumer + example + package doc — Task 3.
- `6325f89` refactor(core): nil vacated tail slot in PublishSubscribeChannel.remove — whole-branch review Minor fix.

## 3. Traceability pointers

Delivered design bundle (all on `main`): `docs/specs/004-publish-subscribe.md` → `docs/adrs/0014-publish-subscribe.md`
→ `docs/plans/009-publish-subscribe-phase3.md` (3-task plan, both audit rounds folded). Companion: `docs/specs/003-composition-endpoints.md`
(D7 Phase 3) + `docs/adrs/0013-composition-endpoints.md` (Phase-1 composition model). SDD ledger (gitignored scratch):
`.superpowers/sdd/progress.md`.

## 4. What shipped (Spec 004 / ADR 0014)

- **EIP-native topics** — a topic is a *named* `PublishSubscribeChannel`; the `PubSub` registry maps topic name → channel
  (lazy-create, drop-on-empty), so a future native-topic broker adapter implements the same topic pub/sub SPI generically.
- **3 layers** — SPI (`TopicPublisher`/`TopicSubscriber`/`Subscription`, split per ISP) → `PublishSubscribeChannel`
  (single-topic fan-out; an `OutboundAdapter` via `Send`, so `To(psChannel)` broadcasts) → `PubSub` registry.
- **Synchronous** dispatch (no goroutine → leak-free), registration order, snapshot-under-RLock, dispatch outside the lock.
- **Settlement** — all-succeed-before-Ack default (`errors.Join` → Consumer retries; unit-settlement, so a permanent
  subscriber error propagates to the invalid sink), `WithFanOut(FanOutBestEffort)` opt-in (log-and-continue via injected
  `WithPubSubLogger`, default discard). Per-subscriber panic isolation (`safeFanOut` → transient `ErrHandlerPanic`).
- **Public API:** `PublishSubscribeChannel`/`NewPublishSubscribeChannel`/`Send`/`Subscribe`, `Subscription.Cancel`,
  `PubSub`/`NewPubSub`/`Publish`/`Subscribe`/`TopicCount`, `TopicPublisher`/`TopicSubscriber`, `FanOutPolicy`
  (`FanOutAllSucceed`/`FanOutBestEffort`), `WithFanOut`/`WithPubSubLogger`.

**Deferred (documented in Spec 004 / ADR 0014):** `Close()` (O4-1, YAGNI for a goroutine-free channel);
`Router`/`Filter` → `OutboundAdapter` widening (O4-2 — `pick`-return widening is breaking, own ADR); Wire Tap /
Recipient List; native-topic broker adapters. Consumer groups remain adapter-provided (sql `SKIP LOCKED` + lease).

**Backlog from the whole-branch review (triaged, not a blocker):** `PublishSubscribeChannel.Send` allocates a snapshot
slice (`make`+`copy`) per publish. A copy-on-write `subs` slice (Subscribe/remove rebuild it; Send reads the header under
RLock, no copy) would make the broadcast hot path allocation-free — a future perf increment; the current approach is
correct and documented.

## 5. Next actions

1. **Next increment = scheduled/delayed-send API.** Run the deliberate-design loop: `superpowers:brainstorming` →
   `docs/specs/005-*.md` → ADR(s) → `superpowers:writing-plans` (`docs/plans/010-*.md`) → **independent adversarial Opus
   audit of the full bundle (spec + ADR + plan), 2 rounds** → **ask the user for go-ahead + execution mode** before any code.
   The surface: expose a public scheduled/delayed send over the `sql` adapter's existing `visible_after` (`dialect.go`
   `Insert(delay)`/`Nack(delay)`) — no new storage machinery, a thin API increment.
2. Start from a fresh branch off `main` (`git checkout -b feat/<slug> main`). Per-task commits only after the plan is
   approved and a task-by-task mode is chosen.

## 6. Gotchas / environment

- **Go 1.25 pinned:** always `GOTOOLCHAIN=go1.25.12` (the `go` directive stays `1.25.0`). Stdlib-only core — no new dep.
- **Tooling on PATH:** `golangci-lint` (homebrew), `gopls`/`govulncheck`/`staticcheck`/`gosec`/`mockgen` under `$(go env GOPATH)/bin`
  (`govulncheck` is NOT on bare PATH — call it by full path). `gofumpt` is not installed; `gofmt` is clean.
- **Custom skills (mandatory):** start Go work from `cc-skills-golang:golang-how-to`; TDD via
  `superpowers:test-driven-development`; `gopls` for navigation; `table-test` (assert-closure tables, `t.Context()`),
  `use-mockgen`, `use-testcontainers`; blackbox `_test` packages.
- **`.claude/settings.json`** shows as modified in `git status` — it is the user's own file; do not stage or commit it.
