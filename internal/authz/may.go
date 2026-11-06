package authz

import (
	"context"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// CallerMayListEnvironment reports whether a project's environment listing would
// show the caller this environment, WITHOUT leaving a grant.denied record. It is
// for cosmetic disclosure on a read that has already authorized its own subject
// (for example naming an environment's org and project): a refusal there is not
// an attempted operation, so it must neither become audit noise nor turn the
// successful read into a denial. It mints no proof and gates nothing; any write
// or sensitive read still calls Authorize.
func (a *TxAuthorizer) CallerMayListEnvironment(ctx context.Context, caller Identity, scope domain.Scope, env domain.EnvID) (bool, error) {
	before := len(a.denials)
	proof, err := a.Authorize(ctx, caller, OpEnvList, scope)
	if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrUnauthorized) {
		a.denials = a.denials[:before]
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return NavigationEnvironmentAllowed(proof, env), nil
}
