# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

**Greenfield.** The repo is bootstrapped (git remote, `.claude/` skills) but has **no Go module or source yet**. Everything under "Architecture blueprint" below is the **target design**, not existing code — treat it as the brief to build toward, not something to reverse-engineer.

`msgin` is a Go **library** implementing the messaging patterns from Gregor Hohpe & Bobby Woolf's *Enterprise Integration Patterns* (EIP) — the same conceptual model as the Spring Integration project, reimplemented in idiomatic, minimal-dependency Go. The scope is a **focused subset**, not all of Spring Integration (see "What this is — and is not").

**Do first, as ADRs** (before or with the first `feat` commit): ratify the module path `github.com/kartaladev/msgin` (matches the git remote), the top-level package/API naming, and the open design decisions listed under "Architecture blueprint → Open decisions". As the module takes shape, keep this file's architecture section in sync with the code and add concrete build commands to "Commands".

## Development workflow (mandatory)

Follow this loop for every feature or bugfix, not just large ones. The skills named here are required steps, not suggestions.

1. **Brainstorm first.** Before any creative/implementation work, run `superpowers:brainstorming` to explore intent, requirements, and design. Don't jump to code. For a multi-step task, follow up with `superpowers:writing-plans`.
2. **TDD — red → green → refactor.** Use `superpowers:test-driven-development`. Write a failing test first (red), make it pass with the simplest code (green), then refactor. Never write implementation ahead of a failing test.
3. **Consult the Go skills while coding** (see below) — start from `cc-skills-golang:golang-how-to`, which routes you to the specific `golang-*` skills for the task.
4. **On big or complex features, refactor with `/simplify`** once green, to clean up reuse/simplification/efficiency/altitude before review.
5. **Gate before delivering (committing):** in order — run **`/code-review`** on the diff and address findings, run **`/security-review`** on the pending changes and resolve anything it flags, confirm the **test-coverage gate** (see below) holds, then **re-run the project-wide test suite** (`go test ./... -race`) and confirm it passes. Use `superpowers:verification-before-completion` — evidence before claiming done. Only commit after all pass. For release-bound changes, the full **Library quality gates** (see below) also apply.

**Adversarial design audit — AFTER the plan is written, BEFORE any code (hard rule).** Once the **spec, its ADR(s), AND the implementation plan** all exist, run an **independent adversarial audit** — a fresh subagent (Opus), handed the **complete design bundle together (spec + ADR + plan)** — to attack the design for holes before any implementation code is written. **The plan is part of what gets audited**: auditing the spec+ADR alone misses plan-level flaws (task decomposition, hot-path/coverage gaps, sequencing, the test strategy, sizing). Fold every material finding back into the spec/ADR/plan; **re-audit** when the fixes destabilize the design (two rounds is the established norm on this project). Only after the audited plan is approved does implementation begin. This is the design-time gate; it composes with — and precedes — the "prefer SDD + always ask before implementation" rule below.

**Prefer SDD, and ALWAYS ASK before implementation — hard rule.** For any implementation work that writes or changes non-trivial code (a feature, bugfix, or plan task), the **default and preferred execution mode is subagent-driven development** (`superpowers:subagent-driven-development`): a **fresh implementer subagent per task** writes the code + tests (TDD red→green), the **coordinator (main session) verifies green and commits**, then an **adversarial reviewer subagent** reviews before delivery. The main session must **not self-implement by default**.

- **Never begin writing implementation code without first asking the user.** Design, brainstorming, reading/searching, and planning may proceed, but the transition to *writing implementation code* is gated on an **explicit user go-ahead**. Before that transition, state the approach **and the intended execution mode** (SDD vs. direct main-session implementation) and wait for approval.
- **Direct (non-SDD) main-session implementation is allowed only when the user explicitly approves it for that specific task.** Absent that explicit OK, dispatch an SDD implementer subagent. Approval is per-task, never standing.
- This holds even mid-plan-execution: the per-task-commit pre-authorization (below) covers *committing* green tasks, not the choice to bypass SDD or to start implementing without asking.

