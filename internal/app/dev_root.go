package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
	"github.com/gofrs/flock"
)

// ensureDevRootKey serializes creation across processes and publishes the whole
// key atomically. No process may replace a key another startup has already used.
func ensureDevRootKey(path string) (created bool, err error) {
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return false, err
	}
	path = filepath.Join(parent, filepath.Base(path))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	lock := flock.New(path + ".lock")
	locked, err := lock.TryLockContext(ctx, 25*time.Millisecond)
	if err != nil || !locked {
		return false, fmt.Errorf("development root key creation lock: %w", errors.Join(err, ctx.Err()))
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		key, err := crypto.GenerateRootKey()
		if err != nil {
			return false, err
		}
		defer crypto.Zero(key)
		raw := []byte(crypto.EncodeRootKey(key) + "\n")
		defer crypto.Zero(raw)
		if err := securefile.WriteAtomic(path, raw, 0600); err != nil {
			return false, fmt.Errorf("publish development root key: %w", err)
		}
		created = true
	} else if err != nil {
		return false, err
	} else if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return false, errors.New("development root key must be a regular mode-0600 file")
	}
	// Repeat the durability check on retries after an earlier directory-sync
	// failure. Existing file entries alone do not prove durable publication.
	ancestry, err := filedurability.DirectoryAncestry(parent)
	if err != nil {
		return false, err
	}
	for _, directory := range ancestry {
		if err := filedurability.SyncDirectory(directory); err != nil {
			return false, fmt.Errorf("persist development root key directory: %w", err)
		}
	}
	return created, nil
}
