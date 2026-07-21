# Session handover — msgin

> **READ FIRST, before doing anything.** Read `CLAUDE.md` (root), then §3's artifacts. Trust those files over this
> handover and over any memory. **Safepoint: `go test ./... -race` green on all 6 packages, lint 0 issues,
> `govulncheck` clean, working tree clean apart from `.claude/settings.json`.**

## 1. Objective & roadmap position

**Plan 023 — producer-side outbound retry — is DELIVERED, gate-cleared and MERGED to `main`.**

| Plan | Scope | State |
|---|---|---|
| **023** — core producer retry | Spec 013 / ADR 0025 | ✅ **MERGED.** 5 tasks + 2 gate-fix commits. |
| **022** — `HeaderMessageID` rename | ADR 0026 | **Plan written + round-3 audited, NOT started.** Next increment. |
| **024** — HTTP outbound O1/O2 | Spec 011 Phase 2 | **NOT WRITTEN.** 40-defect brief at `docs/plans/024-http-outbound-source-brief.md`. |

**NEXT ACTION: Plan 022, on a FRESH branch off `main`** (`refactor/header-message-id-rename`). ADR 0026 §4 requires the
rename to ship alone. Its Step 0a precondition is now satisfied: ADR 0026 and Plan 022 are already on `main`, so the
branch needs no cherry-pick. Run Step 0 (`test -z "$(git status --porcelain)"`) first — `.claude/settings.json` is
permanently dirty and must be stashed, never committed.

## 2. What shipped in Plan 023

New exported surface (all additive → **minor** bump): `WithProducerRetry`, `WithProducerRetryAfterCap`,
`WithProducerRetryBudget`, `WithProducerDeadLetterTimeout`, `WithProducerHooks`, `WithProducerLogger`; `RetryAfter`;
`BytesPayloadCodec`; sentinels `ErrInvalidRetryAfterCap`, `ErrInvalidRetryBudget`, `ErrInvalidDeadLetterTimeout`,
`ErrUnboundedRetry`, `ErrDeadLettered`, `ErrRetryBudgetExhausted`.

Defaults, and the two inequalities that are load-bearing (NOT the numbers):
`cap 60s < budget 2m < adapter/database/sql's 5m defaultLeaseTTL`, floor 100ms, dead-letter timeout 30s.
A budget below the cap would defeat `Retry-After` compliance; a budget above the lease would let the source reclaim
and redeliver mid-send.

Coverage: **every function in `producer.go`, `codec.go`, `backoff.go` at 100%**; package total 99.1% → 99.2%.

## 2.1 ⚠️ The lesson that cost the most this session

**"Fix the class, not the instance" recurred FOUR times**, twice in my own fixes:

1. Plan 022 took three audit rounds — `git add -A` → a curated path list (incomplete *and* polluting) → `git add -u`
   behind a precondition that did not actually assert tree state.
2. Round 2 of Plan 023 found the defaults changed in the constants block only, leaving six stale sites incl. two tests
   that would HANG.
3. The budget fix for one finding reopened the ~900k-attempt flood through `MaxAttempts > 0`.
4. The `jitter` overflow fix guarded `>= MaxInt64` but not `NaN` — **the same defect class, in the sibling function of
   the same commit.** Only caught by the whole-branch `/security-review`.

**And twice a test was 100%-covered while pinning nothing**: the cancellation divert (mutating it to silently drop the
message left the suite green) and `BytesPayloadCodec`'s nil round-trip (`assert.Empty` accepts `[]byte{}`). Both found
by mutation testing in the whole-branch review, not by coverage. **Mutate before believing a green test.**

## 2.2 Process finding worth keeping

