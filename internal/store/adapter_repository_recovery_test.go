package store_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func repositoryRecoveryDB(t *testing.T, engine store.Engine) *store.DB {
	t.Helper()
	if engine == store.EnginePostgres {
		return postgresTestDB(t)
	}
	return openKeyTestDB(t, store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "recovery.db")})
}

func recoveryCount(t *testing.T, db *store.DB, query string) int {
	t.Helper()
	var n int
	queryAdapterRow(t, db, query, &n)
	return n
}

func runReleasedAdoptionRecovery(t *testing.T, engine store.Engine) {
	for _, control := range []string{"current", "vault_current", "stale_artifact", "consumed_artifact", "wrong_destination", "version_missing", "foreign_owner", "held"} {
		t.Run(control, func(t *testing.T) {
			db := repositoryRecoveryDB(t, engine)
			seedAdoptionFixture(t, db)
			// History belongs to an old route. Reclaiming must use the current
			// verified route, rather than revive old destination ownership.
			execAdapter(t, db, `INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,destination_scope,repository_id,destination_id,surface,effective_name,normalized_name,state,missing,updated_at) VALUES ('led_history','org_adopt','prj_adopt','env_adopt','tgt_1','https://old.example','repository','old',0,77,'secret','Token','TOKEN','released',TRUE,'2026-08-17T00:00:00Z')`)
			if engine == store.EngineSQLite {
				execAdapter(t, db, `UPDATE adapters SET credential_ciphertext=X'01' WHERE id='adp_1'`)
			} else {
				execAdapter(t, db, `UPDATE adapters SET credential_ciphertext=decode('01','hex') WHERE id='adp_1'`)
			}
			if err := storetx.Read(t.Context(), db, func(ctx context.Context, repos store.ReadRepos, az *authz.TxAuthorizer) error {
				p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_adopt"}, authz.OpAdapterPlan, domain.Scope{Org: "org_adopt", Project: "prj_adopt"})
				if err != nil {
					return err
				}
				material, err := repos.Adapters().PlanMaterial(ctx, p, "tgt_1")
				if err == nil && len(material.Ledger) != 0 {
					t.Fatalf("released history visible as managed: %+v", material.Ledger)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			// Obtain a new plan artifact after the value-blind plan read.
			execAdapter(t, db, `DELETE FROM adapter_conflicts WHERE artifact_id='plan_1'`)
			if err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
				p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_adopt"}, authz.OpAdapterPlan, domain.Scope{Org: "org_adopt", Project: "prj_adopt"})
				if err != nil {
					return err
				}
				return repos.Adapters().RecordPlan(ctx, p, "tgt_1", "plan_1", 1, 0, 42, []store.AdapterConflictEntry{{Surface: "secret", EffectiveName: "TOKEN"}}, time.Now().UTC())
			}); err != nil {
				t.Fatal(err)
			}
			switch control {
			case "stale_artifact":
				execAdapter(t, db, `UPDATE adapter_conflicts SET target_generation=2 WHERE artifact_id='plan_1'`)
			case "consumed_artifact":
				execAdapter(t, db, `UPDATE adapter_conflicts SET adopted_at='2026-08-18T00:00:00Z' WHERE artifact_id='plan_1'`)
			case "wrong_destination":
				execAdapter(t, db, `UPDATE adapter_conflicts SET destination_id=43 WHERE artifact_id='plan_1'`)
			case "version_missing":
				execAdapter(t, db, `UPDATE adapters SET provider='vault-kv' WHERE id='adp_1'`)
			case "vault_current":
				execAdapter(t, db, `UPDATE adapters SET provider='vault-kv' WHERE id='adp_1'`)
				execAdapter(t, db, `UPDATE adapter_conflicts SET observed_provider_version=7 WHERE artifact_id='plan_1'`)
			case "foreign_owner":
				execAdapter(t, db, `INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_foreign','org_adopt','prj_adopt','env_adopt','adp_1','repository','acme','other',42,'',1,'active','never','2026-08-17T00:00:00Z')`)
				execAdapter(t, db, `INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,repository_id,destination_id,surface,effective_name,normalized_name,state,updated_at) VALUES ('led_foreign','org_adopt','prj_adopt','env_adopt','tgt_foreign','https://git.example','repository',0,42,'secret','TOKEN','TOKEN','owned','2026-08-17T00:00:00Z')`)
			case "held":
				execAdapter(t, db, `UPDATE adapter_ledger SET state='owned' WHERE id='led_history'`)
			}
			err := adoptTOKEN(t, db)
			if control != "current" && control != "vault_current" {
				if !errors.Is(err, store.ErrConflict) {
					t.Fatalf("invalid adoption = %v; want conflict", err)
				}
				if recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE id='led_history' AND provider_origin='https://old.example' AND destination_id=77 AND missing=TRUE`) != 1 {
					t.Fatal("refused adoption changed historical custody")
				}
				if control == "foreign_owner" && recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE id='led_foreign' AND state='owned' AND destination_id=42`) != 1 {
					t.Fatal("refused adoption changed foreign custody")
				}
				return
			}
			if err != nil {
				t.Fatalf("fresh adoption over released history: %v", err)
			}
			if recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE id='led_history' AND org_id='org_adopt' AND project_id='prj_adopt' AND environment_id='env_adopt' AND target_id='tgt_1' AND provider_origin='https://git.example' AND destination_kind='repository' AND destination_scope='' AND repository_id=0 AND destination_id=42 AND effective_name='TOKEN' AND state='owned' AND missing=FALSE`) != 1 {
				t.Fatal("reclaimed history does not match current route/custody")
			}
			if recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_conflicts WHERE artifact_id='plan_1' AND adopted_at IS NOT NULL`) != 1 {
				t.Fatal("fresh artifact not consumed")
			}
			runtime := store.NewAdapterRuntime(db, func(context.Context, adapter.Job, adapter.Effect) error { return nil })
			now := time.Now().UTC()
			job, ok, err := runtime.ClaimDue(t.Context(), "reclaimed_worker", now, now.Add(adapter.LeaseTime))
			if err != nil || !ok || job.ID != "job_adopt" {
				t.Fatalf("claim reclaimed converge = %+v,%v,%v", job, ok, err)
			}
			execution, err := runtime.LoadExecution(t.Context(), job)
			if err != nil || len(execution.Ledger) != 1 || !execution.Ledger[0].AdoptionPending {
				t.Fatalf("reclaimed worker material = %+v,%v", execution.Ledger, err)
			}
			if control == "vault_current" && (execution.Ledger[0].AdoptionVersion == nil || *execution.Ledger[0].AdoptionVersion != 7) {
				t.Fatalf("reclaimed Vault witness = %v; want7", execution.Ledger[0].AdoptionVersion)
			}
			effect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "TOKEN", Disposition: adapter.Update}
			effect.RequireExplicitRecovery = control == "vault_current"
			journal := runtime.Journal(job)
			if err := journal.Prepare(t.Context(), effect, adapter.Owned); err != nil {
				t.Fatal(err)
			}
			if err := journal.Finish(t.Context(), effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Owned}); err != nil {
				t.Fatal(err)
			}
			if err := adoptTOKEN(t, db); !errors.Is(err, store.ErrConflict) {
				t.Fatalf("replay adoption = %v; want conflict", err)
			}
		})
	}
}

