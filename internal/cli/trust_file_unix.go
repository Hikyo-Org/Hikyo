//go:build !windows

package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const trustFileLimit = 1 << 20

func openTrustStateDir(dir string) (*os.File, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("trust store directory must be absolute: %s", dir)
	}
	pathInfo, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("trust store directory %s must not be a symbolic link", dir)
	}
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	directory := os.NewFile(uintptr(fd), dir)
	info, err := directory.Stat()
	if err != nil {
		_ = directory.Close()
		return nil, err
	}
	if !os.SameFile(pathInfo, info) || !info.IsDir() || info.Mode().Perm() != 0o700 || !ownedByEUID(info) {
		_ = directory.Close()
		return nil, fmt.Errorf("trust store directory %s must be an owner-controlled 0700 directory", dir)
	}
	return directory, nil
}

func readTrustFile(dir string) ([]byte, error) {
	return readPrivateStateFile(dir, "trust.json")
}

func readPrivateStateFile(dir, name string) ([]byte, error) {
	if name != "trust.json" && name != "sessions.json" && name != "developer-credentials.json" {
		return nil, fmt.Errorf("unsupported private state file")
	}
	path := filepath.Join(dir, name)
	pathInfo, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("trust store file %s must not be a symbolic link", path)
	}
	// Existing XDG state directories are commonly created as 0755. Tighten a
	// safely owner-controlled directory before requiring the trust store's 0700
	// invariant; unsafe group/world-writable directories still fail closed.
	if err := ensureTrustStateDir(dir); err != nil {
		return nil, err
	}
	directory, err := openTrustStateDir(dir)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(pathInfo, info) || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByEUID(info) {
		return nil, fmt.Errorf("trust store file %s must be an owner-controlled regular 0600 file", file.Name())
	}
	raw, err := io.ReadAll(io.LimitReader(file, trustFileLimit+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > trustFileLimit {
		return nil, fmt.Errorf("trust store file %s exceeds 1 MiB", file.Name())
	}
	return raw, nil
}

func ensureTrustStateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	pathInfo, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.IsDir() || !ownedByEUID(pathInfo) || pathInfo.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("trust store directory %s must be an owner-controlled 0700 directory (mode %04o)", dir, pathInfo.Mode().Perm())
	}
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(fd), dir)
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(pathInfo, info) {
		return fmt.Errorf("trust store directory %s changed while it was opened", dir)
	}
	if err := unix.Fchmod(int(directory.Fd()), 0o700); err != nil {
		return err
	}
	return nil
}

func lockStateDir(dir string) (func(), error) {
	if err := ensureTrustStateDir(dir); err != nil {
		return nil, fmt.Errorf("prepare state directory: %w", err)
	}
	directory, err := openTrustStateDir(dir)
	if err != nil {
		return nil, fmt.Errorf("open state directory: %w", err)
	}
	fd, err := unix.Openat(int(directory.Fd()), "state.lock", unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if errors.Is(err, unix.EEXIST) {
		fd, err = unix.Openat(int(directory.Fd()), "state.lock", unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	}
	_ = directory.Close()
	if err != nil {
		return nil, fmt.Errorf("open state lock: %w", err)
	}
	lock := os.NewFile(uintptr(fd), filepath.Join(dir, "state.lock"))
	info, err := lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByEUID(info) {
		_ = lock.Close()
		return nil, fmt.Errorf("state lock must be an owner-controlled regular 0600 file")
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("acquire state lock: %w", err)
	}
	return func() {
		_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
		_ = lock.Close()
	}, nil
}
