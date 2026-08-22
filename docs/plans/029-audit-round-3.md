# Plan 029 — adversarial design audit, round 3 (2026-08-21)

Independent Opus subagent, handed the complete **revision-3** bundle (Spec 016 rev 3 + ADR 0032 rev 3 + Plan 029
rev 3) **before any implementation code exists**, per CLAUDE.md's design-time gate. Round 1
([`029-audit-round-1.md`](029-audit-round-1.md)) returned NOT SAFE with 2 BLOCKERs / 7 MAJORs / 10 MINORs; round 2
([`029-audit-round-2.md`](029-audit-round-2.md)) returned NOT SAFE with 3 BLOCKERs / 6 MAJORs / 8 MINORs and
**10 of round 1's 19 fixes not landed cleanly**. This record attacks the revision that folded round 2.

This record is **evidence-primary** — an audit artifact, not a user-facing summary. Every structural claim below was
re-derived first-hand on the tree at `48bbe83` with `GOTOOLCHAIN=go1.25.13`, darwin/arm64, from a throwaway module
outside the repository (`replace github.com/kartaladev/msgin => …`), deleted afterwards. No repository file was
modified except this report. **Neither the bundle's numbers nor the prior audit records' numbers were trusted** —
and one of round 2's certified-correct citations and one of revision 3's "corrections" did not survive.

**Verdict: NOT SAFE TO IMPLEMENT.** 1 BLOCKER, 4 MAJORs, 7 MINORs. **5 of round 2's 17 findings did not land
cleanly** (5 LANDED-BUT-FLAWED, 0 REGRESSED, 0 NOT LANDED) — a marked improvement on rounds 1→2 (10 of 19).

---

## What revision 3 got right

This is the best revision of the three, and the improvement is real rather than cosmetic. **Every one of round 2's
three BLOCKERs was addressed with a measurement rather than a rewording, and the measurements reproduce.** The AST
scan yields **17 / 27 / 22 / 44** exactly as claimed, under the shipped gate's own walk rules; §2.1's 17 keys are
*set-identical* to what the scan finds; the "16 options vs 17 conformance keys" distinction is now stated once and
used consistently in all three documents with no residual "16". `WithConcurrency`'s `int32(n) < 0` band table
reproduces **verbatim, all ten rows**, and the end-to-end `Run` panic reproduces on the first try. The single error
shape `"%w: %s: %d not in [%d, %d]"` renders truthfully at both ends for all seven knobs, keeps `errors.Is`, and
keeps `IsPermanent` false for R1 / true for R2 through the `fmt.Errorf` layer — verified, not reasoned. All six R1
sites were read and all four code shapes are correctly assigned, with verbatim-accurate snippets. AC-4's baselines
(288 B / 120 B / 48.0 MiB, noise floor exactly 0 B) reproduce 3/3, and — for the first time in three revisions —
`credit.go:21` has a mutant whose cost provably escapes the stated assertion bounds. The `Recv == nil` boundary is
now a stated decision with its 27-method exclusion named, and the "root declares no SQL driver" justification for
the two uncovered rows is **true** (verified against `go.mod`).

The remaining defects cluster where they always have: in **classification** (which knobs are in the class) and in
**fold-ins copied forward without re-deriving them against revision 3's own changes**.

---

## BLOCKER-1 — §2.1's partition is *still* incomplete under the contract's second clause: at least three knobs certified "safe" are unbounded-memory levers, two of them measured

**The claim.** Spec §2.1: *"The 16 partition into **7 defective** and **9 safe**, **with no residual**."* §3's
contract: *"No exported msgin sizing option can panic, corrupt runtime state, **or leave a bounded structure
unbounded** — at construction or at any later use."* The nine safe rows carry three verdict strings:
*"comparison only"*, *"limit, never allocated"*, *"bounded retention"*.

**The evidence.** Two of those verdict strings are false for three of the rows they are applied to.

### (a) `routing.WithCompletionSize` — *"safe — comparison only"* — is the §1.3 shape exactly

`aggregator.go:132-135` sets the release predicate to `len(g.Messages()) >= n`. `Handle` (`aggregator.go:371-408`)
adds every message to the group *first* and releases only when that predicate holds. At `n = 1<<62` it never holds,
so the group accumulates without bound. Measured, one correlation key, `memory.GroupStore`:

```
$ go test -run TestCompletionSize -v .
NewAggregator(WithCompletionSize(1<<62)) err=<nil>  (ACCEPTED)
after 60000 Handle() into ONE group: released=0 groups=1 live heap delta=30078064 B (28.7 MiB)
WithCompletionSize(2) over 6 msgs: released=3 (cap works at small n)
```

A 400 000-message run of the same probe **did not finish inside a 4m20s test timeout** (`memory.GroupStore.Add`
clones the group snapshot per call, so the growth is quadratic in time as well as linear in memory):

```
NewAggregator(WithCompletionSize(1<<62)) err=<nil>  (ACCEPTED)
panic: test timed out after 4m20s
	running tests:
		TestCompletionSizeUnboundedGrowth (4m20s)
```

