package lint

// RawSQL as a go/analysis pass (lint-analysis driver ADR, decisions 2 and 3).
//
// Per package, the pass judges the package's own production files: raw SQL
// execution sites, protocol hashes, helper dependencies and restricted
// references. What follows imports travels as an object fact on each production
// function another package can name: its owner key, its body hash and the
// helpers it reaches (the closure, cut at reviewed owners). vet hands a package
// only the facts of its direct imports and of methods, so each fact is
// self-contained: an owner resolves a helper two imports away through the closure
// its direct import exported. What needs the whole repository travels
// as summary diagnostics the driver aggregates per context (see
// rawSQLSummaryFindings): an owner no package declares, a reviewed caller
// nothing references, and a helper dependency pin that is absent or no longer
// reached. The pin JSON and the generated-query files are declared inputs.

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Summary kinds of the rawsql analyzer. The message is "<kind> <subject>".
const (
	// summaryRawOwner: a production function that is a protocol owner; subject
	// is the owner key (file:Name).
	summaryRawOwner = "rawowner"
	// summaryRawCaller: "<target> <caller>", a restricted protocol referenced
	// from a named function.
	summaryRawCaller = "rawcaller"
	// summaryRawHelper: a production function that some protocol pins as a
	// helper dependency; subject is its key.
	summaryRawHelper = "rawhelper"
	// summaryRawUnreached: "<owner> <dependency>", a pinned helper dependency the
	// owner's pass could not reach.
	summaryRawUnreached = "rawunreached"
	// summaryRawDump is reported only under -rawsql.dump, for the reviewed-pin
	// updater: "fn <key> <hash>" for a protocol owner or reviewed caller, and
	// "dep <owner> <helper key> <hash>" for each helper an owner reaches.
	summaryRawDump = "rawdump"
)

// reviewedPinsPath is the pin inventory, relative to the repository root.
const reviewedPinsPath = "internal/lint/testdata/reviewed_pins.json"

func rawSQLSpecs(root string) ([]InputSpec, error) {
	queries, err := queryInputs(root)
	return append([]InputSpec{{File: reviewedPinsPath}}, queries...), err
}

// rawSQLFunc is the object fact on a production function another package can
// name (an exported function or method).
type rawSQLFunc struct {
	// Key is the owner key, file:Name relative to the root.
	Key string
	// Hash is the body hash, empty when the body cannot be pinned.
	Hash string
	// Closure are the helpers the function reaches through handwritten
	// references, in key order. A reviewed protocol owner is an independently
	// pinned boundary: it is not followed and not listed.
	Closure []rawSQLDep
}

type rawSQLDep struct{ Key, Hash string }

func (*rawSQLFunc) AFact() {}

func (f *rawSQLFunc) String() string { return "rawsql " + f.Key }

// RawSQL confines handwritten SQL execution to the reviewed protocol owners.
var RawSQL = newRawSQL()

func newRawSQL() *analysis.Analyzer {
	a := &analysis.Analyzer{
		Name:      "rawsql",
		Doc:       "confines handwritten SQL execution to reviewed protocol owners",
		FactTypes: []analysis.Fact{(*rawSQLFunc)(nil)},
	}
	prefix := Module
	var context string
	native := runtime.GOOS
	var dump bool
	a.Flags.StringVar(&prefix, "prefix", Module, "package path prefix the check applies to")
	a.Flags.StringVar(&context, "context", "", "analysis context name (set by the driver; part of the vet cache key)")
	a.Flags.StringVar(&native, "native", runtime.GOOS, "native helper target the pins are expanded for (the reviewed-pin updater passes linux; the driver leaves the host)")
	a.Flags.BoolVar(&dump, "dump", false, "report the actual owner, caller and helper hashes as rawdump summaries (the reviewed-pin updater)")
	in := flagInputs(a)
	a.Run = func(pass *analysis.Pass) (any, error) {
		path := pass.Pkg.Path()
		if path != prefix && !strings.HasPrefix(path, prefix+"/") {
			return nil, nil
		}
		src, root, err := in.open()
		if err == nil {
			err = anchorUnderRoot(pass, root)
		}
		if err != nil {
			reportRefusal(pass, err)
			return nil, nil
		}
		if err := runRawSQL(pass, src, root, prefix, context, native, dump); err != nil {
			reportRefusal(pass, err)
		}
		return nil, nil
	}
	return a
}

