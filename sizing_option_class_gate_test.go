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
//  2. CONFORMANCE (behavioral). Every one of the 17 AST-discovered keys, plus
//     2 MANUAL rows for the class members the Recv == nil boundary excludes
//     but a root test can still construct, gets an executable row — never a
//     declaration string — in one of three arms matching Spec 016 §2.1's
//     three verdicts: "fixed" (the fault is reported through the surface
//     Spec 016 §3 names for it), "deferred" (accepts 1<<62, annotated so it
//     never reads as a safety certificate), or "safe" (accepts 1<<62 AND its
//     product is usable).
//
// # The Recv == nil boundary — a decision, not an omission (Spec 016 §2.0)
//
// In go/ast a method is a *ast.FuncDecl with a non-nil Recv, so "every
// exported function with an int/int64 parameter" reads two ways. Measured on
// this tree: Recv == nil yields 17 keys, every one constructible from a root
// blackbox test; ANY FuncDecl yields 44, of which 22 sit on UNEXPORTED
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
//     root-module packages. All 17 keys live in root-module packages today
//     (endpoint, adapter/http, adapter/memory, channel, resilience, routing),
//     so both halves cover all of them. A sizing option added to expr, one of
//     the sql dialects, or another leaf module FAILS half 1 and cannot be
//     added to half 2 — a deliberate gate failure demanding a spec revision,
//     not a silent pass.
//   - The Recv == nil boundary above: two excluded class members, both
//     covered by the manual rows — no uncovered residue.
//   - time.Duration PARAMETERS ARE OUTSIDE THE GATE BY CONSTRUCTION — the AST
//     sees time.Duration, not int64. This is deliberate, NOT a claim that
//     duration knobs are "checked" or "currently safe": the shipped
//     NewTicker/NewTimer sites each guard their duration, but that is not the
//     whole class — clock.After(d) with a variable duration is a further,
//     unaudited five sites (Spec 016 §3.7.4 / ADR 0032 D-AA). Do not read a
//     green run of THIS file as saying anything about time.Duration.

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
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

