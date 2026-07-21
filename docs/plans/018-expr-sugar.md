# expr Sugar for the Transformation-Group Endpoints — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.
>
> **Go skills are mandatory (CLAUDE.md writing-plans override):** every task starts from **`cc-skills-golang:golang-how-to`**, uses **`superpowers:test-driven-development`** (red→green→refactor), navigates/refactors via **`gopls`**, and obeys the project testing overrides: **`table-test`** (assert-closure tables, `t.Context()`), **`use-mockgen`**, **`use-testcontainers`**. Blackbox `_test` packages only.

**Goal:** Add the runtime-expression variants of the transformation-group endpoints to the core `msgin` package — `TransformExpr[A,B]`, `SplitExpr[A,B]`, and the Aggregator options `WithCorrelationExpr[A]` / `WithReleaseExpr[A]` — reusing the existing `compile[A]` primitive and `Transform`/`Split`/aggregator machinery. **No DB, no new dependency** (`expr` already in-core, ADR 0019); aggregate-by-expr is deferred (Spec 009 §2).

**Architecture:** Each constructor compiles its expression **once at construction** into a Go closure and hands that closure to the **existing** `Transform`/`Split`/aggregator strategy seams — no new runtime. Two internal `compile` extensions: an `exprAny` output kind (result asserted to `B` at eval) for the projection endpoints, and a group-scoped env (`{messages []{payload,header}, size int}`) for `WithReleaseExpr`. One internal refactor: the Aggregator's release-strategy signature evolves to `func(MessageGroup) (bool, error)` so an expr eval error propagates (the public `WithReleaseStrategy(bool)` is unchanged). Expr *options* report compile errors through `NewAggregator` (they cannot return an error themselves).

**Tech Stack:** Go 1.25, `github.com/expr-lang/expr` (existing core dep), stdlib.

**Spec:** [Spec 009 §3.5 (D12–D14)](../specs/009-splitter-aggregator-endpoints.md). **ADR:** [ADR 0019 Addendum (2026-07-21)](../adrs/0019-runtime-expression-evaluation.md). Continues [Plan 014](014-expr-endpoints.md) (`FilterExpr`/`RouterExpr`), [Plan 015](015-splitter.md) (`Split`), [Plans 016–017](016-aggregator.md) (Aggregator + group store).

## Global Constraints

- **Go 1.25 only** (`go.mod` `go 1.25`; build/test `GOTOOLCHAIN=go1.25.12`). No features newer than 1.25.
- **No new dependency.** `expr` is already in-core. The whole-branch gate re-asserts `go mod tidy`/`go.sum` show **no** new module and `go mod verify` passes.
- **No exported signature change to any shipped symbol.** New surfaces are purely additive → **minor SemVer**. In particular `WithReleaseStrategy(func(MessageGroup) bool)` keeps its signature; only the *internal* `aggregatorConfig.release` type changes (D14c). Verify with `apidiff`/`gorelease` (or manual): additions only.
- **All expr constructors live in `expr.go`** (ADR 0019 §2 blast-radius decision), alongside `FilterExpr`/`RouterExpr`. The release-strategy `(bool,error)` refactor + the `optErr` plumbing live in `aggregator.go`; the shared child-forward helper lives in `splitter.go`.
- **Blackbox tests** (`package msgin_test`), **assert-closure tables**, `t.Context()`. `Example…` tests double as godoc. **`goleak`** already guards the aggregator suite — keep it clean.
- **Coverage** ≥85% on the core package for new code; **every hot-path/typed-error branch has a covering test** (enumerated per task).
- **`go test ./... -race`** green; `go vet`/`gofmt`/`golangci-lint`/`govulncheck`/`CGO_ENABLED=0` clean; `go mod tidy`/`go mod verify` stable.
- Every exported symbol has a godoc comment carrying the ADR 0019 §6 trade-offs (type-safety, debuggability, security/no-ctx-cancellation) **and** the struct-B ceiling for `TransformExpr`/`SplitExpr`.
- **Traceability:** every commit carries `Spec: 009` / `Plan: 018` / `ADR: 0019` trailers.

---

## File Structure

- `expr.go` (core `msgin`) — add `exprAny` kind; `TransformExpr[A,B]`; `SplitExpr[A,B]`; `WithCorrelationExpr[A]`; `WithReleaseExpr[A]`; `exprMember[A]`; `groupExprEnv[A]`; `compileGroup[A]`; `toGroupEnv[A]`. **Modify.**
- `splitter.go` — extract the child-forward-with-sequence-stamping loop into an unexported `forwardSplit[B]` helper reused by `Split` and `SplitExpr`. **Modify.**
- `aggregator.go` — evolve `aggregatorConfig.release` to `func(MessageGroup) (bool, error)`; update `defaultRelease`/`WithCompletionSize`/`WithReleaseStrategy`/`Handle`/`release`(drain)/`reapGroup`; add `optErr` + surface it in `NewAggregator`. **Modify.**
- `errors.go` — add `ErrExprResultType` (runtime expr result-type mismatch). **Modify.**
- `expr_test.go` (`package msgin_test`) — tests + Examples for the four new surfaces. **Modify.**
- `aggregator_test.go` / `aggregator_settlement_test.go` — regression + a `WithReleaseExpr` eval-error-propagates case; confirm existing release paths unchanged. **Modify.**
- `doc_composition.go` — one short paragraph naming the new expr endpoints. **Modify.**

---

### Task 1: `exprAny` output kind + `TransformExpr[A,B]` + `ErrExprResultType`

**Files:** Modify `expr.go`, `errors.go`, `expr_test.go`.

**Interfaces:**
- Consumes (existing): `Transform[A,B](fn func(ctx, Message[A]) (Message[B], error)) Step`, `compile[A]`, `WithPayload[A,B]`, `Message[A].Payload/Header`.
- Produces: `exprAny exprOutputKind`; `func TransformExpr[A, B any](expression string) (Step, error)`; sentinel `ErrExprResultType`.

