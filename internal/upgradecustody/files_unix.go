//go:build unix

package upgradecustody

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Walk from / with directory descriptors and O_NOFOLLOW. Every ancestor must
// be controlled by root (or the injected owner in tests). Root-owned sticky
// temporary directories are safe because each next component is also checked.
func custodyDirectory(path string, create bool, owner int) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || os.Geteuid() != owner {
		return nil, errors.New("operator custody requires an absolute clean path and root privileges")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("open operator custody root")
	}
	current := os.NewFile(uintptr(fd), "/")
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		last := i == len(parts)-1
		child, err := unix.Openat(int(current.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(err, unix.ENOENT) && last && create {
			if err = unix.Mkdirat(int(current.Fd()), part, 0700); err == nil {
				err = current.Sync()
			}
			if err == nil {
				child, err = unix.Openat(int(current.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			}
		}
		current.Close()
		if err != nil {
			return nil, errors.New("operator custody path must contain safe real directories")
		}
		current = os.NewFile(uintptr(child), part)
		var st unix.Stat_t
		err = unix.Fstat(child, &st)
		safe := err == nil && (st.Uid == 0 || st.Uid == uint32(owner)) && (st.Mode&0022 == 0 || st.Uid == 0 && st.Mode&unix.S_ISVTX != 0)
		if last {
			safe = err == nil && st.Uid == uint32(owner) && st.Mode&07777 == 0700
		}
		if !safe {
			current.Close()
			return nil, errors.New("operator custody directory has unsafe ownership or permissions")
		}
	}
	return current, nil
}

func publish(dir *os.File, ciphertext []byte) error {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return errors.New("create encrypted custody temporary name")
	}
	name := ".operator-" + hex.EncodeToString(nonce[:])
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return errors.New("create encrypted operator custody")
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	defer unix.Unlinkat(int(dir.Fd()), name, 0)
	if _, err := f.Write(ciphertext); err != nil {
		return errors.New("write encrypted operator custody")
	}
	if err := f.Sync(); err != nil {
		return errors.New("sync encrypted operator custody")
	}
	if err := f.Close(); err != nil {
		return errors.New("close encrypted operator custody")
	}
	// linkat is the atomic no-overwrite publication point. A crash may leave an
	// encrypted temporary file, never a plaintext secret or a partial final file.
	if err := unix.Linkat(int(dir.Fd()), name, int(dir.Fd()), fileName, 0); err != nil {
		return errors.New("publish operator custody: existing custody is never replaced")
	}
	if err := unix.Unlinkat(int(dir.Fd()), name, 0); err != nil {
		return errors.New("remove encrypted operator custody temporary link")
	}
	if err := dir.Sync(); err != nil {
		return errors.New("sync operator custody directory")
	}
	return nil
}

func read(dir *os.File, owner int) ([]byte, error) {
	fd, err := unix.Openat(int(dir.Fd()), fileName, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("open encrypted operator custody")
	}
	f := os.NewFile(uintptr(fd), fileName)
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&07777 != 0600 || st.Uid != uint32(owner) || st.Nlink != 1 || st.Size <= 0 || st.Size > maxCiphertext {
		return nil, errors.New("operator custody file has unsafe type, ownership, permissions, links, or size")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxCiphertext+1))
	if err != nil || len(raw) > maxCiphertext {
		return nil, errors.New("read encrypted operator custody")
	}
	return raw, nil
}
