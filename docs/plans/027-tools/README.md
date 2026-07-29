# Derivation tools (Plan 027)

Committed because Spec 014 §8.1 and Plan 027 Task 12 invoke them as mandatory gates. Round-3 audit finding
**B6**: they previously existed only as compiled binaries under `/tmp/msgin-derive/`, so a fresh clone or a
`/tmp` reap made those gates unrunnable with no rebuild instructions.

These are standalone `main` packages kept OUT of the module build (build tag `ignore`). Run with `go run`.

```bash
# AST dump of every top-level declaration: file<TAB>line<TAB>kind<TAB>name<TAB>exported
go run docs/plans/027-tools/decls.go <dir>

# AST-based requalification of bare identifiers to <alias>.X.
# Operates on the AST, so comments and string literals are NEVER touched.
# A regex version corrupted EIP pattern names inside godoc ("a msgin.Message Translator");
# do not reintroduce one.
go run docs/plans/027-tools/qualify.go <pkgdir> <alias> <Sym>...
```

```bash
# JOIN CHECK (round-7 counter-rule 6). For each decision, extract the owning
# Task number cited near every mention, in every bundle document, and report
# disagreements. Point it at a directory of flat .md copies:
#
#   T=$(mktemp -d)
#   for f in specs/014-core-package-layout.md plans/027-core-package-layout.md \
#            adrs/0027-*.md adrs/0028-*.md adrs/0029-*.md adrs/0030-*.md rfcs/0002-*.md; do
#     cp docs/$f $T/$(echo $f | tr '/' '_')          # or: git show <sha>:docs/$f > …
#   done
python3 docs/plans/027-tools/joincheck.py $T
```

**Why it exists.** Round 6's fix pass was partitioned by file; four agents produced four internally-coherent
documents and three broken **joins**, every one a forward reference (ADR→Plan, Spec→Plan, Spec→ADR) the owning
agent could not see. Round 7 found three of those blockers with two-line greps of exactly this shape. Run it
against the **committed** state (`git show <sha>:…`), not a working tree mid-pass, or it reports transient
states.

**Limitation — it is a triage signal, not a verdict.** Ownership is inferred from a `Task N` citation within
200 characters after a decision's mention, so an unrelated task named nearby is a false positive. Read every
row before acting; the value is that a decision cited with *no* task, or with a task no other document names,
is surfaced mechanically instead of by an auditor building the table by hand.

`symmap.tsv` is the symbol -> destination-package map used by §8.1's staleness sweep (ARM 1).

Regenerate the root exported-symbol set after any move:

```bash
go run docs/plans/027-tools/decls.go . | grep -v '_test\.go' \
  | awk -F'\t' '$5=="exported" && $3!="method" {print $4}' | sort -u
```
