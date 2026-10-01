package isolation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func seedLegacyRetirementFixture(t *testing.T, db *store.DB) {
	t.Helper()
	truthy := map[store.Engine]string{store.EngineSQLite: "1", store.EnginePostgres: "TRUE"}[db.Engine()]
	execRaw(t, db, fmt.Sprintf(`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_adopt','Adopt',%s,'{}','2026-08-17T00:00:00Z')`, truthy))
	execRaw(t, db, `INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_adopt','org_adopt','Adopt','2026-08-17T00:00:00Z')`)
	execRaw(t, db, `INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_adopt','org_adopt','prj_adopt','prod','','2026-08-17T00:00:00Z',0)`)
	execRaw(t, db, `INSERT INTO principals (id,kind,created_at) VALUES ('usr_adopt','human','2026-08-17T00:00:00Z')`)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_adopt','usr_adopt','manage-adapters','org_adopt','prj_adopt',NULL,'2026-08-17T00:00:00Z')`)
	execRaw(t, db, `INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_1','org_adopt','prj_adopt','forgejo','https://git.example','usr_adopt','active','2026-08-17T00:00:00Z')`)
	execRaw(t, db, `INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,active_job_id,created_at) VALUES ('tgt_1','org_adopt','prj_adopt','env_adopt','adp_1','repository','acme','app',42,'',1,'active','converging','job_1','2026-08-17T00:00:00Z')`)
	execRaw(t, db, `INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,authority_principal_id,generation,dedup_key,next_attempt_at,state,created_at) VALUES ('job_1','org_adopt','prj_adopt','env_adopt','tgt_1','converge','usr_adopt',1,'tgt_1','2026-08-17T00:00:00Z','queued','2026-08-17T00:00:00Z')`)

	// The un-adopted artifact adoptAdapter's COUNT requires: gen 1, repo 0, dest 42.
	scope := domain.Scope{Org: "org_adopt", Project: "prj_adopt"}
	if err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_adopt"}, authz.OpAdapterPlan, scope)
		if err != nil {
			return err
		}
		return repos.Adapters().RecordPlan(ctx, p, "tgt_1", "plan_1", 1, 0, 42, []store.AdapterConflictEntry{{Surface: "secret", EffectiveName: "TOKEN"}}, time.Now().UTC())
	}); err != nil {
		t.Fatalf("record plan: %v", err)
	}
}

func adoptLegacyRetirementToken(t *testing.T, db *store.DB) error {
	t.Helper()
	scope := domain.Scope{Org: "org_adopt", Project: "prj_adopt"}
	return storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_adopt"}, authz.OpAdapterAdopt, scope)
		if err != nil {
			return err
		}
		_, err = repos.Adapters().Adopt(ctx, p, store.AdapterAdoption{
			TargetID: "tgt_1", ArtifactID: "plan_1", Entries: []store.AdapterConflictEntry{{Surface: "secret", EffectiveName: "TOKEN"}},
			AuthorityPrincipalID: "usr_adopt", LedgerIDs: []string{"led_adopt"}, JobID: "job_adopt", AuditAt: time.Now().UTC(),
		})
		return err
	})
}

func seedLegacyRetirementPeer(t *testing.T, db *store.DB, provider, currentOrigin, foreignOrigin, foreignProvider, foreignState, foreignScope string, aws bool) {
	t.Helper()
	seedLegacyRetirementFixture(t, db)
	execRaw(t, db, fmt.Sprintf(`UPDATE adapters SET provider='%s',origin='%s' WHERE id='adp_1'`, provider, currentOrigin))
	if aws {
		execRaw(t, db, `UPDATE adapter_targets SET destination_kind='json-object',destination_owner='123456789012',destination_name='TOKEN',destination_id=123456789012 WHERE id='tgt_1'`)
		execRaw(t, db, `UPDATE adapter_conflicts SET destination_id=123456789012 WHERE target_id='tgt_1'`)
	}
	execRaw(t, db, fmt.Sprintf(`INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_peer','org_adopt','prj_adopt','%s','%s','usr_adopt','active','2026-08-17T00:00:00Z')`, foreignProvider, foreignOrigin))
	execRaw(t, db, `INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,destination_scope,name_prefix,generation,state,sync_status,created_at) SELECT 'tgt_peer',org_id,project_id,environment_id,'adp_peer',destination_kind,destination_owner,destination_name,destination_id,'','',1,'active','never',created_at FROM adapter_targets WHERE id='tgt_1'`)
	if foreignState != "" {
		execRaw(t, db, fmt.Sprintf(`INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,destination_scope,repository_id,destination_id,surface,effective_name,normalized_name,state,updated_at) SELECT 'led_peer',org_id,project_id,environment_id,'tgt_peer','%s',destination_kind,'%s',repository_id,destination_id,'secret','TOKEN','TOKEN','%s',created_at FROM adapter_targets WHERE id='tgt_1'`, foreignOrigin, foreignScope, foreignState))
	}
}

func runLegacyOriginPublicRetirement(t *testing.T, db *store.DB) {
	seedLegacyRetirementPeer(t, db, "forgejo", "https://git.example", "https://GIT.example:443/", "forgejo", "owned", "", false)
	if err := adoptLegacyRetirementToken(t, db); !errors.Is(err, adapter.ErrOperatorReview) {
		t.Fatalf("ambiguous legacy custody not held:%v", err)
	}
	// No module factory/keyring is configured. Public metadata-only retirement
	// must still work, and therefore cannot build a client or contact a provider.
	svc := &service.Adapters{DB: db}
	result, err := svc.Delete(t.Context(), service.LocalPrincipal("usr_adopt"), domain.Scope{Org: "org_adopt", Project: "prj_adopt"}, "adp_peer", true)
	if err != nil {
		t.Fatalf("public legacy keep-remote delete:%v", err)
	}
	if len(result.Orphaned) != 1 || result.Orphaned[0] != "secret:TOKEN" {
		t.Fatalf("retirement orphan warning=%v", result.Orphaned)
	}
	if queryInt(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE id='led_peer' AND state='released' AND provider_origin='https://GIT.example:443/'`) != 1 {
		t.Fatal("explicit retirement did not release only legacy claim")
	}
	if queryInt(t, db, `SELECT COUNT(*) FROM adapters WHERE id='adp_peer' AND state='tombstoned'`) != 1 {
		t.Fatal("legacy adapter was not retired")
	}
	if err := adoptLegacyRetirementToken(t, db); err != nil {
		t.Fatalf("fresh verified adoption after retirement:%v", err)
	}
}

func TestLegacyOriginPublicRetirementSQLite(t *testing.T) {
	runLegacyOriginPublicRetirement(t, openSQLite(t))
}

func TestLegacyOriginPublicRetirementPostgres(t *testing.T) {
	runLegacyOriginPublicRetirement(t, openPostgres(t))
}
