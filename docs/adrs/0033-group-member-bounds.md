# ADR 0033 — A message group's member bound lives at the store, not at the release decision

- **Status:** **PROPOSED — revision 1, pre-audit, NOT accepted.** Written before any code, per
  [CLAUDE.md](../../CLAUDE.md)'s design-time gate. The adversarial design audit over the assembled bundle
  (spec + ADR + plan) has **not** run.
  - 🔴 **Decisions D-AC through D-AL were taken WITHOUT USER RATIFICATION.** The user was away when this bundle was
    drafted. Every decision below is **open to reversal**;
    [Spec 017 §8](../specs/017-group-member-bounds.md) lists the four that most deserve a second look, and each
    such decision carries a **REVERSIBILITY** line stating what undoing it costs.
  - The decision-letter series continues from [ADR 0032](0032-sizing-option-bounds.md), which ends at **D-AB**.
- **Prompted by:** [Spec 017](../specs/017-group-member-bounds.md); the backlog item in
  [`docs/HANDOVER.md`](../HANDOVER.md) §6 item **7**, filed at Plan 029's delivery gate and widened here from one
  option to **three release paths plus two stores**.
- **Realized by:** [Plan 031](../plans/031-group-member-bounds.md).
- **Extends** [ADR 0032](0032-sizing-option-bounds.md): this ADR is the direct successor to that defect class. It
  reuses **D-X** (reuse the existing sentinels; mint none; wrap the value into the error), **D-Z**'s ceiling
  reasoning for the value 65,536, and **D-AB**'s membership criterion, all unchanged. It **does not supersede** any
  part of ADR 0032 — `routing.WithCompletionSize`'s ceiling stays exactly as delivered.
- **Related:** [ADR 0031](0031-nil-option-elements.md) **D-R** (per-package unexported helpers, four independent
  copies rather than a shared internal package — the precedent for the fifth `checkRange`); ADR 0021 (the SQL group
  store and its `GroupDialect` SPI).

## Context

**Plan 029 bounded one of the four ways an Aggregator decides a group is complete.** `routing.WithCompletionSize`
got a ceiling of 65,536 members (`routing/aggregator.go:33`), enforced in `NewAggregator`
(`aggregator.go:353-358`) and gated on `cfg.completionSizeSet` — a field **only** `WithCompletionSize` writes
(`aggregator.go:156`). The other three paths never set it, so the check never runs for them:

| Path | Site | Why no bound can be expressed there |
|---|---|---|
| `WithReleaseStrategy(fn)` | `aggregator.go:116` | `fn` is a caller-supplied closure; the option body is `c.release = fn` |
| `WithReleaseWhen(fn)` | `aggregator.go:128` | sugar over the above |
| `defaultRelease` — **no option at all** | `aggregator.go:222` | reads the threshold from the **first member's `msgin.HeaderSequenceSize` header**: the bound is **DATA** |

**The accumulation itself is unbounded in both first-party stores.** `memory.GroupStore.Add`
(`adapter/memory/groupstore.go:117-136`) admits new **keys** against `WithMaxGroups` (`:122-124`) and then appends
at `:134` with **no per-group cap**; it also `slices.Clone`s the group on every call (`:130`, `:135`), so per-group
cost is **quadratic in time**. Spec 016 §1.4 measured reaching 65,536 members at **48.3 GiB of allocation churn and
8.6 s**. `SettleGroup` (`:159-186`) shrinks the group only after a successful release, and the reaper is opt-in —
with no `WithGroupTimeout`, `memory.GroupStore.RecoverInterval()` returns `0` (`:219`) and `Aggregator.Run` blocks
on `ctx.Done()` without sweeping. **A group whose release never fires grows monotonically forever.**

`adapter/database/sql.GroupStore` is worse in kind and had not been examined: it has **no member cap and no
group-count cap of any kind**, and its `Add` (`groupstore.go:249-280`) re-fetches and re-decodes **every live
member** on every arrival, per `GroupDialect.AddMember`'s own contract (`groupdialect.go:108-126`).

