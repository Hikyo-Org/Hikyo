package securefile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicReplacesWithExactMode(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("new"), 0o400); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "new" {
		t.Fatalf("contents = %q, want new", contents)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o400 {
		t.Fatalf("mode = %04o, want 0400", got)
	}
}

func TestWriteAtomicPreparedRunsHookBeforeWriteAndAbortsOnError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "env")
	var sawMode os.FileMode
	var sawSize int64
	err := WriteAtomicPrepared(path, []byte("payload"), 0o640, func(f *os.File) error {
		info, err := f.Stat()
		if err != nil {
			return err
		}
		sawMode, sawSize = info.Mode().Perm(), info.Size()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if sawMode != 0o640 || sawSize != 0 {
		t.Fatalf("hook saw mode %04o size %d, want 0640 and an empty file", sawMode, sawSize)
	}
	if contents, err := os.ReadFile(path); err != nil || string(contents) != "payload" {
		t.Fatalf("contents = %q, %v", contents, err)
	}

	hookErr := errors.New("ownership refused")
	err = WriteAtomicPrepared(path, []byte("replacement"), 0o640, func(*os.File) error { return hookErr })
	if !errors.Is(err, hookErr) {
		t.Fatalf("err = %v, want the hook error", err)
	}
	if contents, err := os.ReadFile(path); err != nil || string(contents) != "payload" {
		t.Fatalf("destination changed after an aborted publication: %q, %v", contents, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "env" {
		t.Fatalf("temporary file left behind: %v", entries)
	}
}
