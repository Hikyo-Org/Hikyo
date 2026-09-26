package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/oidcrp"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Federated sign-up on the OIDC kind (#607; docs/spec/social-signin.md
// section 4, "Federated login callback", the unknown-and-sign-up branch).
// It runs inside completeLogin's transaction, after the intent-blind
// (kind, issuer, subject) resolution missed, in the normative order:
//
//	admission gate (policy present and active, provider admitted; uncharged)
//	-> `signup` budget -> verified-email assertion -> claim allowlist
//	-> fresh-org cap -> write
//
// The write runs under the policy's authority principal as the caller for
// the landing (#585 d1): the new principal, account and identity first (so an
// `identity-exists` race refuses before any org row exists), then the org row
// and the admin template for a fresh org, or the org template for an org
// policy, with grant origin `registration`. Every refusal answers the uniform
// login refusal; the cause lives in registration.signup_refused.

// orgCreateSerialization is the WriteSerialized key org minting shares: an
// operator's org.create and a sign-up-intent callback (which may mint one).
const orgCreateSerialization = "hikyo:org-create"

// Sign-up refusal causes (audit-model banner 2026-09-03) this leg emits.
const (
	signupCauseClosed          = "closed"
	signupCausePredicate       = "predicate"
	signupCausePrecondition    = "precondition"
	signupCauseAuthorityLost   = "authority-lost"
	signupCauseBudget          = "budget"
	signupCauseNoVerifiedEmail = "no-verified-email"
	signupCauseCap             = "cap"
	signupCauseIdentityExists  = "identity-exists"
)

// errSignupIdentityRace is the UNIQUE (kind, issuer, subject) key refusing
// the sign-up's identity insert: a concurrent bind of the same identity won.
// The failed statement aborted the transaction, so completeLogin records the
// `identity-exists` refusal in a fresh one.
var errSignupIdentityRace = errors.New("service: sign-up identity lost the uniqueness race")

// oidcSignup is one sign-up-intent callback's registration leg. It is built
// once per callback, outside the retried transaction, and carries the refund
// of the budget charge its current attempt made: the charge is in memory and
// survives a rolled-back attempt, so every retry (and a transaction that fails
// outright) refunds it first. A committed attempt's charge is exactly what its
// committed events say: charged from the budget step on, never before it.
type oidcSignup struct {
	auth   *Auth
	prov   authz.OIDCProvider
	txn    authz.OIDCTransaction
	claims oidcrp.Claims
	refund func()
	// policyID is the policy the last attempt resolved, for the refusal
	// written after an identity race.
	policyID string
}

// rollback refunds the charge of an attempt that did not commit.
func (g *oidcSignup) rollback() {
	if g.refund != nil {
		g.refund()
		g.refund = nil
	}
}

// newOIDCSignup holds one callback's verified claims and its budget refund
// across transaction retries.
func newOIDCSignup(s *Auth, prov authz.OIDCProvider, txn authz.OIDCTransaction, claims oidcrp.Claims) *oidcSignup {
	return &oidcSignup{auth: s, prov: prov, txn: txn, claims: claims}
}

// scope returns the transaction's sign-up scope; an empty org ID represents
// the instance policy.
func (g *oidcSignup) scope() domain.Scope {
	return domain.Scope{Org: domain.OrgID(g.txn.SignupScopeOrgID)}
}

// refusal stages registration.signup_refused and marks the attempt refused:
// the uniform login refusal, or the shared 429 for the budget.
func (g *oidcSignup) refusal(ctx context.Context, az *authz.TxAuthorizer, attempt *sessionCompletionAttempt, cause, policyID string) (authz.Account, error) {
	if err := g.stageRefused(ctx, az, cause, policyID); err != nil {
		return authz.Account{}, err
	}
	attempt.refused = sessionRefusedUnauthenticated
	if cause == signupCauseBudget {
		attempt.refused = sessionRefusedOverloaded
	}
	return authz.Account{}, nil
}

