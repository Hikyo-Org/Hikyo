package lint

import (
	"go/ast"
	"go/types"
	"maps"
	"slices"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
)

// Analyzer 5 (audit-model ADR CI invariant 8): the redaction guardrail
// around the sensitive types and the audit-content/ops-log boundary.
//
// Three rules:
//  1. Every pinned sensitive type implements the full redaction surface
//     (String, GoString, LogValue, MarshalText, MarshalJSON).
//  2. No formatting/marshaling/logging call takes a sensitive-typed
//     argument outside the type's owning package: the marshal interfaces
//     redact, but a third-party serializer or a deliberate field walk would
//     not, so the expression itself is banned at the boundary where it can
//     be seen.
//  3. Audit events are never mirrored to the ops log: no logging call
//     (log, log/slog) takes an audit-content-typed argument anywhere: the
//     ops log records audit-subsystem health, never event content.
//
// This is a guardrail with named evasion limits, per the ADR: never a proof.
// Reflection and custom serializers are not seen. In-package extraction is not
// seen. Rule 2's walk skips the receiver of a field selection or a method call
// whose result is plain data (readsPlainData): a holder field of an int, or a
// method returning an int or time.Time, is not a leak. A holder method whose
// result is an interface, a func, a chan or a type parameter, or carries a
// string, byte, rune or a number slice/array/map at any depth ([]string,
// []rune, []uint16, [4]uint64, *string, a map with text keys or values, a
// struct with a string field, a math/big value) keeps the holder flagged.
// Not seen: a plain-typed field the holder fills from key material (h.text of
// type string is read as plain data), a scalar numeric result (a uint64 derived
// from key material), and the zone name and instant of a time.Time result. An
// indexed holder (h.items[0].seq) stays flagged, which only over-reports.
// A named type whose underlying struct is identical to a pinned type's is
// treated as that pinned type, so a conversion cannot rename it away.

// SensitiveTypes pins the material-holding types; crypto's compile-time
// redactionSurface pins name the same exported set. The synchronization test
// documents internal-only exceptions; the redactsurface analyzer asserts this
// list actually carries the five methods. Neither discovers unlisted holders.
var SensitiveTypes = map[string]bool{
	Module + "/internal/crypto.LocalKeys":      true,
	Module + "/internal/crypto.Keyring":        true,
	Module + "/internal/crypto.ProjectSealer":  true,
	Module + "/internal/crypto.InstanceSealer": true,
	Module + "/internal/crypto.swapHandle":     true,
	Module + "/internal/crypto.TransitKey":     true,
}

// sensitiveOwner may format its own types (deliberate extraction inside the
// owning package is a stated evasion limit, reviewed there).
var sensitiveOwner = Module + "/internal/crypto"

// auditContentTypes carry audit-event content. Logging one is mirroring the
// trail into a weaker store.
var auditContentTypes = map[string]bool{
	Module + "/internal/audit.Event":      true,
	Module + "/internal/audit.Payload":    true,
	Module + "/internal/audit.Row":        true,
	Module + "/internal/store.AuditEvent": true,
}

// formattingPackages are where rule-2 calls live; loggingPackages the
// stricter rule-3 set (fmt is allowed for audit content: wrapping an error
// with an event id is not mirroring the trail; log emission is).
var formattingPackages = map[string]bool{
	"fmt": true, "encoding/json": true, "encoding/json/v2": true,
	"log": true, "log/slog": true,
	"encoding/xml": true, "encoding/gob": true, "encoding/csv": true,
	"text/template": true, "html/template": true,
}

var loggingPackages = map[string]bool{
	"log": true, "log/slog": true,
}

var redactionSurfaceMethods = []string{
	"String", "GoString", "LogValue", "MarshalText", "MarshalJSON",
}

// RedactSurfaces is the redactsurface analyzer over the crypto package.
var RedactSurfaces = &analysis.Analyzer{
	Name: "redactsurface",
	Doc:  "every pinned sensitive type implements the full redaction surface",
	Run: func(pass *analysis.Pass) (any, error) {
		if pass.Pkg.Path() != sensitiveOwner {
			return nil, nil
		}
		reportTarget(pass)
		scope := pass.Pkg.Scope()
		for _, name := range slices.Sorted(maps.Keys(SensitiveTypes)) {
			short := strings.TrimPrefix(name, sensitiveOwner+".")
			obj := scope.Lookup(short)
			if obj == nil {
				pass.Reportf(packagePos(pass), "pinned sensitive type %s not found in %s", name, sensitiveOwner)
				continue
			}
			ms := types.NewMethodSet(types.NewPointer(obj.Type()))
			for _, m := range redactionSurfaceMethods {
				if ms.Lookup(nil, m) == nil {
					pass.Reportf(obj.Pos(),
						"sensitive type %s lacks %s(): the full formatting surface must redact (audit-model ADR CI invariant 8)",
						name, m)
				}
			}
		}
		return nil, nil
	},
}

