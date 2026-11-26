package authz

import (
	"context"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// AuthorizeRule proves a member-management operation against the complete
// selector being changed, rather than a single representative key. It shares
// all caller, assurance, denial and proof checks with Authorize.
func (a *TxAuthorizer) AuthorizeRule(ctx context.Context, caller Identity, op Operation, scope domain.Scope, target domain.Rule) (Proof, error) {
	if op != OpRuleCreate && op != OpRuleRevoke && op != OpRuleListProject {
		return nil, fmt.Errorf("authz: operation %q does not address a rule selector", op)
	}
	if caller.Principal == "" || target.Org != scope.Org || !target.Where.Names(scope.Project) {
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
// access selector. The service must filter the listing with RulesManageable
// once, before resolving principal names or rendering selector metadata.
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
			if rule.Org == scope.Org && rule.Capability == domain.CapManageMembers && rule.Validate() == nil && rule.Where.Names(scope.Project) {
				return a.AuthorizeRule(ctx, caller, OpRuleListProject, scope, rule)
			}
		}
	}
	return a.Authorize(ctx, caller, OpRuleListProject, scope)
}

// RulesManageable evaluates a listing against one live authorization snapshot.
// Key metadata is shared only inside this call, never across later mutations.
func (a *TxAuthorizer) RulesManageable(ctx context.Context, caller Identity, targets []domain.Rule, project domain.ProjectID) ([]bool, error) {
	grants, err := a.r.Grants(ctx, caller.Principal)
	if err != nil {
		return nil, err
	}
	var rules []domain.Rule
	if rulesApply(caller) {
		rules, err = a.r.Rules(ctx, caller.Principal)
		if err != nil {
			return nil, err
		}
	}
	type keyAddress struct {
		org domain.OrgID
		id  string
	}
	folders := map[keyAddress]domain.RuleKey{}
	// One environment read per org and project for every target, as for key metadata:
	// the same transaction, so no row can change between targets.
	envRefs := map[domain.Scope][]domain.EnvRef{}
	held := make([]bool, len(targets))
	for i, target := range targets {
		if target.Capability == domain.CapRead {
			target.Where.KeyMode, target.Where.Keys = domain.AxisAll, nil
		}
		resolve := func(id string) (domain.RuleKey, error) {
			address := keyAddress{org: target.Org, id: id}
			if key, ok := folders[address]; ok {
				return key, nil
			}
			key, err := a.r.ResolveRuleKey(ctx, string(target.Org), string(project), id, "")
			if err == nil {
				folders[address] = key
			}
			return key, err
		}
		refs := func() ([]domain.EnvRef, error) {
			at := domain.Scope{Org: target.Org, Project: project}
			if got, ok := envRefs[at]; ok {
				return got, nil
			}
			got, err := a.r.RuleEnvRefs(ctx, string(target.Org), string(project))
			if err == nil {
				envRefs[at] = got
			}
			return got, err
		}
		held[i], err = a.ruleSelectorHeldResolving(ctx, domain.Scope{Org: target.Org, Project: project}, grants, rules, domain.CapManageMembers, target.Where, resolve, refs)
		if err != nil {
			return nil, err
		}
	}
	return held, nil
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

// RuleFutureProjectsHeld is the future-project half of the authority over an
// all-projects rule (ADR D7, #1446). AuthorizeRule and RuleCapabilityHeld
// prove the caller in every current project; a rule over all projects also
// reaches every project created later, so the caller's management and the
// conferred capability must cover domain.FutureProjectWitness as well. Only an
// org- or instance-scope grant, or another all-projects rule, names it: a
// list of today's projects never covers tomorrow's.
func (a *TxAuthorizer) RuleFutureProjectsHeld(ctx context.Context, caller Identity, target domain.Rule) (manage, capability bool, err error) {
	if !target.Where.AllProjects {
		return false, false, fmt.Errorf("authz: the future-project check addresses an all-projects rule")
	}
	grants, err := a.r.Grants(ctx, caller.Principal)
	if err != nil {
		return false, false, err
	}
	var rules []domain.Rule
	if rulesApply(caller) {
		rules, err = a.r.Rules(ctx, caller.Principal)
		if err != nil {
			return false, false, err
		}
	}
	chain := domain.Scope{Org: target.Org, Project: domain.FutureProjectWitness}
	manage, err = a.ruleDelegationHeld(ctx, Formula{{Cap: domain.CapManageMembers, At: domain.LevelProject}}, chain, grants, rules, target)
	if err != nil || !manage {
		return false, false, err
	}
	w := target.Where
	if target.Capability == domain.CapRead {
		w.KeyMode, w.Keys = domain.AxisAll, nil
	}
	capability, err = a.ruleSelectorHeld(ctx, chain, grants, rules, target.Capability, w)
	return manage, capability, err
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
	resolve := func(id string) (domain.RuleKey, error) {
		return a.r.ResolveRuleKey(ctx, string(chain.Org), string(chain.Project), id, "")
	}
	refs := func() ([]domain.EnvRef, error) {
		return a.r.RuleEnvRefs(ctx, string(chain.Org), string(chain.Project))
	}
	return a.ruleSelectorHeldResolving(ctx, chain, grants, rules, capability, target, resolve, refs)
}

func (a *TxAuthorizer) ruleSelectorHeldResolving(ctx context.Context, chain domain.Scope, grants []domain.Grant, rules []domain.Rule, capability domain.Capability, target domain.Where, resolve func(string) (domain.RuleKey, error), envRefs func() ([]domain.EnvRef, error)) (bool, error) {
	// Legacy grants still satisfy the same atom. Environment grants can cover
	// an only-environments selector, but never an all/future selector.
	if evaluate(Formula{{Cap: capability, At: domain.LevelProject}}, chain, grants) {
		return true, nil
	}
	// Environments compare by name (#1447): bind the target and name the
	// environment grants against the project's environments now.
	refs, err := envRefs()
	if err != nil {
		return false, err
	}
	target = target.BindEnvs(refs)
	// An only list naming nothing is no rule (Validate refuses it). It is
	// left when sent ids match no environment, so refusing it here keeps an
	// unknown id answering as a known one outside the caller's authority.
	if target.EnvMode == domain.AxisOnly && len(target.EnvNames) == 0 {
		return false, nil
	}
	// Environment grants hold an only-list whose every name matches an
	// environment now; a name matching none stands for an environment that
	// does not exist yet, which no grant covers.
	if target.EnvMode == domain.AxisOnly && len(target.EnvNames) > 0 && len(target.Envs[chain.Project]) == len(target.EnvNames) {
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
		name, ok := domain.EnvRefName(refs, chain.Project, grant.Scope.Env)
		if ok && evaluate(Formula{{Cap: capability, At: domain.LevelEnv}}, s, []domain.Grant{grant}) {
			selectors = append(selectors, domain.Where{Projects: []domain.ProjectID{chain.Project}, EnvMode: domain.AxisOnly, EnvNames: []string{name}, KeyMode: domain.AxisAll}.BindEnvs(refs))
		}
	}
	for _, rule := range rules {
		if rule.Org != chain.Org || rule.Capability != capability || rule.Validate() != nil || !rule.Where.Names(chain.Project) {
			continue
		}
		outer := rule.Where
		if capability == domain.CapRead {
			outer.KeyMode, outer.Keys = domain.AxisAll, nil
		}
		folders := map[string]string{}
		for _, items := range [][]domain.RuleKeyItem{outer.KeysIn(chain.Project), target.KeysIn(chain.Project)} {
			for _, item := range items {
				if item.IsFolder {
					continue
				}
				key, err := resolve(item.KeyID)
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
