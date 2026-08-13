# Plan 027 — adversarial design audit, ROUND 6

**Artifact:** audit record for the Plan 027 design bundle.
**Traceability:** audits [Spec 014](../specs/014-core-package-layout.md) · [Plan 027](027-core-package-layout.md) ·
[ADR 0027](../adrs/0027-core-package-restructure.md) · [ADR 0028](../adrs/0028-channel-interface-segregation.md) ·
[ADR 0029](../adrs/0029-eip-lexical-alignment.md) · [ADR 0030](../adrs/0030-reply-channel-exclusivity-probe.md) ·
[RFC 0002](../rfcs/0002-eip-alignment.md). Prior rounds: [round 1](027-audit-round-1.md), [round 2](027-audit-round-2.md)
(§E verified-sound, not re-opened); rounds 3–5 are recorded in [`docs/HANDOVER.md`](../HANDOVER.md).
**Produces:** decisions **D-L**, **D-M**, and a revision of **D-K**.

**Date:** 2026-07-28 · **Tree audited:** `aae6160`, working tree clean.
**Code pin:** `dadc775`. `git diff --name-only dadc775..aae6160 | grep -v '^docs/'` → `CLAUDE.md` only, so **all root
code is byte-identical to `dadc775`** and every measurement below is pinned there.

## Provenance

Four independent Opus subagents, four lenses, run in parallel against the committed bundle. **All four returned
`NEEDS-REVISION`: 25 blockers, 34 minors.**

| Lens | Verdict | Blockers | Minors | New this round |
|---|---|---|---|---|
| Consistency & measurement | NEEDS-REVISION | 9 | 10 | — |
| Executability | NEEDS-REVISION | 7 | 10 | — |
| Design | NEEDS-REVISION | 5 | 5 | compile-proved in a throwaway worktree |
| **Meta / git-facts / traceability** | NEEDS-REVISION | 4 | 9 | **yes — added to close the gap that survived rounds 1–5** |

The meta lens exists because a false `main` SHA survived five lenses: all five were briefed on the bundle
*documents*, and nobody's brief covered the handover's own git facts. That lens confirms **the git facts are now
fully correct** (see §3) and found its four blockers in files no prior round had opened.

---

## §0 · The class round 6 exposes

> **Rounds 4 and 5 swept Spec 014, Plan 027 and ADR 0027 repeatedly. They never opened ADR 0028's Consequences,
> ADR 0029's Consequences, or the Plan's Risks table.**

Five of the consistency lens's nine blockers live in exactly those three places, and both meta B2 and consistency
B4 are the *same* withdrawn claim surviving in the Risks table. Round-5 BLOCKER 3 already named this shape
(*"swept in ADR 0027's Context, left stale in its Consequences"*); it was fixed in ADR 0027 and nowhere else.

**Counter-rules adopted for the round-6 fix pass** (in addition to those from rounds 4–5):

1. **A decision's amendment surface includes every Consequences and Risks section in every bundle document** —
   not the sections that state the decision. When a decision closes or a claim is withdrawn, grep all seven
   documents for the claim, including the sections nobody edited.
2. **A withdrawn argument must be swept where it is used as a live *mitigation*, not only where it is stated as a
   fact.** Round 5 swept three statements of the `Test*` name-set proof and missed the one instruction that told a
   worker to rely on it.
3. **A godoc gate must scan backwards from the declaration.** A doc comment sits *above* its declaration; every
   `grep -A` gate on a godoc is reading the wrong lines. Prefer `go doc`, which extracts the comment by
   construction so the direction bug cannot recur.
4. **Every verify command must be demonstrated RED at the start of its task.** A gate that is green on the
   untouched tree ticks with zero work.
5. **"REGENERATED" is a provenance claim.** A block a re-run contradicts falsifies not just itself but every
   unverified "generated" label in the bundle.

---

## §1 · DECISIONS TAKEN THIS ROUND

### D-L — `ExclusiveSubscribable.SingleSubscriber()` is an end-to-end policy predicate, and is lifetime-invariant

**Supersedes** ADR 0030 §1's handle-local definition. **Prompted by** design B2 + B3, both compile-proven.

ADR 0030 defined the predicate as a property of *this local handle* (`0030:71`, `0030:227`). Three defeats were
demonstrated by implementing D-J in a throwaway worktree at `aae6160` and running ADR 0030's own guard:

```
bare plain pub-sub                      -> REJECTED ErrSharedReplyChannel
A: struct{ msgin.SubscribableChannel }  -> ACCEPTED (probe absent)
B: struct{ *PublishSubscribeChannel }   -> ACCEPTED (probe reports exclusive)
   ...after 2 Subscribes: len(subs)=2, SingleSubscriber()=true
C: state-reading probe (0 subscribers)  -> ACCEPTED (probe reports exclusive)
   ...after 2 Subscribes: n=2, SingleSubscriber()=false (both accepted, no error)
```

- **B** — a wrapper embedding `*PublishSubscribeChannel` **inherits `SingleSubscriber` by method promotion** and
  reports `true` while its own `Subscribe` fans out to 2. ADR 0030 §5 presents embed-and-shadow as the *remedy*
  and never states that promotion is also the *hazard*.
- **C** — a third-party probe that reads its own live subscriber count is exactly what the godoc's *"while one is
  registered"* phrasing invites. It answers `true` at construction, then admits N with **no error at all**.
- **A** — `struct{ msgin.SubscribableChannel }`, the idiomatic one-line decorator for logging/metrics/tracing,
  promotes `Send` and `Subscribe` but **not** `SingleSubscriber`. The *same* fan-out channel that is rejected bare
  is accepted when wrapped. This falsifies §4's cheapness premise (*"only a third-party implementation can be
  unknown to it"*), which counts **declarations**, not the values a caller passes.

C also falsifies ADR 0030 `:194-196` (*"that path already yields `ErrChannelSubscribed`"*): in cases B and C
nothing yields `ErrChannelSubscribed`; both `Subscribe` calls succeed silently.

**Decision.** Three changes, all to the contract rather than the implementation:

1. **Redefine the predicate end-to-end.** `SingleSubscriber()` reports whether **this exchange will be the sole
   recipient of every message sent to this channel** — a statement about the channel's **policy**, not about how
   many subscribers it currently has. An implementation **MUST NOT** compute it from a live subscriber count. A
   channel whose deliveries reach other processes (a broker subject, Redis pub/sub, an SSE stream) **MUST** return
   `false` even when its local handle admits one subscriber.
2. **Require invariance, not merely concurrency-safety.** The value **MUST be constant for the channel's
   lifetime**; msgin calls it once, at construction, and treats the answer as an invariant. Concurrency-safety is
   the *weaker* property — the failure mode is TOCTOU, and a race-free `atomic.Load(&n) == 0` is concurrency-safe
   and still lies. Both requirements are stated; neither replaces the other.
