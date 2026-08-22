# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then this file. **Trust `git log` and the tree over this document.** Every
> count below was measured when written; **re-derive before relying on one** — that has failed in fourteen
> consecutive handovers, including several inside the session that wrote this one.
>
> ### ✅ PLAN 029 IS DELIVERED, MERGED AND PUSHED. `origin/main` = `5dbbf1d`. Nothing is in flight.
> ### Next: pick a backlog item from §6. Item 7 is new and the most substantive.
>
> | | State |
> |---|---|
> | Branch | **`main`**, clean. `fix/sizing-option-bounds` is merged and still exists locally — see §5 |
> | `main` | **`5dbbf1d`** — merged `--no-ff` and pushed; `git ls-remote origin main` confirms |
> | Working tree | **clean** |
> | Suite | **11/11 root packages green** under `-race -shuffle=on` at `5dbbf1d` |
> | Exported surface | **delta ZERO** vs the branch point — 442 decls, AST-diffed non-vacuously |
> | Tags | **zero, as always.** Do NOT propose tagging |

## 1. What Plan 029 delivered

**Nine exported sizing options gained a stated per-knob ceiling**, enforced *before* the hazard and reported
through the **existing** typed sentinel. Before this, each panicked, corrupted runtime state, or silently stopped
bounding what it exists to bound at a large `n`.

| Package | Knobs | Ceiling |
|---|---|---|
| `endpoint` | `WithMaxInFlight`, `WithConcurrency` | `1<<20`, `1<<16` |
| `msghttp` | `WithConnectionBuffer`, `WithMaxConnections`, `WithReplayBuffer` | `1<<16` each |
| `memory` | `WithCapacity`, `WithMaxGroups`, `WithBuffer` | `1<<20` each |
| `routing` | `WithCompletionSize` | `1<<16` |

- **Net exported-surface delta ZERO** — 442 declarations, identical to the branch point. Verified by AST diff
  **non-vacuously**: a first attempt compared 0 against 0 and was rejected rather than recorded as a pass.
- **Three *byte* knobs are class members with a DEFERRED remedy, NOT certified safe** — `WithMaxBodyBytes`,
  `WithMaxEventBytes`, `WithMaxResponseBytes`. CLAUDE.md's Sensible-defaults gate forbids guessing a byte cap.
  See §6 item 6.
- **A class gate** (`sizing_option_class_gate_test.go`, 19 executable rows in three arms) stops the class
  returning. It asserts a **key→arm mapping**, not counts — a pairwise swap defeats counts.

**Cost:** five adversarial design-audit rounds *before* any code (19 → 17 → 12 → 16 → 15 findings), then SDD
delivery in 9 tasks with per-task reviews, a whole-branch review and one fix wave. Audit records live at
`docs/plans/029-audit-round-{1..5}.md` — **immutable execution records, do not edit them.**

## 2. Governing artifacts

`docs/specs/016-sizing-option-bounds.md` (rev 6) · `docs/adrs/0032-sizing-option-bounds.md` (rev 6, decisions
**D-W**…**D-AB**) · `docs/plans/029-sizing-option-bounds.md` (rev 6, Tasks 0–8).

**The two load-bearing subtleties, if you touch this code:**

1. **D-Y — `memory.WithBuffer`'s `return` must stay OUTSIDE the `if b.err == nil` latch**
   (`adapter/memory/memory.go`). `New`'s apply loop `continue`s past a nil option (ADR 0031 D-U), so a later
   `WithBuffer(1<<62)` still runs when the latch is already taken. **Nesting the `return` compiles, reads
   naturally, passes every test except AC-3, and panics in production.** Three separate reviewers mutated it and
   confirmed AC-3 kills it with the real `makechan` panic.
2. **D-AB — class membership is a stated CRITERION, not a list:** *a knob is a class member iff `n` is the sole
   bound on an accumulation.* **Re-derive §2.1 row by row FROM the criterion; never read the verdict column.**
   Two of D-AB's four safety causes were emptied in consecutive revisions by rows whose stale "safe" verdict
   survived the criterion written to catch them — and **neither emptying changed the 16/17 totals**, so no
   count-check could have found either.

## 3. Accepted, with written rationale — do not "discover" these

- **`adapter/memory` coverage ~74%**, below CLAUDE.md's 85% target. **Pre-existing** (73.3% before Plan 029);
  every function Plan 029 touched is at 100%. Blackbox attribution — much of the package is exercised from
  sibling packages.
- **Peak RSS 2.27 GB under `-race`**, 2.06 GB in a single test — 8× the plan's estimate. Deliberately not
  "fixed": both remedies would stop the ceilings being exercised in CI at all, since CI runs only the `-race`
  pass. Fits `ubuntu-latest`. Recorded so a future OOM is diagnosed in one step rather than rediscovered.
