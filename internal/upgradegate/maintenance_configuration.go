package upgradegate

import (
	"context"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// The ordinary gate authenticates the executing image, embedded migrations,
// installed trust floor and operator custody before calling this projection.
func inspectMaintenanceConfiguration(ctx context.Context, session *upgrade.Session, request Request, bundle upgradebundle.Bundle, node upgradecompat.VerifiedNode, current upgrade.State) (Result, error) {
	if !current.Maintenance || !request.Operator.Valid() || request.Operator.InstanceID() != current.InstanceID || desiredTarget(request, node) != node.Identity() || request.CheckConfiguration == nil {
		return Result{}, upgrade.ErrConflict
	}
	sourceIdentity, routeDigest := current.Pending.RouteSource, current.Pending.RouteDigest
	if current.Pending.Phase == upgrade.BackupPreparing {
		if current.Pending.Preparation == nil || current.Pending.Preparation.Target != node.Identity() || current.Pending.Preparation.OperatorKeyID != request.Operator.KeyID() {
			return Result{}, upgrade.ErrConflict
		}
		sourceIdentity, routeDigest = current.Applied, current.Pending.Preparation.RouteDigest
	}
	source, err := routeSource(bundle, sourceIdentity, request.Store.Engine)
	if err != nil {
		return Result{}, err
	}
	plan, err := bundle.Plan(source, node.Identity())
	if err != nil || plan.Digest() != routeDigest {
		return Result{}, upgrade.ErrConflict
	}
	keys, err := session.MaintenanceConfigurationKeys(ctx, current)
	if err != nil {
		return Result{}, err
	}
	check := func(ctx context.Context, projection *upgrade.CandidateConfiguration, values map[string]string) error {
		if projection == nil {
			return errors.New("maintenance transport requires saved managed configuration")
		}
		return request.CheckConfiguration(ctx, projection, values)
	}
	if err := checkUpgradeableConfiguration(ctx, keys, request.RootKey, check); err != nil {
		return Result{}, err
	}
	return Result{State: current, SchemaOnly: true}, nil
}
