package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"k8s.io/apimachinery/pkg/types"
)

type deploymentProbe struct {
	mu          sync.Mutex
	identity    DeploymentIdentity
	installed   bool
	sent        map[uint64]bool
	submissions int
}

func (p *deploymentProbe) RootPreparation(context.Context, configrollout.SignedCommand) (*crypto.WrappedKey, error) {
	return nil, nil
}
func (p *deploymentProbe) SeedSources(context.Context) (config.ManagedBootstrapSources, error) {
	return config.ManagedBootstrapSources{}, nil
}
func (p *deploymentProbe) Identity() DeploymentIdentity {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.identity
}
func (p *deploymentProbe) PrepareCommand(_ context.Context, intent configrollout.Intent, _ *runtimeconfig.Bundle, sequence uint64) (configrollout.SignedCommand, error) {
	return configrollout.SignedCommand{Command: configrollout.Command{EnrollmentID: p.Identity().EnrollmentID, Sequence: sequence, Action: configrollout.ActionPrepare, Intent: intent}, Signature: make([]byte, 64)}, nil
}
func (p *deploymentProbe) DecisionCommand(_ context.Context, prepared configrollout.SignedCommand, action configrollout.Action, sequence uint64, digest string, ack *configrollout.ApplicationAcknowledgement) (configrollout.SignedCommand, error) {
	return configrollout.SignedCommand{Command: configrollout.Command{EnrollmentID: p.Identity().EnrollmentID, Sequence: sequence, Action: action, Intent: prepared.Command.Intent, PlanDigest: digest, Acknowledgement: ack}, Signature: make([]byte, 64)}, nil
}
func (p *deploymentProbe) RenewCommand(_ context.Context, committed configrollout.SignedCommand, sequence uint64) (configrollout.SignedCommand, error) {
	committed.Command.Sequence = sequence
	committed.Command.IssuedAt = time.Now().UTC()
	committed.Command.ExpiresAt = committed.Command.IssuedAt.Add(5 * time.Minute)
	return committed, nil
}
func (p *deploymentProbe) Send(_ context.Context, c configrollout.SignedCommand) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.sent[c.Command.Sequence] && c.Command.Action == configrollout.ActionSubmit {
		p.submissions++
	}
	p.sent[c.Command.Sequence] = true
	return nil
}
func (p *deploymentProbe) Response(_ context.Context, c configrollout.SignedCommand) (configrollout.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.sent[c.Command.Sequence] {
		return configrollout.Response{}, configrollout.ErrNotSubmitted
	}
	response := configrollout.Response{EnrollmentID: p.identity.EnrollmentID, Sequence: c.Command.Sequence, Outcome: "complete", PlanDigest: strings.Repeat("a", 64), TemplateStamp: "replacement-template"}
	if c.Command.Action != configrollout.ActionPrepare {
		phase := configrollout.RolloutRequested
		if p.installed {
			phase = configrollout.RolloutReady
		}
		if c.Command.Acknowledgement != nil && p.installed {
			phase = configrollout.Applied
		}
		response.Receipt = &configrollout.Receipt{Intent: c.Command.Intent, PlanDigest: c.Command.PlanDigest, DeploymentUID: types.UID(p.identity.DeploymentUID), Phase: phase, ApplicationAcknowledged: phase == configrollout.Applied}
	}
	return response, nil
}
func (p *deploymentProbe) VerifyInstalled(_ context.Context, bundle *runtimeconfig.Bundle) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if bundle.BootstrapSources() == (config.ManagedBootstrapSources{}) || p.installed {
		return nil
	}
	return ErrDeploymentSourcesPending
}

func TestSelfConfigDeploymentCommitsBeforeSendingAndRequiresApplicationAck(t *testing.T) {
	testSelfConfigDeploymentAuthorization(t, "database_source")
}

func TestSelfConfigUpgradeDeploymentRequiresExactMFAAndApplicationAck(t *testing.T) {
	testSelfConfigDeploymentAuthorization(t, "upgrade_source")
}

func testSelfConfigDeploymentAuthorization(t *testing.T, sourceKey string) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, _ := installerFixture(t, engine)
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			actor, session := selfConfigSession(t, s, local)
			status, err := s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			owner, inc, err := s.DB.RecoveryIdentity()
			if err != nil {
				t.Fatal(err)
			}
			deployment := &deploymentProbe{identity: DeploymentIdentity{EnrollmentID: "enrolled", OwnerInstanceID: owner, Incarnation: inc, DeploymentUID: "deployment-uid", TemplateStamp: "original-template"}, sent: make(map[uint64]bool)}
			s.Deployment = deployment
			scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
			draft, err := (&Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}).Set(t.Context(), local, scope, config.ManagedBootstrapSourcesKey, fmt.Sprintf(`{"version":1,%q:"replacement"}`, sourceKey), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := (&Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}).PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{draft.VersionID}}); err != nil {
				t.Fatal(err)
			}
			status, err = s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			req := installerRequest(status, "bootstrap-plan")
			req.PrepareOnly = true
			done := beginInstallerApply(t, s, actor, req)
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			result := awaitInstallerApply(t, done)
			if result.err != nil || result.status.Job.PlanDigest != strings.Repeat("a", 64) {
				t.Fatalf("plan missing: %v", result.err)
			}
			deployment.mu.Lock()
			submitted := deployment.submissions
			deployment.mu.Unlock()
			if submitted != 0 {
				t.Fatal("preview changed deployment")
			}
			req.PrepareOnly = false
			req.PlanDigest = result.status.Job.PlanDigest
			selfConfigReauthenticate(t, s, session, SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: owner, Revision: req.Revision, SchemaVersion: req.SchemaVersion, ExpectedGeneration: req.ExpectedGeneration, PlanDigest: req.PlanDigest})
			wrong := req
			wrong.PlanDigest = strings.Repeat("b", 64)
			if _, err := s.Apply(t.Context(), actor, wrong); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("changed plan accepted: %v", err)
			}
			if _, err := s.Apply(t.Context(), actor, req); err != nil {
				t.Fatal(err)
			}
			deployment.mu.Lock()
			submitted = deployment.submissions
			deployment.mu.Unlock()
			if submitted != 0 {
				t.Fatal("request sent before worker reconciled committed intent")
			}
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
				t.Fatal("old process acknowledged replacement source")
			}
			deployment.mu.Lock()
			deployment.installed = true
			deployment.identity.TemplateStamp = "replacement-template"
			deployment.mu.Unlock()
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			current, err := s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			if current.Job.State == "completed" {
				t.Fatal("runtime swap substituted for executor application receipt")
			}
			for i := 0; i < 3; i++ {
				if err := s.ReconcileRuntime(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			current, err = s.Status(t.Context(), local)
			if err != nil || current.Job.State != "completed" {
				t.Fatalf("exact acknowledgement did not finish: %v", err)
			}
			deployment.mu.Lock()
			submitted = deployment.submissions
			deployment.mu.Unlock()
			if submitted != 1 {
				t.Fatalf("idempotent command caused %d submissions", submitted)
			}
		})
	}
}
