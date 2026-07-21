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
- **Coverage must not move.** A rename cannot change coverage; a moved number means something other than a rename
  happened.

## Baseline (captured 2026-07-22, before any edit)

Re-verify these before starting; if any differs, the tree has drifted from what ADR 0026 was written against and the
ADR's blast-radius claim must be corrected first.

| Fact | Value |
|---|---|
| `go test ./... -race` | all 6 packages `ok` |
| Coverage | core **99.1%**, `adapter/http` **100.0%**, `adapter/http/stdlib` **100.0%**, `adapter/database/sql` **93.7%**, `adapter/memory` **71.3%**, `adapter/cron` **50.8%** |
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

**Files:**
- Modify: `message.go` — the declaration and its value
- Modify (via `gopls`, 28 code refs): `message_test.go`, `splitter.go`, `splitter_test.go`, `adapter/http/encode.go`,
  and `adapter/database/sql/**` **including the separate `harness` module** (16 of the 36 refs live there — see Step 6)
- Modify (by hand, 8 comment mentions of the NAME): listed in Step 3
- Modify (by hand, 22 comment mentions of the VALUE): 10 files, listed in Step 3b — includes
  `adapter/database/sql/framing.go`'s on-wire stability contract and `adapter/http/options.go`'s security CAUTION
- Modify: `adapter/http/encode_test.go` — one new case
- Modify: 13 markdown files — listed in Step 5
- Modify: `docs/adrs/0026-header-message-id-rename.md` — Status → Accepted

**Interfaces:**
- **Produces:** `const HeaderMessageID = "msgin.message-id"` in package `msgin`. **`HeaderID` ceases to exist** — no
  deprecation alias (ADR 0026 §3), so any stale reference is a compile error by design.
- **Consumes:** nothing.

**Hot-path branches introduced:** none. No new branch, no new error path. The coverage gate is satisfied by the
existing cases continuing to pass at the baseline numbers.

- [ ] **Step 0: PRECONDITION — the working tree must be clean of anything this plan will stage**

The tree carries pre-existing, unrelated changes (the Plan 023/024 design artifacts, `.claude/settings.json`,
`docs/HANDOVER.md`). **`docs/specs/011-http-adapter.md` is both already dirty with ~18 lines of Plan 023/024 design AND
on this plan's edit list** — staging it would sweep unrelated architectural design into a commit that claims to be a
pure mechanical rename, and would simultaneously strip Plan 023 of its governing spec edit.

```bash
cd /Users/zakyalvan/Documents/RND/msgin
git diff --name-only        # tracked-but-modified
git status --short
```