3. **Document both embedding directions and the wrapper hole.** Method promotion is a hazard as well as a remedy,
   and interface-embedding decorators silently opt out of the probe.

**Why this is strictly better, not merely safer.** Under the handle-local definition ADR 0030 §Topology concludes
Topology 2 (a broker-backed reply channel) is **undetectable**, because such an adapter's honest local answer is
`true`. Under the end-to-end definition its honest answer is `false`, so **Topology 2 becomes detectable by the
design the ADR currently says cannot detect it** — with the same two in-tree implementations
(`DirectChannel` → `true`; `PublishSubscribeChannel` + `WithSingleSubscriber` → `true`) and no added cost.

**Godoc wording (normative — Task 9.6 writes it, Task 11b gates it):**

```go
// SingleSubscriber reports whether THIS exchange will be the sole recipient of
// every message sent to this channel. It is a statement about the channel's
// POLICY, not about how many subscribers it currently has: an implementation
// MUST NOT compute it from a live subscriber count. A channel whose deliveries
// reach other processes (a broker subject, a Redis pub/sub channel, an SSE
// stream) MUST return false even when its local handle admits one subscriber —
// the core has no other way to learn that replies fan out beyond this process.
//
// The value MUST be constant for the lifetime of the channel. msgin calls it
// once, at construction, and treats the answer as an invariant; a value that can
// change afterwards makes the check a TOCTOU race the core cannot detect.
// Implementations must also be safe for concurrent use.
//
// EMBEDDING CUTS BOTH WAYS. A type that embeds a *channel.DirectChannel or a
// *channel.PublishSubscribeChannel inherits SingleSubscriber by method
// promotion, so it reports on the EMBEDDED channel even when it overrides
// Subscribe with its own multi-subscriber dispatch. A wrapper that changes
// subscription behavior MUST declare its own SingleSubscriber.
```

`NewChannelExchange`'s godoc gains, as part of the accepted-no-probe arm:

> A reply channel that WRAPS another channel by embedding the `msgin.SubscribableChannel` interface does not
> inherit `SingleSubscriber`, so it is accepted under this arm even when the channel it wraps would be rejected. A
> decorator over a fan-out channel must forward `SingleSubscriber` explicitly (or embed the concrete type) if it
> wants the check applied.

**Consequences.** ADR 0030 §4's cheapness argument is restated: the accept-unknown arm is reachable from
**in-tree types via wrapping**, not only from outside the repo. ADR 0030 `:194-196` is deleted — it is false for
the wrapper and state-reading cases.

---

### D-M — a deterministic endpoint fault carries its own retry classification; `ErrNilFunc` is `Permanent`

**Generalizes** D-K from one sentinel to the class. **Prompted by** design B4.

ADR 0029 §5.0b's reasoning — `IsPermanent` is a **closed enumeration**, so a deterministic fault outside it is
classified transient and retried — is correct and **is not specific to `ErrExprResultType`**. Measured at
`aae6160`:

```
IsPermanent(msgin: nil endpoint function            ) = false
IsPermanent(msgin: no route for message             ) = false
IsPermanent(msgin: payload is not of the expected type) = true
IsPermanent(msgin: message has no correlation key   ) = false
```

End-to-end, a `transform.Transform(nil)` step over a `memory` broker with `RetryPolicy{MaxAttempts: 3}`:

```
nil-func step: OnRetry=2  OnDeadLetter=1  OnInvalidMessage=0  (IsPermanent=false)
```

A nil endpoint function — the most deterministic fault the library can produce, identical on every redelivery for
the process's lifetime — consumes the full retry budget, lands in the **dead-letter** sink instead of the
**invalid-message** sink, and, via `endpoint/consumer.go:614`
(`c.safeRecord(md.Msg.ID(), err == nil || msgin.IsPermanent(err))`) and `endpoint/consumer.go:733`, **records an
unhealthy signal that trips the circuit breaker**. One mis-wired `Filter(nil)` opens the circuit for the whole
consumer.

The tree already contains the correct precedent, with D-K's exact rationale, written before this window —
`routing/aggregator.go:151-160` wraps `ErrNoCorrelation` in `msgin.Permanent` *"so the message would be retried to
the dead-letter sink instead of diverted to the invalid-message channel"* without it.

**The discriminator (this is the rule, not the list).** Classify by **when the fault's inputs are fixed**:

- **Fixed at construction, or by the message itself → `Permanent`.** It cannot change on redelivery.
- **Evaluated per message against caller-supplied, possibly-mutable state → transient.** It may legitimately
  resolve on redelivery.

Applied:

| Sentinel | Inputs fixed | Classification | Rationale |
|---|---|---|---|
| `ErrNilFunc` (all producers) | at construction — the nil is captured in the closure | **`Permanent`** | Verified: `nilFuncStep` closes over nothing; `Router.pick` is set once in `NewRouter`. Nothing can make it non-nil later. |
| `ErrExprResultType` | at construction (expression) + by `T` | **`Permanent`** | D-K, unchanged in substance. |
| `ErrNoCorrelation` | by the message's headers | **`Permanent`** (already) | `routing/aggregator.go:160` — precedent. |
| **`ErrNoRoute`** | **per message, by caller-supplied `pick`** | **transient — UNCHANGED** | `routing/router.go:48-56`: `pick` is a caller function evaluated per message; it may consult a routing table, feature flag or lookup service that changes. A message unroutable now may be routable after a config reload. `WithDefaultChannel` is the documented way to make the outcome deterministic. |

**Scope.** Five producers become `msgin.Permanent(msgin.ErrNilFunc)`:

| Site | Verified at `dadc775` |
|---|---|
| `endpoint/helpers.go:21` | `nilFuncStep` |
| `routing/helpers.go:23` | `nilFuncStep` (package-local copy) |
| `transform/transformer.go:38` | `nilFuncStep` (package-local copy) |
| `routing/router.go:48` | `Router.Handle`, `r.pick == nil` |
| Task 9's `Predicate.And` / `Or` / `Not` | new — see below |

**Task 9's combinators are amended by this decision.** Plan `:490-494` decided they degrade to
`(false, msgin.ErrNilFunc)` **at evaluation, per message** — three *new* producers of a bare, deterministic
`ErrNilFunc` on the retry hot path, authored after D-K identified the class. They now return
**`(false, msgin.Permanent(msgin.ErrNilFunc))`**, and Task 9's hot-path branch list gains a case asserting
`msgin.IsPermanent(err)` on a combinator's nil result.

**Debuggability (design M3).** `errors.Is` is preserved, but the bare sentinel collapses six distinct nil positions
into the single string `msgin: nil endpoint function` — no indication of `And` vs `Or` vs `Not`, receiver vs
argument, or which link of `p.And(q).Or(r)`. CLAUDE.md requires *"typed, wrapping errors that name the offending
field/input"*. Each combinator therefore wraps with context:

