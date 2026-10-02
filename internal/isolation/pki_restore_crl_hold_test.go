package isolation

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/gofrs/flock"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPKIRestoreHoldBlocksFreshCRLSigning(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		clock := &pkiClock{now: time.Now().UTC().Truncate(time.Second)}
		svc := pkiSvc(t, db, clock)
		op := service.LocalPrincipal(root)
		created, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "root", Name: "held-crl", CommonName: "Held CRL", TTL: 365 * 24 * time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.PublishIssuerCRL(ctx, op, "held-crl", 1); err != nil {
			t.Fatal(err)
		}
		storedDER, err := svc.IssuerCRL(ctx, op, "held-crl", 1)
		if err != nil {
			t.Fatal(err)
		}
		clock.Advance(13 * time.Hour)
		candidates, err := svc.Runtime.DueCRLs(ctx, clock.Now())
		if err != nil || len(candidates) != 1 {
			t.Fatalf("pre-hold candidate: count=%d err=%v", len(candidates), err)
		}
		candidate := candidates[0]
		execRaw(t, db, "UPDATE pki_issuers SET restore_hold=1 WHERE id='"+created.Issuer.ID+"'")
		// A held signer must refuse before opening its key. A nil keyring makes
		// any accidental key-open visible instead of masking it with valid custody.
		withoutKeys := *svc
		withoutKeys.Keyring = nil
		if _, err := withoutKeys.PublishIssuerCRL(ctx, op, "held-crl", 1); !errors.Is(err, service.ErrPKIIssuerHeld) {
			t.Fatalf("held manual signing: %v", err)
		}
		if due, err := svc.Runtime.DueCRLs(ctx, clock.Now()); err != nil || len(due) != 0 {
			t.Fatalf("held issuer entered worker signing candidates: %d %v", len(due), err)
		}
		if moved, err := withoutKeys.RunPKISweep(ctx); err != nil || moved {
			t.Fatalf("held worker performed signing: moved=%v err=%v", moved, err)
		}
		called := false
		if published, err := svc.Runtime.SignAndPublishCRL(ctx, candidate, clock.Now(), clock.Now().Add(24*time.Hour), func(store.PKICRLCandidate, []store.PKIRevokedEntry) ([]byte, int64, error) {
			called = true
			return storedDER, candidate.CRLNumber + 1, nil
		}); err != nil || published || called {
			t.Fatalf("pre-hold candidate bypassed fresh signer check: published=%v signed=%v err=%v", published, called, err)
		}
		// Even a candidate captured before the hold cannot replace the stored
		// CRL, move its sequence/number, or emit a successful publication audit.
		beforeAudit := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='pki.crl_published'")
		if published, err := svc.Runtime.PublishCRL(ctx, candidate, storedDER, candidate.CRLNumber+1, 0, clock.Now(), clock.Now().Add(24*time.Hour)); err != nil || published {
			t.Fatalf("pre-hold worker candidate published: %v %v", published, err)
		}
		if afterAudit := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='pki.crl_published'"); afterAudit != beforeAudit {
			t.Fatal("held publication emitted a success audit")
		}
		readDER, err := withoutKeys.IssuerCRL(ctx, op, "held-crl", 1)
		if err != nil || !bytes.Equal(readDER, storedDER) {
			t.Fatalf("held stored CRL read changed or required signing: %v", err)
		}
		if _, err := svc.ReleaseIssuerHold(ctx, op, "held-crl"); err != nil {
			t.Fatal(err)
		}
		if moved, err := svc.RunPKISweep(ctx); err != nil || !moved {
			t.Fatalf("reconciled worker did not resume: moved=%v err=%v", moved, err)
		}
		refreshed, err := svc.IssuerCRL(ctx, op, "held-crl", 1)
		if err != nil || bytes.Equal(refreshed, storedDER) {
			t.Fatalf("reconciled CRL was not refreshed: %v", err)
		}
		if _, err := svc.PublishIssuerCRL(ctx, op, "held-crl", 1); err != nil {
			t.Fatalf("reconciled manual publication: %v", err)
		}
	})
}

func TestPKICRLSigningRetainsRestoreMaintenanceExclusion(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		clock := &pkiClock{now: time.Now().UTC().Truncate(time.Second)}
		svc := pkiSvc(t, db, clock)
		if _, err := svc.CreateIssuer(ctx, service.LocalPrincipal(root), service.PKIIssuerRequest{Mode: "root", Name: "signing-guard", CommonName: "Signing guard", TTL: 365 * 24 * time.Hour}); err != nil {
			t.Fatal(err)
		}
		candidates, err := svc.Runtime.DueCRLs(ctx, clock.Now())
		if err != nil || len(candidates) != 1 {
			t.Fatalf("signing candidate count=%d err=%v", len(candidates), err)
		}
		var fileLock *flock.Flock
		if db.Engine() == store.EngineSQLite {
			path := queryString(t, db, "SELECT file FROM pragma_database_list WHERE name='main'")
			fileLock = flock.New(path + ".lock")
		}
		probeExclusion := func(held bool) {
			t.Helper()
			if db.Engine() == store.EngineSQLite {
				acquired, err := fileLock.TryLock()
				if err != nil {
					t.Error(err)
					return
				}
				if acquired {
					if err := fileLock.Unlock(); err != nil {
						t.Error(err)
					}
				}
				if acquired == held {
					t.Errorf("SQLite restore maintenance acquired=%v while signer-held=%v", acquired, held)
				}
				return
			}
			maintenance, err := db.PG().Begin(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			defer maintenance.Rollback(ctx)
			_, err = maintenance.Exec(ctx, "SELECT singleton FROM upgrade_control WHERE singleton=1 FOR UPDATE NOWAIT")
			if held {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
					t.Errorf("Postgres maintenance crossed signing admission guard: %v", err)
				}
			} else if err != nil {
				t.Errorf("Postgres signing did not release maintenance exclusion: %v", err)
			}
		}
		stop := errors.New("stop after guarded local signing probe")
		called := false
		published, err := svc.Runtime.SignAndPublishCRL(ctx, candidates[0], clock.Now(), clock.Now().Add(24*time.Hour), func(fresh store.PKICRLCandidate, _ []store.PKIRevokedEntry) ([]byte, int64, error) {
			called = true
			if fresh.IssuerID != candidates[0].IssuerID || len(fresh.EncryptedPrivateKey) == 0 {
				t.Error("signing callback did not receive fresh guarded issuer custody")
			}
			probeExclusion(true)
			return nil, 0, stop
		})
		if !called || published || !errors.Is(err, stop) {
			t.Fatalf("guarded signing probe called=%v published=%v err=%v", called, published, err)
		}
		probeExclusion(false)
	})
}
