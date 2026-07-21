# Plan 022 — Rename `HeaderID` → `HeaderMessageID` (value → `msgin.message-id`)

> **For agentic workers:** REQUIRED SUB-SKILL: use `superpowers:subagent-driven-development` (the project default) or
> `superpowers:executing-plans`. Steps use checkbox (`- [ ]`) syntax.
>
> **Go-skills hard rule (CLAUDE.md, restated because `writing-plans` omits it):** start from
> **`cc-skills-golang:golang-how-to`**; use **`gopls`** for the rename itself (this plan is *about* a semantic refactor —
> a text substitution is explicitly forbidden, see Task 1 Step 2); **`superpowers:test-driven-development`** governs the
> one test case this plan adds; the **`table-test`** override governs its shape. `use-mockgen` / `use-testcontainers` do
> not apply.

**Goal:** Rename the exported `HeaderID` constant to `HeaderMessageID` and its value from `"msgin.id"` to
`"msgin.message-id"`, before the repo's first tag makes either change expensive.

**Architecture:** A pure mechanical refactor. The Go identifier is renamed semantically with `gopls`; the 8 references
that live in *comment prose* (which `gopls` does not touch) are corrected by hand; the value change is a one-line edit
whose only behavioural surface is the reserved-namespace strip, which gets a new covering test.

**Tech stack:** Go 1.25, `gopls`. No dependency change, no new package.

**Traceability:** Implements [ADR 0026](../adrs/0026-header-message-id-rename.md). Amends the header vocabulary of
[Spec 001](../specs/001-messaging-core.md).

> **Why this is its own increment.** It was originally folded into the producer-retry/HTTP increment as "Task 0". The
> round-1 adversarial audit (three independent auditors) flagged the combined review surface as a real risk — a large
> mechanical rename diff degrades exactly the whole-branch gate that, on Plan 021, caught two proven vulnerabilities
> every per-task review had cleared. The rename has **zero coupling** to the retry work, so it ships alone.
> The retry work follows in [Plan 023](023-producer-outbound-retry.md), then
> [Plan 024](024-http-outbound.md).

---

## Global Constraints

- **Go 1.25 only.** `GOTOOLCHAIN=go1.25.12` on every invocation.
- **No dependency change.** `go.mod`/`go.sum` must be byte-identical at the end.
- **Behaviour-preserving except for the wire value.** The identifier rename changes nothing observable; the value
  change is a deliberate data-format break, accepted and documented in ADR 0026.
- **Blackbox tests only** (`package <pkg>_test`), assert-closure tables, `t.Context()`.
- **No test may be edited beyond the mechanical rename**, except the single case added in Task 1 Step 4. A test that
  needs more than that means the change was not behaviour-preserving — stop and investigate.
- **Coverage must not move — measured per function, NOT by the package total.** A rename cannot change coverage, but
  the package total is not a trustworthy witness: `consumer.go`'s `admit()` flaps 90.0% <-> 100.0% between runs, so
  the core total oscillates 99.2%/99.3% with no code change at all. Compare the per-function profile with that entry
  excluded (Steps 1 and 6). **Fixing the flake is backlog, not this increment** — `admit` is the credit-based
  in-flight gate and its uncovered arm is race-dependent, the same class as the Plan 021 interleaving lesson.

## Baseline (captured 2026-07-22, before any edit)

Re-verify these before starting; if any differs, the tree has drifted from what ADR 0026 was written against and the
ADR's blast-radius claim must be corrected first.

| Fact | Value |
|---|---|
| `go test ./... -race` | all 6 packages `ok` |
| Coverage | core **99.2%**, `adapter/http` **100.0%**, `adapter/http/stdlib` **100.0%**, `adapter/database/sql` **93.7%**, `adapter/memory` **71.3%**, `adapter/cron` **50.8%** |
| `grep -rn 'HeaderID' --include='*.go' . \| wc -l` | **36** total |
| — of which **code references** `gopls` renames | **28** |
| — of which **comment prose** `gopls` does NOT rename | **8** (listed in Step 3) |
| `grep -rn '"msgin\.id"' --include='*.go' . \| wc -l` | **1** (the declaration) |
| doc-comment mentions of the value `msgin.id` in `*.go` | **22**, across 10 files (Step 3b) |
| Go packages touched | **4** — `msgin`, `msghttp`, `msginsql`, and the separate **`harness` module** |
| `go list ./...` from the repo root | **6 packages, ZERO from `harness`** — `./...` does not cross module boundaries |
| Markdown files mentioning `HeaderID` or `msgin.id` | **13**, excluding ADR 0026 and this plan |

---

## Task 1: The rename

**Files — 23 Go files and 16 markdown files. Round 3 found the previous listing mis-attributed which file is touched
by which mechanism, and that error propagated into Step 8's allow-list, so the breakdown below is derived from the
greps rather than from prose.**