Implementing exposed defects two full audit rounds had missed — a self-contradictory gate, a commit body contradicting
its own fix, and **five places where Spec 013 / ADR 0025 / MESSAGING.md disagreed with the code**, including the ADR
specifying `Retry-After` semantics BACKWARDS (a server could shorten the client's backoff to zero) and MESSAGING.md
documenting cenkalti/backoff as an accepted core dependency in four places. None was findable by reviewing a design
against itself. **Prefer writing the code earlier; let the compiler and tests carry the verification.**

## 3. Traceability pointers — read in this order

1. `CLAUDE.md` — workflow, dependency policy, testing rules, coverage gate, multi-instance rule, commit discipline.
2. `docs/plans/022-header-message-id-rename.md` — **the active artifact.** Read Step 0a and Step 0 first.
3. `docs/specs/013-producer-outbound-retry.md` and `docs/adrs/0025-producer-outbound-retry.md` — ⚠️ **both still
   describe the PRE-AUDIT design.** Plan 023 Task 5 Step 2 reconciles them. Do not treat them as current.
4. `docs/specs/013-producer-outbound-retry.md` + `docs/adrs/0025-producer-outbound-retry.md` — now RECONCILED with the shipped code (Accepted).
5. `docs/plans/024-http-outbound-source-brief.md` — the input to Plan 024.
6. `docs/specs/011-http-adapter.md` §3.4 — the O1/O2 design.
7. `docs/specs/012-exchange-panic-safe-cleanup.md` §7 + `docs/adrs/0022-messaging-gateway.md` Addendum A3 — the
   `RequestReplyExchange` no-leak-on-unwind contract that O2 is the first external implementation of.

## 4. What happened this session, and the decisions taken

### Plan 022 — round-3 audit returned NOT READY (3 critical + 4 major), all folded in

The critical one is instructive: the plan's own self-declared "remaining judgement call" — Step 8's 35-entry
expected-file allow-list — **was in fact wrong**. It omitted three files carrying `HeaderID` *code* references
(`framing_test.go`, `outbound_unit_test.go`, `source_unit_test.go`) because it had been derived from the *prose*
survey rather than the union of both greps, so `COMMIT CONTENTS EXACT` could never have printed on a correct commit.
Corrected to **38** entries (23 Go + 15 markdown) and the derivation formula made normative over the literal list.

Also: Step 0's precondition was **vacuous** (it named a file that was no longer dirty while `.claude/settings.json`
*was*, and `git add -u` would have swept it in) → replaced with a `test -z "$(git status --porcelain)"` state
assertion. And there was **no branch step at all** → new Step 0a, plus the sequencing decision in §1.

**The three-round meta-lesson, which matters more than any single item:** each round fixed the *instance* the previous
auditor named, and each time the *class* re-manifested through a different file — `git add -A` → a curated path list
(itself incomplete **and** polluting) → `git add -u` behind a precondition that did not actually assert tree state.
The durable fix was always to assert an invariant, never to enumerate known-bad cases. **Apply this to 023/024.**

### ADR 0026 — a real defect fixed

Its migration SQL covered the queue/outbox `headers JSONB` column but **not** the aggregator group-member table, whose
`headers` column is **`BYTEA`** (`adapter/database/sql/postgres/groupddl.go:48`), where the `-`/`?` jsonb operators do
not exist. A consumer following the ADR would have migrated half their data and silently left the group store on the
dead key. Both forms are now given, with a recommendation to drain the group store instead.

### Plan 023 — written, then round-1 audited by two independent Opus auditors. Both returned NOT READY.

Everything is folded in; the plan's "Round-1 audit of THIS plan" section is the authoritative record. The
**behaviour-changing** ones:

1. **Message LOST on cancel-during-backoff** — the *common* cancellation path returned without diverting, which is
   exactly the loss the detached-ctx fix claims to prevent. The original fix only covered the narrow "already
   cancelled at exhaustion" case its own test constructed. **This is the single most important finding of the session.**
2. **The divert was uncancellable AND untimed** — `context.WithoutCancel` with no deadline meant a hung DeadLetter sink
   blocked the caller's goroutine forever, immune to their own cancel: strictly *worse* than the un-retried
   passthrough. Now `WithTimeout(WithoutCancel(ctx), 30s)` + `WithProducerDeadLetterTimeout`.
3. **`ErrUnboundedRetry` let a ~900,000-attempt flood through** — it tested `Backoff == nil`, missing
   `ExponentialBackoff{}` (a non-nil interface whose `Delay` is always 0). Now `MaxAttempts == 0 && delayFor(1) <= 0`.
