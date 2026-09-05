//go:build !windows

package backupreceipt

import (
	"os"

	"golang.org/x/sys/unix"
)

func openCiphertextSource(path string) (*os.File, error) {
	// Nonblocking open lets the following fstat reject a FIFO without waiting
	// for a writer. Never follow a substituted final-component symlink.
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func openPinnedCiphertext(root *os.Root) (*os.File, error) {
	return openOwnedReadonly(root, "ciphertext.age")
}

func openOwnedReadonly(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
}