func TestReleasedAdoptionRecoverySQLite(t *testing.T) {
	runReleasedAdoptionRecovery(t, store.EngineSQLite)
}
func TestReleasedAdoptionRecoveryPostgres(t *testing.T) {
	runReleasedAdoptionRecovery(t, store.EnginePostgres)
}

func runTeardownMoveRefusal(t *testing.T, engine store.Engine) {
	for _, state := range []string{"scrubbing", "activating", "attention_required", "completed", "canceled"} {
		t.Run(state, func(t *testing.T) {
			db := repositoryRecoveryDB(t, engine)
			seedAdoptionFixture(t, db)
			unfinished := state != "completed" && state != "canceled"
			if unfinished {
				execAdapter(t, db, `UPDATE adapter_targets SET state='moving' WHERE id='tgt_1'`)
			}
			execAdapter(t, db, fmt.Sprintf(`INSERT INTO adapter_route_moves (id,org_id,project_id,adapter_id,target_id,kind,authority_principal_id,state,keep_remote,created_at) VALUES ('move_1','org_adopt','prj_adopt','adp_1','tgt_1','target','usr_adopt','%s',TRUE,'2026-08-17T00:00:00Z')`, state))
			err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
				p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_adopt"}, authz.OpAdapterDelete, domain.Scope{Org: "org_adopt", Project: "prj_adopt"})
				if err != nil {
					return err
				}
				_, err = repos.Adapters().TeardownAdapter(ctx, p, "adp_1", true, time.Now().UTC())
				return err
			})
			if unfinished {
				if !errors.Is(err, store.ErrConflict) {
					t.Fatalf("delete during %s = %v; want conflict", state, err)
				}
				if recoveryCount(t, db, `SELECT COUNT(*) FROM adapters WHERE id='adp_1' AND state='active'`) != 1 || recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_targets WHERE id='tgt_1' AND state='moving' AND generation=1`) != 1 || recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_outbox WHERE id='job_1' AND state='queued'`) != 1 {
					t.Fatal("refused deletion changed adapter/target/job")
				}
			} else if err != nil {
				t.Fatalf("delete after terminal move = %v", err)
			}
		})
	}
}

