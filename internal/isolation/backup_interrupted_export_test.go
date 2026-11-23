package isolation

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofrs/flock"

	"github.com/Hikyo-Org/hikyo/internal/app"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
)

// #1126: a process killed in the middle of an export leaves a staged
// `.hikyo-export-*.partial` file and a work directory. The next export, a
// fresh run of the real verb, must succeed beside that debris and the prefix
// must refuse to restore. The same export sweeps dead work directories and
// spares a live one and look-alikes (#1412).
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
	prefix := raw[:ageStreamBoundary(t, raw)]
	dir := target.cfg.BackupDir
	mk := func(rel, content string) string {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	// Debris a killed export leaves (#1412). The work directory holds an
	// unencrypted datastore copy plus the staged prefix; the kernel dropped
	// its liveness lock with the process, leaving a stale marker file.
	mk(".hikyo-export-work-111/snapshot.db", "stand-in for an unencrypted snapshot")
	mk(".hikyo-export-work-111/.live", "")
	mk(".hikyo-export-work-111/.hikyo-export-1.partial", string(prefix))
	// Pre-#1412 shapes: work directory without a marker, top-level partial.
	mk(".export-222/snapshot.db", "stand-in for an unencrypted snapshot")
	mk(".hikyo-export-333.partial", string(prefix))
	dead := []string{".hikyo-export-work-111", ".export-222", ".hikyo-export-333.partial"}
	// Not Hikyo's: the operator may point --out at a folder with their own
	// files. None of these may be touched, and a stray partial must not block
	// the export.
	survivors := []string{
		mk(".export-notours/photo.jpg", "user data"),
		mk(".export-444/photo.jpg", "user data under the legacy name shape"),
		mk(".hikyo-export-work-555/photo.jpg", "prefix-matching directory without a marker"),
		mk(".hikyo-export-killed.partial", string(prefix)),
	}
	// A concurrent live export: marker lock held.
	liveFile := mk(".hikyo-export-work-666/snapshot.db", "live export staging")
	live := flock.New(filepath.Join(dir, ".hikyo-export-work-666", ".live"))
	if ok, err := live.TryLock(); err != nil || !ok {
		t.Fatalf("hold live export lock: %v %v", ok, err)
	}
	defer live.Unlock()
	partial := filepath.Join(t.TempDir(), "prefix.partial")
	if err := os.WriteFile(partial, prefix, 0o600); err != nil {
		t.Fatal(err)
	}

	// "Restart": a fresh run of the operator verb.
	archive := exportArchive(t, target)
	if !strings.HasSuffix(archive, ".age") {
		t.Fatalf("export after an interruption published %q", archive)
	}
	for _, name := range dead {
		if _, err := os.Lstat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("debris %s of the killed export survived the next export: %v", name, err)
		}
	}
	for _, path := range append(survivors, liveFile) {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s was swept: %v", path, err)
		}
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
