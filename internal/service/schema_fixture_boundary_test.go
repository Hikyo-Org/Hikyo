package service

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

func TestPreparedServiceFixtureStillRequiresAuthenticAdmission(t *testing.T) {
	fixture, err := preparedServiceSchema()
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"none", "schema", "release-root", "encryption-root"} {
		t.Run(mutation, func(t *testing.T) {
			cfg := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "copy.db")}
			material := fixture.install(t, cfg.Path)
			if db, err := store.Open(t.Context(), cfg, upgrade.Admission{}); err == nil {
				db.Close()
				t.Fatal("schema-only fixture admitted without boot-gate authority")
			}
			root, err := crypto.GenerateRootKey()
			if err != nil {
				t.Fatal(err)
			}
			defer crypto.Zero(root)
			request := serviceSchemaRequest(cfg.Path, material, root, upgradegate.Boot)
			switch mutation {
			case "schema":
				db, err := sql.Open("sqlite", cfg.Path)
				if err != nil {
					t.Fatal(err)
				}
				_, err = db.ExecContext(t.Context(), `CREATE TABLE unexpected_schema (id TEXT)`)
				closeErr := db.Close()
				if err != nil || closeErr != nil {
					t.Fatalf("mutate schema: %v; close: %v", err, closeErr)
				}
			case "release-root":
				request.Pinned.Root[0] ^= 1
			case "encryption-root":
				if _, err := upgradegate.RunDevelopment(t.Context(), request); err != nil {
					t.Fatal(err)
				}
				request.RootKey, err = crypto.GenerateRootKey()
				if err != nil {
					t.Fatal(err)
				}
				defer crypto.Zero(request.RootKey)
			}
			result, err := upgradegate.RunDevelopment(t.Context(), request)
			if mutation != "none" {
				if err == nil || result.Admission.Valid() {
					t.Fatal("mutated fixture admitted runtime access")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			wrong := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "other.db")}
			fixture.install(t, wrong.Path)
			if db, err := store.Open(t.Context(), wrong, result.Admission); err == nil {
				db.Close()
				t.Fatal("admission accepted another copy's database identity")
			}
		})
	}
}

func TestPreparedServiceFixtureWritesStayInTheirCopy(t *testing.T) {
	var copies [2]*store.DB
	for i := range copies {
		cfg := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "copy.db")}
		db, err := openPreparedServiceFixture(t, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		copies[i] = db
	}
	if _, err := copies[0].SQLiteWrite().ExecContext(t.Context(), `INSERT INTO principals (id,kind,created_at) VALUES ('fixture_private','human','2026-10-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := copies[1].SQLiteRead().QueryRowContext(t.Context(), `SELECT count(*) FROM principals WHERE id='fixture_private'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("another copy observed fixture writes: count=%d, err=%v", count, err)
	}
}
