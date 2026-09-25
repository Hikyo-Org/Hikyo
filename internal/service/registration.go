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
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
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
//
// Build it with NewRegistration: every dependency is required, so a missing
// mail predicate is a construction error rather than a silent "unconfigured".
type Registration struct {
	db   *store.DB
	auth *Auth
	now  func() time.Time
	// publicOriginExplicit is config.ExternalOriginExplicit: the
	// `no-public-origin` precondition (#579 d6).
	publicOriginExplicit bool
	// mailConfigured is the static "mailer configured" predicate of
	// mailer-seam 7.3 (clauses 1 and 2; clause 3 is publicOriginExplicit). It
	// never dials. An error is a fault, never "unconfigured".
	mailConfigured func(context.Context) (bool, error)
}

// RegistrationConfig carries Registration's dependencies. Auth may be nil
// only where no network caller exists (local host authority); a network
// mutation without it fails closed.
type RegistrationConfig struct {
	DB                   *store.DB
	Auth                 *Auth
	Now                  func() time.Time
	PublicOriginExplicit bool
	MailConfigured       func(context.Context) (bool, error)
}

// NewRegistration validates the configuration.
func NewRegistration(c RegistrationConfig) (*Registration, error) {
	if c.DB == nil {
		return nil, errors.New("service: registration needs a datastore")
	}
	if c.MailConfigured == nil {
		return nil, errors.New("service: registration needs the mailer-configured predicate")
	}
	return &Registration{
		db: c.DB, auth: c.Auth, now: func() time.Time { return nowOr(c.Now) },
		publicOriginExplicit: c.PublicOriginExplicit, mailConfigured: c.MailConfigured,
	}, nil
}

// SelfConfigMailConfigured adapts the active runtime configuration to the
// registration mailer predicate: the captured bundle's prepared mail client
// (mailer-seam 7.3 clauses 1 and 2). Only the known fenced states (a
// suspended, restoring or unreconciled configuration) read "unconfigured":
// the policy must not open on a mail transport the runtime will not use.
// Every other capture failure is a fault and propagates.
func SelfConfigMailConfigured(sc *SelfConfig) (func(context.Context) (bool, error), error) {
	if sc == nil {
		return nil, errors.New("service: the mailer predicate needs the runtime configuration")
	}
	return mailPredicate(sc.Capture), nil
}

func mailPredicate(capture func(context.Context) (*runtimeconfig.Bundle, error)) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		bundle, err := capture(ctx)
		switch {
		case errors.Is(err, ErrSelfConfigFenced):
			return false, nil
		case err != nil:
			return false, err
		default:
			return bundle.MailConfigured(), nil
		}
	}
}

// RegistrationScope addresses one policy: the instance, or one org. The zero
// value addresses nothing and is refused, so "no org" can never silently
// mean "the instance".
type RegistrationScope struct {
	org      domain.OrgID
	instance bool
}

// InstanceRegistrationScope addresses the instance policy.
func InstanceRegistrationScope() RegistrationScope { return RegistrationScope{instance: true} }

// OrgRegistrationScope addresses one org's policy.
func OrgRegistrationScope(org domain.OrgID) RegistrationScope { return RegistrationScope{org: org} }

// Instance reports whether the scope is the instance.
func (s RegistrationScope) Instance() bool { return s.instance }

// Org is the addressed org, "" for the instance.
func (s RegistrationScope) Org() domain.OrgID { return s.org }

func (s RegistrationScope) valid() error {
	if s.instance == (s.org != "") {
		return fmt.Errorf("%w: a registration policy is addressed at one org or at the instance", domain.ErrInvalid)
	}
	return nil
}

func (s RegistrationScope) authzScope() domain.Scope { return domain.Scope{Org: s.org} }

func (s RegistrationScope) label() string { return renderScope(s.authzScope()) }

func (s RegistrationScope) ops() (get, put, del authz.Operation) {
	if s.instance {
		return authz.OpRegistrationPolicyGetInstance, authz.OpRegistrationPolicyPutInstance, authz.OpRegistrationPolicyDeleteInstance
	}
	return authz.OpRegistrationPolicyGetOrg, authz.OpRegistrationPolicyPutOrg, authz.OpRegistrationPolicyDeleteOrg
}

// insertEvent lands an event on the scope's trail: tenant for an org policy,
// instance for the instance policy (spec section 5).
func (s RegistrationScope) insertEvent(ctx context.Context, r store.Repos, p authz.Proof, ev audit.Event) error {
	if s.instance {
		return r.Audit().InsertInstance(ctx, p, ev)
	}
	return r.Audit().InsertTenant(ctx, p, ev)
}