**The class gate cannot reach any of this, and not for a fixable reason.** `sizing_option_class_gate_test.go`'s
`isIntOrInt64` (`:191-202`) matches `*ast.Ident{"int"|"int64"}` through `...`, slice and pointer wrappers.
`WithReleaseWhen`'s parameter is an `*ast.FuncType` (falls through to `false`); `WithReleaseStrategy`'s is an
`*ast.Ident{"ReleaseStrategy"}` (a named type — a limitation the gate's header already states for
`type Bytes int64`); and `defaultRelease` has no parameter at all. **Spec 016's class is defined over options with
an integer parameter. A bound that arrives as a closure or as a header value is outside its premise, not outside
its implementation.**

## Decision

### D-AC — the bound lives at the ACCUMULATION SITE (the store), not at the release decision

**Decision.** The per-group member bound is enforced in `MessageGroupStore.Add`, in both first-party stores, and
stated as a requirement on the SPI. It is **not** expressed at, derived from, or validated against the release
strategy.

**Rationale, in order of weight.**

1. **Completeness.** Three of the four release paths are opaque to the library: two are caller-supplied closures,
   one is a message header. **No bound expressed at the release decision can ever cover more than path 1** — which
   is exactly the state Plan 029 shipped. The store, by contrast, observes **every** member that joins a group,
   for every path, including paths added later.
2. **Precedent inside the same function.** `memory.GroupStore`'s existing admission check for group **count**
   already lives in `Add` (`groupstore.go:122-124`). The member check goes **two lines below it**, under the same
   mutex, returning the same sentinel. The two overflow arms become symmetric rather than one being a special case
   living in a different package.
3. **The SPI is the right place for a contract every store must honour.** `msgin.MessageGroupStore`
   (`groupstore.go:37`) is public and third-party-implementable. A bound stated in `routing` protects only the
   stores `routing` ships with.

**Consequence for `WithCompletionSize`:** its ceiling is **not** removed. Two bounds on the same quantity is not
redundancy — the option ceiling is a *construction-time* rejection of a nonsense configuration (best
debuggability), the store cap is a *runtime* bound on the accumulation (complete coverage).

**REVERSIBILITY:** this is the load-bearing decision; reversing it discards the increment. Everything below is
downstream of it.

### D-AD — two new options, reusing `checkRange` and `msgin.ErrInvalidCapacity`; mint no sentinel

**Decision.** `memory.WithMaxGroupMembers(n int)` and `sql.WithMaxGroupMembers(n int)`, both **default `1 << 16`**
(65,536), both with **ceiling `1 << 20`** (1,048,576), both validated in their `NewGroupStore` with the shipped
per-package unexported `checkRange` helper against `msgin.ErrInvalidCapacity` — exactly as `WithMaxGroups` does
today (`adapter/memory/groupstore.go:104-107`).

```
memory.NewGroupStore(memory.WithMaxGroupMembers(0))
  → "msgin: capacity out of range: memory.WithMaxGroupMembers: 0 not in [1, 1048576]"
```

**Why 65,536 is not a new judgement.** ADR 0032 **D-Z** fixed `completionSizeCeiling = 1 << 16` on the ground that
65,536 group members is *"far beyond any plausible aggregation"*, sized by **time, not bytes**, and that row
survived attack in **all five** of Plan 029's audit rounds. The unit here — members of one correlation group — is
**identical**. Reusing a ratified reference value for the identical quantity is the opposite of guessing.

**Why `1 << 20` for the ceiling.** It matches `maxGroupsCeiling` (`adapter/memory/groupstore.go:62`), the sibling
bound in the same struct, so `WithMaxGroups` and `WithMaxGroupMembers` read as one number for "the largest
in-flight aggregation quantity this library accepts." It is 16× the default, giving a real escape hatch to a caller
with a genuinely enormous aggregation.

