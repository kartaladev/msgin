# Session handover — msgin

> **READ FIRST, before doing anything.** Read `CLAUDE.md` (root), then the governing artifacts in §3 —
> **`docs/specs/011-http-adapter.md`**, **`docs/adrs/0023-http-channel-adapter.md`** (incl. **Addendum A**), and
> **`docs/plans/020-http-adapter-inbound.md`** — and trust those files over anything in this handover or in memory.
> They were reconciled against the shipped code in `c691f67`, so they now describe what actually exists.
> The branch is at a clean safepoint: **all code is written, reviewed, gate-clean and committed; nothing is merged.**

## 1. Objective & roadmap position

**Building:** the **HTTP channel adapter** for `msgin` (Spec 011) — the sixth adapter family, and the first external
transport built on the `RequestReplyExchange` SPI from Plan 019.

| Phase | Plan | Content | Status |
|-------|------|---------|--------|
| **1** | **020** | `adapter/http` (`msghttp`) + `adapter/http/stdlib`: **I1** async + **I2** sync gateway | **COMPLETE — awaiting merge approval** |
| 2 | 021 | `adapter/http/stdlib` outbound: **O1** webhook + **O2** request-reply (`RequestReplyExchange`, Return Address) | not started |
| 3 | 022 | SSE **server** (S-out) | not started |
| 4 | 023 | SSE **client** (S-in, `StreamingSource`) | not started |
| 5 | 024 | `adapter/http/gin` module + **ADR 0024** (gin dependency) | not started |

**Active position: Plan 020 delivered; the only remaining step is the user's merge decision.**

## 2. Exact state

- **Branch:** `feat/http-adapter-inbound`, **7 commits ahead of `main`**, **not pushed, not merged**.
- **Base:** `main` @ **`7f9b544`** — verified with `git merge-base main HEAD`. ⚠️ The *previous* handover claimed
  `57b3ffd`; that was **wrong**. Reviewing against it silently included two commits already on `main`.
- **`git status --short`:** only ` M .claude/settings.json` — a **pre-existing, intentional, unrelated** local change.
  It was deliberately kept out of every commit this session. **Leave it alone.**
- **Commits (oldest → newest):**

  | SHA | Commit |
  |-----|--------|
  | `8df9611` | `spec:` Spec 011 |
  | `fb9d48b` | `docs:` ADR 0023 + Plan 020 (design, audited) |
  | `2c7a886` | `docs:` previous handover |
  | `6db0c12` | `feat(http)` Task 1 — `msghttp` core: Config/options, `DecodeRequest`/`EncodeResponse` |
  | `99e3cb1` | `feat(http)` Task 2 — I1 async: `ServeAsync` + `stdlib.NewInbound` |
  | `8ce81d0` | `feat(http)` Task 3 — I2 gateway: `ServeGateway` + `NewInboundGateway` + `Register` |
  | `f6bff4c` | `test(http)` Task 4 — Examples + minors + mechanical gate |
  | `e6f9a77` | `fix(http)` whole-branch review + security findings (F1–F10) |
  | `1a9fe20` | `fix(http)` residual godoc, `ErrAbortHandler` passthrough, advisory-id rename |
  | `c691f67` | `docs:` reconcile Spec 011 / ADR 0023 / Plan 020 with shipped code |

- **Safepoint: yes.** `go test ./... -race` green on all 6 packages; `adapter/http` and `adapter/http/stdlib` both at
  **100.0%** statement coverage; `go vet`, `gofmt`, `golangci-lint` (0 issues), `govulncheck`, `CGO_ENABLED=0 go build`,
  `go mod tidy` (no diff) and `go mod verify` all clean. **No new dependency.**
- **API:** strictly **additive** relative to `main` (both packages are new; the only root-module change is a
  comment-only hunk in `errors.go`) → **minor SemVer bump**.

## 3. Traceability pointers — read these first (in order)

1. `CLAUDE.md` (root) — workflow, dependency policy, multi-instance rule, sensible-defaults + coverage gates.
2. `docs/specs/011-http-adapter.md` — Phase 1 marked DELIVERED and rewritten to the as-shipped design.
3. `docs/adrs/0023-http-channel-adapter.md` — architecture **plus Addendum A (A1–A6)**, the review-driven changes.
   **A1 and A2 are recorded as genuine architectural *reversals*, not refinements** (see §4).
4. `docs/plans/020-http-adapter-inbound.md` — the plan, all steps ticked, with a "Delivered — outcome and deviations" section.
5. **`docs/specs/012-exchange-panic-safe-cleanup.md`** — the **open follow-up** this branch deliberately did not fix (§4).
6. Reused context: `docs/adrs/0022-messaging-gateway.md` + `docs/specs/010-messaging-gateway.md` (the
   `RequestReplyExchange` SPI), `docs/adrs/0002-adapter-spi.md`, `docs/adrs/0001-message-payload-typing.md`.
7. Full session record incl. every review verdict: `.superpowers/sdd/progress.md` (git-ignored scratch — **not** in git;
   it will not survive a fresh clone, so this handover is the durable record).

## 4. Decisions, deviations & the one open residual

**Execution:** SDD — a fresh implementer subagent per task, coordinator verified green + committed, an adversarial
reviewer per task, then whole-branch `/code-review` + `/security-review` (both Opus), one consolidated fix wave, and a
re-review. Final verdicts: code review **READY TO MERGE: YES** (0 Critical / 0 Important), security **NO BLOCKER**
(0 HIGH). The re-reviewer re-ran the original attack reproductions itself and applied **16 mutations, 15 caught**.

