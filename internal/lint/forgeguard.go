package lint

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

// CheckProofForgery is analyzer 3 (tenant-isolation ADR, invariant 9). The
// concrete proof type is unexported, so it cannot be named outside
// internal/authz at all — what remains expressible, and is flagged here:
//
//   - a nil literal in a Proof-typed position (call argument, variable
//     initialisation, assignment, return, struct literal): the only
//     forgeable Proof value. The store boundary already rejects nil at
//     runtime; the guard removes the expression at build time.
//
//   - reflect or unsafe imported by a package that handles Proof values:
//     conservative by design — the pair is how a forged proof would be
//     assembled, and no package outside authz has business combining them.
//
//   - the same forgery spelled differently: a package-level authz.Proof
//     variable with no initialiser (always nil), an authz.Proof(nil)
//     conversion, and new(authz.Proof). A local declare-then-assign variable
//     is not flagged: production code uses that pattern and assigns the proof
//     before use.
//
// The authorization package itself (and its tests) are exempt: boundary
// tests must construct exactly these invalid values.
//
// A test whose subject is the nil proof (the store boundary refusing it), or
// that can reach no real proof, carries an inline `forgeguard:nil-proof
// <reason>` marker on the flagged line. The marker is honoured only in
// _test.go files and needs a reason; one in a production file is itself a
// finding.
func CheckProofForgery(pkgs []*packages.Package) []string {
	return RunOver(Check{Analyzer: ForgeGuard}, pkgs).Findings
}

// ForgeGuard is CheckProofForgery as an analyzer. "Handles Proof values" is
// decided from the files of the variant being analyzed, in-package tests
// included.
var ForgeGuard = &analysis.Analyzer{
	Name: "forgeguard",
	Doc:  "no nil authz.Proof, and no reflect/unsafe import beside Proof values",
	Run: func(pass *analysis.Pass) (any, error) {
		path := pass.Pkg.Path()
		// The authorization package and its test variants are exempt:
		// boundary tests construct exactly these invalid values on purpose.
		// Exact paths only — a prefix match would silently exempt a
		// neighbouring `internal/authzforge` that reflects on proof values.
		if !inModule(path) || authzExempt[path] {
			return nil, nil
		}
		usesProof := false
		for _, obj := range pass.TypesInfo.Uses {
			if isAuthzProof(obj.Type()) {
				usesProof = true
				break
			}
		}
		if usesProof {
			for _, file := range pass.Files {
				for _, spec := range file.Imports {
					if imp := strings.Trim(spec.Path.Value, "\"`"); imp == "reflect" || imp == "unsafe" {
						pass.Reportf(spec.Pos(), "%s imports %q while handling authz.Proof values", path, imp)
					}
				}
			}
		}
		nilProofLiterals(pass)
		return nil, nil
	},
}

// authzExempt is the exact set of package paths allowed to construct
// non-canonical proof values: the authorization package (in-package tests
// share its path), the test binary go/packages reports beside it, and the
// external-test package path should one ever be added.
var authzExempt = map[string]bool{
	Module + "/internal/authz":      true,
	Module + "/internal/authz.test": true,
	Module + "/internal/authz_test": true,
}

func isAuthzProof(t types.Type) bool {
	n, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := n.Obj()
	return obj.Name() == "Proof" && obj.Pkg() != nil && obj.Pkg().Path() == Module+"/internal/authz"
}

