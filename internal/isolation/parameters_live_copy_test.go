package isolation

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestParameterCopyAndCloneUseLiveSource(t *testing.T) {
	for _, state := range []string{"never-published", "declarations-added", "escaped-template", "live-value-changed", "declarations-removed", "literal-without-declarations", "pending-draft"} {
		t.Run(state, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				identityFixtures(t, db)
				seedDeliveryCatalogue(t, db)
				execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_live_copy_reveal','`+string(identAdmin)+`','reveal','org_a','prj_a1',NULL,`+ts+`)`)
				actor := service.LocalPrincipal(identAdmin)
				projectScope := domain.Scope{Org: orgA, Project: prjA1}
				sourceScope := scopeEnv(orgA, prjA1, envA1)
				envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
				destination, err := envs.Create(t.Context(), actor, projectScope, "live-copy-destination", nil)
				if err != nil {
					t.Fatal(err)
				}
				template := "https://host/${NUMBER}"
				if state == "escaped-template" {
					template = "https://host/$${NUMBER}"
				}
				want := template
				if state == "never-published" {
					source, err := envs.Create(t.Context(), actor, projectScope, "unpublished-live-source", nil)
					if err != nil {
						t.Fatal(err)
					}
					sourceScope.Env = domain.EnvID(source.ID)
					// Create normally materializes an empty initial snapshot. Remove
					// it to exercise the copy source's no-snapshot state explicitly.
					execRaw(t, db, fmt.Sprintf(`DELETE FROM snapshot_entries WHERE environment_id='%s'`, source.ID))
					execRaw(t, db, fmt.Sprintf(`DELETE FROM snapshots WHERE environment_id='%s'`, source.ID))
					// Seed committed live rows independently of delivery snapshots.
					// Values.Set creates private drafts, not these source cells.
					seedLiveParameterCopyCell(t, db, source.ID, "key_fed_url", template)
					seedLiveParameterCopyCell(t, db, source.ID, "key_fed_pw", "secret")
				} else if state != "live-value-changed" && state != "pending-draft" {
					publishDeliveryValues(t, db, envA1, map[string]string{"DATABASE_URL": template})
				}
				if state != "literal-without-declarations" {
					if err := envs.SetParameter(t.Context(), actor, sourceScope, "NUMBER", "[0-9]+", false); err != nil {
						t.Fatal(err)
					}
				}
				if state == "live-value-changed" {
					seedLiveParameterCopyCell(t, db, string(sourceScope.Env), "key_fed_url", template)
				}
				if state == "declarations-removed" {
					publishDeliveryValues(t, db, envA1, map[string]string{"DATABASE_URL": template})
					if err := envs.SetParameter(t.Context(), actor, sourceScope, "NUMBER", "", true); err != nil {
						t.Fatal(err)
					}
				}
				if state == "pending-draft" {
					if _, err := valueSvc(t, db).Set(t.Context(), actor, sourceScope, "DATABASE_URL", template, nil); err != nil {
						t.Fatal(err)
					}
					want = "postgres://dev" // Copy ignores the unpublished private draft.
				}
				refuseCopy := state == "never-published" || state == "declarations-added" || state == "escaped-template" || state == "live-value-changed"
				if refuseCopy {
					// A template refusal must precede even an attempted secret open.
					reencExec(t, db, t.Context(),
						`UPDATE value_entries SET ciphertext=? WHERE environment_id=? AND key_id='key_fed_pw'`,
						`UPDATE value_entries SET ciphertext=$1 WHERE environment_id=$2 AND key_id='key_fed_pw'`,
						[]byte("unopenable-secret"), string(sourceScope.Env))
				}
				disclosures := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='disclosure.value_revealed'`)
				destinationSnapshots := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM snapshots WHERE environment_id='%s'`, destination.ID))
				_, err = valueSvc(t, db).Copy(t.Context(), actor, projectScope, service.CopyRequest{
					SourceEnvironmentID: string(sourceScope.Env), KeyNames: []string{"DATABASE_PASSWORD", "DATABASE_URL"},
					DestinationEnvironmentIDs: []string{destination.ID}, ConfirmProtected: true,
				})
				if refuseCopy {
					if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "declare destination environment parameters") {
						t.Fatalf("live template copied or secret opened: %v", err)
					}
					if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM value_entries WHERE environment_id='%s'`, destination.ID)); got != 0 {
						t.Fatal("refused copy left destination values")
					}
					if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM snapshots WHERE environment_id='%s'`, destination.ID)); got != destinationSnapshots {
						t.Fatal("refused copy published destination")
					}
					if got := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='disclosure.value_revealed'`); got != disclosures {
						t.Fatal("refused copy recorded disclosure")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					assertLiveParameterCopyURL(t, db, domain.EnvID(destination.ID), want)
				}
				clone, _, err := envs.Clone(t.Context(), actor, projectScope, "live-source-clone", string(sourceScope.Env), nil)
				if refuseCopy || state == "pending-draft" {
					if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "cannot clone a parameterized environment") {
						t.Fatalf("clone did not refuse live declarations: %v", err)
					}
					if got := queryInt(t, db, `SELECT COUNT(*) FROM environments WHERE name='live-source-clone'`); got != 0 {
						t.Fatal("refused clone left environment")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					assertLiveParameterCopyURL(t, db, domain.EnvID(clone.ID), want)
				}
			})
		})
	}
}

func seedLiveParameterCopyCell(t *testing.T, db *store.DB, envID, keyID, value string) {
	t.Helper()
	rowID := "live-copy-" + envID + "-" + keyID
	sealer, err := probeKeyring(t, db).ForProject(t.Context(), string(orgA), string(prjA1))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := sealer.SealValue(crypto.ValueAAD{OrgID: string(orgA), ProjectID: string(prjA1), EnvID: envID, KeyID: keyID, RowID: rowID, FieldTag: "value"}, []byte(value))
	if err != nil {
		t.Fatal(err)
	}
	reencExec(t, db, t.Context(), `DELETE FROM value_entries WHERE environment_id=? AND key_id=?`, `DELETE FROM value_entries WHERE environment_id=$1 AND key_id=$2`, envID, keyID)
	reencExec(t, db, t.Context(),
		`INSERT INTO value_entries (id,org_id,project_id,environment_id,key_id,ciphertext,updated_at,updated_by) VALUES (?,'org_a','prj_a1',?,?,?,'2026-01-01T00:00:00Z',?)`,
		`INSERT INTO value_entries (id,org_id,project_id,environment_id,key_id,ciphertext,updated_at,updated_by) VALUES ($1,'org_a','prj_a1',$2,$3,$4,'2026-01-01T00:00:00Z',$5)`,
		rowID, envID, keyID, ciphertext, string(identAdmin))
}

func assertLiveParameterCopyURL(t *testing.T, db *store.DB, env domain.EnvID, want string) {
	t.Helper()
	out, err := deliverySvc(t, db).FetchAs(t.Context(), service.LocalPrincipal(identAdmin), scopeEnv(orgA, prjA1, env), "", service.FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range out.Keys {
		if key.Name == "DATABASE_URL" && key.Value != nil && *key.Value == want {
			return
		}
	}
	t.Fatalf("copied URL differs from live value %q: %+v", want, out.Keys)
}
