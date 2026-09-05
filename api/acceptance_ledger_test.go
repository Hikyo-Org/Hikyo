package api_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// This is inventory closure, deliberately not a passing-release assertion.
// Owning suites and their engine/viewport results remain separate gates.
func TestReleaseAcceptanceLedgerReferences(t *testing.T) {
	root := ".."
	read := func(path string) string {
		raw, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	criterion := regexp.MustCompile(`(?m)^\| (C-APV|[CAKSMO][0-9]+|S[ACS][0-9]+) \|`)
	expected := map[string]bool{"GH-E2E": true, "GH-STUB": true, "GH-CONTRACT": true}
	for _, source := range []string{"mvp-boundary", "saml-sp", "scim-provisioning", "secret-scanning"} {
		for _, m := range criterion.FindAllStringSubmatch(read("docs/adr/"+source+".md"), -1) {
			expected[m[1]] = true
		}
	}
	document := read("docs/release/acceptance-1.0.md")
	row := regexp.MustCompile(`(?m)^\| (C-APV|[CAKSMO][0-9]+|S[ACS][0-9]+|GH-(?:E2E|STUB|CONTRACT)) \|`)
	seen := make(map[string]bool)
	for _, m := range row.FindAllStringSubmatch(document, -1) {
		if seen[m[1]] {
			t.Errorf("duplicate criterion %s", m[1])
		}
		if !expected[m[1]] {
			t.Errorf("criterion %s has no owning acceptance row", m[1])
		}
		seen[m[1]] = true
	}
	for id := range expected {
		if !seen[id] {
			t.Errorf("owning criterion %s missing from release ledger", id)
		}
	}
	// Parse the linked file, rather than finding a name in a comment/string.
	functions := make(map[string]map[string]bool)
	references := regexp.MustCompile(`\[(Test[A-Za-z0-9_]+)\]\(../../([^()]+\.go)\)`)
	matches := references.FindAllStringSubmatch(document, -1)
	if len(matches) == 0 {
		t.Fatal("release ledger names no executable Go tests")
	}
	for _, ref := range matches {
		name, path := ref[1], ref[2]
		if !strings.HasSuffix(path, "_test.go") {
			t.Errorf("%s is not a test source", path)
			continue
		}
		if functions[path] == nil {
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, path), nil, 0)
			if err != nil {
				t.Error(err)
				continue
			}
			functions[path] = make(map[string]bool)
			for _, declaration := range file.Decls {
				if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Recv == nil {
					functions[path][fn.Name.Name] = true
				}
			}
		}
		if !functions[path][name] {
			t.Errorf("ledger names %s but %s does not declare it", name, path)
		}
	}
}
