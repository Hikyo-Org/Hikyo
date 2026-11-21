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
// from ~1.1GB to ~2.5GB). Fixtures reach the placeholder through the
// "-overlay" build flag instead, so no code in the module may set the field,
// bar the two mutation tests that overlay in-memory source onto one small
// scripts/security package (not a context placeholder) and load nothing else.
var overlayMutationTests = map[string]bool{
	"internal/lint/alert_collector_protocol_test.go": true,
	"internal/lint/rawsql_artifact_test.go":          true,
}

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
		if rel, err := filepath.Rel(root, path); err == nil && overlayMutationTests[filepath.ToSlash(rel)] {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				sel, ok := n.Type.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Config" {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "packages" {
					return true
				}
				for _, elt := range n.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Overlay" {
							t.Errorf("%s sets packages.Config.Overlay", path)
						}
					}
				}
			case *ast.AssignStmt:
				for _, lhs := range n.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "Overlay" {
						t.Errorf("%s assigns an Overlay field: use the -overlay build flag, never packages.Config.Overlay", path)
					}
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
