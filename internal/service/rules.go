package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Member access rules (member-access-rules ADR, stage A: the backend core).
//
// A rule is stored whole and never modified in place: an edit is a revoke
// plus a create, so the session invalidation is exactly the grant surface's.
// Workload and automation credentials may hold rules under their class's
// allowlist and depth, never narrowed by keys, and a machine rule change goes
// through the machine widening gate like a machine grant.
// Delegation checks run at the authorization chokepoint against the whole
// selector; ordinary scope-wide grant operations remain blind to management rules.

var (
	// ErrRuleMachineKeys refuses a machine rule that names keys or folders.
	// The machine widening gate diffs per-environment reach and would
	// undercount a key-narrowed machine rule.
	ErrRuleMachineKeys = fmt.Errorf("%w: service: a machine principal's rule covers every key; it cannot name keys or folders", domain.ErrInvalid)
	// ErrNoSuchRule is a revoke of a rule that does not exist (or that the
	// caller may not reach, indistinguishably).
	ErrNoSuchRule = fmt.Errorf("%w: service: no such rule", domain.ErrNotFound)
	// ErrRuleAllProjectsNoProject refuses an all-projects rule in an org with
	// no project: a v1 rule response lists the projects a rule covers and is
	// never empty (owner decision 2026-11-25, #1446).
	ErrRuleAllProjectsNoProject = fmt.Errorf("%w: service: an organization with no project cannot hold a rule over all projects; create a project first", domain.ErrConflict)
	// ErrRuleAllProjectsStale refuses an all-projects create whose project
	// list differs from the org's current projects (owner decision R2).
	ErrRuleAllProjectsStale = fmt.Errorf("%w: service: a rule over all projects must list exactly the organization's current projects", domain.ErrConflict)
	// ErrRuleAllProjectsTooLarge refuses an all-projects rule whose listing,
	// its folders repeated under every current project, would pass the v1
	// selector bound (#1446).
	ErrRuleAllProjectsTooLarge = fmt.Errorf("%w: service: a rule over all projects would list more than %d projects and folder items", domain.ErrLimitExceeded, domain.MaxRuleItems)
	// ErrRuleAllProjectsAuthority refuses an all-projects rule change by a
	// caller whose Manage access does not cover projects created later (ADR
	// D7): an organization-scope grant or another all-projects rule.
	ErrRuleAllProjectsAuthority = fmt.Errorf("%w: service: a rule over all projects needs Manage access over every project, including projects created later", domain.ErrConflict)
	// ErrRuleTooLarge refuses a named-projects rule whose listing, one
	// environment item per name in each project, would pass the v1 selector
	// bound (#1447).
	ErrRuleTooLarge = fmt.Errorf("%w: service: a rule would list more than %d selector items", domain.ErrLimitExceeded, domain.MaxRuleItems)
	// ErrRuleEnvItemsStale refuses a rule create whose environment items
	// differ from the environments its names match now in the rule's
	// projects: a client that read a different set, or a pre-revision-22
	// client whose id list a name-bound rule cannot express exactly (#1447).
	ErrRuleEnvItemsStale = fmt.Errorf("%w: service: a rule's environment items must be exactly the environments its names match in its projects", domain.ErrConflict)
	// ErrRuleUnmatchedNamesDropped refuses an all rule sent without names (a
	// client before revision 22, #1447) over a project where the principal's
	// all rules except a name no environment there carries yet: that client
	// cannot show the name, so the new rule would silently lack the exception
	// and reach an environment created or renamed to it later.
	ErrRuleUnmatchedNamesDropped = fmt.Errorf("%w: service: a rule's environment names that match no environment yet must be sent by name", domain.ErrConflict)
)

// MoveWideningError refuses a key folder move that gives people access
// through their rules without an explicit confirmation naming exactly them
// (ADR D9). It is a conflict: the caller is authorized, the resulting state
// needs their consent.
//
// Who gains is administrative information: Principals and Gains are set only
// when the caller holds legacy manage-members on the project (the audience of
// the rule listing). Anyone else learns only Count, and cannot confirm the
// move.
type MoveWideningError struct {
	KeyID      string
	Count      int
	Principals []domain.PrincipalID
	Gains      []WideningGain
}

// WideningGain is one capability a principal gains, with the environments it
// newly reaches (empty for a project-wide atom only).
type WideningGain struct {
	Principal  domain.PrincipalID
	Name       string
	Capability domain.Capability
	Envs       []domain.EnvID
	// Keys is the key selector the gain reaches; nil is every key, and for a
	// key folder move the moved key, which the refusal names already.
	Keys *WideningKeys
}

// WideningKeys is a rule's key selector in Project: the keys a gain reaches.
type WideningKeys struct {
	Project domain.ProjectID
	Mode    domain.AxisMode
	Items   []domain.RuleKeyItem
}

func (e *MoveWideningError) Error() string {
	return fmt.Sprintf("%v: %s", domain.ErrConflict, e.SafeDetail())
}

func (e *MoveWideningError) Unwrap() error { return domain.ErrConflict }

// SafeDetail names the principals only when they were disclosed to the caller.
func (e *MoveWideningError) SafeDetail() string {
	if len(e.Principals) == 0 {
		return fmt.Sprintf("moving key %s widens access for %d people through their access rules; a member manager must confirm it", e.KeyID, e.Count)
	}
	ids := make([]string, 0, len(e.Principals))
	for _, p := range e.Principals {
		ids = append(ids, string(p))
	}
	return fmt.Sprintf("moving key %s widens access for %s; confirm naming exactly them", e.KeyID, strings.Join(ids, ", "))
}

// Widening is the structured half of the refusal the transport renders; nil
// unless the gainers were disclosed.
func (e *MoveWideningError) Widening() (count int, gains []WideningGain) { return e.Count, e.Gains }

// moveWideningError builds the refusal for gains, naming the people only to a
// caller holding legacy manage-members on the project. The grant read happens
// only on this refusal path.
func moveWideningError(ctx context.Context, az *authz.TxAuthorizer, caller domain.PrincipalID, scope domain.Scope,
	keyID string, gains []WideningGain) error {
	rows, err := az.GrantRowsForPrincipal(ctx, caller)
	if err != nil {
		return err
	}
	gained := gainedPrincipals(gains)
	e := &MoveWideningError{KeyID: keyID, Count: len(gained)}
	if !holds(rows, domain.CapManageMembers, domain.Scope{Org: scope.Org, Project: scope.Project}) {
		return e
	}
	names := newPrincipalNames()
	e.Principals = gained
	for _, g := range gains {
		if g.Name, err = names.get(ctx, az, g.Principal); err != nil {
			return err
		}
		e.Gains = append(e.Gains, g)
	}
	return e
}

// mayConfirmWidening reports whether caller may confirm a widening move in
// scope's project: only a holder of legacy manage-members on the project, the
// same people the refusal names the gainers to. Anyone else learns only the
// count and cannot confirm, even by resending ids learned elsewhere.
func mayConfirmWidening(ctx context.Context, az *authz.TxAuthorizer, caller domain.PrincipalID, scope domain.Scope) (bool, error) {
	rows, err := az.GrantRowsForPrincipal(ctx, caller)
	if err != nil {
		return false, err
	}
	return holds(rows, domain.CapManageMembers, domain.Scope{Org: scope.Org, Project: scope.Project}), nil
}