```go
fmt.Errorf("%w: routing.Predicate.And: nil argument", msgin.Permanent(msgin.ErrNilFunc))
```

and each combinator's godoc states that `errors.Is(err, msgin.ErrNilFunc)` still matches.

**Consequences.** This is a **behavior change to shipped code** (four existing producers), so it joins Spec §2.1's
table — see finding P-B6 below, which already raises that table from four rows to five; D-M makes it **seven**
(D-J is row 5, `ErrNilFunc` permanence is row 6, the combinator wrapping is row 7 only if counted separately —
recorded as one row, "deterministic endpoint faults are Permanent"). ADR 0029 §5.0b gains a closing paragraph
naming the class and its full in-tree instance list so it reads as a rule rather than a one-off.

---

### D-K (REVISED) — the expr providers wrap `msgin.ErrPayloadType`; `expr.ErrExprResultType` is not declared

**Revises** the D-K recorded at ADR 0029 §5.0b on 2026-07-28. **Prompted by** design M4.

ADR 0029 §5.0b argues a result-type mismatch is *"the expression-domain **twin** of `ErrPayloadType`"*, then has
the provider declare its own `expr.ErrExprResultType`. §5.0c records the resulting cost — future CEL/starlark
providers each mint another sentinel, callers get no shared `errors.Is` target — and offers two escapes, **neither
of which is "wrap the twin"**. The obvious alternative was never considered; the gap is the silence.

**Decision.** The provider returns

```go
fmt.Errorf("%w: expr result %T is not %T", msgin.ErrPayloadType, got, want)
```

and **`expr.ErrExprResultType` is not declared at all.**

**Why.** One shared `errors.Is` target for every future expression provider; the correct retry classification for
free (`ErrPayloadType` is already in `IsPermanent`, so no `msgin.Permanent` wrap is needed and D-K's classification
concern dissolves); no root change; no new import edge. `ErrPayloadType`'s godoc (`errors.go:6`) is already
domain-generic — *"a `Message[any]` payload cannot be asserted to T"* — and a result-type mismatch is that same
statement.

**Consequences.** D-I is **unaffected**: `ErrInvalidExpression` still leaves root and the `expr` module still mints
it with the `msgin/expr:` prefix (it is a construction-time fault with no root twin). The projected sentinel count
changes: Task 12's `43 − 2 + 1 = 42` becomes **`43 − 2 + 1 = 42`** for root (unchanged — root loses both, the
`expr` module declares one), but the *`expr`-module* sentinel count drops from 2 to **1**. §5.0c's two escapes are
withdrawn as moot for this sentinel and retained only for `ErrInvalidExpression`.

---

## §2 · BLOCKERS, by target file

De-duplicated across lenses. `C-` consistency, `E-` executability, `D-` design, `M-` meta.

### ADR 0029 — `docs/adrs/0029-eip-lexical-alignment.md`

- **C-B1 · `:392-397`** — the Consequences still declare **D-I an OPEN decision**, present both options as live,
  and assign it to **Task 10**. The header (`:24-25`) and §5.0a (`:249`) both record it settled with option B, and
  the deletion belongs to **Task 9.5**. An implementer reading Consequences defers a Task 9.5 checkbox and
  re-opens a settled decision. → Replace with the decided outcome, or delete and forward-point to §5.0a.
- **C-B2 · `:398-399`** — *"Seven survive today in `errors.go`, `routing/splitter.go`, and
  `routing/aggregator.go`"* — the claim rounds 4 and 5 established is fiction. Re-measured: arm 2 returns exactly
  `WithRelease`, one survivor, `routing/aggregator.go:316` only; `errors.go` and `routing/splitter.go` contribute
  **zero**. Spec §8.1 and Plan §9.5.1 both carry `ROUND-4 CORRECTION` blocks saying so; the ADR does not. →
  Replace with the measured statement.
- **C-B3 · `:393`** — a **third** line-number pair for the two sentinels (`161`/`183`), and both hold unrelated
  godoc. Correct is `errors.go:180` / `:206` (declarations), per `decls.go`. → Cite the declarations.
- **§5.0b** — record **D-M** (the class) and **revised D-K** (wrap `ErrPayloadType`). Add the
  `NOT-YET-IMPLEMENTED` marker to §5.0b as §5.0a already carries.

### ADR 0028 — `docs/adrs/0028-channel-interface-segregation.md`

- **C-B6 · `:337-344`, `:367`** — the Consequences are un-amended by D-J and assert the **opposite** of ADR 0030:
  that two exchanges over one pub-sub reply channel *"still compiles, still runs, and still silently hands every
  correlated reply to the second exchange's unmatched-reply sink"*, that `NewChannelExchange` *"does **not** opt
  its reply channel in — deliberately"*, and that an exchange-side opt-in is *"**not this window**"*. ADR 0030 §3/§5,
  Spec §5.1 and Task 9.6 make it exactly this window. `:367`'s *"opt-in-enforceable … mitigated rather than
  eliminated"* is likewise superseded. The `AMENDED by ADR 0030` banner is present at `:26` and `:234` and **stops
  before Consequences**. → Add the marker inside the Consequences quote-block and restate the residual as closed
  by D-J.
- **M-4 · `:146`, `:149`** — `NewChannelExchange` cited at `endpoint/exchange.go:223`; it is **`:225`**
  (`:223` is a godoc tail). Both rows of §4a's table. Round-4 B7 fixed the Spec echo, not the ADR's.

### ADR 0030 — `docs/adrs/0030-reply-channel-exclusivity-probe.md`

- **D-B2 / D-B3** — implement **D-L** (§1 above): redefine §1's godoc end-to-end, add the invariance requirement
  alongside concurrency-safety, add the promotion hazard and the interface-wrapper hole, restate §4's cheapness
  argument, **delete `:194-196`**.
- **C-M7 / E-M6 · `:178-184`** — still carries *"for its default-fan-out case"*, the framing round 4 ruled
  **inexpressible** (the per-case field is `opts []channel.PubSubOption`, applied at `exchange_test.go:444`, while
  `exA`/`exB` sit in the shared `t.Run` body). Spec §5.1 and Plan Task 9.6 are corrected; the ADR that Task 9.6
  orders read **first** is not. Also claims both constructions *"assert `require.NoError`"* — only `exA` does
  (`:447`); `exB`'s error is captured into `secondErr` (`:453`). → Propagate the round-4 correction.

### Spec 014 — `docs/specs/014-core-package-layout.md`

