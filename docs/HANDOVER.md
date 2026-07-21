# Session handover — msgin

> **READ FIRST, before doing anything.** Read `CLAUDE.md` (root), then §3's artifacts. Trust those files over this
> handover and over any memory. **Safepoint: the tree builds, `go test ./... -race` is green on all 6 packages, and no
> Go file has been touched this session.** All work so far is design documents only.

## 1. Objective & roadmap position

**Previous increment (DELIVERED, merged + pushed):** Plan 021 — panic-safe `ChannelExchange` cleanup.

**Active work: producer-side outbound retry + HTTP outbound, now SPLIT INTO THREE INCREMENTS** after a round-1
adversarial audit returned **3 critical + ~20 major findings**. The original single combined plan was withdrawn.

| Plan | Scope | State |
|---|---|---|
| **022** — `HeaderMessageID` rename | ADR 0026; unrelated to retry, zero coupling | **Plan WRITTEN, round-1 fixes folded in, round-2 audit was IN FLIGHT when this session ended.** Ready to implement once that audit clears. |
| **023** — core producer retry | Spec 013 / ADR 0025 | **NOT WRITTEN.** All design decisions settled (§4); audit findings to fold in are in §5. |
| **024** — HTTP outbound O1/O2 | Spec 011 Phase 2 | **NOT WRITTEN.** Source material preserved (§6). |

**Exact position: writing Plan 023 is the next authoring task; implementing Plan 022 is the next execution task.**

## 2. Exact state

- **Branch:** `feat/producer-retry-http-outbound`, off `main` @ **`1f17e64`**. **Two docs-only commits on it:**

  | SHA | Commit |
  |---|---|
  | `df7eacb` | `spec: producer-side outbound retry, and the Spec 011 Phase 2 delta` |
  | *(HEAD)* | `docs: ADR 0025/0026 and the audited rename plan, ahead of the code` — this file is inside that commit, so it cannot carry its own hash; run `git log --oneline main..HEAD` |

  ⚠️ **The branch name is now wrong** — it was named for the combined increment, which no longer exists. Recommended:
  keep this branch for **Plan 023** (core retry, its natural scope) and start **Plan 022** on a fresh
  `refactor/header-message-id-rename` branch off `main`, cherry-picking nothing — the design docs are already committed
  here and will merge independently. Renaming the branch is also fine; it has never been pushed.

- **`git status --short`** — exactly one entry, and it must stay that way:

  ```
   M .claude/settings.json     ← pre-existing, intentional, UNRELATED. Leave it alone. Do NOT commit it.
  ```

- **All design work is COMMITTED** (user-approved, 2026-07-22), under CLAUDE.md's documented docs-ahead-of-code
  handoff exception: a complete, gate-cleared design committed standalone so it survives a fresh clone or tree wipe.
  Specs went in their own `spec:` commit per the convention; the ADRs, plan and this file rode together.

- ✅ **Plan 022's Step 0 precondition is now SATISFIED.** That step exists because `docs/specs/011-http-adapter.md` was
  dirty with unrelated Plan 023/024 design that would have been swept into the "pure rename" commit (round-2 finding
  C2). `df7eacb` landed it properly, so the tree is clean of everything on Plan 022's edit list. **Step 0 must still be
  RUN** — it is a precondition check, not a one-time chore — but it should now pass immediately.
- **No Go code changed.** Baseline captured this session: all 6 packages `ok` under `-race`; coverage core **99.1%**,
  `adapter/http` **100.0%**, `adapter/http/stdlib` **100.0%**, `database/sql` **93.7%**, `memory` **71.3%**,
  `cron` **50.8%**.
- **`.superpowers/sdd/` has been cleaned.** Stale `progress.md`, `task-*-report.md` **and `task-*-brief.md`** (the last
  being what an SDD implementer subagent reads first) plus ~100 review diffs are now under
  `.superpowers/sdd/archive/`. Only historical `plan-0NN-audit-*.md` files remain, which do not collide.

## 3. Traceability pointers — read in this order

1. `CLAUDE.md` — workflow, dependency policy, testing rules, coverage gate, multi-instance rule, commit discipline.
2. `docs/specs/013-producer-outbound-retry.md` — the retry spec. **§7 was rewritten this session** and now records the
   three-way split and why.
3. `docs/adrs/0025-producer-outbound-retry.md` — the retry decision; supersedes ADR 0005's outbound-HTTP clause.
4. `docs/adrs/0026-header-message-id-rename.md` — the rename decision. **§4 and §Consequences revised this session.**
5. `docs/plans/022-header-message-id-rename.md` — the only plan currently written.
6. `docs/specs/011-http-adapter.md` §3.4 — the O1/O2 design (input to Plan 024).
7. `docs/specs/012-exchange-panic-safe-cleanup.md` §7 + `docs/adrs/0022-messaging-gateway.md` Addendum A3 — the
   `RequestReplyExchange` no-leak-on-unwind contract that O2 is the first external implementation of.

