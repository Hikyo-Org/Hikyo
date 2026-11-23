package service

// Export staging: the unencrypted datastore copy (#1412).
//
// An export's work directory holds snapshot.db or COPY output in plaintext. A
// SIGKILL skips every defer, so cleanup cannot be the only defence: each export
// holds an flock on `<work>/.live` for its lifetime, the kernel drops it on any
// process death, and a sweep removes only directories that are provably Hikyo's
// (exact name shape plus the marker) and whose lock is free. No age threshold.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/gofrs/flock"
)

const exportLiveLock = ".live"

var (
	// os.MkdirTemp appends a decimal; nothing else matches.
	exportWorkName = regexp.MustCompile(`^\.hikyo-export-work-[0-9]+$`)
	// Shapes written by releases before #1412, swept only when provably ours.
	legacyExportWorkName    = regexp.MustCompile(`^\.export-[0-9]+$`)
	legacyExportPartialName = regexp.MustCompile(`^\.hikyo-export-[0-9]+\.partial$`)
	// Work directories owned by this process. On NFS flock is emulated with
	// process-wide POSIX locks, so a same-process sweep could "acquire" a live
	// export's lock; this set is the in-process liveness record.
	liveExportWork sync.Map
)

func (s *Backup) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// stageExportWork creates the export's private work directory (0700 from
// MkdirTemp) and holds the liveness lock until the returned cleanup runs.
// Creation and sweeping share the directory lock, so a sweep cannot see a new
// directory before its lock is held.
func stageExportWork(ctx context.Context, dir string) (string, func(), error) {
	unlock, err := lockBackupDirectory(ctx, dir)
	if err != nil {
		return "", nil, fmt.Errorf("backup staging directory lock: %w", err)
	}
	defer unlock()
	// The live set and the sweep both key on the symlink-free absolute path.
	if dir, err = canonicalDir(dir); err != nil {
		return "", nil, fmt.Errorf("backup staging directory: %w", err)
	}
	work, err := os.MkdirTemp(dir, ".hikyo-export-work-")
	if err != nil {
		return "", nil, fmt.Errorf("backup staging directory: %w", err)
	}
	live := flock.New(filepath.Join(work, exportLiveLock))
	ok, err := live.TryLock()
	if err != nil {
		return "", nil, errors.Join(fmt.Errorf("backup staging lock: %w", err), os.RemoveAll(work))
	}
	if !ok {
		return "", nil, errors.Join(errors.New("backup staging lock: held by another process"), os.RemoveAll(work))
	}
	liveExportWork.Store(work, struct{}{})
	return work, func() {
		// Plaintext goes first and the lock is released before the directory
		// itself is removed: Windows cannot delete a directory holding a
		// locked file.
		removeChildren(work)
		_ = live.Unlock()
		_ = os.RemoveAll(work)
		liveExportWork.Delete(work)
	}, nil
}

func removeChildren(work string) {
	entries, _ := os.ReadDir(work)
	for _, e := range entries {
		if e.Name() != exportLiveLock {
			_ = os.RemoveAll(filepath.Join(work, e.Name()))
		}
	}
}

func canonicalDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// sweepDirectory takes the directory lock and removes debris of dead exports.
// A missing directory has none; any other failure to look is reported.
func (s *Backup) sweepDirectory(ctx context.Context, dir string) error {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	unlock, err := lockBackupDirectory(ctx, dir)
	if err != nil {
		return err
	}
	defer unlock()
	return sweepExportDebris(dir)
}

// sweepExportDebris requires the directory lock. Anything not matching an exact
// Hikyo shape is left alone: the operator may point --out at a folder holding
// their own files.
func sweepExportDebris(dir string) error {
	dir, err := canonicalDir(dir)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		switch {
		case e.Type().IsRegular() && legacyExportPartialName.MatchString(e.Name()):
			if err := os.Remove(path); err != nil {
				errs = append(errs, err)
			}
		case e.IsDir() && exportWorkName.MatchString(e.Name()):
			errs = append(errs, sweepWorkDir(path))
		case e.IsDir() && legacyExportWorkName.MatchString(e.Name()):
			errs = append(errs, sweepLegacyWorkDir(path))
		}
	}
	return errors.Join(errs...)
}

func sweepWorkDir(work string) error {
	if _, live := liveExportWork.Load(work); live {
		return nil
	}
	marker := filepath.Join(work, exportLiveLock)
	before, err := os.Lstat(marker)
	if err != nil || !before.Mode().IsRegular() {
		// No marker: an export killed between mkdir and lock holds nothing,
		// and rmdir refuses anything else.
		if err := os.Remove(work); err != nil {
			slog.Warn("backup export work directory has no liveness marker and was left in place; remove it by hand if no export is running", "path", work)
		}
		return nil
	}
	lock := flock.New(marker)
	if ok, err := lock.TryLock(); err != nil || !ok {
		return err
	}
	// The open follows symlinks; refuse a marker swapped for a link meanwhile.
	if after, err := os.Lstat(marker); err != nil || !os.SameFile(before, after) {
		_ = lock.Unlock()
		return err
	}
	removeChildren(work)
	_ = lock.Unlock()
	return os.RemoveAll(work)
}

// A pre-#1412 directory has no marker, so ownership is proven by content: only
// the names store.Export writes there.
func sweepLegacyWorkDir(work string) error {
	entries, err := os.ReadDir(work)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !legacyStagedName(e.Name()) {
			return nil
		}
	}
	return os.RemoveAll(work)
}

func legacyStagedName(name string) bool {
	switch name {
	case "snapshot.db", "snapshot.db-journal", "snapshot.db-wal", "snapshot.db-shm":
		return true
	}
	return strings.HasPrefix(name, "copy-")
}