`WithMaxGroups` does not help — it caps the *number of groups*, not members per group. The reaper does not help
either: `reapInterval()` (`aggregator.go:526-532`) returns `cfg.timeout`, and `memory.GroupStore.RecoverInterval()`
returns `0` (`groupstore.go:205`), so with no `WithGroupTimeout` the interval is `0` and `Run` blocks on
`ctx.Done()` without ever sweeping (`aggregator.go:505-509`).

**The verdict string is not a discriminator.** Compare the two rows in the *same table*:

| Row | Predicate | §2.1 verdict |
|---|---|---|
| `memory.WithMaxGroups` | `len(s.groups) >= n` — `groupstore.go:108` | **DEFECTIVE** — unbounded growth |
| `routing.WithCompletionSize` | `len(g.Messages()) >= n` — `aggregator.go:134` | safe — **comparison only** |

Identical shape, opposite verdicts, no stated criterion. (`WithBreakerThreshold`, which shares the verdict string,
*is* genuinely safe — `b.fails >= b.threshold` over an `int` counter, `breaker.go:164`, accumulates nothing. So the
string is true for one row and false for the other.)

### (b) `msghttp.WithMaxBodyBytes` — *"safe — limit, never allocated"* — is allocated

`encode.go`'s `DecodeRequest` does `raw, err := io.ReadAll(http.MaxBytesReader(nil, body, cfg.maxBody()))`. The knob
*is* the only bound on that read. Measured against a 64 MiB body:

```
$ go test -run TestMaxBodyBytesIsTheOnlyBound -v .
WithMaxBodyBytes(1048576)            body=64 MiB -> err=msghttp: decode request failed: http: request body too large  TotalAlloc delta=5.0 MiB
WithMaxBodyBytes(4611686018427387904) body=64 MiB -> err=<nil>                                                        TotalAlloc delta=375.2 MiB
```

This lever is driven by a **remote peer**, not by the caller — strictly worse than the three in §1.3, which need a
caller-supplied flood. (`WithMaxPayloadBytes`, which carries the same verdict string, *is* correct:
`consumer.go:1199` tests `len(b) > c.maxPayloadBytes` against an already-materialised slice. Again: the string is
true for one row and false for another.)

### (c) `msghttp.WithMaxEventBytes` — same shape, read rather than run

`sse.go:384-389`:

```
384		case "data":
385			p.dataBuf.WriteString(value)
386			p.dataBuf.WriteByte('\n')
387			if int64(p.dataBuf.Len()) > p.maxEventBytes {
388				return ErrEventTooLarge
```

`p.dataBuf` is a `bytes.Buffer` fed by a remote SSE server; `maxEventBytes` is the only thing that stops it.
(`WithMaxResponseBytes` is genuinely safe by contrast: `drainBounded` is `io.CopyN(io.Discard, body, max)` — CPU,
not memory.)

**Why it matters.** This is **round-1 BLOCKER-2 verbatim, two revisions later, for three different knobs**: a
partition asserted complete that is not, with three knobs certified *safe* in the same table that are OOM levers
under the contract's own second clause. The consequence is not documentary — **AC-5 half 2 would ship a gate that
asserts `WithMaxBodyBytes(1<<62)` "accepts `1<<62` and its product is usable"**, i.e. a test that actively certifies
an unbounded remote-driven read as conformant and fails if anyone later bounds it. That is the inversion §1.1
warns about ("certifying as safe precisely the value with the more severe failure mode"), embedded in the class
gate.

**Recommended fix.** The same fork round 1 offered for BLOCKER-2, resolved consistently this time:

