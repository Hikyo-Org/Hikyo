package lint

import (
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Grant-lock analyzer (#54 B14). credential-reset's org-bounded test is made
// serializable by locking the target's `principals` row: the reset reads the
// target's grant set under that lock, so a concurrent grant landing must take the
// same lock and therefore serialize behind (or ahead of) the reset. That
// guarantee holds only if EVERY grant writer takes the lock. The general grant
// surface is #55's; this analyzer pins the obligation now so a future grant
// writer cannot forget it — a grant writer that skips the lock is a build
// failure, not a review miss.
//
// The check is structural, like the sole-writer analyzer it is modelled on: any
// function — in ANY module package, not just the resolution surface — that names
// a grant-table-mutating generated query must also take the principal-row lock in
// the same body, and the lock must type-resolve to the REAL LockPrincipalRow (the
// generated query or the authn resolver wrapper that calls it), not merely any
// selector spelled that way. Residual, stated so it is not mistaken for more:
// the analyzer cannot prove the lock is taken on the RIGHT principal row, nor
// that it precedes the write in execution order within the same transaction —
// that is review's job. Where it cannot be sure it errs toward flagging
// (fail-closed): a grant writer with no resolvable lock at all is a build
// failure, never a silent pass.

// grantTables are the tables whose writers must hold the principal-row lock.
// `grant_origins` joins `grants` (#55): releasing the last origin deletes the
// grant row, so an origin write is a grant write with one more step, and the
// same serialization obligation binds it.
// The member access rule tables join them (member-access-rules ADR): a rule is
// standing authority read by authorize(), so its writers serialize the same way.
// The footprint read (ListPrincipalFootprintOrgs) also covers access_grants,
// scim_users, developer_credentials and service_accounts; those writers are not
// grant writers here, and their consistency with it rests on serializable
// isolation, not on this row lock.
var grantTables = map[string]bool{"grants": true, "grant_origins": true, "rules": true, "rule_items": true}

// lockName is the lock taken before a grant write.
const lockName = "LockPrincipalRow"

// lockDefiners are the packages that define the real principal-row lock: the
// generated query packages and the resolution-surface wrapper that calls them
// (authn.CreateGrant reaches the lock through the wrapper). A selector named
// LockPrincipalRow that type-resolves anywhere else is NOT the lock and does not
// satisfy the obligation.
var lockDefiners = map[string]bool{
	Module + "/internal/store/sqlitegen": true,
	Module + "/internal/store/pggen":     true,
	Module + "/internal/store/authn":     true,
}

// queryInputs are the sqlc query files the grant and denial analyzers derive
// their writer sets from.
func queryInputs(string) ([]InputSpec, error) {
	return []InputSpec{
		{Dir: "internal/store/queries/sqlite", Exts: []string{".sql"}},
		{Dir: "internal/store/queries/postgres", Exts: []string{".sql"}},
	}, nil
}

// grantWriters returns the mutating generated queries that target the grant
// table, derived from the query files rather than a curated list so a new grant
// writer is covered the moment it is added. Classification is by SQL verb/target,
// NOT the sqlc command annotation: an `INSERT ... RETURNING` is a `:one` yet
// writes a grant row (the old cmd-based skip classified it as a read and let it
// through). Only a SELECT on the grant table is a read.
func grantWriters(src source, repoRoot string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, engine := range []string{"sqlite", "postgres"} {
		queries, err := parseQueries(src, filepath.Join(repoRoot, "internal", "store", "queries", engine))
		if err != nil {
			return nil, err
		}
		for _, q := range queries {
			table, kind, ok := statementTarget(strings.ToUpper(normalizeSpace(q.SQL)))
			if !ok || !grantTables[strings.ToLower(table)] {
				continue
			}
			switch kind {
			case "INSERT", "UPDATE", "DELETE":
				out[q.Name] = true
			}
		}
	}
	return out, nil
}

// reachesGenerated reports whether pkg can name a generated query: it is, or
// transitively imports, a sqlc output package.
func reachesGenerated(pkg *types.Package) bool {
	for path := range generatedPackages {
		if findPackage(pkg, path) != nil {
			return true
		}
	}
	return false
}

// GrantLock enforces the obligation over EVERY module package that can call a
// generated grant-mutation query, not only the resolution surface: a grant
// write is a grant write wherever it is issued from, and its in-package test
// files are packages' callers too. It skips the generated query packages
// themselves (whose method bodies ARE the queries, not callers of them);
// testdata fixtures are never loaded by the `module/...` pattern, so the
// negative fixture does not false-positive the repo run. The writer set comes
// from the declared query files.
var GrantLock = newGrantLock()

func newGrantLock() *analysis.Analyzer {
	a := &analysis.Analyzer{Name: "grantlock", Doc: "every grant-table writer takes the principal-row lock"}
	surface := ""
	a.Flags.StringVar(&surface, "surface", "", "analyze only this package (default: every module package)")
	in := flagInputs(a)
	a.Run = func(pass *analysis.Pass) (any, error) {
		path := pass.Pkg.Path()
		if !inModule(path) || generatedPackages[path] || (surface != "" && path != surface) {
			return nil, nil
		}
		if path == surface || path == Module+"/internal/store/authn" {
			reportTarget(pass)
		}
		if !reachesGenerated(pass.Pkg) {
			return nil, nil
		}
		src, root, err := in.open()
		if err != nil {
			reportRefusal(pass, err)
			return nil, nil
		}
		writers, err := grantWriters(src, root)
		if err != nil {
			pass.Report(analysis.Diagnostic{Pos: packagePos(pass), Message: err.Error()})
			return nil, nil
		}
		if len(writers) == 0 {
			pass.Report(analysis.Diagnostic{Pos: packagePos(pass), Message: "no grant-table writers found — the analyzer would be vacuously green"})
			return nil, nil
		}
		checkGrantLockPass(pass, writers, lockName)
		return nil, nil
	}
	return a
}

// checkGrantLockPass reports every function of the pass's package that names a
// grant-mutating writer but does not take the real principal-row lock in the
// same body.
func checkGrantLockPass(pass *analysis.Pass, writers map[string]bool, lock string) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			var namesWriter, namesLock bool
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				obj, ok := pass.TypesInfo.Uses[sel.Sel]
				if !ok {
					return true
				}
				f, ok := obj.(*types.Func)
				if !ok || f.Pkg() == nil {
					return true
				}
				pkgPath := f.Pkg().Path()
				// The lock counts only when it type-resolves to the REAL
				// LockPrincipalRow — the generated query or the authn wrapper —
				// not merely a selector spelled that way (a struct field or an
				// unrelated method named LockPrincipalRow no longer satisfies it).
				if f.Name() == lock && lockDefiners[pkgPath] {
					namesLock = true
				}
				if generatedPackages[pkgPath] && writers[f.Name()] {
					namesWriter = true
				}
				return true
			})
			if namesWriter && !namesLock {
				pass.Report(analysis.Diagnostic{Pos: fn.Pos(), Message: fmt.Sprintf(
					"%s writes a grant table but does not take the %s principal-row lock — every grant writer must hold it so credential-reset's org-bounded test is serializable (#54 B14)",
					fn.Name.Name, lock)})
			}
			return true
		})
	}
}
