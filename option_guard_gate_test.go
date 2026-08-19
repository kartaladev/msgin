package msgin_test

// Spec: 015 (AC-7)  Plan: 028 (Task 7)  ADR: 0031
//
// The class gate for nil option elements.
//
// Plan 028 fixed 32 functional-option constructors one at a time so that a nil
// option ELEMENT reports a fault instead of panicking. This file stops the class
// from coming back: it parses every .go file in the repository — the root module
// is the repo root, so a walk from "." reaches all eight modules — finds every
// function whose signature carries a variadic "...XxxOption" parameter, and fails
// when that function's body has no nil-element guard over it.
//
// # What counts as a guard
//
// All five shapes the increment shipped are accepted (Spec 015 §3.1–§3.3):
//
//	R1 folded     for i, opt := range opts { if opt == nil { return nil, nilOptionAt(...) }; opt(&cfg) }
//	R1 pre-check  for i, opt := range opts { if opt == nil { return nil, nilOptionAt(...) } }
//	R2 latch      for i, opt := range opts { if opt == nil { if x.err == nil { x.err = ... }; continue }; opt(&cfg) }
//	R3 skip       for _, opt := range opts { if opt == nil { continue }; opt(&cfg) }
//	return-in-loop  routing.Filter returns a failing Step from inside the loop
//
// Structurally: a range statement over the option parameter whose body contains an
// "if <elem> == nil" (or "if opts[i] == nil") at its top level, whose block disposes
// of the element at that block's own top level. The inverted form
// "if opt != nil { opt(&cfg) }" is accepted too.
//
// # Disposal depends on REPORTABILITY, not just on not panicking
//
// Spec 015 §3 states the contract over what a constructor can report the fault
// through, so the gate does too:
//
//   - The function RETURNS AN ERROR (it is an R1) ⇒ only a `return` disposes of the
//     element. A bare `continue`/`break` is rejected: it silently discards a caller's
//     nil while a perfectly good error return sits unused. That is the mechanism the
//     round-1 audit killed as BLOCKER-2, and handler.go:44-51 rejects it verbatim for
//     a nil Chain step ("skipping was rejected … a dropped Filter stops filtering").
//   - The function returns NO error (R2 latch, R3 skip) ⇒ there is no surface to
//     report through, so `continue`/`break` is the disposal the spec mandates and
//     stays accepted.
//
// This is a no-op on the tree as delivered — all 25 R1 constructors already `return`,
// and the 5 R2 + 2 R3 constructors return no error — and it is what stops a FUTURE
// error-returning constructor from regressing into a silent skip.
//
// # Limitations, accepted deliberately
//
//  1. FUNC-NESS IS MATCHED BY NAME, NOT BY TYPE. go/parser cannot tell whether the
//     identifier "Option" names a func type; deciding that needs go/types loaded per
//     module, and the eight modules have separate go.mod files. This gate therefore
//     matches any variadic parameter whose element type NAME ends in "Option"
//     (Spec 015 AC-7 explicitly permits this, "and document that as the limitation").
//     Consequence: a variadic option parameter whose type is not named "...Option" is
//     invisible to the gate, and a non-func type so named would be checked anyway.
//  2. THE PREDICATE IS SYNTACTIC, NOT A DOMINANCE PROOF. It establishes that a
//     nil-element guard is PRESENT in a range over the parameter; it does not prove
//     that guard DOMINATES the site where the element is applied. Proving that is
//     dataflow analysis and is deliberately out of scope. Three positional rules
//     narrow the gap — the nil-check must be at the top level of the loop body, its
//     terminator (return/continue/break) at the top level of the nil branch, and the
//     "!= nil" form must APPLY the element inside its own branch — so the ordinary
//     ways a guard drifts out of position are caught. Two deliberately adversarial
//     shapes still defeat it, and are named here rather than left to be discovered:
//
//     (a) a VACUOUS FIRST LOOP — "for _, opt := range opts { if opt == nil { continue } }"
//     that applies nothing, followed by a SECOND, unguarded apply loop over the same
//     parameter; and
//     (b) a guard inside a NEVER-CALLED CLOSURE (or any other dead code) in the same
//     body, followed by an unguarded apply loop.
//
//     Both are found guarded and both panic on a nil element. Neither occurs in any
//     shipped constructor, and neither is a shape a contributor writes by accident —
//     but a reader must not take a green run as proof that no nil option can panic.
//     It is proof that no constructor has drifted into one of the ordinary
//     unguarded shapes.
//  3. Only a range over the parameter is recognised. An index loop
//     ("for i := 0; i < len(opts); i++") or a slices.Contains pre-check would be
//     reported as unguarded. No constructor uses those today.
//  4. Directories whose name begins with "." are not walked (.git, .github,
//     .superpowers), nor is "vendor". Nothing shipped lives there.
//  5. _test.go files and func literals are out of scope by construction — see the
//     hazard notes on scanOptionParams.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AST node kinds a variadic option element type can take. All four are load-bearing:
// kinds 3 and 4 carry endpoint.NewConsumer, .NewProducer and .NewGateway, so a checker
// built to kinds 1–2 skips the increment's flagship constructors in silence.
const (
	kindIdent     = "*ast.Ident"         // Option, RouterOption
	kindSelector  = "*ast.SelectorExpr"  // msghttp.Option
	kindIndex     = "*ast.IndexExpr"     // ConsumerOption[T]
	kindIndexList = "*ast.IndexListExpr" // GatewayOption[Req, Rep]
)

