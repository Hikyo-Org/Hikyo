package app

import (
	"errors"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
)

// ValidateBackupPolicy refuses a hybrid recipient policy that no export could
// satisfy, at startup instead of at the first scheduled export: a mixed,
// oversized, duplicated or malformed hybrid set. internal/config is a leaf
// package and cannot import the backup package, so every caller of
// config.Load or config.LoadBootstrap runs this next. An X25519-only list is
// left exactly as it was: it is validated at export, as before.
func ValidateBackupPolicy(cfg *config.Config) error {
	if cfg == nil || !backup.AnyHybridRecipient(cfg.BackupRecipients) {
		return nil
	}
	if err := (backup.Options{Recipients: cfg.BackupRecipients}).Validate(); err != nil {
		if backup.IsPolicyError(err) {
			return fmt.Errorf("HIKYO_BACKUP_RECIPIENTS: %w", err)
		}
		return errors.New("HIKYO_BACKUP_RECIPIENTS contains an invalid hybrid recipient")
	}
	return nil
}

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
