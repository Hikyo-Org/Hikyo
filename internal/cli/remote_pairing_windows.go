//go:build windows

package cli

import (
	"os"
	"path/filepath"
)

func openPairingInputFile(path string) (*os.File, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return openPrivateWindowsFile(absolute, false, false)
}
