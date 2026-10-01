package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/multicall"
)

func TestModeRegistryMatchesDispatch(t *testing.T) {
	fset := token.NewFileSet()
	constantsFile, err := parser.ParseFile(fset, "../../internal/multicall/modes.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	constants := map[string]string{}
	ast.Inspect(constantsFile, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || len(spec.Values) != 1 {
			return true
		}
		if literal, ok := spec.Values[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
			constants[spec.Names[0].Name], _ = strconv.Unquote(literal.Value)
		}
		return true
	})
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var runBody *ast.BlockStmt
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == "run" {
			runBody = function.Body
		}
	}
	if runBody == nil {
		t.Fatal("run dispatcher missing")
	}
	dispatched := map[string]bool{}
	ast.Inspect(runBody, func(node ast.Node) bool {
		clause, ok := node.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, expression := range clause.List {
			switch value := expression.(type) {
			case *ast.BinaryExpr:
				name, ok := value.X.(*ast.Ident)
				literal, isLiteral := value.Y.(*ast.BasicLit)
				if ok && name.Name == "cmd" && value.Op == token.EQL && isLiteral && literal.Kind == token.STRING {
					mode, _ := strconv.Unquote(literal.Value)
					dispatched[mode] = true
				}
			case *ast.SelectorExpr:
				owner, ok := value.X.(*ast.Ident)
				if ok && owner.Name == "multicall" {
					mode, known := constants[value.Sel.Name]
					if !known {
						t.Errorf("unresolved dispatch constant %s", value.Sel.Name)
					} else {
						dispatched[mode] = true
					}
				}
			}
		}
		return true
	})
	seen := map[string]bool{}
	for _, mode := range multicall.Modes() {
		if seen[mode.Name] || mode.Name == "" {
			t.Errorf("duplicate or empty registry mode %q", mode.Name)
		}
		seen[mode.Name] = true
		if !dispatched[mode.Name] {
			t.Errorf("registered mode %q has no dispatcher", mode.Name)
		}
	}
	for mode := range dispatched {
		if _, known := multicall.Lookup(mode); !known {
			t.Errorf("dispatched mode %q has no canonical classification inventory entry", mode)
		}
	}
}