- Modify: `message.go` — the declaration and its value
- Modify (via `gopls`, code refs — `gopls` resolves these semantically across every module in `go.work`):
  `message.go`, `message_test.go`, `splitter.go`, `adapter/database/sql/{errors,groupdialect,groupstore}.go`,
  `adapter/database/sql/{fakedialect_test,framing_test,outbound_unit_test,source_unit_test}.go`,
  and the six files of the separate **`harness` module** (`dialect.go`, `groupstore.go`, `harness.go`, `outbound.go`,
  `outbox.go`, `queuestore.go` — 16 of the 36 refs live there; see Step 6)
- Modify (by hand, 8 comment mentions of the NAME — `gopls` does not touch prose): listed in Step 3. Note
  `splitter_test.go` and `adapter/http/encode.go` have **comment-only** references and so appear *here*, not in the
  `gopls` list above — the previous version of this plan had that backwards.
- Modify (by hand, 22 comment mentions of the VALUE `msgin.id`): 10 files, listed in Step 3b — includes
  `adapter/database/sql/framing.go`'s on-wire stability contract and `adapter/http/options.go`'s security CAUTION
- Modify: `adapter/http/encode_test.go` — one new case (Step 4); the only Go file in the commit that is not a rename
- Modify: 14 markdown files — listed in Step 5
- Modify: `docs/adrs/0026-header-message-id-rename.md` — Status → Accepted; plus this plan file

> **The authoritative derivation**, which Step 8's allow-list must be rebuilt from and which every count above comes
> from:
>
> ```bash
> { grep -rl '\bHeaderID\b' --include='*.go' . ; grep -rl 'msgin\.id' --include='*.go' . ; } \
>   | sed 's|^\./||' | sort -u
> ```
>
> That union is **22** files today; `adapter/http/encode_test.go` is the 23rd, added by Step 4.

**Interfaces:**
- **Produces:** `const HeaderMessageID = "msgin.message-id"` in package `msgin`. **`HeaderID` ceases to exist** — no
  deprecation alias (ADR 0026 §3), so any stale reference is a compile error by design.
- **Consumes:** nothing.

**Hot-path branches introduced:** none. No new branch, no new error path. The coverage gate is satisfied by the
existing cases continuing to pass at the baseline numbers.

- [ ] **Step 0a: PRECONDITION — the right branch (ADR 0026 §4)**

ADR 0026 §4 requires the rename to **ship alone**, so the whole-branch `/code-review` gate sees a diff that is only the
rename. Round 3 found the plan had no step enforcing that, and that executing it where the plan was authored
(`feat/producer-retry-http-outbound`, which carries the Plan 023 design and code) would defeat exactly the mitigation
§4 was written to buy.

**Sequencing decision (round 3).** This plan runs **after Plan 023 has merged to `main`**, not before. Plan 023's
branch already carries ADR 0026 and this plan file in commit `8459d07`, so once it merges, both artifacts are on `main`
and this branch needs **no cherry-pick and creates no duplicate-doc merge conflict**. The alternative — cutting this
branch off `main` first and re-landing the two docs — was rejected because `8459d07` also contains ADR 0025, so it
cannot be cherry-picked whole, and the resulting duplicate ADR 0026 would conflict on the later merge.

```bash
cd /Users/zakyalvan/Documents/RND/msgin
git checkout main && git pull --ff-only 2>/dev/null || true
test -f docs/adrs/0026-header-message-id-rename.md || { echo "ADR 0026 not on main — Plan 023 has not merged yet"; false; }
git checkout -b refactor/header-message-id-rename
git rev-parse --abbrev-ref HEAD    # must print refactor/header-message-id-rename
git log --oneline main..HEAD       # must print NOTHING — the branch starts empty
```

**Both assertions are hard stops.** A non-empty `main..HEAD` means this branch carries someone else's work and the
gate's "pure rename diff" property is already lost.

- [ ] **Step 0: PRECONDITION — the working tree must be completely clean**

Step 7 uses `git add -u`, which stages **every** tracked modification. Its safety therefore rests entirely on the tree
containing nothing else. Round 3 proved the previous form of this step did not deliver that: it asked only whether one
named file (`docs/specs/011-http-adapter.md`) was dirty, which by then it no longer was, so the step read as
"already satisfied" and was skippable — while `.claude/settings.json` **was** dirty and would have been swept into a
commit claiming to be a pure mechanical rename.

**This is the class fix.** Assert the tree state itself; do not enumerate files that might be dirty.

```bash
test -z "$(git status --porcelain)" && echo "TREE CLEAN" || { git status --short; echo "TREE DIRTY — STOP"; false; }
```

**`TREE CLEAN` is required to proceed. There is no allowed exception.** If anything is listed:

- **`.claude/settings.json`** — a pre-existing, intentional, unrelated local change that has been in the tree for
  several sessions. It is *not* this plan's to commit. Stash it:
  `git stash push -m "unrelated settings, not part of the rename" .claude/settings.json`
  and restore it with `git stash pop` after Step 8.
- **anything else** — land it as its own commit with the user's approval, or stash it. Do not proceed with a dirty
  tree, and do not "just be careful with `git add`" — round 1 and round 2 both tried a curated path list and both
  produced a list that was simultaneously incomplete and polluting.

