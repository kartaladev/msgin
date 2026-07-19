# expr Runtime-Expression Endpoints Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.
>
> **Go skills are mandatory (CLAUDE.md writing-plans override):** every task starts from **`cc-skills-golang:golang-how-to`**, uses **`superpowers:test-driven-development`** (red→green→refactor), navigates via **`gopls`**, and obeys the project testing overrides: **`table-test`** (assert-closure tables, `t.Context()`), **`use-mockgen`**, **`use-testcontainers`**. Blackbox `_test` packages only.

**Goal:** Add runtime-expression Filter/Router endpoints (`FilterExpr`, `RouterExpr`) to the core `msgin` package, backed by `github.com/expr-lang/expr`, so predicates/routes can be supplied as strings at runtime — with **no change to the `Filter`/`Router` runtime**.

**Architecture:** Each constructor compiles an expression **once at construction** (via an unexported `compile[A]` primitive, env = `payload` + `header(k)`) into a Go closure, then hands that closure to the **existing** `Filter`/`NewRouter`. Compilation failures surface as `ErrInvalidExpression` at construction; eval errors propagate like any predicate error.

**Tech Stack:** Go 1.25, `github.com/expr-lang/expr` (new core dep — zero-transitive, pure-Go, MIT), stdlib.

## Global Constraints

- **Go 1.25 only** (`go.mod` `go 1.25`; build/test `GOTOOLCHAIN=go1.25.12`). No features newer than 1.25.
- **New core dependency: `github.com/expr-lang/expr`** (ADR 0019). It MUST add **zero transitive dependencies** — `go mod tidy` + `go.sum` inspection must show only `expr` itself added (the acceptance gate, mirroring ADR 0016). Pin a released version (`go get github.com/expr-lang/expr@latest`, currently v1.17.x — record the exact version).
- **No change to the `Filter`/`Router`/`Consumer` runtime.** The new constructors return the existing `Step`/`*Router` and delegate to the existing `Filter`/`NewRouter`. Only `expr.go` (new) + `errors.go` (one sentinel) + `go.mod`/`go.sum` + a CLAUDE.md dependency-policy line change.
- **Blackbox tests** (`package msgin_test`), **assert-closure tables**, `t.Context()`. `Example…` tests double as godoc.
- **Coverage** ≥85% on the core package for new code; **every hot-path/typed-error branch has a covering test** (compile ok/fail, each output kind, pass/reject/discard, empty/nil-channel routes, key hit/miss/default, eval error).
- **`go test ./... -race`** green; `go vet`/`gofmt`/`golangci-lint`/`govulncheck`/`CGO_ENABLED=0` clean; `go mod tidy` stable.
- Every exported symbol has a godoc comment, incl. the documented trade-offs (type-safety, debuggability, security — ADR 0019 §6).
- **Traceability:** every commit carries `Spec: 008` / `Plan: 014` / `ADR: 0019` trailers.

---

## File Structure

- `expr.go` (core `msgin`) — `compile[A]` primitive, `exprEnv[A]`, `exprOutputKind`, `FilterExpr`, `RouterExpr`. New.
- `errors.go` — add `ErrInvalidExpression`. Modify.
- `expr_test.go` (`package msgin_test`) — tests. New.
- `go.mod` / `go.sum` — add `expr`. Modify.
- `CLAUDE.md` — add `expr` to the accepted-core-dependencies list (rides in Task 1). Modify.

---

### Task 1: `expr` dependency + `compile[A]` primitive + `FilterExpr`

**Files:** Create `expr.go`, `expr_test.go`; Modify `errors.go`, `go.mod`, `go.sum`, `CLAUDE.md`.

**Interfaces:**
- Consumes (existing): `Filter[A](pred func(ctx, Message[A]) (bool, error), opts ...FilterOption) Step`, `PayloadOf[A]`, `Message[A].Payload()`, `Message[A].Header(key) (any, bool)`, `FilterOption`.
- Produces: unexported `compile[A any](expression string, kind exprOutputKind) (func(Message[A]) (any, error), error)`; `type exprOutputKind int` (`exprBool`, `exprString`); `type exprEnv[A any] struct`; `func FilterExpr[A any](expression string, opts ...FilterOption) (Step, error)`; sentinel `ErrInvalidExpression`.

