package lint

import (
	"errors"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// inputFlags are the -root and -inputs flags the driver passes an
// input-reading analyzer: the declared inputs under root, hashed.
func inputFlags(t *testing.T, analyzer, root string) []string {
	t.Helper()
	specs, ok, err := inputDeclaration(analyzer, root)
	if err != nil || !ok {
		t.Fatalf("inputs of %s under %s: declared=%t err=%v", analyzer, root, ok, err)
	}
	digest, err := inputDigest(root, specs)
	if err != nil {
		t.Fatal(err)
	}
	return []string{"-" + analyzer + ".root=" + root, "-" + analyzer + ".inputs=" + digest}
}

// repoInputFlags are the input flags of every input-reading analyzer for the
// real repository.
func repoInputFlags(t *testing.T) []string {
	t.Helper()
	var flags []string
	for _, name := range inputAnalyzers() {
		flags = append(flags, inputFlags(t, name, moduleRoot())...)
	}
	// jsonv1 reads no declared input; it needs the root its paths are relative to.
	return append(flags, "-jsonv1.root="+moduleRoot())
}

// findingMessages strips the position and analyzer prefix of the analyzer's
// findings, for old entry points that have no positions.
func findingMessages(findings []string, analyzer string) []string {
	var out []string
	prefix := ": " + analyzer + ": "
	for _, f := range findings {
		if cut := strings.Index(f, prefix); cut >= 0 {
			out = append(out, f[cut+len(prefix):])
		}
	}
	return out
}

func TestInputsRefuseUndeclaredPathAndStaleDigest(t *testing.T) {
	root := t.TempDir()
	write := func(rel, text string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := mkdirWrite(path, text); err != nil {
			t.Fatal(err)
		}
	}
	write("q/a.sql", "select 1;")
	write("q/skip.txt", "ignored")
	write("f.json", "{}")
	specs := []InputSpec{{Dir: "q", Exts: []string{".sql"}}, {File: "f.json"}, {File: "absent.json"}}
	digest, err := inputDigest(root, specs)
	if err != nil {
		t.Fatal(err)
	}
	in, err := OpenInputs(root, digest, specs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.ReadFile(filepath.Join(root, "q", "a.sql")); err != nil {
		t.Errorf("declared file refused: %v", err)
	}
	for _, path := range []string{filepath.Join(root, "q", "skip.txt"), filepath.Join(root, "other.sql")} {
		if _, err := in.ReadFile(path); !errors.Is(err, ErrUndeclaredInput) {
			t.Errorf("ReadFile(%s) = %v, want ErrUndeclaredInput", path, err)
		}
	}
	if _, err := in.ReadDir(filepath.Join(root, "elsewhere")); !errors.Is(err, ErrUndeclaredInput) {
		t.Errorf("ReadDir of an undeclared directory = %v", err)
	}
	if _, err := in.ReadFile(filepath.Join(root, "absent.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a declared but absent file must read as not-exist, got %v", err)
	}
	// Each of these changes what the analyzer would read, so each changes the digest.
	for name, mutate := range map[string]func(){
		"edited file":   func() { write("q/a.sql", "select 2;") },
		"added file":    func() { write("q/b.sql", "select 3;") },
		"created input": func() { write("absent.json", "{}") },
	} {
		mutate()
		if _, err := OpenInputs(root, digest, specs); !errors.Is(err, ErrInputDigest) {
			t.Errorf("%s: OpenInputs = %v, want ErrInputDigest", name, err)
		}
		fresh, err := inputDigest(root, specs)
		if err != nil {
			t.Fatal(err)
		}
		digest = fresh
	}
	if _, err := OpenInputs(root, "", specs); !errors.Is(err, ErrInputDigest) {
		t.Errorf("an empty digest must be refused, got %v", err)
	}
	// An undeclared extension change does not move the digest: the file is not an input.
	before := digest
	write("q/skip.txt", "changed")
	if after, _ := inputDigest(root, specs); after != before {
		t.Error("a file outside the declaration changed the digest")
	}
	t.Run("directories", checkRefusedDirectories)
}

func mkdirWrite(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o600)
}

// Each refusal path of the input helper, through the vet driver: a digest that
// does not match the declared inputs, absent input flags, and a read of a path
// the analyzer did not declare (reachable on purpose, through stablecopies'
// -copies flag, so the fail-closed rule is exercised end to end).
func TestVetDriverRefusesBadInputs(t *testing.T) {
	bin := buildVetTool(t)
	const root = "internal/lint/testdata/badinputsrefusal"
	good := inputFlags(t, "stablecopies", filepath.Join(moduleRoot(), filepath.FromSlash(root)))
	anchor := "-stablecopies.anchor=" + fixtureImportRoot + "badinputsrefusal"
	cases := []struct {
		name  string
		flags []string
		want  string
	}{
		{"stale digest", []string{good[0], "-stablecopies.inputs=0000"}, "input digest mismatch"},
		{"missing flags", nil, "the driver did not pass -root and -inputs"},
		{"undeclared path", append(slices.Clone(good), "-stablecopies.copies=scripts/ci/undeclared/main.go"), "undeclared input"},
		// A root that is not the anchor's, with the digest of its own inputs: the
		// digest matches, so only the root/anchor tie refuses it.
		{"foreign root with a matching digest", inputFlags(t, "stablecopies", filepath.Join(moduleRoot(), "internal/lint/testdata/badstablecopies")), "-root does not contain the anchor package"},
	}
	fixture := vetFixture{name: "refusal", analyzer: "stablecopies", patterns: []string{fixtureRoot + "badinputsrefusal"}}
	ctx := Contexts[0]
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flags := append([]string{"-stablecopies", anchor}, tc.flags...)
			out := runVet(t, bin, ctx, flags, fixture.patterns)
			checkWants(t, fixture, out.Findings)
			if !strings.Contains(strings.Join(out.Findings, "\n"), "inputs refused: "+tc.want) {
				t.Errorf("findings lack the %q refusal:\n%s", tc.want, strings.Join(out.Findings, "\n"))
			}
			if len(out.Targets["stablecopies"]) != 1 {
				t.Errorf("the anchor must still report its target, got %v", out.Targets)
			}
		})
	}
}

