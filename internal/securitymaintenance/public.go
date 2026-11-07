package securitymaintenance

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/securefile"
	"github.com/Hikyo-Org/hikyo/internal/securitystatus"
)

// Public is the subset of Status the serving application may show. It leaves out
// the operation record and every recovery fence. Action is passed through whole
// because an administrator needs the exact command, and its confirmation value (a
// manifest digest or a recovery incarnation) is not a credential. Detail is the
// failure text and can echo arbitrary tool output, so it is scrubbed and bounded.
func (s Status) Public() securitystatus.Public {
	public := securitystatus.Public{
		Format:            securitystatus.Format,
		Enrolled:          s.Enrollment != nil,
		Phase:             s.Phase,
		Reason:            s.Reason,
		ProtectionGap:     s.ProtectionGap(),
		Applicable:        append([]string{}, s.Applicable...),
		Coverage:          append([]string{}, s.Coverage...),
		CoveredComponents: append([]string{}, s.CoveredComponents...),
		CoverageThrough:   s.CoverageThrough,
		CatalogSequence:   s.CatalogSequence,
		LastAttempt:       s.LastAttempt,
		LastVerified:      s.LastVerified,
		KnownUntil:        s.KnownUntil,
		Action:            s.Action,
		Detail:            securitystatus.Detail(s.Detail, maxDetailBytes),
	}
	if s.Policy != nil {
		public.Mode, public.Channel = string(s.Policy.Mode), string(s.Policy.Channel)
	}
	if s.Running != nil {
		public.RunningVersion = s.Running.Identity.Version
	}
	if s.Selected != nil {
		public.TargetVersion = s.Selected.Identity.Version
	}
	return public
}

// publish runs after the private state is durable. The file is a courtesy to
// the serving application, so a failure here is logged and never fails maintenance.
func (s *Store) publish(state Status) {
	if s.PublicStatusFile == "" {
		return
	}
	raw, err := json.MarshalIndent(state.Public(), "", "  ")
	if err == nil {
		err = s.outsidePrivateState(filepath.Dir(s.PublicStatusFile))
	}
	if err == nil {
		err = ensurePublicDirectory(filepath.Dir(s.PublicStatusFile))
	}
	if err == nil {
		err = securefile.WriteAtomic(s.PublicStatusFile, append(raw, '\n'), 0644)
	}
	if err != nil {
		slog.Warn("security maintenance public status was not written", "path", s.PublicStatusFile, "err", err)
	}
}

// The reader is another user's process and the coordinator's unit sets a 0077
// umask, so each directory created here is chmodded, parents included. An
// existing one is never touched: it may be shared with private files.
func ensurePublicDirectory(dir string) error {
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := ensurePublicDirectory(filepath.Dir(dir)); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0755); errors.Is(err, fs.ErrExist) {
		return nil
	} else if err != nil {
		return err
	}
	return os.Chmod(dir, 0755)
}

// outsidePrivateState compares directories by identity, so neither an alias nor a
// mount can let the public subset replace the private state or its lock. It runs
// before the public directory is created, from the nearest existing ancestor, so a
// refused path leaves nothing behind inside the private state.
func (s *Store) outsidePrivateState(dir string) error {
	private, err := os.Stat(s.directory)
	if err != nil {
		return err
	}
	existing := dir
	for {
		_, err := os.Lstat(existing)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return err
		}
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return err
	}
	for {
		info, err := os.Stat(resolved)
		if err != nil {
			return err
		}
		if os.SameFile(info, private) {
			return errors.New("public status file lies inside the private maintenance state")
		}
		parent := filepath.Dir(resolved)
		if parent == resolved {
			return nil
		}
		resolved = parent
	}
}
