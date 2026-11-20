package lint

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
)

// jsonV1Exception is the one directory allowed to import encoding/json:
// generated oapi-codegen output, whose generator has no v2 mode. Everything
// else uses encoding/json/v2 and encoding/json/jsontext.
const jsonV1Exception = "api/apigen"

// CheckJSONv1Imports reports encoding/json imports outside jsonV1Exception
// and a stale exception when that directory stops importing it. It reads every .go file,
// whatever its build tags, outside testdata, vendor and node_modules.
func CheckJSONv1Imports(repoRoot string) []string {
	var findings []string
	exceptionImports := false
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
			if dir == jsonV1Exception {
				exceptionImports = true
			} else {
				findings = append(findings, fmt.Sprintf(
					"jsonv1: %s imports encoding/json; use encoding/json/v2 and encoding/json/jsontext", filepath.ToSlash(rel)))
			}
		}
		return nil
	})
	if err != nil {
		return append(findings, "jsonv1: "+err.Error())
	}
	if !exceptionImports {
		findings = append(findings, fmt.Sprintf(
			"jsonv1: %s no longer imports encoding/json; remove jsonV1Exception", jsonV1Exception))
	}
	return findings
}