// stageRefused records the refusal on the instance trail. The policy ID is
// omitted when no policy was resolved; audit creation and write errors propagate.
func (g *oidcSignup) stageRefused(ctx context.Context, az *authz.TxAuthorizer, cause, policyID string) error {
	payload := audit.Payload{
		"cause": cause, "scope": renderScope(g.scope()),
		"kind": OIDCKind, "provider_id": g.prov.ID,
	}
	if policyID != "" {
		payload["policy_id"] = policyID
	}
	if cause == signupCauseNoVerifiedEmail {
		payload["verified_by"] = "none"
	}
	ev, err := newAuditEvent(ctx, audit.EventRegistrationSignupRefused, "",
		audit.Object{Type: "oidc_transaction"}, audit.OutcomeFailure, "", payload)
	if err != nil {
		return err
	}
	return az.RecordAuthEvent(ctx, ev)
}

// refuseAfterRace commits the identity-exists refusal of an attempt the
// UNIQUE key aborted, and answers the uniform refusal.
func (g *oidcSignup) refuseAfterRace(ctx context.Context) error {
	err := tx.Write(ctx, g.auth.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		return g.stageRefused(ctx, az, signupCauseIdentityExists, g.policyID)
	})
	if err != nil {
		return err
	}
	return domain.ErrUnauthenticated
}

