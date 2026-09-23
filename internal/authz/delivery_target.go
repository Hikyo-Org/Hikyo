package authz

import (
	"context"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// DeliveryReporterLive reports whether a principal that owns a delivery-target
// row can still report on it (k8s-condition-reporting ADR D5): it holds
// `report-delivery-status` on the scope, and at least one live credential
// (an unrevoked, unexpired `hikyo-token` or an active federation binding).
//
// It records no denial and applies no session policy: the principal is not
// the caller. It answers only about a principal that already owns a row in an
// environment the caller was authorized to list, so it discloses nothing the
// list does not already name.
func (a *TxAuthorizer) DeliveryReporterLive(ctx context.Context, principal domain.PrincipalID, scope domain.Scope, now time.Time) (bool, error) {
	_, _, holds, err := a.principalFormulaEvaluation(ctx, principal, OpDeliveryTargetReport, scope)
	if err != nil || !holds {
		return false, err
	}
	sa, err := a.r.ServiceAccountByPrincipal(ctx, principal)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	epoch, err := a.r.CredentialEpoch(ctx)
	if err != nil {
		return false, err
	}
	live, err := a.r.LiveMachineCredentialCount(ctx, sa.ID, epoch, now)
	return live > 0, err
}