// LandingKind is where a sign-up arrives (spec 2.1). An org policy lands
// org-template; an instance policy lands none or fresh-org.
type LandingKind string

const (
	LandingOrgTemplate LandingKind = "org-template"
	LandingNone        LandingKind = "none"
	LandingFreshOrg    LandingKind = "fresh-org"
)

// RegistrationPrecondition names a precondition (api-cli-spellings section
// 8). At write it refuses 400 naming itself (and the provider row where
// relevant); at use it is the named reason behind `precondition`.
type RegistrationPrecondition string

const (
	PreconditionNoPublicOrigin           RegistrationPrecondition = "no-public-origin"
	PreconditionMailerUnconfigured       RegistrationPrecondition = "mailer-unconfigured"
	PreconditionProviderDisabled         RegistrationPrecondition = "provider-disabled"
	PreconditionProviderMissingEmail     RegistrationPrecondition = "provider-missing-email-scope"
	PreconditionCapZero                  RegistrationPrecondition = "cap-zero"
	PreconditionTemplateNotOrgApplicable RegistrationPrecondition = "template-not-org-applicable"
	// PreconditionProviderMissing is a stored entry whose provider row was
	// deleted: nothing auto-deletes entries (#579 d6).
	PreconditionProviderMissing RegistrationPrecondition = "provider-missing"
	// PreconditionProviderKindUnsupported is a provider kind with no
	// configured table yet (`oauth2` until #609).
	PreconditionProviderKindUnsupported RegistrationPrecondition = "provider-kind-unsupported"
)

// InactiveCause is why a stored policy admits nobody (#579 d6/d11). The row
// stays; sign-ups refuse uniformly with the audited cause (#607, #608).
type InactiveCause string

const (
	InactiveAuthorityLost       InactiveCause = "authority-lost"
	InactiveAuthorityUnassigned InactiveCause = "authority-unassigned"
	InactivePrecondition        InactiveCause = "precondition"
)

// SignupMethodKind is one way an open door admits: a federated provider kind
// or the local entry.
type SignupMethodKind string

const (
	SignupMethodOIDC   SignupMethodKind = SignupMethodKind(domain.ProviderOIDC)
	SignupMethodOAuth2 SignupMethodKind = SignupMethodKind(domain.ProviderOAuth2)
	SignupMethodLocal  SignupMethodKind = "local"
)

// RegistrationLanding is where a sign-up arrives.
type RegistrationLanding struct {
	Kind     LandingKind
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
	InactiveCause        InactiveCause
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
	// Landing is the open door's landing kind (#607), for the confirmation
	// step; empty unless Open.
	Landing LandingKind
}

