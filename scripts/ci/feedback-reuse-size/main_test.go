package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectBoundsActualContents(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "entry"), []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		bytes int64
		files int
		valid bool
	}{
		{"exact boundary", 5, 1, true},
		{"one byte over", 4, 1, false},
		{"one file over", 5, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bytes, files, err := inspect(root, tc.bytes, tc.files)
			if (err == nil) != tc.valid {
				t.Fatalf("inspect = %d bytes, %d files, %v", bytes, files, err)
			}
			if tc.valid && (bytes != 5 || files != 1) {
				t.Fatalf("wrong measurements: %d bytes, %d files", bytes, files)
			}
		})
	}
}

func TestInspectRefusesMissingEmptyAndLinkedRoots(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "missing"), t.TempDir(), file, link} {
		if _, _, err := inspect(path, 100, 100); err == nil {
			t.Fatalf("accepted invalid root %s", path)
		}
	}
	if err := os.Symlink(file, filepath.Join(root, "nested-link")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := inspect(root, 100, 100); err == nil {
		t.Fatal("accepted linked entry")
	}
}
