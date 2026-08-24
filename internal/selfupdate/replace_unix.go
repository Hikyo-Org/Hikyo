//go:build !windows

package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

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
