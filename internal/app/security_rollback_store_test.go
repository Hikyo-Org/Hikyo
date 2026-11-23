package app

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// rollbackFixture is a ledger installation frozen for backup, its authenticated
// upgrade archive, and a live database that the "target" then moved past the fence.
type rollbackFixture struct {
	cfg     upgrade.Config
	plan    upgradecompat.Plan
	archive *backupreceipt.AuthenticatedArchive
	frozen  upgrade.State
	basis   upgrade.RollbackBasis
	pending upgrade.Operation
}

func newRollbackFixture(t *testing.T, engine store.Engine) *rollbackFixture {
	t.Helper()
	return newRollbackFixtureFor(t, engine, false)
}

// newRollbackFixtureFor with native set models the host-fenced route: no backup fence
// exists, so the archive is taken from the healthy source itself.
func newRollbackFixtureFor(t *testing.T, engine store.Engine, native bool) *rollbackFixture {
	t.Helper()
	f := newUpgradeDrillFixture(t, engine, true, true)
	cfg := upgrade.Config{Engine: releaseidentity.Engine(engine), Path: f.cfg.Path, DSN: f.cfg.DSN}
	initial, err := DrillUpgrade(t.Context(), f.request)
	if err != nil {
		t.Fatal(err)
	}
	material := backupreceipt.EvidenceMaterial{Receipt: f.request.Receipt, Attestation: initial.Attestation, Signature: trustfixture.Sign(t, f.signer.PrimarySigner, initial.Attestation)}
	evidence, err := backupreceipt.VerifyLegacyEvidence(t.Context(), f.request.Operator, f.request.Plan, f.request.Ciphertext, material, backupreceipt.LegacyInspection{InstanceID: f.source.InstanceID, Engine: releaseidentity.Engine(engine), SchemaSHA256: f.source.SchemaDigest, MigrationSHA256: f.source.MigrationDigest, RestoreEpoch: f.source.RestoreEpoch}, f.proposal, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	statement := evidence.Statement()
	var incarnation, nonce upgrade.Incarnation
	if err := incarnation.UnmarshalText([]byte(statement.RecoveryIncarnation)); err != nil {
		t.Fatal(err)
	}
	if err := nonce.UnmarshalText([]byte(statement.Nonce)); err != nil {
		t.Fatal(err)
	}
	manifest, err := f.request.Plan.SourceManifest(releaseidentity.Engine(engine))
	if err != nil {
		t.Fatal(err)
	}
	acceptance := upgrade.Acceptance{Floor: f.bundle.Bundle.Snapshot().Floor(), ReleaseRootDigest: releaseidentity.Hash(f.bundle.Pinned.Root), Attestation: &upgrade.AttestationUse{Authority: string(statement.Authority), Nonce: nonce, EvidenceDigest: evidence.Digest(), OperatorKeyID: statement.OperatorKeyID, InstanceID: statement.InstanceID, RestoreEpoch: statement.RestoreEpoch, RecoveryIncarnation: incarnation, RouteGeneration: statement.RouteGeneration, RouteDigest: statement.RouteSHA256, IssuedAt: statement.IssuedAt, ExpiresAt: statement.ExpiresAt}}
	operation := upgrade.Operation{Kind: upgrade.UpgradeOperation, Source: f.source.Source, RouteSource: f.source.Source, Target: f.request.Plan.Target(), SourceMigrationDigest: f.source.MigrationDigest, TargetMigrationDigest: f.source.MigrationDigest, SourceSchemaDigest: f.source.SchemaDigest, TargetSchemaDigest: f.source.SchemaDigest, RouteDigest: f.request.Plan.Digest(), RouteLength: 1, Generation: 1, RecoveryIncarnation: incarnation, BackupID: string(evidence.Receipt().Snapshot.BackupID), Phase: upgrade.Prepared, Acceptance: acceptance}
	var healthy upgrade.State
	err = upgrade.WithLock(t.Context(), cfg, func(session *upgrade.Session) error {
		state, err := session.Bootstrap(t.Context(), manifest, operation, upgrade.Production)
		if err != nil {
			return err
		}
		for _, phase := range []upgrade.Phase{upgrade.SchemaWriteStarted, upgrade.SchemaApplied, upgrade.Healthy} {
			if state, err = session.Advance(t.Context(), state, phase); err != nil {
				return err
			}
		}
		healthy = state
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	inspected, err := upgrade.InspectInstalled(t.Context(), cfg, manifest)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := f.bundle.Bundle.Plan(upgradecompat.InstalledSource{Identity: inspected.Source, Migrations: manifest, SchemaSHA256: inspected.SchemaDigest}, f.bundle.Identities[1])
	if err != nil {
		t.Fatal(err)
	}
	frozen := healthy
	if !native {
		err = upgrade.WithLock(t.Context(), cfg, func(session *upgrade.Session) error {
			var err error
			frozen, err = session.FenceBackup(t.Context(), healthy, upgrade.BackupPreparation{Target: plan.Target(), RouteDigest: plan.Digest(), OperatorKeyID: statement.OperatorKeyID})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	identity, recipient, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	exported := exportDrillPreparation(t, f.cfg, backup.Options{Recipients: []string{recipient}}, plan, nil)
	raw, err := os.ReadFile(exported.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := backupreceipt.PinCiphertext(t.Context(), exported.Path, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pinned.Close() })
	authenticated, err := backupreceipt.AuthenticateArchive(t.Context(), pinned, raw, plan, backup.Unlock{Identity: identity}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { authenticated.Close() })
	snapshot := authenticated.Snapshot()
	if snapshot.SourceGeneration != frozen.Generation || snapshot.RouteGeneration != frozen.Generation+1 {
		t.Fatalf("archive generation %d, fence %d", snapshot.SourceGeneration, frozen.Generation)
	}
	return &rollbackFixture{cfg: cfg, plan: plan, archive: authenticated, frozen: frozen, pending: *frozen.Pending, basis: upgrade.RollbackBasis{
		Instance: snapshot.InstanceID, Source: snapshot.SourceIdentity, Target: plan.Target(), Route: plan.Digest(), BackupID: string(snapshot.BackupID),
		SourceGeneration: snapshot.SourceGeneration, RestoreEpoch: snapshot.RestoreEpoch, RecoveryIncarnation: string(snapshot.RecoveryIncarnation), HostFenced: native}}
}

// moveOn models what Prepare and the target did after the fence: the next
// generation naming the backup, and a write the snapshot does not have.
func (f *rollbackFixture) moveOn(t *testing.T, mutate func(*upgrade.State)) {
	t.Helper()
	state := f.frozen
	pending := f.pending
	pending.Phase, pending.Preparation, pending.BackupID = upgrade.Prepared, nil, f.basis.BackupID
	pending.Source, pending.RouteSource, pending.Target, pending.RouteDigest = state.Applied, state.Applied, f.plan.Target(), f.plan.Digest()
	pending.Hop, pending.RouteLength, pending.Generation = 0, 1, state.Generation+1
	state.Pending, state.Generation, state.Maintenance = &pending, state.Generation+1, true
	if mutate != nil {
		mutate(&state)
	}
	if err := state.Validate(); err != nil {
		t.Fatalf("fixture row is not a valid control row: %v", err)
	}
	operation, err := json.Marshal(state.Pending, jsonwire.Stable)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := json.Marshal(state.Applied, jsonwire.Stable)
	if err != nil {
		t.Fatal(err)
	}
	maintenance := 0
	if state.Maintenance {
		maintenance = 1
	}
	if _, err := execSQL(t, f.cfg, `UPDATE upgrade_control SET generation=$1, maintenance=$2, applied_json=$3 WHERE singleton=1`, state.Generation, maintenance, string(applied)); err != nil {
		t.Fatal(err)
	}
	if _, err := execSQL(t, f.cfg, `UPDATE upgrade_pending SET operation_json=$1 WHERE singleton=1`, string(operation)); err != nil {
		t.Fatal(err)
	}
	if _, err := execSQL(t, f.cfg, `INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_after','After',TRUE,'{}','2026-09-05T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
}

func execSQL(t *testing.T, cfg upgrade.Config, query string, args ...any) (sql.Result, error) {
	t.Helper()
	driver, dsn := "pgx", cfg.DSN
	if cfg.Engine == releaseidentity.SQLite {
		driver, dsn = "sqlite", store.SQLiteDSN(cfg.Path)
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.ExecContext(t.Context(), query, args...)
}

func orgIDs(t *testing.T, cfg upgrade.Config) string {
	t.Helper()
	driver, dsn := "pgx", cfg.DSN
	if cfg.Engine == releaseidentity.SQLite {
		driver, dsn = "sqlite", store.SQLiteDSN(cfg.Path)
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.QueryContext(t.Context(), `SELECT id FROM orgs ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids += id + ","
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

// rollback runs the lock, the decision and the restore as the app does.
func (f *rollbackFixture) rollback(ctx context.Context, basis upgrade.RollbackBasis) (needed bool, err error) {
	err = upgrade.WithRollbackLock(ctx, f.cfg, func(rb *upgrade.Rollback) error {
		var err error
		if needed, err = rb.Decide(ctx, basis); err != nil || !needed {
			return err
		}
		admission, err := rb.Admit(basis)
		if err != nil {
			return err
		}
		reader, err := f.archive.Open()
		if err != nil {
			return err
		}
		if f.cfg.Engine == releaseidentity.SQLite {
			_, err = store.RollbackUpgradeSQLite(ctx, reader, f.plan, admission)
			return err
		}
		return store.RollbackUpgradePostgres(ctx, store.Config{Engine: store.EnginePostgres, DSN: f.cfg.DSN}, f.archive, f.plan, admission, store.MigrationsFS, "migrations/postgres")
	})
	return needed, err
}

func TestRollbackRestoresTheFencedSnapshotAndNothingElse(t *testing.T) {
	rollbackEngines(t, checkRollbackRestoresTheFencedSnapshotAndNothingElse)
}

func checkRollbackRestoresTheFencedSnapshotAndNothingElse(t *testing.T, engine store.Engine) {
	f := newRollbackFixture(t, engine)
	cfg := f.cfg
	f.moveOn(t, nil)
	// A stale WAL and shm from the dead target must never meet the restored file.
	stale := []string{"-wal", "-shm"}
	if engine == store.EnginePostgres {
		stale = nil
	}
	for _, suffix := range stale {
		if err := os.WriteFile(cfg.Path+suffix, []byte("stale"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := f.frozen
	needed, err := f.rollback(t.Context(), f.basis)
	if err != nil || !needed {
		t.Fatalf("needed=%t err=%v", needed, err)
	}
	for _, suffix := range stale {
		if _, err := os.Stat(cfg.Path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale %s survived: %v", suffix, err)
		}
	}
	after, err := upgrade.InspectControl(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if after.Generation != before.Generation || after.RestoreEpoch != before.RestoreEpoch || after.RecoveryIncarnation != before.RecoveryIncarnation || after.InstanceID != before.InstanceID || after.Pending.Phase != upgrade.BackupPreparing || !after.Maintenance || after.Pending.Invalidated {
		t.Fatalf("restored row is not the fence: %+v", after)
	}
	if ids := orgIDs(t, cfg); ids != "org_drill," {
		t.Fatalf("data after rollback = %q", ids)
	}
	// The row is now untouched, so a resumed rollback needs no second restore.
	if needed, err := f.rollback(t.Context(), f.basis); err != nil || needed {
		t.Fatalf("a restored source asked for another restore: needed=%t err=%v", needed, err)
	}
}

func TestRollbackRefusesAnythingButTheFencedSnapshotsOwnRows(t *testing.T) {
	rollbackEngines(t, checkRollbackRefusesAnythingButTheFencedSnapshotsOwnRows)
}

func checkRollbackRefusesAnythingButTheFencedSnapshotsOwnRows(t *testing.T, engine store.Engine) {
	wrong := func(b *upgrade.RollbackBasis) { b.BackupID = string(releaseidentity.Hash([]byte("another backup"))) }
	for _, test := range []struct {
		name   string
		mutate func(*rollbackFixture, *upgrade.State)
		basis  func(*upgrade.RollbackBasis)
	}{
		{name: "another backup id", basis: wrong},
		{name: "healthy at the target: it may have served", mutate: func(f *rollbackFixture, s *upgrade.State) {
			s.Maintenance, s.Pending.Phase, s.Applied = false, upgrade.Healthy, upgrade.Source{Release: s.Pending.Target}
		}},
		{name: "generation not above the snapshot", mutate: func(f *rollbackFixture, s *upgrade.State) {
			// A fence re-taken for another route repeats the snapshot's generation.
			frozen := f.frozen
			*s = frozen
			p := *frozen.Pending
			p.BackupID, p.Preparation = f.basis.BackupID, &upgrade.BackupPreparation{Target: p.Preparation.Target, RouteDigest: releaseidentity.Hash([]byte("another route")), OperatorKeyID: p.Preparation.OperatorKeyID}
			s.Pending = &p
		}},
		{name: "another instance", basis: func(b *upgrade.RollbackBasis) { b.Instance = "another-instance" }},
		{name: "another restore epoch", basis: func(b *upgrade.RollbackBasis) { b.RestoreEpoch++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newRollbackFixture(t, engine)
			cfg := f.cfg
			f.moveOn(t, func(s *upgrade.State) {
				if test.mutate != nil {
					test.mutate(f, s)
				}
			})
			basis := f.basis
			if test.basis != nil {
				test.basis(&basis)
			}
			if needed, err := f.rollback(t.Context(), basis); err == nil || needed {
				t.Fatalf("rolled back: needed=%t err=%v", needed, err)
			}
			if ids := orgIDs(t, cfg); ids != "org_after,org_drill," {
				t.Fatalf("a refusal replaced the database: %q", ids)
			}
		})
	}
}

func TestRollbackRefusesAnArchiveFromAnotherRouteBeforePublishing(t *testing.T) {
	rollbackEngines(t, checkRollbackRefusesAnArchiveFromAnotherRouteBeforePublishing)
}

func checkRollbackRefusesAnArchiveFromAnotherRouteBeforePublishing(t *testing.T, engine store.Engine) {
	f := newRollbackFixture(t, engine)
	cfg := f.cfg
	f.moveOn(t, nil)
	basis := f.basis
	basis.Route = releaseidentity.Hash([]byte("another route"))
	if _, err := f.rollback(t.Context(), basis); err == nil {
		t.Fatal("an archive fenced for another route was restored")
	}
	if engine == store.EngineSQLite {
		if ids := orgIDs(t, cfg); ids != "org_after,org_drill," {
			t.Fatalf("the live database was replaced: %q", ids)
		}
		return
	}
	// PostgreSQL is emptied before the import, so the check can only keep the
	// wrong archive from being committed: the row is absent and "rolling-back" repeats.
	if _, err := upgrade.InspectControl(t.Context(), cfg); !errors.Is(err, upgrade.ErrAbsent) {
		t.Fatalf("a wrong archive was published: %v", err)
	}
}

func TestRollbackReadFailuresAreNotPermission(t *testing.T) {
	rollbackEngines(t, checkRollbackReadFailuresAreNotPermission)
}

func checkRollbackReadFailuresAreNotPermission(t *testing.T, engine store.Engine) {
	f := newRollbackFixture(t, engine)
	cfg := f.cfg
	f.moveOn(t, nil)
	if engine == store.EngineSQLite {
		if err := os.Remove(cfg.Path); err != nil {
			t.Fatal(err)
		}
	} else if _, err := execSQL(t, cfg, `DROP TABLE upgrade_pending, upgrade_nonces, upgrade_control`); err != nil {
		t.Fatal(err)
	}
	// Absent outside "rolling-back", or at the wrong database, stays refused.
	if _, err := f.rollback(t.Context(), f.basis); err == nil {
		t.Fatal("an absent database was read as permission outside rolling-back")
	}
	basis := f.basis
	basis.Rolling, basis.Database = true, "another-database"
	if _, err := f.rollback(t.Context(), basis); err == nil {
		t.Fatal("an absent database was accepted under another identity")
	}
	// Absent at the recorded identity resumes, and creates the file from the archive.
	var identity string
	err := upgrade.WithRollbackLock(t.Context(), cfg, func(rb *upgrade.Rollback) (err error) {
		identity, err = rb.Identity(t.Context())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	basis.Database = identity
	if needed, err := f.rollback(t.Context(), basis); err != nil || !needed {
		t.Fatalf("crash resume: needed=%t err=%v", needed, err)
	}
	if ids := orgIDs(t, cfg); ids != "org_drill," {
		t.Fatalf("resumed data = %q", ids)
	}
}

func rollbackEngines(t *testing.T, check func(*testing.T, store.Engine)) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) { check(t, engine) })
	}
}

func TestRollbackPostgresRefusesWhileAnotherSessionIsConnected(t *testing.T) {
	f := newRollbackFixture(t, store.EnginePostgres)
	f.moveOn(t, nil)
	other, err := sql.Open("pgx", f.cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := other.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.rollback(t.Context(), f.basis); err == nil {
		t.Fatal("a rollback ran beside a connected session")
	}
	if ids := orgIDs(t, f.cfg); ids != "org_after,org_drill," {
		t.Fatalf("the refusal changed the database: %q", ids)
	}
}

// journaledRollback writes the evidence and journal a stopped target leaves, and
// runs the locked part of the rollback against the fixture's live database.
func (f *rollbackFixture) journaledRollback(t *testing.T, phase string, mutate func(*automaticJournal, string)) (restored bool, journal *automaticJournal, evidence string, err error) {
	t.Helper()
	directory := t.TempDir()
	evidence = filepath.Join(directory, "evidence")
	if err := os.Mkdir(evidence, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"receipt.json", "attestation.json", "attestation.sigstore.json"} {
		if err := os.WriteFile(filepath.Join(evidence, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := f.plan.SourceManifest(f.cfg.Engine)
	if err != nil {
		t.Fatal(err)
	}
	journal = &automaticJournal{Format: "hikyo.container-upgrade/v1", Phase: phase, Target: f.plan.Target(), Instance: f.basis.Instance, Route: f.plan.Digest(), Hop: 1,
		Source:  upgradecompat.InstalledSource{Identity: f.plan.Source(), Migrations: manifest, SchemaSHA256: f.plan.SourceSchemaDigest()},
		Runtime: hostupgrade.RuntimeEvidence{EvidenceDirectory: evidence, CiphertextPath: filepath.Join(directory, "backup.age"), BundleDirectory: directory}}
	if mutate != nil {
		mutate(journal, directory)
	}
	path := filepath.Join(directory, "operation.json")
	if err := writeAutomaticJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	basis := f.basis
	basis.Rolling, basis.Database = phase == "rolling-back", journal.RollbackDatabase
	cfg := &config.Config{Store: config.Datastore{Engine: config.Engine(f.cfg.Engine), Path: f.cfg.Path, DSN: f.cfg.DSN}}
	restored, err = rollbackJournaled(t.Context(), cfg, f.cfg, path, journal, f.archive, f.plan, basis, nil)
	return restored, journal, evidence, err
}

func TestRollbackJournaledLeavesAJournalTheSourceAccepts(t *testing.T) {
	rollbackEngines(t, func(t *testing.T, engine store.Engine) {
		f := newRollbackFixture(t, engine)
		f.moveOn(t, nil)
		restored, journal, evidence, err := f.journaledRollback(t, "write-intent", nil)
		if err != nil || !restored {
			t.Fatalf("restored=%t err=%v", restored, err)
		}
		for _, name := range []string{"attestation.json", "attestation.sigstore.json"} {
			if _, err := os.Stat(filepath.Join(evidence, name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s survived the rollback: %v", name, err)
			}
		}
		written, err := readAutomaticJournal(filepath.Join(filepath.Dir(evidence), "operation.json"))
		if err != nil || written.Phase != "preparing" || written.Aborted == "" || written.RollbackDatabase != "" || written.Runtime.EvidenceDirectory != "" || written.Runtime.CiphertextPath != "" || written.Hop != 0 {
			t.Fatalf("journal after rollback: %+v err=%v", written, err)
		}
		state, err := upgrade.InspectControl(t.Context(), f.cfg)
		if err != nil || !written.preparationNeverWrote(state, nil) {
			t.Fatalf("the source does not accept the rolled-back journal: %v", err)
		}
		// The same restored row is not accepted while the journal still says "rolling-back".
		rolling := *journal
		rolling.Phase = "rolling-back"
		if rolling.preparationNeverWrote(state, nil) {
			t.Fatal("the source accepted a journal that is still rolling back")
		}
	})
}

func TestRollbackJournaledResumesFromRollingBack(t *testing.T) {
	rollbackEngines(t, func(t *testing.T, engine store.Engine) {
		f := newRollbackFixture(t, engine)
		f.moveOn(t, nil)
		var identity string
		if err := upgrade.WithRollbackLock(t.Context(), f.cfg, func(rb *upgrade.Rollback) (err error) {
			identity, err = rb.Identity(t.Context())
			return err
		}); err != nil {
			t.Fatal(err)
		}
		// A first run died after restoring and before the cleanup: the row is already the fence.
		if restored, _, _, err := f.journaledRollback(t, "write-intent", nil); err != nil || !restored {
			t.Fatalf("first run: restored=%t err=%v", restored, err)
		}
		restored, _, evidence, err := f.journaledRollback(t, "rolling-back", func(j *automaticJournal, _ string) { j.RollbackDatabase = identity })
		if err != nil || !restored {
			t.Fatalf("resume over a restored row: restored=%t err=%v", restored, err)
		}
		if _, err := os.Stat(filepath.Join(evidence, "attestation.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the resume left the attestation: %v", err)
		}
		// A run that finds the row untouched under a journal that never started rolling back
		// restores nothing, but still takes the exit: the lagging journal would pin the source.
		restored, input, evidence, err := f.journaledRollback(t, "write-intent", nil)
		if err != nil || restored {
			t.Fatalf("an untouched row was not released: restored=%t err=%v", restored, err)
		}
		if _, err := os.Stat(filepath.Join(evidence, "attestation.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the release left the attestation: %v", err)
		}
		if written, err := readAutomaticJournal(filepath.Join(input.Runtime.BundleDirectory, "operation.json")); err != nil || written.Phase != "preparing" || written.Aborted == "" {
			t.Fatalf("journal after the release: %+v %v", written, err)
		}
	})
}

func TestRollbackPreflightRefusesObjectsTheRoleDoesNotOwn(t *testing.T) {
	f := newRollbackFixture(t, store.EnginePostgres)
	if err := upgrade.PreflightPostgresRollback(t.Context(), f.cfg); err != nil {
		t.Fatalf("an owned schema was refused: %v", err)
	}
	// Roles are cluster-wide, so a unique name keeps an interrupted run from blocking the next.
	role := fmt.Sprintf("rollback_preflight_%d", time.Now().UnixNano())
	if _, err := execSQL(t, f.cfg, `CREATE ROLE `+role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = execSQL(t, f.cfg, `REASSIGN OWNED BY `+role+` TO CURRENT_USER`)
		_, _ = execSQL(t, f.cfg, `DROP ROLE `+role)
	})
	if _, err := execSQL(t, f.cfg, `ALTER TABLE orgs OWNER TO `+role); err != nil {
		t.Fatal(err)
	}
	if err := upgrade.PreflightPostgresRollback(t.Context(), f.cfg); err == nil {
		t.Fatal("a schema holding another role's table was accepted")
	}
}
