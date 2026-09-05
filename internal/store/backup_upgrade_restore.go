package store

import (
	"errors"
	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func matchUpgradePlan(snapshot backupreceipt.Snapshot, plan upgradecompat.Plan) error {
	if snapshot.Validate() != nil || !plan.Valid() {
		return errors.New("upgrade restore requires valid source evidence")
	}
	manifest, err := plan.SourceManifest(snapshot.Engine)
	if err != nil {
		return err
	}
	digest, err := manifest.Digest()
	if err != nil {
		return err
	}
	if digest != snapshot.MigrationSHA256 || snapshot.SourceIdentity != plan.Source() || snapshot.SourceSchemaSHA256 != plan.SourceSchemaDigest() {
		return errors.New("upgrade restore source differs from verified route")
	}
	return nil
}

// Compare imported source facts before the credential callback changes epochs.
// A public manifest cannot substitute a different instance, ledger or proposal.
func matchRestoredUpgradeSnapshot(actual upgrade.InstalledSource, snapshot backupreceipt.Snapshot) error {
	if actual.InstanceID != snapshot.InstanceID || actual.Source != snapshot.SourceIdentity || actual.SchemaDigest != snapshot.SourceSchemaSHA256 || actual.MigrationDigest != snapshot.MigrationSHA256 || actual.RestoreEpoch != snapshot.RestoreEpoch {
		return errors.New("restored source differs from authenticated archive snapshot")
	}
	if snapshot.Authority == backupreceipt.LegacyProposalAuthority {
		if actual.Ledger != nil {
			return errors.New("legacy proposal archive carries a ledger")
		}
		return nil
	}
	if actual.Ledger == nil {
		return errors.New("applied source archive omits its ledger")
	}
	incarnation, err := actual.Ledger.RecoveryIncarnation.MarshalText()
	if err != nil {
		return err
	}
	if string(incarnation) != string(snapshot.RecoveryIncarnation) || actual.Ledger.Generation != snapshot.SourceGeneration {
		return errors.New("restored source authority differs from authenticated archive snapshot")
	}
	return nil
}
