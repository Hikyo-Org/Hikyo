package store

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// #1412: a staged plaintext file is removed as soon as it is archived.
func TestWriteFileMemberRemovesStagedFile(t *testing.T) {
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
}

func TestCreateStagedIsOwnerOnlyAndExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "copy-x")
	f, err := createStaged(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows reports synthetic permission bits.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("staged file mode = %v, want 0600", info.Mode().Perm())
	}
	if _, err := createStaged(path); err == nil {
		t.Fatal("createStaged replaced an existing staged file")
	}
}
