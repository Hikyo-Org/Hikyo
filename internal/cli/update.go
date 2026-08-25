package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/console"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/gofrs/flock"
)

const (
	releaseSnapshotTTL = 24 * time.Hour
	updateStateSchema  = 1
)

type updateState struct {
	Schema          int                   `json:"schema,omitempty"`
	Channel         updatecheck.Channel   `json:"channel"`
	ChannelExplicit bool                  `json:"channel_explicit,omitempty"`
	CheckedAt       time.Time             `json:"checked_at,omitempty"`
	Releases        []updatecheck.Release `json:"releases,omitempty"`
}

func (s *State) updatesPath() string     { return filepath.Join(s.dir, "updates.json") }
func (s *State) updatesLockPath() string { return filepath.Join(s.dir, "updates.lock") }

func (s *State) updates(defaultChannel updatecheck.Channel) (updateState, error) {
	if defaultChannel == "" {
		defaultChannel = updatecheck.ChannelOff
	}
	raw, err := os.ReadFile(s.updatesPath())
	if errors.Is(err, os.ErrNotExist) {
		channel, parseErr := updatecheck.ParseChannel(string(defaultChannel))
		if parseErr != nil {
			return updateState{}, fmt.Errorf("built-in update channel: %w", parseErr)
		}
		return updateState{Schema: updateStateSchema, Channel: channel}, nil
	}
	if err != nil {
		return updateState{}, err
	}
	var state updateState
	if err := json.Unmarshal(raw, &state); err != nil {
		return updateState{}, fmt.Errorf("update state at %s is unreadable: %w", s.updatesPath(), err)
	}
	channel, err := updatecheck.ParseChannel(string(state.Channel))
	if err != nil {
		return updateState{}, fmt.Errorf("update state at %s: %w", s.updatesPath(), err)
	}
	state.Channel = channel
	if state.Schema < 0 || state.Schema > updateStateSchema {
		return updateState{}, fmt.Errorf("update state at %s has unsupported schema %d", s.updatesPath(), state.Schema)
	}
	// Schema zero predates artifact-specific defaults and always persisted an
	// implicit stable channel. It is not a user override: migrate it to the
	// channel stamped into this binary and force a fresh snapshot.
	if state.Schema == 0 {
		channel, err := updatecheck.ParseChannel(string(defaultChannel))
		if err != nil {
			return updateState{}, fmt.Errorf("built-in update channel: %w", err)
		}
		state.Channel = channel
		state.ChannelExplicit = false
		state.CheckedAt = time.Time{}
		state.Releases = nil
	} else if (defaultChannel == updatecheck.ChannelOff || !state.ChannelExplicit) && state.Channel != defaultChannel {
		state.Channel = defaultChannel
		state.ChannelExplicit = false
		state.CheckedAt = time.Time{}
		state.Releases = nil
	}
	return state, nil
}

func (s *State) putUpdatesUnlocked(state updateState) error {
	state.Schema = updateStateSchema
	return s.writeJSON(s.updatesPath(), state)
}

