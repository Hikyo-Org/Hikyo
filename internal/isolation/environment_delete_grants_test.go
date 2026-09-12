package isolation

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestEnvironmentDeletionReleasesOnlyScopedGrants(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		for _, viaDefinitions := range []bool{false, true} {
			t.Run(fmt.Sprintf("definitions=%t", viaDefinitions), func(t *testing.T) {
				f := seedDefinitionsProject(t, db, fmt.Sprintf("scoped_grants_%t", viaDefinitions), true)
				grantID := "g_" + f.env
				execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('%s','%s','read','org_a','%s','%s',%s)`, grantID, bob, f.project, f.env, ts))
				seedOrigins(t, db)
				execRaw(t, db, fmt.Sprintf(`INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('extra_%s','%s','scim','provisioning-origin',%s)`, grantID, grantID, ts))
				otherGrants := queryInt(t, db, "SELECT COUNT(*) FROM grants WHERE id <> '"+grantID+"'")
				generation := queryInt(t, db, "SELECT session_generation FROM principals WHERE id = '"+string(bob)+"'")
				if viaDefinitions {
					svc := definitionsService(t, db)
					bundle := parseDefinitions(t, exportDefinitions(t, svc, f))
					bundle.Environments = nil
					plan := planDefinitions(t, svc, f, encodeDefinitions(t, bundle))
					if _, err := svc.Apply(t.Context(), service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{AllowDelete: true}); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := (&service.Environments{DB: db, Keyring: probeKeyring(t, db)}).Delete(t.Context(), service.LocalPrincipal(alice), f.envScope()); err != nil {
						t.Fatal(err)
					}
				}
				if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='grant.revoked' AND object_id='"+grantID+"'"); n != 1 {
					t.Fatalf("revoked grant audit count = %d, want 1", n)
				}
				var payload struct {
					TargetPrincipal  string `json:"target_principal"`
					Capability       string `json:"capability"`
					Scope            string `json:"scope"`
					OriginKind       string `json:"origin_kind"`
					OriginsRemaining int    `json:"origins_remaining"`
					SessionsRevoked  bool   `json:"sessions_revoked"`
				}
				if err := json.Unmarshal([]byte(queryStrings(t, db, "SELECT payload FROM audit_tenant_events WHERE type='grant.revoked' AND object_id='"+grantID+"'")), &payload); err != nil {
					t.Fatal(err)
				}
				if payload.TargetPrincipal != string(bob) || payload.Capability != "read" || payload.OriginKind != "manual,scim" || payload.OriginsRemaining != 0 || !payload.SessionsRevoked {
					t.Fatalf("incomplete revocation event: %+v", payload)
				}
				if n := queryInt(t, db, "SELECT COUNT(*) FROM environments WHERE id = '"+f.env+"'"); n != 0 {
					t.Fatal("environment retained")
				}
				if n := queryInt(t, db, "SELECT COUNT(*) FROM grants WHERE id = '"+grantID+"'"); n != 0 {
					t.Fatal("environment grant retained")
				}
				if n := queryInt(t, db, "SELECT COUNT(*) FROM grant_origins WHERE grant_id = '"+grantID+"'"); n != 0 {
					t.Fatal("grant origins retained")
				}
				if n := queryInt(t, db, "SELECT COUNT(*) FROM grants"); n != otherGrants {
					t.Fatal("unrelated grants changed")
				}
				if n := queryInt(t, db, "SELECT session_generation FROM principals WHERE id = '"+string(bob)+"'"); n != generation+1 {
					t.Fatal("removed authority did not invalidate principal generation")
				}
			})
		}
	})
}
