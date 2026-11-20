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
	return flags
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
	const root = "internal/lint/testdata/badstablecopies/root"
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
