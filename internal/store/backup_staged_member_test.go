package store

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// #1412: a staged plaintext file must be owner-only and is removed as soon as
// it is archived.
func TestWriteFileMemberRequiresOwnerOnlyAndRemovesStagedFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	if err := os.WriteFile(good, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(&bytes.Buffer{})
	if err := writeFileMember(tw, "good", good); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(good); !os.IsNotExist(err) {
		t.Fatalf("archived staged file survived: %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	loose := filepath.Join(dir, "loose")
	if err := os.WriteFile(loose, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileMember(tw, "loose", loose); err == nil {
		t.Fatal("group/world-readable staged plaintext was archived")
	}
	if _, err := os.Lstat(loose); err != nil {
		t.Fatalf("refused file was removed without archiving: %v", err)
	}
}
