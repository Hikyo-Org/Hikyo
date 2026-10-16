//go:build darwin || linux

package cli

import "golang.org/x/sys/unix"

func loginSocketOptions(fd uintptr) error {
	if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 0); err != nil {
		return err
	}
	return unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 0)
}