// rawSQLConfig loads the protocols in force for the context: a refusal for an
// unreadable or invalid inventory and for a context the driver did not name.
func rawSQLConfig(src source, root, context, native string) (map[string]RawSQLProtocol, error) {
	if context == "" {
		return nil, errors.New("the driver did not pass -rawsql.context")
	}
	if !slices.ContainsFunc(Contexts, func(c BuildContext) bool { return c.Name == context }) {
		return nil, fmt.Errorf("unknown build context %q", context)
	}
	raw, err := src.ReadFile(filepath.Join(root, filepath.FromSlash(reviewedPinsPath)))
	if err != nil {
		return nil, err
	}
	inventory, err := parseReviewedPins(raw)
	if err != nil {
		return nil, err
	}
	protocols, err := inventory.rawProtocolsForGOOS(native)
	if err != nil {
		return nil, err
	}
	return rawSQLProtocolsInContext(protocols, context)
}

type rawSQLUnit struct {
	pass   *analysis.Pass
	prefix string
}

func (u rawSQLUnit) underPrefix(path string) bool {
	return path == u.prefix || strings.HasPrefix(path, u.prefix+"/")
}

// rel is the path of a file relative to the root, or false outside it.
func rawSQLRel(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

type rawSQLFile struct {
	file *ast.File
	rel  string
}

func runRawSQL(pass *analysis.Pass, src source, root, prefix, context, native string, dump bool) error {
	protocols, err := rawSQLConfig(src, root, context, native)
	if err != nil {
		return err
	}
	return runRawSQLWith(pass, src, root, prefix, context, protocols, dump)
}

// runRawSQLWith is runRawSQL over protocols already in force for the context.
func runRawSQLWith(pass *analysis.Pass, src source, root, prefix, context string, protocols map[string]RawSQLProtocol, dump bool) error {
	u := rawSQLUnit{pass: pass, prefix: prefix}
	report := func(pos token.Pos, format string, args ...any) {
		pass.Report(analysis.Diagnostic{Pos: pos, Message: "rawsql: " + fmt.Sprintf(format, args...)})
	}
	// Production files: the decisions use the on-disk name, so a //line directive
	// cannot make a production file a test or generated one.
	var files []rawSQLFile
	for _, file := range pass.Files {
		path := fileName(pass, file.Pos())
		if isTestFile(path) {
			continue
		}
		generated, err := generatedSQLSourceIn(src, root, path, file)
		if err != nil {
			return err
		}
		if generated {
			continue
		}
		rel, ok := rawSQLRel(root, path)
		if !ok {
			// A source with no repository owner: any execution in it fails closed.
			ast.Inspect(file, func(node ast.Node) bool {
				if rawSQLNode(pass.TypesInfo, node) {
					report(node.Pos(), "cannot identify source owner for %s", path)
				}
				return true
			})
			continue
		}
		files = append(files, rawSQLFile{file, rel})
	}

	// The production functions of this package, by owner key and by name.
	type node struct {
		key  string
		obj  *types.Func
		decl *ast.FuncDecl
		hash string
		refs []*types.Func
	}
	nodes := map[*ast.FuncDecl]*node{}
	byName := map[string]*node{}
	hashErrors := map[string]error{}
	for _, f := range files {
		for _, decl := range f.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			name := rawSQLFunctionName(fn)
			n := &node{key: f.rel + ":" + name, decl: fn}
			n.obj, _ = pass.TypesInfo.Defs[fn.Name].(*types.Func)
			hash, err := rawSQLBodyHashOf(pass.Fset, pass.TypesInfo, fn)
			if err != nil {
				hash, hashErrors[n.key] = "", err
			}
			n.hash = hash
			n.refs = u.references(fn)
			nodes[fn] = n
			if name != "init" && name != "_" {
				byName[name] = n
			}
		}
	}
	// target resolves a referenced function to its owner key and body hash, and
	// to the closure behind it when another package declares it.
	type target struct {
		key, hash string
		local     *node
		closure   []rawSQLDep
	}
	resolve := func(fn *types.Func) (target, bool) {
		if fn.Pkg() == pass.Pkg {
			if n := byName[rawSQLObjectName(fn)]; n != nil {
				return target{key: n.key, hash: n.hash, local: n}, true
			}
			return target{}, false
		}
		var fact rawSQLFunc
		if !pass.ImportObjectFact(fn, &fact) {
			return target{}, false
		}
		return target{key: fact.Key, hash: fact.Hash, closure: fact.Closure}, true
	}
	// reach is the helpers a function reaches, cut at reviewed owners: the walk
	// over this package's functions and the closures of imported ones.
	reach := func(start *node) map[string]string {
		out := map[string]string{}
		seen := map[string]bool{start.key: true}
		var visit func(*node)
		visit = func(n *node) {
			for _, ref := range n.refs {
				t, ok := resolve(ref)
				if !ok || seen[t.key] {
					continue
				}
				seen[t.key] = true
				if _, reviewed := protocols[t.key]; reviewed {
					continue
				}
				out[t.key] = t.hash
				if t.local != nil {
					visit(t.local)
					continue
				}
				for _, d := range t.closure {
					seen[d.Key] = true
					out[d.Key] = d.Hash
				}
			}
		}
		visit(start)
		return out
	}
	for _, n := range nodes {
		if n.obj == nil || !rawSQLNameable(n.obj) {
			continue
		}
		closure := make([]rawSQLDep, 0)
		for key, hash := range reach(n) {
			closure = append(closure, rawSQLDep{key, hash})
		}
		slices.SortFunc(closure, func(a, b rawSQLDep) int { return strings.Compare(a.Key, b.Key) })
		pass.ExportObjectFact(n.obj, &rawSQLFunc{Key: n.key, Hash: n.hash, Closure: closure})
	}

	pinnedHelpers := map[string]bool{}
	reviewedCallers := map[string]bool{}
	for _, protocol := range protocols {
		for caller := range protocol.Callers {
			reviewedCallers[caller] = true
		}
		for dependency := range rawSQLProtocolDependencies(protocol, context) {
			pinnedHelpers[dependency] = true
		}
	}
	restricted := func(node ast.Node) string {
		id, ok := node.(*ast.Ident)
		if !ok {
			return ""
		}
		fn, ok := pass.TypesInfo.Uses[id].(*types.Func)
		if !ok || fn.Pkg() == nil || !u.underPrefix(fn.Pkg().Path()) {
			return ""
		}
		t, ok := resolve(fn.Origin())
		if ok && protocols[t.key].Callers != nil {
			return t.key
		}
		return ""
	}

	for _, f := range files {
		for _, declaration := range f.file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok {
				ast.Inspect(declaration, func(node ast.Node) bool {
					if target := restricted(node); target != "" {
						report(node.Pos(), "%s: references protocol %s outside a named reviewed caller", pass.Fset.Position(node.Pos()), target)
					}
					if rawSQLNode(pass.TypesInfo, node) {
						report(node.Pos(), "%s: handwritten SQL outside a named function owner", pass.Fset.Position(node.Pos()))
					}
					return true
				})
				continue
			}
			if fn.Body == nil {
				continue
			}
			owner := f.rel + ":" + rawSQLFunctionName(fn)
			if _, reviewed := protocols[owner]; reviewed {
				reportSummary(pass, summaryRawOwner, owner)
			}
			if pinnedHelpers[owner] {
				reportSummary(pass, summaryRawHelper, owner)
			}
			self := nodes[fn]
			actual := self.hash
			if actual == "" {
				report(fn.Pos(), "%s: cannot pin body: %v", owner, hashErrors[owner])
				continue
			}
			if dump {
				_, isOwner := protocols[owner]
				if isOwner || reviewedCallers[owner] {
					reportSummary(pass, summaryRawDump, "fn "+owner+" "+actual)
				}
				if isOwner {
					for dependency, hash := range reach(self) {
						reportSummary(pass, summaryRawDump, "dep "+owner+" "+dependency+" "+hash)
					}
				}
			}
			var sites []token.Pos
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				if target := restricted(node); target != "" {
					reportSummary(pass, summaryRawCaller, target+" "+owner)
					if protocols[target].Callers[owner] != actual {
						report(node.Pos(), "%s references protocol %s outside its reviewed caller body", owner, target)
					}
				}
				if rawSQLNode(pass.TypesInfo, node) {
					sites = append(sites, node.Pos())
				}
				return true
			})
			protocol, allowed := protocols[owner]
			if len(sites) == 0 {
				if allowed {
					report(fn.Pos(), "%s no longer executes raw SQL; retire its protocol exception", owner)
				}
				continue
			}
			if !allowed || strings.TrimSpace(protocol.Reason) == "" {
				for _, site := range sites {
					report(site, "%s: %s executes handwritten SQL outside a named engine protocol", pass.Fset.Position(site), owner)
				}
				continue
			}
			expected := rawSQLProtocolDependencies(protocol, context)
			for name, dependencies := range protocol.BuildDependencies {
				if !slices.ContainsFunc(Contexts, func(c BuildContext) bool { return c.Name == name }) {
					report(fn.Pos(), "%s has unknown dependency build context %s", owner, name)
				}
				for dependency := range dependencies {
					if _, common := protocol.Dependencies[dependency]; common {
						report(fn.Pos(), "%s duplicates common helper dependency %s in context %s", owner, dependency, name)
					}
				}
			}
			actualDependencies := reach(self)
			for dependency, hash := range actualDependencies {
				if want, exists := expected[dependency]; !exists {
					report(fn.Pos(), "%s is missing reviewed helper dependency %s", owner, dependency)
				} else if want != hash || hash == "" {
					report(fn.Pos(), "%s changed reviewed helper dependency %s (hash %s)", owner, dependency, hash)
				}
			}
			for dependency := range expected {
				if _, reachable := actualDependencies[dependency]; !reachable {
					reportSummary(pass, summaryRawUnreached, owner+" "+dependency)
				}
			}
			if protocol.Hash != actual {
				report(fn.Pos(), "%s changed its reviewed engine protocol (hash %s)", owner, actual)
			}
		}
	}
	return nil
}

