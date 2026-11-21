package lint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// go/packages abandons export data for every dependency when it is given
// Config.Overlay (the ui context's webui placeholder took one load's live heap
// from ~1.1GB to ~2.5GB), so no code in the module may set the field. Fixtures
// reach the placeholder through the "-overlay" build flag, and in-memory source
// goes through Config.ParseFile (loadWithSources). The rule is syntactic, so it
// does not depend on how a file names the type: every file is checked, and any
// Overlay field assignment or address, and any Overlay key whose value is not a
// string literal (which a map[string][]byte never is), fails it. Aliases,
// embedding, dot imports and elided types all reach one of those forms; a
// positional literal or reflection would not.
func TestNoPackagesConfigOverlay(t *testing.T) {
	root := moduleRoot()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "node_modules" || name == "testdata" || name == "vendor") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); path != root && err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.KeyValueExpr:
				key, ok := n.Key.(*ast.Ident)
				if lit, isString := n.Value.(*ast.BasicLit); ok && key.Name == "Overlay" && (!isString || lit.Kind != token.STRING) {
					t.Errorf("%s sets an Overlay key: use the -overlay build flag or loadWithSources, never packages.Config.Overlay", path)
				}
			case *ast.AssignStmt:
				for _, lhs := range n.Lhs {
					if isOverlayField(lhs) {
						t.Errorf("%s assigns an Overlay field: use the -overlay build flag or loadWithSources, never packages.Config.Overlay", path)
					}
				}
			case *ast.UnaryExpr:
				if n.Op == token.AND && isOverlayField(n.X) {
					t.Errorf("%s takes an Overlay field's address: use the -overlay build flag or loadWithSources, never packages.Config.Overlay", path)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func isOverlayField(e ast.Expr) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Overlay"
}
