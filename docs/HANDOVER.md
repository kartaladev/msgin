# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then `docs/specs/014-core-package-layout.md` and
> `docs/plans/027-core-package-layout.md`, then `docs/adrs/0030-reply-channel-exclusivity-probe.md` (new).
> Trust those over this file and over any memory.
>
> **STATE (2026-07-28, fourth session): ROUND 6 has RUN — 4/4 lenses `NEEDS-REVISION`, 25 blockers / 34 minors —
> and ITS FIX PASS IS FULLY APPLIED AND RE-VERIFIED.** Round 6 produced three new decisions: **D-L**, **D-M**,
> and a **revision of D-K**. Record: [`docs/plans/027-audit-round-6.md`](plans/027-audit-round-6.md); read its
> **§6 (corrections to that record, found while applying it)** before trusting §1–§4 of the same file.
>
> **ROUND 7 IS THE NEXT AUDIT and has NOT run.** Every round so far has found defects in the previous round's
> fixes, and round 6's fix pass is the largest yet (+1686/−317 across 24 files).
>
> **No `.go` file has been touched since `3d0b87a`.** The code is green and unchanged; every edit in this
> window is documentation. That means the tree is a safepoint, and it also means **every count in the bundle is a
> claim about `dadc775`'s code**, which is why the fix pass pins them to that SHA.
>
> **Round 6's three decisions, all DECIDED but NOT YET IMPLEMENTED:**
>
> | | Decision | Effect |
> |---|---|---|
> | **D-L** | `SingleSubscriber()` is an **end-to-end policy predicate**, not a report about the local handle, and is **lifetime-invariant** | Supersedes ADR 0030 §1 in place and **reverses §Topology's conclusion** — the broker-backed topology becomes *detectable*. Fixes three compile-proven defeats (method promotion, a live-count probe, and `struct{ msgin.SubscribableChannel }` silently dropping the probe) |
> | **D-M** | A deterministic endpoint fault carries its own retry classification; **`ErrNilFunc` is `Permanent`** at all producers | A behavior change to **shipped** code — `ErrNilFunc` was burning the full retry budget and **tripping the circuit breaker**. New **Task 9.7**. `ErrNoRoute` stays **transient** (its `pick` is evaluated per message against caller state) |
> | **D-K (revised)** | The `expr` providers wrap **`msgin.ErrPayloadType`**; `expr.ErrExprResultType` is **not declared at all** | One shared `errors.Is` target for every future expression provider, and correct permanence for free |
>
> Spec §2.1's behavior-change table therefore holds **six** rows, not four — D-J is row 5, D-M row 6. That is
> what unblocks Task 9.6, which previously instructed an implementer to **stop and report** on hitting it.
>
> **PARTLY PUSHED — the previous handovers said otherwise, and they were wrong three ways.**
>
> ```
> $ git rev-parse --short main                     0de54e9
> $ git rev-parse --short @{u}                     6f44db6      # origin/claude/repo-structure-refactor-jt79t1
> $ git rev-parse --short HEAD                     <this file's own commit — see the note below>
> $ git rev-list --count main..HEAD                16
> $ git rev-list --count @{u}..HEAD                13           (behind: 0)
> ```
>
> **`6f44db6` is `origin/<branch>` — the REMOTE TRACKING HEAD — not `main`.** Every prior handover read it as
> `main` and concluded *"main is at 6f44db6; the branch is 6 commits ahead; nothing is pushed"*. All three
> halves were false: `main` is `0de54e9`, the branch is **16** ahead of it, and the branch **has been pushed**
> up to `6f44db6`, leaving **13 unpushed commits** — every one of the refactor commits among them.
>
> **Why this is operational, not cosmetic.** `main..HEAD` is the exact range CLAUDE.md's whole-branch
> `/code-review` and `/security-review` gate runs over. The stale figure would have scoped that gate to 6 of
> 16 commits — silently skipping the entire extraction (`c83dde9`), the segregation (`b6ce7bb`), and the
> round-3 fixes (`1d7fc80`). **Five audit lenses missed it** because all five were briefed on the bundle
> documents; nobody's brief covered the handover's own git facts.
>
> **Verify, never copy:** `git rev-parse --short main`, `git rev-parse --short @{u}`,
> `git rev-list --left-right --count @{u}...HEAD`.
>
> **Do not commit or push without explicit approval.** Nothing has been pushed in this session.

