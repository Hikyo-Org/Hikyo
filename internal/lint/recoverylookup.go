package lint

import (
	"fmt"
	"go/ast"
	"strings"

	"golang.org/x/tools/go/packages"
)

// RecoveryRule confines one protected name (the containment-bypassing recovery
// lookup, or a containment setter) to the functions that may reference it.
type RecoveryRule struct {
	// Name is the selector the lookup is reached by (a method or a generated
	// query). Matching is by name alone, so a renamed receiver, a local
	// interface, a method value or a type assertion cannot route around it.
	Name string
	// Allowed maps a package path to the functions in it that may name Name. A
	// method is keyed "Receiver.Method" (the receiver's type name, without a
	// pointer or type arguments) and a plain function by its name, so a method of
	// the same name on another type in the package is not licensed with it.
	Allowed map[string][]string
}

// RecoveryLookupRules is the one recovery lookup (incident-containment ADR):
// the resolver methods are reached only from credential reset issuance and
// credential establishment through the authz forwarders, and the generated
// query only from the resolver's single private helper.
var RecoveryLookupRules = []RecoveryRule{
	{Name: "RecoveryAccountByID", Allowed: map[string][]string{
		Module + "/internal/service": {"Auth.EstablishCredential"},
		Module + "/internal/authz":   {"TxAuthorizer.RecoveryAccountByID"},
	}},
	{Name: "RecoveryAccountByPrincipal", Allowed: map[string][]string{
		Module + "/internal/service": {"Auth.ResetCredential", "Auth.BreakGlassResetCredential"},
		Module + "/internal/authz":   {"TxAuthorizer.RecoveryAccountByPrincipal"},
	}},
	{Name: "GetAccountForRecovery", Allowed: map[string][]string{
		Module + "/internal/store/authn": {"Resolver.recoveryAccount"},
	}},
}

// ContainmentSetterRules confines the two setters of the containment state.
// In S1 no service code may call them: only the authz forwarders and the
// store resolver that implements them. The incident service (S3) adds its
// own entry here, in the same change that introduces it.
var ContainmentSetterRules = []RecoveryRule{
	{Name: "ContainPrincipal", Allowed: map[string][]string{
		Module + "/internal/authz":       {"TxAuthorizer.ContainPrincipal"},
		Module + "/internal/store/authn": {"Resolver.ContainPrincipal"},
	}},
	{Name: "ReleasePrincipalContainment", Allowed: map[string][]string{
		Module + "/internal/authz":       {"TxAuthorizer.ReleasePrincipalContainment"},
		Module + "/internal/store/authn": {"Resolver.ReleasePrincipalContainment"},
	}},
}

// CheckRecoveryLookup reports every non-test reference to a protected name
// from a function outside its allowlist, and every allowlisted function that
// never references it (a stale entry would silently widen the surface).
func CheckRecoveryLookup(pkgs []*packages.Package, rules []RecoveryRule) []string {
	return checkSelectorConfinement(pkgs, rules, "recovery lookup", "the reset and establishment flows")
}

// CheckContainmentSetters applies the same confinement to the containment
// setters, so no production caller can set or clear the state unreviewed.
func CheckContainmentSetters(pkgs []*packages.Package, rules []RecoveryRule) []string {
	return checkSelectorConfinement(pkgs, rules, "containment setter", "the authz forwarders and the authn store")
}

func checkSelectorConfinement(pkgs []*packages.Package, rules []RecoveryRule, label, where string) []string {
	seen := map[string]bool{}
	var findings []string
	for _, pkg := range flatten(pkgs) {
		if !strings.HasPrefix(pkg.PkgPath, Module) {
			continue
		}
		for _, file := range pkg.Syntax {
			if strings.HasSuffix(pkg.Fset.Position(file.Pos()).Filename, "_test.go") {
				continue
			}
			for _, decl := range file.Decls {
				owner := ""
				if fn, ok := decl.(*ast.FuncDecl); ok {
					owner = declName(fn)
				}
				ast.Inspect(decl, func(n ast.Node) bool {
					sel, ok := n.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					for _, rule := range rules {
						if sel.Sel.Name != rule.Name {
							continue
						}
						allowed := false
						for _, fn := range rule.Allowed[pkg.PkgPath] {
							if fn == owner {
								allowed = true
								seen[rule.Name+"|"+pkg.PkgPath+"|"+fn] = true
							}
						}
						if !allowed {
							findings = append(findings, fmt.Sprintf("%s: %s: %s referenced outside %s", label, pkg.Fset.Position(sel.Pos()), rule.Name, where))
						}
					}
					return true
				})
			}
		}
	}
	for _, rule := range rules {
		for path, fns := range rule.Allowed {
			for _, fn := range fns {
				if !seen[rule.Name+"|"+path+"|"+fn] {
					findings = append(findings, fmt.Sprintf("%s: %s never referenced in %s.%s (stale allowlist entry)", label, rule.Name, path, fn))
				}
			}
		}
	}
	return findings
}

// declName is the allowlist key of a function declaration: "Receiver.Method"
// for a method, the bare name for a function.
func declName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	typ := fn.Recv.List[0].Type
	for {
		switch t := typ.(type) {
		case *ast.StarExpr:
			typ = t.X
		case *ast.ParenExpr:
			typ = t.X
		case *ast.IndexExpr:
			typ = t.X
		case *ast.IndexListExpr:
			typ = t.X
		case *ast.Ident:
			return t.Name + "." + fn.Name.Name
		default:
			return fn.Name.Name
		}
	}
}
