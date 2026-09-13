package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/jackc/pgx/v5"
)

func TestUnattendedScratchPreservesUnownedDataAndReusesOwnedSchema(t *testing.T) {
	live := upgradeDrillDatabase(t, store.EnginePostgres)
	scratch := upgradeDrillDatabase(t, store.EnginePostgres)
	reference := upgradeDrillDatabase(t, store.EnginePostgres)
	_, expected, err := upgrade.BuildScratchSchema(t.Context(), upgrade.Config{Engine: releaseidentity.Postgres, DSN: reference.DSN}, store.MigrationsFS, "migrations/postgres")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Store: config.Datastore{Engine: config.EnginePostgres, DSN: live.DSN}, Upgrade: config.UpgradeConfiguration{ScratchPostgresDSN: scratch.DSN}}
	directory := t.TempDir()
	if _, _, err := prepareUnattendedScratch(t.Context(), cfg, directory, expected.Digest()); err != nil {
		t.Fatal(err)
	}
	_, _, err = upgrade.BuildScratchSchema(t.Context(), upgrade.Config{Engine: releaseidentity.Postgres, DSN: scratch.DSN}, store.MigrationsFS, "migrations/postgres")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareUnattendedScratch(t.Context(), cfg, directory, expected.Digest()); err != nil {
		t.Fatalf("owned scratch did not reset: %v", err)
	}
	empty := releaseidentity.MigrationManifest{Engine: releaseidentity.Postgres, Entries: []releaseidentity.Migration{}}
	inspected, err := upgrade.Inspect(t.Context(), upgrade.Config{Engine: releaseidentity.Postgres, DSN: scratch.DSN}, empty)
	if err != nil || inspected.Genesis != releaseidentity.FreshGenesisV1 {
		t.Fatalf("scratch not reusable: %v", err)
	}
	conn, err := pgx.Connect(t.Context(), scratch.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(t.Context(), "CREATE TABLE unrelated_data (value text); INSERT INTO unrelated_data VALUES ('preserve')"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareUnattendedScratch(t.Context(), cfg, directory, expected.Digest()); err == nil {
		t.Fatal("unknown owned-schema drift was deleted")
	}
	if _, _, err := prepareUnattendedScratch(t.Context(), cfg, t.TempDir(), expected.Digest()); err == nil {
		t.Fatal("unowned populated database was adopted")
	}
	var value string
	if err := conn.QueryRow(t.Context(), "SELECT value FROM unrelated_data").Scan(&value); err != nil || value != "preserve" {
		t.Fatalf("unowned data changed: %v", err)
	}
	if _, _, err := prepareUnattendedScratch(t.Context(), cfg, filepath.Join(directory, "missing"), expected.Digest()); err == nil {
		t.Fatal("invalid ownership directory accepted")
	}
}

func TestUnattendedScratchRefusesLiveAliasesAndChangedDSN(t *testing.T) {
	live := upgradeDrillDatabase(t, store.EnginePostgres)
	cfg := &config.Config{Store: config.Datastore{Engine: config.EnginePostgres, DSN: live.DSN}, Upgrade: config.UpgradeConfiguration{ScratchPostgresDSN: live.DSN + "&application_name=scratch-alias"}}
	schema := releaseidentity.Hash([]byte("expected signed schema"))
	if _, _, err := prepareUnattendedScratch(t.Context(), cfg, t.TempDir(), schema); err == nil {
		t.Fatal("same live database accepted through a different DSN")
	}
	scratch := upgradeDrillDatabase(t, store.EnginePostgres)
	cfg.Upgrade.ScratchPostgresDSN = scratch.DSN
	directory := t.TempDir()
	if _, _, err := prepareUnattendedScratch(t.Context(), cfg, directory, schema); err != nil {
		t.Fatal(err)
	}
	cfg.Upgrade.ScratchPostgresDSN += "&application_name=changed-target"
	if _, _, err := prepareUnattendedScratch(t.Context(), cfg, directory, schema); err == nil {
		t.Fatal("changed scratch authority silently adopted")
	}
}

func TestUnattendedCoordinatorDisabledNeedsNoCustody(t *testing.T) {
	cleanup, err := RunUnattendedUpgrade(t.Context(), &config.Config{}, UnattendedUpgradeOptions{Progress: func(string) { t.Fatal("disabled coordinator reported progress") }})
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	_, err = RunUnattendedUpgrade(t.Context(), &config.Config{Dev: true, Upgrade: config.UpgradeConfiguration{Unattended: true}}, UnattendedUpgradeOptions{})
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatal("unsupported coordinator configuration accepted")
	}
}
