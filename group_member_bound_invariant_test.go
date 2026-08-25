package msgin_test

// Spec: 017 (§3.5)  Plan: 031 (Task 3)  ADR: 0033 (D-AQ)
//
// The mechanical enforcement of Spec 017 §3.5's invariant:
//
//	defaultMaxGroupMembers >= routing.completionSizeCeiling, in BOTH stores.
//
// A caller may legally configure routing.WithCompletionSize anywhere up to
// completionSizeCeiling. If either GroupStore's unset default admitted FEWER
// members than that, such a group would reject its own completing member
// before the release predicate could ever fire — a silent deadlock in place of
// a bound. The relation is therefore a correctness invariant spanning three
// packages, not a coincidence of three numbers that happen to match today.
//
// All three constants are UNEXPORTED and live in three different packages, so
// no ordinary blackbox test can compare them. A PARSER does not care: this
// test reads the constants straight out of the AST, exactly as the shipped
// sizing_option_class_gate_test.go reads the sizing-option class (see
// scanSizingParamRepo there — the model for both the os.Getwd() root, which
// for a root-package test IS the repository root and so reaches every module
// without go.work, and for the completeness walk below).
//
// THE FILE LIST IS ASSERTED, NEVER DISCOVERED. The invariant is read from the
// three sites named below and from nowhere else: a missing file or a renamed
// constant is a FAILURE, not a shorter run, because a not-found constant must
// never reach the comparison as a zero value where 0 >= 0 passes.
//
// TWO HALVES, and the second exists because the first is not enough:
//
//  1. INVARIANT (TestGroupMemberBoundInvariant). Read the ceiling and each
//     store's default out of the AST and assert default >= ceiling per store.
//     The cases are GENERATED from groupBoundStoreSites, so a store cannot be
//     dropped from the run without being dropped from that slice.
//  2. COMPLETENESS (TestGroupMemberBoundStoreSitesAreComplete). Walk every
//     non-test .go file in the repository and assert that the set of files
//     declaring `const defaultMaxGroupMembers` equals groupBoundStoreSites
//     EXACTLY, in both directions. Without this, deleting a store from the
//     slice leaves a vet-clean, lint-clean test that PASSES with one subtest —
//     the gate silently shrinking to one store while the other carries the
//     identical risk. That is this increment's own "fix the class, not the
//     instance" lesson reappearing one level up, inside the gate's structure.
//
// Discovery is a check ON the asserted list, not a replacement FOR it. Half 1
// never reads a discovered file.
//
// The cross-reference comments on the constants themselves (routing's
// completionSizeCeiling and each store's defaultMaxGroupMembers) are
// human-facing explanation. THIS test is the defence.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// groupBoundStoreConstName is the constant every first-party GroupStore
// declares for its unset per-group member cap. It is both the identifier half 1
// reads and the needle half 2 walks the repository for.
const groupBoundStoreConstName = "defaultMaxGroupMembers"

// groupBoundStoreFieldName is the config field each store's constructor
// installs its default into. Both stores use the same name — verified by grep
// over the non-test sources, not assumed — so one helper covers both.
const groupBoundStoreFieldName = "maxGroupMembers"

// groupBoundWiringSite is one place a source file sets
// groupBoundStoreFieldName: the field-set rendered as written, and the value's
// identifier name when the value is a bare identifier (empty otherwise, so a
// literal, a call or a qualified name can never satisfy the wiring assertion).
type groupBoundWiringSite struct {
	text  string
	ident string
}

// String renders the site as it appears in the source, for failure messages.
func (w groupBoundWiringSite) String() string { return w.text }

// groupBoundSite names one constant declaration this gate reads: a
// repository-relative file and the identifier declared in it.
type groupBoundSite struct {
	file string
	name string
}

// String renders the site the way every failure message in this file names a
// constant: identifier first, then the file it is declared in.
func (s groupBoundSite) String() string { return s.name + " (" + s.file + ")" }

// key renders the site as the comparable token the completeness check comes
// down to, so a site pointed at the wrong file AND a site renamed to a
// different constant both fail the same set equality.
func (s groupBoundSite) key() string { return s.file + " declares " + s.name }

