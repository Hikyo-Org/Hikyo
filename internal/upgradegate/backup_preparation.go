package upgradegate

import (
	"bytes"
	"context"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// prepareUnattendedRoute runs only after exact build, installed trust and
// persistent operator pin verification in run. Neither mode executes migration
// SQL nor mints runtime admission. Preparation preserves the old source until
// a backup has been exported, restored and independently attested.
func prepareUnattendedRoute(ctx context.Context, session *upgrade.Session, request Request, bundle upgradebundle.Bundle, node upgradecompat.VerifiedNode, current upgrade.State, root releaseidentity.Digest) (Result, error) {
	if !request.Operator.Valid() || request.Operator.InstanceID() != current.InstanceID || current.Pending.Invalidated || !current.Applied.IsRelease() || desiredTarget(request, node) != node.Identity() {
		return Result{}, upgrade.ErrConflict
	}
	if current.Pending.Phase != upgrade.Healthy && current.Pending.Phase != upgrade.BackupPreparing {
		return Result{}, errors.New("unattended preparation requires a completed or already frozen source")
	}
	observed, source, err := inspectSource(ctx, request.Store, bundle)
	if err != nil {
		return Result{}, err
	}
	plan, err := bundle.Plan(source, node.Identity())
	if err != nil {
		return Result{}, err
	}
	if len(plan.Steps()) == 0 {
		return Result{}, errors.New("backup preparation requires a newer authenticated target")
	}
	intent := upgrade.BackupPreparation{Target: node.Identity(), RouteDigest: plan.Digest(), OperatorKeyID: request.Operator.KeyID()}
	if request.Mode == PrepareBackup {
		keys, err := session.HealthyKeys(ctx, current)
		if current.Pending.Phase == upgrade.BackupPreparing {
			keys, err = session.FrozenKeys(ctx, current)
		}
		if err != nil {
			return Result{}, err
		}
		if err := crypto.VerifyExistingHierarchy(ctx, keys, bytes.Clone(request.RootKey)); err != nil {
			return Result{}, err
		}
		if err := checkUpgradeableConfiguration(ctx, keys, request.RootKey, request.CheckConfiguration); err != nil {
			return Result{}, err
		}
		state, err := session.FenceBackup(ctx, current, intent)
		return Result{State: state, SchemaOnly: true}, err
	}
	if current.Pending.Phase != upgrade.BackupPreparing || current.Pending.Preparation == nil || *current.Pending.Preparation != intent {
		return Result{}, upgrade.ErrConflict
	}
	acceptance, incarnation, backup, err := verifyNewEvidence(ctx, request, observed, plan, bundle.Snapshot().Floor(), root)
	if err != nil {
		return Result{}, err
	}
	state, err := session.Prepare(ctx, current, operationFor(plan, 0, current.Generation+1, incarnation, backup, acceptance))
	return Result{State: state, SchemaOnly: true}, err
}
