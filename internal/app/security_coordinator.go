package app

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/securitydeployment"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/Masterminds/semver/v3"
)

// coordinatorFollower moves a coordinator's own executable to the release of the
// server image it has proven healthy: a host coordinator replaces its binary, and
// an in-cluster coordinator moves its CronJob to the image. An update replaces the
// image but never the packaged binary running it, so a flaw in the coordinator
// itself, or a later catalog format, would otherwise outlive every fix. Native
// hosts update their own binary. A failure here is only logged: the update is
// already proven, and the next run retries.
//
// The running release is reported by the serving container, which a compromise
// controls, so it names nothing until the image the deployment owner observed is
// found in that release's signed inventory and the catalog has not withdrawn it.
type coordinatorFollower struct {
	own     func() (upgradecompat.Declaration, error)
	check   func() error
	prepare func(context.Context, releaseidentity.Identity, string) (preparedSecurityRelease, error)
	bind    func(releasetrust.VerifiedRelease, securitymaintenance.Target) error
	replace func(context.Context, preparedSecurityRelease, string) error
	// followed stops a long-lived `run` process, whose embedded release never
	// changes, from fetching the same release again after replacing its file.
	followed releaseidentity.Identity
}

// newCoordinatorFollower serves the host coordinator, or the in-cluster one when
// driver is the deployment driver of an in-cluster configuration.
func newCoordinatorFollower(c SecurityMaintenanceConfiguration, driver *securityDeploymentDriver) *coordinatorFollower {
	f := &coordinatorFollower{
		own:     func() (upgradecompat.Declaration, error) { _, d, err := buildcompat.Current(); return d, err },
		check:   func() error { return selfupdate.CheckExecutable(securitydeployment.RequireInstalledExecutable) },
		prepare: prepareSecurityRelease,
		bind:    signedSecurityImage,
		replace: func(ctx context.Context, p preparedSecurityRelease, _ string) error {
			return p.Installer.ReplaceWithPrepared(ctx, p.Target, securitydeployment.RequireInstalledExecutable)
		},
	}
	if c.inCluster() {
		// The pod's own file system is read-only and no host root is involved.
		f.check = func() error { return nil }
		f.replace = func(ctx context.Context, _ preparedSecurityRelease, image string) error {
			return driver.FollowCoordinator(ctx, image)
		}
	}
	return f
}

// afterRun has no result on purpose: it cannot change the engine's outcome. Only
// an applying run reaches it, so check, enroll and reconcile never change a workload.
func (f *coordinatorFollower) afterRun(ctx context.Context, c SecurityMaintenanceConfiguration, state securitymaintenance.Status) {
	if runtime.GOOS != "linux" || c.Kind == "native" || c.inCluster() && c.Deployment.Kubernetes.CronJob == "" {
		return
	}
	if err := f.follow(ctx, c.downloadCache(), state); err != nil {
		slog.Warn("security maintenance coordinator executable was not moved to the running release; retrying on the next run", "err", err)
	}
}

func (f *coordinatorFollower) follow(ctx context.Context, cache string, state securitymaintenance.Status) error {
	// Only a quiet, healthy installation: an operation in progress or a failed
	// journal means the running release is not yet settled.
	if state.Operation != nil || state.Running == nil || !state.Running.Healthy() || state.Phase != "current" && state.Phase != "healthy" {
		return nil
	}
	running := state.Running.Identity
	if running == f.followed {
		return nil
	}
	own, err := f.own()
	if err != nil {
		slog.Info("security maintenance coordinator has no release identity; not replacing its executable", "err", err)
		return nil
	}
	ownVersion, err := semver.StrictNewVersion(own.Version)
	if err != nil {
		return err
	}
	runningVersion, err := semver.StrictNewVersion(running.Version)
	if err != nil {
		return err
	}
	// Ordering by version, not trust profile, so a nightly or stable image never
	// pulls the coordinator backwards.
	if releaseidentity.CompareUpdateVersions(ownVersion, runningVersion) >= 0 {
		return nil
	}
	if state.RunningWithdrawn {
		return fmt.Errorf("release %s is withdrawn by the security catalog", running.Version)
	}
	if err := f.check(); err != nil {
		slog.Info("security maintenance coordinator executable is not a root-installed file; not replacing it", "err", err)
		return nil
	}
	prepared, err := f.prepare(ctx, running, cache)
	if err != nil {
		return fmt.Errorf("prepare release %s: %w", running.Version, err)
	}
	if err := f.bind(prepared.Release, securitymaintenance.Target{Identity: running, Image: state.Running.Image}); err != nil {
		return fmt.Errorf("running image is not release %s: %w", running.Version, err)
	}
	if err := f.replace(ctx, prepared, state.Running.Image); err != nil {
		return err
	}
	f.followed = running
	slog.Info("security maintenance coordinator executable now matches the running release", "version", running.Version)
	return nil
}