// gainedPrincipals is the sorted set of people gains name.
func gainedPrincipals(gains []WideningGain) []domain.PrincipalID {
	var out []domain.PrincipalID
	for _, g := range gains {
		if !slices.Contains(out, g.Principal) {
			out = append(out, g.Principal)
		}
	}
	slices.Sort(out)
	return out
}

// mergeGains unions b into a per (principal, capability, key selector),
// keeping the order moveWidening produces.
func mergeGains(a, b []WideningGain) []WideningGain {
	for _, g := range b {
		// A project-wide gain (no environments) stays its own entry, so a
		// merge with an environment gain does not hide the project scope.
		i := slices.IndexFunc(a, func(x WideningGain) bool {
			return x.Principal == g.Principal && x.Capability == g.Capability && (len(x.Envs) == 0) == (len(g.Envs) == 0) &&
				x.Keys.selector() == g.Keys.selector()
		})
		if i < 0 {
			a = append(a, WideningGain{Principal: g.Principal, Capability: g.Capability, Envs: slices.Clone(g.Envs), Keys: g.Keys})
			continue
		}
		for _, env := range g.Envs {
			if !slices.Contains(a[i].Envs, env) {
				a[i].Envs = append(a[i].Envs, env)
			}
		}
		slices.Sort(a[i].Envs)
	}
	slices.SortFunc(a, func(x, y WideningGain) int {
		return cmp.Or(
			strings.Compare(string(x.Principal), string(y.Principal)),
			strings.Compare(string(x.Capability), string(y.Capability)),
			cmp.Compare(min(len(x.Envs), 1), min(len(y.Envs), 1)), // the project gain first
			strings.Compare(x.Keys.selector(), y.Keys.selector()), // every key first
		)
	})
	return a
}

// selector is k's canonical key selector; "" is every key.
func (k *WideningKeys) selector() string {
	if k == nil {
		return ""
	}
	return keySelector(k.Mode, k.Items)
}

// gainAtom is one (principal, capability, environment, key selector) gain;
// env "" is the project.
type gainAtom struct {
	principal  domain.PrincipalID
	capability domain.Capability
	env        domain.EnvID
	// keys is the key selector (keySelector), kept so a plan attributes a gain
	// over one selector to the change causing it; "" is every key.
	keys string
}

// gainsOf merges atoms of project into gains for display and confirmation,
// one per key selector; a gain over every key absorbs a narrowed one for the
// same environment.
func gainsOf(project domain.ProjectID, atoms map[gainAtom]bool) []WideningGain {
	var out []WideningGain
	for a := range atoms {
		every := a
		every.keys = ""
		if a.keys != "" && atoms[every] {
			continue
		}
		g := WideningGain{Principal: a.principal, Capability: a.capability, Keys: keysOf(project, a.keys)}
		if a.env != "" {
			g.Envs = []domain.EnvID{a.env}
		}
		out = mergeGains(out, []WideningGain{g})
	}
	return out
}

// MaxRulesPerOrg is the loud sanity cap on rule rows per organization,
// mirroring MaxGrantsPerOrg: it makes runaway rule minting loud.
const MaxRulesPerOrg = 1000

// Rules owns the rule surface. Auth is the reauthentication seam a machine
// rule widening consumes, as on Grants.
type Rules struct {
	DB   *store.DB
	Auth *Auth
	Now  func() time.Time
}

func (s *Rules) now() time.Time { return nowOr(s.Now) }

// RuleSpec names one rule: who gets which capability, where.
type RuleSpec struct {
	Target     domain.PrincipalID
	Capability domain.Capability
	Org        domain.OrgID
	Where      domain.Where
	// ClaimedProjects is, for an all-projects rule, the project list the
	// client read and showed: it must equal the org's current projects,
	// compared under the create transaction's project lock (owner decision
	// R2, #1446). It is not stored.
	ClaimedProjects []domain.ProjectID
	// Environment selectors bind by name (#1447). Where.EnvNames non-nil
	// means names were sent; Where.Envs is then empty or exactly the
	// environments those names match now in the rule's projects. Without
	// names (a client before revision 22) the names are derived from
	// Where.Envs, which must match them exactly. Only names are stored.
}

// Create writes one rule. Manage access must cover its complete selector,
// and the grantor must hold the delegated capability throughout that selector.
// Existing org- and instance-scope managers retain the grant-unheld exception.
func (s *Rules) Create(ctx context.Context, actor Actor, spec RuleSpec) (RuleView, error) {
	var out RuleView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		add, err := prepareRuleCreate(ctx, az, actor, spec, now)
		if err != nil {
			return err
		}
		rule, caller, p := add.rule, add.caller, add.proof
		widened, err := s.gateMachineRules(ctx, az, caller, add.class, nil, []domain.Rule{rule})
		if err != nil {
			return err
		}
		existing, err := az.RuleIDsForOrg(ctx, spec.Org)
		if err != nil {
			return err
		}
		if len(existing) >= MaxRulesPerOrg {
			return fmt.Errorf("%w: an organization holds at most %d rules", domain.ErrLimitExceeded, MaxRulesPerOrg)
		}
		id, err := newID("rul")
		if err != nil {
			return err
		}
		rule.ID = id
		if err := az.CreateRule(ctx, rule, caller.Principal, now, func() (string, error) { return newID("rli") }); err != nil {
			return err
		}
		if err := invalidateGrantChange(ctx, az, spec.Target); err != nil {
			return err
		}
		if err := insertRuleCreated(ctx, r, p, caller.Principal, rule, add.unheld, widened[0]); err != nil {
			return err
		}
		name, err := newPrincipalNames().get(ctx, az, spec.Target)
		if err != nil {
			return err
		}
		out = RuleView{ID: id, Principal: spec.Target, PrincipalName: name, Capability: spec.Capability,
			Org: spec.Org, Where: rule.Where, Covers: add.covers, CreatedBy: caller.Principal, CreatedAt: now}
		return nil
	})
	return out, err
}

// insertRuleCreated records rule.created and then, for a machine rule that
// made plaintext newly reachable, identity.grant_widened: a separate fact,
// as on the grant surface.
func insertRuleCreated(ctx context.Context, r store.Repos, p authz.Proof, actor domain.PrincipalID,
	rule domain.Rule, unheld bool, widened []audit.Payload) error {
	payload := rulePayload(rule)
	payload["unheld"] = unheld
	object := audit.Object{Type: "rule", ID: rule.ID}
	ev, err := domainEvent(ctx, audit.EventRuleCreated, actor, object, payload)
	if err != nil {
		return err
	}
	if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
		return err
	}
	for _, w := range widened {
		ev, err := domainEvent(ctx, audit.EventMachineGrantWidened, actor, object, w)
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return err
		}
	}
	return nil
}

