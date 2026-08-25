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
// THREE HALVES, each existing because the one before it is not enough:
//
//  1. INVARIANT (TestGroupMemberBoundInvariant). Read the ceiling and each
//     store's default out of the AST and assert default >= ceiling per store.
//     The cases are GENERATED from groupBoundStoreSites, so a store cannot be
//     dropped from the run without being dropped from that slice.
//  2. LEXICAL COMPLETENESS (TestGroupMemberBoundStoreSitesAreComplete). Walk
//     every non-test .go file in the repository and assert that the set of
//     files declaring `const defaultMaxGroupMembers` equals
//     groupBoundStoreSites EXACTLY, in both directions. Without this, deleting
//     a store from the slice leaves a vet-clean, lint-clean test that PASSES
//     with one subtest — the gate silently shrinking to one store while the
//     other carries the identical risk. That is this increment's own "fix the
//     class, not the instance" lesson reappearing one level up, inside the
//     gate's structure.
//  3. STRUCTURAL COMPLETENESS (TestGroupMemberBoundStoreImplementationsAreComplete).
//     Half 2 keys on a LITERAL IDENTIFIER, so it can only see a store that
//     already spells its default `defaultMaxGroupMembers`. Half 3 keys on the
//     property instead: discover every first-party type whose METHOD SET
//     covers msgin.MessageGroupStore, and assert the set of packages declaring
//     one equals the packages in groupBoundStoreSites. A new store is then
//     caught by what it IS, not by what it happens to name a constant.
//
// Discovery is a check ON the asserted list, not a replacement FOR it. Half 1
// never reads a discovered file.
//
// WHY HALF 3 EXISTS — Plan 031 review R-14, measured in both directions before
// it was written. Planting `const defaultMaxGroupMembers` in a leaf module
// correctly turned half 2 red; planting the SAME constant under the name
// `defaultMaxMembersPerGroup` left the whole root package GREEN. Renaming an
// EXISTING constant was caught; adding a NEW store under a new spelling was
// not — and the planned pgx, redis and nats adapters each grow a GroupStore.
// The residual was recorded in Spec 017 §6 AC-3.3 and ADR 0033 D-AQ but not in
// this file, which otherwise records every limitation it has. It is now closed
// rather than merely disclosed; what half 3 does NOT reach is stated on the
// test itself.
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
	"path"
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

// groupBoundSPISite names the interface half 3 derives its discriminator from.
// The method set is READ, never transcribed: when MessageGroupStore gains a
// method, the discriminator tightens with it and no list here goes stale.
var groupBoundSPISite = groupBoundSite{file: "groupstore.go", name: "MessageGroupStore"}

// TestGroupMemberBoundStoreImplementationsAreComplete is half 3: every
// first-party package that implements msgin.MessageGroupStore must be one of
// the packages in groupBoundStoreSites, and every package in
// groupBoundStoreSites must implement it — in BOTH directions.
//
// This is R-14's narrowing. Half 2 asks "which files declare the constant?",
// which a new store can answer with silence simply by choosing another
// spelling. Half 3 asks "which types ARE a MessageGroupStore?", which a new
// store cannot answer with silence at all: implementing the SPI is the whole
// point of writing one. Failing here tells the author to declare the canonical
// constant and add the store to groupBoundStoreSites, which then subjects it
// to half 1's invariant.
//
// THE DISCRIMINATOR IS WIDE-MARGINED, not a coincidence of naming. It requires
// ALL SEVEN method names of the SPI on ONE receiver type. Measured over the
// whole repository: exactly two types match, and the nearest non-match reaches
// 3 of 7 — the sql GroupDialect family, which shares ClaimGroup/SettleGroup/
// AbandonGroup but has AddMember, ExpiredGroups and no RecoverInterval or
// EmitsLiveValue. There is no near-miss anywhere in the tree, so this cannot
// false-red on an unrelated type, which is the failure mode that gets a gate
// deleted (this plan's own R4-4 lesson).
//
// RESIDUALS, measured and stated rather than papered over (ADR 0033 D-AL):
//
//   - EMBEDDING. The walk sees `func (T) Add(...)` declarations, so a store
//     that acquires part of its method set by embedding another type declares
//     fewer than seven and is invisible. Verified. Closing it needs a full
//     type-check of eight modules, which this gate deliberately does not do —
//     it parses, exactly as sizing_option_class_gate_test.go does.
//   - GENERATED METHODS. A store whose methods are produced by `go:generate`
//     into a file that is not committed is invisible until it is committed.
//   - OUT-OF-TREE STORES. A third-party MessageGroupStore in someone else's
//     module is not this gate's business; the SPI godoc carries the bound's
//     contract for them.
//
// Both remaining in-tree holes need a store assembled indirectly. The hole
// R-14 reported — a store written the ordinary way under a different constant
// name — is closed.
func TestGroupMemberBoundStoreImplementationsAreComplete(t *testing.T) {
	t.Parallel()

	root, err := os.Getwd()
	require.NoError(t, err, "a root-package test's working directory is the repository root")

	spi := mustGroupBoundSPIMethods(t, root)
	t.Logf("=== msgin.%s method set, read from %s: %d methods\n\t%s",
		groupBoundSPISite.name, groupBoundSPISite.file, len(spi), strings.Join(spi, ", "))

	// Non-vacuity: an empty or truncated method set would make EVERY type a
	// match (a set trivially covers the empty set), turning this gate into
	// noise rather than silence — the opposite failure, but a failure. The
	// floor is deliberately loose: it asserts the SPI is substantial, not that
	// it has exactly seven methods, so adding one does not red this line.
	require.GreaterOrEqual(t, len(spi), 5,
		"the discriminator read only %d method(s) from msgin.%s. A short method set matches types that "+
			"are not GroupStores at all; a set of zero matches everything. Either the interface moved or "+
			"the parse is broken — do not lower this floor.", len(spi), groupBoundSPISite.name)

	found := scanGroupBoundStoreImpls(t, root, spi)
	sort.Strings(found)

	want := make([]string, 0, len(groupBoundStoreSites))
	for _, site := range groupBoundStoreSites {
		want = append(want, path.Dir(site.file))
	}
	sort.Strings(want)

	t.Logf("=== packages declaring a type whose method set covers the SPI: %d\n\t%s",
		len(found), strings.Join(found, "\n\t"))

	require.NotEmpty(t, found,
		"the structural walk found NO type implementing msgin.%s anywhere under %s. Both first-party "+
			"GroupStores do — that is a broken traversal, not a clean tree.",
		groupBoundSPISite.name, root)

	assert.Equal(t, want, found,
		"the set of PACKAGES implementing msgin.%s must match the packages in groupBoundStoreSites in "+
			"BOTH directions (Spec 017 §3.5; ADR 0033 D-AQ).\n"+
			"A package in the found set but NOT in groupBoundStoreSites is a first-party GroupStore that "+
			"escaped this gate — most likely a NEW store that spells its member-cap default something "+
			"other than %q, which half 2 cannot see by construction (Plan 031 review R-14). Give it a "+
			"`const %s`, wire the constructor from it, and add its file to groupBoundStoreSites so half 1 "+
			"checks its value against routing's completion-size ceiling.\n"+
			"A package in groupBoundStoreSites but NOT in the found set means the store stopped "+
			"implementing the SPI, or was moved — update the slice deliberately, do not delete the entry "+
			"to make this pass.",
		groupBoundSPISite.name, groupBoundStoreConstName, groupBoundStoreConstName)
}