// sizingConformanceKeys is Spec 016 §2's conformance key set: the 16 exported
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
//	  '^func With[A-Za-z]+(\[[^]]*\])?\([a-z]+ (int|int64)\)' | wc -l   # → 16 `With...` options
//
// and cross-check the full 17 (including the positional burst) against
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
func hasIntOrInt64Param(ft *ast.FuncType) bool {
	if ft.Params == nil {
		return false
	}
	for _, field := range ft.Params.List {
		if id, ok := field.Type.(*ast.Ident); ok && (id.Name == "int" || id.Name == "int64") {
			return true
		}
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
		"Spec 016 §2's 17-key conformance set in BOTH directions (ADR 0032 D-AA). A new sizing option must be "+
		"folded into sizingConformanceKeys AND given a row in TestSizingOptionClass_Conformance — this diff "+
		"alone is not the gate, it is half of it.")
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

// sizingConformanceCase is one of the 19 rows Spec 016 §6 AC-5 half 2
// requires: 17 AST-discovered keys (matching sizingConformanceKeys) plus 2
// manual rows for the Recv == nil boundary's two excluded class members.
// assert is a closure, never a want/wantErr field pair (project table-test
// rule) — each row's construction differs too much (different packages,
// different constructor shapes) to share one SUT call.
type sizingConformanceCase struct {
	key    string // matches a sizingConformanceKeys entry, or "(manual) ..." for the two excluded methods
	arm    string // "fixed" | "deferred" | "safe" — Spec 016 §2.1's three verdicts
	assert func(t *testing.T)
}

// TestSizingOptionClass_Conformance is Spec 016 §6 AC-5's half 2: every key
// half 1 discovers is EXECUTABLE, never a declaration string (round-1 M-4).
func TestSizingOptionClass_Conformance(t *testing.T) {
	tests := []sizingConformanceCase{
		// ---- arm: fixed — 9 class members fixed by this increment + WithSuccessStatus ----
		// (WithSuccessStatus is "safe (a)" by Spec 016 §2.1's criterion — a pure
		// comparison, nothing accumulates — but it already rejects 1<<62 with its
		// own pre-existing [100,599] check, so §2.1 places its AC-5 row here,
		// alongside the fixed knobs, rather than in the "safe, accepts" arm.)
		{
			key: "endpoint.WithMaxInFlight",
			arm: "fixed",
			assert: func(t *testing.T) {
				h := func(context.Context, msgin.Message[any]) error { return nil }
				_, err := endpoint.NewConsumer[any](discardPollingSource{}, h, endpoint.WithMaxInFlight[any](1<<62))
				require.ErrorIs(t, err, msgin.ErrInvalidMaxInFlight)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: max in-flight out of range: endpoint.WithMaxInFlight: 4611686018427387904 not in [1, 1048576]")
			},
		},
		{
			key: "endpoint.WithConcurrency",
			arm: "fixed",
			assert: func(t *testing.T) {
				h := func(context.Context, msgin.Message[any]) error { return nil }
				_, err := endpoint.NewConsumer[any](discardPollingSource{}, h, endpoint.WithConcurrency[any](1<<62))
				require.ErrorIs(t, err, msgin.ErrInvalidConcurrency)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: concurrency out of range: endpoint.WithConcurrency: 4611686018427387904 not in [1, 65536]")
			},
		},
		{
			key: "msghttp.WithConnectionBuffer",
			arm: "fixed",
			assert: func(t *testing.T) {
				_, err := msghttp.NewConfig(msghttp.WithConnectionBuffer(1 << 62))
				require.ErrorIs(t, err, msghttp.ErrInvalidConnectionBuffer)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: connection buffer out of range: msghttp.WithConnectionBuffer: 4611686018427387904 not in [1, 65536]")
			},
		},
		{
			key: "memory.WithBuffer",
			arm: "fixed",
			assert: func(t *testing.T) {
				// R2 (Spec 016 §3.2): New has no error return, so the fault must
				// surface at the object's first use — Send — not at New itself.
				b := memory.New(memory.WithBuffer(1 << 62))
				require.NotNil(t, b, "New must not panic")
				err := b.Send(t.Context(), msgin.New[any]("x"))
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.True(t, msgin.IsPermanent(err), "R2: latched and reported wrapped in Permanent (ADR 0031 D-V)")
				assert.EqualError(t, err,
					"msgin: permanent: msgin: capacity out of range: memory.WithBuffer: 4611686018427387904 not in [0, 1048576]")
			},
		},
		{
			key: "memory.WithCapacity",
			arm: "fixed",
			assert: func(t *testing.T) {
				_, err := memory.NewQueueStore(memory.WithCapacity(1 << 62))
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: memory.WithCapacity: 4611686018427387904 not in [1, 1048576]")
			},
		},
		{
			key: "memory.WithMaxGroups",
			arm: "fixed",
			assert: func(t *testing.T) {
				_, err := memory.NewGroupStore(memory.WithMaxGroups(1 << 62))
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: memory.WithMaxGroups: 4611686018427387904 not in [1, 1048576]")
			},
		},
		{
			key: "msghttp.WithMaxConnections",
			arm: "fixed",
			assert: func(t *testing.T) {
				_, err := msghttp.NewConfig(msghttp.WithMaxConnections(1 << 62))
				require.ErrorIs(t, err, msghttp.ErrInvalidMaxConnections)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: max connections out of range: msghttp.WithMaxConnections: 4611686018427387904 not in [1, 65536]")
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
				_, err = routing.NewAggregator[int, int](store, fn, routing.WithCompletionSize(1<<62))
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: routing.WithCompletionSize: 4611686018427387904 not in [1, 65536]")
			},
		},
		{
			key: "msghttp.WithReplayBuffer",
			arm: "fixed",
			assert: func(t *testing.T) {
				_, err := msghttp.NewConfig(msghttp.WithReplayBuffer(1 << 62))
				require.ErrorIs(t, err, msghttp.ErrInvalidReplayBuffer)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: replay buffer out of range: msghttp.WithReplayBuffer: 4611686018427387904 not in [1, 65536]")
			},
		},
		{
			key: "msghttp.WithSuccessStatus",
			arm: "fixed",
			assert: func(t *testing.T) {
				_, err := msghttp.NewConfig(msghttp.WithSuccessStatus(1 << 62))
				require.ErrorIs(t, err, msghttp.ErrInvalidStatusCode)
				assert.EqualError(t, err, "msghttp: status code must be in [100,599]")
			},
		},

		// ---- arm: deferred — 3 class members whose ceiling is DEFERRED (Spec 016 §3.8) ----
		// Each is the sole bound on a REMOTE-PEER-DRIVEN read retained in memory,
		// which CLAUDE.md's Sensible-defaults gate names as the one case where no
		// library-picked ceiling can be safe. The assertion is deliberately just
		// "accepts" — asserting "and its product is usable" here would certify an
		// unbounded remote-driven read as conformant, which is the exact inversion
		// ADR 0032's round-5 BLOCKER-1 was about.
		{
			key: "msghttp.WithMaxBodyBytes",
			arm: "deferred", // class member, remedy deferred — Spec 016 §3.8
			assert: func(t *testing.T) {
				cfg, err := msghttp.NewConfig(msghttp.WithMaxBodyBytes(1 << 62))
				require.NoError(t, err)
				require.NotNil(t, cfg)
			},
		},
		{
			key: "msghttp.WithMaxEventBytes",
			arm: "deferred", // class member, remedy deferred — Spec 016 §3.8
			assert: func(t *testing.T) {
				cfg, err := msghttp.NewConfig(msghttp.WithMaxEventBytes(1 << 62))
				require.NoError(t, err)
				require.NotNil(t, cfg)
			},
		},
		{
			key: "msghttp.WithMaxResponseBytes",
			arm: "deferred", // class member, remedy deferred — Spec 016 §3.8
			assert: func(t *testing.T) {
				cfg, err := msghttp.NewConfig(msghttp.WithMaxResponseBytes(1 << 62))
				require.NoError(t, err)
				require.NotNil(t, cfg)
			},
		},

		// ---- arm: safe — 4 AST rows + 2 manual rows ----
		// Each accepts 1<<62 AND its product is proven usable, not merely
		// constructed: a comparison-only knob is exercised past the point where a
		// buggy comparison (e.g. an int32 truncation) would misbehave.
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
					endpoint.WithPollMaxBatch[any](1<<62),
					endpoint.WithPollInterval[any](time.Millisecond))
				require.NoError(t, err, "accepts 1<<62 — derivatively safe: held is bounded by "+
					"min(pollMaxBatch, free credits), and free credits are maxInFlight's own ceiling (Spec 016 §2.1)")

				stop := runAndStop(t, c.Run)
				defer stop()
				require.Eventually(t, func() bool {
					mu.Lock()
					defer mu.Unlock()
					return seen == 2
				}, 2*time.Second, time.Millisecond,
					"product usable: both queued messages must still be delivered under a 1<<62 poll-batch cap")
			},
		},
		{
			key: "resilience.WithBreakerThreshold",
			arm: "safe",
			assert: func(t *testing.T) {
				b, err := resilience.NewCircuitBreaker(resilience.WithBreakerThreshold(1 << 62))
				require.NoError(t, err, "accepts 1<<62 — the option silently ignores n < 1 and never rejects "+
					"(Spec 016 §2.1 safety cause (a): a pure comparison over a scalar counter)")
				for range 1000 {
					b.Record(false)
				}
				assert.True(t, b.Allow(),
					"product usable: 1,000 consecutive failures must not trip a breaker whose threshold is 1<<62")
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
					endpoint.WithMaxPayloadBytes[[]byte](1<<62),
					endpoint.WithPollInterval[[]byte](time.Millisecond))
				require.NoError(t, err, "accepts 1<<62 — the option never validates n at all "+
					"(Spec 016 §2.1 safety cause (b): len(b) is already-materialised)")

				stop := runAndStop(t, c.Run)
				defer stop()
				require.Eventually(t, func() bool {
					mu.Lock()
					defer mu.Unlock()
					return got != nil
				}, 2*time.Second, time.Millisecond,
					"product usable: a normal payload must still be decoded and delivered under a 1<<62 byte cap")
				mu.Lock()
				assert.Equal(t, []byte("hello"), got)
				mu.Unlock()
			},
		},
		{
			key: "resilience.NewTokenBucket",
			arm: "safe",
			assert: func(t *testing.T) {
				rl, err := resilience.NewTokenBucket(1, 1<<62) // burst is the 17th key, positional
				require.NoError(t, err, "accepts 1<<62 — burst is a scalar comparison, safety cause (a)")
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
				ds, err := store.Claim(t.Context(), 1<<62)
				require.NoError(t, err, "accepts 1<<62 — capacity is min(max, len(s.ready)), never max itself, "+
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
				ds, err := qc.Poll(t.Context(), 1<<62)
				require.NoError(t, err, "accepts 1<<62 — delegates into QueueStore.Claim's safety")
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
	require.Len(t, tests, 19,
		"17 AST rows + 2 manual rows (memory.QueueStore.Claim, channel.QueueChannel.Poll — Spec 016 §2.0)")

	for _, tc := range tests {
		t.Run(tc.arm+"/"+tc.key, func(t *testing.T) {
			tc.assert(t)
		})
	}
}
