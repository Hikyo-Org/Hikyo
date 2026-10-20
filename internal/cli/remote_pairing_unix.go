//go:build !windows

package cli

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func openPairingInputFile(path string) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0o077 != 0 || !ownedByEUID(before) {
		return nil, errors.New("pairing input lacks private custody")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Mode().Perm()&0o077 != 0 || !ownedByEUID(after) {
		_ = file.Close()
		return nil, errors.New("pairing input changed while opening it")
	}
	return file, nil
}
