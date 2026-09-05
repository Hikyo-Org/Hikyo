package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

type doctorFinding struct {
	Provider    string  `json:"provider"`
	Code        string  `json:"code"`
	Severity    string  `json:"severity"`
	Message     string  `json:"message"`
	EffectiveAt string  `json:"effective_at"`
	Fingerprint *string `json:"fingerprint,omitempty"`
}

type doctorResult struct {
	Status   string          `json:"status"`
	Findings []doctorFinding `json:"findings"`
}

func runDoctor(ctx context.Context, ios IO, args []string) error {
	var format string
	st, flags, err := parseCommon("doctor", ios, args, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
	})
	if err != nil {
		return err
	}
	if err := flags.checkNoPositionals("doctor"); err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	client, _, err := authenticatedClient(st, ios, flags)
	if err != nil {
		return err
	}
	var health apigen.RetentionHealth
	if err := client.Do(ctx, http.MethodGet, api.PathPrefix+"/instance/retention-health", nil, &health); err != nil {
		return err
	}
	var providers apigen.SamlProviderList
	if err := client.Do(ctx, http.MethodGet, api.PathPrefix+"/instance/saml-providers", nil, &providers); err != nil {
		return err
	}
	result, rows := doctorResults(providers, health, time.Now().UTC())
	if err := Render(ios.Stdout, f, Table{
		Columns: []string{"STATUS", "PROVIDER", "CHECK", "EFFECTIVE AT", "MESSAGE"}, Rows: rows, JSON: result,
	}); err != nil {
		return err
	}
	if result.Status == "error" {
		return failf(ExitRefused, "doctor found errors")
	}
	return nil
}

func doctorResults(providers apigen.SamlProviderList, health apigen.RetentionHealth, now time.Time) (doctorResult, [][]string) {
	result := doctorResult{Status: "ok", Findings: []doctorFinding{}}
	rows := make([][]string, 0, 2)
	prune := doctorPruneFinding(health, now)
	result.Findings = append(result.Findings, prune)
	rows = append(rows, []string{prune.Severity, prune.Provider, prune.Code, prune.EffectiveAt, prune.Message})
	if prune.Severity == "warn" {
		result.Status = "warning"
	}
	storage := doctorStorageFinding(health)
	result.Findings = append(result.Findings, storage)
	rows = append(rows, []string{storage.Severity, storage.Provider, storage.Code, storage.EffectiveAt, storage.Message})
	if storage.Severity == "warn" && result.Status == "ok" {
		result.Status = "warning"
	}
	for _, finding := range doctorBackupFindings(health.Backup, now) {
		result.Findings = append(result.Findings, finding)
		rows = append(rows, []string{finding.Severity, finding.Provider, finding.Code, finding.EffectiveAt, finding.Message})
		switch finding.Severity {
		case "error":
			result.Status = "error"
		case "warn":
			if result.Status == "ok" {
				result.Status = "warning"
			}
		}
	}
	adapters := doctorAdapterFinding(health)
	result.Findings = append(result.Findings, adapters)
	rows = append(rows, []string{adapters.Severity, adapters.Provider, adapters.Code, adapters.EffectiveAt, adapters.Message})
	if adapters.Severity == "warn" && result.Status == "ok" {
		result.Status = "warning"
	}
	for _, finding := range doctorDiagnosticFindings(health) {
		result.Findings = append(result.Findings, finding)
		rows = append(rows, []string{finding.Severity, finding.Provider, finding.Code, finding.EffectiveAt, finding.Message})
		switch finding.Severity {
		case "error":
			result.Status = "error"
		case "warn", "unknown":
			if result.Status == "ok" {
				result.Status = "warning"
			}
		}
	}
	providerRowStart := len(rows)
	for _, provider := range providers.Providers {
		for _, warning := range provider.Warnings {
			finding := doctorFinding{
				Provider: provider.Slug, Code: string(warning.Code), Severity: string(warning.Severity),
				Message: warning.Message, EffectiveAt: warning.EffectiveAt.UTC().Format(time.RFC3339), Fingerprint: warning.Fingerprint,
			}
			result.Findings = append(result.Findings, finding)
			rows = append(rows, []string{finding.Severity, finding.Provider, finding.Code, finding.EffectiveAt, finding.Message})
			if warning.Severity == apigen.SamlProviderWarningSeverityError {
				result.Status = "error"
			} else if result.Status == "ok" {
				result.Status = "warning"
			}
		}
	}
	if len(rows) == providerRowStart {
		rows = append(rows, []string{"ok", "-", "saml-providers", "-", "no provider warnings"})
	}
	return result, rows
}

