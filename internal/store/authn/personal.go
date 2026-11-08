package authn

import (
	"context"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// PurgePersonalValuesForOwner deletes every personal value an owner holds,
// across every project, and leaves a notice naming the cause for each
// (#1235, personal-values ADR, Which accounts qualify). It runs inside the
// credential change that cost the owner qualification, keyed on that
// account's own principal, so a planted recovery code can destroy values
// but never leave them readable by a newly enrolled factor.
func (r *Resolver) PurgePersonalValuesForOwner(ctx context.Context, owner domain.PrincipalID, cause string, at time.Time) (int64, error) {
	var (
		n   int64
		err error
	)
	if r.sq != nil {
		if err = r.sq.RecordPersonalValueNoticesForOwner(ctx, sqlitegen.RecordPersonalValueNoticesForOwnerParams{
			Cause: cause, CreatedAt: encodeTime(at), OwnerPrincipalID: string(owner),
		}); err != nil {
			return 0, fmt.Errorf("authn: record personal value notices: %w", err)
		}
		n, err = r.sq.DeletePersonalValuesForOwner(ctx, string(owner))
	} else {
		if err = r.pg.RecordPersonalValueNoticesForOwner(ctx, pggen.RecordPersonalValueNoticesForOwnerParams{
			Cause: cause, CreatedAt: pgTimestamp(at), OwnerPrincipalID: string(owner),
		}); err != nil {
			return 0, fmt.Errorf("authn: record personal value notices: %w", err)
		}
		n, err = r.pg.DeletePersonalValuesForOwner(ctx, string(owner))
	}
	if err != nil {
		return 0, fmt.Errorf("authn: delete personal values: %w", err)
	}
	return n, nil
}

// PersonalValueOwners lists every principal holding a personal value, so a
// restore purges only those owners rather than every principal.
func (r *Resolver) PersonalValueOwners(ctx context.Context) ([]domain.PrincipalID, error) {
	var (
		rows []string
		err  error
	)
	if r.sq != nil {
		rows, err = r.sq.ListPersonalValueOwners(ctx)
	} else {
		rows, err = r.pg.ListPersonalValueOwners(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("authn: list personal value owners: %w", err)
	}
	out := make([]domain.PrincipalID, len(rows))
	for i, row := range rows {
		out[i] = domain.PrincipalID(row)
	}
	return out, nil
}