**Test-coverage gate (mandatory).** There is **no hard coverage-percentage floor**, but:
- **Target ≥ 85%** statement coverage on every package you change (`go test ./... -cover`; inspect with `go test -coverprofile=… && go tool cover`). Treat a drop below ~85% as a prompt to add tests, not a number to game.
- **Hard requirement — every hot-path logic branch must have a covering test case.** The "hot path" is the library's primary execution path plus **every typed-error branch** (including construction-time validation that returns a typed error — those *are* the debuggability surface). Each `if`/`else`, `switch` case, early-return, condition-gate, and error-return on that path must be exercised by at least one test. A hot-path branch with no test is a delivery blocker, regardless of the overall percentage.
- **Plan for it.** When writing a plan, enumerate the hot-path branches each task introduces and ensure a test case targets each (fold into the existing `table-test` tables). Verify coverage as part of the per-task and whole-branch verification, not as an afterthought.

**Fix review findings before delivering — mandatory.** The gate's `/code-review` and `/security-review` are not advisory: every finding they surface must be **fixed**, or **explicitly triaged to a backlog with a written rationale**, before the work is delivered. Re-run the affected review and the `-race` suite after fixing. **When completing a feature branch, run `/code-review` and `/security-review` over the whole-branch diff** (`main..HEAD`, not just the last commit) as the final pre-merge gate, resolve/triage every finding, and confirm the suite is green — this is the same path used per-increment, applied once more before any merge/push. Never merge or push a branch that still has unaddressed review findings.

**Delete the feature branch after it merges to `main`.** Once the branch is merged, remove it — `git branch -d <branch>` locally, and delete the remote branch too if it was pushed (`git push origin --delete <branch>`). Don't leave merged branches lingering; each increment starts from a fresh branch off `main`.

**Never `git commit` or `git push` without explicit user approval — this is a hard rule, with one scoped exception (below).** Ask first every time, even for trivial or "obvious" changes, and even when the user previously approved a similar action; approval is per-action, never standing. When work is ready, stage it, show what would be committed/pushed, and wait for the go-ahead. When the user does approve, the **pre-commit gate** (Development workflow §5: `/code-review` → `/security-review` → full `-race` suite) is an additional hard precondition before committing.

**Exception — per-task commits during plan execution.** Once the user has approved a written plan (`docs/plans/*`) *and* chosen a task-by-task execution mode (`superpowers:subagent-driven-development` or `superpowers:executing-plans`), the per-task commits enumerated in that plan are **pre-authorized**: commit each completed, green task without pausing for per-commit approval. This standing authorization is narrowly scoped and does **not** relax anything else:
- It covers `git commit` **only** — `git push`, merges, tags, and any branch deletion still require explicit per-action approval.
- Each task must be a **green unit** — its `go test ./... -race` passes — before its commit; no WIP/broken-build commits.
- The **whole-branch delivery gate** (`/code-review` + `/security-review` over `main..HEAD`, findings resolved/triaged, `-race` green) still runs before the final increment commit, exactly as §5 requires.
- It applies only to commits the approved plan spells out. Any commit *not* in the plan (or made outside an active plan-execution workflow) falls back to the default: ask first.

**Proactively recommend alternatives.** Whenever a decision has to be made — design, library, API shape, trade-off — don't silently pick one. Surface the viable options with their pros/cons and state a recommended default, so the user can steer before you proceed.

## Documentation artifacts

Persist the workflow's written outputs under `docs/`, each **prefixed with an incrementing version number**:

- **Specs** (from `superpowers:brainstorming` / spec work) → `docs/specs/` — e.g. `docs/specs/001-<slug>.md`.
- **Plans** (from `superpowers:writing-plans`) → `docs/plans/` — e.g. `docs/plans/001-<slug>.md`. Pair a plan's number with its originating spec where practical.
- **Architecture Decision Records** → `docs/adrs/`, one file per decision, following **Michael Nygard's ADR convention** (Title, Status, Context, Decision, Consequences), numbered incrementally — e.g. `docs/adrs/0001-<slug>.md`. Record *every* architectural decision as it is made; supersede rather than rewrite old ADRs (set the old one's Status to `Superseded by ADR-NNNN`).

**Traceability is a hard requirement.** Every artifact must be cross-linked so any decision can be traced end to end — spec → plan → ADR(s) → code/commit — and back. Concretely:

- A **plan** must reference the **spec** it implements; a **spec** should list the plans that realize it.
- An **ADR** must cite the spec/plan that prompted it; the plan (and any relevant spec) must link back to the ADRs it depends on.
- **Code and commits** must reference the driving artifact (e.g. `Implements spec 003 / plan 003; see ADR-0007`) so reviewers can follow the chain.
- Do not merge or commit work whose governing spec/plan/ADR link is missing. A new artifact with no traceable parent (or a decision with no ADR) is incomplete.

