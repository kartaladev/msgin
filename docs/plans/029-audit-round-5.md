# Plan 029 — Adversarial design audit, round 5

**What was audited.** The complete **revision-5** design bundle, together, as CLAUDE.md's design-time gate
requires (spec + ADR + plan in one pass, before any implementation code):

- [`docs/specs/016-sizing-option-bounds.md`](../specs/016-sizing-option-bounds.md) (revision 5)
- [`docs/adrs/0032-sizing-option-bounds.md`](../adrs/0032-sizing-option-bounds.md) (revision 5)
- [`docs/plans/029-sizing-option-bounds.md`](029-sizing-option-bounds.md) (revision 5)

Prior records read and treated as **immutable history, not authority**: [round 1](029-audit-round-1.md),
[round 2](029-audit-round-2.md), [round 3](029-audit-round-3.md), [round 4](029-audit-round-4.md).

**Tree state.** `48bbe83` (`Merge branch 'feat/nil-option-elements'`), darwin/arm64, `GOTOOLCHAIN=go1.25.13`.
No implementation code exists for this increment.

**Method.** Every figure below is my own command output, produced in throwaway modules **outside** the
repository (`/private/tmp/…/scratchpad/probe/{scan,maxresp,replay,agg,shape}`, each with
`replace github.com/kartaladev/msgin => /Users/zakyalvan/Documents/RND/msgin`). Every figure names its
**fixture and its measurement protocol**, the standard revision 5 imposes on itself. No repository file was
modified except this report. See the method note at the foot for re-derived vs spot-checked.

---

**VERDICT: NOT SAFE TO IMPLEMENT** — **2 BLOCKERs, 5 MAJORs, 8 MINORs.**
Round-4 fix verification: **11 LANDED, 5 LANDED-BUT-FLAWED, 0 NOT LANDED, 0 REGRESSED** (of 16) — the
cleanest fold-in of the five rounds, and the first in which the **ADR received every fold-in aimed at it**.

The two BLOCKERs are the project's two named failure modes, one each:

1. **BLOCKER-1 is round-4 BLOCKER-1 one door over.** `msghttp.WithMaxResponseBytes` is certified *safe (d) —
   discarded, never retained*, on the strength of `drainBounded`. It is also the **sole bound on an
   `io.ReadAll` of a remote-driven response body** (`exchange.go:130-131`) — structurally identical to
   `WithMaxBodyBytes`, which §2.1 files as a class member. The option's **own godoc says so in its first
   line**. Under D-AB it is a class member; the census is 9 fixed + **3** deferred + **4** safe. Round 4
   affirmatively re-certified this row in its Verified-negative table, so this is the fifth consecutive round
   in which a partition asserted "with no residual" has one.
2. **BLOCKER-2 is round-4 M4-1 one file over.** Revision 5's BLOCKER-1 fold-in (`WithReplayBuffer` → 9th
   defective knob) landed in the spec's §1.5/§2.1/§3.1/§3.4/§3.5 and Plan Task 2 — and **not** in Plan Task 7,
   the task that writes the gate. Task 7 still counts *"fixed here (8)"* / *"safe (6 + burst)"* and still
   files `WithReplayBuffer` in the **accepts, safe** arm. Following Task 7 as written produces a conformance
   row asserting `NewConfig(WithReplayBuffer(1<<62))` accepts — which **Task 2 in the same increment makes
   false**. Four further stale twins survive in the spec (§3.7.1, §4, §6, §7) and two more in the plan.
   Revision 5's own new guard — *"after folding a finding, grep its key phrase across all three files"* —
   would have caught every one of them; it was not run.

---

## What revision 5 got right

This is a good revision and three of its four headline claims verify exactly.

1. **Retraction #1 is correct and reproduces.** `NewConfig` really does reject an explicit non-positive byte
   cap; revision 4's *"`WithMaxBodyBytes(-1)` today means use 1 MiB"* was false.

   ```
   NewConfig(WithMaxBodyBytes(-1))        -> cfg!=nil=false err=msghttp: max body bytes must be > 0
   NewConfig(WithMaxBodyBytes(0))         -> cfg!=nil=false err=msghttp: max body bytes must be > 0
   NewConfig(WithSuccessStatus(1<<62))    -> cfg!=nil=false err=msghttp: status code must be in [100,599]
   NewConfig(WithReplayBuffer(0))         -> cfg!=nil=false err=msghttp: replay buffer must be > 0
   NewConfig(WithReplayBuffer(1<<62))     -> cfg!=nil=true  err=<nil>
   ```

2. **Retraction #2's re-measurement is correct**, and the GC protocol is the right one. Fixture: one
   correlation key, realistic `msgin.New` messages, `WithCompletionSize(1<<62)`; protocol: `runtime.GC()`
   before **both** reads, `HeapAlloc` delta = live, `TotalAlloc` = cumulative, `KeepAlive(agg, store)`:

   ```
   members=4096    elapsed=49ms     cumulative=  206.8 MiB  live(GC'd)= 2.09 MiB
   members=16384   elapsed=641ms    cumulative= 3143.9 MiB  live(GC'd)= 8.13 MiB
   members=60000   elapsed=7.267s   cumulative=41475.2 MiB  live(GC'd)=29.99 MiB
   members=65536   elapsed=8.406s   cumulative=49453.2 MiB  live(GC'd)=32.52 MiB
   ```

   §1.4's published 2.0 / 7.8 / 28.7 / 31.0 MiB is right; revision 4's 3.2 / 13.8 / 67.8 was un-GC'd garbage.
   *(The correction reached §1.4 and nowhere else — MAJOR M5-1.)*

3. **The five-part `WithCompletionSize` fixture is right, and so is the retraction of the retraction.** The
   gate order is verbatim what §6.2 and Task 4 print, and the figure holds:

   ```
   NewAggregator(nil, fn)                       err=msgin: channel store is nil
   NewAggregator(store, fn)                     err=msgin: aggregator output channel is nil
   NewAggregator(store, fn, WithOutputChannel)  err=<nil>
     + WithCompletionSize(1<<62)                err=<nil>
   default correlate, Handle(no corr header)    err=msgin: permanent: msgin: message has no correlation key
   5-part fixture NewAggregator err=<nil>
   WithCompletionSize(2) over 6 msgs: released=3
   ```

   `msgin: aggregator output channel is nil` is indeed the **next** gate, not a wrong string.

4. **R1-e is a real, correctly-diagnosed sixth shape, and the prescribed edit is right.**
   `options.go:1174` is verbatim `if cfg.replayBufferSet && cfg.replayBuffer <= 0 { return nil, ErrInvalidReplayBuffer }`
   — no default assignment, no `if !set` arm. Extending the same condition preserves unset ⇒ replay OFF
   (`replaySize()` returns the zero value, and `sse_server.go:429` gates on `> 0`). It compiles as written and
   the single error shape works on the 9th knob:

   ```
   R1 n=0      "msghttp: replay buffer must be > 0: msghttp.WithReplayBuffer: 0 not in [1, 65536]"      is=true permanent=false
   R1 n=65537  "msghttp: replay buffer must be > 0: msghttp.WithReplayBuffer: 65537 not in [1, 65536]"  is=true permanent=false
   R2 n=-1     "msgin: permanent: msgin: capacity must be > 0: memory.WithBuffer: -1 not in [0, 1048576]" is=true permanent=true
   ```

