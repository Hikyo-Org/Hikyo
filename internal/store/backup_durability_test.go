package store

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
)

func TestRestoreSQLiteDirectoryDurabilityPreservesCommittedMutations(t *testing.T) {
	archive := publicationMutationArchive(t)
	for _, test := range []struct {
		name   string
		failAt int
	}{
		{"success", -1},
		{"destination", 0},
		{"parent", 1},
		{"ancestor", 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "new", "restored")
			if err := os.MkdirAll(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(directory, "instance.db")
			injected := errors.New("injected restore directory sync failure")
			operations := defaultSQLiteRestoreOperations()
			var order []string
			fileSync := operations.fsyncFile
			operations.fsyncFile = func(path string) error {
				order = append(order, "file-sync")
				return fileSync(path)
			}
			link := operations.link
			operations.link = func(from, to string) error {
				order = append(order, "publish")
				return link(from, to)
			}
			syncDirectory := operations.syncDirectory
			var synced []string
			operations.syncDirectory = func(path string) error {
				if !slices.Equal(order, []string{"file-sync", "publish"}) {
					t.Fatalf("directory sync preceded file sync/publication: %v", order)
				}
				if _, err := os.Stat(target); err != nil {
					t.Fatal(err)
				}
				synced = append(synced, path)
				if len(synced)-1 == test.failAt {
					return injected
				}
				return syncDirectory(path)
			}
			mutate := func(ctx context.Context, db *DB) error {
				tx, err := db.sqWrite.BeginTx(ctx, nil)
				if err != nil {
					return err
				}
				defer tx.Rollback()
				if _, err := tx.ExecContext(ctx, "UPDATE restore_publication_probe SET value = 'recovered' WHERE id = 1"); err != nil {
					return err
				}
				return tx.Commit()
			}
			manifest, err := restoreSQLite(t.Context(), bytes.NewReader(archive), target, mutate, operations)
			if test.failAt >= 0 {
				if !errors.Is(err, ErrRestoreDurabilityUnconfirmed) || !errors.Is(err, injected) || !strings.Contains(err.Error(), target) {
					t.Fatalf("error = %v, want named retained target and durability failure", err)
				}
				if !reflect.DeepEqual(manifest, Manifest{}) {
					t.Fatalf("failed restore returned a manifest: %+v", manifest)
				}
				if len(synced) != test.failAt+1 {
					t.Fatalf("sync did not stop at failure: %v", synced)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if manifest.Engine != EngineSQLite {
					t.Fatalf("successful manifest = %+v", manifest)
				}
				resolved, err := filepath.EvalSymlinks(directory)
				if err != nil {
					t.Fatal(err)
				}
				if len(synced) < 3 || synced[0] != resolved || synced[1] != filepath.Dir(resolved) || filepath.Dir(synced[len(synced)-1]) != synced[len(synced)-1] {
					t.Fatalf("directory sync ancestry = %v", synced)
				}
			}
			assertNoRestoreStaging(t, target)
			// Storage must preserve the callback's committed mutations even
			// when publication durability fails. The isolation drill suite owns
			// the real credential-epoch and reconciliation contract.
			db, err := Open(t.Context(), Config{Engine: EngineSQLite, Path: target})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var value string
			if err := db.sqRead.QueryRowContext(t.Context(), "SELECT value FROM restore_publication_probe WHERE id = 1").Scan(&value); err != nil {
				t.Fatal(err)
			}
			if value != "recovered" {
				t.Fatalf("retained target lost the committed mutation: %q", value)
			}
			if _, err := RestoreSQLite(t.Context(), bytes.NewReader(archive), target, nil); !errors.Is(err, ErrTargetNotEmpty) {
				t.Fatalf("retry must not overwrite retained target: %v", err)
			}
		})
	}
}

// Use the real embedded schema and VACUUM export, rather than a synthetic file,
// so the failure path proves committed mutations survive publication intact.
func publicationMutationArchive(t *testing.T) []byte {
	t.Helper()
	db, err := Open(t.Context(), Config{Engine: EngineSQLite, Path: filepath.Join(t.TempDir(), "source.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrations, err := fs.Sub(MigrationsFS, "migrations/sqlite")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(database.DialectSQLite3, db.sqWrite, migrations)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sqWrite.ExecContext(t.Context(), "CREATE TABLE restore_publication_probe(id INTEGER PRIMARY KEY, value TEXT NOT NULL); INSERT INTO restore_publication_probe VALUES (1, 'original')"); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if _, err := Export(t.Context(), db, &archive, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}
