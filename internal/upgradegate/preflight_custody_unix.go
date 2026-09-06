//go:build linux || darwin

package upgradegate

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// InspectCustodyDirectory captures identity without creating a directory, lock
// or journal. A later selection must refer to this same persistent object.
func InspectCustodyDirectory(path string) (os.FileInfo, error) {
	file, err := openCustodyDirectoryReadonly(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return file.Stat()
}

func openCustodyDirectoryReadonly(path string) (*os.File, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open(canonical, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), canonical)
	if err := operatorSecure(file, true); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func inspectOperatorCustody(path string, installed os.FileInfo) (operatorCustody, []byte, error) {
	directory, err := openCustodyDirectoryReadonly(path)
	if err != nil {
		return operatorCustody{}, nil, err
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil || installed == nil || !os.SameFile(installed, info) {
		return operatorCustody{}, nil, errors.New("upgrade state must retain the installed persistent custody directory; copying or relocating custody requires an explicit operator procedure")
	}
	fd, err := unix.Openat(int(directory.Fd()), operatorCustodyName, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return operatorCustody{}, nil, err
	}
	file := os.NewFile(uintptr(fd), operatorCustodyName)
	defer file.Close()
	if err := operatorSecure(file, false); err != nil {
		return operatorCustody{}, nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return operatorCustody{}, nil, errors.New("operator custody exceeds bound")
	}
	value, err := decodeOperatorCustody(raw)
	return value, raw, err
}