**If `docs/specs/011-http-adapter.md` (or any other file on Step 7's list) appears**, resolve it before going further —
either land those changes as their own `spec:`/`docs:` commit with the user's approval, or:

```bash
git stash push -m "plan-023/024 design, not part of the rename" docs/specs/011-http-adapter.md
```

Re-run `git diff --name-only` and confirm **no file on Step 7's path list** is listed. Only then proceed.

> Round 1 caught `git add -A` sweeping the dirty tree; round 2 caught the replacement path list re-introducing the
> identical defect through one specific file. The lesson is that the *tree state* is the precondition — a curated `add`
> list cannot save you from a file that is legitimately on it and also already dirty.

- [ ] **Step 1: Re-verify the baseline**

Coverage output embeds a per-package timing that differs between a cached and an uncached run (`(cached)` vs `2.994s`),
so the raw text is **not** comparable across Step 1 and Step 6. Normalize it away and force `-count=1` on both captures.

```bash
GOTOOLCHAIN=go1.25.12 go test ./... -race >/dev/null && echo "GREEN" && \
GOTOOLCHAIN=go1.25.12 go test ./... -cover -count=1 2>&1 | grep coverage \
  | sed -E 's/[[:space:]]+(\(cached\)|[0-9.]+s)[[:space:]]+/ /' > /tmp/cov-before.txt && cat /tmp/cov-before.txt && \
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

Update every one to `msgin.message-id`. Then verify **both** names are gone in one grep:

```bash
grep -rn '\bHeaderID\b\|msgin\.id\b' --include='*.go' .
```

Expected: **no output**.

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
`docs/plans/019-messaging-gateway.md`, `docs/plans/020-http-adapter-inbound.md`, `MESSAGING.md`.

**Edit in place; do not annotate as superseded** — these describe the same header under its new name, not a reversed
decision (ADR 0026 §Traceability). Where a doc quotes the old value inside a code block or a persisted-row example,
update it too so no example teaches the dead key.

Then set ADR 0026's Status to `Accepted (2026-07-22)`.

```bash
grep -rn 'HeaderID\|msgin\.id' docs/ *.md 2>/dev/null | grep -vE '0026-header-message-id-rename|022-header-message-id-rename'
```

Expected: **no output**. (Matches inside ADR 0026 and this plan are correct — both document the old name deliberately.)

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

# 3. EVERY nested module, standalone — mirrors .github/workflows/ci.yml.
for d in adapter/database/sql/harness adapter/database/sql/postgres adapter/database/sql/mysql \
         adapter/database/sql/sqlite adapter/database/sql/dbtest adapter/cron/crontest; do
  [ -f "$d/go.mod" ] || continue
  echo "--- $d"
  ( cd "$d" && GOWORK=off GOTOOLCHAIN=go1.25.12 go build ./... && GOWORK=off GOTOOLCHAIN=go1.25.12 go vet ./... ) \
    || { echo "MODULE FAILED: $d"; break; }
done

# 4. Coverage must not move — same normalization and -count=1 as Step 1.
GOTOOLCHAIN=go1.25.12 go test ./... -cover -count=1 2>&1 | grep coverage \
  | sed -E 's/[[:space:]]+(\(cached\)|[0-9.]+s)[[:space:]]+/ /' > /tmp/cov-after.txt
diff /tmp/cov-before.txt /tmp/cov-after.txt && echo "COVERAGE UNCHANGED"

# 5. Module hygiene — run UNCONDITIONALLY, not chained behind the coverage diff.
GOTOOLCHAIN=go1.25.12 go mod tidy && git diff --exit-code go.mod go.sum && echo "MODULE CLEAN"
```

Expected: `GOFMT CLEAN`; both builds succeed; vet and lint clean; all root packages `ok`; **every nested module builds
and vets** with no `MODULE FAILED`; **`COVERAGE UNCHANGED`**; `MODULE CLEAN`.

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
git add docs/adrs/0026-header-message-id-rename.md docs/plans/022-header-message-id-rename.md
```

**`git add -u` is safe here ONLY because Step 0 guaranteed the tree contains nothing unrelated**, and it is *safer* than
a hand-written path list: round 2 proved the curated list was simultaneously **incomplete** (it omitted
`message_test.go`, whose 4 renamed references would have made HEAD a broken-build commit, and `adapter/http/options.go`
from Step 3b) and **polluting** (it staged an already-dirty `docs/specs/011-http-adapter.md`). A list that must
enumerate ~21 paths correctly, by hand, is the wrong tool; a clean precondition plus `-u` is the right one.

Note `-u` stages only tracked files, so the two new untracked docs are added explicitly and nothing else untracked can
sneak in.

Now verify the staged set is **exactly** what this plan touches — bidirectionally:

```bash
echo "=== modified-but-UNSTAGED (must be EMPTY — else the commit won't compile) ==="
git diff --name-only

echo "=== untracked (must be EMPTY of anything this plan created) ==="
git status --porcelain | grep '^??' || echo "(none)"

echo "=== staged set ==="
git diff --cached --name-only | sort
```

Expected: the first two are empty; the staged set is the 6 Go files, the 13 markdown files, ADR 0026 and this plan.
**A non-empty first list is a hard stop** — it means a renamed file is not in the commit.

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
docs/specs/001-messaging-core.md
docs/specs/006-cron-source.md
docs/specs/009-splitter-aggregator-endpoints.md
docs/specs/011-http-adapter.md
message.go
message_test.go
splitter.go
splitter_test.go
EOF
diff <(git show --name-only --format= HEAD | grep -v '^$' | sort) <(sort /tmp/expected-files.txt) \
  && echo "COMMIT CONTENTS EXACT"
```

Expected: the first grep prints only the added test case's lines; the diff prints nothing and
**`COMMIT CONTENTS EXACT`** appears. Anything else is unintended change riding along in a commit that claims to be
mechanical — reset and restage before proceeding.

> **Re-derive the expected list from the actual Step 2/3/3b/5 edits before running this** — it is written from the
> pre-implementation survey and a file may legitimately drop out (e.g. if a doc mention turns out to be inside a code
> fence that should keep the old name for historical accuracy).
>
> This replaces an extension-pattern check (`grep -vE '\.go$|^docs/|\.md$'`) that round 2 proved was blind to the
> pollution class that actually exists on this tree: its `^docs/` clause whitelisted **everything** under `docs/`, and
> 3 of the 4 currently-dirty tracked files live there. Round 1 patched the check for the one file it had named
> (`.claude/settings.json`) rather than for the class — an exact allow-list is the only form that closes it.

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

**Remaining judgement call for round 3:** Step 8's expected-file list is written from a pre-implementation survey and
is the one place this plan hard-codes a 35-entry list — exactly the brittleness that sank the Step 7 path list. It is
retained because its failure mode is *inverted*: a stale entry makes the check fail loudly on a correct commit
(annoying, safe), whereas a stale `git add` list failed *silently* on a broken one. The step says to re-derive it from
the actual edits before running.
