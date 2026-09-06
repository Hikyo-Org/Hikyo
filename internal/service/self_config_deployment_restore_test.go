package service

import (
	"context"
	"errors"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"k8s.io/apimachinery/pkg/types"
	"strings"
	"testing"
)

type restoreRaceProbe struct {
	BootstrapDeployment
	before func(context.Context) error
}

func (p *restoreRaceProbe) DecisionCommand(ctx context.Context, command configrollout.SignedCommand, action configrollout.Action, sequence uint64, digest string, ack *configrollout.ApplicationAcknowledgement) (configrollout.SignedCommand, error) {
	if err := p.before(ctx); err != nil {
		return configrollout.SignedCommand{}, err
	}
	return p.BootstrapDeployment.DecisionCommand(ctx, command, action, sequence, digest, ack)
}

func partialDeploymentFixture(t *testing.T, engine store.Engine) (*SelfConfig, Actor, Actor, string, *deploymentProbe, SelfConfigDeploymentRestoreRequest) {
	return committedDeploymentFixture(t, engine, true)
}

func committedDeploymentFixture(t *testing.T, engine store.Engine, reconcile bool) (*SelfConfig, Actor, Actor, string, *deploymentProbe, SelfConfigDeploymentRestoreRequest) {
	t.Helper()
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
	draft, err := (&Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}).Set(t.Context(), local, scope, config.ManagedBootstrapSourcesKey, `{"version":1,"database_source":"replacement"}`, nil)
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
	req := installerRequest(status, "restore-fixture")
	req.PrepareOnly = true
	pending := beginInstallerApply(t, s, actor, req)
	if err := s.ReconcileRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	prepared := awaitInstallerApply(t, pending)
	if prepared.err != nil {
		t.Fatal(prepared.err)
	}
	req.PrepareOnly = false
	req.PlanDigest = prepared.status.Job.PlanDigest
	selfConfigReauthenticate(t, s, session, SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: owner, Revision: req.Revision, ExpectedGeneration: req.ExpectedGeneration, SchemaVersion: req.SchemaVersion, PlanDigest: req.PlanDigest})
	applied, err := s.Apply(t.Context(), actor, req)
	if err != nil {
		t.Fatal(err)
	}
	if !reconcile {
		return s, local, actor, session, deployment, SelfConfigDeploymentRestoreRequest{Revision: req.Revision, ExpectedGeneration: applied.Generation, SchemaVersion: req.SchemaVersion, PlanDigest: req.PlanDigest}
	}
	if err := s.ReconcileRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	at, err := s.runtimeTimestamp(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = tx.Write(t.Context(), s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		return r.SelfConfig().FinishJob(ctx, p, applied.Job.ID, "partial", "convergence_timeout", at)
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, local, actor, session, deployment, SelfConfigDeploymentRestoreRequest{Revision: req.Revision, ExpectedGeneration: applied.Generation, SchemaVersion: req.SchemaVersion, PlanDigest: req.PlanDigest}
}

func TestSelfConfigDeploymentRestoreRequiresExactMFAAndJournalsBeforeSending(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, actor, session, probe, req := partialDeploymentFixture(t, engine)
			// A rollout can be partial at the executor while its application graph
			// is already serving. The restore commit must durably fence that graph.
			probe.mu.Lock()
			probe.installed = true
			probe.identity.TemplateStamp = "replacement-template"
			probe.mu.Unlock()
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Capture(t.Context()); err != nil {
				t.Fatalf("fixture runtime did not install: %v", err)
			}
			owner, _, err := s.DB.RecoveryIdentity()
			if err != nil {
				t.Fatal(err)
			}
			target := SelfConfigReauthTarget{Action: "rollout-restore", OwnerInstanceID: owner, Revision: req.Revision, ExpectedGeneration: req.ExpectedGeneration, SchemaVersion: req.SchemaVersion, PlanDigest: req.PlanDigest}
			if _, err := s.RestoreDeployment(t.Context(), actor, req); err == nil {
				t.Fatal("restore without fresh factor accepted")
			}
			applyProof := target
			applyProof.Action = "apply"
			selfConfigReauthenticate(t, s, session, applyProof)
			if _, err := s.RestoreDeployment(t.Context(), actor, req); err == nil {
				t.Fatal("Apply proof authorized restoration")
			}
			selfConfigReauthenticate(t, s, session, target)
			wrong := req
			wrong.PlanDigest = strings.Repeat("b", 64)
			if _, err := s.RestoreDeployment(t.Context(), actor, wrong); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("wrong plan: %v", err)
			}
			wrong = req
			wrong.ExpectedGeneration++
			if _, err := s.RestoreDeployment(t.Context(), actor, wrong); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("wrong generation: %v", err)
			}
			cancelled, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := s.RestoreDeployment(cancelled, actor, req); err == nil {
				t.Fatal("cancelled restore accepted")
			}
			// A racing worker may advance the rollout journal after signing. The
			// final CAS must refuse without spending the still-exact MFA window.
			s.Deployment = &restoreRaceProbe{BootstrapDeployment: probe, before: func(ctx context.Context) error {
				return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
					p, err := az.SelfConfigRuntimeAuthority(ctx, "")
					if err != nil {
						return err
					}
					jobs, err := r.SelfConfig().Jobs(ctx, p)
					if err != nil {
						return err
					}
					row, err := r.SelfConfig().Rollout(ctx, p, jobs[0].ID)
					if err != nil {
						return err
					}
					return r.SelfConfig().PutRollout(ctx, p, row)
				})
			}}
			if _, err := s.RestoreDeployment(t.Context(), actor, req); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("racing journal accepted: %v", err)
			}
			s.Deployment = probe
			before, err := s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			probe.mu.Lock()
			sent := len(probe.sent)
			probe.mu.Unlock()
			restored, err := s.RestoreDeployment(t.Context(), actor, req)
			if err != nil {
				t.Fatal(err)
			}
			if restored.Generation != before.Generation || *restored.DesiredRevision != *before.DesiredRevision || restored.Job.State != "partial" {
				t.Fatal("restoration changed the runtime target")
			}
			if !restored.Job.DeploymentRestorePending || restored.Job.DeploymentRestored {
				t.Fatal("restoration claimed controller completion")
			}
			var command configrollout.SignedCommand
			var version int64
			read := func() {
				t.Helper()
				err := tx.Read(t.Context(), s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
					p, err := az.SelfConfigRuntimeAuthority(ctx, "")
					if err != nil {
						return err
					}
					row, err := r.SelfConfig().Rollout(ctx, p, restored.Job.ID)
					if err != nil {
						return err
					}
					version = row.RowVersion
					command, err = decodeRolloutCommand(row.CommandJSON)
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			read()
			if command.Command.Action != configrollout.ActionRestore || command.Command.PlanDigest != req.PlanDigest {
				t.Fatal("wrong durable restore command")
			}
			originalVersion := version
			probe.mu.Lock()
			after := len(probe.sent)
			probe.mu.Unlock()
			if after != sent {
				t.Fatal("restore request sent before durable worker reconciliation")
			}
			if _, err := s.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
				t.Fatal("restore enabled business configuration")
			}
			if _, err := s.RestoreDeployment(t.Context(), actor, req); err != nil {
				t.Fatal(err)
			}
			read()
			if version != originalVersion {
				t.Fatal("idempotent retry replaced authorized command")
			}
		})
	}
}

