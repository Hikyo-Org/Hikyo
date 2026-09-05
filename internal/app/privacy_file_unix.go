//go:build !windows

package app

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func readPrivacyReceipt(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 {
		return nil, errors.New("privacy: receipt must be an owner-only regular file owned by the current user")
	}
	if st.Size > 4096 {
		return nil, errors.New("privacy: receipt exceeds 4096 bytes")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return nil, err
	}
	if len(raw) > 4096 {
		return nil, fmt.Errorf("privacy: receipt exceeds 4096 bytes")
	}
	return raw, nil
}
