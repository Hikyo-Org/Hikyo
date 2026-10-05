package main

import "github.com/Hikyo-Org/hikyo/internal/multicall"

// reviewedWireExtras is the compile-checked authoring owner for contract-external
// entries and supplemental authority/audit links. It runs only during generation.
// OpenAPI owns each HTTP route class and primary operation.
var reviewedWireExtras = wireExtras{
	Version: 1,
	Extensions: map[string]wireRow{
		// Replacement prepares independent create and revoke proofs in one transaction.
		"http:POST /api/v1/orgs/{org}/rules/replace":         {Ops: []string{"OpRuleRevoke"}},
		"http:POST /api/v1/auth/signup":                      {Events: []string{"EventRegistrationSignupAdmitted", "EventRegistrationSignupRefused", "EventRegistrationSignupExpired", "EventRegistrationMailIntent", "EventRegistrationMailOutcome"}},
		"http:POST /api/v1/auth/signup/verify":               {Events: []string{"EventRegistrationSignupRefused", "EventRegistrationSignupCompleted", "EventOrgCreated", "EventGrantCreated", "EventGrantTemplateApplied"}},
		"http:DELETE /api/v1/auth/identities/{id}":           {Events: []string{"EventIdentityUnlinked", "EventAuthSessionCreated", "EventAuthThrottleCrossed"}},
		"http:DELETE /api/v1/auth/totp":                      {Events: []string{"EventAuthFactorRemoved", "EventAuthSessionCreated", "EventAuthThrottleCrossed"}},
		"http:DELETE /api/v1/auth/webauthn/credentials/{id}": {Events: []string{"EventAuthPasskeyRemoved", "EventAuthSessionCreated", "EventAuthThrottleCrossed"}},
		// The self-scoped revoke. A workspace session's death is a #71 event; an
		// ordinary session's is a logout, already the trail's own vocabulary.
		"http:DELETE /api/v1/me/sessions/{session}":                                                             {Events: []string{"EventAuthLogout", "EventRemoteWorkspaceSessionRevoked"}},
		"http:DELETE /api/v1/orgs/{org}/projects/{project}/access-policies/{policy}":                            {Events: []string{"EventAccessPolicyChanged"}},
		"http:DELETE /api/v1/orgs/{org}/projects/{project}/approval-policies/{policy}":                          {Events: []string{"EventApprovalPolicyChanged"}},
		"http:DELETE /api/v1/orgs/{org}/projects/{project}/environments/{environment}/pins/{workloadPrincipal}": {Events: []string{"EventPinReleased"}},
		"http:DELETE /api/v1/orgs/{org}/projects/{project}/environments/{environment}/values/{key}":             {Events: []string{"EventValueStaged"}},
		"http:GET /api/v1/auth/cli-reauth/transactions/{state}":                                                 {Events: []string{"EventAuthCLIReauthHandoff"}},
		// A sign-up-intent login (#607) adds the registration outcomes and, for
		// a landing, the org creation and template grants its authority writes.
		"http:GET /api/v1/auth/oidc/{provider}/callback":       {Events: []string{"EventOIDCLogin", "EventOIDCRefused", "EventIdentityLinked", "EventAuthSessionCreated", "EventAuthReauthenticated", "EventAuthThrottleCrossed", "EventRegistrationSignupAdmitted", "EventRegistrationSignupRefused", "EventRegistrationSignupCompleted", "EventOrgCreated", "EventGrantCreated", "EventGrantTemplateApplied"}},
		"http:GET /api/v1/auth/oauth2/{provider}/callback":     {Events: []string{"EventOAuth2Login", "EventOAuth2Refused", "EventIdentityLinked", "EventCredentialEstablish", "EventAuthCredentialEstablished", "EventAuthSessionCreated", "EventAuthThrottleCrossed", "EventRegistrationSignupAdmitted", "EventRegistrationSignupRefused", "EventRegistrationSignupCompleted", "EventOrgCreated", "EventGrantCreated", "EventGrantTemplateApplied"}},
		"http:GET /api/v1/auth/workspace/transactions/{state}": {Events: []string{"EventRemoteWorkspaceHandoffRead"}},
		// The audit trail read surface (#45). Query and export at each addressed
		// depth; the depth is in the path, so one operation per route. Reading is
		// itself audited: the query op commits its own audit.query, the export op
		// its INTENT/OUTCOME pair. All tenant-class: an audit surface the caller may
		// not read answers exactly like a scope that is not there.
		"http:GET /api/v1/orgs/{org}/audit":        {Events: []string{"EventAuditQuery"}},
		"http:GET /api/v1/orgs/{org}/audit/export": {Events: []string{"EventAuditExportStarted", "EventAuditExportCompleted"}},
		// Temporary access (#152). Policy administration is manage-members at the
		// project; the request queue and every decision are read@env with the
		// finer eligibility checked after the grant (a reachable 403).
		"http:GET /api/v1/orgs/{org}/projects/{project}/access-policies": {Events: []string{"EventAccessPolicyRead"}},
		// Secret-change approvals (#151). Policy administration is project-scoped;
		// the review queue is a read@env audited-none; voting is publish@env. The
		// merge/bypass decision rides the publish route, not these.
		"http:GET /api/v1/orgs/{org}/projects/{project}/approval-policies":                       {Events: []string{"EventApprovalPolicyRead"}},
		"http:GET /api/v1/orgs/{org}/projects/{project}/audit":                                   {Events: []string{"EventAuditQuery"}},
		"http:GET /api/v1/orgs/{org}/projects/{project}/audit/export":                            {Events: []string{"EventAuditExportStarted", "EventAuditExportCompleted"}},
		"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/audit":        {Events: []string{"EventAuditQuery"}},
		"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/audit/export": {Events: []string{"EventAuditExportStarted", "EventAuditExportCompleted"}},
		// Machine delivery (#62) is tenant-class at environment depth: unreadable
		// and missing environments answer identically, keeping conditional answers
		// safe. OpenAPI owns delivery.fetch and its access record. The supplemental
		// events cover pre-principal federation refusal and JWKS refresh failure,
		// staleness breach or throttled unknown-kid refresh. They use the resolution
		// surface's pre-authentication audit writer because no operation proof
		// exists yet; completeness therefore unions the operation and event links.
		"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/delivery": {Events: []string{"EventFederationRefused", "EventJWKSRefreshFailed"}},
		// The stream authorizes twice: once at connect over the project, and once
		// per event over the environment the event names.
		"http:GET /api/v1/orgs/{org}/projects/{project}/events":       {Ops: []string{"OpAdvisoryEvent"}},
		"http:GET /api/v1/orgs/{org}/scim/v2/{binding}/ResourceTypes": {Events: []string{"EventSCIMCredentialRefused"}},
		"http:GET /api/v1/orgs/{org}/scim/v2/{binding}/Schemas":       {Events: []string{"EventSCIMCredentialRefused"}},
		// The credential-versus-binding-path mismatch (#73 §8). It is refused
		// BEFORE any operation authorizes; there is no proof and no operation
		// row to hang it on, so like the authentication surface's own events it
		// is declared here, against the mount every wire request enters through.
		//
		// All THREE discovery routes declare it, and they are the only routes that
		// must: their operation (`scim-discovery.read`) declares no events at all
		// (ADR §10 annotates the probe class audited-none-equivalent, pinned in
		// internal/isolation/testdata/audited_exemptions.json), so this is the whole
		// of their audit linkage. Every other wire route inherits an event list from
		// its own operation.
		"http:GET /api/v1/orgs/{org}/scim/v2/{binding}/ServiceProviderConfig": {Events: []string{"EventSCIMCredentialRefused"}},
		"http:PATCH /api/v1/me/profile":                                       {Events: []string{"EventAuthProfileUpdated", "EventAuthThrottleCrossed"}},
		// Credential reset (#54). Unauthenticated-class for its probe contract:
		// the target-principal path parameter makes enumeration uniformity the
		// dominant concern, so every failure that could reveal the target's grant
		// shape answers a uniform 401 (the instance-capability refusal is the one
		// named 403, reached only after the caller is authorized at instance scope).
		// The route dispatches at runtime between two credential-reset operations, so
		// it names no single operation in Ops; its audit obligation is discharged
		// through Events like the account-security surface.
		// Credential reset (#54). ONE route dispatches at runtime between the
		// org-scoped and instance-scoped credential-reset operations by the target's
		// grant classification, resolved under the target-row lock inside the
		// handler's tx. Both are mapped here so the operation linkage records that
		// this route reaches CapCredentialReset (MFA-mandatory). The chokepoint
		// authorize(), called on the chosen operation inside that transaction,
		// enforces capability + MFA + assurance. The route keeps its unauthenticated
		// probe class (enumeration uniformity is its dominant contract, reinforced by
		// B2's uniform refusal) and carries no single x-hikyo-operation, since two ops
		// of different classes cannot be named by one contract row; its audit events
		// also ride Events.
		// Credential reset (#54). A successful reset mints a credential-establishment
		// authority (its own record, factors MEDIUM-7) and records the reset issuance
		// naming the tier. See the exception note above for why this route audits here
		// rather than through an operation row.
		"http:POST /api/v1/accounts/{principal}/credential-reset": {Ops: []string{"OpCredentialReset", "OpCredentialResetInstance"}, Events: []string{"EventAuthCredentialResetIssued", "EventAuthAuthorityMinted"}, NoPrimary: "Credential reset selects OpCredentialReset or OpCredentialResetInstance by target principal scope; OpenAPI has no single x-hikyo-operation."},
		"http:POST /api/v1/auth/cli-reauth/approve":               {Events: []string{"EventAuthCLIReauthHandoff"}},
		"http:POST /api/v1/auth/cli-reauth/redeem":                {Events: []string{"EventAuthCLIReauthHandoff"}},
		"http:POST /api/v1/auth/cli-reauth/start":                 {Events: []string{"EventAuthCLIReauthHandoff"}},
		"http:POST /api/v1/auth/credential/establish":             {Events: []string{"EventAuthCredentialEstablished", "EventAuthAuthorityRefused"}},
		"http:POST /api/v1/auth/identities/link":                  {Events: []string{"EventAuthThrottleCrossed"}},
		"http:POST /api/v1/auth/local/login":                      {Events: []string{"EventAuthLogin", "EventAuthSessionCreated", "EventAuthThrottleCrossed"}},
		// Login-challenge finish endpoints (#760). A browser password login on an
		// account with an enrolled factor mints no session; these present the factor
		// against the single-use challenge and mint. Unauthenticated-class like the
		// step-up endpoints: they take a challenge id, not a session, and a
		// spent/expired challenge is exactly the case they must not distinguish. The
		// mint discharges its audit obligation through the same login/session events
		// as `local/login`; the webauthn finish also carries the clone event.
		"http:POST /api/v1/auth/login/challenge/{challenge}/totp":            {Events: []string{"EventAuthLogin", "EventAuthSessionCreated", "EventAuthThrottleCrossed"}},
		"http:POST /api/v1/auth/login/challenge/{challenge}/webauthn/finish": {Events: []string{"EventAuthLogin", "EventAuthSessionCreated", "EventAuthPasskeyCloned", "EventAuthThrottleCrossed"}},
		"http:POST /api/v1/auth/logout":                                      {Events: []string{"EventAuthLogout"}},
		// whoami resolves a session and reports it. It writes nothing and its
		// result duplicates what the login event already recorded, so it is the
		// one auth path with no event of its own, pinned in the exemption
		// fixture with that reason rather than silently absent.
		// OIDC (#54). start emits only a throttle crossing directly; the callback
		// is where a login/link/reauth lands, so it carries the family of outcomes
		// (login success, refusal by cause, link, the reissued/rotated session,
		// reauth). link start mirrors start; unlink emits the unlink plus the
		// reissued session. Provider administration is operation-modeled (Ops).
		"http:POST /api/v1/auth/oidc/{provider}/start":   {Events: []string{"EventAuthThrottleCrossed"}},
		"http:POST /api/v1/auth/oauth2/{provider}/start": {Events: []string{"EventAuthThrottleCrossed"}},
		// The TOTP half of the disclosure ceremony (#58). Unauthenticated-class
		// for the same reason as every other reauth leg: it authenticates a factor
		// rather than acting on a tenant object, and its refusals are uniform.
		"http:POST /api/v1/auth/reauth/totp":               {Events: []string{"EventAuthReauthenticated", "EventAuthThrottleCrossed"}},
		"http:POST /api/v1/auth/recovery-codes/regenerate": {Events: []string{"EventAuthRecoveryCodesGenerated", "EventAuthSessionCreated", "EventAuthThrottleCrossed"}},
		"http:POST /api/v1/auth/recovery/begin":            {Events: []string{"EventAuthRecoveryCodeConsumed", "EventAuthAuthorityMinted", "EventAuthThrottleCrossed"}},
		"http:POST /api/v1/auth/saml/{provider}/acs":       {Events: []string{"EventSAMLLogin", "EventSAMLReauth", "EventIdentityLinked", "EventAuthSessionCreated", "EventAuthThrottleCrossed"}},
		// SAML SP (#72). Start and ACS are purpose-polymorphic identity-protocol
		// endpoints: login is pre-auth, while link/reauth bind an existing session;
		// enumeration uniformity is therefore their probe contract. Metadata is
		// documentation-class public material under pre-auth admission. Provider
		// administration is instance-config.
		"http:POST /api/v1/auth/saml/{provider}/start": {Events: []string{"EventAuthThrottleCrossed"}},
		"http:POST /api/v1/auth/totp/enrol/confirm":    {Events: []string{"EventAuthFactorEnrolled", "EventAuthSessionCreated", "EventAuthThrottleCrossed"}},
		// Factor endpoints (#54). Unauthenticated-class like logout/whoami: they
		// take a session but an unresolvable one is exactly the case they must not
		// distinguish, so their probe contract is enumeration uniformity, not
		// tenancy. `recovery/begin` is fully pre-auth. None reaches an authz
		// operation; the account-security mutations resolve and rotate the acting
		// session, which is resolution rather than authorization, so their audit
		// obligation is discharged directly through Events like every other
		// authentication-surface endpoint.
		// Factor endpoints (#54). The account-security mutations emit their
		// mutation event plus auth.session_created for the reissued session; step-up
		// emits auth.reauthenticated (it rotates, mints no new session row);
		// recovery/begin emits recovery_code_consumed (success and failure) and
		// mints an establishment authority whose consumption is recorded by the
		// establish path.
		// Each factor ceremony validates a proof under the per-account backoff, so
		// a crossed threshold is an event it can emit, declared here so the
		// audit-completeness contract covers it.
		"http:POST /api/v1/auth/totp/enrol/start":      {Events: []string{"EventAuthThrottleCrossed"}},
		"http:POST /api/v1/auth/totp/step-up":          {Events: []string{"EventAuthReauthenticated", "EventAuthThrottleCrossed"}},
		"http:POST /api/v1/auth/webauthn/enrol/finish": {Events: []string{"EventAuthPasskeyAdded", "EventAuthSessionCreated"}},
		// WebAuthn / passkeys (#54). Enrolment, login, step-up, reauth, removal and
		// the credential inventory. Login is fully pre-auth; the rest take a session
		// but an unresolvable one is exactly the case they must not distinguish, so
		// all are unauthenticated-class (enumeration uniformity). None reaches an
		// authz operation; the mutations resolve and rotate the acting session,
		// which is resolution rather than authorization, so their audit obligation
		// is discharged directly through Events.
		// WebAuthn / passkeys (#54). The three start ceremonies and the credential
		// read emit nothing directly and are exemption-pinned; the finish endpoints
		// carry the outcomes. enrol validates a proof under the per-account backoff
		// (a crossed threshold is its own event) and adds a credential + reissues
		// the session; login mints a session and, on a signature-count regression,
		// disables the cloned credential; step-up and reauth append the factor
		// (reauthenticated) and can likewise detect a clone; removal removes the
		// credential and reissues the session.
		"http:POST /api/v1/auth/webauthn/enrol/start":    {Events: []string{"EventAuthThrottleCrossed"}},
		"http:POST /api/v1/auth/webauthn/login/finish":   {Events: []string{"EventAuthLogin", "EventAuthSessionCreated", "EventAuthPasskeyCloned", "EventAuthThrottleCrossed"}},
		"http:POST /api/v1/auth/webauthn/reauth/finish":  {Events: []string{"EventAuthReauthenticated", "EventAuthPasskeyCloned"}},
		"http:POST /api/v1/auth/webauthn/step-up/finish": {Events: []string{"EventAuthReauthenticated", "EventAuthPasskeyCloned"}},
		"http:POST /api/v1/auth/workspace/approve":       {Events: []string{"EventRemoteHandoffFailed"}},
		// Redeem carries two shapes, because a redemption is two acts: an
		// establishment ISSUES a workspace session, while a step-up ELEVATES the one
		// it was bound to and mints nothing; the trail records that as the ordinary
		// reauthentication it is, on the session that was elevated.
		"http:POST /api/v1/auth/workspace/redeem": {Events: []string{"EventRemoteWorkspaceSessionIssued", "EventAuthReauthenticated", "EventRemoteHandoffFailed"}},
		// Multi-instance handoff (#71). These three carry the workspace tier's
		// pre-authentication audit obligation, which no operation can carry for
		// them: start and redeem authenticate nobody, and a handoff FAILURE
		// predates any session at every stage.
		"http:POST /api/v1/auth/workspace/start":       {Events: []string{"EventRemoteHandoffFailed"}},
		"http:POST /api/v1/instance/config/adoption":   {Ops: []string{"OpSelfConfigProvisionProject"}},
		"http:POST /api/v1/instance/reencrypt":         {Events: []string{"EventReencryptCompleted"}},
		"http:POST /api/v1/instance/rotate-dek":        {Events: []string{"EventDEKRotated"}},
		"http:POST /api/v1/instance/rotate-master-key": {Events: []string{"EventMasterKeyRotated"}},
		"http:POST /api/v1/instance/rotate-root-key":   {Events: []string{"EventRootKeyRotationPrepared", "EventRootKeyRotationVerified", "EventRootKeyRotationFinalized"}},
		// The scanning fingerprint key is instance-scoped too (#74).
		"http:POST /api/v1/instance/rotate-scanning-key": {Events: []string{"EventScanningKeyRotated"}},
		// The root token key belongs to the instance, so there is no tenant object
		// whose nonexistence a refusal could mimic. The same holds for every DEK: a
		// DEK belongs to the instance's crypto hierarchy, not a tenant.
		"http:POST /api/v1/instance/rotate-token-key":                                                                         {Events: []string{"EventTokenKeyRotated"}},
		"http:POST /api/v1/orgs/{org}/projects/{project}/access-policies":                                                     {Events: []string{"EventAccessPolicyChanged"}},
		"http:POST /api/v1/orgs/{org}/projects/{project}/approval-policies":                                                   {Events: []string{"EventApprovalPolicyChanged"}},
		"http:POST /api/v1/orgs/{org}/projects/{project}/environments/clone":                                                  {Ops: []string{"OpValueList", "OpValueCopySource", "OpValueCopyDestination", "OpValueCopyDestinationConfig", "OpValuePublish"}},
		"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/access-requests":                          {Events: []string{"EventAccessRequested"}},
		"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/access-requests/emergency":                {Events: []string{"EventAccessBypassed"}},
		"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/access-requests/{accessRequest}/cancel":   {Events: []string{"EventAccessCancelled"}},
		"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/access-requests/{accessRequest}/revoke":   {Events: []string{"EventAccessRevoked"}},
		"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/access-requests/{accessRequest}/vote":     {Events: []string{"EventAccessVoted", "EventAccessGranted", "EventAccessInvalidated"}},
		"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/approval-requests/{approvalRequest}/vote": {Events: []string{"EventApprovalVoted", "EventApprovalInvalidated"}},
		// Delivery-target reports (#788) ride the fetch credential, so a federated
		// presentation carries the same pre-authentication refusals. The list is a
		// human read.
		"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/delivery-targets":           {Events: []string{"EventFederationRefused", "EventJWKSRefreshFailed"}},
		"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/delivery-targets/tombstone": {Events: []string{"EventFederationRefused", "EventJWKSRefreshFailed"}},
		"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/delivery/offline-records":   {Events: []string{"EventFederationRefused", "EventJWKSRefreshFailed"}},
		"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/pins":                       {Ops: []string{"OpPinSetHistory"}, Events: []string{"EventPinCreated", "EventPinReassigned", "EventPinRenewed", "EventPinExpiryRefused"}},
		// Drafts, publishing and revisions (#51). Every one is tenant-class: an
		// environment the caller may not reach answers byte-identically to one that
		// is not there, history included.
		// Drafts, publishing and revisions (#51). A publish authorizes
		// value.publish once per AFFECTED environment, which is the addressed one
		// plus any other environment the selected versions -- or key-group closure
		// -- reach.
		"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/publish":                       {Events: []string{"EventRevisionPublished"}},
		"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/revisions/diff/reveal":         {Ops: []string{"OpValueExport", "OpValueExportRevealHistory"}, Events: []string{"EventValueRevealed"}, Index: 1},
		"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/revisions/{revision}/rollback": {Ops: []string{"OpRevisionRestoreHistory", "OpRevisionRestoreCurrent"}, Events: []string{"EventRevisionRestoreStaged", "EventValueStaged"}},
		"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/values/export":                 {Ops: []string{"OpValueExport", "OpValueExportRevealHistory"}, Events: []string{"EventValueRevealed"}, Index: 1},
		// A manifest-carrying import re-evaluates phase 1's read op for every
		// environment the manifest names, inside its own transaction, so this route
		// genuinely reaches both operations at runtime.
		"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/values/import": {Ops: []string{"OpImportPresence"}},
		// reencrypt of a PROJECT is tenant-class: it reads and writes the project's
		// own tenant-owned rows, so its refusal mimics that project's nonexistence.
		"http:POST /api/v1/orgs/{org}/projects/{project}/reencrypt":                 {Events: []string{"EventReencryptCompleted"}},
		"http:POST /api/v1/orgs/{org}/projects/{project}/values/copy":               {Ops: []string{"OpValueList", "OpValueCopySource", "OpValueCopyDestinationConfig", "OpValuePublish"}, Index: 2},
		"http:POST /api/v1/orgs/{org}/projects/{project}/values/declare":            {Ops: []string{"OpValuePublish"}},
		"http:PUT /api/v1/orgs/{org}/projects/{project}/access-policies/{policy}":   {Events: []string{"EventAccessPolicyChanged"}},
		"http:PUT /api/v1/orgs/{org}/projects/{project}/approval-policies/{policy}": {Events: []string{"EventApprovalPolicyChanged"}},
		// Drafts, publishing and revisions (#51). Staging rides the value routes'
		// existing entries; these routes are the ones that emit something new.
		"http:PUT /api/v1/orgs/{org}/projects/{project}/environments/{environment}/values/{key}": {Events: []string{"EventValueStaged"}},
		"http:PUT /api/v1/orgs/{org}/projects/{project}/keys/{key}/classification":               {Ops: []string{"OpKeyDeclassify"}},
		// These two routes REACH a second operation at runtime - the reveal gate
		// the schema-model ADR puts in front of a value-dependent rule change on a
		// `secret` key, and in front of declassification. Both are listed for the
		// same reason credential-reset lists its pair: the linkage must record
		// every operation a route can reach, or the registry describes an
		// authorization posture the router does not have.
		"http:PUT /api/v1/orgs/{org}/projects/{project}/keys/{key}/declaration": {Ops: []string{"OpKeySecretRuleChange"}},
	},
	Entries: map[string]wireRow{
		"cli:" + multicall.RootKeyStage:          {Class: "ClassSystem"},
		"cli:" + multicall.TLSStage:              {Class: "ClassSystem"},
		"cli:" + multicall.RolloutAuthorityStage: {Class: "ClassSystem"},
		"cli:" + multicall.ImportSubprocess:      {Class: "ClassTenant"},
		"cli:operator":                           {Class: "ClassSystem"},
		"cli:updater":                            {Class: "ClassSystem"},
		"cli:upgrade":                            {Class: "ClassSystem"},
		"cli:config-rollout":                     {Class: "ClassSystem"},
		"cli:--version":                          {Class: "ClassUnauthenticated"},
		"cli:--upgrade-bundle-formats":           {Class: "ClassUnauthenticated"},
		"cli:about":                              {Class: "ClassUnauthenticated"},
		// `access` reaches BOTH classes; the org/project/env grant routes are
		// tenant-class, the instance-scope ones are instance-class. It is
		// classified instance because that is the WEAKER probe contract of the
		// two: a verb that can reach a grant-refusal route must not ride in under
		// the uniform-nonexistent contract it does not always satisfy. The
		// per-route classification above is the authoritative one either way.
		"cli:access":  {Class: "ClassInstance"},
		"cli:account": {Class: "ClassUnauthenticated"},
		// `adapter` reaches only project-owned adapter and target routes. Dynamic
		// affected-environment checks happen behind those tenant routes.
		"cli:adapter": {Class: "ClassTenant"},
		// `hikyo admin create`: the bootstrap member of the closed local-authority
		// exception set. System class, whose probe contract is network
		// unreachability; the totality invariant asserts it by finding no HTTP
		// route, which is the guarantee that matters here: a first-administrator
		// endpoint reachable from the network is the trust-on-first-use race the
		// ADR rejected outright.
		// The bootstrap verb, running on the server's own host under local
		// authority. Its mint is audited including the DELIVERY MODE, because a
		// token that reached a log shipper is a different event from one written
		// to a root-owned file. `hikyo admin reset-credential` (#54 break-glass) is the
		// same local-authority verb group and emits the reset issuance beside the mint.
		// `hikyo admin grant` (#55 break-glass) joins the same local-authority verb
		// group: a recovery grant issued on the host, with no network route.
		"cli:admin": {Class: "ClassSystem", Events: []string{"EventAuthAuthorityMinted", "EventAuthCredentialResetIssued", "EventBreakGlassGrant", "EventPrivacySubjectCorrected", "EventPrivacySubjectExported", "EventPrivacySubjectRestricted", "EventPrivacySubjectReleased", "EventPrivacySubjectErased"}},
		"job:converge": {Class: "ClassSystem", Ops: []string{"OpAdapterPush"}, Events: []string{
			"EventAdapterPushIntent", "EventAdapterPushOutcome", "EventAdapterKeyDelivered", "EventAdapterAbort",
		}},
		"job:scrub": {Class: "ClassSystem", Ops: []string{"OpAdapterPush"}, Events: []string{
			"EventAdapterPushIntent", "EventAdapterPushOutcome", "EventAdapterScrub",
		}},
		"job:activate": {Class: "ClassSystem", Ops: []string{"OpAdapterConfigure"}, Events: []string{
			"EventAdapterPushIntent", "EventAdapterPushOutcome",
		}},
		"cli:approval": {Class: "ClassTenant"},
		"cli:backup":   {Class: "ClassSystem", Events: []string{"EventBackupExported", "EventBackupExportSkipped"}},
		"cli:cert":     {Class: "ClassTenant"},
		"cli:compose":  {Class: "ClassTenant"},
		// `context` is entirely client-local: the trust store and the named
		// contexts live on this box and reach no server.
		"cli:context": {Class: "ClassUnauthenticated"},
		// `definitions` (#70) reaches only the tenant-scoped export/check/plan/apply
		// routes; server operations own every authorization and audit decision.
		"cli:definitions": {Class: "ClassTenant"},
		"cli:doctor":      {Class: "ClassInstance"},
		// Dynamic secrets (#147): both verbs are client transport for the
		// tenant-scoped provider and lease routes and nothing wider.
		"cli:dynamic-provider": {Class: "ClassTenant"},
		"cli:env":              {Class: "ClassTenant"},
		// `hikyo backup` and `hikyo restore` (#76): the operator lifecycle, on the
		// server's own host. System class, and the probe contract that matters is
		// exactly the one the totality invariant asserts by finding no HTTP route.
		// A restore endpoint reachable from the network would be an instance
		// replacement one request away, and the reconciliation that follows a
		// restore is unreachable by any other means anyway, because a restore
		// leaves no principal able to authorize anything.
		// The operator lifecycle (#76). `backup` writes its export record;
		// `restore` writes the reconstruction and one event per principal the
		// operator reconciles afterwards.
		"cli:escrow":    {Class: "ClassSystem", Events: []string{"EventRootEscrowVerified"}},
		"cli:file-sync": {Class: "ClassTenant"},
		// Generic file destinations (#164). `file-target` reaches only the
		// project-scoped file-target routes; `file-sync` reaches the tenant-scoped
		// delivery route, its offline-records reconciliation and the
		// environment-scoped report route, and nothing wider.
		"cli:file-target": {Class: "ClassTenant"},
		"cli:folder":      {Class: "ClassTenant"},
		// `import` (#68) reaches the tenant-scoped phase-1 presence route and the
		// tenant-scoped phase-2 import route, and nothing else. Its class flipped
		// off ClassStub in the same change that registered its operations; the
		// totality invariant refuses a stub verb that already has operations, which
		// is exactly the "implementation rides in on a stale class" case.
		"cli:import":          {Class: "ClassTenant"},
		"cli:instance-config": {Class: "ClassInstance"},
		// `key` reaches the catalogue and the group routes, all tenant-class.
		"cli:key":   {Class: "ClassTenant"},
		"cli:lease": {Class: "ClassTenant"},
		// Client verbs that reach the server. Their probe contract is the HTTP
		// route they call, classified above; the verb itself carries the class of
		// what it reaches, so a verb whose class is still ClassStub cannot
		// silently start making requests.
		"cli:login":  {Class: "ClassUnauthenticated"},
		"cli:logout": {Class: "ClassUnauthenticated"},
		// The automatic pre-migration export (ops spec section 11) rides the two
		// entry points that can apply a migration, so both of them now have an
		// auditable act at the operation surface and leave the exemption fixture:
		// an export taken (or LOUDLY SKIPPED for want of recipients) immediately
		// before a schema change is the record that says whether there is
		// anything to fall back to.
		"cli:migrate": {Class: "ClassSystem", Events: []string{"EventBackupExported", "EventBackupExportSkipped"}},
		"cli:org":     {Class: "ClassInstance"},
		"cli:pin":     {Class: "ClassTenant"},
		// Private PKI (#154): `pki` drives the instance issuer and profile
		// routes, `cert` the environment certificate routes.
		"cli:pki":     {Class: "ClassInstance"},
		"cli:project": {Class: "ClassTenant"},
		// `project-settings` reaches only the two environment-scoped routes.
		"cli:project-settings": {Class: "ClassTenant"},
		// `reencrypt` reaches both the instance route and the project route; like
		// `access`, the verb takes the instance class.
		"cli:reencrypt": {Class: "ClassInstance"},
		// `org` still reaches the instance-scoped create/list as well as the
		// tenant-scoped by-id routes, so it carries the wider of the two classes:
		// a verb whose class understated its reach would let an instance-scoped
		// call ride in under a tenant probe contract. `project`, `env` and `folder`
		// reach tenant routes exclusively.
		// Multi-instance (#71). Both families are instance-scoped: the viewing
		// side's remotes are instance configuration read under instance-directory,
		// and the serving side's connection credentials are custody under
		// instance-config.
		"cli:remote":            {Class: "ClassInstance"},
		"cli:remote-credential": {Class: "ClassInstance"},
		"cli:restore":           {Class: "ClassSystem", Events: []string{"EventRestoreCompleted", "EventRestorePrincipalReconciled", "EventRestoreDrillCompleted"}},
		// `revision` reaches the two tenant-scoped history routes (#51). It
		// discloses no value: history is lineage, and the one verb that reads a
		// snapshot's values is `values export`.
		"cli:revision":          {Class: "ClassTenant"},
		"cli:rotate-dek":        {Class: "ClassInstance"},
		"cli:rotate-master-key": {Class: "ClassInstance"},
		"cli:rotate-root-key":   {Class: "ClassInstance"},
		// `rotate-scanning-key` reaches one instance-scoped route: the scanning
		// fingerprint key belongs to the instance, same shape as rotate-token-key.
		"cli:rotate-scanning-key": {Class: "ClassInstance"},
		// `rotate-token-key` reaches one instance-scoped route: the root token key
		// belongs to the instance, so there is no tenant object whose nonexistence
		// a refusal could mimic. `rotate-dek` reaches the DEK rotation route on the
		// same instance-scoped grounds.
		"cli:rotate-token-key": {Class: "ClassInstance"},
		// The Compose delivery verbs (#63). `run` and `compose` both reach the
		// tenant-scoped delivery routes (GET .../delivery and its offline-records
		// reconciliation POST) and nothing wider, so both carry ClassTenant: a
		// caller who cannot read the environment gets what an environment that does
		// not exist gives. `compose` dispatches render|sync|doctor internally; the
		// class is the verb's, and every sub-verb reaches only those two routes.
		"cli:run": {Class: "ClassTenant"},
		// `sa` reaches the project-scoped identity routes, all tenant-class:
		// a project whose identities the caller may not administer answers
		// exactly like a project that is not there. The instance credential
		// policy rides `instance-config`, not this verb.
		"cli:sa": {Class: "ClassTenant"},
		// `scan` (#153) is entirely client-local: it reads local files and the
		// local Git repository and reaches no server, like `definitions scaffold`.
		"cli:scan": {Class: "ClassUnauthenticated"},
		// `scim` reaches ONLY tenant-class routes: every SCIM administration
		// operation is org-addressed, so a binding the caller may not reach answers
		// exactly like one that is not there. The wire routes are tenant-class too,
		// but no CLI verb reaches them; they are the identity provider's.
		"cli:scim": {Class: "ClassTenant"},
		// Process entry points with no principal: boot (server) and migration.
		// Their system-proof mint sites are enumerated in systemSites; the probe
		// contract is network unreachability, which the totality check asserts
		// by finding no HTTP route for them.
		"cli:server": {Class: "ClassSystem", Events: []string{"EventBackupExported", "EventBackupExportSkipped"}},
		// SSH user certificates (#155): client transport for the tenant-scoped
		// CA, profile and certificate routes and nothing wider.
		"cli:ssh-ca":      {Class: "ClassTenant"},
		"cli:ssh-cert":    {Class: "ClassTenant"},
		"cli:ssh-profile": {Class: "ClassTenant"},
		"cli:transit":     {Class: "ClassTenant"},
		// `update` reads and writes only client-local public release metadata.
		"cli:update": {Class: "ClassUnauthenticated"},
		// `values` reaches only the tenant-scoped value routes.
		"cli:values": {Class: "ClassTenant"},
		// Local product-information commands print build metadata: no principal,
		// no server, no store; the pre-auth contract is trivially total.
		"cli:version":                   {Class: "ClassUnauthenticated"},
		"cli:welcome":                   {Class: "ClassUnauthenticated"},
		"cli:whoami":                    {Class: "ClassUnauthenticated"},
		"http:GET /healthz":             {Class: "ClassUnauthenticated"},
		"http:GET /metrics":             {Class: "ClassUnauthenticated"},
		"http:GET /readyz":              {Class: "ClassUnauthenticated"},
		"mcp:initialize":                {Class: "ClassUnauthenticated"},
		"mcp:notifications/cancelled":   {Class: "ClassUnauthenticated"},
		"mcp:notifications/initialized": {Class: "ClassUnauthenticated"},
		"mcp:ping":                      {Class: "ClassUnauthenticated"},
		"mcp:server/discover":           {Class: "ClassUnauthenticated"},
		"mcp:tools/list":                {Class: "ClassUnauthenticated"},
	},
}

