package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// The directory lock outlives a canceled datastore transaction, including an
// unlink already executing in the filesystem. All publication/record/prune paths cooperate
// on it; a shared backup mount must support cross-process advisory locking.
func lockBackupDirectory(ctx context.Context, dir string) (func(), error) {
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(canonical, ".hikyo-backup-retention.lock")
	info, err := os.Lstat(path)
	if err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("backup directory lock is not a regular file")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	lock := flock.New(path)
	ok, err := lock.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("backup directory lock unavailable")
	}
	return func() { _ = lock.Unlock() }, nil
}

// Verification reads ciphertext only; export/prune never load decryption keys.
func verifyBackupArtifact(ctx context.Context, path string, size int64, digest string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if size <= 0 || len(digest) != sha256.Size*2 {
		return errors.New("backup artifact has no verified publication identity")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return errors.New("backup artifact differs from recorded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, opened) {
		return errors.New("backup artifact changed while opening")
	}
	return verifyBackupCiphertext(ctx, file, size, digest)
}

func verifyBackupCiphertext(ctx context.Context, reader io.Reader, size int64, digest string) error {
	hash := sha256.New()
	n, err := io.Copy(hash, &backupContextReader{ctx: ctx, reader: io.LimitReader(reader, size+1)})
	if err != nil {
		return err
	}
	if n != size || hex.EncodeToString(hash.Sum(nil)) != digest {
		return fmt.Errorf("backup artifact ciphertext identity differs from successful export")
	}
	return nil
}

func backupNameExpired(name string, now time.Time) bool {
	m := archiveName.FindStringSubmatch(name)
	if m == nil {
		return false
	}
	at, err := time.Parse("20060102T150405Z", m[2])
	return err == nil && at.Before(now.Add(-BackupRetentionCeiling))
}

// A canceled lease/job stops verification between reads. A filesystem read
// already in progress still finishes under the directory lock before release.
type backupContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *backupContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
