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
)

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
