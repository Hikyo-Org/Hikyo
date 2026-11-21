package lint

import (
	"cmp"
	"fmt"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// driverHandles are the store's raw driver-handle accessors. They exist for
// the transaction package (which must speak to the engine to open a
// transaction) and the test harnesses (fixture seeding, query-count
// instrumentation) — Go has no friend packages, so the accessors are
// exported and this check is what keeps them narrow.
//
// Without it the whole chokepoint is bypassable in one line: any package
// holding a *store.DB could run `pggen.New(db.PG()).GetEnvironment(...)` (or
// raw SQL) with caller-controlled chain values and no proof at all. The
// proof-signature analyzer only sees the repository interfaces and the SQL
// analyzer only sees the query text, so every other guardrail stays green.
var driverHandles = map[string]bool{
	"PG": true, "SQLiteWrite": true, "SQLiteRead": true,
	"BeginSQLite": true, "BeginPostgres": true, "BeginPostgresSerialized": true,
}

// driverTypes are the concrete engine handles. Naming one at all — in a
// signature, a field, a local, or a locally-declared interface method — is
// what the accessor check alone cannot see: a package can declare
// `interface{ PG() *pgxpool.Pool }`, accept a *store.DB through it, and call
// PG() on the LOCAL interface, so the selector never resolves to the store's
// method. Requiring that non-allowlisted packages cannot even name these
// types closes that escape and the type-assertion variant with it, because
// both must write the driver type somewhere.
//
// The remaining trusted set is the explicit source-file list in
// handle_positions.go. Changes inside those files still require review.
// Keyed by the NAMED type, not its pointer spelling: matching "*pkg.Pool"
// as a string misses `*pool` where `type pool = pgxpool.Pool`, and misses a
// value-typed occurrence entirely.
var driverTypes = map[string]bool{
	"github.com/jackc/pgx/v5/pgxpool.Pool":                 true,
	"github.com/jackc/pgx/v5/pgxpool.Conn":                 true,
	"github.com/jackc/pgx/v5.Tx":                           true,
	"github.com/jackc/pgx/v5.Conn":                         true,
	"database/sql.DB":                                      true,
	"database/sql.Tx":                                      true,
	"database/sql.Conn":                                    true,
	Module + "/internal/store.SQLiteTransaction":           true,
	Module + "/internal/store.PostgresTransaction":         true,
	Module + "/internal/store.RecoverySQLiteTransaction":   true,
	Module + "/internal/store.RecoveryPostgresTransaction": true,
}

// namedKey is a named type's stable identity, independent of how any use
// site spells it (pointer, alias, generic instantiation).
func namedKey(n *types.Named) string {
	obj := n.Obj()
	if obj == nil || obj.Pkg() == nil {
		return ""
	}
	return obj.Pkg().Path() + "." + obj.Name()
}

// generatedPackages are the sqlc outputs. Reaching them directly is the same
// bypass as a raw handle: generated queries take chain parameters as plain
// values, so a caller that can call them can pass any tenant's chain.
var generatedPackages = map[string]bool{
	Module + "/internal/store/sqlitegen": true,
	Module + "/internal/store/pggen":     true,
}

// generatedImporters may import the sqlc outputs.
var generatedImporters = map[string]bool{
	Module + "/internal/store/upgrade":   true, // closed candidate-health wrapper inventory under migration exclusion
	Module + "/internal/store":           true, // the binding layer
	Module + "/internal/store/authn":     true, // the resolution surface
	Module + "/internal/store/auditrow":  true, // shared audit Row→params mapping
	Module + "/internal/store/sqlitegen": true,
	Module + "/internal/store/pggen":     true,
	Module + "/internal/isolation":       true, // DBTX instrumentation (tests)
	Module + "/internal/boundary":        true, // names them as strings only
}

// Handles is the handles analyzer. Imports are judged from the
// files of the variant being analyzed.
var Handles = &analysis.Analyzer{
	Name: "handles",
	Doc:  "raw driver handles and generated query packages stay inside their allowlists",
	Run: func(pass *analysis.Pass) (any, error) {
		if !inModule(pass.Pkg.Path()) {
			return nil, nil
		}
		runHandles(pass)
		return nil, nil
	},
}

func runHandles(pass *analysis.Pass) {
	base := pass.Pkg.Path()
	info := pass.TypesInfo
	for ident, obj := range info.Uses {
		if obj.Pkg() == nil || obj.Pkg().Path() != Module+"/internal/store" || obj.Name() != "openConfigured" {
			continue
		}
		if !permittedRawConstructor(pass.Fset, base, ident.Pos()) {
			pass.Reportf(ident.Pos(), "private datastore construction is confined to explicit runtime, preparation and recovery openers")
		}
	}
	for ident, obj := range info.Uses {
		if permittedHandlePosition(pass.Fset, base, ident.Pos()) {
			continue
		}
		fn, ok := obj.(*types.Func)
		if !ok || !driverHandles[fn.Name()] {
			continue
		}
		if fn.Pkg() == nil || fn.Pkg().Path() != Module+"/internal/store" {
			continue
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok || sig.Recv() == nil {
			continue
		}
		pass.Reportf(ident.Pos(),
			"%s calls store.DB.%s — a raw driver handle bypasses the proof boundary entirely", base, fn.Name())
	}
	// Naming a driver type at all: catches the structural-interface
	// and type-assertion escapes, where the accessor call resolves to
	// a locally declared method rather than the store's.
	type naming struct {
		pos   token.Pos
		named string
	}
	var namings []naming
	collect := func(pos token.Pos, named string) {
		if named == "" || permittedHandlePosition(pass.Fset, base, pos) || permittedGuardedTransaction(pass.Fset, base, pos, named) {
			return
		}
		namings = append(namings, naming{pos, named})
	}
	for ident, obj := range info.Defs {
		if obj == nil || ident == nil {
			continue
		}
		collect(ident.Pos(), mentionsDriverType(obj.Type(), map[types.Type]bool{}))
	}
	// Declarations alone miss types that exist only as expressions:
	// an inline type assertion, and a generic instantiation like
	// holder[*pgxpool.Pool] whose declaration carries only the type
	// parameter. Expression types close both.
	for expr, tv := range info.Types {
		if expr == nil {
			continue
		}
		collect(expr.Pos(), mentionsDriverType(tv.Type, map[types.Type]bool{}))
	}
	// One diagnostic per line and type, at its first position, so the output
	// does not depend on map iteration order.
	slices.SortFunc(namings, func(a, b naming) int { return cmp.Compare(a.pos, b.pos) })
	reported := map[string]bool{}
	for _, n := range namings {
		at := pass.Fset.Position(n.pos)
		key := fmt.Sprintf("%s@%s:%d", n.named, at.Filename, at.Line)
		if reported[key] {
			continue
		}
		reported[key] = true
		pass.Reportf(n.pos,
			"%s names driver type %s — only the transaction boundary and the harnesses may hold engine handles", base, n.named)
	}
	if generatedImporters[base] {
		return
	}
	for _, file := range pass.Files {
		for _, spec := range file.Imports {
			if imp := strings.Trim(spec.Path.Value, "\"`"); generatedPackages[imp] {
				pass.Reportf(spec.Pos(),
					"%s imports %s — generated queries take chain parameters as plain values; go through the store's binding layer", base, imp)
			}
		}
	}
}

// mentionsDriverType reports the first driver type reachable from t —
// through signatures, interface methods, struct fields, and the usual
// composite types — or "" if none is.
func mentionsDriverType(t types.Type, seen map[types.Type]bool) string {
	if t == nil || seen[t] {
		return ""
	}
	seen[t] = true
	// Unwrap aliases: `type pool = pgxpool.Pool` would otherwise make
	// `interface{ PG() *pool }` invisible to both the String() match and the
	// module-scope guard below.
	if u := types.Unalias(t); u != t {
		return mentionsDriverType(u, seen)
	}
	switch u := t.(type) {
	case *types.Named:
		if key := namedKey(u); driverTypes[key] {
			return key
		}
		// Named driver types are matched by String() above. Descend only into
		// interfaces declared in THIS module: that is where a structural
		// escape would have to be written, and it keeps the walk out of the
		// driver libraries' own type graphs, where nearly every interface
		// transitively exposes a *pgx.Conn and would flag every legitimate
		// holder of a generated Queries value.
		obj := u.Obj()
		if obj == nil || obj.Pkg() == nil || !strings.HasPrefix(obj.Pkg().Path(), Module) {
			return ""
		}
		// A generic instantiation carries its arguments here, and nowhere in
		// the generic's own declaration.
		if args := u.TypeArgs(); args != nil {
			for i := range args.Len() {
				if s := mentionsDriverType(args.At(i), seen); s != "" {
					return s
				}
			}
		}
		if iface, ok := u.Underlying().(*types.Interface); ok {
			return mentionsDriverType(iface, seen)
		}
		return ""
	case *types.Pointer:
		return mentionsDriverType(u.Elem(), seen)
	case *types.Slice:
		return mentionsDriverType(u.Elem(), seen)
	case *types.Array:
		return mentionsDriverType(u.Elem(), seen)
	case *types.Map:
		if s := mentionsDriverType(u.Key(), seen); s != "" {
			return s
		}
		return mentionsDriverType(u.Elem(), seen)
	case *types.Chan:
		return mentionsDriverType(u.Elem(), seen)
	case *types.Signature:
		for _, tup := range []*types.Tuple{u.Params(), u.Results()} {
			for i := range tup.Len() {
				if s := mentionsDriverType(tup.At(i).Type(), seen); s != "" {
					return s
				}
			}
		}
		return ""
	case *types.Interface:
		for i := range u.NumMethods() {
			if s := mentionsDriverType(u.Method(i).Type(), seen); s != "" {
				return s
			}
		}
		return ""
	case *types.Struct:
		for i := range u.NumFields() {
			if s := mentionsDriverType(u.Field(i).Type(), seen); s != "" {
				return s
			}
		}
		return ""
	}
	return ""
}
