package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Environment selectors bind by name (member-access-rules ADR D3, #1447), so
// renaming, creating or deleting an environment can change what a rule
// reaches: renamed or deleted out of an `all` list's exceptions, or renamed or
// created into an `only` list. A delete widens the rule to the project-wide
// atoms once no environment it excepts is left there; its name stays, so a
// later environment of that name is excepted again. Like a key folder move (D9), such a change needs an explicit
// confirmation naming exactly the principals who gain, people and machines,
// and a machine gaining access also passes the machine widening gate.

// EnvWideningError refuses an environment rename, create or delete that gives
// principals access through their rules without a confirmation naming exactly
// them. Principals and Gains are disclosed only to a caller holding legacy
// manage-members on the project, as for MoveWideningError.
type EnvWideningError struct {
	EnvName string
	// Deleting is set when the refused change deletes EnvName.
	Deleting bool
	// Plan is set when no single environment change of a definitions plan
	// explains its gains: several changes, a key move beside them, or only
	// the changes together widen. EnvName is then empty.
	Plan       bool
	Count      int
	Principals []domain.PrincipalID
	Gains      []WideningGain
}

func (e *EnvWideningError) Error() string {
	return fmt.Sprintf("%v: %s", domain.ErrConflict, e.SafeDetail())
}

func (e *EnvWideningError) Unwrap() error { return domain.ErrConflict }

// SafeDetail names the principals only when they were disclosed to the caller.
func (e *EnvWideningError) SafeDetail() string {
	change, widens := fmt.Sprintf("naming an environment %q", e.EnvName), "widens"
	switch {
	case e.Plan:
		change, widens = "this plan's changes", "widen"
	case e.Deleting:
		change = fmt.Sprintf("deleting the environment %q", e.EnvName)
	}
	if len(e.Principals) == 0 {
		return fmt.Sprintf("%s %s access for %d principals through their access rules; a member manager must confirm it", change, widens, e.Count)
	}
	ids := make([]string, 0, len(e.Principals))
	for _, p := range e.Principals {
		ids = append(ids, string(p))
	}
	return fmt.Sprintf("%s %s access for %s; confirm naming exactly them", change, widens, strings.Join(ids, ", "))
}

// Widening is the structured half of the refusal the transport renders.
func (e *EnvWideningError) Widening() (count int, gains []WideningGain) { return e.Count, e.Gains }

// envWideningCaps are every rule capability an environment selector can
// narrow (environment- and key-shaped), derived from the rule shapes so a new
// one cannot be missed; manage-members is tracked by its selector reach as
// Edit, as in the move census.
var envWideningCaps = func() []domain.Capability {
	var out []domain.Capability
	for _, c := range domain.RuleCapabilities() {
		if shape, _ := domain.RuleShapeOf(c); shape != domain.ShapeProject {
			out = append(out, c)
		}
	}
	return out
}()

// envChange is one environment rename, create or delete inside its
// transaction: the project's environments before it (refs) and the
// environment's id, old name ("" for a create) and new name ("" for a delete).
type envChange struct {
	read     bool
	scope    domain.Scope // the project
	refs     []domain.EnvRef
	census   []authz.CensusRule
	env      domain.EnvID
	from, to string
	// afterRefs, when set, is the project's environments read after a plan
	// wrote several changes; gained, when set, is gains() computed already.
	afterRefs []domain.EnvRef
	gained    []WideningGain
}

// with is the census for one change of a plan whose environments after it
// are after.
func (c envChange) with(after []domain.EnvRef, env domain.EnvID, from, to string) envChange {
	c.afterRefs, c.env, c.from, c.to = after, env, from, to
	return c
}

// readEnvChange reads the census and the project's environments BEFORE the
// rename, create or delete writes, in its transaction.
func readEnvChange(ctx context.Context, az *authz.TxAuthorizer, project domain.Scope, env domain.EnvID, from, to string) (envChange, error) {
	refs, err := az.RuleEnvRefs(ctx, project.Org, project.Project)
	if err != nil {
		return envChange{}, err
	}
	census, err := az.RuleCensusForProject(ctx, project.Org, project.Project)
	if err != nil {
		return envChange{}, err
	}
	return envChange{read: true, scope: project, refs: refs, census: census, env: env, from: from, to: to}, nil
}

// after is the project's environments once the change is written.
func (c envChange) after() []domain.EnvRef {
	if c.afterRefs != nil {
		return c.afterRefs
	}
	out := slices.Clone(c.refs)
	if c.to == "" {
		return slices.DeleteFunc(out, func(e domain.EnvRef) bool { return e.ID == c.env })
	}
	if c.from == "" {
		return append(out, domain.EnvRef{Project: c.scope.Project, ID: c.env, Name: c.to})
	}
	for i := range out {
		if out[i].ID == c.env {
			out[i].Name = c.to
		}
	}
	return out
}

// gains computes what principals gain through their rules: each capability at
// the project, and at the changed environment, reached after but not before.
// Before is the census as bound before the write; after re-binds it to the
// environments after the change. An `all` rule reaches an environment id it
// does not except on both sides, so a created environment it does not name
// needs no confirmation, while an `only` list naming it gains it. A deleted
// environment gains nothing itself, but an `all` rule excepting only it gains
// the project. Other environments keep their names, so their reach cannot
// change.
// A key-narrowed rule reaches the environment over its key selector: the atom
// keeps the selector, so a rule reaching other keys of an environment the
// principal reaches already still gains. It is held already only by the same
// selector or by reach over every key; overlapping selectors are not merged,
// which may ask for a confirmation that is not strictly needed, never miss one.
func (c envChange) gains() []WideningGain {
	type atom struct {
		principal  domain.PrincipalID
		capability domain.Capability
		env        domain.EnvID
		keys       string // the key selector; "" is every key
	}
	after := c.after()
	// A deleted environment has no reach left to gain.
	gone := !slices.ContainsFunc(after, func(e domain.EnvRef) bool { return e.ID == c.env })
	reach := func(bound func(domain.Where) domain.Where) map[atom]bool {
		out := map[atom]bool{}
		for _, cr := range c.census {
			rule := cr.Rule
			rule.Where = bound(rule.Where)
			for _, cap := range envWideningCaps {
				evaluated, capability := rule, cap
				if cap == domain.CapManageMembers && rule.Capability == cap {
					evaluated.Capability, capability = domain.CapEdit, domain.CapEdit
				}
				keys := ""
				if capability != domain.CapRead && evaluated.Where.KeyNarrowedIn(c.scope.Project) {
					items := evaluated.Where.KeysIn(c.scope.Project)
					if evaluated.Where.KeyMode == domain.AxisOnly && len(items) == 0 {
						continue
					}
					keys = keySelector(evaluated.Where.KeyMode, items)
					evaluated.Where.KeyMode, evaluated.Where.Keys = domain.AxisAll, nil
				}
				if evaluated.Reaches(capability, domain.LevelProject, c.scope, nil) {
					out[atom{rule.Principal, cap, "", keys}] = true
				}
				s := domain.Scope{Org: c.scope.Org, Project: c.scope.Project, Env: c.env}
				if !gone && evaluated.Reaches(capability, domain.LevelEnv, s, nil) {
					out[atom{rule.Principal, cap, c.env, keys}] = true
				}
			}
		}
		return out
	}
	before := reach(func(w domain.Where) domain.Where { return w })
	now := reach(func(w domain.Where) domain.Where { return w.BindEnvs(after) })
	var gains []WideningGain
	for a := range now {
		every := a
		every.keys = ""
		if before[a] || before[every] {
			continue
		}
		g := WideningGain{Principal: a.principal, Capability: a.capability}
		if a.env != "" {
			g.Envs = []domain.EnvID{a.env}
		}
		gains = mergeGains(gains, []WideningGain{g})
	}
	return gains
}

// confirm refuses a widening change unless confirmed names exactly the
// gaining set and the caller may confirm, then records the confirmed set and
// sends every gaining machine through the machine widening gate.
func (c envChange) confirm(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, auth *Auth,
	caller authz.Identity, confirmed []domain.PrincipalID, now time.Time) error {
	gains := c.gained
	if gains == nil {
		gains = c.gains()
	}
	gained := gainedPrincipals(gains)
	if len(gained) == 0 {
		return nil
	}
	want := slices.Clone(confirmed)
	slices.Sort(want)
	want = slices.Compact(want)
	refuse := func() error { return c.refusal(ctx, az, caller.Principal, gains) }
	if !slices.Equal(want, gained) {
		return refuse()
	}
	if ok, err := mayConfirmWidening(ctx, az, caller.Principal, c.scope); err != nil {
		return err
	} else if !ok {
		return refuse()
	}
	names := make([]string, 0, len(gained))
	for _, g := range gained {
		names = append(names, string(g))
	}
	ev, err := domainEvent(ctx, audit.EventRuleEnvWideningConfirmed, caller.Principal, audit.Object{Type: "environment", ID: string(c.env)}, audit.Payload{
		"env_id":     string(c.env),
		"from_name":  audit.SanitizeFreeText(c.from),
		"to_name":    audit.SanitizeFreeText(c.to),
		"principals": names,
	})
	if err != nil {
		return err
	}
	if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
		return err
	}
	return c.gateMachines(ctx, r, az, p, auth, caller, gained, now)
}