5. **Deleting cause (c) was the right call**, and the ADR's lesson paragraph (*"a criterion only helps if every
   row is re-derived from it, including the rows you are not changing"*) is the correct diagnosis. BLOCKER-1
   below is that sentence applied to the one row revision 5 also did not re-derive.

---

## BLOCKER-1 — `msghttp.WithMaxResponseBytes` is a class member under D-AB, certified *safe (d)*; AC-5's safe arm would again ship a gate certifying an unbounded remote-driven read

### The claim

Spec §2.1 files `WithMaxResponseBytes` as **safe (d)** — *"`drainBounded` = `io.CopyN(io.Discard, body, max)`
… discarded, never retained"* — and Plan Task 7 puts it in the **accepts, safe** arm. Round 4 affirmed it in
Verified-negative (*"(d) `WithMaxResponseBytes` → `io.CopyN(io.Discard, …)` — nothing retained"*).

`drainBounded` accounts for **five of the six** reads of `cfg.maxResponseBytes` (`exchange.go:126`,
`sseclient.go:335/338/341`, `outbound.go:370`). The sixth is the one that matters:

```go
// adapter/http/exchange.go:130-131
max := x.cfg.maxResponseBytes
body, err := io.ReadAll(io.LimitReader(resp.Body, max))
```

That is the same construct as `WithMaxBodyBytes`'s `io.ReadAll(http.MaxBytesReader(…, cfg.maxBody()))`
(`encode.go:102`), which §2.1 files as **DEFECTIVE — ceiling DEFERRED**, and the read result is **retained**:
it becomes the reply message's payload, returned to the caller. `n` is the sole bound on it.

The option's **own godoc states the accumulation in its first sentence**:

```
$ sed -n '712,713p' adapter/http/options.go
// WithMaxResponseBytes caps the number of response-body bytes an Exchange reads
// into the reply payload. Default: defaultMaxResponseBytes (1 MiB). Name it
```

### Evidence

Fixture: `httptest` server returning a **64 MiB** 200 body; `msghttp.NewExchange(url, opts…)`;
`Exchange(ctx, msgin.New[any]([]byte("ping")))`. Protocol: `runtime.GC()` before the read, **`TotalAlloc`**
delta (cumulative, GC-independent), `runtime.KeepAlive(reply)`.

```
default (unset)              err=msghttp: reply body exceeds max response bytes  replyBytes=0         TotalAlloc delta=6.1 MiB
WithMaxResponseBytes(1<<20)  err=msghttp: reply body exceeds max response bytes  replyBytes=0         TotalAlloc delta=6.1 MiB
WithMaxResponseBytes(1<<62)  err=<nil>                                           replyBytes=67108864  TotalAlloc delta=376.4 MiB

NewConfig(WithMaxResponseBytes(1<<62)) -> cfg!=nil=true err=<nil>
```

67,108,864 bytes **retained in the returned payload**, 376.4 MiB of churn — against §3.8's own measurement of
the acknowledged twin, `WithMaxBodyBytes(1<<62)` at 375.2 MiB. The two rows are the same defect, measured to
within 0.3%, and one is certified safe.

### Why it matters

1. **The census is wrong again, for the fifth consecutive round.** §2.1's summary line
   *"9 fixed + 2 deferred + 5 safe = 16 options"* becomes **9 fixed + 3 deferred + 4 safe = 16**. The
   16/17 totals are unchanged — which is exactly why the error survives a count-consistency grep and why the
   *partition*, not the total, has to be re-derived per row.
2. **AC-5 half 2 would ship an inverted gate — the precise inversion §3.8 exists to prevent.** The safe arm
   asserts a key *"accepts `1<<62` and its product is usable"*. For `WithMaxResponseBytes` "its product is
   usable" is true and is a **certificate that an unbounded remote-driven `io.ReadAll` is conformant** — and
   the row would fail if a later increment bounded it. Spec §3.8 states this argument in full and applies it
   to two of the three byte knobs.
3. **The escape phrase is not a discriminator**, the same defect D-AB was written to kill. *"Discarded, never
   retained"* is true of `drainBounded` and false of `Exchange`; §2.1's own warning — *"in each pair one row is
   safe and the other is a class member, which is exactly why the criterion, not the string, is normative"* —
   applies verbatim to this row and was not applied.

### Recommended fix

`WithMaxResponseBytes` is a **byte** cap, so the user's standing *"split by kind"* rule puts it exactly where
its two siblings already are — **class member, remedy deferred**. No ceiling, no new code shape, no seventh
shape, no change to Tasks 1–6. Concretely:

- §2.1: move the row from *safe (d)* to *DEFECTIVE — ceiling DEFERRED (§3.8)*, with the true reason
  (*sole bound on a remote-driven `io.ReadAll` at `exchange.go:130-131`*); summary line → **9 + 3 + 4 = 16**.
- §3.8: name **three** byte knobs throughout, and add the hazard disclosure to `WithMaxResponseBytes`'s godoc
  as it already requires for the other two. Note the existing godoc's CAVEAT paragraph is a natural home.
- §6 AC-5 / Plan Task 7: move the row into the **accepts, deferred** arm with the *"class member, remedy
  deferred — §3.8"* annotation. The `msghttp` arm table in **both** files then reads: rejects =
  `WithConnectionBuffer`, `WithMaxConnections`, `WithReplayBuffer`, `WithSuccessStatus`; accepts-deferred =
  `WithMaxBodyBytes`, `WithMaxEventBytes`, `WithMaxResponseBytes`; **accepts-safe = none**.
- ADR D-AB: the deferred set is three, not two; `docs/HANDOVER.md` §8 item 6 gains the third knob.
- Cause **(d)** — *"bounds CPU/streaming, not retention"* — is left with **zero** members. Delete it as a
  tombstone beside (c), or keep it with an explicit *"no current member"* note. A cause with no member is the
  shape that produced round-4 BLOCKER-1.

---

## BLOCKER-2 — revision 5's own BLOCKER-1 fold-in stopped one file short: Plan Task 7 still files `WithReplayBuffer` as *safe*, so the gate it writes contradicts Task 2

### The claim

Revision 5 makes `WithReplayBuffer` a 9th defective knob and deletes cause (c). The fold-in reached Spec
§1.5, §2.1, §3.1, §3.4, §3.5, §6's arm table, ADR Context/D-Z/D-AB, and Plan Task 2. It did **not** reach
Plan Task 7 — the task that *writes* AC-5 — nor four other sites.

### Evidence

Seven surviving twins, all outside a quoted-history block:

