# Spec 008 — Runtime-expression endpoints (`expr-lang/expr`) in the core

- **Status:** Approved by the user (2026-07-20) — build authorized (user relaxed the core dependency policy: "no
  problem to add expr to core deps"; placement "directly in core `msgin` package"; "treat it like SpEL"). **Round-1
  adversarial audit complete** (Opus, verified via a real expr v1.17.8 compile+run spike — all API assumptions TRUE,
  no blockers); findings folded (H1/M1/M2/L1-L3). **Resolved:** O8-1 → `header("k")` function form; O8-2 → **cut
  `RouterExprCases`**, ship `FilterExpr` + `RouterExpr` only; O8-3 (extensible context) deferred per the user
  ("consistent root model now, extensibility later"). **Both audit rounds complete — bundle SOUND** (round 2
  verified the folded fixes + closed a RouterExpr coverage hole). Gated only on an explicit user go-ahead for SDD.
- **Governing product spec:** [Spec 001 — Messaging core](001-messaging-core.md); builds on the composition
  endpoints ([ADR 0013](../adrs/0013-composition-endpoints.md)) — `Filter`/`Router` are the wrapped endpoints.
- **Records decisions in:** [ADR 0019 — Runtime expression evaluation (`expr-lang/expr`)](../adrs/0019-runtime-expression-evaluation.md)
  (to be **rewritten** from "deferred/module-only" into an *acceptance* ADR — expr accepted as a core dependency,
  mirroring [ADR 0016](../adrs/0016-robfig-cron-dependency.md) for robfig/cron).
- **Realized by:** [Plan 014 — expr endpoints](../plans/014-expr-endpoints.md) (planned).

## 1. Motivation

msgin's `Filter` and `Router` take **Go function values** — maximal for anything known at compile time. The one gap
a Go func cannot fill is a **runtime-defined** rule: a predicate/route supplied as a *string* from config, a
database, an admin UI, or per-tenant data (Spring Integration's SpEL role, e.g. `<filter expression="payload.amount
> 100"/>`). This increment adds that capability using `github.com/expr-lang/expr` — a safe, non-Turing-complete,
statically-checkable, **zero-dependency pure-Go (MIT)** expression engine (verified: its `go.mod` has no `require`
block; go 1.18).

The user has explicitly accepted `expr` as a **core dependency** and chosen to place the integration **directly in
the core `msgin` package** (not a submodule). The endpoints are thin wrappers that return the core's **existing**
`Step`/`*Router` types — the `Filter`/`Router` runtime is untouched.

## 2. Goals / Non-goals

### Goals

- **G1. `FilterExpr[A any](expression string, opts ...FilterOption) (Step, error)`** — a Message Filter whose bool
  predicate is a runtime `expr` string. Compiled **once at construction** with bool output enforced
  (`expr.AsBool()`); an invalid/mistyped expression returns a typed **construction** error (`ErrInvalidExpression`,
  the debuggability surface). Reuses the existing `FilterOption`/`WithDiscardChannel`. Returns the same `Step` type
  as `Filter`.
- **G2. Key-expression router** — `RouterExpr[A any](keyExpr string, routes map[string]MessageChannel, opts
  ...RouterOption) (*Router, error)`: `keyExpr` evaluates to a **string key** (`expr.AsKind(reflect.String)`); the
  message routes to `routes[key]`, falling back to `WithDefaultChannel` / `ErrNoRoute`. Spring's `<router
  expression=…>`. A ternary key (`payload.Amount >= 100 ? "big" : "small"`) covers multi-way routing in one
  construct. (A separate predicate-case router `RouterExprCases` was considered and **cut** — O8-2 — as redundant
  with the ternary key; it can be added later, non-breaking.)
- **G3. One internal, reusable compile primitive** (`unexported`) that compiles a `Message[A]` expression once into
  a typed callable, shared by G1/G2 and reusable by future `Splitter`/`Aggregator` — kept internal for now
  (minimal public surface; export later on concrete demand).
- **G4. Expression environment:** an expression can reference the message's **payload** (typed `A`, field access
  type-checked at compile time) and its **headers**. Exact header-access shape is a design point (D5) — `Headers` is
  a *struct*, not a map.
- **G5. Documented trade-offs on every public constructor's godoc:** the type-safety downgrade (a string typo is a
  construction error, not a build error), the debuggability downgrade (no Go stack trace inside an expression), and
  the security posture (D8).

### Non-goals

- **N1. No change to the `Filter`/`Router` runtime or any other core type.** The new constructors produce the
  existing `Step`/`*Router`; `handler.go`/`consumer.go`/`filter.go`/`router.go` behavior is unchanged.
- **N2. No exported compile primitive** this increment (G3 keeps it internal).
- **N3. No expr on other endpoints yet** — `Transformer` projection, header enrichment, `Splitter` collection,
  `Aggregator` correlation/release, `Delayer`. Future, non-breaking, reusing G3's primitive as those endpoints land.
- **N4. No config-file / DSL / rule-authoring surface.** msgin stays code-first: the caller supplies the expression
  *string* from wherever it lives; msgin only turns a string into an endpoint. (Consistent with Spec 001 "no
  config-authoring surface.")
- **N5. Not a sandbox for arbitrary untrusted input** without the caller's own additional limits (D8).

## 3. Decisions (settled with the user, 2026-07-20)

- **D1 — `expr` accepted as a core dependency (ADR 0019 rewritten).** Verified **zero-transitive-dependency,
  pure-Go, MIT** (`go.mod` has no `require` block), so the burden is one direct dep and *zero* transitive deps —
  the same profile as `robfig/cron` (ADR 0016). It becomes the **4th** blessed direct dependency of the repo and
  the **3rd third-party import of the pattern-core package** (after `clockwork` and `cenkalti/backoff`). This is a
  deliberate, user-authorized relaxation of the "core = stdlib + 2 exceptions" policy; ADR 0019 records the
  justification. CLAUDE.md's Dependency policy section is updated to list `expr` as an accepted core dependency.

- **D2 — Placement: the core `msgin` package, in its own `expr.go` file** (user's explicit choice over a submodule).
  Endpoints return the existing `Step` (`= func(next MessageHandler) MessageHandler`, handler.go:24) and `*Router`.
  Keeping them in a dedicated file limits the blast radius on the rest of the core package.

- **D3 — `FilterExpr` mirrors `Filter`, compiling at construction.** It `PayloadOf[A]`-asserts the payload (exactly
  as `Filter`), evaluates the compiled program against the environment, and treats the bool result identically to
  `Filter`'s predicate (pass → next; false → `WithDiscardChannel` or silent drop). Because compilation can fail, the
  signature returns `(Step, error)` (vs `Filter`'s `Step`) — the compile/type-check error is surfaced **once, at
  construction**, never per message.

- **D4 — `RouterExpr` returns `*Router` built on the existing `NewRouter`.** It compiles `keyExpr` to a string
  (all errors up front, at construction) and builds a `pick` closure that looks the key up in `routes` (miss → nil
  → `WithDefaultChannel`/`ErrNoRoute`, unchanged Router semantics). A nil/empty `routes` map or a nil channel value
  in `routes` is `ErrInvalidExpression` at construction (fail-fast; audit L2).

- **D5 — Expression environment: `payload` + `header("k")` (RESOLVED — function form).** `payload` = the typed `A`
  (struct-field access type-checked by expr against `A` **when `A` is concrete**; with `A = any` there is no
  compile-time check and errors surface at eval — audit L1). Header access is a `header("key")` env **function**
  returning the raw value (`any`) or nil if absent. Chosen over a `headers["key"]` map view because `msgin.Headers`
  deliberately never exposes its internal map (the immutability invariant, message.go:64): the map form would
  either hand expr the internal map or clone it per message, whereas the function form is zero-allocation and
  invariant-preserving. Env keys are lowercase (`payload`, `header`) for SpEL-like ergonomics. `header()` is
  untyped, so a missing/wrong-typed header in an ordering/typed comparison is a runtime eval error (audit L3).

- **D6 — One internal `compile[A](expression string, out exprOutputKind) (func(context.Context, Message[A]) (any,
  error), error)` primitive.** Sets the env types for `A`+headers, applies the output-type option (`AsBool` for
  filters, `AsKind(String)` for keys), compiles to an `*vm.Program`, and returns a per-message evaluator plus any
  compile error. Shared by D3/D4; unexported (N2/G3).

- **D7 — Typed errors + error routing.** Construction: `ErrInvalidExpression` (wraps expr's compile/type-check
  error, naming the offending expression) for a bad expression; a nil/empty expression is also `ErrInvalidExpression`;
  a nil `routes` map / nil channel follows existing Router conventions. **Runtime eval errors** (an expression that
  compiles but errors at eval — e.g. a nil-map index) propagate as the endpoint's returned `error`, exactly like a
  Go predicate's error today — into the runtime's retry/DLQ/invalid-message path. Every such branch is a hot-path
  typed-error branch requiring a test.

- **D8 — Security posture (real defaults + one concrete caveat; audit M1).** `expr` is **not** merely "documented,
  not enforced" — its defaults enforce concrete limits (verified v1.17.8): a compile-time AST cap
  (`MaxNodes = 1e4`) and a runtime `MemoryBudget` (`1e6`), which reject the usual abuse vectors (`repeat`, huge
  ranges, deep nesting, big `reduce` — all rejected as "memory budget exceeded"). It is non-Turing-complete (no
  unbounded loops), does no I/O, and calls no arbitrary Go code unless injected. It is therefore safe for
  **operator/config-authored** expressions. **The real residual gap:** there is **no time budget** and `expr`'s
  `vm.Run` **takes no context, so an evaluation cannot be cancelled** by the flow's context (this is why the
  evaluator drops `ctx`). The godoc states the concrete defaults AND this caveat, and that arbitrary **untrusted
  end-user** input still needs the caller's own limits. An `expr`-options escape-hatch (`WithExprEnv`/functions) for
  custom limits/functions is deferred (O8-3) — the default compile uses expr's safe defaults.

## 4. Architecture

Units (dependency points inward; the runtime is untouched):

1. **`expr.go` (core `msgin` package)** — `FilterExpr`, `RouterExpr`, the
   unexported `compile[A]` primitive, `ErrInvalidExpression`. Imports `github.com/expr-lang/expr` (+ `expr/vm`).
2. **`errors.go`** — add `ErrInvalidExpression`.
3. **`go.mod`** — add `require github.com/expr-lang/expr vX.Y.Z` (pin the latest release; confirm zero transitive
   at implementation).

Data flow (Filter): construction → `compile[A](expr, AsBool)` → (err? return `(nil, ErrInvalidExpression)`) → a
`Step` closure that, per message, `PayloadOf[A]` → build env `{payload, header}` → `program.Run(env)` → bool →
next/discard. Router: same compile-at-construction, evaluating `keyExpr` to a string key to pick `routes[key]`,
then the existing `Router.Handle` default/`ErrNoRoute` semantics.

## 5. Testing posture (per CLAUDE.md + custom skills)

- **Blackbox `_test` package** (`package msgin_test`); **assert-closure tables** (`table-test`); `t.Context()`;
  `gopls`; start each task from `cc-skills-golang:golang-how-to`; strict TDD.
- **`FilterExpr`:** a valid expr passes/rejects the right payloads; `WithDiscardChannel` receives rejects; an
  invalid expression → `ErrInvalidExpression` at construction (not at first message); a type-mismatched expression
  (`payload.Amount` where `Amount` is a string, or a non-bool result) → `ErrInvalidExpression`; a runtime eval error
  propagates as the handler error; header access works.
- **`RouterExpr`:** key→channel routing incl. default/`ErrNoRoute`; every construction-error branch (bad keyExpr,
  nil/empty routes, nil channel value in routes); an eval error propagates.
- **Coverage** ≥85% on the core package for the new code; **every hot-path/typed-error branch covered** — compile
  success/failure, each output-kind, pass/reject/discard, empty/nil-channel routes, key-hit/miss/default, eval-error.
  `Example…` tests for `FilterExpr` and `RouterExpr` (double as godoc).
- **No goroutine leaks** (the endpoints start none — pure wrappers); `-race` clean; `go mod tidy`/`go mod verify`
  confirm expr adds no transitive deps (the acceptance gate, mirroring ADR 0016).

## 6. Sequencing (the plan may split)

1. **ADR 0019 rewrite** (accept expr as a core dep) + **CLAUDE.md dependency-policy update** + `go.mod` add + the
   internal `compile[A]` primitive + `FilterExpr` + `ErrInvalidExpression` — fully unit-tested. The DB-free,
   smallest useful surface ships here.
2. **`RouterExpr` (key→map)** — key routing + tests.
3. Package doc / `Example` tests + whole-branch gate. (The former predicate-list router task is **cut**, O8-2.)

## 7. Risks

- **R1 — Debuggability downgrade (CLAUDE.md first-class criterion).** An expression is opaque to a Go debugger.
  Mitigation: compile-at-construction with type-checking surfaces most errors early with the offending expression
  named; documented prominently; the Go-func `Filter`/`Router` remain the recommended path for compile-time rules.
- **R2 — Type-safety downgrade.** A string typo escapes the compiler. Mitigation: `expr`'s type-checking against
  `A` + `AsBool`/`AsKind` output enforcement makes it a **construction/startup** error, not a mid-flow surprise.
- **R3 — (Resolved) Two router forms may be redundant.** Resolved by the audit (O8-2): `RouterExprCases` **cut**;
  ship only `RouterExpr` (a ternary key covers multi-way routing). One fewer type.
- **R4 — Security misuse.** A caller evaluating untrusted end-user expressions without limits. Mitigation: expr's
  default node/memory limits (D8) reject the usual abuse vectors; explicit godoc on the residual no-time-budget /
  no-ctx-cancellation caveat; a limits/functions escape-hatch is deferred (O8-3).
- **R5 — expr gains a transitive dep in a future version.** Mitigation: pin the version; the whole-branch gate
  asserts `go mod tidy`/`go.sum` show zero transitive deps (ADR 0016-style acceptance gate); re-verify on upgrade.
- **R6 — Reversibility / SemVer.** A core dep is hard to remove later (breaking). Mitigation: accepted deliberately
  by the user; ADR 0019 records it; the added API is additive (minor bump).

## 8. Open items (to close in the ADR + at the spec-review gate / audit)

- **O8-1 — (RESOLVED) Header-access shape (D5).** `header("k")` **function** form — zero-alloc, preserves the
  `Headers` immutability invariant. (The SpEL `headers["k"]` map form would expose/clone the internal map.)
- **O8-2 — (RESOLVED) Predicate-case router.** **Cut** — ship only `RouterExpr`; add later non-breaking if a
  concrete "many independent conditions" need appears.
- **O8-3 — `WithExprEnv` escape-hatch** (inject custom functions/variables/limits — SpEL's extensible context).
  Deferred per the user ("consistent root model now, extensibility later"); default = expr safe defaults.
- **O8-4 — (RESOLVED) Names** — `FilterExpr`/`RouterExpr`/`ErrInvalidExpression` confirmed idiomatic by the audit
  (`golang-naming`).
- **O8-5 — Reusable primitive export** (G3/N2) — export when `Splitter`/`Aggregator` land.

## 9. Traceability

- **Realized by:** Plan 014 → `Spec: 008` / `Plan: 014` / `ADR: 0019` trailers.
- **Records decisions in:** ADR 0019 (rewritten as the expr acceptance + integration design).
- **Builds on:** ADR 0013 (composition endpoints — `Filter`/`Router`), ADR 0016 (the zero-transitive-dep acceptance
  precedent for a core dependency), Spec 001 (code-first, no config-authoring surface).
- **Governing product spec:** Spec 001.
