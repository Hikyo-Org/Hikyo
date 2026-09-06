package upgradeassembly

import (
	"os"

	"golang.org/x/sys/unix"
)

func openDocument(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
}

func publishDirectory(stage, output string) error {
	return unix.RenamexNp(stage, output, unix.RENAME_EXCL)
}