// RedactFormat is the redactfmt analyzer.
var RedactFormat = newRedactFormat(subexpressionTypes)

// newRedactFormat builds the analyzer around the function that collects an
// argument's sub-expression types, so the differential test can run the
// pre-narrowing collector (TestRedactFormatNarrowingDifferential).
func newRedactFormat(collect func(*types.Info, ast.Expr) []types.Type) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "redactfmt",
		Doc:  "sensitive types are not formatted outside crypto; audit content is not logged",
		Run:  func(pass *analysis.Pass) (any, error) { return runRedactFormat(pass, collect) },
	}
}

func runRedactFormat(pass *analysis.Pass, collect func(*types.Info, ast.Expr) []types.Type) (any, error) {
	if !inModule(pass.Pkg.Path()) {
		return nil, nil
	}
	exemptSensitive := pass.Pkg.Path() == sensitiveOwner
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			calleePkg := calleePackage(pass.TypesInfo, call)
			if calleePkg == "" {
				return true
			}
			fmtCall := formattingPackages[calleePkg]
			logCall := loggingPackages[calleePkg]
			if !fmtCall && !logCall {
				return true
			}
			for _, arg := range call.Args {
				// Static-type erasure is the obvious bypass: `any(ev)` and
				// `ev.Payload["formula"]` both type as `any` at the argument
				// position while still carrying the value. Every
				// sub-expression's type is therefore checked, not just the
				// argument's own.
				argTypes := collect(pass.TypesInfo, arg)
				if len(argTypes) == 0 {
					continue
				}
				if !exemptSensitive {
					if name := mentionsAny(argTypes, SensitiveTypes); name != "" {
						pass.Reportf(arg.Pos(),
							"%s passes sensitive type %s to %s: formatting/marshaling sensitive types outside their owning package is banned",
							pass.Pkg.Path(), name, calleePkg)
					}
				}
				if logCall {
					if name := mentionsAny(argTypes, auditContentTypes); name != "" {
						pass.Reportf(arg.Pos(),
							"%s logs audit content %s: audit events are never mirrored to the ops log (audit-model ADR)",
							pass.Pkg.Path(), name)
					}
				}
			}
			return true
		})
	}
	return nil, nil
}

// calleePackage resolves the package path of a call's callee: a package
// function (fmt.Sprintf) or a method whose defining package matches
// (slog.Logger.Info).
func calleePackage(info *types.Info, call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		if obj, ok := info.Uses[fn.Sel]; ok {
			if f, ok := obj.(*types.Func); ok && f.Pkg() != nil {
				return f.Pkg().Path()
			}
		}
	case *ast.Ident:
		if obj, ok := info.Uses[fn]; ok {
			if f, ok := obj.(*types.Func); ok && f.Pkg() != nil {
				return f.Pkg().Path()
			}
		}
	}
	return ""
}

// mentionsNamed reports the first pinned named type reachable from t
// (directly, behind pointers/slices/maps, or as a struct field of a module
// type), or "".
func mentionsNamed(t types.Type, pinned map[string]bool, seen map[types.Type]bool) string {
	if t == nil || seen[t] {
		return ""
	}
	seen[t] = true
	if u := types.Unalias(t); u != t {
		return mentionsNamed(u, pinned, seen)
	}
	switch u := t.(type) {
	case *types.Named:
		if key := pinnedKey(u, pinned); key != "" {
			return key
		}
		obj := u.Obj()
		if obj == nil || obj.Pkg() == nil || !strings.HasPrefix(obj.Pkg().Path(), Module) {
			return ""
		}
		return mentionsNamed(u.Underlying(), pinned, seen)
	case *types.Pointer:
		return mentionsNamed(u.Elem(), pinned, seen)
	case *types.Slice:
		return mentionsNamed(u.Elem(), pinned, seen)
	case *types.Array:
		return mentionsNamed(u.Elem(), pinned, seen)
	case *types.Map:
		if s := mentionsNamed(u.Key(), pinned, seen); s != "" {
			return s
		}
		return mentionsNamed(u.Elem(), pinned, seen)
	case *types.Struct:
		for i := range u.NumFields() {
			if s := mentionsNamed(u.Field(i).Type(), pinned, seen); s != "" {
				return s
			}
		}
		return ""
	}
	return ""
}

