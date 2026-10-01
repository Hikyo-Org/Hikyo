package lint

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"
)

// CheckServiceSeams confines global reauthentication duration and schema-revision
// advancement to their policy helpers. Type identity catches renamed receivers
// and method values while allowing unrelated fields and methods with those names.
// This checks the access boundary, not policy correctness inside the helpers.
func CheckServiceSeams(pkgs []*packages.Package, servicePath, storePath string) []string {
	var svc, store *packages.Package
	for _, p := range flatten(pkgs) {
		if p.PkgPath == servicePath {
			svc = p
		}
		if p.PkgPath == storePath {
			store = p
		}
	}
	if svc == nil || store == nil || svc.TypesInfo == nil || store.Types == nil {
		return []string{"service seams: service/store type information not loaded"}
	}
	auth := svc.Types.Scope().Lookup("Auth")
	catalogue := store.Types.Scope().Lookup("CatalogueRepo")
	if auth == nil || catalogue == nil {
		return []string{"service seams: Auth/CatalogueRepo types not found"}
	}
	window, _, _ := types.LookupFieldOrMethod(auth.Type(), true, svc.Types, "ReauthWindow")
	opener, _, _ := types.LookupFieldOrMethod(auth.Type(), true, svc.Types, "effectiveReauthWindow")
	bump, _, _ := types.LookupFieldOrMethod(catalogue.Type(), true, store.Types, "BumpSchemaRevision")
	budget := svc.Types.Scope().Lookup("bumpSchemaRevision")
	if window == nil || opener == nil || bump == nil || budget == nil {
		return []string{"service seams: protected members or policy helpers not found"}
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
	if settings := svc.Types.Scope().Lookup("ProjectSettings"); settings != nil {
		rules[0].otherOwner, _, _ = types.LookupFieldOrMethod(settings.Type(), true, svc.Types, "SetEnvironment")
	}
	var findings []string
	for _, file := range svc.Syntax {
		if strings.HasSuffix(svc.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			var owner types.Object
			if fn, ok := decl.(*ast.FuncDecl); ok {
				owner = svc.TypesInfo.Defs[fn.Name]
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				selection := svc.TypesInfo.Selections[sel]
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
						findings = append(findings, fmt.Sprintf("service seams: %s: %s access outside %s", svc.Fset.Position(sel.Pos()), rule.member.Name(), rule.owner.Name()))
					}
				}
				return true
			})
		}
	}
	for _, rule := range rules {
		if !rule.seen {
			findings = append(findings, fmt.Sprintf("service seams: %s never accessed in %s", rule.member.Name(), rule.owner.Name()))
		}
	}
	return findings
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
