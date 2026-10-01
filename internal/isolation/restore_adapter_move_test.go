package isolation

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestSupportedRestoreDestroysPendingAdapterMoveCredentials(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ciphertext := "X'010203'"
		if db.Engine() == store.EnginePostgres {
			ciphertext = "decode('010203','hex')"
		}
		execRaw(t, db, `INSERT INTO adapters(id,org_id,project_id,provider,origin,credential_ciphertext,authority_principal_id,state,created_at) VALUES('adapter_restore','org_a','prj_a1','forgejo','https://original.example',`+ciphertext+`,'usr_root','moving','2026-09-01T00:00:00Z')`)
		execRaw(t, db, `INSERT INTO adapter_route_moves(id,org_id,project_id,adapter_id,kind,pending_origin,pending_credential_ciphertext,authority_principal_id,state,keep_remote,created_at) VALUES('move_restore','org_a','prj_a1','adapter_restore','origin','https://redirect.attacker',`+ciphertext+`,'usr_root','activating',false,'2026-09-01T00:00:00Z')`)
		db = restoreIsolationFixture(t, db)
		if got := queryInt(t, db, `SELECT COUNT(*) FROM adapters WHERE id='adapter_restore' AND credential_ciphertext IS NULL`); got != 1 {
			t.Fatal("active provider credential survived supported restore")
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_route_moves WHERE id='move_restore' AND pending_credential_ciphertext IS NULL AND pending_origin IS NULL AND state='canceled'`); got != 1 {
			t.Fatal("pending route retained archive-selected destination, secret, or activation authority")
		}
	})
}