// The asserted parse set — three files, named explicitly. Do NOT shorten this
// list to whatever a given tree happens to contain; covering one store while
// the other carries the identical risk is this project's "fix the class, not
// the instance" lesson violated inside the fix for it.
var (
	groupBoundCeilingSite = groupBoundSite{file: "routing/aggregator.go", name: "completionSizeCeiling"}
	groupBoundMemorySite  = groupBoundSite{file: "adapter/memory/groupstore.go", name: groupBoundStoreConstName}
	groupBoundSQLSite     = groupBoundSite{file: "adapter/database/sql/groupstore.go", name: groupBoundStoreConstName}
)

// groupBoundStoreSites is the single source of the stores under this gate. The
// invariant table is GENERATED from it and the completeness check ASSERTS it
// against the repository, so removing an entry does not shrink the run — it
// turns the completeness check red.
var groupBoundStoreSites = []groupBoundSite{groupBoundMemorySite, groupBoundSQLSite}

// TestGroupMemberBoundInvariant enforces Spec 017 §3.5 for every store in
// groupBoundStoreSites. sql takes the same default under the same Aggregator
// with the same WithCompletionSize as memory, so it carries the identical
// silent-deadlock risk and gets its own case.
func TestGroupMemberBoundInvariant(t *testing.T) {
	t.Parallel()

	// Half 1 generates its cases from groupBoundStoreSites, so an EMPTIED
	// slice would give zero cases, zero subtests and a green PASS. Half 2
	// would catch that, but half 1 must not depend on half 2 to be non-vacuous.
	require.NotEmpty(t, groupBoundStoreSites,
		"groupBoundStoreSites is empty, so this test would run ZERO cases and pass vacuously. "+
			"Both first-party GroupStores belong in it (Spec 017 §3.5).")

	root, err := os.Getwd()
	require.NoError(t, err, "a root-package test's working directory is the repository root")

	ceiling := mustGroupBoundConst(t, root, groupBoundCeilingSite)

	defaults := make([]int, len(groupBoundStoreSites))
	wirings := make([][]groupBoundWiringSite, len(groupBoundStoreSites))
	rendered := []string{fmt.Sprintf("%s = %d", groupBoundCeilingSite, ceiling)}
	for i, site := range groupBoundStoreSites {
		defaults[i] = mustGroupBoundConst(t, root, site)
		wirings[i] = mustGroupBoundWiring(t, root, site)
		rendered = append(rendered, fmt.Sprintf("%s = %d", site, defaults[i]))
	}

	inventory := "AST-read values: " + strings.Join(rendered, " | ")
	t.Log(inventory)

	type testCase struct {
		name         string
		site         groupBoundSite
		storeDefault int
		wiring       []groupBoundWiringSite
		assert       func(t *testing.T, site groupBoundSite, storeDefault int, wiring []groupBoundWiringSite)
	}

	holds := func(t *testing.T, site groupBoundSite, storeDefault int, wiring []groupBoundWiringSite) {
		t.Helper()

		// The two halves of "the default is correct": the constant holds a
		// legal value, AND the constructor actually installs it. Neither
		// implies the other — a one-token edit to `maxGroupMembers: 1024`
		// leaves the constant declared, correct and entirely DEAD, and the
		// `unused` linter is deliberately off in .golangci.yml. Both are
		// asserted, and both report, so a failure names which half broke.
		groupBoundAssertWired(t, site, wiring)

		require.GreaterOrEqualf(t, storeDefault, ceiling,
			"Spec 017 §3.5 INVARIANT VIOLATED: %s = %d is below %s = %d.\n%s\n"+
				"A caller may legally configure routing.WithCompletionSize up to %d, and a store "+
				"default below that rejects the group's own completing member before the release "+
				"predicate can fire — a silent deadlock in place of a bound. Raise the default, or "+
				"lower the ceiling; do not weaken this gate.",
			site, storeDefault, groupBoundCeilingSite, ceiling, inventory, ceiling)
	}

	cases := make([]testCase, 0, len(groupBoundStoreSites))
	for i, site := range groupBoundStoreSites {
		cases = append(cases, testCase{
			name:         site.file + " default admits any legal completion size",
			site:         site,
			storeDefault: defaults[i],
			wiring:       wirings[i],
			assert:       holds,
		})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.assert(t, tc.site, tc.storeDefault, tc.wiring)
		})
	}
}