// gateMachineRules sends a rule change on a machine principal through the
// machine widening gate. Per project the added rules name, the reach of the
// target's grants and rules after the change (removed rules gone, added
// rules in) is diffed against the reach before it. It returns, per added
// rule, the identity.grant_widened payloads to record once the rule exists;
// nil for a person or a change that reaches no new plaintext.
func (s *Rules) gateMachineRules(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity,
	class domain.PrincipalClass, removed []string, added []domain.Rule) ([][]audit.Payload, error) {
	out := make([][]audit.Payload, len(added))
	if !domain.IsMachineClass(class) || len(added) == 0 {
		return out, nil
	}
	target, org := added[0].Principal, added[0].Org
	actorGrants, err := az.GrantRowsForPrincipal(ctx, caller.Principal)
	if err != nil {
		return nil, err
	}
	grants, err := az.GrantsOf(ctx, target)
	if err != nil {
		return nil, err
	}
	held, err := az.MachineRulesOf(ctx, target, class)
	if err != nil {
		return nil, err
	}
	kept := slices.DeleteFunc(slices.Clone(held), func(r domain.Rule) bool { return slices.Contains(removed, r.ID) })
	var projects []domain.ProjectID
	for _, rule := range added {
		for _, project := range rule.Where.Projects {
			if !slices.Contains(projects, project) {
				projects = append(projects, project)
			}
		}
	}
	slices.Sort(projects)
	for _, project := range projects {
		scope := domain.Scope{Org: org, Project: project}
		envs, err := az.EnvironmentsInProject(ctx, scope)
		if err != nil {
			return nil, err
		}
		pre := append(slices.Clone(grants), authz.RuleGrants(held, scope, envs)...)
		post := append(append(slices.Clone(grants), authz.RuleGrants(kept, scope, envs)...), authz.RuleGrants(added, scope, envs)...)
		current, historical, err := gateMachineWidening(ctx, az, s.Auth, caller, actorGrants, scope, envs, pre, post, s.now())
		if err != nil {
			return nil, err
		}
		if len(current) == 0 && len(historical) == 0 {
			continue
		}
		// Attribute the delta rule by rule: each event names what its rule
		// adds on top of the pre-state and the rules before it, so the
		// events together equal the gated delta without any rule claiming
		// another's environments.
		so := slices.Clone(pre)
		for i, rule := range added {
			if !slices.Contains(rule.Where.Projects, project) || rule.Capability != domain.CapRead {
				continue
			}
			next := append(slices.Clone(so), authz.RuleGrants([]domain.Rule{rule}, scope, envs)...)
			b, a := authz.ReachableFrom(scope, envs, so), authz.ReachableFrom(scope, envs, next)
			so = next
			cur, hist := newlyReachable(b.Current, a.Current), newlyReachable(b.Historical, a.Historical)
			if len(cur) == 0 && len(hist) == 0 {
				continue
			}
			out[i] = append(out[i], audit.Payload{
				"target_principal":           string(target),
				"principal_class":            string(class),
				"capability":                 string(rule.Capability),
				"scope":                      renderScope(scope),
				"newly_reachable_current":    envStrings(cur),
				"newly_reachable_historical": envStrings(hist),
			})
		}
	}
	return out, nil
}

// preparedRule is one authorized rule creation, before any write.
type preparedRule struct {
	rule   domain.Rule
	caller authz.Identity
	proof  authz.Proof
	unheld bool
	class  domain.PrincipalClass
	// covers is, for an all-projects rule, the org's projects it was proved
	// in (locked); a response lists them (#1446).
	covers []domain.ProjectID
}