- [ ] **Step 1: Add the sentinel** to `errors.go` (existing `var (…)` block):

```go
	// ErrExprResultType is returned (as the endpoint's handler error) by
	// TransformExpr/SplitExpr when a compiled expression evaluates to a value that
	// is not the asserted output type — a non-B TransformExpr result, or a
	// non-slice SplitExpr result / non-B SplitExpr element. It is an EVALUATION
	// (not construction) error: ErrInvalidExpression covers compile-time faults,
	// this covers a well-typed-at-compile expression whose runtime value is the
	// wrong Go type (possible when A/B is an interface, so expr cannot type-check
	// the result). It propagates into the runtime's retry/DLQ path like any eval
	// error.
	ErrExprResultType = errors.New("msgin: expression result type mismatch")
```

- [ ] **Step 2: Write the failing test** (`expr_test.go`, new `TestTransformExpr`, assert-closure table). Cover: empty/bad expr → `ErrInvalidExpression` at construction; a scalar projection (`payload.Amount * 2` → int) evaluates and the child carries the parent's id/correlation (header propagation via `WithPayload`); a non-B result → `ErrExprResultType` handler error (use `A=any`/`B=string` with an int-yielding expr so the mismatch is a runtime, not compile, fault); a raw eval error (`header("missing") > 100`) propagates. Reuse Plan 014's existing `exprOrder` (`Amount int`, `Currency string`) / `collector` / `runStep` helpers (already in `expr_test.go` — the type is named `exprOrder`, not `order`).

```go
func TestTransformExpr(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "empty expression is ErrInvalidExpression at construction",
			assert: func(t *testing.T) {
				_, err := msgin.TransformExpr[exprOrder, int]("   ")
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "unparseable (non-empty) expression is ErrInvalidExpression (L-4: the Compile-failure branch)",
			assert: func(t *testing.T) {
				_, err := msgin.TransformExpr[exprOrder, int]("payload.")
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "scalar projection propagates parent headers via WithPayload",
			assert: func(t *testing.T) {
				step, err := msgin.TransformExpr[exprOrder, int]("payload.Amount * 2")
				require.NoError(t, err)
				var got msgin.Message[any]
				next := msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error { got = m; return nil })
				parent := msgin.New[any](exprOrder{Amount: 21}).WithHeader(msgin.HeaderCorrelationID, "c1")
				require.NoError(t, step(next).Handle(t.Context(), parent))
				require.Equal(t, 42, got.Payload())
				cid, _ := got.Header(msgin.HeaderCorrelationID)
				require.Equal(t, "c1", cid) // WithPayload copied the parent's headers
			},
		},
		{
			name: "non-B result is ErrExprResultType at eval",
			assert: func(t *testing.T) {
				// A=any → no compile-time result type-check; expr yields int, B=string.
				step, err := msgin.TransformExpr[any, string]("1 + 1")
				require.NoError(t, err)
				next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
				err = step(next).Handle(t.Context(), msgin.New[any](0))
				require.ErrorIs(t, err, msgin.ErrExprResultType)
			},
		},
		{
			name: "numeric ceiling (M-3): int-yielding expr with B=int64 is ErrExprResultType",
			assert: func(t *testing.T) {
				// expr integer math yields Go int, not int64; out.(int64) fails.
				// Documents the numeric-B ceiling as a tested contract.
				step, err := msgin.TransformExpr[exprOrder, int64]("payload.Amount")
				require.NoError(t, err)
				next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
				err = step(next).Handle(t.Context(), msgin.New[any](exprOrder{Amount: 7}))
				require.ErrorIs(t, err, msgin.ErrExprResultType)
			},
		},
		{
			name: "runtime eval error propagates",
			assert: func(t *testing.T) {
				step, err := msgin.TransformExpr[exprOrder, any](`header("missing") > 100`)
				require.NoError(t, err)
				next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
				require.Error(t, step(next).Handle(t.Context(), msgin.New[any](exprOrder{})))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}
```

- [ ] **Step 3: Run to verify it fails.** `GOTOOLCHAIN=go1.25.12 go test . -run TestTransformExpr` → `undefined: msgin.TransformExpr` / `msgin.ErrExprResultType`.

- [ ] **Step 4: Add `exprAny` to `compile`** (`expr.go`). Add the const and a switch case that applies **no** output option:

```go
const (
	exprBool exprOutputKind = iota
	exprString
	exprAny // no AsBool/AsKind constraint; the result is asserted by the caller
)
```
and in `compile`'s switch:
```go
	case exprAny:
		// no output constraint; caller asserts the result type at eval
```
(The existing `default` remains `exprBool`.)

- [ ] **Step 5: Implement `TransformExpr`** (`expr.go`), delegating to the existing `Transform`:

```go
func TransformExpr[A, B any](expression string) (Step, error) {
	eval, err := compile[A](expression, exprAny)
	if err != nil {
		return nil, err
	}
	return Transform(func(_ context.Context, m Message[A]) (Message[B], error) {
		out, err := eval(m)
		if err != nil {
			return Message[B]{}, err
		}
		b, ok := out.(B)
		if !ok {
			return Message[B]{}, fmt.Errorf("%w: result %T is not %T", ErrExprResultType, out, *new(B))
		}
		return WithPayload(m, b), nil
	}), nil
}
```
Godoc: mirror `FilterExpr`'s trade-off/security block; **add the struct-B ceiling** ("expr cannot construct an arbitrary Go struct; B is realistically a scalar/slice/map/named-field type — use `Transform` for struct projection") **and the numeric-B ceiling (M-3)** ("expr integer math yields Go `int` and float math yields `float64`; `out.(B)` is an exact assertion, so B must be exactly `int`/`float64`/`string`/`bool` or a concrete element type — an `int64`/`uint`/`float32` B fails at eval with `ErrExprResultType`"); note it returns the same `Step` as `Transform` (two-line construct-then-`Chain`).

- [ ] **Step 6: Run to verify it passes.** `GOTOOLCHAIN=go1.25.12 go test . -run TestTransformExpr -race` → PASS.

