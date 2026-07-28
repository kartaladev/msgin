# Plan 027 — adversarial design audit, ROUND 7

**Traceability:** audits [Spec 014](../specs/014-core-package-layout.md) · [Plan 027](027-core-package-layout.md) ·
[ADR 0027](../adrs/0027-core-package-restructure.md) · [ADR 0028](../adrs/0028-channel-interface-segregation.md) ·
[ADR 0029](../adrs/0029-eip-lexical-alignment.md) · [ADR 0030](../adrs/0030-reply-channel-exclusivity-probe.md) ·
[RFC 0002](../rfcs/0002-eip-alignment.md). Prior: [round 1](027-audit-round-1.md), [round 2](027-audit-round-2.md),
[round 6](027-audit-round-6.md) (read its **§6** — it corrects its own §1–§4); rounds 3–5 in [`docs/HANDOVER.md`](../HANDOVER.md).
**Produces:** **D-L (revised)**, **D-N**, **D-O**, and a scope correction to **D-M**.

**Date:** 2026-07-28 · **Tree audited:** `c4582ba`, clean. **Code pin:** `dadc775` for `.go`
(`git diff --stat 3d0b87a..HEAD -- '*.go'` empty) — but **`.github/workflows/ci.yml` and `CLAUDE.md` changed in
`c4582ba`**, which round 6's `dadc775..aae6160` pin did not cover. That gap is blocker X-B6 below.

## Provenance

Four Opus lenses, run in parallel against `c4582ba`. **All four `NEEDS-REVISION`: 26 blockers, 37 minors.**

| Lens | Verdict | B | M |
|---|---|---|---|
| Fix-pass regression (**new** — aimed at the parallel-authorship seam) | NEEDS-REVISION | 5 | 7 |
| Design | NEEDS-REVISION | 8 | 7 |
| Executability | NEEDS-REVISION | 8 | 14 |
| Meta / git-facts / repo-wide links | NEEDS-REVISION | 5 | 9 |

---

## §0 · THE CLASS ROUND 7 EXPOSES

> **Round 6's fix pass was partitioned BY FILE. Four agents produced four internally-coherent documents and
> three broken JOINS — every one a *forward* reference (ADR→Plan, Spec→Plan, Spec→ADR) that the owning agent
> structurally could not see.** The three coherence greps run afterwards checked *shared strings*; nothing
> checked *shared obligations*.

**Counter-rules adopted for the round-7 fix pass** (cumulative with rounds 4–6):

6. **Partition by DECISION, not by file.** A decision's statement, its consequence, its task and its gate are
   one artifact spread across four documents. One owner edits all of them, accepting write conflicts. Where a
   file partition is unavoidable, the pass MUST end with a **join check**: for each decision, extract its
   normative text, its gate and its task number from every document and `diff` them mechanically.
7. **A gate must measure the observable the change actually moves.** Round 7 found a RED baseline that measures
   `IsPermanent(bare sentinel)` for a change that wraps at the *producer* — unsatisfiable by any correct
   implementation (X-B2). The inverse of round 4's "proof that cannot fail".
8. **A RED baseline is pinned to the tree at ITS OWN task's start, not to the untouched tree.** Task 11 runs
   after Task 9.6, which writes the godoc three of Task 11's gates check (X-B8).
9. **A derivation command scoped to one name or one qualified form is not a class sweep.** §5.0b's
   `grep 'msgin\.ErrNilFunc'` is structurally blind to producers *inside* package `msgin` — which is how
   `ErrNilSink` survived (D-B1).
10. **When a pass edits a file, re-run every pasted command in the bundle that reads that file.** `c4582ba`
    added comments to `ci.yml` and thereby falsified a command pasted in the plan *in the same commit* (X-B6).

---

## §1 · DECISIONS TAKEN THIS ROUND

### D-L (REVISED) — count RECIPIENTS REACHED, not PROCESSES TRAVERSED

**Supersedes** the D-L recorded in [round 6 §1](027-audit-round-6.md). **Prompted by** design B2.

D-L's two normative sentences give **opposite answers for the same channel**. Sentence 1 — *"reports whether
THIS exchange will be the sole recipient"* — answers `true` for a NATS per-instance `_INBOX.<nuid>` or an
exclusive auto-delete AMQP reply queue. Sentence 2 — *"a channel whose deliveries reach other processes
(a broker subject, a Redis pub/sub channel, an SSE stream) MUST return false"* — answers `false` for that same
channel, which is literally "a broker subject", its own first example.

That channel is **the canonical Return Address implementation** — the pattern ADR 0022 and ADR 0030 §Topology
both name as *the* distributed answer. **D-L as recorded made a correct implementation unrepresentable**, and it
worked under the superseded handle-local definition. The root error: the predicate was worded about *processes
traversed* when the property that matters is *recipients reached*.

Secondary defect (design M3): *"THIS exchange"* is unanswerable by the method it is written on —
`SingleSubscriber()` is nullary, lives on the **channel**, and is called **before** the exchange subscribes, so
no implementation can observe which exchange is asking.