- **`govulncheck` lives in `$(go env GOPATH)/bin`**, not on `PATH` — `which govulncheck` reports it missing while
  the binary exists. It was installed and is **clean on all 8 modules**.

## 4. Gotchas — these will bite

- **Verify the REMOTE, not the local ref:** `git ls-remote origin main`, never `git rev-parse origin/main`.
- **A gate that has never been proven to fire proves nothing** — and a gate whose rows all pass can still be
  decorative if the *classification* those rows carry is asserted nowhere. Both happened here, one revision apart.
- **A measurement is only as good as its fixture AND its protocol.** State both beside every figure: realistic
  `msgin.New` messages (never a zero value), `runtime.GC()` before the read, `TotalAlloc` (cumulative) vs
  `HeapAlloc` (retained) named explicitly, `KeepAlive` on the product. Conflating those two shipped a public
  godoc overstating live memory ~6×, caught only by the final whole-branch review.
- **Docs contradicting the code they describe recurred FOUR times in this plan.** Read prose against the
  constructor, not for plausibility.
- **An implementer subagent can invent scope and stall.** One dispatched its own out-of-scope security review,
  reported "completed", and had made no commit and written no report. Check the tree, the commit **and** the
  report independently rather than trusting a status.
- **The docs-link gate has two known false positives** — `docs/plans/m` and `docs/specs/factory(fireTime` are Go
  identifiers inside code spans, not links. Anything else is a blocker.
- **`GOTOOLCHAIN=go1.25.13`.** `harness` has no test files — `go test` there is a false pass; use `go vet`.
- **`.superpowers/` is git-ignored** (rule added by this increment) — SDD ledgers and briefs, not deliverables.
- Never commit `.claude/settings.json`.

## 5. Two housekeeping actions NOT taken — they need explicit approval

1. **`fix/sizing-option-bounds` still exists locally.** CLAUDE.md says delete a merged feature branch
   (`git branch -d fix/sizing-option-bounds`). It was **never pushed**, so there is no remote branch to delete.
   Branch deletion needs per-action approval, which is why it is still here.
2. **The SDD workspace `.superpowers/sdd/029-sizing-option-bounds/` still exists** — ledger, task briefs, reports
   and review packages. The git history is the durable record now, so `rm -rf` is safe whenever you want it gone.

## 6. Backlog

1. ~~The sizing-option class~~ — **DONE**, Plan 029, merged at `5dbbf1d`.
2. **Seven copies of the delegator pre-check loop** in `adapter/http` (×5) and `adapter/http/stdlib` (×2).
   A package-local helper collapses each to one line (~35 lines).
3. **The Plan 028 AST gate is syntactic, not a dominance proof.** Two contrived shapes defeat it; both named in
   the file header. Promoting it to a `go/analysis` analyzer was rejected as out of scope pre-v1.
4. **The `gin` increment** still needs a plan number, and its ADR is still a forward reference.
5. **Minor godoc wording class** — four sites say the apply loop is "this constructor's first statement" when a
   `cfg := …` initializer precedes it. Fix the class in one pass.
6. **The byte-ceiling class** — `msghttp.WithMaxBodyBytes`, `WithMaxEventBytes`, `WithMaxResponseBytes` are each
   the sole bound on a **remote-peer-driven** read into memory that is **retained** (`encode.go`
   `io.ReadAll(http.MaxBytesReader(…))`; `sse.go` a `bytes.Buffer`; `exchange.go`
   `io.ReadAll(io.LimitReader(resp.Body, max))` — note `drainBounded` is only 5 of that field's 6 reads).
   Measured: a 64 MiB body is rejected at the 1 MiB default and **fully read at `1<<62`**.
   **The open question is NOT "invent an off-state"** — `NewConfig` already **rejects** an explicit `n <= 0`
   (`ErrInvalidMaxBodyBytes`), and leaving the option unset already **is** the documented default state. It is
   *"should an explicit off-state exist at all, and which sentinel value carries it"* — a negative `n` is already
   taken by the rejection, so it would need a new sentinel value, not a reinterpretation.
7. **🆕 `routing.WithReleaseWhen` reaches the same unbounded per-group growth** that `WithCompletionSize`'s
   ceiling was added to stop — `WithReleaseWhen(func(g) bool { return len(g.Messages()) >= 1<<62 })` reproduces
   it exactly. Being **func-typed, it is structurally invisible to the class gate**. Verified as outside Spec
   016's stated class, so the shipped prose does **not** over-claim — but it needs its own spec/ADR. A
   one-sentence cross-reference on `WithCompletionSize`'s godoc would close the inference gap meanwhile.
8. **Minor, recorded not fixed:** `GOARCH=386 GOOS=linux go vet ./...` is clean on the pre-029 tree and now fails
   in 4 packages — `1 << 62` overflows on 32-bit, in **test files only**. No gate covers 386 and no 32-bit
   support is claimed. A `math.MaxInt`-style constant would fix it but would make the `EqualError` assertions
   architecture-dependent.