**Hot-path branches:** empty/bad expr → `ErrInvalidExpression`; eval ok → asserted-B + `WithPayload` header propagation; non-B result → `ErrExprResultType`; raw eval error propagates.

- [ ] **Step 7: Coverage + vet.** `GOTOOLCHAIN=go1.25.12 go test . -run TestTransformExpr -cover && go vet ./...`.

- [ ] **Step 8: Commit** (design docs ride in this first task):

```bash
git add expr.go errors.go expr_test.go \
        docs/specs/009-splitter-aggregator-endpoints.md \
        docs/adrs/0019-runtime-expression-evaluation.md \
        docs/plans/018-expr-sugar.md
git commit -m "$(cat <<'EOF'
feat(core): TransformExpr — runtime expr projection endpoint

Add an exprAny compile output kind (no AsBool/AsKind constraint) and
TransformExpr[A,B], which evaluates a projection expression, asserts the
result to B (ErrExprResultType on mismatch), and delegates to the existing
Transform with WithPayload header propagation.

Spec: 009
Plan: 018
ADR: 0019
EOF
)"
```

---

### Task 2: `SplitExpr[A,B]` (+ extract shared `forwardSplit` helper)

**Files:** Modify `splitter.go`, `expr.go`, `expr_test.go`.

**Interfaces:**
- Consumes (existing): `Split`'s stamping loop, `stampSequence`, `boxMessage`, `PayloadOf[A]`, `WithPayload`, `compile[A]` (`exprAny`, Task 1).
- Produces: unexported `forwardSplit[B any](ctx, next MessageHandler, parent Message[any], children []Message[B]) error`; `func SplitExpr[A, B any](expression string) (Step, error)`.

- [ ] **Step 1: Refactor `Split` to extract `forwardSplit`** (`splitter.go`) — behavior-preserving; the existing `TestSplit`/`Example_splitter` must stay green. `forwardSplit` is the current `for i, child := range children { stampSequence… ; next.Handle(boxMessage…) }` loop; `Split`'s handler calls it after `fn`. (Use `gopls` extract-function.)

```go
// forwardSplit stamps each child for reassembly (see stampSequence) and forwards
// it to next IN ORDER, aborting on the first error (remaining children not sent).
// An empty children slice forwards nothing and returns nil. Shared by Split and
// SplitExpr.
func forwardSplit[B any](ctx context.Context, next MessageHandler, parent Message[any], children []Message[B]) error {
	n := len(children)
	for i, child := range children {
		stamped := stampSequence(child, parent, i+1, n)
		if err := next.Handle(ctx, boxMessage(stamped)); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 2: Confirm the refactor is green.** `GOTOOLCHAIN=go1.25.12 go test . -run 'TestSplit|Example_splitter' -race` → PASS (no behavior change).

- [ ] **Step 3: Write the failing test** (`expr_test.go`, `TestSplitExpr`, assert-closure table). Cover: empty/bad expr → `ErrInvalidExpression`; a slice-yielding expr (`payload.Items` where `Items []item`) fans out N children **in order** with sequence headers stamped (assert `HeaderSequenceNumber`/`HeaderSequenceSize` + `parentID#seq` id like `TestSplit`); an empty slice forwards nothing; a **non-slice** result → `ErrExprResultType`; a **non-B element** → `ErrExprResultType` (use `B` concrete, elements `[]any` with a wrong-typed entry); a raw eval error propagates.

- [ ] **Step 4: Run to verify it fails.** `GOTOOLCHAIN=go1.25.12 go test . -run TestSplitExpr` → `undefined: msgin.SplitExpr`.

- [ ] **Step 5: Implement `SplitExpr`** (`expr.go`):

```go
func SplitExpr[A, B any](expression string) (Step, error) {
	eval, err := compile[A](expression, exprAny)
	if err != nil {
		return nil, err
	}
	return func(next MessageHandler) MessageHandler {
		return HandlerFunc(func(ctx context.Context, msg Message[any]) error {
			in, err := PayloadOf[A](msg)
			if err != nil {
				return err
			}
			out, err := eval(in)
			if err != nil {
				return err
			}
			children, err := exprSliceToChildren[A, B](out, in)
			if err != nil {
				return err
			}
			return forwardSplit(ctx, next, msg, children)
		})
	}, nil
}

// exprSliceToChildren reflects over a SplitExpr result: it must be a slice/array;
// each element is asserted to B and wrapped via WithPayload(parent, elem) so the
// child inherits the parent's headers (Split then stamps sequence/id/correlation).
// A non-slice result or a non-B element is ErrExprResultType.
func exprSliceToChildren[A, B any](out any, parent Message[A]) ([]Message[B], error) {
	rv := reflect.ValueOf(out)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, fmt.Errorf("%w: SplitExpr result %T is not a slice", ErrExprResultType, out)
	}
	children := make([]Message[B], rv.Len())
	for i := 0; i < rv.Len(); i++ {
		elem := rv.Index(i).Interface()
		b, ok := elem.(B)
		if !ok {
			return nil, fmt.Errorf("%w: SplitExpr element %d %T is not %T", ErrExprResultType, i, elem, *new(B))
		}
		children[i] = WithPayload(parent, b)
	}
	return children, nil
}
```
Godoc on `SplitExpr`: mirror `FilterExpr`/`TransformExpr` trade-offs; note dual-param `[A,B]`, per-element assert to B, non-slice → `ErrExprResultType`, empty slice → forwards nothing, sequence-stamping identical to `Split`, struct-B ceiling.

- [ ] **Step 6: Run to verify it passes.** `GOTOOLCHAIN=go1.25.12 go test . -run 'TestSplitExpr|TestSplit' -race` → PASS.

**Hot-path branches:** empty/bad expr → `ErrInvalidExpression`; `PayloadOf` error; eval error; non-slice result → `ErrExprResultType`; non-B element → `ErrExprResultType`; empty slice → forwards nothing; N>0 → ordered fan-out + sequence headers stamped; mid-child error aborts remainder (inherited from `forwardSplit`, covered by `TestSplit`).

