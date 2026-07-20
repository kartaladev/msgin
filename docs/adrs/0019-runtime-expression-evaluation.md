# ADR 0019 — Accept `expr-lang/expr` as a core dependency; runtime-expression endpoints

- **Status:** Proposed (2026-07-20) — **both adversarial audit rounds complete; bundle SOUND.** Round 1 (Opus,
  verified via a real expr v1.17.8 compile+run spike: all 6 API assumptions TRUE, no blockers) folded H1/M1/M2/L1-L3;
  round 2 verified every fix and folded NEW-1 (a RouterExpr compile-error coverage hole the L2 guard-ordering
  created) + LOW tidies. Resolved: O8-1→`header()` form, O8-2→`RouterExprCases` cut. Gated only on an explicit user
  go-ahead before implementation (SDD). **Decision: accept `github.com/expr-lang/expr` as a core dependency and add
  runtime-expression endpoints (`FilterExpr`/`RouterExpr`) directly in the core `msgin` package.** This ADR
  **replaces the earlier draft** of ADR 0019 (which recommended deferring `expr` to an optional module) — the user
  explicitly relaxed the core dependency policy and chose to build now, in core.
- **Spec:** [Spec 008 — Runtime-expression endpoints](../specs/008-expr-endpoints.md).
- **Plan:** [Plan 014 — expr endpoints](../plans/014-expr-endpoints.md) (planned).
- **Depends on / relates to:** [ADR 0016 — robfig/cron core dependency](0016-robfig-cron-dependency.md) (the
  zero-transitive-dep acceptance precedent this mirrors), [ADR 0013 — composition endpoints](0013-composition-endpoints.md)
  (the `Filter`/`Router` endpoints wrapped here), [ADR 0003](0003-multi-module-repository-layout.md),
  [ADR 0004](0004-clockwork-dependency.md)/[0005](0005-cenkalti-backoff-dependency.md) (the existing dependency
  exceptions), and the Dependency policy + debuggability criterion in `CLAUDE.md`.

## Context

msgin's `Filter`/`Router` accept **Go function values** — strictly better than any expression engine for logic
known at compile time (more powerful, statically typed, zero-dependency, debuggable). The one thing a Go func
cannot do is a **runtime-defined** rule: a predicate/route supplied as a *string* from config, a database, an admin
UI, or per-tenant data — Spring Integration's SpEL role.

Two constraints governed whether to add `github.com/expr-lang/expr` for this:

- **Dependency policy (was: core = stdlib + `clockwork` + `cenkalti/backoff` only).** A third-party dep in the core
  is forced on every consumer.
- **Debuggability (a first-class criterion).** An `expr` string is opaque to a Go debugger.

**What changed:** the user **explicitly relaxed the dependency policy** for this case and chose to build now, with
the integration **in the core `msgin` package** (not a submodule). Two facts make that defensible: (1) `expr` is
**zero-transitive-dependency, pure-Go, MIT** — verified: its `go.mod` has no `require` block (`go 1.18`) — so the
cost is one direct dep and *zero* transitive deps, the same profile ADR 0016 accepted for `robfig/cron`; (2) the
debuggability downgrade is **opt-in per use** — it affects only code that calls an `expr` constructor, never the
Go-func path.

## Decision

### 1. Accept `expr` as a core dependency

`github.com/expr-lang/expr` becomes the **4th blessed direct dependency** of the repo and the **3rd third-party
import of the pattern-core package** (after `clockwork` and `cenkalti/backoff`). The version is pinned in the root
`go.mod`; the whole-branch gate asserts `go mod tidy`/`go.sum` show **zero transitive dependencies** (the
acceptance gate, mirroring ADR 0016). `CLAUDE.md`'s Dependency-policy section is updated to list `expr` as an
accepted core dependency with this justification.

### 2. Runtime-expression endpoints in the core `msgin` package (`expr.go`)

Thin constructors that return the **existing** `Step`/`*Router` types — the `Filter`/`Router` runtime is untouched
(no change to `handler.go`/`consumer.go`/`filter.go`/`router.go`):