// analyzedGoFiles are the Go files (canonical paths) the run's jsonv1 passes
// reported, and whether the exception package reported importing
// encoding/json.
func analyzedGoFiles(out *vetOutcome) (files map[string]bool, exception bool) {
	files = map[string]bool{}
	for _, s := range out.Summaries[JSONv1.Name] {
		switch s.Kind {
		case summaryGoFile:
			files[canonicalPath(s.Subject)] = true
		case summaryException:
			exception = true
		}
	}
	return files, exception
}

// Every input-reading analyzer refuses a digest that does not match its
// declared inputs, on its own fixture. The fixtures that declare an inputsRoot
// must cover exactly inputAnalyzers(), so a new input analyzer without a
// fixture fails here.
func TestVetDriverEveryInputAnalyzerRefusesStaleDigest(t *testing.T) {
	bin := buildVetTool(t)
	var fixtures []vetFixture
	var covered []string
	for _, f := range vetFixtures {
		if f.inputsRoot != "" {
			fixtures = append(fixtures, f)
			covered = append(covered, f.analyzer)
		}
	}
	slices.Sort(covered)
	if want := inputAnalyzers(); !slices.Equal(covered, want) {
		t.Fatalf("fixtures with inputs cover %v, inputAnalyzers() is %v", covered, want)
	}
	flags, patterns := fixtureFlagsAndPatterns(t, fixtures)
	for i, flag := range flags {
		if strings.Contains(flag, ".inputs=") {
			flags[i] = flag[:strings.Index(flag, ".inputs=")] + ".inputs=stale"
		}
	}
	out := runVet(t, bin, Contexts[0], flags, patterns)
	for _, f := range fixtures {
		found := false
		for _, finding := range findingMessages(out.Findings, f.analyzer) {
			found = found || strings.HasPrefix(finding, "inputs refused: input digest mismatch")
		}
		if !found {
			t.Errorf("%s did not refuse a stale digest", f.analyzer)
		}
		for _, finding := range findingMessages(out.Findings, f.analyzer) {
			// appendonly's per-package scan reads the package's own Go files, not
			// declared inputs, so it still reports under a stale digest.
			perPackage := f.analyzer == "appendonly" && strings.HasPrefix(finding, "appendonly: sync.go issues SET synchronous_commit")
			if !strings.HasPrefix(finding, "inputs refused:") && !perPackage {
				t.Errorf("%s analyzed despite a stale digest: %s", f.analyzer, finding)
			}
		}
	}
}

