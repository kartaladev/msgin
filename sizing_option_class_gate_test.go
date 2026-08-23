package msgin_test

// Spec: 016 (AC-5, AC-6)  Plan: 029 (Task 7)  ADR: 0032
//
// The class gate for sizing options: the test that stops Spec 016's whole
// defect class (a sizing option validating only its lower bound) from coming
// back silently when a future option is added. Modeled on the shipped
// option_guard_gate_test.go — same root-blackbox placement, same os.Getwd()
// walk reaching all 8 modules without go.work, stdlib + testify only.
//
// Two halves (ADR 0032 D-AA):
//
//  1. COMPLETENESS (AST). Walk every non-test .go file from os.Getwd() and
//     collect every exported FUNCTION (Recv == nil — see the boundary below)
//     with an int/int64 parameter in ANY position (not just a first
//     "func With..." parameter — resilience.NewTokenBucket's positional
//     `burst` is why). Fail if that set differs from sizingConformanceKeys in
//     EITHER direction.
//  2. CONFORMANCE (behavioral). Every one of the 18 AST-discovered keys, plus
//     2 MANUAL rows for the class members the Recv == nil boundary excludes
//     but a root test can still construct, gets an executable row — never a
//     declaration string — in one of FOUR arms. The arms are BEHAVIORAL and are
//     NOT a relabelling of Spec 016 §2.1's three classification verdicts; §2.1's
//     "classification arms ≠ AC-5 behavioral arms" note is normative for the
//     split, and §6 AC-5 tabulates it:
//       - "fixed"   (13) — the fault is reported through the surface Spec 016
//                          §3 names for it: the constructor's return, or the
//                          first use of the object it produced. 9 were bounded
//                          by Spec 016 / Plan 029; memory.WithMaxGroupMembers
//                          by Spec 017 / Plan 031; the 3 msghttp byte caps
//                          joined them from "deferred" at Spec 018 / Plan 032.
//       - "rejects"  (1) — msghttp.WithSuccessStatus. It is safe (a) by §2.1's
//                          criterion and NOTHING HERE FIXES IT; it rejects
//                          1<<30 only through its own pre-existing [100,599]
//                          check. It gets an arm of its own because it belongs
//                          in neither "fixed" (not a class member) nor "safe"
//                          (which asserts ACCEPTS).
//       - "deferred" (0) — no members as of Plan 032; see Spec 018. The arm is
//                          retained so a future knob with a genuinely deferred
//                          remedy has it, and so this file keeps documenting
//                          four arms rather than silently dropping the
//                          vocabulary. NOTE: byArm below is built by COUNTING,
//                          so an empty arm has NO KEY there — it is absent,
//                          not zero.
//       - "safe"     (6) — accepts math.MaxInt AND its product is usable.
//     13 + 1 + 6 = 20 rows = 18 AST keys + 2 manual rows.
//
// # THE OVERSIZED LITERAL IS TWO-DIMENSIONAL (Plan 030 Task 2; Plan 032)
//
// Every row above previously passed the literal 1<<62, which does not fit an
// int on GOARCH=386 and made this whole test binary fail to COMPILE there.
// The replacement is NOT "one value per arm" — a blanket rewrite to one value
// would leave rows green while probing nothing. Read it in two dimensions, in
// this order:
//
// DIMENSION 1 — THE ARM FIXES THE REQUIRED PROPERTY.
//
//   - "safe" (6): the value must be ACCEPTED and must stay MAXIMALLY absurd ⇒
//     math.MaxInt, and nothing else. The parameter type does NOT get a vote
//     here — all six of these rows are int-typed, and demoting them to a
//     reject-arm literal would silently disable the probe (see below).
//   - "fixed" (13) and "rejects" (1): the value must be OUT OF RANGE and must
//     render an architecture-INDEPENDENT decimal, because these rows assert an
//     EqualError against a rendered decimal. That fixed decimal is the whole
//     point; math.MaxInt here would render differently on 386 and 64-bit and
//     break the assertions.
//
// DIMENSION 2 — ONLY WITHIN THE REJECT ARMS ("fixed" / "rejects") DOES THE
// PARAMETER TYPE CHOOSE THE LITERAL.
//
//   - int-typed → 1<<30 = 1,073,741,824. It fits an int32 yet still exceeds
//     every INT-TYPED ceiling in the codebase (the largest of those is 1<<20 =
//     1,048,576), so it selects the identical out-of-range branch on both
//     architectures while keeping the expected decimal — 1073741824 —
//     architecture-INDEPENDENT.
//
//   - int64-typed → 1<<62. The three msghttp byte caps (WithMaxBodyBytes,
//     WithMaxEventBytes, WithMaxResponseBytes) are `func(n int64)`, so 1<<62
//     is in range on EVERY architecture, compiles fine on 386, and was never
//     part of that defect. 🔴 DO NOT "finish the job" by converting them to an
//     int-sized literal. 1<<30 in particular CANNOT be used for them:
//     byteCapCeiling is an int64 ceiling of 2,147,483,647, which is ABOVE
//     1<<30, so those rows would be ACCEPTED and every require.ErrorIs would
//     fail. And on 386 math.MaxInt would shrink the value to exactly
//     2,147,483,647 — the ceiling itself, hence in range — leaving the row
//     green while asserting nothing.
//
// AND THE WARNING DIMENSION 2 DOES NOT REACH — CARRIED FORWARD VERBATIM:
//
//   - "safe" (6) → math.MaxInt. These rows assert require.NoError plus a
//     product-usable check and carry NO decimal string, so architecture
//     dependence is harmless — and the value must stay MAXIMALLY absurd,
//     because that is the row's entire purpose (see the arm's comment at the
//     "safe" section: the knob is exercised "past the point where a buggy
//     comparison, e.g. an int32 truncation, would misbehave"). 1<<30 IS an
//     int32 value, so demoting these rows to it would leave every assertion
//     green while the int32-truncation probe silently stopped running. Worse,
//     if an implementation regressed to make([]T, n), math.MaxInt fails fast
//     whereas 1<<30 quietly allocates ~1 GiB.
//
//     ACCEPTED, RECORDED LIMITATION: on GOARCH=386 no int value exceeds int32,
//     so the int32-truncation probe these six rows exist to run is
//     UNACHIEVABLE there by any choice of magnitude. math.MaxInt keeps the
//     probe intact where it is meaningful (64-bit) and degrades to a tautology
//     where it cannot be (32-bit). Do not "fix" that by picking a smaller
//     number — that would disable the probe on BOTH architectures.
//
// # The Recv == nil boundary — a decision, not an omission (Spec 016 §2.0)
//
// In go/ast a method is a *ast.FuncDecl with a non-nil Recv, so "every
// exported function with an int/int64 parameter" reads two ways. Measured on
// this tree: Recv == nil yields 18 keys, every one constructible from a root
// blackbox test; ANY FuncDecl yields 45, of which 22 sit on UNEXPORTED
// receivers (mysqlDialect.Claim, postgresGroupDialect.AddMember, ... — 21 in
// leaf modules, plus msghttp's own responseTracker.WriteHeader) that a
// root-module blackbox test cannot construct at all, which would make half 2
// unsatisfiable for those keys. So: functions only.
//
// The exclusion costs nothing once membership is derived from a stated
// criterion (Spec 016 §2.1 / ADR 0032 D-AB: "n is the sole bound on an
// accumulation") rather than by hand: applying it to the 27 excluded methods
// yields exactly TWO class members, both constructible from root —
// memory.QueueStore.Claim (make([]msgin.Delivery, 0, min(max, len(s.ready)))
// — max is an argument to the capacity) and channel.QueueChannel.Poll, which
// delegates verbatim into it. Both get manual conformance rows below; because
// Poll delegates into Claim, one exercised chain proves both. There is no
// "named but uncovered" residue: the sql-adapter siblings that pass a
// superficially similar `max`/`limit` do NOT qualify — their make capacity is
// len(rows)/len(cands), sized by what the database returned, not by the
// parameter (which reaches only the SQL LIMIT).
//
// # Accepted limitations (ADR 0032 D-AA), stated rather than left implicit
//
//   - ROOT-MODULE IMPORT BOUNDARY. Half 1 sees all 8 modules (a filesystem
//     walk needs no go.mod); half 2 is a root-module test and can only import
//     root-module packages. All 18 keys live in root-module packages today
//     (endpoint, adapter/http, adapter/memory, channel, resilience, routing),
//     so both halves cover all of them. A sizing option added to expr, one of
//     the sql dialects, or another leaf module FAILS half 1 and cannot be
//     added to half 2 — a deliberate gate failure demanding a spec revision,
//     not a silent pass.
//   - The Recv == nil boundary above: two excluded class members, both
//     covered by the manual rows — no uncovered residue.
//   - A NAMED integer type is INVISIBLE. hasIntOrInt64Param looks through
//     `...int`, `[]int` and `*int`, but it matches the predeclared int/int64
//     by NAME on the AST, so a knob spelled `WithCap(n Bytes)` over
//     `type Bytes int64` does not register. Resolving that needs go/types
//     (the AST alone cannot tell `Bytes` from any other identifier), which
//     would make this gate depend on a loadable build of all 8 modules — the
//     exact coupling the filesystem walk exists to avoid. No such declaration
//     exists today. State it; do not assume it away.
//   - time.Duration PARAMETERS ARE OUTSIDE THE GATE BY CONSTRUCTION — the AST
//     sees time.Duration, not int64. This is deliberate, NOT a claim that
//     duration knobs are "checked" or "currently safe": the shipped
//     NewTicker/NewTimer sites each guard their duration, but that is not the
//     whole class — clock.After(d) with a variable duration is a further,
//     unaudited five sites (Spec 016 §3.7.4 / ADR 0032 D-AA). Do not read a
//     green run of THIS file as saying anything about time.Duration.