**Why no new sentinel.** ADR 0032 **D-X**'s reuse rule, applied unchanged: one `errors.Is` target for *"this size
is wrong,"* with the site name in the wrap doing the disambiguation. **This makes `msgin.ErrInvalidCapacity` a
SIXTH producer**, where Spec 016 §3.5 counted four and warned that a fifth *"should be a conscious decision rather
than a default."* It is conscious: the alternative adds two exported sentinels to a pre-v1 surface we are keeping
small and splits a caller's size-validation branch four ways. **A seventh producer needs its own ADR.**

`adapter/database/sql` has no `checkRange` today (four copies exist, in `endpoint`, `routing`, `adapter/memory`,
`adapter/http`) and gains a fifth, unexported, identical copy — ADR 0031 **D-R** / Spec 014 §3.3's
four-independent-copies precedent, extended by one rather than converted into a shared internal package.

**REVERSIBILITY:** the default value is one constant per store (Spec 017 §8 item 3). The sentinel choice is one
`checkRange` argument per call site.

### D-AE — exceeding the cap returns `msgin.ErrOverflowDropped`, wrapped, and NOT `Permanent`

**Decision.** An `Add` that would take a group past its cap returns an error wrapping `msgin.ErrOverflowDropped` —
the same sentinel as the group-count arm two lines above it — in the shape:

```go
fmt.Errorf("%w: %s: group %q holds %d members, limit %d",
    msgin.ErrOverflowDropped, "memory.GroupStore.Add", key, len(g.msgs), s.maxGroupMembers)
```

`Aggregator.Handle` (`aggregator.go:412-415`) returns `store.Add`'s error unchanged, so the fault travels the
runtime's ordinary `RetryPolicy`: retry with backoff, dead-letter on exhaustion. **Fail loud — never silently drop,
never evict, never force-release.**

**Why the same sentinel.** Symmetry with the arm two lines above is worth more than a new `errors.Is` target: a
caller handling *"the store rejected this message because a cap was hit"* handles both with one branch, and both
arms are the same phenomenon (an admission check in `Add`) at two granularities.

**Why WRAPPED, where the existing arm is bare.** `groupstore.go:123` reads `return nil, msgin.ErrOverflowDropped`
with no context. Debuggability is [CLAUDE.md](../../CLAUDE.md)'s stated **core** quality criterion, and
decisively — `msgin.ErrOverflowDropped` already has **four** producers (`queuestore.go:170`, `:175`,
`groupstore.go:123`, `endpoint/consumer.go:576`), so a bare sentinel cannot tell an operator which cap fired. **The
existing bare arm is upgraded to the same shape in the same commit** — fix the class, not the instance. This is a
message-text change only; `errors.Is` is unaffected and no test asserts the string (the plan verifies this before
the edit).

**Why NOT `Permanent`-wrapped.** Verified: `IsPermanent` (`reliability.go:35-46`) matches `*permanentError`,
`ErrPayloadType`, `ErrPayloadDecode` and `ErrPayloadTooLarge` — `ErrOverflowDropped` is **not** among them, so it
classifies as **transient** today. That classification is *correct* for this fault and not merely inherited: an
over-cap `Add` **can** succeed later, once the group releases and `SettleGroup` deletes the claimed prefix, or once
the reaper expires it. `Permanent` would deny a retry that works. For a group whose release genuinely never fires,
the retry budget exhausts and the message dead-letters with a cause naming the group and the limit — loud, bounded
and diagnosable.

**REVERSIBILITY:** the wrap is one `fmt.Errorf`; the classification is a one-line change if the audit disagrees.

### D-AF — `memory` counts live + claimed; `sql` counts live

**Decision.** `memory.GroupStore` bounds `len(g.msgs)` — **live plus claimed** — because that slice is what the
**process** retains. `sql.GroupStore` bounds the **live** member set (`claimed_epoch IS NULL`), because for `sql`
the claimed members are retained by the **database**, not by the process, and the quantity being bounded is what
one `Add` drags into the process heap.