## 4. Decisions settled with the user THIS session

1. **O1/O2 live in `adapter/http` (package `msghttp`), not `adapter/http/stdlib`** — `stdlib` binds neutral cores to
   `net/http` *server* types; an HTTP *client* has no framework variant. Needs a Spec 011 §3.0 delta in Plan 024.
2. **`WithProducerRetryAfterCap` defaults to 5 minutes.**
3. **`HeaderID` → `HeaderMessageID`, value → `"msgin.message-id"`** (both the identifier *and* the wire value — a
   deliberate data-format break, free only because the repo has zero tags). ADR 0026.
4. **`cenkalti/backoff` stays OUT.** The user granted permission to add it; the design does not need it (Spec 013 §6.1
   declined it on merit). **CLAUDE.md's dependency policy still wrongly lists it — correcting that is a Plan 023 task.**
5. **`BytesPayloadCodec` + explicit pairing** for the `[]byte` codec defect (§5, C4/M7). Additive core codec; no change
   to SQL/Redis/NATS behaviour; `NewOutbound`/`NewExchange` godoc + every example must pair it explicitly.
6. **Add `WithProducerRetryBudget(d)`** — a cumulative wall-clock budget on the producer's clock, with a safe finite
   default, so ADR 0025 §3's claimed safety property becomes true instead of being weakened to match the code.
7. **Split into three increments** (022 rename → 023 core → 024 HTTP), reversing the earlier "one increment" choice.

## 5. Round-1 audit findings — MUST be folded into Plans 023/024

Three independent Opus auditors over the complete bundle. Findings below are **cross-confirmed and, where noted,
independently verified by the main session against the code**. Do not re-litigate them; fold them in.

### Verified personally (not just reported)
- **`json.Marshal([]byte("payload"))` == `"cGF5bG9hZA=="`.** `resolveCodec` defaults every wire adapter to
  `JSONPayloadCodec`, so `NewProducer[[]byte](msghttp.NewOutbound(url))` — the increment's flagship composition —
  POSTs base64. `JSONPayloadCodec` is the only codec in the repo. → decision §4.5.
- **`payloadBytes` does not exist** in `adapter/http`; the logic is inline in `EncodeResponse`. Any plan claiming to
  "reuse" it is wrong, and extracting it edits a file that must then be staged in the same commit or the tree won't
  compile.
- **`Gateway` exposes `Request(ctx, Req) (Rep, error)`**, not `Exchange`. `Gateway` has **no codec at all**
  (`boxMessage`/`PayloadOf`), so `msghttp.Exchange` behind it works only for `[]byte`/`string` payloads — a real
  limitation of "drops into `Gateway` unchanged" that no artifact records.
- **`clockwork.Advance` never appends waiters** (`fc.waiters = fc.waiters[1:]`), and `BlockUntil` is **`// Deprecated:`**
  in v0.5.0 in favour of `BlockUntilContext` "which offers context cancellation to prevent deadlock". Any fake-clock
  driver looping on `BlockUntil` and expecting a later `Advance` to release it **deadlocks deterministically**.
  Use `BlockUntilContext` (the repo already does, ~10 sites in `aggregator_test.go`).

### CRITICAL — security (Plan 024)
- **Redirect-following SSRF.** `validateURL` checks scheme+host once at construction, but the default client follows
  up to 10 redirects. `302 → http://169.254.169.254/…` makes **O2 return IMDS credentials into the flow**; O1 is a
  blind-SSRF port-scan oracle; 307/308 replays the POST body and custom allow-listed headers (Go strips only
  `Authorization`/`Cookie`) to the attacker's host; https→http downgrade is permitted. **Fix:** default
  `CheckRedirect: func(...) error { return http.ErrUseLastResponse }` — which also makes the `3xx → Permanent`
  classification arm live instead of dead code. Also state plainly that msgin does **no** private-IP/metadata
  filtering, and qualify the "SSRF invariant" language in Spec 011 §4 / Spec 013 §4, which currently reads as a
  guarantee.
- **Reflected XSS reopened from the outbound side.** Writing the remote server's `Content-Type` onto the reserved
  `msgin.HeaderContentType` hands an untrusted party the exact key ADR 0023 Addendum A1 forbids, because
  `EncodeResponse` trusts it as the response media type (`nosniff` does not stop an explicit `text/html`).
  **Fix:** land it on the non-reserved `http.content-type` (constant already exists at `encode.go:25`), mirroring
  `DecodeRequest`; make any trusted variant an explicit warned opt-in.

### CRITICAL/MAJOR — core (Plan 023)
- **Dead-letter runs on the cancelled ctx** → message reaches neither target nor DLQ. Repo precedent for the fix is
  already in the tree: `exchange.go:347` uses `context.WithoutCancel`. Add a covering branch.
