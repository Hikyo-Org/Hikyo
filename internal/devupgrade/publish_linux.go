//go:build linux

package devupgrade

import (
	"golang.org/x/sys/unix"
	"os"
)

func publish(parent *os.File, from, to string) error {
	return unix.Renameat2(int(parent.Fd()), from, int(parent.Fd()), to, unix.RENAME_NOREPLACE)
}
