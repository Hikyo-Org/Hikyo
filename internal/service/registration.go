package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Registration is the registration policy (#606, #579 as amended; spec
// sections 2.1, 3, 4 and 5): at most one policy per org and one at instance
// scope, administered by that scope's manage-members holder under fresh
// proof, and the only path by which an unsolicited unknown identity becomes
// an account. The sign-up legs that USE a policy are #607 (federated) and
// #608 (local); this file owns the policy itself, its standing delegation,
// its preconditions and the public door `/auth/methods` renders.
type Registration struct {
	DB   *store.DB
	Auth *Auth
	Now  func() time.Time
	// PublicOriginExplicit is config.ExternalOriginExplicit: the
	// `no-public-origin` precondition (#579 d6).
	PublicOriginExplicit bool
	// MailConfigured is the static "mailer configured" predicate of
	// mailer-seam 7.3 (clauses 1 and 2; clause 3 is PublicOriginExplicit). It
	// never dials. Nil reads "unconfigured", fail-closed.
	MailConfigured func(context.Context) bool
}

func (s *Registration) now() time.Time { return nowOr(s.Now) }

func (s *Registration) mailerConfigured(ctx context.Context) bool {
	return s.MailConfigured != nil && s.MailConfigured(ctx)
}

// SelfConfigMailConfigured adapts the active runtime configuration to the
// registration mailer predicate: the captured bundle's prepared mail client
// (mailer-seam 7.3 clauses 1 and 2). A capture refusal (a fenced or restoring
// configuration) reads "unconfigured": the policy must not open on a mail
// transport the runtime will not currently use.
func SelfConfigMailConfigured(sc *SelfConfig) func(context.Context) bool {
	return func(ctx context.Context) bool {
		if sc == nil {
			return false
		}
		bundle, err := sc.Capture(ctx)
		return err == nil && bundle.MailConfigured()
	}
}

// Landing kinds (spec 2.1). An org policy lands org-template; an instance
// policy lands none or fresh-org.
const (
	LandingOrgTemplate = "org-template"
	LandingNone        = "none"
	LandingFreshOrg    = "fresh-org"
)

// Write-time precondition names (api-cli-spellings section 8). Each refuses
// 400 naming itself (and the provider row where relevant) in the body.
const (
	PreconditionNoPublicOrigin           = "no-public-origin"
	PreconditionMailerUnconfigured       = "mailer-unconfigured"
	PreconditionProviderDisabled         = "provider-disabled"
	PreconditionProviderMissingEmail     = "provider-missing-email-scope"
	PreconditionCapZero                  = "cap-zero"
	PreconditionTemplateNotOrgApplicable = "template-not-org-applicable"
)

// Use-time inactive causes (#579 d6/d11). The row stays; sign-ups refuse
// uniformly with the audited cause (#607, #608).
const (
	InactiveAuthorityLost       = "authority-lost"
	InactiveAuthorityUnassigned = "authority-unassigned"
	InactivePrecondition        = "precondition"
)

// RegistrationLanding is where a sign-up arrives.
type RegistrationLanding struct {
	Kind     string
	Template domain.Template // org-template only
	Cap      int64           // fresh-org only
}

// RegistrationExternalEntry admits one federated provider, optionally behind
// an allowlist of one issuer-specific string claim.
type RegistrationExternalEntry struct {
	Provider domain.ProviderRef
	Claim    string
	Values   []string
	// DisplayName is the provider's, on a view only.
	DisplayName string
}

// RegistrationLocalEntry admits email + password sign-up (#584), optionally
// only for addresses under the listed domains.
type RegistrationLocalEntry struct {
	Domains []string
}

// RegistrationPolicyInput is a full replacement (PUT). The caller becomes the
// authority principal.
type RegistrationPolicyInput struct {
	External []RegistrationExternalEntry
	Local    *RegistrationLocalEntry
	Landing  RegistrationLanding
}

// RegistrationPolicyView is a policy as the Members panel renders it, with
// the standing delegation and preconditions evaluated at read time.
type RegistrationPolicyView struct {
	ID                   string
	Org                  domain.OrgID // "" = instance scope
	External             []RegistrationExternalEntry
	Local                *RegistrationLocalEntry
	Landing              RegistrationLanding
	AuthorityPrincipalID domain.PrincipalID
	Active               bool
	InactiveCause        string
	// InactivePrecondition names the failing precondition when InactiveCause
	// is `precondition`, with the provider when one is involved.
	InactivePrecondition string
	// FreshOrgCount is set for a fresh-org landing: the `n` of `n / cap`.
	FreshOrgCount *int64
	RowVersion    int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SignupDoor is the public sign-up door of one scope (`/auth/methods`).
// Closed (no policy, or an unknown org) and paused (a policy that is
// inactive) are both not open; paused is the one fact the public page may
// show ("Sign-up is paused.", #587 d3). The cause never leaves the Members
// panel.
type SignupDoor struct {
	Open    bool
	Paused  bool
	Methods []SignupMethod
}

// SignupMethod is one way the door admits: a federated provider by
// {kind, slug}, or the local entry (Kind "local", no slug).
type SignupMethod struct {
	Kind string
	Slug string
}

// registrationRefusal is a 400 whose body names the failing item: a request
// member, or a precondition by name (with the provider row where relevant).
// It is decided after authorization, so naming it discloses nothing the
// caller could not read.
type registrationRefusal struct{ detail string }

func (e *registrationRefusal) Error() string {
	return "service: registration policy refused: " + e.detail
}
func (e *registrationRefusal) Unwrap() error      { return domain.ErrInvalid }
func (e *registrationRefusal) SafeDetail() string { return e.detail }

func refuseRegistration(format string, args ...any) error {
	return &registrationRefusal{detail: fmt.Sprintf(format, args...)}
}

func preconditionRefusal(name, item string) error {
	if item == "" {
		return &registrationRefusal{detail: name}
	}
	return &registrationRefusal{detail: name + ": " + item}
}

// ErrRegistrationProofUnavailable refuses a network mutation when no
// reauthentication library is wired: the gate fails closed, never open.
var ErrRegistrationProofUnavailable = fmt.Errorf(
	"%w: service: reauthentication is required to change a registration policy and is not configured",
	domain.ErrConflict)

// ErrRegistrationPolicyRace reports that the policy moved underneath the
// write (a concurrent edit or delete); the caller re-reads and retries.
var ErrRegistrationPolicyRace = fmt.Errorf("%w: service: the registration policy changed underneath this write", domain.ErrConflict)

func registrationOps(org domain.OrgID) (get, put, del authz.Operation) {
	if org == "" {
		return authz.OpRegistrationPolicyGetInstance, authz.OpRegistrationPolicyPutInstance, authz.OpRegistrationPolicyDeleteInstance
	}
	return authz.OpRegistrationPolicyGetOrg, authz.OpRegistrationPolicyPutOrg, authz.OpRegistrationPolicyDeleteOrg
}

func registrationScope(org domain.OrgID) domain.Scope { return domain.Scope{Org: org} }

// insertRegistrationEvent lands an event on the scope's trail: tenant for an
// org policy, instance for the instance policy (spec section 5).
func insertRegistrationEvent(ctx context.Context, r store.Repos, p authz.Proof, org domain.OrgID, ev audit.Event) error {
	if org == "" {
		return r.Audit().InsertInstance(ctx, p, ev)
	}
	return r.Audit().InsertTenant(ctx, p, ev)
}

// validateRegistrationInput is the body-only half of the write: shape, the
// allowlist rules and the named shape preconditions (cap-zero,
// template-not-org-applicable). It depends on the request alone.
func validateRegistrationInput(org domain.OrgID, in RegistrationPolicyInput) (RegistrationPolicyInput, error) {
	out := RegistrationPolicyInput{Landing: in.Landing}
	switch {
	case org != "" && in.Landing.Kind == LandingOrgTemplate:
		if in.Landing.Cap != 0 {
			return out, refuseRegistration("landing.cap: only a fresh-org landing has a cap")
		}
		if _, err := domain.ExpandTemplate(in.Landing.Template, domain.LevelOrg); err != nil {
			return out, preconditionRefusal(PreconditionTemplateNotOrgApplicable, string(in.Landing.Template))
		}
	case org == "" && in.Landing.Kind == LandingNone:
		if in.Landing.Template != "" || in.Landing.Cap != 0 {
			return out, refuseRegistration("landing: a none landing carries no template and no cap")
		}
	case org == "" && in.Landing.Kind == LandingFreshOrg:
		if in.Landing.Template != "" {
			return out, refuseRegistration("landing.template: a fresh-org landing always uses the admin template")
		}
		if in.Landing.Cap <= 0 {
			return out, preconditionRefusal(PreconditionCapZero, "")
		}
	case org != "":
		return out, refuseRegistration("landing.kind: an organisation policy lands org-template")
	default:
		return out, refuseRegistration("landing.kind: an instance policy lands none or fresh-org")
	}
	if len(in.External) == 0 && in.Local == nil {
		return out, refuseRegistration("external: admit at least one way to sign up, or delete the policy")
	}
	seen := map[domain.ProviderRef]bool{}
	for i, e := range in.External {
		at := "external[" + strconv.Itoa(i) + "]"
		ref := domain.ProviderRef{Kind: e.Provider.Kind, Slug: strings.TrimSpace(e.Provider.Slug)}
		switch ref.Kind {
		case domain.ProviderOIDC, domain.ProviderOAuth2:
		default:
			return out, refuseRegistration("%s.provider.kind: %q is not a sign-up provider kind", at, string(ref.Kind))
		}
		if ref.Slug == "" {
			return out, refuseRegistration("%s.provider.slug: required", at)
		}
		if seen[ref] {
			return out, refuseRegistration("%s.provider: %s is admitted twice", at, ref)
		}
		seen[ref] = true
		claim := strings.TrimSpace(e.Claim)
		if claim == "email" {
			// #579 d5: email is never a linking key, so it is never an
			// allowlist claim either.
			return out, refuseRegistration("%s.claim: email is not accepted as an allowlist claim", at)
		}
		if (claim == "") != (len(e.Values) == 0) {
			return out, refuseRegistration("%s: an allowlist claim needs at least one accepted value, and values need a claim", at)
		}
		values := make([]string, 0, len(e.Values))
		for j, v := range e.Values {
			if v == "" {
				return out, refuseRegistration("%s.values[%d]: empty", at, j)
			}
			if slices.Contains(values, v) {
				return out, refuseRegistration("%s.values[%d]: %q is listed twice", at, j, v)
			}
			values = append(values, v)
		}
		out.External = append(out.External, RegistrationExternalEntry{Provider: ref, Claim: claim, Values: values})
	}
	if in.Local != nil {
		local := &RegistrationLocalEntry{Domains: []string{}}
		for j, d := range in.Local.Domains {
			domainName := strings.ToLower(strings.TrimSpace(d))
			if domainName == "" || strings.ContainsAny(domainName, "@ \t") || strings.HasSuffix(domainName, ".") {
				return out, refuseRegistration("local.domains[%d]: %q is not a domain", j, d)
			}
			if slices.Contains(local.Domains, domainName) {
				return out, refuseRegistration("local.domains[%d]: %q is listed twice", j, domainName)
			}
			local.Domains = append(local.Domains, domainName)
		}
		out.Local = local
	}
	return out, nil
}

// resolvedEntry is an input entry bound to its provider row.
type resolvedEntry struct {
	entry      RegistrationExternalEntry
	providerID string
}

// resolveEntryProvider binds a {kind, slug} to a provider row and checks the
// provider preconditions (#598: an oidc row must request the email scope).
// Only oidc rows exist today; oauth2 rows arrive with #609, which replaces
// the not-found branch below with its own table lookup.
func resolveEntryProvider(ctx context.Context, az *authz.TxAuthorizer, at string, ref domain.ProviderRef) (string, string, error) {
	if ref.Kind != domain.ProviderOIDC {
		return "", "", refuseRegistration("%s.provider: unknown provider %s", at, ref)
	}
	p, err := az.ProviderBySlug(ctx, ref.Slug)
	if errors.Is(err, domain.ErrNotFound) {
		return "", "", refuseRegistration("%s.provider: unknown provider %s", at, ref)
	}
	if err != nil {
		return "", "", err
	}
	if !p.Enabled {
		return "", "", preconditionRefusal(PreconditionProviderDisabled, ref.String())
	}
	if !slices.Contains(strings.Fields(p.Scopes), "email") {
		return "", "", preconditionRefusal(PreconditionProviderMissingEmail, ref.String())
	}
	return p.ID, p.DisplayName, nil
}

// verifyRegistrationReauth is the reauthentication half of a policy
// mutation (#579 d8): fresh proof under the existing step-up primitive,
// verified before the transaction and consumed inside it. No session is
// purged or reissued. Local host authority has no session and is exempt.
func (s *Registration) verifyRegistrationReauth(ctx context.Context, actor Actor, proof string) (ReauthEvidence, error) {
	if actor.bearer == "" {
		return ReauthEvidence{kind: reauthEvidenceExempt}, nil
	}
	if s.Auth == nil {
		return ReauthEvidence{}, ErrRegistrationProofUnavailable
	}
	return s.Auth.VerifyReauthProof(ctx, actor.bearer, proof)
}

func (s *Registration) consumeRegistrationReauth(ctx context.Context, az *authz.TxAuthorizer, ev ReauthEvidence, caller domain.PrincipalID) error {
	if ev.kind == reauthEvidenceExempt {
		return nil
	}
	if s.Auth == nil {
		return ErrRegistrationProofUnavailable
	}
	return s.Auth.ConsumeReauthEvidence(ctx, az, ev, caller)
}

// authorityOperation is the operation whose formula the authority principal
// must still satisfy for the landing (#579 d4, #585 d1): the template grant
// at the org for an org policy; org.create for a fresh-org landing (the
// admin template on the new org follows from the same instance grants by
// inheritance); and, for a none landing, which grants nothing, the policy's
// own instance manage-members formula.
func authorityOperation(p authz.RegistrationPolicy) (authz.Operation, domain.Scope) {
	switch p.Landing {
	case LandingOrgTemplate:
		return authz.OpTemplateApplyOrg, domain.Scope{Org: p.OrgID}
	case LandingFreshOrg:
		return authz.OpOrgCreate, domain.Scope{}
	default:
		return authz.OpRegistrationPolicyPutInstance, domain.Scope{}
	}
}

// evaluatePolicy is the standing-delegation and precondition re-check (#579
// d4 and d6) run at every read and, by #607/#608, at every use. It returns
// the view with the state and, for a fresh-org landing, the live org count.
func (s *Registration) evaluatePolicy(ctx context.Context, az *authz.TxAuthorizer, p authz.RegistrationPolicy, mailer bool) (RegistrationPolicyView, error) {
	view := RegistrationPolicyView{
		ID: p.ID, Org: p.OrgID, AuthorityPrincipalID: p.AuthorityPrincipalID,
		Landing:    RegistrationLanding{Kind: p.Landing, Template: domain.Template(p.Template), Cap: p.FreshOrgCap},
		RowVersion: p.RowVersion, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		External: []RegistrationExternalEntry{},
	}
	if p.LocalEnabled {
		view.Local = &RegistrationLocalEntry{Domains: append([]string{}, p.Domains...)}
	}
	precondition := ""
	if !s.PublicOriginExplicit {
		precondition = PreconditionNoPublicOrigin
	} else if p.LocalEnabled && !mailer {
		precondition = PreconditionMailerUnconfigured
	}
	for _, e := range p.Entries {
		entry := RegistrationExternalEntry{
			Provider: domain.ProviderRef{Kind: domain.ProviderKind(e.ProviderKind), Slug: e.ProviderID},
			Claim:    e.Claim, Values: append([]string{}, e.Values...),
		}
		failure := PreconditionProviderDisabled
		if e.ProviderKind == string(domain.ProviderOIDC) {
			row, err := az.ProviderForCallback(ctx, e.ProviderID)
			switch {
			case errors.Is(err, domain.ErrNotFound):
				// The provider row was deleted; nothing auto-deletes the
				// entry (#579 d6), so it names the row id it pointed at.
			case err != nil:
				return RegistrationPolicyView{}, err
			default:
				entry.Provider.Slug, entry.DisplayName = row.Slug, row.DisplayName
				switch {
				case !row.Enabled:
				case !slices.Contains(strings.Fields(row.Scopes), "email"):
					failure = PreconditionProviderMissingEmail
				default:
					failure = ""
				}
			}
		}
		if failure != "" && precondition == "" {
			precondition = failure + ": " + entry.Provider.String()
		}
		view.External = append(view.External, entry)
	}
	slices.SortFunc(view.External, func(a, b RegistrationExternalEntry) int {
		return strings.Compare(a.Provider.String(), b.Provider.String())
	})
	if p.Landing == LandingFreshOrg {
		n, err := az.CountRegistrationPolicyOrgs(ctx, p.ID)
		if err != nil {
			return RegistrationPolicyView{}, err
		}
		view.FreshOrgCount = &n
	}
	switch {
	case p.AuthorityPrincipalID == "":
		view.InactiveCause = InactiveAuthorityUnassigned
	default:
		op, scope := authorityOperation(p)
		holds, err := az.RegistrationAuthorityHolds(ctx, p.AuthorityPrincipalID, op, scope)
		if err != nil {
			return RegistrationPolicyView{}, err
		}
		if !holds {
			view.InactiveCause = InactiveAuthorityLost
		} else if precondition != "" {
			view.InactiveCause = InactivePrecondition
			view.InactivePrecondition = precondition
		}
	}
	view.Active = view.InactiveCause == ""
	return view, nil
}

// Get returns the scope's policy (org "" = instance), or nil when the scope
// has none: closed. The read is recorded as a membership-surface read.
func (s *Registration) Get(ctx context.Context, actor Actor, org domain.OrgID) (*RegistrationPolicyView, error) {
	getOp, _, _ := registrationOps(org)
	mailer := s.mailerConfigured(ctx)
	var out *RegistrationPolicyView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		out = nil
		caller, p, err := authorize(ctx, az, actor, getOp, registrationScope(org), s.now())
		if err != nil {
			return err
		}
		rows := 0
		policy, err := az.RegistrationPolicyFor(ctx, org)
		switch {
		case errors.Is(err, domain.ErrNotFound):
		case err != nil:
			return err
		default:
			view, err := s.evaluatePolicy(ctx, az, policy, mailer)
			if err != nil {
				return err
			}
			out, rows = &view, 1
		}
		ev, err := domainEvent(ctx, audit.EventGrantMembershipRead, caller.Principal,
			audit.Object{Type: "registration-policy", ID: renderScope(registrationScope(org))},
			audit.Payload{"scope": renderScope(registrationScope(org)), "row_count": rows})
		if err != nil {
			return err
		}
		return insertRegistrationEvent(ctx, r, p, org, ev)
	})
	return out, err
}