**Decision — replace ADR 0030 §1's second normative paragraph and Spec §8 obligation 11(b) with:**

```go
// A channel MUST return false whenever a message sent to it can be received by
// any recipient other than the single subscriber registered on it — INCLUDING a
// recipient in another process. A broadcast broker subject, a Redis pub/sub
// channel, or an SSE stream fanned out to N instances MUST therefore return
// false even when its local handle admits one subscriber. A broker-backed
// channel MAY return true only when the broker guarantees the destination is
// private to this process's subscription — a per-instance NATS _INBOX reply
// subject, an exclusive auto-delete AMQP reply queue. That is the Return
// Address pattern, and it is what an honest true means here.
```

Sentence 1 becomes *"reports whether every message sent to this channel reaches **at most one recipient,
counted across every process**"*. **§Topology's reversal claim narrows**: Topology 2 becomes detectable **for
the broadcast case**, which was always the real claim — a private inbox is Topology 1 with a broker in the
middle. Everything else in D-L (MUST NOT compute from a live count; lifetime-invariance; concurrency-safety;
EMBEDDING CUTS BOTH WAYS) is **unchanged**.

**Also (design M5) — state the wrapper invariant by shape, not by mechanism.** ADR 0030 §4 and Spec obligation
12 name only *"embedding the `msgin.SubscribableChannel` interface"*. Compile-proven, a generic wrapper holding
the **concrete** type in a named field with hand-written forwarders strips the probe identically. Restate:
***any* wrapper that does not itself declare `SingleSubscriber` is accepted under this arm, however it holds
the channel it wraps.**

---

### D-O — the probe MUST NOT block or panic, and msgin defends against both

**Prompted by** design B3, compile-proven.

```
D1 panicking probe  -> PANIC ESCAPES NewChannelExchange
D2 blocking probe   -> NewChannelExchange HUNG (no ctx, no timeout)
    goleak: Goroutine in state select (no cases), blockProbe.SingleSubscriber
            on top of endpoint.NewChannelExchange(...) exchange.go:249
```

Four authorities in this repo already forbid this:

1. **CLAUDE.md** — *"library code … must not `panic` on caller input"*, plus "Fault isolation & recovery".
2. **`ErrUnboundedRetry`'s own godoc**, verbatim: *"The check is deliberately **STRUCTURAL** — it tests
   `Backoff` for nil rather than evaluating it — because `BackoffStrategy` is a public interface and **calling
   caller code inside a constructor may panic, may block**, and is non-deterministic."* D-J does precisely what
   that paragraph exists to forbid.
3. **The `safeX` class** — `endpoint/consumer.go` carries **eleven** recover-wrappers for third-party interface
   methods. `SingleSubscriber` would be a twelfth call site, outside the class.
4. **`ErrNilSubscription`'s godoc**, 20 lines away in the same constructor: *"a faulty implementation is caller
   input: it is rejected at CONSTRUCTION with this typed error … rather than deferred into a nil-pointer
   panic."*

**Decision.**

