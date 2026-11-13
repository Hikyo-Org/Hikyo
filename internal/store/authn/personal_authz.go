package authn

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// PersonalOwnerHolds reports whether an active human principal holds a
// personal value or a notice in the environment (#1235, personal-values ADR,
// Who can do what). authorize() reads it for the owner's own list and delete,
// and reads it against an empty decoy scope on a missing chain so both
// refusals do the same work.
func (r *Resolver) PersonalOwnerHolds(ctx context.Context, scope domain.Scope, principal domain.PrincipalID) (bool, error) {
	if r.sq != nil {
		if r.historicalRecoveryBeforeContainment {
			// Source schemas 86 through 88 hold personal values but no containment column.
			n, err := r.sq.RecoveryPersonalOwnerHoldsBeforeContainment(ctx, sqlitegen.RecoveryPersonalOwnerHoldsBeforeContainmentParams{
				PrincipalID: string(principal), OrgID: string(scope.Org), ProjectID: string(scope.Project), EnvID: string(scope.Env),
			})
			return n > 0, err
		}
		n, err := r.sq.PersonalOwnerHoldsInEnvironment(ctx, sqlitegen.PersonalOwnerHoldsInEnvironmentParams{
			PrincipalID: string(principal), OrgID: string(scope.Org), ProjectID: string(scope.Project), EnvID: string(scope.Env),
		})
		return n > 0, err
	}
	if r.historicalRecoveryBeforeContainment {
		n, err := r.pg.RecoveryPersonalOwnerHoldsBeforeContainment(ctx, pggen.RecoveryPersonalOwnerHoldsBeforeContainmentParams{
			PrincipalID: string(principal), OrgID: string(scope.Org), ProjectID: string(scope.Project), EnvID: string(scope.Env),
		})
		return n > 0, err
	}
	n, err := r.pg.PersonalOwnerHoldsInEnvironment(ctx, pggen.PersonalOwnerHoldsInEnvironmentParams{
		PrincipalID: string(principal), OrgID: string(scope.Org), ProjectID: string(scope.Project), EnvID: string(scope.Env),
	})
	return n > 0, err
}

// PersonalEligibleCandidates lists humans who might hold reveal in the org:
// a superset that authz narrows with its own grant and rule evaluation.
func (r *Resolver) PersonalEligibleCandidates(ctx context.Context, org domain.OrgID) ([]domain.PrincipalID, error) {
	var ids []string
	var err error
	if r.sq != nil {
		ids, err = r.sq.PersonalEligibleCandidates(ctx, nullString(string(org)))
	} else {
		ids, err = r.pg.PersonalEligibleCandidates(ctx, pgText(string(org)))
	}
	if err != nil {
		return nil, err
	}
	out := make([]domain.PrincipalID, len(ids))
	for i, id := range ids {
		out[i] = domain.PrincipalID(id)
	}
	return out, nil
}