- [ ] **Step 1: Add the dependency.** `GOTOOLCHAIN=go1.25.12 go get github.com/expr-lang/expr@latest`. Then verify zero transitive deps: `go mod tidy && git diff go.sum` shows only `github.com/expr-lang/expr` lines added. Record the pinned version.

Run: `GOTOOLCHAIN=go1.25.12 go mod tidy && grep -c expr-lang go.sum`
Expected: only `expr-lang/expr` entries added; no other new module.

- [ ] **Step 2: Write the failing test** (`expr_test.go`).

```go
package msgin_test

import (
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/require"
)

type order struct {
	Amount   int
	Currency string
}

// collector is a MessageChannel that records what it receives (blackbox helper).
type collector struct{ got []msgin.Message[any] }

func (c *collector) Send(_ context.Context, m msgin.Message[any]) error { c.got = append(c.got, m); return nil }
func (c *collector) Subscribe(msgin.MessageHandler) error               { return nil }

// run drives a single message through a Step to a terminal recording handler.
func runStep(t *testing.T, step msgin.Step, m msgin.Message[any]) (passed bool) {
	t.Helper()
	next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { passed = true; return nil })
	require.NoError(t, step(next).Handle(t.Context(), m))
	return passed
}

func TestFilterExpr(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "invalid expression is ErrInvalidExpression at construction",
			assert: func(t *testing.T) {
				_, err := msgin.FilterExpr[order]("payload.Amount >") // syntax error
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "empty expression is ErrInvalidExpression",
			assert: func(t *testing.T) {
				_, err := msgin.FilterExpr[order]("   ")
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "non-bool result is rejected at construction (AsBool)",
			assert: func(t *testing.T) {
				_, err := msgin.FilterExpr[order]("payload.Amount") // int, not bool
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "unknown field is rejected at construction (type-check against A)",
			assert: func(t *testing.T) {
				_, err := msgin.FilterExpr[order]("payload.Nope > 1")
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "true predicate passes the message through",
			assert: func(t *testing.T) {
				step, err := msgin.FilterExpr[order](`payload.Amount > 100 && payload.Currency == "USD"`)
				require.NoError(t, err)
				require.True(t, runStep(t, step, msgin.New[any](order{Amount: 150, Currency: "USD"})))
			},
		},
		{
			name: "false predicate drops (silent) and can divert to WithDiscardChannel",
			assert: func(t *testing.T) {
				disc := &collector{}
				step, err := msgin.FilterExpr[order](`payload.Amount > 100`, msgin.WithDiscardChannel(disc))
				require.NoError(t, err)
				require.False(t, runStep(t, step, msgin.New[any](order{Amount: 5})))
				require.Len(t, disc.got, 1)
			},
		},
		{
			name: "header access via header()",
			assert: func(t *testing.T) {
				step, err := msgin.FilterExpr[order](`header("vip") == true`)
				require.NoError(t, err)
				m := msgin.New[any](order{}).WithHeader("vip", true)
				require.True(t, runStep(t, step, m))
			},
		},
		{
			name: "runtime eval error propagates as the handler error",
			assert: func(t *testing.T) {
				// A missing header is nil; ordering a nil against an int is a real
				// expr eval error (verified v1.17.8: "invalid operation: <nil> > int").
				// NB: divide-by-zero does NOT error in expr — do not use it here.
				step, err := msgin.FilterExpr[order](`header("missing") > 100`)
				require.NoError(t, err) // compiles fine (header() is any)
				next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
				err = step(next).Handle(t.Context(), msgin.New[any](order{}))
				require.Error(t, err) // the eval error surfaces to the runtime
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}
```