- [ ] **Step 7: Commit.**

```bash
git add splitter.go expr.go expr_test.go
git commit -m "$(cat <<'EOF'
feat(core): SplitExpr — runtime expr splitter (dual-param, per-element assert)

Extract Split's child-forward loop into a shared forwardSplit helper and add
SplitExpr[A,B]: evaluate an expression to a slice, assert each element to B,
WithPayload-wrap into children, and forward through the same sequence-stamping
path Split uses. Non-slice/non-B → ErrExprResultType.

Spec: 009
Plan: 018
ADR: 0019
EOF
)"
```

---

### Task 3: `refactor` — internal release strategy → `func(MessageGroup) (bool, error)` (D14c)

**Behavior-preserving.** The public `WithReleaseStrategy(func(MessageGroup) bool)` signature is UNCHANGED; only the internal `aggregatorConfig.release` field and its call sites move to `(bool, error)`. All existing aggregator/settlement tests MUST stay green — this task adds no new behavior, it prepares the seam `WithReleaseExpr` (Task 4) needs to propagate an eval error.

**Files:** Modify `aggregator.go`; add a regression case in `aggregator_test.go`.

- [ ] **Step 1: Note the current green baseline.** `GOTOOLCHAIN=go1.25.12 go test . -run 'Aggregator|Release|Completion|Reap' -race` → record PASS (the refactor must not change it).

- [ ] **Step 2: Evolve the release seam** (`aggregator.go`), via `gopls` where possible:
  - `aggregatorConfig.release` → `func(MessageGroup) (bool, error)`.
  - `defaultRelease` → returns `(…, nil)`.
  - `WithCompletionSize` → `c.release = func(g MessageGroup) (bool, error) { return len(g.Messages()) >= n, nil }`.
  - `WithReleaseStrategy(fn func(MessageGroup) bool)` (public signature unchanged) → stores `func(g MessageGroup) (bool, error) { return fn(g), nil }`.
  - `Handle` — a release-decision error propagates (Nack → retry/DLQ), giving the operator an immediate signal that a `WithReleaseExpr` is faulty; a persistently-unevaluable group is never stranded because the reaper's expiry fall-through (H-1 below) surfaces it to the visible expired sink:
    ```go
    ok, err := a.cfg.release(group)
    if err != nil {
        return err // release-decision error (e.g. WithReleaseExpr eval) → retry/DLQ;
                   // the reaper's expiry fall-through surfaces a stuck group (ADR 0019 A3).
    }
    if !ok {
        return nil // held; source Acks
    }
    ```
  - `release` (drain loop) — **once the main `releaseOnce` succeeds, the ENTIRE drain is best-effort and must NEVER return a non-nil error (audit H-2 + H-3).** By that point the main claimed set — which INCLUDES the `msg` Handle is processing — is aggregated, forwarded, and **settled (deleted)**. ANY non-nil return from here Nacks `msg` → the source redelivers it → idempotent `Add` re-inserts the just-deleted member as a fresh live member → it is re-mixed into a SECOND aggregate (a real double-count) and a successfully-processed message is DLQ'd. There are **three** post-settle exits and all three must swallow — the release-check error/incomplete (H-2), the residual `releaseOnce` failure (H-3), **and** a transient `ClaimGroup` error (the shipped `return err` at aggregator.go:353-354 is the same latent bug). Show the **COMPLETE** method:
    ```go
    func (a *Aggregator) release(ctx context.Context, claim MessageGroupClaim) error {
        if err := a.releaseOnce(ctx, claim); err != nil {
            return err // MAIN release failed → NOT settled (defer abandoned the claim) →
                       // Nack+redeliver is correct (idempotent Add re-groups, no double-count).
        }
        // Main group settled (incl. the current member). The drain below is a
        // best-effort optimization that claims/emits any already-complete residual
        // formed during the lease; it must NEVER return a non-nil error (H-2/H-3),
        // or it would Nack the already-settled member. Residuals it leaves are
        // recovered by the reaper (durable store) / a later Add.
        for {
            next, err := a.store.ClaimGroup(ctx, claim.Key())
            if err != nil || next == nil {
                return nil // no residual, leased by another, or a transient claim error → leave for reaper
            }
            ok, rerr := a.cfg.release(next)
            if rerr != nil || !ok {
                _ = a.store.AbandonGroup(ctx, next) // residual not evaluable / not complete; leave it live
                return nil
            }
            if err := a.releaseOnce(ctx, next); err != nil {
                return nil // residual re-release failed (its claim already abandoned by the defer) → reaper
            }
        }
    }
    ```
    (Note the `reapGroup` recovery call `_ = a.release(ctx, claim)` already discards this return, so the reaper path was always benign; the fix matters only on the `Handle` path.)
  - `reapGroup` — **audit H-1: on a release-check ERROR, fall THROUGH to the age-expiry branch** (do not early-return), so a deterministically-erroring group still reaches `WithExpiredGroupChannel` instead of busy-spinning claim→error→abandon every tick forever. **Show the COMPLETE post-refactor method** (audit M-2 — the expiry/abandon tail below is shipped behavior; the only change is the release check + the fall-through):
    ```go
    func (a *Aggregator) reapGroup(ctx context.Context, g MessageGroup, cutoff time.Time) {
        claim, err := a.store.ClaimGroup(ctx, g.Key())
        if err != nil || claim == nil {
            return // released/leased concurrently, or gone
        }
        ok, rerr := a.cfg.release(claim)
        if rerr == nil && ok {
            _ = a.release(ctx, claim) // RECOVERY: re-emit a crashed/complete group to OUTPUT (+ settle)
            return
        }
        // H-1: a release ERROR or a not-yet-complete group both fall through to
        // age-expiry so an unevaluable group is surfaced to the visible sink,
        // never stranded/busy-spinning.
        if !cutoff.IsZero() && claim.CreatedAt().Before(cutoff) {
            for _, m := range claim.Messages() {
                if sendErr := a.cfg.expired.Send(ctx, m); sendErr != nil {
                    _ = a.store.AbandonGroup(ctx, claim) // retry next tick rather than drop
                    return
                }
            }
            _ = a.store.SettleGroup(ctx, claim)
            return
        }
        _ = a.store.AbandonGroup(ctx, claim) // fresh residual re-formed since the Expired() scan, or not yet due
    }
    ```