// Contract-only authority rationale, retained from the former wire table.
// http:GET /api/v1/runtime/status
// The contract surface (#47). Every entry below exists in
// api/openapi.yaml and carries the same class there under
// `x-hikyo-class`; api.TestContractClassesMatchTheWireRegistry fails the
// build if the two ever disagree, so the document cannot describe an
// authorization posture the router does not have.
//
// Identity-protocol endpoints are unauthenticated-class: their probe
// contract is enumeration uniformity; no pre-authentication path may
// distinguish an existing account, session or authority from a missing
// one. `logout` and `whoami` take an artifact but are classified here
// too, because an unresolvable artifact is exactly the case they must not
// distinguish.

// http:GET /api/v1/me/orgs
// The navigation surface (#56). Self-scoped like whoami and the identity
// list: it projects the caller's OWN grant rows onto the organisations
// they name, reaches no chokepoint operation and can disclose nothing the
// caller does not already hold. Its probe contract is therefore
// enumeration uniformity, not tenancy: an unresolvable session must be
// indistinguishable from one whose grants name no org.

// http:GET /api/v1/auth/methods
// A successful consume mints a recovery-issued credential-establishment
// authority; the authority coming into existence is its own record.
// Pre-auth like login: a crossed per-account backoff threshold is its
// own event, emitted directly by recordThrottleCrossing.
// OIDC (#54). Login/callback are pre-auth; link/reauth take a session but an
// unresolvable one is exactly the case they must not distinguish, so all are
// unauthenticated-class (enumeration uniformity). methods is public
// discovery. Provider administration is instance-config (below).

