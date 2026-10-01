package isolation

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestDynamicRuntimeGeneratedFenceAndSubsecondDeadline(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		now := time.Now().UTC().Truncate(time.Second)
		due := now.Add(time.Microsecond)
		stamp := func(v time.Time) string { return "'" + store.CanonTime(v).Format("2006-01-02T15:04:05.000000Z") + "'" }
		execRaw(t, db, `INSERT INTO dynamic_providers (id,org_id,project_id,kind,origin,tls_mode,grant_role,authority_principal_id,state,created_at) VALUES ('dpr_sqlc','org_a','prj_a1','postgres','postgres://sqlc.example/db','verify-full','reader','usr_alice','active',`+stamp(now)+`)`)
		execRaw(t, db, `INSERT INTO dynamic_leases (id,org_id,project_id,environment_id,provider_id,principal_id,principal_class,provider_handle,state,max_ttl_seconds,last_transition_at,next_attempt_at,created_at) VALUES ('dls_sqlc','org_a','prj_a1','env_a1','dpr_sqlc','usr_alice','human','sqlc-role','minting',60,`+stamp(now)+`,`+stamp(due)+`,`+stamp(now)+`)`)
		runtime := store.NewDynamicRuntime(db)
		if _, found, err := runtime.ClaimDueLease(t.Context(), "sqlc-worker", now, now.Add(time.Hour)); err != nil || found {
			t.Fatalf("future microsecond lease claimed: found=%v err=%v", found, err)
		}
		lease, found, err := runtime.ClaimDueLease(t.Context(), "sqlc-worker", due, now.Add(time.Hour))
		if err != nil || !found {
			t.Fatalf("due lease not claimed: found=%v err=%v", found, err)
		}
		effect, err := runtime.RecordIntent(t.Context(), lease, "mint")
		if err != nil {
			t.Fatal(err)
		}
		beforeAudit := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events")
		beforeEffects := queryInt(t, db, "SELECT COUNT(*) FROM dynamic_effects")
		for _, field := range []string{"org", "project", "environment"} {
			t.Run(field, func(t *testing.T) {
				forged := lease
				switch field {
				case "org":
					forged.OrgID = "org_b"
				case "project":
					forged.ProjectID = "prj_a2"
				case "environment":
					forged.EnvironmentID = "env_a2"
				}
				if _, err := runtime.RecordIntent(t.Context(), forged, "mint"); !errors.Is(err, store.ErrConflict) {
					t.Fatalf("forged intent: %v", err)
				}
				if err := runtime.RecordOutcome(t.Context(), forged, effect, "mint", "success", "active", now, now.Add(time.Minute)); !errors.Is(err, store.ErrConflict) {
					t.Fatalf("forged outcome: %v", err)
				}
				if err := runtime.Retry(t.Context(), forged, now.Add(time.Minute)); !errors.Is(err, store.ErrConflict) {
					t.Fatalf("forged retry: %v", err)
				}
				if _, err := runtime.LatestEffectKind(t.Context(), forged); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("forged effect lookup: %v", err)
				}
				if _, err := runtime.LoadProviderMaterial(t.Context(), forged); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("forged provider lookup: %v", err)
				}
				if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events"); got != beforeAudit {
					t.Fatalf("audit changed: %d -> %d", beforeAudit, got)
				}
				if got := queryInt(t, db, "SELECT COUNT(*) FROM dynamic_effects"); got != beforeEffects {
					t.Fatalf("effect count changed: %d -> %d", beforeEffects, got)
				}
				if got := queryInt(t, db, fmt.Sprintf("SELECT COUNT(*) FROM dynamic_effects WHERE id='%s' AND outcome IS NULL", effect)); got != 1 {
					t.Fatal("forged outcome changed effect")
				}
			})
		}
		if err := runtime.RecordOutcome(t.Context(), lease, effect, "mint", "success", "active", now, now.Add(time.Minute)); err != nil {
			t.Fatalf("valid outcome: %v", err)
		}
	})
}