**The stated principle, from which both follow:** *a store bounds what it retains.* The two rules differ because
the two stores retain different sets, not because the contract is inconsistent.

**The cost, stated rather than discovered later.** `ClaimGroup` sets `g.claimedLen = len(g.msgs)` without shrinking
`g.msgs`; the trim happens in `SettleGroup` (`groupstore.go:172-175`). So between claim and settle, a `memory`
group sitting at exactly the cap **rejects new arrivals for that key**, even though its live residual is empty.
That window is bounded (release settles or abandons on every path, including a panic-safe defer-abandon) and
recoverable (`ErrOverflowDropped` is transient — D-AE, so the retry succeeds after the settle). It is documented on
the option's godoc.

> 🔴 **This asymmetry is the finding most likely to be reversed by the audit.** The uniform alternative — both
> stores count live only — makes the SPI contract one sentence instead of two, at the price of letting `memory`
> transiently retain up to 2× the cap across a claim boundary. **REVERSIBILITY: one line per store**, plus one
> sentence in the SPI godoc. See Spec 017 §8 item 1.

### D-AG — the SQL bound is enforced INSIDE the dialect's transaction, and `AddMember` takes `maxMembers`

**Decision.** `GroupDialect.AddMember` gains a trailing `maxMembers int` parameter. Each dialect, **inside the
transaction it already opens and after the statement that takes the group row lock**, counts the live members and —
if the cap is exceeded — returns the D-AE error, letting the existing `RunInTx` wrapper roll the transaction back.
**Nothing is committed.**

**Why not the one-line alternative.** Three enforcement points exist:

| | Bounds the durable table? | Bounds the raw fetch? | Bounds the decode? | SPI change | Atomic across instances? |
|---|---|---|---|---|---|
| (A) count in `GroupStore.Add` after `AddMember` returns | **no** | **no** | yes | none | **no** |
| (B) (A) + `LIMIT max+1` on the dialect's SELECT | no | yes | yes | signature | no |
| **(C) count in-transaction, roll back — CHOSEN** | **yes** | **yes** | **yes** | signature | **yes** |

(A) is a bound that halves the peak and fixes nothing: the member row is already committed and `[]MemberRow`
already carries every live member's framed payload bytes. **A remedy that leaves the actual lever in place while
reading as "bounded" is the false-safety inversion Spec 016 §1.1 and §3.8 both warn about** — and it is the same
mistake as ADR 0032's twice-emptied safety causes (c) and (d), where a verdict outran its evidence.

(C) is additionally the only option that is **atomic across processes**, which D-AG's multi-instance obligation
requires: the check sits after the group row lock (`postgres/groupdialect.go:105-112`, whose own comment records
that this statement *"serializes same-key adds (H1)"*), so two instances adding concurrently to a group at `cap-1`
serialize — one commits, the other counts `cap`, exceeds, and rolls back. Under (A) each could commit past the cap.

**Why the breaking SPI change is affordable.** `GroupDialect`'s godoc states *"This is a pre-1.0 (v0) contract that
may still evolve"* (`groupdialect.go:106`); the project is **unreleased, untagged, with no consumers**
([CLAUDE.md](../../CLAUDE.md) project status); and `grep -rn 'msginsql.GroupDialect'` finds only first-party
implementers. Five call sites change: `postgres`, `mysql`, `sqlite`, `harness/groupstore.go:345`, and
`groupdialect_fake_test.go:137`.

**Why the dialects return `msgin.ErrOverflowDropped` directly** rather than a new `msginsql` sentinel or an
exported helper: D-X's *mint none* rule, and an exported `msginsql` helper taking an `int` would itself become a
class-gate key. The three dialect modules already depend on the `msgin` root module transitively through
`msginsql`, so the import is **zero net dependency** — the plan verifies `go mod tidy` leaves each dialect's
`go.mod` unchanged.

