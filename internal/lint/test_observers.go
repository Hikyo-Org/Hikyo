package lint

import (
	"fmt"
	"go/types"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

func defaultObserverInstallers() map[string][]string {
	return map[string][]string{
		Module + "/internal/store/authn": {"SetQueryObserver", "SetMutationFailureObserver"},
		Module + "/internal/service":     {"SetSCIMPhaseObserver"},
	}
}

// TestObservers is the testobservers analyzer.
var TestObservers = newTestObservers(defaultObserverInstallers())

func newTestObservers(installers map[string][]string) *analysis.Analyzer {
	a := &analysis.Analyzer{
		Name: "testobservers",
		Doc:  "mutable acceptance-test hook installers are referenced only from tests",
	}
	a.Flags.Var((*installersFlag)(&installers), "installers", "owner=Name,Name;owner=Name installer inventory")
	a.Run = func(pass *analysis.Pass) (any, error) {
		runTestObservers(pass, installers)
		return nil, nil
	}
	return a
}

func runTestObservers(pass *analysis.Pass, installers map[string][]string) {
	if names, ok := installers[pass.Pkg.Path()]; ok {
		reportTarget(pass)
		for _, name := range names {
			if _, ok := pass.Pkg.Scope().Lookup(name).(*types.Func); !ok {
				pass.Reportf(packagePos(pass), "installer %s.%s not found", pass.Pkg.Path(), name)
			}
		}
	}
	if !strings.HasPrefix(pass.Pkg.Path(), Module+"/") {
		return
	}
	for id, obj := range pass.TypesInfo.Uses {
		fn, ok := obj.(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Type().(*types.Signature).Recv() != nil {
			continue
		}
		for _, name := range installers[fn.Pkg().Path()] {
			if fn.Name() != name {
				continue
			}
			if !strings.HasSuffix(fileName(pass, id.Pos()), "_test.go") {
				pass.Reportf(id.Pos(), "production reference to %s.%s", fn.Pkg().Path(), name)
			}
		}
	}
}

// installersFlag carries the installer inventory through a vet flag as
// "owner=Name,Name;owner=Name", so a changed inventory changes the vet cache key.
type installersFlag map[string][]string

func (f *installersFlag) String() string {
	if f == nil {
		return ""
	}
	owners := slices.Sorted(maps.Keys(*f))
	parts := make([]string, len(owners))
	for i, owner := range owners {
		parts[i] = owner + "=" + strings.Join((*f)[owner], ",")
	}
	return strings.Join(parts, ";")
}

func (f *installersFlag) Set(value string) error {
	parsed := map[string][]string{}
	for entry := range strings.SplitSeq(value, ";") {
		owner, names, ok := strings.Cut(entry, "=")
		if !ok || owner == "" || names == "" {
			return fmt.Errorf("installer entry %q is not owner=Name,Name", entry)
		}
		parsed[owner] = strings.Split(names, ",")
	}
	*f = parsed
	return nil
}
