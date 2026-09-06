package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestSelfConfigSQLite(t *testing.T) {
	runSelfConfigStore(t, openKeyTestDB(t, store.Config{Engine: store.EngineSQLite, Path: t.TempDir() + "/self-config.db"}))
}
func TestSelfConfigPostgres(t *testing.T) { runSelfConfigStore(t, postgresTestDB(t)) }

func runSelfConfigStore(t *testing.T, db *store.DB) {
	ctx := t.Context()
	now := time.Now().UTC()
	scope := domain.Scope{Org: "org_system", Project: "prj_system", Env: "env_system"}
	exec := func(statement string) {
		t.Helper()
		var err error
		if db.Engine() == store.EnginePostgres {
			_, err = db.PG().Exec(ctx, statement)
		} else {
			_, err = db.SQLiteWrite().ExecContext(ctx, statement)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"system", "other"} {
		exec(fmt.Sprintf(`INSERT INTO orgs(id,name,active,metadata,created_at) VALUES('org_%s','%s',%s,'{}','2026-09-01T00:00:00Z')`, name, name, map[store.Engine]string{store.EngineSQLite: "1", store.EnginePostgres: "TRUE"}[db.Engine()]))
		exec(fmt.Sprintf(`INSERT INTO projects(id,org_id,name,created_at) VALUES('prj_%s','org_%s','%s','2026-09-01T00:00:00Z')`, name, name, name))
		exec(fmt.Sprintf(`INSERT INTO environments(id,org_id,project_id,name,note,display_order,created_at) VALUES('env_%s','org_%s','prj_%s','Production','',0,'2026-09-01T00:00:00Z')`, name, name, name))
		for rev := 1; rev <= 4; rev++ {
			exec(fmt.Sprintf(`INSERT INTO snapshots(id,org_id,project_id,environment_id,revision,schema_revision,published_by,published_at) VALUES('snp_%s_%d','org_%s','prj_%s','env_%s',%d,1,'usr_admin','2026-09-01T00:00:00Z')`, name, rev, name, name, name, rev))
		}
	}
	for _, principal := range []string{"admin", "tenant", "instance"} {
		exec(fmt.Sprintf(`INSERT INTO principals(id,kind,created_at) VALUES('usr_%s','human','2026-09-01T00:00:00Z')`, principal))
	}
	for _, principal := range []string{"admin", "tenant"} {
		for i, cap := range []string{"read", "edit", "publish", "reveal", "reveal-history", "definitions-edit", "project-settings", "manage-members", "manage-adapters"} {
			exec(fmt.Sprintf(`INSERT INTO grants(id,principal_id,capability,org_id,created_at) VALUES('grt_%s_%d','usr_%s','%s','org_system','2026-09-01T00:00:00Z')`, principal, i, principal, cap))
		}
	}
	for _, principal := range []string{"admin", "instance"} {
		for _, cap := range []string{"instance-config", "manage-members"} {
			exec(fmt.Sprintf(`INSERT INTO grants(id,principal_id,capability,created_at) VALUES('grt_%s_%s','usr_%s','%s','2026-09-01T00:00:00Z')`, principal, cap, principal, cap))
		}
	}
	exec(`INSERT INTO grants(id,principal_id,capability,org_id,created_at) VALUES('grt_admin_other','usr_admin','read','org_other','2026-09-01T00:00:00Z')`)
	admin := authz.Identity{Principal: "usr_admin", Class: domain.ClassHuman}
	var binding store.SelfConfigBinding
	write := func(fn func(context.Context, store.Repos, *authz.TxAuthorizer) error) error {
		return storetx.Write(ctx, db, fn)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(write(func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		owner, err := az.InstanceIdentity(ctx)
		if err != nil {
			return err
		}
		_, incarnation, err := db.RecoveryIdentity()
		if err != nil {
			return err
		}
		binding = store.SelfConfigBinding{AdoptionKey: "adopt-test", AdoptedBy: "usr_admin", OwnerInstanceID: owner, OrgID: string(scope.Org), ProjectID: string(scope.Project), EnvironmentID: string(scope.Env), SchemaVersion: 1, Generation: 1, DesiredRevision: 1, DesiredSnapshotID: "snp_system_1", Incarnation: incarnation, CreatedAt: now, UpdatedAt: now}
		p, err := az.Authorize(ctx, admin, authz.OpSelfConfigAdopt, domain.Scope{})
		if err != nil {
			return err
		}
		foreign := binding
		foreign.OwnerInstanceID = "another-instance"
		if err := r.SelfConfig().CreateBinding(ctx, p, foreign); !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("foreign owner binding = %v", err)
		}
		return r.SelfConfig().CreateBinding(ctx, p, binding)
	}))
	t.Run("protected_profile_and_metadata", func(t *testing.T) {
		for _, who := range []string{"tenant", "instance"} {
			err := write(func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
				_, e := az.Authorize(ctx, authz.Identity{Principal: domain.PrincipalID("usr_" + who), Class: domain.ClassHuman}, authz.OpValueRead, scope)
				return e
			})
			if !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("%s read = %v", who, err)
			}
		}
		must(write(func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			orgs, err := az.OrgsForPrincipal(ctx, authz.Identity{Principal: "usr_tenant", Class: domain.ClassHuman})
			if err != nil {
				return err
			}
			if len(orgs) != 0 {
				return fmt.Errorf("tenant navigation leaked system org: %+v", orgs)
			}
			p, err := az.Authorize(ctx, admin, authz.OpValueRead, scope)
			if err != nil {
				return err
			}
			if !authz.IsSelfConfig(p) {
				return errors.New("missing protected profile")
			}
			return nil
		}))
		for _, op := range []authz.Operation{authz.OpOrgDelete, authz.OpProjectDelete, authz.OpEnvDelete, authz.OpKeyCreate, authz.OpDefinitionsApply, authz.OpDefinitionsSettingsSet, authz.OpProjectMachineRevealSet, authz.OpValueCopySource, authz.OpAdapterConfigure} {
			addressed := scope
			switch op {
			case authz.OpOrgDelete:
				addressed = domain.Scope{Org: scope.Org}
			case authz.OpProjectDelete, authz.OpKeyCreate, authz.OpDefinitionsApply, authz.OpDefinitionsSettingsSet, authz.OpProjectMachineRevealSet, authz.OpAdapterConfigure:
				addressed.Env = ""
			}
			err := write(func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
				_, e := az.Authorize(ctx, admin, op, addressed)
				return e
			})
			if !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("protected %s = %v", op, err)
			}
		}
		err := write(func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			machine := admin
			machine.Class = domain.ClassInstanceConn
			_, e := az.Authorize(ctx, machine, authz.OpValueRead, scope)
			return e
		})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("machine read = %v", err)
		}
	})

	t.Run("protected_operations_require_instance_admin_mfa", func(t *testing.T) {
		for _, op := range []authz.Operation{authz.OpValueRead, authz.OpValueStage, authz.OpValuePublish, authz.OpSelfConfigApply, authz.OpSelfConfigTest} {
			for _, factors := range [][]string{{"password"}, {"recovery-code"}, {"password", "totp"}, {"webauthn"}} {
				caller := admin
				caller.SessionID = "ses_profile_assurance"
				caller.Assurance.Factors = factors
				want := len(factors) > 1 || factors[0] == "webauthn"
				err := write(func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
					holds, err := az.CallerHolds(ctx, caller, op, scope)
					if err != nil || holds != want {
						t.Fatalf("capability projection %s %v = %v, %v", op, factors, holds, err)
					}
					instanceHolds, err := az.HoldsInstanceCapability(ctx, caller, authz.OpSelfConfigStatus)
					if err != nil || instanceHolds != want {
						t.Fatalf("instance capability projection %v = %v, %v", factors, instanceHolds, err)
					}
					orgs, err := az.OrgsForPrincipal(ctx, caller)
					if err != nil {
						return err
					}
					listed := false
					for _, org := range orgs {
						listed = listed || org.ID == scope.Org
					}
					if listed != want {
						t.Fatalf("navigation projection %v includes protected org = %v", factors, listed)
					}
					_, err = az.Authorize(ctx, caller, op, scope)
					return err
				})
				if !want {
					if !errors.Is(err, domain.ErrUnauthorized) {
						t.Fatalf("single factor %v %s = %v", factors, op, err)
					}
				} else if err != nil {
					t.Fatalf("MFA %v %s = %v", factors, op, err)
				}
			}
			for _, who := range []string{"tenant", "instance"} {
				caller := authz.Identity{Principal: domain.PrincipalID("usr_" + who), Class: domain.ClassHuman, SessionID: "ses_wrong_admin", Assurance: authz.Assurance{Factors: []string{"webauthn"}}}
				err := write(func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
					holds, err := az.CallerHolds(ctx, caller, op, scope)
					if err != nil || holds {
						t.Fatalf("%s with MFA capability projection %s = %v, %v", who, op, holds, err)
					}
					_, err = az.Authorize(ctx, caller, op, scope)
					return err
				})
				if !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("%s with MFA %s = %v", who, op, err)
				}
			}
			for _, kind := range []domain.PrincipalClass{domain.ClassInstanceConn, domain.ClassWorkload, domain.ClassAutomation} {
				caller := admin
				caller.Class = kind
				err := write(func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
					holds, err := az.CallerHolds(ctx, caller, op, scope)
					if err != nil || holds {
						t.Fatalf("machine %s capability projection %s = %v, %v", kind, op, holds, err)
					}
					instanceHolds, err := az.HoldsInstanceCapability(ctx, caller, authz.OpSelfConfigStatus)
					if err != nil || instanceHolds {
						t.Fatalf("machine %s instance capability projection = %v, %v", kind, instanceHolds, err)
					}
					_, err = az.Authorize(ctx, caller, op, scope)
					return err
				})
				if !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("machine %s %s = %v", kind, op, err)
				}
			}
		}
		// The stronger profile must not impose MFA on ordinary tenant reads.
		caller := admin
		caller.SessionID = "ses_ordinary_password"
		caller.Assurance.Factors = []string{"password"}
		must(write(func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			_, err := az.Authorize(ctx, caller, authz.OpValueRead, domain.Scope{Org: "org_other", Project: "prj_other", Env: "env_other"})
			return err
		}))
	})

	t.Run("mail_test_cannot_mint_authority_for_other_project", func(t *testing.T) {
		err := write(func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			other := domain.Scope{Org: "org_other", Project: "prj_other", Env: "env_other"}
			if _, err := az.Authorize(ctx, admin, authz.OpValueRead, other); err != nil {
				return err
			}
			_, err := az.Authorize(ctx, admin, authz.OpSelfConfigTest, other)
			return err
		})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("foreign test proof=%v", err)
		}
	})
	t.Run("runtime_snapshot_confinement", func(t *testing.T) {
		must(write(func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			if _, err := az.SelfConfigRuntimeAuthority(operation.WithNetwork(ctx), ""); err == nil {
				return errors.New("network runtime mint accepted")
			}
			if _, err := az.SelfConfigRecoveryAuthority(operation.WithNetwork(ctx), 1); err == nil {
				return errors.New("network recovery mint accepted")
			}
			if _, err := authz.SystemAuthority(authz.SiteSelfConfigRuntime, az.Token()); err == nil {
				return errors.New("generic runtime mint accepted")
			}
			if _, err := az.ScopedSystemAuthority(ctx, authz.SiteSelfConfigRuntime, scope); err == nil {
				return errors.New("generic scoped runtime mint accepted")
			}
			if _, err := az.SelfConfigRuntimeAuthority(ctx, "snp_other_1"); !errors.Is(err, domain.ErrNotFound) {
				return fmt.Errorf("foreign runtime target=%v", err)
			}
			p, err := az.SelfConfigRuntimeAuthority(ctx, "")
			if err != nil {
				return err
			}
			if _, err := r.Snapshots().AtRevision(ctx, p, 2); err == nil {
				return errors.New("runtime proof read another revision")
			}
			_, err = r.Snapshots().AtRevision(ctx, p, 1)
			return err
		}))
	})
	var job store.SelfConfigJob
	t.Run("durable_singleflight_idempotency_and_preflight", func(t *testing.T) {
		must(write(func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			p, err := az.Authorize(ctx, admin, authz.OpSelfConfigApply, scope)
			if err != nil {
				return err
			}
			want := store.SelfConfigJob{ID: "job_first", IdempotencyKey: "first", PrincipalID: string(admin.Principal), SnapshotID: "snp_system_2", Revision: 2, SchemaVersion: 1, ExpectedGeneration: 1, LocalNodeID: "node-local", CreatedAt: now.Add(time.Second)}
			job, err = r.SelfConfig().BeginJob(ctx, p, want)
			if err != nil {
				return err
			}
			again, err := r.SelfConfig().BeginJob(ctx, p, want)
			if err != nil || again.ID != job.ID {
				return fmt.Errorf("idempotency = %+v %v", again, err)
			}
			want.Revision = 3
			want.SnapshotID = "snp_system_3"
			if _, err := r.SelfConfig().BeginJob(ctx, p, want); !errors.Is(err, domain.ErrConflict) {
				return fmt.Errorf("changed idempotency payload = %v", err)
			}
			want.ID = "job_second"
			want.IdempotencyKey = "second"
			if _, err := r.SelfConfig().BeginJob(ctx, p, want); !errors.Is(err, domain.ErrConflict) {
				return fmt.Errorf("parallel apply = %v", err)
			}
			if _, err := r.SelfConfig().CommitJob(ctx, p, job.ID, now.Add(2*time.Second)); !errors.Is(err, domain.ErrConflict) {
				return fmt.Errorf("unprepared commit = %v", err)
			}
			return nil
		}))
		must(write(func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			p, err := az.SelfConfigRuntimeAuthority(ctx, job.SnapshotID)
			if err != nil {
				return err
			}
			if _, err := r.Snapshots().AtRevision(ctx, p, 2); err != nil {
				return err
			}
			return r.SelfConfig().PutNode(ctx, p, store.SelfConfigNode{NodeID: "node-local", JobID: job.ID, SchemaVersion: 1, Prepared: true, Incarnation: binding.Incarnation, UpdatedAt: now.Add(2 * time.Second)})
		}))
		must(write(func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			p, err := az.Authorize(ctx, admin, authz.OpSelfConfigApply, scope)
			if err != nil {
				return err
			}
			b, err := r.SelfConfig().CommitJob(ctx, p, job.ID, now.Add(3*time.Second))
			if err != nil {
				return err
			}
			if b.Generation != 2 || b.DesiredRevision != 2 {
				return fmt.Errorf("committed target=%+v", b)
			}
			return nil
		}))
	})
	t.Run("postcommit_reconciliation_and_bounded_roots", func(t *testing.T) {
		must(write(func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			p, err := az.SelfConfigRuntimeAuthority(ctx, "")
			if err != nil {
				return err
			}
			if err := r.SelfConfig().FinishJob(ctx, p, job.ID, "applied", "", now.Add(4*time.Second)); !errors.Is(err, domain.ErrConflict) {
				return fmt.Errorf("false convergence=%v", err)
			}
			if err := r.SelfConfig().FinishJob(ctx, p, job.ID, "partial", "convergence_timeout", now.Add(4*time.Second)); err != nil {
				return err
			}
			if err := r.SelfConfig().PutNode(ctx, p, store.SelfConfigNode{NodeID: "node-local", JobID: job.ID, SchemaVersion: 1, Prepared: true, ActiveGeneration: 2, ActiveRevision: 2, Incarnation: binding.Incarnation, UpdatedAt: now.Add(5 * time.Second)}); err != nil {
				return err
			}
			if err := r.SelfConfig().FinishJob(ctx, p, job.ID, "applied", "", now.Add(5*time.Second)); err != nil {
				return err
			}
			roots, err := r.SelfConfig().Retained(ctx, p)
			if err != nil {
				return err
			}
			if len(roots) != 2 {
				return fmt.Errorf("retention roots=%v", roots)
			}
			return nil
		}))
	})

	t.Run("gc_cannot_collect_runtime_roots", func(t *testing.T) {
		must(write(func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			p, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
			if err != nil {
				return err
			}
			for _, id := range []string{"snp_system_1", "snp_system_2"} {
				collected, err := r.Retention().MarkCollected(ctx, p, id, "test", now)
				if err != nil {
					return err
				}
				if collected {
					return fmt.Errorf("GC collected runtime root %s", id)
				}
			}
			collected, err := r.Retention().MarkCollected(ctx, p, "snp_system_4", "test", now)
			if err != nil {
				return err
			}
			if !collected {
				return errors.New("released snapshot unexpectedly retained")
			}
			if _, err := az.SelfConfigRecoveryAuthority(ctx, 4); !errors.Is(err, domain.ErrNotFound) {
				return fmt.Errorf("recovery accepted collected payload: %v", err)
			}
			return nil
		}))
	})
	t.Run("restore_fences_acks_and_target", func(t *testing.T) {
		must(write(func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			p, err := az.SelfConfigRuntimeAuthority(ctx, "")
			if err != nil {
				return err
			}
			if err := r.SelfConfig().FenceRestored(ctx, p, "restored-incarnation", now.Add(10*time.Second)); err != nil {
				return err
			}
			b, err := r.SelfConfig().Binding(ctx, p)
			if err != nil {
				return err
			}
			if !b.Suspended || b.Generation != 2 || b.DesiredRevision != 2 {
				return fmt.Errorf("restore target=%+v", b)
			}
			nodes, err := r.SelfConfig().Nodes(ctx, p)
			if err != nil {
				return err
			}
			if len(nodes) != 0 {
				return fmt.Errorf("restore left acknowledgements=%v", nodes)
			}
			return nil
		}))
	})
	t.Run("prepare_stalled_job_before_host_recovery", func(t *testing.T) {
		must(write(func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			p, err := az.Authorize(ctx, admin, authz.OpSelfConfigApply, scope)
			if err != nil {
				return err
			}
			_, err = r.SelfConfig().BeginJob(ctx, p, store.SelfConfigJob{ID: "job_stalled", IdempotencyKey: "stalled", PrincipalID: string(admin.Principal), SnapshotID: "snp_system_3", Revision: 3, SchemaVersion: 1, ExpectedGeneration: 2, LocalNodeID: "local", CreatedAt: now, UpdatedAt: now})
			return err
		}))
	})

	t.Run("host_recovery_preserves_restore_suspension", func(t *testing.T) {
		must(write(func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			p, err := az.SelfConfigRecoveryAuthority(ctx, 3)
			if err != nil {
				return err
			}
			if _, err := r.Snapshots().AtRevision(ctx, p, 2); err == nil {
				return errors.New("recovery proof read unrelated snapshot")
			}
			snapshot, err := r.Snapshots().AtRevision(ctx, p, 3)
			if err != nil {
				return err
			}
			if _, err := r.SelfConfig().RecoverTarget(ctx, p, 1, 3, snapshot.ID, now.Add(time.Minute)); !errors.Is(err, domain.ErrConflict) {
				return fmt.Errorf("recovery stale CAS: %v", err)
			}
			b, err := r.SelfConfig().RecoverTarget(ctx, p, 2, 3, snapshot.ID, now.Add(time.Minute))
			if err != nil {
				return err
			}
			jobs, err := r.SelfConfig().Jobs(ctx, p)
			if err != nil {
				return err
			}
			found := false
			for _, job := range jobs {
				if job.ID == "job_stalled" {
					found = true
					if job.Status != "aborted" || job.ErrorCode != "recovered" {
						return fmt.Errorf("recovery left job=%+v", job)
					}
				}
			}
			if !found {
				return errors.New("recovery removed job history")
			}
			if b.Generation != 3 || b.DesiredRevision != 3 || !b.Suspended {
				return fmt.Errorf("recovery state=%+v", b)
			}
			roots, err := r.SelfConfig().Retained(ctx, p)
			if err != nil {
				return err
			}
			if len(roots) != 2 || roots[0] != "snp_system_2" || roots[1] != "snp_system_3" {
				return fmt.Errorf("recovery roots=%v", roots)
			}
			return nil
		}))
	})

}
