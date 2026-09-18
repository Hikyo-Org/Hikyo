// Package securefile owns atomic publication of permission-sensitive files.
package securefile

import (
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/filedurability"
)

// WriteAtomic writes contents beside path, syncs and closes the file, then
// atomically replaces path and syncs the directory so the rename is durable.
// Callers own validation and contextual error text.
func WriteAtomic(path string, contents []byte, mode os.FileMode) error {
	return WriteAtomicPrepared(path, contents, mode, nil)
}

// WriteAtomicPrepared is WriteAtomic with a prepare hook that runs on the open
// temporary file after its mode is set and before any byte is written, so a
// caller can apply ownership or other attributes that must be in place before
// publication. A nil prepare is a plain WriteAtomic; a prepare error aborts the
// publication and removes the temporary file.
func WriteAtomicPrepared(path string, contents []byte, mode os.FileMode, prepare func(*os.File) error) (returnErr error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".secure-write-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if returnErr != nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if prepare != nil {
		if err := prepare(tmp); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if _, err := tmp.Write(contents); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return filedurability.SyncDirectory(filepath.Dir(path))
}