// A declared directory that is absent, a symlink or not a directory is a
// refusal, never an empty listing, and each state hashes differently.
func checkRefusedDirectories(t *testing.T) {
	specs := []InputSpec{{Dir: "q", Exts: []string{".sql"}, Recursive: true}}
	rootWith := func(setup func(root string)) (string, *Inputs) {
		root := t.TempDir()
		setup(root)
		in, err := SnapshotInputs(root, specs)
		if err != nil {
			t.Fatal(err)
		}
		return root, in
	}
	absentRoot, absent := rootWith(func(string) {})
	emptyRoot, empty := rootWith(func(root string) {
		if err := os.Mkdir(filepath.Join(root, "q"), 0o755); err != nil {
			t.Fatal(err)
		}
	})
	linkRoot, link := rootWith(func(root string) {
		if err := mkdirWrite(filepath.Join(root, "real", "a.sql"), "select 1;"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "q")); err != nil {
			t.Fatal(err)
		}
	})
	fileRoot, file := rootWith(func(root string) {
		if err := mkdirWrite(filepath.Join(root, "q"), "not a directory"); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := empty.ReadDir(filepath.Join(emptyRoot, "q")); err != nil {
		t.Errorf("an empty directory must read as empty, got %v", err)
	}
	for name, tc := range map[string]struct {
		in       *Inputs
		root     string
		notExist bool
	}{
		"absent":    {absent, absentRoot, true},
		"symlinked": {link, linkRoot, false},
		"file":      {file, fileRoot, false},
	} {
		dir := filepath.Join(tc.root, "q")
		_, errDir := tc.in.ReadDir(dir)
		_, errWalk := tc.in.WalkFiles(dir, []string{".sql"})
		for _, err := range []error{errDir, errWalk} {
			if err == nil || errors.Is(err, fs.ErrNotExist) != tc.notExist {
				t.Errorf("%s directory: got %v, want a refusal (not-exist=%t)", name, err, tc.notExist)
			}
		}
		// The osSource disk reader refuses the same states.
		if _, err := (osSource{}).ReadDir(dir); err == nil {
			t.Errorf("%s directory: osSource.ReadDir returned no error", name)
		}
		if _, err := (osSource{}).WalkFiles(dir, []string{".sql"}); err == nil {
			t.Errorf("%s directory: osSource.WalkFiles returned no error", name)
		}
	}
	t.Run("symlinked parent", checkSymlinkedParent)
	digests := map[string]string{}
	for name, in := range map[string]*Inputs{"absent": absent, "empty": empty, "irregular": link} {
		if other, dup := digests[in.digest]; dup {
			t.Errorf("%s and %s directories hash the same", name, other)
		}
		digests[in.digest] = name
	}
	if _, err := SnapshotInputs(emptyRoot, append(specs, specs...)); err == nil {
		t.Error("two specs naming one directory must be refused")
	}
}

// A symlink in a parent directory of a declared path, pointing outside the
// root, is never followed: the directory and the file read as irregular and are
// refused, and the outside content does not reach the snapshot.
func checkSymlinkedParent(t *testing.T) {
	outside := t.TempDir()
	if err := mkdirWrite(filepath.Join(outside, "q", "a.sql"), "select 1;"); err != nil {
		t.Fatal(err)
	}
	if err := mkdirWrite(filepath.Join(outside, "f.json"), "{}"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "a")); err != nil {
		t.Fatal(err)
	}
	in, err := SnapshotInputs(root, []InputSpec{{Dir: "a/q", Exts: []string{".sql"}, Recursive: true}, {File: "a/f.json"}})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "a", "q")
	_, errDir := in.ReadDir(dir)
	_, errWalk := in.WalkFiles(dir, []string{".sql"})
	_, errFile := in.ReadFile(filepath.Join(root, "a", "f.json"))
	for name, err := range map[string]error{"ReadDir": errDir, "WalkFiles": errWalk, "ReadFile": errFile} {
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s through a symlinked parent: got %v, want a refusal", name, err)
		}
	}
	empty, err := SnapshotInputs(t.TempDir(), []InputSpec{{Dir: "a/q", Exts: []string{".sql"}, Recursive: true}, {File: "a/f.json"}})
	if err != nil {
		t.Fatal(err)
	}
	if in.digest == empty.digest {
		t.Error("a symlinked parent hashes like an absent one")
	}

	// A symlink that stays inside the root is refused as well.
	inner := t.TempDir()
	if err := mkdirWrite(filepath.Join(inner, "real", "q", "a.sql"), "select 1;"); err != nil {
		t.Fatal(err)
	}
	// Relative target: os.Root would refuse an absolute one as an escape anyway.
	if err := os.Symlink("real", filepath.Join(inner, "b")); err != nil {
		t.Fatal(err)
	}
	if in, err := SnapshotInputs(inner, []InputSpec{{Dir: "b/q", Exts: []string{".sql"}}}); err != nil {
		t.Fatal(err)
	} else if _, err := in.ReadDir(filepath.Join(inner, "b", "q")); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReadDir through an in-root symlinked parent: got %v, want a refusal", err)
	}

	// A symlinked subdirectory inside a recursive declared directory is refused,
	// not skipped unhashed.
	if err := os.Symlink(filepath.Join(inner, "real", "q"), filepath.Join(inner, "real", "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := SnapshotInputs(inner, []InputSpec{{Dir: "real", Exts: []string{".sql"}, Recursive: true}}); err == nil {
		t.Error("a symlinked subdirectory in a declared directory was accepted")
	}
}

