package lint

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// jsonV1Exception is the one directory allowed to import encoding/json:
// generated oapi-codegen output, whose generator has no v2 mode. Everything
// else uses encoding/json/v2 and encoding/json/jsontext.
const jsonV1Exception = "api/apigen"

const jsonV1Message = "jsonv1: %s imports encoding/json; use encoding/json/v2 and encoding/json/jsontext"

func staleJSONv1Exception() string {
	return fmt.Sprintf("jsonv1: %s no longer imports encoding/json; remove jsonV1Exception", jsonV1Exception)
}

// walkJSONv1 parses the imports of every .go file under repoRoot that skip does
// not exclude, and reports encoding/json imports outside the exception and
// whether the exception directory imports it.
func walkJSONv1(repoRoot string, skip func(path string) bool) (findings []string, exceptionImports bool) {
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
		if filepath.Ext(path) != ".go" || skip != nil && skip(path) {
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
				findings = append(findings, fmt.Sprintf(jsonV1Message, filepath.ToSlash(rel)))
			}
		}
		return nil
	})
	if err != nil {
		findings = append(findings, "jsonv1: "+err.Error())
	}
	return findings, exceptionImports
}

// JSONv1 is the per-package jsonv1 analyzer: every file of an
// analyzed package, test variants included, is judged by its imports, and each
// file is reported as a gofile summary so the driver can tell which files of
// the repository the vet run did not see. The package that is the exception
// reports an exception summary when it imports encoding/json, so the driver
// can find the exception stale.
var JSONv1 = newJSONv1()

func newJSONv1() *analysis.Analyzer {
	a := &analysis.Analyzer{Name: "jsonv1", Doc: "encoding/json (v1) is imported only by the generated API package"}
	root := ""
	exception := Module + "/" + jsonV1Exception
	a.Flags.StringVar(&root, "root", "", "repository root the reported paths are relative to (set by the driver)")
	a.Flags.StringVar(&exception, "exception", exception, "the one package allowed to import encoding/json")
	a.Run = func(pass *analysis.Pass) (any, error) {
		if root == "" {
			pass.Report(analysis.Diagnostic{Pos: packagePos(pass), Message: "inputs refused: the driver did not pass -root"})
			return nil, nil
		}
		isException := strings.TrimSuffix(pass.Pkg.Path(), "_test") == exception
		for _, file := range pass.Files {
			name := fileName(pass, file.Package)
			reportSummary(pass, summaryGoFile, name)
			for _, imp := range file.Imports {
				if p, _ := strconv.Unquote(imp.Path.Value); p != "encoding/json" {
					continue
				}
				if isException {
					reportSummary(pass, summaryException, pass.Pkg.Path())
					continue
				}
				rel, err := filepath.Rel(root, name)
				if err != nil {
					rel = name
				}
				pass.Report(analysis.Diagnostic{Pos: imp.Pos(), Message: fmt.Sprintf(jsonV1Message, filepath.ToSlash(rel))})
			}
		}
		return nil, nil
	}
	return a
}

// checkUnanalyzedGoFiles is what the vet driver cannot see: the Go files of the
// repository that no analyzed package of any context contains (a file excluded
// by build tags in every context, a lone program). analyzed holds the files
// the contexts' jsonv1 passes reported, and exceptionSeen whether any of them
// reported the exception importing encoding/json. It applies the jsonv1 rule
// and the synchronous_commit ban to those files and finds the exception stale.
// It runs in the test process on every run, so it is never a replayed result.
func checkUnanalyzedGoFiles(repoRoot string, analyzed map[string]bool, exceptionSeen bool) []string {
	skip := func(path string) bool { return analyzed[canonicalPath(path)] }
	findings, exceptionImports := walkJSONv1(repoRoot, skip)
	if !exceptionImports && !exceptionSeen {
		findings = append(findings, staleJSONv1Exception())
	}
	findings = append(findings, unanalyzedSyncCommit(repoRoot, skip)...)
	return findings
}

// unanalyzedSyncCommit applies the synchronous_commit ban to the unanalyzed Go
// files of internal/store. Unlike the jsonv1 walk it reads testdata, as the old
// harness did.
func unanalyzedSyncCommit(repoRoot string, skip func(path string) bool) []string {
	var findings []string
	root := filepath.Join(repoRoot, "internal", "store")
	paths, err := osSource{}.WalkFiles(root, []string{".go"})
	if err != nil {
		return []string{fmt.Sprintf("appendonly: walk %s: %v", root, err)}
	}
	for _, path := range paths {
		if skip(path) {
			continue
		}
		b, err := osSource{}.ReadFile(path)
		if err != nil {
			findings = append(findings, fmt.Sprintf("appendonly: %v", err))
			continue
		}
		if finding, ok := syncCommitFinding(repoRoot, path, b); ok {
			findings = append(findings, finding)
		}
	}
	return findings
}

// canonicalPath resolves symlinks so the paths vet reports and the paths a
// walk visits compare equal.
func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}
