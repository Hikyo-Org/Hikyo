package lint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJSONv1ImportsRepo(t *testing.T) {
	for _, f := range CheckJSONv1Imports(repoRoot(t)) {
		t.Error(f)
	}
}

func TestJSONv1ImportsCatchesViolations(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("fresh/new.go", "//go:build ignore\n\npackage fresh\n\nimport stdjson \"encoding/json\"\n\nvar _ = stdjson.Marshal\n")
	write("fresh/v2.go", "package fresh\n\nimport \"encoding/json/v2\"\n\nvar _ = json.Marshal\n")
	write("fresh/testdata/fixture.go", "package fixture\n\nimport \"encoding/json\"\n")
	assertFindings(t, CheckJSONv1Imports(root), []string{
		"jsonv1: fresh/new.go imports encoding/json",
		"jsonv1: allowlisted api/apigen no longer imports encoding/json",
	})
	for _, f := range CheckJSONv1Imports(root) {
		if f == "jsonv1: fresh/v2.go imports encoding/json; use encoding/json/v2 and encoding/json/jsontext" ||
			f == "jsonv1: fresh/testdata/fixture.go imports encoding/json; use encoding/json/v2 and encoding/json/jsontext" {
			t.Errorf("unexpected finding: %s", f)
		}
	}
}
