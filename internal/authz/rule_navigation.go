package authz

import (
	"maps"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// Navigation is an explicit metadata projection, never a capability grant.
// These operations expose no values and permit no mutation. List services
// filter their results through the proof's selector before returning them.
func ruleNavigationOperation(op Operation) bool {
	switch op {
	case OpOrgGet, OpProjectList, OpProjectGet, OpEnvList, OpKeyList, OpKeyGet, OpFolderList, OpFolderGet, OpKeyGroupList, OpKeyGroupGet, OpDefinitionsSettingsGet:
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
		// See ignores a card's key axis, including items attached solely
		// to projects with no selected environment (ADR D5).
		rule.Where.KeyMode, rule.Where.Keys = domain.AxisAll, nil
		// An only-environments rule may name several projects but select
		// environments in only some of them. Project metadata follows actual
		// environment reach, rather than the unfiltered project axis.
		if rule.Where.EnvMode == domain.AxisOnly {
			rule.Where.Projects = slices.DeleteFunc(slices.Clone(rule.Where.Projects), func(project domain.ProjectID) bool {
				return len(rule.Where.Envs[project]) == 0
			})
			rule.Where.Envs = maps.Clone(rule.Where.Envs)
			rule.Where.Keys = maps.Clone(rule.Where.Keys)
			for project := range rule.Where.Envs {
				if !slices.Contains(rule.Where.Projects, project) {
					delete(rule.Where.Envs, project)
				}
			}
			for project := range rule.Where.Keys {
				if !slices.Contains(rule.Where.Projects, project) {
					delete(rule.Where.Keys, project)
				}
			}
		}
		if len(rule.Where.Projects) == 0 || chain.Project != "" && !slices.Contains(rule.Where.Projects, chain.Project) {
			continue
		}
		reached = append(reached, rule)
	}
	return reached
}
