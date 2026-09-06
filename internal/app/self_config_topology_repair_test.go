package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// topologyOwnerFixtureCommit constructs durable job transitions through checked
// repositories. Service topology tests cover Publish, exact MFA and Restore;
// this fixture isolates actual owner construction from transport and ceremonies.
func topologyOwnerFixtureCommit(t *testing.T, srv *Server, principal domain.PrincipalID, id string, topology *domain.SingletonTopologyChange, previousStamp, stamp string, restore bool) {
	t.Helper()
	at, err := srv.db.Coordination().Now(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = tx.Write(t.Context(), srv.db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		runtime, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		repo := r.SelfConfig()
		b, err := repo.Binding(ctx, runtime)
		if err != nil {
			return err
		}
		p, err := az.Authorize(ctx, authz.Identity{Principal: principal}, authz.OpSelfConfigApply, domain.Scope{Org: domain.OrgID(b.OrgID), Project: domain.ProjectID(b.ProjectID), Env: domain.EnvID(b.EnvironmentID)})
		if err != nil {
			return err
		}
		job, err := repo.BeginJob(ctx, p, store.SelfConfigJob{ID: id, IdempotencyKey: id, PrincipalID: string(principal), SnapshotID: b.DesiredSnapshotID, Revision: b.DesiredRevision, SchemaVersion: b.SchemaVersion, ExpectedGeneration: b.Generation, CreatedAt: at, LocalNodeID: srv.selfConfig.NodeID})
		if err != nil {
			return err
		}
		n := store.SelfConfigNode{NodeID: srv.selfConfig.NodeID, JobID: id, Incarnation: b.Incarnation, SchemaVersion: b.SchemaVersion, Prepared: true, UpdatedAt: at}
		if err := repo.PutNode(ctx, runtime, n); err != nil {
			return err
		}
		rollout := topology != nil || restore
		var row store.SelfConfigRollout
		var signed configrollout.SignedCommand
		if rollout {
			sequence, err := repo.NextRolloutSequence(ctx, p, "owner-fixture")
			if err != nil {
				return err
			}
			signed.Command = configrollout.Command{Action: configrollout.ActionPrepare, Topology: topology, PreviousTemplateStamp: previousStamp}
			raw, err := json.Marshal(signed)
			if err != nil {
				return err
			}
			row = store.SelfConfigRollout{JobID: id, EnrollmentID: "owner-fixture", Incarnation: b.Incarnation, Sequence: sequence, CommandJSON: string(raw)}
			if err := repo.PutRollout(ctx, p, row); err != nil {
				return err
			}
			row, err = repo.Rollout(ctx, p, id)
			if err != nil {
				return err
			}
			signed.Command.Action = configrollout.ActionSubmit
			raw, err = json.Marshal(signed)
			if err != nil {
				return err
			}
			response, err := json.Marshal(configrollout.Response{TemplateStamp: stamp})
			if err != nil {
				return err
			}
			row.CommandJSON, row.ResponseJSON, row.PlanDigest = string(raw), string(response), strings.Repeat("a", 64)
			row.Sequence++
			if err := repo.PutRollout(ctx, p, row); err != nil {
				return err
			}
		}
		committed, err := repo.CommitJob(ctx, p, id, at)
		if err != nil {
			return err
		}
		if restore {
			row, err = repo.Rollout(ctx, p, id)
			if err != nil {
				return err
			}
			signed.Command.Action = configrollout.ActionRestore
			raw, err := json.Marshal(signed)
			if err != nil {
				return err
			}
			row.CommandJSON, row.ExternalPhase = string(raw), "restored"
			row.Sequence++
			if err := repo.PutRollout(ctx, p, row); err != nil {
				return err
			}
			return repo.FinishJob(ctx, runtime, id, "partial", "convergence_timeout", at)
		}
		n.ActiveGeneration, n.ActiveRevision = committed.Generation, job.Revision
		if err := repo.PutNode(ctx, runtime, n); err != nil {
			return err
		}
		return repo.FinishJob(ctx, runtime, id, "applied", "", at)
	})
	if err != nil {
		t.Fatalf("%s: %v", id, err)
	}
}

