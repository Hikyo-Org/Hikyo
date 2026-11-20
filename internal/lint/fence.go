package lint

import (
	"go/ast"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

// CheckFenceCompleteness enforces the writer fence's coverage (encryption-model
// ADR § Rotation, invariant 7): every ciphertext a service seals for storage
// must be guarded, in its write transaction, by the writer fence — otherwise a
// row sealed under a DEK version a concurrent rotate-dek is retiring can strand
// under a key `reencrypt` has already walked past.
//
// The rule, per top-level function in internal/service: a function that calls
// ProjectSealer.SealValue/SealField or InstanceSealer.SealField must also call
// one of the fence helpers (fenceProject / fenceInstance / fenceInstanceVersion)
// or the authentication-surface fence (TxAuthorizer.AssertActiveInstanceDEKVersion).
// The granularity is the function, not the individual write, because the
// FOR SHARE lock the fence takes is held to the transaction's commit and so
// covers every sealed write committed in the same function.
//
// Two escape hatches, both explicit and reviewable — never silent:
//
//   - a `fence:delegated` marker in the function's doc comment: the function
//     seals but returns the ciphertext and its DEK version to a caller that
//     fences on that version before writing (the sealVerifier / sealSecret /
//     generateSPKey helpers).
//   - a `fence:exempt` marker: the sealed bytes are never written to any table
//     (the recovery timing-equalisation dummy), so no row can be stranded.
//   - a `fence:fixture <reason>` marker, honoured only in _test.go files: a test
//     helper that seeds an isolated fixture database no rotation can race. In a
//     production file the marker is itself a finding.
//
// reencrypt.go is exempt wholesale: it IS the mover, re-sealing rows under the
// new active version; fencing it against the version it is installing would be
// circular.
//
// Stated limits, in the spirit of the other analyzers — each is a build-time
// surfacing, not the enforcement, which is the runtime fence itself:
//   - A function that seals on one path and fences on an unrelated path would
//     pass. The service methods are one operation each, so this does not arise.
//   - The analyzer proves a seal and a fence CO-OCCUR in a function; it does not
//     prove the fence runs after the seal and inside the SAME transaction as the
//     store write. A function that sealed, fenced-and-committed in transaction A,
//     then wrote in a separate transaction B would pass while leaving the write
//     unfenced. Proving transaction co-location needs dataflow this type-only
//     pass does not attempt; all current sealing sites were verified in-tx by
//     hand (each fences immediately before its write, in the write's own tx).
//   - `fence:delegated` / `fence:exempt` / `fence:fixture` markers are trusted, not verified: a
//     reviewer owns that a delegated helper's version truly reaches a fencing
//     caller, and that an exempt seal truly never reaches a table.
func CheckFenceCompleteness(pkgs []*packages.Package, servicePath string) []string {
	return RunOver(Check{Analyzer: newFence(servicePath), Targets: []string{servicePath}}, pkgs).Findings
}

// Fence is CheckFenceCompleteness as an analyzer over the service package.
var Fence = newFence(Module + "/internal/service")

func newFence(service string) *analysis.Analyzer {
	a := &analysis.Analyzer{
		Name: "fence",
		Doc:  "functions that seal ciphertext also call the writer fence",
	}
	a.Flags.StringVar(&service, "service", service, "import path of the service package")
	a.Run = func(pass *analysis.Pass) (any, error) {
		runFence(pass, service)
		return nil, nil
	}
	return a
}

func runFence(pass *analysis.Pass, servicePath string) {
	if pass.Pkg.Path() != servicePath {
		return
	}
	reportTarget(pass)
	for _, file := range pass.Files {
		if strings.HasSuffix(fileName(pass, file.Pos()), "reencrypt.go") {
			continue // the mover re-seals under the new active version by design
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if marked(fn, "fence:delegated") || marked(fn, "fence:exempt") {
				continue
			}
			if marked(fn, fenceFixtureMarker) {
				switch {
				case !isTestFile(fileName(pass, file.Pos())):
					pass.Reportf(fn.Pos(), "%s carries %s, which is honoured only in _test.go files", fn.Name.Name, fenceFixtureMarker)
				case !fixtureReason.MatchString(fn.Doc.Text()):
					pass.Reportf(fn.Pos(), "%s carries %s without a reason", fn.Name.Name, fenceFixtureMarker)
				default:
					continue
				}
			}
			seals, fences := false, false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch {
				case sealCall(pass.TypesInfo, call):
					seals = true
				case fenceCall(pass.TypesInfo, call, servicePath):
					fences = true
				}
				return true
			})
			if seals && !fences {
				pass.Reportf(fn.Pos(),
					"%s seals ciphertext but never calls the writer fence (invariant 7) — "+
						"add fenceProject/fenceInstance(Version) or az.AssertActiveInstanceDEKVersion before the write, "+
						"or mark the function fence:delegated / fence:exempt with a reason",
					fn.Name.Name)
			}
		}
	}
}

// fenceFixtureMarker exempts a test helper that seeds a fixture database.
const fenceFixtureMarker = "fence:fixture"

var fixtureReason = regexp.MustCompile(`fence:fixture\s+\S`)

// marked reports whether a function's doc comment carries a marker token.
func marked(fn *ast.FuncDecl, token string) bool {
	return fn.Doc != nil && strings.Contains(fn.Doc.Text(), token)
}

// sealCall reports a call to ProjectSealer.SealValue/SealField/SealPersonalValue or
// InstanceSealer.SealField, resolved through the type-checked selection so a
// same-named method on any other type does not match.
func sealCall(info *types.Info, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	selection := info.Selections[sel]
	if selection == nil {
		return false
	}
	name := selection.Obj().Name()
	if name != "SealValue" && name != "SealField" && name != "SealPersonalValue" {
		return false
	}
	return namedTypeIs(selection.Recv(), Module+"/internal/crypto", "ProjectSealer", "InstanceSealer")
}

// fenceCall reports a call to one of the fence helpers or the authn-surface
// fence. The helpers are package functions in internal/service; the authn
// fence is a method named AssertActiveInstanceDEKVersion.
func fenceCall(info *types.Info, call *ast.CallExpr, servicePath string) bool {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if obj, ok := info.Uses[fun].(*types.Func); ok {
			switch obj.Name() {
			case "fenceProject", "fenceInstance", "fenceInstanceVersion":
				return obj.Pkg() != nil && obj.Pkg().Path() == servicePath
			}
		}
	case *ast.SelectorExpr:
		if selection := info.Selections[fun]; selection != nil {
			return selection.Obj().Name() == "AssertActiveInstanceDEKVersion"
		}
	}
	return false
}

// namedTypeIs reports whether t (possibly a pointer) is a named type from pkgPath
// whose name is one of names.
func namedTypeIs(t types.Type, pkgPath string, names ...string) bool {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	n, ok := t.(*types.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != pkgPath {
		return false
	}
	for _, name := range names {
		if n.Obj().Name() == name {
			return true
		}
	}
	return false
}
