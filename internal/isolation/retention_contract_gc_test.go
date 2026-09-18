package isolation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// TestRetentionGCReclaimsParameterContractBytes is the F02 regression: the
// parameter contract is payload-class, charged to the project quota beside
// the entries, so collecting a snapshot must release its contract bytes too
// and leave the empty contract behind.
func TestRetentionGCReclaimsParameterContractBytes(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		seedDeliveryCatalogue(t, db)
		actor := service.LocalPrincipal(identAdmin)
		scope := scopeEnv(orgA, prjA1, envA1)
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
		if err := envs.SetParameter(t.Context(), actor, scope, "LABEL", "^é+$", false); err != nil {
			t.Fatal(err)
		}
		// Revision 2 carries the contract; revision 3 makes it collectable
		// under a keep-only-the-newest policy.
		publishDeliveryValues(t, db, envA1, map[string]string{"DATABASE_URL": "${LABEL}"})
		publishDeliveryValues(t, db, envA1, map[string]string{"DATABASE_URL": "postgres://dev-3"})
		contract := queryString(t, db, `SELECT parameter_contract FROM snapshots WHERE environment_id = 'env_a1' AND revision = 2`)
		if !strings.Contains(contract, "é") {
			t.Fatalf("revision 2 contract %q does not carry the declared parameter", contract)
		}
		if raw := queryString(t, db, `SELECT parameter_contract FROM snapshots WHERE environment_id = 'env_a1' AND revision = 1`); raw != "{}" {
			t.Fatalf("revision 1 contract = %q, want the empty contract before any parameter existed", raw)
		}
		collectedEntryBytes := queryInt(t, db, `SELECT COALESCE(SUM(LENGTH(ciphertext)), 0) FROM snapshot_entries
			WHERE environment_id = 'env_a1' AND snapshot_id IN (SELECT id FROM snapshots WHERE environment_id = 'env_a1' AND revision IN (1, 2))`)

		payloadBytes := func() int64 {
			t.Helper()
			var got int64
			if err := tx.Read(t.Context(), db, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
				p, err := az.Authorize(ctx, authz.Identity{Principal: identAdmin}, authz.OpValuePublish, scope)
				if err != nil {
					return err
				}
				got, err = r.Snapshots().PayloadBytesForProject(ctx, p)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			return got
		}
		before := payloadBytes()

		retention := &service.Retention{DB: db, Now: func() time.Time { return time.Now().UTC().Add(365 * 24 * time.Hour) }}
		if _, err := retention.SetProject(t.Context(), service.LocalPrincipal(orgAdmin), scopeProject(orgA, prjA1),
			&service.RetentionPolicy{MaxAge: time.Hour, LastRevisions: 1}); err != nil {
			t.Fatalf("set project retention: %v", err)
		}
		collected, err := retention.Sweep(t.Context())
		if err != nil {
			t.Fatalf("retention sweep: %v", err)
		}
		if collected != 2 {
			t.Fatalf("collected = %d, want revisions 1 and 2 of env_a1 (env_prod keeps its only revision)", collected)
		}
		if raw := queryString(t, db, `SELECT parameter_contract FROM snapshots WHERE environment_id = 'env_a1' AND revision = 2`); raw != "{}" {
			t.Fatalf("collected revision 2 contract = %q, want the empty contract", raw)
		}
		if raw := queryString(t, db, `SELECT parameter_contract FROM snapshots WHERE environment_id = 'env_a1' AND revision = 3`); !strings.Contains(raw, "é") {
			t.Fatalf("live revision 3 contract = %q, want it retained as published", raw)
		}
		after := payloadBytes()
		want := before - collectedEntryBytes - int64(len(contract))
		if after != want {
			t.Fatalf("quota after GC = %d, want %d (before %d minus %d entry bytes minus %d contract bytes)",
				after, want, before, collectedEntryBytes, len(contract))
		}
	})
}
