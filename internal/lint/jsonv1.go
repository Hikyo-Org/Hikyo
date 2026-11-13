package lint

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
)

// The module is migrating from encoding/json (v1) to encoding/json/v2 and
// encoding/json/jsontext. New code must not import v1. Directories that still
// import it are listed below; each migration removes its entries, and a stale
// entry is itself a finding, so the list only shrinks. Generated oapi-codegen
// output (api/apigen) is the one permanent exception: the generator has no v2
// mode, and v1 is v2 with v1-compatible options underneath.
var jsonV1Allowlist = map[string]bool{
	"api/apigen": true,
}

// CheckJSONv1Imports reports encoding/json imports outside the allowlist and
// allowlist entries that no longer import it. It reads every .go file,
// whatever its build tags, outside testdata, vendor and node_modules.
func CheckJSONv1Imports(repoRoot string) []string {
	var findings []string
	importing := map[string]bool{}
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p != "encoding/json" {
				continue
			}
			importing[dir] = true
			if !jsonV1Allowlist[dir] {
				findings = append(findings, fmt.Sprintf(
					"jsonv1: %s imports encoding/json; use encoding/json/v2 and encoding/json/jsontext", filepath.ToSlash(rel)))
			}
		}
		return nil
	})
	if err != nil {
		return append(findings, "jsonv1: "+err.Error())
	}
	var stale []string
	for dir := range jsonV1Allowlist {
		if !importing[dir] {
			stale = append(stale, dir)
		}
	}
	sort.Strings(stale)
	for _, dir := range stale {
		findings = append(findings, fmt.Sprintf(
			"jsonv1: allowlisted %s no longer imports encoding/json; remove it from jsonV1Allowlist", dir))
	}
	return findings
}
