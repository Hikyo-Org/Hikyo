package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// CallerCanDefineKeys reports the caller's own existential catalogue
// affordances. It never mints authority: each write still checks its actual
// key, folder and complete publication footprint. A single-key selector can
// offer editing, but cannot offer creation of a different, future key.
func (a *TxAuthorizer) CallerCanDefineKeys(ctx context.Context, caller Identity, scope domain.Scope) (creatable, editable bool, err error) {
	if caller.Principal == "" {
		return false, false, errors.New("authz: empty principal")
	}
	level, err := scope.Level()
	if err != nil || level != domain.LevelProject {
		return false, false, errors.New("authz: key definition affordance requires a project")
	}
	chain, err := a.r.ResolveChain(ctx, scope)
	if err != nil {
		return false, false, err
	}
	grants, err := a.r.Grants(ctx, caller.Principal)
	if err != nil {
		return false, false, err
	}
	protected, permitted, err := a.selfConfigProfile(ctx, caller, OpKeyCreate, chain, grants)
	if err != nil || !permitted || protected && !selfConfigSessionEligible(caller) || a.assuranceInadequate(caller, OpKeyCreate) {
		return false, false, err
	}
	spec, ok := registry.authorizationSpec(OpKeyCreate)
	if !ok {
		return false, false, errors.New("authz: key create is not registered")
	}
	if evaluate(spec.formula, chain, grants) {
		return true, true, nil
	}
	if !rulesApply(caller) {
		return false, false, nil
	}
	rules, err := a.r.Rules(ctx, caller.Principal)
	if err != nil {
		return false, false, err
	}
	for _, rule := range rules {
		if rule.Org != chain.Org || rule.Capability != domain.CapDefinitionsEdit || rule.Validate() != nil || !slices.Contains(rule.Where.Projects, chain.Project) {
			continue
		}
		items := rule.Where.Keys[chain.Project]
		if rule.Where.KeyMode == domain.AxisAll {
			// Finite exclusions cannot exclude every possible non-root folder.
			// These are virtual creation targets, never catalogue reads.
			for i := 0; i <= len(items)+1; i++ {
				folder := ""
				if i > 0 {
					folder = fmt.Sprintf("definition-affordance-%d", i)
				}
				if rule.Reaches(domain.CapDefinitionsEdit, domain.LevelProject, chain, &domain.RuleKey{Folder: folder}) {
					creatable, editable = true, true
					break
				}
			}
			continue
		}
		for _, item := range items {
			key := domain.RuleKey{ID: item.KeyID, Folder: item.Folder}
			if !rule.Reaches(domain.CapDefinitionsEdit, domain.LevelProject, chain, &key) {
				continue
			}
			editable = true
			if item.IsFolder {
				creatable = true
			}
		}
	}
	return creatable, editable, nil
}