- **(a) Wrap the call and FAIL CLOSED.** A probe that panics has not proven exclusivity, so the recovered value
  is `false` (CLAUDE.md's "default to the safe, conservative value"):
  ```go
  func safeSingleSubscriber(ex msgin.ExclusiveSubscribable) (b bool) {
      defer func() { if r := recover(); r != nil { b = false } }()
      return ex.SingleSubscriber()
  }
  ```
  Surface the recovered value through the exchange logger. Task 9.6 gains this checkbox and a **sixth**
  truth-table row.
- **(b) Blocking cannot be defended against — make it a stated MUST** in ADR 0030 §1's normative godoc:
  *"`SingleSubscriber` MUST NOT block and MUST NOT panic. msgin calls it inside `NewChannelExchange`, on the
  caller's goroutine, with no context and no timeout; it must be a constant-time accessor over state fixed at
  construction."*

---

### D-N — `divert` falls back to the DeadLetter sink before discarding

**Amends** ADR 0007 D7. **Prompted by** design B4.

D-M introduces an **unacknowledged data-loss path**. With `RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}` and no
`WithInvalidMessageSink` (**the default** — `invalidSink` is nil and ADR 0007 D7 discards):

```
BEFORE D-M: bare ErrNilFunc         DLQ=1  discarded=false
AFTER  D-M: Permanent(ErrNilFunc)   DLQ=0  discarded=true
```

The bundle frames D-M as purely an improvement (*"diverted to the invalid-message channel instead of … landing
in the dead-letter sink"*) and never states that a message can now be **dropped where it was previously
captured durably**. CLAUDE.md: *"When a wrong default could silently corrupt (… lose data …), pick the value
that fails safe."*

**Decision.** In `endpoint/consumer.go`'s `divert` path, when `invalidSink == nil` **and**
`c.policy.DeadLetter != nil`, route to the DeadLetter sink rather than discarding. Discard remains the terminal
behavior only when neither sink is configured.

**Consequences.** No configuration that previously captured a message starts dropping it. This **amends ADR 0007
D7** and needs a note there; it is a further behavior change to shipped code and joins Spec §2.1 (row 7). It
needs its own covering case, and a case for the neither-sink-configured discard that remains.

---

### D-M — SCOPE CORRECTION: `ErrNilSink` is in the class

**Prompted by** design B1.

`handler.go:55` (`msgin.To(sink)`) returns a bare `ErrNilSink` **from a `MessageHandler` body**, with `sink`
captured at construction — the same shape and the same discriminator arm as `nilFuncStep`. Measured:

```
IsPermanent(ErrNilFunc) = false      transform.Transform(nil)  OnRetry=2 OnDeadLetter=1 OnInvalidMessage=0
IsPermanent(ErrNilSink) = false      msgin.To(nil)             OnRetry=2 OnDeadLetter=1 OnInvalidMessage=0
```

**Why it was missed is mechanical** (counter-rule 9): §5.0b's derivation command greps `msgin\.ErrNilFunc` —
scoped to one sentinel name *and* the qualified form — so it cannot see a producer inside package `msgin`.

**Decision.** Task 9.7 gains a **fifth edit site**: `handler.go:55` →
`fmt.Errorf("%w: msgin.To: nil sink", msgin.Permanent(msgin.ErrNilSink))`, with `handler.go:50` in the godoc
sweep. **Restate §5.0b's invariant sentinel-agnostically:** *"**every** deterministic typed error msgin returns
from inside a `MessageHandler` body is `Permanent`; every one returned from a constructor is bare"*, and give
it a re-derivation command that is not scoped to one name and covers unqualified root producers.

**Also correct Spec §2.1 row 6** (design B7, exec B3): it currently says *"**Every** producer of `ErrNilFunc`
returns `Permanent`"* — a universal quantifier that contradicts ADR 0029 §5.0b's deliberate exclusion of
`routing/aggregator.go:251` (`NewAggregator`, a constructor). Round 6 §6 asked for *"the invariant, not the
five-row list"*; a universal quantifier is strictly worse than the list it replaced.

---

### D-K — the `ErrPayloadType` godoc must be widened, and the cost recorded

**Prompted by** design B8.

`errors.go:6` reads *"ErrPayloadType is returned when a `Message[any]` payload cannot be asserted to T."* An
expression's evaluated **result** is not a `Message[any]` payload. ADR 0029 asserts the godoc *"is already
domain-generic"* — it is not — and **no task amends it**, while both the ADR (*"No root change"*) and Spec
(*"Cost: none to root's surface"*) assert there is nothing to do.

**The unrecorded caller-visible cost:** after Task 10, `errors.Is(err, msgin.ErrPayloadType)` no longer
distinguishes *"the inbound payload was not `T`"* (fix the producer/codec) from *"the expression evaluated to
the wrong type"* (fix the expression) — two faults with disjoint remedies collapsed onto one target. §5.0c's
cost analysis was withdrawn as moot and **nothing replaced it**, so §5.0b now lists only benefits. That is the
identical silence-shaped defect that overturned the first D-K one round earlier.

**Decision.** (a) A task owns amending `errors.go:6` to name both conditions, the permanence, and that the
error *string* distinguishes what `errors.Is` deliberately does not. (b) The cost is recorded in ADR 0029 §5.0b
and Spec §7 as an **accepted trade-off, not an absence**. (c) *"No root change"* → *"no new root **symbol** and
no new import edge; `ErrPayloadType`'s godoc is widened."*

---

## §2 · BLOCKERS, by decision owner

`R` fix-pass regression · `D` design · `X` executability · `M` meta.

### Owner 1 — D-L / D-O / D-J joins
*(ADR 0030, Spec §5.1 · §8 obligations 10–13 · §10, Plan Task 9.6 + Task 11b/11c gates)*

- **R-B1 / D-B5 / X-B1 · Plan `:821-823`** — Task 9.6's first checkbox still orders *"a report about **this
  channel in this process**"* — the withdrawn handle-local wording. It occurs exactly twice in the bundle: once
  inside ADR 0030's `SUPERSEDED IN PLACE` block, once **live, in the document the implementer executes**. ADR
  0030 `:140` asserts *"Task 9.6 writes it verbatim"*; the Plan says no such thing. **Fix:** replace with
  *"write ADR 0030 §1's godoc verbatim — it is normative; Task 11b's obligation-11 gate is a phrase match. Do
  not paraphrase."*
- **R-B3 / X-B8 · Spec §8.0b vs Plan §11** — **the two documents publish different gate sets for the same
  obligations, and only the weaker one is executed.** Obligation 11: Spec has **4** conjuncts, Plan has **2** —
  and the two dropped are *precisely D-L's substance*. Obligation 11a: **no plan gate at all**, while the Risks
  table claims 11b/11c owns all thirteen. Obligation 13: Spec has 2 conjuncts, Plan 1. §8.4: Spec gates six
  types, Plan gates two. Both sets are RED today so the divergence is invisible until execution. **Combined
  with R-B1, an implementer can write the withdrawn godoc, satisfy every gate they run, and ship a contract
  that contradicts D-L.** **Fix:** copy Spec §8.0b's gates **verbatim** into the Plan (they are the normative
  set), add 11a and the four §8.4 type gates, re-run and re-paste the baseline. Standing check: the Plan's `g`
  block and Spec §8.0b must be diff-identical.
- **X-B8 (second half) · Plan `:1410-1447`** — Task 11's RED baseline is pinned to the **untouched tree**, but
  `:1414` says Task 11 runs **after** Task 9.6, which writes the godoc gates 8.11/11/11a check. At Task 11's
  actual start they are **GREEN**, and `:1447` says every line must print RED. **Fix (counter-rule 8):** split
  the baseline — gates whose symbols Task 9/9.6 create are RED *at their own task*; Task 11's baseline covers
  only what Task 11 writes, and names which gates 9.6 is expected to have already turned green.
- **D-B3 · D-O** — the probe hardening above: `safeSingleSubscriber`, the sixth truth-table row, and the
  MUST-NOT-block/panic godoc clause.
- **D-B2 · D-L (revised)** — the recipients-not-processes rewording above, in ADR 0030 §1, Spec obligation
  11(b), §Topology's narrowed reversal claim, and D-L's restatement in the round-6 record's §1.
- **D-M5 · wrapper invariant by shape** (above). **D-M1 / R-M5 · obligation 12's gate counts LINES, not
  matches** — `grep -c 'A\|B\|C\|D' … -ge 4` yields a **false RED** if two phrases wrap onto one `go doc` line;
  obligation 11 already uses the correct ANDed-`grep -q` shape. **D-M6 · ADR 0030's Alternatives table never
  weighs the soft opt-in** (`endpoint.WithRequireExclusiveReply()`, or a warn-level log on the accept-unknown
  arm) — D-L killed the cheapness premise that made accept-unknown look free; CLAUDE.md's sensible-defaults
  rule names this shape. Add the row with a decision either way.

### Owner 2 — D-M (incl. `ErrNilSink`) / D-N
*(ADR 0029 §5.0b, ADR 0007 D7, Spec §2.1 · §6 · AC-5, Plan Tasks 9 + 9.7)*

- **R-B2 / D-B6 · Spec `:1592-1596`** — **§6 still specifies the pre-D-M combinator semantics** (bare
  `ErrNilFunc`, no `Permanent`, no positional wrap, no correction block) while §2.1 row 6 and §7 both cite D-M.
  `grep -n Permanent` returns **zero hits between `:149` and `:1682`**. The spec is normative, so an
  implementer following §6 writes the bare sentinel and Task 9's own new `IsPermanent` case fails. Group B
  added row 6 and never swept the section row 6 itself cites.
- **R-B4 / D-B7 / X-B3 · Task 9.7 exists in no normative document.** `grep -rn 'Task 9\.7'` across Spec 014,
  ADRs 0027–0030, RFC 0002 and CLAUDE.md → **zero hits**. ADR 0029 §5.0b says *"Task 9 (combinators + producers)
  and Task 10 own the edits"*; Spec §2.1 row 6 and AC-5 both cite **Task 9**. A worker executing Task 9 who
  reads its governing ADR does 9.7's work inside Task 9's commit — exactly what Task 9.7's own rationale forbids.
  CLAUDE.md: *"Do not merge or commit work whose governing … link is missing."* **This is round 6's own C-B1
  recurring in the same ADR**, three paragraphs above the block that fixed it.
- **X-B2 · Plan `:1056-1062`, `:1130`** — **Task 9.7's RED→GREEN gate is unsatisfiable by any correct
  implementation.** The census measures `IsPermanent(bare msgin.ErrNilFunc)`; D-M wraps at the **producer** and
  deliberately does not touch `IsPermanent`'s enumeration, so row 1 reads `false` before *and* after. The block
  disproves itself three lines later, where row 4's `ErrNoCorrelation` is noted as *already wrapped at its
  producer* and still prints `false`. The only way to make it green is to add `ErrNilFunc` to the enumeration —
  which D-M rejects and which would make `NewAggregator`'s deliberately-bare return permanent. **Fix
  (counter-rule 7):** replace with the **producer-path** measurement the ADR already ran —
  `OnRetry=2 OnDeadLetter=1 OnInvalidMessage=0` → `OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1`; keep the
  sentinel census only as a labelled no-regression guard.
- **X-B4 · Plan `:1093-1114`** — Task 9.7's *"exhaustive"* godoc list is a **hard-coded five-file grep** that
  misses two sites, one of them **`ErrNilFunc`'s own sentinel godoc** (`errors.go:152` — the single statement
  every caller reads, and the natural home for the constructor-vs-handler invariant) and the other
  **`routing/aggregator.go:239`**, the deliberate exclusion. Same class as §8.1 arm 2's fixed declared-side
  list, which round 6 promoted to a blocker. **Fix:** unrestricted `grep -rn 'ErrNilFunc' --include='*.go' .`
  (10 lines today) and explicit checkboxes for both sites.
