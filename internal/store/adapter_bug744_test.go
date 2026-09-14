package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Regressions for #744: a tombstoned adapter reserved its origin forever
// (delete then recreate at the same origin failed on Save), and a retried
// converge kept minting fresh conflict artifacts for the same name while the
// UI surfaced stale-generation groups that could never be adopted.

// seedAdapterBase writes the minimal project/principal/grant/key fixtures the
// adapter Create path needs, on either engine.
func seedAdapterBase(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := t.Context()
	truthy := map[store.Engine]string{store.EngineSQLite: "1", store.EnginePostgres: "TRUE"}[db.Engine()]
	falsy := map[store.Engine]string{store.EngineSQLite: "0", store.EnginePostgres: "FALSE"}[db.Engine()]
	exec := func(statement string) {
		t.Helper()
		var err error
		if db.Engine() == store.EnginePostgres {
			_, err = db.PG().Exec(ctx, statement)
		} else {
			_, err = db.SQLiteWrite().ExecContext(ctx, statement)
		}
		if err != nil {
			t.Fatalf("seed %q: %v", statement, err)
		}
	}
	exec(fmt.Sprintf(`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_744','744',%s,'{}','2026-08-17T00:00:00Z')`, truthy))
	exec(`INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_744','org_744','744','2026-08-17T00:00:00Z')`)
	exec(`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_744','org_744','prj_744','prod','','2026-08-17T00:00:00Z',0)`)
	exec(`INSERT INTO principals (id,kind,created_at) VALUES ('usr_744','human','2026-08-17T00:00:00Z')`)
	exec(`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_744','usr_744','manage-adapters','org_744','prj_744',NULL,'2026-08-17T00:00:00Z')`)
	exec(fmt.Sprintf(`INSERT INTO keys (id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) VALUES ('key_744','org_744','prj_744','DB_URL','','secret','',%s,'','optional','none','none','2026-08-17T00:00:00Z')`, falsy))
}

func runAdapterOriginReuse(t *testing.T, db *store.DB) {
	seedAdapterBase(t, db)
	ctx := t.Context()
	now := time.Now().UTC()
	scope := domain.Scope{Org: "org_744", Project: "prj_744"}
	const origin = "https://git.744"

	create := func(adapterID, targetID string) error {
		return storetx.Write(ctx, db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
			p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_744"}, authz.OpAdapterConfigure, scope)
			if err != nil {
				return err
			}
			_, _, err = repos.Adapters().Create(ctx, p, store.AdapterCreate{
				ID: adapterID, Provider: "forgejo", Origin: origin,
				CredentialCiphertext: []byte("provider-token"), AuthorityPrincipalID: "usr_744",
				Target: store.AdapterTargetMutation{
					ID: targetID, AdapterID: adapterID, EnvironmentID: "env_744",
					DestinationKind: "repository", DestinationOwner: "acme", DestinationName: "app",
					DestinationID: 42, NamePrefix: "PROD_", KeyIDs: []string{"key_744"},
				},
				At: now,
			})
			return err
		})
	}

	if err := create("adp_a", "tgt_a"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// Two live adapters must never share an origin.
	if err := create("adp_b", "tgt_b"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second active create at same origin = %v, want ErrConflict", err)
	}
	// Tombstone the adapter; keepRemote releases the ledger inline.
	if err := storetx.Write(ctx, db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_744"}, authz.OpAdapterDelete, scope)
		if err != nil {
			return err
		}
		_, err = repos.Adapters().TeardownAdapter(ctx, p, "adp_a", true, now)
		return err
	}); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	// #744: the freed origin is reusable now that uniqueness ignores tombstones.
	if err := create("adp_c", "tgt_c"); err != nil {
		t.Fatalf("recreate at freed origin: %v", err)
	}
}

func TestAdapterOriginReusableAfterTombstoneSQLite(t *testing.T) {
	runAdapterOriginReuse(t, openKeyTestDB(t, store.Config{Engine: store.EngineSQLite, Path: t.TempDir() + "/adapter-origin.db"}))
}

func TestAdapterOriginReusableAfterTombstonePostgres(t *testing.T) {
	runAdapterOriginReuse(t, postgresTestDB(t))
}

