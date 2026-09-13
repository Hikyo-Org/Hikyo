//go:build linux

package app

import (
	"encoding/hex"
	"os"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"golang.org/x/sys/unix"
)

// A sealed memfd permits repeated legacy credential reads without writing the
// root key to disk or placing it in process arguments or environment values.
func unattendedRootCredential(_ *config.Config, root []byte) (*os.File, string, error) {
	fd, err := unix.MemfdCreate("hikyo-unattended-root", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, "", err
	}
	file := os.NewFile(uintptr(fd), "hikyo-unattended-root")
	raw := make([]byte, hex.EncodedLen(len(root)))
	hex.Encode(raw, root)
	defer clear(raw)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(raw)
	}
	if err == nil {
		_, err = file.Seek(0, 0)
	}
	if err == nil {
		_, err = unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL)
	}
	if err != nil {
		file.Close()
		return nil, "", err
	}
	return file, "/proc/self/fd/3", nil
}