1. **Preferred — extend the census to 8+ and state the criterion.** Add `WithCompletionSize` (a `memory`/`routing`
   ceiling in "group members"), `WithMaxBodyBytes` and `WithMaxEventBytes` (byte ceilings). CLAUDE.md's
   Sensible-defaults gate explicitly contemplates the byte-cap case: *"If **no** value can be safe for an unknown
   caller (e.g. a byte cap that depends on the caller's legitimate payload size), make it **explicit/opt-in** with a
   clear typed error or documented off state"* — so for the two byte knobs a documented, opt-in "off" state
   (`WithMaxBodyBytes(-1)` meaning *explicitly unbounded*) is a legitimate landing point that the current bundle
   does not consider.
2. **Or — narrow the contract's second clause** to the knobs whose *own godoc claims to bound something*, and say so
   in one sentence, moving the rest to a named deferred class. This is a wording change but it must be **stated**,
   because §2.1's current reasons (*"comparison only"*, *"limit, never allocated"*) are false as written for these
   three rows and true for their siblings.

Whichever is chosen, §2.1 must replace the three shared verdict strings with a per-row reason that actually
discriminates, and AC-5 half 2's arm for these keys must follow the choice.

---

## MAJOR findings

| # | Finding | Evidence | Recommended fix |
|---|---|---|---|
| **M3-1** | **The mandatory small-`n` proof for `WithCapacity` HANGS as written** — the `m-2`/`M2-5` class through the next door. BLOCKER-3's fix replaced the unexecutable ceiling case with a small-`n` case, and both Spec §6 and Plan Task 3 state it *without* the fixture it needs. Spec §6 pastes `Capacity(1): enq1=<nil> enq2=msgin: message dropped by overflow policy`; Task 3's RED bullet says *"`WithMaxGroups(1)` and `WithCapacity(1)` each shed on the **second** insert with `msgin.ErrOverflowDropped`"*. `QueueStore`'s default policy is `OverflowBlock`, so the second `Enqueue` blocks on `s.sem <- struct{}{}` (`queuestore.go:124-129`) until the binary's panic. The `WithOverflow(OverflowReject)` requirement appears **only in the next bullet**, scoped to the *optional* ceiling-sized exercise that BLOCKER-3's own split removed. The cited precedent `groupstore_test.go:30-39` does **not** transfer: `GroupStore.Add` has no overflow policy. | `$ go test -race -run TestSmallNCaps -v .` → `MaxGroups(1): add1=<nil> add2=msgin: message dropped by overflow policy` / `Capacity(1) [OverflowReject]: enq1=<nil> enq2=msgin: message dropped by overflow policy` / `Capacity(1) [default policy] enq1=<nil> (2nd enqueue would BLOCK)`. | Move `memory.WithOverflow(msgin.OverflowReject)` into the **mandatory** small-`n` bullet (Task 3) and into Spec §6's pasted line, exactly as Task 2 names `serveInBackground`. Note in §6 that the `groupstore_test.go` precedent covers only the `GroupStore` half. |
| **M3-2** | **Revision 3's "corrected" growth costs are fixture artifacts, and its claim that round 2's figure "did not survive re-derivation" is false.** All three document headers assert *"the `WithMaxGroups` growth cost (~1 GB → **283 MiB live**)"* as one of four numbers that failed re-derivation. Re-measured with a realistic message, round 2's figure reproduces and revision 3's does not; revision 3's figures reproduce only with a **zero-value** `Message[any]` (no headers map, no id, no timestamp), which is not what the plan's exercise would enqueue. | `-race`, this tree. Realistic (`msgin.New[any](i)`): `MaxGroups(1<<20): shed at i=1048576 elapsed=2.708s cumulative=1093350816 (1042.7 MiB) live-after-GC=894818704 (853.4 MiB)`; `Capacity(1<<20): elapsed=1.682s cumulative=808145432 (770.7 MiB) live-after-GC=507580288 (484.1 MiB)`. Zero-value message: `ZERO-MSG MaxGroups(1<<20): cumulative=362.7 MiB live=221.3 MiB`; `ZERO-MSG Capacity(1<<20): cumulative=298.7 MiB live=60.1 MiB` — note revision 3's *"108 MiB live / 299 MiB cumulative"* for `WithCapacity` matches this cumulative figure to 0.1 MiB. | Restate the figures **with their fixture** ("zero-value `Message[any]`" vs "a `msgin.New` message"), or quote the realistic pair. Delete the claim that round 2's number failed re-derivation from all three headers — it did not; the two probes measured different things. |
| **M3-3** | **"The six `msghttp` keys" is wrong in count *and* in content — round 2's M2-6 sizing was copied forward without re-deriving it against revision 3's own changes.** Spec §6 and Plan Task 6 both state *"the six `msghttp` keys produce an `SSEServer`, so exercising one from a root test needs a root-local equivalent of `serveInBackground`"*. There are **seven** `msghttp` keys among the 17. Worse, this increment moves **two of them** (`WithConnectionBuffer`, `WithMaxConnections`) into the *rejects* arm, and `WithSuccessStatus` was already there — so at most **four** need an "accepts + product usable" fixture, and of those only `WithReplayBuffer` plausibly needs a live `SSEServer` (`appendRing`, `sse_server.go:466-476`). The largest fixture in the plan is sized against a set that the increment itself dissolves. | AST scan, `adapter/http/options.go`: `WithConnectionBuffer:883`, `WithMaxBodyBytes:426`, `WithMaxConnections:865`, `WithMaxEventBytes:819`, `WithMaxResponseBytes:730`, `WithReplayBuffer:926`, `WithSuccessStatus:566` = **7**. All seven are `msghttp.Option` validated by `NewConfig` (`options.go:1119`); `NewSSEServer(opts ...Option)` (`sse_server.go:129`) delegates to it. | Recount to seven, then partition the seven by *arm after this increment* (3 rejects / 4 accepts) and name the SSE fixture only for the row that needs it. Also define *"its product is usable"* — round 2 flagged it as unsized and it is still undefined for the read-limit knobs. |
| **M3-4** | **§2.0's "four class members among the 27 methods" is hand-derived, not criterion-derived — the count moved 3 → 4 and is still not reproducible.** The row admitting `sql.Source.Poll` reads: *"`max` becomes the query `LIMIT` and sizes `make([]msgin.Delivery, 0, len(rows))`"*. Under that criterion **three more methods qualify identically** and are omitted; under a strict criterion (capacity derived from the parameter, e.g. `min(max, len(s.ready))`) **`sql.Source.Poll` and `sql.QueueStore.Claim` do not qualify at all** and the set is two. | `{postgres,mysql,sqlite}GroupDialect.ExpiredGroups(ctx, q, table, before, leaseTTL, limit int)` pass `limit` as the SQL `LIMIT` and then `out := make([]msginsql.GroupRows, 0, len(cands))` — `postgres/groupdialect.go:266`+`:305`, `mysql/groupdialect.go:258`+`:296`, `sqlite/groupdialect.go:274`+`:312`. Verified by reading `postgresGroupDialect.ExpiredGroups` in full. The `memory`/`channel` pair is the only one where the parameter itself sizes the `make` (`queuestore.go:173 min(max, len(s.ready))`). | State the criterion in §2.0 ("the parameter itself sizes a `make`"), then re-derive the list mechanically from that criterion. Under the strict criterion the answer is **two** — both already covered by manual rows — and the "named but uncovered" pair disappears, which *simplifies* D-AA's limitation rather than complicating it. |

---

## MINOR findings

| # | Finding | Evidence | Recommended fix |
|---|---|---|---|
| **m3-1** | **Three stale `file:line` citations survived the "every figure re-derived" pass**, all carried unchanged from round 2 — and one of them round 2 explicitly certified as correct. | (a) `memory.go:58` for `b := &Broker{ch: make(...)}` in **Spec §3.3 and ADR D-Y**: `58:func New(opts ...Option) *Broker` / `59:	b := &Broker{ch: make(chan msgin.Message[any])}`. Round-2 m2-1 listed `memory.go:58` among the citations it had "checked and are correct". (b) `memory.go:34-37` for the *clamped to 0* godoc in **Spec §4 and Plan Task 4**: line 34 is `\n` (verified by `od -c`); the godoc is `:35-37`. (c) `ratelimit.go:45` for the `rps <= 0` rejection in **Spec §3.7.4**: `43:	if rps <= 0 \|\| burst < 1 {` / `44:		return nil, msgin.ErrInvalidRateLimit` / `45:	}`. | `:58`→`:59`, `:34-37`→`:35-37`, `:45`→`:43`. The corrections revision 3 *did* make (`sse_server_test.go:176`→`:180`, `memory.go:17`→`:16`, `aggregator.go:505-509`→`:511`, `main_test.go:14`→`:15`) all verify correct — this is the same class, three instances short. |
| **m3-2** | **§3.7.4's "positive evidence" for the `time.Duration` exclusion enumerates only `NewTicker`/`NewTimer` and misses the `clock.After(d)` class** — including the one site fed by the very value the paragraph is about. So *"outside the gate **and** currently safe, checked"* is not established. | The five `NewTicker`/`NewTimer` sites named are exactly right (`sse_server.go:262`, `producer.go:505`, `exchange.go:449`, `attempts.go:77`, `aggregator.go:511` — all verified, all guarded). But non-test `.After(d)` with a variable duration also exists: `resilience/ratelimit.go:85 case <-b.clock.After(wait)` (`wait` derives from the saturated `b.interval`), `endpoint/poller.go:172`, `endpoint/consumer.go:485`, `adapter/http/sseclient.go:442`, `adapter/cron/source.go:280`. | Either widen the enumeration to `After`/`Sleep`/`NewTimer`/`NewTicker`, or drop the word *"checked"* and keep only *"outside the gate by construction"*, which is the claim that actually holds. |
| **m3-3** | **Spec and plan disagree on AC-4's `queuestore.go:108` mutant arithmetic.** Spec §6 AC-4 states the mutant as *"`chan struct{}` → non-empty ⇒ **50,331,648 B**"* and the assertion as *"48× below the mutant"* (a 48-byte element). Plan Task 5 names the mutant concretely as `chan struct{}` → **`chan msgin.Message[any]`** = 24 B → 25,165,824 B, i.e. **24×**. | `sizeof(msgin.Message[any]) = 24`, measured: `memory.New(WithBuffer(1<<20))` → `25166072` B. The `delta < 1 MiB` assertion kills either mutant (verified), so only the stated arithmetic is wrong. | Pick one mutant type and one figure; state it identically in both documents. |
| **m3-4** | **Two different code shapes are shown for the same function.** Spec §3.3's required shape calls a helper `b.latchSizing(n)`; Plan's R2 block inlines `if b.err == nil { b.err = fmt.Errorf(...) }`. No document defines `latchSizing`, and Plan 028's shipped latch is the inline form (`memory.go:60-64`). | Spec §3.3 code block vs Plan §"The FOUR code shapes" R2 block. | Use the plan's inline form in both, or introduce `latchSizing` explicitly in the plan's GREEN bullet. |
| **m3-5** | **AC-5 half 2's row shape does not fit `WithBuffer`.** *"A defective knob asserts it **rejects** `1<<62` with a typed error"* — but after this increment `memory.New(WithBuffer(1<<62))` returns a `*Broker` with **no error**; the fault surfaces at `Send`/`Stream`. One of the 17 rows cannot be written to the stated shape. | Spec §6 AC-5.2 and ADR D-AA half 2; §3.2/§3.3 (R2). Spec §3's contract already states the two surfaces — AC-5 abbreviates it away. | Restate half 2's defective arm as *"reports the fault through the surface §3 names for it — the constructor's return, or the first use"*, matching §3 verbatim. |
| **m3-6** | **§2.1's summary line nests backticks inside a code span and renders mangled.** | Spec line 256: ``**`7 defective + 9 safe = 16 options; + 1 positional (`burst`) = 17 conformance keys.`**`` | Drop the outer backticks. |
| **m3-7** | **The lower bound `lo` is never stated for the four `<= 0` knobs**, yet AC-2b requires asserting *"the full `[lo, hi]` range render"*. Both worked examples (`WithMaxInFlight`, `WithConcurrency`) are `< 1` knobs with `lo = 1`; §3.1's table gives only *"Existing arm `<= 0`"* and §3.4 gives only the ceiling. | Spec §3.1 table; §3.4 table; Plan's ceilings table. | Add a `lo` column to §3.1's table (`1` for the five `<= 0`/`< 1` R1 knobs, `0` for `WithBuffer`). |

---

## Verified-negative (attacked, found sound)

Do not re-litigate these in round 4.

**The AST scan reproduces exactly, under the shipped gate's own walk rules** (`strings.HasPrefix(base, ".") || base == "vendor"`, `_test.go` excluded — `option_guard_gate_test.go:386-399`), which is stricter than skipping `docs/`:

```
=== EXPORTED FUNCTIONS (Recv==nil) with int/int64 param in ANY position: 17
=== EXPORTED METHODS with int/int64 param: 27
=== ... of which on UNEXPORTED receivers: 22
=== TOTAL any FuncDecl: 44
```

The 22 partition as §2.0 says: **21 in leaf modules** (mysql/postgres/sqlite dialects and group-dialects) **plus
`responseTracker.WriteHeader` in root** (`adapter/http/inbound.go:29`). The two tracked `.go` files under `docs/`
(`docs/plans/027-tools/{decls,qualify}.go`) declare **no** exported function with an int parameter, so they do not
perturb the key set — a real hazard, checked.

**§2.1's 17 rows are set-identical to the AST's 17 keys.** Name-by-name diff: no extra, no missing.

**The census command still yields exactly 16**, and the doc counts match Task 7's targets:

```
$ git ls-files '*.go' | grep -v _test | xargs grep -hnE '^func With[A-Za-z]+(\[[^]]*\])?\([a-z]+ (int|int64)\)' | wc -l
      16
$ ls docs/specs/[0-9]*.md | wc -l                                          16
$ ls docs/adrs/[0-9]*.md | wc -l                                           31
$ ls docs/plans/[0-9]*.md | sed -E 's|.*/([0-9]{3}).*|\1|' | sort -u | wc -l    29
```

**"16 vs 17" is now used consistently in all three documents** — every occurrence was inspected; there is no
residual "16" standing where 17 is meant. BLOCKER-1 of round 2 landed on the count.

**`WithConcurrency`'s band table reproduces verbatim — all ten rows, including the counter-examples:**

```
$ go test -run TestWGBand -v .
n=1073741824             int32=1073741824   Add ok
n=2147483647             int32=2147483647   Add ok
n=2147483648             int32=-2147483648  Add PANICS: sync: negative WaitGroup counter
n=2147483649             int32=-2147483647  Add PANICS: sync: negative WaitGroup counter
n=4294967295             int32=-1           Add PANICS: sync: negative WaitGroup counter
n=4294967296             int32=0            Add ok
n=4294967301             int32=5            Add ok
n=3221225472             int32=-1073741824  Add PANICS: sync: negative WaitGroup counter
n=1099511627776          int32=0            Add ok
n=4611686018427387904    int32=0            Add ok

$ go test -run TestConcurrencyRunPanics -v .
WithConcurrency(2147483648): NewConsumer err=<nil> consumer nil?false
WithConcurrency(2147483648): Run PANICKED: sync: negative WaitGroup counter
```

**The single error shape is true at both ends for all seven knobs, and both wrap directions survive `fmt.Errorf`:**

```
$ go test -run TestRenderBothEnds -v .
WithMaxInFlight        n=0        Is=true  IsPermanent=false | …: endpoint.WithMaxInFlight: 0 not in [1, 1048576]
WithMaxInFlight        n=1048577  Is=true  IsPermanent=false | …: endpoint.WithMaxInFlight: 1048577 not in [1, 1048576]
WithConcurrency        n=0/65537  Is=true  IsPermanent=false | …: endpoint.WithConcurrency: … not in [1, 65536]
WithConnectionBuffer   n=0/65537  Is=true  IsPermanent=false | …: msghttp.WithConnectionBuffer: … not in [1, 65536]
WithMaxConnections     n=0/65537  Is=true  IsPermanent=false | …: msghttp.WithMaxConnections: … not in [1, 65536]
WithCapacity           n=0/1048577 Is=true IsPermanent=false | …: memory.WithCapacity: … not in [1, 1048576]
WithMaxGroups          n=0/1048577 Is=true IsPermanent=false | …: memory.WithMaxGroups: … not in [1, 1048576]
WithBuffer             n=-1       Is=true  IsPermanent=true  | msgin: permanent: …: memory.WithBuffer: -1 not in [0, 1048576]
WithBuffer             n=1048577  Is=true  IsPermanent=true  | msgin: permanent: …: memory.WithBuffer: 1048577 not in [0, 1048576]
```

`IsPermanent` is `errors.As(err, &pe)` first (`reliability.go:86-92`) and its sentinel list (`:94-96`) covers only
`ErrPayloadType`/`ErrPayloadDecode`/`ErrPayloadTooLarge` — so constraint 4's asymmetry is implementable exactly as
written. **Nothing in the workspace asserts any of the five sentinel strings**: the only `.go` hits for the message
texts are the declarations themselves (`errors.go:51`, `:144`, `:258`, `adapter/http/errors.go:145`, `:150`) plus
three comments. The genericisation is test-safe.

**All four code shapes are correctly assigned, and every pasted snippet is verbatim-accurate.** Read in full:
`consumer.go:270-274` (R1-a, `else if` on `maxInFlightSet`), `consumer.go:251`+`:262-264` (R1-b, no set flag),
`options.go:1156-1160` and `:1162-1166` (R1-a ×2), `groupstore.go:84`+`:91-93` (R1-b), `queuestore.go:97-103`
(R1-c, nested inside `if cfg.capacitySet`, so the unset→`defaultCapacity` path is untouched by the added arm),
`memory.go:38-45` (R2). Every §3.1 "Checked in" citation is exact.

**AC-4's baselines reproduce, 3/3 each, with a noise floor of exactly 0 B**, and both mutants provably escape the
stated bounds:

```
$ go test -run TestAllocDeltas -v .
noop noise floor                           delta=0 B          (×3)
NewQueueStore(WithCapacity(1<<20))         delta=288 B        (×3)   → assertion `< 1 MiB`
NewGroupStore(WithMaxGroups(1<<20))        delta=120 B        (×3)
memory.New(WithBuffer(1<<20))              delta=25180328 / 25176568 / 25166072 B (24.0 MiB)
NewConsumer(WithMaxInFlight(1<<20))+Run    delta=50353096 / 50336168 / 50335992 B (48.0 MiB) → `40 MiB < d < 64 MiB`
```

Mutant (a) `queuestore.go:108` → a 24-byte element = 25,165,824 B, far above `1 MiB`; mutant (b) `credit.go:21` →
a second 48 MiB channel ≈ 96 MiB, above the `64 MiB` upper bound. **Both fire.** The `40 MiB` lower bound is
genuinely load-bearing, as §6 says.

**Every ceiling-case AC is executable, measured rather than argued:**

```
WithConcurrency(1<<16)+Run: err=context deadline exceeded elapsed=389.6ms StackSys delta=268926976 (256.5 MiB) TotalAlloc delta=41.8 MiB goroutines-now=2
ServeHTTP @ connBuffer=1<<16: TotalAlloc delta=1592920 B (1.52 MiB) code=200 ; ServeHTTP returned after cancel; goroutine joined
naive ServeHTTP BLOCKED for 3s (test would hang / leak)
```

65,536 workers spawn and join in 0.39 s under `-race` at 256.5 MiB of stack (2,049.6 B/goroutine plain, 4,103 B
under `-race` — the *"~128 MiB / ~257 MiB"* pair is right and the MB/MiB mix is gone); the connBuffer ceiling costs
1.52 MiB for one connection; `m-2`'s naive-`ServeHTTP` hang is still real. `adapter/http` and `adapter/memory` each
do run `goleak.VerifyTestMain` (`encode_test.go:21`, `memory_test.go:16`), as do root (`main_test.go:15`) and
`endpoint` (`consumer_test.go:25`).

**The "root declares no SQL driver" justification for the two uncovered rows is TRUE.** Root `go.mod` requires only
`clockwork`, `robfig/cron/v3`, `testify`, `goleak` (+ six indirects). No `mattn`, `modernc`, `lib/pq`,
`go-sql-driver` or `sqlmock`. D-AA's stated limitation is sound on its own terms.

**Revision 3's two corrections to round 2's `WithCompletionSize` facts are both right.**
`NewAggregator(store, fn, opts...)` — the second positional is the aggregation **`fn`** (`aggregator.go:300-303`);
`release` is an option defaulted in the config literal (`:311`). The bare call returns **`msgin: channel store is
nil`** (`ErrNilStore`, `aggregator.go:306`), not round 2's string. Verified by execution.

**The 17-key constructibility probe reproduces**: 15 return `err=<nil>` at `1<<62`, `WithSuccessStatus` rejects with
`msghttp: status code must be in [100,599]`, `WithCompletionSize`'s bare-call error is a fixture gap. So half 2 has
19 plantable rows (17 + 2 manual) as claimed.

**D-Y's premise still holds and AC-3 is still the only test that can catch the wrong shape:**

```
memory.New(nil, WithBuffer(1<<62)) PANICKED: makechan: size out of range   <- D-Y premise
memory.New(WithBuffer(1<<62), nil) PANICKED: makechan: size out of range
WithBuffer(-1): no panic (silently clamped to 0)
```

**The two manual conformance rows are constructible from a root blackbox test**: `memory.NewQueueStore(opts…)`
(`queuestore.go:89`) takes no required args, and `channel.NewQueueChannel(store msgin.ChannelStore)`
(`queuechannel.go:34`) accepts it; `QueueChannel.Poll` forwards verbatim to `QueueStore.Claim`
(`queuechannel.go:50-52`), so one exercised chain covers both. `capability_test.go` already imports both packages
from `package msgin_test`.

**Every remaining `file:line` in the bundle that I checked is exact** (beyond the three in m3-1):
`queuestore.go:108`, `:132`, `:166`, `:173`, `groupstore.go:84`, `:91`, `:94`, `:108`, `sse_server.go:182`, `:201`,
`:262`, `:466-476`, `sse_server_test.go:180`, `consumer.go:251`, `:262`, `:272`, `:330`, `:384`, `:385`,
`:457-459`, `credit.go:21`, `poller.go:36`, `aggregator.go:134`, `:306`, `:511`, `ratelimit.go:42`, `:48-49`,
`options.go:1158`, `:1164`, `errors.go:51`, `:144`, `:258`, `adapter/http/errors.go:145`, `:150`,
`adapter/database/sql/queuestore.go:71`, `source.go:155`, `main_test.go:15`, `groupstore_test.go:30-39`,
`memory.go:16-23`, `:38`, `:43`, `exchange.go:317`, `:449`, `attempts.go:77`, `producer.go:505`.

**Housekeeping is clean.** Docs-link gate **arms 1 and 2 report nothing** over the three bundle files, the two
audit records, and the two modified working-tree files. `git status --short` shows exactly the seven expected doc
files and nothing else. The tree is green: `GOWORK=off go test ./...` → **11/11 `ok`**.

**CLAUDE.md compliance holds** where checked: blackbox-only (constraint 1), assert-closure tables (2), zero
exported-surface delta (3, and the ceilings are unexported consts), the Go-skills header note, TDD, per-task green
units, the 8×8 delivery gate, `/simplify` before the reviews, `Spec:`/`Plan:`/`ADR:` trailers, the multi-instance
statement (Spec §7), and the Sensible-defaults escape clause quoted in D-W. The hot-path branch table enumerates
every new arm.

---

## Round-2 fix verification

One row per round-2 finding. **LANDED** = fixed correctly and nothing else broke. **LANDED-BUT-FLAWED** = addressed
but the fix is wrong, incomplete, or reintroduces the class elsewhere. **REGRESSED** = the named instance is fixed
and the same defect is back through another door.

| R2 id | Round-2 finding (one line) | Verdict | Why |
|---|---|---|---|
| **B2-1** | Key set is 17 not 16; function/method boundary unstated | **LANDED-BUT-FLAWED** | The count landed cleanly — 17/27/22/44 all re-derived, `Recv == nil` stated as a decision in Spec §2.0 + D-AA + Task 6, no residual "16" anywhere. The *limitation* it required is flawed: "four class members" has no stated criterion and is not the measured set. → **M3-4** |
| **B2-2** | `WithConcurrency` panics inside `Consumer.Run`; predicate `int32(n) < 0` | **LANDED** | Reproduced end-to-end on the first try; the band table reproduces verbatim in all ten rows including `1<<32`/`1<<40`/`1<<62`; ADR says "FOUR panic"; Task 0 asserts both modes and explicitly forbids both prior wrong formulations; the second mode is recorded as *timed out, not observed*. |
| **B2-3** | The `n <= ceiling` row is unexecutable for `WithMaxConnections` | **LANDED-BUT-FLAWED** | The three-family split is right and **every resulting row is executable** — I ran the allocating three (48.0 / 1.52 / 24.0 MiB) and `WithConcurrency` at the ceiling (0.39 s, 256.5 MiB, goroutines back to 2). But the small-`n` replacement it introduced hangs for `WithCapacity` as written → **M3-1**, and the growth costs quoted alongside it are fixture artifacts → **M3-2**. |
| **M2-1** | Merged R1 condition renders a false message on the lower arm | **LANDED** | One shape everywhere; verified true at both ends for all 7 knobs; `errors.Is` and `IsPermanent` both survive the wrap; AC-2b mandates the lower-end case; constraint 4 says *"Never render `exceeds`"*. |
| **M2-2** | R1-a/R1-b assignment wrong for two of six knobs | **LANDED** | Four shapes named, all six sites pasted verbatim and all six verified against the source; R1-c correctly identified as nested inside `if cfg.capacitySet`, which preserves the unset-default path. |
| **M2-3** | AC-4's premise false for the `endpoint` arm; `credit.go:21` unmutated | **LANDED** | Per-site assertions, both baselines reproduce 3/3 at a 0 B noise floor, the `40 MiB` lower bound is present and load-bearing, and `credit.go:21` finally has mutant (b) — which provably escapes the upper bound. Only the *sibling* mutant's arithmetic is inconsistent → **m3-3**. |
| **M2-4** | `time.Duration` exclusion justified by a false overflow claim | **LANDED-BUT-FLAWED** | The false claim is retracted and replaced with the measured saturation statement, and the five `NewTicker`/`NewTimer` sites named are exactly the five that exist and are all guarded. But the positive evidence misses the `clock.After` class — including the one site fed by that very saturated value → **m3-2**; and `ratelimit.go:45` is miscited → **m3-1**. |
| **M2-5** | "State the cost" applied to Task 1 only; Task 3's growth exercise hangs | **LANDED-BUT-FLAWED** | Tasks 3 and 5 now state costs and `WithOverflow(OverflowReject)` is named — but only inside the *optional* ceiling-exercise bullet, leaving the **mandatory** small-`n` case unqualified and hanging → **M3-1**; and the stated costs are 3–4× low → **M3-2**. |
| **M2-6** | "Product is usable" unsized; two keys are not one-liners | **LANDED-BUT-FLAWED** | Both fixtures are named and round 2's two wrong facts about the aggregator fixture are corrected and verified. But "the six `msghttp` keys" is wrong in count (7) and in content (the increment moves 2 of them to the rejects arm) → **M3-3**, and *"usable"* is still undefined. |
| **m2-1** | `queuestore.go:131` off by one | **LANDED** | `132: s.ready = append(s.ready, entry{…})` — both §1.3 and §2.1 now cite `:132`. |
| **m2-2** | `concurrencyCeiling` rationale mixes MB and MiB | **LANDED** | Now *"~128 MiB of stack, ~257 MiB under `-race`"* consistently in Spec §3.4, D-Z, and Task 1. Measured 2,049.6 B/goroutine plain → 128.1 MiB; 4,103 B under `-race` → 256.5 MiB. |
| **m2-3** | `WithBuffer` is the only D-Z row with no measured cost | **LANDED** | **25,166,072 B (24.0 MiB)** in both §3.4 and D-Z; I measured exactly `25166072` on one of three runs. |
| **m2-4** | Sentinel-message treatment inconsistent across the six | **LANDED** | All five sentinel messages genericised in one increment (§3.5's table, D-X, Tasks 1/2/3); all five "Today" texts verified verbatim; test-safety independently re-verified. |
| **m2-5** | Task 3 edits root but commits as `fix(memory)` | **LANDED** | Task 3 states *"touches **root + `adapter/memory`**"* with `errors.go:258` and re-states constraint 7. |
| **m2-6** | Widen `memory.go:16-23`; escape sentence needs no change | **LANDED** | Task 4 states the determinate answer, corrects `:17`→`:16` (verified: the comment starts at :16, field at :23), and ADR's "Bad, accepted" bullet records the resolution. |
| **m2-7** | `WithBuffer`'s clamp godoc contradicts §3.6 | **LANDED** | An explicit Task 4 bullet deletes the sentence, states the replacement content, and cross-references AC-3b's `WithBuffer(-1)` case so the godoc edit is not orphaned. (Its `:34-37` citation is off by one → **m3-1**.) |
| **m2-8** | AC-4's delta needs three stated measurement conditions | **LANDED** | All three (`no t.Parallel`, `runtime.KeepAlive`, `TotalAlloc`) are in Spec §6 and Task 5. Measured with all three: noise floor exactly 0 B, 288 B / 120 B / 48.0 MiB stable across three runs. |

**Totals: LANDED 12 · LANDED-BUT-FLAWED 5 · NOT LANDED 0 · REGRESSED 0. Five of seventeen did not land cleanly**
(rounds 1→2 were 10 of 19). Every remaining flaw is in a *fold-in copied forward without re-deriving it against
revision 3's own changes* — none is a regression of the named instance.

---

## Carried into round 4 — suspected, not proven

- **Peak RSS of the whole `go test ./...` run.** Still unmeasurable end-to-end because the code does not exist.
  Individually measured this round: Task 1 ≈ 48 MiB + 256.5 MiB of goroutine stack, Task 2 ≈ 1.5 MiB, Task 5
  ≈ 48 MiB, Task 3 ≈ 0 under BLOCKER-3's split (or 484–853 MiB live if a ceiling-sized growth case is written with
  realistic messages). Go runs packages in parallel, so the peak is a sum. Task 7 must measure and record it.
- **Whether `WithReplayBuffer` belongs in BLOCKER-1's residual class.** `appendRing` (`sse_server.go:470-476`) does
  evict at the cap, so it is not "the cap stops capping" — but at `1<<40` the ring reaches that size one entry at a
  time from *remote* frames, which is the same remote-driven shape as `WithMaxEventBytes`. §2.1's *"a resource
  decision under an explicit bound"* may or may not survive that framing; not settled here.
- **`msgin.Message[any]`'s per-message cost in the growth probes.** M3-2 establishes that the growth figures are
  fixture-dependent by a factor of ~3; it does not establish which fixture the plan's exercise would actually use,
  because the exercise is (correctly) no longer required.
- **Whether the `sql`/`channel` delegation chain should be covered at all.** M3-4 shows the "four" is underived; it
  does not settle whether the strict criterion (two members, both already covered) is the right one — that is a
  design call for the author, not an audit finding.

---

## Auditor's method note

Every command and every figure in this record was run first-hand by the auditor on the tree at `48bbe83` with
`GOTOOLCHAIN=go1.25.13`, `GOWORK=off`, darwin/arm64, from a throwaway module outside the repository
(`replace github.com/kartaladev/msgin => /Users/zakyalvan/Documents/RND/msgin`). Nothing was transcribed from the
bundle or from rounds 1–2: the `go/ast` scan, the census, the `WaitGroup` band, the end-to-end `Run` panic, the
seven-knob render/`errors.Is`/`IsPermanent` matrix, the five allocation-delta series, the goroutine-stack
measurement, the ceiling-case `Run` and `ServeHTTP` runs, the naive-`ServeHTTP` hang, the four growth fills
(realistic and zero-value messages), the `WithCompletionSize` and `WithMaxBodyBytes` OOM-lever probes, the 17-key
constructibility probe, the D-Y premise, the docs-link gate and every `file:line` citation are all first-hand
output.

A pre-existing probe directory from an earlier session was found in the shared scratchpad; all measurements above
were re-run in a **separate, freshly created module** (`…/scratchpad/r3`) so no earlier session's output could be
mistaken for mine. All probe files live outside the repository and were deleted after the run; `git status --short`
was re-checked and shows exactly the seven expected documentation files. No repository file was modified except
this report.

---

**VERDICT: NOT SAFE TO IMPLEMENT.**