import (
	"context"
	stdsql "database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	msghttp "github.com/kartaladev/msgin/adapter/http"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/kartaladev/msgin/channel"
	"github.com/kartaladev/msgin/endpoint"
	"github.com/kartaladev/msgin/resilience"
	"github.com/kartaladev/msgin/routing"
)

// ---------------------------------------------------------------------------
// Half 1 — completeness (AST)
// ---------------------------------------------------------------------------

// sizingConformanceKeys is Spec 016 §2's conformance key set: the 17 exported
// `With...` sizing options that take an int/int64 PLUS
// resilience.NewTokenBucket's positional `burst` (§2's note — the scan is
// "any position", not "first", precisely so this one is not invisible).
//
// RE-DERIVED, not copied, from a go/ast walk of all 8 modules — two prior
// revisions of Spec 016 printed a stale count (16 under a 17-row table) that
// would have made this gate fail on its first run (Spec 016 §2, revision-2
// BLOCKER-1). Re-derive with:
//
//	git ls-files '*.go' | grep -v _test | xargs grep -hnE \
//	  '^func With[A-Za-z]+(\[[^]]*\])?\([a-z]+ (int|int64)\)' | wc -l   # → 17 `With...` options
//
// and cross-check the full 18 (including the positional burst) against
// TestSizingOptionClass_Completeness's own output below, which is the
// authoritative source — not this comment, not the spec.
var sizingConformanceKeys = []string{
	"endpoint.WithMaxInFlight",
	"endpoint.WithConcurrency",
	"endpoint.WithPollMaxBatch",
	"endpoint.WithMaxPayloadBytes",
	"memory.WithBuffer",
	"memory.WithCapacity",
	"memory.WithMaxGroups",
	"memory.WithMaxGroupMembers",
	"sql.WithMaxGroupMembers",
	"msghttp.WithConnectionBuffer",
	"msghttp.WithMaxConnections",
	"msghttp.WithReplayBuffer",
	"msghttp.WithMaxBodyBytes",
	"msghttp.WithMaxEventBytes",
	"msghttp.WithMaxResponseBytes",
	"msghttp.WithSuccessStatus",
	"routing.WithCompletionSize",
	"resilience.WithBreakerThreshold",
	"resilience.NewTokenBucket", // positional `burst`, not a `With...` option
}

