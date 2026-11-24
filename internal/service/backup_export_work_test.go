package service

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

func writeTree(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, rel := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestSweepLegacyWorkDirectories(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir,
		".export-1/snapshot.db", ".export-1/snapshot.db-journal",
		".export-2/copy-orgs", ".export-2/copy-keys",
		".export-3/copy-orgs", ".export-3/sub/file", // subdirectory disqualifies
		".export-4/copy-orgs", // symlink below disqualifies
		".hikyo-export-5.partial",
	)
	if err := os.Symlink("/etc/hosts", filepath.Join(dir, ".export-4", "copy-link")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if err := sweepExportDebris(slog.Default(), dir); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{".export-1", ".export-2", ".hikyo-export-5.partial"} {
		if exists(filepath.Join(dir, gone)) {
			t.Errorf("%s survived", gone)
		}
	}
	for _, kept := range []string{".export-3", ".export-4"} {
		if !exists(filepath.Join(dir, kept)) {
			t.Errorf("%s with a disqualifying entry was swept", kept)
		}
	}
}

func TestStagedWorkIsLiveToTheSweepEvenViaARelativeDirectory(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	if err := os.Mkdir("backups", 0o700); err != nil {
		t.Fatal(err)
	}
	svc := &Backup{}
	work, release, err := stageExportWork(t.Context(), slog.Default(), "./backups")
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, work, "snapshot.db")
	// Same process, so a second flock could succeed on some filesystems; the
	// live set must skip the directory regardless of how dir is spelled.
	for _, spelling := range []string{"backups", "./backups", filepath.Join(base, "backups")} {
		if err := svc.sweepDirectory(t.Context(), spelling); err != nil {
			t.Fatal(err)
		}
		if !exists(filepath.Join(work, "snapshot.db")) {
			t.Fatalf("live export staging swept via %q", spelling)
		}
	}
	release()
	if exists(work) {
		t.Fatal("release left the work directory")
	}
}

func TestPruneSweepsDeadExportDebris(t *testing.T) {
	db, err := openServiceFixture(t, preparationDatabase(t, store.EngineSQLite))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	dir := t.TempDir()
	writeTree(t, dir, ".hikyo-export-work-9/snapshot.db", ".hikyo-export-work-9/.live", "keep.txt")
	svc := &Backup{DB: db}
	// The prune outcome is irrelevant here; the sweep runs under its lock first.
	_, _ = svc.Prune(t.Context(), dir, PrunePolicy{RetainCount: 7, RetainDays: 180})
	if exists(filepath.Join(dir, ".hikyo-export-work-9")) {
		t.Fatal("prune left dead export debris")
	}
	if !exists(filepath.Join(dir, "keep.txt")) {
		t.Fatal("prune swept a foreign file")
	}
}

// A child that cannot be removed must leave the marker and directory behind,
// so a later sweep (free lock, marker present) retries instead of orphaning
// plaintext the sweep no longer recognises.
func TestUnremovableStagedPlaintextKeepsItsMarker(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX directory permissions and a non-root user")
	}
	dir := t.TempDir()
	work, release, err := stageExportWork(t.Context(), slog.Default(), dir)
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, work, "sub/snapshot.db")
	sub := filepath.Join(work, "sub")
	if err := os.Chmod(sub, 0o500); err != nil { // files inside cannot be unlinked
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o700) })

	release()
	if !exists(filepath.Join(work, ".live")) || !exists(filepath.Join(sub, "snapshot.db")) {
		t.Fatal("failed plaintext removal dropped the liveness marker")
	}
	if err := sweepExportDebris(slog.Default(), dir); err == nil {
		t.Fatal("sweep reported success while plaintext could not be removed")
	}
	if !exists(filepath.Join(work, ".live")) {
		t.Fatal("failed sweep dropped the liveness marker")
	}

	if err := os.Chmod(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := sweepExportDebris(slog.Default(), dir); err != nil {
		t.Fatal(err)
	}
	if exists(work) {
		t.Fatal("a later sweep did not retry the removal")
	}
}