| # | File:line | Text | Contradicts |
|---|---|---|---|
| 1 | `plans/029:554` | *"class member, fixed here **(8)**"* | Spec §6: **(9)** |
| 2 | `plans/029:562` | *"**safe (6 + `burst`)**"* | Spec §6: **(5 + `burst`)**; §2.1: 5 safe |
| 3 | `plans/029:576` | arm table: *"accepts, safe \| `WithMaxResponseBytes`, **`WithReplayBuffer`***" | Task 2 (same plan) rejects `1<<62` |
| 4 | `plans/029:583` | *"for `WithReplayBuffer` it additionally means one event round-trips through a live `SSEServer`"* | Spec §6: *"NO row needs a live `SSEServer` at all"* |
| 5 | `plans/029:633` | Out of scope 1: *"**`WithReplayBuffer`'s retention** — bounded and evicting; a resource decision under an explicit cap"* | verbatim deleted cause (c) |
| 6 | `specs/016:598` | §3.7.1, same sentence | §2.1 row: **DEFECTIVE — fixed here** |
| 7 | `specs/016:738` + `:965` | §4 *"`WithReplayBuffer`'s evicting ring — get a one-line comment … naming that property"*; §7 **Out:** *"`WithReplayBuffer`'s retention (§3.7.1)"* | §7 **In:** *"the 9 defective knobs of §2.1"* |

Row 4 is also self-contradicting **inside the spec**: §6 point 1 states *"After revision 5's BLOCKER-1 fix, NO
row needs a live `SSEServer` at all"* and then, eight lines later, defines *"its product is usable"* for
`WithReplayBuffer` as *"one event round-trips through a live `SSEServer`"*.

Revision 5's own guard finds all seven in one command:

```
$ grep -n "ReplayBuffer" docs/{specs/016,plans/029,adrs/0032}-*.md | grep -i "evict|retention|safe|usable|round-trip"
plans/029-sizing-option-bounds.md:576:  | accepts, safe | `WithMaxResponseBytes`, `WithReplayBuffer` | …
plans/029-sizing-option-bounds.md:583:  …for `WithReplayBuffer` it additionally means *"one event round-trips…
plans/029-sizing-option-bounds.md:633:1. **`WithReplayBuffer`'s retention** (Spec §3.7.1) — bounded and evicting…
specs/016-sizing-option-bounds.md:598:1. **`WithReplayBuffer`'s retention** (§2.1) — bounded and evicting…
specs/016-sizing-option-bounds.md:738:`WithReplayBuffer`'s evicting ring — get a one-line comment…
specs/016-sizing-option-bounds.md:965:**Out:** `WithReplayBuffer`'s retention (§3.7.1)…
```

### Why it matters

This is not stale prose — it is an **executable table in the gate task**. An implementer who runs Task 2 and
then Task 7 as written writes a conformance row asserting `NewConfig(WithReplayBuffer(1<<62))` **accepts**,
against a `NewConfig` the previous task just taught to reject it. Half 2 is red on arrival, and the natural
debugging move — "the table says safe, the plan's out-of-scope list says retention is out of scope, so back
out Task 2" — undoes the BLOCKER fix. The arithmetic even hides itself: 8 + 2 + 6 + `burst` = 17, the same
total as the correct 9 + 2 + 5 + `burst`.

Rows 5–7 additionally tell an implementer, in the **Scope** section of the spec and the **Out of scope**
section of the plan, that the knob this increment now fixes is out of scope.

### Recommended fix

Apply the seven edits, then **run the guard revision 5 wrote and paste its (empty) output into the revision
header**. A guard that is described but never executed is the vacuity failure this project already has a
stored lesson for. Concretely: after each fold-in, `grep -n '<key phrase>' docs/{specs,adrs,plans}/…` and
require zero hits outside a `>` quoted-history block — for BLOCKER-1's fold-in the key phrases are
`ReplayBuffer` ∩ {`safe`, `evict`, `retention`, `usable`}, and for the census, the literal strings
`fixed here (`, `safe (`, `deferred (`.

---

## MAJOR findings