// Put replaces the scope's policy (creating it when absent) and makes the
// caller its authority principal (#579 d4: any edit reassigns authority).
// Reauth-gated: proof is verified before the transaction and consumed in it;
// the caller's sessions are untouched.
func (s *Registration) Put(ctx context.Context, actor Actor, org domain.OrgID, in RegistrationPolicyInput, proof string) (RegistrationPolicyView, error) {
	_, putOp, _ := registrationOps(org)
	in, err := validateRegistrationInput(org, in)
	if err != nil {
		return RegistrationPolicyView{}, err
	}
	evidence, err := s.verifyRegistrationReauth(ctx, actor, proof)
	if err != nil {
		return RegistrationPolicyView{}, err
	}
	mailer := s.mailerConfigured(ctx)
	var out RegistrationPolicyView
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		caller, err := actor.resolve(ctx, az, now)
		if err != nil {
			return err
		}
		if err := s.consumeRegistrationReauth(ctx, az, evidence, caller.Principal); err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, putOp, registrationScope(org))
		if err != nil {
			return err
		}
		// A fresh-org landing mints orgs under the editor's org.create (#585
		// d1): an editor who cannot create orgs cannot delegate it.
		if in.Landing.Kind == LandingFreshOrg {
			if _, err := az.Authorize(ctx, caller, authz.OpOrgCreate, domain.Scope{}); err != nil {
				return err
			}
		}
		// Write-time preconditions (#579 d6), by name.
		if !s.PublicOriginExplicit {
			return preconditionRefusal(PreconditionNoPublicOrigin, "")
		}
		if in.Local != nil && !mailer {
			return preconditionRefusal(PreconditionMailerUnconfigured, "")
		}
		resolved := make([]resolvedEntry, 0, len(in.External))
		for i, e := range in.External {
			id, _, err := resolveEntryProvider(ctx, az, "external["+strconv.Itoa(i)+"]", e.Provider)
			if err != nil {
				return err
			}
			resolved = append(resolved, resolvedEntry{entry: e, providerID: id})
		}

		row := authz.RegistrationPolicy{
			OrgID: org, AuthorityPrincipalID: caller.Principal, Landing: in.Landing.Kind,
			Template: string(in.Landing.Template), LocalEnabled: in.Local != nil, FreshOrgCap: in.Landing.Cap,
			CreatedAt: now, UpdatedAt: now,
		}
		if in.Local != nil {
			row.Domains = in.Local.Domains
		}
		for _, e := range resolved {
			id, err := newID("rpe")
			if err != nil {
				return err
			}
			row.Entries = append(row.Entries, authz.RegistrationEntry{
				ID: id, ProviderKind: string(e.entry.Provider.Kind), ProviderID: e.providerID,
				Claim: e.entry.Claim, Values: e.entry.Values, CreatedAt: now,
			})
		}
		existing, err := az.RegistrationPolicyFor(ctx, org)
		typ := audit.EventRegistrationPolicyUpdated
		previous := domain.PrincipalID("")
		switch {
		case errors.Is(err, domain.ErrNotFound):
			typ = audit.EventRegistrationPolicyCreated
			if row.ID, err = newID("rpol"); err != nil {
				return err
			}
			if err := az.CreateRegistrationPolicy(ctx, row); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			row.ID, row.CreatedAt, previous = existing.ID, existing.CreatedAt, existing.AuthorityPrincipalID
			ok, err := az.ReplaceRegistrationPolicy(ctx, row, existing.RowVersion)
			if err != nil {
				return err
			}
			if !ok {
				return ErrRegistrationPolicyRace
			}
		}
		saved, err := az.RegistrationPolicyFor(ctx, org)
		if err != nil {
			return err
		}
		if out, err = s.evaluatePolicy(ctx, az, saved, mailer); err != nil {
			return err
		}
		payload := audit.Payload{
			"policy_id": saved.ID, "scope": renderScope(registrationScope(org)),
			"landing": saved.Landing, "authority_principal_id": string(saved.AuthorityPrincipalID),
		}
		if previous != "" && previous != saved.AuthorityPrincipalID {
			payload["previous_authority_principal_id"] = string(previous)
		}
		ev, err := domainEvent(ctx, typ, caller.Principal, audit.Object{Type: "registration-policy", ID: saved.ID}, payload)
		if err != nil {
			return err
		}
		return insertRegistrationEvent(ctx, r, p, org, ev)
	})
	return out, err
}

