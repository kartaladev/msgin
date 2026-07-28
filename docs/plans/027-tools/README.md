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

`symmap.tsv` is the symbol -> destination-package map used by §8.1's staleness sweep (ARM 1).

Regenerate the root exported-symbol set after any move:

```bash
go run docs/plans/027-tools/decls.go . | grep -v '_test\.go' \
  | awk -F'\t' '$5=="exported" && $3!="method" {print $4}' | sort -u
```
