package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"
)

// TestBumpSchemaRevisionOnlyThroughBudget pins the § 151 invariant structurally:
// r.Catalogue().BumpSchemaRevision is called in exactly ONE place in this
// package's production code — the body of bumpSchemaRevision, the single helper
// that charges the 60/h per-project schema-revision rate before it advances the
// revision. A future schema-mutating method that bumps the revision directly
// skips the paired charge and quietly bypasses the bound. The budget totality
// test only catches an unclassified operation, not an unpaired bump, so no
// behavioral test can catch this class; this one can, before it ships.
func TestBumpSchemaRevisionOnlyThroughBudget(t *testing.T) {
	fset := token.NewFileSet()
	pkg, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var sites []string
	for _, p := range pkg {
		for name, file := range p.Files {
			var enclosing string
			ast.Inspect(file, func(n ast.Node) bool {
				if fn, ok := n.(*ast.FuncDecl); ok {
					enclosing = fn.Name.Name
				}
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "BumpSchemaRevision" {
					return true
				}
				sites = append(sites, enclosing+" ("+name+":"+
					fset.Position(sel.Pos()).String()+")")
				return true
			})
		}
	}
	if len(sites) != 1 || !strings.HasPrefix(sites[0], "bumpSchemaRevision") {
		t.Fatalf("BumpSchemaRevision called in %d site(s), want exactly 1 in bumpSchemaRevision: %v", len(sites), sites)
	}
}
