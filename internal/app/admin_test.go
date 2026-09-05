package app

import (
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestAdminAuthRefusesUnverifiedLegacySchema(t *testing.T) {
	cfg := devConfig(t)
	fixture := &storeFixture{sc: storeConfig(cfg), dir: filepath.Dir(cfg.Store.Path)}
	legacySchema(t, fixture)
	cfg.BackupDir = filepath.Join(fixture.dir, "backups")
	_, closeDB, err := adminAuth(t.Context(), cfg, testLogger())
	if err == nil {
		closeDB()
		t.Fatal("admin authentication admitted a legacy database without signed upgrade proof")
	}
	if n := archiveCount(t, cfg.BackupDir); n != 0 {
		t.Fatalf("refused admin published %d archives", n)
	}
}

func TestAdminAuthWarnsWhenRootRotationPending(t *testing.T) {
	cfg := devConfig(t)
	auth, closeDB, err := adminAuth(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}

	newRoot, err := crypto.GenerateRootKey()
	if err != nil {
		closeDB()
		t.Fatal(err)
	}
	defer crypto.Zero(newRoot)
	wrapper, err := auth.Keyring.PrepareRootKeyRotation(t.Context(), newRoot)
	if err != nil {
		closeDB()
		t.Fatal(err)
	}
	wrapper.CreatedAt = store.CanonTime(time.Now())
	if _, err := auth.DB.SQLiteWrite().ExecContext(t.Context(), `
		INSERT INTO master_keys (version, root_key_epoch, state, blob, created_at)
		VALUES (?, ?, 'active', ?, ?)`, wrapper.Version, wrapper.RootKeyEpoch,
		wrapper.Blob, wrapper.CreatedAt.Format(time.RFC3339Nano)); err != nil {
		closeDB()
		t.Fatal(err)
	}
	closeDB()

	var logged strings.Builder
	log := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn}))
	_, closeDB, err = adminAuth(t.Context(), cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	closeDB()

	if !strings.Contains(logged.String(), "root key rotation is UNFINISHED") {
		t.Fatalf("admin auth did not warn about pending root rotation; got %q", logged.String())
	}
}

func TestAdminAuthResourceOwnership(t *testing.T) {
	t.Run("invalid candidate configuration opens no runtime database", func(t *testing.T) {
		record := &bootResourceRecord{}
		cfg := devConfig(t)
		cfg.Argon2MemoryKiB = 1

		_, _, err := adminAuthWithResources(t.Context(), cfg, testLogger(), recordingBootResources(record))
		if err == nil {
			t.Fatal("admin auth with invalid password parameters succeeded")
		}
		if record.databaseCloses != 0 || record.database != nil {
			t.Fatalf("invalid candidate acquired a runtime database; closes=%d", record.databaseCloses)
		}
	})

	t.Run("success transfers ownership to returned cleanup", func(t *testing.T) {
		record := &bootResourceRecord{}
		auth, closeDB, err := adminAuthWithResources(t.Context(), devConfig(t), testLogger(), recordingBootResources(record))
		if err != nil {
			t.Fatal(err)
		}
		if record.databaseCloses != 0 {
			t.Fatalf("database closes before cleanup = %d, want 0", record.databaseCloses)
		}
		if err := auth.DB.Ping(t.Context()); err != nil {
			t.Fatalf("database closed before cleanup: %v", err)
		}

		closeDB()
		if record.databaseCloses != 1 {
			t.Fatalf("database closes after cleanup = %d, want 1", record.databaseCloses)
		}
		if err := auth.DB.Ping(t.Context()); err == nil {
			t.Fatal("database remained open after cleanup")
		}
	})
}
