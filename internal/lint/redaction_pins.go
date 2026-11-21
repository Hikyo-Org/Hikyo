package lint

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// RedactPins reports crypto's compile-time redactionSurface pins as summary
// diagnostics, read from the actual typed assertions (pointer and value
// expressions) rather than a third hand-maintained set. The driver test
// compares them with SensitiveTypes.
var RedactPins = &analysis.Analyzer{
	Name: "redactpins",
	Doc:  "reports crypto's redactionSurface compile-time pins",
	Run: func(pass *analysis.Pass) (any, error) {
		if pass.Pkg.Path() != sensitiveOwner {
			return nil, nil
		}
		reportTarget(pass)
		for _, file := range pass.Files {
			// Only production package-level blank assertions are pins: a test
			// file's or a local variable's would not hold the build.
			if isTestFile(pass.Fset.PositionFor(file.Pos(), false).Filename) {
				continue
			}
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.VAR {
					continue
				}
				for _, spec := range gen.Specs {
					v, ok := spec.(*ast.ValueSpec)
					if !ok || v.Type == nil || len(v.Names) != 1 || v.Names[0].Name != "_" {
						continue
					}
					surface, ok := pass.TypesInfo.TypeOf(v.Type).(*types.Named)
					if !ok || surface.Obj().Name() != "redactionSurface" || surface.Obj().Pkg().Path() != sensitiveOwner {
						continue
					}
					for _, value := range v.Values {
						typ := types.Unalias(pass.TypesInfo.TypeOf(value))
						if pointer, ok := typ.(*types.Pointer); ok {
							typ = types.Unalias(pointer.Elem())
						}
						named, ok := typ.(*types.Named)
						if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != sensitiveOwner {
							pass.Reportf(value.Pos(), "unexpected redaction pin")
							continue
						}
						reportSummary(pass, summaryPin, namedKey(named))
					}
				}
			}
		}
		return nil, nil
	},
}