- [ ] **Step 3: Add a focused regression test** — a `WithReleaseStrategy` whose bool wrapper still releases exactly as before (proves the wrap is transparent), folded into the existing release table. (The full existing suite is the primary regression proof.)

- [ ] **Step 4: Run the full aggregator suite green.** `GOTOOLCHAIN=go1.25.12 go test . -run 'Aggregator|Release|Completion|Reap|Settlement' -race` → PASS, identical to Step 1's baseline.

**Hot-path branches (unchanged behavior, re-verified):** release true → claim/aggregate/forward; false → held; `WithCompletionSize` count; `WithReleaseStrategy` custom; drain-loop residual complete/incomplete; reaper release-check true (recovery) / false (expiry/abandon). NEW error branches (exercised in Task 4): `Handle` release-err → `return err` (retry/DLQ); drain — **all post-settle exits swallow → `return nil`** (H-2/H-3: never Nack the settled member); reap release-err → **fall through to expiry** (H-1, never early-return).

- [ ] **Step 5: Commit.**

```bash
git add aggregator.go aggregator_test.go
git commit -m "$(cat <<'EOF'
refactor(core): internal release strategy returns (bool, error)

Evolve aggregatorConfig.release to func(MessageGroup) (bool, error) so a
release expression's eval error can propagate (Handle/reaper/drain). The
public WithReleaseStrategy(func(MessageGroup) bool) signature is unchanged
(wrapped to always-nil-error); defaultRelease/WithCompletionSize likewise.
Behavior-preserving — existing aggregator/settlement suite unchanged.

Spec: 009
Plan: 018
ADR: 0019
EOF
)"
```

---

### Task 4: `WithCorrelationExpr[A]` + `WithReleaseExpr[A]` (+ group env, option-error plumbing)

**Files:** Modify `expr.go` (surfaces + `exprMember`/`groupExprEnv`/`compileGroup`/`toGroupEnv`), `aggregator.go` (`optErr` field + `NewAggregator` surfaces it), `expr_test.go`, `aggregator_test.go`.

**Interfaces:**
- Consumes: `compile[A]` (`exprString`), `PayloadOf[A]`, `asInt`/`firstHeader`/`HeaderSequenceSize` (aggregator.go), `expr.Compile`/`vm.Run`, the `(bool,error)` release seam (Task 3).
- Produces: `func WithCorrelationExpr[A any](expression string) AggregatorOption`; `func WithReleaseExpr[A any](expression string) AggregatorOption`; unexported `exprMember[A]`, `groupExprEnv[A]`, `compileGroup[A]`, `toGroupEnv[A]`; `aggregatorConfig.optErr`.

- [ ] **Step 1: Add the `optErr` seam** (`aggregator.go`): a field `optErr error` on `aggregatorConfig`, and in `NewAggregator`, **after** the options loop and **before** the output/timeout checks:
```go
	if cfg.optErr != nil {
		return nil, cfg.optErr // e.g. ErrInvalidExpression from WithCorrelationExpr/WithReleaseExpr
	}
```