// fakePass runs an analyzer on its anchor package with no files: enough for the
// file-input part, which reads the declared inputs and reports at the package.
func fakePass(a *analysis.Analyzer) (*analysis.Pass, *[]string) {
	var messages []string
	anchor := a.Flags.Lookup("anchor").Value.String()
	return &analysis.Pass{
		Analyzer: a, Fset: token.NewFileSet(), Pkg: types.NewPackage(anchor, "anchor"),
		Report: func(d analysis.Diagnostic) {
			if d.Category != SummaryCategory {
				messages = append(messages, d.Message)
			}
		},
	}, &messages
}

// With every declared directory absent, each input analyzer reports a finding,
// never a clean pass or a refusal, and the per-package helpers fail whichever
// way they read: through a verified snapshot or the osSource disk reader.
func TestInputAnalyzersReportAbsentDirectories(t *testing.T) {
	root := t.TempDir()
	builders := map[string]func() *analysis.Analyzer{
		"appendonly":        newAuditAppendOnly,
		"containmentgates":  newContainmentGates,
		"reencryptcoverage": newReencryptCoverage,
		"sqlpredicate":      newSQLPredicates,
		"stablecopies":      newStableCopies,
	}
	run := func(a *analysis.Analyzer) []string {
		pass, messages := fakePass(a)
		if _, err := a.Run(pass); err != nil {
			t.Fatal(err)
		}
		return *messages
	}
	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			vet := build()
			for _, flag := range inputFlags(t, name, root) {
				key, value, _ := strings.Cut(strings.TrimPrefix(flag, "-"+name+"."), "=")
				if err := vet.Flags.Set(key, value); err != nil {
					t.Fatal(err)
				}
			}
			findings := run(vet)
			if len(findings) == 0 {
				t.Error("no finding with every declared directory absent")
			}
			for _, f := range findings {
				if strings.HasPrefix(f, "inputs refused:") {
					t.Errorf("refused instead of reporting the absent directories: %s", f)
				}
			}
		})
	}
	// The per-package analyzers have no anchor: their helpers fail on the absent
	// directories, through both readers.
	for source, src := range map[string]func(string) source{
		"vet":  func(name string) source { return snapshotAbsent(t, name, root) },
		"disk": func(string) source { return osSource{} },
	} {
		if _, err := grantWriters(src("grantlock"), root); err == nil {
			t.Errorf("grantlock (%s): absent query directories produced no error", source)
		}
		if _, _, err := mutatingQueries(src("denialwriter"), root); err == nil {
			t.Errorf("denialwriter (%s): absent query directories produced no error", source)
		}
	}
	in := snapshotAbsent(t, "containmentgates", root)
	for source, src := range map[string]source{"vet": in, "disk": osSource{}} {
		if _, err := parseQueries(src, filepath.Join(root, "internal", "store", "queries", "sqlite")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("containmentgates (%s): absent query directory = %v, want not-exist (reported as a finding)", source, err)
		}
	}

	// RawSQL reads the pins and the query directories, and judges a sqlc file
	// only as generated when its query file exists. With the pins present and
	// both query directories absent, a generated file is production code: it
	// fails closed (its SQL execution is judged), never open as "generated".
	rawRoot := t.TempDir()
	if err := mkdirWrite(filepath.Join(rawRoot, filepath.FromSlash(reviewedPinsPath)), `{"protocols":{}}`); err != nil {
		t.Fatal(err)
	}
	generated, err := parser.ParseFile(token.NewFileSet(), "a.sql.go", "// Code generated by sqlc. DO NOT EDIT.\n\npackage sqlitegen\n", parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	rawIn := snapshotAbsent(t, RawSQL.Name, rawRoot)
	if _, err := rawSQLConfig(rawIn, rawRoot, "default", "linux"); err != nil {
		t.Errorf("rawsql: the pins with absent query directories were refused: %v", err)
	}
	path := filepath.Join(rawRoot, "internal", "store", "sqlitegen", "a.sql.go")
	for source, src := range map[string]source{"vet": rawIn, "disk": osSource{}} {
		if got, err := generatedSQLSourceIn(src, rawRoot, path, generated); err != nil || got {
			t.Errorf("rawsql (%s): generated file with absent query directories = (%t, %v), want judged as production (false, nil)", source, got, err)
		}
	}
}

