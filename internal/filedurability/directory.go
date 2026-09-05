// Package filedurability synchronizes published file directory entries.
package filedurability

import (
	"errors"
	"os"
	"path/filepath"
)

// DirectoryAncestry returns the complete resolved directory ancestry, leaf first.
// An existing directory may come from an earlier or concurrent publication whose
// parent sync failed; existence alone cannot establish its durability.
func DirectoryAncestry(dir string) ([]string, error) {
	path, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	var paths []string
	for {
		paths = append(paths, path)
		parent := filepath.Dir(path)
		if parent == path {
			return paths, nil
		}
		path = parent
	}
}

// SyncDirectory persists directory entries and reports sync or close failures.
func SyncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