// prepareRuleCreate validates authority before any rule or session mutation.
func prepareRuleCreate(ctx context.Context, az *authz.TxAuthorizer, actor Actor, spec RuleSpec, now time.Time) (preparedRule, error) {
	rule := domain.Rule{Principal: spec.Target, Capability: spec.Capability, Org: spec.Org, Where: spec.Where}
	namesSent := spec.Where.EnvNames != nil
	// Checks before the caller is proved read only the request: request
	// stands in for rule with one name per environment id sent, so no
	// refusal depends on which org, project or environment exists.
	request := rule
	if !namesSent {
		request.Where.EnvNames = sentEnvStandIns(spec.Where.Envs)
	}
	// A sent name takes the environment name grammar: one an environment
	// could never carry (padded, direction controls) would only look like it
	// selects one.
	for _, name := range spec.Where.EnvNames {
		if err := checkName("environment name", name); err != nil {
			return preparedRule{}, err
		}
	}
	if err := request.Validate(); err != nil {
		return preparedRule{}, err
	}
	// An environment item belongs to one of the rule's projects, refused from
	// the request alone: read against the org's environments, an item of an
	// unlisted project would tell its id and name apart before the caller
	// is proved there.
	if !spec.Where.AllProjects {
		for p := range spec.Where.Envs {
			if !slices.Contains(spec.Where.Projects, p) {
				return preparedRule{}, fmt.Errorf("%w: an environment item names a project the rule does not list", domain.ErrInvalid)
			}
		}
	}
	for _, items := range spec.Where.Keys {
		for _, it := range items {
			if it.IsFolder {
				if err := checkKeyFolderPath(it.Folder); err != nil {
					return preparedRule{}, err
				}
			}
		}
	}
	projects := spec.Where.Projects
	if spec.Where.AllProjects {
		// An all-projects rule is proved in every current project and in a
		// future-project witness (ADR D7). The unlocked ids only choose the
		// scopes to authorize; the lock is taken once the caller is proved.
		var err error
		if projects, err = az.RuleOrgProjects(ctx, spec.Org, false); err != nil {
			return preparedRule{}, err
		}
		if len(projects) == 0 {
			return preparedRule{}, refuseAllProjectsWithoutProject(ctx, az, actor, spec.Org, now)
		}
	}
	tooLarge := func(n int) error {
		return &detailErr{err: ErrRuleTooLarge, detail: fmt.Sprintf(
			"a rule is listed with one environment item per name in each of its %d projects: up to %d items, above the %d a rule carries",
			len(projects), n, domain.MaxRuleItems)}
	}
	if !spec.Where.AllProjects {
		// Before authorization only what was sent counts: its names, or the
		// v1 selector of projects, environment ids and keys it carries.
		n := request.Where.ListedItems()
		if !namesSent {
			v1 := request.Where
			v1.EnvNames = nil
			n = v1.ListedItems()
			for _, ids := range spec.Where.Envs {
				n += len(ids)
			}
		}
		if n > domain.MaxRuleItems {
			return preparedRule{}, tooLarge(n)
		}
	}
	// Bind the names to the org's environments in this transaction (#1447),
	// once the request alone is known to be well formed and in bounds.
	// Nothing about them is disclosed before the caller is proved.
	refs, err := az.RuleEnvRefs(ctx, spec.Org, "")
	if err != nil {
		return preparedRule{}, err
	}
	if !namesSent {
		rule.Where.EnvNames = envNamesOf(refs, spec.Where.Envs)
	}
	rule.Where = rule.Where.BindEnvs(refs)
	// The caller is proved over the sent ids as it would be over existing
	// environments: an id matching none of its project's adds a name no
	// environment can carry, which only an all-environments authority holds.
	// Dropping it instead would let a narrow caller tell unknown ids apart.
	proved := rule
	if !namesSent {
		proved.Where.EnvNames = append(slices.Clone(rule.Where.EnvNames), unmatchedEnvStandIns(refs, spec.Where.Envs)...)
		proved.Where = proved.Where.BindEnvs(refs)
	}
	first := domain.Scope{Org: spec.Org, Project: projects[0]}
	caller, proof, err := authorizeRule(ctx, az, actor, authz.OpRuleCreate, first, proved, now)
	if err != nil {
		return preparedRule{}, err
	}
	for _, project := range projects[1:] {
		if _, err := az.AuthorizeRule(ctx, caller, authz.OpRuleCreate, domain.Scope{Org: spec.Org, Project: project}, proved); err != nil {
			return preparedRule{}, err
		}
	}
	// The names the sent ids carry, now that the caller is proved: an id no
	// environment has leaves an only list naming nothing.
	if err := rule.Validate(); err != nil {
		return preparedRule{}, err
	}
	if n := rule.Where.ListedItems(); !spec.Where.AllProjects && n > domain.MaxRuleItems {
		return preparedRule{}, tooLarge(n)
	}
	if spec.Where.AllProjects {
		// Lock the org's projects so its last one cannot be deleted under the
		// new rule, and prove the caller in any project that appeared since.
		locked, err := az.RuleOrgProjects(ctx, spec.Org, true)
		if err != nil {
			return preparedRule{}, err
		}
		if len(locked) == 0 {
			return preparedRule{}, refuseAllProjectsWithoutProject(ctx, az, actor, spec.Org, now)
		}
		for _, project := range locked {
			if slices.Contains(projects, project) {
				continue
			}
			if _, err := az.AuthorizeRule(ctx, caller, authz.OpRuleCreate, domain.Scope{Org: spec.Org, Project: project}, proved); err != nil {
				return preparedRule{}, err
			}
		}
		projects = locked
	}
	if spec.Where.AllProjects {
		// Only after Manage access is proved in every current project may the
		// refusal name them.
		if err := checkClaimedProjects(spec.ClaimedProjects, projects); err != nil {
			return preparedRule{}, err
		}
		folders, names := len(spec.Where.Keys[domain.AnyProject]), len(rule.Where.EnvNames)
		if n := domain.AllProjectsExpansion(len(projects), folders, names); n > domain.MaxRuleItems {
			return preparedRule{}, &detailErr{err: ErrRuleAllProjectsTooLarge, detail: fmt.Sprintf(
				"a rule over all projects is listed with its %d folder items and up to one environment item for each of its %d names under each of the organization's %d projects: up to %d items, above the %d a rule carries",
				folders, names, len(projects), n, domain.MaxRuleItems)}
		}
	}
	// Only after Manage access is proved in every covered project may the
	// refusal name environments.
	if err := checkClaimedEnvs(rule.Where, projects, spec.Where.Envs, namesSent); err != nil {
		return preparedRule{}, err
	}
	if !namesSent {
		if err := checkHiddenExceptionsKept(ctx, az, caller, spec.Org, spec.Target, rule.Where, projects, refs); err != nil {
			return preparedRule{}, err
		}
	}
	grantorGrants, err := az.GrantRowsForPrincipal(ctx, caller.Principal)
	if err != nil {
		return preparedRule{}, err
	}
	if err := checkTargetFootprint(ctx, az, grantorGrants, caller.Principal, spec.Target, spec.Org); err != nil {
		return preparedRule{}, err
	}
	if err := az.LockTargetPrincipal(ctx, spec.Target); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return preparedRule{}, ErrUnknownPrincipal
		}
		return preparedRule{}, err
	}
	class, err := az.PrincipalClass(ctx, spec.Target)
	if errors.Is(err, domain.ErrNotFound) {
		return preparedRule{}, ErrUnknownPrincipal
	}
	if err != nil {
		return preparedRule{}, err
	}
	if class != domain.ClassHuman {
		if err := checkMachineRule(ctx, az, class, rule); err != nil {
			return preparedRule{}, err
		}
	}
	if err := checkRuleReferences(ctx, az, rule); err != nil {
		return preparedRule{}, err
	}
	// Management and capability reach are independent. Only existing org-
	// and instance-scope managers retain the grant-unheld exception.
	unheld := false
	for _, project := range projects {
		scope := domain.Scope{Org: spec.Org, Project: project}
		held, err := az.RuleCapabilityHeld(ctx, caller, rule, project)
		if err != nil {
			return preparedRule{}, err
		}
		if held {
			continue
		}
		if !mayGrantUnheld(grantorGrants, scope) {
			return preparedRule{}, ErrGrantorLacksCapability
		}
		unheld = true
	}
	if spec.Where.AllProjects {
		manage, held, err := az.RuleFutureProjectsHeld(ctx, caller, rule)
		if err != nil {
			return preparedRule{}, err
		}
		if !manage {
			return preparedRule{}, ErrRuleAllProjectsAuthority
		}
		if !held {
			if !mayGrantUnheld(grantorGrants, domain.Scope{Org: spec.Org}) {
				return preparedRule{}, ErrGrantorLacksCapability
			}
			unheld = true
		}
	}
	var covers []domain.ProjectID
	if spec.Where.AllProjects {
		covers = projects
	}
	return preparedRule{rule: rule, caller: caller, proof: proof, unheld: unheld, class: class, covers: covers}, nil
}

// envNamesOf derives the names of a pre-revision-22 request's environment
// items. An id naming no environment contributes nothing, so the exact
// comparison in checkClaimedEnvs refuses it.
func envNamesOf(refs []domain.EnvRef, claimed map[domain.ProjectID][]domain.EnvID) []string {
	var names []string
	for _, e := range refs {
		if slices.Contains(claimed[e.Project], e.ID) && !slices.Contains(names, e.Name) {
			names = append(names, e.Name)
		}
	}
	return names
}

// unmatchedEnvStandIns names each sent id no environment of its project has
// with a name checkName refuses, so no environment ever matches it.
func unmatchedEnvStandIns(refs []domain.EnvRef, claimed map[domain.ProjectID][]domain.EnvID) []string {
	var names []string
	for project, ids := range claimed {
		for _, id := range ids {
			if !slices.ContainsFunc(refs, func(e domain.EnvRef) bool { return e.Project == project && e.ID == id }) {
				names = append(names, "\x00"+string(id))
			}
		}
	}
	return names
}

// sentEnvStandIns is one stand-in name per distinct environment id sent, so
// a rule's shape and size are checked from the request alone.
func sentEnvStandIns(claimed map[domain.ProjectID][]domain.EnvID) []string {
	var names []string
	for _, ids := range claimed {
		for _, id := range ids {
			if !slices.Contains(names, string(id)) {
				names = append(names, string(id))
			}
		}
	}
	return names
}

