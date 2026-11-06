package service

import (
	"context"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// requireQualified checks, live, that the caller's account holds a standing
// possession factor. shortcut: federated qualification (a passwordless SSO
// account whose provider passes the reveal gate) is not yet recognized, so
// such accounts enrol a factor; upgrade with the qualification follow-up.
func (s *PersonalValues) requireQualified(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity) error {
	ok, err := personalQualified(ctx, az, caller.Principal)
	if err != nil {
		return err
	}
	if !ok {
		return ErrPersonalUnqualified
	}
	return nil
}

func personalQualified(ctx context.Context, az *authz.TxAuthorizer, owner domain.PrincipalID) (bool, error) {
	account, err := az.AccountByPrincipal(ctx, owner)
	if err != nil {
		return false, err
	}
	factors, err := enrolledFactors(ctx, az, account.ID)
	return len(factors) > 0, err
}

// purgePersonalValues deletes the owner's personal values in this
// authentication transaction and records the purge on the instance trail.
func purgePersonalValues(ctx context.Context, az *authz.TxAuthorizer, owner domain.PrincipalID, cause string, now time.Time) error {
	n, err := az.PurgePersonalValues(ctx, owner, cause, now)
	if err != nil || n == 0 {
		return err
	}
	e, err := newAuditEvent(ctx, audit.EventPersonalValueDeleted, owner, audit.Object{Type: "principal", ID: string(owner)},
		audit.OutcomeSuccess, "", audit.Payload{"owner_principal_id": string(owner), "cause": cause, "count": int(n)})
	if err != nil {
		return err
	}
	return az.RecordAuthEvent(ctx, e)
}

// purgePersonalIfUnqualified purges once a credential change leaves the
// account with no standing possession factor.
func purgePersonalIfUnqualified(ctx context.Context, az *authz.TxAuthorizer, account authz.Account, now time.Time) error {
	factors, err := enrolledFactors(ctx, az, account.ID)
	if err != nil || len(factors) > 0 {
		return err
	}
	return purgePersonalValues(ctx, az, account.PrincipalID, PersonalCauseQualificationLost, now)
}
