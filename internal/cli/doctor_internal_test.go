package cli

import (
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

func TestDoctorResultsUseServerWarningsWithoutRecalculation(t *testing.T) {
	effectiveAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	lastPrune := effectiveAt.Add(-time.Hour)
	result, rows := doctorResults(apigen.SamlProviderList{Providers: []apigen.SamlProvider{{
		Slug: "corp",
		Warnings: []apigen.SamlProviderWarning{{
			Code: apigen.MetadataExpired, Severity: apigen.SamlProviderWarningSeverityError,
			Message: "server message", EffectiveAt: effectiveAt,
		}},
	}}}, apigen.RetentionHealth{LastPruneSuccess: &lastPrune, Stale: false, StaleAfterSeconds: 86400, Backup: healthyBackup(effectiveAt)}, effectiveAt)
	// Findings: [0] retention-prune, [1] project-storage, [2] backup-rpo,
	// [3] restore-drill, [4] the provider error.
	if result.Status != "error" || len(result.Findings) != 5 {
		t.Fatalf("doctor result = %#v", result)
	}
	if got := result.Findings[4]; got.Provider != "corp" || got.Code != "metadata_expired" || got.Message != "server message" {
		t.Fatalf("doctor finding = %#v", got)
	}
	if len(rows) != 5 || rows[4][4] != "server message" {
		t.Fatalf("doctor rows = %#v", rows)
	}
}

// healthyBackup is a scheduled, fresh, drilled DR state: both backup findings
// come back ok, so the tests above see only what they assert.
func healthyBackup(now time.Time) apigen.BackupHealth {
	export := now.Add(-time.Hour)
	drill := now.Add(-24 * time.Hour)
	return apigen.BackupHealth{
		Scheduled: true, LastSuccessAt: &export, ArtifactAgeSeconds: 3600, RpoSeconds: 26 * 3600,
		LastDrillAt: &drill, LastDrillOk: true,
	}
}

func TestDoctorBackupFindings(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	export := now.Add(-27 * time.Hour)
	failure := now.Add(-time.Hour)
	drill := now.Add(-91 * 24 * time.Hour)
	tests := []struct {
		name          string
		backup        apigen.BackupHealth
		rpoSeverity   string
		rpoMessage    string
		drillSeverity string
		drillMessage  string
	}{
		{"healthy", healthyBackup(now), "ok", "newest export is 1h0m0s old (RPO 26h0m0s)", "ok", "last successful restore drill is 24h0m0s old"},
		{"unscheduled", apigen.BackupHealth{DrillStale: true}, "warn", "no export schedule: set HIKYO_BACKUP_RECIPIENTS and HIKYO_BACKUP_DIR",
			"warn", "no restore drill has ever been recorded: run `hikyo restore drill` quarterly"},
		{"never exported", apigen.BackupHealth{Scheduled: true, RpoSeconds: 26 * 3600, RpoExceeded: true, DrillStale: true},
			"error", "no successful export has ever been recorded (RPO 26h0m0s)",
			"warn", "no restore drill has ever been recorded: run `hikyo restore drill` quarterly"},
		{"rpo exceeded with a later failure", apigen.BackupHealth{
			Scheduled: true, LastSuccessAt: &export, ArtifactAgeSeconds: 27 * 3600, RpoSeconds: 26 * 3600, RpoExceeded: true,
			LastFailureAt: &failure, LastFailureReason: "backup destination /mnt/backups: permission denied",
			LastDrillAt: &drill, LastDrillOk: true, DrillStale: true,
		}, "error", "newest export is 27h0m0s old, over the 26h0m0s RPO; last export failed: backup destination /mnt/backups: permission denied",
			"warn", "last successful restore drill is 2184h0m0s old (> 90d)"},
		{"failed drill", apigen.BackupHealth{
			Scheduled: true, LastSuccessAt: &failure, ArtifactAgeSeconds: 3600, RpoSeconds: 26 * 3600,
			LastDrillAt: &failure, LastDrillOk: false, DrillStale: true,
		}, "ok", "newest export is 1h0m0s old (RPO 26h0m0s)", "warn", "last restore drill 1h0m0s ago FAILED; the recovery path is not proven"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := doctorBackupFindings(tc.backup, now)
			if len(got) != 2 || got[0].Code != "backup-rpo" || got[1].Code != "restore-drill" {
				t.Fatalf("findings = %#v", got)
			}
			if got[0].Severity != tc.rpoSeverity || got[0].Message != tc.rpoMessage {
				t.Fatalf("rpo finding = %#v", got[0])
			}
			if got[1].Severity != tc.drillSeverity || got[1].Message != tc.drillMessage {
				t.Fatalf("drill finding = %#v", got[1])
			}
		})
	}
	// An RPO breach is an ERROR, so doctor exits refused: the check a cron
	// job runs must fail, not merely print.
	result, _ := doctorResults(apigen.SamlProviderList{}, apigen.RetentionHealth{
		Stale: false, StaleAfterSeconds: 86400,
		Backup: apigen.BackupHealth{Scheduled: true, RpoSeconds: 26 * 3600, RpoExceeded: true, DrillStale: true},
	}, now)
	if result.Status != "error" {
		t.Fatalf("an exceeded RPO left doctor status %q", result.Status)
	}
}

func TestDoctorStorageFinding(t *testing.T) {
	const gib = 1 << 30
	tests := []struct {
		name     string
		health   apigen.RetentionHealth
		severity string
		message  string
	}{
		{"empty", apigen.RetentionHealth{PeakProjectBytes: 0, StorageWarn: false}, "ok", "peak project holds 0.0 MiB"},
		{"under", apigen.RetentionHealth{PeakProjectBytes: 512 << 20, StorageWarn: false}, "ok", "peak project holds 512.0 MiB"},
		{"warn", apigen.RetentionHealth{PeakProjectBytes: 3 * gib / 2, StorageWarn: true}, "warn",
			"peak project holds 1.50 GiB, at or over the 1 GiB warn (4 GiB refuses new publishes)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := doctorStorageFinding(tc.health)
			if got.Severity != tc.severity || got.Message != tc.message {
				t.Fatalf("finding = %#v", got)
			}
		})
	}
}

func TestDoctorPruneHealthRows(t *testing.T) {
	now := time.Date(2026, 9, 2, 0, 0, 1, 0, time.UTC)
	old := now.Add(-24*time.Hour - time.Second)
	tests := []struct {
		name    string
		health  apigen.RetentionHealth
		status  string
		message string
	}{
		{"never", apigen.RetentionHealth{Stale: true, StaleAfterSeconds: 86400}, "warn", "never recorded"},
		{"stale", apigen.RetentionHealth{LastPruneSuccess: &old, Stale: true, StaleAfterSeconds: 86400}, "warn", "last_prune_success is 24h0m1s old (> 24h)"},
		{"fresh", apigen.RetentionHealth{LastPruneSuccess: &now, Stale: false, StaleAfterSeconds: 86400}, "ok", "last_prune_success is 0s old"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := doctorPruneFinding(tc.health, now)
			if got.Severity != tc.status || got.Message != tc.message {
				t.Fatalf("finding = %#v", got)
			}
		})
	}
}
