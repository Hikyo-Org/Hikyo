package isolation

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/app"
	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/devupgrade"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
	"github.com/jackc/pgx/v5"
)

// recoverRestoredTarget performs the supported recovery ceremony explicitly.
// Data restoration alone cannot reopen runtime admission. A new authenticated
// backup and real isolated drill must prove escrow before the same-release gate.
func recoverRestoredTarget(t *testing.T, target drillTarget, c custody) {
	t.Helper()
	recoverRestoredTargetAs(t, target, c, identAdmin)
}

func recoverRestoredTargetAs(t *testing.T, target drillTarget, c custody, principal domain.PrincipalID) {
	t.Helper()
	cfg := target.storeConfig()
	uc := upgrade.Config{Engine: releaseidentity.Engine(cfg.Engine), Path: cfg.Path, DSN: cfg.DSN}
	control, err := upgrade.InspectControl(t.Context(), uc)
	if err != nil {
		t.Fatal(err)
	}
	if control.Pending == nil || !control.Pending.Invalidated || !control.Maintenance {
		t.Fatal("restore did not invalidate runtime authority")
	}
	if db, err := openBootedIsolationFixture(t, target.cfg); err == nil {
		db.Close()
		t.Fatal("ordinary restore reopened runtime without new evidence")
	}
	material, err := devupgrade.Open(t.Context(), target.cfg.Upgrade.StateDirectory)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := upgradebundle.Load(t.Context(), material.Directory, material.Pinned, control.Floor)
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := buildcompat.Development()
	if err != nil {
		t.Fatal(err)
	}
	node, err := bundle.MatchBuild(raw)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := node.Manifest(uc.Engine)
	if err != nil {
		t.Fatal(err)
	}
	source, err := upgrade.InspectInstalled(t.Context(), uc, manifest)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := bundle.Plan(upgradecompat.InstalledSource{Identity: source.Source, Migrations: manifest, SchemaSHA256: source.SchemaDigest}, node.Identity())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps()) != 0 {
		t.Fatal("same-release recovery unexpectedly changes release")
	}
	var exported service.ExportResult
	err = upgrade.WithLock(t.Context(), uc, func(session *upgrade.Session) error {
		authority, err := session.PrepareExport(t.Context(), plan)
		if err != nil {
			return err
		}
		db, err := store.OpenPreparation(t.Context(), cfg, authority)
		if err != nil {
			return err
		}
		defer db.Close()
		exported, err = service.ExportPreparedUpgrade(t.Context(), db, backup.Options{Recipients: []string{c.recipient(t)}}, t.TempDir(), plan, nil)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := os.ReadFile(exported.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := backupreceipt.PinCiphertext(t.Context(), exported.Path, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	signer := trustfixture.New(t)
	operator, err := backupreceipt.PinOperator(source.InstanceID, signer.PrimaryPublic)
	if err != nil {
		t.Fatal(err)
	}
	result, err := app.DrillUpgrade(t.Context(), app.UpgradeDrillRequest{
		Scratch: drillScratch(t, target), Ciphertext: pinned, Receipt: receipt, Plan: plan,
		Operator: operator, Unlock: backup.Unlock{Identity: c.read(t, c.backupStore, "identity")}, RootKey: c.rootKey(t),
		Principal: principal, Scope: prjScope(), Now: time.Now().UTC(), Lifetime: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.HierarchyReadable || result.CredentialProof != "reconciled-minted-revoked" {
		t.Fatal("real scratch custody or credential recovery proof incomplete")
	}
	root := c.rootKey(t)
	defer crypto.Zero(root)
	admitted, err := upgradegate.RunDevelopment(t.Context(), upgradegate.Request{
		Store: uc, BundleDirectory: material.Directory, Pinned: material.Pinned,
		Migrations: store.MigrationsFS, MigrationDirectory: "migrations/" + string(cfg.Engine),
		RootKey: root, Mode: upgradegate.Boot, AllowMigrations: true, Operator: operator, Ciphertext: pinned,
		Evidence: backupreceipt.EvidenceMaterial{Receipt: receipt, Attestation: result.Attestation, Signature: trustfixture.Sign(t, signer.PrimarySigner, result.Attestation)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !admitted.Admission.Valid() || admitted.State.Pending == nil || admitted.State.Pending.Kind != upgrade.RecoveryOperation || admitted.State.Maintenance || admitted.State.Generation != control.Generation+1 || admitted.State.RestoreEpoch != control.RestoreEpoch {
		t.Fatal("same-release recovery did not retain epoch and advance fenced generation")
	}
}

// forgeFixtureArchive models an attacker with raw datastore bytes and the
// public backup recipient. It intentionally bypasses the honest exporter only
// to construct malicious test INPUT; no runtime authority is fabricated.
func forgeFixtureArchive(t *testing.T, db *store.DB, c custody, original string) string {
	t.Helper()
	input, err := os.Open(original)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	var plain bytes.Buffer
	if err := backup.ExtractTo(&plain, input, backup.Unlock{Identity: c.read(t, c.backupStore, "identity")}); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "forged.hikyo.age")
	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	encrypted, err := backup.Encrypt(out, backup.Options{Recipients: []string{c.recipient(t)}})
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(encrypted)
	reader := tar.NewReader(&plain)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		payload, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case header.Name == "payload/sqlite.db":
			snapshot := filepath.Join(t.TempDir(), "forged.db")
			if _, err := db.SQLiteWrite().ExecContext(t.Context(), "VACUUM INTO ?", snapshot); err != nil {
				t.Fatal(err)
			}
			payload, err = os.ReadFile(snapshot)
			if err != nil {
				t.Fatal(err)
			}
		case strings.HasPrefix(header.Name, "payload/postgres/"):
			table := strings.TrimPrefix(header.Name, "payload/postgres/")
			conn, err := db.PG().Acquire(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			var copy bytes.Buffer
			_, err = conn.Conn().PgConn().CopyTo(t.Context(), &copy, "COPY "+pq(table)+" TO STDOUT")
			conn.Release()
			if err != nil {
				t.Fatal(err)
			}
			payload = copy.Bytes()
		}
		header.Size = int64(len(payload))
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := encrypted.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return outPath
}

// restoreIsolationFixture preserves the real fixture's signed custody while
// replacing the database through actual archive restore and proof-based recovery.
// Callers must explicitly replace every service's old runtime handle.
func restoreIsolationFixture(t *testing.T, db *store.DB) *store.DB {
	t.Helper()
	value, ok := isolationCustody.Load(db)
	if !ok {
		t.Fatal("restore fixture requires original signed bundle custody")
	}
	material, ok := value.(devupgrade.Material)
	if !ok {
		t.Fatal("invalid restore fixture custody")
	}
	root, err := (probeRootSource{db: db}).Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.Zero(root)
	c := newCustody(t)
	if err := os.WriteFile(filepath.Join(c.rootStore, "rootkey"), []byte(crypto.EncodeRootKey(root)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := store.Config{Engine: db.Engine()}
	if cfg.Engine == store.EnginePostgres {
		cfg.DSN = db.PG().Config().ConnString()
	} else {
		var seq int
		var name string
		if err := db.SQLiteRead().QueryRowContext(t.Context(), "PRAGMA database_list").Scan(&seq, &name, &cfg.Path); err != nil {
			t.Fatal(err)
		}
	}
	target := drillTarget{cfg: &config.Config{Dev: true, AutoMigrate: true, Store: config.Datastore{Engine: config.Engine(cfg.Engine), Path: cfg.Path, DSN: cfg.DSN}, BackupDir: t.TempDir(), BackupRecipients: []string{c.recipient(t)}, Upgrade: config.UpgradeConfiguration{StateDirectory: filepath.Dir(filepath.Dir(material.Directory))}}}
	target.configureCustody(t, c)
	// This distinct fixture operator exists only to prove scratch mint/revoke.
	// It is never reconciled on the actual restored target or used for SCIM facts.
	const operator = domain.PrincipalID("usr_isolation_recovery")
	execRaw(t, db, "INSERT INTO principals (id,kind,created_at) VALUES ('"+string(operator)+"','human',"+ts+")")
	for i, capability := range []string{"manage-identities", "manage-members", "read"} {
		execRaw(t, db, "INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_isolation_recovery_"+string(rune('a'+i))+"','"+string(operator)+"','"+capability+"','org_a','prj_a1',NULL,"+ts+")")
	}
	seedOrigins(t, db)
	archive := exportArchive(t, target)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if cfg.Engine == store.EnginePostgres {
		conn, err := pgx.Connect(t.Context(), cfg.DSN)
		if err != nil {
			t.Fatal(err)
		}
		_, err = conn.Exec(t.Context(), "DROP SCHEMA public CASCADE; CREATE SCHEMA public")
		_ = conn.Close(t.Context())
		if err != nil {
			t.Fatal(err)
		}
	} else {
		for _, suffix := range []string{"", "-wal", "-shm", ".lock"} {
			if err := os.Remove(cfg.Path + suffix); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
	}
	if err := app.RunRestore(t.Context(), target.cfg, drillLogger(), []string{"run", "--from", archive, "--identity-file", c.identityFile()}, io.Discard, nil, nil); err != nil {
		t.Fatal(err)
	}
	recoverRestoredTargetAs(t, target, c, operator)
	restored := target.open(t)
	loadAndRegisterKeyring(t, restored, c.rootKey(t))
	isolationCustody.Store(restored, material)
	t.Cleanup(func() { isolationCustody.Delete(restored) })
	return restored
}
