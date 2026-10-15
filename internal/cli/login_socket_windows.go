package cli

import "golang.org/x/sys/windows"

func loginSocketOptions(fd uintptr) error {
	if err := windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 0); err != nil {
		return err
	}
	// Winsock2.h defines SO_EXCLUSIVEADDRUSE as ~SO_REUSEADDR. Exclusive
	// binding also prevents another process from forcing a shared bind.
	return windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, ^windows.SO_REUSEADDR, 1)
}
