package authz

import (
	"context"
	"errors"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// CallerHoldsApprovalKeys is the live quorum eligibility check, scoped to
// approval decisions only. It never mints a proof or captures denials while
// recomputing recorded votes. Each pinned key must remain within the voter's
// current Publish reach; losing one key invalidates that voter's contribution.
func (a *TxAuthorizer) CallerHoldsApprovalKeys(ctx context.Context, caller Identity, scope domain.Scope, ids []string) (bool, error) {
	if caller.Principal == "" {
		return false, errors.New("authz: empty principal")
	}
	if len(ids) == 0 {
		return false, nil
	}
	spec, ok := registry.authorizationSpec(OpApprovalVote)
	if !ok {
		return false, errors.New("authz: approval vote is not in the operation registry")
	}
	level, err := scope.Level()
	if err != nil || level != spec.level {
		return false, errors.New("authz: approval decision requires an environment scope")
	}
	chain, err := a.r.ResolveChain(ctx, scope)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	grants, err := a.r.Grants(ctx, caller.Principal)
	if err != nil {
		return false, err
	}
	protected, permitted, err := a.selfConfigProfile(ctx, caller, OpApprovalVote, chain, grants)
	if err != nil || !permitted {
		return false, err
	}
	if a.assuranceInadequate(caller, OpApprovalVote) || protected && !selfConfigSessionEligible(caller) {
		return false, nil
	}
	var rules []domain.Rule
	if rulesApply(caller) {
		rules, err = a.r.Rules(ctx, caller.Principal)
		if err != nil {
			return false, err
		}
	}
	for i, id := range ids {
		if id == "" || slices.Contains(ids[:i], id) {
			return false, nil
		}
		key, err := a.r.ResolveRuleKey(ctx, string(chain.Org), string(chain.Project), id, "")
		if errors.Is(err, domain.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !evaluateWithRules(spec.formula, chain, grants, rules, &key) {
			return false, nil
		}
	}
	return true, nil
}