// checkClaimedEnvs refuses a create whose environment items are not exactly
// the environments the bound names match in the covered projects. With names
// sent, an empty item list claims nothing.
func checkClaimedEnvs(w domain.Where, projects []domain.ProjectID, claimed map[domain.ProjectID][]domain.EnvID, namesSent bool) error {
	n := 0
	for _, ids := range claimed {
		n += len(ids)
	}
	if namesSent && n == 0 {
		return nil
	}
	var missing, extra []string
	for p, ids := range claimed {
		for i, id := range ids {
			if !slices.Contains(projects, p) || !slices.Contains(w.Envs[p], id) || slices.Contains(ids[:i], id) {
				extra = append(extra, string(p)+"/"+string(id))
			}
		}
	}
	for _, p := range projects {
		for _, id := range w.Envs[p] {
			if !slices.Contains(claimed[p], id) {
				missing = append(missing, string(p)+"/"+string(id))
			}
		}
	}
	if len(missing)+len(extra) == 0 {
		return nil
	}
	slices.Sort(missing)
	slices.Sort(extra)
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "matched by its names but not listed "+strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		parts = append(parts, "listed but not matched by its names "+strings.Join(extra, ", "))
	}
	return &detailErr{err: ErrRuleEnvItemsStale, detail: "a rule's environment items are the environments its names match in each of its projects: " + strings.Join(parts, "; ")}
}

// checkClaimedProjects refuses an all-projects create whose project list is
// not exactly the org's current projects: a stale list (a project created or
// deleted since the client read it) or a duplicate fails closed, naming the
// difference.
func checkClaimedProjects(claimed, current []domain.ProjectID) error {
	var missing, extra, dup []string
	for i, p := range claimed {
		switch {
		case slices.Contains(claimed[:i], p):
			dup = append(dup, string(p))
		case !slices.Contains(current, p):
			extra = append(extra, string(p))
		}
	}
	for _, p := range current {
		if !slices.Contains(claimed, p) {
			missing = append(missing, string(p))
		}
	}
	if len(missing)+len(extra)+len(dup) == 0 {
		return nil
	}
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "missing "+strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		parts = append(parts, "not in the organization "+strings.Join(extra, ", "))
	}
	if len(dup) > 0 {
		parts = append(parts, "listed twice "+strings.Join(dup, ", "))
	}
	return &detailErr{err: ErrRuleAllProjectsStale, detail: "a rule over all projects must list exactly the organization's current projects: " + strings.Join(parts, "; ")}
}

// refuseAllProjectsWithoutProject answers an all-projects rule in an org with
// no project. Only a caller the chokepoint proves a member manager of the org
// (in an org without projects nothing narrower exists) learns why; anyone else
// gets the chokepoint's own refusal, as for a missing rule.
func refuseAllProjectsWithoutProject(ctx context.Context, az *authz.TxAuthorizer, actor Actor, org domain.OrgID, now time.Time) error {
	if _, _, err := authorize(ctx, az, actor, authz.OpRuleListOrg, domain.Scope{Org: org}, now); err != nil {
		return err
	}
	return &detailErr{err: ErrRuleAllProjectsNoProject, detail: "an organization with no project cannot hold a rule over all projects; create a project first"}
}

// checkMachineRule holds a machine rule to the class's allowlist, to every
// key, to the class's depth and to the owning project, with the grant
// surface's named refusals.
func checkMachineRule(ctx context.Context, az *authz.TxAuthorizer, class domain.PrincipalClass, rule domain.Rule) error {
	switch domain.CheckMachineRule(class, rule) {
	case domain.MachineRuleOK:
	case domain.MachineRuleCapability:
		if rule.Capability == domain.CapReveal || rule.Capability == domain.CapRevealHistory {
			return fmt.Errorf("%w: class %q holds %q only through a grant under the project's machine-reveal opt-in", ErrMachineCapability, class, rule.Capability)
		}
		return fmt.Errorf("%w: class %q may not hold %q", ErrMachineCapability, class, rule.Capability)
	case domain.MachineRuleKeys:
		return ErrRuleMachineKeys
	default:
		depth, _ := domain.MachineScopeDepth(class)
		if depth == domain.LevelEnv {
			return fmt.Errorf("%w: a class %q rule names one project and only listed environments", ErrMachineScope, class)
		}
		return fmt.Errorf("%w: a class %q rule names exactly one project", ErrMachineScope, class)
	}
	// A rule's project comes only from the service-account row; the prior-grant
	// fallback checkMachineProject keeps for grants would let a machine with no
	// row (provisioning, instance, pre-table) hold a rule in any project.
	if _, err := az.ServiceAccountByPrincipal(ctx, rule.Principal); errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("%w: this machine principal has no service-account row, so it may not hold a rule", ErrMachineProject)
	} else if err != nil {
		return err
	}
	return checkMachineProject(ctx, az, rule.Principal, domain.Scope{Org: rule.Org, Project: rule.Where.Projects[0]})
}

// RuleView is one rule on the listing surface. On a project listing Where
// holds only the addressed project's part and OtherProjects reports that the
// rule also names projects the listing does not show. Environments and keys
// are ids (and folder paths): their names need `read`, which a member manager
// may not hold.
type RuleView struct {
	ID            string
	Principal     domain.PrincipalID
	PrincipalName string
	Capability    domain.Capability
	Org           domain.OrgID
	// Where is bound to the environments its names match now, so a response
	// lists them per project (#1447).
	Where domain.Where
	// Covers lists, for an all-projects rule, the projects it reaches at read
	// time: the org's current projects, or only the addressed one on a
	// project listing. A v1 response renders them as `projects`.
	Covers        []domain.ProjectID
	OtherProjects bool
	CreatedBy     domain.PrincipalID
	CreatedAt     time.Time
	// Fault is the manager-facing reason the upgrade to environment names
	// disabled the rule; such a rule reaches nothing until replaced.
	Fault string
}

// ruleListOps is the rule listing's operation per addressed depth, mirroring
// the grant listing.
var ruleListOps = map[domain.Level]authz.Operation{
	domain.LevelOrg:     authz.OpRuleListOrg,
	domain.LevelProject: authz.OpRuleListProject,
}

