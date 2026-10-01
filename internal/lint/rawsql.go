package lint

import (
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// RawSQLProtocol grants one source-owned engine operation raw execution, not
// ambient authority to a package or file. Hash pins the formatted function
// body and resolved constant bindings; named helper dependency bodies bind SQL
// builders, escaping and admission paths without granting them raw execution.
type RawSQLProtocol struct {
	Hash              string                       `json:"hash"`
	Reason            string                       `json:"reason"`
	Callers           map[string]string            `json:"callers,omitempty"`
	Dependencies      map[string]string            `json:"dependencies,omitempty"`
	BuildDependencies map[string]map[string]string `json:"build_dependencies,omitempty"`
}

// CheckRawSQL confines handwritten execution to the reviewed protocol owners.
// Generated sqlc files and test fixtures are excluded from production ownership.
// Method values, aliases and interface calls are checked by their typed call
// signature, so changing the receiver spelling cannot hide an execution site.
func CheckRawSQL(pkgs []*packages.Package, repoRoot, packagePrefix string, protocols map[string]RawSQLProtocol, buildContext ...string) []string {
	var findings []string
	contextName := "default"
	if len(buildContext) > 0 {
		contextName = buildContext[0]
	}
	knownContext := false
	for _, context := range Contexts {
		knownContext = knownContext || context.Name == contextName
	}
	if !knownContext || len(buildContext) > 1 {
		return []string{fmt.Sprintf("rawsql: unknown or ambiguous build context %q", contextName)}
	}
	seen := map[string]bool{}
	declarations := map[string]bool{}
	references := map[string]map[string]bool{}
	dependencyDeclarations := rawSQLDeclarations(pkgs, repoRoot, packagePrefix)
	for _, pkg := range flatten(pkgs) {
		if (pkg.PkgPath != packagePrefix && !strings.HasPrefix(pkg.PkgPath, packagePrefix+"/")) || pkg.TypesInfo == nil {
			continue
		}
		for _, file := range pkg.Syntax {
			path := pkg.Fset.Position(file.Pos()).Filename
			if strings.HasSuffix(path, "_test.go") || generatedSQLSource(repoRoot, path, file) {
				continue
			}
			relative, err := filepath.Rel(repoRoot, path)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				// packages.Load also returns cmd/go's synthetic test mains from
				// the build cache. They have no SQL execution and no repository
				// owner. Any execution in an unowned source still fails closed.
				ast.Inspect(file, func(node ast.Node) bool {
					if rawSQLNode(pkg.TypesInfo, node) {
						findings = append(findings, fmt.Sprintf("rawsql: cannot identify source owner for %s", path))
					}
					return true
				})
				continue
			}
			for _, declaration := range file.Decls {
				fn, ok := declaration.(*ast.FuncDecl)
				if !ok {
					ast.Inspect(declaration, func(node ast.Node) bool {
						if target := rawSQLRestrictedReference(pkg, repoRoot, node, protocols); target != "" {
							findings = append(findings, fmt.Sprintf("rawsql: %s references protocol %s outside a named reviewed caller", pkg.Fset.Position(node.Pos()), target))
						}
						if rawSQLNode(pkg.TypesInfo, node) {
							findings = append(findings, fmt.Sprintf("rawsql: %s: handwritten SQL outside a named function owner", pkg.Fset.Position(node.Pos())))
						}
						return true
					})
					continue
				}
				if fn.Body == nil {
					continue
				}
				owner := filepath.ToSlash(relative) + ":" + rawSQLFunctionName(fn)
				declarationID := fmt.Sprintf("%s:%d", owner, pkg.Fset.Position(fn.Pos()).Offset)
				if declarations[declarationID] {
					continue
				}
				declarations[declarationID] = true
				seen[owner] = true
				actual, err := rawSQLBodyHash(pkg, fn)
				if err != nil {
					findings = append(findings, fmt.Sprintf("rawsql: %s: cannot pin body: %v", owner, err))
					continue
				}
				var sites []string
				ast.Inspect(fn.Body, func(node ast.Node) bool {
					if target := rawSQLRestrictedReference(pkg, repoRoot, node, protocols); target != "" {
						if references[target] == nil {
							references[target] = map[string]bool{}
						}
						references[target][owner] = true
						if protocols[target].Callers[owner] != actual {
							findings = append(findings, fmt.Sprintf("rawsql: %s references protocol %s outside its reviewed caller body", owner, target))
						}
					}
					if rawSQLNode(pkg.TypesInfo, node) {
						sites = append(sites, pkg.Fset.Position(node.Pos()).String())
					}
					return true
				})
				if len(sites) == 0 {
					if _, allowed := protocols[owner]; allowed {
						findings = append(findings, fmt.Sprintf("rawsql: %s no longer executes raw SQL; retire its protocol exception", owner))
					}
					continue
				}
				protocol, allowed := protocols[owner]
				if !allowed || strings.TrimSpace(protocol.Reason) == "" {
					for _, site := range sites {
						findings = append(findings, fmt.Sprintf("rawsql: %s: %s executes handwritten SQL outside a named engine protocol", site, owner))
					}
					continue
				}
				expectedDependencies := rawSQLProtocolDependencies(protocol, contextName)
				for context, dependencies := range protocol.BuildDependencies {
					known := false
					for _, supported := range Contexts {
						known = known || context == supported.Name
					}
					if !known {
						findings = append(findings, fmt.Sprintf("rawsql: %s has unknown dependency build context %s", owner, context))
					}
					for dependency := range dependencies {
						if _, common := protocol.Dependencies[dependency]; common {
							findings = append(findings, fmt.Sprintf("rawsql: %s duplicates common helper dependency %s in context %s", owner, dependency, context))
						}
					}
				}
				actualDependencies := rawSQLDependencies(owner, dependencyDeclarations, protocols, repoRoot)
				for dependency, hash := range actualDependencies {
					if expected, exists := expectedDependencies[dependency]; !exists {
						findings = append(findings, fmt.Sprintf("rawsql: %s is missing reviewed helper dependency %s", owner, dependency))
					} else if expected != hash || hash == "" {
						findings = append(findings, fmt.Sprintf("rawsql: %s changed reviewed helper dependency %s (hash %s)", owner, dependency, hash))
					}
				}
				for dependency := range expectedDependencies {
					if _, exists := dependencyDeclarations[dependency]; !exists {
						findings = append(findings, fmt.Sprintf("rawsql: %s helper dependency %s is absent from analyzed production source", owner, dependency))
					} else if _, reachable := actualDependencies[dependency]; !reachable {
						findings = append(findings, fmt.Sprintf("rawsql: %s no longer references helper dependency %s; retire its dependency pin", owner, dependency))
					}
				}
				if protocol.Hash != actual {
					findings = append(findings, fmt.Sprintf("rawsql: %s changed its reviewed engine protocol (hash %s)", owner, actual))
				}
			}
		}
	}
	for owner := range protocols {
		if !seen[owner] {
			findings = append(findings, fmt.Sprintf("rawsql: protocol owner %s is absent from analyzed production source", owner))
		}
		for caller := range protocols[owner].Callers {
			if !references[owner][caller] {
				findings = append(findings, fmt.Sprintf("rawsql: protocol %s no longer has reviewed caller %s; retire its caller pin", owner, caller))
			}
		}
	}
	slices.Sort(findings)
	return findings
}