func (s *State) withUpdatesLock(ctx context.Context, action func() error) (err error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	lock := flock.New(s.updatesLockPath())
	locked, err := lock.TryLock()
	if err != nil {
		return fmt.Errorf("acquire update-state lock: %w", err)
	}
	if !locked {
		return errors.New("another Hikyo process is updating the release state")
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	if err := os.Chmod(lock.Path(), 0o600); err != nil {
		return fmt.Errorf("protect update-state lock: %w", err)
	}
	return action()
}

func runUpdate(ctx context.Context, ios IO, args []string) error {
	if len(args) == 0 {
		return failf(ExitUsage, "usage: hikyo update channel stable|nightly|off | hikyo update check")
	}
	state, err := NewState(ios.Env)
	if err != nil {
		return err
	}
	switch args[0] {
	case "channel":
		if len(args) != 2 {
			return failf(ExitUsage, "usage: hikyo update channel stable|nightly|off")
		}
		channel, err := updatecheck.ParseChannel(args[1])
		if err != nil {
			return failf(ExitUsage, "%v", err)
		}
		if ios.DefaultUpdateChannel == updatecheck.ChannelOff && channel != updatecheck.ChannelOff {
			return failf(ExitUsage, "source builds keep update checks off; install a published Hikyo artifact to select a release channel")
		}
		if err := state.withUpdatesLock(ctx, func() error {
			return state.putUpdatesUnlocked(updateState{Channel: channel, ChannelExplicit: true})
		}); err != nil {
			return err
		}
		fmt.Fprintf(ios.Stdout, "Update channel: %s\n", channel)
		return nil
	case "check":
		if len(args) != 1 {
			return failf(ExitUsage, "usage: hikyo update check")
		}
		current, err := state.updates(ios.DefaultUpdateChannel)
		if err != nil {
			return err
		}
		if current.Channel == updatecheck.ChannelOff {
			fmt.Fprintln(ios.Stdout, "Update checks are off.")
			return nil
		}
		fmt.Fprint(ios.Stderr, console.UpdateCheckMessage(string(current.Channel)))
		if err := refreshReleaseSnapshot(ctx, ios); err != nil {
			return failf(ExitUnavailable, "update check failed: %v", err)
		}
		current, err = state.updates(ios.DefaultUpdateChannel)
		if err != nil {
			return err
		}
		status, err := updatecheck.Select(ios.Version, current.Channel, current.Releases)
		if err != nil {
			return err
		}
		if !status.Available {
			fmt.Fprint(ios.Stdout, console.UpdateCurrentMessage(console.UpdateInfo{
				Current: ios.Version, Channel: string(current.Channel),
			}))
			return nil
		}
		fmt.Fprint(ios.Stdout, console.UpdateAvailableMessage(console.UpdateInfo{
			Current: ios.Version, Latest: status.LatestVersion,
			Channel: string(current.Channel), ReleaseURL: status.URL,
		}))
		if _, err := promptAndApplyUpdate(ctx, ios, status); err != nil {
			return failf(ExitUnavailable, "update failed: %v", err)
		}
		return nil
	default:
		return failf(ExitUsage, "unknown update command %q", args[0])
	}
}

func updateSource(ios IO) (updatecheck.Source, error) {
	if ios.UpdateSource != nil {
		return ios.UpdateSource, nil
	}
	client, err := updatecheck.NewHTTPClient(2 * time.Second)
	if err != nil {
		return nil, err
	}
	return updatecheck.NewGitHubSource(client), nil
}

func refreshReleaseSnapshot(ctx context.Context, ios IO) error {
	state, err := NewState(ios.Env)
	if err != nil {
		return err
	}
	return state.withUpdatesLock(ctx, func() error {
		current, err := state.updates(ios.DefaultUpdateChannel)
		if err != nil {
			return err
		}
		if current.Channel == updatecheck.ChannelOff {
			return nil
		}
		source, err := updateSource(ios)
		if err != nil {
			return err
		}
		releases, err := source.Releases(ctx)
		if err != nil {
			// A schema-zero snapshot has no explicit channel provenance or asset
			// metadata. Leave it untouched on failure so the next invocation
			// retries migration instead of offering stale, unverified assets.
			if current.Schema == 0 {
				return err
			}
			// Persist the attempt time even when the public source is unavailable.
			// Ordinary commands stay best-effort and do not pay the network timeout
			// repeatedly; `hikyo update check` remains an explicit immediate retry.
			current.CheckedAt = ios.now().UTC()
			if writeErr := state.putUpdatesUnlocked(current); writeErr != nil {
				return errors.Join(err, writeErr)
			}
			return err
		}
		current.CheckedAt = ios.now().UTC()
		current.Releases = releases
		return state.putUpdatesUnlocked(current)
	})
}

// NotifyUpdate emits a best-effort interactive notice before command dispatch.
// It reports whether the binary changed so the caller can end this process and
// let the user restart into the new executable.
func NotifyUpdate(ctx context.Context, ios IO) bool {
	if ios.Version == "" || ios.StderrIsTerminal == nil || !ios.StderrIsTerminal() {
		return false
	}
	state, err := NewState(ios.Env)
	if err != nil {
		return false
	}
	current, err := state.updates(ios.DefaultUpdateChannel)
	if err != nil || current.Channel == updatecheck.ChannelOff {
		return false
	}
	age := ios.now().Sub(current.CheckedAt)
	if current.Schema != updateStateSchema || current.CheckedAt.IsZero() || age < 0 || age >= releaseSnapshotTTL {
		if err := refreshReleaseSnapshot(ctx, ios); err == nil {
			current, err = state.updates(ios.DefaultUpdateChannel)
		}
	}
	if err != nil {
		return false
	}
	status, err := updatecheck.Select(ios.Version, current.Channel, current.Releases)
	if err != nil || !status.Available {
		return false
	}
	fmt.Fprint(ios.Stderr, console.UpdateAvailableMessage(console.UpdateInfo{
		Current: ios.Version, Latest: status.LatestVersion,
		Channel: string(status.Channel), ReleaseURL: status.URL,
	}))
	updated, err := promptAndApplyUpdate(ctx, ios, status)
	if err != nil {
		fmt.Fprintf(ios.Stderr, "Update failed: %v\n", err)
	}
	return updated
}

func promptAndApplyUpdate(ctx context.Context, ios IO, status updatecheck.Status) (bool, error) {
	if ios.TerminalSession == nil {
		return false, nil
	}
	confirmed, err := ios.TerminalSession.Confirm(
		fmt.Sprintf("Update Hikyo to %s now?", status.LatestVersion),
	)
	if err != nil {
		return false, fmt.Errorf("confirmation: %w", err)
	}
	if !confirmed {
		return false, nil
	}
	if ios.BinaryUpdater == nil {
		return false, errors.New("binary updater is unavailable")
	}
	if err := ios.BinaryUpdater.Apply(ctx, status); err != nil {
		return false, err
	}
	fmt.Fprintf(ios.Stderr, "Hikyo %s is verified and updated in place. Restart the command to use it.\n",
		status.LatestVersion)
	return true, nil
}
