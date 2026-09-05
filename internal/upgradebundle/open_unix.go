//go:build !windows

package upgradebundle

import (
	"golang.org/x/sys/unix"
	"os"
)

func openDocument(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
}

func openDirectory(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
}