// A helper that accepts caller-selected catalog names must not become a new
// raw SQL access path. Track references, including aliases and callbacks,
// rather than only direct calls to the helper.
func rawSQLRestrictedReference(pkg *packages.Package, root string, node ast.Node, protocols map[string]RawSQLProtocol) string {
	identifier, ok := node.(*ast.Ident)
	if !ok {
		return ""
	}
	function, ok := pkg.TypesInfo.Uses[identifier].(*types.Func)
	if !ok {
		return ""
	}
	position := pkg.Fset.Position(function.Pos())
	path, err := filepath.Rel(root, position.Filename)
	if err != nil {
		return ""
	}
	owner := filepath.ToSlash(path) + ":" + rawSQLObjectName(function)
	if protocols[owner].Callers != nil {
		return owner
	}
	return ""
}

func generatedSQLSource(repoRoot, path string, file *ast.File) bool {
	if !ast.IsGenerated(file) {
		return false
	}
	sqlcHeader := false
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if comment.Text == "// Code generated by sqlc. DO NOT EDIT." && comment.Pos() < file.Package {
				sqlcHeader = true
			}
		}
	}
	if !sqlcHeader {
		return false
	}
	for _, engine := range []string{"sqlite", "postgres"} {
		output := map[string]string{"sqlite": "sqlitegen", "postgres": "pggen"}[engine]
		relative, err := filepath.Rel(filepath.Join(repoRoot, "internal", "store", output), path)
		if err != nil || filepath.Base(relative) != relative {
			continue
		}
		if relative == "db.go" || relative == "models.go" {
			return true
		}
		if !strings.HasSuffix(relative, ".sql.go") {
			continue
		}
		query := filepath.Join(repoRoot, "internal", "store", "queries", engine, strings.TrimSuffix(relative, ".go"))
		if info, err := os.Lstat(query); err == nil && info.Mode().IsRegular() {
			contents, err := os.ReadFile(query)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(contents), "\n") {
				if nameRe.MatchString(strings.TrimSpace(line)) {
					return true
				}
			}
		}
	}
	return false
}