| # | Finding | Evidence | Recommended fix |
|---|---|---|---|
| **M5-1** | **Retraction #2 was folded into ONE of the four sites round-4 M4-4 named — and the three that still carry the withdrawn number are the ADR's public rationale, the spec's ceiling table, and the plan's task text.** M4-4's fix instruction was explicit: *"state the protocol beside the fixture in §1.4, **§3.4 and D-Z**. Use **31.0 MiB** at the ceiling."* §1.4 was re-measured correctly. **Spec §3.4's `WithCompletionSize` row, ADR D-Z's `WithCompletionSize` row, and Plan Task 4's cost table all still print the un-GC'd 67.8 MiB** (and Task 4 still prints the whole un-GC'd live column, 3.2 / 13.8 / 67.8). D-Z's rationale is the text CLAUDE.md's documentation contract sends to the **public godoc**, and Task 4's table is what the implementer reads. This is round-4 M4-1's shape — a fold-in short of the normative artifact — applied to the very retraction whose header announces it in all three files, and it is one `grep -n '67\.8'` from being caught. | `specs/016:528`, `adrs/0032:192`, `plans/029:450-452` all carry `67.8 MiB live` / `3.2` / `13.8`. Spec `:194` (28.7 at 60,000) and `:195` (31.0 at 65,536) are the corrected values in the **same document**. Re-measured (fixture + protocol as §1.4 states): `live(GC'd)= 2.09 / 8.13 / 29.99 / 32.52 MiB` at `1<<12` / `1<<14` / 60,000 / `1<<16`. | Replace `67.8` with `31.0` and `3.2 / 13.8` with `2.0 / 7.8` in all three sites, and add the one-clause protocol note (*"GC'd `HeapAlloc` delta, `KeepAlive`d"*) beside each, as M4-4 asked. Then `grep -n '67\.8\|13\.8\|3\.2 MiB' docs/{specs/016,adrs/0032,plans/029}-*.md` must return only the retraction headers. |
| **M5-2** | **"AC-4's protocol" names the opposite of what AC-4 says, in the one place the figure reaches a public godoc.** Plan Task 2 instructs: *"Re-measure the per-event cost **under AC-4's protocol (GC before read, `HeapAlloc` delta, `KeepAlive`)** and put the figure in `WithReplayBuffer`'s godoc"*, and Spec §3.4's `WithReplayBuffer` row repeats *"Task 2 re-measures under AC-4's protocol"*. **AC-4's three stated conditions are: no `t.Parallel()`; `KeepAlive`; and read `TotalAlloc`, NOT `HeapAlloc`.** They are a *cumulative-allocation* protocol for a construction-time delta; §1.4's is a *retention* protocol for a growth figure. They are not the same protocol and cannot both be "AC-4's". Spec §1.4 additionally asserts *"AC-4 already imposes exactly these three conditions on the tests"* — **false**, AC-4 forbids the statistic §1.4 mandates. An implementer following Task 2 literally reads `TotalAlloc` for a retention number and publishes a per-event cost inflated by churn: precisely the M4-4 defect, re-created by the fix for M4-4. | Spec AC-4 `:861-863`: *"it must read **`TotalAlloc`**, not `HeapAlloc` — cumulative and GC-independent"*. Spec §1.4 `:197-204` mandates `runtime.GC()` + `HeapAlloc` for live. Plan Task 2 `:341-343`. Measured both ways for the same fixture (1 KiB payload, ids auto-assigned, 20,000 `Send`s, no connections): `TotalAlloc` delta **78.4 MiB** vs GC'd `HeapAlloc` delta **23.47 MiB** — a 3.3× difference on the figure headed for the godoc. | Name the two protocols separately and stop calling both "AC-4's". Give the retention protocol its own label in §1.4 (e.g. *"protocol R: `GC` before both reads, `HeapAlloc` for live, `TotalAlloc` for cumulative, `KeepAlive`"*), have Task 2 and §3.4 cite **that**, and delete §1.4's false claim that AC-4 already imposes it. |
| **M5-3** | **Task 2's mandatory `WithReplayBuffer(8)` case is not observable as written — the fifth consecutive round with an AC whose text cannot become a running test.** Task 2 and the branch-coverage table both require *"`WithReplayBuffer(8)`: after 20 `Send`s **the ring holds 8**"*, and Task 2 adds *"this case needs **no connection at all** — it is cheaper than its two siblings"*. `*SSEServer` exports exactly three methods — `ServeHTTP`, `Close`, `Send` — and none exposes the ring. The **only** blackbox observation of ring contents is a `ServeHTTP` request carrying a `Last-Event-ID` header, which triggers `replayAfter` (`sse_server.go:213`). So the assertion needs a live server *and* a client, and the repo's `serveInBackground` helper **cannot supply it**: it builds its own `httptest.NewRequest(GET, "/events", nil)` internally, with no header hook. The "grows with no client" observation (true, `sse_server.go:429-431`) is about the *hazard*, not about the *assertion*. | Measured, `WithReplayBuffer(8)`, 20 `Send`s of `msgin.New` messages, then a `Last-Event-ID` request: after **ids[12]** → **7 frames replayed** (entries 13–19); after **ids[0]** (already evicted) → **0 frames**. Both prove the ring holds 8; neither is reachable without a connection. `grep -n '^func (s \*SSEServer) [A-Z]' adapter/http/sse_server.go` → `ServeHTTP:169`, `Close:345`, `Send:409`. `sse_server_test.go:180-189` shows the helper's hard-coded request. | State the case completely, the way M3-1 and M4-3 were fixed: *"20 `Send`s, then `ServeHTTP` a request carrying `Last-Event-ID: <id of send #13>` and assert 7 replayed frames (and `Last-Event-ID: <id of send #1>` ⇒ 0, proving eviction)"*, and note that `serveInBackground` must be extended to take a `*http.Request` (or a header) before it can serve this case. If instead the intent is the retention assertion (*"17 KiB regardless"*), say so and give it §1.4's protocol — but then it is a memory assertion, not a "ring holds 8" assertion. |
| **M5-4** | **Plan Task 7 still carries the retraction revision 5's own header withdraws, 550 lines after withdrawing it.** Task 7's `WithCompletionSize` bullet reads: *"the bare call's error is `msgin: channel store is nil`, **not round 2's `msgin: aggregator output channel is nil` — do not paste that string into an assertion**"*, and sizes the fixture as *"a `msgin.MessageGroupStore` **and an aggregation `fn`**, both positional"* — i.e. the two-part fixture M4-3 proved does not construct. Spec §6.2 and Plan Task 4 both carry the corrected five-part fixture and the explicit *"two prior corrections are themselves retracted"* block. Task 7 is the task that writes the AC-5 row, so the implementer who needs the fixture reads the withdrawn version. | `plans/029:585-588` vs `plans/029:424-441` (Task 4, correct) and `specs/016:928-948` (§6.2, correct). Measured: **both** strings are real and consecutive — `NewAggregator(nil, fn)` → `msgin: channel store is nil`; `NewAggregator(store, fn)` → `msgin: aggregator output channel is nil`. | Replace Task 7's bullet with a pointer to Task 4's five-part fixture, and delete the *"do not paste that string"* sentence. Same `grep` guard: `grep -n 'aggregator output channel is nil' docs/…` must show only quoted-history hits and the two correct fixture statements. |
| **M5-5** | **Task 8's coverage gate is red on arrival and its number is not this repo's number.** Task 8 requires *"Coverage `-coverpkg=./...` ≥ the **93.9%** baseline"*. Run as written, `go test ./... -coverpkg=./...` prints **eleven** per-package figures spanning **1.4%–42.4%** — every one below 93.9%, because `-coverpkg=./...` re-bases each package's tests against all 11 packages. The plain `go test ./... -cover` figures span **51.8%–100%**, and **93.9% is exactly `adapter/database/sql`'s** — a package this increment does not touch. There is no whole-tree 93.9%. Round 4 carried this as *suspected, not proven*; it is now measured. This is the shape Task 0 exists to prevent (*"Plan 028's round-3 audit produced a BLOCKER because a later task gated on a baseline that was already red on arrival"*), sitting in the delivery gate itself. Related: **`adapter/memory` is at 73.3%** today — below CLAUDE.md's ~85% target — and Tasks 3 and 5 both change it, with no bullet acknowledging the starting point. | ```$ go test ./... -cover``` → `msgin 95.6% · cron 51.8% · database/sql 93.9% · http 100.0% · http/stdlib 100.0% · memory 73.3% · channel 100.0% · endpoint 99.5% · resilience 99.1% · routing 100.0% · transform 100.0%`. ```$ go test ./... -coverpkg=./... -cover``` → `msgin 15.7% · cron 11.3% · database/sql 12.1% · http 42.4% · http/stdlib 11.2% · memory 5.5% · channel 14.6% · endpoint 33.1% · resilience 3.9% · routing 12.8% · transform 1.4%`. | State the gate **per changed package** against the plain `-cover` number measured in Task 0, not against one repo-wide constant: `endpoint 99.5%`, `routing 100.0%`, `adapter/http 100.0%`, `adapter/memory 73.3%`, root `95.6%`. Keep `-coverpkg=./...` only as the cross-package attribution check the project's stored lesson calls for, with its own (much lower) expected values. Add a bullet noting `adapter/memory` starts below the 85% target so the increment does not get blamed for it — or budget the tests to raise it. |

---

## MINOR findings