- **D-B1 · `:1926-1929`** — §10 still asserts the probe *"adds **no** cross-instance state and makes **no**
  cross-instance claim"* — **the exact sentence ADR 0030 `:204-207` formally retracts**. Round 4's fix said "add
  Topology 2 to ADR 0030 §Topology **and** Spec §10's D-J bullet"; only the ADR was edited. `grep` for
  `shared external|Topology 2|broker-backed|local handle` returns **zero hits in the spec and the plan**. The two
  normative documents now contradict each other on the one CLAUDE.md-mandatory multi-instance review. → Import the
  ADR's two-topology treatment; end on *"a truthful local answer is indistinguishable from a safe one"*. Under
  **D-L** this section is rewritten again: Topology 2 becomes **detectable**, and the bullet says so.
- **E-B6 · `:109`, `:137`, `:1837`** — **D-J is a fifth behavior change and §2.1's table has four rows.** The
  D-I/D-J pass propagated D-J into twelve other spec locations and skipped §2.1. Load-bearing: `:109` says
  *"Out of scope: any behavior change, with the four exceptions §2.1 enumerates"*; AC-5 (`:1837`) says *"No test's
  assertions change, outside §2.1's four exceptions"* while Task 9.6 requires changing exactly such an assertion;
  Plan `:123-127` tells a worker who finds themselves rewriting an assertion to **stop and report it**. *An
  implementer executing Task 9.6 literally is instructed to stop.* → Add row 5 (D-J) **and row 6 (D-M)**; sweep
  the cardinality word at Spec `:109`, `:137`, `:1837`, Plan `:123`, `:724`, `:1086`, ADR 0028 `:229`.
- **C-B8 · `:458-463`** — the block headed `# REGENERATED at dadc775 (round-5)`, carrying its own note about the
  `sort | uniq -c` ordering, **prints six lines where the command emits five**: `1 adapter/cron/sqlutil.go`
  appears at `:460` **and again at `:463`**. `sort | uniq -c` cannot emit a path twice. The pass that corrected
  the sort order re-typed the block instead of pasting it. → Delete line 463.
- **C-B9 · `:939-947`** (echoed ADR 0027 `:85-87`, Plan `:453`) — the `115 CODE / 39 COMMENT / 0 STRING`
  *"provably exhaustive"* classification rests on `adapt-classify.tsv` and an `adaptscan` tool that are **not in
  the repo** (`find . -name 'adapt-classify*'` → nothing; `docs/plans/027-tools/` holds only `decls.go`,
  `qualify.go`, `symmap.tsv`, `README.md`). The **`0 STRING` arm is the load-bearing half** — the evidence that no
  `msgin.X` hid in a string literal, the class `ErrUnsupportedSource`'s message belonged to. Violates §8.1's own
  rule that *"the tools are committed, not in `/tmp`"*. → **Recommendation: demote.** Rebuilding `adaptscan` to
  re-derive a historical, already-verified-by-compiler measurement is not worth a task; mark §3.6's block, ADR
  0027's echo and Task 7a's *"provably exhaustive"* as a **historical, non-reproducible** measurement citing
  ledger F9.2, and drop the word "provably".
- **E-B7 / D-M5 · `:1877-1884`** — AC-9's *"proved by a case asserting the channel has no subscriber after a
  rejected construction"* has **no owning checkbox**, and is **not expressible blackbox**: the rejected arm needs
  `*channel.PublishSubscribeChannel` (the only in-tree non-exclusive type), whose subscriber count is not public —
  `isEmpty()` is unexported (`channel/pubsub.go:157`) and `Send` with zero subscribers returns `nil` by documented
  design (`:172-173`). → Task 9.6 gains a **second** test fake (`countingSharedChannel`: `SingleSubscriber() →
  false`, `Subscribe` increments a counter) and a fifth table row asserting the counter is 0 after rejection.
- **E-B2 / C-M8 · `:1738-1770`** — **Task 10 breaks the arm-2 sweep**, which §9.5 makes a merge criterion. Arm 2's
  comment side is tree-wide; its declared side enumerates a fixed package list that will not include `expr`. Task
  10 writes `expr/errors.go` whose godoc (quoted verbatim at `:1590-1599`) begins `// ErrInvalidExpression is…` →
  **false survivors**. Not speculative: the existing allow-list exists solely to paper over this same mechanism
  for satellite modules. `:1745` also **mis-describes** the scope gap as a false-*negative* when the allow-list
  proves it generates false-*positives*. → Add `expr` to the declared-side loop (a Task 10 checkbox), correct
  `:1745`, and **do not** allow-list the sentinels — that would re-decorate the gate. Under **revised D-K** only
  `ErrInvalidExpression` is at issue.
- **D-B5 / E-B4 / E-B5 · obligations 10–13** — see the shared gate finding below.

### Plan 027 — `docs/plans/027-core-package-layout.md`

- **M-B1 · `:313`** — the Progress status block says the D-I/D-J pass is *"Uncommitted at this moment"*. It is
  `aae6160`; `git status --short` is empty. The file list was **also incomplete when written** — it omits
  `docs/adrs/0027-*` and `docs/rfcs/0002-*`, both of which that pass modified. Third recurrence, in the block that
  names itself as *"the one place three rounds have failed to apply it"*. → *"Committed as `aae6160`"* + a
  `git diff --name-only`-derived file list. **Invariant:** no status block asserts working-tree state; it names
  the commit that carries the change.
- **M-B2 / C-B4 · `:1086`** — the Risks table still prescribes *"identity is proved by identical `Test*` name sets
  plus a normalised per-file diff"*. Round-5 BLOCKER 2 **withdrew, not repaired**, that argument after measuring
  it in every frame (`ab233d9` 224 · `c83dde9~1` 224 · `c83dde9` 211 · `b6ce7bb` 218 · `dadc775` 221). The Risks
  table is where a worker looks for the mitigation to *apply*, and AC-5 explicitly forbids using it. → Cite the
  normalised per-file diff alone.
- **C-B5 · `:1094`** — the Risks table asserts *"Task 1 preserved the test cases in the ledger that Task 10 must
  satisfy"*. Contradicted twice in the same file: `:152-156` (*"none of the twelve deleted test functions is
  recorded anywhere under `docs/`"*) and `:853-858`'s round-3 `CORRECTED` block (*"a worker following the
  instruction as written would have found nothing and either invented a parity bar or skipped it"*). → Task 10's
  parity bar is `git show ab233d9:expr_test.go` + `expr.go`, **not** the ledger.
- **C-B7 · `:809-816`** — a block presented as pasted `grep -nE '^func '` output shows six lines; the command
  emits **ten**. The four dropped (`compile:35`, `compileGroup:262`, `toGroupEnv:277`, `exprSliceToChildren:418`)
  are **load-bearing in the next paragraphs**: `:818` cites `compile[A]` at `expr.go:35`, `:872` tells the worker
  to read all four, `:878` derives the M-1/M-6 cases from `toGroupEnv`. Round-4 B10's class, fixed at the Task 9.6
  instance and not swept. → Paste all ten, or relabel as a derived summary with the filter shown.
