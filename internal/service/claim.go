package service

import (
	"context"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Invitation claim by an external identity (#610; #582 as amended, spec
// social-signin.md section 4 "Claim"). The establish page presents a
// credential-establishment authority as the proof of a purpose-claim start on
// either federated kind; the callback spends it, binds the identity to the
// pre-created account and mints the session that identity's login would
// have minted, all in one write transaction. Both kinds share this core: the
// kind owns its provider guard, its refusal event and its session mint.

// errClaimIdentityRace is the UNIQUE (kind, issuer, subject) key refusing the
// claimed identity after the in-transaction lookup missed it: a concurrent
// bind won. The insert aborted the transaction, the authority's consumption
// rolled back with it, and the refusal is recorded in a fresh transaction.
var errClaimIdentityRace = errors.New("service: claimed identity lost the uniqueness race")

// authorityCause is the live-row half of EstablishCredential's phase-1
// checks, plus the one claim-only cause: an authority issued by recovery-code
// consumption never claims (#589 section 14.4), so the recovery => password
// rule of the credential-establishment authority stands. Empty means usable.
func authorityCause(a authz.CredentialAuthority, epoch int64, now time.Time) string {
	switch {
	case a.Consumed:
		return "consumed"
	case !now.Before(a.ExpiresAt):
		return "expired"
	case a.CredentialEpoch != epoch:
		return "epoch-superseded"
	case a.Purpose != "establish-credential" || a.IssuedBy == "recovery":
		return "purpose"
	}
	return ""
}

// holdsAnyCredential reports whether the account holds a credential of any
// kind: a password, a confirmed TOTP, any passkey row or a bound external
// identity. A claim is accepted only while it holds none. A disabled passkey
// (a clone response, say) proves nothing, so hasLocalProof skips it for
// `establish`; here it still counts, because a claim spends an authority on
// the account's FIRST credential and an account that ever enrolled one is
// past that point (fail closed).
func holdsAnyCredential(ctx context.Context, az *authz.TxAuthorizer, accountID string) (bool, error) {
	local, err := hasLocalProof(ctx, az, accountID)
	if err != nil || local {
		return local, err
	}
	keys, err := az.WebAuthnCredentialsForAccount(ctx, accountID)
	if err != nil {
		return false, err
	}
	if len(keys) > 0 {
		return true, nil
	}
	ids, err := az.ExternalIdentitiesForAccount(ctx, accountID)
	if err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}

// claimAuthority is phase 1 of a purpose-claim start: EstablishCredential's
// checks run without consuming anything, plus the claim preconditions (not
// recovery-issued, no credential of any kind). A non-empty cause is a refusal
// the caller records as auth.credential_authority_refused, once outside this
// read transaction, before any transaction row or round-trip; err is a fault.
func (s *Auth) claimAuthority(ctx context.Context, az *authz.TxAuthorizer, proof string, epoch int64) (authz.CredentialAuthority, authz.Account, string, error) {
	if crypto.ParseArtifact(proof, crypto.ArtifactBootstrap) != nil {
		return authz.CredentialAuthority{}, authz.Account{}, "malformed", nil
	}
	authority, err := az.AuthorityByValue(ctx, crypto.ArtifactVerifier(proof))
	if errors.Is(err, domain.ErrNotFound) {
		return authz.CredentialAuthority{}, authz.Account{}, "unknown", nil
	}
	if err != nil {
		return authz.CredentialAuthority{}, authz.Account{}, "", err
	}
	if cause := authorityCause(authority, epoch, s.now()); cause != "" {
		return authz.CredentialAuthority{}, authz.Account{}, cause, nil
	}
	account, err := az.AccountByID(ctx, authority.AccountID)
	if err != nil {
		return authz.CredentialAuthority{}, authz.Account{}, "", err
	}
	held, err := holdsAnyCredential(ctx, az, account.ID)
	if err != nil {
		return authz.CredentialAuthority{}, authz.Account{}, "", err
	}
	if held {
		return authz.CredentialAuthority{}, authz.Account{}, causePurpose, nil
	}
	return authority, account, "", nil
}

// federatedClaim is the per-kind half of a claim callback.
type federatedClaim struct {
	kind        string // OIDCKind or OAuth2Kind; also the established credential kind
	providerID  string
	issuer      string
	subject     string
	epoch       int64 // the transaction's credential epoch
	authorityID string
	accountID   string
	// revalidate is the kind's provider guard; a non-empty cause refuses.
	revalidate func(context.Context, *authz.TxAuthorizer) (string, error)
	// refuse stages auth.<kind>_refused {cause, purpose: claim}.
	refuse func(context.Context, *authz.TxAuthorizer, string) error
	// mint creates the session the identity's login would have minted (no
	// reauth window) and records auth.<kind>_login {purpose: claim}.
	mint func(context.Context, *authz.TxAuthorizer, authz.Account, time.Time) (LoginResult, error)
}

// completeClaim is the claim callback's one write transaction: provider
// guard, epoch, the account lock that serializes every claim of the account,
// the live authority re-check (auth.credential_authority_refused by cause, so
// the loser of two concurrent claims records `consumed`), the precondition
// "no credential of any kind" (`purpose`), the identity uniqueness check
// (`identity-exists`, authority NOT consumed), the consume CAS, the identity
// row, auth.credential_established and the session. Every refusal answers the
// uniform domain.ErrUnauthenticated with its event committed.
func (s *Auth) completeClaim(ctx context.Context, c federatedClaim) (LoginResult, error) {
	attempt, err := writeCommittedSessionAttempt(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer, a *sessionCompletionAttempt) error {
		now := s.now()
		refuse := func(cause string) error {
			a.refused = sessionRefusedUnauthenticated
			return c.refuse(ctx, az, cause)
		}
		refuseAuthority := func(cause string) error {
			a.refused = sessionRefusedUnauthenticated
			return s.refuseAuthorityIn(ctx, az, cause)
		}
		if cause, err := c.revalidate(ctx, az); err != nil {
			return err
		} else if cause != "" {
			return refuse(cause)
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		if epoch != c.epoch {
			return refuse(causeEpoch)
		}
		account, err := az.AccountByID(ctx, c.accountID)
		if err != nil {
			return err
		}
		// Lock BEFORE reading the authority: a concurrent claim of the same
		// account waits here and then reads the winner's committed consumption.
		if err := az.LockTargetPrincipal(ctx, account.PrincipalID); err != nil {
			return err
		}
		authority, err := az.CredentialAuthorityByID(ctx, c.authorityID)
		if errors.Is(err, domain.ErrNotFound) {
			return refuseAuthority("unknown")
		}
		if err != nil {
			return err
		}
		if authority.AccountID != account.ID {
			return refuseAuthority("unknown")
		}
		if cause := authorityCause(authority, epoch, now); cause != "" {
			return refuseAuthority(cause)
		}
		held, err := holdsAnyCredential(ctx, az, account.ID)
		if err != nil {
			return err
		}
		if held {
			return refuse(causePurpose)
		}
		_, err = az.ExternalIdentityByKey(ctx, c.kind, c.issuer, c.subject)
		if err == nil {
			return refuse(causeIdentityExists)
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		claimed, err := az.ClaimFederatedAuthority(ctx, authority.ID, c.kind, now)
		if err != nil {
			return err
		}
		if !claimed {
			return refuseAuthority("consumed")
		}
		identityID, err := newID("eid")
		if err != nil {
			return err
		}
		if err := az.CreateExternalIdentity(ctx, authz.NewExternalIdentity{
			ID: identityID, AccountID: account.ID, Kind: c.kind, Issuer: c.issuer, Subject: c.subject,
			ProviderID: c.providerID, CredentialEpoch: epoch, CreatedAt: now,
		}); err != nil {
			if isUniquenessRace(err) {
				return errClaimIdentityRace
			}
			return err
		}
		ev, err := newAuditEvent(ctx, audit.EventAuthCredentialEstablished, account.PrincipalID,
			audit.Object{Type: "account", ID: account.ID}, audit.OutcomeSuccess, "",
			audit.Payload{
				"authority_id": authority.ID, "account_id": account.ID, "credential": c.kind,
				"established_credential_kind": c.kind, "identity_id": identityID,
				"provider_id": c.providerID, "kind": c.kind,
			})
		if err != nil {
			return err
		}
		if err := az.RecordAuthEvent(ctx, ev); err != nil {
			return err
		}
		a.result, err = c.mint(ctx, az, account, now)
		return err
	})
	if errors.Is(err, errClaimIdentityRace) {
		if werr := tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			return c.refuse(ctx, az, causeIdentityExists)
		}); werr != nil {
			return LoginResult{}, werr
		}
		return LoginResult{}, domain.ErrUnauthenticated
	}
	if err != nil {
		return LoginResult{}, err
	}
	if refused := attempt.refusal(); refused != nil {
		return LoginResult{}, refused
	}
	return attempt.result, nil
}