- **D-B1 · `ErrNilSink`** and **D-B4 / D-N** — as decided in §1.
- **D-B7 · Spec §2.1 row 6's universal quantifier** — as decided in §1.
- Minors: **D-M1 / X-M9 · the decided wrap produces a doubled prefix** —
  `"msgin: permanent: msgin: nil endpoint function: routing.Predicate.And: nil argument"` — and an inverted
  causal chain (context *after* cause) for a change justified on debuggability. Context-first is equally
  `errors.Is`/`IsPermanent`-clean (verified). **Print the produced string in the ADR and the plan whichever form
  is chosen** — five tests will be written against it. **D-M2 / X-M2 · Task 9.7 is ordered after Task 9 against
  its own argument** (`:1029-1031` says the mixed state is *"worse than either uniform answer"*, then `:1044`
  creates it across three commits). **X-M4 · "six nil sites" is five** in Task 9's frame (`Not` has no
  argument). **X-M5 · Task 9's branch list omits the positional-text assertion** Task 9.7's requires.
  **X-M14 · Task 9.7's coverage gate is flaky** — `endpoint` measured 99.4% and 99.1% on two runs of the
  unchanged tree; state it as *"no new uncovered block outside AC-7's six"*, not a percentage floor.

### Owner 3 — revised D-K / Task 10
*(ADR 0029 §5.0a–c, Spec §7 · AC-10, Plan Tasks 9.5 + 10, `errors.go:6` ownership)*