> Round 1 caught `git add -A` sweeping the dirty tree. Round 2 replaced it with a hand-written path list that was
> itself incomplete (missing `message_test.go`) and polluting (staging an already-dirty spec). Round 3 caught the
> replacement precondition being vacuous. Three rounds, one lesson: **the tree state is the precondition**, and it must
> be asserted as a state, never as a checklist of known-suspect files.

- [ ] **Step 1: Re-verify the baseline**

Coverage output embeds a per-package timing that differs between a cached and an uncached run (`(cached)` vs `2.994s`),
so the raw text is **not** comparable across Step 1 and Step 6. Normalize it away and force `-count=1` on both captures.

```bash
GOTOOLCHAIN=go1.25.12 go test ./... -race >/dev/null && echo "GREEN" && \
GOTOOLCHAIN=go1.25.12 go test -coverprofile=/tmp/cov-before.out -count=1 . >/dev/null && \
GOTOOLCHAIN=go1.25.12 go tool cover -func=/tmp/cov-before.out | grep -v 'consumer.go:.*admit' > /tmp/cov-before.txt && \
echo "total HeaderID refs: $(grep -rn 'HeaderID' --include='*.go' . | wc -l)" && \
echo "  code refs (gopls): $(( $(grep -rn 'HeaderID' --include='*.go' . | wc -l) - $(grep -rn 'HeaderID' --include='*.go' . | grep -cE '// .*HeaderID') ))" && \
echo "  comment mentions:  $(grep -rn 'HeaderID' --include='*.go' . | grep -cE '// .*HeaderID')" && \
echo "msgin.id literals:   $(grep -rn '\"msgin\.id\"' --include='*.go' . | wc -l)" && \
echo "msgin.id prose:      $(( $(grep -rn 'msgin\.id' --include='*.go' . | wc -l) - 1 ))"
```

Expected: `GREEN`; coverage matching the baseline table; **36** total refs = **28** code + **8** comment; **1** literal;
**22** prose mentions of the value.

- [ ] **Step 2: Rename the identifier with `gopls` — NOT with `sed`**

Use the `LSP` tool's rename on the symbol at `message.go:15` (`HeaderID` → `HeaderMessageID`). It resolves references
semantically across every module in `go.work`.

A text substitution is **forbidden here** and this is not stylistic: `gopls` renames *references*, and the 8 comment
mentions must be reviewed individually in Step 3 because two of them (`splitter.go:17`, `splitter.go:64`) use the name
inside a formula (`HeaderID = parentID#seq`) where a blind replace produces prose that no longer parses as English.

```bash
grep -rn '\bHeaderID\b' --include='*.go' . | wc -l
grep -rn 'HeaderMessageID' --include='*.go' . | wc -l
```

Expected: the first prints **8** (the comment mentions only — *not* zero; `gopls` correctly leaves prose alone), the
second prints **28**.

> The plan this was extracted from expected `0` and `36` here. Both were wrong, and the audit caught it: an implementer
> following the old text would have hit a false "the tree has drifted — stop" on a perfectly healthy tree.

- [ ] **Step 3: Fix the 8 comment mentions by hand**

Each is prose, not a reference. Update the name (and, where the *value* is quoted, the value too):

| File:line | Current text |
|---|---|
| `splitter.go:17` | `// child id (HeaderID = parentID#seq — unique within the split yet stable across` |
| `splitter.go:64` | `//   - A deterministic child HeaderID = parentID#num (only when the parent has an` |
| `splitter_test.go:131` | `// headers (incl. HeaderID), so Split MUST overwrite each child id.` |
| `adapter/database/sql/groupdialect.go:98` | `// deliveries carry msgin.HeaderID and the Splitter stamps a deterministic` |
| `adapter/database/sql/groupstore.go:174` | `// msgin.HeaderID and the Splitter stamps a deterministic child id, so this is` |
| `adapter/database/sql/errors.go:130` | `// msgin.HeaderID and the Splitter stamps a deterministic child id, so this` |
| `adapter/database/sql/harness/groupstore.go:86` | `// no HeaderID` (trailing comment) |
| `adapter/http/encode.go:30` | `// header constant (msgin.HeaderID, msgin.HeaderTimestamp,` |

Verify:

```bash
grep -rn '\bHeaderID\b' --include='*.go' .
```

Expected: **no output**.

- [ ] **Step 3b: Update the 22 doc-comment mentions of the VALUE `msgin.id`**

The single string literal is the *functional* radius of the value change; it is **not** the documentation radius.
22 doc-comment mentions of `msgin.id` live across 10 Go files, and two of them are load-bearing public contract text:

| File | Why it matters |
|---|---|
| `adapter/database/sql/framing.go` | inside the godoc block headed **"On-wire header format (stability contract)"** — leaving it stale actively misdocuments the persisted format this ADR just broke |
| `adapter/http/options.go` | the `WithResponseHeaders` security CAUTION enumerating leakable reserved keys |
| `message.go` (9 mentions), `message_test.go` (3), `adapter/database/sql/groupstore.go` (2), `errors.go` (1), `inbox_dedup.go` (1), `groupstore_unit_test.go` (2), `fakedialect_test.go` (1), `adapter/http/encode.go` (1) | godoc and test prose |

