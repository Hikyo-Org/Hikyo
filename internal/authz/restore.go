package authz

// The in-transaction restore-reconciliation surface (#76).
//
// It hangs off TxAuthorizer for the same reason the human-authentication
// surface does: the resolution surface stays importable by exactly
// {authz, tx}, and the service layer reaches it only inside a transaction it
// already holds. What is different here is WHY there is no Authorize() call
// in front of it — see SiteRecoveryReconcile, the tenant-isolation ADR's
// enumerated system mint site for exactly this: a restore leaves every
// session dead and every grant inert, so no principal exists who could
// authorize the reconciliation that ends that state. The gate is host access,
// and host access is operator-equivalent under the threat model already.

import (
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// RestoreState is the instance's restore posture.
type RestoreState = authn.RestoreState

// PrincipalRef names a principal awaiting reconciliation.
type PrincipalRef = authn.PrincipalRef
