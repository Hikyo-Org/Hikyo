//go:build !windows

package app

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// chownSQLiteFiles hands the restored database back to the service account. The root
// rollback wrote it, and the source cannot open a root-owned 0600 file. The service account
// owns this directory, so a link it left must never steer root's chown at another file.
func chownSQLiteFiles(path string, uid int) error {
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("restored database file %s is not a regular file", name)
		}
		if err := chownSingleLinkFile(name, info, uid); err != nil {
			return err
		}
	}
	return nil
}

// chownSingleLinkFile changes the owner through a descriptor of the file Lstat saw, so neither a hard
// link nor a swap after the check can redirect it.
func chownSingleLinkFile(name string, seen os.FileInfo, uid int) error {
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return err
	}
	st, ok := opened.Sys().(*syscall.Stat_t)
	if !ok || !opened.Mode().IsRegular() || st.Nlink != 1 || !os.SameFile(seen, opened) {
		return fmt.Errorf("restored database file %s is multiply linked or was replaced", name)
	}
	return unix.Fchown(fd, uid, -1)
}
