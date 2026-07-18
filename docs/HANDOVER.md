# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md`, then the design bundle for the NEXT increment — `docs/specs/003-composition-endpoints.md`,
> `docs/adrs/0014-channel-settlement.md` (QueueChannel/PubSub, still untracked until Phase 2 starts), and
> (once written) the Phase-2 plan. Plan 008 (Phase 1) is DONE and merged.

_Updated 2026-07-18: **Plan 008 Phase 1 COMPLETE and merged to `main`.** The in-process composition layer +
four linear EIP endpoints shipped via SDD (8 tasks, each adversarially reviewed), whole-branch gate CLEAN
(`/code-review` + `/security-review`, `-race`, govulncheck, golangci-lint v2.12.2, root coverage 99.0%). The
branch `feat/composition-endpoints` was merged (no-ff) to `main` and pushed; the feature branch is deleted._

## 1. Objective & roadmap position

`msgin` (`github.com/kartaladev/msgin`) — Go 1.25 EIP library, minimal deps, multi-module monorepo. `main`
carries Plans 001–007 (core + reliability + resilience + `sql`/`memory`/dialects incl. SQLite) **and now Plan
008 Phase 1** (in-process composition layer), all merged.

**Delivered = Spec 003 / Plan 008, Phase 1.** The **in-process composition backbone** — `MessageHandler` +
`MessageChannel`, the synchronous `DirectChannel`, `PayloadOf[T]`/`WithPayload[A,B]`, `Step`/`Chain`/`To`, and
the four linear EIP endpoints: **Transform** (Message Translator), **Filter** (+`WithDiscardChannel`),
**Content-Based Router** (`NewRouter`/`WithDefaultChannel`/`ErrNoRoute`), **Service Activator**
(`Activate[A,B]` reply / `Consume[A]` one-way). A composed `Chain` drives off the existing `Consumer` runtime
via `NewConsumer[any](src, flow.Handle)` — proven by an end-to-end integration test over `memory.New()`.

**Phasing (from Spec 003 §6.1 / ADR 0014):** **P1 = DONE** (this increment). **P2 = `QueueChannel`** (buffered,
ADR 0014) — the next increment. **P3 = `PublishSubscribeChannel`** (fan-out, ADR 0014, un-defers ADR 0002 §4
pub-sub). **P4 = fluent `Flow` DSL = GATED go/no-go**, decide later.

## 2. Exact state (safepoint — Phase 1 delivered)

- **`main`** now contains Plan 008 Phase 1 (merged no-ff, pushed to origin). Feature branch deleted.
- **`git status --short`** (post-merge working tree):
  ```
   M .claude/settings.json                       (user's — leave untouched)
  ?? docs/adrs/0014-channel-settlement.md         (Phase 2 ADR — intentionally untracked until P2)
  ```
- **Build/test:** whole tree builds, `go test ./... -race` green, goleak-clean, root coverage 99.0%.
- **ADR 0014 stays uncommitted** by design — it governs Phases 2–3 and lands with the Phase-2 code.

## 3. Traceability pointers (read FIRST, before acting on Phase 2)

`CLAUDE.md` → `docs/specs/003-composition-endpoints.md` (spec, D1–D9; D7 = QueueChannel/PubSub, the P2/P3
scope) → `docs/adrs/0013-composition-endpoints.md` (P1 composition model — MERGED) +
`docs/adrs/0014-channel-settlement.md` (QueueChannel/PubSub settlement, Phases 2–3 — the P2 driver) →
`docs/plans/008-composition-endpoints-phase1.md` (the delivered P1 plan, for reference). The SDD progress
ledger + task briefs/reports live under `.superpowers/sdd/` (gitignored scratch).

## 4. Decisions & findings this session (all resolved)

**Delivered design (Spec 003 §3 / ADR 0013):** C-core/A-sugar typing (monomorphic `Message[any]` core + typed
generic free-function constructors that box into `any`; Go forbids generic methods, so no fully-typed fluent
chain); synchronous-direct error model (endpoint errors propagate into the EXISTING retry/DLQ/invalid runtime;
`ErrPayloadType` → invalid sink); Spring-aligned overridable defaults (Filter silent-drop, Router `ErrNoRoute`,
Activate/Consume split).

**Whole-branch gate findings (both resolved before merge):**
- **CR#1 (fixed):** `PayloadOf[any]` godoc claimed "always succeeds" but returns `ErrPayloadType` on an
  untyped-nil payload (`New[any](nil)` has no dynamic type). Resolved doc-only + pinning test (commit 3d6e00a).
- **CR#2 (triaged, accepted):** a typed-nil `MessageChannel` (nil concrete pointer boxed in the interface)
  bypasses the `== nil` guards in Router (pick-return/default) and Filter (discard) and panics on `Send`. This
  matches the codebase's existing nil-check convention (`Source`/`OutboundAdapter`/`To`) — accepted pattern,
  not a regression. Handler panics are recovered at the endpoint boundary by the runtime (fault isolation).
- `/security-review`: no HIGH/MEDIUM findings (pure in-process layer; no injection/crypto/I/O surface).

**Minor backlog (non-blocking, carry to a future increment):** no concurrent-goroutine test for
`DirectChannel` (mutex correct by inspection); `Router.Handle` method doc terse (type doc carries the full
contract); a couple of inherited-from-brief test-style nits (see `.superpowers/sdd/progress.md`).

## 5. Next actions (Phase 2 — QueueChannel)

1. **Brainstorm → spec-delta → adversarial audit → plan** for **P2 (`QueueChannel`, buffered, ADR 0014)**
   before any code (CLAUDE.md design-time gate: spec + ADR + plan, then an independent Opus audit, two rounds
   is the project norm). `QueueChannel` adds an async buffered hand-off with its own goroutine/shutdown/drain
   — the goleak + graceful-shutdown constraints bite here (unlike P1's synchronous `DirectChannel`).
2. **Then P3 (`PublishSubscribeChannel`, fan-out)**, then the **P4 DSL go/no-go**.
3. **Still deferred** (after P1–P3): Splitter/Aggregator/Resequencer, Messaging Gateway, Recipient List /
   Wire Tap / Message History; then the **pended adapters** (pgx/redis/nats/http) + Plan 005 Task 11 examples.

## 6. Gotchas / environment

- **Go 1.25 pinned:** always `GOTOOLCHAIN=go1.25.12` (the `go` directive stays `1.25.0`). Stdlib-only core —
  no new dependency; `go mod tidy` stays clean.
- **Typed endpoints need live-value payloads** (Spec 003 D8 / F3): a composed flow's typed constructors assume
  the payload is the live Go value — true for `memory.New()` (a `LiveValueSource`). A **wire** source at
  `T=any` decodes to `map[string]any`, so decode to the concrete type in the first endpoint. Documented in
  `doc_composition.go` + ADR 0013 D8.
- **Reused core symbols (do NOT redeclare):** `ErrPayloadType`/`ErrPayloadDecode` (`errors.go`), `NewMessage[T]`,
  `New[T]`, `Message[any]` accessors, `NewConsumer`/`Handler[T]`/`WithShutdownTimeout`, `adapter/memory`.
- **Custom skills (mandatory):** `table-test` (assert-closure tables, `t.Context()`), `use-mockgen`,
  `use-testcontainers`; blackbox `_test` packages; start Go work from `cc-skills-golang:golang-how-to`; execute
  via SDD (`superpowers:subagent-driven-development`).
- **SDD ledger:** `.superpowers/sdd/progress.md` (gitignored) has the full per-task record for Plan 008.
