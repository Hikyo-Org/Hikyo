package store

import (
	"context"
	"errors"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeSQLiteChildExclusion(t *testing.T) {
	path := os.Getenv("HIKYO_ADMISSION_CHILD_PATH")
	if path == "" {
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	err := upgrade.WithLock(ctx, upgrade.Config{Engine: releaseidentity.SQLite, Path: path}, func(*upgrade.Session) error { return nil })
	if os.Getenv("HIKYO_ADMISSION_CHILD_BLOCKED") == "1" {
		if err == nil {
			t.Fatal("migration process passed live WAL reader")
		}
	} else if err != nil {
		t.Fatal(err)
	}
}
func TestSQLiteRuntimeReaderBlocksOtherProcessAndRejectsAliases(t *testing.T) {
	cfg := Config{Engine: EngineSQLite, Path: filepath.Join(t.TempDir(), "instance.db")}
	db, err := admittedStoreFixture(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	transaction, err := db.BeginSQLite(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := func(blocked bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, executable, "-test.run=^TestRuntimeSQLiteChildExclusion$")
		value := "0"
		if blocked {
			value = "1"
		}
		command.Env = append(os.Environ(), "HIKYO_ADMISSION_CHILD_PATH="+cfg.Path, "HIKYO_ADMISSION_CHILD_BLOCKED="+value)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("child exclusion proof: %v: %s", err, output)
		}
	}
	child(true)
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	child(false)
	alias := filepath.Join(filepath.Dir(cfg.Path), "alias.db")
	if err := os.Link(cfg.Path, alias); err != nil {
		t.Fatal(err)
	}
	if tx, err := db.BeginSQLite(t.Context(), true); err == nil {
		_ = tx.Rollback()
		t.Fatal("runtime admitted multiply linked SQLite file")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	original := cfg.Path + ".old"
	if err := os.Rename(cfg.Path, original); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Path, []byte("replacement inode"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := db.CheckAdmission(t.Context()); !errors.Is(err, upgrade.ErrConflict) {
		t.Fatalf("replaced identity did not refuse before SQL: %v", err)
	}
}
func TestRuntimeTransactionPanicReleasesHostExclusion(t *testing.T) {
	cfg := Config{Engine: EngineSQLite, Path: filepath.Join(t.TempDir(), "panic.db")}
	db, err := admittedStoreFixture(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, write := range []bool{false, true} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("fixture did not panic")
				}
			}()
			if write {
				_ = dbTransaction(t.Context(), db, func(adapterDBTX) error { panic("owned test panic") })
			} else {
				_ = dbRead(t.Context(), db, func(adapterDB) error { panic("owned test panic") })
			}
		}()
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		err := upgrade.WithLock(ctx, upgradeConfig(cfg), func(*upgrade.Session) error { return nil })
		cancel()
		if err != nil {
			t.Fatalf("panic retained migration exclusion: %v", err)
		}
	}
}