- **X-B5 · Plan `:1285-1287`** — Task 10 is told to *"recover the godoc from `git show 3d0b87a:errors.go`, lines
  168–180"*. Executed literally that (a) **reintroduces `ErrExprResultType` into shipped `.go` source**, failing
  Spec AC-10 arm 2's *"must become EMPTY workspace-wide"*, and (b) pastes *"It is exported **here, not in the
  provider**"* — the exact premise **D-I reversed** — into the provider. **Fix:** state the *content* to carry
  and write fresh godoc, with the counterpart clause naming **`msgin.ErrPayloadType`**.
- **D-B8 · the `ErrPayloadType` godoc widening and cost record** — as decided in §1.
- **X-B7 (shared with owner 4) · Task 10 has no checkbox to extend the arm-2 declared-side loop with `expr`** —
  round 6's E-B2 fix landed in the Spec's comment block and nowhere executable.
- Minors: **X-M3 · Task 9.5's commit scope** omits `adapter/http` and `adapter/http/stdlib` though its own
  checkbox places two capability sites there. **X-M10 · Task 9.5's capability widening has no available RED** —
  the segregation landed in `b6ce7bb`, so the five new sites pass immediately; say so rather than leaving a
  worker hunting an impossible red. **X-M13 · Task 10's "before Task 9.5 deletes it" is vacuous**
  (`git show 3d0b87a:` reads history). Moot if X-B5's fix lands.

### Owner 4 — repo meta, sweeps, hygiene
*(HANDOVER, CLAUDE.md, `docs/rfcs/README.md`, `docs/plans/006`, `docs/plans/021`, ADR 0027, Plan sizing/scopes/trailers, Task 12)*

- **M-B1 · `docs/HANDOVER.md:33-38`, `:42-43`, `:47`** — the banner's `$`-prompt block publishes
  `main..HEAD 16` / `@{u}..HEAD 13`; measured **17** / **14**, and §2 of the same file says 17/14. The file
  guards `HEAD`'s **SHA** against self-reference and not its **counts**, which have the identical problem:
  committing the handover increments both. **Fix:** re-run both; add the counts to the self-reference box.
- **M-B2 · `docs/HANDOVER.md:87-91`** — the *"identify `HEAD` by subject, not hash"* block names `aae6160`'s
  subject as *"this file's commit"*. `HEAD` is `c4582ba`. The anti-staleness mechanism went stale, and because
  `aae6160` is real and adjacent the error is **silent**.
- **M-B3 / R-M1 · `docs/HANDOVER.md:13`, `:75`** — fix-pass size published as `+1686/−317`; measured
  **`25 files, +2397/−323`**. A commit cannot state its own final diffstat — same self-reference the file already
  documents for `HEAD`'s SHA.
- **M-B5 · `docs/HANDOVER.md:369-382`** — the trailer triage says *"over the **16** commits … **two** carry none"*.
  Measured **17** commits and **four** trailer-less (`0e376fa`, `4d29958`, `6f44db6`, `28dd9e4`). The stated
  rationale extends cleanly to the two new ones so the **disposition survives**; the **enumeration** does not,
  in the one artifact whose whole purpose is *"nothing is unaddressed"*. `Do not rebase` stands — independently
  re-verified that `6f44db6` **is** the published upstream head.