// references lists the module functions fn's body references.
func (u rawSQLUnit) references(fn *ast.FuncDecl) []*types.Func {
	seen := map[*types.Func]bool{}
	var refs []*types.Func
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		used, ok := u.pass.TypesInfo.Uses[id].(*types.Func)
		if !ok || used.Pkg() == nil || !u.underPrefix(used.Pkg().Path()) {
			return true
		}
		if used = used.Origin(); !seen[used] {
			seen[used] = true
			refs = append(refs, used)
		}
		return true
	})
	return refs
}

// rawSQLNameable is whether another package can name fn: an exported function
// or an exported method, which is all a fact must describe.
func rawSQLNameable(fn *types.Func) bool { return fn.Exported() }

// rawSQLSummaryFindings is the whole-repository half of the RawSQL check: the
// findings the per-package passes cannot decide, from the summaries of every
// package of one context, with the pins expanded for native (the reviewed-pin
// updater verifies for linux, whatever its host). src must be the verified
// inputs the vet run used, so the inventory judged here is the one the passes saw.
func rawSQLSummaryFindings(src source, root, context, native string, summaries []Summary) ([]string, error) {
	protocols, err := rawSQLConfig(src, root, context, native)
	if err != nil {
		return nil, err
	}
	return rawSQLProtocolSummaryFindings(protocols, summaries)
}

