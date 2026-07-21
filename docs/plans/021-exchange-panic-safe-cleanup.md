# Plan 021 — Panic-safe `ChannelExchange` cleanup

> **For agentic workers:** REQUIRED SUB-SKILL: use **`superpowers:subagent-driven-development`** (the project default —
> a fresh implementer subagent per task, coordinator verifies green and commits, adversarial reviewer subagent per
> task) or `superpowers:executing-plans`. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **REQUIRED GO SKILLS — every task starts here** (CLAUDE.md hard rule; `writing-plans`' own header template omits it,
> so it is restated per that override):
> - **`cc-skills-golang:golang-how-to`** — the always-on orchestrator; load it **first** on every task and let it pull
>   the relevant `golang-*` skills. For this plan expect at minimum `golang-concurrency` (the correlator's
>   `register`/`deliver`/`closeAll`/`giveUp` interleaving), `golang-error-handling` (panic/recover semantics and the
>   panic-transparency requirement), `golang-safety` (defer ordering, nil/blocking hazards), `golang-documentation`
>   (the SPI contract godoc in Task 2) and `golang-testing`.
> - **`superpowers:test-driven-development`** — strict red → green → refactor. Never write implementation ahead of a
>   failing test. In this plan the **red state is unusually valuable**: it is the proof the defect exists.
> - **`gopls`** for all Go navigation/diagnostics/refactor (native `LSP` tool). ⚠️ gopls' MCP server was observed
>   disconnected in a prior session — if `LSP` is unavailable, fall back to standard Go tooling and say so.
> - **Project-local overrides** (they beat `cc-skills-golang:golang-testing` where they conflict): **`table-test`**
>   (assert-closure tables, `ctx` modifier, `t.Context()`), **`use-mockgen`**, **`use-testcontainers`** (not needed
>   here — this plan is pure in-process, no external resource).

**Goal:** Make `ChannelExchange.Exchange` release its correlator slot on **every** exit path — including a panic
unwinding out of `request.Send` — without recovering the panic, and write the resulting no-leak-on-unwind obligation
into the `RequestReplyExchange` SPI contract.

**Architecture:** Two changes, in dependency order. **(1)** `register`'s `deregister` closure becomes
**identity-checked** (`ok && s == slot`) so that `deregister()==false` genuinely implies *our* slot was taken by a
`deliver` or closed by `closeAll` — closing a silent-reply-drop plus a **permanent goroutine hang** (Spec 012 §5.1 /
ADR 0022 Addendum A2). **(2)** `Exchange`'s three explicit `e.giveUp(…)` call sites are replaced with **one** deferred
reconciler guarded by a `settled bool`, set **only** in the `case reply, open := <-slot:` arm — the one state in which
the slot is provably no longer ours. (1) must land first: (2)'s bounded-drain guarantee is false without it.
`deliver`, `closeAll` and `giveUp`'s **body** stay unchanged, so the interleaving ADR 0022's round-2 audit hand-traced
is preserved. No `recover()` is introduced anywhere, so a consumer panic keeps unwinding with its original value and
stack.

**Tech Stack:** Go 1.25 (stdlib only for the change), `stretchr/testify`, `jonboulle/clockwork` (fake clock, existing),
`go.uber.org/goleak` (existing `VerifyNone`/`VerifyTestMain` guards).

## Governing artifacts — read before Task 1

| Artifact | What to take from it |
|---|---|
| `CLAUDE.md` (root) | Workflow, testing rules (blackbox-only, assert-closure tables), coverage gate, dependency policy, commit discipline |
| [`docs/specs/012-exchange-panic-safe-cleanup.md`](../specs/012-exchange-panic-safe-cleanup.md) | The defect (§1), the seven-arm exit table and the decided fix (§5), the **`deregister` prerequisite (§5.1)**, rejected alternatives (§5.2), the raced-reply policy (§5.3), the six required test cases (§6), contract + downstream scope (§7) |
| [`docs/adrs/0022-messaging-gateway.md`](../adrs/0022-messaging-gateway.md) **Addendum A** | A1 the deferred `settled`-guarded reconciler, **A2 identity-checked `deregister`**, A3 the SPI contract, A4 consequences. §2 (the G4 give-up reconciliation) and §1 (the SPI) are the sections amended |
| [`docs/specs/011-http-adapter.md`](../specs/011-http-adapter.md) §3.3 + [`docs/adrs/0023-http-channel-adapter.md`](../adrs/0023-http-channel-adapter.md) Addendum A5 | Where the residual is currently documented as *unfixed* — reconciled in Task 4 |
| `exchange.go`, `exchange_test.go` | The code under change and the conventions the new tests must match |

## Global Constraints

Copied verbatim from CLAUDE.md and Spec 012 §7. Every task's requirements implicitly include this section.