- **M-B4 / R-M3 / X-M11 · the link-break CLASS was never fixed.** A repo-wide check (86 files, 692 links) finds
  **3 breaks still live**, all identical to the one round 6 patched by hand: a `docs/plans/*` file linking an ADR
  by bare filename, so it resolves to `docs/plans/0003-…`. `docs/plans/006-sql-engine-dialect-split.md:14` (×2)
  and `docs/plans/021-exchange-panic-safe-cleanup.md:1052`. **The 027 bundle itself has zero broken links.**
  **Fix the check, not the links:** repoint all three *and* add a repo-wide link+anchor sweep to the pre-merge
  gate.
- **X-B6 · Plan `:1197-1198`** — the evidence command for CI edit #3 (*"`grep -n crontest ci.yml` → no output"*)
  was **falsified by the same commit that carries this plan**: group D added `crontest` comments to `ci.yml` in
  `c4582ba`, so it now returns 3 hits. A worker concludes edit #3 is done; it is not (`grep -c 'dir:'` → 6).
  Group D derived the correct comment-stripped form for CLAUDE.md and did not propagate it here. **Counter-rule 10.**
- **X-B7 · the two-arm staleness sweep runs once, at Task 9.5, and three later tasks invalidate it.** Task 12
  has no sweep checkbox; Tasks 9.6, 9.7 and 10 each add godoc the sweep would check. The gate the Risks table
  names as the mitigation for *"a change is invisible to the compiler"* cannot certify the delivered tree —
  and it would have caught X-B5's dangling `ErrExprResultType`. **Fix:** add the sweep to **Task 12**, add the
  `expr` loop-extension to **Task 10**, re-attribute the Risks row to *"Task 9.5 and again in Task 12"*.
- **M-M1 · CLAUDE.md's new "superset of CI" is true on directories, false on steps.** CI runs **eight** steps per
  module (build, vet, gofmt, `CGO_ENABLED=0`, tidy stability, `govulncheck`, `golangci-lint`, `-race -shuffle`);
  CLAUDE.md's loops run two, directly under *"the pre-commit gate is only satisfied when…"*. **Fix:** *"a
  superset in **module coverage** — 7 to CI's 6 — but a **subset in steps**."*
- Sizing and hygiene: **X-M1 · Task 9.6 → `M` and Task 9.7 → `M`** (9.6 now ships 2 root symbols with normative
  godoc, 2 `channel` methods, an `endpoint` option + guard, a full godoc rewrite, two fakes, a 5-row table and
  both coverage arms — plus D-O's recover helper and sixth row; 9.7 changes `nilFuncStep`'s **signature** in
  three packages, rippling to five call sites). **X-M2 · Task 9.7's commit scope** is wrong in both directions
  (`core` unearned, `endpoint` missing). **X-M12 · only Tasks 9.6 and 9.7 carry trailer blocks** — add the
  four-line footer to 9, 9.5, 10, 11, 12. **X-M7 · gate 11c-2 matches a single incidental word** (`grep -qi
  instance` matches *"for instance"*) — the class round 6 just rejected for §8.11. **X-M6 · Task 9.7's godoc
  block is presented as pasted output but re-typed** (order matches neither the argument nor the command), and
  says *"seven lines across **six** declarations"* while naming seven. **M-M2 · `027-audit-round-6.md` has no
  reverse link** from Spec 014 or Plan 027, both of which link rounds 1 and 2. **M-M3 · ADR 0027 is the only
  bundle ADR with no round-6 acknowledgment.** **M-M9 / M-M7 · Plan `:3` and `docs/rfcs/README.md:97` are stale**
  (*"ROUND-3 AUDIT"*; *"the bundle is at `aae6160`"*). **M-M4 · ADR 0019 publishes a grep that cannot match**
  (`grep` without `-E` and a `|` alternation — 0 hits by construction; the conclusion is true, the evidence
  vacuous). **X-M8 · Task 9.6's stated reason for the unconditional opt-out is falsified by D-M2** (under the
  reordered guard the probe is never called; the conclusion survives, the reason does not). **R-M6 · the
  `countingSharedChannel` fake never states it must implement `Send`.**

---

## §3 · VERIFIED SOUND — do not re-open in round 8

Every item below was **run**, not read, this round.

- **Every gate in both documents executed.** All **11** Plan §11 baseline gates print `RED` exactly as pasted;
  all **6** Spec §8.0b gates print RED; Spec AC-10's four arms reproduce exactly. **No gate in either document
  is green-with-zero-work today, and none is unsatisfiable** — round 6's `grep -A/-B` → `go doc` conversion is
  correct and complete *for the gates that exist* (the defect is the gates that are missing, R-B3).
- **`go doc` genuinely extracts interface METHOD comments and struct FIELD comments** — the non-obvious
  assumption the entire gate set rests on, verified with a probe module.
- **The D-M wrap mechanics hold** (the load-bearing claim nobody had run):
  `fmt.Errorf("%w: …", msgin.Permanent(msgin.ErrNilFunc))` → `errors.Is(err, ErrNilFunc) = true` **and**
  `IsPermanent(err) = true`. Same for revised D-K's `%w` over `ErrPayloadType`. `permanentError` has `Unwrap()`.
