# Session handover — msgin

> **READ FIRST, before doing anything.** Read `CLAUDE.md` (root), then the governing artifacts in §3.
> Trust those files over anything in this handover or in memory. The tree is at a clean safepoint:
> **Plan 021 is delivered, gate-clean, merged to `main` and pushed. Nothing is in flight.**

## 1. Objective & roadmap position

**Last increment (COMPLETE):** **Plan 021 — panic-safe `ChannelExchange` cleanup** (Spec 012 / ADR 0022 Addendum A).
A **core** correctness fix to the in-process request-reply primitive shipped by Plan 019.

**Active position: no increment in flight.** The next step is a **fresh design cycle** for a new increment (§5).

| Increment | Plan | Status |
|---|---|---|
| Messaging Gateway (in-process request-reply) | 019 | merged |
| HTTP adapter **Phase 1** — inbound I1 async + I2 sync gateway | 020 | merged |
| **Exchange panic-safe cleanup** | **021** | **merged (this session)** |
| HTTP **Phase 2** — outbound O1 webhook + O2 request-reply | 022 | **not started** |
| HTTP Phase 3 — SSE server · Phase 4 — SSE client · Phase 5 — gin module | 023 · 024 · 025 | not started |

> ⚠️ **Plan numbers shifted.** Plan **021** was claimed by this increment, so Spec 011's HTTP phases moved to
> **022–025**. Spec 011, ADR 0023 and Plan 020 were all reconciled — a stale cross-reference was caught by the
> whole-branch review and fixed. Do not resurrect the old numbering.

## 2. Exact state

- **Branch:** `main` @ **`a7eef3b`** (merge commit), **pushed**. `fix/exchange-panic-safe-cleanup` **deleted** (local;
  it was never pushed, so there is no remote copy).
- **`git status --short`:** only ` M .claude/settings.json` — a **pre-existing, intentional, unrelated** local change.
  It was deliberately kept out of every commit. **Leave it alone.**
- **Commits merged (oldest → newest):**

  | SHA | Commit |
  |-----|--------|
  | `2a7fa7a` | `fix(core)` identity-checked `deregister` (+ Spec 012, ADR 0022 Addendum A, Plan 021) |
  | `706b375` | `fix(core)` the deferred `settled`-guarded reconciler in `Exchange` |
  | `ba4852a` | `test(core)` panic-drain coverage + the `RequestReplyExchange` SPI contract godoc |
  | `a7de9f9` | `docs(http)` residual reconciliation + security disclosures |
  | `a7eef3b` | merge to `main` |

- **Gate (coordinator re-ran all of it independently — nothing taken on report):** `go test ./... -race -count=2`
  green on all 6 packages; `golangci-lint` **0 issues**; `govulncheck` **no vulnerabilities**; `go mod tidy` leaves
  `go.mod`/`go.sum` **byte-identical**; `go vet`, `gofmt`, `CGO_ENABLED=0 go build`, `go mod verify`, `Example` tests
  all clean. Coverage: root **99.3%**, `adapter/http` + `stdlib` **100%**, and **100% on all 14 `exchange.go`
  functions**.
- **API: strictly behaviour + godoc.** No exported symbol added, removed or changed → **patch** SemVer.

## 3. Traceability pointers — read these first (in order)

1. `CLAUDE.md` (root) — workflow, dependency policy, testing rules, coverage gate, multi-instance rule.
2. `docs/specs/012-exchange-panic-safe-cleanup.md` — the defect, the decided fix, the seven-arm exit table (§5), the
   `deregister` prerequisite (§5.1), the raced-reply policy (§5.3), the seven required test cases (§6).
3. `docs/adrs/0022-messaging-gateway.md` — **Addendum A (A1–A4)**: A1 the reconciler, A2 identity-checked `deregister`,
   A3 the SPI contract, A4 consequences **including the security consequence**.
4. `docs/plans/021-exchange-panic-safe-cleanup.md` — the 4-task plan, all steps ticked.
5. `docs/specs/011-http-adapter.md` + `docs/adrs/0023-http-channel-adapter.md` — the HTTP adapter; **Phase 2 is next**.
6. `.superpowers/sdd/progress.md` — the full session ledger (git-ignored scratch, **not** in git; will not survive a
   fresh clone — this handover is the durable record).

## 4. What shipped, and the two things worth knowing

**Two defects were fixed, not one.** The increment set out to fix a leak; the design audit found something worse.

1. **The panic leak (the stated goal).** `Exchange` called its `giveUp` cleanup at **three explicit call sites**. The
   request channel is a `DirectChannel`, which runs handlers **synchronously on the caller's goroutine**, so a
   panicking application handler is a panicking `request.Send` — and the panic unwound past all three, leaking the
   correlator entry forever. Replaced by **one deferred reconciler guarded by a `settled` flag**, set only in the
   `case reply, open := <-slot:` arm (the one state where the slot is provably no longer ours — running `giveUp` there
   would **deadlock** on an emptied, never-closed channel). **No `recover()` was added**: panic transparency is a hard
   requirement.
