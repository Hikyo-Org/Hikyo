package upgrade

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func restoreSchemaAttempt(t *testing.T, cfg Config, manifest releaseidentity.MigrationManifest, schema releaseidentity.Digest, commit bool) error {
	t.Helper()
	if cfg.Engine == releaseidentity.SQLite {
		db, err := open(cfg, false)
		if err != nil {
			return err
		}
		defer db.Close()
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := PrepareSQLiteRestoreControlSchema(t.Context(), tx, manifest, schema); err != nil {
			return err
		}
		if commit {
			return tx.Commit()
		}
		return nil
	}
	conn, err := pgx.Connect(t.Context(), cfg.DSN)
	if err != nil {
		return err
	}
	defer conn.Close(t.Context())
	tx, err := conn.Begin(t.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(t.Context())
	if err := PreparePostgresRestoreControlSchema(t.Context(), tx, manifest, schema); err != nil {
		return err
	}
	if commit {
		return tx.Commit(t.Context())
	}
	return nil
}

func TestRestoreControlSchemaIsExactEmptyAndTransactional(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		if err := migrateFixture(t, cfg); err != nil {
			t.Fatal(err)
		}
		declaration, err := legacyDeclaration(cfg.Engine)
		if err != nil {
			t.Fatal(err)
		}
		manifest, schema := declaration.Migrations, declaration.Catalog.Digest()
		if err := restoreSchemaAttempt(t, cfg, manifest, releaseidentity.Hash([]byte("wrong schema")), true); err == nil {
			t.Fatal("wrong schema installed control")
		}
		if err := restoreSchemaAttempt(t, cfg, manifest, schema, false); err != nil {
			t.Fatal(err)
		}
		err = WithLock(t.Context(), cfg, func(s *Session) error {
			catalog, err := inspectCatalog(t.Context(), s.conn, cfg.Engine)
			if err == nil && controlPresent(catalog) {
				t.Fatal("rolled-back restore left control schema")
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := restoreSchemaAttempt(t, cfg, manifest, schema, true); err != nil {
			t.Fatal(err)
		}
		if err := restoreSchemaAttempt(t, cfg, manifest, schema, true); err != nil {
			t.Fatalf("exact empty schema not reusable: %v", err)
		}
		query(t, cfg, `INSERT INTO upgrade_nonces(trust_domain,instance_id,incarnation,restore_epoch,nonce,generation,evidence_digest) VALUES('production','existing','existing',0,'existing',1,'existing')`)
		if err := restoreSchemaAttempt(t, cfg, manifest, schema, true); err == nil {
			t.Fatal("nonempty authority table accepted")
		}
	})
}

func TestSQLiteCatalogExcludesOnlyReservedSQLitePrefix(t *testing.T) {
	cfg := testConfig(t, releaseidentity.SQLite)
	if err := migrateFixture(t, cfg); err != nil {
		t.Fatal(err)
	}
	query(t, cfg, `CREATE TRIGGER sqliteXcredential_epoch AFTER UPDATE ON auth_instance_state BEGIN SELECT 1; END`)

	err := WithLock(t.Context(), cfg, func(s *Session) error {
		catalog, err := inspectCatalog(t.Context(), s.conn, releaseidentity.SQLite)
		if err != nil {
			return err
		}
		for _, object := range catalog.Objects {
			if strings.Contains(object, `"sqliteXcredential_epoch"`) {
				return nil
			}
		}
		t.Fatal("catalog omitted a non-reserved sqliteX trigger")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
