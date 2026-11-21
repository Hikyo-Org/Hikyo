package lint

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

// The transport may not name service.LocalPrincipal.
//
// That constructor bypasses session resolution by construction — it is for
// callers already below the network boundary (the isolation harness, and
// local-authority verbs running on the server's own host). A transport that
// could build one could authorize as anybody, and the whole point of handing
// the raw artifact down is that the decision about who is asking happens
// inside the transaction that acts on the answer.
//
// This is the check that keeps the Actor split a rule rather than a habit.
const transportPath = Module + "/internal/server"

// ActorSplit keeps the transport from naming service.LocalPrincipal. The
// server package's in-package tests are judged too.
var ActorSplit = &analysis.Analyzer{
	Name: "actorsplit",
	Doc:  "internal/server never names service.LocalPrincipal",
	Run: func(pass *analysis.Pass) (any, error) {
		if pass.Pkg.Path() != transportPath {
			return nil, nil
		}
		reportTarget(pass)
		const forbidden = "LocalPrincipal"
		for _, file := range pass.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != forbidden {
					return true
				}
				obj, ok := pass.TypesInfo.Uses[sel.Sel]
				if !ok || obj.Pkg() == nil {
					return true
				}
				if obj.Pkg().Path() == Module+"/internal/service" {
					pass.Reportf(sel.Pos(), "%s names service.%s — the transport must hand over the raw artifact and let the service resolve it in-transaction",
						pass.Pkg.Path(), forbidden)
				}
				return true
			})
		}
		return nil, nil
	},
}
