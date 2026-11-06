package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitydeployment"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
)

// slowRestoreDeployment spends restoreUse of its window restoring, and its reads fail once their context is spent.
type slowRestoreDeployment struct {
	restoringSecurityDeployment
	restoreUse time.Duration
}

func (d *slowRestoreDeployment) RestoreSource(ctx context.Context, aborted securitydeployment.Observation) error {
	select {
	case <-ctx.Done():
	case <-time.After(d.restoreUse):
	}
	return d.restoringSecurityDeployment.RestoreSource(ctx, aborted)
}

func (d *slowRestoreDeployment) Observe(ctx context.Context) (securitydeployment.Observation, error) {
	if err := ctx.Err(); err != nil {
		return securitydeployment.Observation{}, err
	}
	return d.delayedSecurityDeployment.Observe(ctx)
}

// A restore that ends just inside its window still leaves the source a full window to come back.
func TestSecurityDeploymentProvesAnAbortedSourceOnAFreshWindowAfterTheRestore(t *testing.T) {
	identity := releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: "1.2.0", Sequence: 1, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compat")), ManifestSHA256: releaseidentity.Hash([]byte("manifest"))}
	image := "ghcr.io/example/hikyo@sha256:" + strings.Repeat("d", 64)
	source := securitymaintenance.Observation{Engine: releaseidentity.SQLite, Identity: identity, Instance: "example", Incarnation: strings.Repeat("b", 64), Generation: 4, Image: image, Phase: "healthy", Ready: true}
	aborted := securitydeployment.Observation{Engine: source.Engine, Identity: identity, Instance: source.Instance, Incarnation: source.Incarnation, Generation: source.Generation, Image: "ghcr.io/example/hikyo@sha256:" + strings.Repeat("c", 64), Phase: "backup-preparing", Maintenance: true, AbortedBeforeMutation: true}
	back := securitydeployment.Observation{Engine: source.Engine, Identity: identity, Instance: source.Instance, Incarnation: source.Incarnation, Generation: source.Generation, Image: image, Phase: "healthy", Ready: true}
	target := securitymaintenance.Target{Identity: identity}
	target.Identity.Version, target.Identity.Sequence = "1.3.1", 2
	const window = 5 * time.Second
	for name, run := range map[string]func(context.Context, *securityDeploymentDriver) error{
		"a later run": func(ctx context.Context, d *securityDeploymentDriver) error {
			return d.RestoreAborted(ctx, maintenanceObservation(aborted), securitymaintenance.Operation{Source: source})
		},
		"the applying run": func(ctx context.Context, d *securityDeploymentDriver) error {
			return d.waitForSecurityTarget(ctx, target, source)
		},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				executor := &slowRestoreDeployment{restoreUse: window - time.Millisecond}
				executor.observations = []securitydeployment.Observation{aborted, aborted, back}
				if name == "a later run" {
					executor.observations = executor.observations[1:]
				}
				err := run(t.Context(), &securityDeploymentDriver{executor: executor, timeout: window})
				if !errors.Is(err, securitymaintenance.ErrNotStarted) || executor.restored != 1 || executor.reconciled != 1 {
					t.Fatalf("the source was not proven after a restore that used its window: %v restored=%d reconciled=%d", err, executor.restored, executor.reconciled)
				}
			})
		})
	}
}

// Shutdown stops the wait for the source at once and keeps every pin, rather than outliving the stop signal.
func TestSecurityDeploymentSourceWaitEndsOnShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source, _, back, running := deadlineFixtureIdentities()
		back.Generation++ // never the source, so only shutdown or the window ends the wait
		executor := &scriptedSecurityDeployment{observe: func(int) (securitydeployment.Observation, error) { return running, nil }, restored: true, back: back}
		driver := &securityDeploymentDriver{executor: executor, timeout: time.Hour}
		ctx, stop := context.WithCancel(t.Context())
		defer stop()
		time.AfterFunc(3*time.Second, stop)
		start := time.Now()
		err := driver.RestoreAborted(ctx, maintenanceObservation(running), securitymaintenance.Operation{Source: source})
		if elapsed := time.Since(start); elapsed > time.Minute {
			t.Fatalf("the wait outlived shutdown by %s", elapsed)
		}
		if !errors.Is(err, context.Canceled) || errors.Is(err, securitymaintenance.ErrNotStarted) || executor.rolled != 1 || executor.reconciled != 0 {
			t.Fatalf("shutdown released the pin or skipped the rollback: %v rolled=%d reconciled=%d", err, executor.rolled, executor.reconciled)
		}
	})
}
