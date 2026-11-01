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

type delayedSecurityDeployment struct {
	observations []securitydeployment.Observation
	calls        int
}

func (d *delayedSecurityDeployment) Prepare(context.Context, securitydeployment.Target) (securitydeployment.Observation, error) {
	return securitydeployment.Observation{}, errors.New("unused staging")
}
func (d *delayedSecurityDeployment) Apply(context.Context, securitydeployment.Target) error {
	return errors.New("unused mutation")
}
func (d *delayedSecurityDeployment) Observe(context.Context) (securitydeployment.Observation, error) {
	index := d.calls
	d.calls++
	if index >= len(d.observations) {
		index = len(d.observations) - 1
	}
	return d.observations[index], nil
}

func TestSecurityDeploymentWaitsForActualNewProcessReadiness(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		identity := releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: "1.3.1", Sequence: 2, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compat")), ManifestSHA256: releaseidentity.Hash([]byte("manifest"))}
		source := securitymaintenance.Observation{Engine: releaseidentity.SQLite, Identity: identity, Instance: "example", Incarnation: strings.Repeat("b", 64), Generation: 4, Phase: "healthy", Ready: true}
		source.Identity.Version = "1.2.0"
		source.Identity.Sequence = 1
		target := securitymaintenance.Target{Identity: identity, Image: "ghcr.io/example/hikyo@sha256:" + strings.Repeat("c", 64), EligibilityUntil: time.Now().Add(500 * time.Millisecond)}
		old := securitydeployment.Observation{Engine: source.Engine, Identity: source.Identity, Instance: source.Instance, Incarnation: source.Incarnation, Generation: source.Generation, Phase: "healthy", Ready: true}
		starting := old
		starting.Identity = identity
		starting.Image = target.Image
		starting.Generation++
		starting.Phase = "schema-applied"
		starting.Maintenance = true
		starting.Ready = false
		healthy := starting
		healthy.Phase = "healthy"
		healthy.Maintenance = false
		healthy.Ready = true
		executor := &delayedSecurityDeployment{observations: []securitydeployment.Observation{old, starting, healthy}}
		driver := &securityDeploymentDriver{executor: executor, timeout: 10 * time.Second}
		if err := driver.waitForSecurityTarget(t.Context(), target, source); err != nil {
			t.Fatal(err)
		}
		if executor.calls != 3 {
			t.Fatalf("did not distinguish old ready/starting/target ready: %d", executor.calls)
		}
		if time.Now().Before(target.EligibilityUntil) {
			t.Fatal("fixture did not cross admission expiry during already-started operation")
		}
		// Metadata expiry after an admitted stop does not cancel the same
		// target's migration/readiness work or authorize an old-image restart.
	})
}

func TestSecurityDeploymentWaitRejectsRestoreAndNeverReadyTarget(t *testing.T) {
	for _, restore := range []bool{false, true} {
		t.Run(map[bool]string{false: "never ready", true: "restore"}[restore], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				identity := releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: "1.3.1", Sequence: 2, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compat")), ManifestSHA256: releaseidentity.Hash([]byte("manifest"))}
				source := securitymaintenance.Observation{Engine: releaseidentity.SQLite, Identity: identity, Instance: "example", Incarnation: strings.Repeat("b", 64), Generation: 4, Phase: "healthy", Ready: true}
				observation := securitydeployment.Observation{Engine: source.Engine, Identity: identity, Instance: source.Instance, Incarnation: source.Incarnation, Generation: 5, Phase: "schema-applied", Maintenance: true}
				if restore {
					observation.RestoreEpoch = 1
				}
				executor := &delayedSecurityDeployment{observations: []securitydeployment.Observation{observation}}
				driver := &securityDeploymentDriver{executor: executor, timeout: 3 * time.Second}
				if err := driver.waitForSecurityTarget(t.Context(), securitymaintenance.Target{Identity: identity}, source); err == nil {
					t.Fatal("unproven running target accepted")
				}
				if restore && executor.calls != 1 {
					t.Fatal("restore incarnation was retried")
				}
			})
		})
	}
}