| # | Finding | Evidence | Recommended fix |
|---|---|---|---|
| **m5-1** | **The §3.9 → §3.10 renumber left three dangling cross-references and did not fix the ordering it was meant to fix.** Round-4 m4-8 offered *"swap them, or renumber §3.9 → §3.10"*; revision 5 renumbered without moving, so the spec now reads §3.7 → **§3.10** → §3.8, which is *more* jarring than before. Three references still point at the retired `§3.9`. | `specs/016` headings: `:596 ### 3.7`, `:639 ### 3.10`, `:671 ### 3.8`. Dangling: `specs/016:542` (*"revision 4's `WithCompletionSize`, §3.9"*), `adrs/0032:81`, `adrs/0032:192`. | Move §3.10 after §3.8 (or renumber §3.8 → §3.9 and keep file order), and fix the three `§3.9` citations. |
| **m5-2** | **Two more stale seven/eight-knob counts, the class round-4 M4-5 was about.** Plan `:238` — *"Revision 2 demanded … for **all 7** (now **8**)"* — should be 9; Plan `:229` branch-coverage row says the lower arm is *"already covered"* for *"8 of 9"*, which is right, so the two rows disagree. | `plans/029:238`, `:229`. Spec's counterpart `:779` correctly reads *"for all 7 (now **9**…)"*. | `(now 8)` → `(now 9)`. Re-run M4-5's adjudication grep over the plan only — the spec and ADR are clean. |
| **m5-3** | **The new `ErrInvalidReplayBuffer` row in §3.5's genericisation table is the only one of six without a line number**, in a table whose whole purpose is to be re-derived before citing. | `specs/016:570` — *"`msghttp.ErrInvalidReplayBuffer` \| `adapter/http/errors.go` \|"* vs its five siblings (`errors.go:51`, `:144`, `:258`, `http/errors.go:145`, `:150`). Ground truth: `adapter/http/errors.go:162`. | `adapter/http/errors.go` → `adapter/http/errors.go:162`. |
| **m5-4** | **Task 2's R1-e mutant is not mechanically defined and is not the mutant that targets R1-e's actual risk.** *"Convert R1-e's flattened gate to an `else if` → the unset-default path breaks (unset must leave replay OFF, not defaulted)"* — there is no preceding `if` to attach an `else` to and **no `defaultReplayBuffer` constant to assign**, so the implementer has to invent both before the mutant compiles. The risk R1-e actually carries is losing the `set &&` guard (which would reject every `NewConfig` that never set the option), and that mutant is one deletion. | `plans/029:353-354`; `adapter/http/options.go:1174` has no sibling default and `grep -n 'defaultReplayBuffer' adapter/http/` returns nothing. | Restate as: *"delete the `cfg.replayBufferSet &&` conjunct → every `NewConfig` without `WithReplayBuffer` must fail"*. That is compilable, one edit, and targets the assertion. |
| **m5-5** | **Task 8's audit-record count will be wrong on arrival.** *"the audit-round records are satellites (there are **four**: `029-audit-round-1..4`)"* — this report makes five, and Task 8's own instruction two lines above is *"re-derive them, do not increment the printed numbers"*. | `plans/029:625`. Re-derived now: `ls docs/specs/[0-9]*.md \| wc -l` → **16**; `ls docs/adrs/[0-9]*.md \| wc -l` → **31**; distinct plan numbers → **29**; plan files → **44**. | Delete the parenthetical count and rely on the re-derivation command already in the bullet. The spec/ADR/plan totals it quotes (16 / 31 / 29) all verify. |
| **m5-6** | **`1 << 16` bounds `WithReplayBuffer`'s COUNT, not its footprint, and D-W's "far below the unrecoverable band" cannot be shown for this row.** Every other ceiling is justified by `n × elemSize` (48 MiB, 1.5 MiB, 24.0 MiB). Here the element is a caller-supplied frame with no library bound short of `WithMaxEventBytes`' 1 MiB default, so the ceiling permits ~64 GiB of retention in the worst legal case. §3.4 and D-Z *do* say the cost is not a library constant, so this is disclosed — but §3.4's framing sentence (*"sized … far below the unrecoverable band of §1.1"*) is then untrue for this one row. | Measured, 1 KiB payload: 1.20 KiB/event (23.47 MiB @ 20,000; 93.84 MiB @ 80,000 — round 4's figures reproduce exactly). At a 1 MiB frame the same ceiling is ~64 GiB. | One clause in §3.4/D-Z: the ceilings bound **counts**; the byte product of a count ceiling and a caller-sized element is the operator's concern, the same argument §3.4 already makes for the `WithMaxConnections × WithConnectionBuffer` product. |
| **m5-7** | **The `WithReplayBuffer(8)` line sits inside §1.4's `WithCompletionSize` evidence block**, where it reads as evidence for the aggregator finding rather than for §1.5. | `specs/016:179`, inside the §1.4 fenced block. | Move it to §1.5, or label it *"(§1.5, for comparison)"*. |
| **m5-8** | **Spec §3.6 reason 4 still says `ErrInvalidCapacity` will mean "out of range" at *three* producers.** *"`ErrInvalidCapacity` comes to mean 'out of range' at **all three** producers instead of '≤ 0' at two and '> ceiling' at one"* — since revision 4 there are **four** (§3.5, §3.10, D-X, Task 4 all say four). Last surviving instance of round-1 M-1's arithmetic. | `specs/016:591-592` vs `:542`, `:667`, `adrs/0032:142-144`, `plans/029:456`. | `three` → `four`, `two` → `three`. |

---

## Verified-negative (attacked, found sound)

Everything below was attacked with a command and **held**. This list is as important as the findings.

| Attacked | Result |
|---|---|
| **The AST census — 17 functions / 27 methods, and the 17 keys.** | **Sound.** My own `go/ast` walk from the repo root, all 8 modules, non-test files: `EXPORTED FUNCTIONS (Recv==nil) with int/int64 param in ANY position: 17`; `EXPORTED METHODS: 27`. The 17 names match §2.1's 17 rows **exactly** (including `NewTokenBucket`), at the exact lines the bundle cites. The `grep` census still returns **16**. |
| **Every one of the SIX code shapes, read at its own site.** | **Sound, and there is no seventh among the nine.** R1-a ×3 (`consumer.go:270-274`, `options.go:1156-1160`, `:1162-1166`), R1-b ×2 (`consumer.go:262` single arm with the literal at `:251`; `groupstore.go:91` with the literal at `:84`), R1-c (`queuestore.go:97-103`, nested), R1-d (`aggregator.go:132-135`, closure only; `aggregatorConfig` at `:14-21` has no `completionSize`), R1-e (`options.go:1174`, flattened, no default), R2 (`memory.go:38-45`, with `b.ch` initialised at `:59` and the inline latch at `:60-64`). 3+2+1+1+1+1 = 9. Every shape is pasted verbatim in the plan, as it claims. |
| **R1-e's prescribed edit, and the unset-means-OFF path.** | **Sound.** Extending the same condition to `cfg.replayBufferSet && (cfg.replayBuffer < 1 \|\| cfg.replayBuffer > replayBufferCeiling)` compiles and leaves the unset path untouched: `replaySize()` returns the zero `replayBuffer`, and both consumers gate on `> 0` (`sse_server.go:213`, `:429`). Warning against pattern-matching it onto R1-a is correct — there is no `if !set` arm and no default constant to assign. |
| **`WithReplayBuffer` really is a class member, and round 4's retention figures reproduce.** | **Sound to two decimal places.** Fixture: `msgin.New[any]` with a 1 KiB payload, ids auto-assigned, **no registered connections**; protocol: `runtime.GC()` before both reads, `HeapAlloc` delta for live, `KeepAlive(server)`. `WithReplayBuffer(1<<62)` → **23.47 MiB** at 20,000 `Send`s and **93.84 MiB** at 80,000 (round 4: 23.5 / 93.8; exactly 4× sends ⇒ 4.0× retention). `WithReplayBuffer(8)` → flat (live delta ≈ 0 across 20,000 sends). |
| **`msgin.New` assigns a non-empty id, so the ring fires with ordinary messages.** | **Sound — not a fixture gap.** `appendRing` runs only for `ev.ID != ""` (`sse_server.go:429`) and `ev.ID` is `msg.ID()` (`sse.go:148`); measured, `msgin.New[any](...)` yields e.g. `"c27bbde1bc591be286196afc8722746c"`. An implementer using ordinary messages gets the ring populated. |
| **§1.4's quadratic table under revision 5's own protocol.** | **Sound.** cumulative **206.8 / 3143.9 / 41475.2 / 49453.2 MiB** vs the spec's 206.7 / 3143.5 / 41474.0 / 49451.7 — within 0.01%; elapsed 49 ms / 641 ms / 7.27 s / **8.41 s** vs 50 ms / 644 ms / — / 8.6 s; live 2.09 / 8.13 / 29.99 / 32.52 vs 2.0 / 7.8 / 28.7 / 31.0. Both retractions in this table are correct. |
| **The five-part `WithCompletionSize` fixture and `released=3`.** | **Sound, executable exactly as written.** `NewAggregator(store, fn, WithOutputChannel(ch), WithCorrelationStrategy(fixedKey), WithCompletionSize(2))` + `ch.Subscribe(counter)` → `released=3` over 6 messages. The four-line gate order in §6.2/Task 4 reproduces verbatim, including the `Handle` correlation-key error. *(Two API details worth pre-empting in the task: `memory.NewGroupStore()` returns `(*GroupStore, error)`, and `Subscribe` takes a `msgin.MessageHandler` — a bare `func` needs `msgin.HandlerFunc`.)* |
| **Retraction #1 — `NewConfig` rejects an explicit non-positive byte cap.** | **Sound.** Measured for `WithMaxBodyBytes(-1/0)` and `WithMaxEventBytes(-1)`; `options.go:1128-1131` is the validator and `options.go:232-234`'s comment says the `maxBody()` back-fill only fires for a hand-built `*Config`. The reshaped follow-up question (*"should an explicit off-state exist, and which sentinel value carries it"*) is the right one. |
| **The single error shape on the 9th knob: render, `errors.Is`, `IsPermanent`.** | **Sound.** R1 renders truthfully at **both** ends (`… 0 not in [1, 65536]`, `… 65537 not in [1, 65536]`), `errors.Is(err, msghttp.ErrInvalidReplayBuffer)` **true**, `IsPermanent` **false**. R2 through `msgin.Permanent`: `errors.Is(err, msgin.ErrInvalidCapacity)` **true**, `IsPermanent` **true**. The rejection of `"%d exceeds %d"` remains correct. |
| **`ErrInvalidCapacity`'s four producers across four units, and the six-row genericisation table.** | **Sound.** The nine knobs use exactly six distinct sentinels (`ErrInvalidCapacity` fronting four of them), so §3.5's *"six rows for nine knobs"* is right and m4-4's arithmetic is finally closed. `ErrInvalidReplayBuffer` exists at `adapter/http/errors.go:162` with the quoted text. |
| **Safe causes (a) and (b) on the rows that claim them, and `burst`.** | **Sound.** (a) `breaker.go:164 b.fails >= b.threshold` — a scalar `int`. (b) `consumer.go:1199 len(b) > c.maxPayloadBytes` — `b` already materialised. `burst` → `float64(burst)` with `ratelimit.go:43` rejecting `burst < 1`. `WithSuccessStatus(1<<62)` rejects with the spec's exact string. **Only cause (d) fails** — BLOCKER-1. |
| **`WithPollMaxBatch` is derivatively safe, and the promised note is now written and true.** | **Sound.** `pollMaxBatch` appears in exactly one non-config site — `poller.go:36 for held < c.pollMaxBatch && gate.tryAcquire()` — so `held ≤ min(pollMaxBatch, free credits)` and free credits are `maxInFlight`. The row's safety genuinely is created by Task 1, and the warning to revisit it if the credit gate is decoupled is correct. |
| **D-AB's method criterion ⇒ exactly two members, both root-constructible.** | **Sound, re-derived from the rule.** Of the 27 methods only `memory.QueueStore.Claim` (`min(max, len(s.ready))`) and `channel.QueueChannel.Poll` (delegates) have the parameter size a `make`. `sql.Source.Poll`, `sql.QueueStore.Claim` and the three `…GroupDialect.ExpiredGroups` size on `len(rows)`/`len(cands)`; `ExponentialBackoff.Delay` and `responseTracker.WriteHeader` fall out. |
| **Deleting cause (c) leaves no row unjustified.** | **Sound**, with the BLOCKER-1 exception. (c) had exactly one member and it is now a class member; (a) covers `WithPollMaxBatch`, `WithBreakerThreshold`, `WithSuccessStatus`, `burst`; (b) covers `WithMaxPayloadBytes`. After BLOCKER-1, **(d) is left with no member** — see the recommended fix. |
| **`1 << 16` for `WithReplayBuffer` as a COUNT ceiling.** | **Defensible.** 65,536 retained events is ~3–4 orders above any plausible reconnect window, matches its two `msghttp` siblings, and the raise-is-safe/lower-is-breaking asymmetry favours it. The *footprint* caveat is m5-6, and §3.4/D-Z already disclose that the byte cost is caller-sized. |
| **`make(chan struct{}, 1<<62)` — AC-4's direct half.** | **Sound.** `cap=4611686018427387904`. Executable exactly as written. |
| **Traceability and relative links.** | **Sound.** Docs-link gate arm 1 over the three bundle files: **zero** broken links. Spec↔ADR↔plan cross-links all present and bidirectional; every commit-trailer requirement (`Spec: 016`, `Plan: 029`, `ADR: 0032`) is stated. The artifact counts Task 8 quotes re-derive: specs **16**, ADR files **31**, distinct plan numbers **29**. |
| **CLAUDE.md compliance on the testable axes.** | **Sound.** Blackbox-only and assert-closure tables in Global constraints 1–2 and restated per task; no new exported symbol and no signature change (all nine ceilings unexported, the one new field inside the already-unexported `aggregatorConfig`); the multi-instance statement is present, correct and non-vacuous (§7); the hot-path branch enumeration is a table covering every new arm; Task 8 is 8 modules × 8 CI steps with the `harness`/`go vet` and Docker caveats; the Go-skills header and the `table-test`/`use-mockgen`/`use-testcontainers` overrides are all restated, as the writing-plans override requires. |
| **Mutation coverage, per assertion.** | **Sound except one.** Tasks 1, 3, 4, 5 and 6 each list mutants that target *distinct* assertions, including AC-3's mandatory *"move the `return` inside the latch"* and AC-4's `credit.go:21` mutant. Global constraint 5's carve-out (guard-deletion for AC-1 only) is correctly reasoned. The exception is Task 2's new R1-e mutant — m5-4. |

---

## Round-4 fix verification

One row per round-4 finding, checked in **all three files**. **LANDED** = fixed everywhere it appeared.
**LANDED-BUT-FLAWED** = fixed, but incompletely or with a new defect. **NOT LANDED** = unchanged.
**REGRESSED** = made worse.

| # | Round-4 finding | Verdict | Why (all three files checked) |
|---|---|---|---|
| **BLOCKER-1** | `WithReplayBuffer` is a class member under D-AB and is certified *safe (c)* | **LANDED-BUT-FLAWED** | The substance landed and landed well: §1.5 is a new, correct, measured section; §2.1's row is **DEFECTIVE — fixed here**; cause (c) is **deleted** with a tombstone; the ceiling is in §3.4 and D-Z; `ErrInvalidReplayBuffer` joins §3.5's table; R1-e is a correctly-read sixth shape; Task 2 owns it. **But the fold-in stopped one file short of the gate task and left six other twins** — Plan Task 7's arm table still files it *safe*, and both Scope sections still call its retention out of scope. See **BLOCKER-2**. |
| **M4-1** | ADR missed three of revision 4's own fold-ins (+ `memory.go:58`) | **LANDED** | All four fixed in the ADR: D-AA half 2 is now phrased over the **surface** (`:216-219`), the `time.Duration` bullet now says *"Do NOT claim … currently safe, checked"* (`:265-271`), D-X says **FOUR producers** (`:142`), D-Y cites `memory.go:59` (`:176`). This is the first round in which the ADR received every fold-in aimed at it. *(The **procedural** half of the fix — the cross-file grep — was written into the revision procedure but demonstrably not executed; that is BLOCKER-2, M5-1 and M5-4.)* |
| **M4-2** | `WithMaxBodyBytes(-1)` does not mean "use 1 MiB" | **LANDED** | Retracted in all three headers; §3.8 and D-AB both replaced with the measured truth and the reshaped follow-up question; *"there is no off-state to document"* is gone. Re-measured: `NewConfig(WithMaxBodyBytes(-1))` → `msghttp: max body bytes must be > 0`. |
| **M4-3** | The `WithCompletionSize` fixture needs five parts, and round 2's error string is not "wrong" | **LANDED-BUT-FLAWED** | Spec §6.2 and Plan Task 4 both state the five-part fixture and both retract the mislabel, correctly and with the gate order pasted. **Plan Task 7 still carries the two-part fixture and the withdrawn *"do not paste that string"* instruction** — M5-4. |
| **M4-4** | §1.4's live column was read without a GC | **LANDED-BUT-FLAWED** | §1.4 is re-measured, the protocol is stated, and the rule is correctly generalised to *"fixture AND protocol"*. **But M4-4 named three sites (§1.4, §3.4, D-Z) and only §1.4 was fixed** — §3.4, D-Z **and** Plan Task 4's table still print 67.8 / 13.8 / 3.2 MiB (M5-1), and the protocol is then mis-named as "AC-4's" in the two places it is cited (M5-2). |
| **M4-5** | Ten stale seven-knob counts | **LANDED-BUT-FLAWED** | All ten fixed and verified individually (spec `:770`, `:812`, `:955`, `:962`, `:463`; ADR `:111`; plan `:264`, `:283`, `:96`). **Three new instances of the same class were introduced by the ninth knob** — plan `:238` *"(now 8)"*, `:554` *"fixed here (8)"*, `:562` *"safe (6 + burst)"* — which is why the recommended adjudication grep needs to be re-run *after* every census move, not once. |
| **m4-1** | Spec §3.6 cites "Task 3" for the `WithBuffer` rewrite | **LANDED** | `specs/016:589` now reads *"a closure **Task 5** already rewrites"*. |
| **m4-2** | Plan Task 5 cites `memory.go:34-37` | **LANDED** | `plans/029:478` now `:35-37`, matching Spec §4 `:742`. Verified against source. |
| **m4-3** | ADR D-Y cites `memory.go:58` | **LANDED** | `adrs/0032:176` now `:59`. `sed -n '59p'` → `b := &Broker{ch: make(chan msgin.Message[any])}`. |
| **m4-4** | *"All six sentinel messages"* over a five-row table | **LANDED** | Now genuinely six: `ErrInvalidReplayBuffer` is the sixth row in §3.5 and is named in D-X's enumeration. The parenthetical *"six rows for nine knobs"* is correct. *(Only the row's line number is missing — m5-3.)* |
| **m4-5** | Dangling *"see note"* on `WithPollMaxBatch` | **LANDED** | The note is written (§2.1, after the table), states the derivation from `poller.go:34-36` + `consumer.go:385`, and says explicitly that the row's safety is **created by this increment**. Verified in source. |
| **m4-6** | Task 3's producer arithmetic wrong at that point in the sequence | **LANDED** | Task 3 now says *"write the godoc for the END STATE of four producers … do **not** write 'three' here"* and cross-references Tasks 4 and 5. |
| **m4-7** | Backlog *"items 2–5"* should be *"2–6"* | **LANDED** | Both sites (`plans/029:620`, `:637`). `docs/HANDOVER.md` §8 does have six items, item 6 being the deferred byte class. |
| **m4-8** | Section order: §3.9 printed before §3.8 | **LANDED-BUT-FLAWED** | Revision 5 took the *renumber* option (§3.9 → §3.10) **without reordering**, so the file now reads §3.7 → §3.10 → §3.8, and three references to the retired `§3.9` dangle (spec `:542`, ADR `:81`, `:192`). m5-1. |
| **m4-9** | The inert-`n` over-rejection was undecided and undocumented | **LANDED** | Decided on the record in Spec §3.10's closing paragraph, with a Task 4 bullet requiring a table case **and** a godoc sentence. The decision (fail loud) is defensible and is now traceable. |
| **m4-10** | `ErrInvalidCapacity` spans four **units**, not four ranges | **LANDED** | Stated in Spec §3.5 and ADR D-X, both with the rendered example and the *"a fifth producer should be a conscious decision"* clause. |