- **E-B1 · `:780-792`** — **Task 10 has no `go.work` checkbox.** The `use` line appears only in a subordinate
  clause (*"necessary but not sufficient"*). CI edit #2 adds `expr` to the workspace job, which runs
  `go build ./...` with the workspace **on**; proven in isolation: a module absent from `use` gives
  `pattern ./...: directory prefix . does not contain modules listed in go.work` (exit 1), and `GOWORK=off`
  succeeds. → Add `add ./expr to go.work's use block` as a prerequisite of CI edit #2.
- **E-B3 · `:850-876`** — the twelve-function parity list is **unexecutable**: five are `Example*` functions named
  for deleted identifiers, and `go vet` — mandated after every move — hard-fails
  (`ExampleFilterExpr refers to unknown identifier: FilterExpr`). The providers are `Predicate`, `RouteFunc`,
  `Transformer`, `SplitFunc`, `Release`, and the old constructors carried option variadics
  (`FilterExpr[A](expr, opts ...FilterOption) (Step, error)`) the new ones do not — composition moves to the call
  site. → Replace with an old→new mapping and state that option-carrying cases move to the base constructor.
- **D-B4 · `:490-494`, `:508-516`** — Task 9's combinators are amended by **D-M**: return
  `Permanent(ErrNilFunc)` wrapped with positional context, and add a hot-path case asserting `IsPermanent(err)`.
- **D-M1 · `:680-683`** — Task 9.6 says *"state the **three arms**"*; ADR 0030 `:230-233` and Spec `:1691` require
  **four** outcomes; Spec AC-9 `:1881` says *"all three acceptance outcomes"*. An implementer writes three and
  Task 11 rewrites the comment. → Defer to Spec §8 obligation 12 (four) and let Task 11b own final wording.
- **D-M2 · guard order** — ADR 0030 `:107` and Spec `:1298` evaluate `ex.SingleSubscriber()` **before**
  `cfg.allowShared`, so `WithSharedReplyChannel()` suppresses the *rejection*, not the *probe* — contradicting
  Plan `:958`, which requires the option's godoc to say it **suppresses the probe**. On a third-party
  implementation that locks or does work in the method, the opt-out still pays for it. → Reorder to
  `if !cfg.allowShared { if ex, ok := …; ok && !ex.SingleSubscriber() { … } }`.

### The shared gate finding — Task 11b, obligations 10–13 (D-B5 + E-B4 + E-B5)

**All four new D-J godoc gates are decorative; three pass with zero work done.** Measured in a worktree where
`ExclusiveSubscribable` was added with ADR 0030's godoc copied verbatim and **no obligation text written at all**:

- **§8.10** `grep -A20 'type SubscribableChannel' channel.go | grep -c ExclusiveSubscribable` → **2**.
  `SubscribableChannel`'s godoc is `channel.go:24-42`, **above** the declaration at `:43`; `-A20` reads `:43-63`.
  The gate cannot see the godoc it gates, and self-satisfies by matching the **new interface's own declaration** —
  which Task 9.6 places in this very file. Every sibling gate uses `-B`; `-A20` is the odd one out.
- **§8.11** `grep -B12 'SingleSubscriber() bool' channel.go | grep -ci concurrent` → **1**, satisfied by the word
  *"concurrent"* occurring incidentally in ADR 0030 §1's own snippet (*"a second **concurrent** subscriber"*). The
  obligation exists to force a concurrency-safety requirement **on implementers**.
- **§8.12** `grep -B30 'func NewChannelExchange' endpoint/exchange.go | grep -c ErrChannelSubscribed` → **1**
  **on the untouched tree**, matching pre-existing prose at `:210-211`. The obligation is that the godoc's *error
  list* (`:221-224`, naming only `ErrNilChannel`/`ErrInvalidReplyTimeout`/`ErrNilSubscription`) enumerate it.
  Round-4 B6's *"a proof that cannot fail"* class, reintroduced by the round-5 pass.
- **§11c-1** `grep -A22 'func WithSingleSubscriber' channel/pubsub.go | grep -i 'single-process|per-process'` →
  **exit 1, unsatisfiable**. The godoc is `:66-82`; the function body is one line at `:83`, so `-A22` reads
  `:83-105`. No correct edit can turn it green.

→ Replace all four with `go doc`-based, property-asserting checks, and add a **"must be red before the edit"**
baseline to every 11b/11c gate:

```bash
go doc github.com/kartaladev/msgin.SubscribableChannel | grep -q ExclusiveSubscribable
go doc github.com/kartaladev/msgin.ExclusiveSubscribable | grep -Eq 'safe for concurrent use' \
  && go doc github.com/kartaladev/msgin.ExclusiveSubscribable | grep -Eq 'constant for the lifetime'
go doc github.com/kartaladev/msgin/endpoint.NewChannelExchange \
  | grep -c 'ErrSharedReplyChannel\|ErrChannelSubscribed\|does not implement\|within this process'   # >= 4
go doc github.com/kartaladev/msgin/channel.WithSingleSubscriber | grep -Eiq 'single-process|per-process'
```

Verified genuinely red today (no change needed): §8.1, §8.3, §8.7, §11c-`WithSingleSubscriber`, §11c-`MaxAttempts`.

### Repo meta — CLAUDE.md, HANDOVER, indexes, CI

- **M-B3 · `docs/rfcs/README.md:97`** — still reads *"**Round-1 audit `NEEDS-REVISION`** … AWAITING ROUND 2. No
  code until round 2 passes"*, five rounds and three merged code commits later (`c83dde9`, `b6ce7bb`, `1d7fc80`),
  in an index whose own closing section tells a fresh session to read it **second**. Also omits **ADR 0030** from
  RFC 0002's promotion targets (one-way link). → Name the commits and the remaining tasks; a status cell is a
  measurement.
- **M-B4 · `CLAUDE.md:295-303`** — publishes a **7**-directory loop as *"exactly as CI runs it"*; `ci.yml`'s
  matrix has **6** and the `workspace` job hard-codes 6, both omitting `adapter/cron/crontest`
  (`grep -c 'dir:' ci.yml` → 6; `grep -n crontest ci.yml` → nothing). The second snippet labels a **two**-command
  build as *"the separate CI `workspace` job"*, which builds six. The gap is owned by Task 10; the defect is
  CLAUDE.md **asserting the equality anyway**, in the file every session reads first — round-5 M11's class, in the
  adjacent paragraph. → *"a superset of CI: `ci.yml` omits `adapter/cron/crontest` from both jobs (pre-existing;
  Task 10 adds it)"*, and correct the workspace snippet.

---

## §3 · MINORS

### Wrong `file:line` (one class — every citation was checked mechanically, not sampled)

