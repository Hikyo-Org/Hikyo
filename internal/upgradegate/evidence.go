package upgradegate

import (
	"context"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func verifyNewEvidence(ctx context.Context, request Request, observed upgrade.InstalledSource, plan upgradecompat.Plan, floor releaseidentity.SnapshotFloor, root releaseidentity.Digest) (upgrade.Acceptance, upgrade.Incarnation, string, error) {
	acceptance := upgrade.Acceptance{Floor: floor, ReleaseRootDigest: root}
	if observed.Source.Genesis == releaseidentity.FreshGenesisV1 {
		return acceptance, upgrade.Incarnation{}, "", nil
	}
	var evidence backupreceipt.VerifiedEvidence
	var incarnation upgrade.Incarnation
	if observed.Ledger == nil {
		if !request.LegacyWritersStopped {
			return acceptance, incarnation, "", errors.New("legacy adoption requires all previous writers stopped")
		}
		receipt, err := backupreceipt.ParseReceipt(request.Evidence.Receipt)
		if err != nil {
			return acceptance, incarnation, "", err
		}
		proposal := backupreceipt.LegacyProposal{RecoveryIncarnation: receipt.Snapshot.RecoveryIncarnation}
		inspected := backupreceipt.LegacyInspection{InstanceID: observed.InstanceID, Engine: request.Store.Engine, SchemaSHA256: observed.SchemaDigest, MigrationSHA256: observed.MigrationDigest, RestoreEpoch: observed.RestoreEpoch}
		evidence, err = backupreceipt.VerifyLegacyEvidence(ctx, request.Operator, plan, request.Ciphertext, request.Evidence, inspected, proposal, time.Now())
		if err != nil {
			return acceptance, incarnation, "", err
		}
		if err := incarnation.UnmarshalText([]byte(proposal.RecoveryIncarnation)); err != nil {
			return acceptance, incarnation, "", err
		}
	} else {
		state := observed.Ledger
		if state.Pending != nil && !state.Pending.Invalidated && state.Pending.Acceptance.Attestation != nil && state.Pending.Acceptance.Attestation.OperatorKeyID != request.Operator.KeyID() {
			return acceptance, incarnation, "", errors.New("operator key differs from installed pin; explicit rotation required")
		}
		encoded, err := state.RecoveryIncarnation.MarshalText()
		if err != nil {
			return acceptance, incarnation, "", err
		}
		live := backupreceipt.LiveSource{InstanceID: observed.InstanceID, Engine: request.Store.Engine, Source: observed.Source, SourceSchemaSHA256: observed.SchemaDigest, MigrationSHA256: observed.MigrationDigest, RestoreEpoch: observed.RestoreEpoch, RecoveryIncarnation: backupreceipt.Nonce(encoded), Generation: state.Generation}
		evidence, err = backupreceipt.VerifyEvidence(ctx, request.Operator, plan, request.Ciphertext, request.Evidence, live, time.Now())
		if err != nil {
			return acceptance, incarnation, "", err
		}
		incarnation = state.RecoveryIncarnation
	}
	statement := evidence.Statement()
	var nonce upgrade.Incarnation
	if err := nonce.UnmarshalText([]byte(statement.Nonce)); err != nil {
		return acceptance, incarnation, "", err
	}
	acceptance.Attestation = &upgrade.AttestationUse{Authority: string(statement.Authority), Nonce: nonce, EvidenceDigest: evidence.Digest(), OperatorKeyID: statement.OperatorKeyID, InstanceID: statement.InstanceID, RestoreEpoch: statement.RestoreEpoch, RecoveryIncarnation: incarnation, RouteGeneration: statement.RouteGeneration, RouteDigest: statement.RouteSHA256, IssuedAt: statement.IssuedAt, ExpiresAt: statement.ExpiresAt}
	return acceptance, incarnation, string(evidence.Receipt().Snapshot.BackupID), nil
}
