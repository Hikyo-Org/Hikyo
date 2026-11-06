package service

import (
	"context"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// requireQualified checks, live, that the caller's account qualifies for
// personal values (personal-values ADR, "Which accounts qualify").
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
	return accountQualified(ctx, az, account.ID, time.Now())
}

// accountQualified: a standing possession factor, or, on a passwordless
// account, a federated identity that can pass the reveal gate.
func accountQualified(ctx context.Context, az *authz.TxAuthorizer, accountID string, now time.Time) (bool, error) {
	factors, err := enrolledFactors(ctx, az, accountID)
	if err != nil || len(factors) > 0 {
		return len(factors) > 0, err
	}
	return federatedQualified(ctx, az, accountID, "")
}

// federatedQualified reports a passwordless account holding a current-epoch
// OIDC identity whose live provider carries an assurance policy, the static
// half of the reveal gate that provider enforces at reauthentication. A
// password row in the current credential epoch disqualifies it, and OAuth2
// identities never qualify (OAuth2 opens no reauthentication window).
//
// shortcut: a SAML identity does not qualify, because no SAML window can open
// a personal value (see admitFederatedPersonalWindow); a SAML-qualified owner
// could keep values it can never set or reveal. Count SAML once its windows
// are admitted.
//
// A non-empty oidcProvider names the OIDC provider that opened a window: only
// an identity through that very provider then counts.
func federatedQualified(ctx context.Context, az *authz.TxAuthorizer, accountID, oidcProvider string) (bool, error) {
	inventory, err := localProofInventory(ctx, az, accountID)
	if err != nil || inventory.password {
		return false, err
	}
	epoch, err := az.CredentialEpoch(ctx)
	if err != nil {
		return false, err
	}
	identities, err := az.ExternalIdentitiesForAccount(ctx, accountID)
	if err != nil {
		return false, err
	}
	for _, identity := range identities {
		if identity.Kind != OIDCKind || identity.CredentialEpoch != epoch ||
			(oidcProvider != "" && identity.ProviderID != oidcProvider) {
			continue
		}
		ok, err := identityPassesRevealGate(ctx, az, identity)
		if err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

func identityPassesRevealGate(ctx context.Context, az *authz.TxAuthorizer, identity authz.ExternalIdentity) (bool, error) {
	provider, err := az.ProviderForCallback(ctx, identity.ProviderID)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return provider.Enabled && provider.Issuer == identity.Issuer && provider.AssurancePolicy != nil, nil
}

// admitFederatedPersonalWindow decides whether a window an identity provider
// opened may open a personal value. Only a qualifying passwordless account
// with no standing factor may use one: a factor holder must use the factor,
// so an identity-provider administrator cannot reach their values. The
// provider that opened the window must itself be one the account qualifies
// through, so a second linked provider cannot borrow that qualification.
//
// shortcut: only an OIDC window is admitted. SAML reauthentication does not
// bind the session's provider, so the session cannot name the opener, and
// reauth_windows cannot store a SAML factor class today; name the opener from
// the window's SAML transaction once such a window can exist.
func admitFederatedPersonalWindow(ctx context.Context, az *authz.TxAuthorizer, w authz.ReauthWindow, now time.Time) error {
	if w.FactorClass != OIDCKind {
		return ErrPersonalFactorRequired
	}
	session, err := az.AuthenticateSessionByID(ctx, w.SessionID, now)
	if err != nil {
		return err
	}
	account, err := az.AccountByPrincipal(ctx, session.Principal)
	if err != nil {
		return err
	}
	factors, err := enrolledFactors(ctx, az, account.ID)
	if err != nil {
		return err
	}
	if len(factors) > 0 {
		return ErrPersonalFactorRequired
	}
	// The OIDC reauthentication callback refuses any provider but the
	// session's own, so the session names the provider that opened the window.
	if session.ProviderID == "" {
		return ErrPersonalFactorRequired
	}
	ok, err := federatedQualified(ctx, az, account.ID, session.ProviderID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrPersonalFactorRequired
	}
	return nil
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

// purgePersonalIfUnqualified purges once a credential change (a factor
// removed, a password established) leaves the account unqualified.
func purgePersonalIfUnqualified(ctx context.Context, az *authz.TxAuthorizer, account authz.Account, now time.Time) error {
	ok, err := accountQualified(ctx, az, account.ID, now)
	if err != nil || ok {
		return err
	}
	return purgePersonalValues(ctx, az, account.PrincipalID, PersonalCauseQualificationLost, now)
}
