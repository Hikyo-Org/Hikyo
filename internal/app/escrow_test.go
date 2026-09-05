package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	gatefixture "github.com/Hikyo-Org/hikyo/internal/upgradegate/testfixture"
)

func TestEscrowLocalCLIUsesAdmittedCurrentHierarchy(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			source := upgradeDrillDatabase(t, engine)
			root := bytes.Repeat([]byte{83}, 32)
			_, material := gatefixture.PrepareWithMaterial(t, backupGateConfig(source), store.MigrationsFS, "migrations/"+string(engine), root)
			primary := filepath.Join(t.TempDir(), "server-root")
			recovered := filepath.Join(t.TempDir(), "recovered-root")
			encoded := []byte(crypto.EncodeRootKey(root))
			for _, path := range []string{primary, recovered} {
				if err := os.WriteFile(path, encoded, 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg := &config.Config{Dev: true, RootKeyFile: primary, Store: config.Datastore{Engine: config.Engine(engine), Path: source.Path, DSN: source.DSN}, Upgrade: config.UpgradeConfiguration{StateDirectory: filepath.Dir(filepath.Dir(material.Directory))}}
			var output bytes.Buffer
			run := func(path string, assertion bool) error {
				args := []string{"verify", "--root-key-file", path}
				if assertion {
					args = append(args, "--assert-separate-custody")
				}
				return RunEscrow(t.Context(), cfg, quietLogger(), args, &output, nil, nil)
			}
			if err := run(recovered, false); err == nil {
				t.Fatal("missing custody assertion accepted")
			}
			if err := run(primary, true); err == nil {
				t.Fatal("server root accepted as separate custody")
			}
			if err := run(recovered, true); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(output.String(), string(encoded)) {
				t.Fatal("private root leaked to output")
			}
			db, err := openBackupRuntime(t.Context(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			health, err := (&service.Retention{DB: db}).OperationalHealth(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !health.Diagnostics.EscrowCurrent {
				t.Fatal("CLI did not persist current admitted escrow proof")
			}
		})
	}
}

func TestEscrowLocalCLIRefusesUnstampedProductionBeforeDatabase(t *testing.T) {
	root := filepath.Join(t.TempDir(), "recovered-root")
	if err := os.WriteFile(root, []byte(crypto.EncodeRootKey(bytes.Repeat([]byte{84}, 32))), 0600); err != nil {
		t.Fatal(err)
	}
	primary := filepath.Join(t.TempDir(), "server-root")
	if err := os.WriteFile(primary, []byte(crypto.EncodeRootKey(bytes.Repeat([]byte{84}, 32))), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{RootKeyFile: primary, Store: config.Datastore{Engine: config.EngineSQLite, Path: filepath.Join(t.TempDir(), "must-not-exist.db")}}
	if err := RunEscrow(t.Context(), cfg, quietLogger(), []string{"verify", "--root-key-file", root, "--assert-separate-custody"}, &bytes.Buffer{}, nil, nil); err == nil {
		t.Fatal("unstamped production local command accepted")
	}
	if _, err := os.Stat(cfg.Store.Path); !os.IsNotExist(err) {
		t.Fatal("refusal created database", err)
	}
}