Update every one to `msgin.message-id`. Then **re-format**, and verify **both** names are gone in one grep:

```bash
GOTOOLCHAIN=go1.25.12 gofmt -w .
grep -rn '\bHeaderID\b\|msgin\.id\b' --include='*.go' .
```

Expected: **no output** from the grep.

> **`gofmt -w .` is a required action here, not a check.** Round 3 simulated the rename and confirmed the longer
> identifier breaks `gofmt`'s column alignment in exactly three files — `message.go` (the const block),
> `message_test.go` (two `map[string]any` literals) and `adapter/database/sql/framing_test.go` (one literal). Step 6
> asserts `gofmt -l .` is empty but gave no remediation, so an implementer would hit `GOFMT DIRTY` and stop with no
> instruction. Round 3 also verified that **only the renamed lines move** — no sibling line realigns — so Step 8's
> purity grep stays clean after the reformat.

> Note this pulls `message_test.go` and `adapter/http/options.go` into the modified set — both must appear in Step 7's
> `git add` list (they do). Step 8's first purity grep deliberately filters out lines matching `msgin\.id`, so it can
> **not** detect a missed mention here; this grep is the only check that can.

- [ ] **Step 4: Change the value, and re-prove the reserved-namespace strip**

In `message.go`, change the declaration to:

```go
	HeaderMessageID     = "msgin.message-id"
```

The new value stays inside the reserved `msgin.` namespace, so the HTTP adapter's client-forgery strip still covers it
by construction — but that is the one security-relevant property this change could silently break, so prove it.

Add a case to the **existing** `TestDecodeRequest` table in `adapter/http/encode_test.go`. That table's case struct is:

```go
type testCase struct {
	name    string
	opts    []msghttp.Option
	request func() *http.Request
	assert  func(t *testing.T, msg msgin.Message[any], err error)
}
```

so the case must build its request through the `request` closure (there is **no** `reqHdrs` field — the plan this was
extracted from invented one, and the audit caught it):

```go
		{
			name: "a client cannot forge the renamed message-id header",
			// Allow-listed ON PURPOSE: even a misconfigured operator allow-list
			// naming a reserved key must not let the client set it.
			opts: []msghttp.Option{msghttp.WithRequestHeaders(msgin.HeaderMessageID)},
			request: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("body"))
				r.Header.Set(msgin.HeaderMessageID, "forged-by-client")
				return r
			},
			assert: func(t *testing.T, msg msgin.Message[any], err error) {
				t.Helper()
				require.NoError(t, err)
				got, ok := msg.Header(msgin.HeaderMessageID)
				require.True(t, ok, "the server-minted id must be present")
				assert.NotEqual(t, "forged-by-client", got,
					"the reserved strip must still cover the renamed key")
				assert.Equal(t, msg.ID(), got, "the id must be the server-minted one")
			},
		},
```

> Match the surrounding cases' exact field usage and helper style before pasting — read the neighbouring cases first.

- [ ] **Step 5: Sweep the 13 markdown files**

Update `HeaderID` → `HeaderMessageID` and `msgin.id` → `msgin.message-id` in:

`docs/specs/001-messaging-core.md`, `docs/specs/006-cron-source.md`, `docs/specs/009-splitter-aggregator-endpoints.md`,
`docs/specs/011-http-adapter.md`, `docs/adrs/0010-poller-sql-adapter.md`,
`docs/adrs/0020-splitter-aggregator-group-store.md`, `docs/adrs/0021-sql-group-store.md`,
`docs/adrs/0023-http-channel-adapter.md`, `docs/plans/001-core-foundation.md`, `docs/plans/015-splitter.md`,
`docs/plans/019-messaging-gateway.md`, `docs/plans/020-http-adapter-inbound.md`,
`docs/plans/024-http-outbound-source-brief.md`, `MESSAGING.md`.

> **`024-http-outbound-source-brief.md` was added after this plan was written** (it landed with Plan 023). It is the
> input to Plan 024, so leaving it stale would have Plan 024 implemented against the dead key. This is exactly why
> Step 5's list is re-derived from the grep, not trusted.

**Edit in place; do not annotate as superseded** — these describe the same header under its new name, not a reversed
decision (ADR 0026 §Traceability). Where a doc quotes the old value inside a code block or a persisted-row example,
update it too so no example teaches the dead key.

Then set ADR 0026's Status to `Accepted (2026-07-22)`.

```bash
grep -rn 'HeaderID\|msgin\.id' docs/ *.md 2>/dev/null \
  | grep -vE '0026-header-message-id-rename|022-header-message-id-rename|docs/HANDOVER\.md'
```

Expected: **no output**.

