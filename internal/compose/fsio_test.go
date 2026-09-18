package compose

import (
	"os"
	"path/filepath"
	"testing"
)

// atomicWriteEnv delegates publication to securefile but keeps its own
// contract: an existing file's mode is preserved (never widened), a new file
// is 0600, and no temporary file survives the rename.
func TestAtomicWriteEnvPreservesModeAndLeavesNoTemporary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := atomicWriteEnv(path, []byte("A=1\n")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("new .env mode = %v, err = %v, want 0600", fi.Mode().Perm(), err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteEnv(path, []byte("A=2\n")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Fatalf("rewritten .env mode = %04o, want the existing 0640 preserved", fi.Mode().Perm())
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "A=2\n" {
		t.Fatalf("contents = %q, %v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries after publication, want only .env: %v", len(entries), entries)
	}
}
