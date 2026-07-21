# Splitter (Phase 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Go skills are mandatory (CLAUDE.md writing-plans override):** every task starts from **`cc-skills-golang:golang-how-to`** (which pulls in the applicable `golang-*` skills), uses **`superpowers:test-driven-development`** (red→green→refactor), navigates/refactors via **`gopls`** (the `LSP` tool), and obeys the project test overrides — **`table-test`** (assert-closure tables, `t.Context()`), **`use-mockgen`**, **`use-testcontainers`**. None of Phase 1 needs mocks or containers (pure in-process, stdlib).

**Goal:** Ship the EIP **Splitter** endpoint — `Split[A,B]` — a stateless `Step` that fans one message into N children, forwards each downstream in order, and stamps the reserved sequence headers a downstream Aggregator reassembles by.

**Architecture:** A generic free-function constructor returning a `Step` (the ADR 0013 middleware idiom), exactly like `Transform`/`Filter`. It asserts the payload to `A` (`ErrPayloadType`), calls the user `fn` for `[]Message[B]`, stamps `HeaderSequenceNumber`/`HeaderSequenceSize` + a correlation-id fallback on each child, and forwards each to `next` on the delivery goroutine (synchronous-direct → the driving `Consumer` Acks only after every child succeeds, at-least-once inherited from `Chain`). No new dependency, no core runtime change, no new error sentinel.

**Tech Stack:** Go 1.25, stdlib only. Root module `github.com/kartaladev/msgin`, root `package msgin`. Tests: blackbox `package msgin_test`, `stretchr/testify` (assert/require), assert-closure tables.

## Global Constraints

- **Go 1.25 pinned:** `go 1.25` directive; build/test with `GOTOOLCHAIN=go1.25.12` (local default is newer). No language/stdlib features past 1.25.
- **Module path:** `github.com/kartaladev/msgin`; code in the **root `package msgin`** (new file `splitter.go`).
- **No new dependency.** Core stays on its current dep set; Phase 1 is stdlib-only. `go mod tidy` must leave `go.mod`/`go.sum` unchanged.
- **Blackbox tests only:** every `_test.go` is `package msgin_test`, exercising only the exported API. Assert-closure tables (never `want`/`wantErr` fields); `t.Context()` over `context.Background()`. Example tests are `_test`-package too.
- **Debuggability / no panics on caller input:** a nil `fn` returns `ErrNilFunc` at dispatch (no panic); typed errors name the offending input.
- **Hot-path branch coverage (mandatory):** every branch listed in each task's tests must be exercised; target ≥85% statement coverage on the root package (it is already high — do not regress).
- **Gates before the final task's commit:** `go build ./...`, `go test ./... -race`, `go vet ./...`, `golangci-lint run ./...`, `gofmt -l .` (empty), `CGO_ENABLED=0 go build ./...`, `govulncheck ./...` all clean.
- **Commit trailers** on every commit: `Spec: 009`, `Plan: 015`, `ADR: 0020`.
- **Traceability:** governing [Spec 009](../specs/009-splitter-aggregator-endpoints.md) §3.1 (D1–D4); [ADR 0020](../adrs/0020-splitter-aggregator-group-store.md) §1; builds on [ADR 0013](../adrs/0013-composition-endpoints.md).

---

## File Structure

- **Create `splitter.go`** (root `package msgin`) — `Split[A,B]` + the private `stampSequence` helper.
- **Modify `message.go`** — add two reserved header constants (`HeaderSequenceNumber`, `HeaderSequenceSize`) beside the existing `Header*` block.
- **Create `splitter_test.go`** (`package msgin_test`) — the table tests.
- **Create `example_splitter_test.go`** (`package msgin_test`) — the runnable `Example` (godoc).

Existing symbols reused (do **not** redefine): `Step`, `MessageHandler`, `HandlerFunc`, `PayloadOf[T]`, `WithPayload`, `boxMessage`, `nilFuncStep`, `HeaderCorrelationID`, `Message.WithHeader`, `Message.Header`, `Message.ID`, `ErrNilFunc`, `ErrPayloadType`.

---

## Task 1: Sequence-header constants + `Split` core forwarding

Deliver `Split[A,B]` that asserts the payload, calls `fn`, and forwards each child to `next` **in order** — plus the two reserved header constants. Sequence-header *stamping* lands in Task 2; this task forwards the children **unstamped** so the forwarding/ordering/nil-fn/empty branches are isolated and independently testable.