// SignupMethod is one way the door admits: a federated provider by
// {kind, slug}, or the local entry (Kind local, no slug).
type SignupMethod struct {
	Kind SignupMethodKind
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

// preconditionDetail is the named form `<precondition>[: <kind>:<slug>]`.
func preconditionDetail(name RegistrationPrecondition, item string) string {
	if item == "" {
		return string(name)
	}
	return string(name) + ": " + item
}

func preconditionRefusal(name RegistrationPrecondition, item string) error {
	return &registrationRefusal{detail: preconditionDetail(name, item)}
}

// ErrRegistrationProofUnavailable refuses a network mutation when no
// reauthentication library is wired: the gate fails closed, never open.
var ErrRegistrationProofUnavailable = fmt.Errorf(
	"%w: service: reauthentication is required to change a registration policy and is not configured",
	domain.ErrConflict)

// ErrRegistrationPolicyRace reports that the policy moved underneath the
// write (a concurrent edit or delete); the caller re-reads and retries.
var ErrRegistrationPolicyRace = fmt.Errorf("%w: service: the registration policy changed underneath this write", domain.ErrConflict)

// validateRegistrationInput is the body-only half of the write: shape, the
// allowlist rules and the named shape preconditions (cap-zero,
// template-not-org-applicable). It depends on the request alone.
func validateRegistrationInput(scope RegistrationScope, in RegistrationPolicyInput) (RegistrationPolicyInput, error) {
	out := RegistrationPolicyInput{Landing: in.Landing}
	switch {
	case !scope.Instance() && in.Landing.Kind == LandingOrgTemplate:
		if in.Landing.Cap != 0 {
			return out, refuseRegistration("landing.cap: only a fresh-org landing has a cap")
		}
		if _, err := domain.ExpandTemplate(in.Landing.Template, domain.LevelOrg); err != nil {
			return out, preconditionRefusal(PreconditionTemplateNotOrgApplicable, string(in.Landing.Template))
		}
	case scope.Instance() && in.Landing.Kind == LandingNone:
		if in.Landing.Template != "" || in.Landing.Cap != 0 {
			return out, refuseRegistration("landing: a none landing carries no template and no cap")
		}
	case scope.Instance() && in.Landing.Kind == LandingFreshOrg:
		if in.Landing.Template != "" {
			return out, refuseRegistration("landing.template: a fresh-org landing always uses the admin template")
		}
		if in.Landing.Cap <= 0 {
			return out, preconditionRefusal(PreconditionCapZero, "")
		}
	case !scope.Instance():
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

// providerPrecondition is the ONE eligibility rule for a provider row named
// by an entry, shared by the write and every read or use: the row must be
// enabled, and an oidc row must request the email scope, or it cannot assert
// the verified address (#598). "" means eligible.
func providerPrecondition(p authz.OIDCProvider) RegistrationPrecondition {
	switch {
	case !p.Enabled:
		return PreconditionProviderDisabled
	case !slices.Contains(strings.Fields(p.Scopes), "email"):
		return PreconditionProviderMissingEmail
	default:
		return ""
	}
}

// resolvedEntry is an input entry bound to its provider row.
type resolvedEntry struct {
	entry      RegistrationExternalEntry
	providerID string
}

// resolveEntryProvider binds a {kind, slug} to a provider row and applies
// providerPrecondition. Only oidc rows exist today: an `oauth2` entry is
// refused by name until #609 adds its table, which replaces this branch.
func resolveEntryProvider(ctx context.Context, az *authz.TxAuthorizer, at string, ref domain.ProviderRef) (string, error) {
	if ref.Kind != domain.ProviderOIDC {
		return "", preconditionRefusal(PreconditionProviderKindUnsupported, ref.String())
	}
	p, err := az.ProviderBySlug(ctx, ref.Slug)
	if errors.Is(err, domain.ErrNotFound) {
		return "", refuseRegistration("%s.provider: unknown provider %s", at, ref)
	}
	if err != nil {
		return "", err
	}
	if failing := providerPrecondition(p); failing != "" {
		return "", preconditionRefusal(failing, ref.String())
	}
	return p.ID, nil
}

// verifyRegistrationReauth is the reauthentication half of a policy
// mutation (#579 d8): fresh proof under the existing step-up primitive,
// verified before the transaction and consumed inside it. No session is
// purged or reissued. Local host authority has no session and is exempt.
func (s *Registration) verifyRegistrationReauth(ctx context.Context, actor Actor, proof string) (ReauthEvidence, error) {
	if actor.bearer == "" {
		return ReauthEvidence{kind: reauthEvidenceExempt}, nil
	}
	if s.auth == nil {
		return ReauthEvidence{}, ErrRegistrationProofUnavailable
	}
	return s.auth.VerifyReauthProof(ctx, actor.bearer, proof)
}

func (s *Registration) consumeRegistrationReauth(ctx context.Context, az *authz.TxAuthorizer, ev ReauthEvidence, caller domain.PrincipalID) error {
	if ev.kind == reauthEvidenceExempt {
		return nil
	}
	if s.auth == nil {
		return ErrRegistrationProofUnavailable
	}
	return s.auth.ConsumeReauthEvidence(ctx, az, ev, caller)
}

// authorityOperation is the operation whose formula the authority principal
// must still satisfy for the landing (#579 d4, #585 d1): the template grant
// at the org for an org policy; org.create for a fresh-org landing (the
// admin template on the new org follows from the same instance grants by
// inheritance); and, for a none landing, which grants nothing, the policy's
// own instance manage-members formula.
func authorityOperation(p authz.RegistrationPolicy) (authz.Operation, domain.Scope, error) {
	switch LandingKind(p.Landing) {
	case LandingOrgTemplate:
		return authz.OpTemplateApplyOrg, domain.Scope{Org: p.OrgID}, nil
	case LandingFreshOrg:
		return authz.OpOrgCreate, domain.Scope{}, nil
	case LandingNone:
		return authz.OpRegistrationPolicyPutInstance, domain.Scope{}, nil
	default:
		return "", domain.Scope{}, fmt.Errorf("service: registration policy %s has unknown landing %q", p.ID, p.Landing)
	}
}

// evaluatePolicy is the standing-delegation and precondition re-check (#579
// d4 and d6) run at every read and, by #607/#608, at every use, inside the
// caller's transaction. It returns the view with the state and, for a
// fresh-org landing, the live org count.
func (s *Registration) evaluatePolicy(ctx context.Context, az *authz.TxAuthorizer, p authz.RegistrationPolicy) (RegistrationPolicyView, error) {
	view := RegistrationPolicyView{
		ID: p.ID, Org: p.OrgID, AuthorityPrincipalID: p.AuthorityPrincipalID,
		Landing:    RegistrationLanding{Kind: LandingKind(p.Landing), Template: domain.Template(p.Template), Cap: p.FreshOrgCap},
		RowVersion: p.RowVersion, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		External: []RegistrationExternalEntry{},
	}
	precondition := ""
	if !s.publicOriginExplicit {
		precondition = string(PreconditionNoPublicOrigin)
	}
	if p.LocalEnabled {
		view.Local = &RegistrationLocalEntry{Domains: append([]string{}, p.Domains...)}
		if precondition == "" {
			configured, err := s.mailConfigured(ctx)
			if err != nil {
				return RegistrationPolicyView{}, err
			}
			if !configured {
				precondition = string(PreconditionMailerUnconfigured)
			}
		}
	}
	for _, e := range p.Entries {
		entry := RegistrationExternalEntry{
			Provider: domain.ProviderRef{Kind: domain.ProviderKind(e.ProviderKind), Slug: e.ProviderID},
			Claim:    e.Claim, Values: append([]string{}, e.Values...),
		}
		var failing RegistrationPrecondition
		if entry.Provider.Kind != domain.ProviderOIDC {
			failing = PreconditionProviderKindUnsupported
		} else {
			row, err := az.ProviderForCallback(ctx, e.ProviderID)
			switch {
			case errors.Is(err, domain.ErrNotFound):
				// The row was deleted; the entry names the id it pointed at.
				failing = PreconditionProviderMissing
			case err != nil:
				return RegistrationPolicyView{}, err
			default:
				entry.Provider.Slug, entry.DisplayName = row.Slug, row.DisplayName
				failing = providerPrecondition(row)
			}
		}
		if failing != "" && precondition == "" {
			precondition = preconditionDetail(failing, entry.Provider.String())
		}
		view.External = append(view.External, entry)
	}
	slices.SortFunc(view.External, func(a, b RegistrationExternalEntry) int {
		return strings.Compare(a.Provider.String(), b.Provider.String())
	})
	if view.Landing.Kind == LandingFreshOrg {
		n, err := az.CountRegistrationPolicyOrgs(ctx, p.ID)
		if err != nil {
			return RegistrationPolicyView{}, err
		}
		view.FreshOrgCount = &n
	}
	if p.AuthorityPrincipalID == "" {
		view.InactiveCause = InactiveAuthorityUnassigned
	} else {
		op, scope, err := authorityOperation(p)
		if err != nil {
			return RegistrationPolicyView{}, err
		}
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

// Get returns the scope's policy, or nil when the scope has none: closed.
// The read is recorded as a membership-surface read.
func (s *Registration) Get(ctx context.Context, actor Actor, scope RegistrationScope) (*RegistrationPolicyView, error) {
	if err := scope.valid(); err != nil {
		return nil, err
	}
	getOp, _, _ := scope.ops()
	var out *RegistrationPolicyView
	err := tx.Write(ctx, s.db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		out = nil
		caller, p, err := authorize(ctx, az, actor, getOp, scope.authzScope(), s.now())
		if err != nil {
			return err
		}
		rows := 0
		policy, err := az.RegistrationPolicyFor(ctx, scope.Org())
		switch {
		case errors.Is(err, domain.ErrNotFound):
		case err != nil:
			return err
		default:
			view, err := s.evaluatePolicy(ctx, az, policy)
			if err != nil {
				return err
			}
			out, rows = &view, 1
		}
		ev, err := domainEvent(ctx, audit.EventGrantMembershipRead, caller.Principal,
			audit.Object{Type: "registration-policy", ID: scope.label()},
			audit.Payload{"scope": scope.label(), "row_count": rows})
		if err != nil {
			return err
		}
		return scope.insertEvent(ctx, r, p, ev)
	})
	return out, err
}

// Put replaces the scope's policy (creating it when absent) and makes the
// caller its authority principal (#579 d4: any edit reassigns authority).
// Reauth-gated: proof is verified before the transaction and consumed in it;
// the caller's sessions are untouched. Every precondition is evaluated inside
// the write transaction.
func (s *Registration) Put(ctx context.Context, actor Actor, scope RegistrationScope, in RegistrationPolicyInput, proof string) (RegistrationPolicyView, error) {
	if err := scope.valid(); err != nil {
		return RegistrationPolicyView{}, err
	}
	_, putOp, _ := scope.ops()
	in, err := validateRegistrationInput(scope, in)
	if err != nil {
		return RegistrationPolicyView{}, err
	}
	evidence, err := s.verifyRegistrationReauth(ctx, actor, proof)
	if err != nil {
		return RegistrationPolicyView{}, err
	}
	var out RegistrationPolicyView
	err = tx.Write(ctx, s.db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		caller, err := actor.resolve(ctx, az, now)
		if err != nil {
			return err
		}
		if err := s.consumeRegistrationReauth(ctx, az, evidence, caller.Principal); err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, putOp, scope.authzScope())
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
		if !s.publicOriginExplicit {
			return preconditionRefusal(PreconditionNoPublicOrigin, "")
		}
		if in.Local != nil {
			configured, err := s.mailConfigured(ctx)
			if err != nil {
				return err
			}
			if !configured {
				return preconditionRefusal(PreconditionMailerUnconfigured, "")
			}
		}
		resolved := make([]resolvedEntry, 0, len(in.External))
		for i, e := range in.External {
			id, err := resolveEntryProvider(ctx, az, "external["+strconv.Itoa(i)+"]", e.Provider)
			if err != nil {
				return err
			}
			resolved = append(resolved, resolvedEntry{entry: e, providerID: id})
		}

		row := authz.RegistrationPolicy{
			OrgID: scope.Org(), AuthorityPrincipalID: caller.Principal, Landing: string(in.Landing.Kind),
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
		existing, err := az.RegistrationPolicyFor(ctx, scope.Org())
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
		saved, err := az.RegistrationPolicyFor(ctx, scope.Org())
		if err != nil {
			return err
		}
		if out, err = s.evaluatePolicy(ctx, az, saved); err != nil {
			return err
		}
		payload := audit.Payload{
			"policy_id": saved.ID, "scope": scope.label(),
			"landing": saved.Landing, "authority_principal_id": string(saved.AuthorityPrincipalID),
		}
		if previous != "" && previous != saved.AuthorityPrincipalID {
			payload["previous_authority_principal_id"] = string(previous)
		}
		ev, err := domainEvent(ctx, typ, caller.Principal, audit.Object{Type: "registration-policy", ID: saved.ID}, payload)
		if err != nil {
			return err
		}
		return scope.insertEvent(ctx, r, p, ev)
	})
	return out, err
}

// Delete closes the scope's registration: the policy row goes, and so do the
// pending local sign-ups it admitted, inside the same transaction, each with
// `registration.signup_expired {cause: policy-deleted}` (spec section 4).
// Reauth-gated like Put; no session is purged.
func (s *Registration) Delete(ctx context.Context, actor Actor, scope RegistrationScope, proof string) error {
	if err := scope.valid(); err != nil {
		return err
	}
	_, _, delOp := scope.ops()
	evidence, err := s.verifyRegistrationReauth(ctx, actor, proof)
	if err != nil {
		return err
	}
	return tx.Write(ctx, s.db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, s.now())
		if err != nil {
			return err
		}
		if err := s.consumeRegistrationReauth(ctx, az, evidence, caller.Principal); err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, delOp, scope.authzScope())
		if err != nil {
			return err
		}
		existing, err := az.RegistrationPolicyFor(ctx, scope.Org())
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
			if err := scope.insertEvent(ctx, r, p, ev); err != nil {
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
				"policy_id": existing.ID, "scope": scope.label(),
				"landing": existing.Landing, "authority_principal_id": string(existing.AuthorityPrincipalID),
			})
		if err != nil {
			return err
		}
		return scope.insertEvent(ctx, r, p, ev)
	})
}

// SignupDoor renders one scope's public sign-up door. Proof-free public
// discovery like the rest of `/auth/methods`: an unknown org and an org
// without a policy are the same closed door, byte for byte.
func (s *Registration) SignupDoor(ctx context.Context, scope RegistrationScope) (SignupDoor, error) {
	if err := scope.valid(); err != nil {
		return SignupDoor{}, err
	}
	door := SignupDoor{Methods: []SignupMethod{}}
	err := tx.Read(ctx, s.db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		door = SignupDoor{Methods: []SignupMethod{}}
		policy, err := az.RegistrationPolicyFor(ctx, scope.Org())
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		view, err := s.evaluatePolicy(ctx, az, policy)
		if err != nil {
			return err
		}
		if !view.Active {
			door.Paused = true
			return nil
		}
		door.Open, door.Landing = true, view.Landing.Kind
		for _, e := range view.External {
			door.Methods = append(door.Methods, SignupMethod{Kind: SignupMethodKind(e.Provider.Kind), Slug: e.Provider.Slug})
		}
		if view.Local != nil {
			door.Methods = append(door.Methods, SignupMethod{Kind: SignupMethodLocal})
		}
		return nil
	})
	return door, err
}