4. **Defaults were unsafe in both directions** — the 5m `Retry-After` cap was 2.5× the worst legitimate value its own
   godoc cited, and the 15m budget **outlived `adapter/database/sql`'s 5m default lease**, so the source would reclaim
   and redeliver the message mid-send. Now **cap 60s < budget 2m < the 5m lease**, floor raised 1ms → 100ms.
   **Those two inequalities are the load-bearing part, not the numbers.**
5. **The default budget silently truncated an explicit `MaxAttempts`** and dead-lettered indistinguishably from genuine
   exhaustion. The default budget now applies only when `MaxAttempts == 0`.
6. **`OnDeadLetter` never fired when the divert failed** — no telemetry for the most operationally important event the
   loop can produce. Now fires on both arms.
7. **`jitter` had the identical overflow** to the one Task 2 fixes — measured at 1.29e19 for an uncapped policy at
   attempt 33 — so Task 2 would have claimed to close a class it left half open.
8. **Task 4's own table could not pass** — `scriptedOutbound` isn't a `LiveValueSource`, so the DLQ payload assertion
   compared raw bytes against base64. Task 3 is now a hard prerequisite of Task 4.
9. **The coverage gate was unachievable** — `reliability.go` has two *pre-existing* blackbox-unreachable arms
   (`isPermanent` nil 83.3%, `NativeRedelivery` 0.0%, verified by running coverage). Gate rescoped.

**Two defects the main session found independently before the auditors reported them** (both then confirmed):
the `backoff.go` fix as first drafted would have **broken the existing passing test** `"overflow without Max returns
Initial"` and turned a 1s fallback into a 292-year delay on a live consumer path; and the producer had **no logger**,
so the hook-panic `recover()` silently discarded it, inconsistent with `consumer.safeFire` (`consumer.go:807`).

### Design decisions taken this session (beyond the audit folds)

1. **`Retry-After` is a MINIMUM, not an override** (RFC 9110 §10.2.3). The drafted design let a server *shorten* the
   client's backoff to zero — a remote-triggerable hot spin. Effective wait = `max(computed, min(server, cap))`.
2. **`BytesPayloadCodec` is explicit, never an automatic default for `T == []byte`** — auto-switching would silently
   change the on-wire format of the sql/redis/nats adapters, which persist base64 envelopes today.
3. **`WithProducerLogger` added**, matching `NewConsumer`'s discard-logger default.
4. **`ErrUnboundedRetry` makes `RetryPolicy{}` valid for a Consumer but invalid for a Producer** — a deliberate,
   documented asymmetry: on the consumer "retry forever, immediately" means broker redelivery; on the producer it is a
   spin on the caller's goroutine.

## 5. ⚠️ Next actions — READ THIS BEFORE DOING ANYTHING

1. **Run a round-2 adversarial audit of Plan 023. This is MANDATORY and is the next action.** CLAUDE.md requires a
   re-audit when the round-1 fixes destabilize the design, and these did: the retry loop's control flow, three
   defaults, the validation predicate, the divert's context, and the coverage gate all changed. **Two rounds is the
   established norm on this project and round 1 was not clean.** Plan 023's Self-review lists the still-open items to
   aim the auditor at — in particular the **assert-closure violations in its own embedded tests** (a hard project
   rule), the **architecture-dependent red step** in Task 2, and the **understated reachability** of the `backoff.go`
   bug (`poller.go:132` busy-spins at full CPU after ~16 minutes of continuous poll failure — it should probably be
   re-framed as an availability fix with regression coverage on `pollErrorBackoff`, not just on `Delay` in isolation).
2. **Then implement Plan 023 via SDD** (`superpowers:subagent-driven-development`), a fresh implementer per task.
   Per-task commits are pre-authorized by CLAUDE.md once the plan is approved; `git push`, merges and branch deletion
   still need explicit per-action approval.
3. **Then Plan 022**, on a fresh branch off the updated `main` (Step 0a).
4. **Then write Plan 024** from `docs/plans/024-http-outbound-source-brief.md`, and give it its own two-round audit. Its
   `/security-review` is not a formality — it introduces an outbound network client.