// Delete closes the scope's registration: the policy row goes, and so do the
// pending local sign-ups it admitted, inside the same transaction, each with
// `registration.signup_expired {cause: policy-deleted}` (spec section 4).
// Reauth-gated like Put; no session is purged.
func (s *Registration) Delete(ctx context.Context, actor Actor, org domain.OrgID, proof string) error {
	_, _, delOp := registrationOps(org)
	evidence, err := s.verifyRegistrationReauth(ctx, actor, proof)
	if err != nil {
		return err
	}
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, s.now())
		if err != nil {
			return err
		}
		if err := s.consumeRegistrationReauth(ctx, az, evidence, caller.Principal); err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, delOp, registrationScope(org))
		if err != nil {
			return err
		}
		existing, err := az.RegistrationPolicyFor(ctx, org)
		if err != nil {
			return err
		}
		pending, err := az.RegistrationSignupsForPolicy(ctx, existing.ID)
		if err != nil {
			return err
		}
		for _, signup := range pending {
			gone, err := az.DeleteRegistrationSignup(ctx, signup.ID)
			if err != nil {
				return err
			}
			if !gone {
				continue
			}
			ev, err := domainEvent(ctx, audit.EventRegistrationSignupExpired, caller.Principal,
				audit.Object{Type: "registration-signup", ID: signup.ID},
				audit.Payload{"signup_id": signup.ID, "policy_id": existing.ID, "cause": "policy-deleted"})
			if err != nil {
				return err
			}
			if err := insertRegistrationEvent(ctx, r, p, org, ev); err != nil {
				return err
			}
		}
		ok, err := az.DeleteRegistrationPolicy(ctx, existing.ID, existing.RowVersion)
		if err != nil {
			return err
		}
		if !ok {
			return ErrRegistrationPolicyRace
		}
		ev, err := domainEvent(ctx, audit.EventRegistrationPolicyDeleted, caller.Principal,
			audit.Object{Type: "registration-policy", ID: existing.ID},
			audit.Payload{
				"policy_id": existing.ID, "scope": renderScope(registrationScope(org)),
				"landing": existing.Landing, "authority_principal_id": string(existing.AuthorityPrincipalID),
			})
		if err != nil {
			return err
		}
		return insertRegistrationEvent(ctx, r, p, org, ev)
	})
}

// SignupDoor renders one scope's public sign-up door (org "" = instance).
// Proof-free public discovery like the rest of `/auth/methods`: an unknown
// org and an org without a policy are the same closed door, byte for byte.
func (s *Registration) SignupDoor(ctx context.Context, org domain.OrgID) (SignupDoor, error) {
	mailer := s.mailerConfigured(ctx)
	door := SignupDoor{Methods: []SignupMethod{}}
	err := tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		door = SignupDoor{Methods: []SignupMethod{}}
		policy, err := az.RegistrationPolicyFor(ctx, org)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		view, err := s.evaluatePolicy(ctx, az, policy, mailer)
		if err != nil {
			return err
		}
		if !view.Active {
			door.Paused = true
			return nil
		}
		door.Open = true
		for _, e := range view.External {
			door.Methods = append(door.Methods, SignupMethod{Kind: string(e.Provider.Kind), Slug: e.Provider.Slug})
		}
		if view.Local != nil {
			door.Methods = append(door.Methods, SignupMethod{Kind: "local"})
		}
		return nil
	})
	return door, err
}