**Totals (16 findings): 11 LANDED · 5 LANDED-BUT-FLAWED · 0 NOT LANDED · 0 REGRESSED.**

**Trend across rounds.** 1→2: 10 of 19 unclean, 2 regressions. 2→3: 5 of 17, 0. 3→4: 6 of 12, 0. **4→5: 5 of
16 unclean (31%), 0 regressions** — the best ratio since round 2, and the ADR is clean for the first time. The
residual has a single, stable shape: **every one of the five unclean rows is a fold-in that reached some but
not all of the places the finding named**, and in three of the five (M4-4, M4-3, BLOCKER-1) the missing place
is the *plan task that has to execute it*. The failure mode has migrated from the ADR to the plan's later
tasks; the countermeasure (grep every fold-in across all three files) is already written and simply has to be
run.

---

## Carried into round 6 — suspected, not proven

- **Whether cause (d) should be deleted like (c).** If BLOCKER-1 is fixed by deferral, (d) has no member. A
  cause list where two of four letters are tombstones is worth re-examining as a whole rather than patching
  again; the honest end-state may be *"a knob is safe iff (a) or (b)"*, with everything else either fixed or
  explicitly deferred. A design call, not settled here.
- **Whether `WithMaxEventBytes` belongs in the same deferral or is worse.** `sse.go:384-389` appends the line to
  `dataBuf` and *then* tests the cap, so the buffer always overshoots by one line — carried by round 4 and
  still unadjudicated. Not a finding against this bundle, but the deferred increment inherits three knobs with
  three different overshoot semantics.