- **Go 1.25.** Always run with `GOTOOLCHAIN=go1.25.12` (a bare `go1.25` is rejected — "a language version but not a
  toolchain version"). Use no language/stdlib feature newer than 1.25.
- **No new dependency.** The fix is pure stdlib control flow inside existing functions. `go mod tidy` must leave
  `go.mod`/`go.sum` **unchanged**.
- **No exported symbol is added, removed or changed.** API/SemVer impact is **patch**. If any task finds itself wanting
  a new exported symbol, **stop and escalate to the coordinator** — that is a design change, not an implementation choice.
- **Blackbox tests only.** `package msgin_test`; exercise only the exported API. `replyCorrelator` is unexported and
  must **not** be reached for — use the `ErrDuplicateCorrelation` probe defined in Spec 012 §6.
- **Assert-closure tables** (`table-test` skill) for any ≥2 cases sharing a call shape; `t.Context()`, never
  `context.Background()`. `exchange_test.go`'s existing header note explains why several tests there are deliberately
  standalone (divergent concurrency setup) — respect that precedent rather than force-tabling unlike shapes.
- **Panic transparency is non-negotiable.** No `recover()` may be added to `exchange.go`. `Exchange` must never convert
  a consumer panic into an error return.
- **Exactly two functions change: `register`'s `deregister` closure (Task 1) and `Exchange` (Task 2).**
  `deliver`, `closeAll` and `giveUp`'s **body** are **not** to be modified. If a task appears to require touching them,
  **stop and escalate to the coordinator** — that is a design change, not an implementation choice.
- **Correction (Task 1 review): the `ok && s != slot` arm IS deterministically forceable.** The earlier claim here
  that it was stress-covered only, by construction, was disproved. `TestChannelExchange_reusedIDAbandon_drainsOwnReply`
  scripts the request channel so `Exchange`'s send-error arm deterministically reaches `giveUp` with caller B's slot
  already registered under caller A's reused id — no scheduler preemption needed. The stress test
  (`TestChannelExchange_reusedIDConcurrentAbandon_neverHangs`) is complementary concurrent-hang coverage, not a
  substitute. No test-only internal seam was needed.
- **Coverage gate:** every arm of Spec 012 §5's exit table (arms 0–6) and both `giveUp` arms have a covering case; the
  changed package holds its current level (Plan 019 shipped 99.1% overall, 100% on the exchange hot paths). Verify with
  `go test ./... -cover`, not by assumption.
- **Never call `require.*`, `t.Fatal` or `t.FailNow` from a spawned goroutine** (audit M-1n). `t.FailNow` outside the
  test goroutine is invalid: it calls `runtime.Goexit`, abandoning in-flight exchanges and workers, so `goleak` then
  reports a straggler storm that **masks** the assertion which actually fired. Inside a spawned goroutine, record the
  failure (send it on a buffered `chan string`) and `return`; assert on the test goroutine after joining. This applies
  to every stress test in this plan.
- **A hang is a test failure, never a wedged CI run.** Tasks 1 and 3 add concurrent tests whose regression mode is a
  permanent block. Every such test must bound itself (run the racing work in a goroutine and `select` on a
  `time.After`, failing with a clear message) so a regression reports rather than hangs. Run them with an explicit
  `-timeout` as a second line of defence.
- **Race + leak clean:** `go test ./... -race` green; the existing `goleak` guards stay in force and are not weakened.

---

## File structure

| File | Change | Responsibility |
|---|---|---|
| `exchange.go` | Modify — `register`'s `deregister` closure (`:71-79`), `Exchange` (`:248-276`), `RequestReplyExchange` godoc (`:20-23`), `WithUnmatchedReplySink` godoc (`:132-139`), `WithExchangeLogger` godoc (`:158-165`) | A2 identity-checked `deregister`, A1 the reconciler, A3 the SPI contract, the must-not-panic notes (§5.3) |
| `exchange_test.go` | Modify — append new tests | Blackbox proof of the fix; regression cover for the deleted explicit call sites |
| `adapter/http/inbound.go` | Modify — the two residual **notes** (the `NOTE:` block in `recoverHandler`'s godoc ≈`:51-58`, and `ServeGateway`'s godoc ≈`:173-183`). There is **one** shared `recoverHandler` (`:59-80`), not two recover sites | Reconcile godoc that describes the residual as unfixed |
| `adapter/http/inbound_test.go` | Modify — the comment at ~line 447 | Same, for the deliberately-un-asserted note |
| `docs/specs/011-http-adapter.md` | Modify — the Phase-1 status residual paragraph + §3.3 | Point at Spec 012 as the resolution |
| `docs/adrs/0023-http-channel-adapter.md` | Modify — Addendum A5 | Annotate as resolved by ADR 0022 Addendum A |

`exchange.go` is 302 lines and single-responsibility (the exchange + its correlator); no split is warranted or in scope.

**Line numbers above were verified against the tree at the branch point.** Re-confirm with `gopls`/`grep` before
editing rather than trusting them — they drift.

---

### Task 1: Identity-checked `deregister` — close the silent-drop and permanent-hang window

Spec 012 §5.1 / ADR 0022 Addendum A2. **This task must land before Task 2**: Task 2's bounded-drain guarantee is false
without it, and Task 2's whole test strategy reuses correlation ids, which is exactly what makes this window reachable.

**Files:**
- Modify: `exchange.go` — the `deregister` closure inside `register` (`:71-79`)
- Test: `exchange_test.go` (append; `package msgin_test`)

**Interfaces:**
- Consumes: existing exported API only.
- Produces: **no new exported symbol.** Behaviour: `deregister` reports `true` only when it removed *its own* slot, so a
  caller abandoning a slot that a `deliver` already claimed always takes the drain arm.

- [ ] **Step 1: Read the defect trace before touching code**

Read Spec 012 §5.1 in full — the five-step interleaving. You are fixing a **liveness** bug: the failure mode is a
goroutine blocked forever on `<-slot` in `giveUp` (`exchange.go:289`), unreachable by `deliver` (its slot is not in the
map) and by `closeAll` (which iterates the map). Understand *why* delete-by-id is insufficient before changing it.

- [ ] **Step 2: Write the failing test**

This stress test targets the **concurrent hang** half of §5.1. Stress it, and **bound it** so a regression fails
instead of wedging CI.

> **Corrected during the Task 1 review.** This step originally claimed the window "needs a `deliver` preempted between
> its map-delete and its channel-send, so it cannot be forced deterministically through the exported API." **That was
> disproved** — see Global Constraints above. The arm needs only that `deliver` *complete*, a second `register` land
> under the same id, and the first caller then reach `giveUp`; `Exchange`'s **send-error arm** reaches `giveUp` with no
> `select` race at all, so a scripted `MessageChannel` drives the ordering deterministically. The deterministic guard is
> `TestChannelExchange_reusedIDAbandon_drainsOwnReply` (§6 case 7), added in the same task. **Both tests ship:** this one
> covers the concurrent hang, that one deterministically covers the silent drop.

```go
// Spec 012 §5.1 / §6 case 6 (audit H-1): with two callers reusing one
// correlation id and replies delivered from another goroutine, a delete-by-id
// deregister can (a) delete the OTHER caller's slot and return true, dropping
// its own committed reply silently, and (b) orphan a slot so its owner's giveUp
// blocks on <-slot forever — unreachable by deliver (not in the map) and by
// closeAll (which iterates the map). Identity-checked deregister closes both.
//
// The window is a preemption between deliver's delete and its send, so this
// stresses rather than forces it. Two detectors, because the hang half is only
// probabilistically reachable: reply ACCOUNTING catches the silent drop
// deterministically whenever the window is hit, and the outer budget catches
// the hang. Bounded throughout: a regression must fail here, never wedge CI.
func TestChannelExchange_reusedIDConcurrentAbandon_neverHangs(t *testing.T) {
	defer goleak.VerifyNone(t)

	const (
		iterations = 200
		id         = "corr-reused-concurrent"
		budget     = 30 * time.Second
	)

	// Nothing inside the loop may call require/t.Fatal: t.FailNow outside the
	// test goroutine Goexits the worker, abandoning in-flight state and turning
	// the real failure into a goleak storm. Record and return instead.
	failures := make(chan string, 1)
	fail := func(format string, args ...any) {
		select {
		case failures <- fmt.Sprintf(format, args...):
		default:
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < iterations; i++ {
			var sunk atomic.Int64
			sink := msgin.NewDirectChannel()
			if err := sink.Subscribe(msgin.HandlerFunc(func(_ context.Context, _ msgin.Message[any]) error {
				sunk.Add(1)
				return nil
			})); err != nil {
				fail("iteration %d: sink subscribe: %v", i, err)
				return
			}

			request := msgin.NewDirectChannel()
			reply := msgin.NewDirectChannel()
			ex, err := msgin.NewChannelExchange(request, reply, msgin.WithUnmatchedReplySink(sink))
			if err != nil {
				fail("iteration %d: new exchange: %v", i, err)
				return
			}

			// The flow hands the reply to a worker goroutine, so deliver races
			// the waiter's abandonment rather than running inline.
			var (
				workers sync.WaitGroup
				sent    atomic.Int64
			)
			if err := request.Subscribe(msgin.Chain(msgin.Consume(func(_ context.Context, m msgin.Message[any]) error {
				workers.Add(1)
				go func() {
					defer workers.Done()
					sent.Add(1)
					_ = reply.Send(context.WithoutCancel(t.Context()), m)
				}()
				return nil
			}))); err != nil {
				fail("iteration %d: request subscribe: %v", i, err)
				return
			}

			// Two callers, SAME id, both abandoning via ctx cancel. Whichever
			// registers second only gets in once the first's slot has left the
			// map — precisely the reuse window.
			var (
				callers  sync.WaitGroup
				returned atomic.Int64
			)
			for c := 0; c < 2; c++ {
				callers.Add(1)
				go func() {
					defer callers.Done()
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()

					// Race the cancel against the exchange, but JOIN it so the
					// final iteration cannot leave a straggler for goleak.
					var canceller sync.WaitGroup
					canceller.Add(1)
					go func() {
						defer canceller.Done()
						cancel()
					}()

					req := msgin.New[any]("payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: id}))
					// Every error here is legitimate (ctx.Err,
					// ErrDuplicateCorrelation). What must hold is that this
					// RETURNS AT ALL — a hang is the H-1 regression — and that
					// a delivered reply is accounted for below.
					if _, err := ex.Exchange(ctx, req); err == nil {
						returned.Add(1)
					}
					canceller.Wait()
				}()
			}
			callers.Wait()
			workers.Wait()

			// H-1's SILENT-DROP half: every reply the flow produced was either
			// returned to its caller or routed to the unmatched sink. A
			// delete-by-id deregister drops one on the floor here — a direct
			// violation of ADR 0022 §2's G4 guarantee.
			if got, want := returned.Load()+sunk.Load(), sent.Load(); got != want {
				fail("iteration %d: %d replies accounted for but %d were sent — a committed reply was dropped (Spec 012 §5.1)", i, got, want)
				return
			}
			if err := ex.Close(); err != nil {
				fail("iteration %d: close: %v", i, err)
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(budget):
		t.Fatal("a caller blocked forever in giveUp: deregister deleted another caller's slot and orphaned it (Spec 012 §5.1)")
	}
	select {
	case msg := <-failures:
		t.Fatal(msg)
	default:
	}
}
```

> **Implementer notes.**
> - New imports for `exchange_test.go`: `fmt` and `sync/atomic` (`sync`, `time`, `context` are already imported).
> - `context.WithoutCancel(t.Context())` on the worker's `reply.Send` is deliberate — the caller's ctx is cancelled to
>   drive abandonment, and the reply must still be delivered for the race to be real.
> - The **reply-accounting** assertion is the load-bearing one. `Exchange` returning `nil` error means it consumed the
>   reply; the sink counts the rest. `returned + sunk != sent` means a committed reply vanished — exactly H-1's
>   silent-drop half, and unlike the hang it fails **deterministically** whenever the window is hit.
> - Every assertion is on the test goroutine (audit M-1n); the loop only records into `failures`.

- [ ] **Step 3: Run the test against the CURRENT code**

```bash
GOTOOLCHAIN=go1.25.12 go test -run TestChannelExchange_reusedIDConcurrentAbandon -race -timeout 120s . -v
```

**The expected outcome is non-deterministic** (audit L-1n). The window is a sub-microsecond preemption gap, so 200
iterations may or may not hit it. A failure looks like the reply-accounting message (most likely), the 30s budget
message, or a `goleak` report of a goroutine parked in `giveUp`.

**The proof of record is the §5.1 hand-trace, not this test's red state.** If it passes on the current code, do *not*
conclude the defect is absent: raise `iterations`, and confirm by inspection that `exchange.go:73` matches by key
alone. Report the outcome to the coordinator either way — a non-reproducing stress test is a finding about the *test*.
This is the one task in this plan whose TDD red state is probabilistic; say so in the task report rather than claiming
a clean red→green.

- [ ] **Step 4: Apply the fix**

In `exchange.go`, inside `register`, change the `deregister` closure to match on slot identity:

```go
	deregister = func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		// Identity, not just key: a reused id can have OUR entry already
		// removed by deliver and a DIFFERENT caller's slot registered under the
		// same key. Deleting that one would drop our committed reply silently
		// and orphan theirs — leaving its owner blocked forever in giveUp, on a
		// slot no deliver can find and closeAll cannot close (ADR 0022 A2).
		if s, ok := c.waiters[id]; ok && s == slot {
			delete(c.waiters, id)
			return true
		}
		return false
	}
```

Extend `register`'s doc comment where it explains `deregister`'s return value:

```go
// deregister returns true only if it removed OUR slot (the waiter still owned
// it, so no delivery is in flight). It returns false if our slot was already
// gone — claimed by a concurrent deliver (a reply is committed to it) or closed
// by closeAll — INCLUDING the case where a different caller has since
// registered the same id. That identity check is what makes false imply
// deliver-or-closeAll, and therefore what makes giveUp's drain bounded
// (audit G4/H-1). On false the caller must drain the slot.
```

- [ ] **Step 5: Verify green, race-clean, and that nothing regressed**

```bash
GOTOOLCHAIN=go1.25.12 go test -run TestChannelExchange -race -timeout 120s . -v
GOTOOLCHAIN=go1.25.12 go test ./... -race
```
Expected: the new test PASSES; every pre-existing `TestChannelExchange_*` still passes — in particular
`timeoutRacesDelivery` (`exchange_test.go:436`) and `closeRacesGiveUp` (`:521`), which exercise the arms whose
`deregister` result just changed meaning.

- [ ] **Step 6: Commit**

```bash
git add exchange.go exchange_test.go
git commit -m "$(cat <<'EOF'
fix(core): make the exchange correlator's deregister identity-checked

deregister deleted by correlation id alone, so with a reused id a caller could
delete a DIFFERENT caller's slot and return true: its own committed reply was
then dropped silently, and the other caller's slot was orphaned — absent from
the map, so no deliver could reach it and closeAll could not close it, leaving
its owner blocked forever on <-slot in giveUp.

Match on slot identity so deregister()==false genuinely implies deliver-or-
closeAll, which is what makes giveUp's drain bounded.

Spec: 012
Plan: 021
ADR: 0022
EOF
)"
```

---

### Task 2: The panic-safe reconciler in `Exchange`

**Files:**
- Modify: `exchange.go` — `func (e *ChannelExchange) Exchange` (`:248-276`; note `:283` is already inside `giveUp`)
- Test: `exchange_test.go` (append; `package msgin_test`)

**Interfaces:**
- Consumes: existing exported API only — `msgin.NewChannelExchange`, `msgin.NewDirectChannel`, `msgin.New[any]`,
  `msgin.WithHeaders`, `msgin.HeaderCorrelationID`, `msgin.Chain`, `msgin.Consume`, `msgin.HandlerFunc`,
  `msgin.ErrDuplicateCorrelation`, `msgin.ErrReplyTimeout`, `msgin.WithExchangeClock`, `msgin.WithUnmatchedReplySink`.
- Produces: **no new exported symbol.** Behaviour only: after a panic unwinds out of `Exchange`, the correlation id is
  reusable (a subsequent `Exchange` with the same id no longer returns `ErrDuplicateCorrelation`).

- [ ] **Step 1: Write the failing tests**

Append to `exchange_test.go`. Two behaviours, one shared setup — a `panicFlow` helper plus a table over the panic value
(per the `table-test` skill, the two panic-value cases share an identical call+assert shape):

```go
// panicExchange builds a ChannelExchange whose request flow panics with
// panicVal. Because a DirectChannel runs its subscriber chain synchronously on
// the caller's goroutine, the panic unwinds out of request.Send inside
// Exchange — the exact defect path of Spec 012 §1.
func panicExchange(t *testing.T, panicVal any, opts ...msgin.ExchangeOption) (*msgin.ChannelExchange, msgin.MessageChannel) {
	t.Helper()
	request := msgin.NewDirectChannel()
	reply := msgin.NewDirectChannel()
	ex, err := msgin.NewChannelExchange(request, reply, opts...)
	require.NoError(t, err)
	require.NoError(t, request.Subscribe(msgin.Chain(msgin.Consume(func(_ context.Context, _ msgin.Message[any]) error {
		panic(panicVal)
	}))))
	return ex, reply
}

// exchangeRecoveringPanic calls ex.Exchange and returns the recovered panic
// value (nil if it did not panic), so a test can assert on the value WITHOUT
// the recover happening inside library code.
func exchangeRecoveringPanic(t *testing.T, ex *msgin.ChannelExchange, req msgin.Message[any]) (recovered any) {
	t.Helper()
	defer func() { recovered = recover() }()
	_, _ = ex.Exchange(t.Context(), req)
	return nil
}

// Spec 012 §6 cases 1 & 2: a panicking flow handler must propagate its panic
// UNCHANGED (no recover/re-panic laundering in the library) and must not leave
// the correlation id registered. The reclamation probe is ErrDuplicateCorrelation:
// replyCorrelator is unexported, so id reuse is the blackbox observable.
func TestChannelExchange_panickingFlow_propagatesAndReclaimsSlot(t *testing.T) {
	defer goleak.VerifyNone(t)

	tests := []struct {
		name     string
		panicVal any
		assert   func(t *testing.T, recovered any)
	}{
		{
			name:     "string panic value propagates identically",
			panicVal: "boom",
			assert: func(t *testing.T, recovered any) {
				assert.Equal(t, "boom", recovered)
			},
		},
		{
			name:     "error panic value propagates as the same error instance",
			panicVal: errors.New("handler exploded"),
			assert: func(t *testing.T, recovered any) {
				err, ok := recovered.(error)
				require.True(t, ok, "expected the recovered value to still be an error, got %T", recovered)
				assert.Equal(t, "handler exploded", err.Error())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClock := clockwork.NewFakeClock()
			ex, _ := panicExchange(t, tt.panicVal, msgin.WithExchangeClock(fakeClock))
			const id = "corr-panic"
			req := msgin.New[any]("payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: id}))

			recovered := exchangeRecoveringPanic(t, ex, req)

			require.NotNil(t, recovered, "Exchange must not swallow the handler panic")
			tt.assert(t, recovered)

			// The reclamation probe (Spec 012 §6 case 2): the slot must be gone,
			// so REUSING the id must get past register(). It panics again (same
			// flow) rather than failing with ErrDuplicateCorrelation — which is
			// exactly the proof. Capture the error too, so a leaked slot fails
			// with the precise cause rather than a confusing "no panic".
			var (
				secondErr error
				second    any
			)
			func() {
				defer func() { second = recover() }()
				_, secondErr = ex.Exchange(t.Context(), req)
			}()
			require.NotErrorIs(t, secondErr, msgin.ErrDuplicateCorrelation,
				"the panicking first request leaked its correlator slot — Spec 012 §1")
			require.NotNil(t, second, "the reused correlation id must reach the flow again, not fail registration")
		})
	}
}

```

Also append the Spec 012 §6 case 4 regression cover for the **deleted** explicit call sites. The send-error arm is
already covered by the existing `TestChannelExchange_sendError`; add the cancel and timeout arms using the same
"reused id must not be duplicate" probe:

```go
// Spec 012 §6 case 4: the ctx-cancel and reply-timeout arms lose their explicit
// giveUp call in this task and are reconciled by the deferred path instead.
// These pin that the slot is still reclaimed on both.
func TestChannelExchange_abandonedArmsReclaimSlot(t *testing.T) {
	defer goleak.VerifyNone(t)

	tests := []struct {
		name string
		// trigger drives the in-flight Exchange to its abandonment arm. It owns
		// everything arm-specific — cancelling the ctx, or advancing the clock —
		// so the shared body below needs no per-case branching.
		trigger func(t *testing.T, cancel context.CancelFunc, fakeClock *clockwork.FakeClock)
		assert  func(t *testing.T, err error)
	}{
		{
			name: "ctx cancel reclaims the slot",
			trigger: func(_ *testing.T, cancel context.CancelFunc, _ *clockwork.FakeClock) {
				cancel()
			},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, context.Canceled) },
		},
		{
			name: "reply timeout reclaims the slot",
			trigger: func(t *testing.T, _ context.CancelFunc, fakeClock *clockwork.FakeClock) {
				require.NoError(t, fakeClock.BlockUntilContext(t.Context(), 1))
				fakeClock.Advance(30 * time.Second)
			},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrReplyTimeout) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClock := clockwork.NewFakeClock()
			ex, _, sinkHit := newBlockingExchange(t, msgin.WithExchangeClock(fakeClock))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			const id = "corr-abandon"

			// First request: registers the waiter, then abandons via tt.trigger.
			req := msgin.New[any]("payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: id}))
			errCh := make(chan error, 1)
			go func() {
				_, err := ex.Exchange(ctx, req)
				errCh <- err
			}()
			<-sinkHit // the flow ran, so the waiter is registered
			tt.trigger(t, cancel, fakeClock)
			tt.assert(t, <-errCh)

			// Reclamation probe: the id must be reusable. The second call hits
			// the same never-replying flow, so drive it to its own timeout on
			// a ctx the first case's cancel cannot affect.
			second := msgin.New[any]("second", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: id}))
			secondErrCh := make(chan error, 1)
			go func() {
				_, err := ex.Exchange(t.Context(), second)
				secondErrCh <- err
			}()
			<-sinkHit
			require.NoError(t, fakeClock.BlockUntilContext(t.Context(), 1))
			fakeClock.Advance(30 * time.Second)
			secondErr := <-secondErrCh
			require.NotErrorIs(t, secondErr, msgin.ErrDuplicateCorrelation, "the abandoned slot was not reclaimed")
			assert.ErrorIs(t, secondErr, msgin.ErrReplyTimeout)
		})
	}
}
```

> **Implementer note:** `newBlockingExchange`'s `sinkHit` is a `cap 1` buffered channel reused across both requests in
> this test, so each `<-sinkHit` must be matched by exactly one flow invocation — do not add a third `Exchange` to a
> case without a matching receive. If the timeout case's `fakeClock.Advance` races the *second* request's timer
> registration, `BlockUntilContext(t.Context(), 1)` is what serializes it; keep that call, do not replace it with a
> sleep.

- [ ] **Step 2: Run the tests to verify they fail — this is the defect proof**

```bash
GOTOOLCHAIN=go1.25.12 go test -run 'TestChannelExchange_panickingFlow|TestChannelExchange_abandonedArmsReclaimSlot' -race -timeout 120s . -v
```

Expected: `TestChannelExchange_panickingFlow_propagatesAndReclaimsSlot` **FAILS** on the reclamation probe — the second
call returns `ErrDuplicateCorrelation` instead of reaching the flow, so `require.NotErrorIs` fires (and `recover()`
yields `nil`, so `require.NotNil` would fire too). `TestChannelExchange_abandonedArmsReclaimSlot` should **PASS**
already — it is regression cover for behaviour the fix must preserve, not new behaviour. Task 1's
`reusedIDConcurrentAbandon` test must also still pass.

**If the abandoned-arms test fails at this point, stop and escalate** — that would mean an existing arm is already
broken and the plan's premise needs revisiting.

- [ ] **Step 3: Apply the fix**

In `exchange.go`, replace the body of `Exchange` from the `register` call through the `select` with:

```go
	slot, deregister, err := e.corr.register(id)
	if err != nil {
		return Message[any]{}, err // ErrGatewayClosed | ErrDuplicateCorrelation
	}
	// settled is false on every exit that abandons the slot — send error, ctx,
	// timeout, AND a panic unwinding out of request.Send (a DirectChannel runs
	// the flow synchronously on this goroutine). The deferred reconciler is the
	// SINGLE give-up site; it deliberately does not recover, so a consumer panic
	// keeps unwinding with its original value and stack (ADR 0022 Addendum A1).
	settled := false
	defer func() {
		if !settled {
			e.giveUp(ctx, slot, deregister)
		}
	}()
	if err := e.request.Send(ctx, req); err != nil {
		return Message[any]{}, err
	}
	timer := e.clock.NewTimer(e.timeout)
	defer timer.Stop()
	select {
	case reply, open := <-slot:
		// The ONLY state in which the slot is provably no longer ours: a
		// deliver consumed it, or closeAll removed and closed it. Running
		// giveUp here would deadlock — deregister returns false and the drain
		// blocks on an emptied, never-closed channel (Spec 012 §5, arm 3).
		settled = true
		if !open {
			return Message[any]{}, ErrGatewayClosed // closeAll closed our slot
		}
		return reply, nil
	case <-ctx.Done():
		return Message[any]{}, ctx.Err()
	case <-timer.Chan():
		return Message[any]{}, ErrReplyTimeout
	}
```

The three former `e.giveUp(ctx, slot, deregister)` calls are **deleted**. `giveUp`'s own **body** is untouched — but
its **doc comment** must be corrected (audit L-4n): it currently enumerates only three triggers ("send error, ctx,
timeout") and would otherwise contradict ADR 0022 Addendum A1.

```go
// giveUp reconciles a waiter that is abandoning its slot — send error, ctx,
// reply timeout, or a PANIC unwinding out of request.Send — with a
// possibly-concurrent deliver. [...rest of the existing comment unchanged...]
```

Update `Exchange`'s doc comment's final sentence — `A request-channel send error propagates (waiter deregistered).` —
to cover the widened arm set:

```go
// A request-channel send error propagates, and any abandonment — send error,
// ctx, reply timeout, or a PANIC unwinding out of the flow — releases the
// waiter via a single deferred reconciler. The panic is never recovered here:
// it propagates unchanged, with its slot already reclaimed (ADR 0022 Addendum A1).
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
GOTOOLCHAIN=go1.25.12 go test -run 'TestChannelExchange' . -v
```
Expected: PASS, including the two previously-failing panic tests and every pre-existing `TestChannelExchange_*`.

- [ ] **Step 5: Run the full suite with the race detector and leak checks**

```bash
GOTOOLCHAIN=go1.25.12 go test ./... -race
```
Expected: `ok` for every package, no `goleak` failure. The `timeoutRacesDelivery` and `closeRacesGiveUp` interleaving
tests are the ones that matter most here — they exercise `giveUp`'s reconciliation under the new call site.

- [ ] **Step 6: Confirm coverage on the changed package**

```bash
GOTOOLCHAIN=go1.25.12 go test . -coverprofile=/tmp/cov.out && GOTOOLCHAIN=go1.25.12 go tool cover -func=/tmp/cov.out | grep -E 'exchange.go|total'
```
Expected: every `exchange.go` line at 100.0%; total no lower than the pre-change figure. If any `Exchange` or `giveUp`
line is uncovered, add the missing case before committing — a hot-path branch with no test is a delivery blocker.

- [ ] **Step 7: Commit**

```bash
git add exchange.go exchange_test.go
git commit -m "$(cat <<'EOF'
fix(core): reclaim the exchange correlator slot on a panicking flow handler

ChannelExchange.Exchange registered its reply waiter before sending and called
giveUp non-deferred, so a panic unwinding out of request.Send (a DirectChannel
runs the flow synchronously on the caller's goroutine) bypassed every cleanup
arm and leaked the correlator entry until Close.

Replace the three explicit giveUp call sites with one deferred reconciler
guarded by a settled flag, set only where the slot is provably no longer ours.
giveUp's own logic is unchanged, so the audited deliver/closeAll interleaving is
preserved. No recover() is introduced: the panic propagates unchanged.

Spec: 012
Plan: 021
ADR: 0022
EOF
)"
```

---

### Task 3: SPI contract + the raced-reply-under-panic drain

**Files:**
- Modify: `exchange.go` — `RequestReplyExchange` godoc (`:20-23`), `WithUnmatchedReplySink` godoc (`:132-139`),
  `WithExchangeLogger` godoc (`:158-165`)
- Test: `exchange_test.go` (append)

**Interfaces:**
- Consumes: Task 2's reconciler (and Task 1's identity-checked `deregister`, without which the drain is unbounded) —
  this task proves the drain arm and documents the contract it
  establishes. Same exported API set as Task 1, plus `msgin.WithUnmatchedReplySink`.
- Produces: **no new exported symbol.** Godoc contract only.

- [ ] **Step 1: Write the failing test**

Spec 012 §6 case 3 — a handler that replies and *then* panics. This exercises the `deregister()==false` arm of `giveUp`
**during a panic unwind**: the reply must be drained to the unmatched sink, and the panic must still propagate.

```go
// Spec 012 §5.3 / §6 case 3: when the flow sends its reply and THEN panics, a
// deliver is already committed to the slot when the unwind reaches the deferred
// reconciler. giveUp's deregister()==false arm must drain that reply to the
// unmatched sink — identical treatment to the timeout/cancel arms — while the
// panic still propagates unchanged.
func TestChannelExchange_panickingFlowAfterReply_drainsToUnmatchedSink(t *testing.T) {
	defer goleak.VerifyNone(t)

	sink := msgin.NewDirectChannel()
	received := make(chan msgin.Message[any], 1)
	require.NoError(t, sink.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		received <- m
		return nil
	})))

	request := msgin.NewDirectChannel()
	reply := msgin.NewDirectChannel()
	ex, err := msgin.NewChannelExchange(request, reply, msgin.WithUnmatchedReplySink(sink))
	require.NoError(t, err)

	const id = "corr-reply-then-panic"
	// The flow replies (delivering into the waiter's slot) and only then panics.
	require.NoError(t, request.Subscribe(msgin.Chain(msgin.Consume(func(ctx context.Context, m msgin.Message[any]) error {
		if sendErr := reply.Send(ctx, msgin.WithPayload(m, any("echo"))); sendErr != nil {
			return sendErr
		}
		panic("boom after reply")
	}))))

	req := msgin.New[any]("payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: id}))
	recovered := exchangeRecoveringPanic(t, ex, req)

	require.Equal(t, "boom after reply", recovered, "the panic must propagate unchanged through the drain")

	select {
	case got := <-received:
		assert.Equal(t, "echo", got.Payload())
	default:
		t.Fatal("expected the raced-in reply to be drained to the unmatched sink, not dropped")
	}

	// And the slot is still reclaimed: the id is reusable.
	require.NotNil(t, exchangeRecoveringPanic(t, ex, req),
		"the reused correlation id must reach the flow again")
}
```

> **Implementer note:** `reply.Send` inside the flow runs `receiver()` → `corr.deliver` **synchronously on this same
> goroutine**, so by the time `panic` executes the slot already holds the reply and has been removed from the map.
> `deregister()` therefore returns `false` and `giveUp` takes its drain arm — the exact path under test.
>
> `msgin.WithPayload` is a **package-level generic function** (`payload.go:24`), *not* a method on `Message[T]` — the
> only `Message[T]` methods are `Payload`, `Headers`, `Header`, `ID`, `WithHeader`, `WithoutHeader` (`message.go`). It
> preserves headers, which is required here: the correlation id must survive onto the reply or `deliver` will not
> match it.

- [ ] **Step 2: Run the test to verify it behaves as expected**

```bash
GOTOOLCHAIN=go1.25.12 go test -run TestChannelExchange_panickingFlowAfterReply . -v
```
Expected with **Tasks 1–2** already applied: **PASS**. This case is *characterization* of the drain arm rather than a
new red → green cycle — **Task 2's** reconciler already routes it. (With only Task 1 applied it would fail: no `giveUp`
runs on the panic arm, so the reply never reaches the sink. Do not run it before Task 2 and report a false alarm.)

