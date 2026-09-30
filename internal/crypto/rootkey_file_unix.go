//go:build unix

package crypto

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

const rootKeyFileLimit = 4096

func readRootKeyFile(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w (file %s does not exist)", ErrNoRootKey, path)
		}
		return nil, fmt.Errorf("%w: root key file cannot be opened safely: %v", ErrRootKeyPerms, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("crypto: root key file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrRootKeyPerms, path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%w: %s is mode %04o", ErrRootKeyPerms, path, info.Mode().Perm())
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (stat.Uid != uint32(os.Geteuid()) && stat.Uid != 0) {
		return nil, fmt.Errorf("%w: %s is not owned by the current operator or root", ErrRootKeyPerms, path)
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
