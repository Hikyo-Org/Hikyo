package hostupgrade

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var publicName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$`)

// StagePublicBundle copies a verified private cache into a new root-owned
// public directory. It never reuses a directory writable by the runtime user.
// The caller must verify the published bytes before admitting a migration.
func (h *Host) StagePublicBundle(source string) (string, error) {
	if err := trustedDirectory(source); err != nil {
		return "", err
	}
	if err := trustedDirectory(h.config.PublicDirectory); err != nil {
		return "", err
	}
	destination, err := os.MkdirTemp(h.config.PublicDirectory, "bundle-")
	if err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			os.RemoveAll(destination)
		}
	}()
	var total int64
	count := 0
	directories := []string{destination}
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		count++
		if count > 200000 {
			return errors.New("upgrade bundle exceeds 200000 entries")
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("upgrade bundle contains a symbolic link")
		}
		if entry.IsDir() {
			if err = trustedDirectory(path); err != nil {
				return err
			}
			if err = os.Mkdir(target, 0755); err != nil {
				return err
			}
			directories = append(directories, target)
			return nil
		}
		if err = trustedFile(path); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(output, io.LimitReader(input, (8<<30)-total+1))
		total += n
		if err = errors.Join(copyErr, output.Sync(), output.Close()); err != nil {
			return err
		}
		if total > 8<<30 {
			return errors.New("upgrade bundle exceeds 8 GiB")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if err = os.Chmod(destination, 0755); err != nil {
		return "", err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err = syncDirectory(directories[i]); err != nil {
			return "", err
		}
	}
	if err = syncDirectory(h.config.PublicDirectory); err != nil {
		return "", err
	}
	complete = true
	return destination, nil
}

func (h *Host) PublishPublicEvidence(name string, raw []byte) (string, error) {
	if !publicName.MatchString(name) || len(raw) == 0 || len(raw) > 1<<20 {
		return "", errors.New("invalid public evidence name or size")
	}
	path := filepath.Join(h.config.PublicDirectory, name)
	return path, atomicWrite(path, raw, 0644)
}

// PreparePublicOutput gives the runtime an empty leaf for backup export. Its
// root-owned parent prevents renaming another upgrade's evidence into place.
func (h *Host) PreparePublicOutput(name string) (string, error) {
	if !publicName.MatchString(name) {
		return "", errors.New("invalid output directory name")
	}
	if err := trustedDirectory(h.config.PublicDirectory); err != nil {
		return "", err
	}
	path := filepath.Join(h.config.PublicDirectory, name)
	if err := os.Mkdir(path, 0700); err != nil {
		return "", fmt.Errorf("create fresh backup output: %w", err)
	}
	if err := os.Chown(path, int(h.uid), int(h.gid)); err != nil {
		return "", err
	}
	return path, syncDirectory(h.config.PublicDirectory)
}

// PrunePublic removes the bundle-*, evidence-* and backup-* directories that
// earlier upgrade runs left under the public directory, keeping only the
// entries the given evidence still references. The caller passes the journal
// of the run that just completed, so the retained encrypted backup is always
// the one taken before the current schema. Other names are never touched.
func (h *Host) PrunePublic(keep RuntimeEvidence) error {
	if err := trustedDirectory(h.config.PublicDirectory); err != nil {
		return err
	}
	retained := map[string]bool{}
	for _, path := range []string{keep.BundleDirectory, keep.EvidenceDirectory, keep.CiphertextPath} {
		if path == "" {
			continue
		}
		if !safePath(path) || !within(h.config.PublicDirectory, path) || path == h.config.PublicDirectory {
			return errors.New("retained upgrade evidence must be inside the public upgrade directory")
		}
		rel := strings.TrimPrefix(path, h.config.PublicDirectory+string(filepath.Separator))
		retained[strings.SplitN(rel, string(filepath.Separator), 2)[0]] = true
	}
	entries, err := os.ReadDir(h.config.PublicDirectory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		generated := strings.HasPrefix(name, "bundle-") || strings.HasPrefix(name, "evidence-") || strings.HasPrefix(name, "backup-")
		if !generated || retained[name] || !entry.IsDir() {
			continue
		}
		if err := os.RemoveAll(filepath.Join(h.config.PublicDirectory, name)); err != nil {
			return err
		}
	}
	return syncDirectory(h.config.PublicDirectory)
}
