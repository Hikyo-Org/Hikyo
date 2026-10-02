//go:build linux

package filesync

import (
	"os"

	"github.com/Hikyo-Org/hikyo/internal/compose"
	"golang.org/x/sys/unix"
)

// isTmpfs is the Compose path's statfs check (TMPFS_MAGIC or RAMFS_MAGIC).
func isTmpfs(dir string) (bool, error) { return compose.IsTmpfs(dir) }

func isTmpfsFile(file *os.File) (bool, error) {
	var stat unix.Statfs_t
	if err := unix.Fstatfs(int(file.Fd()), &stat); err != nil {
		return false, err
	}
	return stat.Type == unix.TMPFS_MAGIC || stat.Type == unix.RAMFS_MAGIC, nil
}