- [ ] **Step 2: Write the failing tests** (`expr_test.go` + `aggregator_test.go`). Cover:
  - `WithCorrelationExpr`: bad expr → `NewAggregator` returns `ErrInvalidExpression`; a good expr (`header("tenant")` or `payload.Region`) groups messages by the evaluated key (drive two messages with different keys → two groups); an eval error surfaces from `Handle`; **empty evaluated key → `Permanent(ErrNoCorrelation)`** (L-1 — an expr like `""` or a coalesced-empty header).
  - `WithReleaseExpr`: bad expr → `NewAggregator` returns `ErrInvalidExpression`; **`size > 0 && len(messages) >= size`** releases at the sequence size (Splitter round-trip — use the gated form per M-4, and add a case asserting the un-gated `len(messages) >= size` releases a singleton when no `HeaderSequenceSize` is present, documenting the footgun); a member-`header()` predicate (`messages[0].header("priority") == "high"`) releases; **an eval error propagates from `Handle`** (D14c seam); **first-expr-option-error-wins** (pass a bad `WithCorrelationExpr` and a bad `WithReleaseExpr` → the first-applied error is returned).
  - **Fake `MessageGroupStore` cases** (blackbox — the SPI is exported; a hand-rolled fake returns crafted groups a real store can't): **empty-group release check does NOT panic** (M-1); **non-A member → `ErrPayloadType`** from `toGroupEnv` (M-6).
  - **Reaper fall-through (H-1)** — an aggregator with `WithReleaseExpr` whose expr errors on a group's data + `WithGroupTimeout`/`WithExpiredGroupChannel`: after the group ages past the timeout, one `reap` tick (driven by a `clockwork.FakeClock`) routes the group's members to the **expired** channel (not busy-spin, not the output channel). Assert the expired sink receives the members and the group is settled.
  - **Drain swallow (H-2 + H-3)** — count-based recurring-key aggregation (`WithCompletionSize` or a size expr) where a residual forms during a lease: assert the main aggregate emits exactly once, the triggering member is Acked (no error from `Handle`), and it is NOT re-mixed into a second aggregate — across **both** post-settle drain exits: (H-2) the residual's release **check** errors/incomplete, and (H-3) the residual is complete but its **`releaseOnce` fails** (agg-fn or output.Send error via a fake output channel that errors on the second Send). Both must leave `Handle` returning nil. (Drive two `*Aggregator`s over one `memory.GroupStore`, or a controlled single-key feed with a fake erroring output channel.)

- [ ] **Step 3: Run to verify it fails.** `GOTOOLCHAIN=go1.25.12 go test . -run 'TestWithCorrelationExpr|TestWithReleaseExpr' ` → undefined symbols.

- [ ] **Step 4: Implement the group env + compile path** (`expr.go`):

```go
// exprMember is one group member as seen by a group-scoped expression: the typed
// payload and a header(key) accessor, matching exprEnv's single-message shape (a
// raw Message[A] is unusable here — its Header accessor returns (any, bool), which
// expr cannot call, and its fields are unexported).
type exprMember[A any] struct {
	Payload A                `expr:"payload"`
	Header  func(string) any `expr:"header"`
}

// groupExprEnv is the environment a WithReleaseExpr expression evaluates against:
// the members in arrival order and the group's declared size (HeaderSequenceSize,
// 0 if absent). E.g. len(messages) >= size, all(messages, .payload.Amount > 0).
type groupExprEnv[A any] struct {
	Messages []exprMember[A] `expr:"messages"`
	Size     int             `expr:"size"`
}

// compileGroup compiles a bool expression over groupExprEnv[A] once, mirroring
// compile but for the group-scoped env. Empty/unparseable/non-bool → ErrInvalidExpression.
func compileGroup[A any](expression string) (func(groupExprEnv[A]) (any, error), error) {
	if strings.TrimSpace(expression) == "" {
		return nil, fmt.Errorf("%w: empty expression", ErrInvalidExpression)
	}
	program, err := expr.Compile(expression, expr.Env(groupExprEnv[A]{}), expr.AsBool())
	if err != nil {
		return nil, fmt.Errorf("%w %q: %w", ErrInvalidExpression, expression, err)
	}
	return func(env groupExprEnv[A]) (any, error) { return vm.Run(program, env) }, nil
}

// toGroupEnv projects a MessageGroup into groupExprEnv[A]: each member's payload is
// re-asserted to A (a non-A member → ErrPayloadType) and its headers exposed via a
// per-member header() closure; size is the first member's HeaderSequenceSize.
func toGroupEnv[A any](g MessageGroup) (groupExprEnv[A], error) {
	msgs := g.Messages()
	if len(msgs) == 0 {
		return groupExprEnv[A]{}, nil // M-1: guard before firstHeader (mirrors defaultRelease's len==0 guard)
	}
	members := make([]exprMember[A], len(msgs))
	for i, m := range msgs {
		tm, err := PayloadOf[A](m)
		if err != nil {
			return groupExprEnv[A]{}, err // non-A member → ErrPayloadType (M-6: tested via a fake store)
		}
		member := m // Go 1.25 per-iteration var; bound explicitly for the closure's clarity
		members[i] = exprMember[A]{
			Payload: tm.Payload(),
			Header:  func(k string) any { v, _ := member.Header(k); return v },
		}
	}
	size, _ := asInt(firstHeader(msgs, HeaderSequenceSize)) // 0 if absent/non-numeric
	return groupExprEnv[A]{Messages: members, Size: size}, nil
}
```

- [ ] **Step 5: Implement the two options** (`expr.go`), compiling eagerly and reporting via `optErr`:

```go
func WithCorrelationExpr[A any](expression string) AggregatorOption {
	eval, err := compile[A](expression, exprString)
	return func(c *aggregatorConfig) {
		if err != nil {
			if c.optErr == nil {
				c.optErr = err
			}
			return
		}
		c.correlate = func(m Message[any]) (string, error) {
			in, perr := PayloadOf[A](m)
			if perr != nil {
				return "", perr
			}
			out, eerr := eval(in)
			if eerr != nil {
				return "", eerr // raw eval error (retried), consistent with FilterExpr (L-2)
			}
			key, _ := out.(string) // AsKind(String) guarantees a string
			if key == "" {
				return "", Permanent(ErrNoCorrelation) // L-1: symmetry with defaultCorrelate
			}
			return key, nil
		}
	}
}

func WithReleaseExpr[A any](expression string) AggregatorOption {
	eval, err := compileGroup[A](expression)
	return func(c *aggregatorConfig) {
		if err != nil {
			if c.optErr == nil {
				c.optErr = err
			}
			return
		}
		c.release = func(g MessageGroup) (bool, error) {
			env, berr := toGroupEnv[A](g)
			if berr != nil {
				return false, berr
			}
			out, eerr := eval(env)
			if eerr != nil {
				return false, eerr
			}
			b, _ := out.(bool) // AsBool guarantees a bool
			return b, nil
		}
	}
}
```
Godoc on both: the ADR 0019 §6 trade-off/security block; that a compile error surfaces from `NewAggregator` as `ErrInvalidExpression` (not from the option), **and takes precedence over `ErrNilOutput`/`ErrExpiryChannelRequired`** (L-5, deliberate — the expr fault is the more specific misconfiguration). For `WithReleaseExpr` additionally document:
  - the `{messages, size}` env shape (per-member `payload`/`header(k)`); `size` is `HeaderSequenceSize`, **0 if absent** — so the canonical release form is `size > 0 && len(messages) >= size`; the un-gated `len(messages) >= size` releases a group-of-one when no size header is present (M-4).
  - an eval error propagates through `Handle` (→ retry/DLQ); a **persistently-unevaluable** group is surfaced to `WithExpiredGroupChannel` by the reaper's expiry fall-through (requires `WithGroupTimeout`), never stranded.
  - **poison-member caveat (M-5):** unlike the O(1) size-header default, a release expr evaluates over **all** members, so one member whose data makes the expression error poisons the whole group's release check (all further members on that key retry→DLQ until expiry surfaces the group). Write **total** expressions (guard nil/typed headers), and prefer the Go-func `WithReleaseStrategy` for large groups — `WithReleaseExpr` is **O(n) per `Add`** (O(n²) over a group's lifetime) vs the default's O(1) (L-3).
  - **held-not-lost without a reaper (L-7):** with `memory.GroupStore` (`RecoverInterval()==0`) and **no** `WithGroupTimeout`, there is no reaper tick — a persistently-unevaluable group is **held (not lost)**, bounded by store capacity. Pair `WithReleaseExpr` with `WithGroupTimeout` to surface such a group to the visible expired sink.
  - **dual-surface (L-8):** with `WithGroupTimeout` set, a poison member may appear at **both** the DLQ (via `Handle` retry-exhaustion of the member that triggered the error) and the expired channel (the reaper routes the whole aged group there). No output double-emit, no loss — within the at-least-once contract.

