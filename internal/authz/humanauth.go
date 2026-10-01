package authz

import (
	"context"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// The in-transaction human-authentication surface.
//
// It hangs off TxAuthorizer rather than off a package the service layer could
// import directly, and that is the whole point: the resolution surface stays
// importable by exactly {authz, tx}, the service layer reaches it only inside
// a transaction it already holds, and authentication therefore cannot happen
// anywhere the authorization chokepoint is not already standing.
//
// These methods are deliberately thin. Policy — liveness, epoch, generation,
// uniform refusal — lives in session.go and in the service; storage lives in
// internal/store/authn. This file is the seam, and a seam that starts making
// decisions is how two places end up disagreeing about who is logged in.

// Account is a resolved human account.
type Account = authn.Account

// PasswordCredential is a resolved verifier row with its CAS version.
type PasswordCredential = authn.PasswordCredential

// KDFParams are the Argon2id parameters recorded with a verifier.
type KDFParams = authn.KDFParams

// CredentialAuthority is a resolved credential-establishment authority.
type CredentialAuthority = authn.CredentialAuthority

// NewCredentialAuthority is the mint carrier.
type NewCredentialAuthority = authn.NewCredentialAuthority

// NewSession is the session-mint carrier.
type NewSession = authn.NewSession

// LoginChallenge is a resolved login challenge (#760).
type LoginChallenge = authn.LoginChallenge

// NewLoginChallenge is the login-challenge insert carrier.
type NewLoginChallenge = authn.NewLoginChallenge

// CreateHumanPrincipal and CreateAccount are the bootstrap path's writes,
// reachable only from `hikyo admin create` on the server's own host — the
// closed local-authority exception set's boot/bootstrap member. There is no
// HTTP route to either, and the classification-totality invariant is what
// keeps that true.
func (a *TxAuthorizer) CreateHumanPrincipal(ctx context.Context, id domain.PrincipalID, at time.Time) error {
	return a.r.CreatePrincipal(ctx, id, "human", at)
}

// Factor seam (#54). TOTP, recovery codes and step-up rotation reach the
// resolution surface through the same in-transaction authorizer, for the same
// reason the login writers do: they mutate the artifacts that decide how a
// caller authenticated, which is resolution rather than authorization.

// TOTPCredential is a resolved TOTP factor.
type TOTPCredential = authn.TOTPCredential

// NewTOTPCredential is the TOTP insert carrier.
type NewTOTPCredential = authn.NewTOTPCredential

// RecoveryBatch is a resolved recovery-code batch.
type RecoveryBatch = authn.RecoveryBatch

// OIDC seam (#54). Login, callback, link and reauth reach the resolution
// surface through the same in-transaction authorizer as the login writers: they
// mutate the artifacts that decide who a caller is, which is resolution rather
// than authorization. Provider administration is proof-bound and does NOT come
// through here.

// OIDCProvider is a resolved provider row.
type OIDCProvider = authn.OIDCProvider

// OIDCTransaction is a resolved transaction row.
type OIDCTransaction = authn.OIDCTransaction

// NewOIDCTransaction is the transaction insert carrier.
type NewOIDCTransaction = authn.NewOIDCTransaction

// ExternalIdentity is a resolved linked identity.
type ExternalIdentity = authn.ExternalIdentity

// NewExternalIdentity is the link insert carrier.
type NewExternalIdentity = authn.NewExternalIdentity

// NewReauthWindow is the reauth-window insert carrier.
type NewReauthWindow = authn.NewReauthWindow
type CLIReauthHandoff = authn.CLIReauthHandoff
type NewCLIReauthHandoff = authn.NewCLIReauthHandoff

// NewProvider is the provider create carrier.
type NewProvider = authn.NewProvider

// ProviderUpdate is the provider reconfigure carrier.
type ProviderUpdate = authn.ProviderUpdate

// ReauthWindow is a resolved reauthentication-window row.
type ReauthWindow = authn.ReauthWindow

// SAML seam (#72). The strict XML/signature policy lives in internal/samlsp;
// this surface only keeps its durable resolution and write phase inside the
// transaction that will mint or rotate a session.

// SAMLProvider is a resolved SAML identity-provider row.
type SAMLProvider = authn.SAMLProvider

// NewSAMLProvider is the provider insert carrier.
type NewSAMLProvider = authn.NewSAMLProvider

// SAMLProviderUpdate is the provider compare-and-swap carrier.
type SAMLProviderUpdate = authn.SAMLProviderUpdate

// SAMLTransaction is a server-side AuthnRequest transaction.
type SAMLTransaction = authn.SAMLTransaction

// NewSAMLTransaction is the transaction insert carrier.
type NewSAMLTransaction = authn.NewSAMLTransaction

// NewSAMLReplay is the durable assertion replay insert carrier.
type NewSAMLReplay = authn.NewSAMLReplay

// SAMLSPKey is stored SP signing material.
type SAMLSPKey = authn.SAMLSPKey

// NewSAMLSPKey is the SP signing-key insert carrier.
type NewSAMLSPKey = authn.NewSAMLSPKey

// OrgIdentity re-exports the resolution surface's navigation record so
// internal/service can name it without importing internal/store/authn, which
// the boundary test forbids.
type OrgIdentity = authn.OrgIdentity

// OrgsForPrincipal projects the caller's OWN grants onto the organisations
// they name. It authorizes nothing and needs no proof: the result set is
// defined by the caller's own grant rows, so it can disclose nothing they do
// not already hold. See the resolver for why an instance-scoped principal
// correctly gets an empty set. Protected instance configuration additionally
// requires the caller's current MFA assurance before its org enters navigation.
func (a *TxAuthorizer) OrgsForPrincipal(ctx context.Context, caller Identity) ([]OrgIdentity, error) {
	return a.r.OrgsForPrincipal(ctx, caller.Principal, selfConfigSessionEligible(caller))
}

// EnvironmentChain is a resolved (org, project, env) chain.
type EnvironmentChain = authn.EnvironmentChain

// WebAuthn seam (#54). Passkey enrolment, discoverable login, step-up, reauth
// and removal reach the resolution surface through the same in-transaction
// authorizer as the OIDC and factor writers: they mutate the artifacts that
// decide who a caller is and how strongly they authenticated.

// WebAuthnCredential is a resolved registered passkey.
type WebAuthnCredential = authn.WebAuthnCredential

// NewWebAuthnCredential is the passkey insert carrier.
type NewWebAuthnCredential = authn.NewWebAuthnCredential

// WebAuthnCeremony is a resolved ceremony row.
type WebAuthnCeremony = authn.WebAuthnCeremony

// NewWebAuthnCeremony is the ceremony insert carrier.
type NewWebAuthnCeremony = authn.NewWebAuthnCeremony

// RecordAuthEvent writes an authentication audit event through the resolution
// surface's proof-free path. Authentication events cannot carry a proof: they
// are what produces the principal a proof would be minted for, and credential
// establishment deliberately produces no session at all.
//
// The event commits with the transaction that caused it, so a login without
// its durable record does not complete — the same durability discipline
// domain writes follow.
func (a *TxAuthorizer) RecordAuthEvent(ctx context.Context, e audit.Event) error {
	return a.r.WriteAuthEvent(ctx, e, audit.TrailInstance)
}