**If it FAILS,** that is a genuine finding about the drain arm under unwind: stop, diagnose with
`superpowers:systematic-debugging`, and escalate to the coordinator before changing `giveUp` (which Global Constraints
forbid this plan from modifying).

- [ ] **Step 2b: Write the CONCURRENT panic-races-delivery test (audit H-2)**

Everything above is single-goroutine and deterministic, so none of it actually exercises the claim the fix rests on:
that the drain cannot stall when a `deliver` on **another** goroutine races the unwind. This is the panic arm's
counterpart to the existing `TestChannelExchange_timeoutRacesDelivery` (`exchange_test.go:436`), and ADR 0022's audit
G2 set the precedent that the primitive's headline claim is not left to the synchronous path alone.

```go
// Spec 012 §6 case 5 (audit H-2): the flow hands the message to a worker
// goroutine and THEN panics, so deliver genuinely races the deferred
// reconciler rather than completing before it. Loops so both orderings occur:
//   - worker wins  -> deregister()==false -> giveUp drains to the sink
//   - unwind wins  -> deregister()==true  -> the late reply is unmatched
// Either way the panic must propagate unchanged, the slot must be reclaimed,
// and NOTHING may block. Bounded so a regression fails instead of wedging CI.
func TestChannelExchange_panicRacesDelivery(t *testing.T) {
	defer goleak.VerifyNone(t)

	const (
		iterations = 30
		id         = "corr-panic-race"
		budget     = 30 * time.Second
		grace      = 2 * time.Second
	)

	// Same discipline as Task 1: no require/t.Fatal off the test goroutine.
	failures := make(chan string, 1)
	fail := func(format string, args ...any) {
		select {
		case failures <- fmt.Sprintf(format, args...):
		default:
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < iterations; i++ {
			// cap 2: this iteration drives the flow twice (the probe re-enters
			// it), so up to two replies can land on the unmatched path.
			received := make(chan msgin.Message[any], 2)
			sink := msgin.NewDirectChannel()
			if err := sink.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
				received <- m
				return nil
			})); err != nil {
				fail("iteration %d: sink subscribe: %v", i, err)
				return
			}

			request := msgin.NewDirectChannel()
			reply := msgin.NewDirectChannel()
			ex, err := msgin.NewChannelExchange(request, reply, msgin.WithUnmatchedReplySink(sink))
			if err != nil {
				fail("iteration %d: new exchange: %v", i, err)
				return
			}

			var workers sync.WaitGroup
			if err := request.Subscribe(msgin.Chain(msgin.Consume(func(_ context.Context, m msgin.Message[any]) error {
				// ready is per-INVOCATION, not per-iteration: this handler runs
				// TWICE per iteration (the probe re-enters it), and an
				// iteration-scoped channel would be closed twice — a "close of
				// closed channel" panic masquerading as the flow's own panic
				// (audit H-1n).
				ready := make(chan struct{})
				workers.Add(1)
				go func() {
					defer workers.Done()
					<-ready // release the worker and the panic together
					_ = reply.Send(context.WithoutCancel(t.Context()), msgin.WithPayload(m, any("echo")))
				}()
				close(ready)
				panic("boom racing delivery")
			}))); err != nil {
				fail("iteration %d: request subscribe: %v", i, err)
				return
			}

			req := msgin.New[any]("payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: id}))

			// First drive: the panic must propagate unchanged through the drain.
			if got := exchangeRecoveringPanic(t, ex, req); got != "boom racing delivery" {
				fail("iteration %d: first call recovered %#v, want the flow's own panic value", i, got)
				return
			}
			workers.Wait()

			// The reply is accounted for on whichever arm won — drained by
			// giveUp, or routed as unmatched by the receiver. Never lost.
			select {
			case got := <-received:
				if got.Payload() != "echo" {
					fail("iteration %d: unmatched sink got payload %#v, want \"echo\"", i, got.Payload())
					return
				}
			case <-time.After(grace):
				fail("iteration %d: the raced reply reached neither the drain nor the unmatched path", i)
				return
			}

			// Second drive on the SAME id: proves the slot was reclaimed, and
			// its panic value must be intact too (a "close of closed channel"
			// regression would surface right here).
			if got := exchangeRecoveringPanic(t, ex, req); got != "boom racing delivery" {
				fail("iteration %d: reused id recovered %#v — the slot was not reclaimed, or the barrier double-closed", i, got)
				return
			}
			workers.Wait()

			select {
			case <-received:
			case <-time.After(grace):
				fail("iteration %d: the second raced reply was lost", i)
				return
			}
			if err := ex.Close(); err != nil {
				fail("iteration %d: close: %v", i, err)
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(budget):
		t.Fatal("the deferred reconciler blocked during a panic unwind (Spec 012 §5.3)")
	}
	select {
	case msg := <-failures:
		t.Fatal(msg)
	default:
	}
}
```

