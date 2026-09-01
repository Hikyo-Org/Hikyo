package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

const (
	applyPendingFile  = "apply-pending"
	appliedStampsFile = "applied-stamps.json"
)

// applyPendingExists reports whether a prior sync left an unfinished apply.
func applyPendingExists(stateDir string) bool {
	_, err := os.Stat(filepath.Join(stateDir, applyPendingFile))
	return err == nil
}

// writeApplyPending records the stamps to apply, atomically, BEFORE docker runs.
func writeApplyPending(stateDir string, stamps map[string]string) error {
	data, err := json.Marshal(stamps)
	if err != nil {
		return err
	}
	return writeFileAtomic0600(filepath.Join(stateDir, applyPendingFile), data)
}

// loadAppliedStamps returns the last stamp selection successfully handed to
// Docker. A missing file is an unapplied state, including upgrades from clients
// predating this record. Malformed state fails loudly rather than skipping an
// apply on guessed data.
func loadAppliedStamps(stateDir string) (map[string]string, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, appliedStampsFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading applied stamps: %w", err)
	}
	var stamps map[string]string
	if err := json.Unmarshal(b, &stamps); err != nil {
		return nil, fmt.Errorf("parsing applied stamps: %w", err)
	}
	if stamps == nil {
		return nil, errors.New("parsing applied stamps: expected a target-to-stamp object")
	}
	for target, stamp := range stamps {
		if strings.TrimSpace(target) == "" {
			return nil, errors.New("parsing applied stamps: target name is empty")
		}
		if err := crypto.ParseStamp(stamp); err != nil {
			return nil, fmt.Errorf("parsing applied stamps for %q: %w", target, err)
		}
	}
	return stamps, nil
}

func writeAppliedStamps(stateDir string, stamps map[string]string) error {
	for target, stamp := range stamps {
		if strings.TrimSpace(target) == "" {
			return errors.New("writing applied stamps: target name is empty")
		}
		if err := crypto.ParseStamp(stamp); err != nil {
			return fmt.Errorf("writing applied stamps for %q: %w", target, err)
		}
	}
	data, err := json.Marshal(stamps)
	if err != nil {
		return err
	}
	return writeFileAtomic0600(filepath.Join(stateDir, appliedStampsFile), data)
}

func stampsNeedApply(current, applied map[string]string) bool {
	return !maps.Equal(current, applied)
}

// removeApplyPending clears the marker after a successful docker apply.
func removeApplyPending(stateDir string) error {
	err := os.Remove(filepath.Join(stateDir, applyPendingFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// writeFileAtomic0600 writes data to a temp file in the same dir and renames it
// into place (0600), creating the directory 0700 if needed.
func writeFileAtomic0600(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	committed = true
	return nil
}