> **Three files are excluded deliberately, and `docs/HANDOVER.md` is the round-3 addition.** ADR 0026 and this plan
> quote the old name because documenting it *is* their job. `docs/HANDOVER.md` has three matches
> (lines 74, 160, 170) which likewise **describe the rename decision itself** — "`HeaderID` → `HeaderMessageID`", and
> two round-2 audit findings counted in terms of the old value. Rewriting them would corrupt a historical record and
> make the audit trail nonsense. **Do not edit `docs/HANDOVER.md` in this task**, and note it is therefore absent from
> Step 8's allow-list. Round 3 caught this as a guaranteed false failure: without the exclusion this grep can never
> return empty, so an implementer would either stop on a phantom drift signal or edit a file the commit does not expect.

- [ ] **Step 6: Full verification**

**`go build ./...` from the repo root covers the ROOT MODULE ONLY — 6 packages.** `go.work` does not make `./...`
recurse into workspace modules, and **`adapter/database/sql/harness` is its own module holding 16 of the 36 references**
— more than half the rename surface. Without the per-module loop below, this step can print a full green wall while the
harness module is broken, and CI (which builds each module standalone with `GOWORK=off`) would be the first to notice.

```bash
# 1. Format — asserted, not eyeballed.
test -z "$(gofmt -l .)" && echo "GOFMT CLEAN" || { gofmt -l .; echo "GOFMT DIRTY"; false; }

# 2. Root module.
GOTOOLCHAIN=go1.25.12 go build ./... && \
CGO_ENABLED=0 GOTOOLCHAIN=go1.25.12 go build ./... && \
GOTOOLCHAIN=go1.25.12 go vet ./... && \
GOTOOLCHAIN=go1.25.12 golangci-lint run ./... && \
GOTOOLCHAIN=go1.25.12 go test ./... -race && echo "ROOT MODULE GREEN"

# 3. EVERY nested module, standalone. CI's matrix (.github/workflows/ci.yml:33-45) is
#    . / harness / postgres / mysql / sqlite / dbtest; crontest is an extra this loop
#    adds. Unlike CI, this loop runs build+vet only — no go test, no golangci-lint.
fail=0
for d in adapter/database/sql/harness adapter/database/sql/postgres adapter/database/sql/mysql \
         adapter/database/sql/sqlite adapter/database/sql/dbtest adapter/cron/crontest; do
  [ -f "$d/go.mod" ] || continue
  echo "--- $d"
  ( cd "$d" && GOWORK=off GOTOOLCHAIN=go1.25.12 go build ./... && GOWORK=off GOTOOLCHAIN=go1.25.12 go vet ./... ) \
    || { echo "MODULE FAILED: $d"; fail=1; }
done
[ "$fail" = 0 ] && echo "NESTED MODULES GREEN" || { echo "NESTED MODULES FAILED — STOP"; false; }

# 4. Coverage must not move — but NOT via the package total, which is FLAKY.
#    Measured: consumer.go's admit() flaps 90.0% <-> 100.0% run to run (a
#    race-dependent arm in the credit-based in-flight gate), moving the core
#    package total between 99.2% and 99.3%. consumer.go is not in this plan's
#    modified set and holds no header reference, so keying the gate off the
#    package total produces a FALSE FAILURE roughly one run in three.
#    Compare the per-function profile with that one known-flaky entry excluded.
GOTOOLCHAIN=go1.25.12 go test -coverprofile=/tmp/cov-after.out -count=1 . >/dev/null && \
GOTOOLCHAIN=go1.25.12 go tool cover -func=/tmp/cov-after.out | grep -v 'consumer.go:.*admit' > /tmp/cov-after.txt
diff /tmp/cov-before.txt /tmp/cov-after.txt && echo "COVERAGE UNCHANGED"

# 5. Module hygiene — run UNCONDITIONALLY, not chained behind the coverage diff.
GOTOOLCHAIN=go1.25.12 go mod tidy && git diff --exit-code go.mod go.sum && echo "MODULE CLEAN"
```

Expected: `GOFMT CLEAN`; both builds succeed; vet and lint clean; all root packages `ok`; **`NESTED MODULES GREEN`**;
**`COVERAGE UNCHANGED`**; `MODULE CLEAN`.

> **Run the five blocks in order and read §3's verdict line before trusting §4/§5.** They are separate commands, so a
> §3 failure does not stop §4 and §5 from printing their own green verdicts. Round 3 found the previous form worse
> still: `|| { echo …; break; }` left the block's exit status **0**, so a broken `harness` module — which holds 16 of
> the 36 references and which the root module does not import, so `go build ./...` never touches it — produced a full
> green wall with one `MODULE FAILED` line buried mid-output. That is round-2 finding M3 recurring: the fix added the
> loop but not the failure propagation. `fail=1` (rather than `break`) now also reports *every* broken module, not just
> the first.

> Two round-2 fixes are load-bearing here. (a) The coverage capture is **normalized and `-count=1` on both sides** —
> the previous form compared a cached Step-1 run against an uncached Step-6 run, so `diff` reported all 6 lines changed
> despite identical percentages: a guaranteed false failure. (b) `go mod tidy` is no longer chained behind that diff
> with `&&`, which had silently skipped the module-hygiene gate whenever the false failure fired.
>
> `adapter/http` is already at 100.0% and `_test.go` statements are excluded from the coverage denominator, so the
> added test case cannot move any number. Any movement at all is a signal, not noise.

