package modroot_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/modroot"
)

func TestFindWalksUpToGoMod(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/m\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	got, err := modroot.Find()
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	// Resolve both sides: macOS temp dirs sit behind the /var -> /private/var symlink.
	if gotReal, wantReal := realPath(t, got), realPath(t, root); gotReal != wantReal {
		t.Fatalf("Find() = %q, want %q", gotReal, wantReal)
	}
}

func TestFindFailsWithoutGoMod(t *testing.T) {
	dir := t.TempDir()
	for d := realPath(t, dir); ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			t.Skipf("%s holds a go.mod above the temp dir; no module-free directory to test from", d)
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	t.Chdir(dir)
	if root, err := modroot.Find(); err == nil {
		t.Fatalf("Find() = %q, want an error outside any module", root)
	}
}

func realPath(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return real
}