// http:GET /api/v1/instance/saml-providers
// SAML provider administration (#72), under the same instance-config atom.

// http:GET /api/v1/orgs/{org}/scim-bindings
// SCIM provisioning (#73). Every route is tenant-class at org depth: a
// binding a caller may not reach answers byte-identically to one that is
// not there, which is what keeps the mount from being a cross-org oracle.
// The wire routes are protocol paths (the same closed exception class as
// authentication ceremonies) and are parity-exempt, but they are
// NOT unauthenticated: each one presents a provisioning credential.

// http:GET /api/v1/instance/oidc-providers
// OIDC provider administration (#54), instance-config.

// http:GET /api/v1/orgs
// Org creation and enumeration are instance-scoped: the probe contract is
// grant refusal, not tenancy, because no tenant object exists whose
// nonexistence could be mimicked; a create has no parent tenant and a
// list of every org spans all of them.

// http:GET /api/v1/orgs/{org}
// The hierarchy surface (#48). EVERY by-id route is tenant-class, org
// included: mvp-boundary C1 requires the uniform nonexistent shape at each
// level, and an org route that answered 403 on grant refusal would leak the
// existence of every org an operator cannot reach.

// http:GET /api/v1/instance/grants
// The access surface (#55): grants, role templates, membership inspection
// and the two `project-settings` knobs. One entry per addressed depth,
// because the formula differs per depth; the instance ones are
// instance-class (grant refusal, no tenant object to mimic), every other
// one is tenant-class (uniform nonexistent).
// The access surface (#55). Each route reaches exactly one operation: the
// depth is in the path, so there is no runtime dispatch between formulas.