func snapshotAbsent(t *testing.T, analyzer, root string) *Inputs {
	t.Helper()
	specs, ok, err := inputDeclaration(analyzer, root)
	if err != nil || !ok {
		t.Fatalf("%s: declared=%t err=%v", analyzer, ok, err)
	}
	in, err := SnapshotInputs(root, specs)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// An input edited while vet read it caches a digest-mismatch refusal under the
// digest of the restored input; runRepoVet must not replay it.
func TestRunRepoVetHealsACachedStaleRefusal(t *testing.T) {
	bin := buildVetTool(t)
	// A fresh copy of the stablecopies fixture gets cache keys of its own.
	root, err := os.MkdirTemp(filepath.Join(moduleRoot(), "internal/lint/testdata"), "refusalcache-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.CopyFS(root, os.DirFS(filepath.Join(moduleRoot(), "internal/lint/testdata/badstablecopies"))); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(moduleRoot(), root)
	if err != nil {
		t.Fatal(err)
	}
	pkg := Module + "/" + filepath.ToSlash(rel)
	flags := func() []string {
		return append([]string{"-stablecopies", "-stablecopies.anchor=" + pkg}, inputFlags(t, "stablecopies", root)...)
	}
	patterns := []string{"./" + filepath.ToSlash(rel)}
	input := filepath.Join(root, "internal/jsonwire/jsonwire.go")
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	passed := flags()
	if err := os.WriteFile(input, append(slices.Clone(original), "// edited\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := Contexts[0]
	if !hasDigestRefusal(runVet(t, bin, ctx, passed, patterns).Findings) {
		t.Fatal("an edited input was not refused")
	}
	if err := os.WriteFile(input, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasDigestRefusal(runVet(t, bin, ctx, passed, patterns).Findings) {
		t.Skip("go vet no longer replays the cached refusal; runRepoVet's retry is unneeded")
	}
	if out := runRepoVet(t, bin, ctx, flags, patterns); hasDigestRefusal(out.Findings) {
		t.Fatalf("runRepoVet replayed the stale refusal: %v", out.Findings)
	}
	if hasDigestRefusal(runVet(t, bin, ctx, passed, patterns).Findings) {
		t.Fatal("the rerun did not replace the cached refusal")
	}
}