// TestGroupMemberBoundStoreSitesAreComplete is half 2: the set of files
// declaring `const defaultMaxGroupMembers` anywhere in the repository must
// equal groupBoundStoreSites EXACTLY, in both directions. A site whose
// declaration no longer exists fails it, and — the mutant this test exists for
// — a store DELETED from groupBoundStoreSites fails it too, instead of quietly
// leaving TestGroupMemberBoundInvariant running against one store.
func TestGroupMemberBoundStoreSitesAreComplete(t *testing.T) {
	t.Parallel()

	root, err := os.Getwd()
	require.NoError(t, err, "a root-package test's working directory is the repository root")

	found := scanGroupBoundStoreDecls(t, root)
	sort.Strings(found)

	want := make([]string, 0, len(groupBoundStoreSites))
	for _, site := range groupBoundStoreSites {
		want = append(want, site.key())
	}
	sort.Strings(want)

	t.Logf("=== files declaring `const %s`, discovered by walking %s: %d\n\t%s",
		groupBoundStoreConstName, root, len(found), strings.Join(found, "\n\t"))

	// Non-vacuity: two empty sets compare equal. groupBoundStoreSites is a
	// non-empty literal, so `want` can never be empty and a discovery that
	// found nothing would fail here rather than pass — but assert the floor
	// explicitly so that stays true if the slice is ever built dynamically.
	// (Measured: with this line deleted, a walk forced to find nothing still
	// fails on the set equality below. It is a floor, not the defence.)
	require.NotEmpty(t, found,
		"the completeness walk found NO file declaring `const %s` anywhere under %s. That is a "+
			"broken traversal, not a clean tree — both first-party GroupStores declare it.",
		groupBoundStoreConstName, root)

	assert.Equal(t, want, found,
		"the set of files declaring `const %s` must match groupBoundStoreSites in BOTH directions "+
			"(Spec 017 §3.5). A store MISSING from the found set means its declaration was renamed or "+
			"removed. A store missing from groupBoundStoreSites means the invariant gate silently "+
			"shrank to the other store — which is the exact failure this test exists to make red, "+
			"because TestGroupMemberBoundInvariant generates its cases from that slice and would "+
			"otherwise pass with one subtest. Add the store back; do not edit the slice to match.",
		groupBoundStoreConstName)
}

// mustGroupBoundConst reads one asserted site, failing the test loudly and BY
// NAME when the file or the declaration is missing. This is the non-vacuity
// guard: without it a renamed constant would yield a zero value and the
// comparison would pass — proven load-bearing by renaming the CEILING, which
// makes the invariant read 65536 >= 0 and pass in its absence.
func mustGroupBoundConst(t *testing.T, root string, site groupBoundSite) int {
	t.Helper()

	value, err := groupBoundConstValue(root, site.file, site.name)
	require.NoErrorf(t, err,
		"THE GROUP-MEMBER BOUND GATE COULD NOT READ %s. Its parse set is ASSERTED, not "+
			"discovered — every one of [%s] must be present and must declare its constant. "+
			"A missing file or a renamed constant is a FAILURE, never a shorter run: the value "+
			"would otherwise default to 0 and the invariant could pass vacuously. Restore the "+
			"declaration, or update this gate deliberately (Spec 017 §3.5).",
		site, strings.Join(groupBoundAssertedFiles(), ", "))

	return value
}

// groupBoundAssertedFiles lists every file this gate's asserted parse set
// covers. It is DERIVED from the sites, so adding a third store to
// groupBoundStoreSites does not leave a stale hand-written list behind.
func groupBoundAssertedFiles() []string {
	files := []string{groupBoundCeilingSite.file}
	for _, site := range groupBoundStoreSites {
		files = append(files, site.file)
	}
	return files
}

// mustGroupBoundWiring returns every place site.file sets the
// groupBoundStoreFieldName field, failing loudly when the file cannot be read
// or parsed.
func mustGroupBoundWiring(t *testing.T, root string, site groupBoundSite) []groupBoundWiringSite {
	t.Helper()

	wiring, err := groupBoundWiringSites(root, site.file)
	require.NoErrorf(t, err,
		"THE GROUP-MEMBER BOUND GATE COULD NOT PARSE %s to check how its %s field is wired.",
		site.file, groupBoundStoreFieldName)

	return wiring
}