// List returns the rules in the addressed org, or those naming the addressed
// project (reading only that project's items), audited as a membership read
// like the grant listing.
func (s *Rules) List(ctx context.Context, actor Actor, scope domain.Scope) ([]RuleView, error) {
	level, err := scope.Level()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrInvalid, err)
	}
	op, ok := ruleListOps[level]
	if !ok {
		return nil, fmt.Errorf("%w: rules are listed at org or project scope", domain.ErrInvalid)
	}
	var out []RuleView
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		var caller authz.Identity
		var p authz.Proof
		var err error
		if level == domain.LevelProject {
			caller, err = actor.resolve(ctx, az, s.now())
			if err == nil {
				az.SetClock(s.now())
				p, err = az.AuthorizeRuleList(ctx, caller, scope)
			}
		} else {
			caller, p, err = authorize(ctx, az, actor, op, scope, s.now())
		}
		if err != nil {
			return err
		}
		var lines []authz.RuleLine
		if level == domain.LevelProject {
			lines, err = az.RuleLinesInProject(ctx, scope)
		} else {
			lines, err = az.RuleLinesInOrg(ctx, scope.Org)
		}
		if err != nil {
			return err
		}
		// Listed environments are the ones each rule's names match now, in
		// the listed project only on a project listing.
		listed := domain.ProjectID("")
		if level == domain.LevelProject {
			listed = scope.Project
		}
		refs, err := az.RuleEnvRefs(ctx, scope.Org, listed)
		if err != nil {
			return err
		}
		// An all-projects rule spans more than the listed project as soon as
		// the org has another one; it never names a project in its items.
		var orgProjects []domain.ProjectID
		if slices.ContainsFunc(lines, func(l authz.RuleLine) bool { return l.AllProjects }) {
			if orgProjects, err = az.RuleOrgProjects(ctx, scope.Org, false); err != nil {
				return err
			}
		}
		othersInOrg := len(orgProjects) > 1
		covers := orgProjects
		if level == domain.LevelProject {
			covers = []domain.ProjectID{scope.Project}
		}
		var manageable []bool
		if level == domain.LevelProject {
			manageable, err = ruleLinesManageable(ctx, az, caller, scope, lines)
			if err != nil {
				return err
			}
		}
		names := newPrincipalNames()
		out = make([]RuleView, 0, len(lines))
		for i, line := range lines {
			where := lineWhere(line).BindEnvs(refs)
			other := line.OtherProjects || level == domain.LevelProject && line.AllProjects && othersInOrg
			var lineCovers []domain.ProjectID
			if line.AllProjects {
				lineCovers = covers
			}
			if level == domain.LevelProject && !manageable[i] {
				continue
			}

			name, err := names.get(ctx, az, line.Principal)
			if err != nil {
				return err
			}
			out = append(out, RuleView{
				ID: line.ID, Principal: line.Principal, PrincipalName: name, Capability: line.Capability,
				Org: scope.Org, Where: where, Covers: lineCovers, OtherProjects: other,
				CreatedBy: line.CreatedBy, CreatedAt: line.CreatedAt, Fault: line.Fault,
			})
		}
		return insertGrantEvent(ctx, r, p, caller.Principal, level, grantEventInput{
			typ:     audit.EventGrantMembershipRead,
			object:  audit.Object{Type: "scope", ID: renderScope(scope)},
			payload: audit.Payload{"scope": renderScope(scope), "row_count": len(out)},
		})
	})
	return out, err
}

// lineWhere rebuilds a listed rule's selector for rendering and the manage
// filter; an all-projects rule keeps its folders under domain.AnyProject.
func lineWhere(line authz.RuleLine) domain.Where {
	where := whereFromItems(line.Items)
	where.AllProjects = line.AllProjects
	where.EnvMode, where.KeyMode = line.EnvMode, line.KeyMode
	return where
}

// ruleLinesManageable reports, per project rule line, whether the caller may
// manage it: the filter Rules.List and Rules.Resolve share.
func ruleLinesManageable(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, scope domain.Scope, lines []authz.RuleLine) ([]bool, error) {
	targets := make([]domain.Rule, len(lines))
	for i, line := range lines {
		targets[i] = domain.Rule{Org: scope.Org, Capability: line.Capability, Where: lineWhere(line)}
	}
	return az.RulesManageable(ctx, caller, targets, scope.Project)
}

// manageableProjectRules lists the project's rule lines the caller may manage.
func manageableProjectRules(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, scope domain.Scope) ([]authz.RuleLine, error) {
	lines, err := az.RuleLinesInProject(ctx, scope)
	if err != nil {
		return nil, err
	}
	manageable, err := ruleLinesManageable(ctx, az, caller, scope, lines)
	if err != nil {
		return nil, err
	}
	var out []authz.RuleLine
	for i, line := range lines {
		if manageable[i] {
			out = append(out, line)
		}
	}
	return out, nil
}

// checkRuleReferences refuses, with a clear error rather than a foreign-key
// violation, a rule naming a project, environment or key outside its org.
func checkRuleReferences(ctx context.Context, az *authz.TxAuthorizer, rule domain.Rule) error {
	for _, project := range rule.Where.Projects {
		scope := domain.Scope{Org: rule.Org, Project: project}
		if _, err := az.ResolveChain(ctx, scope); err != nil {
			return fmt.Errorf("%w: project %s is not in organization %s", domain.ErrInvalid, project, rule.Org)
		}
		for _, it := range rule.Where.Keys[project] {
			if it.IsFolder {
				continue
			}
			if err := az.RuleKeyExists(ctx, scope, it.KeyID); err != nil {
				return fmt.Errorf("%w: key %s is not in project %s", domain.ErrInvalid, it.KeyID, project)
			}
		}
	}
	return nil
}

// Revoke removes one rule, killing its holder's sessions in the same
// transaction. A rule the caller cannot reach answers exactly like a missing
// one.
func (s *Rules) Revoke(ctx context.Context, actor Actor, org domain.OrgID, id string) error {
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		stored, caller, p, err := prepareRuleRevoke(ctx, az, actor, org, id, now)
		if err != nil {
			return err
		}
		return revokeRule(ctx, r, az, p, caller.Principal, stored, "revoked")
	})
}

func prepareRuleRevoke(ctx context.Context, az *authz.TxAuthorizer, actor Actor, org domain.OrgID, id string, now time.Time) (stored authz.StoredRule, caller authz.Identity, proof authz.Proof, err error) {
	var valid bool
	stored, valid, err = az.GetRule(ctx, id)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return stored, caller, proof, err
	}
	projects := storedProjects(stored)
	all := err == nil && stored.Rule.Org == org && stored.Rule.Where.AllProjects
	if all {
		// The org's projects stand for the rule's; there is always one, as
		// the org's last project cannot be deleted under an all-projects rule.
		if projects, err = az.RuleOrgProjects(ctx, org, false); err != nil {
			return stored, caller, proof, err
		}
	}
	if err != nil || stored.Rule.Org != org || len(projects) == 0 {
		return stored, caller, proof, refuseMissingRule(ctx, az, actor, org, id, now)
	}
	if !valid {
		// Corrupt/pruned rows have no evaluable selector. Preserve cleanup
		// for scope-wide managers without inventing narrow authority.
		caller, proof, err = authorize(ctx, az, actor, authz.OpRuleRevoke, domain.Scope{Org: org, Project: projects[0]}, now)
		if err != nil {
			return
		}
		for _, project := range projects[1:] {
			if _, err = az.Authorize(ctx, caller, authz.OpRuleRevoke, domain.Scope{Org: org, Project: project}); err != nil {
				return
			}
		}
		if all {
			// Corrupt all-projects rows: only an org-scope manager cleans up.
			var rows []authz.GrantRow
			if rows, err = az.GrantRowsForPrincipal(ctx, caller.Principal); err != nil {
				return
			}
			if !holds(rows, domain.CapManageMembers, domain.Scope{Org: org}) {
				err = ErrRuleAllProjectsAuthority
			}
		}
		return
	}
	caller, proof, err = authorizeRule(ctx, az, actor, authz.OpRuleRevoke, domain.Scope{Org: org, Project: projects[0]}, stored.Rule, now)
	if err != nil {
		return stored, caller, proof, err
	}
	for _, project := range projects[1:] {
		if _, err := az.AuthorizeRule(ctx, caller, authz.OpRuleRevoke, domain.Scope{Org: org, Project: project}, stored.Rule); err != nil {
			return stored, caller, proof, err
		}
	}
	if all {
		manage, _, err := az.RuleFutureProjectsHeld(ctx, caller, stored.Rule)
		if err != nil {
			return stored, caller, proof, err
		}
		if !manage {
			return stored, caller, proof, ErrRuleAllProjectsAuthority
		}
	}
	return
}