## 6. Plan 024 source material

`docs/plans/024-http-outbound-source-brief.md` (~470 lines) condenses the withdrawn combined plan's Tasks 3–7 and
catalogues **40 numbered defects** in that drafted content, each marked VERIFIED or REPORTED-ONLY, plus 8 open
decisions with recommendations. **It supersedes the scratchpad file** the previous handover pointed at, which was
machine-local and will not survive a clone. The highest-severity items:

- **Reflected XSS (CRITICAL, verified)** — `buildReply` writes the remote server's `Content-Type` onto the *reserved*
  `msgin.HeaderContentType`, the exact key `EncodeResponse` trusts as the response media type (`encode.go:193-197`)
  and that `DecodeRequest` deliberately refuses clients. Must use the non-reserved constant at `encode.go:25`.
- **Redirect-following SSRF (CRITICAL)** — `validateURL` is construction-time only and the default client follows 10
  redirects, so O2 returns IMDS credentials into the flow. Needs `CheckRedirect: http.ErrUseLastResponse`, which also
  makes the `3xx → Permanent` arm live rather than dead code.
- `payloadBytes` **does not exist** (the drafted plan says to reuse it); the `Gateway` test **cannot compile**
  (`Gateway` exposes `Request(ctx, Req) (Rep, error)`, not `Exchange`); "drops into `Gateway` unchanged" over-claims
  because `Gateway` has **no codec** at all.

**Two invariants Plan 023's audit fixed onto Plan 024** — they are recorded in Plan 023's audit section and must be
carried across:
- Outbound classification must **never** derive `Permanent` from a remote-controlled status alone. Because
  `isPermanent` short-circuits with no dead-letter, a `413 → ErrPayloadTooLarge` mapping would hand a hostile endpoint
  a one-response "make the producer give up and record nothing" switch.
- Any remote body/status text embedded in a classification error must be **length-capped and CR/LF-stripped** — this
  increment is what makes remote-influenced error text reach caller logs.

## 7. Gotchas / environment

- **Go 1.25 pin:** always `GOTOOLCHAIN=go1.25.12` (a bare `go1.25` is rejected).
- **`gofumpt` is NOT installed**; `golangci-lint` is (and is clean); `govulncheck` runs via `go run golang.org/x/vuln/cmd/...`.
- **`.golangci.yml` sets `linters.default: none`** and enables only `govet, staticcheck, ineffassign, misspell` — in
  particular **`unused` is NOT enabled**, so do not expect it to flag a temporarily-unused helper.
- **`gofmt -l . && …` never fails a chain** (`gofmt -l` exits 0 while listing). Use `test -z "$(gofmt -l .)"`.
- **This machine is `darwin/arm64`.** An out-of-range float→int conversion **saturates** here (`MaxInt64`) but yields
  `MinInt64` on amd64. The `backoff.go` negative-duration bug is therefore **amd64-only — real on CI and on every
  Linux server, invisible locally.** Measured, not assumed.
- **Blackbox tests only**; assert-closure tables; `t.Context()`; `goleak`.
- **Never call `require`/`t.Fatal`/`t.FailNow` from a spawned goroutine** — `t.FailNow` off the test goroutine calls
  `runtime.Goexit`, producing a `goleak` straggler storm that masks the real failure.
- **Retry tests must use `clockwork.NewFakeClock()`** and **`BlockUntilContext`**, never the deprecated `BlockUntil`.
  (Correction to the previous handover: `clockwork.Advance` **does** re-append tickers via `setExpirer`; the earlier
  claim that it "never appends waiters" was wrong. Immaterial for timers, but do not rely on it.)
- **Measured waits must be two-phase** — `Advance(want - 1ns)`, assert not-yet-returned, then `Advance(1ns)`. A
  one-shot `Advance(want)` followed by "did it return?" is true by construction and cannot detect *under*-waiting.
- **`gorelease` cannot verify SemVer** — zero git tags. Compatibility is by inspection. Cutting `v0.1.0` would close
  that standing blind spot, and ADR 0026 is a further argument for doing it soon: the rename is free only until the
  first tag exists.
- **Leave `.claude/settings.json` alone.**