## 1. Objective & position

`msgin` is a Go 1.25 Enterprise Integration Patterns library. The active effort is the **pre-v1 core
refactor** (Plan 027): flatten-to-packages, channel interface segregation, EIP lexical alignment.

Tasks 0–8 are **implemented, green and committed**. Tasks 9, 9.5, 9.6, **9.7 (new — D-M)**, 10, 11, 12 remain.
**No implementation code for them has been written.**

Sizes were revised in round 6: **9.5 is `M`** (was `S`) and **10 is `L`** (was `M`) — re-sized rather than split,
because the task *number* is a cross-document link (`CLAUDE.md`, ADR 0029, Spec §8.1 all cite `9.5`/`9.5.1` by
number). Both tasks record the clean split that stays available. **Task 9.6 is still `S` and is arguably `M`** after
gaining two checkboxes — an open call, flagged rather than changed unilaterally.

## 2. Exact state

Branch `claude/repo-structure-refactor-jt79t1`: **17 commits ahead of `main` (`0de54e9`)**, and
**14 ahead of `origin/<branch>` (`6f44db6`)** — i.e. partly pushed, with all refactor work unpushed.
**Verify these, never copy them** (see the banner) — they were wrong three ways in three consecutive handovers.

The round-6 fix pass landed as **24 modified files + `docs/plans/027-audit-round-6.md` (new)**, `+1686/−317`,
**zero `.go` files changed**. Verified at that state: `gofmt -l .` empty · `go build ./...` clean · root
`go test ./...` **11/11 ok** · **all seven modules GREEN standalone** under
`GOWORK=off go test ./... -race -shuffle=on` (including the Docker-backed `dbtest` and `crontest`).

> ### This file CANNOT name its own commit's SHA — do not try to read one here
>
> This handover is committed **inside** the commit it describes, so any SHA written here for `HEAD` is
> invalidated the moment that commit is amended. It was, twice, and the SHA went stale both times within
> minutes. **`HEAD` is identified by subject, not hash:**
>
> ```
> $ git log --oneline -4
> <sha> docs(027): close D-I/D-J/D-K, add ADR 0030 and Task 9.6, apply rounds 4-5   <- this file's commit
> <sha> docs(handover): record the committed state, both review gates, and the two open decisions
> <sha> docs(027): apply the round-3 audit corrections; commit the derivation tools
> <sha> fix(core): restore the goleak net, cover the poll-backoff cap, reject a nil Subscription
>
> $ git status --short
> (clean)
> ```
>
> Ancestor SHAs (`3d0b87a`, `1d7fc80`, `b6ce7bb`, `c83dde9`, `ab233d9`, `0e2dcf0`, `dadc775`) are stable and
> **are** safe to cite — only `HEAD`'s own hash is self-referential. Every measurement in the bundle is pinned
> to **`dadc775`**, which is `HEAD~1` and unaffected by amends.

