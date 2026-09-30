package authz

import (
	"context"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/jwkssource"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// txForwarded is the explicitly permitted resolver surface.
// Additions require review of the transaction authority boundary.
type txForwarded interface {
	// AccountByID resolves an account by id.
	//
	//hikyo:forward AccountByID
	AccountByID(ctx context.Context, id string) (Account, error)

	// AccountByPrincipal resolves the account a session's principal owns — the
	// bridge the factor paths need to reach an account's password/TOTP/recovery
	// rows from the principal a session carries.
	//
	//hikyo:forward AccountByPrincipal
	AccountByPrincipal(ctx context.Context, p domain.PrincipalID) (Account, error)

	// AccountByUsername resolves a login handle inside this transaction.
	//
	//hikyo:forward AccountByUsername
	AccountByUsername(ctx context.Context, username string) (Account, error)

	// AccountByWebAuthnUserHandle resolves the account a discoverable assertion names.
	//
	//hikyo:forward AccountByWebAuthnUserHandle
	AccountByWebAuthnUserHandle(ctx context.Context, handle []byte) (Account, error)

	// AccountCount answers the bootstrap path's one question. It has no network
	// route: `hikyo admin create` runs on the server's own host.
	//
	//hikyo:forward AccountCount
	AccountCount(ctx context.Context) (int64, error)

	//hikyo:forward AccountProfile
	AccountProfile(ctx context.Context, accountID string) (AccountProfile, error)

	// ActiveSAMLSPKey resolves the signing key used for AuthnRequests.
	//
	//hikyo:forward ActiveSAMLSPKey
	ActiveSAMLSPKey(ctx context.Context) (SAMLSPKey, error)

	// AddGrantOrigin attaches one origin to a grant row.
	//
	//hikyo:forward AddGrantOrigin
	AddGrantOrigin(ctx context.Context, id, grantID string, p domain.PrincipalID, o Origin, at time.Time) error

	// AdvanceGeneration invalidates every session of a principal at once. It runs
	// in the same transaction as the change that triggered it.
	//
	//hikyo:forward AdvanceGeneration
	AdvanceGeneration(ctx context.Context, p domain.PrincipalID) error

	// AdvanceRestoreEpoch performs the restore's invalidation.
	//
	//hikyo:forward AdvanceRestoreEpoch
	AdvanceRestoreEpoch(ctx context.Context, now time.Time) error

	// AdvanceTOTPStep consumes a code's step; false means it was not beyond the last.
	//
	//hikyo:forward AdvanceTOTPStep
	AdvanceTOTPStep(ctx context.Context, id string, rowVersion, step int64) (bool, error)

	// AdvanceWebAuthnSignCount writes the presented counter under a row_version CAS;
	// false means the row moved or was disabled.
	//
	//hikyo:forward AdvanceWebAuthnSignCount
	AdvanceWebAuthnSignCount(ctx context.Context, id string, rowVersion, count int64, at time.Time) (bool, error)

	//hikyo:forward AllowWorkspaceOrigin
	AllowWorkspaceOrigin(ctx context.Context, o authn.WorkspaceOrigin) error

	//hikyo:forward ApproveCLIReauthHandoff
	ApproveCLIReauthHandoff(ctx context.Context, id string, codeVerifier, windows []byte) (bool, error)

	//hikyo:forward ApproveWorkspaceHandoff
	ApproveWorkspaceHandoff(ctx context.Context, id string, codeVerifier []byte, p domain.PrincipalID, factors, factorClass string, authenticatedAt time.Time) (bool, error)

	// AssertActiveInstanceDEKVersion is the writer fence for authentication-surface
	// credential writes, which seal under the instance DEK with no tenant proof to
	// carry the proof-based fence. It refuses (domain.ErrConflict) a write whose
	// sealed instance DEK version a concurrent rotate-dek --instance has retired.
	// See the authn.Resolver method for the query semantics.
	//
	//hikyo:forward AssertActiveInstanceDEKVersion
	AssertActiveInstanceDEKVersion(ctx context.Context, version int64) error

	// AuthorityByValue resolves a presented credential-establishment authority.
	//
	//hikyo:forward CredentialAuthorityByVerifier
	AuthorityByValue(ctx context.Context, verifier []byte) (CredentialAuthority, error)

	// BindSessionToSAMLProvider records SAML provider provenance in the same
	// transaction that mints the session.
	//
	//hikyo:forward BindSessionToSAMLProvider
	BindSessionToSAMLProvider(ctx context.Context, sessionID, providerID string) (bool, error)

	// BindingsForIssuer is the delete guard's census: removing the issuer of a
	// live binding is an authorization change wearing a configuration change's
	// clothes.
	//
	//hikyo:forward BindingsForIssuer
	BindingsForIssuer(ctx context.Context, id string) (int64, error)

	//hikyo:forward CLIReauthHandoffByCode
	CLIReauthHandoffByCode(ctx context.Context, verifier []byte) (CLIReauthHandoff, error)

	//hikyo:forward CLIReauthHandoffByState
	CLIReauthHandoffByState(ctx context.Context, verifier []byte) (CLIReauthHandoff, error)

	// ClaimSAMLReplay atomically records an assertion ID. False means replay.
	//
	//hikyo:forward ClaimSAMLReplay
	ClaimSAMLReplay(ctx context.Context, replay NewSAMLReplay) (bool, error)

	//hikyo:forward ClampCredentialExpiry
	ClampCredentialExpiry(ctx context.Context, ceiling time.Time) (int64, error)

	//hikyo:forward ClampIndefiniteCredentials
	ClampIndefiniteCredentials(ctx context.Context, ceiling time.Time) (int64, error)

	// ClearPendingTOTP removes only in-progress enrolments.
	//
	//hikyo:forward DeletePendingTOTPForAccount
	ClearPendingTOTP(ctx context.Context, accountID string) error

	// ConfirmTOTP promotes and consumes a step in one CAS; false means the row
	// moved or the step was not beyond the last.
	//
	//hikyo:forward ConfirmTOTP
	ConfirmTOTP(ctx context.Context, id string, rowVersion, step int64, at time.Time) (bool, error)

	// ConfirmedTOTP resolves an account's confirmed TOTP factor.
	//
	//hikyo:forward ConfirmedTOTP
	ConfirmedTOTP(ctx context.Context, accountID string) (TOTPCredential, error)

	// ConsumeAuthority claims an authority atomically; false means it was already
	// consumed and the caller must fail closed.
	//
	//hikyo:forward ConsumeCredentialAuthority
	ConsumeAuthority(ctx context.Context, id string, at time.Time) (bool, error)

	//hikyo:forward ConsumeCLIReauthHandoff
	ConsumeCLIReauthHandoff(ctx context.Context, id string, at time.Time) (bool, error)

	// ConsumeLoginChallenge claims a login challenge atomically; false means it was
	// already consumed and the caller must fail closed.
	//
	//hikyo:forward ConsumeLoginChallenge
	ConsumeLoginChallenge(ctx context.Context, id string, at time.Time) (bool, error)

	// ConsumeOIDCTransaction claims a transaction atomically; false means it moved.
	//
	//hikyo:forward ConsumeOIDCTransaction
	ConsumeOIDCTransaction(ctx context.Context, id string, at time.Time) (bool, error)

	// ConsumeOutstandingAuthorities marks every unconsumed authority of an account
	// consumed, in the same transaction as a fresh mint or consumption.
	//
	//hikyo:forward ConsumeOutstandingAuthorities
	ConsumeOutstandingAuthorities(ctx context.Context, accountID string, at time.Time) error

	// ConsumeSAMLTransaction spends a transaction on first presentation, success
	// or failure.
	//
	//hikyo:forward ConsumeSAMLTransaction
	ConsumeSAMLTransaction(ctx context.Context, id string, at time.Time) (bool, error)

	// ConsumeSingleDecisionWindow claims a single-decision window exactly once;
	// false means it was already spent (B11 double-spend).
	//
	//hikyo:forward ConsumeSingleDecisionWindow
	ConsumeSingleDecisionWindow(ctx context.Context, id string, at time.Time) (bool, error)

	// ConsumeWebAuthnCeremony claims a ceremony atomically and stamps the credential
	// that answered it; false means it was already consumed.
	//
	//hikyo:forward ConsumeWebAuthnCeremony
	ConsumeWebAuthnCeremony(ctx context.Context, id, credentialID string, at time.Time) (bool, error)

	//hikyo:forward ConsumeWorkspaceHandoff
	ConsumeWorkspaceHandoff(ctx context.Context, id string, at time.Time) (bool, error)

	//hikyo:forward CorrectPrivacyAccount
	CorrectPrivacyAccount(ctx context.Context, account, username, displayName string) error

	// CountExternalIdentitiesForIssuer counts the links under one (kind, issuer).
	//
	//hikyo:forward CountExternalIdentitiesForIssuer
	CountExternalIdentitiesForIssuer(ctx context.Context, kind, issuer string) (int64, error)

	// CountGrantsInOrg reports how many grant rows an organization holds — the
	// read behind the per-org grant sanity cap.
	//
	//hikyo:forward CountGrantsInOrg
	CountGrantsInOrg(ctx context.Context, org string) (int64, error)

	// CountRegistrationPolicyOrgs counts the live orgs a policy minted.
	//
	//hikyo:forward CountRegistrationPolicyOrgs
	CountRegistrationPolicyOrgs(ctx context.Context, policyID string) (int64, error)

	// CreateAccessGrant writes one environment-scoped temporary grant row.
	//
	//hikyo:forward CreateAccessGrant
	CreateAccessGrant(ctx context.Context, id string, p domain.PrincipalID, g domain.Grant,
		requestID string, at, expiresAt time.Time) error

	//hikyo:forward CreateAccount
	CreateAccount(ctx context.Context, acc Account) error

	//hikyo:forward CreateCLIReauthHandoff
	CreateCLIReauthHandoff(ctx context.Context, h NewCLIReauthHandoff) error

	// CreateExternalIdentity writes a link.
	//
	//hikyo:forward CreateExternalIdentity
	CreateExternalIdentity(ctx context.Context, n NewExternalIdentity) error

	//hikyo:forward CreateFederationIssuer
	CreateFederationIssuer(ctx context.Context, iss NewFederationIssuer) error

	// CreateGrant writes one grant row. The bootstrap path uses it to expand the
	// `admin` template into separate, visible, individually revocable rows rather
	// than an implicit bundle. The general grant surface is #55's.
	//
	//hikyo:forward CreateGrant
	CreateGrant(ctx context.Context, id string, p domain.PrincipalID, g domain.Grant, at time.Time) error

	// CreateLoginChallenge writes a single-use, expiring login challenge (#760).
	//
	//hikyo:forward CreateLoginChallenge
	CreateLoginChallenge(ctx context.Context, c NewLoginChallenge) error

	//hikyo:forward CreateMachineCredential
	CreateMachineCredential(ctx context.Context, c NewCredential) error

	//hikyo:forward CreateMachinePrincipal
	CreateMachinePrincipal(ctx context.Context, id domain.PrincipalID, class domain.PrincipalClass, at time.Time) error

	// CreateOIDCTransaction writes a single-use transaction row.
	//
	//hikyo:forward CreateOIDCTransaction
	CreateOIDCTransaction(ctx context.Context, t NewOIDCTransaction) error

	// CreateProvider inserts a provider row (authorized at the chokepoint first).
	//
	//hikyo:forward CreateProvider
	CreateProvider(ctx context.Context, n NewProvider) error

	// The two ends of the provisioning connection's life: created with its binding,
	// retired by §6's state machine atomically with the structural grant it held.
	// Neither takes a class: the first hardcodes it and the second requires it.
	//
	//hikyo:forward CreateProvisioningPrincipal
	CreateProvisioningPrincipal(ctx context.Context, id domain.PrincipalID, at time.Time) error

	// CreateRegistrationPolicy inserts a policy; a second one per scope is ErrConflict.
	//
	//hikyo:forward CreateRegistrationPolicy
	CreateRegistrationPolicy(ctx context.Context, p RegistrationPolicy) error

	// CreateRegistrationSignup inserts a pending local sign-up row.
	//
	//hikyo:forward CreateRegistrationSignup
	CreateRegistrationSignup(ctx context.Context, n NewRegistrationSignup) error

	//hikyo:forward CreateRule
	CreateRule(ctx context.Context, rule domain.Rule, createdBy domain.PrincipalID, at time.Time, newItemID func() (string, error)) error

	// CreateSAMLProvider inserts a provider after instance-config authorization.
	//
	//hikyo:forward CreateSAMLProvider
	CreateSAMLProvider(ctx context.Context, provider NewSAMLProvider) error

	// CreateSAMLSPKey stores a freshly minted encrypted private key.
	//
	//hikyo:forward CreateSAMLSPKey
	CreateSAMLSPKey(ctx context.Context, key NewSAMLSPKey) error

	// CreateSAMLTransaction writes a single-use AuthnRequest transaction.
	//
	//hikyo:forward CreateSAMLTransaction
	CreateSAMLTransaction(ctx context.Context, transaction NewSAMLTransaction) error

	//hikyo:forward CreateServiceAccountAggregate
	CreateServiceAccountAggregate(ctx context.Context, sa NewServiceAccount) (ServiceAccountCreation, error)

	// CreateTOTP inserts a pending TOTP enrolment.
	//
	//hikyo:forward CreateTOTP
	CreateTOTP(ctx context.Context, c NewTOTPCredential) error

	// CreateWebAuthnCeremony writes a single-use, expiring challenge row.
	//
	//hikyo:forward CreateWebAuthnCeremony
	CreateWebAuthnCeremony(ctx context.Context, c NewWebAuthnCeremony) error

	// CreateWebAuthnCredential inserts a freshly enrolled passkey.
	//
	//hikyo:forward CreateWebAuthnCredential
	CreateWebAuthnCredential(ctx context.Context, c NewWebAuthnCredential) error

	//hikyo:forward CreateWorkspaceHandoff
	CreateWorkspaceHandoff(ctx context.Context, h authn.NewWorkspaceHandoff) error

	// CredentialEpoch reads the instance epoch.
	//
	//hikyo:forward CredentialEpoch
	CredentialEpoch(ctx context.Context) (int64, error)

	//hikyo:forward CredentialPolicy
	CredentialPolicy(ctx context.Context) (CredentialPolicy, error)

	//hikyo:forward CredentialsBeyondCeiling
	CredentialsBeyondCeiling(ctx context.Context, ceiling time.Time) ([]AffectedCredential, error)

	// DeleteAccessGrantsForRequest releases every temporary row one request wrote.
	//
	//hikyo:forward DeleteAccessGrantsForRequest
	DeleteAccessGrantsForRequest(ctx context.Context, p domain.PrincipalID, requestID string) (int64, error)

	// DeleteExpiredSAMLReplay removes replay rows after their signed validity plus
	// skew has elapsed.
	//
	//hikyo:forward DeleteExpiredSAMLReplay
	DeleteExpiredSAMLReplay(ctx context.Context, at time.Time) (int64, error)

	//hikyo:forward DeleteFederationIssuer
	DeleteFederationIssuer(ctx context.Context, id string) (bool, error)

	// DeleteGrantRow removes a grant row whose last origin was released.
	//
	//hikyo:forward DeleteGrantRow
	DeleteGrantRow(ctx context.Context, grantID string, p domain.PrincipalID) (bool, error)

	// DeleteProvider removes a provider.
	//
	//hikyo:forward DeleteProvider
	DeleteProvider(ctx context.Context, id string) error

	// DeleteRegistrationPolicy removes a policy at an expected row version.
	//
	//hikyo:forward DeleteRegistrationPolicy
	DeleteRegistrationPolicy(ctx context.Context, id string, expected int64) (bool, error)

	// DeleteRegistrationSignup deletes one pending local sign-up row.
	//
	//hikyo:forward DeleteRegistrationSignup
	DeleteRegistrationSignup(ctx context.Context, id string) (bool, error)

	// DeleteRetiringSAMLSPKey erases a retiring key.
	//
	//hikyo:forward DeleteRetiringSAMLSPKey
	DeleteRetiringSAMLSPKey(ctx context.Context, id string) (bool, error)

	//hikyo:forward DeleteRule
	DeleteRule(ctx context.Context, id string, p domain.PrincipalID) (bool, error)

	//hikyo:forward DeleteRuleItem
	DeleteRuleItem(ctx context.Context, ruleID, itemID string, p domain.PrincipalID) error

	// DeleteSAMLProvider removes a locked provider row.
	//
	//hikyo:forward DeleteSAMLProvider
	DeleteSAMLProvider(ctx context.Context, id string) error

	//hikyo:forward DeleteServiceAccountAggregate
	DeleteServiceAccountAggregate(ctx context.Context, in DeleteServiceAccountAggregateInput) (ServiceAccountDeletion, error)

	// DeleteWebAuthnCredential removes a credential (de-enrolment) under an
	// account_id predicate. False means zero rows matched — refused fail-closed.
	//
	//hikyo:forward DeleteWebAuthnCredential
	DeleteWebAuthnCredential(ctx context.Context, id, accountID string) (bool, error)

	// DisableWebAuthnCredential sets disabled_at under a CAS (the clone response);
	// false means the row moved or was already disabled.
	//
	//hikyo:forward DisableWebAuthnCredential
	DisableWebAuthnCredential(ctx context.Context, id string, rowVersion int64, at time.Time) (bool, error)

	// EnabledProviderByIssuer resolves the currently enabled provider for an issuer.
	//
	//hikyo:forward EnabledProviderByIssuer
	EnabledProviderByIssuer(ctx context.Context, kind, issuer string) (OIDCProvider, error)

	// EnabledProviderBySlug resolves an enabled provider by slug, for start.
	//
	//hikyo:forward EnabledProviderBySlug
	EnabledProviderBySlug(ctx context.Context, slug string) (OIDCProvider, error)

	// EnvironmentChainByID resolves an environment's chain from its id, so
	// LowerEffectiveWindow can build the grant-coverage predicate from an env id.
	//
	//hikyo:forward EnvironmentChainByID
	EnvironmentChainByID(ctx context.Context, envID string) (EnvironmentChain, error)

	// EnvironmentReauthSettings reads an environment's protection state and its
	// own reauthentication window, if it has one.
	//
	//hikyo:forward EnvironmentReauthSettings
	EnvironmentReauthSettings(ctx context.Context, envID string) (EnvSetting, error)

	// EnvironmentsInProject is the universe the mint and widen reachability
	// formulas range over.
	//
	//hikyo:forward EnvironmentsInProject
	EnvironmentsInProject(ctx context.Context, scope domain.Scope) ([]domain.EnvID, error)

	//hikyo:forward ErasePrivacyAccount
	ErasePrivacyAccount(ctx context.Context, account, p, username string) error

	// ExternalIdentitiesForAccount lists an account's linked identities.
	//
	//hikyo:forward ExternalIdentitiesForAccount
	ExternalIdentitiesForAccount(ctx context.Context, accountID string) ([]ExternalIdentity, error)

	// ExternalIdentityByID resolves a link by id.
	//
	//hikyo:forward ExternalIdentityByID
	ExternalIdentityByID(ctx context.Context, id string) (ExternalIdentity, error)

	// ExternalIdentityByKey resolves a byte-exact (kind, issuer, subject).
	//
	//hikyo:forward ExternalIdentityByKey
	ExternalIdentityByKey(ctx context.Context, kind, issuer, subject string) (ExternalIdentity, error)

	//hikyo:forward FederationIssuerByID
	FederationIssuerByID(ctx context.Context, id string) (FederationIssuer, error)

	// FederationIssuerByIssuer resolves a configuration by its BYTE-EXACT `iss`.
	//
	//hikyo:forward FederationIssuerByIssuer
	FederationIssuerByIssuer(ctx context.Context, issuer string) (FederationIssuer, error)

	//hikyo:forward FederationIssuers
	FederationIssuers(ctx context.Context) ([]FederationIssuer, error)

	// GetRule reads one stored rule, ungated; valid is false for a stored rule
	// that no longer validates (the caller should delete it).
	//
	//hikyo:forward GetRule
	GetRule(ctx context.Context, id string) (StoredRule, bool, error)

	// GrantLinesAtInstance lists the instance-scope membership surface.
	//
	//hikyo:forward GrantLinesAtInstance
	GrantLinesAtInstance(ctx context.Context) ([]GrantLine, error)

	// GrantLinesInOrg lists the membership surface for one org.
	//
	//hikyo:forward GrantLinesInOrg
	GrantLinesInOrg(ctx context.Context, org string) ([]GrantLine, error)

	// GrantLinesInProject lists the membership surface for one project.
	//
	//hikyo:forward GrantLinesInProject
	GrantLinesInProject(ctx context.Context, org, project string) ([]GrantLine, error)

	// GrantOriginCount reports how many origins still hold a grant row.
	//
	//hikyo:forward GrantOriginCount
	GrantOriginCount(ctx context.Context, grantID string) (int64, error)

	// GrantOriginsFor lists the origins holding one grant row.
	//
	//hikyo:forward GrantOriginsFor
	GrantOriginsFor(ctx context.Context, grantID string) ([]Origin, error)

	// GrantOriginsForPrincipal lists every origin holding every grant row of one
	// principal, read at one instant. The SCIM release algorithm (#73 §2.4) decides
	// per origin and then counts what remains per row; seeing the two tables at
	// different instants would let a row be judged against origins that had already
	// moved.
	//
	//hikyo:forward GrantOriginsForPrincipal
	GrantOriginsForPrincipal(ctx context.Context, p domain.PrincipalID) ([]GrantOriginRow, error)

	// GrantRowsForPrincipal lists a principal's grants with their row ids — the
	// dedup read every create performs under the principal-row lock.
	//
	//hikyo:forward GrantRowsForPrincipal
	GrantRowsForPrincipal(ctx context.Context, p domain.PrincipalID) ([]GrantRow, error)

	// GrantsForResetTarget reads the credential-reset target's full grant set for
	// the org-bounded test, under the row lock the reset holds.
	//
	//hikyo:forward GrantsForResetTarget
	GrantsForResetTarget(ctx context.Context, p domain.PrincipalID) ([]domain.Grant, error)

	// GrantsOf returns a principal's full grant set — the pre-state input to the
	// reachability diff.
	//
	//hikyo:forward Grants
	GrantsOf(ctx context.Context, p domain.PrincipalID) ([]domain.Grant, error)

	// GuardProviderForMint locks the pinned provider row inside a Phase-C mint tx
	// and reports whether it still matches the Phase-A snapshot; false means the
	// provider moved and the mint must refuse (A4 TOCTOU, sweep wins).
	//
	//hikyo:forward GuardProviderForMint
	GuardProviderForMint(ctx context.Context, id string, rowVersion int64, issuer string) (bool, error)

	// GuardSAMLProviderForMint proves the provider still matches the Phase-A
	// snapshot immediately before a session or reauth window is written.
	//
	//hikyo:forward GuardSAMLProviderForMint
	GuardSAMLProviderForMint(ctx context.Context, id string, rowVersion int64, entityID string) (bool, error)

	//hikyo:forward HoldRestoredPKIIssuers
	HoldRestoredPKIIssuers(ctx context.Context) error

	//hikyo:forward IndefiniteCredentials
	IndefiniteCredentials(ctx context.Context) ([]AffectedCredential, error)

	//hikyo:forward InstanceConnectionByID
	InstanceConnectionByID(ctx context.Context, id string) (authn.InstanceConnection, error)

	//hikyo:forward InstanceConnections
	InstanceConnections(ctx context.Context) ([]authn.InstanceConnection, error)

	// InstanceIdentity is this instance's own opaque id — the value a directory
	// listing carries and the one self-connection refusal compares against.
	//
	//hikyo:forward InstanceIdentity
	InstanceIdentity(ctx context.Context) (string, error)

	// InvalidateReauthWindowsForEnvironment deletes every open window on one
	// environment (the effective-window transition, B6) and returns the count.
	//
	//hikyo:forward DeleteReauthWindowsForEnvironment
	InvalidateReauthWindowsForEnvironment(ctx context.Context, environmentID string) (int64, error)

	//hikyo:forward InvalidateRestoredAdapterCredentials
	InvalidateRestoredAdapterCredentials(ctx context.Context) error

	//hikyo:forward InvalidateRestoredDynamicProviderCredentials
	InvalidateRestoredDynamicProviderCredentials(ctx context.Context) error

	// ListProviders lists every configured provider.
	//
	//hikyo:forward ListProviders
	ListProviders(ctx context.Context) ([]OIDCProvider, error)

	// ListSAMLProviders lists all configured SAML providers.
	//
	//hikyo:forward ListSAMLProviders
	ListSAMLProviders(ctx context.Context) ([]SAMLProvider, error)

	// LiveAccessGrants lists a principal's unexpired temporary grants.
	//
	//hikyo:forward LiveAccessGrants
	LiveAccessGrants(ctx context.Context, p domain.PrincipalID, now time.Time) ([]AccessGrant, error)

	//hikyo:forward LiveMachineCredentialCount
	LiveMachineCredentialCount(ctx context.Context, serviceAccountID string, epoch int64, now time.Time) (int64, error)

	//hikyo:forward LiveMachineCredentialCounts
	LiveMachineCredentialCounts(ctx context.Context, scope domain.Scope, epoch int64, now time.Time) (map[string]int64, error)

	// LockCredentialPolicy serializes a mint against a concurrent tightening.
	//
	//hikyo:forward LockCredentialPolicy
	LockCredentialPolicy(ctx context.Context) error

	//hikyo:forward LockInstanceIdentityRow
	LockInstanceIdentityRow(ctx context.Context) error

	// LockMachinePrincipal takes a service account's principal-row lock — THE SAME
	// LOCK the grant writers take — so a mint and a grant landing on that
	// principal serialize. Without it a grant can widen the account between the
	// mint's post-state check and its insert, producing a token whose authority
	// never passed the gate.
	//
	//hikyo:forward LockPrincipalRow
	LockMachinePrincipal(ctx context.Context, p domain.PrincipalID) error

	// LockProviderForDelete locks the provider row inside the delete tx so the
	// session sweep runs with the row held and a concurrent mint guard serializes
	// behind it (A14). ErrNotFound means a concurrent delete already removed it.
	//
	//hikyo:forward LockProviderForDelete
	LockProviderForDelete(ctx context.Context, id string) error

	// LockSAMLProviderForDelete serializes provider deletion against Phase-C mints.
	//
	//hikyo:forward LockSAMLProviderForDelete
	LockSAMLProviderForDelete(ctx context.Context, id string) error

	// LockTargetPrincipal takes the target principal's row lock so the org-bounded
	// test and every grant mutation serialize on the same row (B14).
	//
	//hikyo:forward LockPrincipalRow
	LockTargetPrincipal(ctx context.Context, p domain.PrincipalID) error

	// LockWorkspaceOrigin and LockInstanceIdentityRow are the two row locks the
	// serving side's read-then-write decisions serialize on under postgres' READ
	// COMMITTED semantics.
	//
	//hikyo:forward LockWorkspaceOrigin
	LockWorkspaceOrigin(ctx context.Context, origin string) (bool, error)

	// LockoutRetentions lists every `lockout-retention` origin in the instance,
	// which is what the deterministic cure sweep walks (#73 §2.4).
	//
	//hikyo:forward LockoutRetentions
	LockoutRetentions(ctx context.Context) ([]LockoutRetention, error)

	// LockoutRetentionsInOrg is the org-bounded cure sweep.
	//
	//hikyo:forward LockoutRetentionsInOrg
	LockoutRetentionsInOrg(ctx context.Context, org domain.OrgID) ([]LockoutRetention, error)

	// LoginChallengeByID resolves a login challenge, or domain.ErrNotFound.
	//
	//hikyo:forward LoginChallengeByID
	LoginChallengeByID(ctx context.Context, id string) (LoginChallenge, error)

	//hikyo:forward MachineCredentialsFor
	MachineCredentialsFor(ctx context.Context, serviceAccountID string) ([]MachineCredential, error)

	// ManageMembersHolders is the lockout invariant's census at one org, or at
	// instance scope when org is empty.
	//
	//hikyo:forward ManageMembersHolders
	ManageMembersHolders(ctx context.Context, org string) ([]domain.PrincipalID, error)

	// MarkSAMLSPKeyRetiring compare-and-swaps an active key into overlap state.
	//
	//hikyo:forward MarkSAMLSPKeyRetiring
	MarkSAMLSPKeyRetiring(ctx context.Context, id string, rowVersion int64) (bool, error)

	// MintAuthority writes a new credential-establishment authority.
	//
	//hikyo:forward CreateCredentialAuthority
	MintAuthority(ctx context.Context, n NewCredentialAuthority) error

	//hikyo:forward MintInstanceConnection
	MintInstanceConnection(ctx context.Context, n authn.NewInstanceConnection) error

	// MintSession writes a session row.
	//
	//hikyo:forward CreateSession
	MintSession(ctx context.Context, s NewSession) error

	// OIDCTransactionByState resolves a transaction by its state verifier.
	//
	//hikyo:forward OIDCTransactionByState
	OIDCTransactionByState(ctx context.Context, stateVerifier []byte) (OIDCTransaction, error)

	// OpenReauthWindow opens a reauthentication window over one environment.
	//
	//hikyo:forward CreateReauthWindow
	OpenReauthWindow(ctx context.Context, w NewReauthWindow) error

	// PasswordCredentialFor reads an account's verifier row.
	//
	//hikyo:forward PasswordCredential
	PasswordCredentialFor(ctx context.Context, accountID string) (PasswordCredential, error)

	// PendingTOTP resolves an account's in-progress enrolment.
	//
	//hikyo:forward PendingTOTP
	PendingTOTP(ctx context.Context, accountID string) (TOTPCredential, error)

	// PinGeneration reads the conditional cursor's pin component.
	//
	//hikyo:forward PinGeneration
	PinGeneration(ctx context.Context, p domain.PrincipalID, env domain.EnvID) (int64, error)

	// PrincipalClass resolves a principal's class for the normative machine
	// allowlists.
	//
	//hikyo:forward PrincipalClass
	PrincipalClass(ctx context.Context, p domain.PrincipalID) (domain.PrincipalClass, error)

	// PrincipalGeneration reads the principal's current session generation, so a
	// freshly minted session records the generation it was born under.
	//
	//hikyo:forward PrincipalGeneration
	PrincipalGeneration(ctx context.Context, p domain.PrincipalID) (int64, error)

	//hikyo:forward PrivacyAccount
	PrivacyAccount(ctx context.Context, p string) (PrivacyAccountView, error)

	//hikyo:forward PrivacyActivity
	PrivacyActivity(ctx context.Context, p string) ([]PrivacyActivity, error)

	//hikyo:forward PrivacySessions
	PrivacySessions(ctx context.Context, p string) ([]PrivacySession, error)

	// ProjectMachineReveal reads the per-project machine-reveal opt-in
	// (source-of-truth ADR). ErrNotFound for an unknown project.
	//
	//hikyo:forward ProjectMachineReveal
	ProjectMachineReveal(ctx context.Context, projectID string) (authn.MachineRevealState, error)

	// ProviderBySlug resolves a provider by slug for administration (any state).
	// The mutation that follows is authorized at the chokepoint first.
	//
	//hikyo:forward ProviderBySlug
	ProviderBySlug(ctx context.Context, slug string) (OIDCProvider, error)

	// ProviderForCallback resolves the provider a transaction pinned, by id.
	//
	//hikyo:forward ProviderForCallback
	ProviderForCallback(ctx context.Context, id string) (OIDCProvider, error)

	// ReactivateBinding records a restore-time re-validation (§ Restore). #76 owns
	// the operator ceremony; the write exists here because the refusal it drives
	// exists now.
	//
	//hikyo:forward ReactivateBinding
	ReactivateBinding(ctx context.Context, id string, at time.Time) (bool, error)

	// ReauthWindowFor resolves the window over one environment for one session.
	//
	//hikyo:forward ReauthWindowFor
	ReauthWindowFor(ctx context.Context, sessionID, environmentID string) (ReauthWindow, error)

	// RebindSAMLExternalIdentityProvider compare-and-swaps provider provenance
	// after the same byte-exact entity has been removed and configured again.
	//
	//hikyo:forward RebindSAMLExternalIdentityProvider
	RebindSAMLExternalIdentityProvider(ctx context.Context, id, expectedProviderID, newProviderID string) (bool, error)

	// ReconcilePrincipal commits ONE principal's reconciliation. The signature is
	// the guarantee: one id in, one answer out. There is no set-taking sibling of
	// this method anywhere in the module, and the drill asserts that.
	//
	//hikyo:forward ReconcilePrincipal
	ReconcilePrincipal(ctx context.Context, p domain.PrincipalID) (bool, error)

	// RecoveryCodesFor resolves an account's batch.
	//
	//hikyo:forward RecoveryCodes
	RecoveryCodesFor(ctx context.Context, accountID string) (RecoveryBatch, error)

	// RegistrationPolicyFor resolves the policy of one scope ("" = instance).
	//
	//hikyo:forward RegistrationPolicyFor
	RegistrationPolicyFor(ctx context.Context, org domain.OrgID) (RegistrationPolicy, error)

	// RegistrationSignupsForPolicy lists a policy's pending local sign-ups.
	//
	//hikyo:forward RegistrationSignupsForPolicy
	RegistrationSignupsForPolicy(ctx context.Context, policyID string) ([]RegistrationSignupRef, error)

	// ReleaseGrantOrigin releases one origin, reporting whether it held the row.
	//
	//hikyo:forward ReleaseGrantOrigin
	ReleaseGrantOrigin(ctx context.Context, grantID string, p domain.PrincipalID, o Origin) (bool, error)

	// RemoteOrigins is the CSP `connect-src` input. See the resolver's doc comment
	// for why this one read of a class=instance table is proof-free.
	//
	//hikyo:forward RemoteOrigins
	RemoteOrigins(ctx context.Context) ([]string, error)

	// RemoveExternalIdentity removes a link (unlink).
	//
	//hikyo:forward DeleteExternalIdentity
	RemoveExternalIdentity(ctx context.Context, id string) error

	// RemoveTOTPForAccount deletes every TOTP row of an account.
	//
	//hikyo:forward DeleteTOTPForAccount
	RemoveTOTPForAccount(ctx context.Context, accountID string) error

	// RemoveWorkspaceOrigin and RevokeWorkspaceSessionsForOrigin are ONE ACT in two
	// statements and must be called in one transaction. That pairing is the ADR's
	// atomic kill switch; splitting it leaves a window in which an origin is
	// de-allowlisted and its sessions still authenticate.
	//
	//hikyo:forward RemoveWorkspaceOrigin
	RemoveWorkspaceOrigin(ctx context.Context, origin string) (bool, error)

	// ReplacePasswordCredential compare-and-swaps an existing verifier. False
	// means the row moved underneath and the caller must not write a stale
	// verifier back.
	//
	//hikyo:forward UpdatePasswordCredential
	ReplacePasswordCredential(ctx context.Context, c PasswordCredential, at time.Time) (bool, error)

	// ReplaceRecoveryCodes compare-and-swaps the batch; false means it moved.
	//
	//hikyo:forward UpdateRecoveryCodes
	ReplaceRecoveryCodes(ctx context.Context, b RecoveryBatch, at time.Time) (bool, error)

	// ReplaceRegistrationPolicy compare-and-swaps a policy and replaces its children.
	//
	//hikyo:forward ReplaceRegistrationPolicy
	ReplaceRegistrationPolicy(ctx context.Context, p RegistrationPolicy, expected int64) (bool, error)

	// ResolveChain answers "is this (org, project, environment) a real chain" —
	// the same single-query resolution authorize() itself performs, exposed for the
	// one caller that must ask about a scope it is NOT currently authorizing: a
	// SCIM mapping row names a scope at AUTHORING time and expands it at every
	// sync, and a row whose environment does not belong to its project would write
	// grants against a chain that never existed.
	//
	// It mints no proof and reveals nothing a caller could not learn by addressing
	// the scope: an unresolvable chain answers domain.ErrNotFound uniformly,
	// whether the link is missing or foreign.
	//
	//hikyo:forward ResolveChain
	ResolveChain(ctx context.Context, scope domain.Scope) (domain.Scope, error)

	// RestoreState reads the instance's restore posture.
	//
	//hikyo:forward RestoreState
	RestoreState(ctx context.Context) (RestoreState, error)

	//hikyo:forward RestrictPrivacyPrincipal
	RestrictPrivacyPrincipal(ctx context.Context, p, state string) error

	// RevokeAllSessionsFor deletes every session of a principal.
	//
	//hikyo:forward DeleteSessionsForPrincipal
	RevokeAllSessionsFor(ctx context.Context, p domain.PrincipalID) error

	//hikyo:forward RevokeInstanceConnection
	RevokeInstanceConnection(ctx context.Context, id string, at time.Time) (bool, error)

	//hikyo:forward RevokeMachineCredential
	RevokeMachineCredential(ctx context.Context, serviceAccountID, id string, at time.Time) (bool, error)

	// RevokeSession deletes one session in this transaction.
	//
	//hikyo:forward DeleteSession
	RevokeSession(ctx context.Context, id string) error

	//hikyo:forward RevokeSessionForPrincipal
	RevokeSessionForPrincipal(ctx context.Context, id string, p domain.PrincipalID) (bool, error)

	//hikyo:forward RevokeWorkspaceSessionsForOrigin
	RevokeWorkspaceSessionsForOrigin(ctx context.Context, origin string) (int64, error)

	// RotateSessionFactors rotates the acting session token and rewrites its
	// factor set on step-up, preserving the original authentication attribution.
	//
	//hikyo:forward RotateSessionFactors
	RotateSessionFactors(ctx context.Context, id string, verifier []byte, factors string) error

	// SAMLProviderBySlug resolves a provider in any state for administration.
	//
	//hikyo:forward SAMLProviderBySlug
	SAMLProviderBySlug(ctx context.Context, slug string) (SAMLProvider, error)

	// SAMLProviderForCallback resolves the provider pinned by a transaction.
	//
	//hikyo:forward SAMLProviderForCallback
	SAMLProviderForCallback(ctx context.Context, id string) (SAMLProvider, error)

	// SAMLSPKeys lists active and overlap-retiring public material.
	//
	//hikyo:forward SAMLSPKeys
	SAMLSPKeys(ctx context.Context) ([]SAMLSPKey, error)

	// SAMLTransactionByRelayState resolves the opaque front-channel handle before
	// the strict wrapper's single response-validation pass.
	//
	//hikyo:forward SAMLTransactionByRelayState
	SAMLTransactionByRelayState(ctx context.Context, verifier []byte) (SAMLTransaction, error)

	//hikyo:forward SCIMCredentialByVerifier
	SCIMCredentialByVerifier(ctx context.Context, presented []byte) (SCIMCredential, error)

	// ServiceAccountAt resolves one service account within an addressed project;
	// an id from another project answers domain.ErrNotFound.
	//
	//hikyo:forward ServiceAccountAt
	ServiceAccountAt(ctx context.Context, scope domain.Scope, id string) (ServiceAccount, error)

	// ServiceAccountByPrincipal resolves the service account a machine principal
	// is, for the grant surface's subtree confinement.
	//
	//hikyo:forward ServiceAccountByPrincipal
	ServiceAccountByPrincipal(ctx context.Context, p domain.PrincipalID) (ServiceAccount, error)

	//hikyo:forward ServiceAccountsIn
	ServiceAccountsIn(ctx context.Context, scope domain.Scope) ([]ServiceAccount, error)

	// SessionsForPrincipal and RevokeSessionForPrincipal are the self-scoped
	// active-session surface (#71 criterion 5). The principal conjunct is in the
	// SQL, so one caller structurally cannot reach another's row.
	//
	//hikyo:forward SessionsForPrincipal
	SessionsForPrincipal(ctx context.Context, p domain.PrincipalID) ([]authn.SessionSummary, error)

	// SetClock fixes the instant time-bound grants are evaluated against for the
	// rest of this transaction. The service layer's authorize prelude sets it from
	// the service clock, so expiry is judged by one clock per operation. Zero
	// restores wall-clock evaluation at each lookup.
	//
	//hikyo:forward SetClock
	SetClock(now time.Time)

	//hikyo:forward SetCredentialPolicy
	SetCredentialPolicy(ctx context.Context, p CredentialPolicy, actor domain.PrincipalID, at time.Time) error

	// SetPinGeneration advances it. #52 owns pin creation, reassignment and release.
	//
	//hikyo:forward SetPinGeneration
	SetPinGeneration(ctx context.Context, p domain.PrincipalID, env domain.EnvID, generation int64) error

	// SetWebAuthnUserHandle sets the opaque handle once; false means one already
	// exists (the caller reads it back rather than rotating).
	//
	//hikyo:forward SetWebAuthnUserHandle
	SetWebAuthnUserHandle(ctx context.Context, accountID string, handle []byte) (bool, error)

	// SlideReauthWindow advances a sliding window's idle clock; false means the row
	// moved and the caller must not extend it.
	//
	//hikyo:forward SlideReauthWindow
	SlideReauthWindow(ctx context.Context, id string, windowExpires time.Time) (bool, error)

	// SlideSession advances the idle clock only. The absolute lifetime is never
	// extended by activity — two independent clocks is the design.
	//
	//hikyo:forward TouchSession
	SlideSession(ctx context.Context, id string, seen, idleExpires time.Time) error

	// StrandedRevealPrincipals enumerates the reveal-holding principals a 0
	// effective window would strand on the given environment chain (B6).
	//
	//hikyo:forward StrandedRevealPrincipals
	StrandedRevealPrincipals(ctx context.Context, org, project, env string) ([]domain.PrincipalID, error)

	//hikyo:forward SweepExpiredWorkspaceHandoffs
	SweepExpiredWorkspaceHandoffs(ctx context.Context, before time.Time) (int64, error)

	// SweepSessionsForProvider deletes every session minted through a provider and
	// returns the count for audit (A4).
	//
	//hikyo:forward DeleteSessionsForProvider
	SweepSessionsForProvider(ctx context.Context, providerID string) (int64, error)

	// SweepSessionsForSAMLProvider deletes all sessions minted through a SAML IdP.
	//
	//hikyo:forward DeleteSessionsForSAMLProvider
	SweepSessionsForSAMLProvider(ctx context.Context, providerID string) (int64, error)

	// SweepSessionsForWebAuthnCredential deletes every session a passkey login
	// minted through a credential and returns the count for audit (B9 clone sweep).
	//
	//hikyo:forward DeleteSessionsForWebAuthnCredential
	SweepSessionsForWebAuthnCredential(ctx context.Context, credentialID string) (int64, error)

	//hikyo:forward TouchInstanceConnection
	TouchInstanceConnection(ctx context.Context, id string, at time.Time) error

	//hikyo:forward TouchMachineCredential
	TouchMachineCredential(ctx context.Context, id string, at time.Time) error

	//hikyo:forward TouchSCIMCredential
	TouchSCIMCredential(ctx context.Context, id string, at time.Time) error

	// UnreconciledPrincipals lists who is still inert.
	//
	//hikyo:forward UnreconciledPrincipals
	UnreconciledPrincipals(ctx context.Context) ([]PrincipalRef, error)

	//hikyo:forward UpdateAccountProfile
	UpdateAccountProfile(ctx context.Context, accountID string, profile AccountProfile) error

	//hikyo:forward UpdateFederationIssuer
	UpdateFederationIssuer(ctx context.Context, id string, source jwkssource.KeySource, refused []string, caBundle string, actor domain.PrincipalID, at time.Time) (bool, error)

	// UpdateProvider compare-and-swaps a provider; false means the row moved.
	//
	//hikyo:forward UpdateProvider
	UpdateProvider(ctx context.Context, u ProviderUpdate) (bool, error)

	// UpdateSAMLProvider compare-and-swaps a provider configuration.
	//
	//hikyo:forward UpdateSAMLProvider
	UpdateSAMLProvider(ctx context.Context, provider SAMLProviderUpdate) (bool, error)

	// WebAuthnCeremonyByChallenge resolves a ceremony by its challenge verifier.
	//
	//hikyo:forward WebAuthnCeremonyByChallenge
	WebAuthnCeremonyByChallenge(ctx context.Context, challengeVerifier []byte) (WebAuthnCeremony, error)

	// WebAuthnCeremonyByID resolves a ceremony by id, for single-decision window
	// unit matching at disclosure.
	//
	//hikyo:forward WebAuthnCeremonyByID
	WebAuthnCeremonyByID(ctx context.Context, id string) (WebAuthnCeremony, error)

	// WebAuthnCredentialByCredentialID resolves the row a passkey assertion names.
	//
	//hikyo:forward WebAuthnCredentialByCredentialID
	WebAuthnCredentialByCredentialID(ctx context.Context, credentialID []byte) (WebAuthnCredential, error)

	// WebAuthnCredentialByID resolves a credential by its surrogate id.
	//
	//hikyo:forward WebAuthnCredentialByID
	WebAuthnCredentialByID(ctx context.Context, id string) (WebAuthnCredential, error)

	// WebAuthnCredentialsForAccount lists an account's passkeys.
	//
	//hikyo:forward WebAuthnCredentialsForAccount
	WebAuthnCredentialsForAccount(ctx context.Context, accountID string) ([]WebAuthnCredential, error)

	// WebAuthnUserHandle reads an account's opaque handle, or nil when unset.
	//
	//hikyo:forward WebAuthnUserHandle
	WebAuthnUserHandle(ctx context.Context, accountID string) ([]byte, error)

	// WorkloadPinState reads the conditional reveal-history admission fact.
	//
	//hikyo:forward WorkloadPinState
	WorkloadPinState(ctx context.Context, p domain.PrincipalID, env domain.EnvID) (WorkloadPinState, error)

	//hikyo:forward WorkspaceHandoffByCode
	WorkspaceHandoffByCode(ctx context.Context, verifier []byte) (authn.WorkspaceHandoff, error)

	//hikyo:forward WorkspaceHandoffByState
	WorkspaceHandoffByState(ctx context.Context, verifier []byte) (authn.WorkspaceHandoff, error)

	//hikyo:forward WorkspaceOriginAllowed
	WorkspaceOriginAllowed(ctx context.Context, origin string) (bool, error)

	//hikyo:forward WorkspaceOrigins
	WorkspaceOrigins(ctx context.Context) ([]authn.WorkspaceOrigin, error)

	// WritePasswordCredential inserts the first verifier for an account.
	//
	//hikyo:forward CreatePasswordCredential
	WritePasswordCredential(ctx context.Context, c PasswordCredential, at time.Time) error

	// WriteRecoveryCodes writes the first batch for an account.
	//
	//hikyo:forward CreateRecoveryCodes
	WriteRecoveryCodes(ctx context.Context, b RecoveryBatch, at time.Time) error
}