- [ ] **Step 7: Commit — with an EXPLICIT path list**

> **Do not use `git add -A`.** The working tree carries unrelated pre-existing changes (`.claude/settings.json`, the
> Plan 023/024 design artifacts, `docs/HANDOVER.md`). `git add -A` would sweep them into a commit whose message claims
> to be a pure mechanical rename — the audit flagged this as a critical defect in the original plan, and its
> verification step could not detect it because it grepped only `*.go`.

```bash
git add -u   # stage every TRACKED modification — see the note below on why this is now correct
```

**`git add -u` is safe here ONLY because Step 0 asserted `TREE CLEAN`**, and it is *safer* than a hand-written path
list: round 2 proved the curated list was simultaneously **incomplete** (it omitted `message_test.go`, whose 4 renamed
references would have made HEAD a broken-build commit, and `adapter/http/options.go` from Step 3b) and **polluting**
(it staged an already-dirty spec). A list that must enumerate ~23 paths correctly, by hand, is the wrong tool; a clean
precondition plus `-u` is the right one — but **only** with Step 0's state assertion actually in force, which is what
round 3 found missing.

> **ADR 0026 and this plan file are already TRACKED** (committed in `8459d07`), so `-u` picks up their modifications
> and the separate `git add` the previous version of this step performed is unnecessary — its accompanying claim that
> they were "the two new untracked docs" was stale. **Tick this plan's checkboxes as you go and let them ride in the
> commit**; Step 8's allow-list assumes the file is present.

Now verify the staged set is **exactly** what this plan touches — bidirectionally:

```bash
echo "=== modified-but-UNSTAGED (must be EMPTY — else the commit won't compile) ==="
git diff --name-only

echo "=== untracked (must be EMPTY of anything this plan created) ==="
git status --porcelain | grep '^??' || echo "(none)"

echo "=== staged set ==="
git diff --cached --name-only | sort
```

Expected: the first two are empty; the staged set is **23 Go files + 16 markdown files = 39**, matching Step 8's
allow-list exactly. **A non-empty first list is a hard stop** — it means a renamed file is not in the commit.

> Round 3 caught the previous expectation here — "the 6 Go files" — as flatly wrong: 23 Go files are modified. An
> implementer using it as the acceptance criterion would conclude a correct 23-file stage was broken, or that a 6-file
> stage was right.

Then commit. Note the type is `feat!`, not `refactor!`: CLAUDE.md defines `refactor` as a *behaviour-preserving*
restructure, and a wire-format change is not behaviour-preserving. The footer uses the Conventional-Commits
`BREAKING CHANGE:` token so a CI parser recognises it.

```bash
git commit -m "$(cat <<'EOF'
feat!: rename HeaderID to HeaderMessageID, value to msgin.message-id

HeaderID did not say WHICH id it was, which reads worst exactly where it appears
most — beside HeaderCorrelationID. The value carried the same ambiguity on the
wire, where no Go identifier is available to disambiguate it, and msgin.id was
the namespace's only un-hyphenated multi-word key.

Mechanical: 28 references renamed via gopls across the workspace, 8 comment-prose
mentions corrected by hand, no behaviour change. One case added to the existing
DecodeRequest table proving the HTTP reserved-namespace strip still covers the
renamed key.

BREAKING CHANGE: the exported HeaderID symbol is renamed with NO deprecation
alias, and its wire value changes, so envelopes persisted by the database/sql
adapter before this commit carry the old key. Both breaks are free only because
the repo has zero tags and nothing is released; ADR 0026 records the manual
migration a consumer running an unreleased build off main would need.

Plan: 022
ADR: 0026
EOF
)"
```

- [ ] **Step 8: Prove the commit is a pure rename**

```bash
git show --stat HEAD

echo "=== non-rename Go lines in the commit (expect ONLY the new test case) ==="
git show HEAD -- '*.go' | grep -E '^[+-]' | grep -vE '^[+-]{3}' | \
  grep -viE 'HeaderMessageID|HeaderID|msgin\.message-id|msgin\.id'

echo "=== the commit's file set vs the EXACT expected set ==="
cat > /tmp/expected-files.txt <<'EOF'
MESSAGING.md
adapter/database/sql/errors.go
adapter/database/sql/fakedialect_test.go
adapter/database/sql/framing.go
adapter/database/sql/framing_test.go
adapter/database/sql/groupdialect.go
adapter/database/sql/groupstore.go
adapter/database/sql/groupstore_unit_test.go
adapter/database/sql/harness/dialect.go
adapter/database/sql/harness/groupstore.go
adapter/database/sql/harness/harness.go
adapter/database/sql/harness/outbound.go
adapter/database/sql/harness/outbox.go
adapter/database/sql/harness/queuestore.go
adapter/database/sql/inbox_dedup.go
adapter/database/sql/outbound_unit_test.go
adapter/database/sql/source_unit_test.go
adapter/http/encode.go
adapter/http/encode_test.go
adapter/http/options.go
docs/adrs/0010-poller-sql-adapter.md
docs/adrs/0020-splitter-aggregator-group-store.md
docs/adrs/0021-sql-group-store.md
docs/adrs/0023-http-channel-adapter.md
docs/adrs/0026-header-message-id-rename.md
docs/plans/001-core-foundation.md
docs/plans/015-splitter.md
docs/plans/019-messaging-gateway.md
docs/plans/020-http-adapter-inbound.md
docs/plans/022-header-message-id-rename.md
docs/plans/024-http-outbound-source-brief.md
docs/specs/001-messaging-core.md
docs/specs/006-cron-source.md
docs/specs/009-splitter-aggregator-endpoints.md
docs/specs/011-http-adapter.md
message.go
message_test.go
splitter.go
splitter_test.go
EOF
wc -l < /tmp/expected-files.txt      # must print 39
diff <(git show --name-only --format= HEAD | grep -v '^$' | sort) <(sort /tmp/expected-files.txt) \
  && echo "COMMIT CONTENTS EXACT"
```