- **`FilterExpr[A any](expression string, opts ...FilterOption) (Step, error)`** — compiles `expression` once with
  `expr.AsBool()` (bool output enforced at compile time); returns the same `Step` `Filter` returns, or
  `ErrInvalidExpression` (wrapping expr's compile/type-check error, naming the offending expression) at
  **construction**. Per message: `PayloadOf[A]` → evaluate against the environment → pass to `next` / send to
  `WithDiscardChannel` / silent drop, identical to `Filter`.
- **`RouterExpr[A any](keyExpr string, routes map[string]MessageChannel, opts ...RouterOption) (*Router, error)`** —
  `keyExpr` compiled with `expr.AsKind(reflect.String)`; per message the resolved key selects `routes[key]`,
  falling back to `WithDefaultChannel`/`ErrNoRoute` (unchanged `Router` semantics). Built on the existing
  `NewRouter`. A nil/empty `routes` map or a nil channel value in it is `ErrInvalidExpression` at construction. A
  ternary key (`payload.Amount >= 100 ? "big" : "small"`) covers multi-way routing in one construct.
- A separate predicate-case router (`RouterExprCases`) was considered and **cut** (audit O8-2) as redundant with
  the ternary key; it can be added later, non-breaking, if a "many independent conditions" need appears.

### 3. One internal, reusable compile primitive

An unexported `compile[A](expression string, kind exprOutputKind) (func(context.Context, Message[A]) (any, error),
error)` sets the env types for `A` + headers, applies the output-type option, compiles to a program, and returns a
per-message evaluator + any compile error. Shared by §2; reused by future `Splitter`/`Aggregator` endpoints. Kept
**internal** now (minimal public surface, Spec N2/O8-5).

### 4. Expression environment

An expression references `payload` (the typed `A`; struct-field access type-checked by expr against `A` **when `A`
is concrete** — with `A = any` there is no compile-time check and errors surface at eval) and headers via a
`header("key")` env **function** returning the raw value or nil. The function form (resolved, Spec D5/O8-1) is
chosen over a `headers["key"]` map view because `msgin.Headers` deliberately never exposes its internal map (the
immutability invariant): the map form would hand expr the internal map or clone it per message, whereas the
function form is zero-allocation and invariant-preserving. Env keys are lowercase (`payload`, `header`) for
SpEL-like ergonomics. `header()` is untyped, so a missing/wrong-typed header in a comparison is a runtime eval
error.

### 5. Errors and evaluation

Construction errors (`ErrInvalidExpression`) are surfaced **once**, up front, for a nil/empty/invalid/mistyped
expression. **Runtime eval errors** (a compiled expression that errors at eval) propagate as the endpoint's
returned `error` — exactly like a Go predicate's error today — into the runtime's retry/DLQ/invalid path.

### 6. Documented trade-offs (godoc on every constructor)

- **Type-safety downgrade:** a string typo is a construction/startup error, not a build error (mitigated by expr's
  type-checking + output enforcement).
- **Debuggability downgrade:** no Go stack trace inside an expression; the Go-func `Filter`/`Router` remain the
  recommended path for compile-time rules.
- **Security posture (audit-corrected):** expr's **defaults enforce concrete limits** (verified v1.17.8) — a
  compile-time AST cap (`MaxNodes = 1e4`) and a runtime `MemoryBudget` (`1e6`) that reject the usual abuse vectors
  (`repeat`, huge ranges, deep nesting). It is non-Turing-complete, does no I/O, and calls no arbitrary Go unless
  injected → suitable for **operator/config-authored** expressions. **Residual gap (documented):** there is **no
  time budget** and `vm.Run` **takes no context**, so an evaluation is **not cancellable** by the flow's context —
  keep operator expressions bounded, and add your own limits before evaluating arbitrary untrusted input. A
  custom-functions/limits escape-hatch (`WithExprEnv`) is deferred (Spec O8-3).

## Consequences

**Positive.**
- Runtime-defined filtering/routing (the SpEL gap) is available with a tiny, zero-transitive-dep engine, and **no
  change to the core runtime** — the constructors return the existing `Step`/`*Router`.
- The internal compile primitive gives every future EIP endpoint (Splitter/Aggregator/Transformer) a ready,
  consistent expr path without re-implementation.
- Consistent with the ADR 0016 precedent (a zero-transitive-dep core dependency justified in an ADR + guarded by a
  `go.sum`-clean acceptance gate).

**Negative / trade-offs.**
- **Every consumer now carries `expr` in their module graph** — even those who only ever use Go funcs. Accepted
  deliberately by the user; the cost is one zero-transitive-dep module. A separate module (rejected below) would
  have avoided this.
- **Debuggability + type-safety downgrades** for code that uses the expr constructors (per-use, documented).
- **A core dependency is hard to remove later** (SemVer-breaking). Accepted; recorded here.
- **(Resolved) Router surface** — `RouterExprCases` was **cut** (audit O8-2); only `RouterExpr` ships (a ternary
  key covers multi-way routing). One fewer exported type.

**Neutral.**
- The added API is additive → minor SemVer bump.
- Other endpoints' expr support (Transformer/Splitter/Aggregator/…) and the exported primitive are deferred,
  non-breaking future work reusing §3.

## Alternatives considered

- **`expr` in an optional separate module** (the earlier ADR 0019 draft's recommendation, and the
  `pgx`/`redis`/`nats` pattern). It keeps `expr` out of non-users' module graphs and the core package pristine —
  strictly cleaner on the dependency axis, at the cost of a second import path and less discoverability.
  **Rejected by the user** in favor of building it directly in the core package; recorded here as the cleaner
  alternative on dependencies, should the decision ever be revisited.
- **Defer entirely (record-only).** Rejected: the user chose to build now.
- **A first-party mini expression language.** Rejected: re-implements a solved problem worse; `expr` is the mature,
  safe, well-typed, zero-dep choice.
- **`expr` in the pattern-core files (not a dedicated `expr.go`).** Rejected: isolating the integration in one file
  limits the blast radius on the rest of the core package (Spec D2).

---

## Addendum (2026-07-21) — expr on the transformation-group endpoints (Spec 009 Phase 4)

- **Status:** Proposed (2026-07-21) — Phase-4 design settled with the user; pending the adversarial audit + Plan 018
  + implementation go-ahead. Realizes the "future `Splitter`/`Aggregator`/`Transformer` expr support reusing §3" that
  the base ADR named as deferred, non-breaking work (Consequences → Neutral). **Adds no new dependency** (`expr`
  already in-core); **no exported signature change** to any shipped symbol → **minor SemVer**.
- **Spec:** [Spec 009 §3.5 (D12–D14)](../specs/009-splitter-aggregator-endpoints.md). **Plan:** Plan 018.
- **Builds on this ADR's** §3 `compile[A]` primitive and §4 `exprEnv[A]` (`{payload, header(k)}`) and reuses the base
  ADR's error model (§5) and documented trade-offs / security posture (§6) verbatim — a per-use debuggability/
  type-safety downgrade, expr's default node/memory caps, **no** time budget / ctx-cancellation.

### A1. New surfaces (all in the core `msgin` package)

- **`TransformExpr[A, B any](expression string) (Step, error)`** — a Message Translator whose projection is an expr
  over `exprEnv[A]`, result **asserted to B**, wrapped `WithPayload(parent, b)` (id/correlation propagate). Returns
  the same `Step` `Transform` returns. Option-free (matches `Transform`).
- **`SplitExpr[A, B any](expression string) (Step, error)`** — expr over `exprEnv[A]` → a **slice**; each element is
  asserted to B, wrapped `WithPayload(parent, b)`, and forwarded through the **same sequence-stamping loop `Split`
  uses** (extracted into a shared unexported helper). Returns the same `Step` `Split` returns. Option-free.
  (Resolves Spec O9-4: dual-param, per-element assert to B.)
- **`WithCorrelationExpr[A any](expression string) AggregatorOption`** — reuses the existing `exprString` compile;
  produces the `func(Message[any]) (string, error)` correlation strategy (asserts `Message[any]`→A, evaluates to the
  key). An eval error propagates through the strategy's existing error return.
- **`WithReleaseExpr[A any](expression string) AggregatorOption`** — compiles a **bool** over the new group-scoped
  env (A2), producing the release strategy.
- **Aggregate-by-expr: DEFERRED** (Spec 009 §2 non-goals / §3.5 D14). expr cannot construct an arbitrary Go struct,
  and the aggregate fn is a required `NewAggregator` constructor arg (not an option), so an expr aggregate would need
  a separate `NewAggregatorExpr[A,B]` constructor limited to scalar/slice/map results — low value over a Go aggregate
  fn. Non-breaking to add later.

### A2. Two `compile` extensions (both internal, `expr.go`)

1. **`exprAny` output kind** — a third `exprOutputKind` that applies neither `AsBool` nor `AsKind`: the program is
   compiled with only `expr.Env(exprEnv[A]{})`, the raw result returns as `any`, and the caller asserts it to B at
   eval. A non-B result is a **typed evaluation error naming the mismatch** → the runtime's retry/DLQ/invalid path
   (base ADR §5). Used by `TransformExpr` + `SplitExpr`.
2. **A group-scoped compile path over a per-member view env** — the one genuinely new env shape, for
   `WithReleaseExpr`:
   ```go
   type exprMember[A any] struct {
       Payload A                `expr:"payload"`
       Header  func(string) any `expr:"header"`
   }
   type groupExprEnv[A any] struct {
       Messages []exprMember[A] `expr:"messages"` // members in arrival order
       Size     int             `expr:"size"`     // group's HeaderSequenceSize (0 if absent)
   }
   ```
   **Why a view, not raw `Message[A]`:** `Message[A]`'s header accessors are expr-hostile (`Header` /
   `Headers().Get` return `(any, bool)`, which expr's method-call support rejects) and its `payload`/`headers` fields
   are unexported. Projecting each member into the same `{payload, header(k)}` shape `exprEnv[A]` already uses keeps
   the env expr-friendly and consistent: `size > 0 && len(messages) >= size`, `all(messages, .payload.Amount > 0)`,
   `messages[0].header("priority") == "high"`. A fixed, documented shape — **not** a general extension point
   (`WithExprEnv` stays deferred, Spec 008 O8-3). **`size` is 0 when `HeaderSequenceSize` is absent** (M-4), so the
   canonical release form is size-**gated** (`size > 0 && …`); the un-gated `len(messages) >= size` would release a
   group-of-one, opposite to the presence-gating `defaultRelease`. `toGroupEnv` guards an empty group before reading
   the size header (M-1). Verified against expr v1.17.8 by a compile+run spike (audit R1).

### A3. Two behavioral seams this addendum settles

- **Expr *options* report compile errors through the constructor.** An `AggregatorOption` cannot return an error, so
  `WithCorrelationExpr`/`WithReleaseExpr` compile eagerly and stash the first compile error in `aggregatorConfig`;
  `NewAggregator` surfaces it as `ErrInvalidExpression` at construction — the established validate-in-constructor
  pattern (`WithMaxInFlight`/`WithAttemptTTL`). First error wins (a later expr option does not clobber it).
- **The Aggregator's *internal* release strategy evolves to `func(MessageGroup) (bool, error)`; the public
  `WithReleaseStrategy(func(MessageGroup) bool)` is UNCHANGED.** A release expr can error at eval, and per the
  debuggability criterion + the `FilterExpr` precedent (its predicate is `(bool, error)`) that error must propagate
  to `Handle`/reaper → retry/DLQ, not be swallowed as "not released". `WithReleaseStrategy` keeps its `bool`
  signature, wrapped to always-nil-error; `defaultRelease`, `WithCompletionSize`, `Handle`'s release check, and
  `reapGroup`'s release check move to the `(bool, error)` internal shape. **This is the only Phase-4 change to
  shipped Phase-2/3 code** — the whole-branch gate regression-covers the public release paths. Rejected alternative
  (swallow eval-error → held-until-expiry): purely additive but silently swallows the error — against the
  debuggability gate and inconsistent with `FilterExpr`.

  **Per-call-site error policy (audit R1 — the release-`(bool,error)` blast radius):**
  - **`Handle`** — a release-decision error **propagates** (Nack → retry/DLQ): an immediate, visible signal that a
    `WithReleaseExpr` is faulty, consistent with `FilterExpr`. A persistently-unevaluable group is never stranded —
    see the reaper below.
  - **`reapGroup`** (H-1) — on a release-check **error**, control **falls through to the age-expiry branch** (it is
    NOT an early return): a deterministically-erroring group aged past `WithGroupTimeout` is routed to
    `WithExpiredGroupChannel` instead of busy-spinning claim→error→abandon forever. This upholds the spec's
    never-silently-held / never-silent-drop guarantee for the new error surface.
  - **`release` drain loop** (H-2 + H-3) — once the top-level (main) `releaseOnce` **succeeds**, the entire drain
    is best-effort and **every** post-settle exit returns nil: a residual release-check error/incomplete
    (`AbandonGroup` + `return nil`), a residual `releaseOnce` failure (`return nil`; its claim is already abandoned by
    the defer), **and** a transient `ClaimGroup` error (`return nil`). By that point `releaseOnce` has already
    settled the main claimed set — which includes the member `Handle` is processing — so ANY non-nil return would
    Nack it → redeliver → idempotent-`Add` re-insert the just-deleted row → re-mix into a **second** aggregate (a
    real double-count) while DLQ-ing a successfully-processed message. Only the **main** `releaseOnce` failure
    propagates (correct — the main group is then NOT settled, so redeliver re-groups cleanly, no double-count).
    Residuals the drain leaves are recovered by the reaper (durable store) or a later `Add`. NB: `reapGroup`'s
    recovery call discards `release`'s return, so this best-effort contract matters only on the `Handle` path.
  - **Poison-member caveat (M-5):** unlike the O(1) size-header `defaultRelease` (which reads only the first
    member's size header), a release expr evaluates over **all** members, so one member's bad data poisons the
    group's release check for every subsequent member on that key. The escape is the reaper's expiry fall-through
    above; the guidance is "write total expressions". `WithReleaseExpr` is **O(n) per `Add`** (O(n²) per group) vs
    the default's O(1) — recommend the Go-func `WithReleaseStrategy` for large groups.
  - `WithCorrelationExpr` maps an **empty** evaluated key to `Permanent(ErrNoCorrelation)` (symmetry with
    `defaultCorrelate`, L-1); a correlation/release **eval** error propagates raw/retried (FilterExpr stance, L-2).
    `NewAggregator` surfaces an expr-option compile error (`ErrInvalidExpression`) **before** `ErrNilOutput`/
    `ErrExpiryChannelRequired` (L-5, deliberate).

### A4. Consequences (delta)

- **Positive:** the SpEL transformation-group surface (project / split / correlate / release by config string) is
  available reusing the base ADR's primitive with zero new dependency and no public signature change; every new
  surface returns an existing type (`Step`/`AggregatorOption`).
- **Trade-offs:** the base ADR's per-use debuggability/type-safety downgrade and no-ctx-cancellation caveat apply to
  each new expr constructor (documented on every godoc). The internal release-strategy signature change touches
  shipped aggregator code (A3) — contained to one file, regression-gated. The **struct-B limit** (expr cannot build
  an arbitrary struct) is a documented ceiling on `TransformExpr`/`SplitExpr` B — use the Go-func `Transform`/`Split`
  for struct projection.
- **Neutral:** additive API → minor SemVer. Aggregate-by-expr and `WithExprEnv` remain deferred, non-breaking future
  work.