// gateMachines sends each gaining machine principal through the machine
// widening gate (#1473, #1480): its reach over the project from its grants
// and rules before the change, against the same with its rules re-bound to
// the environments after it. The change is written already, so the
// environment list is the post-change one.
func (c envChange) gateMachines(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, auth *Auth,
	caller authz.Identity, gained []domain.PrincipalID, now time.Time) error {
	var actorGrants []authz.GrantRow
	after := c.after()
	for _, principal := range gained {
		if !slices.ContainsFunc(c.census, func(cr authz.CensusRule) bool { return cr.Machine && cr.Rule.Principal == principal }) {
			continue
		}
		class, err := az.PrincipalClass(ctx, principal)
		if err != nil {
			return err
		}
		if !domain.IsMachineClass(class) {
			continue
		}
		if actorGrants == nil {
			if actorGrants, err = az.GrantRowsForPrincipal(ctx, caller.Principal); err != nil {
				return err
			}
		}
		envs, err := az.EnvironmentsInProject(ctx, c.scope)
		if err != nil {
			return err
		}
		grants, err := az.GrantsOf(ctx, principal)
		if err != nil {
			return err
		}
		held, err := az.MachineRulesOf(ctx, principal, class)
		if err != nil {
			return err
		}
		// The machine's rules as the chokepoint reads them are bound to the
		// environments the transaction sees, after the write: re-bind them to
		// both sides explicitly.
		before, rebound := make([]domain.Rule, len(held)), make([]domain.Rule, len(held))
		for i, rule := range held {
			before[i], rebound[i] = rule, rule
			before[i].Where = rule.Where.BindEnvs(c.refs)
			rebound[i].Where = rule.Where.BindEnvs(after)
		}
		pre := append(slices.Clone(grants), authz.RuleGrants(before, c.scope, envs)...)
		post := append(slices.Clone(grants), authz.RuleGrants(rebound, c.scope, envs)...)
		current, historical, err := gateMachineWidening(ctx, az, auth, caller, actorGrants, c.scope, envs, pre, post, now)
		if err != nil {
			return err
		}
		if len(current) == 0 && len(historical) == 0 {
			continue
		}
		ev, err := domainEvent(ctx, audit.EventMachineGrantWidened, caller.Principal, audit.Object{Type: "principal", ID: string(principal)}, audit.Payload{
			"target_principal":           string(principal),
			"principal_class":            string(class),
			"capability":                 string(domain.CapRead),
			"scope":                      renderScope(c.scope),
			"newly_reachable_current":    envStrings(current),
			"newly_reachable_historical": envStrings(historical),
		})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return err
		}
	}
	return nil
}

