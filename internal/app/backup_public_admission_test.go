package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	gatefixture "github.com/Hikyo-Org/hikyo/internal/upgradegate/testfixture"
)

func TestOrdinaryBackupPublicAdmissionAndDataOnlyRestore(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			source := upgradeDrillDatabase(t, engine)
			_, material := gatefixture.PrepareWithMaterial(t, backupGateConfig(source), store.MigrationsFS, "migrations/"+string(engine), bytes.Repeat([]byte{61}, 32))
			cfg := &config.Config{Dev: true, Store: config.Datastore{Engine: config.Engine(engine), Path: source.Path, DSN: source.DSN}, Upgrade: config.UpgradeConfiguration{StateDirectory: filepath.Dir(filepath.Dir(material.Directory))}}
			db, err := openBackupRuntime(t.Context(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			identity, recipient, err := backup.GenerateIdentity()
			if err != nil {
				db.Close()
				t.Fatal(err)
			}
			result, err := (&service.Backup{DB: db, Options: backup.Options{Recipients: []string{recipient}}}).Export(t.Context(), t.TempDir())
			if closeErr := db.Close(); err != nil || closeErr != nil {
				t.Fatal(err, closeErr)
			}
			if result.Manifest.Upgrade != nil || result.Manifest.Format != store.ArchiveFormat {
				t.Fatal("ordinary archive changed format")
			}
			keyFile := filepath.Join(t.TempDir(), "backup.identity")
			if err := os.WriteFile(keyFile, []byte(identity), 0600); err != nil {
				t.Fatal(err)
			}
			target := upgradeDrillDatabase(t, engine)
			restoredCfg := backupTargetConfig(cfg, target)
			var output bytes.Buffer
			if err := runRestoreRun(t.Context(), restoredCfg, quietLogger(), []string{"run", "--from", result.Path, "--identity-file", keyFile}, &output); err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(output.Bytes(), []byte("credential epoch")) {
				t.Fatal("restore omitted invalidation outcome")
			}
			state, err := upgrade.InspectControl(t.Context(), backupGateConfig(target))
			if err != nil {
				t.Fatal(err)
			}
			if !state.Maintenance || state.Pending == nil || !state.Pending.Invalidated || state.Pending.Phase != upgrade.RestoreRequired {
				t.Fatal("data restore admitted serving")
			}
			if admitted, err := openBackupRuntime(t.Context(), restoredCfg); err == nil {
				admitted.Close()
				t.Fatal("restored instance opened runtime without new current-incarnation proof")
			}
			if err := runRestoreStatus(t.Context(), restoredCfg, &output); err != nil {
				t.Fatal(err)
			}

		})
	}
}

func TestInvalidPublicTrustDoesNotCreateSQLiteForBackupOrRestoreStatus(t *testing.T) {
	for _, verb := range []string{"export", "status"} {
		t.Run(verb, func(t *testing.T) {
			cfg := &config.Config{Store: config.Datastore{Engine: config.EngineSQLite, Path: filepath.Join(t.TempDir(), "must-not-exist.db")}}
			var err error
			if verb == "export" {
				_, err = openBackupRuntime(t.Context(), cfg)
			} else {
				err = runRestoreStatus(t.Context(), cfg, &bytes.Buffer{})
			}
			if err == nil {
				t.Fatal("unstamped production trust admitted local data command")
			}
			if _, err := os.Stat(cfg.Store.Path); !os.IsNotExist(err) {
				t.Fatal("invalid trust created database", err)
			}
			if _, err := os.Stat(cfg.Store.Path + ".lock"); !os.IsNotExist(err) {
				t.Fatal("invalid trust acquired database writer lock", err)
			}
		})
	}
}
