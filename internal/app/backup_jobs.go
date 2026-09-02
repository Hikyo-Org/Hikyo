package app

// The in-process disaster-recovery schedule (#145, ops spec § 11): a daily
// export and a retention prune, registered on the same scheduler as payload
// GC so they inherit its startup catch-up, its per-job deadline and, under
// HA, its singleton lease. Neither job loads the root key: the export writes
// ciphertext it never decrypts, and the pruner deletes files by name.
//
// Both jobs are idempotent and re-runnable. The export gates itself on the
// persisted last success rather than on the tick (the scheduler ticks hourly,
// the interval is at least an hour), so a node that failed over mid-day does
// not export twice and a box that slept through its schedule exports on
// boot. The pruner is a pure function of the directory listing.

import (
	"context"
	"log/slog"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// backupPolicy is the config-to-service projection of the DR schedule.
func backupPolicy(cfg *config.Config) service.BackupPolicy {
	return service.BackupPolicy{
		Scheduled: cfg.BackupScheduled(), Interval: cfg.BackupInterval, RPO: cfg.BackupRPO,
		RetainCount: cfg.BackupRetainCount, RetainDays: cfg.BackupRetainDays, RTOTarget: cfg.BackupRTOTarget,
	}
}

// backupJobs builds the two scheduled jobs. It is only called when the
// export policy is complete (config refuses schedule knobs without one), so
// a missing recipient set can never reach Export and be answered with a
// plaintext archive: Export refuses zero recipients regardless, and the
// failure path below records the refusal loudly rather than working around it.
func backupJobs(cfg *config.Config, log *slog.Logger, svc *service.Backup) []ScheduledJob {
	policy := backupPolicy(cfg)
	return []ScheduledJob{{
		Name: "backup_export",
		Run: func(ctx context.Context) error {
			due, err := svc.Due(ctx, policy.Interval)
			if err != nil {
				return err
			}
			if !due {
				return nil
			}
			result, err := svc.Export(ctx, cfg.BackupDir)
			if err != nil {
				// Loud and durable: instance trail + health row, then the
				// scheduler's own error log. Never a fallback format.
				return svc.RecordFailure(ctx, service.TriggerScheduled, err)
			}
			if err := svc.RecordExport(ctx, service.TriggerScheduled, result); err != nil {
				return svc.RecordFailure(ctx, service.TriggerScheduled, err)
			}
			log.Info("scheduled backup exported", "path", result.Path, "bytes", result.Bytes,
				"engine", result.Manifest.Engine, "schema_version", result.Manifest.SchemaVersion)
			return nil
		},
		LastSuccess: svc.LastExportSuccess,
	}, {
		Name: "backup_prune",
		Run: func(ctx context.Context) error {
			result, err := svc.Prune(ctx, cfg.BackupDir, service.PrunePolicy{
				RetainCount: policy.RetainCount, RetainDays: policy.RetainDays,
			})
			if len(result.Deleted) > 0 {
				log.Info("backup archives pruned", "deleted", len(result.Deleted), "kept", result.Kept)
			}
			return err
		},
		LastSuccess: svc.LastPruneSuccess,
	}}
}