## Session handover

To survive context limits without hallucinating, hand off through a written document rather than relying on a cluttered context.

**When to hand over:**
- **On request** — whenever the user asks to "hand over"/"handover"/"hand off".
- **Proactively at ~60% context usage** — stop starting new work, write the handover, and then **ask the user to continue in a fresh session**. Do not silently push past 60%.

**Hand over only from a safepoint — this is mandatory.** The next session must start well-grounded, so never write a handover mid-edit, with a broken build, or with failing tests. A safepoint means: the tree builds, the relevant tests are green, and you are at a clean task/step boundary. To reach one, either finish the in-flight step to green, or revert the incomplete edits back to the last green state — then capture that state in the handover. If you cannot reach a safepoint, say so explicitly and record the exact partial state (files touched, what's half-done, how to revert) so the next session can recover deterministically rather than guess.

**Where:** a single living file **`docs/HANDOVER.md`**, overwritten each time so it always reflects the latest state. It is a `docs:` artifact — offer to commit it (subject to the never-commit-without-approval rule); a committed handover survives a fresh clone.

**What it MUST contain** (enough for a brand-new session to resume with zero prior context — reference the governing artifacts, don't restate them, to avoid drift):
1. **Objective & roadmap position** — what we're building and which spec/plan/increment/**task+step** is active (e.g. "Plan 001, Task 4, Step 3").
2. **Exact state** — done (with commit SHAs), in progress, and the verbatim `git status --short` + last commit line; call out anything uncommitted in the working tree.
3. **Traceability pointers** — the active `docs/specs/*`, `docs/plans/*`, and `docs/adrs/*` files, plus CLAUDE.md, that the fresh session must read *first*.
4. **Decisions & deviations** this session, and any **pending approvals** or open questions blocking progress.
5. **Next actions** — the precise next steps and commands to run to resume.
6. **Gotchas/environment** — anything non-obvious needed to continue (tools, paths, credentials-by-reference).

Begin the document with an explicit instruction to the next session: *read CLAUDE.md and the referenced spec/plan/ADR before acting, and trust those files over any memory.*

## Commit discipline

Commit **completed, green units of work** — a coherent increment whose tests pass (and that has cleared the **pre-commit gate**, Development workflow §5). No WIP or broken-build commits.

Use **Conventional Commits**: `type(scope): summary`, where `type` names the activity —

- `feat` — new behavior/capability
- `fix` — bug fix
- `refactor` — behavior-preserving restructure (e.g. after `/simplify`)
- `spec` — a new/updated spec (from brainstorming), committed **standalone** since specs precede code
- also allowed: `test`, `docs`, `chore`, `perf`, `build`, `ci`

**Couple plans and ADRs with the code that realizes them — one coherent commit.** By default, do *not* make separate plan/ADR commits. The plan/ADR changes ride in the **same commit** as the `feat`/`fix`/`refactor` code that delivers what they planned, so a single commit carries **coherent information — the plan *and* the code that realizes it** — atomic and never out of sync. (Specs are the exception: they're authored before code, so a `spec` commit stands alone.)

**Amend, don't pile on, when the plan changes during implementation.** Plans and ADRs are routinely revised as the code teaches you something — when that happens, **amend** the coherent commit so the revised plan and its code stay in one unit, rather than adding a follow-up `fix:`/`docs:` commit that splits the artifact from its implementation. (This is the in-branch amend preference: keep logical-feature commits coherent; consolidate to them before landing.)

**Exception — commit a completed plan/ADR standalone for a cross-machine or fresh-session handoff.** When the user asks to commit the plan/ADR separately, *or* signals they will continue the implementation elsewhere — on **another machine**, a **fresh clone**, or a new session where the working tree may not survive — commit the finished, audited plan/ADR (with its companion ADR/spec/`docs/HANDOVER.md` edits) as a standalone **`docs:`** (or `spec:`) commit **ahead of** the code, so the design survives a tree wipe / clone. This is a deliberate, user-triggered deviation from the couple-with-code default, not the norm: it applies only to a *complete, gate-cleared* design at a clean safepoint, and the still-uncommitted **code** for each task then lands in its own `feat`/`fix` commit as usual (carrying the `Plan:`/`ADR:` trailers back to the already-committed artifact). The never-commit-without-approval rule still governs — confirm before committing.

**Recommended refinement — traceability trailers.** To make the hard traceability requirement machine-checkable rather than prose-only, put the links in Conventional-Commit **footer trailers** instead of freeform text:

```
feat(core): add the first evaluation path

Implements the initial path and updates the plan/ADR to reflect the
semantics settled during implementation.

Spec: 003
Plan: 003
ADR: 0007
```

This keeps the spec→plan→ADR→commit chain greppable (`git log --grep`), survives rebases, and lets CI enforce that every `feat`/`fix`/`refactor` commit carries at least a `Plan:` (and, for architectural changes, an `ADR:`) trailer. Prefer this over embedding references in the subject line.

## Go conventions & skills

**Go 1.25 — required.** This project targets **Go 1.25**: `go.mod` carries the `go 1.25` directive, and builds/tests run on a Go 1.25 toolchain. `GOTOOLCHAIN` needs a full patch version (bare `go1.25` is rejected — "a language version but not a toolchain version"), so when a newer Go is the local default (currently **1.26**), force 1.25 with `GOTOOLCHAIN=go1.25.12` (bump the patch as 1.25.x security releases land — bumped from `go1.25.0` in Plan 006 Task 6 after `govulncheck` flagged a stdlib CVE fixed by `go1.25.3`+). The module must not silently build on 1.26+. Do not use language/stdlib features newer than 1.25. CI pins 1.25.

**Tooling — mandatory.** Use **`gopls`** (the Go language server) for all Go code navigation, diagnostics, and refactoring — go-to-definition, find-references, rename, extract/inline, package API, post-edit diagnostics — via the native `LSP` tool (or gopls' MCP/CLI). Prefer semantic gopls operations over text search/`grep` when reasoning about Go symbols. → See `cc-skills-golang:golang-gopls`.

**Skills — always load, start from the orchestrator.** Start every Go coding, review, or debug task from **`cc-skills-golang:golang-how-to`** (the always-on orchestrator from `samber/cc-skills-golang`). It reads the task and pulls in the relevant `golang-*` skills (error handling, naming, design patterns, structs/interfaces, concurrency, context, testing, lint, security, …). Load the primary skill plus all applicable secondary skills together, up front. Consult those skills rather than working from memory.

Three **project-local skills override samber's testing guidance** where they conflict — prefer these:

- **`table-test`** — table-driven tests use the `assert` closure form (not `want`/`wantErr` fields), a `ctx` modifier for context-sensitive components, and `t.Context()` over `context.Background()`. Fold ≥2 cases exercising the same call into a table. Overrides `cc-skills-golang:golang-testing`.
- **`use-mockgen`** — generate test doubles with uber-go/mock (`mockgen`, `--typed`), placed alongside the interface in the producer package via `//go:generate`. Overrides mock-generation in `golang-testing` / `golang-stretchr-testify`.
- **`use-testcontainers`** — provision heavy external resources (Postgres, Redis, Kafka, MinIO, …) via testcontainers-go, never mocks/in-memory fakes; expose each through a single `RunTestX(t, opts...)` helper. Overrides integration-test scaffolding in `golang-testing`.

**Plans must activate the Go skills explicitly (writing-plans override).** `superpowers:writing-plans`'s header template mandates only the *execution* sub-skill (`subagent-driven-development`/`executing-plans`) — it **omits** this Go-skills hard rule. Because every `docs/plans/*` is Go-code work, the plan document itself MUST re-state, in its **header note + Global Constraints**, that each task starts from **`cc-skills-golang:golang-how-to`** (+ TDD via `superpowers:test-driven-development`, `gopls` for navigation/refactor, and the `table-test`/`use-mockgen`/`use-testcontainers` overrides). Encode it in the plan; do **not** rely on relaying it only through SDD dispatch prompts.

**Testing rules (mandatory — this project).** In addition to the skills above:

- **Blackbox tests only.** Every `_test.go` uses the external `package <pkg>_test` form and exercises only the exported API. A test that seems to need an unexported helper is rewritten to drive that behavior through the public surface — never fall back to whitebox `package <pkg>`. Example tests are `_test`-package too; keep them so.
- **Export what a test must assert.** When a test needs to `errors.Is` a sentinel, export it (`ErrX`) as part of the public error contract rather than whitebox-testing an unexported `errX`; those sentinels are the same debuggability surface callers rely on.
- **Assert-closure tables always** (reinforces `table-test`): every table case carries an `assert func(t, …)` closure, never `want`/`wantErr` fields.
- **Injectable time via `jonboulle/clockwork` directly — do not create a first-party clock abstraction.** Accept a `clockwork.Clock` (defaulting to `clockwork.NewRealClock()`), inject it via a `WithClock`-style option, and drive time in tests with `clockwork.NewFakeClock()`. Do not wrap it in a project-local `Clock` interface. This makes `clockwork` a direct dependency of the core — an accepted, deliberate exception to the stdlib-only rule (see Dependency policy).

## What this is — and is not

- **Is:** an importable Go **library** providing Enterprise Integration Pattern building blocks — messages, channels, endpoints, and **channel adapters** to external systems — that an application composes into a message flow. Consumed via `go get` + import; the exported API is the product.
- **Is:** deliberately **scoped**. The pattern core plus **six shipped adapters** — **in-memory**, **SQL** (`adapter/database/sql`, generic `database/sql`; v1 dialects PostgreSQL + MySQL), **pgx** (`adapter/database/pgx`, PostgreSQL-native incl. `LISTEN`/`NOTIFY`), **Redis**, **NATS**, and **HTTP** (`adapter/http`, sync request-reply / async / outbound webhook). Not a port of all of Spring Integration. See [`docs/specs/001-messaging-core.md`](docs/specs/001-messaging-core.md).
- **Is:** **open for extension.** The adapter contract is a first-class, public **SPI** (service-provider interface) so that new adapters — Kafka, RabbitMQ, NATS, SQS, … — can be added later by us or contributors **without changing the core**. New adapters live in their own subpackage and implement the same inbound/outbound interfaces; the core neither knows nor imports them.
- **Is:** **production-grade.** Robustness is a requirement, not an afterthought — see "Production robustness (mandatory design constraints)" below.
- **Is not:** a full ESB/BRMS, a workflow engine, or a message broker. It orchestrates flow *between* systems and brokers; it is not itself the broker. No UI, no config-authoring surface, no governance layer.
- **No binary deliverable (default).** There is **no `main` package, no `cmd/`, no CLI or server** to ship. Any `main` is throwaway (examples/manual repro only) and must not become a deliverable. Library code must not call `os.Exit`, `log.Fatal`, or `panic` on caller input (return errors instead), and must not log to a global logger by default — accept an injected logger/handler (`*slog.Logger`) if logging is needed. If msgin is later scoped to ship a binary, revise this section via an ADR.

## Architecture blueprint (target design)

The vocabulary is EIP's; the shape mirrors Spring Integration but is pure, idiomatic Go. These are the concepts a new instance must understand before writing code — they span multiple packages and drive every API decision.

**Core abstractions (the dependency-free heart of the library):**

- **Message** — an immutable payload + headers envelope. `Message[T]` carries a typed payload plus a `Headers` map (string → any) for metadata (id, timestamp, correlation id, reply channel, custom keys). Immutability matters: enrichers/transformers return a *new* message rather than mutating in place, so a message shared across a pub-sub channel is safe to read concurrently.
- **Channel** — the conduit between endpoints. Two families: **point-to-point** (each message consumed by exactly one handler) and **publish-subscribe** (every subscriber sees it). Channels may be **buffered** (queue/async handoff) or **direct** (synchronous, handler runs on the sender's goroutine). Backpressure and capacity are channel concerns, expressed with Go channels/contexts, not busy-loops.
- **Message endpoint / handler** — where application code meets the messaging system: `MessageHandler` (consumes) and the composeable EIP endpoints — **Transformer**, **Filter**, **Router**, **Splitter**, **Aggregator**, **Service Activator**, and **Gateway** (the request-reply bridge into/out of a flow). Keep these small and interface-driven so they chain.
- **Channel adapter (the extension point)** — the bridge to an external system, and the reason this library exists:
  - **Inbound channel adapter** — pulls/receives from an external system and emits messages onto a channel. Most external sources are polled, so there is a shared **Poller** (interval, fixed-delay, max-messages-per-poll, backoff) that drives a `PollingSource`; event-driven sources (e.g. Redis pub/sub, a blocking `BRPOP`) implement a streaming/listening variant instead.
  - **Outbound channel adapter** — subscribes to a channel and writes messages out to the external system.
  - Both sides are defined by **narrow public interfaces (the adapter SPI)** in the core. A shipped adapter (memory/database/redis) is just an implementation; a future Kafka/RabbitMQ adapter is another implementation of the *same* interfaces. The core must never `import` an adapter package — dependency points inward only.

**The six shipped adapters** (full detail in [`docs/specs/001-messaging-core.md`](docs/specs/001-messaging-core.md) §9):

- **In-memory** (`adapter/memory`, core module) — reference adapter and fast test double; Go channels, zero-copy, no codec. The model contributors copy for new adapters.
- **SQL** (`adapter/database/sql`, core module) — generic `database/sql`; polling `SELECT … FOR UPDATE SKIP LOCKED` inbound + `INSERT` outbound; at-least-once. v1 ships **PostgreSQL + MySQL** dialects via a `Dialect` seam; driver injected by the caller.
- **pgx** (`adapter/database/pgx`, own module) — PostgreSQL-native (`jackc/pgx/v5`) + wire-compatible derivatives; adds **`LISTEN`/`NOTIFY` event-driven** (`StreamingSource`) alongside polling, `pgxpool`, `COPY` bulk outbound.
- **Redis** (`adapter/redis`, own module) — list (`LPUSH`/`BRPOP`, at-most-once) or streams (`XADD`/`XREADGROUP`/`XACK`, consumer groups, at-least-once, native redelivery).
- **NATS** (`adapter/nats`, own module) — core subject pub/sub (at-most-once) or JetStream (pull *and* push, at-least-once, `Ack`/`Nak`/`Term`).
- **HTTP** (`adapter/http`, core module) — `net/http`; inbound server (sync request-reply → `Ack`=2xx/`Nack`=5xx, or async 202) + outbound webhook `POST` with cenkalti/backoff retry.

Payload (de)serialization (`T`↔`[]byte`) lives in the typed **runtime** (a `PayloadCodec[T]`, which knows `T`); adapters do only type-agnostic **envelope framing** (headers+body↔storage) — see [ADR 0001](docs/adrs/0001-message-payload-typing.md). Heavy-client adapters (`pgx`, `redis`, `nats`) are separate modules importing their real client directly (ADR 0003).

### Key design invariants

- **Dependency points inward.** core ← adapters, never the reverse. The core defines the SPI; adapters implement it. This is what makes the library open for extension without modification (Open/Closed).
- **Adapters are isolated subpackages/modules.** Each adapter lives under its own package (e.g. `adapter/memory`, `adapter/database/sql`, `adapter/database/pgx`, `adapter/redis`, `adapter/nats`); heavy-client adapters are separate Go modules so a consumer imports — and pays the transitive-dependency cost of — only the adapters they use.
- **Delivery guarantees are explicit contracts,** documented per adapter (at-most-once vs at-least-once, ordering, ack/nack, redelivery). Never leave them implied — they are the difference between "works in a demo" and "safe in production."
- **Everything cancellable.** Every blocking operation (poll, receive, send, handler) takes a `context.Context`; graceful shutdown drains in-flight work and stops pollers cleanly.

### Design decisions — RESOLVED (see spec + ADRs)

These were the open decisions; all are now ratified in [`docs/specs/001-messaging-core.md`](docs/specs/001-messaging-core.md) and `docs/adrs/`:

1. **Module layout → multi-module monorepo** ([ADR 0003](docs/adrs/0003-multi-module-repository-layout.md)). Core (stdlib + clockwork, incl. `memory` + `database/sql`); `database/pgx`, `redis`, `nats` are separate modules. **Supersedes the earlier single-module lean** — release tags are now module-path-prefixed; local dev uses `go.work`.
2. **Message payload typing → generics + split codec** ([ADR 0001](docs/adrs/0001-message-payload-typing.md)). `Message[T]` on the caller API; non-generic SPI over `Message[any]`; payload codec (`T`↔`[]byte`) in the runtime, envelope framing in the adapter.
3. **Adapter SPI → non-generic, dual inbound, runtime-owned reliability** ([ADR 0002](docs/adrs/0002-adapter-spi.md)). `PollingSource` (shared Poller) + `StreamingSource`; `Delivery` with `Ack`/`Nack` closures; `NativeReliability` escape hatch.
4. **Error handling → runtime-owned** ([ADR 0002](docs/adrs/0002-adapter-spi.md)). One configurable `RetryPolicy` (max-attempts → dead-letter, backoff); adapters expose only raw ack/nack.
5. **Concurrency → point-to-point + worker pool** ([ADR 0002](docs/adrs/0002-adapter-spi.md)). Default 1 (ordered), `N>1` = Competing Consumers; consumer groups via adapters; pub-sub deferred.
6. **`clockwork` core dependency → ratified** ([ADR 0004](docs/adrs/0004-clockwork-dependency.md)).

## Production robustness (mandatory design constraints)

"Robust enough for production" is a stated requirement — treat these as gates on the design, not nice-to-haves:

- **No goroutine leaks.** Every goroutine a channel/adapter/poller starts is owned, cancellable via context, and joined on shutdown. Tests use `go.uber.org/goleak` (via the testing skills) to prove it.
- **Graceful shutdown.** A flow can be stopped so that pollers halt, in-flight messages finish or are safely nacked, and buffers drain within a deadline — no lost or double-processed messages beyond the adapter's documented guarantee.
- **Backpressure & flow control — first-phase, mandatory** (spec §7.4, [ADR 0006](docs/adrs/0006-resilience-flow-control.md)). Credit-based `WithMaxInFlight` gates the poller so a message flood (e.g. a huge `sql` backlog) can't hammer the system — pull adapters self-limit to handler capacity and the surplus waits durably in the source. Plus optional rate limit, handler timeout, circuit breaker, and overflow policy — all clockwork-driven interfaces with dependency-free defaults (`x/time/rate`/`sony/gobreaker` are optional plug-ins, not forced deps).
- **Concurrency-safe by construction.** The public types used across goroutines (channels, message headers, registries) are safe; `go test ./... -race` is clean. Prefer immutability (messages) over locks where possible.
- **Fault isolation & recovery.** A panicking handler must not take down the flow; recover at endpoint boundaries and route to the error channel. External I/O (DB, Redis) is wrapped with timeouts, and transient failures surface through the retry/dead-letter policy rather than crashing.
- **Observability hooks, not global state.** Accept an injected `*slog.Logger` and optional metrics/trace hooks; never log to a package global or panic on caller input.

## Dependency policy

Minimal dependencies is a **hard requirement** for this library — every direct dep is a transitive dep forced on every consumer.

- **The core (root package) depends on the Go standard library only — with two accepted exceptions: `github.com/jonboulle/clockwork`** (injectable time, used directly — see Testing rules; [ADR 0004](docs/adrs/0004-clockwork-dependency.md)) **and `github.com/cenkalti/backoff/v4`** (retry backoff strategy; [ADR 0005](docs/adrs/0005-cenkalti-backoff-dependency.md)). No *other* third-party imports in the pattern core or the in-memory/`http`/`database/sql` adapters. `database/sql` is stdlib; the SQL *driver* is the caller's choice, injected — msgin does not import a driver.
- **Adapters that need a heavy client declare a narrow interface and let the consumer inject the implementation** (the Redis case above). Prefer "accept an interface" over "import a client." This keeps the dep out of the module graph, and makes the adapter trivially testable (the interface is mockable via `use-mockgen`).
- **Justify every dependency in an ADR.** Adding a direct dependency to the root module is an architectural decision. Test-only dependencies (`testify`, `goleak`, `testcontainers-go`, a real Redis/SQL client used *in tests*) are fine and do not burden consumers — they live under `require` but are not imported by non-test code.

## Core quality criterion: debuggability

Treat "can I debug this with a normal Go debugger and readable errors?" as a first-class design constraint on every decision. Keep the library **pure Go** (no cgo) so a developer can set a breakpoint, step through, and read a plain Go stack trace. Prefer typed, wrapping errors that name the offending field/input over opaque failures — the typed-error surface is what callers rely on to diagnose problems.

## Sensible defaults (opinionated, but overridable)

Every configurable knob ships an **opinionated, production-safe default** — the library should *just work* for the common case without the caller wiring every option — **while staying fully customizable** for consumer needs via functional options (`WithX`). This is a design gate, not a nicety:

- **Default to the safe, conservative value**, not the permissive one. When a wrong default could silently corrupt (duplicate messages, lose data, unbounded growth, a DoS lever), pick the value that fails safe, and size it with generous margin (e.g. a lease/visibility timeout defaults *comfortably above* any plausible handler round-trip, not to a tight value that footguns a slow handler). If **no** value can be safe for an unknown caller (e.g. a byte cap that depends on the caller's legitimate payload size), make it **explicit/opt-in with a clear typed error or documented off state** rather than guessing a default that lulls the caller into a false guarantee — and say so in the godoc.
- **Every default is overridable** through a `WithX` option; never hard-code a policy the caller cannot change. Prefer the established option pattern (a `set`-flag distinguishing "unset → default" from an explicit invalid value → typed error, per `WithMaxInFlight`/`WithAttemptTTL`).
- **Document the default and its rationale** on the option's godoc (the value, *why* it's safe, and the invariant that makes a custom value safe), so a caller changing it understands the trade-off. Defaults are part of the API contract — changing one later is a behavioral change subject to the SemVer gate.

## License & release

- **License:** Apache-2.0. Add the verbatim text to `LICENSE`. New source files may carry the standard short Apache header; keep third-party attributions in a `NOTICE` file if any are added.
- **Releases are tag-driven.** Push an annotated **SemVer** tag `vX.Y.Z` (e.g. `v0.0.1`) and a `release` GitHub Action publishes a **GitHub Release** with auto-generated notes. Consumers get the version via `go get module@vX.Y.Z`; **the tag itself is the distribution** — nothing is compiled or uploaded (this is a library). Tags with a pre-release suffix (`v0.0.1-rc.1`) are marked as pre-releases.
- Tag versioning must obey the SemVer/API-compatibility gate below (breaking exported API ⇒ major bump ⇒ ADR).

## Library quality gates

Because the deliverable is a package other code imports, the exported surface *is* the contract. In addition to the **pre-commit gate** (Development workflow §5), the following must hold before any release-bound change is considered done:

- **Everything builds & tests green, race-clean:** `go build ./...` and `go test ./... -race` pass; `go vet ./...` and `golangci-lint run ./...` are clean; `gofmt`/`gofumpt` report nothing.
- **Module hygiene:** `go mod tidy` leaves `go.mod`/`go.sum` unchanged and `go mod verify` passes. Keep the **dependency set minimal** — every direct dep becomes a transitive dep for every consumer; justify additions.
- **Public API is documented & deliberate:** every exported symbol has a godoc comment; keep internals in `internal/` so they can't be imported. Prefer a small, stable surface — "accept interfaces, return structs."
- **API compatibility (SemVer):** no breaking change to an exported symbol without a major-version bump. Check with `gorelease` / `go run golang.org/x/exp/cmd/apidiff` against the last tag; **deprecate** (doc comment + keep working) before removing. Any intended break is an architectural decision → record an ADR.
- **Pure Go, no cgo:** `CGO_ENABLED=0 go build ./...` must succeed — keeps the library cross-compilable and debuggable (see debuggability criterion above).
- **Runnable examples & coverage:** exported behavior is covered by `Example…` tests (they double as godoc) and table tests. The **Test-coverage gate** (Development workflow §5) applies: target ≥ 85% on changed packages and — the hard requirement — every hot-path logic branch and every typed-error branch has a covering test. Watch coverage on the public packages; don't just chase a number.
- **Vulnerability scan:** `govulncheck ./...` is clean.
- **Pinned Go version:** builds/tests on **Go 1.25** (the `go 1.25` directive), not whatever newer toolchain is installed locally. CI runs 1.25; verify locally with `GOTOOLCHAIN=go1.25.12`.

## Commands

Standard Go tooling once the module is scaffolded (`go mod init github.com/kartaladev/msgin`):

```bash
# One-time scaffold with the Go 1.25 pin (local default is newer, so force 1.25)
go mod init github.com/kartaladev/msgin
go mod edit -go=1.25                        # pin language version to 1.25
export GOTOOLCHAIN=go1.25.12                # build/test on the 1.25 toolchain; CI sets this too
go install golang.org/x/tools/gopls@latest  # LSP server: code nav / diagnostics / refactor

go build ./...
go test ./...
go test ./... -race                       # run with the race detector
go test -run TestName ./path/to/pkg       # single test
go test -run TestName/subtest ./path      # single subtest
go test -run '^Example' ./...             # runnable example tests
go vet ./...
gofmt -l .                                # or: gofumpt -l .
golangci-lint run ./...                   # once .golangci.yml is added (see cc-skills-golang:golang-lint)
govulncheck ./...                         # vulnerability scan (library quality gate)

# Release (library — the tag is the distribution; the workflow publishes a GitHub Release)
git tag -a v0.0.1 -m "v0.0.1" && git push origin v0.0.1
```

Prefer runnable **Example tests** (`func Example...() { ... // Output: ... }`) for documenting behavior — they double as compilable docs.
