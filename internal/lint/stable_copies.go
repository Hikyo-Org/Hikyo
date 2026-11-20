package lint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// stableCopies are the programs that cannot import internal/jsonwire (a
// sparse checkout, a lone file built outside the module) and keep their own
// copy of jsonwire.Stable under the variable name stableJSON.
var stableCopies = []string{
	"scripts/ci/pr-gate/main.go",
	"scripts/ci/analysis-shards-go/main.go",
}

// CheckStableCopies reports each copy of jsonwire.Stable whose JoinOptions
// argument list differs from the original's, so a copy cannot drift out of
// byte-compatibility with encoding/json (v1) unnoticed.
func CheckStableCopies(repoRoot string) []string {
	want, err := joinOptionsArgs(filepath.Join(repoRoot, "internal/jsonwire/jsonwire.go"), "Stable")
	if err != nil {
		return []string{"stablecopy: " + err.Error()}
	}
	var findings []string
	for _, rel := range stableCopies {
		got, err := joinOptionsArgs(filepath.Join(repoRoot, filepath.FromSlash(rel)), "stableJSON")
		switch {
		case err != nil:
			findings = append(findings, "stablecopy: "+err.Error())
		case !slices.Equal(got, want):
			findings = append(findings, fmt.Sprintf(
				"stablecopy: %s stableJSON options [%s] differ from jsonwire.Stable [%s]",
				rel, strings.Join(got, ", "), strings.Join(want, ", ")))
		}
	}
	return findings
}

// joinOptionsArgs returns the source text of each argument of the
// json.JoinOptions call that initializes the package variable name in path.
func joinOptionsArgs(path, name string) ([]string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, err
	}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != name || len(vs.Values) != 1 {
				continue
			}
			call, ok := vs.Values[0].(*ast.CallExpr)
			if !ok {
				return nil, fmt.Errorf("%s: %s is not a JoinOptions call", path, name)
			}
			var args []string
			for _, arg := range call.Args {
				args = append(args, string(src[fset.Position(arg.Pos()).Offset:fset.Position(arg.End()).Offset]))
			}
			return args, nil
		}
	}
	return nil, fmt.Errorf("%s: no var %s", path, name)
}