| Id | Location | Says | Is | Note |
|---|---|---|---|---|
| C-M1 | Spec `:1680`, `:1708` | `channel.go:33` "Return Address" | **`channel.go:38`** | `:33` is a bare `//`. Round-4 B7 swept §3.2 only. |
| C-M2 | Spec `:1769` | root `doc.go:50` (`WithX` allow-list) | **`adapter/http/doc.go:50`** | root `doc.go` has no `WithX` at all; also `adapter/http/options.go:141`. |
| C-M3 | ADR 0027 `:185` | `channel.go:49` (`Subscription`) | **`channel.go:54`** | Contradicts Spec `:309`, which is right. |
| M-4 / C-M4 | ADR 0028 `:146`, `:149` | `endpoint/exchange.go:223` | **`:225`** | Both rows of §4a. |
| C-M10 | Spec `:1684` | `channel.go:18-19` | **`channel.go:12-19`** | Obligation 5's statement starts at `:12`. |
| C-B3 | ADR 0029 `:393` | `errors.go:161`/`:183` | **`:180`/`:206`** | Promoted to blocker — third variant of one measurement. |

### Numbers and scope

- **C-M5 · Plan `:401-402`** — Task 3's rename census is the stale `30/12/35/14`, which reproduces in **no single
  frame**: `b6ce7bb` is 30/12 **and 30/12** (CLAUDE.md/MESSAGING.md carry zero mentions at that pin, so the
  *"plus five more"* clause is false there), `0e2dcf0` is 30/12/35/14, `dadc775` is 31/13/36/15. Spec §6 already
  carries the `ROUND-4 CORRECTION`; the Plan still says *"ADR 0029 §1's sizing is exactly right"*.
- **C-M6 · Plan `:1069-1071`, `:1084`** — *"the two known pre-existing gaps"*; Spec §9.7 says **six** in so many
  words (*"The earlier wording named two … found eleven … Five were fixed; six remain"*). Re-derived: six
  zero-count blocks. A Task 12 worker told there are two reports the other four as regressions.
- **C-M9 / E-M10 · Spec `:1739`, `:1745`, `:1767`** — *"ten scanned packages"*; the loop scans **eleven** (root
  plus ten). The allow-list rows echo the wrong figure.
- **C-M8 / E-B2** — `expr` missing from the declared-side list (promoted to blocker above).
- **M-2 · `CLAUDE.md:7`** — *"26 plans / 26 ADRs / 13 specs"*; `ls docs/adrs/*.md | wc -l` → **29**, numbered
  0001–0030 with **no 0024**. Even scoped to 0001–0026 it is 26 numbers but **25 files**.

### Task hygiene

- **E-M1 · Plan `:665-667`** — Task 9.6's `NewPubSub(WithSingleSubscriber())` propagation checkbox is **already
  done** (`channel/pubsub_test.go:175-193`), and its only novel reading is **impossible blackbox**: `PubSub`
  exposes only `Publish`/`Subscribe`/`TopicCount`, and ADR 0030 `:98` says so itself. → Drop or restate as
  already-covered.
- **E-M2 · Plan `:505-506`** — Task 9's `apidiff` checkbox says *"do not claim zero output"*, but the only
  committed baseline is root-only and Task 9 retypes `routing`/`transform` symbols already in the 95 removals.
  Root apidiff for Task 9 **is** zero. → Snapshot `./routing` and `./transform` before the edit, or delete the
  checkbox (the compile-only demonstration at `:522-524` already covers source compatibility).
- **E-M3 · Plan `:232-239`** — Global Constraint 5's canonical loop hard-codes `expr` and reports **RED** for
  Tasks 9/9.5/9.6 (`cd: no such file or directory: expr`), while those tasks' own Verify sections say
  "seven-module loop, not eight". A worker copies the constraint block. → Paste the seven-module loop with `expr`
  as a commented add-from-Task-10 line.
- **E-M4 · Plan `:634`** — Task 9.5's commit subject names the dead-helper deletion, already `[x] DONE` at `:578`,
  and is typed `refactor(core)` with **no `!`** while removing two exported sentinels (apidiff 95 → 97). Task 9.6
  marks a *smaller* break `feat(…)!`. → `refactor(core)!: move the expr sentinels out of root, …`.
- **E-M5 · Plan `:774`** — Task 9.6's trailers are inlined in parentheses and omit `RFC:`, though Global
  Constraint 7 requires it and ADR 0030's header declares `RFC: 0002`. → Footer trailers,
  `Spec: 014 / Plan: 027 / ADR: 0030 / RFC: 0002`.
- **E-M7 · sizing** — Task 9.5 (**S**) carries two un-noted scaffolding blocks beyond round 4's HTTP pair:
  capability row 4 needs a `MessageGroupStore` + strategies; row 5 additionally needs `WithGroupTimeout`, a
  clockwork fake, `go agg.Run(ctx)`, a tick advance and goleak-clean teardown — **and returns no error at all**,
  so its `assert(err)` arm is vacuous. Task 10 (**M**) additionally requires a full consumer + RetryPolicy + DLQ
  pipeline inside the `expr` module's tests for D-K. → Re-size 9.5 → **M** (or split 9.5a/9.5b), 10 → **L** (or
  split 10a/10b as round 4 suggested).
- **E-M8 · Spec §5.0 census** — Task 9 retypes `NewRouter(pick RouteFunc, …)`, so `routing/router.go:37` stops
  matching and the 16-line census becomes 15. Task 12 re-measures but is not told the expected shape changed. →
  Note it in Task 9.
- **E-M9 · `endpoint/exchange_test.go:408-412`, `:420`, `:422`** — the test's own prose (*"a legal program"*,
  *"the second exchange **is built**"*, *"is NOT rejected"*) becomes false under D-J, and neither sweep arm can see
  prose of that shape. Task 9.6 instructs the option edit and the godoc rewrite but not this. → Add a checkbox.
- **D-M3** — combinator error context (folded into **D-M** above).
- **M-1 · Plan `:1027`** — Task 12's CLAUDE.md checkbox is stale three ways: `FilterExpr`/`RouterExpr`/
  `StreamingSource` are **already gone** from CLAUDE.md, `:235` is now the `robfig/cron` bullet, and the
  `./...`-is-not-the-repo block was already updated in round 5. → Reduce to the eighth module; **cite sections,
  not line numbers, into CLAUDE.md** — they rot on every edit.

### Repo meta

- **M-3 · ADR status hygiene, a class of 11** — `0010, 0011, 0012, 0013, 0014, 0015, 0018, 0019, 0020, 0021, 0022`
  all carry `Status: Proposed` while their decisions are **shipped** (the three SQL dialect modules exist;
  `PublishSubscribeChannel`, `QueueChannel`, `MessageGroupStore`, `ChannelExchange`, `Aggregator`,
  `ScheduledSender` are all in non-test code). CLAUDE.md cites 0011/0012/0017 as shipped while their own status
  says *"pending the adversarial audit"*. → Sweep as a class to `Accepted (date) — implemented in <SHA/plan>`.
