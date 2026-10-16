package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

const CredentialEstablishLifetime = 5 * time.Minute

// verifyLocalCreationProof extends account-security proof only for the four
// local-credential creation operations. Disclosure, links and removals never
// receive this evidence. A supplied proof always takes the ordinary path.
func (s *Auth) verifyLocalCreationProof(ctx context.Context, presented, password, code string) (authz.Account, ReauthEvidence, error) {
	account, evidence, err := s.verifyAccountSecurityProof(ctx, presented, password, code)
	if !errors.Is(err, ErrNoProofCredential) || password != "" || code != "" {
		return account, evidence, err
	}
	err = tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		live, err := az.Authenticate(ctx, presented, s.now())
		if err != nil {
			return err
		}
		account, err = az.AccountByPrincipal(ctx, live.Principal)
		if err != nil {
			return err
		}
		if err := s.checkEstablish(ctx, az, live, account.ID); err != nil {
			return err
		}
		evidence = ReauthEvidence{Principal: live.Principal, kind: reauthEvidenceEstablish, sessionID: live.SessionID}
		return nil
	})
	return account, evidence, err
}

func (s *Auth) checkEstablish(ctx context.Context, az *authz.TxAuthorizer, live authz.Identity, accountID string) error {
	local, err := hasLocalProof(ctx, az, accountID)
	if err != nil {
		return err
	}
	if local {
		return ErrNoProofCredential
	}
	stamp, err := az.CredentialEstablish(ctx, live.SessionID, live.Principal, s.now())
	if errors.Is(err, domain.ErrNotFound) {
		return ErrNoProofCredential
	}
	if err != nil {
		return err
	}
	return s.establishProviderLive(ctx, az, stamp, false)
}

// consumeLocalCreationProof binds the stamp to this exact acting session and
// locks the principal before re-reading the no-local-proof predicate. The
// mutation and reissue share this transaction, including the stamp's deletion.
func (s *Auth) consumeLocalCreationProof(ctx context.Context, az *authz.TxAuthorizer, evidence ReauthEvidence, live authz.Identity, accountID string) error {
	if evidence.kind != reauthEvidenceEstablish {
		return s.ConsumeReauthEvidence(ctx, az, evidence, live.Principal)
	}
	if evidence.Principal != live.Principal || evidence.sessionID != live.SessionID {
		return domain.ErrUnauthenticated
	}
	stamp, err := az.CredentialEstablish(ctx, live.SessionID, live.Principal, s.now())
	if errors.Is(err, domain.ErrNotFound) {
		return ErrNoProofCredential
	}
	if err != nil {
		return err
	}
	if err := s.establishProviderLive(ctx, az, stamp, true); err != nil {
		return err
	}
	if err := az.LockTargetPrincipal(ctx, live.Principal); err != nil {
		return err
	}
	return s.checkEstablish(ctx, az, live, accountID)
}

// assertLoginCredential evaluates the post-unlink state. TOTP never starts a
// login. A passkey set counts only with its discoverable and recovery floor.
func (s *Auth) assertLoginCredential(ctx context.Context, az *authz.TxAuthorizer, accountID, excluding string) error {
	epoch, err := az.CredentialEpoch(ctx)
	if err != nil {
		return err
	}
	password, err := az.PasswordCredentialFor(ctx, accountID)
	if err == nil && password.CredentialEpoch == epoch {
		return nil
	}
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if usable, err := s.hasUsableExternalIdentity(ctx, az, accountID, excluding, epoch); err != nil {
		return err
	} else if usable {
		return nil
	}
	if err := s.assertPasskeyFloor(ctx, az, accountID); errors.Is(err, ErrPasskeyOnlyViolation) {
		return ErrLastCredential
	} else {
		return err
	}
}

