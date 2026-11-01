package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
)

// AutomaticUpgradeRefusedExitCode preserves a proven pre-mutation refusal
// through re-execution of an independently verified upgrade coordinator.
const AutomaticUpgradeRefusedExitCode = 23

func AutomaticUpgradeExitCode(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, securitymaintenance.ErrNotStarted) {
		return AutomaticUpgradeRefusedExitCode
	}
	return 1
}

// AutomaticUpgradeHandoffError is only for the independently verified child,
// never an arbitrary host subprocess whose exit status may also be 23.
func AutomaticUpgradeHandoffError(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == AutomaticUpgradeRefusedExitCode {
		return errors.Join(securitymaintenance.ErrNotStarted, errors.New("verified native coordinator refused before workload mutation"))
	}
	return err
}

// RunSecurityUpgrade runs the native coordinator for an independently selected
// exact release. The selection is a claim, not artifact authority: the normal
// release verifier, compatibility route and recovery gates still apply. Legacy
// passphrase custody requires an explicit one-time operator migration.
// AutomaticHandoff is returned to the command owner for trusted re-execution.
func RunSecurityUpgrade(ctx context.Context, target releaseidentity.Identity, configuration string, out io.Writer) error {
	return runNativeSecurityUpgrade(ctx, target, configuration, nativeSecurityAuthorization{}, out)
}

func runNativeSecurityUpgrade(ctx context.Context, target releaseidentity.Identity, configuration string, authorization nativeSecurityAuthorization, out io.Writer) error {
	if err := target.Validate(); err != nil {
		return fmt.Errorf("security upgrade target: %w", err)
	}
	raw, err := json.Marshal(target)
	if err != nil {
		return err
	}
	args := []string{"--release-identity", string(raw)}
	if authorization.Route != "" {
		if err := authorization.Route.Validate(); err != nil {
			return fmt.Errorf("security upgrade route: %w", err)
		}
		args = append(args, "--route-sha256", string(authorization.Route))
	}
	if !authorization.Until.IsZero() {
		args = append(args, "--eligibility-until", authorization.Until.UTC().Format(time.RFC3339Nano))
	}
	if authorization.PolicyFile != "" {
		args = append(args, "--maintenance-policy-file", authorization.PolicyFile, "--maintenance-policy-sha256", string(authorization.PolicySHA256))
	}
	if configuration != "" {
		args = append(args, "--config", configuration)
	}
	args, err = appendNativeSecuritySource(args, authorization.Source, &target)
	if err != nil {
		return err
	}
	return RunAutomaticUpgrade(ctx, args, out, nil)
}

func parseAutomaticRouteDigest(raw string, supplied bool, expected *releaseidentity.Identity) (releaseidentity.Digest, error) {
	if !supplied {
		return "", nil
	}
	if expected == nil {
		return "", errors.New("--route-sha256 requires --release-identity")
	}
	digest := releaseidentity.Digest(raw)
	if err := digest.Validate(); err != nil {
		return "", fmt.Errorf("--route-sha256: %w", err)
	}
	return digest, nil
}

func parseAutomaticEligibility(raw string, supplied bool, expected *releaseidentity.Identity) (time.Time, error) {
	if !supplied {
		return time.Time{}, nil
	}
	if expected == nil {
		return time.Time{}, errors.New("--eligibility-until requires --release-identity")
	}
	if len(raw) == 0 || len(raw) > 128 {
		return time.Time{}, errors.New("--eligibility-until requires a bounded RFC3339 timestamp")
	}
	until, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || until.IsZero() {
		return time.Time{}, errors.New("--eligibility-until requires a nonzero RFC3339 timestamp")
	}
	return until.UTC(), nil
}

func requireAutomaticEligibility(until, now time.Time) error {
	if !until.IsZero() && !now.Before(until) {
		return errors.Join(securitymaintenance.ErrNotStarted, errors.New("security update eligibility expired before service maintenance; refresh the signed feed and retry"))
	}
	return nil
}

func authorizeAutomaticMutation(until time.Time, now func() time.Time, authorize func() error) error {
	if err := requireAutomaticEligibility(until, now()); err != nil {
		return err
	}
	if authorize != nil {
		if err := authorize(); err != nil {
			return err
		}
	}
	return requireAutomaticEligibility(until, now())
}

func requireAutomaticRouteDigest(route automaticRoute, expected releaseidentity.Digest) error {
	if expected != "" && (!route.Plan.Valid() || route.Plan.Digest() != expected) {
		return errors.New("authenticated upgrade route differs from the independently staged security route")
	}
	return nil
}

func parseAutomaticReleaseIdentity(raw string, supplied, targetSupplied bool) (*releaseidentity.Identity, error) {
	if !supplied {
		return nil, nil
	}
	if targetSupplied {
		return nil, errors.New("--release-identity and --target are mutually exclusive")
	}
	var identity releaseidentity.Identity
	if len(raw) == 0 || len(raw) > 4096 {
		return nil, errors.New("--release-identity requires a bounded exact release identity JSON object")
	}
	if err := definitions.DecodeStrict([]byte(raw), &identity); err != nil {
		return nil, fmt.Errorf("--release-identity: %w", err)
	}
	if err := identity.Validate(); err != nil {
		return nil, fmt.Errorf("--release-identity: %w", err)
	}
	return &identity, nil
}