(Add `context` import. Verified against the codebase: `Message.WithHeader` exists (message.go:197) and returns a new Message; `collector` satisfies `MessageChannel`.)

- [ ] **Step 3: Run the test to verify it fails.**

Run: `GOTOOLCHAIN=go1.25.12 go test . -run TestFilterExpr`
Expected: FAIL — `undefined: msgin.FilterExpr`, `undefined: msgin.ErrInvalidExpression`.

- [ ] **Step 4: Add the sentinel** to `errors.go` (in the existing `var (…)` block):

```go
	// ErrInvalidExpression is returned by FilterExpr/RouterExpr when an
	// expression is empty, unparseable, or fails type-checking against the
	// payload type at construction. The wrapped error names the offending
	// expression. Runtime evaluation errors are NOT this — they propagate as the
	// endpoint's handler error into the runtime's retry/DLQ path.
	ErrInvalidExpression = errors.New("msgin: invalid expression")
```

- [ ] **Step 5: Write `expr.go`.**

```go
package msgin

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// exprEnv is the environment an expression evaluates against: payload (the typed
// A, whose fields are type-checked at compile time) and a header(key) function
// returning the header value (or nil). Struct tags expose lowercase names for
// SpEL-like ergonomics: payload.Field, header("key").
type exprEnv[A any] struct {
	Payload A                `expr:"payload"`
	Header  func(string) any `expr:"header"`
}

type exprOutputKind int

const (
	exprBool exprOutputKind = iota
	exprString
)

// compile parses and type-checks expression once against exprEnv[A], enforcing the
// requested output kind (bool for predicates, string for routing keys), and
// returns a per-message evaluator. A nil/empty/unparseable/mistyped expression is
// ErrInvalidExpression (naming the expression) — surfaced at construction, never
// per message.
func compile[A any](expression string, kind exprOutputKind) (func(Message[A]) (any, error), error) {
	if strings.TrimSpace(expression) == "" {
		return nil, fmt.Errorf("%w: empty expression", ErrInvalidExpression)
	}
	opts := []expr.Option{expr.Env(exprEnv[A]{})}
	switch kind {
	case exprString:
		opts = append(opts, expr.AsKind(reflect.String))
	default:
		opts = append(opts, expr.AsBool())
	}
	program, err := expr.Compile(expression, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w %q: %w", ErrInvalidExpression, expression, err)
	}
	return func(msg Message[A]) (any, error) {
		return vm.Run(program, exprEnv[A]{
			Payload: msg.Payload(),
			Header:  func(k string) any { v, _ := msg.Header(k); return v },
		})
	}, nil
}

// FilterExpr is a Message Filter (see Filter) whose bool predicate is a runtime
// expr-lang expression evaluated against the payload (A) and headers, e.g.
// `payload.Amount > 100 && header("region") == "EU"`. The expression is compiled
// and type-checked once; an invalid or non-bool expression returns
// ErrInvalidExpression at construction. It returns the same Step as Filter and
// honors WithDiscardChannel.
//
// Because it returns (Step, error), it cannot be passed inline to Chain like a
// bare Filter — construct it first, check the error, then compose (see the
// example).
//
// Trade-offs vs a Go-func Filter:
//   - Type safety: a string typo or type mismatch is a construction error, not a
//     build error. payload field access is type-checked against A ONLY when A is a
//     concrete (non-interface) type; with A = any there is no compile-time check
//     and errors surface at evaluation.
//   - Debuggability: an expression is opaque to a Go debugger. Prefer Filter for
//     compile-time predicates.
//   - header("k") returns an untyped value (any); a missing header is nil, so an
//     ordering/typed comparison on an absent or wrong-typed header is a runtime
//     evaluation error (returned as the handler error, into retry/DLQ).
//
// Security: expr enforces safe defaults — a compile-time AST node cap
// (MaxNodes = 1e4) and a runtime MemoryBudget (1e6) — which reject the usual
// abuse vectors (huge ranges, repeat, deep nesting). It is therefore suitable for
// operator/config-authored expressions. Caveat: there is NO time budget and
// evaluation is NOT context-cancellable (expr's vm.Run takes no context), so keep
// operator expressions bounded; do not evaluate arbitrary untrusted end-user
// input without your own additional limits.
func FilterExpr[A any](expression string, opts ...FilterOption) (Step, error) {
	eval, err := compile[A](expression, exprBool)
	if err != nil {
		return nil, err
	}
	return Filter(func(_ context.Context, m Message[A]) (bool, error) {
		out, err := eval(m)
		if err != nil {
			return false, err
		}
		b, _ := out.(bool) // AsBool guarantees a bool result
		return b, nil
	}, opts...), nil
}
```