Expected: `39`; the first grep prints only the added test case's lines; the diff prints nothing and
**`COMMIT CONTENTS EXACT`** appears. Anything else is unintended change riding along in a commit that claims to be
mechanical — reset and restage before proceeding.

> **ROUND-3 CRITICAL, now fixed.** The previous list had **35** entries and was missing three files that carry
> `HeaderID` **code** references `gopls` will rename — `adapter/database/sql/framing_test.go`,
> `adapter/database/sql/outbound_unit_test.go`, `adapter/database/sql/source_unit_test.go`. `diff` would have reported
> them as extra on every run, so `COMMIT CONTENTS EXACT` could **never** print, on a *correct* commit.
>
> The root cause is worth naming, because it is round 2's shallow-fix pattern for a third time: the list was derived
> from the **prose** survey (Step 3b's `msgin.id` files) rather than from the **union** of both greps, so it silently
> substituted six prose-only files for the six code-reference files. ADR 0026 already had the right shape
> ("13 files under `adapter/database/sql` — 7 in the adapter, 6 under `harness/`") and the plan disagreed with it
> without either noticing.
>
> **Re-derive, do not hand-edit.** The expected set is, by definition:
>
> ```bash
> { grep -rl '\bHeaderID\b' --include='*.go' . ; grep -rl 'msgin\.id' --include='*.go' . ; } \
>   | sed 's|^\./||' | sort -u          # 22 Go files, run BEFORE any edit
> ```
>
> plus `adapter/http/encode_test.go` (Step 4), plus Step 5's 14 markdown files, plus ADR 0026 and this plan = **39**.
> `docs/HANDOVER.md` is deliberately **not** in the set (Step 5). Run that derivation before Step 2 and keep its output;
> the literal list above is a convenience, and if the two ever disagree, **the derivation wins**.
>
> This whole check replaces an extension-pattern grep (`grep -vE '\.go$|^docs/|\.md$'`) that round 2 proved was blind
> to the pollution class that actually exists here: its `^docs/` clause whitelisted **everything** under `docs/`. An
> exact allow-list is the only form that closes it — but only if the list is right, which is what round 3 caught.

- [ ] **Step 9: Merge (requires explicit user approval)**

`git push`, the merge, and branch deletion each need **explicit per-action approval**. Present the diffstat and the
Step 6/8 output, then wait.

---

## Self-review

**ADR 0026 coverage.** §1 identifier rename → Step 2 + Step 3. §2 value change → Step 4. §3 no alias → asserted by the
absence of any alias step, and enforced by Step 2's grep expecting zero remaining code references. §4 "lands first and
alone" → satisfied more strongly than the ADR states, since it is now its own branch rather than Task 0 of a larger one
(ADR 0026's §4 and §Consequences need a one-line amendment to match — fold into Step 5's ADR edit).

**Audit findings folded in from round 1** (all three auditors examined the original Task 0):
- `git add -A` sweeping the dirty tree → Step 7's explicit path list + `git status --short` check.
- Step 8's purity grep being unable to detect non-Go pollution → the second grep added.
- The 36/0 grep expectations being arithmetically wrong → corrected to 28 code refs + 8 comment mentions, with the
  8 enumerated by file:line in Step 3.
- The prose-sweep list missing 5 files (`specs/001`, `specs/006`, `adrs/0010`, `adrs/0020`, `MESSAGING.md`) → added.
- The verification grep never being satisfiable because this plan itself contains matches → exclusion added.
- The invented `reqHdrs` test-table field → rewritten against the real `request func() *http.Request` shape.
- `refactor!` misdescribing a wire-format change, and `BREAKING:` not being the Conventional-Commits token → now
  `feat!` with a `BREAKING CHANGE:` footer.

**Round-2 audit findings, all folded in (verdict was NOT READY):**
- **C1** `message_test.go` (4 renamed refs) missing from the stage list → broken-build commit → Step 7 now uses
  `git add -u` behind a clean precondition, plus a **bidirectional** check that fails on modified-but-unstaged files.
- **C2** the stage list included `docs/specs/011-http-adapter.md`, already dirty with ~18 lines of Plan 023/024 design
  → new **Step 0** makes tree-cleanliness a precondition and names the stash.
- **C3** Step 8's `^docs/` whitelist made C2 invisible to both purity checks → replaced with an exact expected-file
  allow-list.
- **M1** 22 orphaned doc-comment mentions of the value `msgin.id`, including `framing.go`'s **on-wire stability
  contract** and `options.go`'s security CAUTION → new **Step 3b** + a combined verification grep. ADR 0026's Context
  now records the prose radius alongside the literal radius.
- **M2** the coverage `diff` was a **proven** false failure (cached-vs-timed output), which also silently skipped
  `go mod tidy` via `&&` short-circuit → both captures normalized and `-count=1`; hygiene unchained.
- **M3** `go build ./...` never reaches the `harness` module holding 16 of 36 refs → per-module `GOWORK=off` loop
  mirroring CI.
- **M4** ADR 0026 had 4 links to a non-existent plan file, a §4 stating the opposite of the decision taken, a
  self-contradictory Consequences bullet, and two wrong counts (5→4 packages, twelve→thirteen files) → all corrected.
- **m1/m2/m3** `git add <dir>` staging untracked files → superseded by `-u`; `gofmt -l` never asserted → now
  `test -z`; `govulncheck` deliberately skipped (no dependency change) rather than silently omitted.

**Round-3 audit findings, all folded in (verdict was NOT READY).** Round 3's brief was specifically to test whether
round 2's fixes were shallow in the way round 1's had been. They were, in three places:

- **C1** Step 8's expected-file list was **missing 3 files** (`framing_test.go`, `outbound_unit_test.go`,
  `source_unit_test.go`) that carry `HeaderID` *code* references, because the list was derived from the **prose**
  survey rather than the **union** of both greps. `COMMIT CONTENTS EXACT` could never print on a correct commit →
  list corrected to 38 and the derivation formula made normative, with "the derivation wins" over the literal list.
  *This was the exact item the previous round flagged as its own remaining judgement call, and it was indeed wrong.*
