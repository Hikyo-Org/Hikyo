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

func runGitLabAdoptionScope(t *testing.T, db *store.DB, foreignScope string, wantConflict bool) {
	t.Helper()
	seedAdoptionFixture(t, db)
	execAdapter(t, db, `UPDATE adapters SET provider='gitlab' WHERE id='adp_1'`)
	execAdapter(t, db, `UPDATE adapter_targets SET destination_scope='production' WHERE id='tgt_1'`)
	execAdapter(t, db, `UPDATE adapter_conflicts SET surface='variable' WHERE target_id='tgt_1'`)
	// A separate tenant owns the same immutable provider/project/key identity.
	// Only the real GitLab environment scope can separate those claims.
	for _, statement := range []string{
		`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_foreign','Foreign',TRUE,'{}','2026-10-01T00:00:00Z')`,
		`INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_foreign','org_foreign','Foreign','2026-10-01T00:00:00Z')`,
		`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_foreign','org_foreign','prj_foreign','prod','','2026-10-01T00:00:00Z',0)`,
		`INSERT INTO principals (id,kind,created_at) VALUES ('usr_foreign','human','2026-10-01T00:00:00Z')`,
		`INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_foreign','org_foreign','prj_foreign','gitlab','https://git.example','usr_foreign','active','2026-10-01T00:00:00Z')`,
		`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,destination_scope,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_foreign','org_foreign','prj_foreign','env_foreign','adp_foreign','repository','acme','app',42,'` + foreignScope + `','',1,'active','never','2026-10-01T00:00:00Z')`,
		`INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,repository_id,destination_id,destination_scope,surface,effective_name,normalized_name,state,updated_at) VALUES ('led_foreign','org_foreign','prj_foreign','env_foreign','tgt_foreign','https://git.example','repository',0,42,'` + foreignScope + `','variable','TOKEN','TOKEN','owned','2026-10-01T00:00:00Z')`,
	} {
		execAdapter(t, db, statement)
	}
	if !wantConflict {
		// Prove the migration's resume -> create conflict -> fresh adoption
		// path can recover a released blank-scope row without adopting it.
		execAdapter(t, db, `INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,repository_id,destination_id,destination_scope,surface,effective_name,normalized_name,state,updated_at) VALUES ('led_legacy','org_adopt','prj_adopt','env_adopt','tgt_1','https://git.example','repository',0,42,'','variable','TOKEN','TOKEN','released','2026-10-01T00:00:00Z')`)
		runtime := store.NewAdapterRuntime(db, func(context.Context, adapter.Job, adapter.Effect) error { return nil })
		now := time.Now().UTC()
		job, ok, err := runtime.ClaimDue(t.Context(), "gitlab_scope_worker", now, now.Add(adapter.LeaseTime))
		if err != nil || !ok {
			t.Fatalf("claim repair continuation: %v, %v", ok, err)
		}
		journal := runtime.Journal(job)
		effect := adapter.Effect{Surface: adapter.Variable, EffectiveName: "TOKEN", Disposition: adapter.Create}
		state, err := journal.Reserve(t.Context(), effect)
		if err != nil || state != adapter.Reserved {
			t.Fatalf("reactivate released claim: %s, %v", state, err)
		}
		var scope string
		if db.Engine() == store.EngineSQLite {
			err = db.SQLiteWrite().QueryRowContext(t.Context(), `SELECT destination_scope FROM adapter_ledger WHERE id='led_legacy'`).Scan(&scope)
		} else {
			err = db.PG().QueryRow(t.Context(), `SELECT destination_scope FROM adapter_ledger WHERE id='led_legacy'`).Scan(&scope)
		}
		if err != nil || scope != "production" {
			t.Fatalf("reactivated scope = %q, %v; want production", scope, err)
		}
		if err := journal.Prepare(t.Context(), effect, state); err != nil {
			t.Fatal(err)
		}
		// A real provider taken-name response uses this canonical completion:
		// it relinquishes the reservation and records an unowned conflict.
		if err := journal.Finish(t.Context(), effect, adapter.Completion{Outcome: adapter.OutcomeFailure, ReleaseLedger: true, Conflict: true, ProviderStatus: 400}); err != nil {
			t.Fatal(err)
		}
	}
	err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_adopt"}, authz.OpAdapterAdopt, domain.Scope{Org: "org_adopt", Project: "prj_adopt"})
		if err != nil {
			return err
		}
		_, err = repos.Adapters().Adopt(ctx, p, store.AdapterAdoption{
			TargetID: "tgt_1", ArtifactID: "plan_1", Entries: []store.AdapterConflictEntry{{Surface: "variable", EffectiveName: "TOKEN"}},
			AuthorityPrincipalID: "usr_adopt", LedgerIDs: []string{"led_adopt"}, JobID: "job_adopt", AuditAt: time.Now().UTC(),
		})
		return err
	})
	if wantConflict {
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("same-scope foreign owner adoption = %v; want ErrConflict", err)
		}
	} else if err != nil {
		t.Fatalf("different-scope adoption: %v", err)
	}
	count := func(query string) int {
		t.Helper()
		var n int
		var err error
		if db.Engine() == store.EngineSQLite {
			err = db.SQLiteWrite().QueryRowContext(t.Context(), query).Scan(&n)
		} else {
			err = db.PG().QueryRow(t.Context(), query).Scan(&n)
		}
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	want := 1
	if wantConflict {
		want = 0
	}
	if n := count(`SELECT COUNT(*) FROM adapter_ledger WHERE id='led_adopt' AND destination_scope='production' AND state='owned'`); n != want {
		t.Fatalf("adopted exact scope rows = %d; want %d", n, want)
	}
	if n := count(`SELECT COUNT(*) FROM adapter_conflicts WHERE artifact_id='plan_1' AND adopted_at IS NOT NULL`); n != want {
		t.Fatalf("consumed artifacts = %d; want %d", n, want)
	}
	if n := count(fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE id='led_foreign' AND destination_scope='%s' AND state='owned'`, foreignScope)); n != 1 {
		t.Fatalf("foreign custody changed: %d", n)
	}
}

func TestGitLabAdoptionScopeSQLite(t *testing.T) {
	for _, tc := range []struct {
		scope    string
		conflict bool
	}{{"production", true}, {"staging", false}} {
		t.Run(tc.scope, func(t *testing.T) {
			runGitLabAdoptionScope(t, openKeyTestDB(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "gitlab-scope.db")}), tc.scope, tc.conflict)
		})
	}
}

func TestGitLabAdoptionScopePostgres(t *testing.T) {
	for _, tc := range []struct {
		scope    string
		conflict bool
	}{{"production", true}, {"staging", false}} {
		t.Run(tc.scope, func(t *testing.T) {
			runGitLabAdoptionScope(t, postgresTestDB(t), tc.scope, tc.conflict)
		})
	}
}