- [ ] **Step 6: Run the test to verify it passes.**

Run: `GOTOOLCHAIN=go1.25.12 go test . -run TestFilterExpr -race`
Expected: PASS (all subtests).

- [ ] **Step 7: Update CLAUDE.md** — in the Dependency policy section, add `expr` to the accepted core dependencies (one clause, citing ADR 0019 + the zero-transitive verification). Keep it factual and minimal.

- [ ] **Step 8: Coverage + vet.**

Run: `GOTOOLCHAIN=go1.25.12 go test . -run TestFilterExpr -cover && go vet ./...`
Expected: new code covered (each construction-error branch + pass/reject/discard/header); vet clean.

**Hot-path branches:** empty expr; compile error; non-bool/unknown-field type error; pass; reject-silent; reject-discard; header access; eval-error (the `header("missing") > 100` case above — a compiled expr that errors at eval; divide-by-zero does NOT error in expr, do not use it).

- [ ] **Step 9: Commit.**

```bash
git add expr.go expr_test.go errors.go go.mod go.sum CLAUDE.md \
        docs/specs/008-expr-endpoints.md docs/adrs/0019-runtime-expression-evaluation.md \
        docs/plans/014-expr-endpoints.md
git commit -m "$(cat <<'EOF'
feat(core): FilterExpr — runtime expr-lang predicate filter

Add github.com/expr-lang/expr (zero-transitive core dep, ADR 0019); an
unexported compile[A] primitive (env: payload + header()) and FilterExpr,
which compiles a bool expression once and delegates to the existing Filter.

Spec: 008
Plan: 014
ADR: 0019
EOF
)"
```

---

### Task 2: `RouterExpr` (key → routes map)

**Files:** Modify `expr.go`, `expr_test.go`.

**Interfaces:**
- Consumes: `NewRouter(pick func(ctx, Message[any]) (MessageChannel, error), opts ...RouterOption) *Router`, `WithDefaultChannel`, `ErrNoRoute`, `PayloadOf[A]`, `compile[A]` (Task 1).
- Produces: `func RouterExpr[A any](keyExpr string, routes map[string]MessageChannel, opts ...RouterOption) (*Router, error)`.

- [ ] **Step 1: Write the failing test** — assert-closure table: bad keyExpr → `ErrInvalidExpression`; nil routes → `ErrInvalidExpression`; a message whose key hits a route goes there; a miss with `WithDefaultChannel` goes to default; a miss without default → `ErrNoRoute` from `Router.Handle`.