func TestTeardownMoveRefusalSQLite(t *testing.T)   { runTeardownMoveRefusal(t, store.EngineSQLite) }
func TestTeardownMoveRefusalPostgres(t *testing.T) { runTeardownMoveRefusal(t, store.EnginePostgres) }

func TestConcurrentMoveAndAdapterDeletePostgres(t *testing.T) {
	for attempt := range 5 {
		t.Run(fmt.Sprint(attempt), func(t *testing.T) {
			db := postgresTestDB(t)
			seedAdoptionFixture(t, db)
			execAdapter(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_reveal','usr_adopt','reveal','org_adopt','prj_adopt','env_adopt','2026-08-17T00:00:00Z')`)
			execAdapter(t, db, `INSERT INTO keys (id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) VALUES ('key_token','org_adopt','prj_adopt','TOKEN','','secret','',FALSE,'','optional','none','none','2026-08-17T00:00:00Z')`)
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, moving := range []bool{true, false} {
				go func() {
					<-start
					op := authz.OpAdapterDelete
					if moving {
						op = authz.OpAdapterConfigure
					}
					results <- storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
						p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_adopt"}, op, domain.Scope{Org: "org_adopt", Project: "prj_adopt"})
						if err != nil {
							return err
						}
						if moving {
							_, err = repos.Adapters().MoveTarget(ctx, p, store.AdapterRouteMoveMutation{MoveID: "move_race", Target: store.AdapterTargetMutation{ID: "tgt_1", AdapterID: "adp_1", EnvironmentID: "env_adopt", DestinationKind: "repository", DestinationOwner: "acme", DestinationName: "new", KeyIDs: []string{"key_token"}}, ExpectedGeneration: 1, AuthorityPrincipalID: "usr_adopt", KeepRemote: true, At: time.Now().UTC()})
						} else {
							_, err = repos.Adapters().TeardownAdapter(ctx, p, "adp_1", true, time.Now().UTC())
						}
						return err
					})
				}()
			}
			close(start)
			a, b := <-results, <-results
			if (a == nil) == (b == nil) {
				t.Fatalf("exactly one concurrent operation must commit: %v / %v", a, b)
			}
			for _, err := range []error{a, b} {
				if err != nil && !errors.Is(err, store.ErrConflict) && !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("concurrent operation raw failure: %v", err)
				}
			}
			if recoveryCount(t, db, `SELECT COUNT(*) FROM adapters a JOIN adapter_targets t ON t.adapter_id=a.id WHERE a.state='tombstoned' AND t.state='moving'`) != 0 {
				t.Fatal("move remained operational under deleted parent")
			}
		})
	}
}
