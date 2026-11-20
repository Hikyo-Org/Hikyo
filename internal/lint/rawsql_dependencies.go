package lint

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

type rawSQLDeclaration struct {
	pkg *packages.Package
	fn  *ast.FuncDecl
}

// Bind each named constant reference to its resolved value in lexical order.
// Without named constants the established formatted-body digest is unchanged.
func rawSQLBodyHash(pkg *packages.Package, fn *ast.FuncDecl) (string, error) {
	return rawSQLBodyHashOf(pkg.Fset, pkg.TypesInfo, fn)
}

func rawSQLBodyHashOf(fset *token.FileSet, info *types.Info, fn *ast.FuncDecl) (string, error) {
	var body bytes.Buffer
	if err := format.Node(&body, fset, fn.Body); err != nil {
		return "", err
	}
	var bindings []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		c, ok := info.Uses[id].(*types.Const)
		if !ok || c.Pkg() == nil {
			return true
		}
		name := c.Pkg().Path() + "." + c.Name()
		value := c.Val().ExactString()
		bindings = append(bindings, fmt.Sprintf("%d:%s%d:%s\n", len(name), name, len(value), value))
		return true
	})
	if len(bindings) > 0 {
		body.WriteString("\nresolved-constant-bindings\n")
		for _, binding := range bindings {
			body.WriteString(binding)
		}
	}
	digest := sha256.Sum256(body.Bytes())
	return hex.EncodeToString(digest[:]), nil
}

func rawSQLDeclarations(pkgs []*packages.Package, root, prefix string) map[string]rawSQLDeclaration {
	declarations := map[string]rawSQLDeclaration{}
	for _, pkg := range flatten(pkgs) {
		if pkg.TypesInfo == nil || (pkg.PkgPath != prefix && !strings.HasPrefix(pkg.PkgPath, prefix+"/")) {
			continue
		}
		for _, file := range pkg.Syntax {
			path := pkg.Fset.Position(file.Pos()).Filename
			if strings.HasSuffix(path, "_test.go") || generatedSQLSource(root, path, file) {
				continue
			}
			rel, err := filepath.Rel(root, path)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if ok && fn.Body != nil {
					declarations[filepath.ToSlash(rel)+":"+rawSQLFunctionName(fn)] = rawSQLDeclaration{pkg, fn}
				}
			}
		}
	}
	return declarations
}

func rawSQLHelperReferences(declaration rawSQLDeclaration, root string) []string {
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
		pos := declaration.pkg.Fset.Position(fn.Pos())
		rel, err := filepath.Rel(root, pos.Filename)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
		refs[filepath.ToSlash(rel)+":"+rawSQLObjectName(fn)] = true
		return true
	})
	result := make([]string, 0, len(refs))
	for ref := range refs {
		result = append(result, ref)
	}
	slices.Sort(result)
	return result
}

// Follow handwritten helper references, including aliases and callbacks. Another
// reviewed protocol owner is an independently pinned boundary, not a new grant.
func rawSQLDependencies(owner string, declarations map[string]rawSQLDeclaration, protocols map[string]RawSQLProtocol, root string) map[string]string {
	if _, exists := declarations[owner]; !exists {
		return map[string]string{}
	}
	return rawSQLWalkDependencies(owner, protocols,
		func(name string) []string { return rawSQLHelperReferences(declarations[name], root) },
		func(ref string) (string, bool) {
			declaration, exists := declarations[ref]
			if !exists {
				return "", false
			}
			hash, err := rawSQLBodyHash(declaration.pkg, declaration.fn)
			if err != nil {
				hash = ""
			}
			return hash, true
		})
}

// rawSQLWalkDependencies is the dependency walk both harnesses share. refs
// lists a declaration's helper references and hash returns a declared helper's
// body hash (empty when it cannot be pinned); a reference with no declaration
// is not a dependency.
func rawSQLWalkDependencies(owner string, protocols map[string]RawSQLProtocol, refs func(string) []string, hash func(string) (string, bool)) map[string]string {
	dependencies := map[string]string{}
	seen := map[string]bool{owner: true}
	var visit func(string)
	visit = func(name string) {
		for _, ref := range refs(name) {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			if _, reviewed := protocols[ref]; reviewed {
				continue
			}
			digest, exists := hash(ref)
			if !exists {
				continue
			}
			dependencies[ref] = digest
			visit(ref)
		}
	}
	visit(owner)
	return dependencies
}

// Common pins cannot be overridden by build-specific variants. Each supported
// analysis context validates its exact source owner and all four run in CI.
func rawSQLProtocolDependencies(protocol RawSQLProtocol, context string) map[string]string {
	expected := map[string]string{}
	for owner, hash := range protocol.Dependencies {
		expected[owner] = hash
	}
	for owner, hash := range protocol.BuildDependencies[context] {
		if _, common := expected[owner]; !common {
			expected[owner] = hash
		}
	}
	return expected
}

// AST declarations and typed method references use the same receiver-qualified
// owner, including pointer and instantiated generic receivers.
func rawSQLObjectName(fn *types.Func) string {
	name := fn.Name()
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		typ := types.Unalias(sig.Recv().Type())
		if pointer, ok := typ.(*types.Pointer); ok {
			typ = types.Unalias(pointer.Elem())
		}
		if named, ok := typ.(*types.Named); ok {
			name = named.Obj().Name() + "." + name
		}
	}
	return name
}
