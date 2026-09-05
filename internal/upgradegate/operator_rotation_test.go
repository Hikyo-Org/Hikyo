package upgradegate

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

func operatorRotationFixture(t *testing.T, engine releaseidentity.Engine) (Request, []byte, func() error, upgrade.State, *trustfixture.Fixture, *trustfixture.Fixture) {
	t.Helper()
	request, claim, verify := signedFreshGate(t, engine)
	prior, next := trustfixture.New(t), trustfixture.New(t)
	request.StateDirectory = t.TempDir()
	if err := os.Chmod(request.StateDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	request.InitialOperatorPublicKey = prior.PrimaryPublic
	request.Mode = Boot
	request.AllowMigrations = true
	runGate := func() error { _, err := run(t.Context(), request, claim, upgrade.Production, verify); return err }
	if err := runGate(); err != nil {
		t.Fatal(err)
	}
	state, err := upgrade.InspectControl(t.Context(), request.Store)
	if err != nil {
		t.Fatal(err)
	}
	return request, claim, runGate, state, prior, next
}
func rotationRequest(t *testing.T, request Request, state upgrade.State, prior, next *trustfixture.Fixture, mode backupreceipt.RotationMode) OperatorRotationRequest {
	t.Helper()
	oldPin, err := backupreceipt.PinOperator(state.InstanceID, prior.PrimaryPublic)
	if err != nil {
		t.Fatal(err)
	}
	newPin, err := backupreceipt.PinOperator(state.InstanceID, next.PrimaryPublic)
	if err != nil {
		t.Fatal(err)
	}
	var strongest int64
	err = upgrade.WithLock(t.Context(), request.Store, func(session *upgrade.Session) error {
		var err error
		strongest, err = session.OperatorCredentialEpoch(t.Context(), state)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	incarnation, _ := state.RecoveryIncarnation.MarshalText()
	rotation := backupreceipt.Rotation{Format: backupreceipt.RotationFormat, Mode: mode, InstanceID: state.InstanceID, RecoveryIncarnation: backupreceipt.Nonce(incarnation), RestoreEpoch: state.RestoreEpoch, MaxKnownCredentialEpoch: strongest, NextEpoch: strongest + 1, CurrentKeyID: oldPin.KeyID(), NewKeyID: newPin.KeyID(), IssuedAt: time.Now().UTC().Truncate(time.Second)}
	raw, err := json.Marshal(rotation)
	if err != nil {
		t.Fatal(err)
	}
	signer := prior.PrimarySigner
	if mode == backupreceipt.LocalBreakGlass {
		signer = next.PrimarySigner
	}
	return OperatorRotationRequest{Store: request.Store, StateDirectory: request.StateDirectory, NewPublicKey: next.PrimaryPublic, Statement: raw, Signature: trustfixture.Sign(t, signer, raw)}
}
func TestOperatorInstallationPinPersistsWithoutBackupEvidence(t *testing.T) {
	req, _, restart, state, prior, next := operatorRotationFixture(t, releaseidentity.SQLite)
	if err := restart(); err != nil {
		t.Fatal(err)
	}
	if _, err := InstalledOperator(t.Context(), req.StateDirectory, state.InstanceID, next.PrimaryPublic); err == nil {
		t.Fatal("configured replacement accepted")
	}
	if _, err := InstalledOperator(t.Context(), req.StateDirectory, "other-instance", prior.PrimaryPublic); err == nil {
		t.Fatal("different instance accepted")
	}
	if err := os.Remove(filepath.Join(req.StateDirectory, operatorCustodyName)); err != nil {
		t.Fatal(err)
	}
	if err := restart(); err == nil {
		t.Fatal("missing installation pin recreated for existing database")
	}
}
func TestOperatorRotationPriorKeyAndLocalEscrow(t *testing.T) {
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) {
			req, _, restart, before, prior, next := operatorRotationFixture(t, engine)
			rotation := rotationRequest(t, req, before, prior, next, backupreceipt.PriorKeyRotation)
			forged := rotation
			forged.Signature = trustfixture.Sign(t, next.PrimarySigner, rotation.Statement)
			if _, err := RotateOperator(t.Context(), forged); err == nil {
				t.Fatal("new-key-only prior rotation accepted")
			}
			after, err := RotateOperator(t.Context(), rotation)
			if err != nil {
				t.Fatal(err)
			}
			if after.RestoreEpoch <= before.RestoreEpoch || after.Generation != before.Generation+1 || !after.Pending.Invalidated || !after.Maintenance || after.RecoveryIncarnation == before.RecoveryIncarnation {
				t.Fatal("rotation did not invalidate authority")
			}
			if err := restart(); err == nil {
				t.Fatal("old configured pin admitted")
			}
			if _, err := InstalledOperator(t.Context(), req.StateDirectory, before.InstanceID, next.PrimaryPublic); err != nil {
				t.Fatal(err)
			}
			if _, err := RotateOperator(t.Context(), rotation); err == nil {
				t.Fatal("rotation replay accepted")
			}
			breakglass := rotationRequest(t, req, after, next, prior, backupreceipt.LocalBreakGlass)
			if _, err := RotateOperator(t.Context(), breakglass); err == nil {
				t.Fatal("breakglass without escrow accepted")
			}
			breakglass.LocalRecoveryRoot = bytes.Repeat([]byte{19}, 32)
			if _, err := RotateOperator(t.Context(), breakglass); err == nil {
				t.Fatal("breakglass with wrong escrow accepted")
			}
			breakglass.LocalRecoveryRoot = bytes.Clone(req.RootKey)
			if _, err := RotateOperator(t.Context(), breakglass); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestOperatorRotationJournalCrashResume(t *testing.T) {
	for _, boundary := range []string{"journal-durable", "database-committed"} {
		t.Run(boundary, func(t *testing.T) {
			req, _, restart, before, prior, next := operatorRotationFixture(t, releaseidentity.SQLite)
			rotation := rotationRequest(t, req, before, prior, next, backupreceipt.PriorKeyRotation)
			interrupted := errors.New("simulated process loss after durable boundary")
			rotation.afterBoundary = func(reached string) error {
				if reached == boundary {
					return interrupted
				}
				return nil
			}
			if _, err := RotateOperator(t.Context(), rotation); !errors.Is(err, interrupted) {
				t.Fatalf("fault boundary did not run: %v", err)
			}
			file, err := openOperatorFile(t.Context(), req.StateDirectory, nil, false)
			if err != nil {
				t.Fatal(err)
			}
			if file.value.Journal == nil {
				file.close()
				t.Fatal("actual rotation did not preserve journal")
			}
			expected := file.value.Journal.After
			file.close()
			if err := restart(); err == nil || !strings.Contains(err.Error(), "rotation incomplete") {
				t.Fatalf("journal did not block boot: %v", err)
			}
			rotation.afterBoundary = nil
			after, err := RotateOperator(t.Context(), rotation)
			if err != nil {
				t.Fatal(err)
			}
			if !sameOperatorState(after, expected) {
				t.Fatal("retry minted different authority")
			}
		})
	}
}

func TestOperatorCustodyRejectsSymlinksAndUnsafeMode(t *testing.T) {
	prior := trustfixture.New(t)
	for _, kind := range []string{"directory-mode", "file-symlink", "file-mode"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			file, err := openOperatorFile(t.Context(), dir, prior.PrimaryPublic, true)
			if err != nil {
				t.Fatal(err)
			}
			file.close()
			path := filepath.Join(dir, operatorCustodyName)
			switch kind {
			case "directory-mode":
				err = os.Chmod(dir, 0755)
			case "file-mode":
				err = os.Chmod(path, 0644)
			case "file-symlink":
				target := filepath.Join(t.TempDir(), "custody")
				err = os.Rename(path, target)
				if err == nil {
					err = os.Symlink(target, path)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			file, err = openOperatorFile(t.Context(), dir, prior.PrimaryPublic, false)
			if err == nil {
				file.close()
				t.Fatal("unsafe custody accepted")
			}
		})
	}
}

func TestOperatorHistoricalRestoreRequiresCurrentPinAndEscrow(t *testing.T) {
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) {
			req, _, _, historical, retired, current := operatorRotationFixture(t, engine)
			first := rotationRequest(t, req, historical, retired, current, backupreceipt.PriorKeyRotation)
			rotated, err := RotateOperator(t.Context(), first)
			if err != nil {
				t.Fatal(err)
			}
			successor := trustfixture.New(t)
			second := rotationRequest(t, req, rotated, current, successor, backupreceipt.PriorKeyRotation)
			latest, err := RotateOperator(t.Context(), second)
			if err != nil {
				t.Fatal(err)
			}
			// Reproduce restore of the old control and credential snapshot. Existing
			// encrypted hierarchy remains identical, as in an archive from this instance.
			driver, dsn := "pgx", req.Store.DSN
			if engine == releaseidentity.SQLite {
				driver, dsn = "sqlite", req.Store.Path
			}
			db, err := sql.Open(driver, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			incarnation, _ := historical.RecoveryIncarnation.MarshalText()
			pending, _ := json.Marshal(historical.Pending)
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.ExecContext(t.Context(), `UPDATE upgrade_control SET restore_epoch=$1,incarnation=$2,generation=$3,maintenance=0`, historical.RestoreEpoch, string(incarnation), historical.Generation); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(t.Context(), `UPDATE upgrade_pending SET operation_json=$1`, string(pending)); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(t.Context(), `UPDATE auth_instance_state SET credential_epoch=0,restore_epoch=0`); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			next := trustfixture.New(t)
			repair := rotationRequest(t, req, historical, successor, next, backupreceipt.LocalBreakGlass)
			statement, err := backupreceipt.ParseRotation(repair.Statement)
			if err != nil {
				t.Fatal(err)
			}
			statement.MaxKnownCredentialEpoch = latest.RestoreEpoch
			statement.NextEpoch = latest.RestoreEpoch + 1
			repair.Statement = trustfixture.JSON(t, statement)
			repair.Signature = trustfixture.Sign(t, next.PrimarySigner, repair.Statement)
			if _, err := RotateOperator(t.Context(), repair); err == nil {
				t.Fatal("historical restore recovered without local escrow")
			}
			repair.LocalRecoveryRoot = bytes.Clone(req.RootKey)
			result, err := RotateOperator(t.Context(), repair)
			if err != nil {
				t.Fatal(err)
			}
			if result.RestoreEpoch <= latest.RestoreEpoch || !result.Pending.Invalidated {
				t.Fatal("historical restore did not exceed external floor")
			}
			if _, err := InstalledOperator(t.Context(), req.StateDirectory, result.InstanceID, retired.PrimaryPublic); err == nil {
				t.Fatal("retired archive key became authoritative")
			}
		})
	}
}

func TestOperatorUncommittedJournalAcceptsFreshAuthenticatedReplacement(t *testing.T) {
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) {
			req, _, _, before, prior, abandoned := operatorRotationFixture(t, engine)
			interrupted := rotationRequest(t, req, before, prior, abandoned, backupreceipt.PriorKeyRotation)
			loss := errors.New("process lost before DB commit")
			interrupted.afterBoundary = func(boundary string) error {
				if boundary == "journal-durable" {
					return loss
				}
				return nil
			}
			if _, err := RotateOperator(t.Context(), interrupted); !errors.Is(err, loss) {
				t.Fatal(err)
			}
			driver, dsn := "pgx", req.Store.DSN
			if engine == releaseidentity.SQLite {
				driver, dsn = "sqlite", req.Store.Path
			}
			db, err := sql.Open(driver, dsn)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(t.Context(), `UPDATE auth_instance_state SET credential_epoch=9 WHERE id=1`); err != nil {
				db.Close()
				t.Fatal(err)
			}
			db.Close()
			interrupted.afterBoundary = nil
			if _, err := RotateOperator(t.Context(), interrupted); err == nil {
				t.Fatal("stale signed epoch accepted")
			}
			successor := trustfixture.New(t)
			replacement := rotationRequest(t, req, before, prior, successor, backupreceipt.PriorKeyRotation)
			result, err := RotateOperator(t.Context(), replacement)
			if err != nil {
				t.Fatal(err)
			}
			if result.RestoreEpoch != 10 {
				t.Fatal("replacement did not exceed newly observed credential stamp")
			}
			if _, err := InstalledOperator(t.Context(), req.StateDirectory, before.InstanceID, abandoned.PrimaryPublic); err == nil {
				t.Fatal("uncommitted proposed signer became authority")
			}
		})
	}
}
