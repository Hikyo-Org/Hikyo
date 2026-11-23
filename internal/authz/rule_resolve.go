package authz

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// "Who can...?" (member-access-rules ADR D10) at the chokepoint. Every
// principal in the population (AccessPopulation) is evaluated with the
// predicates authorizeTenant uses:
// evaluate and evaluateWithRules for an ordinary atom, the delegation
// selector check for Manage access, rulesApply for whose rules count and
// machineRevealWithdrawn for the machine-reveal opt-in. Nothing is cached.
//
// "Left out by an except" is counterfactual, so it needs no second reading of
// a Where: a rule is excepted when the chokepoint does not admit it, but does
// admit the same rule with that project's excepts removed. Each named except
// is one the rule does not pass with that except alone.

// AccessState is one principal's place in the answer.
type AccessState string

const (
	// AccessCan holds the permission here (Reveal and Reveal history also
	// hold See, D6).
	AccessCan AccessState = "can"
	// AccessExcepted would hold it through a rule, but an except of that rule
	// leaves this place out, or no key was named and the rule narrows keys.
	AccessExcepted AccessState = "excepted"
	// AccessNeedsSee holds Reveal (or Reveal history) but not See.
	AccessNeedsSee AccessState = "needs-see"
	// AccessNone is reached by no grant or rule of theirs.
	AccessNone AccessState = "none"
)

// AccessExcept names one rule that would reach but for its excepts.
type AccessExcept struct {
	RuleID string
	Envs   []domain.EnvID
	Keys   []domain.RuleKeyItem
	// KeyLimited is set when no key was named and the rule narrows keys:
	// it reaches only some keys of the environment.
	KeyLimited bool
}

// AccessAnswer is one principal's answer.
type AccessAnswer struct {
	Principal domain.PrincipalID
	Class     domain.PrincipalClass
	State     AccessState
	// Grants and Rules give the permission (can and needs-see). A grant is
	// named by its scope.
	Grants  []domain.Scope
	Rules   []string
	Excepts []AccessExcept
}

// AccessPopulation is what the caller (a project member manager) may already
// see of the project's membership: the principals of its project grant lines,
// of the org-level grant lines that can decide the question, and of the project
// rules the caller may manage, and those rule ids. The service builds it from
// those reads alone, so the answer never reaches past them: not sibling
// projects, not instance holders, not rules the caller cannot manage.
type AccessPopulation struct {
	Principals []domain.PrincipalID
	// Rules is the set of rule ids the answer may name.
	Rules map[string]bool
}

// ResolveAccess answers "Who can...?" for one permission in the environment
// a live rule.resolve proof was minted for, optionally for one key of its
// project. The population is the caller's own project view (AccessPopulation);
// each principal's state is evaluated over everything it holds, but only the
// rules in the population are named. A key that does not exist is the
// uniform nonexistent outcome; its id is opaque, so a member manager without
// See can confirm one exists (accepted, member-access-rules ADR).
func (a *TxAuthorizer) ResolveAccess(ctx context.Context, p Proof, pop AccessPopulation, capability domain.Capability, key *KeyTarget) ([]AccessAnswer, error) {
	c, ok := p.(*proof)
	if !ok || c == nil || c.kind != kindTenant || c.op != OpRuleResolve || c.tok == nil || c.tok != a.tok || !c.tok.alive() || c.chain.Env == "" {
		return nil, errors.New("authz: access resolution needs a live rule.resolve proof")
	}
	shape, ok := domain.RuleShapeOf(capability)
	if !ok {
		return nil, fmt.Errorf("%w: %q is not a permission a rule can carry", domain.ErrInvalid, capability)
	}
	chain := c.chain
	var bound *domain.RuleKey
	if key != nil {
		if key.kind != keyByID {
			return nil, errors.New("authz: access resolution addresses a key by id")
		}
		resolved, err := a.resolveKeyTarget(ctx, chain, *key, "")
		if err != nil {
			return nil, err
		}
		bound = &resolved
	}
	at := domain.LevelEnv
	if shape == domain.ShapeProject {
		at = domain.LevelProject
	}
	q := accessQuestion{a: a, chain: chain, capability: capability, at: at, key: bound, visible: pop.Rules}
	principals := slices.Clone(pop.Principals)
	slices.Sort(principals)
	principals = slices.Compact(principals)
	out := make([]AccessAnswer, 0, len(principals))
	for _, principal := range principals {
		answer, ok, err := q.answer(ctx, principal)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, answer)
		}
	}
	return out, nil
}

