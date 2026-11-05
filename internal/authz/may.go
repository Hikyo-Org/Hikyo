package authz

import (
	"context"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// CallerMay reports whether Authorize would admit the caller, WITHOUT leaving a
// grant.denied record. It is for cosmetic disclosure on a read that has already
// authorized its own subject (for example naming an environment's org and
// project): a refusal there is not an attempted operation, so it must neither
// become audit noise nor turn the successful read into a denial. It mints no
// proof and gates nothing; any write or sensitive read still calls Authorize.
func (a *TxAuthorizer) CallerMay(ctx context.Context, caller Identity, op Operation, scope domain.Scope) (bool, error) {
	before := len(a.denials)
	_, err := a.Authorize(ctx, caller, op, scope)
	if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrUnauthorized) {
		a.denials = a.denials[:before]
		return false, nil
	}
	return err == nil, err
}
