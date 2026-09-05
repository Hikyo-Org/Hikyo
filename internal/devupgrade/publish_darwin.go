//go:build darwin

package devupgrade

import (
	"golang.org/x/sys/unix"
	"os"
)

func publish(parent *os.File, from, to string) error {
	return unix.RenameatxNp(int(parent.Fd()), from, int(parent.Fd()), to, unix.RENAME_EXCL)
}