```go
func TestRouterExpr(t *testing.T) {
	eu, us, def := &collector{}, &collector{}, &collector{}
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "bad key expression is ErrInvalidExpression",
			assert: func(t *testing.T) {
				// Non-empty routes so the keyExpr COMPILE-error branch is reached
				// (an empty map would trip the len(routes)==0 guard first — NEW-1).
				_, err := msgin.RouterExpr[order]("payload.", map[string]msgin.MessageChannel{"x": eu})
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "nil/empty routes is ErrInvalidExpression",
			assert: func(t *testing.T) {
				_, err := msgin.RouterExpr[order]("payload.Currency", nil)
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "a nil channel value in routes is ErrInvalidExpression",
			assert: func(t *testing.T) {
				_, err := msgin.RouterExpr[order]("payload.Currency",
					map[string]msgin.MessageChannel{"EUR": nil})
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "key hit routes to the mapped channel",
			assert: func(t *testing.T) {
				r, err := msgin.RouterExpr[order]("payload.Currency",
					map[string]msgin.MessageChannel{"EUR": eu, "USD": us})
				require.NoError(t, err)
				require.NoError(t, r.Handle(t.Context(), msgin.New[any](order{Currency: "EUR"})))
				require.Len(t, eu.got, 1)
				require.Empty(t, us.got)
			},
		},
		{
			name: "miss with default routes to default; without default is ErrNoRoute",
			assert: func(t *testing.T) {
				withDef, _ := msgin.RouterExpr[order]("payload.Currency",
					map[string]msgin.MessageChannel{"EUR": eu}, msgin.WithDefaultChannel(def))
				require.NoError(t, withDef.Handle(t.Context(), msgin.New[any](order{Currency: "GBP"})))
				require.Len(t, def.got, 1)

				noDef, _ := msgin.RouterExpr[order]("payload.Currency", map[string]msgin.MessageChannel{"EUR": eu})
				require.ErrorIs(t, noDef.Handle(t.Context(), msgin.New[any](order{Currency: "GBP"})), msgin.ErrNoRoute)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}
```

- [ ] **Step 2: Run to verify it fails.** `GOTOOLCHAIN=go1.25.12 go test . -run TestRouterExpr` → `undefined: msgin.RouterExpr`.

- [ ] **Step 3: Implement `RouterExpr` in `expr.go`.**

```go
// RouterExpr is a content-based Router (see NewRouter) whose channel is chosen by
// a routing-key expression: keyExpr is compiled to a string key and the message
// routes to routes[key], falling back to WithDefaultChannel or ErrNoRoute. An
// empty/nil routes map, a nil channel value in routes, or a bad keyExpr is
// ErrInvalidExpression at construction. Mirrors Spring Integration's
// <router expression="…">; a ternary key (`payload.Amount >= 100 ? "big" : "small"`)
// gives first-match multi-way routing in one construct. Same trade-offs and
// security posture as FilterExpr (type-safety only for concrete A, no Go stack
// trace in an expression, expr's default node/memory limits, no ctx-cancellation).
func RouterExpr[A any](keyExpr string, routes map[string]MessageChannel, opts ...RouterOption) (*Router, error) {
	if len(routes) == 0 {
		return nil, fmt.Errorf("%w: empty routes map", ErrInvalidExpression)
	}
	for k, ch := range routes { // fail fast on a literal-nil channel value (audit L2).
		if ch == nil { // NB: catches a nil interface, not a typed-nil sink — that still fails at Send.
			return nil, fmt.Errorf("%w: nil channel for route %q", ErrInvalidExpression, k)
		}
	}
	eval, err := compile[A](keyExpr, exprString)
	if err != nil {
		return nil, err
	}
	return NewRouter(func(_ context.Context, m Message[any]) (MessageChannel, error) {
		in, err := PayloadOf[A](m)
		if err != nil {
			return nil, err
		}
		out, err := eval(in)
		if err != nil {
			return nil, err
		}
		key, _ := out.(string) // AsKind(String) guarantees a string result
		return routes[key], nil // miss → nil → NewRouter's default/ErrNoRoute handling
	}, opts...), nil
}
```

- [ ] **Step 4: Run to verify it passes.** `GOTOOLCHAIN=go1.25.12 go test . -run TestRouterExpr -race` → PASS.

**Hot-path branches:** empty/nil routes; nil channel value in routes; bad keyExpr; PayloadOf error; eval error (e.g. a key expr like `header("missing") + payload.Currency` where the missing header nils the concat — assert the handler returns the error); key hit; key miss→default; key miss→ErrNoRoute.

- [ ] **Step 5: Commit.**

```bash
git add expr.go expr_test.go
git commit -m "$(cat <<'EOF'
feat(core): RouterExpr — key-expression content router

Compile a routing-key expression to a string and route via a
map[string]MessageChannel, delegating to the existing NewRouter.

Spec: 008
Plan: 014
ADR: 0019
EOF
)"
```

