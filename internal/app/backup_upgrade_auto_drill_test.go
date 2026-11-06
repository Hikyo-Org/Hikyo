package app

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/service"
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

// An installation with only its first administrator holds instance authority and
// no organisation, so no principal can run the credential lifecycle. A tenant
// organisation without an eligible principal refuses even with no stored secret.
func TestUpgradeDrillAutoWithoutCredentialScopeNeedsNoStoredSecret(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		for _, c := range []struct{ secret, tenant bool }{{false, false}, {true, false}, {false, true}} {
			t.Run(fmt.Sprintf("%s/secret=%t/tenant=%t", engine, c.secret, c.tenant), func(t *testing.T) {
				f := newUpgradeDrillFixture(t, engine, c.secret, true, func(db *store.DB) {
					drillExec(t, db, `INSERT INTO principals (id,kind,created_at) VALUES ('usr_first','human','2026-09-05T00:00:00Z')`)
					drillExec(t, db, `INSERT INTO grants (id,principal_id,capability,created_at) VALUES ('gr_first','usr_first','instance-config','2026-09-05T00:00:00Z')`)
					drillExec(t, db, `INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('gor_first','gr_first','manual','usr_first','2026-09-05T00:00:00Z')`)
					if c.secret {
						drillExec(t, db, `DELETE FROM grant_origins WHERE grant_id='gr_drill'`)
						drillExec(t, db, `DELETE FROM grants WHERE id='gr_drill'`)
					}
					if c.tenant {
						drillExec(t, db, `INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_tenant','Tenant',TRUE,'{}','2026-09-05T00:00:00Z')`)
					}
				})
				f.request.Principal, f.request.Scope = "", domain.Scope{}
				f.request.AutoCredentialProof = true
				result, err := DrillUpgrade(t.Context(), f.request)
				if c.secret || c.tenant {
					if !errors.Is(err, service.ErrNoDrillCandidate) || errors.Is(err, service.ErrNoCredentialScope) || len(result.Attestation) != 0 {
						t.Fatalf("a reachable credential scope was waved through: %+v %v", result, err)
					}
					return
				}
				if err != nil || result.SecretProof != "authoritatively-no-secret" || result.CredentialProof != "authoritatively-no-credential-scope" {
					t.Fatalf("first administrator only: %+v %v", result, err)
				}
				material := backupreceipt.EvidenceMaterial{Receipt: f.request.Receipt, Attestation: result.Attestation, Signature: trustfixture.Sign(t, f.signer.PrimarySigner, result.Attestation)}
				inspection := backupreceipt.LegacyInspection{InstanceID: f.source.InstanceID, Engine: releaseidentity.Engine(engine), SchemaSHA256: f.source.SchemaDigest, MigrationSHA256: f.source.MigrationDigest, RestoreEpoch: f.source.RestoreEpoch}
				evidence, err := backupreceipt.VerifyLegacyEvidence(t.Context(), f.request.Operator, f.request.Plan, f.request.Ciphertext, material, inspection, f.proposal, time.Now())
				if err != nil || !evidence.Valid() {
					t.Fatal("the verifying side refused the drill's attestation", err)
				}
			})
		}
	}
}

// seedProtectedConfiguration stores a sealed value in each named organisation, the
// first of which is bound as the instance's protected configuration organisation.
// Only a source that already has the self-configuration binding can hold one, so
// this runs on the schema-65 fixture, not the legacy one.
func seedProtectedConfiguration(orgs []string, principal bool) func(*testing.T, func(string, ...any), *crypto.Keyring) {
	return func(t *testing.T, exec func(string, ...any), kr *crypto.Keyring) {
		t.Helper()
		const at = "2026-09-05T00:00:00Z"
		exec(`INSERT INTO principals (id,kind,created_at) VALUES ('usr_first','human',$1)`, at)
		for i, org := range orgs {
			project, env, key, value := "prj_"+org, "env_"+org, "key_"+org, "val_"+org
			exec(`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ($1,$1,TRUE,'{}',$2)`, org, at)
			exec(`INSERT INTO projects (id,org_id,name,created_at) VALUES ($1,$2,$1,$3)`, project, org, at)
			exec(`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ($1,$2,$3,'e','',$4,0)`, env, org, project, at)
			exec(`INSERT INTO keys (id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) VALUES ($1,$2,$3,'SECRET','','secret','',FALSE,'','{}','none','none',$4)`, key, org, project, at)
			sealer, err := kr.ForProject(t.Context(), org, project)
			if err != nil {
				t.Fatal(err)
			}
			sealed, err := sealer.SealValue(crypto.ValueAAD{OrgID: org, ProjectID: project, EnvID: env, KeyID: key, RowID: value, FieldTag: "value"}, []byte("synthetic-never-output-secret"))
			if err != nil {
				t.Fatal(err)
			}
			exec(`INSERT INTO value_entries (id,org_id,project_id,environment_id,key_id,ciphertext,updated_at,updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,'usr_first')`, value, org, project, env, key, sealed, at)
			if i == 0 {
				exec(`INSERT INTO snapshots (id,org_id,project_id,environment_id,revision,schema_revision,published_by,published_at) VALUES ('snp_cfg',$1,$2,$3,1,1,'usr_first',$4)`, org, project, env, at)
				exec(`INSERT INTO self_config_binding (id,owner_instance_id,adoption_key,adopted_by,org_id,project_id,environment_id,schema_version,generation,desired_revision,desired_snapshot_id,incarnation,created_at,updated_at) VALUES (1,'owner','adoption','usr_first',$1,$2,$3,1,1,1,'snp_cfg','incarnation',$4,$4)`, org, project, env, at)
			}
		}
		if principal {
			exec(`INSERT INTO grants (id,principal_id,capability,created_at) VALUES ('gr_first','usr_first','instance-config',$1)`, at)
			exec(`INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('gor_first','gr_first','manual','usr_first',$1)`, at)
		}
	}
}

// With a stored secret, only the instance's protected configuration organisation
// leaves nowhere to run the credential lifecycle. A tenant organisation without an
// eligible principal, or a principal who cannot list organisations, still refuses.
func TestUpgradeDrillAutoCredentialScopeOnlyWhenOnlyTheProtectedOrganisationExists(t *testing.T) {
	for name, tc := range map[string]struct {
		orgs      []string
		principal bool
		want      string
	}{
		"only the protected configuration":      {orgs: []string{"org_cfg"}, principal: true, want: "authoritatively-no-credential-scope"},
		"a tenant organisation without a grant": {orgs: []string{"org_cfg", "org_tenant"}, principal: true},
		"a principal who cannot list":           {orgs: []string{"org_cfg"}},
	} {
		for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
			t.Run(string(engine)+"/"+name, func(t *testing.T) {
				f := recoverySchema65Fixture(t, engine, seedProtectedConfiguration(tc.orgs, tc.principal))
				f.request.Principal, f.request.Scope = "", domain.Scope{}
				f.request.AutoCredentialProof = true
				result, err := DrillUpgrade(t.Context(), f.request)
				if tc.want == "" {
					if !errors.Is(err, service.ErrNoDrillCandidate) || errors.Is(err, service.ErrNoCredentialScope) || len(result.Attestation) != 0 {
						t.Fatalf("a stored secret with a reachable credential scope was waved through: %+v %v", result, err)
					}
					return
				}
				if err != nil || result.SecretProof != "existing-secret-readable" || result.CredentialProof != tc.want || len(result.Attestation) == 0 {
					t.Fatalf("protected configuration only: %+v %v", result, err)
				}
			})
		}
	}
}