// optionParam is one variadic "...XxxOption" parameter found in a function signature.
type optionParam struct {
	pos      string // file:line:col of the "..." token
	fn       string // enclosing function name
	param    string // parameter name; "" when unnamed or "_"
	typeName string // element type name, e.g. "ConsumerOption"
	kind     string // one of the kind* constants
	guarded  bool
	why      string // why it is unguarded, when it is
}

// scanOptionParams reports every variadic "...XxxOption" parameter declared by a
// top-level function in f, and whether each is guarded.
//
// Hazard 5 (skip *ast.FuncLit) is handled by construction: Go has no nested FuncDecl,
// so ranging over f.Decls reaches declared functions only and never a func literal —
// which is what keeps the four adapter/database/sql/harness literals out.
//
// Hazard 6 (skip _test.go) is the caller's job, since a parsed file does not carry its
// own name reliably; both live callers filter on the ".go" path before calling in.
func scanOptionParams(fset *token.FileSet, f *ast.File) []optionParam {
	var out []optionParam
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Type.Params == nil {
			continue
		}
		name := fd.Name.Name
		if fd.Recv != nil {
			name = "(method) " + name
		}
		for _, field := range fd.Type.Params.List {
			ell, ok := field.Type.(*ast.Ellipsis)
			if !ok {
				continue
			}
			typeName, kind, ok := optionElementType(ell.Elt)
			if !ok {
				continue
			}
			p := optionParam{
				pos:      fset.Position(ell.Pos()).String(),
				fn:       name,
				typeName: typeName,
				kind:     kind,
			}
			if len(field.Names) == 1 && field.Names[0].Name != "_" {
				p.param = field.Names[0].Name
			}
			switch {
			case p.param == "":
				// Fail closed: an unnamed (or "_") variadic option parameter cannot be
				// guarded at all, since the body has no way to reach the elements.
				p.why = "variadic option parameter is unnamed, so no guard is possible"
			case fd.Body == nil:
				p.why = "function has no body"
			case hasNilElementGuard(fd.Body, p.param, returnsError(fd.Type)):
				p.guarded = true
			case returnsError(fd.Type):
				p.why = "no nil-element guard over " + p.param +
					" that REPORTS the fault; this function returns an error, so a bare " +
					"continue/break is not a valid disposal — return the fault (R1)"
			default:
				p.why = "no nil-element guard over " + p.param
			}
			out = append(out, p)
		}
	}
	return out
}

// optionElementType returns the name and AST node kind of a variadic element type
// whose name ends in "Option", covering hazards 1–4.
func optionElementType(elt ast.Expr) (name, kind string, ok bool) {
	switch t := elt.(type) {
	case *ast.Ident:
		name, kind = t.Name, kindIdent
	case *ast.SelectorExpr:
		name, kind = t.Sel.Name, kindSelector
	case *ast.IndexExpr: // generic, one type parameter
		name, kind = baseTypeName(t.X), kindIndex
	case *ast.IndexListExpr: // generic, two or more type parameters
		name, kind = baseTypeName(t.X), kindIndexList
	default:
		return "", "", false
	}
	if name == "" || !strings.HasSuffix(name, "Option") {
		return "", "", false
	}
	return name, kind, true
}

