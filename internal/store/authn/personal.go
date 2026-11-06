package authn

import (
	"context"
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
	if r.sq != nil {
		if err := r.sq.RecordPersonalValueNoticesForOwner(ctx, sqlitegen.RecordPersonalValueNoticesForOwnerParams{
			Cause: cause, CreatedAt: encodeTime(at), OwnerPrincipalID: string(owner),
		}); err != nil {
			return 0, err
		}
		return r.sq.DeletePersonalValuesForOwner(ctx, string(owner))
	}
	if err := r.pg.RecordPersonalValueNoticesForOwner(ctx, pggen.RecordPersonalValueNoticesForOwnerParams{
		Cause: cause, CreatedAt: pgTimestamp(at), OwnerPrincipalID: string(owner),
	}); err != nil {
		return 0, err
	}
	return r.pg.DeletePersonalValuesForOwner(ctx, string(owner))
}
