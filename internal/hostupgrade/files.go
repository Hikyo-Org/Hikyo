package hostupgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func trustedDirectory(path string) error {
	if !safePath(path) && path != "/" {
		return fmt.Errorf("invalid trusted directory %q", path)
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 || fileOwner(info) != 0 {
			return fmt.Errorf("directory %s must be root-owned and not group/world writable", current)
		}
		if current == "/" {
			return nil
		}
	}
}

func trustedFile(path string) error {
	if !safePath(path) {
		return fmt.Errorf("invalid trusted file %q", path)
	}
	if err := trustedDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || fileOwner(info) != 0 {
		return fmt.Errorf("file %s must be a regular root-owned file without group/world write permissions", path)
	}
	return nil
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := trustedDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || fileOwner(info) != 0 {
			return fmt.Errorf("unsafe existing destination %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".hikyo-upgrade-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	_, writeErr := f.Write(data)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("binary is not a regular file")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, 512<<20+1))
	if err != nil {
		return "", err
	}
	if n > 512<<20 {
		return "", errors.New("binary exceeds 512 MiB")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func validDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size && s == hex.EncodeToString(b)
}

// copyBinary hashes the exact opened bytes before the atomic rename. The
// authenticated digest comes from the parent's independently verified release.
func copyBinary(source, destination, digest string) error {
	if !validDigest(digest) {
		return errors.New("invalid expected binary SHA-256")
	}
	if err := trustedFile(source); err != nil {
		return err
	}
	if err := trustedDirectory(filepath.Dir(destination)); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(destination), ".hikyo-binary-")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(out, h), io.LimitReader(in, 512<<20+1))
	if copyErr != nil || n > 512<<20 || hex.EncodeToString(h.Sum(nil)) != digest {
		out.Close()
		return errors.Join(copyErr, errors.New("candidate binary size or SHA-256 mismatch"))
	}
	if err = out.Chmod(0755); err != nil {
		out.Close()
		return err
	}
	if err = errors.Join(out.Sync(), out.Close()); err != nil {
		return err
	}
	if _, err = os.Lstat(destination); err == nil {
		if err = trustedFile(destination); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = os.Rename(out.Name(), destination); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(destination))
}