// subexpressionTypes collects the type of an argument expression and of
// every sub-expression inside it. Without the sub-expressions, wrapping a
// pinned value in a conversion to `any` (or indexing a pinned map) hides it
// from the check while logging exactly the same content.
//
// One sub-expression is not collected: the receiver of a selection that reads
// only plain data from a holder (readsPlainData), as in h.seq or h.now(). The
// selected field or method result is still collected, so a harness struct that
// merely holds a Keyring is not a leak when an int is formatted. Everything
// else is still walked: a receiver that is itself pinned (ev.Actor.ID reads
// audit content), a method or field that can carry any value (an `any`, a
// func), a selection promoted from an embedded pinned type, and a receiver's
// own call arguments and composite literal elements.
func subexpressionTypes(info *types.Info, arg ast.Expr) []types.Type {
	var out []types.Type
	holders := map[ast.Expr]bool{}
	ast.Inspect(arg, func(n ast.Node) bool {
		expr, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		if sel, ok := expr.(*ast.SelectorExpr); ok && readsPlainData(info.Selections[sel]) {
			holders[sel.X] = true
		}
		if holders[expr] {
			// A transparent wrapper passes the holder through unchanged.
			switch e := expr.(type) {
			case *ast.ParenExpr:
				holders[e.X] = true
			case *ast.StarExpr:
				holders[e.X] = true
			}
		}
		if tv, ok := info.Types[expr]; ok && tv.Type != nil {
			if holders[expr] && pinnedDirectly(tv.Type) == "" {
				return true
			}
			out = append(out, tv.Type)
		}
		return true
	})
	return out
}

// readsPlainData reports whether a selection reads only plain data: a field,
// or a method result, whose type cannot carry an arbitrary value, reached
// without passing through an embedded pinned type.
func readsPlainData(sel *types.Selection) bool {
	if sel == nil {
		return false
	}
	var result []types.Type
	switch obj := sel.Obj().(type) {
	case *types.Var:
		result = []types.Type{obj.Type()}
	case *types.Func:
		sig, ok := obj.Type().(*types.Signature)
		if !ok {
			return false
		}
		if recv := sig.Recv(); recv != nil && pinnedDirectly(recv.Type()) != "" {
			return false
		}
		for v := range sig.Results().Variables() {
			// A text or byte result, however nested, can carry key material a
			// holder method built from what it holds (h.reveal()), which the
			// type cannot show.
			if carriesText(v.Type(), map[types.Type]bool{}) {
				return false
			}
			result = append(result, v.Type())
		}
	default:
		return false
	}
	cur := sel.Recv()
	for _, i := range sel.Index()[:len(sel.Index())-1] {
		st, ok := derefStruct(cur)
		if !ok {
			return false
		}
		cur = st.Field(i).Type()
		if pinnedDirectly(cur) != "" {
			return false
		}
	}
	return !slices.ContainsFunc(result, func(t types.Type) bool { return !plainData(t, map[types.Type]bool{}) })
}

