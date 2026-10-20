package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r sqliteProjects) GetCapacity(ctx context.Context, p authz.Proof) (ProjectCapacity, error) {
	chain, err := authz.Verify(p, authz.StoreProjectsGetCapacity, r.tok)
	if err != nil {
		return ProjectCapacity{}, err
	}
	row, err := r.q.GetProjectCapacity(ctx, sqlitegen.GetProjectCapacityParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project)})
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectCapacity{}, ErrNotFound
	}
	if err != nil {
		return ProjectCapacity{}, err
	}
	out := ProjectCapacity{Revision: row.KeyLimitRevision}
	if row.KeyLimitOverride.Valid {
		n := int(row.KeyLimitOverride.Int64)
		out.KeyLimitOverride = &n
	}
	return out, nil
}

func (r sqliteProjects) SetCapacity(ctx context.Context, p authz.Proof, override *int, expectedRevision int64) error {
	chain, err := authz.Verify(p, authz.StoreProjectsSetCapacity, r.tok)
	if err != nil {
		return err
	}
	params := sqlitegen.SetProjectCapacityParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ExpectedRevision: expectedRevision}
	if override != nil {
		params.KeyLimitOverride = sql.NullInt64{Int64: int64(*override), Valid: true}
	}
	n, err := r.q.SetProjectCapacity(ctx, params)
	if err != nil {
		return constraint(err)
	}
	if n == 0 {
		return fmt.Errorf("%w: project capacity revision changed", ErrConflict)
	}
	return nil
}

func (r pgProjects) GetCapacity(ctx context.Context, p authz.Proof) (ProjectCapacity, error) {
	chain, err := authz.Verify(p, authz.StoreProjectsGetCapacity, r.tok)
	if err != nil {
		return ProjectCapacity{}, err
	}
	row, err := r.q.GetProjectCapacity(ctx, pggen.GetProjectCapacityParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project)})
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectCapacity{}, ErrNotFound
	}
	if err != nil {
		return ProjectCapacity{}, err
	}
	out := ProjectCapacity{Revision: row.KeyLimitRevision}
	if row.KeyLimitOverride.Valid {
		n := int(row.KeyLimitOverride.Int64)
		out.KeyLimitOverride = &n
	}
	return out, nil
}

func (r pgProjects) SetCapacity(ctx context.Context, p authz.Proof, override *int, expectedRevision int64) error {
	chain, err := authz.Verify(p, authz.StoreProjectsSetCapacity, r.tok)
	if err != nil {
		return err
	}
	params := pggen.SetProjectCapacityParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ExpectedRevision: expectedRevision}
	if override != nil {
		params.KeyLimitOverride = pgtype.Int8{Int64: int64(*override), Valid: true}
	}
	n, err := r.q.SetProjectCapacity(ctx, params)
	if err != nil {
		return constraint(err)
	}
	if n == 0 {
		return fmt.Errorf("%w: project capacity revision changed", ErrConflict)
	}
	return nil
}