> **Implementer notes.**
> - The per-invocation `ready`/`close(ready)` handshake widens the race window; it does **not** guarantee an ordering,
>   which is the point — both orderings must be safe.
> - Each `exchangeRecoveringPanic` re-enters the same panicking flow, so each spawns a worker and produces a reply.
>   Join (`workers.Wait()`) and drain (`<-received`) **both** before `Close()`, or `goleak` flags the straggler.
> - Every receive is bounded by `grace`; every assertion is on the test goroutine (audit M-1n, L-8n).
> - If this shape proves flaky, tighten the synchronisation — do **not** delete the test or weaken `goleak`.

Run it:

```bash
GOTOOLCHAIN=go1.25.12 go test -run TestChannelExchange_panicRacesDelivery -race -timeout 120s . -v
```
Expected: **PASS** with Tasks 1–2 applied. A hang or a `goleak` report is a genuine finding — stop and escalate.

- [ ] **Step 3: Write the SPI contract godoc (ADR 0022 Addendum A3)**

Extend `RequestReplyExchange`'s doc comment in `exchange.go`:

```go
// RequestReplyExchange is the narrow SPI a gateway delegates to: it sends a
// request and returns the correlated reply (or an error). ChannelExchange is the
// in-process implementation; a future HTTP/NATS adapter implements Exchange for
// a real external round-trip, so both gateway façades work over it unchanged.
//
// Contract: an implementation MUST release every piece of request-scoped state
// it acquires — a correlator entry, an in-flight connection, a response body —
// on EVERY exit path, including a panic unwinding out of a downstream call.
// Deferred cleanup is the only reliable way to honour this; an implementation
// that cleans up at each return site alone will leak whenever a caller-supplied
// handler panics. An implementation MUST NOT recover such a panic into an error
// return: the panic belongs to the caller's code and must propagate with its
// original value and stack (ADR 0022 Addendum A3; Spec 012).
```