type accessQuestion struct {
	a          *TxAuthorizer
	chain      domain.Scope
	capability domain.Capability
	at         domain.Level
	key        *domain.RuleKey
	// visible is the rule ids the answer may name.
	visible map[string]bool
}

// holds is the chokepoint's evaluation of the question's permission over
// these grants and rules. Manage access is the delegation selector check for
// this one environment (and key), as rule creation would ask it; every other
// permission is the ordinary atom.
func (q accessQuestion) holds(ctx context.Context, capability domain.Capability, grants []domain.Grant, rules []domain.Rule) (bool, error) {
	if capability != domain.CapManageMembers {
		at := q.at
		if capability != q.capability {
			at = domain.LevelEnv
		}
		return evaluateWithRules(Formula{{Cap: capability, At: at}}, q.chain, grants, rules, q.key), nil
	}
	target := domain.Where{
		Projects: []domain.ProjectID{q.chain.Project},
		EnvMode:  domain.AxisOnly, Envs: map[domain.ProjectID][]domain.EnvID{q.chain.Project: {q.chain.Env}},
		KeyMode: domain.AxisAll,
	}
	if q.key != nil {
		target.KeyMode = domain.AxisOnly
		target.Keys = map[domain.ProjectID][]domain.RuleKeyItem{q.chain.Project: {{KeyID: q.key.ID}}}
	}
	project := domain.Scope{Org: q.chain.Org, Project: q.chain.Project}
	return q.a.ruleSelectorHeld(ctx, project, grants, rules, domain.CapManageMembers, target)
}

// answer evaluates one principal. A principal that no longer exists, or has
// no class, holds nothing the chokepoint would admit and is left out (false)
// rather than labelled.
func (q accessQuestion) answer(ctx context.Context, principal domain.PrincipalID) (AccessAnswer, bool, error) {
	out := AccessAnswer{Principal: principal, State: AccessNone}
	class, err := q.a.r.PrincipalClass(ctx, principal)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && class == "") {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	out.Class = class
	caller := Identity{Principal: principal, Class: class}
	grants, err := q.a.r.Grants(ctx, principal)
	if err != nil {
		return out, false, err
	}
	var rules []domain.Rule
	if rulesApply(caller) {
		if rules, err = q.a.r.Rules(ctx, principal); err != nil {
			return out, false, err
		}
	}
	held, err := q.holds(ctx, q.capability, grants, rules)
	if err != nil {
		return out, false, err
	}
	if held {
		withdrawn, err := q.a.machineRevealWithdrawn(ctx, caller, Formula{{Cap: q.capability, At: q.at}}, q.chain)
		if err != nil {
			return out, false, err
		}
		if withdrawn {
			return out, true, nil
		}
		if err := q.sources(ctx, &out, grants, rules); err != nil {
			return out, false, err
		}
		out.State = AccessCan
		if q.capability == domain.CapReveal || q.capability == domain.CapRevealHistory {
			see, err := q.holds(ctx, domain.CapRead, grants, rules)
			if err != nil {
				return out, false, err
			}
			if !see {
				out.State = AccessNeedsSee
			}
		}
		return out, true, nil
	}
	for _, rule := range rules {
		except, ok, err := q.excepted(ctx, rule)
		if err != nil {
			return out, false, err
		}
		if ok {
			out.Excepts = append(out.Excepts, except)
		}
	}
	if len(out.Excepts) > 0 {
		out.State = AccessExcepted
	}
	return out, true, nil
}