// http:GET /api/v1/orgs/{org}/rules
// Member access rules: the org route carries the rule's projects in the
// body and the service authorizes project-depth manage-members on each.

// http:POST /api/v1/orgs/{org}/invitations
// Member invitation (#568): one route per depth, like grant.create.

// http:GET /api/v1/orgs/{org}/projects/{project}/service-accounts
// Machine identities (#61). Tenant-class at project depth: an identity
// surface a caller may not administer answers exactly like a project
// that is not there. The instance lifetime controls are instance-class
// under `instance-config`, like every other instance knob.
// Machine identities (#61). One route, one operation: the depth is in the
// path, so there is no runtime dispatch between formulas.

// http:GET /api/v1/instance/directory
// Multi-instance (#71). The instance surfaces are instance-class; the
// handoff family joins the auth-protocol exception class and is
// unauthenticated-class for its probe contract, exactly as the OIDC and
// SAML transports are. The self-scoped session surface is
// unauthenticated-class for the reason /api/v1/me/orgs is: enumeration
// uniformity, not tenancy.
// Multi-instance (#71). The handoff routes reach no authz operation: they
// are pre-authentication by construction, and their audit obligation is
// discharged through Events like every other identity-protocol
// endpoint. The session routes reach none for the same reason
// /api/v1/me/orgs reaches none: they are self-scoped projections.

