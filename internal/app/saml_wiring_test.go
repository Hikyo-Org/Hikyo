package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// The guarded metadata transport is private to service, so app wiring cannot
// inspect it behaviorally. Pin the production construction boundary instead:
// Production graph wiring must call the constructor and must not restore a
// broad struct literal, regardless of which app file owns graph construction.
func TestProductionWiringUsesGuardedSAMLProviderConstructor(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	constructorCalls := 0
	structLiterals := 0
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.CallExpr:
				if isServiceSelector(node.Fun, "NewSAMLProviders") {
					constructorCalls++
				}
			case *ast.CompositeLit:
				if isServiceSelector(node.Type, "SAMLProviders") {
					structLiterals++
				}
			}
			return true
		})
	}
	if constructorCalls != 1 || structLiterals != 0 {
		t.Fatalf("SAML provider wiring constructor calls = %d, struct literals = %d; want 1, 0",
			constructorCalls, structLiterals)
	}
}

func isServiceSelector(expression ast.Expr, name string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != name {
		return false
	}
	packageName, ok := selector.X.(*ast.Ident)
	return ok && packageName.Name == "service"
}