**User decisions made this session (all already implemented):**
1. **Correlation id** — server-mint by default **plus** a separate explicit opt-in. `WithAdvisoryCorrelationID`
   (renamed from `WithCorrelationID`) is advisory-only → non-reserved `http.correlation-id`;
   **`WithTrustedCorrelationID`** is the sole path to a client-keyed exchange and carries a security warning.
   *Why:* a **proven cross-user reply hijack** — attacker reusing a victim's id received the victim's reply body.
2. **Content-Type** — full decouple: client `Content-Type` → non-reserved `http.content-type`; always
   `X-Content-Type-Options: nosniff`; response defaults to `application/octet-stream`.
   *Why:* **proven reflected XSS** — the client could choose the response media type, bypassing the response allow-list.
3. **Core fix scoped out** — adapter-side `recover()` now; the core root cause is its own increment (see residual).
4. Post-re-review polish: corrected the residual godoc, re-panic on `http.ErrAbortHandler`, and the advisory rename.

**Deviations from the audited plan** (all recorded in ADR 0023 Addendum A and Plan 020's deviations section):
`statusFor` was **deleted** (a hand-built `&Config{}` / `nil *Config` is externally constructible and used to panic —
the plan's premise that a `Config` only comes from `NewConfig` was false); replaced by nil-safe per-field accessors.
New exported symbols: `DefaultErrorStatus`, `ErrDecodeRequest`, `ErrWriteResponse`, `WithTrustedCorrelationID`.

**Two latent defects in Plan 020 were found and fixed in the doc:** `request.Subscribe(To(reply))` does not compile
(`Subscribe` takes a `MessageHandler`, `To` returns a `Step` → use `Chain(To(reply))`), and a commit-message template
that contradicted the binding 500/409 mapping. **Nothing shipped wrong** — `8ce81d0` already had the correct mapping.

**⚠️ OPEN RESIDUAL — `docs/specs/012-exchange-panic-safe-cleanup.md`.** A panicking flow handler still leaks a
`ChannelExchange` correlator slot: `exchange.go` registers the reply waiter **before** sending and its `giveUp` cleanup
is **not** `defer`red, so the adapter's `recover` contains the panic (clean 500, server keeps serving) but cannot
reclaim the slot. On the **default** path the impact is **memory-only** — a fresh server-minted id per request means no
leaked slot is ever re-keyed. The 409-poisoning variant needs the opt-in `WithTrustedCorrelationID` with a reused
client value. Either way it requires a **panicking handler** — a bug in the consumer's own code, not attacker-reachable
alone (but attacker-*amplifiable*). The branch strictly **improves** the prior state, where the panic escaped into
`net/http` and aborted the connection. Accepted for merge knowingly; **the fix is core-side and needs its own design cycle.**

**Pending approval (BLOCKING):** the user has **not** approved merge, push, or branch deletion. Per CLAUDE.md these are
per-action and never standing.

## 5. Next actions

1. **Ask the user for merge approval.** On approval: `git checkout main && git merge --no-ff feat/http-adapter-inbound`,
   then **ask again** before `git push`, then delete the branch (`git branch -d feat/http-adapter-inbound`, and the
   remote copy if it was ever pushed — it was not).
2. **Then start the next increment with a fresh design cycle** (brainstorm → spec/plan/ADR → **adversarial audit** →
   ask → SDD). Candidates, user's choice:
   - **Spec 012** — the `exchange.go` panic-safe cleanup fix (small, closes the residual above).
   - **Plan 021 / Phase 2** — HTTP outbound: O1 webhook + O2 request-reply (the first real **Return Address** case).
   - Resequencer, `redis`/`pgx`/`nats` group stores, or aggregate-by-expr.
3. **One point deferred to Phase 2** (recorded in ADR 0023 §5): whether `Producer`/outbound already applies a
   `RetryPolicy` to `OutboundAdapter.Send`. If yes, O1/O2 add no adapter-side backoff (reliability stays runtime-owned
   per ADR 0002); if no, Phase 2 adds a thin producer-side retry.
4. **A forward risk to decide before v1** (ADR 0023 Addendum): one `Config`/`Option` type serves all six HTTP modes.
   Every future option is additive, but the end state is a `Config` where most options are inert for a given mode
   (`WithSuccessStatus` on I2 is already the first case). Splitting into per-mode configs **later would be a major bump**
   — decide deliberately: accept it and add a per-option applicability matrix to `doc.go`, or split before v1.

## 6. Gotchas / environment

- **Go 1.25 pin:** always `GOTOOLCHAIN=go1.25.12` (bare `go1.25` is rejected — "a language version but not a toolchain version").
- **`adapter/http` + `adapter/http/stdlib` are ROOT-module packages** — no new `go.mod`, no `go.work` change, **no new
  dependency**. The gin dependency + its own module land only in Phase 5 (ADR 0024).
- **Package name is `msghttp`** (directory `adapter/http`) to avoid shadowing `net/http`.
- **Tests are fully hermetic** — `httptest.Server` / `httptest.ResponseRecorder`, **no testcontainers**. `goleak`
  `VerifyTestMain` in both packages. Blackbox `_test` packages only; assert-closure tables; `t.Context()`.
- **Inbound is the untrusted boundary.** Any change here re-runs `/security-review`, not just `/code-review`.
- **`gopls`' MCP server disconnected mid-session** — the `LSP` tool may be unavailable; fall back to standard Go tooling.
- **Leave `.claude/settings.json` alone** — intentional pre-existing local modification, unrelated to this work.
