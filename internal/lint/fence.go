package lint

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Fence is the fence analyzer over the service package.
var Fence = newFence(Module + "/internal/service")

func newFence(service string) *analysis.Analyzer {
	a := &analysis.Analyzer{
		Name: "fence",
		Doc:  "functions that seal ciphertext also call the writer fence",
	}
	a.Flags.StringVar(&service, "service", service, "import path of the service package")
	a.Run = func(pass *analysis.Pass) (any, error) {
		runFence(pass, service)
		return nil, nil
	}
	return a
}

func runFence(pass *analysis.Pass, servicePath string) {
	if pass.Pkg.Path() != servicePath {
		return
	}
	reportTarget(pass)
	for _, file := range pass.Files {
		if strings.HasSuffix(fileName(pass, file.Pos()), "reencrypt.go") {
			continue // the mover re-seals under the new active version by design
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if marked(fn, "fence:delegated") || marked(fn, "fence:exempt") {
				continue
			}
			seals, fences := false, false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch {
				case sealCall(pass.TypesInfo, call):
					seals = true
				case fenceCall(pass.TypesInfo, call, servicePath):
					fences = true
				}
				return true
			})
			if seals && !fences {
				pass.Reportf(fn.Pos(),
					"%s seals ciphertext but never calls the writer fence (invariant 7) — "+
						"add fenceProject/fenceInstance(Version) or az.AssertActiveInstanceDEKVersion before the write, "+
						"or mark the function fence:delegated / fence:exempt with a reason",
					fn.Name.Name)
			}
		}
	}
}

// marked reports whether a function's doc comment carries a marker token.
func marked(fn *ast.FuncDecl, token string) bool {
	return fn.Doc != nil && strings.Contains(fn.Doc.Text(), token)
}

// sealCall reports a call to ProjectSealer.SealValue/SealField/SealPersonalValue or
// InstanceSealer.SealField, resolved through the type-checked selection so a
// same-named method on any other type does not match.
func sealCall(info *types.Info, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	selection := info.Selections[sel]
	if selection == nil {
		return false
	}
	name := selection.Obj().Name()
	if name != "SealValue" && name != "SealField" && name != "SealPersonalValue" {
		return false
	}
	return namedTypeIs(selection.Recv(), Module+"/internal/crypto", "ProjectSealer", "InstanceSealer")
}

// fenceCall reports a call to one of the fence helpers or the authn-surface
// fence. The helpers are package functions in internal/service; the authn
// fence is a method named AssertActiveInstanceDEKVersion.
func fenceCall(info *types.Info, call *ast.CallExpr, servicePath string) bool {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if obj, ok := info.Uses[fun].(*types.Func); ok {
			switch obj.Name() {
			case "fenceProject", "fenceInstance", "fenceInstanceVersion":
				return obj.Pkg() != nil && obj.Pkg().Path() == servicePath
			}
		}
	case *ast.SelectorExpr:
		if selection := info.Selections[fun]; selection != nil {
			return selection.Obj().Name() == "AssertActiveInstanceDEKVersion"
		}
	}
	return false
}

// namedTypeIs reports whether t (possibly a pointer) is a named type from pkgPath
// whose name is one of names.
func namedTypeIs(t types.Type, pkgPath string, names ...string) bool {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	n, ok := t.(*types.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != pkgPath {
		return false
	}
	for _, name := range names {
		if n.Obj().Name() == name {
			return true
		}
	}
	return false
}
