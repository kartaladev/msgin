# Plan 027 — adversarial design audit, ROUND 8 (bounded)

**Traceability:** audits [Spec 014](../specs/014-core-package-layout.md) · [Plan 027](027-core-package-layout.md) ·
ADRs [0007](../adrs/0007-reliability-settlement-api.md), [0027](../adrs/0027-core-package-restructure.md),
[0028](../adrs/0028-channel-interface-segregation.md), [0029](../adrs/0029-eip-lexical-alignment.md),
[0030](../adrs/0030-reply-channel-exclusivity-probe.md) · [RFC 0002](../rfcs/0002-eip-alignment.md).
Prior: [round 6](027-audit-round-6.md) (read its §6) · [round 7](027-audit-round-7.md) (read its §5).
**Produces:** **D-P**, and corrections to **D-N**, **D-O**, **D-M** and **D-K**.

**Date:** 2026-07-30 · **Tree audited:** `7ee3fd6`. **Code pin:** `dadc775` (`.go` unchanged since `3d0b87a`).

## Scope — this round was deliberately BOUNDED

Not a fresh full-bundle audit. Two lenses only, plus a coordinator-run join check:

| Lens | Scope | Verdict |
|---|---|---|
| **Design of the new decisions** | D-N, D-O, D-M's `ErrNilSink` invariant, revised D-K — never reviewed by anyone | NEEDS-REVISION · 8 blockers, 5 minors |
| **Gate sets** | Every verification command in Spec §8/§9 and Plan §11, executed | NEEDS-REVISION · 6 blockers, 6 minors |
| **Joins** (coordinator, `joincheck.py`) | Cross-document agreement on the six decisions | 2 defects found and fixed in-pass |

**Both lenses proved their findings by compiling and running**, in throwaway worktrees at `7ee3fd6`.

> ### INCIDENT — an auditor clobbered uncommitted work on the main tree
>
> The gate lens ran `git checkout -- .` with its cwd unexpectedly on the main tree (a compound command's `cd`
> into a reaped scratch worktree failed silently). It discarded two uncommitted files. It had captured a diff
> of `docs/plans/027-core-package-layout.md` and **restored it byte-for-byte** (28 insertions / 9 deletions,
> diffstat matched); `CLAUDE.md` was **lost and re-authored by the coordinator** from the same session's text.
>
> **Rule adopted:** an auditor briefed READ-ONLY must be given a **worktree copy**, never the working tree —
> "read-only" in a brief is not read-only in permissions. A destructive command (`checkout --`, `clean`,
> `reset`) must never appear in an audit brief's toolkit at all.

---

## §1 · DECISION — D-P: the invalid-path fallback is SINGLE-SHOT

**Amends D-N** (round 7). **Prompted by** design B1, compile-proven.

D-N routed a permanently-invalid message to `RetryPolicy.DeadLetter` when no `WithInvalidMessageSink` is
configured, instead of discarding it. Implemented exactly as specified and run against a **DLQ whose `Send`
returns an error**, in the default configuration:

```
BEFORE D-N: deliveries=1   acks=1  nacks=0   dlqSends=0   OnInvalid=1  OnDeadLetter=0  OnRetry=0
AFTER  D-N: deliveries=41  acks=0  nacks=40  dlqSends=40  OnInvalid=0  OnDeadLetter=0  OnRetry=40
```