func authorizeRule(ctx context.Context, az *authz.TxAuthorizer, actor Actor, op authz.Operation, scope domain.Scope, rule domain.Rule, now time.Time) (caller authz.Identity, proof authz.Proof, err error) {
	caller, err = actor.resolve(ctx, az, now)
	if err != nil {
		return
	}
	az.SetClock(now)
	proof, err = az.AuthorizeRule(ctx, caller, op, scope, rule)
	return
}

// refuseMissingRule answers a revoke of a rule that does not exist in org
// exactly like one the caller cannot reach: the chokepoint is asked about a
// project scope that cannot resolve (rule ids never name a project), so the
// same bare not-found comes back and the denial is captured the same way.
func refuseMissingRule(ctx context.Context, az *authz.TxAuthorizer, actor Actor, org domain.OrgID, id string, now time.Time) error {
	if _, _, err := authorize(ctx, az, actor, authz.OpRuleRevoke, domain.Scope{Org: org, Project: domain.ProjectID("rule:" + id)}, now); err != nil {
		return err
	}
	return domain.ErrNotFound
}

// storedProjects lists the projects a stored rule names, from its items, so
// even a rule that no longer validates can be authorized and removed.
func storedProjects(s authz.StoredRule) []domain.ProjectID {
	var out []domain.ProjectID
	for _, it := range s.Items {
		if it.Project != "" && !slices.Contains(out, it.Project) {
			out = append(out, it.Project)
		}
	}
	return out
}

// revokeRule deletes a rule, invalidates its holder's sessions and records
// rule.revoked under the caller's proof.
func revokeRule(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, actor domain.PrincipalID, stored authz.StoredRule, cause string) error {
	deleted, err := az.DeleteRule(ctx, stored.Rule.ID, stored.Rule.Principal)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrNoSuchRule
	}
	if err := invalidateGrantChange(ctx, az, stored.Rule.Principal); err != nil {
		return err
	}
	// A rule that no longer validates carries no Where; render what it
	// covered from its stored items so the trail still says it.
	// An all-projects rule is always rendered from its items: a valid one
	// renders the same, a corrupt one keeps whatever its rows still name.
	shown := stored.Rule
	switch {
	case shown.Where.AllProjects:
		w := whereFromItems(stored.Items)
		w.AllProjects, w.EnvMode, w.KeyMode = true, shown.Where.EnvMode, shown.Where.KeyMode
		shown.Where = w
	case len(shown.Where.Projects) == 0:
		shown.Where = whereFromItems(stored.Items)
	}
	payload := rulePayload(shown)
	payload["cause"] = cause
	ev, err := domainEvent(ctx, audit.EventRuleRevoked, actor, audit.Object{Type: "rule", ID: stored.Rule.ID}, payload)
	if err != nil {
		return err
	}
	return r.Audit().InsertTenant(ctx, p, ev)
}

// whereFromItems rebuilds a Where from stored items for rendering only; it is
// never evaluated. Environment items are rule-wide names (#1447).
func whereFromItems(items []authz.StoredRuleItem) domain.Where {
	w := domain.Where{Keys: map[domain.ProjectID][]domain.RuleKeyItem{}}
	for _, it := range items {
		// A project-less item is a name or an all-projects rule's folder (AnyProject).
		if it.Project != "" && !slices.Contains(w.Projects, it.Project) {
			w.Projects = append(w.Projects, it.Project)
		}
		switch {
		case it.Axis == "env":
			if !slices.Contains(w.EnvNames, it.EnvName) {
				w.EnvNames = append(w.EnvNames, it.EnvName)
			}
		case it.KeyID != "":
			w.Keys[it.Project] = append(w.Keys[it.Project], domain.RuleKeyItem{KeyID: it.KeyID})
		case it.Axis == "folder":
			w.Keys[it.Project] = append(w.Keys[it.Project], domain.RuleKeyItem{Folder: it.Folder, IsFolder: true})
		}
	}
	return w
}

// rulePayload renders a rule for its lifecycle events: projects and keys as
// stable ids, environments as the names the rule stores, prefixed "name:" so
// an entry is never mistaken for an id written before #1447.
func rulePayload(rule domain.Rule) audit.Payload {
	projects := []string{}
	envs := []string{}
	for _, n := range rule.Where.EnvNames {
		envs = append(envs, "name:"+audit.SanitizeFreeText(n))
	}
	keys := []string{}
	for _, project := range rule.Where.Projects {
		projects = append(projects, string(project))
		for _, k := range rule.Where.Keys[project] {
			if k.IsFolder {
				keys = append(keys, "folder:"+string(project)+"/"+audit.SanitizeFreeText(k.Folder))
			} else {
				keys = append(keys, "key:"+k.KeyID)
			}
		}
	}
	projectMode := "only"
	if rule.Where.AllProjects {
		projectMode = "all"
		for _, k := range rule.Where.Keys[domain.AnyProject] {
			keys = append(keys, "folder:*/"+audit.SanitizeFreeText(k.Folder))
		}
	}
	envMode, keyMode := string(rule.Where.EnvMode), string(rule.Where.KeyMode)
	if envMode == "" {
		envMode = string(domain.AxisAll)
	}
	if keyMode == "" {
		keyMode = string(domain.AxisAll)
	}
	return audit.Payload{
		"target_principal": string(rule.Principal),
		"capability":       string(rule.Capability),
		"project_mode":     projectMode,
		"projects":         projects,
		"env_mode":         envMode,
		"envs":             envs,
		"key_mode":         keyMode,
		"keys":             keys,
	}
}

// pruneRules is the deletion paths' half: for every rule in ids, drop the
// items `gone` matches (they name an object being deleted), and delete, with
// rule.revoked, every rule that no longer validates as a result (an only-list
// that lost its last item reaches nothing). Pruning an item naming a deleted
// object never changes what a rule reaches among objects that still exist,
// and ids are never reused, so nothing left behind can match a new object.
func pruneRules(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, actor domain.PrincipalID,
	ids []string, gone func(authz.StoredRuleItem) bool) error {
	for _, id := range ids {
		stored, _, err := az.GetRule(ctx, id)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		for _, it := range stored.Items {
			if gone(it) {
				if err := az.DeleteRuleItem(ctx, id, it.ID, stored.Rule.Principal); err != nil {
					return err
				}
			}
		}
		after, valid, err := az.GetRule(ctx, id)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			// Every item went: remove the bare row.
			if err := revokeRule(ctx, r, az, p, actor, authz.StoredRule{Rule: stored.Rule}, "emptied-by-deletion"); err != nil {
				return err
			}
		case err != nil:
			return err
		case !valid:
			if err := revokeRule(ctx, r, az, p, actor, after, "emptied-by-deletion"); err != nil {
				return err
			}
		}
	}
	return nil
}

// releaseKeyRules prunes a key being deleted out of every rule naming it.
func releaseKeyRules(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, actor domain.PrincipalID, scope domain.Scope, keyID string) error {
	project := domain.Scope{Org: scope.Org, Project: scope.Project}
	ids, err := az.RuleIDsForKey(ctx, project, keyID)
	if err != nil {
		return err
	}
	return pruneRules(ctx, r, az, p, actor, ids, func(it authz.StoredRuleItem) bool {
		return it.Project == scope.Project && it.KeyID == keyID
	})
}

