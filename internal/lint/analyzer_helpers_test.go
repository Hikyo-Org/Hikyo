package lint

// Test helpers that run one analyzer over a few small type-checked packages: a
// fixture loaded with go/packages, or a package built in memory. The repository
// itself is analyzed only through the vet driver (TestVetDriverRepo); nothing
// here loads it.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"
)

// loadFixture loads patterns under ctx with syntax and type information, with
// test variants when tests is set. The context's overlay reaches cmd/go as an
// -overlay build flag: go/packages treats a Config.Overlay as invalidating all
// export data and type-checks the whole dependency closure from source.
func loadFixture(ctx BuildContext, tests bool, patterns ...string) ([]*packages.Package, error) {
	buildFlags := ctx.BuildFlags
	if ctx.needsWebuiPlaceholder() {
		dir, err := os.MkdirTemp("", "hikyo-lint-overlay-")
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(dir) }() // a leaked temp overlay is harmless
		overlay, err := ctx.WriteOverlay(dir)
		if err != nil {
			return nil, err
		}
		if overlay != "" {
			buildFlags = append(slices.Clone(buildFlags), "-overlay="+overlay)
		}
	}
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Tests:      tests,
		BuildFlags: buildFlags,
		Env:        ctx.Env(),
	}, patterns...)
	if err != nil {
		return nil, err
	}
	for _, p := range pkgs {
		for _, e := range p.Errors {
			return nil, fmt.Errorf("load %s under %s: %w", p.PkgPath, ctx.Name, e)
		}
	}
	return pkgs, nil
}

// loadWithSources type-checks pattern under root with the contents of existing
// files replaced in memory (absolute path to source). It swaps the parsed bytes
// through Config.ParseFile, so dependencies keep their export data, which
// Config.Overlay would discard. Every replaced file must be parsed.
func loadWithSources(t *testing.T, root, pattern string, sources map[string][]byte) []*packages.Package {
	t.Helper()
	var mu sync.Mutex
	parsed := map[string]bool{}
	pkgs, err := packages.Load(&packages.Config{
		Dir: root,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			if replaced, ok := sources[filename]; ok {
				src = replaced
				mu.Lock()
				parsed[filename] = true
				mu.Unlock()
			}
			// go/packages' own default mode.
			return parser.ParseFile(fset, filename, src, parser.AllErrors|parser.ParseComments)
		},
	}, pattern)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) == 0 {
		t.Fatalf("%s loaded no packages", pattern)
	}
	for _, p := range pkgs {
		for _, err := range p.Errors {
			t.Fatal(err)
		}
	}
	for filename := range sources {
		if !parsed[filename] {
			t.Fatalf("replaced source %s was never parsed", filename)
		}
	}
	return pkgs
}

// runAnalyzer runs c's analyzer over each package that has type information and
// returns the findings and the target packages it did not see, rendered as the
// vet driver does.
func runAnalyzer(c Check, pkgs []*packages.Package) []string {
	var findings []string
	seenTargets := map[string]bool{}
	for _, p := range pkgs {
		if p.TypesInfo == nil || p.Types == nil {
			continue
		}
		pkg := p.Types
		if pkg.Path() != p.PkgPath {
			// Tests relocate a loaded package by rewriting PkgPath.
			pkg = types.NewPackage(p.PkgPath, p.Name)
		}
		pass := &analysis.Pass{
			Analyzer:  c.Analyzer,
			Fset:      p.Fset,
			Files:     p.Syntax,
			Pkg:       pkg,
			TypesInfo: p.TypesInfo,
			Report: func(d analysis.Diagnostic) {
				if d.Category != SummaryCategory {
					findings = append(findings, formatFinding(p.Fset.Position(d.Pos).String(), c.Analyzer.Name, d.Message))
					return
				}
				summary, err := parseSummary(d.Message)
				if err == nil {
					err = summary.validate(c.Analyzer.Name)
				}
				if err != nil {
					findings = append(findings, formatFinding("-", c.Analyzer.Name, err.Error()))
				} else if summary.Kind == summaryTarget {
					seenTargets[summary.Subject] = true
				}
			},
		}
		if _, err := c.Analyzer.Run(pass); err != nil {
			findings = append(findings, formatFinding(p.PkgPath, c.Analyzer.Name, "analyzer failed: "+err.Error()))
		}
	}
	findings = append(findings, targetFindings(c.Analyzer.Name, c.Targets, seenTargets)...)
	slices.Sort(findings)
	return findings
}

// loadDefault loads patterns under the default context, test variants included.
func loadDefault(patterns ...string) ([]*packages.Package, error) {
	return loadFixture(Contexts[0], true, patterns...)
}

func checkDriverHandles(pkgs []*packages.Package) []string {
	return runAnalyzer(Check{Analyzer: Handles}, pkgs)
}