// hasIntOrInt64Param reports whether ft declares an int or int64 typed
// parameter in ANY position. time.Duration is a distinct *ast.SelectorExpr
// ("time.Duration"), never an *ast.Ident named "int"/"int64", so duration
// knobs are excluded by construction, not by an explicit filter.
//
// It unwraps the three composite forms an int parameter can hide behind —
// `...int` (*ast.Ellipsis), `[]int`/`[N]int` (*ast.ArrayType) and `*int`
// (*ast.StarExpr) — because a bare-*ast.Ident match would let a knob spelled
// `WithSizes(ns ...int)` be silently invisible to half 1 while the file
// header, Spec 016 §6 AC-5 and ADR 0032 D-AA all promise the scan fails in
// EITHER direction for an int parameter in ANY position. No such declaration
// exists in the tree today (the key set is unchanged at 17 with the unwrap in
// place, which is itself the evidence), so this closes a latent hole rather
// than reclassifying anything. A NAMED type — `type Bytes int64` — remains
// invisible, and is stated as an accepted limitation in the header rather
// than silently assumed away: resolving it needs go/types, not go/ast.
func hasIntOrInt64Param(ft *ast.FuncType) bool {
	if ft.Params == nil {
		return false
	}
	for _, field := range ft.Params.List {
		if isIntOrInt64(field.Type) {
			return true
		}
	}
	return false
}

// isIntOrInt64 reports whether expr is the predeclared int/int64, looking
// through ..., slice/array and pointer wrappers. It deliberately does NOT
// follow *ast.SelectorExpr (time.Duration and every other qualified type stay
// out) or resolve named types.
func isIntOrInt64(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name == "int" || t.Name == "int64"
	case *ast.Ellipsis:
		return isIntOrInt64(t.Elt)
	case *ast.ArrayType:
		return isIntOrInt64(t.Elt)
	case *ast.StarExpr:
		return isIntOrInt64(t.X)
	}
	return false
}

// scanSizingParamRepo walks root (a root-package test's os.Getwd() is the
// repository root, so this reaches all 8 modules without go.work — identical
// traversal to the shipped option_guard_gate_test.go's scanRepo: skips
// dot-directories, vendor, and _test.go files) and returns the
// "<package>.<FuncName>" key of every exported, Recv == nil function with an
// int/int64 parameter in any position, plus a COUNT (not a list — the
// exclusion is by design, so there is nothing to compare these against) of
// exported METHODS matching the same parameter test, logged for the
// Recv == nil boundary's own sanity, never asserted against.
func scanSizingParamRepo(t *testing.T, root string) (funcs []string, methodCount int) {
	t.Helper()

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
		src, rderr := os.ReadFile(path)
		if rderr != nil {
			return rderr
		}
		f, perr := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", rel, perr)
		}
		pkg := f.Name.Name
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || !fd.Name.IsExported() || !hasIntOrInt64Param(fd.Type) {
				continue
			}
			if fd.Recv == nil {
				funcs = append(funcs, pkg+"."+fd.Name.Name)
			} else {
				methodCount++
			}
		}
		return nil
	})
	require.NoError(t, err, "walking %s", root)
	return funcs, methodCount
}

// TestSizingOptionClass_Completeness is Spec 016 §6 AC-5's half 1: the set of
// exported, Recv == nil functions with an int/int64 parameter anywhere in
// their signature must match sizingConformanceKeys EXACTLY — a sizing option
// added later without a matching conformance row fails this test, and a
// conformance row for a key that no longer exists fails it too.
func TestSizingOptionClass_Completeness(t *testing.T) {
	root, err := os.Getwd()
	require.NoError(t, err)

	found, methodCount := scanSizingParamRepo(t, root)
	sort.Strings(found)

	want := append([]string(nil), sizingConformanceKeys...)
	sort.Strings(want)

	t.Logf("=== EXPORTED FUNCTIONS (Recv==nil) with int/int64 param in ANY position: %d", len(found))
	t.Logf("=== EXPORTED METHODS with int/int64 param: %d (excluded by the Recv==nil boundary, Spec 016 §2.0)",
		methodCount)

	assert.Equal(t, want, found, "the AST-discovered set of exported sizing-shaped functions must match "+
		"Spec 016 §2's 18-key conformance set in BOTH directions (ADR 0032 D-AA). A new sizing option must be "+
		"folded into sizingConformanceKeys AND given a row in TestSizingOptionClass_Conformance — this diff "+
		"alone is not the gate, it is half of it.")

	// The Recv == nil boundary (Spec 016 §2.0) is a ratified, DO-NOT-RELITIGATE
	// decision — this assertion does not move it. It exists so a change to the
	// excluded METHOD population is noisy rather than silent (Task 8 review
	// M-2): a future method like `func (s *Foo) Resize(n int)` that sizes a
	// make() would otherwise pass this gate without anyone re-deriving Spec
	// 016 §2.0's "exactly two excluded class members, both covered by a manual
	// row" claim. 27 is re-derived, not copied, from this same scan on this
	// tree — a change here means re-deriving §2.0's boundary, not just editing
	// the number.
	require.Equal(t, 27, methodCount, "the excluded-method count moved — re-derive Spec 016 §2.0's "+
		"Recv == nil boundary (which class members, if any, the exclusion now hides) before updating this "+
		"number; do not just bump it to make the gate pass")
}