// releaseProjectRules prunes a project being deleted out of every rule
// naming it (its environments and keys are already gone).
func releaseProjectRules(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, actor domain.PrincipalID, scope domain.Scope) error {
	ids, err := az.RuleIDsForProject(ctx, scope)
	if err != nil {
		return err
	}
	return pruneRules(ctx, r, az, p, actor, ids, func(it authz.StoredRuleItem) bool {
		return it.Project == scope.Project
	})
}

// releaseOrgRules removes every rule in an org being deleted.
func releaseOrgRules(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, actor domain.PrincipalID, org domain.OrgID) error {
	ids, err := az.RuleIDsForOrg(ctx, org)
	if err != nil {
		return err
	}
	for _, id := range ids {
		stored, _, err := az.GetRule(ctx, id)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if err := revokeRule(ctx, r, az, p, actor, stored, "scope-deleted"); err != nil {
			return err
		}
	}
	return nil
}

// moveWideningCaps are the capabilities a folder move can widen through a
// rule: key-shaped atoms, including selector-bound delegation authority.
var moveWideningCaps = []domain.Capability{
	domain.CapEdit, domain.CapPublish, domain.CapReveal, domain.CapRevealHistory, domain.CapDefinitionsEdit, domain.CapManageMembers,
}

// moveWidening computes, inside the moving transaction, what people gain on
// the key through their RULES when it moves from one folder to another (ADR
// D9): each capability with the environments it newly reaches. Legacy grants
// are folder-blind and cannot change with a move, so only rules are consulted.
func moveWidening(ctx context.Context, az *authz.TxAuthorizer, scope domain.Scope, envIDs []string, keyID, from, to string) ([]WideningGain, error) {
	if from == to {
		return nil, nil
	}
	rules, err := az.RulesForProject(ctx, scope.Org, scope.Project)
	if err != nil {
		return nil, err
	}
	type atom struct {
		principal  domain.PrincipalID
		capability domain.Capability
		env        domain.EnvID // "" for the project-wide atom
	}
	reach := func(folder string) map[atom]bool {
		out := map[atom]bool{}
		key := &domain.RuleKey{ID: keyID, Folder: folder}
		for _, rule := range rules {
			for _, c := range moveWideningCaps {
				evaluated, capability := rule, c
				if c == domain.CapManageMembers && rule.Capability == c {
					// Ordinary operations cannot use management rules. The move
					// census nevertheless tracks their selector reach as Edit,
					// which shares exactly the same key/environment shape.
					evaluated.Capability, capability = domain.CapEdit, domain.CapEdit
				}
				if evaluated.Reaches(capability, domain.LevelProject, domain.Scope{Org: scope.Org, Project: scope.Project}, key) {
					out[atom{rule.Principal, c, ""}] = true
				}
				for _, env := range envIDs {
					s := domain.Scope{Org: scope.Org, Project: scope.Project, Env: domain.EnvID(env)}
					if evaluated.Reaches(capability, domain.LevelEnv, s, key) {
						out[atom{rule.Principal, c, domain.EnvID(env)}] = true
					}
				}
			}
		}
		return out
	}
	before, after := reach(from), reach(to)
	var gains []WideningGain
	for a := range after {
		if before[a] {
			continue
		}
		g := WideningGain{Principal: a.principal, Capability: a.capability}
		if a.env != "" {
			g.Envs = []domain.EnvID{a.env}
		}
		gains = mergeGains(gains, []WideningGain{g})
	}
	return gains, nil
}

// confirmMoveWidening refuses a widening move unless the confirmation names
// exactly the gaining set, and records the confirmed set when it does.
func confirmMoveWidening(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, actor domain.PrincipalID,
	scope domain.Scope, keyID, from, to string, gains []WideningGain, confirmed []domain.PrincipalID) error {
	gained := gainedPrincipals(gains)
	if len(gained) == 0 {
		return nil
	}
	want := slices.Clone(confirmed)
	slices.Sort(want)
	want = slices.Compact(want)
	if !slices.Equal(want, gained) {
		return moveWideningError(ctx, az, actor, scope, keyID, gains)
	}
	if ok, err := mayConfirmWidening(ctx, az, actor, scope); err != nil {
		return err
	} else if !ok {
		return moveWideningError(ctx, az, actor, scope, keyID, gains)
	}
	names := make([]string, 0, len(gained))
	for _, g := range gained {
		names = append(names, string(g))
	}
	ev, err := domainEvent(ctx, audit.EventRuleMoveWideningConfirmed, actor, audit.Object{Type: "key", ID: keyID}, audit.Payload{
		"key_id":      keyID,
		"from_folder": audit.SanitizeFreeText(from),
		"to_folder":   audit.SanitizeFreeText(to),
		"principals":  names,
	})
	if err != nil {
		return err
	}
	return r.Audit().InsertTenant(ctx, p, ev)
}

// checkHiddenExceptionsKept refuses ErrRuleUnmatchedNamesDropped when an
// `all` rule sent without names leaves out a name that one of the target's
// `all` rules (including any this replacement revokes, still stored) excepts
// in a project both cover, and that no environment of that project carries.
// A client before revision 22 sees exceptions only as environments, so it
// could not have chosen to drop such a name. Only held rules the caller may
// manage in the project count: the proof covers the submitted selector, not
// the target's other rules, so a rule outside the caller's folders is neither
// named nor lets the refusal reveal that it exists.
func checkHiddenExceptionsKept(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, org domain.OrgID, target domain.PrincipalID,
	w domain.Where, projects []domain.ProjectID, refs []domain.EnvRef) error {
	if w.EnvMode != domain.AxisAll {
		return nil
	}
	var hidden []string
	for _, project := range projects {
		census, err := az.RuleCensusForProject(ctx, org, project)
		if err != nil {
			return err
		}
		var candidates []domain.Rule
		for _, cr := range census {
			held := cr.Rule
			if held.Principal != target || held.Where.EnvMode != domain.AxisAll || !held.Where.Names(project) {
				continue
			}
			candidates = append(candidates, held)
		}
		if len(candidates) == 0 {
			continue
		}
		manageable, err := az.RulesManageable(ctx, caller, candidates, project)
		if err != nil {
			return err
		}
		for i, held := range candidates {
			if !manageable[i] {
				continue
			}
			for _, name := range held.Where.EnvNames {
				carried := slices.ContainsFunc(refs, func(e domain.EnvRef) bool { return e.Project == project && e.Name == name })
				if !carried && !slices.Contains(w.EnvNames, name) && !slices.Contains(hidden, name) {
					hidden = append(hidden, name)
				}
			}
		}
	}
	if len(hidden) == 0 {
		return nil
	}
	slices.Sort(hidden)
	return &detailErr{err: ErrRuleUnmatchedNamesDropped, detail: "this principal's rules except environment names no environment of these projects carries yet (" +
		strings.Join(hidden, ", ") + "); send the rule's environments as names (API revision 22) to keep or drop them explicitly"}
}
