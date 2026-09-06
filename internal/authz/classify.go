package authz

import (
	"fmt"
	"maps"

	"github.com/Hikyo-Org/hikyo/internal/audit"
)

// The wire registry: the probe classification for every non-operation entry
// point (tenant-isolation ADR, invariant 1). Service operations carry their
// class in the operation registry; everything else that can be reached from
// outside — HTTP routes, CLI verbs, background job types, SSE emit sites —
// is classified here. The classification-totality invariant enumerates the
// actual router, the actual CLI verb table and the (currently empty) job and
// SSE registries against this table: an unclassified entry point fails the
// build, and a stale entry here fails it too.

// ClassStub marks a CLI verb that is not an operation yet: it refuses (exit
// 2), reaches no server and no store, and has no entry in the operation
// registry — the totality check enforces exactly that. It is deliberately
// not one of the ADR's four probe classes: classifying an unimplemented
// verb as tenant-scoped or unauthenticated now would let the eventual
// implementation ride in on a stale class without ever meeting its probe
// contract. When a verb's ticket lands, its class here changes and the
// matching probes must exist.
const ClassStub Class = -1

// wireEntry is the single owner of one externally reachable entry point's
// classification, operation linkage, and direct audit-event linkage. Keeping
// all three facts in one row makes contradictions local and reviewable instead
// of spreading them across parallel maps.
type wireEntry struct {
	Class  Class
	Ops    []Operation
	Events []audit.EventType
}

// newWireRegistry validates and clones the wire table. Production access goes
// through projections, so callers cannot mutate installed rows.
func newWireRegistry(table map[string]wireEntry) (map[string]wireEntry, error) {
	entries := make(map[string]wireEntry, len(table))
	for name, entry := range table {
		if name == "" {
			return nil, fmt.Errorf("authz wire registry: empty entry key")
		}
		switch entry.Class {
		case ClassTenant, ClassInstance, ClassUnauthenticated, ClassSystem:
		case ClassStub:
			if len(entry.Ops) > 0 || len(entry.Events) > 0 {
				return nil, fmt.Errorf("authz wire registry: stub entry %q carries operation or event linkage", name)
			}
		default:
			return nil, fmt.Errorf("authz wire registry: entry %q has invalid class %d", name, entry.Class)
		}

		seenOps := make(map[Operation]bool, len(entry.Ops))
		for _, op := range entry.Ops {
			if _, known := registry.ops[op]; !known {
				return nil, fmt.Errorf("authz wire registry: entry %q names unknown operation %q", name, op)
			}
			if seenOps[op] {
				return nil, fmt.Errorf("authz wire registry: entry %q repeats operation %q", name, op)
			}
			seenOps[op] = true
		}

		seenEvents := make(map[audit.EventType]bool, len(entry.Events))
		for _, event := range entry.Events {
			if _, known := audit.Spec(event); !known {
				return nil, fmt.Errorf("authz wire registry: entry %q names unknown event %q", name, event)
			}
			if seenEvents[event] {
				return nil, fmt.Errorf("authz wire registry: entry %q repeats event %q", name, event)
			}
			seenEvents[event] = true
		}

		entry.Ops = append([]Operation(nil), entry.Ops...)
		entry.Events = append([]audit.EventType(nil), entry.Events...)
		entries[name] = entry
	}
	return entries, nil
}

func mustNewWireRegistry(table map[string]wireEntry) map[string]wireEntry {
	entries, err := newWireRegistry(table)
	if err != nil {
		panic(err)
	}
	return entries
}