// The establish proof is an external identity, not a new authentication factor.
// Reissue strips every window and records only that identity's single factor.
func (s *Auth) reissueAccountSecurity(ctx context.Context, az *authz.TxAuthorizer, account authz.Account, proofClass string, live authz.Identity, now time.Time) (LoginResult, error) {
	if proofClass != "establish" {
		return s.reissueSession(ctx, az, account, proofClass, MethodLocalPassword, Artifact(live.Artifact), now)
	}
	stamp, err := az.CredentialEstablish(ctx, live.SessionID, live.Principal, now)
	if err != nil {
		return LoginResult{}, err
	}
	identity, err := az.ExternalIdentityByID(ctx, stamp.IdentityID)
	if err != nil {
		return LoginResult{}, err
	}
	if identity.AccountID != account.ID || (identity.Kind != OIDCKind && identity.Kind != OAuth2Kind) {
		return LoginResult{}, domain.ErrUnauthenticated
	}
	providerID := ""
	if identity.Kind == OIDCKind {
		providerID = identity.ProviderID
	}
	result, err := s.reissueSessionWithProvider(ctx, az, account, identity.Kind, identity.Kind+":"+identity.Issuer, Artifact(live.Artifact), providerID, now)
	if err != nil {
		return LoginResult{}, err
	}
	var bound bool
	if identity.Kind == OIDCKind {
		bound = true
	} else {
		bound, err = az.BindSessionToOAuth2Provider(ctx, result.SessionID, identity.ProviderID)
	}
	if err != nil {
		return LoginResult{}, err
	}
	if !bound {
		return LoginResult{}, domain.ErrUnauthenticated
	}
	return result, nil
}

// Older transactions recorded only a password ceremony ID. New transactions
// retain that proof's class for audit and session reissue without storing it.
func linkProofClass(ceremonyID string) string {
	if strings.HasPrefix(ceremonyID, "totp:") {
		return "totp"
	}
	return "password"
}

type federatedEstablish struct {
	kind, providerID, issuer, subject, sessionID, accountID string
	epoch                                                   int64
	revalidate                                              func(context.Context, *authz.TxAuthorizer) (string, error)
	refuse                                                  func(context.Context, *authz.TxAuthorizer, string) error
}

func (s *Auth) completeFederatedEstablish(ctx context.Context, presented string, f federatedEstablish) (OIDCCallbackResult, error) {
	attempt, err := writeCommittedSessionAttempt(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer, a *sessionCompletionAttempt) error {
		refuse := func(cause string) error { a.refused = sessionRefusedUnauthenticated; return f.refuse(ctx, az, cause) }
		cause, err := f.revalidate(ctx, az)
		if err != nil {
			return err
		}
		if cause != "" {
			return refuse(cause)
		}
		live, err := az.Authenticate(ctx, presented, s.now())
		if err != nil || live.SessionID != f.sessionID {
			return refuse(causeBinding)
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		if epoch != f.epoch {
			return refuse(causeEpoch)
		}
		identity, err := az.ExternalIdentityByKey(ctx, f.kind, f.issuer, f.subject)
		if errors.Is(err, domain.ErrNotFound) {
			return refuse(causeUnknownIdentity)
		}
		if err != nil {
			return err
		}
		account, err := az.AccountByID(ctx, f.accountID)
		if err != nil {
			return err
		}
		if identity.AccountID != account.ID || account.PrincipalID != live.Principal {
			return refuse(causeUnknownIdentity)
		}
		if identity.CredentialEpoch != epoch {
			return refuse(causeEpoch)
		}
		if identity.ProviderID != f.providerID {
			return refuse(causeReconciliation)
		}
		if err := az.LockTargetPrincipal(ctx, live.Principal); err != nil {
			return err
		}
		local, err := hasLocalProof(ctx, az, account.ID)
		if err != nil {
			return err
		}
		if local {
			return refuse(causePurpose)
		}
		a.result, err = s.completeSession(ctx, az, RotateSession{session: live, account: account, factors: live.Assurance.Factors}, s.now())
		if err != nil {
			return err
		}
		if err := az.StampCredentialEstablish(ctx, live.SessionID, identity.ID, s.now().Add(CredentialEstablishLifetime)); err != nil {
			return err
		}
		event, err := newAuditEvent(ctx, audit.EventCredentialEstablish, account.PrincipalID, audit.Object{Type: "session", ID: live.SessionID}, audit.OutcomeSuccess, "", audit.Payload{"account_id": account.ID, "identity_id": identity.ID, "provider_id": f.providerID, "kind": f.kind, "purpose": "account-security"})
		if err != nil {
			return err
		}
		return az.RecordAuthEvent(ctx, event)
	})
	if err != nil {
		return OIDCCallbackResult{}, err
	}
	return OIDCCallbackResult{Login: attempt.result}, attempt.refusal()
}