// groupBoundAssertWired is F-1's fix: the gate must assert the WIRING, not
// just the number. Comparing three literals in source proves nothing on its
// own, because nothing in that comparison ties the declared constant to the
// value a constructor installs. Changing one token —
//
//	maxGroupMembers: defaultMaxGroupMembers   →   maxGroupMembers: 1024
//
// leaves the constant declared, correct, compared by this gate, and DEAD,
// while the store's real default silently becomes 1024 against a ceiling of
// 65,536: precisely the deadlock this gate exists to prevent. `golangci-lint`
// cannot catch it either — .golangci.yml deliberately enables only govet,
// staticcheck, ineffassign and misspell, so `unused` never runs.
//
// It also re-asserts ADR 0033 D-AR mechanically: the default must be a NAMED
// constant, not the bare literal D-AR deviated from (`maxGroups: 1024`, one
// field over in the very same composite literal).
func groupBoundAssertWired(t *testing.T, site groupBoundSite, wiring []groupBoundWiringSite) {
	t.Helper()

	for _, w := range wiring {
		if w.ident == site.name {
			return
		}
	}

	found := "(no site in this file sets the field at all)"
	if len(wiring) > 0 {
		rendered := make([]string, 0, len(wiring))
		for _, w := range wiring {
			rendered = append(rendered, "\t"+w.String())
		}
		found = strings.Join(rendered, "\n")
	}

	assert.Failf(t, "the default constant is declared but NOT WIRED",
		"%s declares %s, and this gate compares its value — but NOTHING in that file installs the "+
			"%s field FROM it, so the constant is dead and the store's real default is whatever the "+
			"sites below install. Every place %s sets %s:\n%s\n"+
			"Expected one of them to be the bare identifier %q (Spec 017 §3.5; ADR 0033 D-AR — the "+
			"default must be a NAMED constant, not a bare literal). Note that `golangci-lint` will "+
			"NOT flag the dead constant: .golangci.yml deliberately leaves `unused` off.",
		site.file, site.name, groupBoundStoreFieldName, site.file, groupBoundStoreFieldName, found, site.name)
}

// groupBoundWiringSites returns every place the file at root/rel sets the
// groupBoundStoreFieldName field, in either of the two shapes Go allows:
// a composite-literal key (`groupStoreConfig{maxGroupMembers: X}`) and an
// assignment to the field (`cfg.maxGroupMembers = X`).
//
// BOTH shapes are collected DELIBERATELY, and the assertion is "at least one
// site installs the constant" rather than "the groupStoreConfig literal's key
// is the constant". Anchoring on the composite literal alone would red on a
// legitimate refactor that split the constructor into
//
//	cfg := groupStoreConfig{…}
//	cfg.maxGroupMembers = defaultMaxGroupMembers
//
// or that renamed the config type — a gate whose failure mode is a false
// positive gets deleted by the first person to hit it (this plan's own R4-4
// lesson). Collecting both shapes cannot weaken the mutants: the option
// setter `c.maxGroupMembers = n` contributes a site whose value is `n`, which
// never satisfies the assertion, and every F-1 mutant removes the only site
// that does.
//
// RESIDUAL, measured and accepted rather than left implicit: "at least one
// site in this file" is file-scoped, not reachability-scoped. A DEAD function
// assigning the field from the constant, while the live constructor installs a
// bare literal, passes this gate — verified. That is accepted deliberately,
// because the alternative (anchoring on the constructor's own literal) is the
// shape proven above to false-red on a legitimate refactor, and a gate whose
// failure mode is a false positive gets deleted. The gap it leaves needs
// someone to write a dead helper that installs the constant; the hole it
// closes was a ONE-TOKEN edit. Sized accordingly.
func groupBoundWiringSites(root, rel string) ([]groupBoundWiringSite, error) {
	file, err := groupBoundParseFile(root, rel)
	if err != nil {
		return nil, err
	}

	var sites []groupBoundWiringSite
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			typeName := "(untyped composite literal)"
			if node.Type != nil {
				typeName = types.ExprString(node.Type)
			}
			for _, elt := range node.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == groupBoundStoreFieldName {
					sites = append(sites, newGroupBoundWiringSite(
						fmt.Sprintf("%s{… %s: %s}", typeName, groupBoundStoreFieldName, types.ExprString(kv.Value)),
						kv.Value))
				}
			}

		case *ast.AssignStmt:
			if len(node.Lhs) != len(node.Rhs) {
				return true
			}
			for i, lhs := range node.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != groupBoundStoreFieldName {
					continue
				}
				sites = append(sites, newGroupBoundWiringSite(
					fmt.Sprintf("%s %s %s", types.ExprString(lhs), node.Tok, types.ExprString(node.Rhs[i])),
					node.Rhs[i]))
			}
		}
		return true
	})

	return sites, nil
}

