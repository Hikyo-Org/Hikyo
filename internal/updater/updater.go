// Package updater preserves legacy deployment journal/protocol compatibility
// while refusing every apply entry point. It executes no deployment commands.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
)

type Backend string

const (
	BackendFlux    Backend = "flux"
	BackendCompose Backend = "compose"
	BackendSystemd Backend = "systemd"
)

func (b Backend) Valid() bool {
	return b == BackendFlux || b == BackendCompose || b == BackendSystemd
}

type State string

const (
	StateQueued         State = "queued"
	StateRunning        State = "running"
	StateSucceeded      State = "succeeded"
	StateFailed         State = "failed"
	StateRolledBack     State = "rolled-back"
	StateRollbackFailed State = "rollback-failed"
)

func (s State) Terminal() bool {
	return s == StateSucceeded || s == StateFailed || s == StateRolledBack || s == StateRollbackFailed
}

const RemoteApplyDisabledReason = config.RemoteApplyDisabledReason

var ErrRemoteApplyDisabled = config.ErrRemoteApplyDisabled

var (
	ErrStableOnly       = errors.New("updater: remote apply admits stable releases only")
	ErrReleaseAuthority = errors.New("updater: release URL is outside the fixed Hikyo authority")
	ErrUpdateActive     = errors.New("updater: another update is active")
	ErrJobNotFound      = errors.New("updater: job not found")
)

const (
	maxJournalBytes = 4 << 20
	maxJournalJobs  = 100
)

type Request struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	ReleaseURL  string `json:"release_url"`
	RequestedBy string `json:"requested_by"`
}

type Job struct {
	ID              string    `json:"id"`
	Backend         Backend   `json:"backend"`
	Version         string    `json:"version"`
	ReleaseURL      string    `json:"release_url"`
	RequestedBy     string    `json:"requested_by"`
	State           State     `json:"state"`
	Phase           Phase     `json:"phase"`
	FailureCode     string    `json:"failure_code,omitempty"`
	RequestedAt     time.Time `json:"requested_at"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	FinishedAt      time.Time `json:"finished_at,omitempty"`
	OutcomeReported bool      `json:"outcome_reported"`
}

type Phase string

const (
	PhaseQueued   Phase = "queued"
	PhasePlan     Phase = "plan"
	PhaseBackup   Phase = "backup"
	PhaseVerify   Phase = "verify"
	PhaseApply    Phase = "apply"
	PhaseHealth   Phase = "health"
	PhaseRollback Phase = "rollback"
	PhaseRecovery Phase = "recovery"
	PhaseComplete Phase = "complete"
)

type Command struct {
	Name           string   `json:"name"`
	Argv           []string `json:"argv"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

// Config retains the backend identity reported by the refused executor.
// Executable phase policy is retired and is never loaded.
type Config struct {
	Backend Backend `json:"backend"`
}

type Runner interface {
	Run(context.Context, Command, Request) error
}

type CommandRunner struct{}

// Run refuses even direct calls from a queued legacy phase.
func (CommandRunner) Run(context.Context, Command, Request) error {
	return ErrRemoteApplyDisabled
}

type Executor struct {
	Config Config
	Now    func() time.Time
}

func (e Executor) now() time.Time {
	if e.Now == nil {
		return time.Now().UTC()
	}
	return e.Now().UTC()
}

// Execute cannot resume or roll back a legacy job. Historical journals remain
// readable; retirement never guesses whether a prior process changed schema.
func (e Executor) Execute(_ context.Context, request Request) (Job, error) {
	now := e.now()
	return Job{
		ID: request.ID, Backend: e.Config.Backend, Version: request.Version,
		ReleaseURL: request.ReleaseURL, RequestedBy: request.RequestedBy,
		State: StateFailed, Phase: PhasePlan, FailureCode: "remote-apply-disabled",
		RequestedAt: now, FinishedAt: now,
	}, ErrRemoteApplyDisabled
}

type journalFile struct {
	Jobs []Job `json:"jobs"`
}

type Journal struct {
	Path string
	mu   sync.Mutex
}

func (j *Journal) Create(job Job) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.read()
	if err != nil {
		return err
	}
	for _, existing := range state.Jobs {
		if !existing.State.Terminal() {
			return ErrUpdateActive
		}
	}
	if len(state.Jobs) >= maxJournalJobs {
		state.Jobs = append([]Job(nil), state.Jobs[len(state.Jobs)-maxJournalJobs+1:]...)
	}
	state.Jobs = append(state.Jobs, job)
	return j.write(state)
}

func (j *Journal) Put(job Job) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.read()
	if err != nil {
		return err
	}
	for i := range state.Jobs {
		if state.Jobs[i].ID == job.ID {
			state.Jobs[i] = job
			return j.write(state)
		}
	}
	return ErrJobNotFound
}

func (j *Journal) Get(id string) (Job, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.read()
	if err != nil {
		return Job{}, err
	}
	for _, job := range state.Jobs {
		if job.ID == id {
			return job, nil
		}
	}
	return Job{}, ErrJobNotFound
}

func (j *Journal) PendingOutcomes() ([]Job, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.read()
	if err != nil {
		return nil, err
	}
	pending := make([]Job, 0)
	for _, job := range state.Jobs {
		if job.State.Terminal() && !job.OutcomeReported {
			pending = append(pending, job)
		}
	}
	return pending, nil
}

// RecoverInterrupted makes a helper restart loud and durable. The helper does
// not guess whether a privileged child completed after its parent disappeared.
func (j *Journal) RecoverInterrupted(now time.Time) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.read()
	if err != nil {
		return err
	}
	changed := false
	for i := range state.Jobs {
		if !state.Jobs[i].State.Terminal() {
			state.Jobs[i].State = StateFailed
			state.Jobs[i].Phase = PhaseRecovery
			state.Jobs[i].FailureCode = "helper-restarted"
			state.Jobs[i].FinishedAt = now.UTC()
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return j.write(state)
}

func (j *Journal) read() (journalFile, error) {
	if j.Path == "" {
		return journalFile{}, errors.New("updater: journal path is required")
	}
	b, err := os.ReadFile(j.Path)
	if errors.Is(err, os.ErrNotExist) {
		return journalFile{}, nil
	}
	if err != nil {
		return journalFile{}, err
	}
	if len(b) > maxJournalBytes {
		return journalFile{}, fmt.Errorf("updater: journal exceeds %d bytes", maxJournalBytes)
	}
	var state journalFile
	if err := json.Unmarshal(b, &state); err != nil {
		return journalFile{}, fmt.Errorf("updater: parse journal: %w", err)
	}
	return state, nil
}

func (j *Journal) write(state journalFile) error {
	dir := filepath.Dir(j.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".updater-state-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, j.Path)
}