// refusal is the change's widening refusal: a delete names the environment
// it deletes, a rename or create the name it gives.
func (c envChange) refusal(ctx context.Context, az *authz.TxAuthorizer, caller domain.PrincipalID, gains []WideningGain) error {
	if c.to == "" {
		return envWideningError(ctx, az, caller, c.scope, &EnvWideningError{EnvName: c.from, Deleting: true}, gains)
	}
	return envWideningError(ctx, az, caller, c.scope, &EnvWideningError{EnvName: c.to}, gains)
}

// planWideningError refuses a definitions plan whose environment changes
// widen access only together, naming no single change.
func planWideningError(ctx context.Context, az *authz.TxAuthorizer, caller domain.PrincipalID, scope domain.Scope, gains []WideningGain) error {
	return envWideningError(ctx, az, caller, scope, &EnvWideningError{Plan: true}, gains)
}

// envWideningError builds the refusal, naming the gainers only to a caller
// holding legacy manage-members on the project.
func envWideningError(ctx context.Context, az *authz.TxAuthorizer, caller domain.PrincipalID, scope domain.Scope,
	e *EnvWideningError, gains []WideningGain) error {
	gained := gainedPrincipals(gains)
	e.Count = len(gained)
	ok, err := mayConfirmWidening(ctx, az, caller, scope)
	if err != nil || !ok {
		return firstErr(err, e)
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

func firstErr(err error, otherwise error) error {
	if err != nil {
		return err
	}
	return otherwise
}

// keySelector is a canonical form of a rule's key selector in one project, so
// two rules selecting the same keys compare equal.
func keySelector(mode domain.AxisMode, items []domain.RuleKeyItem) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		if it.IsFolder {
			parts = append(parts, "folder:"+it.Folder)
		} else {
			parts = append(parts, "key:"+it.KeyID)
		}
	}
	slices.Sort(parts)
	return fmt.Sprintf("%s|%s", mode, strings.Join(slices.Compact(parts), "\x00"))
}
