package lint

import (
	"fmt"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"
)

// CheckTestObservers prohibits production references to mutable acceptance-test
// hook installers, including function values and uses in their defining files.
// Type resolution recognizes import aliases and ignores unrelated names.
func CheckTestObservers(pkgs []*packages.Package) []string {
	return checkTestObserversIn(pkgs, map[string][]string{
		Module + "/internal/store/authn": {"SetQueryObserver", "SetMutationFailureObserver"},
		Module + "/internal/service":     {"SetSCIMPhaseObserver"},
	})
}

func checkTestObserversIn(pkgs []*packages.Package, installers map[string][]string) []string {
	var findings []string
	all := flatten(pkgs)
	owners := map[string]*packages.Package{}
	for _, pkg := range all {
		owners[pkg.PkgPath] = pkg
	}
	for path, names := range installers {
		owner := owners[path]
		if owner == nil || owner.Types == nil {
			findings = append(findings, fmt.Sprintf("test observers: owner package %s not loaded", path))
			continue
		}
		for _, name := range names {
			if _, ok := owner.Types.Scope().Lookup(name).(*types.Func); !ok {
				findings = append(findings, fmt.Sprintf("test observers: installer %s.%s not found", path, name))
			}
		}
	}
	for _, pkg := range all {
		if !strings.HasPrefix(pkg.PkgPath, Module+"/") || pkg.TypesInfo == nil {
			continue
		}
		for id, obj := range pkg.TypesInfo.Uses {
			fn, ok := obj.(*types.Func)
			if !ok || fn.Pkg() == nil || fn.Type().(*types.Signature).Recv() != nil {
				continue
			}
			for _, name := range installers[fn.Pkg().Path()] {
				if fn.Name() != name {
					continue
				}
				pos := pkg.Fset.Position(id.Pos())
				if !strings.HasSuffix(pos.Filename, "_test.go") {
					findings = append(findings, fmt.Sprintf("test observers: %s: production reference to %s.%s", pos, fn.Pkg().Path(), name))
				}
			}
		}
	}
	return findings
}
