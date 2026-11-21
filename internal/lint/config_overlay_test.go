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
// goes through Config.ParseFile (loadWithSources). A file that imports
// go/packages, under any name, may use no Overlay key or field assignment at
// all, so no aliased, dot-imported or elided type escapes the rule.
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
		importsPackages := false
		for _, imp := range file.Imports {
			importsPackages = importsPackages || imp.Path.Value == `"golang.org/x/tools/go/packages"`
		}
		if !importsPackages {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.KeyValueExpr:
				if key, ok := n.Key.(*ast.Ident); ok && key.Name == "Overlay" {
					t.Errorf("%s sets an Overlay key: use the -overlay build flag or loadWithSources, never packages.Config.Overlay", path)
				}
			case *ast.AssignStmt:
				for _, lhs := range n.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "Overlay" {
						t.Errorf("%s assigns an Overlay field: use the -overlay build flag or loadWithSources, never packages.Config.Overlay", path)
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