// checkRawSQL runs the RawSQL pass over pkgs against the supplied pins, the way
// the vet tool would across packages: facts flow from a dependency to its
// importers, and the summaries are judged as the driver judges them. pins are
// expanded for the context first.
func checkRawSQL(pkgs []*packages.Package, root, prefix string, pins map[string]RawSQLProtocol, buildContext ...string) []string {
	contextName := "default"
	if len(buildContext) > 0 {
		contextName = buildContext[0]
	}
	if len(buildContext) > 1 || !slices.ContainsFunc(Contexts, func(c BuildContext) bool { return c.Name == contextName }) {
		return []string{fmt.Sprintf("rawsql: unknown or ambiguous build context %q", contextName)}
	}
	protocols, err := rawSQLProtocolsInContext(pins, contextName)
	if err != nil {
		return []string{err.Error()}
	}
	type factKey struct {
		pkg, path string
		typ       reflect.Type
	}
	facts := map[factKey]analysis.Fact{}
	keyOf := func(obj types.Object, fact analysis.Fact) (factKey, bool) {
		if obj.Pkg() == nil {
			return factKey{}, false
		}
		path, err := objectpath.For(obj)
		return factKey{obj.Pkg().Path(), string(path), reflect.TypeOf(fact)}, err == nil
	}
	var findings []string
	var summaries []Summary
	for _, p := range orderedByImports(pkgs) {
		if p.TypesInfo == nil || p.Types == nil {
			continue
		}
		pass := &analysis.Pass{
			Analyzer:  RawSQL,
			Fset:      p.Fset,
			Files:     p.Syntax,
			Pkg:       p.Types,
			TypesInfo: p.TypesInfo,
			Report: func(d analysis.Diagnostic) {
				if d.Category != SummaryCategory {
					findings = append(findings, formatFinding(p.Fset.Position(d.Pos).String(), RawSQL.Name, d.Message))
					return
				}
				summary, err := parseSummary(d.Message)
				if err != nil {
					findings = append(findings, err.Error())
					return
				}
				summaries = append(summaries, summary)
			},
			ImportObjectFact: func(obj types.Object, fact analysis.Fact) bool {
				key, ok := keyOf(obj, fact)
				stored, found := facts[key]
				if ok && found {
					reflect.ValueOf(fact).Elem().Set(reflect.ValueOf(stored).Elem())
				}
				return ok && found
			},
			ExportObjectFact: func(obj types.Object, fact analysis.Fact) {
				if key, ok := keyOf(obj, fact); ok {
					facts[key] = fact
				}
			},
		}
		if p.PkgPath != prefix && !strings.HasPrefix(p.PkgPath, prefix+"/") {
			continue
		}
		if err := runRawSQLWith(pass, osSource{}, root, prefix, contextName, protocols, false); err != nil {
			findings = append(findings, err.Error())
		}
	}
	aggregated, err := rawSQLProtocolSummaryFindings(protocols, summaries)
	if err != nil {
		findings = append(findings, err.Error())
	}
	findings = append(findings, aggregated...)
	slices.Sort(findings)
	return findings
}

// orderedByImports lists pkgs with every package after the loaded packages it
// imports, as the vet tool analyzes them, so a fact exists before its importer.
func orderedByImports(pkgs []*packages.Package) []*packages.Package {
	seen := map[*packages.Package]bool{}
	var out []*packages.Package
	var visit func(p *packages.Package)
	visit = func(p *packages.Package) {
		if seen[p] {
			return
		}
		seen[p] = true
		for _, path := range slices.Sorted(mapKeys(p.Imports)) {
			visit(p.Imports[path])
		}
		out = append(out, p)
	}
	for _, p := range pkgs {
		visit(p)
	}
	return out
}

func mapKeys[V any](m map[string]V) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

type rawSQLDeclaration struct {
	pkg *packages.Package
	fn  *ast.FuncDecl
}

// rawSQLDeclarations indexes the production function declarations of the packages
// under prefix by owner key (file:Name relative to root).
func rawSQLDeclarations(pkgs []*packages.Package, root, prefix string) map[string]rawSQLDeclaration {
	declarations := map[string]rawSQLDeclaration{}
	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil || (pkg.PkgPath != prefix && !strings.HasPrefix(pkg.PkgPath, prefix+"/")) {
			continue
		}
		for _, file := range pkg.Syntax {
			path := pkg.Fset.Position(file.Pos()).Filename
			if isTestFile(path) {
				continue
			}
			rel, ok := rawSQLRel(root, path)
			if !ok {
				continue
			}
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
					declarations[rel+":"+rawSQLFunctionName(fn)] = rawSQLDeclaration{pkg, fn}
				}
			}
		}
	}
	return declarations
}

func rawSQLBodyHash(pkg *packages.Package, fn *ast.FuncDecl) (string, error) {
	return rawSQLBodyHashOf(pkg.Fset, pkg.TypesInfo, fn)
}

// rawSQLDependencies is the independent oracle for the helpers an owner reaches:
// it follows handwritten references, stopping at another reviewed owner.
func rawSQLDependencies(owner string, declarations map[string]rawSQLDeclaration, protocols map[string]RawSQLProtocol, root string) map[string]string {
	dependencies := map[string]string{}
	seen := map[string]bool{owner: true}
	var visit func(string)
	visit = func(name string) {
		declaration := declarations[name]
		refs := map[string]bool{}
		ast.Inspect(declaration.fn.Body, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			fn, ok := declaration.pkg.TypesInfo.Uses[id].(*types.Func)
			if !ok {
				return true
			}
			if rel, ok := rawSQLRel(root, declaration.pkg.Fset.Position(fn.Pos()).Filename); ok {
				refs[filepath.ToSlash(rel)+":"+rawSQLObjectName(fn)] = true
			}
			return true
		})
		for _, ref := range slices.Sorted(mapKeys(refs)) {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			if _, reviewed := protocols[ref]; reviewed {
				continue
			}
			dep, exists := declarations[ref]
			if !exists {
				continue
			}
			hash, err := rawSQLBodyHash(dep.pkg, dep.fn)
			if err != nil {
				hash = ""
			}
			dependencies[ref] = hash
			visit(ref)
		}
	}
	if _, exists := declarations[owner]; exists {
		visit(owner)
	}
	return dependencies
}

// generatedSQLSource reads the query files from disk; a read failure is not
// generated.
func generatedSQLSource(root, path string, file *ast.File) bool {
	generated, err := generatedSQLSourceIn(osSource{}, root, path, file)
	return err == nil && generated
}
