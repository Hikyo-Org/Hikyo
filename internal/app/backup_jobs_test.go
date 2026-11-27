package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate/testfixture"
)

// scheduleFixture is a fully migrated sqlite instance with a real export
// policy, so the scheduled jobs have a datastore and a destination.
func scheduleFixture(t *testing.T) (*config.Config, *service.Backup, string) {
	t.Helper()
	dir := t.TempDir()
	sc := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(dir, "hikyo.db")}
	root, err := crypto.GenerateRootKey()
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.Zero(root)
	admission := testfixture.Prepare(t, upgrade.Config{Engine: "sqlite", Path: sc.Path}, store.MigrationsFS, "migrations/sqlite", root)
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
	db, err := store.Open(t.Context(), sc, admission)
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
	old := "hikyo-sqlite-" + now.AddDate(0, 0, -400).Format("20060102T150405Z") + ".age"
	for _, name := range []string{old} {
		if err := os.WriteFile(filepath.Join(backupDir, name), []byte("archive"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := svc.Export(t.Context(), backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordExport(t.Context(), service.TriggerManual, result); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Base(result.Path)
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

// A job refused by the runtime-configuration gate never reaches Run, so the
// refusal must still land on the health row and the instance trail.
func TestRefusedBackupExportRecordsFailure(t *testing.T) {
	cfg, svc, backupDir := scheduleFixture(t)
	refusal := errors.Join(service.ErrSelfConfigUnavailable, errors.New("read generation: could not serialize access"))
	jobs := backupJobs(cfg, quietLogger(), svc)
	gated := gateOnRuntimeConfig(jobs[0], 0, func(context.Context) error { return refusal })

	err := gated.Run(t.Context())
	if !errors.Is(err, service.ErrSelfConfigUnavailable) {
		t.Fatalf("refused run error = %v, want the refusal", err)
	}
	if n := countInstanceEvents(t, storeConfig(cfg), "backup.export_failed"); n != 1 {
		t.Fatalf("backup.export_failed events = %d, want 1", n)
	}
	st, serr := svc.State(t.Context())
	if serr != nil {
		t.Fatal(serr)
	}
	if st.LastFailureAt.IsZero() || !strings.Contains(st.LastFailureReason, "could not serialize access") {
		t.Fatalf("refusal not recorded with its cause: %+v", st)
	}
	if n := archiveCount(t, backupDir); n != 0 {
		t.Fatalf("a refused export published %d archives", n)
	}
	// A job without OnRefused (prune) just returns the refusal.
	if err := gateOnRuntimeConfig(jobs[1], 0, func(context.Context) error { return refusal }).Run(t.Context()); !errors.Is(err, service.ErrSelfConfigUnavailable) {
		t.Fatalf("gated prune error = %v", err)
	}
}

// Only a refusal of an export that was due is a failure. A fence is by design
// and a not-due pass has nothing to fail, so neither may write the trail or
// overwrite the health reason.
func TestRefusedBackupExportOnlyRecordsDueNonFencedFailures(t *testing.T) {
	cfg, svc, backupDir := scheduleFixture(t)
	jobs := backupJobs(cfg, quietLogger(), svc)
	fault := errors.Join(service.ErrSelfConfigUnavailable, errors.New("read generation: boom"))
	refuse := func(err error) error {
		return gateOnRuntimeConfig(jobs[0], 0, func(context.Context) error { return err }).Run(t.Context())
	}
	failed := func() int64 { return countInstanceEvents(t, storeConfig(cfg), "backup.export_failed") }

	// Due, but fenced: refused, not recorded.
	if err := refuse(service.ErrSelfConfigFenced); !errors.Is(err, service.ErrSelfConfigFenced) {
		t.Fatalf("fenced refusal = %v", err)
	}
	if n := failed(); n != 0 {
		t.Fatalf("a fenced refusal wrote %d failure events", n)
	}

	// Not due (an export just succeeded): a fault refusal is not recorded either.
	if err := jobs[0].Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := refuse(fault); !errors.Is(err, service.ErrSelfConfigUnavailable) {
		t.Fatalf("not-due refusal = %v", err)
	}
	if n := failed(); n != 0 {
		t.Fatalf("a not-due refusal wrote %d failure events", n)
	}
	st, err := svc.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !st.LastFailureAt.IsZero() || st.LastFailureReason != "" {
		t.Fatalf("health row overwritten by a refusal that was not a failure: %+v", st)
	}
	if n := archiveCount(t, backupDir); n != 1 {
		t.Fatalf("archives = %d, want the single earlier export", n)
	}
}

// When the due check itself fails the refusal must still surface, joined with
// the check's error rather than replaced by it or silently dropped.
func TestRefusedBackupExportKeepsDueCheckError(t *testing.T) {
	cfg, svc, _ := scheduleFixture(t)
	jobs := backupJobs(cfg, quietLogger(), svc)
	refusal := errors.Join(service.ErrSelfConfigUnavailable, errors.New("read generation: boom"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // svc.Due reads state with this context and fails.
	err := gateOnRuntimeConfig(jobs[0], 0, func(context.Context) error { return refusal }).Run(ctx)
	if !errors.Is(err, service.ErrSelfConfigUnavailable) || !errors.Is(err, context.Canceled) {
		t.Fatalf("refusal with failing due check = %v, want both the refusal and the due-check error", err)
	}
}