// mustGroupBoundSPIMethods reads the method names msgin.MessageGroupStore
// declares, failing loudly rather than returning an empty set — an empty
// discriminator would match every type in the repository.
func mustGroupBoundSPIMethods(t *testing.T, root string) []string {
	t.Helper()

	methods, err := groupBoundInterfaceMethods(root, groupBoundSPISite.file, groupBoundSPISite.name)
	require.NoErrorf(t, err,
		"THE GROUP-MEMBER BOUND GATE COULD NOT READ the %s interface from %s, so it cannot tell a "+
			"GroupStore from any other type. Restore the declaration, or point groupBoundSPISite at "+
			"wherever the SPI moved to.",
		groupBoundSPISite.name, groupBoundSPISite.file)

	return methods
}

// groupBoundInterfaceMethods returns the explicitly-declared method names of
// the named interface in root/rel. Embedded interfaces contribute no names —
// they are *ast.Field entries with no Names — which only ever makes the
// discriminator narrower, never wider, so it cannot cause a false positive.
func groupBoundInterfaceMethods(root, rel, name string) ([]string, error) {
	file, err := groupBoundParseFile(root, rel)
	if err != nil {
		return nil, err
	}

	var methods []string
	var found bool
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Name.Name != name {
			return true
		}
		iface, ok := spec.Type.(*ast.InterfaceType)
		if !ok {
			return true
		}
		found = true
		for _, field := range iface.Methods.List {
			for _, ident := range field.Names {
				methods = append(methods, ident.Name)
			}
		}
		return false
	})

	if !found {
		return nil, fmt.Errorf("interface %s is not declared in %s", name, rel)
	}
	sort.Strings(methods)
	return methods, nil
}

// scanGroupBoundStoreImpls walks root — the same traversal half 2 uses — and
// returns the repository-relative directory of every package declaring a type
// whose METHOD SET covers spi. Directories are deduplicated, so a store split
// across several files in one package is reported once.
//
// Methods are keyed by (package directory, receiver type name) because Go
// requires a method to live in its receiver's package: accumulating across the
// directory is exactly the language's own rule, not an approximation.
func scanGroupBoundStoreImpls(t *testing.T, root string, spi []string) []string {
	t.Helper()

	// dir -> receiver type -> declared method names
	methods := make(map[string]map[string]map[string]bool)
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
		dir := filepath.ToSlash(filepath.Dir(rel))
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			recv := groupBoundReceiverName(fn.Recv.List[0].Type)
			if recv == "" {
				continue
			}
			if methods[dir] == nil {
				methods[dir] = make(map[string]map[string]bool)
			}
			if methods[dir][recv] == nil {
				methods[dir][recv] = make(map[string]bool)
			}
			methods[dir][recv][fn.Name.Name] = true
		}
		return nil
	})
	require.NoError(t, err, "walking %s", root)

	seen := make(map[string]bool)
	var found []string
	for dir, types := range methods {
		for _, declared := range types {
			if !groupBoundCoversSPI(declared, spi) || seen[dir] {
				continue
			}
			seen[dir] = true
			found = append(found, dir)
		}
	}
	return found
}

// groupBoundCoversSPI reports whether declared contains every name in spi.
func groupBoundCoversSPI(declared map[string]bool, spi []string) bool {
	for _, name := range spi {
		if !declared[name] {
			return false
		}
	}
	return true
}

// groupBoundReceiverName unwraps a receiver expression to the bare type name,
// through the pointer and generic-instantiation forms Go allows: T, *T, T[P]
// and *T[P, Q]. Anything else yields "" and is skipped rather than guessed at.
func groupBoundReceiverName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return groupBoundReceiverName(e.X)
	case *ast.IndexExpr:
		return groupBoundReceiverName(e.X)
	case *ast.IndexListExpr:
		return groupBoundReceiverName(e.X)
	case *ast.Ident:
		return e.Name
	}
	return ""
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
