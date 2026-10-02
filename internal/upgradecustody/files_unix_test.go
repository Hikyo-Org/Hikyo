//go:build unix

package upgradecustody

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRecoversInterruptedInitialPublication(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	temporary := filepath.Join(directory, ".operator-test")
	want := []byte("encrypted-custody")
	if err := os.WriteFile(temporary, want, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(temporary, filepath.Join(directory, fileName)); err != nil {
		t.Fatal(err)
	}
	dir, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()

	got, err := read(dir, os.Geteuid())
	if err != nil {
		t.Fatalf("read interrupted publication: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("read = %q, want %q", got, want)
	}
	if _, err := os.Lstat(temporary); !os.IsNotExist(err) {
		t.Fatalf("temporary link survives recovery: %v", err)
	}
}

func TestReadRejectsUnrelatedHardLink(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(directory, fileName)
	if err := os.WriteFile(final, []byte("encrypted-custody"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(final, filepath.Join(directory, "unrelated")); err != nil {
		t.Fatal(err)
	}
	dir, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()

	if _, err := read(dir, os.Geteuid()); err == nil {
		t.Fatal("read accepted an unrelated hard link")
	}
}