// baseTypeName unwraps the generic base of an index expression, which is either a bare
// identifier (ConsumerOption[T]) or a qualified one (pkg.ConsumerOption[T]).
func baseTypeName(x ast.Expr) string {
	switch t := x.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}

// hasNilElementGuard reports whether body ranges over param and rejects, latches or
// skips a nil element inside that loop.
//
// mustReport tightens what counts as disposal: see terminatesIteration.
func hasNilElementGuard(body *ast.BlockStmt, param string, mustReport bool) bool {
	guarded := false
	ast.Inspect(body, func(n ast.Node) bool {
		if guarded {
			return false
		}
		rs, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		if id, ok := rs.X.(*ast.Ident); !ok || id.Name != param {
			return true
		}
		if rs.Body != nil && loopChecksNil(rs.Body, param, identName(rs.Key), identName(rs.Value), mustReport) {
			guarded = true
			return false
		}
		return true
	})
	return guarded
}

// loopChecksNil looks for the guard inside one range-loop body.
//
// The nil-check must sit at the TOP LEVEL of the loop body. A check nested inside
// another condition — "if strict { if opt == nil { continue } }; opt(cfg)" — runs on
// one arm only and leaves the application below it live on the other, so it is not a
// guard even though a syntactic search would find it.
func loopChecksNil(loop *ast.BlockStmt, param, key, value string, mustReport bool) bool {
	for _, stmt := range loop.List {
		is, ok := stmt.(*ast.IfStmt)
		if !ok {
			continue
		}
		switch nilComparison(is.Cond, param, key, value) {
		case token.EQL:
			// "if opt == nil { ... }" — the branch must leave the iteration, or the
			// unguarded application below it still runs.
			if terminatesIteration(is.Body, mustReport) {
				return true
			}
		case token.NEQ:
			// "if opt != nil { opt(&cfg) }" — this is a guard only when the element is
			// actually APPLIED inside the branch. "if opt != nil { _ = opt }" followed
			// by a bare opt(cfg) is not a guard, it is a nil check with no consequence.
			if appliesElement(is.Body, param, key, value) {
				return true
			}
		}
	}
	return false
}

// appliesElement reports whether blk calls the loop element — opt(...) or opts[i](...).
func appliesElement(blk *ast.BlockStmt, param, key, value string) bool {
	if blk == nil {
		return false
	}
	found := false
	ast.Inspect(blk, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			found = value != "" && fn.Name == value
		case *ast.IndexExpr:
			base, okBase := fn.X.(*ast.Ident)
			idx, okIdx := fn.Index.(*ast.Ident)
			found = okBase && okIdx && base.Name == param && key != "" && idx.Name == key
		}
		return !found
	})
	return found
}

// nilComparison classifies cond as an == or != comparison of the loop element (either
// the range value or param[key]) against nil, and returns token.ILLEGAL otherwise.
func nilComparison(cond ast.Expr, param, key, value string) token.Token {
	be, ok := cond.(*ast.BinaryExpr)
	if !ok || (be.Op != token.EQL && be.Op != token.NEQ) {
		return token.ILLEGAL
	}
	isElem := func(e ast.Expr) bool {
		if id, ok := e.(*ast.Ident); ok {
			return value != "" && id.Name == value
		}
		ix, ok := e.(*ast.IndexExpr)
		if !ok {
			return false
		}
		base, okBase := ix.X.(*ast.Ident)
		idx, okIdx := ix.Index.(*ast.Ident)
		return okBase && okIdx && base.Name == param && key != "" && idx.Name == key
	}
	isNil := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		return ok && id.Name == "nil"
	}
	if (isElem(be.X) && isNil(be.Y)) || (isNil(be.X) && isElem(be.Y)) {
		return be.Op
	}
	return token.ILLEGAL
}