var wireRegistry = mustNewWireRegistry(map[string]wireEntry{
	"http:GET /api/v1/instance/config":            {Class: ClassInstance, Ops: []Operation{OpSelfConfigStatus}},
	"http:GET /api/v1/instance/config/adoption":   {Class: ClassInstance, Ops: []Operation{OpSelfConfigPreview}},
	"http:POST /api/v1/instance/config/adoption":  {Class: ClassInstance, Ops: []Operation{OpSelfConfigAdopt, OpSelfConfigProvisionProject}},
	"http:POST /api/v1/instance/config/apply":     {Class: ClassTenant, Ops: []Operation{OpSelfConfigApply}},
	"http:POST /api/v1/instance/config/mail/test": {Class: ClassTenant, Ops: []Operation{OpSelfConfigTest}},
	"http:GET /healthz":                           {Class: ClassUnauthenticated},
	"http:GET /metrics":                           {Class: ClassUnauthenticated},
	"http:GET /readyz":                            {Class: ClassUnauthenticated},
	"mcp:server/discover":                         {Class: ClassUnauthenticated},
	"mcp:tools/list":                              {Class: ClassUnauthenticated},

	// The contract surface (#47). Every entry below exists in
	// api/openapi.yaml and carries the same class there under
	// `x-hikyo-class`; api.TestContractClassesMatchTheWireRegistry fails the
	// build if the two ever disagree, so the document cannot describe an
	// authorization posture the router does not have.
	//
	// Identity-protocol endpoints are unauthenticated-class: their probe
	// contract is enumeration uniformity — no pre-authentication path may
	// distinguish an existing account, session or authority from a missing
	// one. `logout` and `whoami` take an artifact but are classified here
	// too, because an unresolvable artifact is exactly the case they must not
	// distinguish.
	"http:GET /api/v1/meta": {Class: ClassUnauthenticated},
	"http:POST /api/v1/auth/credential/establish": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventAuthCredentialEstablished,
		audit.EventAuthAuthorityRefused,
	}},
	"http:POST /api/v1/auth/local/login": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventAuthLogin,
		audit.EventAuthSessionCreated,
		audit.EventAuthThrottleCrossed,
	}},
	"http:POST /api/v1/auth/logout": {Class: ClassUnauthenticated, Events: []audit.EventType{audit.EventAuthLogout}},
	"http:GET /api/v1/auth/whoami":  {Class: ClassUnauthenticated},

	// The navigation surface (#56). Self-scoped like whoami and the identity
	// list: it projects the caller's OWN grant rows onto the organisations
	// they name, reaches no chokepoint operation and can disclose nothing the
	// caller does not already hold. Its probe contract is therefore
	// enumeration uniformity — an unresolvable session must be
	// indistinguishable from one whose grants name no org — not tenancy.
	"http:GET /api/v1/me/orgs": {Class: ClassUnauthenticated},

	// Factor endpoints (#54). Unauthenticated-class like logout/whoami: they
	// take a session but an unresolvable one is exactly the case they must not
	// distinguish, so their probe contract is enumeration uniformity, not
	// tenancy. `recovery/begin` is fully pre-auth. None reaches an authz
	// operation — the account-security mutations resolve and rotate the acting
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
	// a crossed threshold is an event it can emit — declared here so the
	// audit-completeness contract covers it.
	"http:POST /api/v1/auth/totp/enrol/start": {Class: ClassUnauthenticated, Events: []audit.EventType{audit.EventAuthThrottleCrossed}},
	"http:POST /api/v1/auth/totp/enrol/confirm": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventAuthFactorEnrolled,
		audit.EventAuthSessionCreated,
		audit.EventAuthThrottleCrossed,
	}},
	"http:POST /api/v1/auth/totp/step-up": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventAuthReauthenticated,
		audit.EventAuthThrottleCrossed,
	}},
	"http:GET /api/v1/auth/totp": {Class: ClassUnauthenticated},
	"http:DELETE /api/v1/auth/totp": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventAuthFactorRemoved,
		audit.EventAuthSessionCreated,
		audit.EventAuthThrottleCrossed,
	}},
	"http:POST /api/v1/auth/recovery-codes/regenerate": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventAuthRecoveryCodesGenerated,
		audit.EventAuthSessionCreated,
		audit.EventAuthThrottleCrossed,
	}},
	"http:POST /api/v1/auth/recovery/begin": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventAuthRecoveryCodeConsumed,
		// A successful consume mints a recovery-issued credential-establishment
		// authority; the authority coming into existence is its own record.
		audit.EventAuthAuthorityMinted,
		// Pre-auth like login: a crossed per-account backoff threshold is its
		// own event, emitted directly by recordThrottleCrossing.
		audit.EventAuthThrottleCrossed,
	}},

	// OIDC (#54). Login/callback are pre-auth; link/reauth take a session but an
	// unresolvable one is exactly the case they must not distinguish, so all are
	// unauthenticated-class (enumeration uniformity). methods is public
	// discovery. Provider administration is instance-config (below).
	"http:GET /api/v1/auth/methods": {Class: ClassUnauthenticated},
	// whoami resolves a session and reports it. It writes nothing and its
	// result duplicates what the login event already recorded, so it is the
	// one auth path with no event of its own — pinned in the exemption
	// fixture with that reason rather than silently absent.
	// OIDC (#54). start emits only a throttle crossing directly; the callback
	// is where a login/link/reauth lands, so it carries the family of outcomes
	// (login success, refusal by cause, link, the reissued/rotated session,
	// reauth). link start mirrors start; unlink emits the unlink plus the
	// reissued session. Provider administration is operation-modeled (Ops).
	"http:POST /api/v1/auth/oidc/{provider}/start": {Class: ClassUnauthenticated, Events: []audit.EventType{audit.EventAuthThrottleCrossed}},
	"http:GET /api/v1/auth/oidc/{provider}/callback": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventOIDCLogin,
		audit.EventOIDCRefused,
		audit.EventIdentityLinked,
		audit.EventAuthSessionCreated,
		audit.EventAuthReauthenticated,
		audit.EventAuthThrottleCrossed,
	}},
	"http:GET /api/v1/auth/identities":       {Class: ClassUnauthenticated},
	"http:POST /api/v1/auth/identities/link": {Class: ClassUnauthenticated, Events: []audit.EventType{audit.EventAuthThrottleCrossed}},
	"http:DELETE /api/v1/auth/identities/{id}": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventIdentityUnlinked,
		audit.EventAuthSessionCreated,
		audit.EventAuthThrottleCrossed,
	}},

	// SAML SP (#72). Start and ACS are purpose-polymorphic identity-protocol
	// endpoints: login is pre-auth, while link/reauth bind an existing session;
	// enumeration uniformity is therefore their probe contract. Metadata is
	// documentation-class public material under pre-auth admission. Provider
	// administration is instance-config.
	"http:POST /api/v1/auth/saml/{provider}/start": {Class: ClassUnauthenticated, Events: []audit.EventType{audit.EventAuthThrottleCrossed}},
	"http:POST /api/v1/auth/saml/{provider}/acs": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventSAMLLogin,
		audit.EventSAMLReauth,
		audit.EventIdentityLinked,
		audit.EventAuthSessionCreated,
		audit.EventAuthThrottleCrossed,
	}},
	"http:GET /api/v1/auth/saml/{provider}/metadata": {Class: ClassUnauthenticated},
	// SAML provider administration (#72), under the same instance-config atom.
	"http:GET /api/v1/instance/saml-providers":                          {Class: ClassInstance, Ops: []Operation{OpSAMLProviderList}},
	"http:GET /api/v1/instance/retention-health":                        {Class: ClassInstance, Ops: []Operation{OpRetentionHealthRead}},
	"http:GET /api/v1/instance/update-status":                           {Class: ClassInstance, Ops: []Operation{OpUpdateStatusRead}},
	"http:POST /api/v1/instance/update":                                 {Class: ClassInstance, Ops: []Operation{OpUpdateRequest}},
	"http:GET /api/v1/instance/updates/{job}":                           {Class: ClassInstance, Ops: []Operation{OpUpdateJobRead}},
	"http:GET /api/v1/instance/saml-providers/{slug}":                   {Class: ClassInstance, Ops: []Operation{OpSAMLProviderGet}},
	"http:PUT /api/v1/instance/saml-providers/{slug}":                   {Class: ClassInstance, Ops: []Operation{OpSAMLProviderPut}},
	"http:PATCH /api/v1/instance/saml-providers/{slug}":                 {Class: ClassInstance, Ops: []Operation{OpSAMLProviderPatch}},
	"http:DELETE /api/v1/instance/saml-providers/{slug}":                {Class: ClassInstance, Ops: []Operation{OpSAMLProviderDelete}},
	"http:POST /api/v1/instance/saml-providers/{slug}/refresh-metadata": {Class: ClassInstance, Ops: []Operation{OpSAMLProviderRefreshMetadata}},
	"http:GET /api/v1/instance/saml-sp-keys":                            {Class: ClassInstance, Ops: []Operation{OpSAMLSPKeyList}},

	// SCIM provisioning (#73). Every route is tenant-class at org depth: a
	// binding a caller may not reach answers byte-identically to one that is
	// not there, which is what keeps the mount from being a cross-org oracle.
	// The wire routes are protocol paths — the same closed exception class the
	// authentication ceremonies belong to — and are parity-exempt, but they are
	// NOT unauthenticated: each one presents a provisioning credential.
	"http:GET /api/v1/orgs/{org}/scim-bindings":                               {Class: ClassTenant, Ops: []Operation{OpSCIMBindingList}},
	"http:POST /api/v1/orgs/{org}/scim-bindings":                              {Class: ClassTenant, Ops: []Operation{OpSCIMBindingCreate}},
	"http:GET /api/v1/orgs/{org}/scim-bindings/{binding}":                     {Class: ClassTenant, Ops: []Operation{OpSCIMBindingGet}},
	"http:DELETE /api/v1/orgs/{org}/scim-bindings/{binding}":                  {Class: ClassTenant, Ops: []Operation{OpSCIMBindingDelete}},
	"http:GET /api/v1/orgs/{org}/scim-bindings/{binding}/mappings":            {Class: ClassTenant, Ops: []Operation{OpSCIMMappingList}},
	"http:POST /api/v1/orgs/{org}/scim-bindings/{binding}/mappings":           {Class: ClassTenant, Ops: []Operation{OpSCIMMappingCreate}},
	"http:PUT /api/v1/orgs/{org}/scim-bindings/{binding}/mappings":            {Class: ClassTenant, Ops: []Operation{OpSCIMMappingUpdate}},
	"http:DELETE /api/v1/orgs/{org}/scim-bindings/{binding}/mappings":         {Class: ClassTenant, Ops: []Operation{OpSCIMMappingDelete}},
	"http:GET /api/v1/orgs/{org}/scim-bindings/{binding}/credentials":         {Class: ClassTenant, Ops: []Operation{OpSCIMCredentialList}},
	"http:POST /api/v1/orgs/{org}/scim-bindings/{binding}/credentials":        {Class: ClassTenant, Ops: []Operation{OpSCIMCredentialMint}},
	"http:GET /api/v1/orgs/{org}/scim-bindings/{binding}/credentials/{id}":    {Class: ClassTenant, Ops: []Operation{OpSCIMCredentialGet}},
	"http:DELETE /api/v1/orgs/{org}/scim-bindings/{binding}/credentials/{id}": {Class: ClassTenant, Ops: []Operation{OpSCIMCredentialRevoke}},
	"http:GET /api/v1/orgs/{org}/scim-bindings/{binding}/directory/users":     {Class: ClassTenant, Ops: []Operation{OpSCIMDirectoryUsers}},
	"http:GET /api/v1/orgs/{org}/scim-bindings/{binding}/directory/groups":    {Class: ClassTenant, Ops: []Operation{OpSCIMDirectoryGroups}},
	// The credential-versus-binding-path mismatch (#73 §8). It is refused
	// BEFORE any operation authorizes — there is no proof and no operation
	// row to hang it on — so like the authentication surface's own events it
	// is declared here, against the mount every wire request enters through.
	//
	// All THREE discovery routes declare it, and they are the only routes that
	// must: their operation (`scim-discovery.read`) declares no events at all
	// (ADR §10 annotates the probe class audited-none-equivalent, pinned in
	// internal/isolation/testdata/audited_exemptions.json), so this is the whole
	// of their audit linkage. Every other wire route inherits an event list from
	// its own operation.
	"http:GET /api/v1/orgs/{org}/scim/v2/{binding}/ServiceProviderConfig": {Class: ClassTenant, Ops: []Operation{OpSCIMDiscovery}, Events: []audit.EventType{
		audit.EventSCIMCredentialRefused,
	}},
	"http:GET /api/v1/orgs/{org}/scim/v2/{binding}/ResourceTypes": {Class: ClassTenant, Ops: []Operation{OpSCIMDiscovery}, Events: []audit.EventType{
		audit.EventSCIMCredentialRefused,
	}},
	"http:GET /api/v1/orgs/{org}/scim/v2/{binding}/Schemas": {Class: ClassTenant, Ops: []Operation{OpSCIMDiscovery}, Events: []audit.EventType{
		audit.EventSCIMCredentialRefused,
	}},
	"http:GET /api/v1/orgs/{org}/scim/v2/{binding}/Users":                     {Class: ClassTenant, Ops: []Operation{OpSCIMUserList}},
	"http:POST /api/v1/orgs/{org}/scim/v2/{binding}/Users":                    {Class: ClassTenant, Ops: []Operation{OpSCIMUserCreate}},
	"http:GET /api/v1/orgs/{org}/scim/v2/{binding}/Users/{id}":                {Class: ClassTenant, Ops: []Operation{OpSCIMUserGet}},
	"http:PUT /api/v1/orgs/{org}/scim/v2/{binding}/Users/{id}":                {Class: ClassTenant, Ops: []Operation{OpSCIMUserReplace}},
	"http:PATCH /api/v1/orgs/{org}/scim/v2/{binding}/Users/{id}":              {Class: ClassTenant, Ops: []Operation{OpSCIMUserPatch}},
	"http:DELETE /api/v1/orgs/{org}/scim/v2/{binding}/Users/{id}":             {Class: ClassTenant, Ops: []Operation{OpSCIMUserDelete}},
	"http:GET /api/v1/orgs/{org}/scim/v2/{binding}/Groups":                    {Class: ClassTenant, Ops: []Operation{OpSCIMGroupList}},
	"http:POST /api/v1/orgs/{org}/scim/v2/{binding}/Groups":                   {Class: ClassTenant, Ops: []Operation{OpSCIMGroupCreate}},
	"http:GET /api/v1/orgs/{org}/scim/v2/{binding}/Groups/{id}":               {Class: ClassTenant, Ops: []Operation{OpSCIMGroupGet}},
	"http:PUT /api/v1/orgs/{org}/scim/v2/{binding}/Groups/{id}":               {Class: ClassTenant, Ops: []Operation{OpSCIMGroupReplace}},
	"http:PATCH /api/v1/orgs/{org}/scim/v2/{binding}/Groups/{id}":             {Class: ClassTenant, Ops: []Operation{OpSCIMGroupPatch}},
	"http:DELETE /api/v1/orgs/{org}/scim/v2/{binding}/Groups/{id}":            {Class: ClassTenant, Ops: []Operation{OpSCIMGroupDelete}},
	"http:POST /api/v1/orgs/{org}/scim/v2/{binding}/Bulk":                     {Class: ClassTenant, Ops: []Operation{OpSCIMUnsupported}},
	"http:GET /api/v1/orgs/{org}/scim/v2/{binding}/Me":                        {Class: ClassTenant, Ops: []Operation{OpSCIMUnsupported}},
	"http:POST /api/v1/orgs/{org}/scim/v2/{binding}/Users/.search":            {Class: ClassTenant, Ops: []Operation{OpSCIMUnsupported}},
	"http:POST /api/v1/orgs/{org}/scim/v2/{binding}/Groups/.search":           {Class: ClassTenant, Ops: []Operation{OpSCIMUnsupported}},
	"http:POST /api/v1/instance/saml-sp-keys/rotate":                          {Class: ClassInstance, Ops: []Operation{OpSAMLSPKeyRotate}},
	"http:DELETE /api/v1/instance/saml-sp-keys/{fingerprint}":                 {Class: ClassInstance, Ops: []Operation{OpSAMLSPKeyRetire}},
	"http:POST /api/v1/instance/saml-sp-keys/{fingerprint}/compromise-retire": {Class: ClassInstance, Ops: []Operation{OpSAMLSPKeyCompromiseRetire}},
	// WebAuthn / passkeys (#54). Enrolment, login, step-up, reauth, removal and
	// the credential inventory. Login is fully pre-auth; the rest take a session
	// but an unresolvable one is exactly the case they must not distinguish, so
	// all are unauthenticated-class (enumeration uniformity). None reaches an
	// authz operation — the mutations resolve and rotate the acting session,
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
	"http:POST /api/v1/auth/webauthn/enrol/start": {Class: ClassUnauthenticated, Events: []audit.EventType{audit.EventAuthThrottleCrossed}},
	"http:POST /api/v1/auth/webauthn/enrol/finish": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventAuthPasskeyAdded,
		audit.EventAuthSessionCreated,
	}},
	"http:POST /api/v1/auth/webauthn/login/start": {Class: ClassUnauthenticated},
	"http:POST /api/v1/auth/webauthn/login/finish": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventAuthLogin,
		audit.EventAuthSessionCreated,
		audit.EventAuthPasskeyCloned,
		audit.EventAuthThrottleCrossed,
	}},
	"http:POST /api/v1/auth/webauthn/step-up/start": {Class: ClassUnauthenticated},
	"http:POST /api/v1/auth/webauthn/step-up/finish": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventAuthReauthenticated,
		audit.EventAuthPasskeyCloned,
	}},
	"http:POST /api/v1/auth/webauthn/reauth/start": {Class: ClassUnauthenticated},
	"http:POST /api/v1/auth/webauthn/reauth/finish": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventAuthReauthenticated,
		audit.EventAuthPasskeyCloned,
	}},
	// The TOTP half of the disclosure ceremony (#58). Unauthenticated-class
	// for the same reason as every other reauth leg: it authenticates a factor
	// rather than acting on a tenant object, and its refusals are uniform.
	"http:POST /api/v1/auth/reauth/totp": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventAuthReauthenticated,
		audit.EventAuthThrottleCrossed,
	}},
	"http:GET /api/v1/auth/webauthn/credentials": {Class: ClassUnauthenticated},
	"http:DELETE /api/v1/auth/webauthn/credentials/{id}": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventAuthPasskeyRemoved,
		audit.EventAuthSessionCreated,
		audit.EventAuthThrottleCrossed,
	}},
	// OIDC provider administration (#54), instance-config.
	"http:GET /api/v1/instance/oidc-providers":           {Class: ClassInstance, Ops: []Operation{OpProviderList}},
	"http:GET /api/v1/instance/oidc-providers/{slug}":    {Class: ClassInstance, Ops: []Operation{OpProviderGet}},
	"http:PUT /api/v1/instance/oidc-providers/{slug}":    {Class: ClassInstance, Ops: []Operation{OpProviderPut}},
	"http:DELETE /api/v1/instance/oidc-providers/{slug}": {Class: ClassInstance, Ops: []Operation{OpProviderDelete}},

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
	// this route reaches CapCredentialReset (MFA-mandatory): the chokepoint —
	// authorize(), which the service calls on the chosen op inside that tx —
	// enforces capability + MFA + assurance. The route keeps its unauthenticated
	// probe class (enumeration uniformity is its dominant contract, reinforced by
	// B2's uniform refusal) and carries no single x-hikyo-operation, since two ops
	// of different classes cannot be named by one contract row; its audit events
	// also ride Events.
	// Credential reset (#54). A successful reset mints a credential-establishment
	// authority (its own record, factors MEDIUM-7) and records the reset issuance
	// naming the tier. See the exception note above for why this route audits here
	// rather than through an operation row.
	"http:POST /api/v1/accounts/{principal}/credential-reset": {Class: ClassUnauthenticated, Ops: []Operation{OpCredentialReset, OpCredentialResetInstance}, Events: []audit.EventType{
		audit.EventAuthCredentialResetIssued,
		audit.EventAuthAuthorityMinted,
	}},

	// Org creation and enumeration are instance-scoped: the probe contract is
	// grant refusal, not tenancy, because no tenant object exists whose
	// nonexistence could be mimicked — a create has no parent tenant and a
	// list of every org spans all of them.
	"http:GET /api/v1/orgs":  {Class: ClassInstance, Ops: []Operation{OpOrgList}},
	"http:POST /api/v1/orgs": {Class: ClassInstance, Ops: []Operation{OpOrgCreate}},

	// The hierarchy surface (#48). EVERY by-id route is tenant-class, org
	// included: mvp-boundary C1 requires the uniform nonexistent shape at each
	// level, and an org route that answered 403 on grant refusal would leak the
	// existence of every org an operator cannot reach.
	"http:GET /api/v1/orgs/{org}":    {Class: ClassTenant, Ops: []Operation{OpOrgGet}},
	"http:PATCH /api/v1/orgs/{org}":  {Class: ClassTenant, Ops: []Operation{OpOrgRename}},
	"http:DELETE /api/v1/orgs/{org}": {Class: ClassTenant, Ops: []Operation{OpOrgDelete}},

	"http:GET /api/v1/orgs/{org}/projects":                     {Class: ClassTenant, Ops: []Operation{OpProjectList}},
	"http:POST /api/v1/orgs/{org}/projects":                    {Class: ClassTenant, Ops: []Operation{OpProjectCreate}},
	"http:GET /api/v1/orgs/{org}/projects/{project}":           {Class: ClassTenant, Ops: []Operation{OpProjectGet}},
	"http:PATCH /api/v1/orgs/{org}/projects/{project}":         {Class: ClassTenant, Ops: []Operation{OpProjectRename}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}":        {Class: ClassTenant, Ops: []Operation{OpProjectDelete}},
	"http:GET /api/v1/orgs/{org}/retention":                    {Class: ClassTenant, Ops: []Operation{OpOrgRetentionRead}},
	"http:PUT /api/v1/orgs/{org}/retention":                    {Class: ClassTenant, Ops: []Operation{OpOrgRetentionUpdate}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/retention": {Class: ClassTenant, Ops: []Operation{OpProjectRetentionRead}},
	"http:PUT /api/v1/orgs/{org}/projects/{project}/retention": {Class: ClassTenant, Ops: []Operation{OpProjectRetentionUpdate}},

	"http:GET /api/v1/orgs/{org}/projects/{project}/environments":                  {Class: ClassTenant, Ops: []Operation{OpEnvList}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments":                 {Class: ClassTenant, Ops: []Operation{OpEnvCreate}},
	"http:PUT /api/v1/orgs/{org}/projects/{project}/environments/order":            {Class: ClassTenant, Ops: []Operation{OpEnvReorder}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}":    {Class: ClassTenant, Ops: []Operation{OpEnvRead}},
	"http:PATCH /api/v1/orgs/{org}/projects/{project}/environments/{environment}":  {Class: ClassTenant, Ops: []Operation{OpEnvRename}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/environments/{environment}": {Class: ClassTenant, Ops: []Operation{OpEnvDelete}},

	"http:GET /api/v1/orgs/{org}/projects/{project}/folders":             {Class: ClassTenant, Ops: []Operation{OpFolderList}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/folders":            {Class: ClassTenant, Ops: []Operation{OpFolderCreate}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/folders/{folder}":    {Class: ClassTenant, Ops: []Operation{OpFolderGet}},
	"http:PATCH /api/v1/orgs/{org}/projects/{project}/folders/{folder}":  {Class: ClassTenant, Ops: []Operation{OpFolderRename}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/folders/{folder}": {Class: ClassTenant, Ops: []Operation{OpFolderDelete}},

	// The access surface (#55): grants, role templates, membership inspection
	// and the two `project-settings` knobs. One entry per addressed depth,
	// because the formula differs per depth — the instance ones are
	// instance-class (grant refusal, no tenant object to mimic), every other
	// one is tenant-class (uniform nonexistent).
	// The access surface (#55). Each route reaches exactly one operation: the
	// depth is in the path, so there is no runtime dispatch between formulas.
	"http:GET /api/v1/instance/grants":             {Class: ClassInstance, Ops: []Operation{OpGrantListInstance}},
	"http:POST /api/v1/instance/grants":            {Class: ClassInstance, Ops: []Operation{OpGrantCreateInstance}},
	"http:DELETE /api/v1/instance/grants":          {Class: ClassInstance, Ops: []Operation{OpGrantRevokeInstance}},
	"http:POST /api/v1/instance/grants/template":   {Class: ClassInstance, Ops: []Operation{OpTemplateApplyInstance}},
	"http:GET /api/v1/orgs/{org}/grants":           {Class: ClassTenant, Ops: []Operation{OpGrantListOrg}},
	"http:POST /api/v1/orgs/{org}/grants":          {Class: ClassTenant, Ops: []Operation{OpGrantCreateOrg}},
	"http:DELETE /api/v1/orgs/{org}/grants":        {Class: ClassTenant, Ops: []Operation{OpGrantRevokeOrg}},
	"http:POST /api/v1/orgs/{org}/grants/template": {Class: ClassTenant, Ops: []Operation{OpTemplateApplyOrg}},
	// Member invitation (#568): one route per depth, like grant.create.
	"http:POST /api/v1/orgs/{org}/invitations": {Class: ClassTenant, Ops: []Operation{OpMemberInviteOrg}},
	"http:POST /api/v1/instance/invitations":   {Class: ClassInstance, Ops: []Operation{OpMemberInviteInstance}},

	// Machine identities (#61). Tenant-class at project depth: an identity
	// surface a caller may not administer answers exactly like a project
	// that is not there. The instance lifetime controls are instance-class
	// under `instance-config`, like every other instance knob.
	// Machine identities (#61). One route, one operation: the depth is in the
	// path, so there is no runtime dispatch between formulas.
	"http:GET /api/v1/orgs/{org}/projects/{project}/service-accounts":                                              {Class: ClassTenant, Ops: []Operation{OpServiceAccountList}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/service-accounts":                                             {Class: ClassTenant, Ops: []Operation{OpServiceAccountCreate}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/service-accounts/{serviceAccount}":                          {Class: ClassTenant, Ops: []Operation{OpServiceAccountDelete}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/service-accounts/{serviceAccount}/credentials":                 {Class: ClassTenant, Ops: []Operation{OpCredentialList}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/service-accounts/{serviceAccount}/credentials":                {Class: ClassTenant, Ops: []Operation{OpCredentialMint}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/service-accounts/{serviceAccount}/credentials/{credential}": {Class: ClassTenant, Ops: []Operation{OpCredentialRevoke}},
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
	"http:GET /api/v1/instance/directory":                   {Class: ClassInstance, Ops: []Operation{OpRemoteDirectoryServe}},
	"http:GET /api/v1/instance/remotes":                     {Class: ClassInstance, Ops: []Operation{OpRemoteList}},
	"http:POST /api/v1/instance/remotes":                    {Class: ClassInstance, Ops: []Operation{OpRemoteAdd}},
	"http:GET /api/v1/instance/remotes/{remote}":            {Class: ClassInstance, Ops: []Operation{OpRemoteShow}},
	"http:PATCH /api/v1/instance/remotes/{remote}":          {Class: ClassInstance, Ops: []Operation{OpRemoteRename}},
	"http:DELETE /api/v1/instance/remotes/{remote}":         {Class: ClassInstance, Ops: []Operation{OpRemoteRemove}},
	"http:GET /api/v1/instance/connections":                 {Class: ClassInstance, Ops: []Operation{OpRemoteCredentialList}},
	"http:POST /api/v1/instance/connections":                {Class: ClassInstance, Ops: []Operation{OpRemoteCredentialCreate}},
	"http:GET /api/v1/instance/connections/{connection}":    {Class: ClassInstance, Ops: []Operation{OpRemoteCredentialShow}},
	"http:DELETE /api/v1/instance/connections/{connection}": {Class: ClassInstance, Ops: []Operation{OpRemoteCredentialRevoke}},
	"http:GET /api/v1/instance/workspace-origins":           {Class: ClassInstance, Ops: []Operation{OpWorkspaceOriginList}},
	"http:POST /api/v1/instance/workspace-origins":          {Class: ClassInstance, Ops: []Operation{OpWorkspaceOriginAdd}},
	"http:DELETE /api/v1/instance/workspace-origins":        {Class: ClassInstance, Ops: []Operation{OpWorkspaceOriginRemove}},
	// Multi-instance handoff (#71). These three carry the workspace tier's
	// pre-authentication audit obligation, which no operation can carry for
	// them: start and redeem authenticate nobody, and a handoff FAILURE
	// predates any session at every stage.
	"http:POST /api/v1/auth/workspace/start":               {Class: ClassUnauthenticated, Events: []audit.EventType{audit.EventRemoteHandoffFailed}},
	"http:GET /api/v1/auth/workspace/transactions/{state}": {Class: ClassUnauthenticated, Events: []audit.EventType{audit.EventRemoteWorkspaceHandoffRead}},
	"http:POST /api/v1/auth/workspace/approve":             {Class: ClassUnauthenticated, Events: []audit.EventType{audit.EventRemoteHandoffFailed}},
	// Redeem carries two shapes, because a redemption is two acts: an
	// establishment ISSUES a workspace session, while a step-up ELEVATES the one
	// it was bound to and mints nothing — the trail records that as the ordinary
	// reauthentication it is, on the session that was elevated.
	"http:POST /api/v1/auth/workspace/redeem": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventRemoteWorkspaceSessionIssued,
		audit.EventAuthReauthenticated,
		audit.EventRemoteHandoffFailed,
	}},
	"http:POST /api/v1/auth/cli-reauth/start":               {Class: ClassUnauthenticated, Events: []audit.EventType{audit.EventAuthCLIReauthHandoff}},
	"http:GET /api/v1/auth/cli-reauth/transactions/{state}": {Class: ClassUnauthenticated, Events: []audit.EventType{audit.EventAuthCLIReauthHandoff}},
	"http:POST /api/v1/auth/cli-reauth/approve":             {Class: ClassUnauthenticated, Events: []audit.EventType{audit.EventAuthCLIReauthHandoff}},
	"http:POST /api/v1/auth/cli-reauth/redeem":              {Class: ClassUnauthenticated, Events: []audit.EventType{audit.EventAuthCLIReauthHandoff}},
	"http:GET /api/v1/me/sessions":                          {Class: ClassUnauthenticated},
	// The self-scoped revoke. A workspace session's death is a #71 event; an
	// ordinary session's is a logout, already the trail's own vocabulary.
	"http:DELETE /api/v1/me/sessions/{session}": {Class: ClassUnauthenticated, Events: []audit.EventType{
		audit.EventAuthLogout,
		audit.EventRemoteWorkspaceSessionRevoked,
	}},
	"http:GET /api/v1/instance/credential-policy": {Class: ClassInstance, Ops: []Operation{OpCredentialPolicyRead}},
	"http:PUT /api/v1/instance/credential-policy": {Class: ClassInstance, Ops: []Operation{OpCredentialPolicyUpdate}},

	// OIDC federation (#62). Issuer configuration is instance-class under
	// `instance-config` — the same siting as OIDC and SAML provider
	// administration, and for the same reason #16 gave: an org-scoped issuer
	// would let an org admin add a provider and mint identities authenticating
	// into the instance.
	// OIDC federation (#62). One route, one operation.
	"http:GET /api/v1/instance/federation-issuers":             {Class: ClassInstance, Ops: []Operation{OpFederationIssuerList}},
	"http:POST /api/v1/instance/federation-issuers":            {Class: ClassInstance, Ops: []Operation{OpFederationIssuerCreate}},
	"http:PATCH /api/v1/instance/federation-issuers/{issuer}":  {Class: ClassInstance, Ops: []Operation{OpFederationIssuerUpdate}},
	"http:DELETE /api/v1/instance/federation-issuers/{issuer}": {Class: ClassInstance, Ops: []Operation{OpFederationIssuerDelete}},
	// A binding is a credential row, so it is created beside the credentials and
	// listed and revoked THROUGH them. There is no PUT and no PATCH: bindings
	// are immutable, and a change is a replacement mint through this same POST
	// naming the predecessor it supersedes.
	"http:POST /api/v1/orgs/{org}/projects/{project}/service-accounts/{serviceAccount}/bindings": {Class: ClassTenant, Ops: []Operation{OpBindingCreate}},

	// The machine delivery surface (#62). Tenant-class at environment depth: a
	// caller who cannot read the environment gets exactly what a caller
	// addressing an environment that does not exist gets, which is what makes
	// the conditional answer safe to give.
	// The machine delivery route deliberately carries BOTH fields (#62), the same
	// exception credential-reset already is. It reaches an operation —
	// delivery.fetch, which carries its formula and its access record — AND it
	// emits two events with no operation behind them, because they happen BEFORE
	// a principal exists: a federated presentation refused by cause, and the
	// JWKS observations (a tolerated refresh failure, a staleness-bound breach,
	// a throttled unknown-`kid` refresh). Both ride the resolution surface's
	// pre-authentication audit writer, exactly as `auth.oidc_refused` does, so
	// there is no proof to write them under and no operation row to hang them
	// on. The completeness invariant unions both sources.
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/delivery": {Class: ClassTenant, Ops: []Operation{OpDeliveryFetch}, Events: []audit.EventType{
		audit.EventFederationRefused,
		audit.EventJWKSRefreshFailed,
	}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/delivery/offline-records": {Class: ClassTenant, Ops: []Operation{OpDeliveryReconcileOffline}, Events: []audit.EventType{
		audit.EventFederationRefused,
		audit.EventJWKSRefreshFailed,
	}},

	"http:GET /api/v1/orgs/{org}/projects/{project}/grants":                                      {Class: ClassTenant, Ops: []Operation{OpGrantListProject}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/grants":                                     {Class: ClassTenant, Ops: []Operation{OpGrantCreateProject}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/grants":                                   {Class: ClassTenant, Ops: []Operation{OpGrantRevokeProject}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/grants/template":                            {Class: ClassTenant, Ops: []Operation{OpTemplateApplyProject}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/grants":          {Class: ClassTenant, Ops: []Operation{OpGrantCreateEnv}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/environments/{environment}/grants":        {Class: ClassTenant, Ops: []Operation{OpGrantRevokeEnv}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/grants/template": {Class: ClassTenant, Ops: []Operation{OpTemplateApplyEnv}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/settings":         {Class: ClassTenant, Ops: []Operation{OpEnvSettingsRead}},
	"http:PUT /api/v1/orgs/{org}/projects/{project}/environments/{environment}/settings":         {Class: ClassTenant, Ops: []Operation{OpEnvSettingsUpdate}},
	// The key catalogue (#49). Every route is tenant-class at project depth:
	// a key is declared once per project, and a key the caller cannot reach
	// answers byte-identically to one that is not there — including the two
	// reveal-gated routes, whose refusal must be indistinguishable or the gate
	// itself becomes the one-bit oracle it exists to close.
	"http:GET /api/v1/orgs/{org}/projects/{project}/keys":            {Class: ClassTenant, Ops: []Operation{OpKeyList}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/keys":           {Class: ClassTenant, Ops: []Operation{OpKeyCreate}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/keys/{key}":      {Class: ClassTenant, Ops: []Operation{OpKeyGet}},
	"http:PATCH /api/v1/orgs/{org}/projects/{project}/keys/{key}":    {Class: ClassTenant, Ops: []Operation{OpKeyUpdateMetadata}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/keys/{key}":   {Class: ClassTenant, Ops: []Operation{OpKeyDelete}},
	"http:PUT /api/v1/orgs/{org}/projects/{project}/keys/{key}/name": {Class: ClassTenant, Ops: []Operation{OpKeyRename}},
	// These two routes REACH a second operation at runtime - the reveal gate
	// the schema-model ADR puts in front of a value-dependent rule change on a
	// `secret` key, and in front of declassification. Both are listed for the
	// same reason credential-reset lists its pair: the linkage must record
	// every operation a route can reach, or the registry describes an
	// authorization posture the router does not have.
	"http:PUT /api/v1/orgs/{org}/projects/{project}/keys/{key}/declaration":    {Class: ClassTenant, Ops: []Operation{OpKeyUpdateDeclaration, OpKeySecretRuleChange}},
	"http:PUT /api/v1/orgs/{org}/projects/{project}/keys/{key}/classification": {Class: ClassTenant, Ops: []Operation{OpKeyReclassify, OpKeyDeclassify}},
	"http:PUT /api/v1/orgs/{org}/projects/{project}/keys/{key}/group":          {Class: ClassTenant, Ops: []Operation{OpKeySetGroup}},

	// Definitions Git flow (#70). Every route is project-addressed tenant
	// material; grant refusal and a missing project/plan share one wire shape.
	"http:GET /api/v1/orgs/{org}/projects/{project}/definitions/export":              {Class: ClassTenant, Ops: []Operation{OpDefinitionsExport}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/definitions/check":              {Class: ClassTenant, Ops: []Operation{OpDefinitionsCheck}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/definitions/plans":              {Class: ClassTenant, Ops: []Operation{OpDefinitionsPlanCreate}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/definitions/plans/{plan}":        {Class: ClassTenant, Ops: []Operation{OpDefinitionsPlanGet}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/definitions/plans/{plan}/apply": {Class: ClassTenant, Ops: []Operation{OpDefinitionsApply}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/definitions/settings":            {Class: ClassTenant, Ops: []Operation{OpDefinitionsSettingsGet}},
	"http:PUT /api/v1/orgs/{org}/projects/{project}/definitions/settings":            {Class: ClassTenant, Ops: []Operation{OpDefinitionsSettingsSet}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/machine-reveal":                  {Class: ClassTenant, Ops: []Operation{OpProjectMachineRevealGet}},
	"http:PUT /api/v1/orgs/{org}/projects/{project}/machine-reveal":                  {Class: ClassTenant, Ops: []Operation{OpProjectMachineRevealSet}},
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
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/values":         {Class: ClassTenant, Ops: []Operation{OpValueList}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/reveal-window":  {Class: ClassTenant, Ops: []Operation{OpRevealWindowRead}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/values/reveal": {Class: ClassTenant, Ops: []Operation{OpValueReveal}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/values/{key}":   {Class: ClassTenant, Ops: []Operation{OpValueRead}},
	// Drafts, publishing and revisions (#51). Staging rides the value routes'
	// existing entries; these routes are the ones that emit something new.
	"http:PUT /api/v1/orgs/{org}/projects/{project}/environments/{environment}/values/{key}":         {Class: ClassTenant, Ops: []Operation{OpValueStage}, Events: []audit.EventType{audit.EventValueStaged}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/environments/{environment}/values/{key}":      {Class: ClassTenant, Ops: []Operation{OpValueStage}, Events: []audit.EventType{audit.EventValueStaged}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/values/{key}/reveal": {Class: ClassTenant, Ops: []Operation{OpValueReveal}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/values/declare":                                 {Class: ClassTenant, Ops: []Operation{OpValueSet, OpValuePublish}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/values/copy": {Class: ClassTenant, Ops: []Operation{
		OpValueList, OpValueCopySource, OpValueCopyDestination,
		OpValueCopyDestinationConfig, OpValuePublish,
	}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/values/diff":         {Class: ClassTenant, Ops: []Operation{OpValueList}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/values/diff/reveal": {Class: ClassTenant, Ops: []Operation{OpValueReveal}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/clone": {Class: ClassTenant, Ops: []Operation{
		OpEnvCreate, OpValueList, OpValueCopySource, OpValueCopyDestination,
		OpValueCopyDestinationConfig, OpValuePublish,
	}},
	// The import path (#68). Tenant-class like every other value route: an
	// environment the caller may not read answers exactly like one that is not
	// there, and phase 1's presence read is precisely a read of that
	// environment.
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/values/occurrences": {Class: ClassTenant, Ops: []Operation{OpImportPresence}},
	// A manifest-carrying import re-evaluates phase 1's read op for every
	// environment the manifest names, inside its own transaction, so this route
	// genuinely reaches both operations at runtime.
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/values/import": {Class: ClassTenant, Ops: []Operation{
		OpValueImport, OpImportPresence,
	}},

	// Drafts, publishing and revisions (#51). Every one is tenant-class: an
	// environment the caller may not reach answers byte-identically to one that
	// is not there, history included.
	// Drafts, publishing and revisions (#51). A publish authorizes
	// value.publish once per AFFECTED environment, which is the addressed one
	// plus any other environment the selected versions -- or key-group closure
	// -- reach.
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/publish":             {Class: ClassTenant, Ops: []Operation{OpValuePublish}, Events: []audit.EventType{audit.EventRevisionPublished}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/pending":              {Class: ClassTenant, Ops: []Operation{OpValuePendingList}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/signals":              {Class: ClassTenant, Ops: []Operation{OpRevisionSignals}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/revisions":            {Class: ClassTenant, Ops: []Operation{OpRevisionList}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/revisions/{revision}": {Class: ClassTenant, Ops: []Operation{OpRevisionShow}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/revisions/{revision}/rollback": {Class: ClassTenant, Ops: []Operation{
		OpRevisionRestore, OpRevisionRestoreHistory, OpRevisionRestoreCurrent,
	}, Events: []audit.EventType{audit.EventRevisionRestoreStaged, audit.EventValueStaged}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/pins":                        {Class: ClassTenant, Ops: []Operation{OpPinList}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/pins":                       {Class: ClassTenant, Ops: []Operation{OpPinSet, OpPinSetHistory}, Events: []audit.EventType{audit.EventPinCreated, audit.EventPinReassigned, audit.EventPinRenewed, audit.EventPinExpiryRefused}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/environments/{environment}/pins/{workloadPrincipal}": {Class: ClassTenant, Ops: []Operation{OpPinRelease}, Events: []audit.EventType{audit.EventPinReleased}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/values/export": {Class: ClassTenant, Ops: []Operation{
		OpValueExport, OpValueExportReveal, OpValueExportRevealHistory,
	}, Events: []audit.EventType{audit.EventValueRevealed}},
	// The stream authorizes twice: once at connect over the project, and once
	// per event over the environment the event names.
	"http:GET /api/v1/orgs/{org}/projects/{project}/events": {Class: ClassTenant, Ops: []Operation{OpAdvisoryWatch, OpAdvisoryEvent}},
	// The audit trail read surface (#45). Query and export at each addressed
	// depth; the depth is in the path, so one operation per route. Reading is
	// itself audited: the query op commits its own audit.query, the export op
	// its INTENT/OUTCOME pair. All tenant-class: an audit surface the caller may
	// not read answers exactly like a scope that is not there.
	"http:GET /api/v1/orgs/{org}/audit":                                                      {Class: ClassTenant, Ops: []Operation{OpAuditQueryOrg}, Events: []audit.EventType{audit.EventAuditQuery}},
	"http:GET /api/v1/orgs/{org}/audit/export":                                               {Class: ClassTenant, Ops: []Operation{OpAuditExportOrg}, Events: []audit.EventType{audit.EventAuditExportStarted, audit.EventAuditExportCompleted}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/audit":                                   {Class: ClassTenant, Ops: []Operation{OpAuditQueryProject}, Events: []audit.EventType{audit.EventAuditQuery}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/audit/export":                            {Class: ClassTenant, Ops: []Operation{OpAuditExportProject}, Events: []audit.EventType{audit.EventAuditExportStarted, audit.EventAuditExportCompleted}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/audit":        {Class: ClassTenant, Ops: []Operation{OpAuditQueryEnv}, Events: []audit.EventType{audit.EventAuditQuery}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/audit/export": {Class: ClassTenant, Ops: []Operation{OpAuditExportEnv}, Events: []audit.EventType{audit.EventAuditExportStarted, audit.EventAuditExportCompleted}},
	// Secret-change approvals (#151). Policy administration is project-scoped;
	// the review queue is a read@env audited-none; voting is publish@env. The
	// merge/bypass decision rides the publish route, not these.
	"http:GET /api/v1/orgs/{org}/projects/{project}/approval-policies":                                                       {Class: ClassTenant, Ops: []Operation{OpApprovalPolicyRead}, Events: []audit.EventType{audit.EventApprovalPolicyRead}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/approval-policies":                                                      {Class: ClassTenant, Ops: []Operation{OpApprovalPolicyWrite}, Events: []audit.EventType{audit.EventApprovalPolicyChanged}},
	"http:PUT /api/v1/orgs/{org}/projects/{project}/approval-policies/{policy}":                                              {Class: ClassTenant, Ops: []Operation{OpApprovalPolicyWrite}, Events: []audit.EventType{audit.EventApprovalPolicyChanged}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/approval-policies/{policy}":                                           {Class: ClassTenant, Ops: []Operation{OpApprovalPolicyWrite}, Events: []audit.EventType{audit.EventApprovalPolicyChanged}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/approval-requests":                            {Class: ClassTenant, Ops: []Operation{OpApprovalRequestRead}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/approval-requests/{approvalRequest}/ceremony": {Class: ClassTenant, Ops: []Operation{OpApprovalVote}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/approval-requests/{approvalRequest}/vote":    {Class: ClassTenant, Ops: []Operation{OpApprovalVote}, Events: []audit.EventType{audit.EventApprovalVoted, audit.EventApprovalInvalidated}},
	// The root token key belongs to the instance, so there is no tenant object
	// whose nonexistence a refusal could mimic. The same holds for every DEK: a
	// DEK belongs to the instance's crypto hierarchy, not a tenant.
	"http:POST /api/v1/instance/rotate-token-key": {Class: ClassInstance, Ops: []Operation{OpRotateTokenKey}, Events: []audit.EventType{audit.EventTokenKeyRotated}},
	// The scanning fingerprint key is instance-scoped too (#74).
	"http:POST /api/v1/instance/rotate-scanning-key": {Class: ClassInstance, Ops: []Operation{OpRotateScanningKey}, Events: []audit.EventType{audit.EventScanningKeyRotated}},
	"http:POST /api/v1/instance/rotate-dek":          {Class: ClassInstance, Ops: []Operation{OpRotateDEK}, Events: []audit.EventType{audit.EventDEKRotated}},
	"http:POST /api/v1/instance/rotate-master-key":   {Class: ClassInstance, Ops: []Operation{OpRotateMasterKey}, Events: []audit.EventType{audit.EventMasterKeyRotated}},
	"http:POST /api/v1/instance/rotate-root-key":     {Class: ClassInstance, Ops: []Operation{OpRotateRootKey}, Events: []audit.EventType{audit.EventRootKeyRotationPrepared, audit.EventRootKeyRotationVerified, audit.EventRootKeyRotationFinalized}},
	"http:POST /api/v1/instance/reencrypt":           {Class: ClassInstance, Ops: []Operation{OpReencryptInstance}, Events: []audit.EventType{audit.EventReencryptCompleted}},
	// reencrypt of a PROJECT is tenant-class: it reads and writes the project's
	// own tenant-owned rows, so its refusal mimics that project's nonexistence.
	"http:POST /api/v1/orgs/{org}/projects/{project}/reencrypt": {Class: ClassTenant, Ops: []Operation{OpReencryptProject}, Events: []audit.EventType{audit.EventReencryptCompleted}},

	// Deployment adapters (#65). Every project and target surface is tenant
	// class; dynamic reveal/reauth checks over the adapter's environment set are
	// added by the service after this route-level classification.
	// Deployment adapters (#65). Dynamic reveal and reauthentication checks
	// refine these operations in service, but every route still names the
	// static proof-bearing operation whose audit family it reaches.
	"http:GET /api/v1/orgs/{org}/projects/{project}/adapters":                            {Class: ClassTenant, Ops: []Operation{OpAdapterInspect}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/adapters":                           {Class: ClassTenant, Ops: []Operation{OpAdapterConfigure}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/adapters/{adapter}":                  {Class: ClassTenant, Ops: []Operation{OpAdapterInspect}},
	"http:PATCH /api/v1/orgs/{org}/projects/{project}/adapters/{adapter}":                {Class: ClassTenant, Ops: []Operation{OpAdapterConfigure}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/adapters/{adapter}":               {Class: ClassTenant, Ops: []Operation{OpAdapterDelete}},
	"http:PUT /api/v1/orgs/{org}/projects/{project}/adapters/{adapter}/credential":       {Class: ClassTenant, Ops: []Operation{OpAdapterCredentialSet}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/adapters/{adapter}/credential":    {Class: ClassTenant, Ops: []Operation{OpAdapterCredentialRevoke}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/adapters/{adapter}/targets":          {Class: ClassTenant, Ops: []Operation{OpAdapterInspect}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/adapters/{adapter}/targets":         {Class: ClassTenant, Ops: []Operation{OpAdapterConfigure}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/adapter-targets/{target}":            {Class: ClassTenant, Ops: []Operation{OpAdapterInspect}},
	"http:PATCH /api/v1/orgs/{org}/projects/{project}/adapter-targets/{target}":          {Class: ClassTenant, Ops: []Operation{OpAdapterConfigure}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/adapter-targets/{target}":         {Class: ClassTenant, Ops: []Operation{OpAdapterDelete}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/adapter-targets/{target}/plan":      {Class: ClassTenant, Ops: []Operation{OpAdapterPlan}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/adapter-targets/{target}/sync":      {Class: ClassTenant, Ops: []Operation{OpAdapterSync}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/adapter-targets/{target}/pause":     {Class: ClassTenant, Ops: []Operation{OpAdapterConfigure}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/adapter-targets/{target}/resume":    {Class: ClassTenant, Ops: []Operation{OpAdapterSync}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/adapter-targets/{target}/test":      {Class: ClassTenant, Ops: []Operation{OpAdapterTest}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/adapter-targets/{target}/adoptions": {Class: ClassTenant, Ops: []Operation{OpAdapterAdopt}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/adapter-moves/{move}":                {Class: ClassTenant, Ops: []Operation{OpAdapterInspect}},
	"http:PATCH /api/v1/orgs/{org}/projects/{project}/adapter-moves/{move}":              {Class: ClassTenant, Ops: []Operation{OpAdapterConfigure}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/adapter-moves/{move}":             {Class: ClassTenant, Ops: []Operation{OpAdapterConfigure}},

	// Dynamic secrets (#147).
	"http:GET /api/v1/orgs/{org}/projects/{project}/dynamic-providers":                                 {Class: ClassTenant, Ops: []Operation{OpDynamicProviderInspect}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/dynamic-providers":                                {Class: ClassTenant, Ops: []Operation{OpDynamicProviderConfigure}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/dynamic-providers/{provider}":                      {Class: ClassTenant, Ops: []Operation{OpDynamicProviderInspect}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/dynamic-providers/{provider}":                   {Class: ClassTenant, Ops: []Operation{OpDynamicProviderDelete}},
	"http:PUT /api/v1/orgs/{org}/projects/{project}/dynamic-providers/{provider}/credential":           {Class: ClassTenant, Ops: []Operation{OpDynamicProviderCredentialSet}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/dynamic-providers/{provider}/credential":        {Class: ClassTenant, Ops: []Operation{OpDynamicProviderCredentialRevoke}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/leases":                 {Class: ClassTenant, Ops: []Operation{OpLeaseInspect}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/leases":                {Class: ClassTenant, Ops: []Operation{OpLeaseMint}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/environments/{environment}/leases/{lease}":         {Class: ClassTenant, Ops: []Operation{OpLeaseInspect}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/leases/{lease}/renew":  {Class: ClassTenant, Ops: []Operation{OpLeaseRenew}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/leases/{lease}/revoke": {Class: ClassTenant, Ops: []Operation{OpLeaseRevoke}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/leases/{lease}/settle": {Class: ClassTenant, Ops: []Operation{OpLeaseSettle}},

	"http:GET /api/v1/orgs/{org}/projects/{project}/key-groups":            {Class: ClassTenant, Ops: []Operation{OpKeyGroupList}},
	"http:POST /api/v1/orgs/{org}/projects/{project}/key-groups":           {Class: ClassTenant, Ops: []Operation{OpKeyGroupCreate}},
	"http:GET /api/v1/orgs/{org}/projects/{project}/key-groups/{group}":    {Class: ClassTenant, Ops: []Operation{OpKeyGroupGet}},
	"http:PATCH /api/v1/orgs/{org}/projects/{project}/key-groups/{group}":  {Class: ClassTenant, Ops: []Operation{OpKeyGroupRename}},
	"http:DELETE /api/v1/orgs/{org}/projects/{project}/key-groups/{group}": {Class: ClassTenant, Ops: []Operation{OpKeyGroupDelete}},

	// `hikyo admin create`: the bootstrap member of the closed local-authority
	// exception set. System class, whose probe contract is network
	// unreachability — the totality invariant asserts it by finding no HTTP
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
	"cli:admin": {Class: ClassSystem, Events: []audit.EventType{
		audit.EventAuthAuthorityMinted, audit.EventAuthCredentialResetIssued,
		audit.EventBreakGlassGrant,
		audit.EventPrivacySubjectCorrected, audit.EventPrivacySubjectExported, audit.EventPrivacySubjectRestricted, audit.EventPrivacySubjectReleased, audit.EventPrivacySubjectErased,
	}},

	// Process entry points with no principal: boot (server) and migration.
	// Their system-proof mint sites are enumerated in systemSites; the probe
	// contract is network unreachability, which the totality check asserts
	// by finding no HTTP route for them.
	"cli:server": {Class: ClassSystem, Events: []audit.EventType{audit.EventBackupExported, audit.EventBackupExportSkipped}},
	// The automatic pre-migration export (ops spec section 11) rides the two
	// entry points that can apply a migration, so both of them now have an
	// auditable act at the operation surface and leave the exemption fixture:
	// an export taken (or LOUDLY SKIPPED for want of recipients) immediately
	// before a schema change is the record that says whether there is
	// anything to fall back to.
	"cli:migrate": {Class: ClassSystem, Events: []audit.EventType{audit.EventBackupExported, audit.EventBackupExportSkipped}},

	// `hikyo backup` and `hikyo restore` (#76): the operator lifecycle, on the
	// server's own host. System class, and the probe contract that matters is
	// exactly the one the totality invariant asserts by finding no HTTP route
	// — a restore endpoint reachable from the network would be an instance
	// replacement one request away, and the reconciliation that follows a
	// restore is unreachable by any other means anyway, because a restore
	// leaves no principal able to authorize anything.
	// The operator lifecycle (#76). `backup` writes its export record;
	// `restore` writes the reconstruction and one event per principal the
	// operator reconciles afterwards.
	"cli:escrow":  {Class: ClassSystem, Events: []audit.EventType{audit.EventRootEscrowVerified}},
	"cli:backup":  {Class: ClassSystem, Events: []audit.EventType{audit.EventBackupExported, audit.EventBackupExportSkipped}},
	"cli:restore": {Class: ClassSystem, Events: []audit.EventType{audit.EventRestoreCompleted, audit.EventRestorePrincipalReconciled, audit.EventRestoreDrillCompleted}},

	// Local product-information commands print build metadata — no principal,
	// no server, no store; the pre-auth contract is trivially total.
	"cli:version": {Class: ClassUnauthenticated},
	"cli:about":   {Class: ClassUnauthenticated},
	"cli:welcome": {Class: ClassUnauthenticated},

	// Client verbs that reach the server. Their probe contract is the HTTP
	// route they call, classified above; the verb itself carries the class of
	// what it reaches, so a verb whose class is still ClassStub cannot
	// silently start making requests.
	"cli:login":   {Class: ClassUnauthenticated},
	"cli:logout":  {Class: ClassUnauthenticated},
	"cli:whoami":  {Class: ClassUnauthenticated},
	"cli:account": {Class: ClassUnauthenticated},
	// `context` is entirely client-local: the trust store and the named
	// contexts live on this box and reach no server.
	"cli:context": {Class: ClassUnauthenticated},
	// `update` reads and writes only client-local public release metadata.
	"cli:update": {Class: ClassUnauthenticated},
	// `org` still reaches the instance-scoped create/list as well as the
	// tenant-scoped by-id routes, so it carries the wider of the two classes:
	// a verb whose class understated its reach would let an instance-scoped
	// call ride in under a tenant probe contract. `project`, `env` and `folder`
	// reach tenant routes exclusively.
	// Multi-instance (#71). Both families are instance-scoped: the viewing
	// side's remotes are instance configuration read under instance-directory,
	// and the serving side's connection credentials are custody under
	// instance-config.
	"cli:remote":            {Class: ClassInstance},
	"cli:remote-credential": {Class: ClassInstance},
	"cli:org":               {Class: ClassInstance},
	"cli:project":           {Class: ClassTenant},
	"cli:env":               {Class: ClassTenant},
	"cli:folder":            {Class: ClassTenant},
	// `key` reaches the catalogue and the group routes, all tenant-class.
	"cli:key": {Class: ClassTenant},
	// `values` reaches only the tenant-scoped value routes.
	"cli:values": {Class: ClassTenant},
	// `revision` reaches the two tenant-scoped history routes (#51). It
	// discloses no value: history is lineage, and the one verb that reads a
	// snapshot's values is `values export`.
	"cli:revision": {Class: ClassTenant},
	"cli:pin":      {Class: ClassTenant},
	"cli:approval": {Class: ClassTenant},
	// `rotate-token-key` reaches one instance-scoped route: the root token key
	// belongs to the instance, so there is no tenant object whose nonexistence
	// a refusal could mimic. `rotate-dek` reaches the DEK rotation route on the
	// same instance-scoped grounds.
	"cli:rotate-token-key": {Class: ClassInstance},
	// `rotate-scanning-key` reaches one instance-scoped route: the scanning
	// fingerprint key belongs to the instance, same shape as rotate-token-key.
	"cli:rotate-scanning-key": {Class: ClassInstance},
	"cli:rotate-dek":          {Class: ClassInstance},
	"cli:rotate-master-key":   {Class: ClassInstance},
	"cli:rotate-root-key":     {Class: ClassInstance},
	// `reencrypt` reaches both the instance route and the project route; like
	// `access`, the verb takes the instance class.
	"cli:reencrypt":       {Class: ClassInstance},
	"cli:instance-config": {Class: ClassInstance},
	"cli:doctor":          {Class: ClassInstance},
	// `access` reaches BOTH classes — the org/project/env grant routes are
	// tenant-class, the instance-scope ones are instance-class. It is
	// classified instance because that is the WEAKER probe contract of the
	// two: a verb that can reach a grant-refusal route must not ride in under
	// the uniform-nonexistent contract it does not always satisfy. The
	// per-route classification above is the authoritative one either way.
	"cli:access": {Class: ClassInstance},
	// `project-settings` reaches only the two environment-scoped routes.
	"cli:project-settings": {Class: ClassTenant},
	// `sa` reaches the project-scoped identity routes, all tenant-class:
	// a project whose identities the caller may not administer answers
	// exactly like a project that is not there. The instance credential
	// policy rides `instance-config`, not this verb.
	"cli:sa": {Class: ClassTenant},
	// `scim` reaches ONLY tenant-class routes: every SCIM administration
	// operation is org-addressed, so a binding the caller may not reach answers
	// exactly like one that is not there. The wire routes are tenant-class too,
	// but no CLI verb reaches them — they are the identity provider's.
	"cli:scim": {Class: ClassTenant},
	// `adapter` reaches only project-owned adapter and target routes. Dynamic
	// affected-environment checks happen behind those tenant routes.
	"cli:adapter": {Class: ClassTenant},

	// Dynamic secrets (#147): both verbs are client transport for the
	// tenant-scoped provider and lease routes and nothing wider.
	"cli:dynamic-provider": {Class: ClassTenant},
	"cli:lease":            {Class: ClassTenant},

	// The Compose delivery verbs (#63). `run` and `compose` both reach the
	// tenant-scoped delivery routes (GET .../delivery and its offline-records
	// reconciliation POST) and nothing wider, so both carry ClassTenant: a
	// caller who cannot read the environment gets what an environment that does
	// not exist gives. `compose` dispatches render|sync|doctor internally; the
	// class is the verb's, and every sub-verb reaches only those two routes.
	"cli:run":     {Class: ClassTenant},
	"cli:compose": {Class: ClassTenant},
	// `definitions` (#70) reaches only the tenant-scoped export/check/plan/apply
	// routes; server operations own every authorization and audit decision.
	"cli:definitions": {Class: ClassTenant},
	// `import` (#68) reaches the tenant-scoped phase-1 presence route and the
	// tenant-scoped phase-2 import route, and nothing else. Its class flipped
	// off ClassStub in the same change that registered its operations — the
	// totality invariant refuses a stub verb that already has operations, which
	// is exactly the "implementation rides in on a stale class" case.
	"cli:import": {Class: ClassTenant},

	// Outbox job types and SSE emit sites: none exist. Their registries are
	// this table's "job:" and "sse:" key spaces; the first entry of each
	// kind must arrive with its probe class.
})

// Events records audit event types a wire entry emits DIRECTLY, without an
// operation registry row behind it.
//
// It exists because authentication is the one surface that cannot be modelled
// as an operation: `authorize()` needs a principal, and these endpoints are
// what produce one. Their audit obligation is real all the same — the
// human-auth ADR requires login success and failure, logout, session
// creation, and credential-establishment mint, consumption and refusal — so
// the completeness invariant reads each row beside the operation registry
// rather than letting an unaudited pre-auth path hide behind "no operation".
//
// Most wire entries are either operation-backed (Ops) or declare their events
// directly (the authentication surface). The credential-reset route (#54) is
// deliberately BOTH: its Ops field names the two operations it dispatches
// between at runtime — so the operation linkage records
// that it reaches CapCredentialReset (MFA-mandatory) — AND declares its events
// here, because its writes and audit ride the resolution surface (like the
// account-security mutations) rather than a single operation row. It names no
// single x-hikyo-operation in the contract, since two ops of different classes
// cannot be carried by one row; the completeness invariant unions both sources.

// Ops maps an HTTP entry point to the registered operation(s) it reaches. The
// audit-completeness invariant follows it so a domain route inherits its
// operation's audit mapping instead of needing a second declaration that
// could drift from the first. Most routes reach exactly one operation; a route
// that dispatches at runtime between operations (credential reset) lists them
// all, so the linkage records every operation the route can reach.

// WireRoutes returns the route→operation(s) mapping for the invariant tests and
// the contract cross-check.
func (RegistryFacts) WireRoutes() map[string][]Operation {
	routes := make(map[string][]Operation)
	for name, entry := range wireRegistry {
		if len(entry.Ops) > 0 {
			routes[name] = append([]Operation(nil), entry.Ops...)
		}
	}
	return routes
}

// WireEvents returns the direct wire→event mapping for the invariant tests.
func (RegistryFacts) WireEvents() map[string][]audit.EventType {
	events := make(map[string][]audit.EventType)
	for name, entry := range wireRegistry {
		if len(entry.Events) > 0 {
			events[name] = append([]audit.EventType(nil), entry.Events...)
		}
	}
	return events
}

// Cache is one registered cache holding derived tenant material
// (tenant-isolation ADR invariant 12). Registration is mandatory: the
// invariant test fails on any cache-shaped declaration in the module that
// is not listed here, so a new cache cannot appear without stating how it
// is keyed and who may reach it.
type Cache struct {
	// KeyConstructor is the single function that builds its keys. The ADR's
	// keying rule: the full id chain to the owning scope, structured and
	// injectively encoded (length-prefixed — bare concatenation is how
	// (org "a", project "bc") and (org "ab", project "c") collide).
	KeyConstructor string
	// ProofGatedAt names the layer that supplies the proof for reads and
	// writes. For the DEK LRU this is deliberately NOT inside the cache:
	// internal/crypto is a locked leaf package (encryption-model ADR; enforced by
	// the boundary test) and may not import the authorization package, so
	// its accessors cannot take an authz.Proof. The access rule is therefore
	// discharged one layer up, at the service seam that resolves a scope
	// before asking crypto to seal for it.
	ProofGatedAt string
}

// caches is the closed cache registry.
var caches = map[string]Cache{
	"crypto.dek-lru": {
		KeyConstructor: "internal/crypto.dekScope",
		// No tenant-facing caller exists yet: the DEK LRU is reachable only
		// from Keyring.ForProject, whose only callers today are crypto's own
		// tests and the boot path. The first tenant consumer is #50 (flat
		// encrypted values), which MUST resolve the scope through
		// authorize() and pass the proof's chain — a cache hit must not be a
		// proof-free path to tenant material.
		ProofGatedAt: "service seam (#50); no tenant caller today",
	},
	"oidcfed.jwks": {
		// Keyed by the BYTE-EXACT issuer string, and that string IS the whole
		// key: an issuer is instance configuration under a unique index, so it
		// is already an injective identifier with no chain to compose.
		KeyConstructor: "internal/oidcfed.Issuer.Issuer (byte-exact issuer string)",
		// Not proof-gated, and here that is the right answer rather than a
		// deferral. The contents are the PUBLIC signing keys an issuer publishes
		// at a well-known URL — no tenant material, nothing a proof could
		// protect. What the cache governs is the FRESHNESS of the answer, which
		// is the staleness bound. And it is read pre-authentication by
		// construction: validating the presented token is what produces a
		// principal, so no proof can exist yet.
		ProofGatedAt: "not proof-gated: public issuer signing keys, read pre-authentication (#62)",
	},
	"updatecheck.releases": {
		// One process-wide list from the compile-time fixed Hikyo GitHub
		// repository. Channel selection happens after the list is read.
		KeyConstructor: "singleton: github.com/Hikyo-Org/hikyo releases",
		ProofGatedAt:   "not proof-gated: public release metadata; endpoint authorization precedes access",
	},
}

// Caches returns the cache registry for the invariant test.
func (RegistryFacts) Caches() map[string]Cache {
	return maps.Clone(caches)
}

// Wire returns the wire registry for the invariant tests.
func (RegistryFacts) Wire() map[string]Class {
	classes := make(map[string]Class, len(wireRegistry))
	for name, entry := range wireRegistry {
		classes[name] = entry.Class
	}
	return classes
}
