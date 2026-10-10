package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

var errDeveloperCredentialExpired = errors.New("developer credential expired; mint a new one with hikyo dev session")

// DeveloperCredentialArtifact is origin-bound private custody, never authority.
// It is deliberately separate from sessions and machine configuration.
type DeveloperCredentialArtifact struct {
	ID          string    `json:"id"`
	Origin      string    `json:"origin"`
	Org         string    `json:"org"`
	Project     string    `json:"project"`
	Environment string    `json:"environment"`
	Token       string    `json:"token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type developerCredentialState struct {
	Version     int                                    `json:"version"`
	Credentials map[string]DeveloperCredentialArtifact `json:"credentials"`
}

func (s *State) DeveloperCredentials() (map[string]DeveloperCredentialArtifact, error) {
	raw, err := readPrivateStateFile(s.dir, "developer-credentials.json")
	if errors.Is(err, os.ErrNotExist) {
		return map[string]DeveloperCredentialArtifact{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read private developer credential state: %w", err)
	}
	var state developerCredentialState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("invalid developer credential state: %w", err)
	}
	if state.Version != 1 {
		return nil, fmt.Errorf("unsupported developer credential state version %d", state.Version)
	}
	if state.Credentials == nil {
		state.Credentials = map[string]DeveloperCredentialArtifact{}
	}
	for id, c := range state.Credentials {
		if id != c.ID || c.ID == "" || c.Origin == "" || c.Org == "" || c.Project == "" || c.Environment == "" || c.ExpiresAt.IsZero() || crypto.ParseArtifact(c.Token, crypto.ArtifactDeveloperCredential) != nil {
			return nil, fmt.Errorf("invalid developer credential custody record")
		}
	}
	return state.Credentials, nil
}

func (s *State) PutDeveloperCredential(c DeveloperCredentialArtifact) error {
	if c.ID == "" || c.Origin == "" || c.Org == "" || c.Project == "" || c.Environment == "" || c.ExpiresAt.IsZero() || crypto.ParseArtifact(c.Token, crypto.ArtifactDeveloperCredential) != nil {
		return fmt.Errorf("invalid developer credential custody record")
	}
	unlock, err := lockStateDir(s.dir)
	if err != nil {
		return err
	}
	defer unlock()
	all, err := s.DeveloperCredentials()
	if err != nil {
		return err
	}
	all[c.ID] = c
	return s.writeJSON(filepath.Join(s.dir, "developer-credentials.json"), developerCredentialState{1, all})
}

func (s *State) deleteDeveloperCredentials(origin, id string) error {
	unlock, err := lockStateDir(s.dir)
	if err != nil {
		return err
	}
	defer unlock()
	all, err := s.DeveloperCredentials()
	if err != nil {
		return err
	}
	for key, c := range all {
		if c.Origin == origin && (id == "" || key == id) {
			delete(all, key)
		}
	}
	return s.writeJSON(filepath.Join(s.dir, "developer-credentials.json"), developerCredentialState{1, all})
}

func (s *State) developerCredentialFor(origin, org, project, environment string, now time.Time) (DeveloperCredentialArtifact, error) {
	all, err := s.DeveloperCredentials()
	if err != nil {
		return DeveloperCredentialArtifact{}, err
	}
	var found DeveloperCredentialArtifact
	expired := false
	for _, c := range all {
		if c.Origin != origin || c.Org != org || c.Project != project || c.Environment != environment {
			continue
		}
		if !now.Before(c.ExpiresAt) {
			expired = true
			continue
		}
		if found.ID != "" {
			return DeveloperCredentialArtifact{}, failf(ExitRefused, "multiple developer credentials match this target; revoke one before running")
		}
		found = c
	}
	if found.ID == "" && expired {
		return found, &Error{Code: ExitAuth, Err: errDeveloperCredentialExpired}
	}
	return found, nil
}

// A repository-local override must never turn credential custody into project data.
func (s *State) developerStateOutsideRepository() error {
	stateDir := s.dir
	for probe := stateDir; ; probe = filepath.Dir(probe) {
		if _, err := os.Lstat(probe); err == nil {
			physical, err := filepath.EvalSymlinks(probe)
			if err != nil {
				return err
			}
			suffix, err := filepath.Rel(probe, stateDir)
			if err != nil {
				return err
			}
			stateDir = filepath.Join(physical, suffix)
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("resolve developer credential state directory: %w", err)
		}
		if filepath.Dir(probe) == probe {
			return fmt.Errorf("cannot resolve developer credential state directory")
		}
	}
	for dir := stateDir; ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return failf(ExitRefused, "developer credential state must be outside the repository")
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect developer credential repository containment: %w", err)
		}
		if filepath.Dir(dir) == dir {
			return nil
		}
	}
}
