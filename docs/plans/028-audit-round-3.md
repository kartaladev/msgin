# Plan 028 — adversarial design audit, round 3 (2026-08-14)

Independent Opus subagent, fresh (ran neither prior round), handed the revision-3 bundle plus both prior audit
records, **before any implementation code existed**. Deliberately **narrow**: round 2 scoped it, and the census of 32,
the R1/R2/R3 partition and decision D-S were explicitly excluded from re-attack. Evidence-primary.

**Verdict: DO NOT IMPLEMENT — round 4 required.** 1 BLOCKER, 4 MAJORs, 7 MINORs. No redesign: every finding is in the
verification layer or a derived table. Two required a **user decision** and are now settled as **D-U** and **D-V**.

---

## BLOCKER-D — AC-8 gated on a baseline that is red on arrival

Spec AC-8, Plan Task 1 and Plan Task 8 all required `apidiff` against `docs/plans/027-root-api-baseline.txt` to report
**0 removals / 0 additions**. That file is the **pre-Plan-027** surface.

```
$ apidiff docs/plans/027-root-api-baseline.txt . | grep -c ': removed'
97
$ apidiff docs/plans/027-root-api-baseline.txt . | grep -c ': added'
9
```

**`docs/HANDOVER.md` recorded that exact number** — *"`apidiff` vs `docs/plans/027-root-api-baseline.txt` | **97
removals / 9 additions**"* — in the very file the session read before starting. The AC was unsatisfiable before the
increment touched a line, in **Task 1, the reference task**. Round-1's m-10 corrected this AC's *scope* caveat;
nobody ran the number.

**Folded as:** new **Plan Task 0** — capture `docs/plans/028-root-api-baseline.txt` from clean `main`, prove it diffs
0/0 against its own source, and **commit it** (no gate may depend on `/tmp`). AC-8, Task 1 and Task 8 restated against
it. `027-root-api-baseline.txt` is left untouched as Plan 027's historical record.

---

## MAJOR findings

### M-L — the §3.5 precedence table misclassified `resilience.NewTokenBucket`; the split is 9/8, not 8/9

```
resilience/ratelimit.go
37  func NewTokenBucket(rps float64, burst int, opts ...TokenBucketOption) (msgin.RateLimiter, error) {
38      if rps <= 0 || burst < 1 {
39          return nil, msgin.ErrInvalidRateLimit
...
47      for _, opt := range opts {
```

An argument check precedes the loop, so it is **VALIDATE-FIRST** and `NewTokenBucket(0, 0, nil)` yields
`ErrInvalidRateLimit`. The table's `yields` column stated the wrong expected value, and Task 2's mandated precedence
case — written from that column — would have gone red.

**Root cause, and the reason this is the session's second false table:** the coordinator's derivation script matched
only `== nil` / `== ""` patterns and cannot see a **numeric** guard. The census grep had already failed the same way
on **qualified** types (round-1 BLOCKER-1). *Derive by reading or by AST; never by pattern-matching Go source.*

All 17 rows were re-derived independently; **every other row was correct**.

**Also folded:** the two-way label is lossy — several constructors validate both before *and after* the loop. The
governing rule is the prose one, now stated: **faults validated after the loop lose to the nil option.**

### M-M — the laundering was ordering-dependent, and one ordering still leaked (PROVEN, isolated-module probe)

Round-2 BLOCKER-A killed the *forced-`false`* laundering. The **ordering-dependent** one survived:

```
nil-FIRST  => err=msgin: reply channel permits multiple subscribers; it is not exclusive to this exchange
              | Is(ErrNilFunc)=false | Is(ErrSharedReplyChannel)=true
nil-SECOND => err=msgin: permanent: msgin: nil endpoint function
              | Is(ErrNilFunc)=true  | Is(ErrSharedReplyChannel)=false
```

Under the mandated **`break`**, `NewPublishSubscribeChannel(nil, WithSingleSubscriber())` left `single == false`, so
`endpoint/exchange.go:318-326` rejected the channel **before** `reply.Subscribe` (line 337) could surface the latched
fault. The nil option was never mentioned. Reached by exactly the conditional-helper shape Spec §1 uses as its
motivating example.

