package isolation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestRestoreRetiresApprovedWorkspaceProof(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		clock := time.Now().UTC()
		ws := stepUpWorkspace(t, db)
		ws.Now = func() time.Time { return clock }
		if _, err := ws.AddOrigin(ctx, service.LocalPrincipal(root), stepUpOrigin); err != nil {
			t.Fatal(err)
		}
		actorToken := seedSessionFactors(t, db, root, `["password","totp"]`)
		actor := service.Bearer(actorToken)
		consumed := establishWorkspace(t, ws, actorToken)
		for _, statement := range []string{
			`INSERT INTO accounts (id,principal_id,username,display_name,created_at) VALUES ('acc_restore_proof','usr_root','restore-proof','Restore proof','2026-09-01T00:00:00Z')`,
			`INSERT INTO login_challenges (id,account_id,factors,expires_at,consumed_at,created_at) VALUES ('lc_restore_pending','acc_restore_proof','["totp"]','2099-01-01T00:00:00Z',NULL,'2026-09-01T00:00:00Z')`,
			`INSERT INTO login_challenges (id,account_id,factors,expires_at,consumed_at,created_at) VALUES ('lc_restore_consumed','acc_restore_proof','["totp"]','2099-01-01T00:00:00Z','2026-09-01T00:01:00Z','2026-09-01T00:00:00Z')`,
		} {
			execRaw(t, db, statement)
		}
		verifier, challenge := pkcePair("restored-pending-workspace-proof")
		started, err := ws.StartHandoff(ctx, service.HandoffRequest{
			Origin: stepUpOrigin, RedirectURI: stepUpOrigin + "/workspace/callback", PKCEChallenge: challenge, Purpose: service.HandoffEstablishment,
		})
		if err != nil {
			t.Fatal(err)
		}
		code, _, err := ws.ApproveHandoff(ctx, actor, started.State)
		if err != nil {
			t.Fatal(err)
		}

		// Real archive restore, proof-based recovery, and runtime reopen are
		// essential: a live pre-restore admission correctly rejects epoch drift.
		db = restoreIsolationFixture(t, db)
		if got := queryInt(t, db, `SELECT COUNT(*) FROM login_challenges WHERE id='lc_restore_pending'`); got != 0 {
			t.Fatalf("pending password proof survived restore: %d", got)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM login_challenges WHERE id='lc_restore_consumed' AND consumed_at IS NOT NULL`); got != 1 {
			t.Fatalf("consumed challenge history lost: %d", got)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM workspace_handoffs WHERE id='`+consumed.HandoffID+`' AND consumed_at IS NOT NULL AND principal_id='usr_root'`); got != 1 {
			t.Fatalf("consumed workspace provenance lost: %d", got)
		}
		if _, err := (&service.Restore{DB: db}).Reconcile(ctx, root); err != nil {
			t.Fatal(err)
		}
		ws = stepUpWorkspace(t, db)
		ws.Now = func() time.Time { return clock }
		stale, staleErr := ws.RedeemHandoff(ctx, code, verifier, stepUpOrigin)

		// Keep time fixed across the supported restore so expiration cannot
		// masquerade as retirement. A new-epoch proof must still work.
		freshActor := service.Bearer(seedSessionFactors(t, db, root, `["password","totp"]`))
		freshVerifier, freshChallenge := pkcePair("fresh-restored-workspace-proof")
		fresh, err := ws.StartHandoff(ctx, service.HandoffRequest{
			Origin: stepUpOrigin, RedirectURI: stepUpOrigin + "/workspace/callback", PKCEChallenge: freshChallenge, Purpose: service.HandoffEstablishment,
		})
		if err != nil {
			t.Fatal(err)
		}
		freshCode, _, err := ws.ApproveHandoff(ctx, freshActor, fresh.State)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ws.RedeemHandoff(ctx, freshCode, freshVerifier, stepUpOrigin); err != nil {
			t.Fatalf("fresh post-restore proof: %v", err)
		}
		if !errors.Is(staleErr, service.ErrHandoffInvalid) || stale.Value != "" {
			t.Fatalf("pre-restore approved proof minted a fresh session: token present=%t, error=%v", stale.Value != "", staleErr)
		}
	})
}

func TestRestoreEpochSupportsAbsentPendingProofTables(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		// These tables did not exist before migrations 00020 and 00056.
		// Exercise the actual epoch-bump owner against their absence. A failed
		// write followed by error swallowing would leave PostgreSQL aborted.
		for _, table := range []string{"login_challenges", "workspace_handoffs"} {
			statement := "DROP TABLE " + table
			if db.Engine() == store.EnginePostgres {
				statement += " CASCADE"
			}
			execRaw(t, db, statement)
		}
		if err := tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			if err := az.AdvanceRestoreEpoch(ctx, time.Now().UTC()); err != nil {
				return err
			}
			// A generated query on the same transaction proves the catalog
			// absence was handled before any undefined-table statement.
			_, err := az.RestoreState(ctx)
			return err
		}); err != nil {
			t.Fatalf("historical pending-proof table absence: %v", err)
		}
	})
}