- **Every source citation in Tasks 9, 9.5, 9.6, 9.7, 10 checks out** (~40 file:line pairs re-verified), and
  round 6 §6's own corrections (`pubsub.go:158`, `reliability.go:38-49`, the 16-line §5.0 census with **two**
  `routing/router.go` rows) are right.
- **Measurements:** root 14 files · 102 exported · 43 sentinels · `apidiff` 95/6 with the exact six additions ·
  `symmap.tsv` byte-identical at 91 lines · arm 1 → 2 survivors, arm 2 → exactly `WithRelease` · both
  acyclicity arms empty · seven-module `GOWORK=off` loop **all GREEN** · `go.work` has exactly 7 `use` entries.
- **Task 10's parity sources are exact** — 12 test functions and 10 `^func ` lines at the stated line numbers
  (round 6's C-B7 fix is complete). The five `Example*` renames all map to declared provider names.
- **Task 9.6's blast radius reproduces exactly** — 25 sites, `2/2/1/18/2`; only `:446`/`:453` affected; the
  guard has a legal insertion point; the four-arm table is **branch-complete** under D-M2's reordered guard.
- **Ordering 9 → 9.5 → 9.6 → 9.7 → 10 → 11 → 12 has no symbol inversion**; Task 9.7's independence claim holds
  (combinators return a `Predicate`, never a `Step`, so they never call `nilFuncStep`).
- **Meta:** all **24** SHAs cited in the rewritten ADR status lines exist, are on-branch, and carry the claimed
  subjects — **zero fabricated SHAs, a first for this bundle**; all **31** `file:line` citations in those status
  lines resolve; all **89** trailer references resolve to real artifacts; all 29 ADRs retain the full Nygard
  set; `RELEASE.md` matches `go.work` byte-for-byte; `ci.yml`'s comments match its jobs; ADR 0024's absence and
  the `Plan 028` normalization are consistent everywhere.
- **Tree is a safepoint:** `go build` · `go vet` · `go test ./...` 11/11 · `gofmt -l` empty · `git status` clean.

---

## §4 · Fix-pass protocol (counter-rule 6)

**Partitioned by DECISION, not by file.** Owners 1–3 all touch ADR 0029, Spec 014 and Plan 027, so they run
**sequentially**; owner 4's disjoint files run in parallel.

| Owner | Decision(s) | Files |
|---|---|---|
| 1 | D-L (revised), D-O, D-J joins | ADR 0030, Spec §5.1/§8/§10, Plan Task 9.6 + gates |
| 2 | D-M (+`ErrNilSink`), D-N | ADR 0029 §5.0b, ADR 0007, Spec §2.1/§6/AC-5, Plan Tasks 9 + 9.7 |
| 3 | D-K (revised) | ADR 0029 §5.0a–c, Spec §7/AC-10, Plan Tasks 9.5 + 10, `errors.go:6` ownership |
| 4 | none — meta/hygiene | HANDOVER, CLAUDE.md, rfcs/README, plans/006, plans/021, ADR 0027, sizing/scopes/trailers, Task 12 |

**MANDATORY JOIN CHECK after the pass.** For each of D-J, D-K, D-L, D-M, D-N, D-O, mechanically extract from
**every** document: (a) the normative text, (b) the gate, (c) the owning task number — and `diff` them. Round 7
found three blockers with two-line greps of exactly this shape (`grep 'this channel in this process'`,
`grep -c 'Task 9.7'`, the Spec-vs-Plan gate table). **The pass is not complete until the join check is run and
its output pasted.**

**Then:** a **bounded round 8** — scoped only to (a) the joins and (b) the gate sets, not a fresh full-bundle
audit. If it returns no blockers in the new material, implementation starts at Task 9.

---

## §5 · FIX-PASS STATUS — PARTIAL. Read this before resuming.

**The round-7 fix pass is INCOMPLETE.** Owner 1's subagent died mid-edit on an API session limit
(*"Now Spec §8 obligations 11 and 12"*); the coordinator finished its two highest-value items by hand. Owners
2, 3 and 4b have **not run at all**. The tree is **coherent and green but not fully fixed** — every edit below
is applied and verified; everything in "REMAINING" is untouched.

**Tree at the time of writing:** working tree dirty (docs only), **zero `.go` files changed**, `gofmt -l .`
empty, `go build ./...` clean, `go test ./...` **11/11 ok**. Last commit `c4582ba`.

### APPLIED

| Owner | Finding | State |
|---|---|---|
| 4a | M-B1, M-B2, M-B3/R-M1, M-B5, M-B4 (3 links + repo-wide sweep added to CLAUDE.md's quality gates), M-M1, M-M3, M-M7, M-M4 | ✅ complete, 7 files |
| 1 | **D-L (revised)** and **D-O** in `docs/adrs/0030-*` | ✅ complete (ADR 0030 fully rewritten: §1 normative godoc, §Topology narrowed to the broadcast case, guard-side defence) |
| 1 | Spec §8 **obligation 11** rewritten to five parts (a)–(e) | ✅ complete |
| 1→coordinator | **Spec §8.0b's obligation-11 gate was left grepping `'reach other processes'`** — D-L's *superseded* wording, which after the revision survives only inside ADR 0030's withdrawal blocks. The gate had become **unsatisfiable by any correct godoc** | ✅ fixed by hand: 7 conjuncts, each verified to sit within one line of the normative godoc (`go doc` reproduces the comment's own line breaks, so a phrase spanning one can never match) |
| coordinator | **R-B1** — Task 9.6's first checkbox (the last live use of the withdrawn handle-local wording, in the one document that writes the text) | ✅ fixed: now "write ADR 0030 §1 verbatim", with the correction rationale inline |
| coordinator | **R-B3 / X-B8 / D-M1 / X-M7** — the Plan's gate block | ✅ fixed: 11 gates → **16**, diff-identical in coverage to Spec §8.0b (8.11 2→7 conjuncts, 8.11a added, 8.12 line-count → ANDed `grep -q`, 8.13 +1 conjunct, 8.4c–f added, 11c2 tightened). **Re-run: all 16 RED**, transcript pasted. X-B8's per-task baseline split recorded as a three-group table |
| coordinator | **D-M1 / R-M5** mirrored into Spec §8.0b's obligation-12 gate | ✅ fixed (same line-counting bug) |

**Verified after the above:** `grep -rn "this channel in this process"` over the Plan and Spec → **2 hits, both
inside the labelled `ROUND-7 CORRECTION` block**; zero live sites.

### REMAINING — nothing below has been started

**Owner 1 leftovers:**
- Spec **obligation 12**'s *text* (D-M5 — state the wrapper invariant **by shape**: *any* wrapper that does not
  itself declare `SingleSubscriber` is accepted, however it holds the channel. Compile-proven that a generic
  wrapper over the **concrete** type strips the probe identically). The *gate* is fixed; the obligation wording
  is not.
