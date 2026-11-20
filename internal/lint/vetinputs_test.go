package lint

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// inputFlags are the -root and -inputs flags the driver passes an
// input-reading analyzer: the declared inputs under root, hashed.
func inputFlags(t *testing.T, analyzer, root string) []string {
	t.Helper()
	specs, ok, err := InputDeclaration(analyzer, root)
	if err != nil || !ok {
		t.Fatalf("inputs of %s under %s: declared=%t err=%v", analyzer, root, ok, err)
	}
	digest, err := InputDigest(root, specs)
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
	for _, name := range InputAnalyzers() {
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
	digest, err := InputDigest(root, specs)
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
		fresh, err := InputDigest(root, specs)
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
	if after, _ := InputDigest(root, specs); after != before {
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
// must cover exactly InputAnalyzers(), so a new input analyzer without a
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
	if want := InputAnalyzers(); !slices.Equal(covered, want) {
		t.Fatalf("fixtures with inputs cover %v, InputAnalyzers() is %v", covered, want)
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
		in      *Inputs
		root    string
		notExit bool
	}{
		"absent":    {absent, absentRoot, true},
		"symlinked": {link, linkRoot, false},
		"file":      {file, fileRoot, false},
	} {
		dir := filepath.Join(tc.root, "q")
		_, errDir := tc.in.ReadDir(dir)
		_, errWalk := tc.in.WalkFiles(dir, []string{".sql"})
		for _, err := range []error{errDir, errWalk} {
			if err == nil || errors.Is(err, fs.ErrNotExist) != tc.notExit {
				t.Errorf("%s directory: got %v, want a refusal (not-exist=%t)", name, err, tc.notExit)
			}
		}
		// The disk reader of the old harness refuses the same states.
		if _, err := (osSource{}).ReadDir(dir); err == nil {
			t.Errorf("%s directory: osSource.ReadDir returned no error", name)
		}
		if _, err := (osSource{}).WalkFiles(dir, []string{".sql"}); err == nil {
			t.Errorf("%s directory: osSource.WalkFiles returned no error", name)
		}
	}
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

// With every declared directory absent, each input analyzer's reads fail and
// its check reports a finding (or refuses), never a clean pass.
func TestInputAnalyzersReportAbsentDirectories(t *testing.T) {
	root := t.TempDir()
	snapshot := func(analyzer string) *Inputs {
		specs, ok, err := InputDeclaration(analyzer, root)
		if err != nil || !ok {
			t.Fatalf("%s: declared=%t err=%v", analyzer, ok, err)
		}
		in, err := SnapshotInputs(root, specs)
		if err != nil {
			t.Fatal(err)
		}
		return in
	}
	for name, findings := range map[string][]string{
		"appendonly": append(checkAuditQueries(snapshot("appendonly"), root),
			checkNoSyncCommitDowngrade(snapshot("appendonly"), root, []string{".sql"})...),
		"sqlpredicate":      checkSQLPredicates(snapshot("sqlpredicate"), root, true),
		"reencryptcoverage": checkReencryptCoverage(snapshot("reencryptcoverage"), filepath.Join(root, "internal", "store", "migrations", "sqlite"), reencryptCovered, reencryptExemptBlobs),
	} {
		if len(findings) == 0 {
			t.Errorf("%s: no finding with every declared directory absent", name)
		}
	}
	in := snapshot("containmentgates")
	if _, err := parseQueries(in, filepath.Join(root, "internal", "store", "queries", "sqlite")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("containmentgates: absent query directory = %v, want not-exist (reported as a finding)", err)
	}
	for _, name := range []string{"grantlock", "denialwriter"} {
		src := snapshot(name)
		var err error
		if name == "grantlock" {
			_, err = grantWriters(src, root)
		} else {
			_, _, err = mutatingQueries(src, root)
		}
		if err == nil {
			t.Errorf("%s: absent query directories produced no error", name)
		}
	}
	if findings, err := checkStableCopies(snapshot("stablecopies"), root, stableCopies); err != nil || len(findings) == 0 {
		t.Errorf("stablecopies: absent files gave findings=%v err=%v", findings, err)
	}
}
