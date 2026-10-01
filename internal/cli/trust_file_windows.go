//go:build windows

package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

const trustFileLimit = 1 << 20

func readTrustFile(dir string) ([]byte, error) {
	return readPrivateStateFile(dir, "trust.json")
}

func ensureTrustStateDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("trust store directory must be absolute: %s", dir)
	}
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
			return err
		}
		attributes, err := privateWindowsAttributes(true)
		if err != nil {
			return err
		}
		name, err := windows.UTF16PtrFromString(dir)
		if err != nil {
			return err
		}
		if err := windows.CreateDirectory(name, attributes); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			return err
		}
	} else if err != nil {
		return err
	}
	// Tightening a directory can propagate inherited ACLs to children. Never
	// launder a pre-existing unsafe trust/session file into apparent custody.
	for _, name := range []string{"trust.json", "sessions.json"} {
		file, err := openPrivateWindowsFile(filepath.Join(dir, name), false, false)
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			continue
		}
		if err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	file, err := openPrivateWindowsFile(dir, true, true)
	if err != nil {
		return err
	}
	return file.Close()
}

func lockStateDir(dir string) (func(), error) {
	if err := ensureTrustStateDir(dir); err != nil {
		return nil, err
	}
	directory, err := openPrivateWindowsFile(dir, true, false)
	if err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(filepath.Join(dir, "state.lock"))
	if err != nil {
		_ = directory.Close()
		return nil, err
	}
	attributes, err := privateWindowsAttributes(false)
	if err != nil {
		_ = directory.Close()
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, attributes, windows.OPEN_ALWAYS, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		_ = directory.Close()
		return nil, err
	}
	lock := os.NewFile(uintptr(handle), filepath.Join(dir, "state.lock"))
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil || info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		_ = lock.Close()
		_ = directory.Close()
		return nil, fmt.Errorf("state lock must be a regular non-reparse file")
	}
	if err := verifyWindowsCustody(windows.Handle(lock.Fd()), false); err != nil {
		_ = lock.Close()
		_ = directory.Close()
		return nil, err
	}
	overlapped := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(lock.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, overlapped); err != nil {
		_ = lock.Close()
		_ = directory.Close()
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(lock.Fd()), 0, 1, 0, overlapped)
		_ = lock.Close()
		_ = directory.Close()
	}, nil
}
