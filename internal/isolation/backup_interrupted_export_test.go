package isolation

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/app"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
)

// #1126: a process killed in the middle of an export leaves a staged
// `.hikyo-export-*.partial` file and an `.export-*` work directory. The next
// export, a fresh run of the real verb, must succeed beside that debris and
// the prefix must refuse to restore. The debris itself is not swept (#1412).
// Run for both backup identity classes on both engines.
func TestExportAfterInterruptedExportSQLite(t *testing.T) {
	for name, generate := range map[string]func() (string, string, error){"x25519": backup.GenerateIdentity, "hybrid": backup.GenerateHybridIdentity} {
		t.Run(name, func(t *testing.T) {
			c := newCustodyWith(t, generate)
			runExportAfterInterruptedExport(t, sqliteTarget(t, t.TempDir(), c.recipient(t)), c)
		})
	}
}

func TestExportAfterInterruptedExportPostgres(t *testing.T) {
	for name, generate := range map[string]func() (string, string, error){"x25519": backup.GenerateIdentity, "hybrid": backup.GenerateHybridIdentity} {
		t.Run(name, func(t *testing.T) {
			c := newCustodyWith(t, generate)
			runExportAfterInterruptedExport(t, postgresTarget(t, t.TempDir(), c.recipient(t)), c)
		})
	}
}

func runExportAfterInterruptedExport(t *testing.T, target drillTarget, c custody) {
	db, _ := buildInstance(t, target, c)
	orgs := queryStrings(t, db, "SELECT name FROM orgs ORDER BY id")

	// The state a kill leaves: a complete chunk-aligned prefix of a real
	// archive under the staging name, and a work directory.
	complete := exportArchive(t, target)
	raw, err := os.ReadFile(complete)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(complete); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(target.cfg.BackupDir, ".hikyo-export-killed.partial")
	if err := os.WriteFile(partial, raw[:ageStreamBoundary(t, raw)], 0o600); err != nil {
		t.Fatal(err)
	}
	// The work directory of a killed export holds an unencrypted datastore
	// copy (snapshot.db or COPY output). Nothing sweeps it today (#1412); the
	// stand-in keeps this drill faithful to that state.
	workDir := filepath.Join(target.cfg.BackupDir, ".export-killed")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "snapshot.db"), []byte("stand-in for an unencrypted snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}

	// "Restart": a fresh run of the operator verb.
	archive := exportArchive(t, target)
	if !strings.HasSuffix(archive, ".age") {
		t.Fatalf("export after an interruption published %q", archive)
	}

	t.Run("the_interrupted_prefix_refuses_to_restore", func(t *testing.T) {
		target.destroy(t)
		err := app.RunRestore(t.Context(), target.cfg, drillLogger(),
			[]string{"run", "--from", partial, "--identity-file", c.identityFile()}, io.Discard, nil, nil)
		if !errors.Is(err, backup.ErrTruncated) {
			t.Fatalf("restore of an interrupted export = %v, want %v", err, backup.ErrTruncated)
		}
		assertTargetUntouched(t, target)
	})

	t.Run("the_export_after_the_interruption_restores", func(t *testing.T) {
		target.destroy(t)
		if err := app.RunRestore(t.Context(), target.cfg, drillLogger(),
			[]string{"run", "--from", archive, "--identity-file", c.identityFile()}, io.Discard, nil, nil); err != nil {
			t.Fatalf("restore: %v", err)
		}
		recoverRestoredTarget(t, target, c)
		restored := target.open(t)
		if got := queryStrings(t, restored, "SELECT name FROM orgs ORDER BY id"); got != orgs {
			t.Fatalf("restored organisations = %q, want %q", got, orgs)
		}
	})
}

// #1126 rollback: after a hybrid policy is set back to X25519 recipients the
// archives already written to the hybrid recipient stay restorable with the
// retained hybrid identity and are refused by the X25519 one; new exports use
// the classical recipient. Nothing is rewrapped, so each identity is kept for
// as long as an archive written to it is retained.
func TestRollbackToX25519KeepsHybridArchivesRestorableSQLite(t *testing.T) {
	hybrid := newCustodyWith(t, backup.GenerateHybridIdentity)
	runRollbackToX25519(t, sqliteTarget(t, t.TempDir(), hybrid.recipient(t)), hybrid)
}

func TestRollbackToX25519KeepsHybridArchivesRestorablePostgres(t *testing.T) {
	hybrid := newCustodyWith(t, backup.GenerateHybridIdentity)
	runRollbackToX25519(t, postgresTarget(t, t.TempDir(), hybrid.recipient(t)), hybrid)
}

func runRollbackToX25519(t *testing.T, target drillTarget, hybrid custody) {
	db, _ := buildInstance(t, target, hybrid)
	orgs := queryStrings(t, db, "SELECT name FROM orgs ORDER BY id")
	classical := newCustody(t)

	hybridArchive := exportArchive(t, target)
	target.cfg.BackupRecipients = []string{classical.recipient(t)} // the rollback is configuration only
	classicalArchive := exportArchive(t, target)

	restore := func(t *testing.T, archive, identity string) error {
		t.Helper()
		target.destroy(t)
		return app.RunRestore(t.Context(), target.cfg, drillLogger(),
			[]string{"run", "--from", archive, "--identity-file", identity}, io.Discard, nil, nil)
	}
	t.Run("each_archive_is_refused_by_the_other_identity", func(t *testing.T) {
		// boundary confines filippo.io/age to the crypto packages, so the
		// refusal is pinned by age's own message text.
		const noMatch = "identity did not match any of the recipients"
		if err := restore(t, hybridArchive, classical.identityFile()); err == nil || !strings.Contains(err.Error(), noMatch) {
			t.Fatalf("the rolled-back X25519 identity on a hybrid archive = %v, want age's no-identity-match refusal", err)
		}
		assertTargetUntouched(t, target)
		if err := restore(t, classicalArchive, hybrid.identityFile()); err == nil || !strings.Contains(err.Error(), noMatch) {
			t.Fatalf("the retained hybrid identity on an X25519 archive = %v, want age's no-identity-match refusal", err)
		}
		assertTargetUntouched(t, target)
	})
	for name, tc := range map[string]struct {
		archive string
		custody custody
	}{
		"hybrid_archive_with_the_retained_hybrid_identity": {hybridArchive, hybrid},
		"classical_archive_after_the_rollback":             {classicalArchive, classical},
	} {
		t.Run(name, func(t *testing.T) {
			if err := restore(t, tc.archive, tc.custody.identityFile()); err != nil {
				t.Fatalf("restore: %v", err)
			}
			recoverRestoredTarget(t, target, hybrid)
			if got := queryStrings(t, target.open(t), "SELECT name FROM orgs ORDER BY id"); got != orgs {
				t.Fatalf("restored organisations = %q, want %q", got, orgs)
			}
		})
	}
}