**Third recurrence of the round-1 M-5 / round-2 M-D class** — a property asserted more broadly than it holds.

**Resolved by the user as decision D-U (ADR 0031): `continue`, not `break`.** Latch the first nil, apply every
remaining option. `single == true` in both orderings, the probe passes, `Subscribe` reports the real fault, and the
laundering is **structurally** gone rather than documented around. It also simplifies §3.2 to *"every non-nil option
applies"*. `Chain`'s *"the chain stops at it"* does not transfer: a `Step` is sequential behavior, an option is
configuration. **AC-5c now asserts BOTH orderings**, the nil-first case being the executable proof of D-U.

### M-N — R2 created a method-entry precedence question the bundle never decided

R2 reports at method entry, where three of the five products already validate:

| Surface | Existing first check |
|---|---|
| `PublishSubscribeChannel.Subscribe` | `h == nil → msgin.ErrNilHandler` (`pubsub.go:151`) |
| `PubSub.Subscribe` | `h == nil → msgin.ErrNilHandler` (`pubsub_registry.go:50`) |
| `Router.Handle` | `pick == nil → Permanent(ErrNilFunc)` naming *nil pick* (`router.go:83`) |

The family was already internally inconsistent: `routing.Filter` checks `pred` **before** its loop (`filter.go:31`),
so `Filter(nil, nil)` reports *nil pred*, while `NewRouter(nil, nil)` under a top-of-`Handle` latch would report *nil
option* — opposite answers for the same fault pair in the same package. Round-2's M-F, one abstraction level over.

**Resolved by the user as decision D-V (ADR 0031): latch first, uniformly, and realign `Filter`** by moving its option
loop above the `pred` check. New **AC-5d** asserts it per R2 type; the `Filter` case fails against today's code, which
is what forces the realignment.

### M-O — the mandated `sqlite.DSN` godoc sentence was factually inverted

Revision 3 mandated, verbatim: *"the resulting DSN silently carries **SQLite's defaults**"*.

```
adapter/database/sql/sqlite/dsn.go
 9  const (
10      defaultJournalMode = "WAL"
11      defaultBusyTimeout = 5 * time.Second
12  )
48      cfg := dsnConfig{journalMode: defaultJournalMode, busyTimeout: defaultBusyTimeout}
```

A dropped option leaves **msgin's** opinionated defaults (WAL, 5 s), not SQLite's (`delete`, `0`) — the opposite of
what the sentence told the caller, most sharply for one passing `WithJournalMode("")` to *omit* the pragma and
silently getting WAL. It also omitted **`WithSharedMemory`** (`dsn.go:38`), the third `DSNOption` and the most
consequential drop: the DSN becomes a **file** database instead of `file::memory:?cache=shared`.

R3 exists only because round-1 BLOCKER-2 required the godoc to name the *concrete* consequence. **Naming a false one
is worse than boilerplate.** Rewritten in Spec §3.3 to name all three, correctly.

---

## MINOR findings

