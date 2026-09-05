package upgradegate

import (
	"context"
	"errors"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func recoveryOperation(plan upgradecompat.Plan, state upgrade.State, incarnation upgrade.Incarnation, backup string, acceptance upgrade.Acceptance) upgrade.Operation {
	return upgrade.Operation{Kind: upgrade.RecoveryOperation, Source: state.Applied, RouteSource: state.Applied, Target: plan.Target(), SourceMigrationDigest: state.MigrationDigest, TargetMigrationDigest: state.MigrationDigest, SourceSchemaDigest: state.SchemaDigest, TargetSchemaDigest: state.SchemaDigest, RouteDigest: plan.Digest(), RouteLength: 1, Generation: state.Generation + 1, RecoveryIncarnation: incarnation, BackupID: backup, Acceptance: acceptance, Phase: upgrade.Prepared}
}

func executeRecovery(ctx context.Context, session *upgrade.Session, request Request, node upgradecompat.VerifiedNode, state upgrade.State, root []byte, result *Result) error {
	if state.Pending == nil || state.Pending.Invalidated || state.Pending.Kind != upgrade.RecoveryOperation || state.Pending.Target != node.Identity() {
		return upgrade.ErrConflict
	}
	if err := verifyCatalog(ctx, session, node, request.Store.Engine); err != nil {
		return err
	}
	var err error
	if state.Pending.Phase == upgrade.Prepared {
		request.observe(boundaryPrepared)
		state, err = session.ValidateRecoverySchema(ctx, state)
		if err != nil {
			return err
		}
		request.observe(boundarySchemaApplied)
	}
	if state.Pending.Phase != upgrade.SchemaApplied {
		return upgrade.ErrConflict
	}
	if request.Mode == Migrate {
		*result = Result{State: state, SchemaOnly: true}
		return nil
	}
	if err := candidateHealth(ctx, session, state, root, request.CheckConfiguration); err != nil {
		_, markErr := session.Advance(ctx, state, upgrade.RestoreRequired)
		if markErr == nil {
			request.observe(boundaryHealthFailed)
		}
		return errors.Join(fmt.Errorf("restored candidate health refused: %w", err), markErr)
	}
	state, err = session.Advance(ctx, state, upgrade.Healthy)
	if err != nil {
		return err
	}
	request.observe(boundaryHealthy)
	admission, err := session.Admit(ctx, state, node)
	if err != nil {
		return err
	}
	*result = Result{State: state, Admission: admission}
	return nil
}