// The stamp's identity can differ from the session's original provider. Lock
// that provider before credential writes so disable/reconfigure serializes with
// spending the stamp, including while a concurrent provider update is in flight.
func (s *Auth) establishProviderLive(ctx context.Context, az *authz.TxAuthorizer, stamp authz.CredentialEstablishEvidence, guard bool) error {
	identity, err := az.ExternalIdentityByID(ctx, stamp.IdentityID)
	if err != nil {
		return err
	}
	switch identity.Kind {
	case OIDCKind:
		p, err := az.ProviderForCallback(ctx, identity.ProviderID)
		if err != nil {
			return err
		}
		if !p.Enabled {
			return ErrNoProofCredential
		}
		if guard {
			cause, err := s.revalidateProvider(ctx, az, p)
			if err != nil {
				return err
			}
			if cause != "" {
				return ErrNoProofCredential
			}
		}
	case OAuth2Kind:
		p, err := az.OAuth2ProviderForCallback(ctx, identity.ProviderID)
		if err != nil {
			return err
		}
		if !p.Enabled {
			return ErrNoProofCredential
		}
		if guard {
			cause, err := s.revalidateOAuth2Provider(ctx, az, p)
			if err != nil {
				return err
			}
			if cause != "" {
				return ErrNoProofCredential
			}
		}
	default:
		return ErrNoProofCredential
	}
	return nil
}

type localProofState struct{ password, totp, passkey bool }

// Restored credentials remain inert until the current epoch admits them. The
// same inventory drives establishment preconditions, refusal copy and the CLI.
func localProofInventory(ctx context.Context, az *authz.TxAuthorizer, accountID string) (localProofState, error) {
	var out localProofState
	epoch, err := az.CredentialEpoch(ctx)
	if err != nil {
		return out, err
	}
	password, err := az.PasswordCredentialFor(ctx, accountID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return out, err
	}
	out.password = err == nil && password.CredentialEpoch == epoch
	totp, err := az.ConfirmedTOTP(ctx, accountID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return out, err
	}
	out.totp = err == nil && totp.CredentialEpoch == epoch
	keys, err := az.WebAuthnCredentialsForAccount(ctx, accountID)
	if err != nil {
		return out, err
	}
	for _, key := range keys {
		if !key.Disabled && key.CredentialEpoch == epoch {
			out.passkey = true
		}
	}
	return out, nil
}

// hasUsableExternalIdentity mirrors the static eligibility checks at login. A
// retained identity row is not a login path while its provider is disabled,
// missing or no longer matches its issuer. Network availability is not tested.
func (s *Auth) hasUsableExternalIdentity(ctx context.Context, az *authz.TxAuthorizer, accountID, excluding string, epoch int64) (bool, error) {
	identities, err := az.ExternalIdentitiesForAccount(ctx, accountID)
	if err != nil {
		return false, err
	}
	for _, identity := range identities {
		if identity.ID == excluding || identity.CredentialEpoch != epoch {
			continue
		}
		usable, err := s.externalIdentityUsableForLogin(ctx, az, identity)
		if err != nil {
			return false, err
		}
		if usable {
			return true, nil
		}
	}
	return false, nil
}

func (s *Auth) externalIdentityUsableForLogin(ctx context.Context, az *authz.TxAuthorizer, identity authz.ExternalIdentity) (bool, error) {
	switch identity.Kind {
	case OIDCKind:
		provider, err := az.ProviderForCallback(ctx, identity.ProviderID)
		if errors.Is(err, domain.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return provider.Enabled && provider.Issuer == identity.Issuer, nil
	case OAuth2Kind:
		provider, err := az.OAuth2ProviderForCallback(ctx, identity.ProviderID)
		if errors.Is(err, domain.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return provider.Enabled && provider.Issuer == identity.Issuer, nil
	case SAMLKind:
		// SAML login can rebind a retained identity to a recreated provider of the
		// same entityID. Requiring its historical provider ID would reject that
		// supported login path (completeSAMLLogin).
		providers, err := az.ListSAMLProviders(ctx)
		if err != nil {
			return false, err
		}
		for _, provider := range providers {
			if provider.Enabled && provider.EntityID == identity.Issuer && (provider.MetadataValidUntil == nil || s.now().Before(*provider.MetadataValidUntil)) && samlStoredSubjectAllowed(identity.Subject, provider.AllowEmailNameID) {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, nil
	}
}
