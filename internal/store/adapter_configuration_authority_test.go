package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func runAdapterConfigurationWriterAuthority(t *testing.T, engine store.Engine) {
	db := repositoryRecoveryDB(t, engine)
	seedMovePolicy(t, db, false)
	execAdapter(t, db, `INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_sibling','org_744','sibling','2026-08-17T00:00:00Z')`)
	execAdapter(t, db, `INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_foreign','foreign',TRUE,'{}','2026-08-17T00:00:00Z')`)
	execAdapter(t, db, `INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_foreign','org_foreign','foreign','2026-08-17T00:00:00Z')`)
	execAdapter(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_sibling','usr_744','manage-adapters','org_744','prj_sibling',NULL,'2026-08-17T00:00:00Z'),('gr_foreign','usr_744','manage-adapters','org_foreign','prj_foreign',NULL,'2026-08-17T00:00:00Z'),('gr_read','usr_744','read','org_744','prj_744',NULL,'2026-08-17T00:00:00Z')`)
	for _, tc := range []struct {
		name     string
		op       authz.Operation
		scope    domain.Scope
		allowed  bool
		notFound bool
	}{
		{"configure", authz.OpAdapterConfigure, domain.Scope{Org: "org_744", Project: "prj_744"}, true, false},
		{"wrong_operation", authz.OpAdapterInspect, domain.Scope{Org: "org_744", Project: "prj_744"}, false, false},
		{"sibling_project", authz.OpAdapterConfigure, domain.Scope{Org: "org_744", Project: "prj_sibling"}, false, true},
		{"foreign_org", authz.OpAdapterConfigure, domain.Scope{Org: "org_foreign", Project: "prj_foreign"}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
				p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_744"}, tc.op, tc.scope)
				if err != nil {
					t.Fatalf("control proof must authorize: %v", err)
				}
				record, credential, err := repos.Adapters().ConfigurationForUpdate(ctx, p, "adp_move")
				if err == nil && (record.Origin != "https://old.example" || string(credential) != "sealed") {
					t.Fatalf("wrong configuration: %+v %q", record, credential)
				}
				return err
			})
			if tc.allowed && err != nil {
				t.Fatalf("configure refused: %v", err)
			}
			if !tc.allowed && err == nil {
				t.Fatal("configuration disclosed through unauthorized door")
			}
			if tc.notFound && !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("foreign configuration = %v; want not found", err)
			}
		})
	}
}

func TestAdapterConfigurationWriterAuthoritySQLite(t *testing.T) {
	runAdapterConfigurationWriterAuthority(t, store.EngineSQLite)
}

func TestAdapterConfigurationWriterAuthorityPostgres(t *testing.T) {
	runAdapterConfigurationWriterAuthority(t, store.EnginePostgres)
}