// rawSQLProtocolSummaryFindings judges the summaries against protocols already
// in force for the context.
func rawSQLProtocolSummaryFindings(protocols map[string]RawSQLProtocol, summaries []Summary) ([]string, error) {
	owners := map[string]bool{}
	helpers := map[string]bool{}
	callers := map[string]map[string]bool{}
	var unreached [][2]string
	for _, s := range summaries {
		fields := strings.Fields(s.Subject)
		switch s.Kind {
		case summaryRawOwner, summaryRawHelper:
			if len(fields) != 1 {
				return nil, fmt.Errorf("malformed %s summary %q", s.Kind, s.Subject)
			}
			if s.Kind == summaryRawOwner {
				owners[s.Subject] = true
			} else {
				helpers[s.Subject] = true
			}
		case summaryRawCaller, summaryRawUnreached:
			if len(fields) != 2 {
				return nil, fmt.Errorf("malformed %s summary %q", s.Kind, s.Subject)
			}
			if s.Kind == summaryRawCaller {
				if callers[fields[0]] == nil {
					callers[fields[0]] = map[string]bool{}
				}
				callers[fields[0]][fields[1]] = true
			} else {
				unreached = append(unreached, [2]string{fields[0], fields[1]})
			}
		default:
			return nil, fmt.Errorf("unknown rawsql summary kind %q", s.Kind)
		}
	}
	var findings []string
	for owner, protocol := range protocols {
		if !owners[owner] {
			findings = append(findings, fmt.Sprintf("rawsql: protocol owner %s is absent from analyzed production source", owner))
		}
		for caller := range protocol.Callers {
			if !callers[owner][caller] {
				findings = append(findings, fmt.Sprintf("rawsql: protocol %s no longer has reviewed caller %s; retire its caller pin", owner, caller))
			}
		}
	}
	for _, pair := range unreached {
		if helpers[pair[1]] {
			findings = append(findings, fmt.Sprintf("rawsql: %s no longer references helper dependency %s; retire its dependency pin", pair[0], pair[1]))
		} else {
			findings = append(findings, fmt.Sprintf("rawsql: %s helper dependency %s is absent from analyzed production source", pair[0], pair[1]))
		}
	}
	slices.Sort(findings)
	return slices.Compact(findings), nil
}