- **Peak RSS of the whole `go test ./...` run.** Still unmeasurable — the code does not exist. Component
  figures re-confirmed or carried: Task 1 ≈ 48 MiB + ~257 MiB of goroutine stack under `-race`, Task 2 ≈ 1.5
  MiB + the replay case, Task 4 ≈ 0 under the growth split, Task 6 ≈ 48 MiB. Go runs packages in parallel, so
  the peak is a sum; Task 8 must measure it.
- **Whether the AC-5 gate should key on the `int`/`int64` type name at all.** The `time.Duration` class is
  excluded "by construction" precisely because `go/ast` sees a different identifier — which is a property of
  the recognizer, not of the hazard. Correctly stated as deliberate; whether it is *right* belongs to the
  `time.Duration` increment.
- **Whether any non-`With…`, non-`New…` exported function will ever carry a sizing parameter.** Half 1's
  `Recv == nil` boundary is sound today because all 17 keys are constructor-shaped. The first exported *method*
  that sizes a `make` outside `memory`/`channel` breaks the "no uncovered residue" claim rather than the gate.

---

## Auditor's method note

**Re-derived first-hand this round** (every figure above is my own command output; nothing is transcribed from
the bundle or from rounds 1–4):

- The `go/ast` census — 17 exported functions with an `int`/`int64` parameter in any position, with names and
  lines, plus the 27 methods. Written from scratch in a throwaway module.