// terminatesIteration reports whether blk disposes of the nil element from its own top
// level. Deliberately does NOT descend into a nested if, loop or func literal: a return
// reachable only on one arm of an inner condition leaves the unguarded application live
// on the other arm.
//
// mustReport encodes Spec 015 §3's contract, which is stated over REPORTABILITY, not
// merely over "does not panic". When the enclosing function returns an error it has a
// surface to report the fault through, so it is an R1 constructor and only a `return`
// disposes of the element: a bare `continue`/`break` silently SKIPS a caller's nil
// while a perfectly good error return sits unused. That is the mechanism the round-1
// audit killed as BLOCKER-2, and handler.go:44-51 rejects it verbatim for a nil Chain
// step. When the function returns no error there is nothing to report through, so
// `continue`/`break` is the R2/R3 disposal and stays legal.
func terminatesIteration(blk *ast.BlockStmt, mustReport bool) bool {
	if blk == nil {
		return false
	}
	for _, stmt := range blk.List {
		switch st := stmt.(type) {
		case *ast.ReturnStmt:
			return true
		case *ast.BranchStmt:
			if !mustReport && (st.Tok == token.CONTINUE || st.Tok == token.BREAK) {
				return true
			}
		}
	}
	return false
}

// returnsError reports whether ft declares an `error` result, which is what makes a
// constructor an R1 — able to reject the nil element rather than swallow it.
func returnsError(ft *ast.FuncType) bool {
	if ft == nil || ft.Results == nil {
		return false
	}
	for _, res := range ft.Results.List {
		if id, ok := res.Type.(*ast.Ident); ok && id.Name == "error" {
			return true
		}
	}
	return false
}

func identName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// scanRepo walks root and scans every non-test .go file outside dot-directories and
// vendor. It returns every variadic option parameter it found.
func scanRepo(t *testing.T, root string) []optionParam {
	t.Helper()

	fset := token.NewFileSet()
	var found []optionParam
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
			return nil // hazard 6: _test.go is out of scope
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		src, rderr := os.ReadFile(path)
		if rderr != nil {
			return rderr
		}
		// Parse under the repo-relative name so reported positions are copy-pasteable
		// from the repository root rather than absolute to one machine.
		f, perr := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", rel, perr)
		}
		found = append(found, scanOptionParams(fset, f)...)
		return nil
	})
	require.NoError(t, err, "walking %s", root)
	return found
}

// TestEveryVariadicOptionParameterIsGuarded is the class gate (Spec 015 AC-7): no
// function anywhere in the workspace may take a variadic "...XxxOption" parameter and
// apply it without a nil-element guard.
//
// It walks from the working directory, which for a root-package test is the repository
// root, so it reaches all eight modules without depending on /tmp or on go.work.
func TestEveryVariadicOptionParameterIsGuarded(t *testing.T) {
	root, err := os.Getwd()
	require.NoError(t, err)

	found := scanRepo(t, root)

	byKind := map[string]int{}
	var unguarded []string
	for _, p := range found {
		byKind[p.kind]++
		if !p.guarded {
			unguarded = append(unguarded, fmt.Sprintf("%s: %s(... ...%s): %s", p.pos, p.fn, p.typeName, p.why))
		}
	}
	sort.Strings(unguarded)

	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		t.Logf("node kind %-20s %d", k, byKind[k])
	}
	t.Logf("total variadic ...Option parameters in non-test files: %d", len(found))

	// Self-vacuity guard. A gate that scans nothing passes for free, and the specific
	// way this one could go blind is by losing a node kind — which is how the generic
	// constructors (NewConsumer, NewProducer, NewGateway) would vanish from the scan
	// without a single test turning red. Each kind is therefore required to be present.
	// These are floors, not a census: they assert the scanner still sees each shape,
	// never that the workspace holds a particular number of constructors.
	for _, k := range []string{kindIdent, kindSelector, kindIndex, kindIndexList} {
		assert.Positivef(t, byKind[k], "the scan found no %s option parameter at all: "+
			"the gate has gone blind to that node kind, or the walk missed a module", k)
	}

	assert.Emptyf(t, unguarded, "%d function(s) take a variadic ...Option parameter with no nil-element "+
		"guard, so a nil option element panics on caller input (Spec 015; CLAUDE.md forbids panicking on "+
		"caller input). Fold a guard into the apply loop — R1 return, R2 latch+continue, or R3 continue:\n\t%s",
		len(unguarded), strings.Join(unguarded, "\n\t"))
}