**Zero `.go` files modified**, so `3d0b87a`'s verification still holds: seven modules build + vet, `go test
./... -race -shuffle=on` green across 11 root packages, `-coverpkg=./...` **93.4%**.

## 3. Read in this order

1. `CLAUDE.md` — hard rules. **SDD is the default execution mode; direct main-session implementation needs
   explicit per-task approval, and NO implementation code may be written without asking first.**
2. `docs/specs/014-core-package-layout.md` · `docs/plans/027-core-package-layout.md`
3. `docs/adrs/0030-reply-channel-exclusivity-probe.md` (**new**, amends ADR 0028 §6.2), `0027`, `0028`, `0029`.
4. `docs/plans/027-derivation-findings.md` — F0–F13, the evidence base.
5. `docs/plans/027-audit-round-2.md` §E (verified-sound, do not re-open) and §G.1 (decisions D-A…D-H).

## 4. The three decisions closed this session

| | Decision | Chosen | Status |
|---|---|---|---|
| **D-I** | The two orphaned expr sentinels (`ErrInvalidExpression`, `ErrExprResultType`) | **LEAVE root**; the `expr` module mints its own with the `msgin/expr:` prefix, **not aliases** | Recorded (ADR 0029 §5.0a, Spec §3.2/§7). **Code not written** — Task 9.5 deletes, Task 10 declares |
| **D-J** | Reply-channel exclusivity | **PROBE and reject by default** — `msgin.ExclusiveSubscribable{SubscribableChannel; SingleSubscriber() bool}`, `msgin.ErrSharedReplyChannel`, opt-out `endpoint.WithSharedReplyChannel()`. A channel that does **not** implement the probe is **accepted**, keeping the SPI open | Recorded (**ADR 0030**, Spec §5.1, **Plan Task 9.6**). **Code not written** |
| **D-K** | `ErrExprResultType`'s retry classification | **Wrap in `msgin.Permanent`** — it is `ErrPayloadType`'s expression-domain twin and is deterministic | Recorded (**ADR 0029 §5.0b**, Plan Task 10). **Code not written** |

**D-I's plan-recommendation was wrong and the correction matters.** Plan §9.5.0 had recommended keeping the
sentinels in root, arguing §3.2's rule "cuts the other way only for packages a consumer imports instead of
root". Measured, the rule is symmetric: the three shipped adapters mint 51 sentinels of their own **and**
return root's at 27 file→sentinel pairs. The discriminator is *whose fault it is*.

## 5. ROUND-4 AUDIT — 3/3 NEEDS-REVISION (2026-07-28)

Three fresh Opus subagents, three lenses, run against the bundle **after** D-I/D-J were recorded. The design
lens implemented D-J in a worktree and compile-proved its findings.

**All three confirmed the generated artifacts and found every defect in hand-written prose** — the same split
round 3 found. The consistency lens named the root cause, and it is a *class*:

> **`1d7fc80`** (5 × `doc.go`, 6 × `go.mod`/`go.sum`, root `main_test.go`, the dead-helper deletion,
> `ErrNilSubscription`) **moved a dozen measurements. The round-3 pass swept only the two it had flagged
> (101→102, 42→43) and left the rest.**

The counter-rule adopted for the fix pass: **every generated block names the commit it was derived at, and a
range ending in `HEAD` is not a pin** — it re-evaluates silently on every commit, which is how §3.6's blast
radius went stale three times.

### 5.1 Fixes APPLIED this session

| Block | Was | Now | Finding |
|---|---|---|---|
| Spec §3.1 package file inventory | 11/5/1/4/3, no `doc.go` | **12/6/2/5/4**, all five `doc.go` counted; contradicted §3.5 | B1 |
| Spec §3.2 destination lines | 7 wrong | `channel.go:43`/`:54`, `exchange.go:225/266/279/301/352/382` (11 refs) | B7 |
| Spec §3.4a test-file frames | 45 "today" | **50**, + a `TestMain` block and the `endpoint` caveat | B8 |
| Spec §3.4e coverage | root **81.8%** | root **95.3%**; the "fails the 85% gate" rationale replaced with the attribution one | B2 |
| Spec §3.6 blast radius | 31 files, +239/−191, `..HEAD` | **43 files, +244/−220**, pinned `c83dde9~1..dadc775`, per-module table rebuilt | B6 |
| Spec §3.6 `go.mod` proof | `git status --short` (**cannot fail**) | committed-range diff; claim re-scoped to "for the requalification" | B5 |
| Spec §6 rename census | 30/12/35/14 | **31/13/36/15** (the 13th file is `endpoint/doc.go`) | B9 |
| ADR 0027 | 101, bare `decls`, stale status, stale blast radius | 102, `go run …/decls.go`, committed status, 43/+244/−220 | B11 + B6 echo |
| Plan Task 2 / 3 / 11a headers | "**DONE (uncommitted)**" | `b6ce7bb` / `b6ce7bb` / `1d7fc80` | B11 |

### 5.2 Fixes — ALL APPLIED (2026-07-28, same session)

**All four groups below are DONE.** They are kept in full because the *reasoning* is what a re-audit needs,
not the tick. Each was worked as a class, not as a list of instances — that is what failed in rounds 1–3.

| Group | What | Status |
|---|---|---|
| **A** | Arm-2 staleness sweep — redesigned from a hardcoded name list into an **invariant** | ✅ verified running; yields exactly `WithRelease` |
| **B** | The Task 9.6 / ADR 0030 defects introduced earlier this session | ✅ all applied |
| **C** | Three design gaps: ADR 0030 §Topology's second topology, Task 9.6's `channel` coverage arm, **D-K** | ✅ all applied |
| **D** | Number/godoc/traceability sweeps, incl. Spec §8 obligations 10–13 → Plan Task 11b | ✅ all applied |

**Plus one class the round-5 auditor flagged before dying, found and fixed by self-check:** the bundle was
citing **three different pins** for the same measurements (`3d0b87a`, `dadc775`, and bare "at HEAD"). No
number was wrong — code is byte-identical across all three — but a bare "at HEAD" is unfalsifiable and a
split pin is a cross-document contradiction in form. **Normalized: every measurement claim in the bundle now
names `dadc775`.** Verify with `grep -rn "at HEAD" docs/specs/014* docs/plans/027-core* docs/adrs/002[789]* docs/adrs/0030*`
→ must be empty.

#### A · The arm-2 staleness sweep is VACUOUS — redesign it (consistency B3/B4, executability B1)

Verified: the published pattern `\b(FilterExpr|RouterExpr|TransformExpr|SplitExpr|WithCorrelationExpr|WithReleaseExpr)\b`
returns **0 hits workspace-wide** — at `dadc775`, at `3d0b87a`, and at `0e2dcf0`. So:

- Plan §9.5.1's and Spec §8.1's "**ARM 2: 7 survivors**" lists are **entirely stale**. Every cited line now
  holds unrelated text (`errors.go:156` is a `WithDefaultChannel` comment; `routing/splitter.go:52` says
  "Split constructor"; `routing/aggregator_test.go:1276` says `WithReleaseStrategy`).
- **Plan §9.5.0's new D-I bullet (written by me this session) asserts "3 of arm 2's 7 survivors live in
  `errors.go:175,176,177`". That is false** — those lines are the sentinel godoc and contain no matched
  token. An implementer deleting the blocks and re-running the sweep sees no delta and concludes the gate is
  broken. **Delete that clause.**
- The **one genuine survivor** the published pattern cannot see is `routing/aggregator.go:316`'s
  `// (the WithRelease strategy failed)` — `WithRelease` never existed; it is `WithReleaseStrategy`.
