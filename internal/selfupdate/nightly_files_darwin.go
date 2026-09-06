package selfupdate

import (
	"os"

	"golang.org/x/sys/unix"
)

func openNightlyFile(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
}

func publishNightlyDirectory(stage, output string) error {
	return unix.RenamexNp(stage, output, unix.RENAME_EXCL)
}