// sources names each grant and rule that gives the permission on its own.
// Manage access can be held only by a union of selectors; then every rule
// that takes part is named.
func (q accessQuestion) sources(ctx context.Context, out *AccessAnswer, grants []domain.Grant, rules []domain.Rule) error {
	for _, g := range grants {
		held, err := q.holds(ctx, q.capability, []domain.Grant{g}, nil)
		if err != nil {
			return err
		}
		if held && !slices.Contains(out.Grants, g.Scope) {
			out.Grants = append(out.Grants, g.Scope)
		}
	}
	var partial []string
	for _, r := range rules {
		if !q.candidate(r) {
			continue
		}
		held, err := q.holds(ctx, q.capability, nil, []domain.Rule{r})
		if err != nil {
			return err
		}
		if held {
			out.Rules = append(out.Rules, r.ID)
		} else {
			partial = append(partial, r.ID)
		}
	}
	if len(out.Grants) == 0 && len(out.Rules) == 0 {
		out.Rules = partial
	}
	return nil
}

// candidate reports whether a rule could take part in this question at all
// and may be named: a rule outside the caller's view never is.
func (q accessQuestion) candidate(r domain.Rule) bool {
	return q.visible[r.ID] && r.Org == q.chain.Org && r.Capability == q.capability && r.Validate() == nil && slices.Contains(r.Where.Projects, q.chain.Project)
}

// excepted reports whether the rule is held back only by its own excepts in
// this project (or by its key limit when no key was named), naming them.
func (q accessQuestion) excepted(ctx context.Context, r domain.Rule) (AccessExcept, bool, error) {
	out := AccessExcept{RuleID: r.ID}
	if !q.candidate(r) {
		return out, false, nil
	}
	project := q.chain.Project
	relaxed := r
	relaxed.Where.Envs = maps.Clone(r.Where.Envs)
	relaxed.Where.Keys = maps.Clone(r.Where.Keys)
	var envs []domain.EnvID
	var keys []domain.RuleKeyItem
	if r.Where.EnvMode == domain.AxisAll {
		envs = r.Where.Envs[project]
		delete(relaxed.Where.Envs, project)
	}
	// See ignores the key axis (D5), so a key item never holds it back.
	if q.capability != domain.CapRead && r.Where.KeyNarrowedIn(project) {
		switch {
		case q.key == nil:
			out.KeyLimited = true
			relaxed.Where.KeyMode, relaxed.Where.Keys = domain.AxisAll, nil
		case r.Where.KeyMode == domain.AxisAll:
			keys = r.Where.Keys[project]
			delete(relaxed.Where.Keys, project)
		}
	}
	if len(envs) == 0 && len(keys) == 0 && !out.KeyLimited {
		return out, false, nil
	}
	held, err := q.holds(ctx, q.capability, nil, []domain.Rule{relaxed})
	if err != nil || !held {
		return out, false, err
	}
	for _, env := range envs {
		probe := relaxed
		probe.Where.Envs = maps.Clone(relaxed.Where.Envs)
		probe.Where.Envs[project] = []domain.EnvID{env}
		if held, err := q.holds(ctx, q.capability, nil, []domain.Rule{probe}); err != nil {
			return out, false, err
		} else if !held {
			out.Envs = append(out.Envs, env)
		}
	}
	for _, item := range keys {
		probe := relaxed
		probe.Where.Keys = maps.Clone(relaxed.Where.Keys)
		probe.Where.Keys[project] = []domain.RuleKeyItem{item}
		if held, err := q.holds(ctx, q.capability, nil, []domain.Rule{probe}); err != nil {
			return out, false, err
		} else if !held {
			out.Keys = append(out.Keys, item)
		}
	}
	if len(out.Envs) == 0 && len(out.Keys) == 0 && !out.KeyLimited {
		// Excepts are disjunctive per axis, so one of them always holds the rule back alone.
		return out, false, fmt.Errorf("authz: rule %s is held back by excepts, yet none of them alone", r.ID)
	}
	return out, true, nil
}
