package isolation

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// The privacy seam is an enumerated local-host exception. A future HTTP/MCP
// handler or unrelated service cannot acquire these doors without changing this
// explicit reviewed allowlist alongside the authority model.
func TestPrivacyLocalAuthorityCallSites(t *testing.T) {
	allowed := map[string]map[string]bool{}
	for _, name := range []string{"CorrectPrivacySubject", "ExportPrivacySubject", "ApplyPrivacySubject", "ReapplyPrivacyReceipt"} {
		allowed[name] = map[string]bool{"internal/app/privacy.go": true, "internal/service/privacy.go": true}
	}
	for _, name := range []string{"CorrectPrivacyAccount", "PrivacyAccount", "PrivacyActivity", "PrivacySessions", "RestrictPrivacyPrincipal", "ErasePrivacyAccount"} {
		allowed[name] = map[string]bool{"internal/service/privacy.go": true, "internal/authz/privacy.go": true, "internal/store/authn/privacy.go": true}
	}
	for _, name := range []string{"NewHistoricalRecoverySQLite", "NewHistoricalRecoveryPG"} {
		allowed[name] = map[string]bool{"internal/store/tx/recovery.go": true}
	}
	root := filepath.Join("..", "..")
	for _, scope := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, scope), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, "internal/store/sqlitegen/") || strings.HasPrefix(rel, "internal/store/pggen/") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				sel, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				sites, guarded := allowed[sel.Sel.Name]
				if guarded && !sites[rel] {
					t.Errorf("privacy local authority door %s reached from %s", sel.Sel.Name, rel)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
