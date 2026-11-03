package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

// The policy compared here is the effective owner policy, never the bootstrap
// field: config.LoadBootstrap blanks owner settings into ManagedInputs, so
// Config.BackupRecipients is empty in a production server process, and a value
// set or changed after adoption lives only in the managed configuration.
// Both callers resolve it through resolveCandidateConfiguration, which is the
// code that builds the configuration an export runs under.
//
// requireCustodyMatchesBackupPolicy stops an automatic or unattended upgrade
// that would retain a classical archive on an install that asked for hybrid
// backups. Both flows export to the custody vault's own recipient, which
// enrollment always mints as X25519, and keep the full-data archive: a
// recorded copy would open to a classical key alone (migration E-10). The
// refusal precedes any export. Installs without a hybrid policy are untouched.
func requireCustodyMatchesBackupPolicy(configured []string, custodyRecipient string) error {
	if !backup.AnyHybridRecipient(configured) || backup.IsHybridRecipient(custodyRecipient) {
		return nil
	}
	return ErrHybridPolicyClassicalCustody
}

// ErrHybridPolicyClassicalCustody is the refusal described above.
var ErrHybridPolicyClassicalCustody = errors.New("HIKYO_BACKUP_RECIPIENTS selects hybrid post-quantum backups, but the automatic upgrade custody identity is X25519 and would export, and retain, a classical archive of this data; refusing before any backup. Use the manual upgrade route with a hybrid recipient and a separately held hybrid identity: hikyo backup upgrade-export --recipient <hybrid recipient>, then the operator hikyo backup upgrade-drill --identity-file <hybrid identity> (see Backup and restore: Hybrid post-quantum recipients)")

// automaticPolicyCheck is the configuration check `sudo hikyo upgrade` runs over
// the source's stored managed configuration. An adopted instance's policy is
// the stored HIKYO_BACKUP_RECIPIENTS value, read through the prepared managed
// bundle exactly as the server reads it; an environment line is stale there.
// An instance with no stored configuration yet runs on its environment seed.
func automaticPolicyCheck(cfg *config.Config, custodyRecipient string) upgradegate.ConfigurationCheck {
	return func(_ context.Context, projection *upgrade.CandidateConfiguration, values map[string]string) error {
		recipients := cfg.BackupRecipients
		if projection != nil {
			bundle, err := runtimeconfig.Prepare(values)
			if err != nil {
				return fmt.Errorf("cannot determine the effective backup policy before the backup: %w", err)
			}
			policy, err := config.ParseManagedOwnerPolicy(bundle.OwnerValues())
			if err != nil {
				return fmt.Errorf("cannot determine the effective backup policy before the backup: %w", err)
			}
			recipients = policy.BackupRecipients
		}
		return requireCustodyMatchesBackupPolicy(recipients, custodyRecipient)
	}
}

// requireAutomaticCustodyMatchesPolicy is the seam RunAutomaticUpgrade calls
// right after it opens custody and before it journals, publishes evidence or
// stops the service. It reads the source's stored managed configuration
// read-only. A source with no upgrade ledger (legacy) predates managed
// configuration, so the environment is the only policy there is.
func requireAutomaticCustodyMatchesPolicy(ctx context.Context, cfg *config.Config, database upgrade.Config, root []byte, custodyRecipient string) error {
	check := automaticPolicyCheck(cfg, custodyRecipient)
	checked, err := upgradegate.CheckSourceConfiguration(ctx, database, root, check)
	if err != nil {
		return err
	}
	if !checked {
		return check(ctx, nil, nil)
	}
	return nil
}