- **Unbounded retry / hot spin.** `RetryPolicy{}` (the zero value, valid) + nil `Backoff` = zero-delay infinite loop on
  the caller's goroutine. Remote-triggerable: `parseRetryAfter` honours `Retry-After: 0` and past HTTP-dates, and the
  cap clamps only the *upper* bound. → decision §4.6, plus floor the wait.
- **`Retry-After` must be a MINIMUM, not an override** (RFC 9110 §10.2.3). Current design lets a server *shorten* the
  client's backoff to zero. **Fix:** `max(computed, min(serverDelay, cap))`.
- **The cap is not applied to the computed backoff**, and `ExponentialBackoff.Max <= 0` means uncapped, so an attacker
  who pumps `attempt` cheaply then drops the header detonates an astronomic wait; past `MaxInt64` the float→int
  conversion is out-of-range (yields `MinInt64` on amd64) → negative delay → tight request loop.
- **`ErrUnsupportedPayload` is not `Permanent`** → a missing `Transform` burns the full attempt budget and dead-letters.
  The adapter must `msgin.Permanent`-wrap encode failures.
- **Caller cannot distinguish "dead-lettered" from "failed outright"**, and the producer fires **no hooks and has no
  logger** — a terminal divert is invisible in both the return value and telemetry, against CLAUDE.md's mandatory
  observability constraint. **Fix:** `ErrDeadLettered` sentinel via `fmt.Errorf("%w: %w", ...)`, plus wire the existing
  `Hooks` (`OnRetry`/`OnDeadLetter` already exist at `retry.go:52`) or a `WithProducerLogger`.
- **`Permanent` beats `RetryAfter` is documented everywhere and tested nowhere** — the proposed test asserted only
  `errors.Is(err, cause)`, true of both markers independently.

### MAJOR — other (Plan 024)
- `parseRetryAfter` int64 overflow on a large delay-seconds value → negative → immediate retry.
- `io.ReadAll(io.LimitReader(body, max+1))` overflows when `max == MaxInt64` → `LimitReader(N<0)` returns EOF → **empty
  payload returned as success**, silent data loss.
- Response **headers** are outside `WithMaxResponseBytes` (transport default is ~10 MiB); the option's godoc
  over-claims, unlike its honest inbound twin `WithMaxBodyBytes`.
- `*url.Error` from `client.Do` redacts only the password — **username, host, path and query survive**, so a webhook
  token in the query string lands in every timeout/dial error the caller logs.
- `msghttp.Config` as a shared inbound+outbound bag → `NewOutbound(url, WithMaxBodyBytes(0))` errors on an irrelevant
  setting, and inbound constructors silently accept inert outbound options. Confusable pairs: `WithResponseHeaders`
  vs `WithReplyHeaders`, `WithMaxBodyBytes` vs `WithMaxResponseBytes` — both security-relevant.
- Defensive nil-guards on `Config.client()`/`maxResponse()`/`drainAndClose` are **unreachable blackbox**, so a
  "100% per function" gate is unachievable — and `retry.go`'s `sweepLoop` has a recorded precedent for *declining*
  exactly such guards for that reason.
- `buildReply` starts from `msgin.New` and discards all request headers, so Splitter sequence headers are lost across an
  `OutboundGateway` hop, breaking a downstream Aggregator.
- **Multi-instance:** N instances each retry a throttling endpoint independently, delivering N× the load the server
  asked to shed. CLAUDE.md's rule requires this be stated and the seam named (ADR 0006 rate-limit/breaker).

## 5.1 Round-2 audit of Plan 022 — NOT READY, all findings folded in

Verified personally before folding: **23** `msgin.id` mentions in Go (1 literal + **22 prose**, including
`adapter/database/sql/framing.go:28` inside the block headed *"On-wire header format (stability contract)"*);
`go list ./...` returns **6 packages with ZERO from `harness`**, which holds **16 of the 36** references;
`message_test.go` has **4** references and was missing from the stage list.

| ID | Finding | Fix applied |
|---|---|---|
| C1 | `message_test.go` unstaged → **broken-build commit** | Step 7 now `git add -u` behind a clean precondition + a **bidirectional** staged-vs-modified check |
| C2 | Stage list included `docs/specs/011-http-adapter.md`, **already dirty** with Plan 023/024 design | New **Step 0** makes tree-cleanliness a precondition, names the stash |
| C3 | Step 8's `^docs/` whitelist made C2 invisible to both purity greps | Replaced with an **exact expected-file allow-list** |
| M1 | 22 orphaned `msgin.id` doc mentions, incl. a stability contract + a security CAUTION | New **Step 3b**, combined verification grep, ADR 0026 Context updated |
| M2 | Coverage `diff` a **proven** false failure (cached vs timed), which also `&&`-skipped `go mod tidy` | Both captures normalized + `-count=1`; hygiene unchained |
| M3 | `go build ./...` never reaches the `harness` module | Per-module `GOWORK=off` loop mirroring `ci.yml` |
| M4 | ADR 0026: 4 dead links, §4 stating the opposite of the decision, contradictory bullet, 2 wrong counts | All corrected |

