package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
)

// scheduleFixture is a fully migrated sqlite instance with a real export
// policy, so the scheduled jobs have a datastore and a destination.
func scheduleFixture(t *testing.T) (*config.Config, *service.Backup, string) {
	t.Helper()
	dir := t.TempDir()
	sc := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(dir, "hikyo.db")}
	if err := migrate.Run(t.Context(), sc); err != nil {
		t.Fatal(err)
	}
	_, recipient, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(dir, "backups")
	cfg := &config.Config{
		Store:             config.Datastore{Engine: config.EngineSQLite, Path: sc.Path},
		BackupDir:         backupDir,
		BackupRecipients:  []string{recipient},
		BackupInterval:    config.DefaultBackupInterval,
		BackupRPO:         config.DefaultBackupRPO,
		BackupRetainCount: config.DefaultBackupRetainCount,
		BackupRetainDays:  config.DefaultBackupRetainDays,
		BackupRTOTarget:   config.DefaultBackupRTOTarget,
	}
	db, err := store.Open(t.Context(), sc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := &service.Backup{DB: db, Options: backup.Options{Recipients: []string{recipient}}}
	return cfg, svc, backupDir
}

func TestBackupExportJobRunsOnceThenGatesOnTheInterval(t *testing.T) {
	cfg, svc, backupDir := scheduleFixture(t)
	jobs := backupJobs(cfg, quietLogger(), svc)
	if len(jobs) != 2 || jobs[0].Name != "backup_export" || jobs[1].Name != "backup_prune" {
		t.Fatalf("jobs = %+v", jobs)
	}
	export := jobs[0]

	// Startup catch-up: a never-exported instance is due immediately.
	if err := export.Run(t.Context()); err != nil {
		t.Fatalf("first export: %v", err)
	}
	if n := archiveCount(t, backupDir); n != 1 {
		t.Fatalf("first run published %d archives, want 1", n)
	}
	if n := countInstanceEvents(t, storeConfig(cfg), "backup.exported"); n != 1 {
		t.Fatalf("backup.exported events = %d, want 1", n)
	}

	// A second tick inside the interval must NOT export again: the job gates
	// on the persisted last success, not on the tick.
	if err := export.Run(t.Context()); err != nil {
		t.Fatalf("second export: %v", err)
	}
	if n := archiveCount(t, backupDir); n != 1 {
		t.Fatalf("a within-interval tick published another archive (%d total)", n)
	}

	at, ok, err := export.LastSuccess(t.Context())
	if err != nil || !ok || at.IsZero() {
		t.Fatalf("last-success probe = (%v, %v, %v)", at, ok, err)
	}
}

func TestBackupExportJobRecordsFailureLoudlyWithoutPlaintext(t *testing.T) {
	cfg, svc, _ := scheduleFixture(t)
	// A destination that cannot be created: its parent is a regular file.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.BackupDir = filepath.Join(blocker, "backups")
	job := backupJobs(cfg, quietLogger(), svc)[0]

	if err := job.Run(t.Context()); err == nil {
		t.Fatal("a failing export returned no error")
	}
	if n := countInstanceEvents(t, storeConfig(cfg), "backup.export_failed"); n != 1 {
		t.Fatalf("backup.export_failed events = %d, want 1", n)
	}
	// The destination was never even created, so no archive of any kind, least
	// of all a plaintext one, was written.
	if _, err := os.Stat(cfg.BackupDir); !os.IsNotExist(err) && err == nil {
		t.Fatalf("a failed export created the destination: %v", err)
	}
	st, err := svc.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if st.LastFailureAt.IsZero() || st.LastFailureReason == "" {
		t.Fatalf("failure was not recorded on the health row: %+v", st)
	}
	if !st.LastSuccessAt.IsZero() {
		t.Fatal("a failed export recorded a success timestamp")
	}
}

func TestBackupPruneJobRemovesOnlyAgedArchives(t *testing.T) {
	cfg, svc, backupDir := scheduleFixture(t)
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	fresh := "hikyo-sqlite-" + now.Format("20060102T150405Z") + ".age"
	old := "hikyo-sqlite-" + now.AddDate(0, 0, -400).Format("20060102T150405Z") + ".age"
	for _, name := range []string{fresh, old} {
		if err := os.WriteFile(filepath.Join(backupDir, name), []byte("archive"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg.BackupRetainCount = 1
	cfg.BackupRetainDays = 180
	prune := backupJobs(cfg, quietLogger(), svc)[1]
	if err := prune.Run(t.Context()); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, old)); !os.IsNotExist(err) {
		t.Fatal("the aged archive survived the prune")
	}
	if _, err := os.Stat(filepath.Join(backupDir, fresh)); err != nil {
		t.Fatal("the fresh archive was pruned")
	}
	at, ok, err := prune.LastSuccess(t.Context())
	if err != nil || !ok || at.IsZero() {
		t.Fatalf("prune last-success probe = (%v, %v, %v)", at, ok, err)
	}
}

func TestBackupScheduledGuardsJobRegistration(t *testing.T) {
	// The app only appends the jobs when BackupScheduled() is true; a config
	// with no export policy reports no schedule.
	cfg := &config.Config{Store: config.Datastore{Engine: config.EngineSQLite, Path: filepath.Join(t.TempDir(), "hikyo.db")}}
	if cfg.BackupScheduled() {
		t.Fatal("an unconfigured instance reports a schedule")
	}
}
