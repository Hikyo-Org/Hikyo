package lint

import (
	"fmt"
	"go/ast"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
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
	// Ignores privacy and containment state like the recovery lookup, and returns
	// only the account id for the containment authority sweep.
	{Name: "GetAccountIDForContainmentSweep", Allowed: map[string][]string{
		Module + "/internal/store/authn": {"Resolver.containmentSweepAccount"},
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

type confinementKind struct{ name, doc, where string }

var (
	recoveryKind = confinementKind{"recoverylookup", "the recovery lookup is reached only from the reset and establishment flows", "the reset and establishment flows"}
	setterKind   = confinementKind{"containmentsetters", "the containment setters are reached only from the authz forwarders and the authn store", "the authz forwarders and the authn store"}
)

// RecoveryLookup and ContainmentSetters are the two confinement analyzers.
var (
	RecoveryLookup     = newConfinement(recoveryKind, RecoveryLookupRules)
	ContainmentSetters = newConfinement(setterKind, ContainmentSetterRules)
)

// ruleTargets are the packages the rules license, which must all be analyzed.
func ruleTargets(rules []RecoveryRule) []string {
	set := map[string]bool{}
	for _, rule := range rules {
		for path := range rule.Allowed {
			set[path] = true
		}
	}
	return slices.Sorted(maps.Keys(set))
}

func newConfinement(kind confinementKind, rules []RecoveryRule) *analysis.Analyzer {
	where := kind.where
	a := &analysis.Analyzer{Name: kind.name, Doc: kind.doc}
	a.Flags.Var((*confinementFlag)(&rules), "rules", "Name=pkg:Func,Func|pkg:Func;Name=... confinement rules")
	a.Run = func(pass *analysis.Pass) (any, error) {
		runConfinement(pass, rules, where)
		return nil, nil
	}
	return a
}

func runConfinement(pass *analysis.Pass, rules []RecoveryRule, where string) {
	path := pass.Pkg.Path()
	if !strings.HasPrefix(path, Module) {
		return
	}
	licensed := false
	for _, rule := range rules {
		if _, ok := rule.Allowed[path]; ok {
			licensed = true
		}
	}
	if licensed {
		reportTarget(pass)
	}
	seen := map[string]bool{}
	for _, file := range pass.Files {
		if isTestFile(fileName(pass, file.Pos())) {
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
					if slices.Contains(rule.Allowed[path], owner) {
						seen[rule.Name+"|"+owner] = true
					} else {
						pass.Reportf(sel.Pos(), "%s referenced outside %s", rule.Name, where)
					}
				}
				return true
			})
		}
	}
	for _, rule := range rules {
		for _, fn := range rule.Allowed[path] {
			if !seen[rule.Name+"|"+fn] {
				pass.Reportf(packagePos(pass), "%s never referenced in %s.%s (stale allowlist entry)", rule.Name, path, fn)
			}
		}
	}
}

// confinementFlag carries the rules through a vet flag as
// "Name=pkg:Func,Func|pkg:Func;Name=...". The flag exists to inject fixture
// rules; the production rules are compiled in, and unitchecker's -V=full
// hashes the tool binary, so a changed allowlist already changes the cache key.
type confinementFlag []RecoveryRule

func (f *confinementFlag) String() string {
	if f == nil {
		return ""
	}
	parts := make([]string, len(*f))
	for i, rule := range *f {
		var allowed []string
		for _, path := range slices.Sorted(maps.Keys(rule.Allowed)) {
			allowed = append(allowed, path+":"+strings.Join(rule.Allowed[path], ","))
		}
		parts[i] = rule.Name + "=" + strings.Join(allowed, "|")
	}
	return strings.Join(parts, ";")
}

func (f *confinementFlag) Set(value string) error {
	var parsed []RecoveryRule
	for entry := range strings.SplitSeq(value, ";") {
		name, allowed, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			return fmt.Errorf("confinement rule %q is not Name=pkg:Func,Func", entry)
		}
		rule := RecoveryRule{Name: name, Allowed: map[string][]string{}}
		for part := range strings.SplitSeq(allowed, "|") {
			if part == "" {
				continue
			}
			path, fns, ok := strings.Cut(part, ":")
			if !ok || path == "" {
				return fmt.Errorf("confinement rule %q has no pkg:Func entry %q", entry, part)
			}
			names := strings.Split(fns, ",")
			if slices.Contains(names, "") {
				// "" is the key of package-level declarations, which no entry may allow.
				return fmt.Errorf("confinement rule %q has an empty function name in %q", entry, part)
			}
			rule.Allowed[path] = names
		}
		parsed = append(parsed, rule)
	}
	*f = parsed
	return nil
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
