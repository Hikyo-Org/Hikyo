package lint

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// ServiceSeams is the serviceseams analyzer over the service package.
var ServiceSeams = newServiceSeams(Module+"/internal/service", Module+"/internal/store")

func newServiceSeams(service, store string) *analysis.Analyzer {
	a := &analysis.Analyzer{
		Name: "serviceseams",
		Doc:  "reauth-window and schema-revision access stays inside its policy helpers",
	}
	a.Flags.StringVar(&service, "service", service, "import path of the service package")
	a.Flags.StringVar(&store, "store", store, "import path of the package declaring CatalogueRepo")
	a.Run = func(pass *analysis.Pass) (any, error) {
		runServiceSeams(pass, service, store)
		return nil, nil
	}
	return a
}

func runServiceSeams(pass *analysis.Pass, servicePath, storePath string) {
	if pass.Pkg.Path() != servicePath {
		return
	}
	reportTarget(pass)
	svc := pass.Pkg
	store := findPackage(pass.Pkg, storePath)
	if store == nil {
		pass.Reportf(packagePos(pass), "store type information not loaded")
		return
	}
	auth := svc.Scope().Lookup("Auth")
	catalogue := store.Scope().Lookup("CatalogueRepo")
	if auth == nil || catalogue == nil {
		pass.Reportf(packagePos(pass), "Auth/CatalogueRepo types not found")
		return
	}
	window, _, _ := types.LookupFieldOrMethod(auth.Type(), true, svc, "ReauthWindow")
	opener, _, _ := types.LookupFieldOrMethod(auth.Type(), true, svc, "effectiveReauthWindow")
	bump, _, _ := types.LookupFieldOrMethod(catalogue.Type(), true, store, "BumpSchemaRevision")
	budget := svc.Scope().Lookup("bumpSchemaRevision")
	if window == nil || opener == nil || bump == nil || budget == nil {
		pass.Reportf(packagePos(pass), "protected members or policy helpers not found")
		return
	}
	rules := []struct {
		member, owner types.Object
		otherOwner    types.Object
		seen          bool
	}{
		{member: window, owner: opener},
		{member: bump, owner: budget},
	}
	// Settings updates compare the old/new duration to revoke live windows
	// after a shrink. This exact method may inspect the global default, but
	// window openers and disclosure slides must use effectiveReauthWindow.
	if settings := svc.Scope().Lookup("ProjectSettings"); settings != nil {
		rules[0].otherOwner, _, _ = types.LookupFieldOrMethod(settings.Type(), true, svc, "SetEnvironment")
	}
	for _, file := range pass.Files {
		if strings.HasSuffix(fileName(pass, file.Pos()), "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			var owner types.Object
			if fn, ok := decl.(*ast.FuncDecl); ok {
				owner = pass.TypesInfo.Defs[fn.Name]
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				selection := pass.TypesInfo.Selections[sel]
				if selection == nil {
					return true
				}
				for i := range rules {
					rule := &rules[i]
					if selection.Obj() != rule.member && !sameMethodContract(selection.Obj(), rule.member) {
						continue
					}
					if owner == rule.owner {
						rule.seen = true
					} else if owner == nil || owner != rule.otherOwner {
						pass.Reportf(sel.Pos(), "%s access outside %s", rule.member.Name(), rule.owner.Name())
					}
				}
				return true
			})
		}
	}
	for _, rule := range rules {
		if !rule.seen {
			pass.Reportf(packagePos(pass), "%s never accessed in %s", rule.member.Name(), rule.owner.Name())
		}
	}
}

// A structural interface can rename the declaring type while preserving the
// protected method contract. Compare its receiver-free signature as well.
func sameMethodContract(candidate, member types.Object) bool {
	fn, ok := candidate.(*types.Func)
	want, protected := member.(*types.Func)
	if !ok || !protected || fn.Name() != want.Name() {
		return false
	}
	signature := func(fn *types.Func) *types.Signature {
		sig := fn.Type().(*types.Signature)
		return types.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
	}
	return types.Identical(signature(fn), signature(want))
}
