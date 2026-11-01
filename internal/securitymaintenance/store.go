package securitymaintenance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/localsocket"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
	"github.com/gofrs/flock"
)

type Store struct{ directory string }

var ErrStateUnavailable = errors.New("maintenance state is unavailable; preserve existing recovery evidence")

func NewStore(directory string) (*Store, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, errors.New("maintenance state requires a clean absolute directory")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 || !ownedByCurrentUser(info) {
		return nil, errors.New("maintenance state must be a private directory owned by its coordinator")
	}
	if err := localsocket.ValidatePath(filepath.Join(directory, "coordinator.lock")); err != nil {
		return nil, err
	}
	return &Store{directory: directory}, nil
}

func (s *Store) locked(ctx context.Context, action func() error) (err error) {
	path := filepath.Join(s.directory, "coordinator.lock")
	if info, statErr := os.Lstat(path); statErr == nil {
		if !info.Mode().IsRegular() || !ownedByCurrentUser(info) {
			return errors.New("unsafe maintenance lock")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	lock := flock.New(path)
	if err := ctx.Err(); err != nil {
		return err
	}
	held, err := lock.TryLock()
	if err != nil {
		return err
	}
	if !held {
		return errors.New("another maintenance coordinator owns this installation")
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	return action()
}

func (s *Store) Load() (result Status, returnErr error) {
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(ErrStateUnavailable, returnErr)
		}
	}()
	state := Status{Format: StatusFormat, Phase: "unenrolled", Applicable: []string{}}
	path := filepath.Join(s.directory, "status.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !ownedByCurrentUser(info) {
		return state, errors.New("unsafe maintenance status file")
	}
	f, err := os.Open(path)
	if err != nil {
		return state, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return state, errors.New("maintenance state changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxCatalogBytes+1))
	if err != nil {
		return state, err
	}
	if len(raw) > MaxCatalogBytes || definitions.DecodeStrict(raw, &state) != nil {
		return state, errors.New("invalid maintenance status file")
	}
	if err := validateStatus(state); err != nil {
		return state, err
	}
	return state, nil
}

func (s *Store) Save(state Status) (returnErr error) {
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(ErrStateUnavailable, returnErr)
		}
	}()
	if err := validateStatus(state); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if len(raw) > MaxCatalogBytes {
		return errors.New("maintenance state exceeds byte bound")
	}
	return securefile.WriteAtomic(filepath.Join(s.directory, "status.json"), append(raw, '\n'), 0600)
}

func validateStatus(state Status) error {
	if state.Policy != nil {
		if err := state.Policy.Validate(); err != nil {
			return err
		}
	}
	if state.Format != StatusFormat || state.Phase == "" || len(state.Reason) > 128 || len(state.Applicable) > 2048 {
		return errors.New("invalid maintenance state format or phase")
	}
	if state.CatalogSequence > 0 && state.CatalogSHA256.Validate() != nil {
		return errors.New("maintenance catalog floor is invalid")
	}
	if state.Enrollment != nil {
		e := state.Enrollment
		if e.Instance == "" || len(e.Instance) > 256 || releaseidentity.Digest(e.Incarnation).Validate() != nil || e.RestoreEpoch < 0 || e.ConsentedAt.IsZero() {
			return errors.New("invalid maintenance enrollment")
		}
	}
	if state.Operation != nil {
		op := state.Operation
		if op.Target.Identity.Validate() != nil || op.Source.Validate() != nil || op.RouteSHA256.Validate() != nil || op.StartedAt.IsZero() {
			return errors.New("maintenance operation is invalid; do not discard recovery evidence")
		}
	}
	return nil
}