// doctorDiagnosticFindings preserves server verdicts, including explicit unknown
// measurements. Older servers omit the optional field: absence is not health.
func doctorDiagnosticFindings(health apigen.RetentionHealth) []doctorFinding {
	if health.Diagnostics == nil || len(*health.Diagnostics) == 0 {
		return []doctorFinding{{Provider: "-", Code: "diagnostics", Severity: "unknown", EffectiveAt: "-",
			Message: "operational diagnostics are unavailable from this server; upgrade the server to measure data volume, root escrow, pin expiry, root rotation, re-encryption, database durability, and Argon2 floor"}}
	}
	findings := make([]doctorFinding, 0, len(*health.Diagnostics))
	for _, diagnostic := range *health.Diagnostics {
		findings = append(findings, doctorFinding{Provider: "-", Code: diagnostic.Code,
			Severity: string(diagnostic.Severity), Message: diagnostic.Message, EffectiveAt: "-"})
	}
	return findings
}

func doctorPruneFinding(health apigen.RetentionHealth, now time.Time) doctorFinding {
	finding := doctorFinding{Provider: "-", Code: "retention-prune", Severity: "ok", EffectiveAt: "-"}
	if health.LastPruneSuccess == nil {
		finding.Severity = "warn"
		finding.Message = "never recorded"
		return finding
	}
	at := health.LastPruneSuccess.UTC()
	finding.EffectiveAt = at.Format(time.RFC3339)
	age := now.Sub(at)
	if age < 0 {
		age = 0
	}
	age = age.Truncate(time.Second)
	if health.Stale {
		finding.Severity = "warn"
		finding.Message = fmt.Sprintf("last_prune_success is %s old (> 24h)", age)
		return finding
	}
	finding.Message = fmt.Sprintf("last_prune_success is %s old", age)
	return finding
}

// doctorStorageFinding surfaces the per-project storage high-water (#185): a
// warn once the instance's peak project reaches the 1 GiB threshold, so the
// operator sees it long before the 4 GiB publish refusal. The server owns the
// threshold; storage_warn is its verdict.
func doctorStorageFinding(health apigen.RetentionHealth) doctorFinding {
	finding := doctorFinding{Provider: "-", Code: "project-storage", Severity: "ok", EffectiveAt: "-"}
	peak := formatBytesGiB(health.PeakProjectBytes)
	if health.StorageWarn {
		finding.Severity = "warn"
		finding.Message = fmt.Sprintf("peak project holds %s, at or over the 1 GiB warn (4 GiB refuses new publishes)", peak)
		return finding
	}
	finding.Message = fmt.Sprintf("peak project holds %s", peak)
	return finding
}

