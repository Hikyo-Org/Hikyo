//go:build !unix

package crypto

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

const rootKeyFileLimit = 4096

func readRootKeyFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w (file %s does not exist)", ErrNoRootKey, path)
		}
		return nil, fmt.Errorf("crypto: root key file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrRootKeyPerms, path)
	}
	raw, err := io.ReadAll(io.LimitReader(file, rootKeyFileLimit+1))
	if err != nil {
		return nil, fmt.Errorf("crypto: root key file: %w", err)
	}
	if len(raw) > rootKeyFileLimit {
		Zero(raw)
		return nil, ErrRootKeyFormat
	}
	return raw, nil
}
