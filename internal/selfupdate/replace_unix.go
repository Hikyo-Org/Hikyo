//go:build !windows

package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/gofrs/flock"
)

// checkReplacementOwner refuses a privileged replacement of a user-owned
// executable: the rename would leave it root-owned and break every later
// unprivileged update. A root-owned install directory still updates under sudo.
func checkReplacementOwner(target string, info os.FileInfo, euid int) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("selfupdate: inspect owner of %s", target)
	}
	if euid == 0 && stat.Uid != 0 {
		return fmt.Errorf("selfupdate: %s is owned by uid %d; run the update without sudo", target, stat.Uid)
	}
	return nil
}

func replaceBinary(ctx context.Context, target string, binary []byte, mode os.FileMode) (err error) {
	lock := flock.New(target + ".update.lock")
	locked, err := lock.TryLock()
	if err != nil {
		return fmt.Errorf("acquire replacement lock: %w", err)
	}
	if !locked {
		return errors.New("another Hikyo process is replacing this executable")
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	if err := os.Chmod(lock.Path(), 0o600); err != nil {
		return fmt.Errorf("protect replacement lock: %w", err)
	}

	dir := filepath.Dir(target)
	temporary, err := os.CreateTemp(dir, ".hikyo-update-*")
	if err != nil {
		return fmt.Errorf("create staged executable: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set staged executable mode: %w", err)
	}
	if _, err := temporary.Write(binary); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write staged executable: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync staged executable: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close staged executable: %w", err)
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return fmt.Errorf("atomically replace executable: %w", err)
	}
	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open executable directory for sync: %w", err)
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return fmt.Errorf("sync executable directory: %w", err)
	}
	if err := directory.Close(); err != nil {
		return fmt.Errorf("close executable directory: %w", err)
	}
	return nil
}