- [ ] **Step 6: Run to verify it passes.** `GOTOOLCHAIN=go1.25.12 go test . -run 'TestWithCorrelationExpr|TestWithReleaseExpr|Aggregator' -race` → PASS.

**Hot-path branches:** `WithCorrelationExpr` compile-err→`optErr`→`ErrInvalidExpression`; eval→key; `PayloadOf` err; eval err. `WithReleaseExpr` compile-err→`optErr`→`ErrInvalidExpression`; `toGroupEnv` non-A member→`ErrPayloadType`; release true/false; member-`header()`+`size` access; eval err propagates (D14c). First-error-wins in `optErr`.

- [ ] **Step 7: Commit.**

```bash
git add expr.go aggregator.go expr_test.go aggregator_test.go
git commit -m "$(cat <<'EOF'
feat(core): WithCorrelationExpr / WithReleaseExpr aggregator options

Add correlation-by-expr (reuses the exprString compile) and release-by-expr
over a new group-scoped env {messages []{payload,header}, size int}. Expr
options compile eagerly and report a compile error through NewAggregator
(aggregatorConfig.optErr → ErrInvalidExpression); a release eval error
propagates via the (bool,error) release seam.

Spec: 009
Plan: 018
ADR: 0019
EOF
)"
```

---

### Task 5: Examples, package doc, whole-branch gate

**Files:** Modify `expr_test.go` (Examples), `doc_composition.go`.

- [ ] **Step 1: Add runnable `Example`s** with deterministic `// Output:` — `ExampleTransformExpr`, `ExampleSplitExpr`, and an `ExampleWithReleaseExpr` (or fold correlation/release into one aggregator example) driving messages synchronously through the `Step`/`Handle` to a recording handler (as Plan 014's `runStep`/Plan 016's aggregator examples do). Each MUST show the `x, err := msgin.…Expr[…](…)` → error-check → compose pattern (the `(Step,error)`/option-error friction is explicit in the godoc surface). **`ExampleWithReleaseExpr` MUST use the size-gated canonical form `size > 0 && len(messages) >= size`** (L-9 — never ship the un-gated singleton-releasing form as documentation).

- [ ] **Step 2: Package doc** — extend the `doc_composition.go` expr paragraph (added in Plan 014) to name `TransformExpr`/`SplitExpr`/`WithCorrelationExpr`/`WithReleaseExpr`, pointing at the Go-func `Transform`/`Split`/strategy forms as the compile-time default and noting the struct-B ceiling.

- [ ] **Step 3: Whole-branch delivery gate** (CLAUDE.md §5, over `main..HEAD`):
  - `GOTOOLCHAIN=go1.25.12 go build ./... && go test ./... -race` green; `CGO_ENABLED=0 go build ./...` ok.
  - `go vet ./...`, `golangci-lint run ./...`, `gofmt -l .` clean; `go mod tidy`/`go mod verify` **no diff, no new module** (the no-new-dep gate); `govulncheck ./...` clean.
  - `apidiff`/`gorelease` (or manual): the new exported surface (`TransformExpr`, `SplitExpr`, `WithCorrelationExpr`, `WithReleaseExpr`, `ErrExprResultType`) is **additive** and `WithReleaseStrategy` is unchanged → minor bump.
  - Run **`/code-review`** and **`/security-review`** over `main..HEAD`; the security review MUST reconsider the expr attack surface (untrusted-expression evaluation, now also over a group env — same expr defaults/no-ctx-cancellation caveat as ADR 0019 §6). Resolve/triage every finding; re-run `-race`.
  - Coverage: `go test . -cover` ≥85% on the core package; confirm every enumerated hot-path/eval-error branch is covered.
  - Update `docs/HANDOVER.md`; confirm cross-links + trailers on every commit.

- [ ] **Step 4: Commit** the examples/doc (design docs rode in Task 1).

```bash
git add expr_test.go doc_composition.go docs/HANDOVER.md
git commit -m "$(cat <<'EOF'
test(core): expr-sugar examples + package doc; Phase 4 gate

Runnable Example tests for TransformExpr/SplitExpr/WithReleaseExpr (godoc);
extend the composition package doc. Whole-branch gate green.

Spec: 009
Plan: 018
ADR: 0019
EOF
)"
```

---

## Self-review notes (coverage of Spec 009 §3.5)

