package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/console"
	"github.com/Hikyo-Org/hikyo/internal/diagnostics"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/gofrs/flock"
)

const (
	releaseSnapshotTTL = 24 * time.Hour
	updateStateSchema  = 1
	// The pre-command notice must not stall an unrelated command; an explicit
	// check or upgrade waits long enough for a slow release API.
	passiveCheckTimeout  = 2 * time.Second
	explicitCheckTimeout = 30 * time.Second
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
	// A non-explicit channel that no longer matches the artifact default (or any
	// channel once the artifact is built with updates off) is not a user
	// override: reset it to the channel stamped into this binary and force a
	// fresh snapshot.
	if (defaultChannel == updatecheck.ChannelOff || !state.ChannelExplicit) && state.Channel != defaultChannel {
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
		status, err := checkForUpdate(ctx, ios, state, current.Channel)
		if err != nil || !status.Available {
			return err
		}
		if _, err := promptAndApplyUpdate(ctx, ios, status); err != nil {
			return failf(ExitUnavailable, "update failed: %v", err)
		}
		return nil
	default:
		return failf(ExitUsage, "unknown update command %q", args[0])
	}
}

// RunUpgrade is `hikyo upgrade` where no server host is supported: it installs
// the newest verified release on this CLI's track over the running executable.
// Naming the command is the confirmation, so it does not prompt.
func RunUpgrade(ctx context.Context, ios IO, args []string) error {
	flags := flag.NewFlagSet("hikyo upgrade", flag.ContinueOnError)
	flags.SetOutput(ios.Stderr)
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: hikyo [-v|-vv|-vvv] upgrade")
		fmt.Fprintln(flags.Output(), "Installs the newest verified release on this CLI's update channel over the running executable.")
		fmt.Fprintln(flags.Output(), "Server hosts upgrade with sudo hikyo upgrade on Linux.")
	}
	// Requested help is the payload and goes to stdout; a malformed
	// invocation is a usage error on stderr.
	if HelpRequested(args) {
		flags.SetOutput(ios.Stdout)
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return &Error{Code: ExitUsage, Err: err}
	}
	if flags.NArg() != 0 {
		return failf(ExitUsage, "usage: hikyo upgrade")
	}
	// Refuse before the release refresh writes state, e.g. sudo over a
	// user-owned executable.
	if ios.BinaryUpdater == nil {
		return errors.New("binary updater is unavailable")
	}
	if err := ios.BinaryUpdater.CheckReplaceable(); err != nil {
		return failf(ExitUsage, "%v", err)
	}
	state, err := NewState(ios.Env)
	if err != nil {
		return err
	}
	current, err := state.updates(ios.DefaultUpdateChannel)
	if err != nil {
		return err
	}
	if ios.DefaultUpdateChannel == updatecheck.ChannelOff {
		return failf(ExitUsage, "source builds keep update checks off; rebuild from a reviewed source revision instead")
	}
	if current.Channel == updatecheck.ChannelOff {
		return failf(ExitUsage, "update checks are off; select a track with hikyo update channel stable|nightly")
	}
	status, err := checkForUpdate(ctx, ios, state, current.Channel)
	if err != nil || !status.Available {
		return err
	}
	if _, err := applyUpdate(ctx, ios, status); err != nil {
		return failf(ExitUnavailable, "update failed: %v", err)
	}
	return nil
}

// checkForUpdate refreshes the release snapshot immediately and reports the
// selected release, printing the current or available version.
func checkForUpdate(ctx context.Context, ios IO, state *State, channel updatecheck.Channel) (updatecheck.Status, error) {
	fmt.Fprint(ios.Stderr, console.UpdateCheckMessage(string(channel)))
	if err := refreshReleaseSnapshot(ctx, ios, explicitCheckTimeout); err != nil {
		return updatecheck.Status{}, failf(ExitUnavailable, "update check failed: %v", err)
	}
	current, err := state.updates(ios.DefaultUpdateChannel)
	if err != nil {
		return updatecheck.Status{}, err
	}
	status, err := updatecheck.Select(ios.Version, current.Channel, current.Releases)
	if err != nil {
		return updatecheck.Status{}, err
	}
	if !status.Available {
		fmt.Fprint(ios.Stdout, console.UpdateCurrentMessage(console.UpdateInfo{
			Current: ios.Version, Channel: string(current.Channel),
		}))
		return status, nil
	}
	fmt.Fprint(ios.Stdout, console.UpdateAvailableMessage(console.UpdateInfo{
		Current: ios.Version, Latest: status.LatestVersion,
		Channel: string(current.Channel), ReleaseURL: status.URL,
	}))
	return status, nil
}

func updateSource(ios IO, timeout time.Duration) (updatecheck.Source, error) {
	if ios.UpdateSource != nil {
		return ios.UpdateSource, nil
	}
	client, err := updatecheck.NewHTTPClient(timeout)
	if err != nil {
		return nil, err
	}
	client.Transport = diagnostics.Transport{Base: client.Transport}
	return updatecheck.NewGitHubSource(client), nil
}

func refreshReleaseSnapshot(ctx context.Context, ios IO, timeout time.Duration) error {
	diagnostics.Printf(ctx, 1, "refreshing release metadata")
	defer diagnostics.Time(ctx, "refresh release metadata")()
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
		source, err := updateSource(ios, timeout)
		if err != nil {
			return err
		}
		releases, err := source.Releases(ctx)
		if err != nil {
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
		diagnostics.Printf(ctx, 2, "release metadata contains %d releases", len(releases))
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
		if refreshErr := refreshReleaseSnapshot(ctx, ios, passiveCheckTimeout); refreshErr == nil {
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
	prompt := fmt.Sprintf("Update Hikyo to %s now?", status.LatestVersion)
	if status.Channel == updatecheck.ChannelNightly && status.Prerelease && selfupdate.StagesNightlies() {
		prompt = fmt.Sprintf("Download and verify Hikyo %s for a manual server upgrade?", status.LatestVersion)
	}
	confirmed, err := ios.TerminalSession.Confirm(prompt)
	if err != nil {
		return false, fmt.Errorf("confirmation: %w", err)
	}
	if !confirmed {
		return false, nil
	}
	return applyUpdate(ctx, ios, status)
}

func applyUpdate(ctx context.Context, ios IO, status updatecheck.Status) (bool, error) {
	if ios.BinaryUpdater == nil {
		return false, errors.New("binary updater is unavailable")
	}
	diagnostics.Printf(ctx, 1, "applying verified CLI update")
	if err := ios.BinaryUpdater.Apply(ctx, status); err != nil {
		var staged *selfupdate.StagedNightly
		if errors.As(err, &staged) {
			fmt.Fprintln(ios.Stderr, staged.Error())
			return false, nil
		}
		return false, err
	}
	fmt.Fprintf(ios.Stderr, "Hikyo %s is verified and updated in place. Restart the command to use it.\n",
		status.LatestVersion)
	return true, nil
}
