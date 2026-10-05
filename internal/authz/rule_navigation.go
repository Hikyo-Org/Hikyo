package authz

import (
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// Navigation is an explicit metadata projection, never a capability grant.
// These operations expose no values and permit no mutation. List services
// filter their results through the proof's selector before returning them.
func ruleNavigationOperation(op Operation) bool {
	switch op {
	case OpOrgGet, OpProjectList, OpProjectGet, OpEnvList, OpKeyList, OpFolderList:
		return true
	default:
		return false
	}
}

func ruleNavigationReach(op Operation, chain domain.Scope, rules []domain.Rule, grants []domain.Grant) []domain.Rule {
	if !ruleNavigationOperation(op) {
		return nil
	}
	// Legacy project/environment See participates in the same directory
	// union. These synthetic selectors exist only on a read-only proof.
	rules = slices.Clone(rules)
	for _, grant := range grants {
		if grant.Capability != domain.CapRead || grant.Scope.Org != chain.Org || grant.Scope.Project == "" {
			continue
		}
		where := domain.Where{Projects: []domain.ProjectID{grant.Scope.Project}, EnvMode: domain.AxisAll, KeyMode: domain.AxisAll}
		if grant.Scope.Env != "" {
			where.EnvMode = domain.AxisOnly
			where.Envs = map[domain.ProjectID][]domain.EnvID{grant.Scope.Project: {grant.Scope.Env}}
		}
		rules = append(rules, domain.Rule{Org: chain.Org, Capability: domain.CapRead, Where: where})
	}
	var reached []domain.Rule
	for _, rule := range rules {
		if rule.Org != chain.Org || rule.Capability != domain.CapRead || rule.Validate() != nil {
			continue
		}
		if chain.Project != "" && !slices.Contains(rule.Where.Projects, chain.Project) {
			continue
		}
		reached = append(reached, rule)
	}
	return reached
}
