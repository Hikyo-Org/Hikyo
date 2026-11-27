// Package modroot locates the module root for tools, generators and tests that
// read repository files.
package modroot

import (
	"fmt"
	"os"
	"path/filepath"
)

// Find walks up from the working directory to the directory holding go.mod.
// It does not derive the root from a source file path (runtime.Caller), which
// -trimpath rewrites to an import path.
func Find() (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := start; ; {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found in %s or any parent directory", start)
		}
		dir = parent
	}
}