**REVERSIBILITY: this is the expensive decision.** It roughly doubles the increment (four extra modules:
`postgres`, `mysql`, `sqlite`, `harness`, plus `dbtest` running them). Falling back to (A) plus a named follow-up
is defensible if the increment must be small — **but that choice must be made before Plan 031 starts, not at task
6.** See Spec 017 §8 item 2.

### D-AH — the `msgin.MessageGroupStore` SPI states the per-group bound as a contract requirement

**Decision.** `MessageGroupStore.Add`'s godoc (`groupstore.go:41-52`) gains:

> An implementation MUST bound the number of members it retains for a single group, and MUST report an `Add` that
> would exceed that bound as `msgin.ErrOverflowDropped` rather than growing without limit. The Aggregator's release
> strategy cannot supply this bound: three of its four paths are a caller-supplied closure or a message header, so
> the store is the only site that observes every member. The exact set counted is implementation-specific and MUST
> be stated in the implementation's godoc (D-AF).

**Why it belongs on the SPI and not only on the two stores.** `MessageGroupStore` is public and
third-party-implementable — a future `pgx`, Redis or NATS group store inherits the requirement without `routing`
or the core being edited. Stating it here is what makes D-AC's "the store is the right place" true of *every*
store rather than of the two we happen to ship.

**Honest limitation.** A contract addition is **not compiler-enforced**: a third-party store that ignores it still
compiles and still satisfies the interface. The increment's answer is conformance coverage, not enforcement — both
first-party stores get a case driven through the **interface type** so it is copyable by an implementer, and the
`harness` kit gains one so all three dialects are held to it.

**REVERSIBILITY:** free — it is prose. Removing it would only lose the guidance.

### D-AI — godoc cross-references close the inference gap on the three unbounded release paths

**Decision.** Four godoc edits, no logic:

1. `routing.WithCompletionSize` gains a pointer to the store-level bound.
2. `routing.WithReleaseStrategy` gains a note that it **bypasses `completionSizeCeiling` entirely** and relies on
   the store's member cap.
3. `routing.WithReleaseWhen` gains the same note (it is sugar over the above and inherits the bypass).
4. `defaultRelease` is **unexported**, so its disclosure goes on `msgin.HeaderSequenceSize`'s declaration and on
   `routing.NewAggregator`'s godoc — where a caller relying on the default release path actually looks.

**Why this is a decision and not a chore.** The backlog entry that prompted this increment
([`docs/HANDOVER.md`](../HANDOVER.md) §6 item 7) proposed *"a one-sentence cross-reference on `WithCompletionSize`'s
godoc"* as the **interim** measure. This ADR ships the real bound **and** the cross-references, because a reader who
sees a ceiling on one option and none on its two siblings will infer the siblings are safe. That inference is what
made this defect survive Plan 029's five audit rounds.

**REVERSIBILITY:** free.

### D-AJ — a DEFAULT cap is legitimate here, where a default BYTE cap was not

**Decision.** Ship `1 << 16` as a **default**, not as an opt-in.