func TestSingletonHARestoreBootRetainsCoordinationThroughSourceRepair(t *testing.T) {
	for _, initialHA := range []bool{false, true} {
		t.Run(map[bool]string{false: "after-topology", true: "initial-ha"}[initialHA], func(t *testing.T) {
			cfg := nodePostgresConfig(t)
			cfg.HA = initialHA
			cfg.NodeID = "stable-server"
			stamp := "original-template"
			resources := defaultBootResources()
			resources.configureDeployment = func(_ context.Context, _ *config.Config, db *store.DB, _ *crypto.Keyring) (service.BootstrapDeployment, error) {
				owner, inc, err := db.RecoveryIdentity()
				if err != nil {
					return nil, err
				}
				return &bootDeploymentProbe{identity: service.DeploymentIdentity{EnrollmentID: "owner-fixture", OwnerInstanceID: owner, Incarnation: inc, DeploymentUID: "deployment", TemplateStamp: stamp}}, nil
			}
			first, err := boot(t.Context(), cfg, testLogger(), resources)
			if err != nil {
				t.Fatal(err)
			}
			admin, err := first.owner.current.graph.auth.BootstrapAdmin(t.Context(), "owner", "Owner", "stdout")
			if err != nil {
				t.Fatal(err)
			}
			if err := first.selfConfig.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			target := domain.SingletonTopology{HA: true, NodeID: cfg.NodeID}
			var correspondence *domain.SingletonTopologyChange
			if initialHA {
				correspondence = &domain.SingletonTopologyChange{Before: target, After: target}
			} else {
				topologyOwnerFixtureCommit(t, first, admin.PrincipalID, "topology", &domain.SingletonTopologyChange{Before: domain.SingletonTopology{NodeID: cfg.NodeID}, After: target}, "initial-template", stamp, false)
			}
			topologyOwnerFixtureCommit(t, first, admin.PrincipalID, "source-restore", correspondence, stamp, "source-template", true)
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			cfg.HA, cfg.RootKeyFile = true, devRootKeyPath(cfg)
			restored, err := boot(t.Context(), cfg, testLogger(), resources)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			coordinator := restored.owner.haCoord
			graph := restored.owner.current.graph
			if coordinator == nil || graph.scheduler.Lease != coordinator || graph.scheduler.OnTick == nil {
				t.Fatal("HA repair boot discarded scheduler coordination")
			}
			now, err := coordinator.Now(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if _, held, err := graph.scheduler.Lease.ClaimLease(t.Context(), "scheduler", cfg.NodeID, now, now.Add(time.Minute)); err == nil || held {
				t.Fatal("Restore boot obtained scheduler authority before fresh repair")
			}
			if _, err := restored.selfConfig.Capture(t.Context()); err == nil {
				t.Fatal("Restore boot served business runtime")
			}
			graph.scheduler.OnTick(t.Context())
			// A second actual boot captures the restored-away stamp. Neither later
			// metadata updates nor the valid process's repair may retarget this handle.
			stamp = "source-template"
			wrong, err := boot(t.Context(), cfg, testLogger(), resources)
			if err != nil {
				t.Fatal(err)
			}
			defer wrong.Close()
			if wrong.owner.haCoord == nil {
				t.Fatal("wrong-stamp repair boot lost coordinator")
			}
			topologyOwnerFixtureCommit(t, restored, admin.PrincipalID, "fresh-source-repair", nil, "", "", false)
			if err := restored.selfConfig.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := restored.selfConfig.Capture(t.Context()); err != nil {
				t.Fatalf("fresh repair did not activate actual owner: %v", err)
			}
			graph = restored.owner.current.graph
			if restored.owner.haCoord != coordinator || graph.scheduler.Lease != coordinator {
				t.Fatal("repair replaced captured startup coordinator")
			}
			graph.scheduler.OnTick(t.Context())
			if _, held, err := graph.scheduler.Lease.ClaimLease(t.Context(), "scheduler", cfg.NodeID, now, now.Add(time.Minute)); err != nil || !held {
				t.Fatalf("repaired scheduler cannot claim: %v", err)
			}
			if _, held, err := wrong.owner.current.graph.scheduler.Lease.ClaimLease(t.Context(), "scheduler", cfg.NodeID, now, now.Add(time.Minute)); err == nil || held {
				t.Fatal("restored-away template gained scheduler authority")
			}
			if _, err := wrong.selfConfig.Capture(t.Context()); err == nil {
				t.Fatal("restored-away template served repaired runtime")
			}
			if err := wrong.owner.haCoord.UpsertNode(t.Context(), store.HANode{NodeID: cfg.NodeID, StartedAt: now, HeartbeatAt: now}); err == nil {
				t.Fatal("restored-away template renewed heartbeat")
			}
			// A fresh independent limiter must observe the actual owner's consumption.
			// A local-only repair graph would allow the extra request below.
			_, peerLimiter, err := AuthComponents(graph.cfg)
			if err != nil {
				t.Fatal(err)
			}
			peerLimiter.UseShared(coordinator, testLogger())
			for range admission.MetaPerIPPerMinute {
				if !graph.limiter.AllowDiscovery("192.0.2.87") {
					t.Fatal("shared admission refused before its allowance")
				}
			}
			if peerLimiter.AllowDiscovery("192.0.2.87") {
				t.Fatal("repaired owner did not retain shared admission")
			}
		})
	}

}