- Spec **§10**'s topology bullet — narrow the reversal claim to the **broadcast** case, per D-L revised.
- Plan **Task 9.6**: D-O's `safeSingleSubscriber` checkbox + the **sixth** truth-table row; **R-M6** (the
  `countingSharedChannel` fake must state it implements `Send`); **X-M8** (the stated reason for the
  unconditional opt-out is falsified by D-M2 — the conclusion survives, the reason does not).
- **D-M6** — ADR 0030's Alternatives table still never weighs the soft opt-in
  (`endpoint.WithRequireExclusiveReply()`, or a warn-level log on the accept-unknown arm). Verify whether owner
  1 landed this before dying; if not, it is outstanding.

**Owner 2 — D-M (+`ErrNilSink`) and D-N: NOT STARTED.** All of §2 "Owner 2": R-B2/D-B6 (Spec §6 still
specifies pre-D-M semantics), R-B4/D-B7/X-B3 (Task 9.7 in no normative document), X-B2 (Task 9.7's RED gate is
unsatisfiable — it measures the bare sentinel), X-B4 (the hard-coded five-file godoc grep misses `errors.go:152`
and `routing/aggregator.go:239`), D-B1 (`ErrNilSink`), D-B4/D-N (the divert fallback + ADR 0007 amendment),
D-B7 (Spec §2.1 row 6's universal quantifier), plus minors D-M1/X-M9, D-M2/X-M2, X-M4, X-M5, X-M14.

**Owner 3 — revised D-K: NOT STARTED.** X-B5 (Task 10's verbatim-recovery instruction reintroduces
`ErrExprResultType` and the pre-D-I premise), D-B8 (`ErrPayloadType`'s godoc widening + the cost record), the
shared half of X-B7, plus X-M3, X-M10, X-M13.

**Owner 4b — Plan-027 hygiene: NOT STARTED.** These were mis-partitioned to owner 4a, whose brief forbade
editing the Plan; it correctly refused them. **X-B6** (the plan's *"`grep -n crontest ci.yml` → no output"*
evidence was falsified by `c4582ba` itself — it now returns **3**; CLAUDE.md's comment-stripped form
`grep -v '^\s*#' … | grep -c crontest` → `0` is the one to propagate), **X-B7** (the two-arm sweep runs only at
Task 9.5; add it to **Task 12** and add the `expr` loop-extension to **Task 10**), **M-M2** (no reverse link to
the round-6/7 records from Spec 014 or Plan 027), **M-M9** (Plan `:3` still says *"ROUND-3 AUDIT"*), plus
X-M1 (re-size 9.6 and 9.7 to **M**), X-M2, X-M6, X-M12.

### THEN — the mandatory join check (counter-rule 6), still owed

For each of **D-J, D-K, D-L, D-M, D-N, D-O**, mechanically extract from *every* document: (a) the normative
text, (b) the gate, (c) the owning task number — and `diff` them. **This has NOT been run.** Round 7 found
three blockers with two-line greps of exactly this shape, and the coordinator found a fourth (the
`'reach other processes'` gate) the same way while cleaning up after owner 1. The pass is not complete until
the join check is run and its output pasted here.
