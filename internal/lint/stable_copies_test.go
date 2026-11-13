package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStableCopiesRepo(t *testing.T) {
	for _, f := range CheckStableCopies(repoRoot(t)) {
		t.Error(f)
	}
}

func TestStableCopiesCatchesDrift(t *testing.T) {
	root := t.TempDir()
	repo := repoRoot(t)
	for _, rel := range append([]string{"internal/jsonwire/jsonwire.go"}, stableCopies...) {
		src, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, src, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := CheckStableCopies(root); len(got) != 0 {
		t.Fatalf("pristine copies must agree: %v", got)
	}
	drifted := filepath.Join(root, "scripts/ci/pr-gate/main.go")
	src, err := os.ReadFile(drifted)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(drifted, []byte(strings.Replace(string(src), "\tjsontext.PreserveRawStrings(true),\n", "", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	assertFindings(t, CheckStableCopies(root), []string{"stablecopy: scripts/ci/pr-gate/main.go stableJSON options"})
}