// http:GET /api/v1/instance/federation-issuers
// OIDC federation (#62). Issuer configuration is instance-class under
// `instance-config`, the same siting as OIDC and SAML provider
// administration, and for the same reason #16 gave: an org-scoped issuer
// would let an org admin add a provider and mint identities authenticating
// into the instance.
// OIDC federation (#62). One route, one operation.

// http:POST /api/v1/orgs/{org}/projects/{project}/service-accounts/{serviceAccount}/bindings
// A binding is a credential row, so it is created beside the credentials and
// listed and revoked THROUGH them. There is no PUT and no PATCH: bindings
// are immutable, and a change is a replacement mint through this same POST
// naming the predecessor it supersedes.

// http:GET /api/v1/orgs/{org}/projects/{project}/keys
// The key catalogue (#49). Every route is tenant-class at project depth:
// a key is declared once per project, and a key the caller cannot reach
// answers byte-identically to one that is not there, including the two
// reveal-gated routes, whose refusal must be indistinguishable or the gate
// itself becomes the one-bit oracle it exists to close.

// http:GET /api/v1/orgs/{org}/projects/{project}/definitions/export
// Definitions Git flow (#70). Every route is project-addressed tenant
// material; grant refusal and a missing project/plan share one wire shape.

// http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/values
// The flat value model (#50). Tenant-class throughout: a value the caller
// may not reach answers exactly like one that is not there.
// The flat value model (#50). Three routes reach TWO operations each,
// following the credential-reset precedent: a route that reaches a second
// operation at runtime must say so, or the registry describes an
// authorization posture the router does not have.
//
//   - declare authorizes value.set once PER DESTINATION environment;
//   - copy authorizes the source leg and each destination leg, and which
//     destination operation it reaches depends on the CLASSIFICATION of the
//     material moving (see the registry);
//   - clone is an environment create that then runs the copy legs.

// http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/values/occurrences
// The import path (#68). Tenant-class like every other value route: an
// environment the caller may not read answers exactly like one that is not
// there, and phase 1's presence read is precisely a read of that
// environment.

// http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/revisions/diff
// Revision comparison has the export read boundary. The per-key disclosure
// checks current or historical reveal independently for each retained side.

// http:GET /api/v1/orgs/{org}/projects/{project}/adapters
// Deployment adapters (#65). Every project and target surface is tenant
// class; dynamic reveal/reauth checks over the adapter's environment set are
// added by the service after this route-level classification.
// Deployment adapters (#65). Dynamic reveal and reauthentication checks
// refine these operations in service, but every route still names the
// static proof-bearing operation whose audit family it reaches.

// http:GET /api/v1/orgs/{org}/projects/{project}/dynamic-providers
// Dynamic secrets (#147).

// http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/ssh-cas
// SSH user certificates (#155).

// http:GET /api/v1/instance/pki/issuers
// Private PKI (#154).

// http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/transit-keys
// Transit (#156).

// Adapter worker jobs carry explicit system classifications and audit linkage.
// SSE emit sites remain empty; the first entry must arrive with its probe class.