// carriesText reports whether a value of type t can carry text or bytes that a
// holder method built from what it holds: a string or byte reachable through
// pointers, slices, arrays, maps and struct fields, however deep, runes and
// numeric slices, arrays and maps (a word or float encoding of the text)
// included. time.Time is the one deliberate exception, kept so a plain
// holder.now() stays unflagged; it is a named limit: its zone name
// (time.FixedZone(h.keyring.String(), 0)) and instant are not inspected.
// A scalar number is plain data: a uint64 derived from key material is not seen.
func carriesText(t types.Type, seen map[types.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	t = types.Unalias(t)
	if n, ok := t.(*types.Named); ok {
		if namedKey(n) == "time.Time" {
			return false
		}
		if obj := n.Obj(); obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "math/big" {
			return true
		}
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		// Int32 is rune: []rune(secret) is text. This over-reports plain int32.
		return u.Info()&types.IsString != 0 || u.Kind() == types.Uint8 || u.Kind() == types.Int32
	case *types.Pointer:
		return carriesText(u.Elem(), seen)
	case *types.Slice:
		return numeric(u.Elem()) || carriesText(u.Elem(), seen)
	case *types.Array:
		return numeric(u.Elem()) || carriesText(u.Elem(), seen)
	case *types.Map:
		return numeric(u.Elem()) || carriesText(u.Key(), seen) || carriesText(u.Elem(), seen)
	case *types.Struct:
		for f := range u.Fields() {
			if carriesText(f.Type(), seen) {
				return true
			}
		}
	}
	return false
}

// numeric reports an integer, float or complex element type: a slice of them
// can encode key material as words or floats.
func numeric(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsNumeric != 0
}

func derefStruct(t types.Type) (*types.Struct, bool) {
	for {
		t = types.Unalias(t)
		p, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = p.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	return st, ok
}

// plainData reports whether a value of type t cannot carry an arbitrary value:
// no interface, func, channel or type parameter is reachable from it. A pinned
// type is never plain.
func plainData(t types.Type, seen map[types.Type]bool) bool {
	if seen[t] {
		return true
	}
	seen[t] = true
	switch u := types.Unalias(t).(type) {
	case *types.Basic:
		return u.Kind() != types.UnsafePointer
	case *types.Named:
		return pinnedDirectly(u) == "" && plainData(u.Underlying(), seen)
	case *types.Pointer:
		return plainData(u.Elem(), seen)
	case *types.Slice:
		return plainData(u.Elem(), seen)
	case *types.Array:
		return plainData(u.Elem(), seen)
	case *types.Map:
		return plainData(u.Key(), seen) && plainData(u.Elem(), seen)
	case *types.Struct:
		for f := range u.Fields() {
			if !plainData(f.Type(), seen) {
				return false
			}
		}
		return true
	}
	return false
}

// pinnedDirectly reports the pinned type t is (behind pointers), or "": a
// pinned receiver is read whole, unlike a struct that merely holds one.
func pinnedDirectly(t types.Type) string {
	for {
		t = types.Unalias(t)
		p, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		if key := pinnedKey(n, SensitiveTypes); key != "" {
			return key
		}
		return pinnedKey(n, auditContentTypes)
	}
	return ""
}

// pinnedKey is the key of the pinned type n is, or "". A named struct whose
// underlying type is identical to a pinned type's counts as that type, so
// `type evAlias audit.Event` and a conversion cannot rename it away.
func pinnedKey(n *types.Named, pinned map[string]bool) string {
	if key := namedKey(n); pinned[key] {
		return key
	}
	st, ok := n.Underlying().(*types.Struct)
	if !ok || st.NumFields() == 0 || n.Obj() == nil || n.Obj().Pkg() == nil {
		return ""
	}
	for key, p := range pinnedTypesFrom(n.Obj().Pkg()) {
		if pinned[key] && types.Identical(st, p.Underlying()) {
			return key
		}
	}
	return ""
}

// pinnedTypesCache maps a package to the pinned types reachable through its
// imports (itself included), keyed like SensitiveTypes.
var pinnedTypesCache sync.Map

func pinnedTypesFrom(pkg *types.Package) map[string]*types.Named {
	if v, ok := pinnedTypesCache.Load(pkg); ok {
		return v.(map[string]*types.Named)
	}
	out := map[string]*types.Named{}
	visited := map[*types.Package]bool{}
	var walk func(*types.Package)
	walk = func(p *types.Package) {
		if visited[p] {
			return
		}
		visited[p] = true
		for _, set := range []map[string]bool{SensitiveTypes, auditContentTypes} {
			for key := range set {
				dot := strings.LastIndex(key, ".")
				path, name := key[:dot], key[dot+1:]
				if path != p.Path() {
					continue
				}
				if tn, ok := p.Scope().Lookup(name).(*types.TypeName); ok {
					if named, ok := tn.Type().(*types.Named); ok {
						out[key] = named
					}
				}
			}
		}
		for _, imp := range p.Imports() {
			walk(imp)
		}
	}
	walk(pkg)
	pinnedTypesCache.Store(pkg, out)
	return out
}

// mentionsAny reports the first pinned type reachable from any of ts.
func mentionsAny(ts []types.Type, pinned map[string]bool) string {
	for _, t := range ts {
		if name := mentionsNamed(t, pinned, map[types.Type]bool{}); name != "" {
			return name
		}
	}
	return ""
}