func rawSQLFunctionName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	receiver := fn.Recv.List[0].Type
	if pointer, ok := receiver.(*ast.StarExpr); ok {
		receiver = pointer.X
	}
	// Method references use the named receiver without type arguments.
	switch indexed := receiver.(type) {
	case *ast.IndexExpr:
		receiver = indexed.X
	case *ast.IndexListExpr:
		receiver = indexed.X
	}
	if name, ok := receiver.(*ast.Ident); ok {
		return name.Name + "." + fn.Name.Name
	}
	return expressionString(receiver) + "." + fn.Name.Name
}

func rawSQLCall(info *types.Info, call *ast.CallExpr) bool {
	if selector, ok := call.Fun.(*ast.SelectorExpr); ok && rawSQLNode(info, selector) {
		return true
	}
	if selector, ok := call.Fun.(*ast.SelectorExpr); ok && (selector.Sel.Name == "SQL" || selector.Sel.Name == "SQLPerEngine") {
		if method, ok := info.Uses[selector.Sel].(*types.Func); ok && method.Pkg() != nil && method.Pkg().Path() == Module+"/internal/store" {
			return true
		}
	}
	signature, ok := info.TypeOf(call.Fun).(*types.Signature)
	if !ok {
		return false
	}
	if receiver := signature.Recv(); receiver != nil {
		key := rawSQLNamedType(receiver.Type())
		if key == "github.com/jackc/pgx/v5/pgconn.Batch" || key == "github.com/jackc/pgx/v5/pgproto3.Frontend" {
			return true // low-level batch or frontend protocol entry
		}
	}
	if signature.Results().Len() > 0 && rawSQLNamedType(signature.Results().At(0).Type()) == "github.com/jackc/pgx/v5/pgproto3.Frontend" {
		return true // expose the raw wire-protocol handle, including aliases
	}
	if signature.Params().Len() < 1 {
		return false
	}
	variadicSQL := false
	if signature.Variadic() {
		if arguments, ok := signature.Params().At(signature.Params().Len() - 1).Type().(*types.Slice); ok {
			if element, ok := types.Unalias(arguments.Elem()).Underlying().(*types.Interface); ok {
				variadicSQL = element.Empty()
			}
		}
	}
	databaseResult := false
	if signature.Results().Len() > 0 {
		result := types.Unalias(signature.Results().At(0).Type())
		if pointer, ok := result.(*types.Pointer); ok {
			result = types.Unalias(pointer.Elem())
		}
		if named, ok := result.(*types.Named); ok {
			databaseResult = rawSQLResultTypes[namedKey(named)]
		}
	}
	if databaseResult {
		if first, ok := types.Unalias(signature.Params().At(0).Type()).Underlying().(*types.Basic); ok && first.Kind() == types.String {
			return true // context-free execution, preparation or batch queue
		}
	}
	if signature.Params().Len() < 2 {
		return false
	}
	// Raw execution takes (context.Context, SQL string, ...any). A typed query
	// instead accepts its generated parameter carrier and cannot match this.
	contextType, ok := types.Unalias(signature.Params().At(0).Type()).(*types.Named)
	if !ok || namedKey(contextType) != "context.Context" {
		return false
	}
	if key := rawSQLNamedType(signature.Params().At(1).Type()); key == "github.com/jackc/pgx/v5.Batch" || key == "github.com/jackc/pgx/v5/pgconn.Batch" {
		return true
	}
	if identifier, ok := types.Unalias(signature.Params().At(1).Type()).(*types.Named); ok && namedKey(identifier) == "github.com/jackc/pgx/v5.Identifier" {
		return true // PostgreSQL COPY is a named bulk-transfer protocol
	}
	if signature.Params().Len() == 3 && signature.Results().Len() > 0 {
		if result, ok := types.Unalias(signature.Results().At(0).Type()).(*types.Named); ok && namedKey(result) == "github.com/jackc/pgx/v5/pgconn.CommandTag" {
			if query, ok := types.Unalias(signature.Params().At(2).Type()).Underlying().(*types.Basic); ok && query.Kind() == types.String {
				return true // low-level COPY(ctx, reader/writer, statement), including aliases
			}
		}
	}
	if databaseResult {
		if argument, ok := types.Unalias(signature.Params().At(1).Type()).Underlying().(*types.Basic); ok && argument.Kind() == types.String {
			return true // prepared statements and low-level non-variadic execution
		}
	}
	if !variadicSQL || signature.Params().Len() < 3 || signature.Results().Len() == 0 {
		return false
	}
	argument, ok := types.Unalias(signature.Params().At(1).Type()).Underlying().(*types.Basic)
	return ok && argument.Kind() == types.String
}

