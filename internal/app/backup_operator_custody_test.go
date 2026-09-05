package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
	gatefixture "github.com/Hikyo-Org/hikyo/internal/upgradegate/testfixture"
)

func TestProductionBackupCustodyRefusesBeforeDatabaseAdmission(t *testing.T) {
	source := upgradeDrillDatabase(t, store.EngineSQLite)
	gatefixture.Prepare(t, backupGateConfig(source), store.MigrationsFS, "migrations/sqlite", bytes.Repeat([]byte{46}, 32))
	before, err := upgrade.InspectControl(t.Context(), backupGateConfig(source))
	if err != nil {
		t.Fatal(err)
	}
	// Public custody is independent of release trust. Fixture was initialized by
	// the real signed dev gate; test requests below explicitly select production.
	prior, next := trustfixture.New(t), trustfixture.New(t)
	for _, kind := range []string{"wrong-configured-key", "unfinished-journal", "root-free-public-check"} {
		t.Run(kind, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.Chmod(directory, 0700); err != nil {
				t.Fatal(err)
			}
			if _, err := upgradegate.InstallLegacyOperator(t.Context(), directory, before.InstanceID, prior.PrimaryPublic, before.RestoreEpoch); err != nil {
				t.Fatal(err)
			}
			public := prior.PrimaryPublic
			if kind == "wrong-configured-key" {
				public = next.PrimaryPublic
			}
			if kind == "unfinished-journal" {
				var after upgrade.State
				err := upgrade.WithLock(t.Context(), backupGateConfig(source), func(session *upgrade.Session) error {
					strongest, err := session.OperatorCredentialEpoch(t.Context(), before)
					if err != nil {
						return err
					}
					after, err = session.PlanOperatorRotation(t.Context(), before, strongest)
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				// Exact valid persisted journal shape. Gate package tests inject failures
				// after actual journal fsync and DB commit; this checks app dispatch only.
				type journalFixture struct {
					Digest    releaseidentity.Digest `json:"digest"`
					Before    upgrade.State          `json:"before"`
					After     upgrade.State          `json:"after"`
					PublicKey []byte                 `json:"public_key"`
				}
				journal := journalFixture{releaseidentity.Hash([]byte("interrupted signed rotation")), before, after, next.PrimaryPublic}
				custody := struct {
					Format     string         `json:"format"`
					InstanceID string         `json:"instance_id"`
					PublicKey  []byte         `json:"public_key"`
					EpochFloor int64          `json:"epoch_floor"`
					Journal    journalFixture `json:"journal"`
				}{"hikyo-operator-custody/v1", before.InstanceID, prior.PrimaryPublic, before.RestoreEpoch, journal}
				raw, err := json.Marshal(custody)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "operator-custody.json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			publicPath := filepath.Join(t.TempDir(), "operator.pub")
			if err := os.WriteFile(publicPath, public, 0600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "never-opened.db")
			cfg := &config.Config{Store: config.Datastore{Engine: config.EngineSQLite, Path: target}, Upgrade: config.UpgradeConfiguration{StateDirectory: directory, OperatorPublicKeyFile: publicPath}}
			if kind == "root-free-public-check" {
				var escaped upgradegate.OperatorCustody
				if err := withBackupOperatorCustody(t.Context(), cfg, func(custody *upgradegate.OperatorCustody) error { escaped = *custody; return custody.Check(before) }); err != nil {
					t.Fatal(err)
				}
				if err := escaped.Check(before); err == nil {
					t.Fatal("public custody survived callback")
				}
				return
			}
			expected := "configured operator public key differs"
			if kind == "unfinished-journal" {
				expected = "operator rotation incomplete"
			}
			if db, err := openBackupRuntime(t.Context(), cfg); err == nil {
				db.Close()
				t.Fatal("production backup acquired runtime admission")
			} else if !strings.Contains(err.Error(), expected) {
				t.Fatalf("wrong refusal: %v", err)
			}
			if err := runRestoreStatus(t.Context(), cfg, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), expected) {
				t.Fatalf("restore status bypassed custody: %v", err)
			}
			for _, path := range []string{target, target + ".lock"} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("custody refusal created datastore or writer lock", err)
				}
			}
		})
	}
}
