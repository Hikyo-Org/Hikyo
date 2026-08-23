package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
)

const releaseSnapshotTTL = 24 * time.Hour

type updateState struct {
	Channel   updatecheck.Channel   `json:"channel"`
	CheckedAt time.Time             `json:"checked_at,omitempty"`
	Releases  []updatecheck.Release `json:"releases,omitempty"`
}

func (s *State) updatesPath() string { return filepath.Join(s.dir, "updates.json") }

func (s *State) updates() (updateState, error) {
	raw, err := os.ReadFile(s.updatesPath())
	if errors.Is(err, os.ErrNotExist) {
		return updateState{Channel: updatecheck.ChannelStable}, nil
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
	return state, nil
}

func (s *State) putUpdates(state updateState) error { return s.writeJSON(s.updatesPath(), state) }

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
		if err := state.putUpdates(updateState{Channel: channel}); err != nil {
			return err
		}
		fmt.Fprintf(ios.Stdout, "Update channel: %s\n", channel)
		return nil
	case "check":
		if len(args) != 1 {
			return failf(ExitUsage, "usage: hikyo update check")
		}
		current, err := state.updates()
		if err != nil {
			return err
		}
		if current.Channel == updatecheck.ChannelOff {
			fmt.Fprintln(ios.Stdout, "Update checks are off.")
			return nil
		}
		if err := refreshReleaseSnapshot(ctx, ios); err != nil {
			return failf(ExitUnavailable, "update check failed: %v", err)
		}
		current, err = state.updates()
		if err != nil {
			return err
		}
		status, err := updatecheck.Select(ios.Version, current.Channel, current.Releases)
		if err != nil {
			return err
		}
		if !status.Available {
			fmt.Fprintf(ios.Stdout, "Hikyo %s is current on %s.\n", ios.Version, current.Channel)
			return nil
		}
		fmt.Fprintf(ios.Stdout, "Update available on %s: %s (current %s)\n%s\n",
			current.Channel, status.LatestVersion, ios.Version, status.URL)
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
	current, err := state.updates()
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
		// Persist the attempt time even when the public source is unavailable.
		// Ordinary commands stay best-effort and do not pay the network timeout
		// repeatedly; `hikyo update check` remains an explicit immediate retry.
		current.CheckedAt = ios.now().UTC()
		if writeErr := state.putUpdates(current); writeErr != nil {
			return errors.Join(err, writeErr)
		}
		return err
	}
	current.CheckedAt = ios.now().UTC()
	current.Releases = releases
	return state.putUpdates(current)
}

// NotifyUpdate emits a best-effort interactive notice without changing command status.
func NotifyUpdate(ctx context.Context, ios IO) {
	if ios.Version == "" || ios.Version == "dev" ||
		ios.StderrIsTerminal == nil || !ios.StderrIsTerminal() {
		return
	}
	state, err := NewState(ios.Env)
	if err != nil {
		return
	}
	current, err := state.updates()
	if err != nil || current.Channel == updatecheck.ChannelOff {
		return
	}
	age := ios.now().Sub(current.CheckedAt)
	if current.CheckedAt.IsZero() || age < 0 || age >= releaseSnapshotTTL {
		if err := refreshReleaseSnapshot(ctx, ios); err == nil {
			current, err = state.updates()
		}
	}
	if err != nil {
		return
	}
	status, err := updatecheck.Select(ios.Version, current.Channel, current.Releases)
	if err != nil || !status.Available {
		return
	}
	fmt.Fprintf(ios.Stderr, "Update available: %s (current %s, %s). %s\n",
		status.LatestVersion, ios.Version, current.Channel, status.URL)
}