- **D12 `TransformExpr[A,B]`** → Task 1 (`exprAny` + assert-to-B + `WithPayload` + `ErrExprResultType`). **D13 `SplitExpr[A,B]`** → Task 2 (shared `forwardSplit`, per-element assert, non-slice/non-B → `ErrExprResultType`; O9-4 resolved dual-param). **D14 correlation/release exprs** → Task 4; **aggregate-by-expr deferred** (§2). **D14a per-member view env** → Task 4 (`exprMember`/`groupExprEnv`, not raw `Message[A]`). **D14b option-error via constructor** → Task 4 (`optErr`, first-wins). **D14c internal release `(bool,error)`** → Task 3 (behavior-preserving refactor, public `WithReleaseStrategy` unchanged) consumed by Task 4.
- **Reuse win:** every new surface compiles-then-delegates to an existing seam (`Transform`, `Split`'s `forwardSplit`, the correlate/release strategy fields). Zero duplicated transform/split/aggregate runtime; the endpoints are pure `expr`-over-existing-endpoint adapters. No DB, no new dependency.
- **Sequencing rationale:** Task 3 (release `(bool,error)` refactor) lands **before** Task 4 (`WithReleaseExpr`) so the eval-error-propagation seam exists first and the refactor's regression is isolated in its own `refactor` commit (clean `main..HEAD` diff, focused gate).

## Adversarial audit fold (round 1, 2026-07-21, Opus — verdict NEEDS-REVISION → folded)

The spike (isolated module, expr v1.17.8) retired the expr-viability risk: `exprAny` compile-with-no-output, the group env (`len(messages) >= size`, `all(messages, .payload.X)`, `messages[0].header("k")`), reflect-over-slice, and `exprString` correlation all Verified-True. Findings folded:

- **H-1 (reapGroup strands an erroring group):** on a release-check error `reapGroup` must **fall through to age-expiry**, not early-return — else a deterministically-erroring group busy-spins and never reaches `WithExpiredGroupChannel`. Fixed in Task 3 Step 2 (full method shown). Tested in Task 4 (an erroring `WithReleaseExpr` group ages to the expired sink).
- **H-2 (drain re-delivers a settled member):** the drain loop **swallows** a residual release error/incompleteness (`abandon + return nil`), never `return err` — the main group already settled the current `msg`, so a Nack would re-mix it into a second aggregate + DLQ a good message. Fixed in Task 3 Step 2.
- **M-1 (`toGroupEnv` empty-group panic):** guard `len(msgs)==0` before `firstHeader` (mirrors `defaultRelease`). Fixed in Task 4 Step 4; tested via a fake `MessageGroupStore` returning an empty claim.
- **M-2 (plan showed a truncated `reapGroup`):** the complete post-refactor method is now in Task 3 Step 2.
- **M-3 (numeric-B ceiling):** expr yields Go `int` for integer math, `float64` for float math; `out.(B)` is exact, so B must be exactly `int`/`float64`/`string`/`bool`/a concrete element type — not `int64`/`uint`/`float32`/etc. Documented on `TransformExpr`/`SplitExpr` godoc + spec D12/D13; a `B=int64`-with-int-expr → `ErrExprResultType` case added to Task 1.
- **M-4 (singleton-release footgun):** `size` is 0 when `HeaderSequenceSize` is absent, so `len(messages) >= size` releases a group-of-one — opposite to `defaultRelease` (which gates on presence). The canonical example is now `size > 0 && len(messages) >= size` everywhere (spec D14a, ADR A2, godoc, Example).
- **M-5 (poison member):** a `WithReleaseExpr` evaluates over ALL members, so one member whose data makes the expr error poisons the group's release check. Documented as a caveat (write total expressions); the group is not lost — the reaper's expiry fall-through (H-1) surfaces it to the visible sink (requires `WithGroupTimeout`). Recorded in ADR 0019 A3.
- **M-6 (untested `ErrPayloadType` branch):** `toGroupEnv`'s non-A-member branch is a required typed-error test; covered in Task 4 via a fake `MessageGroupStore` yielding a wrong-typed member (also covers M-1's empty case).
- **L-1 (empty correlation key):** `WithCorrelationExpr` maps an empty evaluated key to `Permanent(ErrNoCorrelation)`, matching `defaultCorrelate`. Fixed in Task 4 Step 5 + tested.
- **L-2 (eval errors not `Permanent`):** correlation/release **eval** errors propagate raw (retried), consistent with the shipped `FilterExpr` stance; only the empty-key case is `Permanent` (L-1). Documented.
- **L-3 (O(n²) release):** `WithReleaseExpr` re-projects+re-evaluates all members per `Add`; documented as a trade-off vs the O(1) size-header default on the godoc.
- **L-4 (construction-error coverage):** Task 1 adds a non-empty unparseable-expr case (the `expr.Compile`-failure branch, distinct from the empty-string guard).
- **L-5 (`optErr` precedence):** `NewAggregator` surfaces `optErr` (`ErrInvalidExpression`) before `ErrNilOutput`/`ErrExpiryChannelRequired`; documented as the deliberate ordering.
- **SOUND (no change):** SplitExpr↔Split header/sequence parity; `exprAny` after `exprString` at iota; `toGroupEnv` closure capture; `*new(B)` safe for pointer/interface B; `reflect.ValueOf(nil).Kind()==Invalid`→`ErrExprResultType`; additive SemVer; Task-3-before-4 sequencing.

### Round 2 (2026-07-21, Opus — verdict NEEDS-REVISION → folded; then SOUND-WITH-NITS)

Round 2 verified every round-1 fold as correct/coherent and found one High + nits:
- **H-3 (drain fix was incomplete):** the H-2 fold fixed only the drain loop's release-**check** exit; the loop's **`releaseOnce`-failure** exit (`aggregator.go:360`) and its **`ClaimGroup`-error** exit (`:353`) still `return err`, re-Nacking the already-settled member (same double-count). Fixed in Task 3 Step 2 by making the **entire** post-settle drain best-effort — **all three exits `return nil`** (only the top-level main `releaseOnce` failure still propagates, correctly, since the main group is not settled then). Complete method shown. H-3 test added to Task 4 (residual `releaseOnce` fails via a fake erroring output channel). Note this was latent Phase-2/3 code, fixed now because Task 3 edits the exact block and it violates the same invariant.
- **L-6:** Task 1 test used `order`; the real helper is `exprOrder` — corrected throughout (would have broken the first `go build`).
- **L-7:** documented that without `WithGroupTimeout` a persistently-unevaluable group is **held (not lost)**, bounded by store capacity (Task 4 godoc).
- **L-8:** documented a poison member may surface at **both** DLQ and the expired channel (no output double-emit, no loss) (Task 4 godoc).
- **L-9:** `ExampleWithReleaseExpr` pinned to the size-gated canonical form (Task 5).
- Round 2 verified CORRECTLY-FOLDED: H-1, H-2 (release-check exit), M-1, M-4, M-6, L-1, L-2, L-5, the D14c refactor equivalence (each edited call site reduces to the shipped boolean for nil-error strategies), SplitExpr parity, exprAny/numeric ceiling. **Post-fold verdict: SOUND-WITH-NITS** (nits folded).
