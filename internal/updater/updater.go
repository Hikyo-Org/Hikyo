// Package updater owns the privileged deployment-helper seam. The Hikyo
// server never receives host, Docker, Git, or cluster credentials; it can only
// submit a versioned job to this separately configured local process.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"
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

var (
	ErrStableOnly       = errors.New("updater: remote apply admits stable releases only")
	ErrReleaseAuthority = errors.New("updater: release URL is outside the fixed Hikyo authority")
	ErrUpdateActive     = errors.New("updater: another update is active")
	ErrJobNotFound      = errors.New("updater: job not found")
)

const (
	maxConfigBytes  = 1 << 20
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

func (c Command) validate(phase string) error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("updater: %s command name is required", phase)
	}
	if !filepath.IsAbs(c.Name) {
		return fmt.Errorf("updater: %s command name must be an absolute path", phase)
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 3600 {
		return fmt.Errorf("updater: %s timeout_seconds must be between 1 and 3600", phase)
	}
	for _, arg := range c.Argv {
		if strings.ContainsRune(arg, '\x00') {
			return fmt.Errorf("updater: %s command contains NUL", phase)
		}
	}
	return nil
}

type PhaseCommands struct {
	Plan     Command `json:"plan"`
	Backup   Command `json:"backup"`
	Verify   Command `json:"verify"`
	Apply    Command `json:"apply"`
	Health   Command `json:"health"`
	Rollback Command `json:"rollback"`
}

type Config struct {
	Backend   Backend       `json:"backend"`
	Socket    string        `json:"socket"`
	StateFile string        `json:"state_file"`
	Commands  PhaseCommands `json:"commands"`
}

func (c Config) Validate() error {
	if !c.Backend.Valid() {
		return fmt.Errorf("updater: backend must be flux, compose, or systemd, got %q", c.Backend)
	}
	for phase, command := range map[string]Command{
		"plan": c.Commands.Plan, "backup": c.Commands.Backup, "verify": c.Commands.Verify,
		"apply": c.Commands.Apply, "health": c.Commands.Health, "rollback": c.Commands.Rollback,
	} {
		if err := command.validate(phase); err != nil {
			return err
		}
	}
	return nil
}

func LoadConfig(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Config{}, err
	}
	if info.Mode().Perm()&0o022 != 0 {
		return Config{}, fmt.Errorf("updater: config %q must not be group/world writable", path)
	}
	if info.Size() > maxConfigBytes {
		return Config{}, fmt.Errorf("updater: config %q exceeds %d bytes", path, maxConfigBytes)
	}
	decoder := json.NewDecoder(io.LimitReader(f, maxConfigBytes))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("updater: parse config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("updater: config must contain exactly one JSON value")
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

type Runner interface {
	Run(context.Context, Command, Request) error
}

type CommandRunner struct{}

func (CommandRunner) Run(ctx context.Context, command Command, request Request) error {
	argv := make([]string, len(command.Argv))
	for i, arg := range command.Argv {
		argv[i] = substitute(arg, request)
	}
	cmd := exec.CommandContext(ctx, command.Name, argv...)
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("updater: command %q failed: %w", command.Name, err)
	}
	return nil
}

func substitute(value string, request Request) string {
	replacer := strings.NewReplacer(
		"{version}", request.Version,
		"{release_url}", request.ReleaseURL,
		"{job_id}", request.ID,
	)
	return replacer.Replace(value)
}

type Executor struct {
	Config Config
	Runner Runner
	Now    func() time.Time
	// Progress durably records the phase before its privileged command starts.
	// A persistence failure refuses further execution.
	Progress func(Job) error
}

func (e Executor) now() time.Time {
	if e.Now == nil {
		return time.Now().UTC()
	}
	return e.Now().UTC()
}

func (e Executor) Execute(ctx context.Context, request Request) (Job, error) {
	version := "v" + request.Version
	if !semver.IsValid(version) || semver.Prerelease(version) != "" {
		return Job{}, ErrStableOnly
	}
	if err := e.Config.Validate(); err != nil {
		return Job{}, err
	}
	runner := e.Runner
	if runner == nil {
		runner = CommandRunner{}
	}
	now := e.now()
	job := Job{
		ID: request.ID, Backend: e.Config.Backend, Version: request.Version,
		ReleaseURL: request.ReleaseURL, RequestedBy: request.RequestedBy,
		State: StateRunning, Phase: PhasePlan, RequestedAt: now, StartedAt: now,
	}
	phases := []struct {
		name    Phase
		command Command
	}{
		{PhasePlan, e.Config.Commands.Plan},
		{PhaseBackup, e.Config.Commands.Backup},
		{PhaseVerify, e.Config.Commands.Verify},
		{PhaseApply, e.Config.Commands.Apply},
		{PhaseHealth, e.Config.Commands.Health},
	}
	applied := false
	for _, phase := range phases {
		job.Phase = phase.name
		if e.Progress != nil {
			if err := e.Progress(job); err != nil {
				job.State = StateFailed
				job.FailureCode = "journal-write-failed"
				job.FinishedAt = e.now()
				return job, err
			}
		}
		phaseCtx, cancel := context.WithTimeout(ctx, time.Duration(phase.command.TimeoutSeconds)*time.Second)
		err := runner.Run(phaseCtx, phase.command, request)
		cancel()
		if err != nil {
			job.FailureCode = string(phase.name) + "-failed"
			job.FinishedAt = e.now()
			// An apply command can mutate the deployment and then return an
			// error. Treat entering apply as the rollback boundary; waiting for
			// a zero exit code would strand a partial rollout.
			if applied || phase.name == PhaseApply {
				job.Phase = PhaseRollback
				if e.Progress != nil {
					if progressErr := e.Progress(job); progressErr != nil {
						job.State = StateRollbackFailed
						job.FailureCode = "journal-write-failed"
						return job, errors.Join(err, progressErr)
					}
				}
				// Cancellation stops the active command, then rollback gets its own
				// bounded cleanup context so helper shutdown cannot strand a partial apply.
				rollbackCtx, cancelRollback := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(e.Config.Commands.Rollback.TimeoutSeconds)*time.Second)
				rollbackErr := runner.Run(rollbackCtx, e.Config.Commands.Rollback, request)
				cancelRollback()
				if rollbackErr != nil {
					job.State = StateRollbackFailed
					job.FailureCode = "rollback-failed"
					job.FinishedAt = e.now()
					return job, errors.Join(err, rollbackErr)
				}
				job.State = StateRolledBack
				job.FinishedAt = e.now()
				return job, err
			}
			job.State = StateFailed
			return job, err
		}
		if phase.name == PhaseApply {
			applied = true
		}
	}
	job.State = StateSucceeded
	job.Phase = PhaseComplete
	job.FinishedAt = e.now()
	return job, nil
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
