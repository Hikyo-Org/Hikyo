package app

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/jackc/pgx/v5"
)

func TestUpgradeDrillAutomaticallyProvesExistingCredentialAuthority(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		for _, orgScope := range []bool{false, true} {
			name := string(engine) + "/project"
			if orgScope {
				name = string(engine) + "/org"
			}
			t.Run(name, func(t *testing.T) {
				f := newUpgradeDrillFixture(t, engine, true, true, func(db *store.DB) {
					// Earlier unsuitable candidates must not be accepted merely to
					// make the automatic proof succeed.
					drillExec(t, db, `INSERT INTO principals (id,kind,created_at) VALUES ('usr_a_no_grants','human','2026-09-05T00:00:00Z')`)
					if orgScope {
						drillExec(t, db, `UPDATE grants SET project_id=NULL WHERE id='gr_drill'`)
						drillExec(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_read','usr_drill','read','org_drill',NULL,NULL,'2026-09-05T00:00:00Z')`)
						drillExec(t, db, `INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('gor_read','gr_read','manual','usr_drill','2026-09-05T00:00:00Z')`)
					}
				})
				f.request.Principal, f.request.Scope = "", domain.Scope{}
				f.request.AutoCredentialProof = true
				result, err := DrillUpgrade(t.Context(), f.request)
				if err != nil {
					t.Fatal(err)
				}
				if result.CredentialProof != "reconciled-minted-revoked" || !result.HierarchyReadable || result.SecretProof != "existing-secret-readable" {
					t.Fatal("automatic drill did not prove real restored data and credential lifecycle")
				}
			})
		}
	}
}

func TestUpgradeDrillAutoSelectionSkipsUnusableScopeBeforeCommit(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		for _, nextPrincipal := range []bool{false, true} {
			for _, emptyOrg := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/next-principal=%t/empty-org=%t", engine, nextPrincipal, emptyOrg), func(t *testing.T) {
					f := newUpgradeDrillFixture(t, engine, true, true, func(db *store.DB) {
						// Both candidates use org scope so sorting cannot skip this
						// regression by prioritizing the valid project-scoped grant.
						drillExec(t, db, `UPDATE grants SET project_id=NULL WHERE id='gr_drill'`)
						drillExec(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,created_at) VALUES ('gr_read','usr_drill','read','org_drill','2026-09-05T00:00:00Z')`)
						drillExec(t, db, `INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('gor_read','gr_read','manual','usr_drill','2026-09-05T00:00:00Z')`)
						drillExec(t, db, `INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_a_unusable','Empty or unreadable',TRUE,'{}','2026-09-05T00:00:00Z')`)
						principal := "usr_drill"
						if nextPrincipal {
							principal = "usr_a_unusable"
							drillExec(t, db, `INSERT INTO principals (id,kind,created_at) VALUES ('usr_a_unusable','human','2026-09-05T00:00:00Z')`)
						}
						drillExec(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,created_at) VALUES ('gr_unusable',$1,'manage-identities','org_a_unusable','2026-09-05T00:00:00Z')`, principal)
						drillExec(t, db, `INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('gor_unusable','gr_unusable','manual',$1,'2026-09-05T00:00:00Z')`, principal)
						if emptyOrg {
							// Authorized project enumeration produces an empty set.
							drillExec(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,created_at) VALUES ('gr_empty_read',$1,'read','org_a_unusable','2026-09-05T00:00:00Z')`, principal)
							drillExec(t, db, `INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('gor_empty_read','gr_empty_read','manual',$1,'2026-09-05T00:00:00Z')`, principal)
						} else {
							// This org has a project, but no existing read authority.
							drillExec(t, db, `INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_a_unusable','org_a_unusable','Unreadable','2026-09-05T00:00:00Z')`)
						}
					})
					f.request.Principal, f.request.Scope = "", domain.Scope{}
					f.request.AutoCredentialProof = true
					result, err := DrillUpgrade(t.Context(), f.request)
					if err != nil {
						t.Fatal(err)
					}
					if result.CredentialProof != "reconciled-minted-revoked" {
						t.Fatal("later existing authority did not prove credentials")
					}
					assertOnlyAutomaticDrillPrincipalReconciled(t, f.request.Scratch, nextPrincipal)
				})
			}
		}
	}
}

func assertOnlyAutomaticDrillPrincipalReconciled(t *testing.T, cfg store.Config, haveUnusablePrincipal bool) {
	t.Helper()
	var count func(string) int
	if cfg.Engine == store.EngineSQLite {
		db, err := sql.Open("sqlite", store.SQLiteDSN(cfg.Path))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		count = func(query string) int {
			t.Helper()
			var n int
			if err := db.QueryRowContext(t.Context(), query).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
	} else {
		db, err := pgx.Connect(t.Context(), cfg.DSN)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close(t.Context())
		count = func(query string) int {
			t.Helper()
			var n int
			if err := db.QueryRow(t.Context(), query).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
	}
	if count(`SELECT COUNT(*) FROM principals WHERE id='usr_drill' AND reconciled_epoch=(SELECT restore_epoch FROM auth_instance_state WHERE id=1)`) != 1 {
		t.Fatal("selected existing principal was not reconciled")
	}
	if haveUnusablePrincipal && count(`SELECT COUNT(*) FROM principals WHERE id='usr_a_unusable' AND reconciled_epoch<(SELECT restore_epoch FROM auth_instance_state WHERE id=1)`) != 1 {
		t.Fatal("unsuitable principal reconciliation was not rolled back")
	}
	if count(`SELECT COUNT(*) FROM audit_instance_events WHERE type='restore.principal_reconciled'`) != 1 {
		t.Fatal("automatic selection committed reconciliation audit events for unsuitable principals")
	}
}

func TestUpgradeDrillAutoSelectionRefusesLostGrantAuthority(t *testing.T) {
	f := newUpgradeDrillFixture(t, store.EngineSQLite, true, true, func(db *store.DB) {
		drillExec(t, db, `DELETE FROM grant_origins WHERE grant_id='gr_drill'`)
		drillExec(t, db, `DELETE FROM grants WHERE id='gr_drill'`)
	})
	f.request.Principal, f.request.Scope = "", domain.Scope{}
	f.request.AutoCredentialProof = true
	if _, err := DrillUpgrade(t.Context(), f.request); err == nil {
		t.Fatal("automatic drill fabricated credential authority")
	}
}

func TestUpgradeDrillManualSelectionStillRequiredByDefault(t *testing.T) {
	f := newUpgradeDrillFixture(t, store.EngineSQLite, true, true)
	f.request.Principal, f.request.Scope = "", domain.Scope{}
	if _, err := DrillUpgrade(t.Context(), f.request); err == nil {
		t.Fatal("ordinary manual drill silently selected a principal")
	}
}
