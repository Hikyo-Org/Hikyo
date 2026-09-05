//go:build linux || darwin

package crypto

import (
	"errors"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// ReadEscrowRootKey reads one bounded, private regular file through the same
// descriptor whose identity is compared with the server root source. It proves
// distinct files, not physical offline custody or distinct failure domains.
func ReadEscrowRootKey(path, serverRootPath string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("escrow root file cannot be opened safely")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("escrow root must be a private regular file")
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("escrow root must be owned by the current operator")
	}
	if serverRootPath != "" {
		primary, err := os.Stat(serverRootPath)
		if err != nil {
			return nil, errors.New("configured server root source cannot be identified")
		}
		if os.SameFile(info, primary) {
			return nil, errors.New("escrow root must be a separate custody file, not the server root source")
		}
	}
	raw, err := io.ReadAll(io.LimitReader(f, 4097))
	defer Zero(raw)
	if err != nil {
		return nil, err
	}
	if len(raw) > 4096 {
		return nil, ErrRootKeyFormat
	}
	return decodeRootKey(raw)
}