---


### Task 3: Examples, package doc, whole-branch gate

**Files:** Modify `expr_test.go` (Examples), `doc_composition.go` (a short expr paragraph — there is no root `doc.go`).

- [ ] **Step 1: Add runnable `Example`s** — `ExampleFilterExpr` and `ExampleRouterExpr` driving a message through the endpoint to a recording handler/channel with deterministic `// Output:` (drive the `Step`/`Router.Handle` synchronously, as Task 1's `runStep` does — no `Consumer.Run`). **`ExampleFilterExpr` MUST show the two-line `step, err := msgin.FilterExpr[...](...)` → error-check → `Chain(step, …)` pattern** (audit M2), so the `(Step, error)`-vs-inline-`Filter` friction is explicit in the godoc-facing example.

- [ ] **Step 2: Package doc** — add a short paragraph to the composition package doc noting the expr endpoints exist, that they are for runtime-defined rules, and pointing at the Go-func `Filter`/`Router` as the compile-time default.

- [ ] **Step 3: Whole-branch delivery gate.**
  - `GOTOOLCHAIN=go1.25.12 go build ./... && go test ./... -race` green; `CGO_ENABLED=0 go build ./...` ok.
  - `go vet ./...`, `golangci-lint run ./...`, `gofmt -l .` clean; `go mod tidy`/`go mod verify` no diff; **`go.sum` shows `expr` added zero transitive deps** (acceptance gate); `govulncheck ./...` clean.
  - `apidiff`/`gorelease` (or manual): the new exported surface (`FilterExpr`, `RouterExpr`, `ErrInvalidExpression`) is additive → minor bump.
  - Run **`/code-review`** and **`/security-review`** over `main..HEAD` — the security review MUST consider the expr attack surface (untrusted-expression evaluation); resolve/triage every finding; re-run `-race`.
  - Update `docs/HANDOVER.md`; confirm cross-links + trailers.

- [ ] **Step 4: Commit** the examples/doc; the design docs (spec/ADR/plan) rode in Task 1.

## Self-review notes (coverage of Spec 008)

- G1 `FilterExpr` → Task 1. G2 `RouterExpr` → Task 2. G3 internal `compile[A]` → Task 1. G4 env (payload + `header()`) → Task 1 (`exprEnv[A]`). G5 documented trade-offs → godoc in Tasks 1-2 (M1-accurate security posture). D1 dep acceptance + zero-transitive gate → Task 1 Step 1 + Task 3 gate. N1 no runtime change — verified: constructors delegate to existing `Filter`/`NewRouter`.
- **Round-1 audit (2026-07-20, Opus, verified via a real expr v1.17.8 compile+run spike) — findings folded:** all 6 expr API assumptions VERIFIED-TRUE; H1 (eval-error repro corrected to `header("missing") > 100` — divide-by-zero does not error in expr), M1 (security godoc rewritten to expr's real defaults — MaxNodes 1e4 / MemoryBudget 1e6 — + the no-time-budget / no-ctx-cancellation caveat), M2 (Example shows the two-line construct-then-Chain), L1 (type-check only for concrete A), L2 (nil channel in routes → `ErrInvalidExpression`), L3 (`header()` untyped, missing→eval error). **Resolved:** O8-1 → `header("k")` function form (zero-alloc, preserves the Headers immutability invariant — the map form would expose/clone the internal map); O8-2 → **cut `RouterExprCases`** (a ternary key in `RouterExpr` covers multi-way routing; smaller surface). O8-3 (`WithExprEnv`/custom functions escape-hatch — extensible context) deferred per the user ("consistent root model now, extensibility later"); expr's safe defaults hold in the meantime.
- **Reuse win:** every constructor compiles-then-delegates to the existing `Filter`/`NewRouter`, so there is zero duplicated filter/route runtime logic — the endpoints are pure adapters over `expr` + the existing endpoints.
