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
	AccountByID(ctx context.Context, id string) (Account, error)

	// AccountByPrincipal resolves the account a session's principal owns — the
	// bridge the factor paths need to reach an account's password/TOTP/recovery
	// rows from the principal a session carries.
	AccountByPrincipal(ctx context.Context, p domain.PrincipalID) (Account, error)

	// AccountByUsername resolves a login handle inside this transaction.
	AccountByUsername(ctx context.Context, username string) (Account, error)

	// AccountByWebAuthnUserHandle resolves the account a discoverable assertion names.
	AccountByWebAuthnUserHandle(ctx context.Context, handle []byte) (Account, error)

	// AccountCount answers the bootstrap path's one question. It has no network
	// route: `hikyo admin create` runs on the server's own host.
	AccountCount(ctx context.Context) (int64, error)
	AccountProfile(ctx context.Context, accountID string) (AccountProfile, error)

	// ActiveSAMLSPKey resolves the signing key used for AuthnRequests.
	ActiveSAMLSPKey(ctx context.Context) (SAMLSPKey, error)

	// AddGrantOrigin attaches one origin to a grant row.
	AddGrantOrigin(ctx context.Context, id, grantID string, p domain.PrincipalID, o Origin, at time.Time) error

	// AdvanceGeneration invalidates every session of a principal at once. It runs
	// in the same transaction as the change that triggered it.
	AdvanceGeneration(ctx context.Context, p domain.PrincipalID) error

	// AdvanceRestoreEpoch performs the restore's invalidation.
	AdvanceRestoreEpoch(ctx context.Context, now time.Time) error

	// AdvanceTOTPStep consumes a code's step; false means it was not beyond the last.
	AdvanceTOTPStep(ctx context.Context, id string, rowVersion, step int64) (bool, error)

	// AdvanceWebAuthnSignCount writes the presented counter under a row_version CAS;
	// false means the row moved or was disabled.
	AdvanceWebAuthnSignCount(ctx context.Context, id string, rowVersion, count int64, at time.Time) (bool, error)
	AllowWorkspaceOrigin(ctx context.Context, o authn.WorkspaceOrigin) error
	ApproveCLIReauthHandoff(ctx context.Context, id string, codeVerifier, windows []byte) (bool, error)
	ApproveWorkspaceHandoff(ctx context.Context, id string, codeVerifier []byte, p domain.PrincipalID, factors, factorClass string, authenticatedAt time.Time) (bool, error)

	// AssertActiveInstanceDEKVersion is the writer fence for authentication-surface
	// credential writes, which seal under the instance DEK with no tenant proof to
	// carry the proof-based fence. It refuses (domain.ErrConflict) a write whose
	// sealed instance DEK version a concurrent rotate-dek --instance has retired.
	// See the authn.Resolver method for the query semantics.
	AssertActiveInstanceDEKVersion(ctx context.Context, version int64) error

	// AuthorityByValue resolves a presented credential-establishment authority.
	//hikyo:forward CredentialAuthorityByVerifier
	AuthorityByValue(ctx context.Context, verifier []byte) (CredentialAuthority, error)

	// BindSessionToSAMLProvider records SAML provider provenance in the same
	// transaction that mints the session.
	BindSessionToSAMLProvider(ctx context.Context, sessionID, providerID string) (bool, error)

	// BindingsForIssuer is the delete guard's census: removing the issuer of a
	// live binding is an authorization change wearing a configuration change's
	// clothes.
	BindingsForIssuer(ctx context.Context, id string) (int64, error)
	CLIReauthHandoffByCode(ctx context.Context, verifier []byte) (CLIReauthHandoff, error)
	CLIReauthHandoffByState(ctx context.Context, verifier []byte) (CLIReauthHandoff, error)

	// ClaimSAMLReplay atomically records an assertion ID. False means replay.
	ClaimSAMLReplay(ctx context.Context, replay NewSAMLReplay) (bool, error)
	ClampCredentialExpiry(ctx context.Context, ceiling time.Time) (int64, error)
	ClampIndefiniteCredentials(ctx context.Context, ceiling time.Time) (int64, error)

	// ClearPendingTOTP removes only in-progress enrolments.
	//hikyo:forward DeletePendingTOTPForAccount
	ClearPendingTOTP(ctx context.Context, accountID string) error

	// ConfirmTOTP promotes and consumes a step in one CAS; false means the row
	// moved or the step was not beyond the last.
	ConfirmTOTP(ctx context.Context, id string, rowVersion, step int64, at time.Time) (bool, error)

	// ConfirmedTOTP resolves an account's confirmed TOTP factor.
	ConfirmedTOTP(ctx context.Context, accountID string) (TOTPCredential, error)

	// ConsumeAuthority claims an authority atomically; false means it was already
	// consumed and the caller must fail closed.
	//hikyo:forward ConsumeCredentialAuthority
	ConsumeAuthority(ctx context.Context, id string, at time.Time) (bool, error)
	ConsumeCLIReauthHandoff(ctx context.Context, id string, at time.Time) (bool, error)

	// ConsumeLoginChallenge claims a login challenge atomically; false means it was
	// already consumed and the caller must fail closed.
	ConsumeLoginChallenge(ctx context.Context, id string, at time.Time) (bool, error)

	// ConsumeOIDCTransaction claims a transaction atomically; false means it moved.
	ConsumeOIDCTransaction(ctx context.Context, id string, at time.Time) (bool, error)

	// ConsumeOutstandingAuthorities marks every unconsumed authority of an account
	// consumed, in the same transaction as a fresh mint or consumption.
	ConsumeOutstandingAuthorities(ctx context.Context, accountID string, at time.Time) error

	// ConsumeSAMLTransaction spends a transaction on first presentation, success
	// or failure.
	ConsumeSAMLTransaction(ctx context.Context, id string, at time.Time) (bool, error)

	// ConsumeSingleDecisionWindow claims a single-decision window exactly once;
	// false means it was already spent (B11 double-spend).
	ConsumeSingleDecisionWindow(ctx context.Context, id string, at time.Time) (bool, error)

	// ConsumeWebAuthnCeremony claims a ceremony atomically and stamps the credential
	// that answered it; false means it was already consumed.
	ConsumeWebAuthnCeremony(ctx context.Context, id, credentialID string, at time.Time) (bool, error)
	ConsumeWorkspaceHandoff(ctx context.Context, id string, at time.Time) (bool, error)
	CorrectPrivacyAccount(ctx context.Context, account, username, displayName string) error

	// CountExternalIdentitiesForIssuer counts the links under one (kind, issuer).
	CountExternalIdentitiesForIssuer(ctx context.Context, kind, issuer string) (int64, error)

	// CountGrantsInOrg reports how many grant rows an organization holds — the
	// read behind the per-org grant sanity cap.
	CountGrantsInOrg(ctx context.Context, org string) (int64, error)

	// CountRegistrationPolicyOrgs counts the live orgs a policy minted.
	CountRegistrationPolicyOrgs(ctx context.Context, policyID string) (int64, error)

	// CreateAccessGrant writes one environment-scoped temporary grant row.
	CreateAccessGrant(ctx context.Context, id string, p domain.PrincipalID, g domain.Grant,
		requestID string, at, expiresAt time.Time) error
	CreateAccount(ctx context.Context, acc Account) error
	CreateCLIReauthHandoff(ctx context.Context, h NewCLIReauthHandoff) error

	// CreateExternalIdentity writes a link.
	CreateExternalIdentity(ctx context.Context, n NewExternalIdentity) error
	CreateFederationIssuer(ctx context.Context, iss NewFederationIssuer) error

	// CreateGrant writes one grant row. The bootstrap path uses it to expand the
	// `admin` template into separate, visible, individually revocable rows rather
	// than an implicit bundle. The general grant surface is #55's.
	CreateGrant(ctx context.Context, id string, p domain.PrincipalID, g domain.Grant, at time.Time) error

	// CreateLoginChallenge writes a single-use, expiring login challenge (#760).
	CreateLoginChallenge(ctx context.Context, c NewLoginChallenge) error
	CreateMachineCredential(ctx context.Context, c NewCredential) error
	CreateMachinePrincipal(ctx context.Context, id domain.PrincipalID, class domain.PrincipalClass, at time.Time) error

	// CreateOIDCTransaction writes a single-use transaction row.
	CreateOIDCTransaction(ctx context.Context, t NewOIDCTransaction) error

	// CreateProvider inserts a provider row (authorized at the chokepoint first).
	CreateProvider(ctx context.Context, n NewProvider) error

	// The two ends of the provisioning connection's life: created with its binding,
	// retired by §6's state machine atomically with the structural grant it held.
	// Neither takes a class: the first hardcodes it and the second requires it.
	CreateProvisioningPrincipal(ctx context.Context, id domain.PrincipalID, at time.Time) error

	// CreateRegistrationPolicy inserts a policy; a second one per scope is ErrConflict.
	CreateRegistrationPolicy(ctx context.Context, p RegistrationPolicy) error

	// CreateRegistrationSignup inserts a pending local sign-up row.
	CreateRegistrationSignup(ctx context.Context, n NewRegistrationSignup) error
	CreateRule(ctx context.Context, rule domain.Rule, createdBy domain.PrincipalID, at time.Time, newItemID func() (string, error)) error

	// CreateSAMLProvider inserts a provider after instance-config authorization.
	CreateSAMLProvider(ctx context.Context, provider NewSAMLProvider) error

	// CreateSAMLSPKey stores a freshly minted encrypted private key.
	CreateSAMLSPKey(ctx context.Context, key NewSAMLSPKey) error

	// CreateSAMLTransaction writes a single-use AuthnRequest transaction.
	CreateSAMLTransaction(ctx context.Context, transaction NewSAMLTransaction) error
	CreateServiceAccountAggregate(ctx context.Context, sa NewServiceAccount) (ServiceAccountCreation, error)

	// CreateTOTP inserts a pending TOTP enrolment.
	CreateTOTP(ctx context.Context, c NewTOTPCredential) error

	// CreateWebAuthnCeremony writes a single-use, expiring challenge row.
	CreateWebAuthnCeremony(ctx context.Context, c NewWebAuthnCeremony) error

	// CreateWebAuthnCredential inserts a freshly enrolled passkey.
	CreateWebAuthnCredential(ctx context.Context, c NewWebAuthnCredential) error
	CreateWorkspaceHandoff(ctx context.Context, h authn.NewWorkspaceHandoff) error

	// CredentialEpoch reads the instance epoch.
	CredentialEpoch(ctx context.Context) (int64, error)
	CredentialPolicy(ctx context.Context) (CredentialPolicy, error)
	CredentialsBeyondCeiling(ctx context.Context, ceiling time.Time) ([]AffectedCredential, error)

	// DeleteAccessGrantsForRequest releases every temporary row one request wrote.
	DeleteAccessGrantsForRequest(ctx context.Context, p domain.PrincipalID, requestID string) (int64, error)

	// DeleteExpiredSAMLReplay removes replay rows after their signed validity plus
	// skew has elapsed.
	DeleteExpiredSAMLReplay(ctx context.Context, at time.Time) (int64, error)
	DeleteFederationIssuer(ctx context.Context, id string) (bool, error)

	// DeleteGrantRow removes a grant row whose last origin was released.
	DeleteGrantRow(ctx context.Context, grantID string, p domain.PrincipalID) (bool, error)

	// DeleteProvider removes a provider.
	DeleteProvider(ctx context.Context, id string) error

	// DeleteRegistrationPolicy removes a policy at an expected row version.
	DeleteRegistrationPolicy(ctx context.Context, id string, expected int64) (bool, error)

	// DeleteRegistrationSignup deletes one pending local sign-up row.
	DeleteRegistrationSignup(ctx context.Context, id string) (bool, error)

	// DeleteRetiringSAMLSPKey erases a retiring key.
	DeleteRetiringSAMLSPKey(ctx context.Context, id string) (bool, error)
	DeleteRule(ctx context.Context, id string, p domain.PrincipalID) (bool, error)
	DeleteRuleItem(ctx context.Context, ruleID, itemID string, p domain.PrincipalID) error

	// DeleteSAMLProvider removes a locked provider row.
	DeleteSAMLProvider(ctx context.Context, id string) error
	DeleteServiceAccountAggregate(ctx context.Context, in DeleteServiceAccountAggregateInput) (ServiceAccountDeletion, error)

	// DeleteWebAuthnCredential removes a credential (de-enrolment) under an
	// account_id predicate. False means zero rows matched — refused fail-closed.
	DeleteWebAuthnCredential(ctx context.Context, id, accountID string) (bool, error)

	// DisableWebAuthnCredential sets disabled_at under a CAS (the clone response);
	// false means the row moved or was already disabled.
	DisableWebAuthnCredential(ctx context.Context, id string, rowVersion int64, at time.Time) (bool, error)

	// EnabledProviderByIssuer resolves the currently enabled provider for an issuer.
	EnabledProviderByIssuer(ctx context.Context, kind, issuer string) (OIDCProvider, error)

	// EnabledProviderBySlug resolves an enabled provider by slug, for start.
	EnabledProviderBySlug(ctx context.Context, slug string) (OIDCProvider, error)

	// EnvironmentChainByID resolves an environment's chain from its id, so
	// LowerEffectiveWindow can build the grant-coverage predicate from an env id.
	EnvironmentChainByID(ctx context.Context, envID string) (EnvironmentChain, error)

	// EnvironmentReauthSettings reads an environment's protection state and its
	// own reauthentication window, if it has one.
	EnvironmentReauthSettings(ctx context.Context, envID string) (EnvSetting, error)

	// EnvironmentsInProject is the universe the mint and widen reachability
	// formulas range over.
	EnvironmentsInProject(ctx context.Context, scope domain.Scope) ([]domain.EnvID, error)
	ErasePrivacyAccount(ctx context.Context, account, p, username string) error

	// ExternalIdentitiesForAccount lists an account's linked identities.
	ExternalIdentitiesForAccount(ctx context.Context, accountID string) ([]ExternalIdentity, error)

	// ExternalIdentityByID resolves a link by id.
	ExternalIdentityByID(ctx context.Context, id string) (ExternalIdentity, error)

	// ExternalIdentityByKey resolves a byte-exact (kind, issuer, subject).
	ExternalIdentityByKey(ctx context.Context, kind, issuer, subject string) (ExternalIdentity, error)
	FederationIssuerByID(ctx context.Context, id string) (FederationIssuer, error)

	// FederationIssuerByIssuer resolves a configuration by its BYTE-EXACT `iss`.
	FederationIssuerByIssuer(ctx context.Context, issuer string) (FederationIssuer, error)
	FederationIssuers(ctx context.Context) ([]FederationIssuer, error)

	// GetRule reads one stored rule, ungated; valid is false for a stored rule
	// that no longer validates (the caller should delete it).
	GetRule(ctx context.Context, id string) (StoredRule, bool, error)

	// GrantLinesAtInstance lists the instance-scope membership surface.
	GrantLinesAtInstance(ctx context.Context) ([]GrantLine, error)

	// GrantLinesInOrg lists the membership surface for one org.
	GrantLinesInOrg(ctx context.Context, org string) ([]GrantLine, error)

	// GrantLinesInProject lists the membership surface for one project.
	GrantLinesInProject(ctx context.Context, org, project string) ([]GrantLine, error)

	// GrantOriginCount reports how many origins still hold a grant row.
	GrantOriginCount(ctx context.Context, grantID string) (int64, error)

	// GrantOriginsFor lists the origins holding one grant row.
	GrantOriginsFor(ctx context.Context, grantID string) ([]Origin, error)

	// GrantOriginsForPrincipal lists every origin holding every grant row of one
	// principal, read at one instant. The SCIM release algorithm (#73 §2.4) decides
	// per origin and then counts what remains per row; seeing the two tables at
	// different instants would let a row be judged against origins that had already
	// moved.
	GrantOriginsForPrincipal(ctx context.Context, p domain.PrincipalID) ([]GrantOriginRow, error)

	// GrantRowsForPrincipal lists a principal's grants with their row ids — the
	// dedup read every create performs under the principal-row lock.
	GrantRowsForPrincipal(ctx context.Context, p domain.PrincipalID) ([]GrantRow, error)

	// GrantsForResetTarget reads the credential-reset target's full grant set for
	// the org-bounded test, under the row lock the reset holds.
	GrantsForResetTarget(ctx context.Context, p domain.PrincipalID) ([]domain.Grant, error)

	// GrantsOf returns a principal's full grant set — the pre-state input to the
	// reachability diff.
	//hikyo:forward Grants
	GrantsOf(ctx context.Context, p domain.PrincipalID) ([]domain.Grant, error)

	// GuardProviderForMint locks the pinned provider row inside a Phase-C mint tx
	// and reports whether it still matches the Phase-A snapshot; false means the
	// provider moved and the mint must refuse (A4 TOCTOU, sweep wins).
	GuardProviderForMint(ctx context.Context, id string, rowVersion int64, issuer string) (bool, error)

	// GuardSAMLProviderForMint proves the provider still matches the Phase-A
	// snapshot immediately before a session or reauth window is written.
	GuardSAMLProviderForMint(ctx context.Context, id string, rowVersion int64, entityID string) (bool, error)
	HoldRestoredPKIIssuers(ctx context.Context) error
	IndefiniteCredentials(ctx context.Context) ([]AffectedCredential, error)
	InstanceConnectionByID(ctx context.Context, id string) (authn.InstanceConnection, error)
	InstanceConnections(ctx context.Context) ([]authn.InstanceConnection, error)

	// InstanceIdentity is this instance's own opaque id — the value a directory
	// listing carries and the one self-connection refusal compares against.
	InstanceIdentity(ctx context.Context) (string, error)

	// InvalidateReauthWindowsForEnvironment deletes every open window on one
	// environment (the effective-window transition, B6) and returns the count.
	//hikyo:forward DeleteReauthWindowsForEnvironment
	InvalidateReauthWindowsForEnvironment(ctx context.Context, environmentID string) (int64, error)
	// IncompatiblePasswordKDFCount is for startup/configuration admission, not a
	// public authentication route. It exposes only aggregate parameter metadata.
	IncompatiblePasswordKDFCount(ctx context.Context, kdf KDFParams) (int64, error)
	InvalidateRestoredAdapterCredentials(ctx context.Context) error
	InvalidateRestoredDynamicProviderCredentials(ctx context.Context) error
	InvalidateRestoredExternalCredentials(ctx context.Context, now time.Time) error

	// ListProviders lists every configured provider.
	ListProviders(ctx context.Context) ([]OIDCProvider, error)

	// ListSAMLProviders lists all configured SAML providers.
	ListSAMLProviders(ctx context.Context) ([]SAMLProvider, error)

	// LiveAccessGrants lists a principal's unexpired temporary grants.
	LiveAccessGrants(ctx context.Context, p domain.PrincipalID, now time.Time) ([]AccessGrant, error)
	LiveMachineCredentialCount(ctx context.Context, serviceAccountID string, epoch int64, now time.Time) (int64, error)
	LiveMachineCredentialCounts(ctx context.Context, scope domain.Scope, epoch int64, now time.Time) (map[string]int64, error)

	// LockCredentialPolicy serializes a mint against a concurrent tightening.
	LockCredentialPolicy(ctx context.Context) error
	LockInstanceIdentityRow(ctx context.Context) error

	// LockMachinePrincipal takes a service account's principal-row lock — THE SAME
	// LOCK the grant writers take — so a mint and a grant landing on that
	// principal serialize. Without it a grant can widen the account between the
	// mint's post-state check and its insert, producing a token whose authority
	// never passed the gate.
	//hikyo:forward LockPrincipalRow
	LockMachinePrincipal(ctx context.Context, p domain.PrincipalID) error

	// LockProviderForDelete locks the provider row inside the delete tx so the
	// session sweep runs with the row held and a concurrent mint guard serializes
	// behind it (A14). ErrNotFound means a concurrent delete already removed it.
	LockProviderForDelete(ctx context.Context, id string) error

	// LockSAMLProviderForDelete serializes provider deletion against Phase-C mints.
	LockSAMLProviderForDelete(ctx context.Context, id string) error

	// LockTargetPrincipal takes the target principal's row lock so the org-bounded
	// test and every grant mutation serialize on the same row (B14).
	//hikyo:forward LockPrincipalRow
	LockTargetPrincipal(ctx context.Context, p domain.PrincipalID) error

	// LockWorkspaceOrigin and LockInstanceIdentityRow are the two row locks the
	// serving side's read-then-write decisions serialize on under postgres' READ
	// COMMITTED semantics.
	LockWorkspaceOrigin(ctx context.Context, origin string) (bool, error)

	// LockoutRetentions lists every `lockout-retention` origin in the instance,
	// which is what the deterministic cure sweep walks (#73 §2.4).
	LockoutRetentions(ctx context.Context) ([]LockoutRetention, error)

	// LockoutRetentionsInOrg is the org-bounded cure sweep.
	LockoutRetentionsInOrg(ctx context.Context, org domain.OrgID) ([]LockoutRetention, error)

	// LoginChallengeByID resolves a login challenge, or domain.ErrNotFound.
	LoginChallengeByID(ctx context.Context, id string) (LoginChallenge, error)
	MachineCredentialsFor(ctx context.Context, serviceAccountID string) ([]MachineCredential, error)

	// ManageMembersHolders is the lockout invariant's census at one org, or at
	// instance scope when org is empty.
	ManageMembersHolders(ctx context.Context, org string) ([]domain.PrincipalID, error)

	// MarkSAMLSPKeyRetiring compare-and-swaps an active key into overlap state.
	MarkSAMLSPKeyRetiring(ctx context.Context, id string, rowVersion int64) (bool, error)

	// MintAuthority writes a new credential-establishment authority.
	//hikyo:forward CreateCredentialAuthority
	MintAuthority(ctx context.Context, n NewCredentialAuthority) error
	MintInstanceConnection(ctx context.Context, n authn.NewInstanceConnection) error

	// MintSession writes a session row.
	//hikyo:forward CreateSession
	MintSession(ctx context.Context, s NewSession) error

	// OIDCTransactionByState resolves a transaction by its state verifier.
	OIDCTransactionByState(ctx context.Context, stateVerifier []byte) (OIDCTransaction, error)

	// OpenReauthWindow opens a reauthentication window over one environment.
	//hikyo:forward CreateReauthWindow
	OpenReauthWindow(ctx context.Context, w NewReauthWindow) error

	// PasswordCredentialFor reads an account's verifier row.
	//hikyo:forward PasswordCredential
	PasswordCredentialFor(ctx context.Context, accountID string) (PasswordCredential, error)

	// PendingTOTP resolves an account's in-progress enrolment.
	PendingTOTP(ctx context.Context, accountID string) (TOTPCredential, error)

	// PinGeneration reads the conditional cursor's pin component.
	PinGeneration(ctx context.Context, p domain.PrincipalID, env domain.EnvID) (int64, error)

	// PrincipalClass resolves a principal's class for the normative machine
	// allowlists.
	PrincipalClass(ctx context.Context, p domain.PrincipalID) (domain.PrincipalClass, error)

	// PrincipalGeneration reads the principal's current session generation, so a
	// freshly minted session records the generation it was born under.
	PrincipalGeneration(ctx context.Context, p domain.PrincipalID) (int64, error)
	PrivacyAccount(ctx context.Context, p string) (PrivacyAccountView, error)
	PrivacyActivity(ctx context.Context, p string) ([]PrivacyActivity, error)
	PrivacySessions(ctx context.Context, p string) ([]PrivacySession, error)

	// ProjectMachineReveal reads the per-project machine-reveal opt-in
	// (source-of-truth ADR). ErrNotFound for an unknown project.
	ProjectMachineReveal(ctx context.Context, projectID string) (authn.MachineRevealState, error)

	// ProviderBySlug resolves a provider by slug for administration (any state).
	// The mutation that follows is authorized at the chokepoint first.
	ProviderBySlug(ctx context.Context, slug string) (OIDCProvider, error)

	// ProviderForCallback resolves the provider a transaction pinned, by id.
	ProviderForCallback(ctx context.Context, id string) (OIDCProvider, error)

	// ReactivateBinding records a restore-time re-validation (§ Restore). #76 owns
	// the operator ceremony; the write exists here because the refusal it drives
	// exists now.
	ReactivateBinding(ctx context.Context, id string, at time.Time) (bool, error)

	// ReauthWindowFor resolves the window over one environment for one session.
	ReauthWindowFor(ctx context.Context, sessionID, environmentID string) (ReauthWindow, error)

	// RebindSAMLExternalIdentityProvider compare-and-swaps provider provenance
	// after the same byte-exact entity has been removed and configured again.
	RebindSAMLExternalIdentityProvider(ctx context.Context, id, expectedProviderID, newProviderID string) (bool, error)

	// ReconcilePrincipal commits ONE principal's reconciliation. The signature is
	// the guarantee: one id in, one answer out. There is no set-taking sibling of
	// this method anywhere in the module, and the drill asserts that.
	ReconcilePrincipal(ctx context.Context, p domain.PrincipalID) (bool, error)

	// RecoveryCodesFor resolves an account's batch.
	//hikyo:forward RecoveryCodes
	RecoveryCodesFor(ctx context.Context, accountID string) (RecoveryBatch, error)

	// RegistrationPolicyFor resolves the policy of one scope ("" = instance).
	RegistrationPolicyFor(ctx context.Context, org domain.OrgID) (RegistrationPolicy, error)

	// RegistrationSignupsForPolicy lists a policy's pending local sign-ups.
	RegistrationSignupsForPolicy(ctx context.Context, policyID string) ([]RegistrationSignupRef, error)

	// ReleaseGrantOrigin releases one origin, reporting whether it held the row.
	ReleaseGrantOrigin(ctx context.Context, grantID string, p domain.PrincipalID, o Origin) (bool, error)

	// RemoveExternalIdentity removes a link (unlink).
	//hikyo:forward DeleteExternalIdentity
	RemoveExternalIdentity(ctx context.Context, id string) error

	// RemoveTOTPForAccount deletes every TOTP row of an account.
	//hikyo:forward DeleteTOTPForAccount
	RemoveTOTPForAccount(ctx context.Context, accountID string) error

	// RemoveWorkspaceOrigin and RevokeWorkspaceSessionsForOrigin are ONE ACT in two
	// statements and must be called in one transaction. That pairing is the ADR's
	// atomic kill switch; splitting it leaves a window in which an origin is
	// de-allowlisted and its sessions still authenticate.
	RemoveWorkspaceOrigin(ctx context.Context, origin string) (bool, error)

	// ReplacePasswordCredential compare-and-swaps an existing verifier. False
	// means the row moved underneath and the caller must not write a stale
	// verifier back.
	//hikyo:forward UpdatePasswordCredential
	ReplacePasswordCredential(ctx context.Context, c PasswordCredential, at time.Time) (bool, error)

	// ReplaceRecoveryCodes compare-and-swaps the batch; false means it moved.
	//hikyo:forward UpdateRecoveryCodes
	ReplaceRecoveryCodes(ctx context.Context, b RecoveryBatch, at time.Time) (bool, error)

	// ReplaceRegistrationPolicy compare-and-swaps a policy and replaces its children.
	ReplaceRegistrationPolicy(ctx context.Context, p RegistrationPolicy, expected int64) (bool, error)

	// ResolveChain answers "is this (org, project, environment) a real chain" —
	// the same single-query resolution authorize() itself performs, exposed for the
	// one caller that must ask about a scope it is NOT currently authorizing: a
	// SCIM mapping row names a scope at AUTHORING time and expands it at every
	// sync, and a row whose environment does not belong to its project would write
	// grants against a chain that never existed.
	// It mints no proof and reveals nothing a caller could not learn by addressing
	// the scope: an unresolvable chain answers domain.ErrNotFound uniformly,
	// whether the link is missing or foreign.
	ResolveChain(ctx context.Context, scope domain.Scope) (domain.Scope, error)

	// RestoreState reads the instance's restore posture.
	RestoreState(ctx context.Context) (RestoreState, error)
	RestrictPrivacyPrincipal(ctx context.Context, p, state string) error

	// RevokeAllSessionsFor deletes every session of a principal.
	//hikyo:forward DeleteSessionsForPrincipal
	RevokeAllSessionsFor(ctx context.Context, p domain.PrincipalID) error
	RevokeInstanceConnection(ctx context.Context, id string, at time.Time) (bool, error)
	RevokeMachineCredential(ctx context.Context, serviceAccountID, id string, at time.Time) (bool, error)

	// RevokeSession deletes one session in this transaction.
	//hikyo:forward DeleteSession
	RevokeSession(ctx context.Context, id string) error
	RevokeSessionForPrincipal(ctx context.Context, id string, p domain.PrincipalID) (bool, error)
	RevokeWorkspaceSessionsForOrigin(ctx context.Context, origin string) (int64, error)

	// RotateSessionFactors rotates the acting session token and rewrites its
	// factor set on step-up, preserving the original authentication attribution.
	RotateSessionFactors(ctx context.Context, id string, verifier []byte, factors string) error

	// SAMLProviderBySlug resolves a provider in any state for administration.
	SAMLProviderBySlug(ctx context.Context, slug string) (SAMLProvider, error)

	// SAMLProviderForCallback resolves the provider pinned by a transaction.
	SAMLProviderForCallback(ctx context.Context, id string) (SAMLProvider, error)

	// SAMLSPKeys lists active and overlap-retiring public material.
	SAMLSPKeys(ctx context.Context) ([]SAMLSPKey, error)

	// SAMLTransactionByRelayState resolves the opaque front-channel handle before
	// the strict wrapper's single response-validation pass.
	SAMLTransactionByRelayState(ctx context.Context, verifier []byte) (SAMLTransaction, error)
	SCIMCredentialByVerifier(ctx context.Context, presented []byte) (SCIMCredential, error)

	// ServiceAccountAt resolves one service account within an addressed project;
	// an id from another project answers domain.ErrNotFound.
	ServiceAccountAt(ctx context.Context, scope domain.Scope, id string) (ServiceAccount, error)

	// ServiceAccountByPrincipal resolves the service account a machine principal
	// is, for the grant surface's subtree confinement.
	ServiceAccountByPrincipal(ctx context.Context, p domain.PrincipalID) (ServiceAccount, error)
	ServiceAccountsIn(ctx context.Context, scope domain.Scope) ([]ServiceAccount, error)

	// SessionsForPrincipal and RevokeSessionForPrincipal are the self-scoped
	// active-session surface (#71 criterion 5). The principal conjunct is in the
	// SQL, so one caller structurally cannot reach another's row.
	SessionsForPrincipal(ctx context.Context, p domain.PrincipalID) ([]authn.SessionSummary, error)

	// SetClock fixes the instant time-bound grants are evaluated against for the
	// rest of this transaction. The service layer's authorize prelude sets it from
	// the service clock, so expiry is judged by one clock per operation. Zero
	// restores wall-clock evaluation at each lookup.
	SetClock(now time.Time)
	SetCredentialPolicy(ctx context.Context, p CredentialPolicy, actor domain.PrincipalID, at time.Time) error

	// SetPinGeneration advances it. #52 owns pin creation, reassignment and release.
	SetPinGeneration(ctx context.Context, p domain.PrincipalID, env domain.EnvID, generation int64) error

	// SetWebAuthnUserHandle sets the opaque handle once; false means one already
	// exists (the caller reads it back rather than rotating).
	SetWebAuthnUserHandle(ctx context.Context, accountID string, handle []byte) (bool, error)

	// SlideReauthWindow advances a sliding window's idle clock; false means the row
	// moved and the caller must not extend it.
	SlideReauthWindow(ctx context.Context, id string, windowExpires time.Time) (bool, error)

	// SlideSession advances the idle clock only. The absolute lifetime is never
	// extended by activity — two independent clocks is the design.
	//hikyo:forward TouchSession
	SlideSession(ctx context.Context, id string, seen, idleExpires time.Time) error

	// StrandedRevealPrincipals enumerates the reveal-holding principals a 0
	// effective window would strand on the given environment chain (B6).
	StrandedRevealPrincipals(ctx context.Context, org, project, env string) ([]domain.PrincipalID, error)
	SweepExpiredWorkspaceHandoffs(ctx context.Context, before time.Time) (int64, error)

	// SweepSessionsForProvider deletes every session minted through a provider and
	// returns the count for audit (A4).
	//hikyo:forward DeleteSessionsForProvider
	SweepSessionsForProvider(ctx context.Context, providerID string) (int64, error)

	// SweepSessionsForSAMLProvider deletes all sessions minted through a SAML IdP.
	//hikyo:forward DeleteSessionsForSAMLProvider
	SweepSessionsForSAMLProvider(ctx context.Context, providerID string) (int64, error)

	// SweepSessionsForWebAuthnCredential deletes every session a passkey login
	// minted through a credential and returns the count for audit (B9 clone sweep).
	//hikyo:forward DeleteSessionsForWebAuthnCredential
	SweepSessionsForWebAuthnCredential(ctx context.Context, credentialID string) (int64, error)
	TouchInstanceConnection(ctx context.Context, id string, at time.Time) error
	TouchMachineCredential(ctx context.Context, id string, at time.Time) error
	TouchSCIMCredential(ctx context.Context, id string, at time.Time) error

	// UnreconciledPrincipals lists who is still inert.
	UnreconciledPrincipals(ctx context.Context) ([]PrincipalRef, error)
	UpdateAccountProfile(ctx context.Context, accountID string, profile AccountProfile) error
	UpdateFederationIssuer(ctx context.Context, id string, source jwkssource.KeySource, refused []string, caBundle string, actor domain.PrincipalID, at time.Time) (bool, error)

	// UpdateProvider compare-and-swaps a provider; false means the row moved.
	UpdateProvider(ctx context.Context, u ProviderUpdate) (bool, error)

	// UpdateSAMLProvider compare-and-swaps a provider configuration.
	UpdateSAMLProvider(ctx context.Context, provider SAMLProviderUpdate) (bool, error)

	// WebAuthnCeremonyByChallenge resolves a ceremony by its challenge verifier.
	WebAuthnCeremonyByChallenge(ctx context.Context, challengeVerifier []byte) (WebAuthnCeremony, error)

	// WebAuthnCeremonyByID resolves a ceremony by id, for single-decision window
	// unit matching at disclosure.
	WebAuthnCeremonyByID(ctx context.Context, id string) (WebAuthnCeremony, error)

	// WebAuthnCredentialByCredentialID resolves the row a passkey assertion names.
	WebAuthnCredentialByCredentialID(ctx context.Context, credentialID []byte) (WebAuthnCredential, error)

	// WebAuthnCredentialByID resolves a credential by its surrogate id.
	WebAuthnCredentialByID(ctx context.Context, id string) (WebAuthnCredential, error)

	// WebAuthnCredentialsForAccount lists an account's passkeys.
	WebAuthnCredentialsForAccount(ctx context.Context, accountID string) ([]WebAuthnCredential, error)

	// WebAuthnUserHandle reads an account's opaque handle, or nil when unset.
	WebAuthnUserHandle(ctx context.Context, accountID string) ([]byte, error)

	// WorkloadPinState reads the conditional reveal-history admission fact.
	WorkloadPinState(ctx context.Context, p domain.PrincipalID, env domain.EnvID) (WorkloadPinState, error)
	WorkspaceHandoffByCode(ctx context.Context, verifier []byte) (authn.WorkspaceHandoff, error)
	WorkspaceHandoffByState(ctx context.Context, verifier []byte) (authn.WorkspaceHandoff, error)
	WorkspaceOriginAllowed(ctx context.Context, origin string) (bool, error)
	WorkspaceOrigins(ctx context.Context) ([]authn.WorkspaceOrigin, error)

	// WritePasswordCredential inserts the first verifier for an account.
	//hikyo:forward CreatePasswordCredential
	WritePasswordCredential(ctx context.Context, c PasswordCredential, at time.Time) error

	// WriteRecoveryCodes writes the first batch for an account.
	//hikyo:forward CreateRecoveryCodes
	WriteRecoveryCodes(ctx context.Context, b RecoveryBatch, at time.Time) error
	// OAuth2's profile-pinned provider and single-use transaction resolution.
	CreateOAuth2Provider(ctx context.Context, n NewOAuth2Provider) error
	OAuth2ProviderBySlug(ctx context.Context, slug string) (OAuth2Provider, error)
	ListOAuth2Providers(ctx context.Context) ([]OAuth2Provider, error)
	UpdateOAuth2Provider(ctx context.Context, u OAuth2ProviderUpdate) (bool, error)
	LockOAuth2ProviderForDelete(ctx context.Context, id string) error
	DeleteOAuth2Provider(ctx context.Context, id string) error
	GuardOAuth2ProviderForMint(ctx context.Context, id string, rowVersion int64, issuer string) (bool, error)
	OAuth2ProviderForCallback(ctx context.Context, id string) (OAuth2Provider, error)
	CreateOAuth2Transaction(ctx context.Context, t NewOAuth2Transaction) error
	OAuth2TransactionByState(ctx context.Context, stateVerifier []byte) (OAuth2Transaction, error)
	ConsumeOAuth2Transaction(ctx context.Context, id string, at time.Time) (bool, error)
	BindSessionToOAuth2Provider(ctx context.Context, sessionID, providerID string) (bool, error)
	DeleteSessionsForOAuth2Provider(ctx context.Context, providerID string) (int64, error)
	CredentialAuthorityByID(ctx context.Context, id string) (CredentialAuthority, error)
	ClaimOAuth2Authority(ctx context.Context, id string, at time.Time) (bool, error)
	StampCredentialEstablish(ctx context.Context, sessionID, identityID string, expires time.Time) error
}
