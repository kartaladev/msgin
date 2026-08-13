//go:build ignore

// Command qualify rewrites bare identifiers to msgin.X across a package's Go
// files, operating on the AST so comments and string literals are never
// touched. Used by the derivation run to move files out of the root package.
//
// usage: qualify <pkgdir> <alias> <Sym1> <Sym2> ...
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: qualify <pkgdir> <alias> <Sym>...")
		os.Exit(2)
	}
	dir, alias := os.Args[1], os.Args[2]
	targets := map[string]bool{}
	for _, s := range os.Args[3:] {
		targets[s] = true
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			fmt.Fprintln(os.Stderr, "parse:", err)
			continue
		}

		// Collect declaration-name idents so we never qualify a definition.
		defs := map[*ast.Ident]bool{}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				defs[d.Name] = true
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						defs[s.Name] = true
					case *ast.ValueSpec:
						for _, n := range s.Names {
							defs[n] = true
						}
					}
				}
			}
		}

		n := 0
		pre := func(c *astutilCursor) bool { return true }
		_ = pre
		ast.Inspect(f, func(node ast.Node) bool {
			switch x := node.(type) {
			case *ast.SelectorExpr:
				// Visit X but never the Sel (a field/method name).
				ast.Inspect(x.X, func(inner ast.Node) bool {
					return replaceIn(inner, targets, defs, alias, &n)
				})
				return false
			case *ast.Field:
				// Field/param names are not references.
				for _, nm := range x.Names {
					defs[nm] = true
				}
			case *ast.KeyValueExpr:
				// Struct-literal keys are field names, not references.
				if id, ok := x.Key.(*ast.Ident); ok {
					defs[id] = true
				}
			}
			return replaceIn(node, targets, defs, alias, &n)
		})

		if n == 0 {
			continue
		}
		var buf bytes.Buffer
		if err := (&printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}).Fprint(&buf, fset, f); err != nil {
			panic(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			panic(err)
		}
		fmt.Printf("%s: qualified %d references\n", path, n)
	}
}

type astutilCursor struct{}

// replaceIn rewrites any child *ast.Ident field of node that names a target.
func replaceIn(node ast.Node, targets map[string]bool, defs map[*ast.Ident]bool, alias string, n *int) bool {
	qual := func(id *ast.Ident) ast.Expr {
		*n++
		return &ast.SelectorExpr{X: ast.NewIdent(alias), Sel: ast.NewIdent(id.Name)}
	}
	hit := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		return ok && targets[id.Name] && !defs[id]
	}

	switch x := node.(type) {
	case *ast.CallExpr:
		if hit(x.Fun) {
			x.Fun = qual(x.Fun.(*ast.Ident))
		}
		for i, a := range x.Args {
			if hit(a) {
				x.Args[i] = qual(a.(*ast.Ident))
			}
		}
	case *ast.IndexExpr:
		if hit(x.X) {
			x.X = qual(x.X.(*ast.Ident))
		}
		if hit(x.Index) {
			x.Index = qual(x.Index.(*ast.Ident))
		}
	case *ast.IndexListExpr:
		if hit(x.X) {
			x.X = qual(x.X.(*ast.Ident))
		}
		for i, e := range x.Indices {
			if hit(e) {
				x.Indices[i] = qual(e.(*ast.Ident))
			}
		}
	case *ast.StarExpr:
		if hit(x.X) {
			x.X = qual(x.X.(*ast.Ident))
		}
	case *ast.Field:
		if hit(x.Type) {
			x.Type = qual(x.Type.(*ast.Ident))
		}
	case *ast.ValueSpec:
		if hit(x.Type) {
			x.Type = qual(x.Type.(*ast.Ident))
		}
		for i, v := range x.Values {
			if hit(v) {
				x.Values[i] = qual(v.(*ast.Ident))
			}
		}
	case *ast.TypeSpec:
		if hit(x.Type) {
			x.Type = qual(x.Type.(*ast.Ident))
		}
	case *ast.ArrayType:
		if hit(x.Elt) {
			x.Elt = qual(x.Elt.(*ast.Ident))
		}
	case *ast.MapType:
		if hit(x.Key) {
			x.Key = qual(x.Key.(*ast.Ident))
		}
		if hit(x.Value) {
			x.Value = qual(x.Value.(*ast.Ident))
		}
	case *ast.ChanType:
		if hit(x.Value) {
			x.Value = qual(x.Value.(*ast.Ident))
		}
	case *ast.CompositeLit:
		if hit(x.Type) {
			x.Type = qual(x.Type.(*ast.Ident))
		}
		for i, e := range x.Elts {
			if hit(e) {
				x.Elts[i] = qual(e.(*ast.Ident))
			}
		}
	case *ast.AssignStmt:
		for i, r := range x.Rhs {
			if hit(r) {
				x.Rhs[i] = qual(r.(*ast.Ident))
			}
		}
	case *ast.ReturnStmt:
		for i, r := range x.Results {
			if hit(r) {
				x.Results[i] = qual(r.(*ast.Ident))
			}
		}
	case *ast.BinaryExpr:
		if hit(x.X) {
			x.X = qual(x.X.(*ast.Ident))
		}
		if hit(x.Y) {
			x.Y = qual(x.Y.(*ast.Ident))
		}
	case *ast.UnaryExpr:
		if hit(x.X) {
			x.X = qual(x.X.(*ast.Ident))
		}
	case *ast.ParenExpr:
		if hit(x.X) {
			x.X = qual(x.X.(*ast.Ident))
		}
	case *ast.TypeAssertExpr:
		if hit(x.Type) {
			x.Type = qual(x.Type.(*ast.Ident))
		}
	case *ast.SwitchStmt:
	case *ast.CaseClause:
		for i, e := range x.List {
			if hit(e) {
				x.List[i] = qual(e.(*ast.Ident))
			}
		}
	case *ast.ExprStmt:
		if hit(x.X) {
			x.X = qual(x.X.(*ast.Ident))
		}
	case *ast.KeyValueExpr:
		if hit(x.Value) {
			x.Value = qual(x.Value.(*ast.Ident))
		}
	}
	return true
}
