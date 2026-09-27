package authn

import (
	"context"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// Approval-mediated temporary access (#152). The time-bound grant rows sit on
// the resolution surface beside `grants` because authorize() reads them: the
// chokepoint's ListGrantsForPrincipal unions the rows whose absolute expiry is
// after the transaction clock. They are a separate table, not an origin on a
// `grants` row, so no origin movement can ever turn one into a permanent
// grant, and the grant writer's grantor bound (GrantRowsForPrincipal) never
// sees them: temporary authority can be used, never re-granted.

// AccessGrant is one live time-bound grant row.
type AccessGrant struct {
	ID        string
	Grant     domain.Grant
	RequestID string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// CreateAccessGrant writes one time-bound grant row for an approved or
// emergency request. The scope must be a concrete environment: temporary
// access never reaches above the environment it was requested for.
func (r *Resolver) CreateAccessGrant(ctx context.Context, id string, p domain.PrincipalID, g domain.Grant,
	requestID string, at, expiresAt time.Time) error {
	if g.Scope.Org == "" || g.Scope.Project == "" || g.Scope.Env == "" {
		return fmt.Errorf("authn: a temporary grant is environment-scoped, got %+v", g.Scope)
	}
	if !expiresAt.After(at) {
		return fmt.Errorf("authn: a temporary grant must expire after it is written")
	}
	if err := r.LockPrincipalRow(ctx, p); err != nil {
		return err
	}
	if r.sq != nil {
		return r.sq.InsertAccessGrant(ctx, sqlitegen.InsertAccessGrantParams{
			ID: id, PrincipalID: string(p), Capability: string(g.Capability),
			OrgID: string(g.Scope.Org), ProjectID: string(g.Scope.Project), EnvID: string(g.Scope.Env),
			RequestID: requestID, CreatedAt: encodeTime(at), ExpiresAt: encodeTime(expiresAt),
		})
	}
	return r.pg.InsertAccessGrant(ctx, pggen.InsertAccessGrantParams{
		ID: id, PrincipalID: string(p), Capability: string(g.Capability),
		OrgID: string(g.Scope.Org), ProjectID: string(g.Scope.Project), EnvID: string(g.Scope.Env),
		RequestID: requestID, CreatedAt: pgTimestamp(at), ExpiresAt: pgTimestamp(expiresAt),
	})
}

// DeleteAccessGrantsForRequest releases every row one request wrote, for
// revocation and for the expiry sweep's bookkeeping. It reports how many rows
// were removed.
func (r *Resolver) DeleteAccessGrantsForRequest(ctx context.Context, p domain.PrincipalID, requestID string) (int64, error) {
	if err := r.LockPrincipalRow(ctx, p); err != nil {
		return 0, err
	}
	if r.sq != nil {
		return r.sq.DeleteAccessGrantsForRequest(ctx, sqlitegen.DeleteAccessGrantsForRequestParams{
			RequestID: requestID, PrincipalID: string(p),
		})
	}
	return r.pg.DeleteAccessGrantsForRequest(ctx, pggen.DeleteAccessGrantsForRequestParams{
		RequestID: requestID, PrincipalID: string(p),
	})
}

// LiveAccessGrants lists a principal's unexpired temporary grants at now.
func (r *Resolver) LiveAccessGrants(ctx context.Context, p domain.PrincipalID, now time.Time) ([]AccessGrant, error) {
	var out []AccessGrant
	if r.sq != nil {
		rows, err := r.sq.ListLiveAccessGrantsForPrincipal(ctx, sqlitegen.ListLiveAccessGrantsForPrincipalParams{
			PrincipalID: string(p), ExpiresAt: encodeTime(now),
		})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			g, err := grantFrom(row.Capability, row.OrgID, row.ProjectID, row.EnvID)
			if err != nil {
				return nil, err
			}
			created, err := decodeTime(row.CreatedAt)
			if err != nil {
				return nil, err
			}
			expires, err := decodeTime(row.ExpiresAt)
			if err != nil {
				return nil, err
			}
			out = append(out, AccessGrant{ID: row.ID, Grant: g, RequestID: row.RequestID, CreatedAt: created.UTC(), ExpiresAt: expires.UTC()})
		}
		return out, nil
	}
	rows, err := r.pg.ListLiveAccessGrantsForPrincipal(ctx, pggen.ListLiveAccessGrantsForPrincipalParams{
		PrincipalID: string(p), ExpiresAt: pgTimestamp(now),
	})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		g, err := grantFrom(row.Capability, row.OrgID, row.ProjectID, row.EnvID)
		if err != nil {
			return nil, err
		}
		out = append(out, AccessGrant{ID: row.ID, Grant: g, RequestID: row.RequestID,
			CreatedAt: row.CreatedAt.Time.UTC(), ExpiresAt: row.ExpiresAt.Time.UTC()})
	}
	return out, nil
}