Extend `WithUnmatchedReplySink`'s doc comment with the §5.3 residual — append to its existing "should be non-blocking
or promptly bounded" paragraph:

```go
// The sink must also neither panic nor block: it can run inside Exchange's
// deferred cleanup while a handler panic is already unwinding. A second panic
// would replace — and therefore mask — the consumer's original one, and a
// blocking sink stalls the unwind itself (Spec 012 §5.3).
```

Give `WithExchangeLogger` the same clause (audit M-1). The logger is on that deferred path in the **default**
configuration, not merely when a sink is opted in: `routeUnmatched` calls `e.logger.Warn` on **both** branches — the
sink-error branch and the warn-log-and-drop branch that runs when no sink is configured.

```go
// The logger must neither panic nor block: routeUnmatched calls it on both
// branches, and can run inside Exchange's deferred cleanup while a handler
// panic is already unwinding, where a panicking slog.Handler would replace —
// and therefore mask — the consumer's original panic, and a blocking one would
// stall the unwind (Spec 012 §5.3).
```

- [ ] **Step 4: Verify the docs build and nothing regressed**

```bash
GOTOOLCHAIN=go1.25.12 go doc github.com/kartaladev/msgin RequestReplyExchange
GOTOOLCHAIN=go1.25.12 go test ./... -race
```
Expected: the contract paragraph appears in the `go doc` output; all packages `ok`.