func rawSQLNamedType(value types.Type) string {
	if value == nil {
		return ""
	}
	value = types.Unalias(value)
	if pointer, ok := value.(*types.Pointer); ok {
		value = types.Unalias(pointer.Elem())
	}
	if named, ok := value.(*types.Named); ok {
		return namedKey(named)
	}
	return ""
}

func rawSQLNode(info *types.Info, node ast.Node) bool {
	if call, ok := node.(*ast.CallExpr); ok {
		return rawSQLCall(info, call)
	}
	if selector, ok := node.(*ast.SelectorExpr); ok {
		if method, ok := info.Uses[selector.Sel].(*types.Func); ok {
			if signature, ok := method.Type().(*types.Signature); ok && signature.Recv() != nil {
				key := rawSQLNamedType(signature.Recv().Type())
				if key == "github.com/jackc/pgx/v5/pgconn.Batch" || key == "github.com/jackc/pgx/v5/pgproto3.Frontend" {
					return true
				}
			}
		}
	}
	if literal, ok := node.(*ast.CompositeLit); ok && rawSQLNamedType(info.TypeOf(literal)) == "github.com/jackc/pgx/v5.QueuedQuery" {
		return true // a literal can carry SQL without Batch.Queue
	}
	return false
}

var rawSQLResultTypes = map[string]bool{
	"database/sql.Result": true, "database/sql.Rows": true, "database/sql.Row": true, "database/sql.Stmt": true,
	"database/sql/driver.Stmt":   true,
	"database/sql/driver.Result": true, "database/sql/driver.Rows": true,
	"github.com/jackc/pgx/v5.Rows": true, "github.com/jackc/pgx/v5.Row": true, "github.com/jackc/pgx/v5.QueuedQuery": true,
	"github.com/jackc/pgx/v5/pgconn.StatementDescription": true,
	"github.com/jackc/pgx/v5/pgconn.MultiResultReader":    true, "github.com/jackc/pgx/v5/pgconn.ResultReader": true,
}