(41 was the harness's redelivery cap, reached in under 10 ms. It is **unbounded** in reality.)

`divert`'s send-failure arm Nacks with `requeue=true`, so a **permanent** message re-enters the flow forever,
and **every bound the library owns is structurally blind to it**:

- **MaxAttempts** — never consulted; the permanent arm bypasses `c.attempts(d)` and passes `attempt` = `1`.
- **Backoff** — `retryDelay(policy, 1)` = `Backoff.Delay(0)`, the *first* step, every iteration. Never escalates.
- **Circuit breaker** — measured: `Record(true)=30, Record(false)=0`. `consumer.go:614` maps
  `IsPermanent → healthy`, so the breaker is told the flow is fine 30 times while the message spins.

**ADR 0007 D7 is the authority being violated**, in its own words: *"retrying anyway would only convert a
configuration gap (no sink configured) into an infinite-retry trap, which is **worse** than a logged,
observable discard."* D-N recreated precisely that trap. Its amendment note argued only the
discard-vs-durable-sink axis and never re-weighed the axis D7 actually decided.

**Decision.** The fallback is **single-shot**. If the fallback target's `Send` fails, **fall through to D7's
discard** — WARN naming both the classification cause *and* the sink error, then Ack — rather than Nacking.
This keeps D-N's gain (a reachable DLQ captures the message) without surrendering D7's guarantee (an invalid
message always terminates). One extra branch, one extra covering case.

**Rejected alternative:** bounding the loop by threading a real attempt count into the invalid-path `divert`.
More code, more state, and it still leaves a permanent fault consuming retry budget for no benefit.

**Class violated (record it):** *a settlement path that is terminal by construction must not become
non-terminal without a bound.* Pre-D-N the no-invalid-sink path had exactly one outcome (Ack); D-N gave it a
retry loop no counter, backoff or breaker could see.

**D-N item (b) — the decode-arm widening — is CONFIRMED, kept as specified**, conditional on D-P plus the
consequence records below. Scoping it back is not cleanly implementable: `:716` receives *any* `IsPermanent`
error, so restricting the fallback to nil-endpoint faults would require an `errors.Is` against a closed list of
endpoint sentinels — exactly the closed-enumeration anti-pattern D-M rejected. And `WithInvalidMessageSink`'s
own godoc already unifies the two arms (*"where **permanent/undecodable** messages are diverted"*), so honoring
it for one arm and not the other makes that option's godoc unwritable. **But (b) is a NEW policy, not a
restoration** — D-N's justification ("D-M moved messages off a path that used to reach the DLQ") is true only
of `:716`; the decode arm was never on that path. It must not be justified by D-M's premise.

---

## §2 · BLOCKERS, by owner

### Owner A — D-N / D-P consequences, and D-M's producer-side blast radius

- **A1 (design B1) — implement D-P** as decided above; add the covering case for *"fallback target configured,
  its `Send` fails"*, which is the newly reachable state and today has **no case at all**.
- **A2 (design B2) — D-N's Consequences weigh only data loss.** Two countervailing costs are unrecorded:
  **(i) `ErrPayloadTooLarge` becomes DLQ amplification** — `WithMaxPayloadBytes` exists to stop the runtime
  *decoding* untrusted oversize bytes, but `divert` sends `d.Msg`, so msgin now **persists into the operator's
  durable queue exactly the bytes the cap declared illegitimate**; **(ii) poison-storm volume** — a mis-configured
  codec turns 100% of the stream into durable DLQ writes where it was previously N log lines. CLAUDE.md names
  this shape verbatim (*"a wrong default could … be a DoS lever"*). The trade is still worth taking; **"accepted
  and stated" is the bar**, and silence is the defect round 7 already ruled on for D-K.
- **A3 (design B3) — the DLQ now receives two operationally distinct classes with no discriminator that
  survives the process.** *"Retries exhausted, may be replayable"* and *"permanently invalid, replaying is
  pointless"* land in one sink; the only discriminator is which in-process hook fired, and `message.go:15-24`
  defines no settlement-reason header. **CLAUDE.md's multi-instance rule applies directly** — the classification
  lives in one process's memory while the artifact lives in a store another process reads. Fix: stamp a
  `HeaderSettlementReason` on the diverted message, or — minimum — record that a shared DLQ cannot distinguish
  them and that operators wanting the distinction must configure `WithInvalidMessageSink`. A replay tool built
  on the pre-D-N assumption will otherwise retry poison forever.
- **A4 (design B4) — the D-N godoc sweep is scoped to D-M's sentinel names**, so it cannot see the one godoc
  that states the old behavior verbatim: `endpoint/consumer.go:66-67`, *"If unset, such messages are logged and
  discarded (ADR 0007 D7)"* — false for every finite-retry consumer. Same-shaped survivors: `reliability.go:9`,
  `:17`, `errors.go:14`, `endpoint/flowcontrol.go:99`. **Class:** *a behavior change's godoc sweep must be
  derived from the behavior changed, not from the sentinels that motivated it.*
- **A5 (design B5) — D-M's blast radius was measured on the consumer only; the Producer loses the same DLQ
  capture.** `endpoint/producer.go:453-455` returns on `IsPermanent` **before** `p.deadLetter(...)`. Measured:
  `dlqSends 1 → 0` and the exported sentinel `errors.Is(err, ErrDeadLettered)` flips **`true` → `false`**.
  Round 7 recorded D-N's premise as *"no configuration that previously captured a message starts dropping it"* —
  true for the consumer, **false for the producer**. Not necessarily wrong behavior, but an unrecorded
  observable change to an exported error contract, in a register whose stated purpose is that none rides in
  silently. Needs a §2.1 row (or explicit sub-clause), an ADR line, and a case.

### Owner B — D-O, D-M's invariant, D-K's godoc

- **B1 (design B6) — D-O destroys the evidence of the fault it recovers from and reports a false diagnosis.**
  Measured, with a genuinely-exclusive channel whose probe panics: `err = "reply channel is not exclusive to
  this exchange"`, `Is(ErrSharedReplyChannel) = true`, **panic value recoverable from err = false**. The channel
  *is* exclusive; the error says it is not, and with the default discard logger **nothing survives**. **The repo
  states the violated rule outright** — `endpoint/poller.go:100-105`: *"safePoll does NOT log … the existing
  error path already logs … with this error, **whose text carries the recovered panic value**"* — and all six
  `safeX` members with an error return embed `%v` of the recovered value. `safeSingleSubscriber` has one (the
  constructor's) and is the sole proposed member to discard it. **Fail-closed is right; a separate sentinel is
  not needed; the logger *is* available.** The defect is only that the log is the *sole* carrier. Fix: return
  `(bool, error)` and wrap — `fmt.Errorf("%w: %w", msgin.ErrSharedReplyChannel, cause)` — so `errors.Is` is
  unchanged and no gate moves. Also add the third cause to `ErrSharedReplyChannel`'s godoc, and make truth-table
  row 6 assert the panic substring is in `err.Error()`, not merely the sentinel — as written it passes against
  the diagnosis-losing implementation.
- **B2 (design B7) — D-M's sentinel-agnostic invariant is FALSE as stated, and the plan writes it verbatim into
  a public godoc.** Compile-proven counter-examples reachable from inside a `MessageHandler` body today:
  `Chain(To(*DirectChannel)).Handle → ErrNoSubscriber, IsPermanent=false` and
  `Chain(To(*QueueChannel[reject])).Handle → ErrOverflowDropped, IsPermanent=false`. Three of the seven
  "deliberately bare" triages therefore rest on a **false rationale** (*"no `MessageHandler` body"*), though
  their conclusions survive. The invariant over-reaches twice: *"deterministic"* is undefined and carries the
  whole load (the real property is **immutable at construction**), and its two arms do not partition the space —
  the largest class (typed errors returned to a *caller* from a non-constructor API) is unaddressed while the
  ADR instructs a maintainer to *"triage the 63"* against it. **Corrected wording:**
  > Every typed error msgin returns from inside a `MessageHandler` body **whose cause was fixed at construction
  > and cannot change for the message's lifetime** is `Permanent`; a fault that a later `Subscribe`, config
  > reload or drain could resolve stays bare and transient; and every typed error returned from a constructor is
  > bare, because construction never reaches a `RetryPolicy`.
  Then correct the two triage cells to cite **mutability**, not *"no `MessageHandler` body"*.
- **B3 (design B8) — revised D-K's drafted `errors.go:6` godoc over-claims and is falsified by D-N.**
  (i) It names three producers as wrapping `want %T, got %T`; only `payload.go:15` does — `endpoint/consumer.go:831`
  and `:838` return the sentinel **bare**, and the trade-off's whole remedy is *"the error string carries the
  discriminator"*. (ii) It says either class is diverted *"to the invalid-message sink"* — falsified by **D-N**,
  decided in the same round: with no invalid sink (the default) it goes to the **dead-letter** sink. *A
  round-7-drafted godoc contradicting a round-7 decision* — the join failure, now between two decisions inside
  one round.

### Owner C — the gate documents

- **C1 (gate B1) — round 7 fixed the §11 baseline block and left the per-checkbox gate arrows on the
  pre-round-7 set.** Task 11b's preamble says *"Each line pairs the edit with the command that proves it"* — the
  arrows are the **instruction**; the block is the transcript. Still live: `:2237` the **line-counting**
  obligation-12 form R-M5 declared replaced; `:2227` **2 conjuncts where the obligation has 7** (R-B3's exact
  defect); `:2242` 1 conjunct, missing `ErrChannelSubscribed`; `:2281` `grep -qi instance` (X-M7's
  incidental-word gate); and **§8.11a has no checkbox at all**. None self-satisfies, so this is not a false
  GREEN — it is a **weaker-instruction path**: the worker writes 2 phrases, then the Verify fails with no
  diagnosis. **Fix the class:** delete the arrows; each checkbox cites a gate id (`→ gate 8.12, §11 block`) so
  there is one source, not two copies.
- **C2 (gate B2) — *"Standing check, now a Global Constraint"* was written; the constraint was never created.**
  The Global Constraints list runs 0–9 and contains no such rule. The mechanism meant to stop the divergence
  recurring does not exist. *(Coordinator's own defect, from the round-7 pass.)*
- **C3 (gate B3) — ADR 0030 §1 publishes a gate form neither gate document has** (*"pipes every `go doc` through
  `sed 's,//, ,g' | tr -s '[:space:]' ' '`"* — one hit repo-wide: the sentence itself). Verified **safe to
  adopt** (all 7+1+4 conjuncts still match under it; obligation 12's two new conjuncts stay RED), so adopting it
  is the cheap fix — **but the ADR's stated reason is half-wrong**: the claimed MATCH→NO-MATCH flip did not
  reproduce in **0 of 46** perturbations. Fix the claim or fix the blocks; do not leave a false one standing.
- **C4 (gate B4) — the per-task RED-pinning table contradicts every other ownership statement.** It pins
  8.10–8.13 to Task 9.6, while Spec §8 says *"all are Plan 027 Task 11 checkboxes"*, the Risks table says
  *"Task 11b/11c owns all thirteen"*, Task 11 carries unchecked items for all four, Task 9.6's own §8.12
  checkbox defers to Task 11b, Task 9.6 has **no** obligation-10 checkbox — and **Task 9.6's Verify contains no
  `go doc` gate at all**, so it never measures the two root symbols and the normative godoc it is the sole
  writer of. That last point is the structural cause.
- **C5 (gate B5) — the baseline heading and Task 11's Verify still demand all-16-RED**, contradicting the split
  table directly beneath them. The correction note landed; the corrected artifact did not. *(Coordinator's own
  defect, same shape as C2.)*
- **C6 (gate B6) — Task 9.6's Verify says the truth table shows FOUR subtests**; the same task's body requires
  **six** (AC-9's ordering row is the fifth, D-O's panicking probe the sixth). A worker satisfying the Verify as
  written ships without either.

### Minors (both lenses)

Design: no case for D-P's new branch · `invalidTarget()`'s drafted nullary signature cannot satisfy its own WARN
checkbox (the sibling WARN carries `d.Msg.ID()`) · the fallback WARN is undeduplicated for a condition constant
for the consumer's lifetime (contrast `governorPanic`, which deduplicates for exactly this reason) · the
invalid-path `attempt` is hard-coded `1`, so `retryDelay` always returns the first backoff step.
Gate: Task 9.7 gate 1's GREEN rows 2/5 publish a state the task's own commit never leaves behind (D-N lands in
the same commit) · Task 10's Verify says *"three"* `ErrPayloadType` conjuncts where there are four · Spec §8's
obligation-4 evidence still uses a guessed `-B8` window · **Spec §10 still carries D-L's superseded wording**
(round 7 §5 logged it open; still open) · Task 9.7's header says both *"placed after 9.6"* and *"runs first"* ·
cosmetic `-Eiq`/`-Eqi` divergence defeats the literal diff C2's missing constraint would run.

---

## §3 · VERIFIED SOUND — do not re-open in any later round

- **All 16 Plan §11 gates print RED**, reproducing the pasted transcript line-for-line; **all 6 Spec §8.0b gates
  RED**, each classified individually as symbol-absent vs phrase-absent — **no gate is RED for a wrong reason.**
- **All five D-J/D-L/D-O gates PROVEN SATISFIABLE** in a worktree: `ExclusiveSubscribable` added with ADR 0030
  §1's godoc **verbatim** → `8.10 8.11 8.11a 8.12 8.13` all GREEN, every one of the 7+1+4 conjuncts exiting 0
  individually. **Every obligation-11 anchor sits within one source line**, confirmed against an in-tree control
  that `go doc` reproduces interface method comments verbatim.
- **AC-10's five arms reproduce exactly**; arm 3 is the bundle's only GREEN and is correctly labelled a
  no-regression guard. `go doc` resolves workspace-sibling modules, so Task 10's gates are runnable once `expr`
  joins `go.work`.
- **Task 9.7 gate 1 is sound and reproduces exactly** — it measures the producer path, the observable the change
  moves. **Gate 2 is correctly labelled a no-regression guard**; all five census rows and both
  `RetryPolicy.Validate()` lines reproduce verbatim. Its class sweep (12 lines / 43 sentinels) and godoc sweep
  (15 lines) both reproduce.
- **Staleness sweep: both invocations present and correctly ordered**; `symmap.tsv` regenerates byte-identical
  at 91 lines; arm 1 → 2 survivors, arm 2 → `WithRelease`; declared-side arithmetic (root + 10 = eleven, `+expr`
  = twelve) checks out and Task 10's checkbox is the executable half.
- **Plan §11 ⊇ Spec §8.0b on the six shared obligations** — R-B3's "strictly weaker" defect **is genuinely fixed
  in the baseline block**; C1 is that the fix did not reach the checkboxes.
- **Revised D-K's widening is otherwise sound** — it names both conditions, `IsPermanent` genuinely enumerates
  the sentinel (`reliability.go:46`), and the trade-off is stated as accepted rather than absent.
- **D-O's shape is right** — fail-closed is correct, a separate sentinel is unnecessary, and `cfg.logger` is
  available at the guard.
- **Joins:** Spec §2.1 has **7 rows** and the count has been removed from every heading that referred to the
  table by length; `expr.ErrExprResultType` survives only in withdrawal contexts; all six decisions resolve to
  an owning task in every document that carries them.

---

## §4 · Fix-pass partition (counter-rule 6 — by decision, not by file)

| Owner | Decisions | Findings |
|---|---|---|
| **A** | D-P, D-N consequences, D-M producer-side | A1–A5 + design minors |
| **B** | D-O, D-M's invariant, D-K's godoc | B1–B3 |
| **C** | the gate documents | C1–C6 + gate minors |

Owners A and B both touch ADR 0029 and Spec §2.1; A, B and C all touch Plan 027. **Run sequentially.** End with
the join check (`docs/plans/027-tools/joincheck.py`) and a re-run of the full gate baseline.

**Then implementation begins at Task 9.** Round 8 is the last audit round: its findings are overwhelmingly stale
*instructions* rather than wrong *design*, and the remaining risk is better caught by writing the code against
these gates than by another pass over the documents.

---

## §5B · FIX PASS — OWNER B (B1, B2, B3) · complete

All three findings **confirmed by compile proof** in a throwaway worktree at `7ee3fd6` (removed afterwards).
Every claim below was re-run against the tree, not recalled.

### B1 — D-O2: the recovered panic now rides in the error

**Confirmed.** Round-7's `safeSingleSubscriber(ex, log) bool` + bare-sentinel guard, implemented verbatim, with
a **genuinely exclusive** channel (`struct{ *channel.DirectChannel }`, second `Subscribe` →
`ErrChannelSubscribed`) whose probe panics:

```
err                                      = "msgin: reply channel permits multiple subscribers; it is not exclusive to this exchange"
errors.Is(err, ErrSharedReplyChannel)    = true
panic value recoverable from err         = false
errors.Unwrap(err)                       = <nil>
anything on stderr/stdout from the logger= (nothing: cfg.logger defaults to io.Discard)
```

With `(bool, error)` + `fmt.Errorf("%w: %w", msgin.ErrSharedReplyChannel, cause)`:

```
err                                      = "msgin: reply channel permits multiple subscribers; it is not exclusive to this exchange: SingleSubscriber panicked: probe: nil map read in tenantExclusivity[tenant]"
errors.Is(err, ErrSharedReplyChannel)    = true
panic value recoverable from err         = true
```

**The rule the round-7 form violated is written down in-repo**, in the godoc of the one `safeX` member that
deliberately does *not* log (`endpoint/poller.go:100-105`): *"safePoll does NOT log — pollLoop's existing error
path already logs … with this error, **whose text carries the recovered panic value**"*. Measured
workspace-wide, **8 of 8** recover-wrappers around caller code that return an error embed `%v` of the recovered
value (`endpoint/consumer.go:863,885,909,921,935` · `endpoint/poller.go:109` · `endpoint/producer.go:563` ·
`channel/pubsub.go:203`). The ninth, `safeLimiterWait` (`consumer.go:514`), sets `err = nil` **deliberately**
(fail *open*) and surfaces `r` through `governorPanic` — it has no error to carry anything.

**NO GATE MOVES — verified, not asserted.** `safeSingleSubscriber` is unexported (invisible to `go doc`); no
gate anywhere reads `go doc $M.ErrSharedReplyChannel` (the three hits for that identifier all read
`go doc …/endpoint.NewChannelExchange` and match it as a *string* inside that godoc). The five D-J/D-L/D-O
gates, run in the worktree with ADR 0030 §1's godoc pasted verbatim, against **both** implementations:

```
===== WITH the B1 fix ((bool, error) + wrap) =====     ===== WITHOUT the B1 fix (ADR 0030 as specified) =====
GREEN: 8.10                                            GREEN: 8.10
GREEN: 8.11                                            GREEN: 8.11
GREEN: 8.11a                                           GREEN: 8.11a
GREEN: 8.12                                            GREEN: 8.12
GREEN: 8.13                                            GREEN: 8.13
```

**Caveat recorded for the test author:** `errors.Unwrap(err)` returns `nil` on a two-verb `%w` wrap (the chain
is `Unwrap() []error`). Assert with `errors.Is` / `errors.As` / `err.Error()`, never `errors.Unwrap`.

**Edited:** ADR 0030 header (D-O2 fold), §3 guard snippet, §3 `ErrSharedReplyChannel` godoc (**third cause**),
§3a (measurement + fix + no-gate-moves proof + helper), Consequences hot-path bullet (row 6 must assert the
panic substring) · Spec §5.1 guard snippet + D-O2 paragraph + normative branch set · Plan Task 9.6
`errors.go` checkbox, `safeSingleSubscriber` checkbox, truth-table row 6, sixth-row covering case.

### B2 — D-M's invariant was FALSE; corrected, and checked against all twelve

**Confirmed, compile-proven** (`go run`, output pasted whole):

```
Chain(To(*DirectChannel)).Handle       err=msgin: channel has no subscriber
  errors.Is(err, msgin.ErrNoSubscriber)=true  msgin.IsPermanent(err)=false
Chain(To(*QueueChannel[reject])).Handle err=msgin: message dropped by overflow policy
  errors.Is(err, msgin.ErrOverflowDropped)=true  msgin.IsPermanent(err)=false
```

`To(sink)` returns a `Step` whose `HandlerFunc` body msgin owns (`handler.go:52-59`), composed by `Chain`, so
both are typed errors msgin returns **from inside a `MessageHandler` body** and neither is `Permanent`. Both
are **correctly** transient, so the withdrawn wording did not mis-describe an edge — it demanded a wrap that
would be wrong. Plan Task 9.7 was about to write it verbatim into `errors.go:152`, a public godoc.

**The corrected invariant carries FOUR arms, not three.** Round 8 §2's prescribed wording has three; checking
it against all twelve swept lines left `endpoint/producer.go:589` outside every one of them (not a
`MessageHandler` body; not resolved by a `Subscribe`/reload/drain; `SendAfter` is not a constructor). A fourth
clause — *"and everything else, a typed error handed to a caller from a non-constructor API, is bare for the
same reason"* — was added and is **flagged as this pass's own extension**, not applied silently.

**It holds for all twelve.** Sweep re-run on the working tree (43 sentinels, `| sort`-pinned, byte-identical to
the block in ADR 0029 §5.0b): arm 1 → `endpoint/helpers.go:21`, `routing/helpers.go:23`,
`transform/transformer.go:38`, `routing/router.go:48`, `handler.go:55` (the five edit sites) · arm 2 →
`routing/router.go:56`, `channel/direct.go:87`, `adapter/memory/queuestore.go:146` + `:151` · arm 3 →
`retry.go:48` + `:51` · arm 4 → `endpoint/producer.go:589`. **Twelve for twelve, no arm vacuous.**

**Three triage cells corrected, no conclusion changed:** `queuestore.go:146`/`:151` and `direct.go:87` cited
*"no `MessageHandler` body"* — false per the transcript — and now cite **mutability** (a drain / a later
`Subscribe` resolves the cause). Incidental accuracy fix in the same table: `producer.go:589` sits in
**`SendAfter`**, not `SendAt` (verified: enclosing `func` at `586`; `SendAt` is one-line sugar at `:602`).

**Edited:** ADR 0029 header (round-8 fold), §5.0b invariant + withdrawal block + twelve-line check table +
triage cells + the "triage the 63" arm reference · Spec §2.1 row 6 · Plan Task 9.7 `errors.go:152` checkbox
(with a ⛔ do-not-write-the-old-sentence block), the twelve-line triage restatement, the arm reference.

### B3 — D-K's `errors.go:6` draft: both defects confirmed

**(i) Over-claim confirmed.** Only `payload.go:15` wraps `want %T, got %T`; `endpoint/consumer.go:831` (live
value) and `:838` (wire `[]byte`) return the sentinel **bare**. **RESOLUTION: narrow the sentence, do not wrap
the two sites** — wrapping is a behavior change to shipped code (new error string, new cases, its own §2.1 row)
inside a task scoped to the `expr` module plus one root comment, and D-K does not need it: the discriminator is
`"expr result"`, whose **absence** identifies the payload side. *(Backlogged rather than absorbed: those two
sites carry no type information at all in their error string — a pre-existing decode-path debuggability gap
that predates D-K and wants its own finding.)*

**(ii) D-N falsification confirmed.** *"either class is diverted to the invalid-message sink"* names the
**non-default** arm. Under D-N as amended by **D-P** the ladder is: `WithInvalidMessageSink` if configured →
else **single-shot** `RetryPolicy.DeadLetter` → else logged discard (ADR 0007 D7). The sentence now states all
three.

**NO GATE MOVES.** The corrected godoc was pasted into `errors.go` in the worktree and AC-10's fifth arm run
verbatim — all four conjuncts flip RED→GREEN, each phrase still within a single godoc line:

```
PAYLOAD SIDE         exit=0
EXPRESSION SIDE      exit=0
ACCEPTED TRADE-OFF   exit=0
expr result          exit=0
```

**Edited:** Plan Task 10's `errors.go:6` godoc draft + a ⛔ correction block + the `expr` provider checkbox's
diversion sentence · Spec §7's discriminator bullet and its `IsPermanent`-comes-for-free bullet.

### Left for another owner (found in passing, NOT fixed by B)

- **Spec AC-9 still says the probe table covers *"all four arms"***, while Spec §5.1 and Plan Task 9.6 both
  carry **six** rows (four truth-table arms + AC-9's ordering row + D-O's panicking row). Same shape as
  **C6**, which owns the Task 9.6 Verify half; AC-9 is its Spec counterpart and no owner is currently assigned
  to it. *(**Picked up by owner C** — see §5C. Both halves fixed in one pass.)*

---

## §5C · FIX PASS — OWNER C (C1–C6 + gate minors) · complete

Every gate touched was **run**, not recalled. The full §11 block was re-run at the end and is pasted in §5D.

### C1 — the per-checkbox arrows are DELETED; one source, cited by gate id

**Confirmed as described.** Fixed as a **class**, not five instances: the arrows and the §11 block were two
copies of one artifact, and round 7 fixed only the copy it was looking at. Every 11b/11c checkbox now reads
`→ gate <id>, §11 block` and restates no command. **§8.11a's missing checkbox is added.** Verified in both
directions, mechanically:

- every checkbox cites a gate — **11 of 11** (`§8.10, §8.11, §8.11a, §8.12, §8.13, §8.1, §8.3, §8.4, §8.7`,
  `11c1`, `11c2`);
- every gate id in the block is cited by a checkbox — **16 of 16**.

The block's own label changed from `GREEN(bad, no work needed)` to plain `GREEN`, because under the per-task
pinning a GREEN is *expected* for eleven of the sixteen.

### C2 — Global Constraint 10 now exists, and it is executable

The round-7 sentence *"Standing check, now a Global Constraint"* described a constraint that was never
written; the list ran 0–9. **Constraint 10** now requires Plan §11 ≡ Spec §8.0b **on the six shared gate ids**
and ships the `diff` that proves it. Both blocks open with `# ==== CANONICAL GATE BLOCK` at column 0 and use
the identical `g <id> "<cmd>"` form, so the check is literal:

```
$ diff <(gates docs/plans/027-core-package-layout.md | grep -E '^g (8\.10|8\.11|8\.11a|8\.12|8\.13|11c1) ') \
       <(gates docs/specs/014-core-package-layout.md)
(no output — diff-identical)
```

Spec §8.0b was rewritten into that form; its per-obligation commentary moved out of the fenced block into a
table beside it, so the block itself stays literally diffable. The **Plan's block is a superset** (ten
Plan-only gates), which the `grep -E` filter selects for; the Spec's side needs no filter, so a Spec-only gate
fails the diff in the other direction.

**One self-inflicted defect, found by running the new gate rather than reading it.** The first draft used an
unanchored `/==== CANONICAL GATE BLOCK/`, which also matches the *constraint's own prose* — and that prose sits
earlier in the Plan than the block it describes, so the `sed` range opened at the constraint and ran to the
block's closing fence. The `diff` still passed (its `grep -E '^g '` filtered the noise out), but `eval`ing the
extracted range died with `parse error near '}'`. The marker is now anchored `^# `. A gate whose failure mode
is invisible to the gate itself is the shape three of these rounds have been about.

### C3 — the ADR's pipe is ADOPTED in both blocks, and its stated reason is CORRECTED

**Option chosen: adopt.** Dropping the sentence would also discard the true half of its reason, which is the
reason obligation 11's part (b) is anchored on two short spans in the first place. Measured 2026-07-30 in a
probe module with ADR 0030 §1's godoc pasted verbatim plus a four-outcome `NewChannelExchange` godoc:

```
8.10   ExclusiveSubscribable                                            raw=MATCH  piped=MATCH
8.11   MUST NOT compute it from a live subscriber count                 raw=MATCH  piped=MATCH
       reaches at most one recipient                                    raw=MATCH  piped=MATCH
       any recipient other than the single subscriber registered on it  raw=MATCH  piped=MATCH
       recipient in another process                                     raw=MATCH  piped=MATCH
       constant for the lifetime                                        raw=MATCH  piped=MATCH
       safe for concurrent use                                          raw=MATCH  piped=MATCH
       MUST NOT block and MUST NOT panic                                raw=MATCH  piped=MATCH
8.11a  promotion                                                        raw=MATCH  piped=MATCH
8.12   ErrSharedReplyChannel / ErrChannelSubscribed /
       does not implement / within this process                         raw=MATCH  piped=MATCH  (×4)
8.13   suppress / ErrChannelSubscribed                                  raw=MATCH  piped=MATCH  (×2)
```

**14 of 14 under both forms — adopting flips no verdict**, and the block stays all-RED on the untouched tree.
Phrases that span a line break match only under the pipe:

```
INCLUDING a recipient in another process     raw=NO   piped=MATCH   (interface method comment, // markers)
MUST therefore return false                  raw=NO   piped=MATCH   (interface method comment)
the probe at all; any wrapper                raw=NO   piped=MATCH   (func comment, re-wrapped)
```

**The ADR's reason was half-wrong and is now corrected in place.** It claimed a func-comment gate *"flips
MATCH→NO-MATCH when the **preceding sentence** changes length"*. `go doc` re-wraps **each block
independently**:

```
perturbing the INTRO PARAGRAPH   (46 shifts) -> 'does not implement' NO-MATCH 0/46   [reproduces round 8's 0/46]
perturbing INSIDE THE SAME BULLET (46 shifts) -> 'does not implement' NO-MATCH 18/46  (first at a 23-char shift)
piped, either perturbation                    -> NO-MATCH 0/46
```

So the hazard is real and the pipe is the right fix; the trigger is **same-block** edits, not preceding ones.
ADR 0030 §1's *"normative down to its line breaks"* is narrowed to *"normative in its wording; copy verbatim"*,
and the two sites that told a worker to preserve line breaks *for the gate's sake* (Plan Task 9.6's checkbox,
Spec obligation 11(b)) are corrected to say wording still matters exactly and line breaks no longer do.

### C4 — the ownership contradiction, resolved by a rule rather than a vote

**Resolution: a godoc obligation is owned by the task that CREATES the symbol it documents.** Go has no state
in which an exported symbol exists without its doc comment, so Task 11 could never be "the writer" of godoc
Task 9.6 must already have written to commit a green unit. Therefore:

| Obligation | Owner | Gates |
|---|---|---|
| §8: 10, 11, 11a, 12, 13 | **Task 9.6** (declares `ExclusiveSubscribable`, `ErrSharedReplyChannel`, `WithSharedReplyChannel`; rewrites `NewChannelExchange`'s godoc) | 8.10 · 8.11 · 8.11a · 8.12 · 8.13 |
| §8: 4, for the four types Task 9 creates | **Task 9** | 8.4c–8.4f |
| §8: 1, 3, 7, 4-for-the-two-shipped-types · §10: (c), (d) | **Task 11b / 11c** | 8.1 · 8.3 · 8.7 · 8.4a · 8.4b · 11c1 · 11c2 |

Task 11 **re-verifies** the first two groups as no-regression checks and must still run after Task 9.6
(`go doc` cannot read an undeclared symbol). Every document that carried a contrary statement was changed:
Spec §8's *"All are Plan 027 Task 11 checkboxes"* → the owner table; Spec §8's intro banner; Spec AC-8; the
Plan's Risks row; the Plan's §11 pinning table (an **owner** column added); Task 11's intro; Task 11's five
9.6-owned checkboxes (now labelled *"Written by Task 9.6 → verify with gate X"*); Task 9.6's *"let Task 11b
own the final wording"* and *"Final wording: Task 11b"*.

**The structural cause is fixed:** Task 9.6's Verify now runs gates 8.10/8.11/8.11a/8.12/8.13 (14 conjuncts),
RED before and GREEN after, and explicitly notes that **11c1 is not its gate** and stays RED. The identical
hole in **Task 9** — its Spring-equivalent checkbox with no gate, while the pinning table pinned 8.4c–8.4f to
it — was fixed in the same pass rather than left as the next instance.

### C5 — the all-16-RED demand

The heading (*"run this BEFORE any edit; every line must print `RED`"*) and Task 11's Verify (*"re-run to
all-GREEN … the RED before"*) both contradicted the pinning table directly beneath them. The heading now names
the per-task pinning and labels the all-RED transcript as the historical baseline; Task 11's Verify carries a
three-row table of what is GREEN on arrival versus what goes RED → GREEN inside Task 11.

### Gate minors — all six applied

| Minor | Fix |
|---|---|
| Task 9.7 gate 1's GREEN rows 2/5 | Annotated **in place** (`← D-M ONLY`) plus a note: D-N lands in the same commit, so those two rows end at `dlqSink=1 … discarded=false`; `discarded=true` survives only where there is no invalid sink **and** no reachable DLQ (D-P's fall-through). Rows 1, 3, 4, 6 explicitly marked unaffected, so nobody "corrects" them |
| Task 10's Verify *"three"* `ErrPayloadType` conjuncts | → **four**, named individually; the dropped one is `expr result`, the discriminator the trade-off's remedy depends on |
| Spec §8 obligation-4 evidence | `grep -n -B8 'type CorrelationStrategy' routing/aggregator.go` (a guessed window) → **gates 8.4a/8.4b**, re-run: both exit 1 |
| Spec §10's D-L wording | The last live site of the superseded predicate (*"deliveries reach other processes MUST return false"* + *"THIS exchange will be the sole recipient"*) rewritten to revised D-L, with the withdrawal recorded. Round 7 §5 logged it open; verified by sweep that every surviving occurrence is now inside a labelled withdrawal block |
| Task 9.7's header ordering | *"placed HERE (after 9.6, before 10)"* now carries a ⚠️ pointer to the ROUND-7 CORRECTION that supersedes it (*"runs first, before Task 9"*), keeping the three reasons that still constrain where it may not go |
| `-Eiq`/`-Eqi` + quoting divergence | Every gate conjunct in both blocks normalised to `grep -q '…'` / `grep -qi '…'` / `grep -Eq '…'` / `grep -Eqi '…'`, always quoted; `-Eq` on non-alternations demoted to `-q`. The one spelled-out `-Eiq` command outside the blocks (Spec §8.0a row (c)) now cites **gate 11c1** instead. This is what makes C2's literal `diff` possible |
| Plan `:3` headline | *"AUDITED THROUGH ROUND 7"* → round 8, with the round-8 reading list and the note that the headline is part of every round's fix pass |

### Portability hazard (owner A's finding) — stated as a rule, applied to what this pass touched

`grep` on this machine is a **ugrep 7.5.0** wrapper that **strips the `./` prefix** system `grep -rn … .`
emits. Every pasted `grep -r` transcript in the bundle is in the stripped form, so it would fail a diff on a
normal shell for a reason unrelated to the code. Measured:

```
$ grep -rn "return msgin.ErrNoSubscriber" --include='*.go' . | head -1
channel/direct.go:87:		return msgin.ErrNoSubscriber
$ /usr/bin/grep -rn "return msgin.ErrNoSubscriber" --include='*.go' . | head -1
./channel/direct.go:87:		return msgin.ErrNoSubscriber
```

**Rule added to Global Constraint 9** (which already governs sweeps): append **`| sed 's,^\./,,'`** to any
`grep -r … .` whose output is pasted or diffed, alongside the existing `| sort` order pin. **Scoped
application, not a bundle-wide re-run:** the two copies of the 43-sentinel class sweep (Plan Task 9.7 and ADR
0029 §5.0b, which are required to stay byte-identical) were both pinned and re-run with `/usr/bin/grep` — the
pinned form reproduces the twelve pasted lines **byte-for-byte**, the un-pinned form differs on all twelve —
and the two blocks were `diff`ed against each other afterwards (identical). Other pasted sweeps are left for
their owning task to pin under the new rule.