- [ ] **Step 5: Commit**

```bash
git add exchange.go exchange_test.go
git commit -m "$(cat <<'EOF'
test(core): cover the panic drain arm and document the exchange SPI contract

RequestReplyExchange now states that an implementation must release all
request-scoped state on every exit path including a panic unwind, and must not
recover a caller panic into an error return. This binds the HTTP request-reply
exchange (Spec 011 Phase 2), which holds its own request-scoped state.

Also documents that neither the unmatched-reply sink nor the injected logger
may panic, since both can run inside the deferred cleanup while a handler panic
is unwinding, and covers the reply-then-panic drain arm both synchronously and
racing a concurrent delivery.

Spec: 012
Plan: 021
ADR: 0022
EOF
)"
```

---

### Task 4: Downstream reconciliation + whole-branch delivery gate

**Files:**
- Modify: `adapter/http/inbound.go` — the **two residual notes**: the `NOTE:` block inside `recoverHandler`'s godoc
  (≈`:51-58`) and the paragraph in `ServeGateway`'s godoc (≈`:173-183`). NB there is **one** shared `recoverHandler`
  (`:59-80`), not two `recover()` sites (audit L-3)
- Modify: `adapter/http/inbound_test.go` (the comment at ~line 447)
- Modify: `docs/specs/011-http-adapter.md` (the Phase-1 status residual paragraph; §3.3's residual note)
- Modify: `docs/adrs/0023-http-channel-adapter.md` (Addendum A5)

**Interfaces:**
- Consumes: Tasks 1–3. No code behaviour changes in this task — **documentation reconciliation only**, plus the gate.
- Produces: nothing consumed by a later task; this is the final task.

- [ ] **Step 1: Read the four residual sites before editing**

```bash
sed -n '44,82p;168,186p' adapter/http/inbound.go
sed -n '440,455p' adapter/http/inbound_test.go
grep -n 'Spec 012\|residual\|leaks a `ChannelExchange`' docs/specs/011-http-adapter.md docs/adrs/0023-http-channel-adapter.md
```

Each currently states the leak is **unfixed**. Each must now say it is **fixed by Spec 012 / ADR 0022 Addendum A**,
while **keeping** the statement that the `recover()` boundary itself is still required. Note `recoverHandler`'s NOTE
block ends with "do not remove this recover" — that instruction **stays**; only its stated *reason* changes.

- [ ] **Step 2: Rewrite each residual note**

The substance to convey at every site (adapt the wording to each site's voice — do not paste one block four times):

> The panic is recovered here so a panicking flow handler cannot kill the server; that fault isolation is still
> required and independent of the exchange. The former residual — that the recover could not reclaim the exchange's
> correlator slot — **no longer applies**: `ChannelExchange.Exchange` reclaims its slot on a panic unwind as of
> Spec 012 / ADR 0022 Addendum A.

For `adapter/http/inbound_test.go`'s comment ("deliberately does NOT assert that no correlator slot leaks: the core's
…"), replace the rationale: the core now reclaims the slot, and the **core-side** test
(`TestChannelExchange_panickingFlow_propagatesAndReclaimsSlot`) owns that assertion — the adapter test stays focused on
containment (clean 500, server survives) rather than duplicating core coverage across a package boundary.

In `docs/specs/011-http-adapter.md`, the Phase-1 status block's closing sentence currently reads "One residual is
recorded honestly, not buried: a panicking flow handler still leaks a `ChannelExchange` correlator slot … tracked as
Spec 012." Change it to record the residual as **resolved** by Spec 012 / Plan 021, keeping the history (it shipped as
a known residual in Phase 1) rather than deleting the sentence.

In `docs/adrs/0023-http-channel-adapter.md`, append a resolution line to Addendum A5 pointing at
[ADR 0022 Addendum A](0022-messaging-gateway.md#addendum-a--panic-safe-cleanup-2026-07-21).

- [ ] **Step 3: Verify no code behaviour changed**

```bash
GOTOOLCHAIN=go1.25.12 git diff --stat
GOTOOLCHAIN=go1.25.12 go test ./... -race
```
Expected: the `.go` diff in this task touches **comments only** (confirm by reading the diff); all packages `ok`.

- [ ] **Step 4: Run the full library quality gate**

```bash
export GOTOOLCHAIN=go1.25.12
go build ./... && CGO_ENABLED=0 go build ./...
go vet ./...
gofmt -l . && gofumpt -l .          # both must print nothing
golangci-lint run ./...
go mod tidy && git diff --exit-code go.mod go.sum   # must be EMPTY — no new dependency
go mod verify
govulncheck ./...
# gofumpt may not be installed here; if `command -v gofumpt` is empty, say so
# in the report rather than silently skipping the check.
go test ./... -race
go test ./... -cover
go test -run '^Example' ./...
```
Expected: every command clean; `git diff --exit-code go.mod go.sum` exits 0.

- [ ] **Step 5: Confirm the API is unchanged (patch-level SemVer)**

```bash
GOTOOLCHAIN=go1.25.12 go run golang.org/x/exp/cmd/gorelease@latest
```
Expected: **no exported API change** reported → patch bump. If anything other than "compatible / patch" is reported,
stop and escalate — Global Constraints forbid an exported-symbol change in this plan.

- [ ] **Step 6: Whole-branch review gate (CLAUDE.md §5 — mandatory before delivery)**

Run over the **whole branch diff** (`main..HEAD`), not just the last commit:
1. `/code-review` — resolve or explicitly triage every finding with a written rationale.
2. `/security-review` — same. The panic/cleanup path is a robustness surface, and the `WithTrustedCorrelationID`
   poisoning variant of Spec 012 §3 is the security-relevant behaviour being fixed.
3. Re-run `go test ./... -race` after any fix.

- [ ] **Step 7: Commit the reconciliation**

```bash
git add adapter/http/inbound.go adapter/http/inbound_test.go docs/specs/011-http-adapter.md docs/adrs/0023-http-channel-adapter.md
git commit -m "$(cat <<'EOF'
docs(http): record the exchange slot-leak residual as resolved

adapter/http's recover boundary is still required fault isolation, but its
caveat that the recover could not reclaim the exchange's correlator slot no
longer holds: ChannelExchange reclaims it on a panic unwind. Reconciles the two
inbound.go notes, the inbound_test.go comment, Spec 011's Phase-1 status block
and ADR 0023 Addendum A5.

Spec: 012
Plan: 021
ADR: 0022
EOF
)"
```

- [ ] **Step 8: Stop — do not merge or push**

Report to the coordinator: gate results, coverage figures, and the review verdicts. **Merge, push and branch deletion
each require explicit per-action user approval** (CLAUDE.md) and are **not** covered by this plan's per-task commit
pre-authorization.

---

## Delivered — outcome and deviations

_To be filled in at delivery, mirroring Plan 020's section: actual commit SHAs, gate results, coverage, review verdicts,
and any deviation from the plan with its rationale._

## Traceability

- **Spec:** [012 — Panic-safe `ChannelExchange` cleanup](../specs/012-exchange-panic-safe-cleanup.md)
- **ADR:** [0022 — Messaging Gateway / `RequestReplyExchange`](../adrs/0022-messaging-gateway.md), **Addendum A**
  (A1 the deferred reconciler, **A2 identity-checked `deregister`**, A3 the SPI contract, A4 consequences)
- **Amends the code of:** [Plan 019](019-messaging-gateway.md) (`exchange.go`, as shipped)
- **Surfaced by:** [Plan 020](020-http-adapter-inbound.md) / [Spec 011](../specs/011-http-adapter.md) Phase 1
- **Unblocks nothing, tightens one thing:** Spec 011 Phase 2 (Plan 022, the O2 HTTP `RequestReplyExchange`) inherits
  the A2 contract.
- **Commit trailers:** every commit in this plan carries `Spec: 012`, `Plan: 021`, `ADR: 0022`.