func TestSelfConfigRolloutRestoreIntentIsDistinct(t *testing.T) {
	target := SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: "instance", Revision: 2, ExpectedGeneration: 2, SchemaVersion: 1, PlanDigest: strings.Repeat("a", 64)}
	apply, err := NewSelfConfigReauthIntent(target)
	if err != nil {
		t.Fatal(err)
	}
	target.Action = "rollout-restore"
	restore, err := NewSelfConfigReauthIntent(target)
	if err != nil {
		t.Fatal(err)
	}
	if apply.keySet == restore.keySet || apply.selfConfigBinding == restore.selfConfigBinding {
		t.Fatal("restore reused Apply authorization")
	}
	target.ConfirmRestoredCredentials = true
	if _, err := NewSelfConfigReauthIntent(target); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("restore consumed restored-credential acknowledgement")
	}
	target.ConfirmRestoredCredentials = false
	target.PlanDigest = ""
	if _, err := NewSelfConfigReauthIntent(target); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("restore accepted unbound plan")
	}
}

// The controller confirms only external restoration. Its currently installed
// source stays original until another separately authorized rollout happens.
type restoredDeploymentProbe struct{ *deploymentProbe }

func (p *restoredDeploymentProbe) VerifyInstalled(_ context.Context, bundle *runtimeconfig.Bundle) error {
	if bundle.BootstrapSources().DatabaseSource == "original" {
		return nil
	}
	return ErrDeploymentSourcesPending
}
func (p *restoredDeploymentProbe) Response(ctx context.Context, command configrollout.SignedCommand) (configrollout.Response, error) {
	if command.Command.Action != configrollout.ActionRestore {
		return p.deploymentProbe.Response(ctx, command)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.sent[command.Command.Sequence] {
		return configrollout.Response{}, configrollout.ErrNotSubmitted
	}
	return configrollout.Response{Outcome: "complete", PlanDigest: command.Command.PlanDigest, Receipt: &configrollout.Receipt{Intent: command.Command.Intent, PlanDigest: command.Command.PlanDigest, DeploymentUID: types.UID(p.identity.DeploymentUID), Phase: configrollout.Restored}}, nil
}
func TestSelfConfigDeploymentRestoreAllowsRepairThenAnotherRollout(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, actor, session, probe, restore := partialDeploymentFixture(t, engine)
			s.Deployment = &restoredDeploymentProbe{probe}
			status, err := s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
			publish := func(value string) {
				t.Helper()
				draft, err := (&Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}).Set(t.Context(), local, scope, config.ManagedBootstrapSourcesKey, value, nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := (&Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}).PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{draft.VersionID}}); err != nil {
					t.Fatal(err)
				}
			}
			publish(`{"version":1,"database_source":"original"}`)
			status, err = s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			repair := installerRequest(status, "repair-after-restore")
			repair.PrepareOnly = true
			if _, err := s.Apply(t.Context(), actor, repair); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("nonterminal rollout superseded: %v", err)
			}
			selfConfigReauthenticate(t, s, session, SelfConfigReauthTarget{Action: "rollout-restore", OwnerInstanceID: status.OwnerInstanceID, Revision: restore.Revision, ExpectedGeneration: restore.ExpectedGeneration, SchemaVersion: restore.SchemaVersion, PlanDigest: restore.PlanDigest})
			if _, err := s.RestoreDeployment(t.Context(), actor, restore); err != nil {
				t.Fatal(err)
			}
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			status, err = s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			if !status.Job.DeploymentRestored || status.Job.State != "partial" {
				t.Fatal("controller restoration substituted for runtime apply")
			}
			if _, err := s.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
				t.Fatal("restoration alone resumed runtime")
			}
			pending := beginInstallerApply(t, s, actor, repair)
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			prepared := awaitInstallerApply(t, pending)
			if prepared.err != nil {
				t.Fatal(prepared.err)
			}
			if prepared.status.Job.PlanDigest != "" {
				t.Fatal("repair of installed source required another deployment")
			}
			repair.PrepareOnly = false
			selfConfigReauthenticate(t, s, session, SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: status.OwnerInstanceID, Revision: repair.Revision, ExpectedGeneration: repair.ExpectedGeneration, SchemaVersion: repair.SchemaVersion})
			if _, err := s.Apply(t.Context(), actor, repair); err != nil {
				t.Fatal(err)
			}
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			status, err = s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			if status.Job.State != "completed" {
				t.Fatalf("repair not applied: %s", status.Job.State)
			}
			publish(`{"version":1,"database_source":"replacement-2"}`)
			status, err = s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			next := installerRequest(status, "next-after-restore")
			next.PrepareOnly = true
			pending = beginInstallerApply(t, s, actor, next)
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			prepared = awaitInstallerApply(t, pending)
			if prepared.err != nil {
				t.Fatal(prepared.err)
			}
			if !prepared.status.Job.Prepared || prepared.status.Job.PlanDigest == "" {
				t.Fatal("later rollout remained stranded by restored plan")
			}
		})
	}
}
