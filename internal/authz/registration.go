package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// Registration policy seam (#606). The policy tables are class=authn, so the
// reads and writes ride the proof-free resolution surface after the
// registration-policy.* operation authorized the caller, exactly like provider
// administration.

// RegistrationPolicy is a resolved policy with its child rows.
type RegistrationPolicy = authn.RegistrationPolicy

// RegistrationEntry is one external entry of a policy.
type RegistrationEntry = authn.RegistrationEntry

// RegistrationSignupRef names one pending local sign-up row.
type RegistrationSignupRef = authn.RegistrationSignupRef

// NewRegistrationSignup is the pending-row insert carrier.
type NewRegistrationSignup = authn.NewRegistrationSignup

// RegistrationAuthorityHolds re-checks a policy's standing delegation (#579
// d4): does the recorded authority principal's CURRENT grant set satisfy op's
// formula at scope? It is a pure evaluation. It records no operation and no
// denial, because it runs where no caller is acting as that principal (the
// Members panel render, the public sign-up door); the principal comes from the
// policy row, never from the request, so it is not the unaudited "what can
// THEY do?" oracle CallerHolds refuses to be. A sign-up that acts on the
// answer records its own refusal cause.
//
// Instance operations evaluate against the empty scope (authorizeInstance's
// shape); tenant operations resolve the chain first, and an unresolvable
// scope holds nothing.
func (a *TxAuthorizer) RegistrationAuthorityHolds(ctx context.Context, principal domain.PrincipalID, op Operation, scope domain.Scope) (bool, error) {
	if principal == "" {
		return false, errors.New("authz: empty registration authority principal")
	}
	spec, ok := registry.authorizationSpec(op)
	if !ok {
		return false, fmt.Errorf("authz: operation %q is not in the operation registry", op)
	}
	// Both paths resolve the principal's grants through the same query every
	// authorization uses, which returns nothing for a principal that is not
	// active (restricted or erased): a disabled authority holds nothing, so
	// the policy reads authority-lost without a second status check.
	switch spec.class {
	case ClassInstance:
		if scope != (domain.Scope{}) {
			return false, fmt.Errorf("authz: instance operation %q addressed with a tenant scope", op)
		}
		return a.principalInstanceHolds(ctx, principal, spec)
	case ClassTenant:
		_, _, holds, err := a.principalFormulaEvaluation(ctx, principal, op, scope)
		return holds, err
	default:
		return false, fmt.Errorf("authz: operation %q is not a grant-evaluated operation", op)
	}
}

type RegistrationSignup = authn.RegistrationSignup
type ExpiredRegistrationSignup = authn.ExpiredRegistrationSignup