- **Fix:** either broaden arm 2 to the real deleted-name class and re-derive the list by *running* it, or
  declare arm 2 empty and reduce the checkbox to arm 1's two genuine survivors (`codec.go:33`,
  `routing/aggregator_test.go:21` — both reproduce exactly). Do not ship a list and a command that disagree.
  Note `boxMessage`/`nilFuncStep` (11 hits each) are **legitimate package-local copies**, not stale refs, and
  `endpoint/consumer.go:945`'s `delayFor` is a deliberate historical note.

#### B · My own Task 9.6 / ADR 0030 defects (consistency B10, M10–M13; executability B3, M12; design MINOR 4/5)

- **Plan Task 9.6's blast-radius block is presented as `grep` output and is not.** It opens with a `$` prompt
  and shows 11 annotated lines; the real command emits **25**. Fourteen `endpoint/exchange_test.go` sites are
  dropped (all verified `channel.NewDirectChannel()`, so the conclusion holds). **Paste the real output, or
  relabel it a derived summary with the command that produced it.**
- **"it is exactly one site" is wrong.** It is one *test* with **two** constructions: `exA` at
  `exchange_test.go:446` and `exB` at `:453`, both over the same plain pub-sub reply. Spec §5.1 and ADR 0030
  say "its **first** exchange" — both understate it. Plan Task 9.6 says "exA **and** exB" correctly.
- **Task 9.6's "add the opt-out **in the default-fan-out case**" is not expressible.** The table's per-case
  field is `opts []channel.PubSubOption` (channel options, applied at `:444`); both constructions sit in the
  shared `t.Run` body. **Restate as:** add `endpoint.WithSharedReplyChannel()` **unconditionally** to both;
  case 2 is unaffected because its channel passes the probe and is rejected later by `Subscribe` with
  `ErrChannelSubscribed`.