// TestOptionGuardRecognizer is the committed vacuity probe for the gate above. The
// project's standing rule is that a gate which has never failed proves nothing, so the
// recognizer is exercised here against synthetic sources: the five guard shapes the
// increment shipped must be accepted, and an unguarded constructor must be flagged in
// BOTH a bare-typed and a GENERIC form — a probe that plants only a non-generic
// constructor certifies a gate blind to hazards 3–4.
func TestOptionGuardRecognizer(t *testing.T) {
	const header = "package p\n\n"

	tests := []struct {
		name   string
		src    string
		assert func(t *testing.T, got []optionParam)
	}{
		{
			name: "R1 folded — return from inside the apply loop",
			src: `func NewConsumer(opts ...Option) (*C, error) {
	for i, opt := range opts {
		if opt == nil {
			return nil, nilOptionAt("p.NewConsumer", i)
		}
		opt(&cfg)
	}
	return &C{}, nil
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, true, kindIdent, "Option")
			},
		},
		{
			name: "R1 pre-check — standalone loop, no opt(&cfg) call",
			src: `func NewInbound(opts ...msghttp.Option) (*I, error) {
	for i, opt := range opts {
		if opt == nil {
			return nil, nilOptionAt("p.NewInbound", i)
		}
	}
	return delegate(opts...)
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, true, kindSelector, "Option")
			},
		},
		{
			name: "R2 latch — nested latch then continue",
			src: `func NewRouter(opts ...RouterOption) *R {
	r := &R{}
	for i, opt := range opts {
		if opt == nil {
			if r.err == nil {
				r.err = fmt.Errorf("nil option at index %d", i)
			}
			continue
		}
		opt(&r.cfg)
	}
	return r
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, true, kindIdent, "RouterOption")
			},
		},
		{
			name: "R3 skip — bare continue",
			src: `func New(opts ...MessageOption) M {
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&cfg)
	}
	return M{}
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, true, kindIdent, "MessageOption")
			},
		},
		{
			// The anti-over-reach half of the reportability rule. A constructor with NO
			// error result has no surface to report through, so `continue` IS its
			// disposal (R3 — msgin.New, sqlite.DSN) and must stay accepted. This case
			// and the one below it move in opposite directions on the same rule.
			name: "R3 skip stays legal — no error result, so continue is the disposal",
			src: `func DSN(path string, opts ...DSNOption) string {
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&cfg)
	}
	return cfg.String()
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, true, kindIdent, "DSNOption")
			},
		},
		{
			// Spec 015 §3 states the contract over REPORTABILITY, not over "does not
			// panic". A constructor that returns an error and merely `continue`s past a
			// nil silently discards a caller's wiring bug with a usable error return
			// sitting unused — the mechanism the round-1 audit killed as BLOCKER-2 and
			// that handler.go:44-51 rejects verbatim for a nil Chain step.
			name: "PROBE R1 that only continues — error result, so skipping is not disposal",
			src: `func NewSwallowing(opts ...Option) (*T, error) {
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&cfg)
	}
	return &T{}, nil
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, false, kindIdent, "Option")
				assert.Contains(t, got[0].why, "REPORTS the fault")
			},
		},
		{
			name: "fifth shape — routing.Filter returns a failing Step from the loop",
			src: `func Filter(pred func() bool, opts ...FilterOption) Step {
	for i, opt := range opts {
		if opt == nil {
			return nilFuncStep(fmt.Sprintf("routing.Filter: nil option at index %d", i))
		}
		opt(&cfg)
	}
	return step
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, true, kindIdent, "FilterOption")
			},
		},
		{
			name: "inverted guard — if opt != nil { opt(&cfg) }",
			src: `func New(opts ...Option) *T {
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &T{}
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, true, kindIdent, "Option")
			},
		},
		{
			name: "index-form guard — opts[i] == nil",
			src: `func New(opts ...Option) (*T, error) {
	for i := range opts {
		if opts[i] == nil {
			return nil, errNil
		}
		opts[i](&cfg)
	}
	return &T{}, nil
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, true, kindIdent, "Option")
			},
		},

		// ---- the planted probes: these MUST be flagged ----
		{
			name: "PROBE bare-typed — unguarded apply loop",
			src: `func NewUnguarded(opts ...Option) (*T, error) {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	return &T{}, nil
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, false, kindIdent, "Option")
			},
		},
		{
			name: "PROBE generic single param — ConsumerOption[T], unguarded",
			src: `func NewUnguardedGeneric[T any](opts ...ConsumerOption[T]) (*C[T], error) {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	return &C[T]{}, nil
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, false, kindIndex, "ConsumerOption")
			},
		},
		{
			name: "PROBE generic multi param — GatewayOption[Req, Rep], unguarded",
			src: `func NewUnguardedGateway[Req, Rep any](opts ...GatewayOption[Req, Rep]) (*G[Req, Rep], error) {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	return &G[Req, Rep]{}, nil
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, false, kindIndexList, "GatewayOption")
			},
		},
		{
			name: "PROBE qualified type — msghttp.Option, unguarded delegator",
			src: `func NewInboundGateway(opts ...msghttp.Option) (*G, error) {
	return delegate(opts...)
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, false, kindSelector, "Option")
			},
		},
		{
			name: "PROBE unnamed parameter — cannot be guarded, must fail closed",
			src: `func NewUnnamed(...Option) *T {
	return &T{}
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, false, kindIdent, "Option")
				assert.Contains(t, got[0].why, "unnamed")
			},
		},
		{
			name: "PROBE conditional return nested one if deeper — not a guard",
			src: `func NewNested(opts ...Option) (*T, error) {
	for i, opt := range opts {
		if opt == nil {
			if strict {
				return nil, errNil
			}
		}
		opt(&cfg)
	}
	return &T{}, nil
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, false, kindIdent, "Option")
			},
		},
		{
			// Review finding D2. The nil-check itself is nested inside another
			// condition, so it runs on one arm only while opt(cfg) below it runs on
			// both. A syntactic search that inspects the whole loop body accepts this;
			// requiring the nil-check at the loop body's TOP LEVEL rejects it.
			name: "PROBE D2 — nil-check nested inside another condition, apply outside it",
			src: `func NewOuterNested(opts ...Option) *T {
	for _, opt := range opts {
		if strict {
			if opt == nil {
				continue
			}
		}
		opt(cfg)
	}
	return &T{}
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, false, kindIdent, "Option")
			},
		},
		{
			// Review finding D3. An "if opt != nil" branch that does not APPLY the
			// element is a nil check with no consequence; the bare opt(cfg) after it
			// still panics.
			name: "PROBE D3 — if opt != nil branch that never applies opt",
			src: `func NewInertNotNil(opts ...Option) *T {
	for _, opt := range opts {
		if opt != nil {
			_ = opt
		}
		opt(cfg)
	}
	return &T{}
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, false, kindIdent, "Option")
			},
		},
		{
			// The inverted form stays accepted when the branch really applies the
			// element through the index form too, so tightening D3 costs no shape.
			name: "index-form inverted guard — if opts[i] != nil { opts[i](cfg) }",
			src: `func New(opts ...Option) *T {
	for i := range opts {
		if opts[i] != nil {
			opts[i](cfg)
		}
	}
	return &T{}
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, true, kindIdent, "Option")
			},
		},
		{
			name: "PROBE guard over the wrong slice — not a guard",
			src: `func NewWrongSlice(other []Option, opts ...Option) (*T, error) {
	for _, opt := range other {
		if opt == nil {
			return nil, errNil
		}
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &T{}, nil
}`,
			assert: func(t *testing.T, got []optionParam) {
				requireOneGuarded(t, got, false, kindIdent, "Option")
			},
		},

		// ---- out of scope: must not be reported at all ----
		{
			name: "func literals are skipped (hazard 5)",
			src: `var f = func(opts ...msghttp.Option) error {
	for _, opt := range opts {
		opt(&cfg)
	}
	return nil
}`,
			assert: func(t *testing.T, got []optionParam) {
				assert.Empty(t, got, "a func literal is not a declared constructor and must be skipped")
			},
		},
		{
			name: "a variadic that is not an option type is skipped",
			src: `func Log(args ...any) {
	for _, a := range args {
		_ = a
	}
}`,
			assert: func(t *testing.T, got []optionParam) {
				assert.Empty(t, got)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "probe.go", header+tt.src, parser.SkipObjectResolution)
			require.NoError(t, err)
			tt.assert(t, scanOptionParams(fset, f))
		})
	}
}

// requireOneGuarded asserts the scan produced exactly one option parameter, of the
// given node kind and type name, with the expected guarded verdict.
func requireOneGuarded(t *testing.T, got []optionParam, guarded bool, kind, typeName string) {
	t.Helper()
	require.Len(t, got, 1, "expected exactly one variadic option parameter, got %+v", got)
	assert.Equal(t, kind, got[0].kind, "node kind")
	assert.Equal(t, typeName, got[0].typeName, "element type name")
	assert.Equalf(t, guarded, got[0].guarded, "guarded verdict (reason recorded: %q)", got[0].why)
}
