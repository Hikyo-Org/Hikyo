//go:build unix

package filesync

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// lockDirectory takes a non-blocking exclusive flock on the destination
// directory itself, so two clients (two configs, or one config run twice) can
// never publish into the same directory at once. The lock lives on the
// directory's own descriptor: it needs no lock file in the destination and the
// kernel drops it when the process dies.
func lockDirectory(dir string, want fs.FileInfo) (*os.File, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	got, err := f.Stat()
	if err != nil || !os.SameFile(want, got) {
		f.Close()
		return nil, fmt.Errorf("filesync: destination %s changed while it was being locked", dir)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w: %s", ErrBusy, dir)
		}
		return nil, fmt.Errorf("filesync: lock %s: %w", dir, err)
	}
	return f, nil
}

func unlockDirectory(f *os.File) error {
	if f == nil {
		return nil
	}
	return errors.Join(unix.Flock(int(f.Fd()), unix.LOCK_UN), f.Close())
}

// ownedAs reports whether fi is owned by uid/gid; -1 matches anything.
func ownedAs(fi fs.FileInfo, uid, gid int) bool {
	u, g := ownerIDs(fi)
	return (uid < 0 || u == uid) && (gid < 0 || g == gid)
}

func ownerIDs(fi fs.FileInfo) (int, int) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return -1, -1
	}
	return int(st.Uid), int(st.Gid)
}

// syncRootDir fsyncs a directory named relative to root.
func syncRootDir(root *os.Root, name string) error {
	d, err := root.Open(name)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}