- **Task 9.6's justification for the test fake is false.** It says "no in-tree type can produce" the
  probe-absent arm; `nilSubChannel` (`exchange_test.go:120`) **is** exactly that. The conclusion survives —
  it returns `(nil, nil)` and is rejected by the `ErrNilSubscription` guard — so restate as "no in-tree type
  can produce an **accepted** probe-absent arm".
- **ADR 0030 §2's `PubSub` sentence is unreachable AND destructive.** `*channel.PubSub` has no `Send`, so it
  can never be a reply channel (`vet` confirms). Worse, following the advice makes every topic
  single-subscriber registry-wide. **Delete the sentence.**
- **ADR 0030's "`DirectChannel` was the only type satisfying `MessageChannel`" drops Spec §1's caveat** —
  `aggregator_test.go:222`'s `failNthChannel` also did. Add "outside a test fake".
- **Task 9.6's "`apidiff` reports exactly two additions" contradicts §9.5.0's table** (8 total). Say "two
  **beyond** the 6 measured at `3d0b87a`, for 8 total".
- **Plan line ~402's `**97 / 8** once D-I and D-J land.` lost its `> ` blockquote prefix** and renders outside
  the quote.
- Spec §3.2/§7 cite `errors.go:168`/`:193` for the sentinels; `decls.go` reports **180**/**206** (168/193 are
  godoc-block starts). Task 10 deliberately uses the range form — **state that convention**, since every
  other citation in the bundle is a declaration line.

#### C · Three design gaps — real specification work (design B1, B2, B3)

- **D1 · ADR 0030 §Topology enumerates ONE topology and claims completeness.** It reasons only about N
  instances each holding their **own in-memory** pub-sub channel. A **local handle onto shared external
  state** — a broker-backed reply channel (NATS subject, Redis pub/sub, SSE stream) — can implement the probe
  *honestly* (one local subscriber), report `true`, and still fan every reply to all N instances, so N−1
  non-owners each write a full copy of another instance's reply to their `WithUnmatchedReplySink`. That is
  ADR 0030's own Context, cross-process, now *endorsed* by a passing probe. `ExclusiveSubscribable` is **root
  SPI** — third-party adapters are exactly the population that will implement it. **The Return Address seam
  is unaffected** (verified: `adapter/http/exchange.go:58` implements `msgin.RequestReplyExchange` directly
  and never touches `NewChannelExchange`). Fixes: add the second topology to ADR 0030 §Topology **and**
  Spec §10's D-J bullet; make `NewChannelExchange`'s godoc state **four** arms, not three (the fourth being
  "accepted-exclusive-but-only-within-this-process"); require `SingleSubscriber()` to be **safe for concurrent
  use** in its godoc.
- **D2 · Task 9.6 adds two exported methods to a 100%-covered package, enumerates zero tests for them, and
  prescribes the one measurement that cannot see it.** Compile-proven by the auditor: with D-J implemented,
  `channel` falls 100.0% → **98.3%** (`direct.go` and `pubsub.go` `SingleSubscriber` both 0.0%), while
  `-coverpkg=./...` reports both at **100%** because the exercising tests live in `endpoint`. Nothing in
  `channel` pins `PublishSubscribeChannel.SingleSubscriber() == cfg.single`, which is the entire load-bearing
  link between D-F and D-J. **Fix:** add a `channel`-package table test for both types (option present and
  absent), and require **both** coverage arms in Verify — per-package (`channel` stays 100.0%) **and**
  `-coverpkg`. Per-package is the only arm that can see this class.
- **D3 · Record D-K.** `ErrExprResultType` is an **evaluation-time** error on the retry hot path.
  Verified: `IsPermanent` (`reliability.go:38`) covers only `ErrPayloadType`/`ErrPayloadDecode`/
  `ErrPayloadTooLarge`, and the deleted originals never wrapped it — so a deterministic result-type mismatch
  is classified **transient** and retried `MaxAttempts` times (`N × MaxAttempts` across N instances per
  Spec §10). D-I closes the only door that could fix this in root. **Decision D-K: the expr providers wrap it
  in `msgin.Permanent`.** Record in Plan Task 10's hot-path list and ADR 0029 §5.0a, and **split §5.0a's
  treatment of the two sentinels** — its "§5 protects *where* the error is raised, not *which* package
  declares it" argument holds for `ErrInvalidExpression` (construction-time) and does **not** transfer to
  `ErrExprResultType`.

#### D · Sweeps and small corrections

- **Number sweep** (all one class, all `1d7fc80` drift): Spec §422's census block prints `42` → **43**;
  Plan line ~544 "`apidiff` still reports 95/**5**" → **95/6**; Plan lines ~871/~946 "reconciled against
  §4.1's **four** classes" → **five** classes / 97; this file's own §7 below.
- **Godoc obligations with no owning bullet** (design MINOR 7 — add to Spec §8's list, owned by Task 11):
  `SubscribableChannel`'s godoc must cross-reference `ExclusiveSubscribable` (otherwise the optional
  capability is undiscoverable from its supertype, and the accept-unknown arm becomes permanent for exactly
  the channels most likely to fan out); **`WithSharedReplyChannel()` has no godoc bullet at all**, though
  CLAUDE.md's sensible-defaults rule names option godoc specifically.
- **`ErrSharedReplyChannel`'s message contradicts its decided meaning** (design MINOR 1). The commonest
  trigger is a **sole** exchange over a plain pub-sub channel with no other subscriber — where the channel
  *is* exclusive in fact. `"reply channel is not exclusive to this exchange"` sends the reader hunting for a
  second subscriber that does not exist. Use `"msgin: reply channel permits multiple subscribers; it is not
  exclusive to this exchange"`.
- **`NewChannelExchange`'s godoc must enumerate `ErrChannelSubscribed`** (design MINOR 2) — it is returned
  unwrapped from `reply.Subscribe` at `exchange.go:250` and is not in the doc's error list. Also state that
  an unknown third-party fan-out channel is **accepted by design**.
- **`WithSharedReplyChannel()` does not confer shareability** (design MINOR 3) — on a `DirectChannel` the
  second exchange still gets `ErrChannelSubscribed`. Its godoc must say it *suppresses the probe*. (A wrapper
  type embedding `*PublishSubscribeChannel` can shadow the method to report `true` — worth one sentence on
  `ExclusiveSubscribable`.)
- **Traceability:** ADR 0029's header decision list must add **D-I** (Spec §47 and Plan §107 already say it
  carries it); ADR 0029 §5.0a needs the **NOT-YET-IMPLEMENTED** marker ADR 0030's status line uses (it says
  "They are deleted from root" in the present tense; `errors.go:180,206` still declare both);
  `docs/rfcs/0002-eip-alignment.md` needs a **backlink to ADR 0030** (ADR 0030 cites it; CLAUDE.md requires
  both directions); ADR 0028:165 cites `OutboundAdapter` at `spi.go:42` → **`spi.go:56`**.
- **CLAUDE.md's D-I paragraph is in the wrong tense** — it says the sentinels "leave root" and "Root keeps a
  producer for every sentinel it declares", both false until Task 9.5 runs. CLAUDE.md is read first by every
  session. Mark it "decided; lands in Plan 027 Task 9.5".
- **Plan task hygiene** (executability M4/M5/M6/M10/M11/M13): Task 9.5's Verify demands the **eight**-module
  loop but `expr` does not exist until Task 10 → Tasks 9/9.5/9.6 verify **seven**. Task 9's branch list names
  `Or`-with-nil-when-left-is-true but omits the mirror **`And`-with-nil-when-left-is-false**. Task 9's
  `apidiff` step has no baseline (`routing`/`transform` were never snapshotted; root's delta is genuinely
  zero). Task 9.5's HTTP capability cases need the three target builders (`newQueueTarget` etc., unexported
  in root's `msgin_test`) **reimplemented** in two other packages — ~6 subtests of unbudgeted scaffolding.
  Task 10 has no checkbox for `go.work`'s `use ./expr` or for `expr-lang/expr` as its `require`. Sizing:
  consider re-marking 9.5 as **M** and splitting Task 10 into 10a (module + CI + providers + `errors.go`) /
  10b (12-function parity).

### 5.3 ROUND 5 — NEEDS-REVISION (3 blockers, 8 minors) — ALL APPLIED

A first attempt died on an API session limit; the retry at 20:31 WIB completed. It verified the round-4 fix
pass claim-group by claim-group, **planted probe comments to test the new arm-2 invariant**, and independently
confirmed the tree green. Verdict **NEEDS-REVISION**. Everything below is now fixed.

| # | Finding | Fix |
|---|---|---|
| **B1** | Spec §3.4a: the round-4 pass **relabelled** a test-file count from "today" to the pin `c83dde9` **without re-running it**. True value at `c83dde9` is **44** (`capability_test.go` arrived at `b6ce7bb`), making the row false *and* identical to the frame above it | Frame corrected to `b6ce7bb`; Global Constraint 0 gained **"a relabel is not a re-measurement"** |
| **B2** | The extraction's behavior-preservation *proof* — *"211 `Test*`/`Example*` before, 211 after, identical name sets"* — **reproduces in no frame**. Measured: `ab233d9` 224 · `c83dde9` 211 · `dadc775` 221 unique. Sets differ by 17 out / 14 in | Name-set argument **withdrawn, not repaired**; the normalised per-file diff is now the sole claim. Corrected in Spec §2, Spec AC-5, Plan Task 4–8 |
| **B3** | §3.6's blast radius was swept in ADR 0027's *Context* and **left stale in its Consequences and in Plan §20** — the diagnosed class recurring inside the pass that diagnosed it | Both re-pinned to `c83dde9~1..dadc775` → 43 files, +244/−220 |
| M4 | **Global Constraint 0 published `c83dde9~1..HEAD` as its own example** — the rule mandated the anti-pattern Spec §3.6/B6 blocks | Rewritten: both ends must be SHAs; `HEAD` is a cursor, not a name |
| M5 | Arm 2's stated scope was overclaimed. Probe comments proved it is **shape-blind** (only `With\|Err\|New`, so 4 of the 6 `*Expr` names and the `StreamingSource` rename are invisible) and **block-comment-blind** | Blind spots now stated **in the command block**; the "asserts the property" wording narrowed |
| M6 | `WithInstanceID`'s allow-list justification was false — `adapter/cron` **is** scanned, and the mention is a deliberate reference to a deleted symbol | Moved to the "deliberate negative" row with `ErrNoPayloadCodec` |
| M7 | §8: *"four unmet, one half-met"* vs a table showing **3 UNMET + 1 HALF** | Corrected to 3 + 1 + the four D-J obligations = 8 Task 11 checkboxes |
| M8 | Spec §3.2 sentinel block: `dadc775` header, `3d0b87a` figure beside it | Unified |
| M9 | The workspace error census was **hand-patched, not re-run** — published descending by count when `sort \| uniq -c` emits path order | Regenerated verbatim, with a note on the sort order |
| M10 | My B9 insertion left a space-indented line after a blockquote; CommonMark **lazy continuation** absorbed it and the following sentence, orphaning a bullet list | Blank line restored, sentence returned to body text |
| M11 | **`CLAUDE.md` said root `go test ./...` covers 6 packages; it is 11** — restructure-induced staleness in the file every session reads first | Corrected, with the `go list` command inline |

**Verified clean by round 5:** claim group **(c)** (round-4 defects in new material) and **(d)** (the three
design fixes) — including that the `channel` 100.0% → 98.3% coverage claim is *arithmetically exact*
(118 statements → 118/120 = 98.33%). All 80 §3.2 declaration rows re-verified mechanically. §4's group
decomposition is an exact partition. All cross-links resolve both ways.

**Re-verified after these fixes:** `go list ./...` = 11 · test files at `b6ce7bb` = 45 · blast radius
43/+244/−220 · 102 exported / 43 sentinels · arm 2 yields exactly `WithRelease` · seven modules green ·
`go test ./... -race -shuffle=on` green.

## 6. Next actions

1. **The design bundle is committed as `aae6160`; round 6 HAS run** (record:
   [`docs/plans/027-audit-round-6.md`](plans/027-audit-round-6.md) — four Opus lenses, all four
   `NEEDS-REVISION`, 25 blockers / 34 minors, producing decisions **D-L**, **D-M** and a **revision of D-K**).
   Its fix pass is partitioned into four groups in §5 of that record. **Round 7 is the next audit** — brief it
   on the §0 counter-rules, on the round-6 record itself, and on **the handover's own git facts**, which is the
   gap that let a false `main` SHA survive five lenses.
2. **Then implementation**, in plan order: Task 9 → 9.5 → 9.6 → 10 → 11 → 12. **Task 11 must run AFTER
   Task 9.6** (Spec §8 obligations 10–13 document symbols 9.6 creates). **Ask before writing any
   implementation code, and default to SDD** (fresh implementer subagent per task, coordinator verifies green
   and commits, adversarial reviewer before delivery). Plan approval does **not** authorize the execution
   mode.
3. **Before merge:** `/code-review` and `/security-review` over `main..HEAD` (both have run per-increment;
   the whole-branch pass has not). Consider `/code-review ultra` — it is user-triggered and billed, and an
   assistant cannot launch it.

## 7. Gotchas & environment

- **`export GOTOOLCHAIN=go1.25.12`** always; **`export PATH="$(go env GOPATH)/bin:$PATH"`** — `apidiff`,
  `gopls`, `govulncheck`, `gofumpt`, `gorelease` live there and none are on `PATH`.
- **`./...` is not the repo** — seven modules (`go.work` `use`s all seven). CI runs each standalone with
  `GOWORK=off`, but **only six of them**: `adapter/cron/crontest` is missing from both CI jobs (see below), so
  CLAUDE.md's seven-directory loop is a **superset** of CI, not a copy of it.
- **`go build ./...` does not compile tests**; `go vet` does but stops after one type-error batch — use
  `go test -c` for a full transcript.
- **Measured at `dadc775`** (all re-run this session): root **14** source files · **102** exported non-method
  symbols · **43** sentinels · `apidiff` **95 removals / 6 additions** · `-coverpkg=./...` **93.4%** ·
  default-profile root **95.3%**. Projected after D-I + D-J: **102 / 42 / 97 / 8**. Task 12 **measures**.
- `.golangci.yml` sets `linters.default: none` — **`ST1000` and `unused` are both off**, so missing package
  docs and dead code after a move are reported by nothing.
- `gopls` has **no Move refactoring**.
- `.github/workflows/ci.yml` omits `adapter/cron/crontest` from both jobs — pre-existing; Task 10 fixes it.
- Repo has **zero git tags** — do NOT propose tagging.
- Never commit `.claude/settings.json`; stage explicit pathspecs.
- **The `../msgin-derive` worktree is GONE — do not try to remove it.** (Bullet corrected 2026-07-28, audit
  round 6 finding M-8; it previously said the worktree was "merged and redundant; safe to
  `git worktree remove`". `ls ../msgin-derive` → *No such file or directory*, and `git worktree list` shows
  only `/Users/zakyalvan/Documents/RND/msgin`. Running the old instruction errors.)

## 8. Triaged, not fixed

- **Commit trailers on `6f44db6` and `28dd9e4` — TRIAGED, will not be fixed** (audit round 6 finding M-9).
  Over the 16 commits in `main..HEAD`, every `refactor`/`fix` commit carries the required
  `Spec:`/`Plan:`/`ADR:` trailers, but two carry none at all: `6f44db6` (`docs(rfcs): fold audit findings into
  RFCs with elaborated caveats`) and `28dd9e4` (`docs(claude): refresh project status from greenfield to
  pre-v1`). **Rationale for accepting them:** both are `docs:` commits authored *before* the traceability-trailer
  convention was extended from code commits to non-code artifacts — `6f44db6` edits only draft RFC markdown, at
  a point when `docs/rfcs/` was itself a brand-new artifact type with no promoted spec/plan/ADR to cite, and
  `28dd9e4` edits only CLAUDE.md, which is not governed by any numbered artifact. CLAUDE.md's own trailer rule
  is scoped to *"every `feat`/`fix`/`refactor` commit"*; neither of these is one. The only way to add trailers
  now is an interactive rebase rewriting all 16 commits — and `6f44db6` **is** `origin/claude/repo-structure-refactor-jt79t1`'s
  head (`git rev-parse @{u}` → `6f44db6`; 13 unpushed, 0 behind), so the rewrite would additionally require a
  **force-push over an already-published commit**. Disproportionate risk to correct two docs commits, and it would invalidate
  every SHA cited across Spec 014, Plan 027, ADRs 0027–0030 and this handover. **Do not rebase.** Every future
  commit on this branch carries its trailers.
