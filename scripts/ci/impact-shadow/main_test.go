package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fixtureGraph(t *testing.T, packages ...packageInfo) graph {
	t.Helper()
	var data bytes.Buffer
	for _, pkg := range packages {
		if err := json.NewEncoder(&data).Encode(pkg); err != nil {
			t.Fatal(err)
		}
	}
	g, err := decodeGraph(&data, "/fixture")
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestSelectTransitiveAndBothTestImportKinds(t *testing.T) {
	g := fixtureGraph(t,
		packageInfo{ImportPath: "example/a", Dir: "/fixture/a", GoFiles: []string{"a.go"}},
		packageInfo{ImportPath: "example/b", Dir: "/fixture/b", Imports: []string{"example/a"}},
		packageInfo{ImportPath: "example/c", Dir: "/fixture/c", TestImports: []string{"example/b"}},
		packageInfo{ImportPath: "example/d", Dir: "/fixture/d", XTestImports: []string{"example/c"}},
		packageInfo{ImportPath: "example/unrelated", Dir: "/fixture/unrelated"},
	)
	changed, candidates, reasons := selectPackages([]string{"a/a.go"}, g, g)
	if !reflect.DeepEqual(changed, []string{"example/a"}) || !reflect.DeepEqual(candidates, []string{"example/a", "example/b", "example/c", "example/d"}) || len(reasons) != 0 {
		t.Fatalf("changed=%v candidates=%v reasons=%v", changed, candidates, reasons)
	}
}

func TestSelectDeletionAndMoveRetainOldGraphDependants(t *testing.T) {
	base := fixtureGraph(t,
		packageInfo{ImportPath: "example/old", Dir: "/fixture/old", GoFiles: []string{"old.go"}},
		packageInfo{ImportPath: "example/consumer", Dir: "/fixture/consumer", Imports: []string{"example/old"}},
	)
	head := fixtureGraph(t,
		packageInfo{ImportPath: "example/new", Dir: "/fixture/new", GoFiles: []string{"new.go"}},
		packageInfo{ImportPath: "example/consumer", Dir: "/fixture/consumer"},
	)
	changed, candidates, reasons := selectPackages([]string{"old/old.go", "new/new.go"}, base, head)
	if !reflect.DeepEqual(changed, []string{"example/new", "example/old"}) || !reflect.DeepEqual(candidates, []string{"example/consumer", "example/new"}) || len(reasons) != 0 {
		t.Fatalf("changed=%v candidates=%v reasons=%v", changed, candidates, reasons)
	}
}

func TestSelectEmbedAndTestdataOwnership(t *testing.T) {
	g := fixtureGraph(t,
		packageInfo{ImportPath: "example", Dir: "/fixture", GoFiles: []string{"main.go"}},
		packageInfo{ImportPath: "example/a", Dir: "/fixture/a", EmbedFiles: []string{"assets/data.json"}, TestEmbedFiles: []string{"fixtures/internal.txt"}, XTestEmbedFiles: []string{"fixtures/external.txt"}},
	)
	for _, file := range []string{"a/assets/data.json", "a/fixtures/internal.txt", "a/fixtures/external.txt", "a/testdata/nested/input.json", "testdata/input.json"} {
		t.Run(file, func(t *testing.T) {
			_, candidates, reasons := selectPackages([]string{file}, g, g)
			want := []string{"example/a"}
			if strings.HasPrefix(file, "testdata/") {
				want = []string{"example"}
			}
			if !reflect.DeepEqual(candidates, want) || len(reasons) != 0 {
				t.Fatalf("candidates=%v reasons=%v", candidates, reasons)
			}
		})
	}
}

func TestSelectUnknownManifestAndIgnoredFilesRequireFullFallback(t *testing.T) {
	g := fixtureGraph(t, packageInfo{ImportPath: "example/a", Dir: "/fixture/a", GoFiles: []string{"a.go"}, IgnoredGoFiles: []string{"windows.go"}})
	for _, file := range []string{"generator.yaml", "a/assets/unknown.txt", "go.mod", "nested/go.mod", "go.sum", "go.work", "go.work.sum", "a/windows.go"} {
		t.Run(file, func(t *testing.T) {
			_, _, reasons := selectPackages([]string{file}, g, g)
			if len(reasons) == 0 {
				t.Fatal("expected full fallback")
			}
		})
	}
}

func TestDecodeRejectsIncompleteGraphsAndChecksSyntheticTestErrors(t *testing.T) {
	for name, data := range map[string]string{
		"empty":            "",
		"malformed":        "{",
		"missing dir":      `{"ImportPath":"example"}`,
		"outside root":     `{"ImportPath":"example","Dir":"/elsewhere"}`,
		"package error":    `{"ImportPath":"example","Dir":"/fixture","Error":{"Err":"broken"}}`,
		"dependency error": `{"ImportPath":"example","Dir":"/fixture","DepsErrors":[{"Err":"broken"}]}`,
		"incomplete":       `{"ImportPath":"example","Dir":"/fixture","Incomplete":true}`,
		"test error":       `{"ImportPath":"example","Dir":"/fixture"}` + "\n" + `{"ImportPath":"example [example.test]","ForTest":"example","Dir":"/fixture","Incomplete":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeGraph(strings.NewReader(data), "/fixture"); err == nil {
				t.Fatal("expected incomplete graph")
			}
		})
	}
}

// This fixture uses an isolated Git configuration, identity and object database;
// it never changes signing policy or commits in the contributor's repository.
type gitFixture struct{ root string }

func newGitFixture(t *testing.T) gitFixture {
	t.Helper()
	f := gitFixture{root: t.TempDir()}
	f.git(t, "init", "--quiet")
	f.write(t, "go.mod", "module example.invalid/shadow\n\ngo 1.23\n")
	f.write(t, "a/a.go", "package a\nfunc A() int { return 1 }\n")
	f.write(t, "b/b.go", "package b\nimport \"example.invalid/shadow/a\"\nfunc B() int { return a.A() }\n")
	f.write(t, "c/c.go", "package c\n")
	f.write(t, "c/c_test.go", "package c_test\nimport (\"testing\"; \"example.invalid/shadow/b\")\nfunc TestC(t *testing.T) { _ = b.B() }\n")
	f.write(t, "unrelated/other.go", "package unrelated\n")
	return f
}

func (f gitFixture) git(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "init.templateDir=", "-C", f.root}, args...)...)
	cmd.Env = cleanEnv(map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_SYSTEM": os.DevNull,
		"GIT_AUTHOR_NAME": "Shadow Fixture", "GIT_AUTHOR_EMAIL": "fixture@example.invalid",
		"GIT_COMMITTER_NAME": "Shadow Fixture", "GIT_COMMITTER_EMAIL": "fixture@example.invalid",
	})
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git fixture: %v: %s", err, data)
	}
	return strings.TrimSpace(string(data))
}

func (f gitFixture) write(t *testing.T, name, value string) {
	t.Helper()
	target := filepath.Join(f.root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f gitFixture) commit(t *testing.T) string {
	t.Helper()
	f.git(t, "add", ".")
	f.git(t, "commit", "--quiet", "-m", "fixture")
	return f.git(t, "rev-parse", "HEAD")
}

func TestAnalyzeRealCommitsAndDoesNotExecuteTests(t *testing.T) {
	f := newGitFixture(t)
	// A test that would fail if executed verifies the analyzer only lists it.
	f.write(t, "a/a_test.go", "package a\nimport \"testing\"\nfunc TestNeverRun(t *testing.T) { panic(\"must not execute\") }\n")
	base := f.commit(t)
	f.write(t, "a/a.go", "package a\nfunc A() int { return 2 }\n")
	head := f.commit(t)
	r := analyze(context.Background(), f.root, base, head)
	want := []string{"example.invalid/shadow/a", "example.invalid/shadow/b", "example.invalid/shadow/c"}
	if r.Mode != "shadow" || r.Authoritative || r.Recommendation != "packages" || !reflect.DeepEqual(r.CandidatePackages, want) {
		t.Fatalf("unexpected report: %+v", r)
	}
	// Committed snapshots are used, even with an unrelated dirty worktree.
	f.write(t, "a/a.go", "broken uncommitted content")
	var output bytes.Buffer
	markdownFile := filepath.Join(t.TempDir(), "summary.md")
	if err := run([]string{"--repo", f.root, "--base", base, "--head", head, "--markdown", markdownFile}, &output); err != nil {
		t.Fatal(err)
	}
	var actual report
	if err := json.Unmarshal(output.Bytes(), &actual); err != nil || !reflect.DeepEqual(actual, r) {
		t.Fatalf("CLI differs from analysis: %v %s", err, output.Bytes())
	}
	data, err := os.ReadFile(markdownFile)
	if err != nil || !bytes.Contains(data, []byte("All backend checks still run")) {
		t.Fatalf("missing advisory summary: %v", err)
	}
}

func TestAnalyzeRealRenamePreservesBothPaths(t *testing.T) {
	f := newGitFixture(t)
	base := f.commit(t)
	f.git(t, "mv", "a/a.go", "a/renamed.go")
	head := f.commit(t)
	r := analyze(context.Background(), f.root, base, head)
	if r.Recommendation != "packages" || !reflect.DeepEqual(r.ChangedFiles, []string{"a/a.go", "a/renamed.go"}) || len(r.CandidatePackages) != 3 {
		t.Fatalf("rename lost an endpoint: %+v", r)
	}
}

func TestAnalyzeRealEmbeddedDataAndTestdata(t *testing.T) {
	for _, file := range []string{"a/assets/message.txt", "a/testdata/input.txt"} {
		t.Run(file, func(t *testing.T) {
			f := newGitFixture(t)
			f.write(t, "a/a.go", "package a\nimport _ \"embed\"\n//go:embed assets/message.txt\nvar Message string\nfunc A() int { return 1 }\n")
			f.write(t, "a/assets/message.txt", "before")
			f.write(t, "a/testdata/input.txt", "before")
			base := f.commit(t)
			f.write(t, file, "after")
			head := f.commit(t)
			r := analyze(context.Background(), f.root, base, head)
			if r.Recommendation != "packages" || len(r.CandidatePackages) != 3 {
				t.Fatalf("data ownership lost: %+v", r)
			}
		})
	}
}

func TestChangedSymlinkRequiresFullFallback(t *testing.T) {
	f := newGitFixture(t)
	base := f.commit(t)
	if err := os.Symlink("a/a.go", filepath.Join(f.root, "link.go")); err != nil {
		t.Fatal(err)
	}
	head := f.commit(t)
	r := analyze(context.Background(), f.root, base, head)
	if r.Recommendation != "full" || !strings.Contains(strings.Join(r.FallbackReasons, "\n"), "changed symlink: link.go") {
		t.Fatalf("symlink fallback missing: %+v", r)
	}
}

func TestAnalyzeFullFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		file   string
		value  string
		reason string
	}{
		{"generated", "a/a.go", "// Code generated by fixture. DO NOT EDIT.\npackage a\nfunc A() int { return 3 }\n", "generated code"},
		{"generator", "a/a.go", "//go:generate sh -c 'exit 42'\npackage a\nfunc A() int { return 3 }\n", "generation directive"},
		{"unknown asset", "a/assets/input.yaml", "value: 1\n", "unknown file ownership"},
		{"missing import", "a/a.go", "package a\nimport _ \"example.invalid/missing\"\n", "graph incomplete"},
		{"missing test import", "a/a_test.go", "package a\nimport _ \"example.invalid/missing\"\n", "graph incomplete"},
		{"missing embed", "a/a.go", "package a\nimport _ \"embed\"\n//go:embed missing.txt\nvar Data string\n", "graph incomplete"},
		{"ignored tag", "a/other.go", "//go:build shadow_nondefault_tag\n\npackage a\n", "excluded from host"},
		{"manifest", "go.mod", "module example.invalid/shadow\n\ngo 1.24\n", "manifest changed"},
		{"export ignore", ".gitattributes", "a/a.go export-ignore\n", "snapshot unavailable"},
		{"export substitute", ".gitattributes", "a/a.go export-subst\n", "snapshot unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newGitFixture(t)
			base := f.commit(t)
			f.write(t, tc.file, tc.value)
			head := f.commit(t)
			r := analyze(context.Background(), f.root, base, head)
			if r.Recommendation != "full" || r.Authoritative || !strings.Contains(strings.Join(r.FallbackReasons, "\n"), tc.reason) {
				t.Fatalf("missing fallback %q: %+v", tc.reason, r)
			}
		})
	}
}

func TestMissingCommitAndTimeoutStillEmitFullReport(t *testing.T) {
	f := newGitFixture(t)
	base := f.commit(t)
	for _, args := range [][]string{
		{"--repo", f.root, "--base", base, "--head", strings.Repeat("f", 40)},
		{"--repo", f.root, "--base", base, "--head", f.git(t, "rev-parse", "HEAD^{tree}")},
		{"--repo", f.root, "--base", base, "--head", base, "--timeout", "1ns"},
	} {
		var output bytes.Buffer
		if err := run(args, &output); err != nil {
			t.Fatal(err)
		}
		var r report
		if err := json.Unmarshal(output.Bytes(), &r); err != nil || r.Recommendation != "full" || len(r.FallbackReasons) == 0 {
			t.Fatalf("missing failure evidence: %v %s", err, output.Bytes())
		}
	}
}

func TestInvocationRejectsSymbolicRevisionsAndUnwritableOutput(t *testing.T) {
	for _, args := range [][]string{
		{"--base", "main", "--head", strings.Repeat("f", 40)},
		{"--base", strings.Repeat("a", 40), "--head", "--help"},
		{"--base", strings.Repeat("a", 40), "--head", strings.Repeat("f", 40), "--timeout", "0s"},
		{"--base", strings.Repeat("a", 40), "--head", strings.Repeat("f", 40), "--json", t.TempDir()},
	} {
		if err := run(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("expected invocation error: %v", args)
		}
	}
}

func TestArchiveExtractionRejectsTraversalAndOmitsSymlinks(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "nested/../../escape", "a/../b", `a\b`} {
		t.Run(name, func(t *testing.T) {
			var data bytes.Buffer
			writer := tar.NewWriter(&data)
			if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := extractSnapshot(&data, t.TempDir()); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
	var data bytes.Buffer
	writer := tar.NewWriter(&data)
	if err := writer.WriteHeader(&tar.Header{Name: "outside", Linkname: "/tmp", Typeflag: tar.TypeSymlink}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	links, err := extractSnapshot(&data, destination)
	if err != nil || !links["outside"] {
		t.Fatalf("symlink not recorded: %v %v", links, err)
	}
	if _, err := os.Lstat(filepath.Join(destination, "outside")); !os.IsNotExist(err) {
		t.Fatal("symlink materialized")
	}
}

func TestMarkdownEscapesUntrustedPaths(t *testing.T) {
	text := markdown(report{FallbackReasons: []string{"unknown: [click](https://bad.invalid)\n<script>alert(1)</script>`"}})
	if strings.Contains(text, "<script>") || strings.Contains(text, "[click]") || strings.Contains(text, "\n<script>") {
		t.Fatalf("unsafe Markdown: %s", text)
	}
}

func TestAnalysisHonorsContextDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	r := analyze(ctx, t.TempDir(), strings.Repeat("a", 40), strings.Repeat("b", 40))
	if r.Recommendation != "full" || len(r.FallbackReasons) == 0 {
		t.Fatalf("deadline lost: %+v", r)
	}
}