// resolveAutomaticReleaseIdentity never lets a new selection replace an
// unfinished operation. Stable operations also remain resumable through the
// ordinary command without turning its default nightly selector into stable.
func resolveAutomaticReleaseIdentity(version string, expected *releaseidentity.Identity, previous *automaticJournal) (string, *releaseidentity.Identity, error) {
	if previous == nil || previous.Phase == "complete" {
		return version, expected, nil
	}
	if expected != nil && *expected != previous.Target || version != "" && version != previous.Target.Version {
		return "", nil, errors.New("unfinished upgrade pins a different exact target; rerun sudo hikyo upgrade to reconcile it")
	}
	if expected == nil && previous.Target.Profile == releaseidentity.StableV1 {
		identity := previous.Target
		expected = &identity
	}
	return previous.Target.Version, expected, nil
}

type exactAutomaticReleasePreparer interface {
	PrepareReleaseSource(context.Context, updatecheck.Status, releaseidentity.Identity) (selfupdate.PreparedNightly, error)
}

func selectExactAutomaticRelease(ctx context.Context, source automaticReleaseSource, expected releaseidentity.Identity) (updatecheck.Status, error) {
	if err := expected.Validate(); err != nil {
		return updatecheck.Status{}, err
	}
	release, err := source.ReleaseByVersion(ctx, expected.Version)
	if err != nil {
		return updatecheck.Status{}, err
	}
	if release.Version != expected.Version || !release.Immutable {
		return updatecheck.Status{}, errors.New("release lookup differs from the selected immutable exact target")
	}
	if expected.Profile == releaseidentity.NightlyV1 {
		return nightlyStatus(release)
	}
	if release.Prerelease {
		return updatecheck.Status{}, errors.New("selected stable security target is a prerelease")
	}
	return updatecheck.Status{Available: true, Channel: updatecheck.ChannelStable, LatestVersion: release.Version, Immutable: true, Assets: release.Assets, URL: release.URL}, nil
}

func prepareExactAutomaticRelease(ctx context.Context, source automaticReleaseSource, installer exactAutomaticReleasePreparer, expected releaseidentity.Identity) (selfupdate.PreparedNightly, error) {
	status, err := selectExactAutomaticRelease(ctx, source, expected)
	if err != nil {
		return selfupdate.PreparedNightly{}, err
	}
	prepared, err := installer.PrepareReleaseSource(ctx, status, expected)
	if err != nil {
		return selfupdate.PreparedNightly{}, err
	}
	if prepared.Identity != expected {
		return selfupdate.PreparedNightly{}, errors.New("prepared release differs from the selected exact security target")
	}
	return prepared, nil
}

func automaticReleaseHandoff(target selfupdate.PreparedNightly, configuration string, expected *releaseidentity.Identity) (*AutomaticHandoff, error) {
	return automaticReleaseHandoffWithAuthorization(target, configuration, expected, nativeSecurityAuthorization{})
}

func automaticReleaseHandoffWithAuthorization(target selfupdate.PreparedNightly, configuration string, expected *releaseidentity.Identity, authorization nativeSecurityAuthorization) (*AutomaticHandoff, error) {
	if target.Identity.Validate() != nil || target.BinaryPath == "" {
		return nil, errors.New("automatic handoff requires a verified exact release executable")
	}
	args := []string{"upgrade", "--config", configuration}
	if expected == nil {
		args = append(args, "--target", target.Identity.Version)
	} else {
		if *expected != target.Identity {
			return nil, errors.New("automatic handoff differs from the selected exact security target")
		}
		raw, err := json.Marshal(*expected)
		if err != nil {
			return nil, err
		}
		args = append(args, "--release-identity", string(raw))
	}
	if authorization.Route != "" {
		if expected == nil || authorization.Route.Validate() != nil {
			return nil, errors.New("automatic route-pinned handoff requires an exact release and valid route digest")
		}
		args = append(args, "--route-sha256", string(authorization.Route))
	}
	if !authorization.Until.IsZero() {
		if expected == nil {
			return nil, errors.New("automatic eligibility handoff requires an exact release")
		}
		args = append(args, "--eligibility-until", authorization.Until.UTC().Format(time.RFC3339Nano))
	}
	if authorization.PolicyFile != "" || authorization.PolicySHA256 != "" {
		path, digest, err := parseAutomaticSecurityPolicy(authorization.PolicyFile, string(authorization.PolicySHA256), true, expected)
		if err != nil {
			return nil, err
		}
		args = append(args, "--maintenance-policy-file", path, "--maintenance-policy-sha256", string(digest))
	}
	args, err := appendNativeSecuritySource(args, authorization.Source, expected)
	if err != nil {
		return nil, err
	}
	return &AutomaticHandoff{Executable: target.BinaryPath, Arguments: append(args, "--handoff")}, nil
}
