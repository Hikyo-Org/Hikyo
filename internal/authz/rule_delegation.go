package authz

import (
	"context"
	"fmt"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// AuthorizeRule proves a member-management operation against the complete
// selector being changed, rather than a single representative key. It shares
// all caller, assurance, denial and proof checks with Authorize.
func (a *TxAuthorizer) AuthorizeRule(ctx context.Context, caller Identity, op Operation, scope domain.Scope, target domain.Rule) (Proof, error) {
	if op != OpRuleCreate && op != OpRuleRevoke && op != OpRuleListProject {
		return nil, fmt.Errorf("authz: operation %q does not address a rule selector", op)
	}
	if caller.Principal == "" || target.Org != scope.Org || !slices.Contains(target.Where.Projects, scope.Project) {
		return nil, fmt.Errorf("authz: invalid rule authorization target")
	}
	if caller.Class == domain.ClassInstanceConn {
		return a.Authorize(ctx, caller, op, scope)
	}
	spec, ok := registry.authorizationSpec(op)
	if !ok {
		return nil, fmt.Errorf("authz: operation %q is not in the operation registry", op)
	}
	return a.authorizeTenant(ctx, caller, op, spec, scope, nil, &target)
}

// AuthorizeRuleList admits a project membership read through any live Manage
// access selector. The service must filter every row with RuleManageable
// before resolving principal names or rendering selector metadata.
func (a *TxAuthorizer) AuthorizeRuleList(ctx context.Context, caller Identity, scope domain.Scope) (Proof, error) {
	grants, err := a.r.Grants(ctx, caller.Principal)
	if err != nil {
		return nil, err
	}
	if evaluate(Formula{{Cap: domain.CapManageMembers, At: domain.LevelProject}}, scope, grants) {
		return a.Authorize(ctx, caller, OpRuleListProject, scope)
	}
	if rulesApply(caller) {
		rules, err := a.r.Rules(ctx, caller.Principal)
		if err != nil {
			return nil, err
		}
		for _, rule := range rules {
			if rule.Org == scope.Org && rule.Capability == domain.CapManageMembers && rule.Validate() == nil && slices.Contains(rule.Where.Projects, scope.Project) {
				return a.AuthorizeRule(ctx, caller, OpRuleListProject, scope, rule)
			}
		}
	}
	return a.Authorize(ctx, caller, OpRuleListProject, scope)
}

func (a *TxAuthorizer) RuleManageable(ctx context.Context, caller Identity, target domain.Rule, project domain.ProjectID) (bool, error) {
	if target.Capability == domain.CapRead {
		target.Where.KeyMode, target.Where.Keys = domain.AxisAll, nil
	}
	target.Capability = domain.CapManageMembers
	return a.RuleCapabilityHeld(ctx, caller, target, project)
}

func (a *TxAuthorizer) ruleDelegationHeld(ctx context.Context, f Formula, chain domain.Scope, grants []domain.Grant, rules []domain.Rule, target domain.Rule) (bool, error) {
	if target.Capability == domain.CapRead {
		target.Where.KeyMode, target.Where.Keys = domain.AxisAll, nil
	}
	for _, atom := range f {
		if evaluate(Formula{atom}, chain, grants) {
			continue
		}
		if atom.Cap != domain.CapManageMembers {
			return false, nil
		}
		held, err := a.ruleSelectorHeld(ctx, chain, grants, rules, domain.CapManageMembers, target.Where)
		if err != nil || !held {
			return false, err
		}
	}
	return true, nil
}

// RuleCapabilityHeld is the grantor bound. A selector may not confer a
// capability beyond the grantor's own reach, including future objects on an
// all-except axis. This is separate from the management proof: possessing
// Manage access never implies Reveal or Publish.
func (a *TxAuthorizer) RuleCapabilityHeld(ctx context.Context, caller Identity, target domain.Rule, project domain.ProjectID) (bool, error) {
	grants, err := a.r.Grants(ctx, caller.Principal)
	if err != nil {
		return false, err
	}
	var rules []domain.Rule
	if rulesApply(caller) {
		rules, err = a.r.Rules(ctx, caller.Principal)
		if err != nil {
			return false, err
		}
	}
	w := target.Where
	// See always confers the whole environment's catalogue (ADR D5).
	if target.Capability == domain.CapRead {
		w.KeyMode, w.Keys = domain.AxisAll, nil
	}
	return a.ruleSelectorHeld(ctx, domain.Scope{Org: target.Org, Project: project}, grants, rules, target.Capability, w)
}

func (a *TxAuthorizer) ruleSelectorHeld(ctx context.Context, chain domain.Scope, grants []domain.Grant, rules []domain.Rule, capability domain.Capability, target domain.Where) (bool, error) {
	// Legacy grants still satisfy the same atom. Environment grants can cover
	// an only-environments selector, but never an all/future selector.
	if evaluate(Formula{{Cap: capability, At: domain.LevelProject}}, chain, grants) {
		return true, nil
	}
	if target.EnvMode == domain.AxisOnly && len(target.Envs[chain.Project]) > 0 {
		all := true
		for _, env := range target.Envs[chain.Project] {
			s := chain
			s.Env = env
			if !evaluate(Formula{{Cap: capability, At: domain.LevelEnv}}, s, grants) {
				all = false
				break
			}
		}
		if all {
			return true, nil
		}
	}
	var selectors []domain.Where
	allFolders := map[string]string{}
	for _, grant := range grants {
		if grant.Scope.Env == "" {
			continue
		}
		s := chain
		s.Env = grant.Scope.Env
		if evaluate(Formula{{Cap: capability, At: domain.LevelEnv}}, s, []domain.Grant{grant}) {
			selectors = append(selectors, domain.Where{Projects: []domain.ProjectID{chain.Project}, EnvMode: domain.AxisOnly, Envs: map[domain.ProjectID][]domain.EnvID{chain.Project: {grant.Scope.Env}}, KeyMode: domain.AxisAll})
		}
	}
	for _, rule := range rules {
		if rule.Org != chain.Org || rule.Capability != capability || rule.Validate() != nil || !slices.Contains(rule.Where.Projects, chain.Project) {
			continue
		}
		outer := rule.Where
		if capability == domain.CapRead {
			outer.KeyMode, outer.Keys = domain.AxisAll, nil
		}
		folders := map[string]string{}
		for _, items := range [][]domain.RuleKeyItem{outer.Keys[chain.Project], target.Keys[chain.Project]} {
			for _, item := range items {
				if item.IsFolder {
					continue
				}
				key, err := a.r.ResolveRuleKey(ctx, string(chain.Org), string(chain.Project), item.KeyID, "")
				if err != nil {
					return false, err
				}
				folders[item.KeyID] = key.Folder
				allFolders[item.KeyID] = key.Folder
			}
		}
		if outer.ContainsWhereInProject(target, chain.Project, folders) {
			return true, nil
		}
		selectors = append(selectors, outer)
	}
	if len(selectors) > 0 {
		return domain.WhereUnionContainsInProject(selectors, target, chain.Project, allFolders), nil
	}
	return false, nil
}