- **M-5 · `docs/RELEASE.md:13-17`, `:58`** — lists 5 non-root modules and says *"6 modules … for local
  development"*; `go.work` has **7** `use` entries. CLAUDE.md makes RELEASE.md authoritative before any tag. →
  Add `crontest` as a never-published runner row, matching `dbtest`.
- **M-6 · `.github/workflows/ci.yml:4`, `:109`** — the workflow's own comments say *"five sql-adapter modules"* and
  *"coherently across all 6 modules"*; `go.work` has 7. → Fix in the same edit as Task 10's three CI changes.
- **M-7 · `docs/HANDOVER.md:309-315`** — Next-actions is numbered `1, 2, 4, 5` (no 3) and **items 2 and 4 are the
  same instruction**.
- **M-8 · `docs/HANDOVER.md:335`** — *"The `../msgin-derive` worktree is … safe to `git worktree remove`"*;
  `ls ../msgin-derive` → **No such file or directory**, absent from `git worktree list`. A next session runs a
  command that errors. → Delete the bullet.
- **M-9 · commit trailers** — over all 16 commits in `main..HEAD`, every `refactor`/`fix` commit is compliant, but
  **`6f44db6`** (`docs(rfcs): fold audit findings into RFCs…`, edits five RFCs) and **`28dd9e4`**
  (`docs(claude): refresh project status…`) carry **no trailer at all**. Not fixable without a rewrite. →
  **Triage with written rationale** (recommended: they are `docs:` commits predating the trailer convention's
  application to non-code artifacts) rather than rebasing 16 commits.
- **M-10 · D-K has no spec presence** — `grep -c 'D-K' docs/specs/014-*.md` → **0**, while `CLAUDE.md:236` points
  readers at *"Spec 014 §3.2/§7"* for it, and CLAUDE.md holds the spec normative. Under **revised D-K** the
  contract is now `msgin.ErrPayloadType`, which is a *root* contract — so a spec bullet is required, not optional.
- **M-4 (plan numbers) · ADR 0023 `:32`, `:199`; Spec 011 `:630`; Plan 020 `:19`** — the gin/ADR-0024 work is
  placed in *"Plan 027"* (now the core-layout plan), *"Plan 028"*, and *"Plan 025"* (which is
  `025-http-sse-server.md`) respectively. Round-1 D3 is recorded FIXED but was fixed only in spec 011. →
  Normalize to Plan 028.

---

## §4 · VERIFIED SOUND — do not re-open in round 7

**Generated/measured layer — every block re-derived and reproducing exactly:**

- §3.1 inventories (root 14 with exact name list; `endpoint` 12 / `routing` 6 / `transform` 2 / `channel` 5 /
  `resilience` 4); **all 80 §3.2 declaration rows on BOTH sides**, joined mechanically by `(kind, name)`:
  `src matched: 80 bad: 0`, `dst matched: 80 bad: 0`.
- §3.4a frames `c83dde9~1`=45 · `c83dde9`=44 · `b6ce7bb`=45 · `dadc775`=50 — **round-5's B1 re-pin to `b6ce7bb`
  is a genuine re-measurement, not a relabel.** §3.4b census 44 rows.
- §3.4e per-package `msgin 95.3% · channel 100.0% · endpoint 99.1% · resilience 99.1% · routing 100.0% ·
  transform 100.0%`; `-coverpkg=./...` **93.4%**.
- §3.6 blast radius `c83dde9~1..dadc775 -- adapter/` → **43 files, +244, −220**, nine-row rollup matching to the
  file and line; `0e2dcf0` still prints 31/239/191.
- 102 exported / 43 sentinels; `apidiff` **95 removed / 6 added** with the exact six additions; §4.1's partition
  (87+6+1+1, empty residual); **§4's closed 102-symbol list is an exact set match against the tree in both
  directions.**
- §6 rename census 31/13/36/15; §7 prefix census 26/10/15; the 51/27 sentinel-precedent figures; §9.7's six
  accepted uncovered blocks; six package-doc counts; 11 root packages; seven modules GREEN standalone under
  `GOWORK=off`; all seven `go mod tidy -diff` clean; `gofmt -l .` clean.
- §8.1 **arm 1** → the two documented survivors (`codec.go:33`, `routing/aggregator_test.go:21`); **arm 2** →
  exactly `WithRelease`, and both stated blind spots were **probed with a synthetic file and confirmed accurate**
  (block comments and non-`With|Err|New` shapes produce zero hits).
- `grep -rn "at HEAD"` across the bundle → **empty**; every surviving `..HEAD` is a correction block quoting the
  anti-pattern or Task 12's legitimate review range.
- `symmap.tsv` regenerates **byte-identical** at 91 lines; both article greps exit 1.

**Git and traceability layer (meta lens):** all seven "safe to cite" SHAs exist, are on-branch, and carry the
claimed subjects; `main`=`0de54e9`, `@{u}`=`6f44db6` (`origin/<branch>`), 16 ahead of main, 13 unpushed, 0 behind;
tree clean; **zero tags**; `HEAD~1`=`dadc775`; *"no `.go` touched since `3d0b87a`"* holds
(`git diff --stat 3d0b87a..HEAD -- '*.go'` empty). Round-4's traceability fixes all landed (RFC 0002 → ADR 0030
backlink; ADR 0028 → ADR 0030 forward pointers; ADR 0029's header carries D-I **and** D-K; §5.0a's present-tense
defect fixed). All 29 ADRs carry the full Nygard set. All relative links resolve. `GOTOOLCHAIN` pin matches
`ci.yml` and both `setup-go` versions. Acyclicity (`go list -deps`) empty in both arms. `expr-lang` absent from
every `go.mod`/`go.sum`/`.go`; `robfig/cron` only in `adapter/cron`.

**Executability layer:** Task 12's three invariant commands reproduce (14 / 102 / 43) and the projection
arithmetic is internally consistent; the committed `apidiff` baseline exists and is not in `/tmp`; **every source
line reference in Tasks 9/9.5/9.6 is correct** (`errors.go:168/180/193/206`, `capability_test.go:152,163,174`,
`routing/router.go:29,37`, `channel/direct.go:29`, `channel/pubsub.go:112`, `endpoint/exchange.go:216,225,250`,
`endpoint/exchange_test.go:120,413,446,453`); **the 25-site `NewChannelExchange` census is correct** and only
`:446`/`:453` are affected by D-J (all 23 others bind a `DirectChannel` reply or `nilSubChannel{}` and pass);
Task 9's four type declarations match the real constructor signatures; **Task 9's combinator branch table is
complete** — a search for a further omission past round 4's `And`/`Or` mirror pair found none; **ordering
9 → 9.5 → 9.6 → 10 → 11 → 12 has no inversion**; `channel` and `routing` both measure 100.0% today, so Task 9.6's
acceptance signal has a real baseline; CI facts (six-dir matrix, six-dir workspace job, `crontest` missing from
both) are accurate.