// newGroupBoundWiringSite records one field-set, keeping the site as written
// for the failure message and — separately — the value's identifier name,
// which is empty unless the value is a BARE identifier. A literal (1024), a
// qualified name (pkg.X) and a call all leave ident empty and so can never
// satisfy the assertion.
func newGroupBoundWiringSite(text string, value ast.Expr) groupBoundWiringSite {
	site := groupBoundWiringSite{text: text}
	if ident, ok := value.(*ast.Ident); ok {
		site.ident = ident.Name
	}
	return site
}

// groupBoundConstValue parses the single file at root/rel and returns the
// integer value of the constant declared as name. It reports an error — never
// a usable zero — when the file cannot be read or parsed, when name is not
// declared as a constant in it, or when the declared value is not an integer
// constant expression this gate can evaluate.
func groupBoundConstValue(root, rel, name string) (int, error) {
	file, err := groupBoundParseFile(root, rel)
	if err != nil {
		return 0, err
	}

	expr, ok := groupBoundConstExpr(file, name)
	if !ok {
		return 0, fmt.Errorf("const %s is not declared in %s", name, rel)
	}

	n, err := groupBoundEvalInt(expr)
	if err != nil {
		return 0, fmt.Errorf("const %s in %s: %w", name, rel, err)
	}
	return n, nil
}

// scanGroupBoundStoreDecls walks root — a root-package test's os.Getwd() is
// the repository root, so this reaches all 8 modules without go.work; the
// traversal is scanSizingParamRepo's, skipping dot-directories, vendor and
// _test.go files — and returns the site key of every non-test file declaring
// groupBoundStoreConstName.
func scanGroupBoundStoreDecls(t *testing.T, root string) []string {
	t.Helper()

	var found []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if path != root && (strings.HasPrefix(base, ".") || base == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		src, rderr := os.ReadFile(path)
		if rderr != nil {
			return rderr
		}
		file, perr := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", rel, perr)
		}
		if _, ok := groupBoundConstExpr(file, groupBoundStoreConstName); ok {
			found = append(found, groupBoundSite{file: rel, name: groupBoundStoreConstName}.key())
		}
		return nil
	})
	require.NoError(t, err, "walking %s", root)

	return found
}

// groupBoundParseFile reads and parses one repository-relative file.
func groupBoundParseFile(root, rel string) (*ast.File, error) {
	src, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}

	file, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", rel, err)
	}
	return file, nil
}

// groupBoundConstExpr returns the expression file assigns to the constant
// name, if file declares it as a constant at all.
func groupBoundConstExpr(file *ast.File, name string) (ast.Expr, bool) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, ident := range vs.Names {
				if ident.Name == name && i < len(vs.Values) {
					return vs.Values[i], true
				}
			}
		}
	}
	return nil, false
}

// groupBoundEvalInt evaluates an untyped integer constant expression from the
// AST: the `1 << 16` *ast.BinaryExpr all three constants use today, and a
// plain integer literal, so that rewriting `1 << 16` as `65536` keeps this
// gate working rather than turning it into a false positive. Anything else is
// an error naming the shape, not a silent zero.
func groupBoundEvalInt(expr ast.Expr) (int, error) {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return groupBoundEvalInt(e.X)

	case *ast.BasicLit:
		if e.Kind != token.INT {
			return 0, fmt.Errorf("value %s is a %s literal, not an integer", e.Value, e.Kind)
		}
		n, err := strconv.ParseInt(e.Value, 0, 64)
		if err != nil {
			return 0, fmt.Errorf("value %s: %w", e.Value, err)
		}
		return int(n), nil

	case *ast.BinaryExpr:
		if e.Op != token.SHL {
			return 0, fmt.Errorf("operator %s is not one this gate evaluates", e.Op)
		}
		lhs, err := groupBoundEvalInt(e.X)
		if err != nil {
			return 0, err
		}
		rhs, err := groupBoundEvalInt(e.Y)
		if err != nil {
			return 0, err
		}
		return lhs << rhs, nil
	}

	return 0, fmt.Errorf("value is a %T, not an integer constant expression this gate evaluates", expr)
}