// ---------------------------------------------------------------------------
// Half 2 — conformance (behavioral)
// ---------------------------------------------------------------------------

// discardPollingSource is a minimal msgin.PollingSource used only by rows
// that never reach Poll: NewConsumer validates WithMaxInFlight/WithConcurrency
// (endpoint/consumer.go:275-290) BEFORE it resolves src's PollingSource/
// EventDrivenSource kind (:327-333), so any non-nil src reaches the
// assertion — this one never needs to actually poll.
type discardPollingSource struct{}

func (discardPollingSource) Poll(context.Context, int) ([]msgin.Delivery, error) { return nil, nil }

// onceByteSource is a minimal wire (non-live-value) msgin.PollingSource that
// delivers payload exactly once, used by the WithMaxPayloadBytes row: it must
// NOT implement LiveValueSource, so the consumer resolves the wire-decode
// path (endpoint/consumer.go's `decode`) rather than the live-value type
// assertion, which is the only path WithMaxPayloadBytes's cap guards.
type onceByteSource struct {
	once    sync.Once
	payload []byte
}

func (s *onceByteSource) Poll(context.Context, int) ([]msgin.Delivery, error) {
	var out []msgin.Delivery
	s.once.Do(func() {
		out = []msgin.Delivery{{
			Msg:  msgin.New[any](s.payload),
			Ack:  func(context.Context) error { return nil },
			Nack: func(context.Context, bool, time.Duration) error { return nil },
		}}
	})
	return out, nil
}

// ---------------------------------------------------------------------------
// sql.WithMaxGroupMembers fixtures (Spec 017 §6 AC-8, Plan 031 Task 5)
// ---------------------------------------------------------------------------
//
// msginsql.NewGroupStore takes THREE required positional arguments (a non-nil
// *sql.DB, a valid table identifier and a non-nil GroupDialect) and validates
// them BEFORE it range-checks any option value, so — unlike every other row in
// this file — the sql row cannot be driven with zero-value arguments. The two
// stubs below supply the minimum that reaches the checkRange arm under test.
// Neither is ever used: the constructor never opens a connection and never
// calls a dialect method.
//
// Deliberately NOT reordered in production to spare this file the stubs: a row
// that passed a nil db and still expected ErrInvalidCapacity would silently
// pin "the member-cap check beats the nil-db check" as a contract.

// nullConnector is a database/sql driver.Connector (and driver.Driver) that
// never connects, so stdsql.OpenDB yields a non-nil *sql.DB with no registered
// driver and no I/O. Close() stops the connectionOpener goroutine, which is
// why every use below defers it (main_test.go's goleak).
type nullConnector struct{}

func (nullConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("nullConnector: never connects")
}
func (c nullConnector) Driver() driver.Driver            { return c }
func (c nullConnector) Open(string) (driver.Conn, error) { return c.Connect(context.Background()) }

// nullGroupDialect is a msginsql.GroupDialect whose methods are never called:
// NewGroupStore only checks it for nil. It exists because GroupDialect is an
// exported SPI with no first-party implementation inside the root module — the
// three shipped dialects are separate modules that IMPORT root, so this
// (root-module, blackbox) file cannot reach them.
type nullGroupDialect struct{}

func (nullGroupDialect) AddMember(context.Context, msginsql.Querier, string, string, string, int64, []byte, []byte, int) (msginsql.GroupRows, error) {
	return msginsql.GroupRows{}, nil
}

func (nullGroupDialect) ClaimGroup(context.Context, msginsql.Querier, string, string, string, time.Duration) (*msginsql.ClaimedGroup, error) {
	return nil, nil
}

func (nullGroupDialect) SettleGroup(context.Context, msginsql.Querier, string, string, string, int64) (bool, error) {
	return false, nil
}

func (nullGroupDialect) AbandonGroup(context.Context, msginsql.Querier, string, string, string, int64) (bool, error) {
	return false, nil
}

func (nullGroupDialect) ExpiredGroups(context.Context, msginsql.Querier, string, time.Time, time.Duration, int) ([]msginsql.GroupRows, error) {
	return nil, nil
}

func (nullGroupDialect) EnsureGroupSchema(context.Context, msginsql.Querier, string) error {
	return nil
}

func (nullGroupDialect) SchemaExists(context.Context, msginsql.Querier, string) (bool, error) {
	return false, nil
}

// runAndStop starts c.Run(ctx) in its own goroutine and returns a stop func
// that cancels ctx and joins Run, so every row that starts a Consumer leaves
// no goroutine behind for main_test.go's goleak.VerifyTestMain to catch.
func runAndStop(t *testing.T, run func(ctx context.Context) error) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return after cancel")
		}
	}
}

