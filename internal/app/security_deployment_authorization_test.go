package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitydeployment"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
)

type finalSecurityPreparation struct {
	staged  securitydeployment.Observation
	applies int
}

func (f *finalSecurityPreparation) Observe(context.Context) (securitydeployment.Observation, error) {
	return securitydeployment.Observation{}, errors.New("refused preparation must not wait for a replacement")
}
func (f *finalSecurityPreparation) Prepare(context.Context, securitydeployment.Target) (securitydeployment.Observation, error) {
	return f.staged, nil
}
func (f *finalSecurityPreparation) Apply(_ context.Context, target securitydeployment.Target) error {
	if target.BeforeMutation != nil {
		if err := target.BeforeMutation(); err != nil {
			return errors.Join(securitydeployment.ErrPreconditionRefused, err)
		}
	}
	f.applies++
	return errors.New("unexpected mutation")
}

func TestSecurityDeploymentFinalPreparationCannotAdoptChangedSource(t *testing.T) {
	for name, change := range map[string]func(*securitydeployment.Observation){
		"platform":    func(o *securitydeployment.Observation) { o.Platform = "unsupported/architecture" },
		"engine":      func(o *securitydeployment.Observation) { o.Engine = releaseidentity.SQLite },
		"identity":    func(o *securitydeployment.Observation) { o.Identity.Sequence++ },
		"instance":    func(o *securitydeployment.Observation) { o.Instance = "another-installation" },
		"generation":  func(o *securitydeployment.Observation) { o.Generation++ },
		"restore":     func(o *securitydeployment.Observation) { o.RestoreEpoch++ },
		"incarnation": func(o *securitydeployment.Observation) { o.Incarnation = strings.Repeat("c", 64) },
		"image":       func(o *securitydeployment.Observation) { o.Image += "changed" },
		"runtime":     func(o *securitydeployment.Observation) { o.RuntimeImageSHA256 = strings.Repeat("d", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			target := securitymaintenance.Target{Identity: nativeSecurityIdentity(releaseidentity.StableV1), Image: "ghcr.io/example/hikyo@sha256:" + strings.Repeat("a", 64), EligibilityUntil: time.Now().Add(time.Hour)}
			source := securitydeployment.Observation{Engine: releaseidentity.Postgres, Identity: target.Identity, Instance: "installation", Incarnation: strings.Repeat("b", 64), Generation: 4, Image: target.Image, RuntimeImageSHA256: strings.Repeat("a", 64), Phase: "healthy", Ready: true, StagedTarget: target.Identity, RouteSHA256: releaseidentity.Hash([]byte("same route"))}
			source.Identity.Version, source.Identity.Sequence = "1.2.0", 7
			executor := &finalSecurityPreparation{staged: source}
			change(&executor.staged)
			driver := &securityDeploymentDriver{executor: executor, routes: map[releaseidentity.Identity]releaseidentity.Digest{target.Identity: source.RouteSHA256}, sources: map[releaseidentity.Identity]securitymaintenance.Observation{target.Identity: maintenanceObservation(source)}}
			// This exercises the final deployment check after independent
			// release verification, without replacing that verifier with a fake.
			err := driver.applyAuthorizedDeployment(t.Context(), target, securitydeployment.Target{Identity: target.Identity, Image: target.Image})
			if !errors.Is(err, securitymaintenance.ErrNotStarted) || executor.applies != 0 {
				t.Fatal("restaging adopted a new source before mutation", err)
			}
		})
	}
}