**The meta-lesson, which matters more than any single item:** round 1's fixes were *shallow* — the `git add -A` finding
was remedied by hand-writing a path list that was itself both incomplete (C1) and polluting (C2), and the purity-check
finding was patched for the one file the auditor named rather than for the class (C3). **Fix the class, not the
instance.** Apply this when folding round-1 findings into Plans 023/024.

### Plan-craft lessons (apply to 023/024 as you write them)
- "Measured wait" assertions that `Advance(wantWait)` then assert elapsed == `wantWait` are **true by construction** and
  cannot detect *under*-waiting. Use a two-phase advance: `Advance(want - 1ns)`, assert not-yet-returned, then
  `Advance(1ns)`.
- Every "reuse the existing helper / extend the existing table" instruction must be verified with `gopls` **while
  writing the plan**, not deferred to the implementer. Round 1 found three such claims false.
- Do not put a branch in Task N's coverage table if it is only observable in Task N+2.
- **Commit discipline:** CLAUDE.md couples plan/ADR with the code that realizes them. The withdrawn plan committed all
  docs last, so every `feat` commit carried `Plan:`/`ADR:` trailers pointing at artifacts not yet in history. Either
  invoke the documented docs-ahead-of-code exception explicitly, or couple per task.
- `goleak`: `adapter/http` already has `TestMain` + `VerifyTestMain` (`encode_test.go:21`); adding per-test
  `VerifyNone` around `httptest` servers is flake-prone (idle keep-alive conns).

## 6. Source material

The withdrawn combined plan — which contains **fully-drafted, partly-audited** task content for the core retry loop and
all of O1/O2, with exact code blocks — is preserved at:

`/private/tmp/claude-501/-Users-zakyalvan-Documents-RND-msgin/0113b927-21c7-4c58-9d70-059e24a89f25/scratchpad/SUPERSEDED-022-combined-source-material.md`

⚠️ **It is scratchpad-local and will not survive a machine change.** Mine it for Plans 023/024 early, and treat every
code block in it as *suspect* until checked against §5 — most of the findings above are defects **in that document**.

## 7. Next actions

1. **Run a round-3 audit of Plan 022, then implement it via SDD.** Round 2 returned **NOT READY** (3 critical + 4 major)
   and **all findings have been folded in** — see §5.1. A third round is warranted because round 2's core lesson was
   that round 1's fixes were *shallow* (the `git add -A` remedy was replaced by a path list that was itself both
   incomplete and polluting), so the same failure mode must be ruled out for round 2's fixes. Round 2 confirmed the
   round-1 arithmetic corrections are sound and that the Step 4 test case compiles and passes as written — those areas
   need no re-checking.
2. **Then implement Plan 022 via SDD.** The user has explicitly authorized SDD implementation through to completion, so
   no further per-task go-ahead is needed — but `git push`, merges, and branch deletion still require per-action
   approval.
3. **Write Plan 023** (core retry) folding in §4.5, §4.6 and every core finding in §5. Then run its own two-round audit.
4. **Write Plan 024** (HTTP outbound) folding in the security findings. Its `/security-review` is not a formality.
5. **~~Offer to commit the design artifacts~~ — DONE** (`df7eacb`, `317186e`). Nothing further is needed here; the
   design survives a fresh clone. `git push` has **not** happened and still requires explicit per-action approval.

## 8. Gotchas / environment

- **Go 1.25 pin:** always `GOTOOLCHAIN=go1.25.12` (a bare `go1.25` is rejected).
- **`gofumpt` is NOT installed**; `golangci-lint` is; `govulncheck` runs via `go run golang.org/x/vuln/cmd/...`.
- **Blackbox tests only**; assert-closure tables; `t.Context()`; `goleak`.
- **Never call `require`/`t.Fatal`/`t.FailNow` from a spawned goroutine** — `t.FailNow` off the test goroutine calls
  `runtime.Goexit`, producing a `goleak` straggler storm that masks the real failure. Buffer and assert on the test
  goroutine.
- **Retry tests must use `clockwork.NewFakeClock()`** and **`BlockUntilContext`**, never the deprecated `BlockUntil`.
- **`gorelease` cannot verify SemVer** — zero git tags. Compatibility is by inspection. ADR 0026 is a further argument
  for cutting `v0.1.0`: the rename is free only until the first tag exists.
- **Leave `.claude/settings.json` alone.**