// nilProofLiterals walks the syntax for nil expressions in Proof-typed
// positions. go/types keeps untyped nil untyped, so each syntactic position
// is resolved explicitly: call arguments, variable declarations,
// assignments, returns, and keyed struct literals. Stated limit: a nil
// smuggled through a positional struct literal or an intermediate untyped
// construct escapes this walk — and still dies at the store boundary, which
// is the enforcement; this guard just surfaces it earlier.
func nilProofLiterals(p *analysis.Pass) {
	// markerLines are the lines of valid markers; a marker in a production file,
	// or without a reason, is a finding of its own.
	markerLines := map[string]map[int]bool{}
	for _, file := range p.Files {
		for _, group := range file.Comments {
			for _, c := range group.List {
				reason, ok := nilProofReason(c.Text)
				position := p.Fset.Position(c.Pos())
				switch {
				case !ok:
				case !isTestFile(position.Filename):
					p.Reportf(c.Pos(), "%s marker is honoured only in _test.go files", nilProofMarker)
				case reason == "":
					p.Reportf(c.Pos(), "%s marker needs a reason", nilProofMarker)
				default:
					if markerLines[position.Filename] == nil {
						markerLines[position.Filename] = map[int]bool{}
					}
					markerLines[position.Filename][position.Line] = true
				}
			}
		}
	}
	marked := func(pos token.Pos) bool {
		position := p.Fset.Position(pos)
		return markerLines[position.Filename][position.Line]
	}
	flag := func(n ast.Node) {
		if marked(n.Pos()) {
			return
		}
		p.Reportf(n.Pos(), "nil in an authz.Proof position — the one forgeable value; get a proof from authorize()")
	}
	isNil := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		return ok && id.Name == "nil" && p.TypesInfo.Uses[id] == types.Universe.Lookup("nil")
	}
	for _, file := range p.Files {
		// A package-level authz.Proof variable with no initialiser is nil for
		// the life of the process.
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Values) != 0 {
					continue
				}
				for _, name := range vs.Names {
					if obj := p.TypesInfo.Defs[name]; obj != nil && isAuthzProof(obj.Type()) {
						flag(name)
					}
				}
			}
		}
		// Enclosing-function result types, resolved by narrowest span, so a
		// `return nil` can be matched against the function's Proof results.
		type fnSpan struct {
			pos, end token.Pos
			results  []types.Type
		}
		var fns []fnSpan
		ast.Inspect(file, func(n ast.Node) bool {
			var sig *types.Signature
			switch node := n.(type) {
			case *ast.FuncDecl:
				if obj := p.TypesInfo.Defs[node.Name]; obj != nil {
					sig, _ = obj.Type().(*types.Signature)
				}
			case *ast.FuncLit:
				if tv, ok := p.TypesInfo.Types[node]; ok {
					sig, _ = tv.Type.(*types.Signature)
				}
			default:
				return true
			}
			if sig != nil {
				var results []types.Type
				for i := range sig.Results().Len() {
					results = append(results, sig.Results().At(i).Type())
				}
				fns = append(fns, fnSpan{pos: n.Pos(), end: n.End(), results: results})
			}
			return true
		})
		enclosingResults := func(pos token.Pos) []types.Type {
			var best *fnSpan
			for i := range fns {
				f := &fns[i]
				if f.pos <= pos && pos < f.end && (best == nil || f.pos > best.pos) {
					best = f
				}
			}
			if best == nil {
				return nil
			}
			return best.results
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.ReturnStmt:
				results := enclosingResults(node.Pos())
				for i, e := range node.Results {
					if i < len(results) && isNil(e) && isAuthzProof(results[i]) {
						flag(e)
					}
				}
			case *ast.CallExpr:
				fun := p.TypesInfo.Types[node.Fun]
				if fun.IsType() && isAuthzProof(fun.Type) && len(node.Args) == 1 && isNil(node.Args[0]) {
					flag(node)
					break
				}
				if id, ok := node.Fun.(*ast.Ident); ok && len(node.Args) == 1 && p.TypesInfo.Uses[id] == types.Universe.Lookup("new") {
					if tv, ok := p.TypesInfo.Types[node.Args[0]]; ok && tv.IsType() && isAuthzProof(tv.Type) {
						flag(node)
						break
					}
				}
				sig, _ := p.TypesInfo.Types[node.Fun].Type.(*types.Signature)
				if sig == nil {
					break
				}
				for i, arg := range node.Args {
					if !isNil(arg) {
						continue
					}
					var pt types.Type
					if sig.Variadic() && i >= sig.Params().Len()-1 {
						continue
					}
					if i < sig.Params().Len() {
						pt = sig.Params().At(i).Type()
					}
					if pt != nil && isAuthzProof(pt) {
						flag(arg)
					}
				}
			case *ast.ValueSpec:
				for _, v := range node.Values {
					if isNil(v) {
						for _, name := range node.Names {
							if obj := p.TypesInfo.Defs[name]; obj != nil && isAuthzProof(obj.Type()) {
								flag(v)
							}
						}
					}
				}
			case *ast.AssignStmt:
				for i, rhs := range node.Rhs {
					if isNil(rhs) && i < len(node.Lhs) {
						if tv, ok := p.TypesInfo.Types[node.Lhs[i]]; ok && isAuthzProof(tv.Type) {
							flag(rhs)
						}
					}
				}
			case *ast.KeyValueExpr:
				if isNil(node.Value) {
					if id, ok := node.Key.(*ast.Ident); ok {
						if obj := p.TypesInfo.Uses[id]; obj != nil && isAuthzProof(obj.Type()) {
							flag(node.Value)
						}
					}
				}
			}
			return true
		})
	}
}

// nilProofMarker is the inline marker that exempts one nil proof in a test.
const nilProofMarker = "forgeguard:nil-proof"

// nilProofReason extracts the reason after the marker when a comment starts
// with it; prose that merely mentions the marker is not one.
func nilProofReason(comment string) (reason string, ok bool) {
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(comment, "//"), "/*"), "*/"))
	rest, ok := strings.CutPrefix(body, nilProofMarker)
	return strings.TrimSpace(rest), ok
}

func isTestFile(name string) bool { return strings.HasSuffix(name, "_test.go") }