**Files:**
- Modify: `message.go` (add two constants in the reserved-header `const` block, currently `message.go:14-20`)
- Create: `splitter.go`
- Test: `splitter_test.go`

**Interfaces:**
- Consumes: `Step`, `MessageHandler`, `HandlerFunc`, `PayloadOf[A]`, `boxMessage`, `nilFuncStep`, `ErrNilFunc`, `ErrPayloadType`.
- Produces: `func Split[A, B any](fn func(ctx context.Context, m Message[A]) ([]Message[B], error)) Step`; constants `HeaderSequenceNumber = "msgin.sequence-number"`, `HeaderSequenceSize = "msgin.sequence-size"`.

- [ ] **Step 1: Write the failing test**

Create `splitter_test.go`:

```go
package msgin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collect returns a terminal MessageHandler that appends every handled message
// to *out, plus the handler. It is the Split children's downstream.
func collect(out *[]msgin.Message[any]) msgin.MessageHandler {
	return msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		*out = append(*out, m)
		return nil
	})
}

func TestSplit_Forwarding(t *testing.T) {
	errBoom := errors.New("boom")

	tests := []struct {
		name   string
		fn     func(ctx context.Context, m msgin.Message[int]) ([]msgin.Message[int], error)
		in     msgin.Message[any]
		assert func(t *testing.T, forwarded []msgin.Message[any], err error)
	}{
		{
			name: "fans one message into three children in order",
			fn: func(_ context.Context, m msgin.Message[int]) ([]msgin.Message[int], error) {
				p := m.Payload()
				return []msgin.Message[int]{msgin.New(p), msgin.New(p + 1), msgin.New(p + 2)}, nil
			},
			in: boxInt(10),
			assert: func(t *testing.T, forwarded []msgin.Message[any], err error) {
				require.NoError(t, err)
				require.Len(t, forwarded, 3)
				assert.Equal(t, []any{10, 11, 12}, []any{
					forwarded[0].Payload(), forwarded[1].Payload(), forwarded[2].Payload(),
				})
			},
		},
		{
			name: "empty split forwards nothing and returns nil",
			fn: func(_ context.Context, _ msgin.Message[int]) ([]msgin.Message[int], error) {
				return nil, nil
			},
			in: boxInt(1),
			assert: func(t *testing.T, forwarded []msgin.Message[any], err error) {
				require.NoError(t, err)
				assert.Empty(t, forwarded)
			},
		},
		{
			name: "fn error propagates and forwards nothing",
			fn: func(_ context.Context, _ msgin.Message[int]) ([]msgin.Message[int], error) {
				return nil, errBoom
			},
			in: boxInt(1),
			assert: func(t *testing.T, forwarded []msgin.Message[any], err error) {
				assert.ErrorIs(t, err, errBoom)
				assert.Empty(t, forwarded)
			},
		},
		{
			name: "wrong payload type yields ErrPayloadType",
			fn: func(_ context.Context, _ msgin.Message[int]) ([]msgin.Message[int], error) {
				return []msgin.Message[int]{msgin.New(1)}, nil
			},
			in: msgin.New[any]("not-an-int"),
			assert: func(t *testing.T, forwarded []msgin.Message[any], err error) {
				assert.ErrorIs(t, err, msgin.ErrPayloadType)
				assert.Empty(t, forwarded)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var forwarded []msgin.Message[any]
			h := msgin.Split(tc.fn)(collect(&forwarded))
			err := h.Handle(t.Context(), tc.in)
			tc.assert(t, forwarded, err)
		})
	}
}

func TestSplit_NilFunc(t *testing.T) {
	h := msgin.Split[int, int](nil)(collect(new([]msgin.Message[any])))
	assert.ErrorIs(t, h.Handle(t.Context(), boxInt(1)), msgin.ErrNilFunc)
}

// boxInt wraps v as a Message[any] with an int payload.
func boxInt(v int) msgin.Message[any] { return msgin.New[any](v) }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.12 go test ./... -run 'TestSplit' -v`
Expected: FAIL — `undefined: msgin.Split` (and `undefined: msgin.HeaderSequenceNumber` if referenced; here Task 1 tests don't yet assert headers).

- [ ] **Step 3: Add the reserved header constants**

In `message.go`, extend the reserved-header `const` block (after `HeaderDeliveryCount`):

```go
	HeaderDeliveryCount = "msgin.delivery-count"
	// HeaderSequenceNumber is the 1-based position of a child within a Splitter's
	// output (int). HeaderSequenceSize is the total number of children (int). A
	// Splitter stamps both so a downstream Aggregator can reassemble the group.
	HeaderSequenceNumber = "msgin.sequence-number"
	HeaderSequenceSize   = "msgin.sequence-size"
```

- [ ] **Step 4: Write the minimal `Split` implementation**

Create `splitter.go`:

```go
package msgin

import "context"

// Split is a Splitter endpoint (EIP): it asserts the input payload to A, calls
// fn to produce N child messages, and forwards each downstream IN ORDER. A
// non-A payload yields ErrPayloadType (routed to the invalid-message channel);
// an fn error propagates without forwarding; a nil fn yields ErrNilFunc (no
// panic on caller input). An empty/nil result forwards nothing and returns nil
// (a valid "nothing to split", like a Filter drop).
//
// (Reassembly headers — sequence number/size, a deterministic child id, and a
// correlation-id fallback — are stamped on each child in the next commit.)
//
// Settlement: all N children forward on the delivery goroutine before Handle
// returns, so a Consumer driving the flow Acks the source only after every
// child succeeds (end-to-end at-least-once, exactly like Chain). A child error
// aborts the remaining children and propagates, so the whole parent is
// redelivered — children must be idempotent downstream.
func Split[A, B any](fn func(ctx context.Context, m Message[A]) ([]Message[B], error)) Step {
	if fn == nil {
		return nilFuncStep()
	}
	return func(next MessageHandler) MessageHandler {
		return HandlerFunc(func(ctx context.Context, msg Message[any]) error {
			in, err := PayloadOf[A](msg)
			if err != nil {
				return err
			}
			children, err := fn(ctx, in)
			if err != nil {
				return err
			}
			for _, child := range children {
				if err := next.Handle(ctx, boxMessage(child)); err != nil {
					return err
				}
			}
			return nil
		})
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOTOOLCHAIN=go1.25.12 go test ./... -run 'TestSplit' -v`
Expected: PASS (all subtests of `TestSplit_Forwarding` + `TestSplit_NilFunc`).

- [ ] **Step 6: Commit**

```bash
git add message.go splitter.go splitter_test.go
git commit -m "$(printf 'feat(core): Split — Splitter endpoint core forwarding\n\nAsserts payload->A, calls fn, forwards N children in order; nil fn ->\nErrNilFunc, bad payload -> ErrPayloadType, empty -> nothing. Adds the\nreserved HeaderSequenceNumber/HeaderSequenceSize keys (stamping in the\nnext task).\n\nSpec: 009\nPlan: 015\nADR: 0020')"
```

---

## Task 2: Sequence-header + child-identity stamping

Stamp `HeaderSequenceNumber`/`HeaderSequenceSize` on every child, assign each child a **deterministic-yet-unique id** derived from the parent (`parentID#seq`), and set `HeaderCorrelationID` to the parent's id **only when the child has none**.

**Why child-id stamping is here (audit H-1):** the canonical way to build a child is `WithPayload(parent, x)`, which copies the parent's headers **verbatim including `HeaderMessageID`** — so *all N children would share the parent's id*. The Phase-2 Aggregator dedups group members by `msg.ID()`, so identical child ids would collapse the group to one member and it would **never release** (silent data loss). Random ids (`New`) would instead break idempotency across a source redelivery. The fix — a deterministic id `parentID + "#" + seq` — is unique within one split (the group fills to N) *and* reproducible across a redelivery of the same parent (the Aggregator's idempotent `Add` recognizes the members). This identity contract is born in the Splitter, so it is fixed here in Phase 1.

**Files:**
- Modify: `splitter.go` (add `stampSequence`; import `strconv`; call it in the forward loop)
- Test: `splitter_test.go` (add `TestSplit_SequenceHeaders`)

**Interfaces:**
- Consumes: `Message.WithHeader`, `Message.Header`, `Message.ID`, `HeaderMessageID`, `HeaderCorrelationID`, `HeaderSequenceNumber`, `HeaderSequenceSize`, `WithPayload`.
- Produces: private `func stampSequence[B any](child Message[B], parent Message[any], num, size int) Message[B]` (implementation detail; not exported) — stamps sequence number/size, a deterministic child `HeaderMessageID`, and the correlation-id fallback.

- [ ] **Step 1: Write the failing test**

Add to `splitter_test.go` (note the canonical cases build children via `WithPayload`, the documented path — audit M-2):

```go
func TestSplit_SequenceHeaders(t *testing.T) {
	tests := []struct {
		name   string
		fn     func(ctx context.Context, m msgin.Message[int]) ([]msgin.Message[int], error)
		parent msgin.Message[any]
		assert func(t *testing.T, children []msgin.Message[any])
	}{
		{
			name: "WithPayload children: distinct deterministic ids, parent-id correlation, 1-based seq",
			// The documented construction path: WithPayload copies the parent's
			// headers (incl. HeaderMessageID), so Split MUST overwrite each child id.
			fn: func(_ context.Context, m msgin.Message[int]) ([]msgin.Message[int], error) {
				return []msgin.Message[int]{msgin.WithPayload(m, 1), msgin.WithPayload(m, 2)}, nil
			},
			parent: msgin.New[any](0, msgin.WithID("parent-123")),
			assert: func(t *testing.T, children []msgin.Message[any]) {
				require.Len(t, children, 2)
				assert.Equal(t, "parent-123#1", children[0].ID())
				assert.Equal(t, "parent-123#2", children[1].ID()) // distinct → group fills to N
				for i, c := range children {
					num, _ := c.Header(msgin.HeaderSequenceNumber)
					size, _ := c.Header(msgin.HeaderSequenceSize)
					corr, _ := c.Header(msgin.HeaderCorrelationID)
					assert.Equal(t, i+1, num)
					assert.Equal(t, 2, size)
					assert.Equal(t, "parent-123", corr)
				}
			},
		},
		{
			name: "re-split of the same parent yields identical child ids (idempotent)",
			fn: func(_ context.Context, m msgin.Message[int]) ([]msgin.Message[int], error) {
				return []msgin.Message[int]{msgin.WithPayload(m, 1), msgin.WithPayload(m, 2)}, nil
			},
			parent: msgin.New[any](0, msgin.WithID("parent-123")),
			assert: func(t *testing.T, children []msgin.Message[any]) {
				// stable, derived only from parent id + seq — see the extra re-run below.
				assert.Equal(t, "parent-123#1", children[0].ID())
				assert.Equal(t, "parent-123#2", children[1].ID())
			},
		},
		{
			name: "child with its own correlation id keeps it (inherited/nested case)",
			fn: func(_ context.Context, m msgin.Message[int]) ([]msgin.Message[int], error) {
				child := msgin.WithPayload(m, 1).WithHeader(msgin.HeaderCorrelationID, "child-own")
				return []msgin.Message[int]{child}, nil
			},
			parent: msgin.New[any](0, msgin.WithID("parent-123")),
			assert: func(t *testing.T, children []msgin.Message[any]) {
				require.Len(t, children, 1)
				corr, _ := children[0].Header(msgin.HeaderCorrelationID)
				assert.Equal(t, "child-own", corr)             // correlation NOT overwritten
				assert.Equal(t, "parent-123#1", children[0].ID()) // id still deterministic
			},
		},
		{
			name: "id-less parent: no deterministic id, no correlation stamped",
			// Covers the parent.ID()=="" hot-path branch (audit M-2).
			fn: func(_ context.Context, m msgin.Message[int]) ([]msgin.Message[int], error) {
				return []msgin.Message[int]{msgin.WithPayload(m, 1)}, nil
			},
			parent: msgin.NewMessage[any](0, msgin.NewHeaders(nil)), // ID()==""
			assert: func(t *testing.T, children []msgin.Message[any]) {
				require.Len(t, children, 1)
				assert.Equal(t, "", children[0].ID()) // no parent id → nothing derived
				_, hasCorr := children[0].Header(msgin.HeaderCorrelationID)
				assert.False(t, hasCorr) // no fallback correlation stamped
				num, _ := children[0].Header(msgin.HeaderSequenceNumber)
				assert.Equal(t, 1, num) // sequence headers still stamped
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var forwarded []msgin.Message[any]
			h := msgin.Split(tc.fn)(collect(&forwarded))
			require.NoError(t, h.Handle(t.Context(), tc.parent))
			tc.assert(t, forwarded)
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.12 go test ./... -run 'TestSplit_SequenceHeaders' -v`
Expected: FAIL — Task 1 forwards children unstamped, so the child `ID()` is still the parent's id (not `parent-123#1`), sequence headers are absent (`num`/`size` nil), and the correlation/id assertions fail.

- [ ] **Step 3: Add `stampSequence` and call it**

First update the import in `splitter.go` from `import "context"` to:

```go
import (
	"context"
	"strconv"
)
```

Update the `Split` godoc — replace the placeholder line

```go
// (Reassembly headers — sequence number/size, a deterministic child id, and a
// correlation-id fallback — are stamped on each child in the next commit.)
```

with the now-shipped behavior:

```go
// Each child is stamped for reassembly by a downstream Aggregator:
// HeaderSequenceNumber (1-based) and HeaderSequenceSize (N); a deterministic
// child id (HeaderMessageID = parentID#seq — unique within the split yet stable across
// a redelivery of the same parent, so the Aggregator's id-dedup holds); and
// HeaderCorrelationID set to the parent's id UNLESS the child already carries a
// correlation id (a caller-set/inherited one is preserved). With these, a
// Splitter->Aggregator round-trip reassembles with no extra configuration.
```

Then add the helper:

```go
// stampSequence returns child stamped for reassembly by a downstream Aggregator:
//   - HeaderSequenceNumber (1-based num) and HeaderSequenceSize (total).
//   - A deterministic child HeaderMessageID = parentID#num (only when the parent has an
//     id). It is unique within one split (so the group fills to size) AND stable
//     across a redelivery of the same parent (so the Aggregator's id-dedup
//     upholds at-least-once). This overwrites the id WithPayload copied from the
//     parent — children built via WithPayload would otherwise all share it.
//   - HeaderCorrelationID = the parent's id, but ONLY if the child carries no
//     correlation id (a caller-set / inherited correlation is preserved, so
//     nested split/aggregate keeps its outer group key).
//
// With an id-less parent (ID()==""), no id/correlation is derived — sequence
// headers are still stamped; such a split is not redelivery-idempotent (rare:
// source-delivered messages carry an id).
func stampSequence[B any](child Message[B], parent Message[any], num, size int) Message[B] {
	out := child.WithHeader(HeaderSequenceNumber, num).WithHeader(HeaderSequenceSize, size)
	pid := parent.ID()
	if pid != "" {
		out = out.WithHeader(HeaderMessageID, pid+"#"+strconv.Itoa(num))
		if _, ok := out.Header(HeaderCorrelationID); !ok {
			out = out.WithHeader(HeaderCorrelationID, pid)
		}
	}
	return out
}
```

Replace the forward loop body to stamp before boxing:

```go
			n := len(children)
			for i, child := range children {
				stamped := stampSequence(child, msg, i+1, n)
				if err := next.Handle(ctx, boxMessage(stamped)); err != nil {
					return err
				}
			}
			return nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOTOOLCHAIN=go1.25.12 go test ./... -run 'TestSplit' -v`
Expected: PASS (`TestSplit_Forwarding`, `TestSplit_NilFunc`, `TestSplit_SequenceHeaders`).

- [ ] **Step 5: Commit**

```bash
git add splitter.go splitter_test.go
git commit -m "$(printf 'feat(core): Split stamps EIP sequence headers on children\n\nStamps HeaderSequenceNumber (1-based) + HeaderSequenceSize on each\nchild, and HeaderCorrelationID = parent id only when the child has no\ncorrelation id (never overwrites a caller-set one).\n\nSpec: 009\nPlan: 015\nADR: 0020')"
```

---

## Task 3: Mid-child error propagation, `Example`, and gate

Prove a mid-child error aborts the remaining children (settlement semantics), add the runnable `Example` (godoc), and run the full pre-commit gate.

**Files:**
- Test: `splitter_test.go` (add `TestSplit_MidChildError`)
- Create: `example_splitter_test.go`

**Interfaces:**
- Consumes: `Split`, `Chain`, `To`, all Task 1/2 symbols.
- Produces: none (tests + example only).

- [ ] **Step 1: Write the failing mid-child-error test**

Add to `splitter_test.go`:

```go
func TestSplit_MidChildError(t *testing.T) {
	errStop := errors.New("stop at second")

	// next fails on the 2nd child; the 3rd must never be forwarded.
	var seen []int
	failing := msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		v, _ := m.Payload().(int)
		seen = append(seen, v)
		if len(seen) == 2 {
			return errStop
		}
		return nil
	})

	fn := func(_ context.Context, _ msgin.Message[int]) ([]msgin.Message[int], error) {
		return []msgin.Message[int]{msgin.New(1), msgin.New(2), msgin.New(3)}, nil
	}

	err := msgin.Split(fn)(failing).Handle(t.Context(), boxInt(0))
	assert.ErrorIs(t, err, errStop)
	assert.Equal(t, []int{1, 2}, seen) // 3rd child never sent
}
```

- [ ] **Step 2: Run the test**

Run: `GOTOOLCHAIN=go1.25.12 go test ./... -run 'TestSplit_MidChildError' -v`
Expected: PASS. This is a **characterization / regression lock** on the settlement contract (abort-remaining-children-on-error), not a red→green cycle — that branch was already implemented in Task 1's forward loop (`if err := next.Handle(...); err != nil { return err }`). Its value is pinning the behavior so a future refactor can't silently forward past a failed child. (Do not fake a red by mutating the assertion.)

- [ ] **Step 3: Write the `Example`**

Create `example_splitter_test.go`:

```go
package msgin_test

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
)

// ExampleSplit fans a batch into its items, printing each item with its 1-based
// sequence position out of the total — the headers a downstream Aggregator uses
// to reassemble the group.
func ExampleSplit() {
	type batch struct{ items []string }

	split := msgin.Split(func(_ context.Context, m msgin.Message[batch]) ([]msgin.Message[string], error) {
		var out []msgin.Message[string]
		for _, it := range m.Payload().items {
			out = append(out, msgin.WithPayload(m, it))
		}
		return out, nil
	})

	print := msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		num, _ := m.Header(msgin.HeaderSequenceNumber)
		size, _ := m.Header(msgin.HeaderSequenceSize)
		fmt.Printf("item %v/%v: %v\n", num, size, m.Payload())
		return nil
	})

	// A Splitter is a Step: split(next) yields the handler wired to next.
	h := split(print)
	_ = h.Handle(context.Background(), msgin.New[any](batch{items: []string{"a", "b", "c"}}))

	// Output:
	// item 1/3: a
	// item 2/3: b
	// item 3/3: c
}
```

- [ ] **Step 4: Run the example + full package tests**

Run: `GOTOOLCHAIN=go1.25.12 go test ./... -run 'Example|TestSplit' -v`
Expected: PASS, including `ExampleSplit` matching its `// Output:` block.

- [ ] **Step 5: Coverage check (hot-path branches)**

Run: `GOTOOLCHAIN=go1.25.12 go test . -run 'TestSplit|ExampleSplit' -coverprofile=/tmp/split.out && GOTOOLCHAIN=go1.25.12 go tool cover -func=/tmp/split.out | grep -E 'splitter.go'`
Expected: `Split` and `stampSequence` at/near 100% — every branch (nil-fn, payload-type, fn-error, empty, forward, mid-error, correlation present/absent) covered.

- [ ] **Step 6: Full pre-commit gate**

Run each; all must be clean:
```bash
GOTOOLCHAIN=go1.25.12 go build ./...
GOTOOLCHAIN=go1.25.12 go test ./... -race
GOTOOLCHAIN=go1.25.12 go vet ./...
golangci-lint run ./...
gofmt -l .
CGO_ENABLED=0 GOTOOLCHAIN=go1.25.12 go build ./...
GOTOOLCHAIN=go1.25.12 go mod tidy && git diff --exit-code go.mod go.sum
govulncheck ./...
```
Expected: build OK, `-race` green, vet/lint clean, `gofmt -l` prints nothing, CGO build OK, `go.mod`/`go.sum` unchanged, govulncheck clean.

- [ ] **Step 7: Commit**

```bash
git add splitter_test.go example_splitter_test.go
git commit -m "$(printf 'test(core): Split mid-child error propagation + ExampleSplit\n\nProves a downstream error aborts the remaining children (settlement),\nadds the runnable godoc example, and confirms the pre-commit gate\n(-race, vet, lint, gofmt, CGO, tidy, govulncheck) is clean.\n\nSpec: 009\nPlan: 015\nADR: 0020')"
```

---

## Audit fold (round 1 → folded; round 2 → SOUND-WITH-NITS, L-1 folded)

**Round 2 verdict: SOUND-WITH-NITS — implement.** The re-audit (Opus) verified every round-1 fix against the real `message.go`/`payload.go`/`framing.go`/`consumer.go`: the `parentID#seq` id scheme is provably collision-free (injective — the final segment is always a bare decimal), overwrites the `WithPayload`-inherited id as intended, keeps the round-trip idempotent across redelivery, and disturbs nothing in the Consumer's id-keyed retry/limiter/breaker/tracker path (children never re-enter the delivery loop). The only nit — **L-1**: Task 1's committed godoc over-promised stamping that lands in Task 2 — is folded (Task 1 godoc trimmed to a forward-reference; Task 2 restores the full godoc). No round-3 needed.

Round-1 adversarial audit (Opus, verified against the real API + sql framing) found the compile/Example mechanics sound but flagged identity/durability contracts. Folded here:
- **H-1 (child identity)** → Task 2 now stamps a deterministic child `HeaderMessageID = parentID#seq` (unique within a split so the group fills; stable across redelivery so the Aggregator's id-dedup holds). Without it, `WithPayload` children all share the parent id and the group never releases (silent loss). Tests build children via `WithPayload` (the documented path) + an idempotent re-split case.
- **M-1 (int headers on sql)** → recorded as a cross-phase contract in ADR 0020 §2/§5 and Spec D2/D8: the default release reads `HeaderSequenceSize` number-tolerantly and `sql.DecodeHeaders` restores the two sequence headers to `int` (Phase 3/ADR 0021). No Phase-1 code change.
- **M-2 (coverage + wrong test path)** → `TestSplit_SequenceHeaders` now uses `WithPayload`, covers the id-less-parent branch (`parent.ID()==""`), and asserts idempotent re-split ids.
- **L-1** Example dead `Chain(split)` scaffold removed. **L-2** mid-child test relabeled a characterization lock (no fake-red). **L-3** inherited-correlation semantics documented in the godoc + covered by a test case.

## Self-Review

**Spec coverage (Spec 009 §3.1 / ADR 0020 §1):**
- D1 shape `Split[A,B]` returning `Step` (option-free, YAGNI) → Task 1. ✅
- D2 sequence-header + child-identity convention (`HeaderSequenceNumber`/`HeaderSequenceSize`, deterministic child id, correlation fallback that never overwrites) → Task 2. ✅
- D3 empty split forwards nothing → Task 1 case. ✅
- D4 settlement / at-least-once + mid-child abort → Task 3 (`TestSplit_MidChildError`); the Consumer-Ack-after-all inheritance is via `Chain` (proven in ADR 0013's increment, not re-proven here). ✅
- Nil fn → `ErrNilFunc`, bad payload → `ErrPayloadType` → Task 1. ✅

**Deviation from spec signature (recorded):** the spec/ADR wrote `Split[A,B](fn, opts ...SplitOption)`; Phase 1 has **no real option** (sequence-stamping is unconditional, O9-5), so the plan ships the option-free `Split[A,B](fn) Step` to avoid an empty unused option type — matching `Transform`'s shape. Spec 009 D1 and ADR 0020 §1 are being updated to drop `SplitOption` for v1 (a future option can be added non-breakingly only if variadic is reintroduced; if a v1.x option is ever needed it is a minor addition via a new constructor or re-widened signature). **The implementer must build the option-free signature.**

**Placeholder scan:** no TBD/TODO; every code step shows complete code. ✅

**Type consistency:** `Split`, `stampSequence`, `HeaderSequenceNumber`, `HeaderSequenceSize`, `collect`, `boxInt` used identically across tasks. ✅

**Note for the `Example` (Task 3):** the example subscribes the printer via `split(print)` directly (a Splitter is a `Step`, so `split(next)` yields the handler); the unused `Chain(split)` line is illustrative only — the implementer may simplify to just `split(print)` if the linter flags the dead `flow` variable (prefer removing the `_ = flow` scaffold).