// run is the registration leg for an unknown identity under sign-up intent.
// It returns the new account (the caller mints the session exactly as for a
// known identity), or stages a refusal on attempt and returns a zero account.
// Policy, audit and write errors propagate; an identity uniqueness race is
// returned for completeLogin to audit in a fresh transaction.
func (g *oidcSignup) run(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, attempt *sessionCompletionAttempt, epoch int64, now time.Time) (authz.Account, error) {
	refuse := func(cause, policyID string) (authz.Account, error) {
		return g.refusal(ctx, az, attempt, cause, policyID)
	}
	reg := g.auth.registration
	if reg == nil {
		return refuse(signupCauseClosed, "")
	}

	// 1. Admission gate, before any charge (spec section 14.2): the scope's
	// policy row is present and active (the standing delegation and the
	// preconditions re-checked at use, #579 d4/d6), and this provider row is
	// one of its entries. An unknown org reads as no policy: closed.
	policy, err := az.RegistrationPolicyFor(ctx, g.scope().Org)
	if errors.Is(err, domain.ErrNotFound) {
		return refuse(signupCauseClosed, "")
	}
	if err != nil {
		return authz.Account{}, err
	}
	g.policyID = policy.ID
	view, err := reg.evaluatePolicy(ctx, az, policy)
	if err != nil {
		return authz.Account{}, err
	}
	switch view.InactiveCause {
	case "":
	case InactiveAuthorityLost, InactiveAuthorityUnassigned:
		// A policy whose authority no longer holds the landing's grants (or
		// never had an authority) is a lost delegation at use (#579 d4).
		return refuse(signupCauseAuthorityLost, policy.ID)
	default:
		return refuse(signupCausePrecondition, policy.ID)
	}
	entryAt := slices.IndexFunc(policy.Entries, func(e authz.RegistrationEntry) bool {
		return e.ProviderKind == string(domain.ProviderOIDC) && e.ProviderID == g.prov.ID
	})
	if entryAt < 0 {
		return refuse(signupCausePredicate, policy.ID)
	}
	entry := policy.Entries[entryAt]

	// 2. The `signup` budget: the first leg that would create state
	// (#585 d3 as amended by #604). Once committed, never refunded; overflow
	// is the shared pre-auth 429.
	refund, err := g.auth.signupBudget.chargeSignup()
	if err != nil {
		if errors.Is(err, admission.ErrOverloaded) {
			// Reset first: a retried transaction reuses attempt, and a
			// refusal without a window must not inherit an earlier one's wait.
			attempt.retryAfter = 0
			if limited, ok := errors.AsType[*admission.RateLimitedError](err); ok {
				attempt.retryAfter = limited.Wait
			}
			return refuse(signupCauseBudget, policy.ID)
		}
		return authz.Account{}, err
	}
	g.refund = refund

	// 3. The verified-email assertion (#598), from the signed ID token only,
	// evaluated before the allowlist so the cause is deterministic.
	address, verifiedBy, ok := verifiedEmail(g.claims.Raw)
	if !ok {
		return refuse(signupCauseNoVerifiedEmail, policy.ID)
	}
	// 4. The entry's claim allowlist (#579 d5): one string claim of the
	// signed token, one of the accepted values.
	if entry.Claim != "" && !claimAdmitted(g.claims.Raw, entry.Claim, entry.Values) {
		return refuse(signupCausePredicate, policy.ID)
	}
	// 5. The fresh-org cap (#585 d9), counted live inside this serialized
	// transaction.
	if LandingKind(policy.Landing) == LandingFreshOrg {
		n, err := az.CountRegistrationPolicyOrgs(ctx, policy.ID)
		if err != nil {
			return authz.Account{}, err
		}
		if n >= policy.FreshOrgCap {
			return refuse(signupCauseCap, policy.ID)
		}
	}

	admitted, err := newAuditEvent(ctx, audit.EventRegistrationSignupAdmitted, "",
		audit.Object{Type: "registration-policy", ID: policy.ID}, audit.OutcomeSuccess, "",
		audit.Payload{
			"policy_id": policy.ID, "scope": renderScope(g.scope()), "landing": policy.Landing,
			"kind": OIDCKind, "issuer": g.txn.Issuer, "provider_id": g.prov.ID,
			"address": audit.SanitizeFreeText(address), "verified_by": verifiedBy,
		})
	if err != nil {
		return authz.Account{}, err
	}
	if err := az.RecordAuthEvent(ctx, admitted); err != nil {
		return authz.Account{}, err
	}

	// 6. The write. The new principal is caller only for its own account,
	// identity and session; accounts.email stays NULL (a federated address
	// is never stored, #598 d9, spec section 4).
	principalID, err := newID("prn")
	if err != nil {
		return authz.Account{}, err
	}
	accountID, err := newID("acc")
	if err != nil {
		return authz.Account{}, err
	}
	if err := az.CreateHumanPrincipal(ctx, domain.PrincipalID(principalID), now); err != nil {
		return authz.Account{}, err
	}
	username := "oidc-" + accountID
	displayName, displayFrom := signupDisplayName(g.claims.Raw, username)
	if err := az.CreateAccount(ctx, authz.Account{
		ID: accountID, PrincipalID: domain.PrincipalID(principalID),
		Username: username, DisplayName: displayName, CreatedAt: now,
	}); err != nil {
		return authz.Account{}, err
	}
	identityID, err := newID("eid")
	if err != nil {
		return authz.Account{}, err
	}
	// The identity row precedes any org row (spec section 9): a concurrent
	// bind of this identity refuses here, and the rollback leaves nothing.
	if err := az.CreateExternalIdentity(ctx, authz.NewExternalIdentity{
		ID: identityID, AccountID: accountID, Kind: OIDCKind, Issuer: g.txn.Issuer,
		Subject: g.claims.Subject, ProviderID: g.prov.ID, CredentialEpoch: epoch, CreatedAt: now,
	}); err != nil {
		if isUniquenessRace(err) {
			return authz.Account{}, errSignupIdentityRace
		}
		return authz.Account{}, err
	}
	landing, err := g.land(ctx, r, az, policy, domain.PrincipalID(principalID), now)
	if err != nil {
		return authz.Account{}, err
	}
	account, err := az.AccountByID(ctx, accountID)
	if err != nil {
		return authz.Account{}, err
	}
	payload := audit.Payload{
		"policy_id": policy.ID, "account_id": accountID, "landing": policy.Landing,
		"kind": OIDCKind, "provider_id": g.prov.ID,
		"address": audit.SanitizeFreeText(address), "verified_by": verifiedBy,
		"display_name_from": displayFrom,
	}
	if landing.orgID != "" {
		payload["org_id"] = landing.orgID
	}
	completed, err := newAuditEvent(ctx, audit.EventRegistrationSignupCompleted, domain.PrincipalID(principalID),
		audit.Object{Type: "account", ID: accountID}, audit.OutcomeSuccess, "", payload)
	if err != nil {
		return authz.Account{}, err
	}
	return account, az.RecordAuthEvent(ctx, completed)
}

// landed is where a sign-up arrived: the fresh org's id, if one was minted.
type landed struct {
	orgID string
}

