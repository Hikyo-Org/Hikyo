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

func publish(dir *os.File, ciphertext []byte, replace bool) error {
	if err := unix.Flock(int(dir.Fd()), unix.LOCK_EX); err != nil {
		return errors.New("lock operator custody directory")
	}
	defer unix.Flock(int(dir.Fd()), unix.LOCK_UN)
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
	if replace {
		// Rewrapping replaces the whole file atomically; the record inside is
		// the same, so a crash leaves either the old or the new container.
		if err := unix.Renameat(int(dir.Fd()), name, int(dir.Fd()), fileName); err != nil {
			return errors.New("replace encrypted operator custody")
		}
	} else {
		// linkat is the atomic no-overwrite publication point. A crash may leave an
		// encrypted temporary file, never a plaintext secret or a partial final file.
		if err := unix.Linkat(int(dir.Fd()), name, int(dir.Fd()), fileName, 0); err != nil {
			return errors.New("publish operator custody: existing custody is never replaced")
		}
		if err := unix.Unlinkat(int(dir.Fd()), name, 0); err != nil {
			return errors.New("remove encrypted operator custody temporary link")
		}
	}
	if err := dir.Sync(); err != nil {
		return errors.New("sync operator custody directory")
	}
	return nil
}

func read(dir *os.File, owner int) ([]byte, error) {
	if err := unix.Flock(int(dir.Fd()), unix.LOCK_EX); err != nil {
		return nil, errors.New("lock operator custody directory")
	}
	defer unix.Flock(int(dir.Fd()), unix.LOCK_UN)
	fd, err := unix.Openat(int(dir.Fd()), fileName, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("open encrypted operator custody")
	}
	f := os.NewFile(uintptr(fd), fileName)
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&07777 != 0600 || st.Uid != uint32(owner) || st.Size <= 0 || st.Size > maxCiphertext {
		return nil, errors.New("operator custody file has unsafe type, ownership, permissions, links, or size")
	}
	if st.Nlink == 2 {
		if err := recoverInterruptedPublication(dir, st, owner); err != nil {
			return nil, err
		}
		if unix.Fstat(fd, &st) != nil {
			return nil, errors.New("restat recovered operator custody")
		}
	}
	if st.Nlink != 1 {
		return nil, errors.New("operator custody file has unsafe type, ownership, permissions, links, or size")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxCiphertext+1))
	if err != nil || len(raw) > maxCiphertext {
		return nil, errors.New("read encrypted operator custody")
	}
	return raw, nil
}

// recoverInterruptedPublication removes only the private temporary name left
// by publish after its atomic no-overwrite link succeeded. An unrelated hard
// link remains a refusal: recovery requires the one expected name, inode,
// owner, mode, size, and link count while the directory is exclusively locked.
func recoverInterruptedPublication(dir *os.File, final unix.Stat_t, owner int) error {
	if _, err := dir.Seek(0, 0); err != nil {
		return errors.New("rewind operator custody directory")
	}
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return errors.New("inspect operator custody publication")
	}
	match := ""
	for _, name := range names {
		if !strings.HasPrefix(name, ".operator-") {
			continue
		}
		fd, openErr := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			continue
		}
		var candidate unix.Stat_t
		statErr := unix.Fstat(fd, &candidate)
		unix.Close(fd)
		if statErr == nil && candidate.Dev == final.Dev && candidate.Ino == final.Ino &&
			candidate.Mode&unix.S_IFMT == unix.S_IFREG && candidate.Mode&07777 == 0600 &&
			candidate.Uid == uint32(owner) && candidate.Nlink == 2 && candidate.Size == final.Size {
			if match != "" {
				return errors.New("operator custody publication has ambiguous temporary links")
			}
			match = name
		}
	}
	if match == "" {
		return errors.New("operator custody file has an unrelated hard link")
	}
	if err := unix.Unlinkat(int(dir.Fd()), match, 0); err != nil {
		return errors.New("recover encrypted operator custody publication")
	}
	if err := dir.Sync(); err != nil {
		return errors.New("sync recovered operator custody directory")
	}
	return nil
}
