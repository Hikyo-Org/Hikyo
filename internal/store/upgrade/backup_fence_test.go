package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/jackc/pgx/v5"
)

func backupFenceIntent(state State) (BackupPreparation, Operation) {
	op := nextOperation(state)
	op.Acceptance.ReleaseRootDigest = state.ReleaseRootDigest
	op.Acceptance.Floor = state.Floor
	op.Acceptance.Floor.HighestReleaseSequence = int64(op.Target.Sequence)
	if state.Pending.Acceptance.Attestation != nil {
		op.Acceptance.Attestation.OperatorKeyID = state.Pending.Acceptance.Attestation.OperatorKeyID
	}
	return BackupPreparation{Target: op.Target, RouteDigest: op.RouteDigest, OperatorKeyID: op.Acceptance.Attestation.OperatorKeyID}, op
}

func TestBackupFenceDrainsRuntimeAndSurvivesCoordinatorRestart(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		state, _, admission := signedAdmissionFixture(t, cfg)
		intent, operation := backupFenceIntent(state)
		var release func() error
		if cfg.Engine == releaseidentity.SQLite {
			guard, err := admission.LockSQLite(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			db, err := open(cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := guard.Check(t.Context(), tx); err != nil {
				t.Fatal(err)
			}
			release = func() error { return errors.Join(tx.Rollback(), guard.Close()) }
		} else {
			conn, err := pgx.Connect(t.Context(), cfg.DSN)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close(context.Background())
			tx, err := conn.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := admission.GuardPostgres(t.Context(), tx); err != nil {
				t.Fatal(err)
			}
			release = func() error { return tx.Rollback(t.Context()) }
		}
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		err := WithLock(ctx, cfg, func(s *Session) error { _, err := s.FenceBackup(ctx, state, intent); return err })
		cancel()
		if err == nil {
			t.Fatal("backup fence crossed an admitted transaction")
		}
		if err := release(); err != nil {
			t.Fatal(err)
		}
		var frozen State
		if err := WithLock(t.Context(), cfg, func(s *Session) error {
			var err error
			frozen, err = s.FenceBackup(t.Context(), state, intent)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if state.Pending.Phase != Healthy || state.Pending.Preparation != nil {
			t.Fatal("fencing mutated caller's source operation")
		}
		if frozen.Applied != state.Applied || frozen.Generation != state.Generation || frozen.SchemaDigest != state.SchemaDigest || frozen.Pending.Target != state.Pending.Target || !frozen.Maintenance {
			t.Fatal("fence falsified installed release or source generation")
		}
		if err := admission.matches(frozen); !errors.Is(err, ErrConflict) {
			t.Fatalf("old runtime admitted after fence: %v", err)
		}
		// Close the migration session, then reconcile through a fresh owner.
		if err := WithLock(t.Context(), cfg, func(s *Session) error {
			read, err := s.Read(t.Context())
			if err != nil {
				return err
			}
			if !equalRecord(read, frozen) {
				t.Fatal("durable fence changed across sessions")
			}
			retried, err := s.FenceBackup(t.Context(), read, intent)
			if err != nil {
				return err
			}
			if !equalRecord(retried, frozen) {
				t.Fatal("retry changed frozen source")
			}
			changed := intent
			changed.RouteDigest = releaseidentity.Hash([]byte("other route"))
			if _, err := s.FenceBackup(t.Context(), read, changed); !errors.Is(err, ErrConflict) {
				t.Fatalf("retargeted frozen source: %v", err)
			}
			prepared, err := s.Prepare(t.Context(), read, operation)
			if err != nil {
				return err
			}
			if prepared.Pending.Preparation != nil || prepared.Pending.Phase != Prepared || prepared.Generation != state.Generation+1 {
				t.Fatal("proof did not convert fence to ordinary preparation")
			}
			raw, err := json.Marshal(prepared.Pending)
			if err != nil {
				return err
			}
			if strings.Contains(string(raw), "preparation") || strings.Contains(string(raw), "backup-preparing") {
				t.Fatal("historical candidate receives unsupported fence fields")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestBackupFenceRejectsWrongProofAndRollsBackFailedPublication(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		state, _, _ := signedAdmissionFixture(t, cfg)
		intent, operation := backupFenceIntent(state)
		if err := WithLock(t.Context(), cfg, func(s *Session) error {
			fault := errors.New("durability failure")
			s.beforeCommit = func() error { return fault }
			if _, err := s.FenceBackup(t.Context(), state, intent); !errors.Is(err, fault) {
				t.Fatalf("fence swallowed failure: %v", err)
			}
			s.beforeCommit = nil
			read, err := s.Read(t.Context())
			if err != nil {
				return err
			}
			if !equalRecord(read, state) {
				t.Fatal("failed fence changed durable state")
			}
			frozen, err := s.FenceBackup(t.Context(), state, intent)
			if err != nil {
				return err
			}
			operation.RouteDigest = releaseidentity.Hash([]byte("unrelated route"))
			if _, err := s.Prepare(t.Context(), frozen, operation); !errors.Is(err, ErrConflict) {
				t.Fatalf("wrong route accepted: %v", err)
			}
			_, operation = backupFenceIntent(state)
			operation.Acceptance.Attestation.OperatorKeyID = releaseidentity.Hash([]byte("other operator"))
			if _, err := s.Prepare(t.Context(), frozen, operation); !errors.Is(err, ErrConflict) {
				t.Fatalf("wrong operator accepted: %v", err)
			}
			read, err = s.Read(t.Context())
			if err != nil {
				return err
			}
			if !equalRecord(read, frozen) {
				t.Fatal("failed proof lifted fence")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestBackupFenceOperatorRotationRequiresRecoveryAndDropsOldIntent(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		state, _, _ := signedAdmissionFixture(t, cfg)
		intent, _ := backupFenceIntent(state)
		if err := WithLock(t.Context(), cfg, func(s *Session) error {
			frozen, err := s.FenceBackup(t.Context(), state, intent)
			if err != nil {
				return err
			}
			epoch, err := s.OperatorCredentialEpoch(t.Context(), frozen)
			if err != nil {
				return err
			}
			rotated, err := s.PlanOperatorRotation(t.Context(), frozen, epoch)
			if err != nil {
				return err
			}
			if rotated.Pending.Preparation != nil || !rotated.Pending.Invalidated || rotated.Pending.Phase != RestoreRequired {
				t.Fatal("rotation retained old backup intent")
			}
			if err := s.ApplyOperatorRotation(t.Context(), frozen, rotated, epoch); err != nil {
				return err
			}
			if _, err := s.FenceBackup(t.Context(), rotated, intent); !errors.Is(err, ErrConflict) {
				t.Fatalf("rotated installation bypassed recovery: %v", err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}