// land mints the landing under the policy's authority principal (#585 d1,
// permission-model 2026-09-03 (a)): the authority is the caller for
// org.create and for the template grant targeting the new principal, re-
// authorized here against its current grants; grant origin `registration`
// with the authority as subject.
func (g *oidcSignup) land(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, policy authz.RegistrationPolicy, target domain.PrincipalID, now time.Time) (landed, error) {
	authority := authz.Identity{Principal: policy.AuthorityPrincipalID}
	grants := &Grants{DB: g.auth.DB, Now: func() time.Time { return now }, originKind: domain.OriginRegistration}
	switch LandingKind(policy.Landing) {
	case LandingNone:
		// A zero-grant account (the retired provisioning's semantics).
		return landed{}, nil
	case LandingOrgTemplate:
		scope := domain.Scope{Org: policy.OrgID}
		p, err := az.Authorize(ctx, authority, authz.OpTemplateApplyOrg, scope)
		if err != nil {
			return landed{}, err
		}
		_, err = grants.applyTemplate(ctx, r, az, p, authority, domain.Template(policy.Template), target, scope, domain.LevelOrg)
		return landed{}, err
	case LandingFreshOrg:
		p, err := az.Authorize(ctx, authority, authz.OpOrgCreate, domain.Scope{})
		if err != nil {
			return landed{}, err
		}
		orgID, err := newID("org")
		if err != nil {
			return landed{}, err
		}
		org := store.Org{
			ID: orgID, Name: "org-" + orgID, Active: true, Metadata: json.RawMessage(`{}`),
			CreatedAt: store.CanonTime(now), Origin: string(domain.OriginRegistration),
			RegistrationPolicyID: policy.ID,
		}
		if err := r.Orgs().Create(ctx, p, org); err != nil {
			return landed{}, err
		}
		ev, err := domainEvent(ctx, audit.EventOrgCreated, authority.Principal,
			audit.Object{Type: "org", ID: orgID},
			audit.Payload{
				"org_id": orgID, "org_name": audit.SanitizeFreeText(org.Name),
				"origin": string(domain.OriginRegistration), "policy_id": policy.ID,
			})
		if err != nil {
			return landed{}, err
		}
		if err := r.Audit().InsertInstance(ctx, p, ev); err != nil {
			return landed{}, err
		}
		scope := domain.Scope{Org: domain.OrgID(orgID)}
		ops, level, err := opsFor(scope)
		if err != nil {
			return landed{}, err
		}
		grantProof, err := az.Authorize(ctx, authority, ops.template, scope)
		if err != nil {
			return landed{}, err
		}
		_, err = grants.applyTemplate(ctx, r, az, grantProof, authority, domain.TemplateAdmin, target, scope, level)
		return landed{orgID: orgID}, err
	default:
		return landed{}, errors.New("service: registration policy has an unknown landing " + policy.Landing)
	}
}

// verifiedEmailClaims is the fixed recognised set (#598 d3), in the order
// that names verified_by when both are present and true.
var verifiedEmailClaims = []string{"email_verified", "xms_edov"}

// verifiedEmail applies #598 d3/d4 to the signed token's raw claims: a
// non-empty string `email`, and at least one recognised claim that is the
// JSON boolean true. Any recognised claim present and not exactly `true`
// refuses, whatever the other says (fail-closed tie-break); the string
// "true" is not true (parse, don't cast).
func verifiedEmail(raw map[string]json.RawMessage) (address, verifiedBy string, ok bool) {
	if err := json.Unmarshal(raw["email"], &address); err != nil || address == "" {
		return "", "", false
	}
	for _, name := range verifiedEmailClaims {
		value, present := raw[name]
		if !present {
			continue
		}
		if !bytes.Equal(bytes.TrimSpace(value), []byte("true")) {
			return "", "", false
		}
		if verifiedBy == "" {
			verifiedBy = name
		}
	}
	return address, verifiedBy, verifiedBy != ""
}

// claimAdmitted reports whether the token's claim is a JSON string among the
// accepted values. A missing, non-string or unlisted value is not admitted.
func claimAdmitted(raw map[string]json.RawMessage, claim string, values []string) bool {
	value, present := raw[claim]
	if !present {
		return false
	}
	var s string
	if err := json.Unmarshal(value, &s); err != nil {
		return false
	}
	return slices.Contains(values, s)
}

// signupDisplayName is the token's `name` claim when it is a string the
// profile itself would accept, else the opaque handle; the second result says
// which (`name-claim` or `handle`), for the signup_completed trail. It is
// display text only; the handle stays `oidc-<account id>` (no provider text in
// a UNIQUE column, the #585 d4 naming rule).
func signupDisplayName(raw map[string]json.RawMessage, handle string) (string, string) {
	var name string
	if err := json.Unmarshal(raw["name"], &name); err != nil || name == "" {
		return handle, "handle"
	}
	if validateAccountProfile(ProfileUpdate{Username: handle, DisplayName: name}) != nil {
		return handle, "handle"
	}
	return name, "name-claim"
}
