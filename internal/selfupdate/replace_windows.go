//go:build windows

package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
	"golang.org/x/sys/windows"
)

// Windows can keep a mapped executable alive after its directory entry moves.
// Move the running image aside first, then publish the staged image at the
// original path. Existing processes keep using the old mapping while every new
// invocation opens the replacement. The update completes before Apply reports
// success.
func replaceBinary(_ context.Context, target string, binary []byte, _ os.FileMode) (err error) {
	lock := flock.New(target + ".update.lock")
	locked, err := lock.TryLock()
	if err != nil {
		return fmt.Errorf("acquire replacement lock: %w", err)
	}
	if !locked {
		return errors.New("another Hikyo process is replacing this executable")
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()

	dir := filepath.Dir(target)
	staged, err := os.CreateTemp(dir, ".hikyo-update-*.exe")
	if err != nil {
		return fmt.Errorf("create staged executable: %w", err)
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)
	if _, err := staged.Write(binary); err != nil {
		_ = staged.Close()
		return fmt.Errorf("write staged executable: %w", err)
	}
	if err := staged.Sync(); err != nil {
		_ = staged.Close()
		return fmt.Errorf("sync staged executable: %w", err)
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("close staged executable: %w", err)
	}

	backup, err := os.CreateTemp(dir, previousPattern(target))
	if err != nil {
		return fmt.Errorf("reserve previous executable path: %w", err)
	}
	backupPath := backup.Name()
	if err := backup.Close(); err != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("close previous executable reservation: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(backupPath)
		}
	}()

	if err := moveFile(target, backupPath, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return fmt.Errorf("move running executable aside: %w", err)
	}
	if err := moveFile(stagedPath, target, windows.MOVEFILE_WRITE_THROUGH); err != nil {
		rollbackErr := moveFile(backupPath, target, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
		if rollbackErr != nil {
			return errors.Join(
				fmt.Errorf("publish replacement executable: %w", err),
				fmt.Errorf("restore previous executable: %w", rollbackErr),
			)
		}
		return fmt.Errorf("publish replacement executable: %w", err)
	}
	return nil
}

func moveFile(fromPath, toPath string, flags uint32) error {
	from, err := windows.UTF16PtrFromString(fromPath)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(toPath)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, flags)
}

// CleanupPrevious removes backups left by completed replacements. A mapped old
// image returns a sharing violation and is retried by a later invocation.
func CleanupPrevious() error {
	target, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable for update cleanup: %w", err)
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return fmt.Errorf("resolve executable for update cleanup: %w", err)
	}
	return cleanupPrevious(target)
}

func cleanupPrevious(target string) error {
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(target), previousPattern(target)))
	if err != nil {
		return fmt.Errorf("find previous executables: %w", err)
	}
	var cleanupErr error
	for _, backup := range backups {
		if err := os.Remove(backup); err != nil &&
			!errors.Is(err, windows.ERROR_SHARING_VIOLATION) &&
			!errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove %s: %w", backup, err))
		}
	}
	return cleanupErr
}

func previousPattern(target string) string {
	return "." + filepath.Base(target) + ".previous-*.exe"
}