| # | Finding | Folded as |
|---|---|---|
| **m-P** | AC-7 hazard 5 said **4** `FuncLit`s; there are **5** (the fifth is `adapter/http/exchange_test.go:590`). Hazard 6 said "13, **9 of them `FuncDecl`s**"; the 13 are **12 `FuncDecl` + 1 `FuncLit`** | Both counts corrected. The stated *consequence* — "skip `FuncLit` does not cover them" — is unchanged and now stronger |
| **m-Q** | Task 7's vacuity probe is **author-written**: the same agent writes the checker and the planted constructors, so shape bias is uncontrolled | Added: run the finished checker against `main` in a throwaway worktree and require it to flag **all 32** real constructors there, **0** on `HEAD`. One command; catches a checker that only recognises its own planted shapes |
| **m-R** | §3.2 said "the scan stops at the nil" for all five R2, but `routing.Filter` **`return`s** and discards the prefix — a **fifth** shape | Stated. Task 7's checker must accept **any `return` inside the loop** as guarded, not only the R1 form |
| **m-S** | The plan's precedence coverage row demanded "a nil option with an **earlier**-validated fault" — unexecutable for the 8 loop-first constructors, which by definition have none | Restated as "assert §3.5's `yields` column, one constructor per distinct order"; loop-first cases pair with a *later*-validated fault |
| **m-T** | AC-5b's mutant **hangs** for `memory.Broker` — without the latch, `Send` blocks on the unbuffered chan and `Stream` on `ctx.Done()`, so the mutant dies at the 10-minute test timeout instead of failing | Task 4 now specifies a 50 ms `context.WithTimeout` for the two `memory` cases |
| **m-U** | §3.5's delegator list gave **7** line refs for **8** delegators — `stdlib/inbound.go:97` missing | Added; all 8 re-verified as calling their delegate in the first statement |
| **m-V** | The whole bundle was **untracked** after three audit rounds — a tree wipe would lose it | User approved committing **after round 4**: Spec 015 standalone as `spec:`, ADR + plan + audit records as `docs:`. Nothing pushed without separate approval |

## Round-2 fixes verified correct (no re-litigation)

1. **AC-2's `NewGateway` exemption is complete** — exported-constructor counts taken for all 22 option types;
   `GatewayOption` is the **only** zero. No third instance of the M-4 → BLOCKER-B pattern.
2. **AC-5's wrap mutant kills in both directions** — `IsPermanent` (`reliability.go:88-96`) matches only
   `*permanentError`, `ErrPayloadType`, `ErrPayloadDecode`, `ErrPayloadTooLarge`; `ErrNilFunc` is **not** among them.
3. **AC-6's index and first-nil mutants kill their paired assertions**, and the D-R delegator mutant is executable
   given the task order.
4. **AC-7's node-kind tally is exact** — 35 `Ident` / 11 `SelectorExpr` / 2 `IndexExpr` / 1 `IndexListExpr` = 49,
   re-derived by independent AST walk.
5. **Census and reconciliation hold** — 32 constructors, 24 loops, 22 option func types, 8 delegators all
   first-statement, **0** existing nil-option guards; `36 non-test rows = 32 + 4 harness literals`, no residual.
6. **D-T's numbers are right** — 10 call sites, 4 files, exactly 2 inline-composed, 1 a published Example, all in the
   root module, so Task 2 stays a green unit.
7. **R2 method inventories are complete**; no error-returning method is missed by AC-5b.
8. **Round-1's carried suspicion is CLOSED favourably** — a sweep of `adapter/http/options.go:407-1106` found every
   option body writes only into its `*Config`, and the only non-trivial one (`WithConnectHeaders`) **clones** the
   caller's header before mutating. §3.1's "no partial configuration is observable" holds for the 8 HTTP/stdlib R1
   constructors; **Task 6's sweep can be downgraded to a confirmation.**
9. **Docs-link gate arm 1 clean** on all bundle files.

## Round-4 scope (narrow)

The D-U/D-V resolutions and their new ACs (5c both orderings, 5d); Task 0's baseline; the corrected §3.5 table; the
`sqlite.DSN` godoc; the seven MINORs. **The census, the R1/R2/R3 partition, D-S and D-T are settled and must not be
re-attacked** — they have now held under three independent rounds.

## Coordinator's independent re-verification

BLOCKER-D (`head -1 027-root-api-baseline.txt`, `grep -n apidiff docs/HANDOVER.md` → line 58's 97/9), M-L
(`sed -n '37,48p' resilience/ratelimit.go`), M-O (`sed -n '9,12p' …/sqlite/dsn.go` plus the three `DSNOption`
constructors), M-N (`pubsub.go:151`, `pubsub_registry.go:50`, `router.go:83`, `filter.go:31`), m-U
(`stdlib/inbound.go:96-98`) and m-V (`git status --short` → 5 × `??`) were all re-run before any edit. **All
confirmed.**