**The gate this must clear.** [CLAUDE.md](../../CLAUDE.md)'s Sensible-defaults escape clause — *"If no value can be
safe for an unknown caller (e.g. a byte cap that depends on the caller's legitimate payload size), make it
explicit/opt-in … rather than guessing a default that lulls the caller into a false guarantee"* — is exactly what
ADR 0032 **D-AB** used to **defer** three byte knobs. So the question is live.

**The strongest objection, stated before it is answered.** Spec 016 §3.4's own words about the ceilings are *"None
of these is reachable by a correct program. They are the boundary between a workload and a typo."* That justifies
65,536 as a **typo boundary**. This decision reuses the number as a **runtime default** — a policy correct programs
live under. Those are different jobs, and a caller aggregating a 200,000-row export into one group is doing
something sensible that this default breaks.

**Three disanalogies with the byte case, and a direct answer.**

1. **The unit is caller-owned and countable, not remote-driven.** A byte cap is unsafe to default because the
   quantity belongs to an unknown peer's payload. A member count belongs to the caller's own aggregation design.
2. **"Off" is not a safe state here.** For the byte knobs, leaving the option unset already **is** the documented,
   safe default (Spec 016 §3.8, measured). For per-group members the unset state is **unbounded** — it is the
   defect. Opt-in-only protects only callers who already know about the problem.
3. **A larger safe value exists.** The byte case had none — that is why it was deferred. Here the escape hatch is
   `WithMaxGroupMembers` up to `1 << 20`, 16× the default, with a typed error at its own boundary.

**The answer to the objection.** The ceiling→default transfer holds *a fortiori*: if no correct program reaches
65,536 members **via `WithCompletionSize`** (the ratified claim), then no correct program reaches 65,536 members
**at all** — the quantity and the unit are identical, and only the enforcement site differs. A group does not
become a different object because its release threshold arrived as a closure. What changes at the boundary is the
**failure mode**, and D-AE makes it loud, typed, retryable and named rather than a silent guess.

**The behavioral break is accepted and stated.** A caller aggregating >65,536 members per group changes behavior.
The project is pre-v1, unreleased, untagged, with no consumers — breaking changes are free. **Free is not
unstated:** it is recorded in Spec 017 §3.10, on both options' godoc, and in Plan 031's final task.

**REVERSIBILITY:** one constant per store. See Spec 017 §8 item 3.

### D-AK — bounded-but-stuck is accepted; liveness stays opt-in via `WithGroupTimeout`

**Decision.** With the cap in place, a group whose release never fires is **memory-bounded but permanently stuck**:
it holds exactly `maxGroupMembers` members forever, and every subsequent member for that key is rejected, retried
and dead-lettered. **This is accepted.**

**Why it is a strict improvement.** On the two axes that matter it strictly dominates the status quo — memory goes
from unbounded to bounded, and observability goes from *silent until the process dies* to *one typed, named error
per rejected member in the operator's dead-letter store.* The third axis, liveness of the stuck group, is
**unchanged**: it never released before and it does not release now. **The cap does not, and is not intended to,
provide liveness.**

**The remedy already ships and stays opt-in:** `routing.WithGroupTimeout` + `routing.WithExpiredGroupChannel`.

**Rejected: making `WithGroupTimeout` mandatory.** It is a second, larger behavioral break; it requires a paired
expiry channel (`NewAggregator` returns `ErrExpiryChannelRequired` otherwise, `aggregator.go:360-362`), so
mandating it forces every caller to provision one; and choosing a default timeout for an unknown aggregation
workload is the *"no value can be safe for an unknown caller"* case **for real** — the very clause D-AJ argues does
not apply to the member count. Recorded as a follow-up: whether *cap without timeout* deserves a construction-time
diagnostic.

**REVERSIBILITY:** free — it is a stated acceptance, not code.

### D-AL — the class gate is extended by hand, and its blind spot is STATED rather than widened

**Decision.** The gate is updated mechanically for the two new options, and a **fifth accepted limitation** is added
to its header. The AST scan is **not** widened to func-typed parameters.

**The mechanical part.** `memory.WithMaxGroupMembers` and `sql.WithMaxGroupMembers` are exported, `Recv == nil`,
`int`-parameter functions in **root-module** packages, so half 1 finds them: **17 → 19 keys**. Half 2 gains two
rows in the `fixed` arm (both constructors reject `1<<62`), making the arms
**11 fixed + 1 rejects + 3 deferred + 6 safe = 21 rows = 19 AST keys + 2 manual rows.**

**The stated limitation, verbatim for the header:**

> A bound that does not arrive as an **integer parameter** is invisible to half 1: a func-typed option
> (`*ast.FuncType`), a named func type (`*ast.Ident` — the same path as `type Bytes int64`), or a threshold read
> from a **message header** (no parameter at all). Spec 017 §1.4 enumerates the three that exist today —
> `routing.WithReleaseWhen`, `routing.WithReleaseStrategy` and `routing`'s header-driven `defaultRelease` — and
> moves their enforcement to the **store**, which observes every member regardless of how the threshold arrived.

**Why not widen the scan.** Catching `*ast.FuncType` would find `WithReleaseWhen` and miss `WithReleaseStrategy`
(a named type), and could never express `defaultRelease` (no parameter). It would also have to decide *which*
func-typed options are sizing knobs — a judgement `go/ast` cannot make without `go/types` and a loadable build of
all eight modules, the exact coupling the filesystem walk exists to avoid (ADR 0032 **D-AA**). **A gate that covers
one of three while reading as complete is worse than a stated limitation** — that is the same inversion D-AB was
written to stop.

**One more thing stated so the next audit need not re-derive it.** `GroupDialect.AddMember` gains an `int`
parameter, but it is a **method**, excluded by the ratified `Recv == nil` boundary (ADR 0032 **D-AA**), and under
**D-AB**'s criterion it is **not** a class member: `maxMembers` **is** the bound, not a quantity bounded by
something else. No manual conformance row is required.

**REVERSIBILITY:** the key set and arm table are data in one test file.

## Consequences

**Positive.**

- **The defect class is closed by construction, not by enumeration.** The bound sits at the one site every member
  passes through, so a **fifth** release path — or a caller's own exotic strategy — is governed without any
  document being edited. This is the project's stored lesson *"fix the class, not the instance"* applied to a class
  whose members cannot all be listed.
- **The SQL store is bounded for the first time**, and D-AG's in-transaction placement makes that bound exact,
  durable and **atomic across horizontally-scaled instances** — properties the cheap alternative does not have.
- **The two overflow arms in `memory.GroupStore.Add` become symmetric**, and both gain a message naming which cap
  fired. Four producers of `ErrOverflowDropped` stop being indistinguishable in a log.
- **The SPI carries the requirement forward** to `pgx`, Redis and NATS group stores that do not exist yet.
- **The class gate's blind spot becomes documented rather than latent.** A future contributor adding a func-typed
  sizing option reads the limitation instead of trusting a green run.

**Negative / accepted costs.**

- **A behavioral break** for any caller aggregating >65,536 members per group (D-AJ). Free at pre-v1; stated.
- **A breaking `GroupDialect.AddMember` signature change** across five call sites in four modules (D-AG), roughly
  doubling the increment. Free at pre-v1; the SPI's own godoc reserves the right.
- **`msgin.ErrInvalidCapacity` reaches SIX producers** across four units — queue depth, group count, channel
  buffer, group members (D-AD). D-X's argument still holds, but the margin is thinner and a seventh needs an ADR.
- **A stuck group stays stuck** (D-AK). The cap converts an OOM into a dead-letter stream; it does not release
  anything.
- **`memory`'s claim window transiently rejects arrivals** for a group at exactly the cap (D-AF). Bounded and
  retryable, but it is a real, observable behavior a caller may report as a bug.
- **The invariant *default `maxGroupMembers` ≥ `completionSizeCeiling`* is NOT mechanically enforced.** Both
  constants are unexported and live in different packages, so no blackbox test can compare them; and executing a
  65,536-member group to prove it behaviorally costs 8.6 s and 48.3 GiB of churn. The defence is a cross-reference
  comment on each constant plus a grep in the final task. **This is a stated limitation, not a closed one** (Spec
  017 §8 item 4).
- **Neither store's quadratic per-`Add` cost is fixed.** The cap bounds the damage; `memory` still clones the group
  on every call and `sql` still re-fetches and re-decodes every live member on every arrival. Both are named
  follow-ups.

**Neutral / to watch.**

- **The `memory`/`sql` counting asymmetry (D-AF)** is defensible per-store but reads as two rules for one option
  name. If the audit prefers uniformity, the change is one line per store and one sentence in the SPI.
- **Nothing in the library can enforce that horizontally-scaled instances agree on the cap** (Spec 017 §7.1). It is
  documented as an operator requirement, in the same family as the existing `WithGroupLeaseTTL` coherence
  requirement.
