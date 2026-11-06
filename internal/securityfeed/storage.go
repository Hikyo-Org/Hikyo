package securityfeed

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/filedurability"
)

func privateDirectory(path, volume string) error { return privateVolumeDirectory(path, volume, false) }

func privateVolumeDirectory(path, volume string, worldWritableVolume bool) error {
	if err := checkVolumeAncestors(path, volume, worldWritableVolume); err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		return errors.New("security feed state directory must be real, private and mode 0700")
	}
	return checkOwner(info)
}

func checkAncestors(path, volume string) error { return checkVolumeAncestors(path, volume, false) }

// volume is a mounted volume root the platform may leave group-writable (a
// Kubernetes fsGroup). Only that one directory skips the group-writability test;
// a world-writable one without the sticky bit also needs worldWritableVolume.
func checkVolumeAncestors(path, volume string, worldWritableVolume bool) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			replaceable := info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0
			if replaceable && current == volume && info.Mode().Perm()&0002 != 0 && !worldWritableVolume {
				return fmt.Errorf("security feed volume root %s is world-writable without the sticky bit: remove its world write permission or accept the risk with accept_world_writable_volume_root", current)
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || replaceable && current != volume {
				return errors.New("security feed authority has a symlink or writable directory ancestor")
			}
			if err := checkAncestorOwner(info); err != nil {
				return err
			}
		}
		if current == filepath.Dir(current) {
			return nil
		}
	}
}

func checkOptionalFile(path string, max int64, private bool) error {
	_, err := readRegular(path, max, private)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func readRegular(path string, max int64, private bool) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 || private && info.Mode().Perm()&0077 != 0 || info.Size() > max {
		return nil, errors.New("security feed file must be bounded, owned and non-writable by other users")
	}
	// A public file may also be root-owned: Kubernetes projects ConfigMaps as root
	// while the coordinator runs unprivileged. For a root coordinator this is the same rule.
	if err := checkOwner(info); err != nil && (private || checkAncestorOwner(info) != nil) {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("security feed file changed during opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > max {
		return nil, errors.New("security feed file exceeds byte bound")
	}
	return raw, nil
}

func validateCache(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	if len(entries) > 64 {
		return errors.New("security feed metadata cache exceeds file bound")
	}
	for _, entry := range entries {
		if _, err := readRegular(filepath.Join(directory, entry.Name()), MaxDocumentBytes, false); err != nil {
			return fmt.Errorf("security feed metadata cache: %w", err)
		}
	}
	return nil
}

func syncMetadata(directory string) error {
	var result error
	for _, name := range []string{"root.json", "timestamp.json", "snapshot.json", "targets.json"} {
		path := filepath.Join(directory, name)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		result = errors.Join(result, syncFile(path))
	}
	return errors.Join(result, filedurability.SyncDirectory(directory))
}

func syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