// sizingConformanceCase is one of the 20 rows Spec 016 §6 AC-5 half 2
// requires: 18 AST-discovered keys (matching sizingConformanceKeys) plus 2
// manual rows for the Recv == nil boundary's two excluded class members.
// assert is a closure, never a want/wantErr field pair (project table-test
// rule) — each row's construction differs too much (different packages,
// different constructor shapes) to share one SUT call.
type sizingConformanceCase struct {
	key    string // matches a sizingConformanceKeys entry, or "(manual) ..." for the two excluded methods
	arm    string // "fixed" | "rejects" | "deferred" (no members as of Plan 032 — see Spec 018) | "safe" — Spec 016 §6 AC-5's four BEHAVIORAL arms, not §2.1's three classification verdicts
	assert func(t *testing.T)
}

// TestSizingOptionClass_Conformance is Spec 016 §6 AC-5's half 2: every key
// half 1 discovers is EXECUTABLE, never a declaration string (round-1 M-4).
func TestSizingOptionClass_Conformance(t *testing.T) {
	tests := []sizingConformanceCase{
		// ---- arm: fixed — the 13 class members bounded here: 9 by Spec 016 /
		// Plan 029, memory.WithMaxGroupMembers by Spec 017 / Plan 031, then the
		// 3 msghttp byte caps that moved out of "deferred" at Spec 018 /
		// Plan 032 (the three int64-typed rows at the end) ----
		{
			key: "endpoint.WithMaxInFlight",
			arm: "fixed",
			assert: func(t *testing.T) {
				h := func(context.Context, msgin.Message[any]) error { return nil }
				_, err := endpoint.NewConsumer[any](discardPollingSource{}, h, endpoint.WithMaxInFlight[any](1<<30))
				require.ErrorIs(t, err, msgin.ErrInvalidMaxInFlight)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: max in-flight out of range: endpoint.WithMaxInFlight: 1073741824 not in [1, 1048576]")
			},
		},
		{
			key: "endpoint.WithConcurrency",
			arm: "fixed",
			assert: func(t *testing.T) {
				h := func(context.Context, msgin.Message[any]) error { return nil }
				_, err := endpoint.NewConsumer[any](discardPollingSource{}, h, endpoint.WithConcurrency[any](1<<30))
				require.ErrorIs(t, err, msgin.ErrInvalidConcurrency)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: concurrency out of range: endpoint.WithConcurrency: 1073741824 not in [1, 65536]")
			},
		},
		{
			key: "msghttp.WithConnectionBuffer",
			arm: "fixed",
			assert: func(t *testing.T) {
				_, err := msghttp.NewConfig(msghttp.WithConnectionBuffer(1 << 30))
				require.ErrorIs(t, err, msghttp.ErrInvalidConnectionBuffer)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: connection buffer out of range: msghttp.WithConnectionBuffer: 1073741824 not in [1, 65536]")
			},
		},
		{
			key: "memory.WithBuffer",
			arm: "fixed",
			assert: func(t *testing.T) {
				// R2 (Spec 016 §3.2): New has no error return, so the fault must
				// surface at the object's first use — Send — not at New itself.
				b := memory.New(memory.WithBuffer(1 << 30))
				require.NotNil(t, b, "New must not panic")
				err := b.Send(t.Context(), msgin.New[any]("x"))
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.True(t, msgin.IsPermanent(err), "R2: latched and reported wrapped in Permanent (ADR 0031 D-V)")
				assert.EqualError(t, err,
					"msgin: permanent: msgin: capacity out of range: memory.WithBuffer: 1073741824 not in [0, 1048576]")
			},
		},
		{
			key: "memory.WithCapacity",
			arm: "fixed",
			assert: func(t *testing.T) {
				_, err := memory.NewQueueStore(memory.WithCapacity(1 << 30))
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: memory.WithCapacity: 1073741824 not in [1, 1048576]")
			},
		},
		{
			key: "memory.WithMaxGroups",
			arm: "fixed",
			assert: func(t *testing.T) {
				_, err := memory.NewGroupStore(memory.WithMaxGroups(1 << 30))
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: memory.WithMaxGroups: 1073741824 not in [1, 1048576]")
			},
		},
		{
			key: "memory.WithMaxGroupMembers",
			arm: "fixed",
			assert: func(t *testing.T) {
				_, err := memory.NewGroupStore(memory.WithMaxGroupMembers(1 << 30))
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: memory.WithMaxGroupMembers: 1073741824 not in [1, 1048576]")
			},
		},
		{
			key: "sql.WithMaxGroupMembers",
			arm: "fixed",
			assert: func(t *testing.T) {
				db := stdsql.OpenDB(nullConnector{})
				defer db.Close()
				_, err := msginsql.NewGroupStore(db, "groups", nullGroupDialect{},
					msginsql.WithMaxGroupMembers(1<<30))
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: sql.WithMaxGroupMembers: 1073741824 not in [1, 1048576]")
			},
		},
		{
			key: "msghttp.WithMaxConnections",
			arm: "fixed",
			assert: func(t *testing.T) {
				_, err := msghttp.NewConfig(msghttp.WithMaxConnections(1 << 30))
				require.ErrorIs(t, err, msghttp.ErrInvalidMaxConnections)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: max connections out of range: msghttp.WithMaxConnections: 1073741824 not in [1, 65536]")
			},
		},
		{
			key: "routing.WithCompletionSize",
			arm: "fixed",
			assert: func(t *testing.T) {
				store, err := memory.NewGroupStore()
				require.NoError(t, err)
				fn := func(context.Context, []msgin.Message[int]) (msgin.Message[int], error) {
					return msgin.New(0), nil
				}
				_, err = routing.NewAggregator[int, int](store, fn, routing.WithCompletionSize(1<<30))
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: routing.WithCompletionSize: 1073741824 not in [1, 65536]")
			},
		},
		{
			key: "msghttp.WithReplayBuffer",
			arm: "fixed",
			assert: func(t *testing.T) {
				_, err := msghttp.NewConfig(msghttp.WithReplayBuffer(1 << 30))
				require.ErrorIs(t, err, msghttp.ErrInvalidReplayBuffer)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: replay buffer out of range: msghttp.WithReplayBuffer: 1073741824 not in [1, 65536]")
			},
		},

		// The three int64-typed byte caps, moved here from the "deferred" arm
		// by Spec 018 / ADR 0034 / Plan 032. Each is the sole bound on a
		// REMOTE-PEER-DRIVEN read retained in memory; each is now bounded at
		// byteCapCeiling = math.MaxInt32 = 2,147,483,647 — a REPRESENTABILITY
		// ceiling (the largest cap exactly expressible as a []byte length on
		// every GOARCH), not a guess about the caller's payload, which is what
		// CLAUDE.md's Sensible-defaults gate forbids and why Spec 016 §3.8
		// deferred them.
		//
		// 🔴 THEY KEEP THE 1<<62 LITERAL — see the file header's dimension 2.
		// 1<<30 is BELOW byteCapCeiling and would be ACCEPTED, turning every
		// require.ErrorIs below into a failure; on 386, math.MaxInt lands
		// exactly ON the ceiling and would be accepted too. The literal is
		// chosen by the PARAMETER TYPE within a reject arm, not by the arm.
		{
			key: "msghttp.WithMaxBodyBytes",
			arm: "fixed",
			assert: func(t *testing.T) {
				_, err := msghttp.NewConfig(msghttp.WithMaxBodyBytes(1 << 62))
				require.ErrorIs(t, err, msghttp.ErrInvalidMaxBodyBytes)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 4611686018427387904 not in [1, 2147483647]")
			},
		},
		{
			key: "msghttp.WithMaxEventBytes",
			arm: "fixed",
			assert: func(t *testing.T) {
				_, err := msghttp.NewConfig(msghttp.WithMaxEventBytes(1 << 62))
				require.ErrorIs(t, err, msghttp.ErrInvalidMaxEventBytes)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: max event bytes out of range: msghttp.WithMaxEventBytes: 4611686018427387904 not in [1, 2147483647]")
			},
		},
		{
			key: "msghttp.WithMaxResponseBytes",
			arm: "fixed",
			assert: func(t *testing.T) {
				_, err := msghttp.NewConfig(msghttp.WithMaxResponseBytes(1 << 62))
				require.ErrorIs(t, err, msghttp.ErrInvalidMaxResponseBytes)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: max response bytes out of range: msghttp.WithMaxResponseBytes: 4611686018427387904 not in [1, 2147483647]")
			},
		},
		// ---- arm: rejects — 1 row that is NOT a class member and is NOT fixed here ----
		// M2 (Task 7 review): this row previously sat in the arm labelled "fixed",
		// where a reader re-deriving Spec 016 §2.1 counted 10 class members against
		// the spec's 9 — the exact count drift that spec fought through six
		// revisions. It is "safe (a)" by §2.1's criterion (a pure comparison over a
		// scalar; nothing accumulates), and it rejects the oversized value only through its OWN
		// pre-existing [100,599] check, which this increment did not add and does
		// not touch. It cannot sit in "safe" either, because that arm asserts the
		// knob ACCEPTS an oversized value. Hence an arm of its own — Spec 016 §6 AC-5.
		{
			key: "msghttp.WithSuccessStatus",
			arm: "rejects",
			assert: func(t *testing.T) {
				_, err := msghttp.NewConfig(msghttp.WithSuccessStatus(1 << 30))
				require.ErrorIs(t, err, msghttp.ErrInvalidStatusCode)
				assert.EqualError(t, err, "msghttp: status code must be in [100,599]")
			},
		},

		// ---- arm: deferred — NO MEMBERS as of Spec 018 / Plan 032 ----
		// TOMBSTONE, deliberately retained. The arm held 3 rows —
		// msghttp.WithMaxBodyBytes, WithMaxEventBytes and WithMaxResponseBytes,
		// each the sole bound on a REMOTE-PEER-DRIVEN read retained in memory,
		// each asserting only "accepts 1<<62" because asserting "and its
		// product is usable" would have certified an unbounded remote-driven
		// read as conformant (ADR 0032's round-5 BLOCKER-1). Spec 018 / ADR
		// 0034 bounded all three at byteCapCeiling, so all three MOVED into the
		// "fixed" arm above, keeping the 1<<62 literal.
		//
		// The name stays, and the file header still documents FOUR arms, so a
		// future knob whose remedy is genuinely deferred has an arm to sit in
		// and does not have to re-invent the vocabulary and its warnings.
		// 🔴 byArm below is built by COUNTING, so this arm has NO KEY there —
		// absent, not zero. A "deferred": 0 entry FAILS that assertion.
		//
		// 🔴 THE WARNING THAT MADE THE MOVE HAPPEN, GENERALISED AND KEPT.
		// M1 (Task 7 review) told a contributor who bounded one of these knobs
		// that the red row they met here was CORRECT, and that the repair was
		// to MOVE the row into "fixed" and rewrite its assertion to the fixed
		// shape (require.ErrorIs on the knob's sentinel + the §3.1 rendered
		// string) — never to weaken the production check back to "accepts"
		// so the gate passes. Plan 032 is that event, and it took that repair.
		// The instruction is not spent: it governs EVERY future row in this
		// file. Whenever bounding a knob turns a row red, the repair is to move
		// the row and rewrite its assertion. Do not delete the assertion, and
		// do not relax the bound — a red row here means the remedy landed, not
		// that it broke.

		// ---- arm: safe — 4 AST rows + 2 manual rows ----
		// Each accepts math.MaxInt AND its product is proven usable, not merely
		// constructed: a comparison-only knob is exercised past the point where a
		// buggy comparison (e.g. an int32 truncation) would misbehave.
		//
		// math.MaxInt, NOT any reject-arm literal (Plan 030 Task 2; Plan 032 —
		// since the three int64-typed byte caps joined "fixed", the reject arms
		// carry TWO literals, 1<<30 for their 10 int-typed rows and 1<<62 for
		// their 3 int64-typed ones, so naming just one of them here would go
		// stale): 1<<30 IS an int32 value, so it would leave every
		// require.NoError below green while the int32-truncation probe stopped
		// running. See the file header for the full two-dimensional split and
		// its accepted 32-bit limitation.
		{
			key: "endpoint.WithPollMaxBatch",
			arm: "safe",
			assert: func(t *testing.T) {
				store, err := memory.NewQueueStore()
				require.NoError(t, err)
				require.NoError(t, store.Enqueue(t.Context(), msgin.New[any]("a")))
				require.NoError(t, store.Enqueue(t.Context(), msgin.New[any]("b")))
				qc, err := channel.NewQueueChannel(store)
				require.NoError(t, err)

				var mu sync.Mutex
				seen := 0
				h := func(context.Context, msgin.Message[any]) error {
					mu.Lock()
					seen++
					mu.Unlock()
					return nil
				}
				c, err := endpoint.NewConsumer[any](qc, h,
					endpoint.WithPollMaxBatch[any](math.MaxInt),
					endpoint.WithPollInterval[any](time.Millisecond))
				require.NoError(t, err, "accepts math.MaxInt — derivatively safe: held is bounded by "+
					"min(pollMaxBatch, free credits), and free credits are maxInFlight's own ceiling (Spec 016 §2.1)")

				stop := runAndStop(t, c.Run)
				defer stop()
				require.Eventually(t, func() bool {
					mu.Lock()
					defer mu.Unlock()
					return seen == 2
				}, 2*time.Second, time.Millisecond,
					"product usable: both queued messages must still be delivered under a math.MaxInt poll-batch cap")
			},
		},
		{
			key: "resilience.WithBreakerThreshold",
			arm: "safe",
			assert: func(t *testing.T) {
				b, err := resilience.NewCircuitBreaker(resilience.WithBreakerThreshold(math.MaxInt))
				require.NoError(t, err, "accepts math.MaxInt — the option silently ignores n < 1 and never rejects "+
					"(Spec 016 §2.1 safety cause (a): a pure comparison over a scalar counter)")
				for range 1000 {
					b.Record(false)
				}
				assert.True(t, b.Allow(),
					"product usable: 1,000 consecutive failures must not trip a breaker whose threshold is math.MaxInt")
			},
		},
		{
			key: "endpoint.WithMaxPayloadBytes",
			arm: "safe",
			assert: func(t *testing.T) {
				src := &onceByteSource{payload: []byte("hello")}
				var mu sync.Mutex
				var got []byte
				h := func(_ context.Context, m msgin.Message[[]byte]) error {
					mu.Lock()
					got = m.Payload()
					mu.Unlock()
					return nil
				}
				c, err := endpoint.NewConsumer[[]byte](src, h,
					endpoint.WithConsumerCodec[[]byte](msgin.BytesPayloadCodec{}),
					endpoint.WithMaxPayloadBytes[[]byte](math.MaxInt),
					endpoint.WithPollInterval[[]byte](time.Millisecond))
				require.NoError(t, err, "accepts math.MaxInt — the option never validates n at all "+
					"(Spec 016 §2.1 safety cause (b): len(b) is already-materialised)")

				stop := runAndStop(t, c.Run)
				defer stop()
				require.Eventually(t, func() bool {
					mu.Lock()
					defer mu.Unlock()
					return got != nil
				}, 2*time.Second, time.Millisecond,
					"product usable: a normal payload must still be decoded and delivered under a math.MaxInt byte cap")
				mu.Lock()
				assert.Equal(t, []byte("hello"), got)
				mu.Unlock()
			},
		},
		{
			key: "resilience.NewTokenBucket",
			arm: "safe",
			assert: func(t *testing.T) {
				rl, err := resilience.NewTokenBucket(1, math.MaxInt) // burst is the 17th key, positional
				require.NoError(t, err, "accepts math.MaxInt — burst is a scalar comparison, safety cause (a)")
				require.NoError(t, rl.Wait(t.Context()),
					"product usable: a bucket that starts full at burst tokens must admit immediately")
			},
		},
		{
			key: "(manual) memory.QueueStore.Claim",
			arm: "safe",
			assert: func(t *testing.T) {
				store, err := memory.NewQueueStore()
				require.NoError(t, err)
				require.NoError(t, store.Enqueue(t.Context(), msgin.New[any]("payload")))
				ds, err := store.Claim(t.Context(), math.MaxInt)
				require.NoError(t, err, "accepts math.MaxInt — capacity is min(max, len(s.ready)), never max itself, "+
					"so it is safe by construction regardless of max (Spec 016 §2.0)")
				require.Len(t, ds, 1, "product usable: the one enqueued message must still be delivered")
				assert.Equal(t, "payload", ds[0].Msg.Payload())
			},
		},
		{
			key: "(manual) channel.QueueChannel.Poll",
			arm: "safe",
			assert: func(t *testing.T) {
				store, err := memory.NewQueueStore()
				require.NoError(t, err)
				require.NoError(t, store.Enqueue(t.Context(), msgin.New[any]("payload")))
				qc, err := channel.NewQueueChannel(store)
				require.NoError(t, err)
				// Poll forwards verbatim into QueueStore.Claim (Spec 016 §2.0) — this
				// row and the one above exercise the same underlying safety property
				// through msgin's two different public entry points to it.
				ds, err := qc.Poll(t.Context(), math.MaxInt)
				require.NoError(t, err, "accepts math.MaxInt — delegates into QueueStore.Claim's safety")
				require.Len(t, ds, 1, "product usable: the one enqueued message must still be delivered")
				assert.Equal(t, "payload", ds[0].Msg.Payload())
			},
		},
	}

	// Self-check: every AST-discovered key (i.e. every row not prefixed
	// "(manual) ") must appear here exactly once, and the set must match
	// sizingConformanceKeys — so this table cannot silently drift from half 1's
	// own list. This is a sanity guard on the table's own construction, not a
	// substitute for TestSizingOptionClass_Completeness's real repo walk.
	var astKeys []string
	seen := map[string]int{}
	for _, tc := range tests {
		seen[tc.key]++
		if !strings.HasPrefix(tc.key, "(manual) ") {
			astKeys = append(astKeys, tc.key)
		}
	}
	for k, n := range seen {
		assert.Equal(t, 1, n, "conformance key %q must appear exactly once", k)
	}
	sort.Strings(astKeys)
	want := append([]string(nil), sizingConformanceKeys...)
	sort.Strings(want)
	require.Equal(t, want, astKeys,
		"every key in sizingConformanceKeys must have exactly one conformance row — half 2 must be executable "+
			"for every key half 1 discovers, never a declaration string (Spec 016 §6 AC-5)")
	require.Len(t, tests, 21,
		"19 AST rows + 2 manual rows (memory.QueueStore.Claim, channel.QueueChannel.Poll — Spec 016 §2.0)")

	// The ARM PARTITION is normative, so assert it rather than merely naming it
	// in a subtest prefix. Spec 016 §2.1's arm table and §6 AC-5 both fix the
	// split — 13/1/0/6 since Spec 017 added memory.WithMaxGroupMembers and
	// Spec 018 moved the three msghttp byte caps out of
	// "deferred"; without this, a contributor could move a row between
	// arms — precisely the reclassification that round-4 BLOCKER-1
	// (WithReplayBuffer, safe -> class member) and round-5 BLOCKER-1
	// (WithMaxResponseBytes, safe -> deferred) each were — and the suite would
	// stay green, because `arm` would otherwise be read exactly once, to build
	// the subtest name. A verdict nothing asserts is a comment.
	//
	// Asserted as a key->arm MAPPING, not a per-arm COUNT (Task 8 review M-1):
	// a count map (map[string]int{"fixed": 12, ...}) is blind to a PAIRWISE
	// swap — relabel two keys' arms in opposite directions and every count
	// stays put, so the gate would stay green through exactly the
	// reclassification this comment describes. Binding each key to its own
	// arm makes that swap a diff against wantArms, which fails immediately.
	wantArms := map[string]string{
		"endpoint.WithMaxInFlight":           "fixed",
		"endpoint.WithConcurrency":           "fixed",
		"msghttp.WithConnectionBuffer":       "fixed",
		"memory.WithBuffer":                  "fixed",
		"memory.WithCapacity":                "fixed",
		"memory.WithMaxGroups":               "fixed",
		"memory.WithMaxGroupMembers":         "fixed",
		"sql.WithMaxGroupMembers":            "fixed",
		"msghttp.WithMaxConnections":         "fixed",
		"routing.WithCompletionSize":         "fixed",
		"msghttp.WithReplayBuffer":           "fixed",
		"msghttp.WithSuccessStatus":          "rejects",
		"msghttp.WithMaxBodyBytes":           "fixed",
		"msghttp.WithMaxEventBytes":          "fixed",
		"msghttp.WithMaxResponseBytes":       "fixed",
		"endpoint.WithPollMaxBatch":          "safe",
		"resilience.WithBreakerThreshold":    "safe",
		"endpoint.WithMaxPayloadBytes":       "safe",
		"resilience.NewTokenBucket":          "safe",
		"(manual) memory.QueueStore.Claim":   "safe",
		"(manual) channel.QueueChannel.Poll": "safe",
	}
	gotArms := map[string]string{}
	byArm := map[string]int{}
	for _, tc := range tests {
		gotArms[tc.key] = tc.arm
		byArm[tc.arm]++
	}
	require.Equal(t, wantArms, gotArms,
		"Spec 016 §2.1's arm table and §6 AC-5 fix EVERY key's arm, not just the per-arm counts: 14 class "+
			"members fixed here (9 by Spec 016/Plan 029, 2 by Spec 017/Plan 031, 3 by Spec 018/Plan 032), "+
			"1 that rejects without "+
			"being a class member (msghttp.WithSuccessStatus), 0 deferred (the arm is a tombstone since "+
			"Spec 018), 6 safe (4 AST + 2 manual). Moving a row between arms is a "+
			"SPEC change — update §2.1 and §6 AC-5, do not just edit this map")
	require.Equal(t, map[string]int{"fixed": 14, "rejects": 1, "safe": 6}, byArm,
		"the per-arm counts follow from wantArms above; a mismatch here means wantArms itself drifted "+
			"from Spec 016 §2.1's split, now 14/1/0/6. NOTE: byArm is built by COUNTING, so the empty "+
			"\"deferred\" arm has NO KEY here — do not add \"deferred\": 0, it would fail")

	for _, tc := range tests {
		t.Run(tc.arm+"/"+tc.key, func(t *testing.T) {
			tc.assert(t)
		})
	}
}