- The `grep` census (16 `With…` options), run verbatim as §2 prints it.
- **D-AB re-derived against all 17 rows, from source, one row at a time** — which is how BLOCKER-1 was found.
  Every `cfg.maxResponseBytes`, `c.maxPayloadBytes`, `b.threshold`, `c.pollMaxBatch`, `burst` and
  `successStatus` consumer was read, not just the one the verdict column cites.
- **`WithMaxResponseBytes`'s unbounded `io.ReadAll`** — the BLOCKER — against a 64 MiB `httptest` reply, at
  three configurations, with fixture and protocol stated.
- **`WithReplayBuffer`** — `NewConfig` acceptance at `1<<62` and rejection at `0`; retention at `n=8` and
  `n=1<<62` for 20,000 and 80,000 `Send`s under the GC/`KeepAlive` protocol; and the **replay-observability
  probe** (`Last-Event-ID` after entry 13 ⇒ 7 frames; after the evicted entry 1 ⇒ 0 frames) that produced M5-3.
- The full `NewAggregator` gate chain (nil store → store+fn → +`WithOutputChannel` → +`WithCompletionSize` →
  `Handle` without a correlation key), the five-part fixture, and `released=3`.
- The `WithCompletionSize` quadratic table at four sizes under §1.4's stated protocol (which is how M5-1's
  scope was established).
- `NewConfig` behaviour for `WithMaxBodyBytes(-1/0)`, `WithSuccessStatus(1<<62)`, `WithReplayBuffer(0/1<<62)`.
- The error-shape matrix at both ends for the 9th knob (R1) and for R2: render, `errors.Is`, `IsPermanent`.
- **All nine code shapes**, read at their own sites, plus `sse_server.go:169/213/345/409/429/466-476`,
  `exchange.go:118-160`, `outbound.go:340-412`, `encode.go:102`, `sse.go:137-153`, `poller.go:20-60`,
  `consumer.go:248-300`, `queuestore.go:94-112`, `groupstore.go:82-96`, `memory.go:34-66`,
  `options.go:1120-1190`, `adapter/http/errors.go`, `sse_server_test.go:175-190`.
- The repo-wide coverage matrix, both plain and `-coverpkg=./...` (M5-5), and the four CLAUDE.md artifact-count
  commands.
- Docs-link gate arm 1 over the three bundle files.

**Spot-checked rather than fully re-run** (verified first-hand by earlier rounds, untouched by revision 5):

- **§1.1's two-regime `makechan` table** and the `GOMEMLIMIT` result. Not re-run — the `2^41` row kills the
  process by design and rounds 1–4 each reproduced it. I re-confirmed only the zero-size-element corollary
  `make(chan struct{}, 1<<62)` (`cap=4611686018427387904`), which AC-4 depends on.
- **§1.2's `int32(n) < 0` band.** Not re-run this round; rounds 3 and 4 reproduced all nine rows verbatim and
  revision 5 did not touch the table. I re-read `consumer.go:457-459` and `:330` to confirm the citation.
- **The four original panic reproductions** through `Consumer.Run` / `ServeHTTP` / `memory.New`. Not re-run;
  identical in rounds 1–4 and unchanged by revision 5.
- **The 48 MiB / 1.5 MiB / 24.0 MiB / ~257 MiB ceiling costs and the AC-4 allocation-delta series.** Not
  re-run; rounds 3 and 4 measured them and revision 5 did not change them.
- **The `WithMaxGroups` / `WithCapacity` growth matrix** in Plan Task 3. Not re-run; round 4 reproduced all
  eight numbers under `-race` and revision 5 did not touch them.

**Hygiene.** Every probe lived outside the repository in a throwaway module
(`…/scratchpad/probe`, `replace github.com/kartaladev/msgin => /Users/zakyalvan/Documents/RND/msgin`), with
sub-packages `scan`, `maxresp`, `replay`, `agg`, `shape`. **No scratch `.go` file was ever written into the
repo**; all probe files were deleted after the run. `git status --short` after the probes shows exactly the
expected **9** documentation files — 2 modified (`docs/HANDOVER.md`, `docs/adrs/0031-nil-option-elements.md`)
and 7 untracked (the 3 bundle files, the 4 prior audit records) — **plus this report**. No repository file was
modified except this one.

---

**VERDICT: NOT SAFE TO IMPLEMENT.** 2 BLOCKERs, 5 MAJORs, 8 MINORs.

**Is the bundle implementable as written? No — but it is one row and one grep from being so, and the design
itself is not in question.** D-W/D-X/D-Y/D-Z/D-AA/D-AB are sound; the nine code shapes are read correctly; the
error shape, the ceilings, the branch table, the mutation strategy and the CLAUDE.md compliance all hold. What
fails is the same *bookkeeping* failure for the fifth round — and this round it fails in a way that would ship
**a red gate** (BLOCKER-2) and **a false safety certificate** (BLOCKER-1), which is why neither can be waived.

I do **not** think round 6 should be another full audit. The two BLOCKERs and five MAJORs are all mechanical:
one row reclassified into an arm that already exists, one protocol renamed, one fixture sentence completed,
one coverage number re-derived, and a grep sweep whose command is already written into the revision procedure.
**Recommended: fix them, then run a targeted verification pass** — re-derive §2.1 row-by-row from D-AB, run the
cross-file grep for every fold-in and paste its empty output into the header, and re-check the five specific
sites this report names. If that pass is clean, implement. A sixth full adversarial round on an otherwise
sound design would be ceremony, not rigor.