// doctorAdapterFinding surfaces deployment-adapter targets that need a human
// (#157): a destination that drifted from the ownership ledger, or a target
// whose last attempt failed. Paused targets are reported but never warn: an
// operator chose that state. Counts only; which targets is a tenant read.
func doctorAdapterFinding(health apigen.RetentionHealth) doctorFinding {
	finding := doctorFinding{Provider: "-", Code: "adapter-targets", Severity: "ok", EffectiveAt: "-"}
	summary := fmt.Sprintf("%d target(s) need attention, %d failed, %d paused, %d job(s) queued", health.AdapterTargetsAttention, health.AdapterTargetsFailed, health.AdapterTargetsPaused, health.AdapterJobsQueued)
	if health.AdapterTargetsAttention > 0 || health.AdapterTargetsFailed > 0 {
		finding.Severity = "warn"
		finding.Message = summary + "; inspect them with `hikyo adapter list`"
		return finding
	}
	finding.Message = summary
	return finding
}

// formatBytesGiB renders a byte count for operator eyes: GiB with two decimals
// once past a gibibyte, MiB below that.
func formatBytesGiB(bytes int) string {
	const gib = 1 << 30
	if bytes >= gib {
		return fmt.Sprintf("%.2f GiB", float64(bytes)/float64(gib))
	}
	return fmt.Sprintf("%.1f MiB", float64(bytes)/float64(1<<20))
}

// doctorBackupFindings is the disaster-recovery half (#145, ops-spec section
// 11): the recovery point objective as an ERROR when exceeded (a silently
// failing export converts the RPO to infinity, and doctor's exit status is
// how a cron-driven check notices), and the quarterly restore drill as a
// warning when stale or never run. The server owns both verdicts; doctor
// renders them.
func doctorBackupFindings(b apigen.BackupHealth, now time.Time) []doctorFinding {
	age := func(at *time.Time) (string, time.Duration) {
		if at == nil {
			return "-", 0
		}
		d := now.Sub(at.UTC())
		if d < 0 {
			d = 0
		}
		return at.UTC().Format(time.RFC3339), d.Truncate(time.Second)
	}
	rpo := doctorFinding{Provider: "-", Code: "backup-rpo", Severity: "ok"}
	rpo.EffectiveAt, _ = age(b.LastSuccessAt)
	_, artifactAge := age(b.LastSuccessAt)
	switch {
	case !b.Scheduled:
		rpo.Severity = "warn"
		rpo.Message = "no export schedule: set HIKYO_BACKUP_RECIPIENTS and HIKYO_BACKUP_DIR"
	case b.RpoExceeded && b.LastSuccessAt == nil:
		rpo.Severity = "error"
		rpo.Message = fmt.Sprintf("no successful export has ever been recorded (RPO %s)", time.Duration(b.RpoSeconds)*time.Second)
	case b.RpoExceeded:
		rpo.Severity = "error"
		rpo.Message = fmt.Sprintf("newest export is %s old, over the %s RPO", artifactAge, time.Duration(b.RpoSeconds)*time.Second)
	default:
		rpo.Message = fmt.Sprintf("newest export is %s old (RPO %s)", artifactAge, time.Duration(b.RpoSeconds)*time.Second)
	}
	if b.LastFailureAt != nil && (b.LastSuccessAt == nil || b.LastFailureAt.After(*b.LastSuccessAt)) {
		rpo.Message += "; last export failed: " + b.LastFailureReason
	}
	drill := doctorFinding{Provider: "-", Code: "restore-drill", Severity: "ok"}
	drill.EffectiveAt, _ = age(b.LastDrillAt)
	_, drillAge := age(b.LastDrillAt)
	switch {
	case b.LastDrillAt == nil:
		drill.Severity = "warn"
		drill.Message = "no restore drill has ever been recorded: run `hikyo restore drill` quarterly"
	case !b.LastDrillOk:
		drill.Severity = "warn"
		drill.Message = fmt.Sprintf("last restore drill %s ago FAILED; the recovery path is not proven", drillAge)
	case b.DrillStale:
		drill.Severity = "warn"
		drill.Message = fmt.Sprintf("last successful restore drill is %s old (> 90d)", drillAge)
	default:
		drill.Message = fmt.Sprintf("last successful restore drill is %s old", drillAge)
	}
	return []doctorFinding{rpo, drill}
}
