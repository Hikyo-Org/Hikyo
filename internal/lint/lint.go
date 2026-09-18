// Package lint holds the three custom static analyzers the tenant-isolation
// ADR fixes (proof signatures, SQL predicate confinement, proof-forgery
// guard). They run against the real repository from this package's tests, so
// a violation is a build failure in CI. Each is a guardrail with its evasion
// limits stated in the ADR — logic bugs inside the trusted set are review's
// job, not lint's.
//
// The ADR sketches these as go/analysis passes; they are implemented
// directly over go/packages type information instead — same inputs, same
// build-failing effect, less framework. The checks return findings as
// strings so the tests (and the invariant suite) can assert emptiness and
// print every violation at once.
package lint

import (
	"fmt"
	"sync"

	"golang.org/x/tools/go/packages"
)

// Module is the module path the analyzers reason about.
const Module = "github.com/Hikyo-Org/hikyo"

var (
	loadOnce sync.Once
	loaded   []*packages.Package
	loadErr  error
)

// LoadRepo loads every package in the module, with syntax and type
// information, exactly once per test process.
func LoadRepo() ([]*packages.Package, error) {
	loadOnce.Do(func() {
		loaded, loadErr = Load(Module + "/...")
	})
	return loaded, loadErr
}

// Load loads arbitrary patterns (the negative-fixture tests use it on
// testdata packages).
//
// The mode deliberately omits NeedDeps. The type checker still resolves every
// imported symbol — a root package's direct imports get their Types from
// compiler export data as a side effect of type-checking the root — so the
// analyzers, which reason about type identity (authz.Proof, driver handles),
// see everything they need. Adding NeedDeps alongside NeedSyntax|NeedTypesInfo
// would instead mark the entire transitive dependency closure (pgx, x/tools,
// stdlib) as "needs source", parsing every dependency's AST and building a
// full types.Info for each and keeping them all resident. On this package's
// test suite that was the difference between a ~7GB and a ~1.2GB peak RSS, and
// it made each tiny testdata load carry the full closure too; macOS Activity
// Monitor's "Memory" column inflated the former past 20GB once compression and
// -race shadow footprint were counted.
//
// The one seam: flatten() walks into imported module packages that are not
// roots of the load and therefore have Types (from export data) but no
// TypesInfo. Analyzers that reach those either select their target packages by
// path first (never touching a non-root's TypesInfo) or guard on nil type
// information before dereferencing it. In LoadRepo every module package is a
// root, so no module package is skipped there regardless.
func Load(patterns ...string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		// Test packages are in scope: a forged proof in a test outside authz
		// is the same breach as one in production code.
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	for _, p := range pkgs {
		for _, e := range p.Errors {
			return nil, fmt.Errorf("load %s: %s", p.PkgPath, e)
		}
	}
	return pkgs, nil
}