**Design layer:** the Return Address seam is unaffected by D-J — `adapter/http/exchange.go:58` implements
`msgin.RequestReplyExchange` directly and never touches `NewChannelExchange`. ADR 0030's citations all check out
(`channel/pubsub.go:112`, `ab233d9:aggregator_test.go:222`, `adapter/http/exchange.go:58`, `spi.go:118`).
Round-2 §E (bare-closure inference against named generic func types on Go 1.25) was not re-opened.

---

## §5 · Fix-pass partition

Findings are grouped by **target file** so the pass can run without write conflicts. Cross-cutting fixes (the
`four`→`five`/`six` cardinality sweep, the withdrawn-claim sweep, the `file:line` corrections) list every site
above; each owner applies its own sites.

| Group | Files | Blockers | Minors |
|---|---|---|---|
| **A — ADRs** | `adrs/0028`, `adrs/0029`, `adrs/0030`, `adrs/0027` | C-B1, C-B2, C-B3, C-B6, D-B2, D-B3, C-M7 | M-4, C-M3, D-M2, + D-L/D-M/D-K-revised records |
| **B — Spec 014** | `specs/014` | D-B1, E-B6, C-B8, C-B9, E-B7, E-B2, gate rewrite | C-M1, C-M2, C-M9, C-M10, M-10 |
| **C — Plan 027** | `plans/027-core-package-layout` | M-B1, M-B2/C-B4, C-B5, C-B7, E-B1, E-B3, D-B4, gate rewrite | D-M1, E-M1…E-M9, C-M5, C-M6, M-1 |
| **D — repo meta** | `CLAUDE.md`, `HANDOVER.md`, `rfcs/README.md`, `RELEASE.md`, `ci.yml`, ADR statuses, `adrs/0023`, `specs/011`, `plans/020` | M-B3, M-B4 | M-2, M-3, M-5, M-6, M-7, M-8, M-9, M-4(plan numbers) |

**After the pass:** re-run the §4 verification set, confirm seven modules green and the tree docs-only, then run
**round 7** — every round so far has found defects in the previous round's fixes, and this pass is the largest
yet. Brief round 7 on the counter-rules in §0 and on this file.

---

## §6 · CORRECTIONS TO THIS RECORD, found while applying it

The fix pass ran as four file-partitioned agents (§5). Each was instructed to **refuse any prescribed fix that
proved false on inspection and report it instead**. Seven did not survive contact with the source. **Round 7 must
treat §1–§4 above as corrected by this section, not as written.**

### §4's "VERIFIED SOUND" list contains one falsified entry

- **`All relative links resolve` — FALSE.** `docs/adrs/0015-scheduled-send.md:13` linked
  `[ADR 0007 D10](0007-composition-endpoints.md)`; **that file does not exist**. The real target is
  `0007-reliability-settlement-api.md`, which does hold `### D10` at `:204`. Fixed in the pass. A link check over
  all 17 files group D touched now reports 0 broken, but **the bundle has never had a repo-wide link check** —
  the meta lens checked only the bundle's own files. Round 7 should run one across all of `docs/`.

Everything else in §4 was re-derived at least once during the pass and held.

### Line facts corrected (the same class §3 lists six of — committed *in this record*)

| Where in this record | Said | Is |
|---|---|---|
| §2 E-B7 / D-M5 | `channel/pubsub.go:157` (`isEmpty`) | **`:158`** — `:156-157` is its godoc |
| §1 D-M, §2 D-B4 | `reliability.go:38-53` (`IsPermanent`) | **`:38-49`** — `:50+` is `retryAfterError` |

### Substantive corrections

- **§1 D-L, case B is under-specified and not reproducible as written.** *"A wrapper embedding
  `*PublishSubscribeChannel` inherits `SingleSubscriber` by method promotion and reports `true`"* holds **only
  when the embedded channel was built `WithSingleSubscriber()`**. With a plain embedded channel it reports
  `false` and is correctly rejected. Established by an **independent re-derivation** of the compile proof (group
  A rebuilt the probe from scratch rather than relaying the design lens's transcript, and reproduced it exactly
  apart from this precondition). The defect D-L fixes is real; this record's description of it was sloppy. ADR
  0030's B bullet now states the precondition.
- **§1 D-M's producer table is missing a sixth site.** `routing/aggregator.go:251` (`NewAggregator`) also returns
  a bare `ErrNilFunc`. It is **construction-time**, never reaches a `RetryPolicy`, and is **deliberately
  excluded** — but the omission read as an oversight. The governing invariant, now recorded in ADR 0029 §5.0b:
  **every `ErrNilFunc` returned from a `MessageHandler` body is `Permanent`; every one returned from a
  constructor is bare.** State the invariant, not the five-row list.
- **§2's gate replacement set omits obligation 13.** It covers 10, 11, 12 and §8.0a(c) only — the same no-owner
  shape §8 exists to catch, reproduced inside the fix for it. Group B added 13's gate; group C additionally
  converted §8.4, §8.3 and §11c-`MaxAttempts` to `go doc` for the same direction-bug reason. **The direction bug
  was a class, and this record scoped it to the four D-J gates.**
- **§3 E-M8 states the §5.0 census goes 16 → 15.** Measured, **two** lines are in `routing/router.go` — `:29`
  (the `Router.pick` field) and `:37` (the `NewRouter` parameter). Retyping only the parameter gives 15; retyping
  the field too gives 14. The plan now states both and requires the executor to record which.
- **§5's partition is not clean for E-B7 / D-M5.** The finding is filed against Spec 014 (group B) but its fix is
  a **Task 9.6 checkbox** (group C's file), which group B could not reach. Group C applied it, labelled. A
  finding's owning file is not always its fixing file; the next partition must be derived from the *fix*, not the
  *finding*.

### Judgement calls made by the fix pass, recorded for review

- **ADR 0028's cardinality site was converted to an invariant, not re-typed as "six"** — *"cite the table, never
  a count"*. The count has now grown twice (D-J, D-M); the Spec's own table remains the single source.
- **Tasks 9.5 and 10 were re-sized (S→M, M→L), not split.** Reason: the task *number* is a cross-document link —
  `CLAUDE.md`, ADR 0029 `:256` and Spec `:1898` all cite `9.5`/`9.5.1` by number, so a renumber is a coordinated
  three-document edit while a size label is a scheduling signal. Both tasks record the clean split that stays
  available (9.5a/9.5b, 10a/10b).
- **`M-9` (two trailer-less commits) is triaged, not fixed**, with a fact this record did not have: `6f44db6`
  **is** the published upstream head, so the rewrite would require a force-push over a published commit.

### Still open after the pass

- **Task 9.6 is still sized `S`** despite gaining two checkboxes (the second test fake and the fifth table row).
  Arguably `M`. Flagged rather than changed unilaterally — decide in round 7 or at execution.
