package isolation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestOfflineReceiptReplicaRotation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		seedDeliveryCatalogue(t, db)
		ident := identitySvc(db)
		sa, err := ident.CreateServiceAccount(t.Context(), service.LocalPrincipal(identAdmin), prjScope(), "receipt-ha", domain.ClassWorkload)
		if err != nil {
			t.Fatal(err)
		}
		credential, err := ident.MintCredential(t.Context(), service.LocalPrincipal(identAdmin), prjScope(), sa.ID, service.MintRequest{})
		if err != nil {
			t.Fatal(err)
		}
		grantMachineRead(t, db, sa.Principal, envA1)
		rootKey, err := (probeRootSource{db: db}).Current(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		staleKeyring, err := crypto.LoadKeyring(t.Context(), &keyring.Store{DB: db}, rootKey)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
		fresh := deliverySvc(t, db)
		stale := &service.Delivery{DB: db, Keyring: staleKeyring}
		fresh.Now = func() time.Time { return now }
		stale.Now = fresh.Now
		env := scopeEnv(orgA, prjA1, envA1)
		old, err := stale.Fetch(t.Context(), credential.Value, env, "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		offlineReceiptRotateWhileLocked(t, db, staleKeyring, credential.Value, now)
		a, err := fresh.Fetch(t.Context(), credential.Value, env, old.Cursor, service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		b, err := stale.Fetch(t.Context(), credential.Value, env, old.Cursor, service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if a.Current || b.Current || a.ChangeToken == old.ChangeToken || a.Cursor == old.Cursor || a.ChangeToken != b.ChangeToken || a.Cursor != b.Cursor {
			t.Fatal("replicas did not converge on the active token key before conditional comparison")
		}
		current, err := stale.Fetch(t.Context(), credential.Value, env, a.Cursor, service.FetchOptions{})
		if err != nil || !current.Current || len(current.Keys) != 0 {
			t.Fatalf("fresh cursor on stale replica = (%+v, %v)", current, err)
		}
		record := func(result service.FetchResult, id string) service.OfflineRecord {
			t.Helper()
			key := deliveredByName(result.Keys)["DATABASE_URL"]
			if key.Value == nil || key.SnapshotReceipt == nil {
				t.Fatal("plaintext config missing its receipt")
			}
			for _, hidden := range result.Keys {
				if hidden.Value == nil && hidden.SnapshotReceipt != nil {
					t.Fatal("presence-only key received a receipt")
				}
			}
			return service.OfflineRecord{RecordID: id, KeyID: key.KeyID, KeyName: key.Name, Classification: key.Classification,
				OccurredAt: now.Add(time.Minute), CredentialID: credential.Credential.ID,
				Generation: "v1-0123456789abcdef0123456789abcdef", ServedFrom: now, SnapshotReceipt: *key.SnapshotReceipt}
		}
		fresh.Now = func() time.Time { return now.Add(2 * time.Minute) }
		stale.Now = fresh.Now
		for _, replica := range []*service.Delivery{fresh, stale} {
			if _, err := replica.ReconcileOfflineRecords(t.Context(), credential.Value, env, []service.OfflineRecord{record(old, "retired")}); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("retired receipt accepted: %v", err)
			}
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='disclosure.value_revealed' AND origin='offline-reconciled'`); n != 0 {
			t.Fatalf("retired receipt committed %d disclosures", n)
		}
		for i, tt := range []struct {
			replica *service.Delivery
			result  service.FetchResult
			id      string
		}{
			{stale, a, "fresh-a"}, {fresh, b, "fresh-b"},
		} {
			accepted, err := tt.replica.ReconcileOfflineRecords(t.Context(), credential.Value, env, []service.OfflineRecord{record(tt.result, tt.id)})
			if err != nil || accepted.Accepted != 1 {
				t.Fatalf("cross-replica receipt %d = (%+v, %v)", i, accepted, err)
			}
		}
	})
}

// The receipt transaction wins first. Observe the real rotator waiting on the
// selected row (PG) or admitted writer (SQLite), then let both commit. The
// subsequent service assertions prove the retirement winner is used on retry
// and on both independently loaded replicas, not a process-local key handle.
func offlineReceiptRotateWhileLocked(t *testing.T, db *store.DB, kr *crypto.Keyring, bearer string, now time.Time) {
	t.Helper()
	locked, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	firstDone, rotateDone := make(chan error, 1), make(chan error, 1)
	go func() {
		firstDone <- tx.Write(t.Context(), db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			caller, err := az.AuthenticateCaller(ctx, bearer, now)
			if err != nil {
				return err
			}
			proof, err := az.Authorize(ctx, caller, authz.OpDeliveryFetch, scopeEnv(orgA, prjA1, envA1))
			if err != nil {
				return err
			}
			row, err := r.Keys().ActiveTokenKeyForReceipt(ctx, proof)
			if err != nil {
				return err
			}
			snapshot, err := kr.DeliveryTokenSnapshot(row, string(orgA), string(prjA1), string(envA1))
			if err != nil {
				return err
			}
			defer snapshot.Close()
			close(locked)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			// Keep key use inside the same authorized transaction and row lock.
			if !snapshot.Verify([]byte("locked claims"), snapshot.Sign([]byte("locked claims"))) {
				return errors.New("locked receipt failed verification")
			}
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-firstDone:
		t.Fatalf("receipt transaction before lock: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("receipt transaction did not acquire current-key lock")
	}
	var before int64
	if db.Engine() == store.EngineSQLite {
		before = db.SQLiteWrite().Stats().WaitCount
	}
	go func() {
		_, err := revisionSvc(t, db).RotateTokenKey(t.Context(), service.LocalPrincipal(root))
		rotateDone <- err
	}()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		waiting := false
		if db.Engine() == store.EnginePostgres {
			waiting = queryInt(t, db, `SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%RetireTier3KeyAtVersion%'`) > 0
		} else {
			waiting = db.SQLiteWrite().Stats().WaitCount > before
		}
		if waiting {
			break
		}
		select {
		case err := <-rotateDone:
			t.Fatalf("rotation passed an in-flight receipt lock: %v", err)
		case <-deadline.C:
			t.Fatal("rotation did not reach current-key lock")
		case <-ticker.C:
		}
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-rotateDone; err != nil {
		t.Fatal(err)
	}
}
