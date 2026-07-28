//go:build ignore

// Command decls lists top-level declarations per Go file, with kind and
// exportedness. Used to generate the declaration-level move tables (§G.4).
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type decl struct {
	file string
	kind string
	name string
	recv string
	line int
}

func main() {
	dir := "."
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	fset := token.NewFileSet()
	var out []decl

	entries, err := os.ReadDir(dir)
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			fmt.Fprintln(os.Stderr, "parse:", err)
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				recv := ""
				if d.Recv != nil && len(d.Recv.List) > 0 {
					recv = typeString(d.Recv.List[0].Type)
				}
				kind := "func"
				if recv != "" {
					kind = "method"
				}
				out = append(out, decl{e.Name(), kind, d.Name.Name, recv, fset.Position(d.Pos()).Line})
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						out = append(out, decl{e.Name(), "type", s.Name.Name, "", fset.Position(s.Pos()).Line})
					case *ast.ValueSpec:
						kind := "var"
						if d.Tok == token.CONST {
							kind = "const"
						}
						for _, n := range s.Names {
							if n.Name == "_" {
								continue
							}
							out = append(out, decl{e.Name(), kind, n.Name, "", fset.Position(n.Pos()).Line})
						}
					}
				}
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].file != out[j].file {
			return out[i].file < out[j].file
		}
		return out[i].line < out[j].line
	})
	for _, d := range out {
		exp := "unexported"
		if ast.IsExported(d.name) {
			exp = "exported"
		}
		name := d.name
		if d.recv != "" {
			name = d.recv + "." + d.name
		}
		fmt.Printf("%s\t%d\t%s\t%s\t%s\n", d.file, d.line, d.kind, name, exp)
	}
}

func typeString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeString(t.X)
	case *ast.IndexExpr:
		return typeString(t.X)
	case *ast.IndexListExpr:
		return typeString(t.X)
	}
	return ""
}
