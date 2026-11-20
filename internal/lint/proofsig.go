package lint

import (
	"fmt"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

// CheckProofSignatures is analyzer 1 (tenant-isolation ADR, invariant 7):
// every store method reachable through the repository bundles takes a proof
// as its first parameter after ctx, and no store-method signature — nor any
// struct type reachable from its parameters, transitively — carries a
// tenant-identifier-typed value (domain.OrgID / ProjectID / EnvID). Caller
// arguments must be structurally unable to reach a chain predicate; the
// stated limit is that a raw string can still smuggle an id, which never
// reaches a chain column because only the proof binds those.
//
// The repository surface is discovered, not curated: starting from
// store.Repos and store.ReadRepos, every interface reachable through method
// results is part of the surface. A new aggregate wired into the bundles is
// covered automatically; one not wired in is unreachable by services and
// therefore dead code.
func CheckProofSignatures(pkgs []*packages.Package, storePath string) []string {
	return RunOver(Check{Analyzer: newProofSig(storePath), Targets: []string{storePath}}, pkgs).Findings
}

// ProofSig is CheckProofSignatures as an analyzer over the store package.
var ProofSig = newProofSig(Module + "/internal/store")

func newProofSig(store string) *analysis.Analyzer {
	a := &analysis.Analyzer{
		Name: "proofsig",
		Doc:  "store methods take (ctx, authz.Proof, ...) and no tenant-identifier-typed parameter",
	}
	a.Flags.StringVar(&store, "store", store, "import path of the package declaring Repos and ReadRepos")
	a.Run = func(pass *analysis.Pass) (any, error) {
		runProofSig(pass, store)
		return nil, nil
	}
	return a
}

func runProofSig(pass *analysis.Pass, storePath string) {
	if pass.Pkg.Path() != storePath {
		return
	}
	reportTarget(pass)
	authzPkg := findPackage(pass.Pkg, Module+"/internal/authz")
	domainPkg := findPackage(pass.Pkg, Module+"/internal/domain")
	if authzPkg == nil || domainPkg == nil {
		pass.Report(analysis.Diagnostic{Pos: packagePos(pass), Message: "authz or domain package not reachable from the store package"})
		return
	}

	proofType := authzPkg.Scope().Lookup("Proof").Type()
	tenantTypes := map[types.Type]bool{
		domainPkg.Scope().Lookup("OrgID").Type():     true,
		domainPkg.Scope().Lookup("ProjectID").Type(): true,
		domainPkg.Scope().Lookup("EnvID").Type():     true,
	}

	scope := pass.Pkg.Scope()
	roots := []string{"Repos", "ReadRepos"}
	seen := map[*types.Interface]bool{}
	var queue []*types.Named
	for _, name := range roots {
		obj := scope.Lookup(name)
		if obj == nil {
			pass.Reportf(packagePos(pass), "store.%s not found", name)
			continue
		}
		if n, ok := obj.Type().(*types.Named); ok {
			queue = append(queue, n)
		}
	}

	for len(queue) > 0 {
		named := queue[0]
		queue = queue[1:]
		iface, ok := named.Underlying().(*types.Interface)
		if !ok || seen[iface] {
			continue
		}
		seen[iface] = true
		for m := range iface.NumMethods() {
			fn := iface.Method(m)
			sig := fn.Signature()
			// Bundle accessors (Orgs(), Environments(), ...) return further
			// interfaces: enqueue them and move on.
			if bundled := bundleResults(sig); len(bundled) > 0 {
				queue = append(queue, bundled...)
				continue
			}
			pos := fn.Pos()
			if !pos.IsValid() {
				pos = named.Obj().Pos()
			}
			label := fmt.Sprintf("store.%s.%s", named.Obj().Name(), fn.Name())
			params := sig.Params()
			if params.Len() < 2 {
				pass.Reportf(pos, "%s: store methods take (ctx, proof, ...)", label)
				continue
			}
			if !isNamed(params.At(0).Type(), "context", "Context") {
				pass.Reportf(pos, "%s: first parameter must be context.Context", label)
			}
			if !types.Identical(params.At(1).Type(), proofType) {
				pass.Reportf(pos, "%s: second parameter must be authz.Proof", label)
			}
			for i := 2; i < params.Len(); i++ {
				tenantTyped(func(msg string) { pass.Reportf(pos, "%s", msg) }, label, params.At(i).Type(), tenantTypes, map[types.Type]bool{})
			}
		}
	}
}

// bundleResults returns named interfaces among a no-parameter method's
// results (the repository-bundle accessors).
func bundleResults(sig *types.Signature) []*types.Named {
	if sig.Params().Len() != 0 {
		return nil
	}
	var out []*types.Named
	res := sig.Results()
	for i := range res.Len() {
		if n, ok := res.At(i).Type().(*types.Named); ok {
			if _, isIface := n.Underlying().(*types.Interface); isIface {
				out = append(out, n)
			}
		}
	}
	return out
}

// tenantTyped reports uses of tenant-identifier types in a parameter type,
// recursing through named structs, pointers, slices and maps.
func tenantTyped(report func(string), label string, t types.Type, tenant map[types.Type]bool, visited map[types.Type]bool) {
	if visited[t] {
		return
	}
	visited[t] = true
	if tenant[t] {
		report(fmt.Sprintf("%s: tenant-identifier-typed parameter (%s) — chain values come from the proof only", label, t))
		return
	}
	switch u := t.(type) {
	case *types.Named:
		tenantTyped(report, label, u.Underlying(), tenant, visited)
	case *types.Pointer:
		tenantTyped(report, label, u.Elem(), tenant, visited)
	case *types.Slice:
		tenantTyped(report, label, u.Elem(), tenant, visited)
	case *types.Array:
		tenantTyped(report, label, u.Elem(), tenant, visited)
	case *types.Map:
		tenantTyped(report, label, u.Key(), tenant, visited)
		tenantTyped(report, label, u.Elem(), tenant, visited)
	case *types.Struct:
		for i := range u.NumFields() {
			tenantTyped(report, label, u.Field(i).Type(), tenant, visited)
		}
	}
}

func isNamed(t types.Type, pkg, name string) bool {
	n, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := n.Obj()
	return obj.Name() == name && obj.Pkg() != nil && obj.Pkg().Name() == pkg
}

// flatten returns the load roots plus all their dependencies, deduplicated.
func flatten(pkgs []*packages.Package) []*packages.Package {
	seen := map[string]*packages.Package{}
	var walk func(p *packages.Package)
	walk = func(p *packages.Package) {
		if seen[p.PkgPath] != nil {
			return
		}
		seen[p.PkgPath] = p
		for _, dep := range p.Imports {
			walk(dep)
		}
	}
	for _, p := range pkgs {
		walk(p)
	}
	out := make([]*packages.Package, 0, len(seen))
	for _, p := range seen {
		out = append(out, p)
	}
	return out
}