2. **Delete-by-id (found by the round-1 design audit, worse than the target bug).** `deregister` deleted by correlation
   id **without checking slot identity**. With a reused id, one caller could evict another's slot — **silently dropping
   a committed reply** (a G4 violation) and **orphaning** the victim's slot, whose owner then blocked **forever** in
   `giveUp`: unreachable by `deliver`, uncloseable by `closeAll`, no `ctx` escape. Fixed with `ok && s == slot`. This
   was reachable *because* the increment makes id reuse succeed — so it had to land in the same increment, first.

**⚠️ ACCEPTED SECURITY TRADE — know this before touching the correlator.** Fixing the leak converts a **fail-closed**
outcome into an exploitable one under an already-warned opt-in. With `msghttp.WithTrustedCorrelationID` (client-supplied
keys), a flow that dispatches to a worker and *then* panics now frees the id immediately, so a late reply can reach
whoever claims that id next. The whole-branch security review **proved the hijack on the fixed tree** and proved it
**fail closed on the pre-fix tree**. It was accepted because the same review also proved the *identical* hijack already
works on **both** trees via the **timeout** arm — so this is a **fourth trigger** for a pre-existing, opt-in-gated
hazard (ADR 0022 audit N1), and the alternative is the unbounded leak. Disclosure is now in `register`'s godoc,
`WithTrustedCorrelationID`'s security warning, ADR 0022 A4 and Spec 012 §3.

**🔬 FORWARD AUDIT TRIGGER — treat any correlator change as a design-gate event.** `giveUp`'s drain is a bare
`<-slot` with **no `ctx` escape**. Its boundedness rests **entirely** on the A2 invariant: a slot leaves `waiters` only
via `deliver` (then committed to a non-blocking `cap 1` send) or `closeAll` (which closes it). **Any future removal path
that neither sends nor closes — a TTL reaper, an eviction, a shard rebalance — reintroduces the permanent hang.**

## 5. Next actions

1. **Start a fresh design cycle** — `superpowers:brainstorming` → spec/ADR/plan → **adversarial Opus audit of the
   complete bundle** → ask the user → SDD. Candidates:
   - **Plan 022 / Spec 011 Phase 2 (recommended)** — HTTP outbound: **O1** webhook `OutboundAdapter` + **O2**
     `NewExchange` as a real `RequestReplyExchange`. It is the first genuine **Return Address** case, and the **second
     implementation of the SPI contract this increment just wrote** — it holds its own request-scoped state (an
     in-flight `*http.Request`, a response body to close) that can leak by exactly the mechanism just fixed, so A3's
     contract binds it directly. Open point to resolve there: whether `Producer` already applies a `RetryPolicy` to
     `OutboundAdapter.Send` (ADR 0023 §5) — if yes, no adapter-side backoff.
   - Resequencer; `redis`/`pgx`/`nats` group stores; aggregate-by-expr.
2. **Backlog (triaged, not blocking):**
   - `exchange_test.go`'s `asyncEcho` uses `context.Background()` where CLAUDE.md mandates `t.Context()` (pre-existing).
   - Consider a defensive `select` with a clock escape on `giveUp`'s drain, so a future invariant break degrades to an
     error instead of a hang.
   - **`gorelease` cannot verify SemVer on this repo at all** — there are **zero git tags**, so it reports "inferred
     base version: none". Every increment so far has established API compatibility *by construction* instead. Cutting a
     first tag (e.g. `v0.1.0`) would close this blind spot permanently.

## 6. Gotchas / environment

- **Go 1.25 pin:** always `GOTOOLCHAIN=go1.25.12` (a bare `go1.25` is rejected — "a language version but not a
  toolchain version").
- **`gofumpt` is NOT installed** in this environment; `golangci-lint` and `govulncheck` (via `go run`) are available.
- **Blackbox tests only** (`package msgin_test`). `replyCorrelator` is unexported — the sanctioned probe for slot
  residency is `ErrDuplicateCorrelation` on id reuse.
- **Never call `require`/`t.Fatal`/`t.FailNow` from a spawned goroutine** — off the test goroutine `t.FailNow` calls
  `runtime.Goexit`, abandoning in-flight state so `goleak` reports a straggler storm that **masks** the real failure.
  Record into a buffered channel and assert on the test goroutine. This bit us once and is now a standing rule.
- **Measure interleaving tests, don't trust them.** A concurrent test on this branch passed, was race-clean and
  line-covered while hitting its target arm **0 times in 200 iterations**. Only instrumentation caught it; the fix was
  `runtime.Gosched()` on alternating iterations (now ~50/50). Apply the same scepticism to any new concurrency test.
- **`.superpowers/sdd/` is shared scratch across plans.** Stale `task-N-report.md` files from a *previous* plan sit at
  the exact paths the next plan writes to — one nearly reached a reviewer as evidence for unrelated code. Archive or
  delete them when starting a new plan.
- **Leave `.claude/settings.json` alone** — intentional pre-existing local modification, unrelated to this work.
