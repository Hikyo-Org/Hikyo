//go:build windows

package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

const trustFileLimit = 1 << 20

func readTrustFile(dir string) ([]byte, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("trust store directory must be absolute: %s", dir)
	}
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("trust store directory %s must be a real directory", dir)
	}
	path := filepath.Join(dir, "trust.json")
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("trust store file %s must be a regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, after) {
		return nil, fmt.Errorf("trust store file %s changed while opening", path)
	}
	raw, err := io.ReadAll(io.LimitReader(file, trustFileLimit+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > trustFileLimit {
		return nil, fmt.Errorf("trust store file %s exceeds 1 MiB", path)
	}
	return raw, nil
}

func ensureTrustStateDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("trust store directory must be absolute: %s", dir)
	}
	return os.MkdirAll(dir, 0o700)
}

func lockStateDir(dir string) (func(), error) {
	if err := ensureTrustStateDir(dir); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "state.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	overlapped := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(lock.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, overlapped); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(lock.Fd()), 0, 1, 0, overlapped)
		_ = lock.Close()
	}, nil
}