- **C2** Step 0's precondition was **vacuous** — it asked only whether one named file was dirty, which by then it was
  not, while `.claude/settings.json` **was** dirty and `git add -u` would have swept it in → replaced with a
  `test -z "$(git status --porcelain)"` state assertion naming the actual dirty file.
- **C3** **No branch step existed**, so the plan would execute on `feat/producer-retry-http-outbound` and its
  whole-branch gate would see the producer-retry design too — defeating ADR 0026 §4's "ships alone" mitigation →
  new **Step 0a**, plus the sequencing decision to run this plan *after* Plan 023 merges so no cherry-pick is needed.
- **M1** Step 5's markdown grep could never return empty because `docs/HANDOVER.md` matches → excluded, with the
  reason (its mentions describe the rename decision and must stay).
- **M2** Step 7's stated expectation was "the 6 Go files"; the real number is **23** → corrected.
- **M3** Step 0's narrative premise was stale (the files it described as dirty had all landed), making the step read as
  already-satisfied → rewritten as a state assertion, which is also the C2 fix.
- **M4** Step 6's module loop **swallowed failure** (`break` left the block's exit status 0), so a broken `harness`
  module — which the root build never touches — produced a green wall → `fail` flag + an asserted verdict line.
- **m1–m5** Task 1's Files list mis-attributed which files `gopls` touches (the likely root of C1); `gofmt -w` was
  required after the rename but never stated (3 files affected, verified); Step 7 called two tracked files "untracked";
  the checkbox-commit policy was unstated; the "mirrors CI" claim was inaccurate (CI's matrix omits `crontest` and CI
  also runs `go test`/lint per module). All corrected inline.

**The three-round meta-lesson, which is the thing to carry into Plans 023/024:** each round's fix addressed the
*instance* the previous auditor named, and each time the *class* re-manifested through a different file. `git add -A`
→ a curated path list (incomplete **and** polluting) → `git add -u` behind a precondition that did not actually assert
tree state. The durable fix in each case was to assert an invariant (`TREE CLEAN`, a derived file set, a propagated
exit status) rather than to enumerate the known-bad cases.

**Remaining judgement calls for a round-4 audit, if one is run:**

1. Step 8's list is now **derived**, but the literal 38-entry copy is still in the document and can go stale again if
   the tree changes before implementation. It is retained because its failure mode is *inverted* — a stale entry fails
   loudly on a correct commit (annoying, safe), whereas a stale `git add` list failed *silently* on a broken one — and
   because the step now states that the derivation is authoritative.
2. Round 3 found an **ADR 0026 defect this plan does not fix**: the migration SQL covers the `JSONB` `headers` column
   of the queue/outbox tables but not the aggregator group-member table, whose `headers` column is **`BYTEA`**
   (`adapter/database/sql/postgres/groupddl.go:48`), where the `-` and `?` jsonb operators do not exist. A consumer
   following the ADR would migrate half their data. **Fixed in ADR 0026 as part of Step 5's ADR edit** — verify it is
   there before committing.
