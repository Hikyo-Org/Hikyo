package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"k8s.io/apimachinery/pkg/types"
)

// Transport-only probe. Real scoped publication, exact MFA, transactions,
// participant correspondence, runtime admission and HA leases stay in use.
type topologyDeploymentProbe struct {
	*deploymentProbe
	actual                domain.SingletonTopology
	stamp                 string
	source, proposedStamp string
	initialCorrespondence bool
}

func (p *topologyDeploymentProbe) Identity() DeploymentIdentity {
	id := p.deploymentProbe.Identity()
	id.TemplateStamp = p.stamp
	return id
}
func (p *topologyDeploymentProbe) PrepareCommand(_ context.Context, intent configrollout.Intent, b *runtimeconfig.Bundle, sequence uint64) (configrollout.SignedCommand, error) {
	target := b.BootstrapSources().Topology
	if b.BootstrapSources().DatabaseSource != p.source {
		command := configrollout.Command{EnrollmentID: p.Identity().EnrollmentID, Sequence: sequence, Action: configrollout.ActionPrepare, Intent: intent, PreviousTemplateStamp: p.stamp, Bootstrap: &configrollout.BootstrapChanges{Database: &configrollout.SourceProof{Alias: b.BootstrapSources().DatabaseSource}}}
		if p.initialCorrespondence {
			command.Topology = &domain.SingletonTopologyChange{Before: p.actual, After: p.actual}
		}
		return configrollout.SignedCommand{Command: command, Signature: make([]byte, 64)}, nil
	}
	return configrollout.SignedCommand{Command: configrollout.Command{EnrollmentID: p.Identity().EnrollmentID, Sequence: sequence, Action: configrollout.ActionPrepare, Intent: intent, Topology: &domain.SingletonTopologyChange{Before: p.actual, After: target}, PreviousTemplateStamp: p.stamp}, Signature: make([]byte, 64)}, nil
}
func (p *topologyDeploymentProbe) DecisionCommand(ctx context.Context, c configrollout.SignedCommand, a configrollout.Action, n uint64, d string, ack *configrollout.ApplicationAcknowledgement) (configrollout.SignedCommand, error) {
	next, err := p.deploymentProbe.DecisionCommand(ctx, c, a, n, d, ack)
	next.Command.Topology = c.Command.Topology
	next.Command.PreviousTemplateStamp = c.Command.PreviousTemplateStamp
	return next, err
}
func (p *topologyDeploymentProbe) VerifyInstalled(_ context.Context, b *runtimeconfig.Bundle) error {
	target := b.BootstrapSources().Topology
	if (target.NodeID == "" || target == p.actual) && b.BootstrapSources().DatabaseSource == p.source {
		return nil
	}
	return ErrDeploymentSourcesPending
}
func (p *topologyDeploymentProbe) Response(ctx context.Context, c configrollout.SignedCommand) (configrollout.Response, error) {
	if c.Command.Action != configrollout.ActionRestore {
		response, err := p.deploymentProbe.Response(ctx, c)
		if c.Command.Action == configrollout.ActionPrepare && p.proposedStamp != "" {
			response.TemplateStamp = p.proposedStamp
		}
		return response, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.sent[c.Command.Sequence] {
		return configrollout.Response{}, configrollout.ErrNotSubmitted
	}
	return configrollout.Response{Outcome: "complete", PlanDigest: c.Command.PlanDigest, Receipt: &configrollout.Receipt{Intent: c.Command.Intent, PlanDigest: c.Command.PlanDigest, DeploymentUID: types.UID(p.identity.DeploymentUID), Phase: configrollout.Restored}}, nil
}
func topologyServiceFixture(t *testing.T, engine store.Engine) (*SelfConfig, Actor, Actor, string, *topologyDeploymentProbe) {
	t.Helper()
	s, local, _ := installerFixture(t, engine)
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	actor, session := selfConfigSession(t, s, local)
	owner, inc, err := s.DB.RecoveryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	p := &topologyDeploymentProbe{deploymentProbe: &deploymentProbe{identity: DeploymentIdentity{EnrollmentID: "enrolled", OwnerInstanceID: owner, Incarnation: inc, DeploymentUID: "deployment-uid", TemplateStamp: "original-template"}, sent: map[uint64]bool{}}, actual: domain.SingletonTopology{NodeID: s.NodeID}, stamp: "original-template"}
	s.Deployment = p
	return s, local, actor, session, p
}
func publishTopology(t *testing.T, s *SelfConfig, local Actor, target domain.SingletonTopology) SelfConfigStatus {
	t.Helper()
	status, err := s.Status(t.Context(), local)
	if err != nil {
		t.Fatal(err)
	}
	scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
	sources, err := json.Marshal(config.ManagedBootstrapSources{Version: 1, Topology: target})
	if err != nil {
		t.Fatal(err)
	}
	node, err := runtimeconfig.EncodeNodeOverrides(map[string]map[string]string{target.NodeID: {"HIKYO_LISTEN": "127.0.0.1:8080", "HIKYO_OPERATIONAL_LISTEN": "127.0.0.1:8081", "HIKYO_ADMISSION_BUDGET_MIB": "272"}})
	if err != nil {
		t.Fatal(err)
	}
	var versions []string
	for key, value := range map[string]string{config.ManagedBootstrapSourcesKey: string(sources), config.ManagedNodeOverridesKey: node} {
		v, err := (&Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}).Set(t.Context(), local, scope, key, value, nil)
		if err != nil {
			t.Fatal(err)
		}
		versions = append(versions, v.VersionID)
	}
	if _, err := (&Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}).PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: versions}); err != nil {
		t.Fatal(err)
	}
	status, err = s.Status(t.Context(), local)
	if err != nil {
		t.Fatal(err)
	}
	return status
}
func commitTopologyCandidate(t *testing.T, s *SelfConfig, actor Actor, session string, status SelfConfigStatus, id string) (SelfConfigStatus, SelfConfigApplyRequest) {
	t.Helper()
	req := installerRequest(status, id)
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
	if _, err := s.Apply(t.Context(), actor, req); err == nil {
		t.Fatal("topology commit accepted without exact MFA")
	}
	selfConfigReauthenticate(t, s, session, SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: status.OwnerInstanceID, Revision: req.Revision, SchemaVersion: req.SchemaVersion, ExpectedGeneration: req.ExpectedGeneration, PlanDigest: req.PlanDigest})
	done, err := s.Apply(t.Context(), actor, req)
	if err != nil {
		t.Fatal(err)
	}
	return done, req
}
func replacementTopologyService(t *testing.T, old *SelfConfig, p *topologyDeploymentProbe, target domain.SingletonTopology) *SelfConfig {
	t.Helper()
	p.mu.Lock()
	p.installed = true
	p.identity.TemplateStamp = "replacement-template"
	p.mu.Unlock()
	next := &SelfConfig{DB: old.DB, Keyring: old.Keyring, Auth: old.Auth, NodeID: target.NodeID, HAMode: target.HA, Deployment: &topologyDeploymentProbe{deploymentProbe: p.deploymentProbe, actual: target, stamp: "replacement-template"}, Installer: old.Installer}
	t.Cleanup(func() { _ = next.CloseRuntime() })
	for range 4 {
		if err := next.ReconcileRuntime(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := next.Capture(t.Context()); err != nil {
		t.Fatalf("replacement not serving: %v", err)
	}
	return next
}
func TestSelfConfigSingletonTopologyFencesOldIdentityAcrossOrdinaryApply(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, actor, session, p := topologyServiceFixture(t, engine)
			target := domain.SingletonTopology{HA: true, NodeID: "replacement"}
			status := publishTopology(t, s, local, target)
			_, _ = commitTopologyCandidate(t, s, actor, session, status, "topology-first")
			if _, err := s.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
				t.Fatal("old identity served committed topology")
			}
			next := replacementTopologyService(t, s, p, target)
			now, err := s.DB.Coordination().Now(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := s.DB.Coordination().UpsertNode(t.Context(), store.HANode{NodeID: s.NodeID, StartedAt: now, HeartbeatAt: now}); err == nil {
				t.Fatal("retired node renewed heartbeat")
			}
			if _, held, err := s.DB.Coordination().ClaimLease(t.Context(), "scheduler", s.NodeID, now, now.Add(time.Minute)); err == nil || held {
				t.Fatal("retired node claimed scheduler")
			}
			fence, held, err := s.DB.Coordination().ForSingletonProcess(target.NodeID, "replacement-template").ClaimLease(t.Context(), "scheduler", target.NodeID, now, now.Add(time.Minute))
			if err != nil || !held {
				t.Fatalf("replacement lease: %v", err)
			}
			ordinary := publishInstallerCandidate(t, next, local, "4")
			done, req := commitTopologyCandidate(t, next, actor, session, ordinary, "ordinary-after-topology")
			if req.PlanDigest != "" {
				t.Fatal("ordinary edit forced rollout")
			}
			for range 2 {
				if err := next.ReconcileRuntime(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			metadata, err := s.DB.Coordination().CurrentSelfConfigGeneration(t.Context())
			if err != nil || metadata.Topology == nil || metadata.Topology.After != target || metadata.Generation != done.Generation {
				t.Fatal("ordinary generation lost topology fence", err)
			}
			if _, err := s.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
				t.Fatal("ordinary apply revived old identity")
			}
			back := domain.SingletonTopology{NodeID: "local"}
			status = publishTopology(t, next, local, back)
			_, _ = commitTopologyCandidate(t, next, actor, session, status, "ha-disable")
			if held, err := s.DB.Coordination().ForSingletonProcess(target.NodeID, "replacement-template").RenewLease(t.Context(), "scheduler", target.NodeID, fence, now, now.Add(time.Minute)); err == nil || held {
				t.Fatal("HA disable retained old leadership")
			}
			if err := tx.Write(store.WithSingletonLease(t.Context(), "scheduler", target.NodeID, fence), s.DB, func(context.Context, store.Repos, *authz.TxAuthorizer) error { return nil }); !errors.Is(err, store.ErrSingletonLeaseLost) {
				t.Fatalf("old term retained transaction authority: %v", err)
			}
			final := replacementTopologyService(t, next, p, back)
			if _, err := final.Capture(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestSelfConfigSingletonTopologyRestoreRequiresFreshRepair(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, actor, session, p := topologyServiceFixture(t, engine)
			target := domain.SingletonTopology{HA: true, NodeID: "replacement"}
			status := publishTopology(t, s, local, target)
			committed, req := commitTopologyCandidate(t, s, actor, session, status, "topology-to-restore")
			at, err := s.runtimeTimestamp(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.Write(t.Context(), s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
				proof, err := az.SelfConfigRuntimeAuthority(ctx, "")
				if err != nil {
					return err
				}
				return r.SelfConfig().FinishJob(ctx, proof, committed.Job.ID, "partial", "convergence_timeout", at)
			}); err != nil {
				t.Fatal(err)
			}
			restore := SelfConfigDeploymentRestoreRequest{Revision: req.Revision, ExpectedGeneration: committed.Generation, SchemaVersion: req.SchemaVersion, PlanDigest: req.PlanDigest}
			if _, err := s.RestoreDeployment(t.Context(), actor, restore); err == nil {
				t.Fatal("restore without fresh MFA")
			}
			selfConfigReauthenticate(t, s, session, SelfConfigReauthTarget{Action: "rollout-restore", OwnerInstanceID: status.OwnerInstanceID, Revision: restore.Revision, ExpectedGeneration: restore.ExpectedGeneration, SchemaVersion: restore.SchemaVersion, PlanDigest: restore.PlanDigest})
			if _, err := s.RestoreDeployment(t.Context(), actor, restore); err != nil {
				t.Fatal(err)
			}
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
				t.Fatal("restoration resumed business")
			}
			now, err := s.DB.Coordination().Now(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if _, held, err := s.DB.Coordination().ClaimLease(t.Context(), "scheduler", target.NodeID, now, now.Add(time.Minute)); err == nil || held {
				t.Fatal("restoration resumed lease")
			}
			repair := publishTopology(t, s, local, p.actual)
			_, repairReq := commitTopologyCandidate(t, s, actor, session, repair, "topology-repair")
			if repairReq.PlanDigest == "" {
				t.Fatal("repair lacked fresh correspondence")
			}
			repaired := replacementTopologyService(t, s, p, p.actual)
			if err := s.ReconcileRuntime(t.Context()); !errors.Is(err, ErrDeploymentSourcesPending) {
				t.Fatalf("original process reinstalled reused identity: %v", err)
			}
			if _, err := s.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
				t.Fatal("original process revived after repair reused its identity")
			}
			ordinary := publishInstallerCandidate(t, repaired, local, "4")
			_, ordinaryReq := commitTopologyCandidate(t, repaired, actor, session, ordinary, "ordinary-after-repair")
			if ordinaryReq.PlanDigest != "" {
				t.Fatal("ordinary repair successor forced rollout")
			}
			if err := repaired.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := repaired.Capture(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSelfConfigTopologySurvivesSourceRestoreAndOrdinaryRepair(t *testing.T) {
	for _, initialHA := range []bool{false, true} {
		for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
			t.Run(map[bool]string{false: "after-topology/", true: "initial-ha/"}[initialHA]+string(engine), func(t *testing.T) {
				original, local, actor, session, transport := topologyServiceFixture(t, engine)
				target := domain.SingletonTopology{HA: true, NodeID: "replacement"}
				before := original
				if initialHA {
					target.NodeID = original.NodeID
					original.HAMode = true
					transport.actual = target
					transport.initialCorrespondence = true
					transport.installed = true
					_ = publishTopology(t, original, local, target)
				} else {
					status := publishTopology(t, original, local, target)
					_, _ = commitTopologyCandidate(t, original, actor, session, status, "initial-topology")
					before = replacementTopologyService(t, original, transport, target)
				}
				beforeProbe := before.Deployment.(*topologyDeploymentProbe)
				beforeProbe.proposedStamp = "source-template"
				status, err := before.Status(t.Context(), local)
				if err != nil {
					t.Fatal(err)
				}
				scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
				sources, err := json.Marshal(config.ManagedBootstrapSources{Version: 1, Topology: target, DatabaseSource: "next"})
				if err != nil {
					t.Fatal(err)
				}
				v, err := (&Values{DB: before.DB, Keyring: before.Keyring, Auth: before.Auth}).Set(t.Context(), local, scope, config.ManagedBootstrapSourcesKey, string(sources), nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := (&Revisions{DB: before.DB, Keyring: before.Keyring, Auth: before.Auth}).PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{v.VersionID}}); err != nil {
					t.Fatal(err)
				}
				status, err = before.Status(t.Context(), local)
				if err != nil {
					t.Fatal(err)
				}
				leaseAt, err := before.DB.Coordination().Now(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				oldCoord := before.DB.Coordination().ForSingletonProcess(target.NodeID, beforeProbe.stamp)
				oldFence, held, err := oldCoord.ClaimLease(t.Context(), "scheduler", target.NodeID, leaseAt, leaseAt.Add(time.Minute))
				if err != nil || !held {
					t.Fatalf("initial lease: %v", err)
				}
				committed, req := commitTopologyCandidate(t, before, actor, session, status, "source-after-topology")
				if _, err := before.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
					t.Fatal("source commit left previous template serving")
				}
				if held, err := oldCoord.RenewLease(t.Context(), "scheduler", target.NodeID, oldFence, leaseAt, leaseAt.Add(time.Minute)); err == nil || held {
					t.Fatal("source commit retained old scheduler lease")
				}
				if err := tx.Write(store.WithSingletonLease(t.Context(), "scheduler", target.NodeID, oldFence), before.DB, func(context.Context, store.Repos, *authz.TxAuthorizer) error { return nil }); !errors.Is(err, store.ErrSingletonLeaseLost) {
					t.Fatalf("source commit retained old term write authority: %v", err)
				}

				after := &SelfConfig{DB: before.DB, Keyring: before.Keyring, Auth: before.Auth, NodeID: target.NodeID, HAMode: true, Installer: before.Installer, Deployment: &topologyDeploymentProbe{deploymentProbe: transport.deploymentProbe, actual: target, stamp: "source-template", source: "next"}}
				t.Cleanup(func() { _ = after.CloseRuntime() })
				if err := after.ReconcileRuntime(t.Context()); err != nil {
					t.Fatal(err)
				}
				if _, err := after.Capture(t.Context()); err != nil {
					t.Fatalf("source replacement lost topology: %v", err)
				}
				metadata, err := before.DB.Coordination().CurrentSelfConfigGeneration(t.Context())
				if err != nil || metadata.TopologyStamp != "source-template" || metadata.Topology.After != target {
					t.Fatal("source rollout discarded membership or stamp", err)
				}
				at, err := after.runtimeTimestamp(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if err := tx.Write(t.Context(), after.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
					p, err := az.SelfConfigRuntimeAuthority(ctx, "")
					if err != nil {
						return err
					}
					return r.SelfConfig().FinishJob(ctx, p, committed.Job.ID, "partial", "convergence_timeout", at)
				}); err != nil {
					t.Fatal(err)
				}
				restore := SelfConfigDeploymentRestoreRequest{Revision: req.Revision, ExpectedGeneration: committed.Generation, SchemaVersion: req.SchemaVersion, PlanDigest: req.PlanDigest}
				selfConfigReauthenticate(t, after, session, SelfConfigReauthTarget{Action: "rollout-restore", OwnerInstanceID: status.OwnerInstanceID, Revision: restore.Revision, ExpectedGeneration: restore.ExpectedGeneration, SchemaVersion: restore.SchemaVersion, PlanDigest: restore.PlanDigest})
				if _, err := after.RestoreDeployment(t.Context(), actor, restore); err != nil {
					t.Fatal(err)
				}
				if err := after.ReconcileRuntime(t.Context()); err != nil {
					t.Fatal(err)
				}
				now, err := before.DB.Coordination().Now(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if _, held, err := before.DB.Coordination().ForSingletonProcess(target.NodeID, beforeProbe.stamp).ClaimLease(t.Context(), "scheduler", target.NodeID, now, now.Add(time.Minute)); err == nil || held {
					t.Fatal("source Restore enabled lease before fresh repair")
				}
				metadata, err = before.DB.Coordination().CurrentSelfConfigGeneration(t.Context())
				if err != nil || !metadata.DeploymentRestoring || metadata.TopologyRestoring {
					t.Fatal("source-only Restore became persistent topology restoration", err)
				}
				repair := publishTopology(t, before, local, target)
				_, repairReq := commitTopologyCandidate(t, before, actor, session, repair, "source-restored-repair")
				if repairReq.PlanDigest != "" {
					t.Fatal("installed source repair unexpectedly required topology rollout")
				}
				for range 2 {
					if err := before.ReconcileRuntime(t.Context()); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := before.Capture(t.Context()); err != nil {
					t.Fatalf("fresh source repair failed: %v", err)
				}
				if _, held, err := before.DB.Coordination().ForSingletonProcess(target.NodeID, beforeProbe.stamp).ClaimLease(t.Context(), "scheduler", target.NodeID, now, now.Add(time.Minute)); err != nil || !held {
					t.Fatalf("repair did not restore matching lease: %v", err)
				}
				if _, err := after.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
					t.Fatal("restored-away source process served repaired generation")
				}
			})
		}
	}

}
