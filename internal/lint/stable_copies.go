package lint

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// stableCopies are the programs that cannot import internal/jsonwire (a
// sparse checkout, a lone file built outside the module) and keep their own
// copy of jsonwire.Stable under the variable name stableJSON.
var stableCopies = []string{
	"scripts/ci/pr-gate/main.go",
	"scripts/ci/analysis-shards-go/main.go",
}

const stableOriginal = "internal/jsonwire/jsonwire.go"

func stableCopiesSpecs(string) ([]InputSpec, error) {
	specs := []InputSpec{{File: stableOriginal}}
	for _, rel := range stableCopies {
		specs = append(specs, InputSpec{File: rel})
	}
	return specs, nil
}

// StableCopies reads the declared jsonwire original and its copies, and runs
// once on internal/jsonwire. The copies are programs outside the anchor's action
// ID. The original is the anchor's own file, declared anyway: the fixture keeps
// its original under its root, not in its anchor package, so it is read by path.
// The cost: an edit re-vets every
// package of the context (the flag is in every action ID); undeclared, it would
// re-vet only jsonwire and the packages whose export data changed.
var StableCopies = newStableCopies()

func newStableCopies() *analysis.Analyzer {
	copies := slices.Clone(stableCopies)
	return fileCheck{
		name: "stablecopies", doc: "reports a copy of jsonwire.Stable whose options drift from the original",
		anchor: Module + "/internal/jsonwire",
		flags: func(a *analysis.Analyzer) {
			a.Flags.Func("copies", "comma-separated copies to compare (default: the registered ones)", func(v string) error {
				copies = strings.Split(v, ",")
				return nil
			})
		},
		run: func(src source, root string) ([]string, error) { return checkStableCopies(src, root, copies) },
	}.analyzer()
}

func checkStableCopies(src source, repoRoot string, copies []string) ([]string, error) {
	want, err := joinOptionsArgs(src, filepath.Join(repoRoot, stableOriginal), "Stable")
	if errors.Is(err, ErrUndeclaredInput) {
		return nil, err
	}
	if err != nil {
		return []string{"stablecopy: " + err.Error()}, nil
	}
	var findings []string
	for _, rel := range copies {
		got, err := joinOptionsArgs(src, filepath.Join(repoRoot, filepath.FromSlash(rel)), "stableJSON")
		switch {
		case errors.Is(err, ErrUndeclaredInput):
			return nil, err
		case err != nil:
			findings = append(findings, "stablecopy: "+err.Error())
		case !slices.Equal(got, want):
			findings = append(findings, fmt.Sprintf(
				"stablecopy: %s stableJSON options [%s] differ from jsonwire.Stable [%s]",
				rel, strings.Join(got, ", "), strings.Join(want, ", ")))
		}
	}
	return findings, nil
}

// joinOptionsArgs returns the source text of each argument of the
// json.JoinOptions call that initializes the package variable name in path.
func joinOptionsArgs(src source, path, name string) ([]string, error) {
	text, err := src.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, text, 0)
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
				args = append(args, string(text[fset.Position(arg.Pos()).Offset:fset.Position(arg.End()).Offset]))
			}
			return args, nil
		}
	}
	return nil, fmt.Errorf("%s: no var %s", path, name)
}