// TestAdapterConflictInsertIdempotentAcrossRetries proves a retried converge
// re-running the same conflicting effect at the same generation records the
// conflict once instead of flooding the target with a fresh artifact per
// attempt (PROD_SSH_KEY x3 at generation 4 in the report).
func TestAdapterConflictInsertIdempotentAcrossRetries(t *testing.T) {
	db := adapterRuntimeDB(t)
	runtime := store.NewAdapterRuntime(db, func(context.Context, adapter.Job, adapter.Effect) error { return nil })
	effect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "PROD_SSH_KEY", Disposition: adapter.Create}

	raiseConflict := func(journal adapter.Journal) {
		t.Helper()
		state, err := journal.Reserve(t.Context(), effect)
		if err != nil {
			t.Fatalf("reserve: %v", err)
		}
		if err := journal.Prepare(t.Context(), effect, state); err != nil {
			t.Fatalf("prepare: %v", err)
		}
		if err := journal.Finish(t.Context(), effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, ReleaseLedger: true, Conflict: true, ProviderStatus: 204, Finding: "possible_capture"}); err != nil {
			t.Fatalf("finish: %v", err)
		}
	}

	now := time.Now().UTC()
	job, ok, err := runtime.ClaimDue(t.Context(), "worker_1", now, now.Add(adapter.LeaseTime))
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	raiseConflict(runtime.Journal(job))

	// Requeue the same job and re-run the same conflicting effect: attempt two
	// runs at the unchanged generation, exactly the report's retry loop.
	due := now.Add(time.Second)
	if err := runtime.Retry(t.Context(), job, due, 0, []adapter.Change{{Surface: adapter.Secret, EffectiveName: "PROD_SSH_KEY", Disposition: adapter.Create}}, nil, errors.New("possible capture")); err != nil {
		t.Fatalf("retry: %v", err)
	}
	retryJob, ok, err := runtime.ClaimDue(t.Context(), "worker_2", due.Add(time.Second), due.Add(time.Second).Add(adapter.LeaseTime))
	if err != nil || !ok {
		t.Fatalf("claim retry: ok=%v err=%v", ok, err)
	}
	if retryJob.ID != job.ID || retryJob.Generation != job.Generation {
		t.Fatalf("retry drifted job/generation: %q/%d vs %q/%d", retryJob.ID, retryJob.Generation, job.ID, job.Generation)
	}
	raiseConflict(runtime.Journal(retryJob))

	var conflicts int
	if err := db.SQLiteRead().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM adapter_conflicts WHERE target_id='tgt_1' AND surface='secret' AND effective_name='PROD_SSH_KEY' AND target_generation=1 AND adopted_at IS NULL`).Scan(&conflicts); err != nil {
		t.Fatal(err)
	}
	if conflicts != 1 {
		t.Fatalf("un-adopted conflicts for PROD_SSH_KEY at generation 1 = %d, want 1", conflicts)
	}
}

// TestAdapterConflictsHideStaleGeneration proves the conflict read only returns
// artifacts for the live generation, so a superseded group can no longer show
// in the UI (where adopting it fails with a generic 409) while history is kept.
func TestAdapterConflictsHideStaleGeneration(t *testing.T) {
	db := adapterRuntimeDB(t)
	if _, err := db.SQLiteWrite().ExecContext(t.Context(), `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_adapter','usr_adapter','manage-adapters','org_adapter','prj_adapter',NULL,'2026-08-17T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	scope := domain.Scope{Org: "org_adapter", Project: "prj_adapter"}
	entry := store.AdapterConflictEntry{Surface: "secret", EffectiveName: "TOKEN"}
	if err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_adapter"}, authz.OpAdapterPlan, scope)
		if err != nil {
			return err
		}
		return repos.Adapters().RecordPlan(ctx, p, "tgt_1", "plan_gen1", 1, 0, 42, []store.AdapterConflictEntry{entry}, now)
	}); err != nil {
		t.Fatal(err)
	}

	conflicts := func() []store.AdapterConflictArtifact {
		t.Helper()
		var out []store.AdapterConflictArtifact
		if err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
			p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_adapter"}, authz.OpAdapterInspect, scope)
			if err != nil {
				return err
			}
			out, err = repos.Adapters().Conflicts(ctx, p, "tgt_1")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}

	if got := conflicts(); len(got) != 1 {
		t.Fatalf("current-generation conflicts = %d, want 1", len(got))
	}
	// A publish/adoption bumps the target past the artifact's generation.
	if _, err := db.SQLiteWrite().ExecContext(t.Context(), `UPDATE adapter_targets SET generation=2 WHERE id='tgt_1'`); err != nil {
		t.Fatal(err)
	}
	if got := conflicts(); len(got) != 0 {
		t.Fatalf("stale-generation conflicts = %d, want 0", len(got))
	}
	// The stale artifact is hidden, not deleted.
	var kept int
	if err := db.SQLiteRead().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM adapter_conflicts WHERE artifact_id='plan_gen1'`).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != 1 {
		t.Fatalf("stale artifact rows in history = %d, want 1", kept)
	}
}
