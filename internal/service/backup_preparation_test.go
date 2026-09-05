package service

import (
	"bytes"
	"context"
	"fmt"
	gatefixture "github.com/Hikyo-Org/hikyo/internal/upgradegate/testfixture"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	bundlefixture "github.com/Hikyo-Org/hikyo/internal/upgradebundle/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/jackc/pgx/v5"
)

func preparationDatabase(t *testing.T, engine store.Engine) store.Config {
	t.Helper()
	cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "preparation.db")}
	if engine == store.EngineSQLite {
		return cfg
	}
	raw := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
	if raw == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires PostgreSQL preparation proof")
		}
		t.Skip("HIKYO_TEST_POSTGRES_DSN not set")
	}
	admin, err := pgx.Connect(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("hikyo_preparation_%d", time.Now().UnixNano())
	ident := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+ident); err != nil {
		admin.Close(t.Context())
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP DATABASE "+ident+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
		admin.Close(ctx)
	})
	dsn, err := url.Parse(raw)
	if err != nil || (dsn.Scheme != "postgres" && dsn.Scheme != "postgresql") {
		t.Fatal("preparation fixture requires PostgreSQL URL")
	}
	dsn.Path = "/" + name
	cfg.DSN = dsn.String()
	cfg.Path = ""
	owned, err := pgx.Connect(t.Context(), cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	var actual string
	err = owned.QueryRow(t.Context(), "SELECT current_database()").Scan(&actual)
	owned.Close(t.Context())
	if err != nil || actual != name {
		t.Fatal("preparation fixture is not allocated database")
	}
	return cfg
}

func TestPreparationExportUsesLiveSessionWithoutRuntimeAdmission(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := preparationDatabase(t, engine)
			if err := migrate.Run(t.Context(), cfg); err != nil {
				t.Fatal(err)
			}
			manifest, err := upgrade.PinnedLegacyManifest(releaseidentity.Engine(engine))
			if err != nil {
				t.Fatal(err)
			}
			sourceConfig := upgrade.Config{Engine: releaseidentity.Engine(engine), Path: cfg.Path, DSN: cfg.DSN}
			installed, err := upgrade.InspectInstalled(t.Context(), sourceConfig, manifest)
			if err != nil {
				t.Fatal(err)
			}
			bundle := bundlefixture.Write(t, upgradecompat.InstalledSource{Identity: installed.Source, Migrations: manifest, SchemaSHA256: installed.SchemaDigest}, []bundlefixture.Target{{Version: "1.0.0", Sequence: 1, Commit: strings.Repeat("a", 40), Migrations: manifest, SchemaSHA256: installed.SchemaDigest}})
			proposal, err := backupreceipt.NewLegacyProposal()
			if err != nil {
				t.Fatal(err)
			}
			identity, recipient, err := backup.GenerateIdentity()
			if err != nil {
				t.Fatal(err)
			}
			options := backup.Options{Recipients: []string{recipient}}
			var retained *store.PreparationDB
			var exported ExportResult
			err = upgrade.WithLock(t.Context(), sourceConfig, func(session *upgrade.Session) error {
				authority, err := session.PrepareExport(t.Context(), bundle.Plan)
				if err != nil {
					return err
				}
				retained, err = store.OpenPreparation(t.Context(), cfg, authority)
				if err != nil {
					return err
				}
				exported, err = ExportPreparedUpgrade(t.Context(), retained, options, t.TempDir(), bundle.Plan, &proposal)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			defer retained.Close()
			raw, err := os.ReadFile(exported.ReceiptPath)
			if err != nil {
				t.Fatal(err)
			}
			pinned, err := backupreceipt.PinCiphertext(t.Context(), exported.Path, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer pinned.Close()
			authenticated, err := backupreceipt.AuthenticateArchive(t.Context(), pinned, raw, bundle.Plan, backup.Unlock{Identity: identity}, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer authenticated.Close()
			if authenticated.Snapshot().InstanceID != installed.InstanceID || authenticated.Snapshot().Authority != backupreceipt.LegacyProposalAuthority {
				t.Fatal("actual source authority changed")
			}
			if _, err := retained.InspectUpgradeSource(t.Context(), manifest); err == nil {
				t.Fatal("expired owner retained source inspection")
			}
			output := t.TempDir()
			if _, err := ExportPreparedUpgrade(t.Context(), retained, options, output, bundle.Plan, &proposal); err == nil {
				t.Fatal("expired owner retained export authority")
			}
			entries, err := os.ReadDir(output)
			if err != nil || len(entries) != 0 {
				t.Fatal("refused export published debris", err)
			}
		})
	}
}

func TestPreparedPublisherPreservesOrdinaryPassphraseBackup(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := preparationDatabase(t, engine)
			authority := gatefixture.Prepare(t, upgrade.Config{Engine: releaseidentity.Engine(engine), Path: cfg.Path, DSN: cfg.DSN}, store.MigrationsFS, "migrations/"+string(engine), bytes.Repeat([]byte{45}, 32))
			db, err := store.Open(t.Context(), cfg, authority)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			exported, err := (&Backup{DB: db, Options: backup.Options{Passphrase: "synthetic local backup passphrase"}}).Export(t.Context(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			sealed, err := os.Open(exported.Path)
			if err != nil {
				t.Fatal(err)
			}
			defer sealed.Close()
			var plain bytes.Buffer
			if err := backup.ExtractTo(&plain, sealed, backup.Unlock{Passphrase: "synthetic local backup passphrase"}); err != nil {
				t.Fatal(err)
			}
			manifest, err := store.ReadManifest(&plain)
			if err != nil {
				t.Fatal(err)
			}
			if manifest.Format != store.ArchiveFormat || manifest.Upgrade != nil || exported.Receipt != nil || exported.ReceiptPath != "" {
				t.Fatal("ordinary passphrase archive changed protocol")
			}
		})
	}
}
