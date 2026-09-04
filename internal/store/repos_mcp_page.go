package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// This file holds the bounded keyset page reads the MCP adapter needs (#629).
// Each method verifies the SAME store authorization its unbounded sibling does,
// so the existing operation proof authorizes it, and fetches strictly past the
// caller's cursor with a LIMIT rather than materializing a whole collection.

func (r sqliteEnvs) ListPage(ctx context.Context, p authz.Proof, afterDisplayOrder int64, afterName string, limit int) ([]Environment, error) {
	chain, err := authz.Verify(p, authz.StoreEnvironmentsListPage, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListEnvironmentsPageAtOrder(ctx, sqlitegen.ListEnvironmentsPageAtOrderParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project),
		AfterDisplayOrder: afterDisplayOrder, AfterName: afterName, PageLimit: int64(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]Environment, 0, limit)
	appendRow := func(id, orgID, projectID, name, note, createdAt string, displayOrder int64) error {
		created, err := parseTime("environment", id, createdAt)
		if err != nil {
			return err
		}
		out = append(out, Environment{ID: id, OrgID: orgID, ProjectID: projectID, Name: name, Note: note, DisplayOrder: displayOrder, CreatedAt: created})
		return nil
	}
	for _, row := range rows {
		if err := appendRow(row.ID, row.OrgID, row.ProjectID, row.Name, row.Note, row.CreatedAt, row.DisplayOrder); err != nil {
			return nil, err
		}
	}
	if len(out) < limit {
		later, err := r.q.ListEnvironmentsPageAfterOrder(ctx, sqlitegen.ListEnvironmentsPageAfterOrderParams{
			ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project),
			AfterDisplayOrder: afterDisplayOrder, PageLimit: int64(limit - len(out)),
		})
		if err != nil {
			return nil, err
		}
		for _, row := range later {
			if err := appendRow(row.ID, row.OrgID, row.ProjectID, row.Name, row.Note, row.CreatedAt, row.DisplayOrder); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func (r pgEnvs) ListPage(ctx context.Context, p authz.Proof, afterDisplayOrder int64, afterName string, limit int) ([]Environment, error) {
	chain, err := authz.Verify(p, authz.StoreEnvironmentsListPage, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListEnvironmentsPageAtOrder(ctx, pggen.ListEnvironmentsPageAtOrderParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project),
		AfterDisplayOrder: afterDisplayOrder, AfterName: afterName, PageLimit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]Environment, 0, limit)
	appendRow := func(id, orgID, projectID, name, note string, createdAt time.Time, createdAtValid bool, displayOrder int64) error {
		if !createdAtValid {
			return fmt.Errorf("store: environment %s: null created_at", id)
		}
		out = append(out, Environment{ID: id, OrgID: orgID, ProjectID: projectID, Name: name, Note: note, DisplayOrder: displayOrder, CreatedAt: createdAt.UTC()})
		return nil
	}
	for _, row := range rows {
		if err := appendRow(row.ID, row.OrgID, row.ProjectID, row.Name, row.Note, row.CreatedAt.Time, row.CreatedAt.Valid, row.DisplayOrder); err != nil {
			return nil, err
		}
	}
	if len(out) < limit {
		later, err := r.q.ListEnvironmentsPageAfterOrder(ctx, pggen.ListEnvironmentsPageAfterOrderParams{
			ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project),
			AfterDisplayOrder: afterDisplayOrder, PageLimit: int32(limit - len(out)),
		})
		if err != nil {
			return nil, err
		}
		for _, row := range later {
			if err := appendRow(row.ID, row.OrgID, row.ProjectID, row.Name, row.Note, row.CreatedAt.Time, row.CreatedAt.Valid, row.DisplayOrder); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func (r sqliteCatalogue) ListPage(ctx context.Context, p authz.Proof, afterName string, limit int) ([]CatalogueKey, error) {
	chain, err := authz.Verify(p, authz.StoreCatalogueListPage, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListKeysPage(ctx, sqlitegen.ListKeysPageParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project),
		AfterName: afterName, PageLimit: int64(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]CatalogueKey, 0, len(rows))
	for _, row := range rows {
		key, err := keyFromSQLite(row)
		if err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, nil
}

func (r pgCatalogue) ListPage(ctx context.Context, p authz.Proof, afterName string, limit int) ([]CatalogueKey, error) {
	chain, err := authz.Verify(p, authz.StoreCatalogueListPage, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListKeysPage(ctx, pggen.ListKeysPageParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project),
		AfterName: afterName, PageLimit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]CatalogueKey, 0, len(rows))
	for _, row := range rows {
		key, err := keyFromPG(row)
		if err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, nil
}

func (r sqliteCatalogue) GetInProject(ctx context.Context, p authz.Proof, id string) (CatalogueKey, error) {
	chain, err := authz.Verify(p, authz.StoreCatalogueGetInProject, r.tok)
	if err != nil {
		return CatalogueKey{}, err
	}
	row, err := r.q.GetKeyInProject(ctx, sqlitegen.GetKeyInProjectParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), KeyID: id,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return CatalogueKey{}, ErrNotFound
	}
	if err != nil {
		return CatalogueKey{}, err
	}
	return keyFromSQLite(row)
}

func (r pgCatalogue) GetInProject(ctx context.Context, p authz.Proof, id string) (CatalogueKey, error) {
	chain, err := authz.Verify(p, authz.StoreCatalogueGetInProject, r.tok)
	if err != nil {
		return CatalogueKey{}, err
	}
	row, err := r.q.GetKeyInProject(ctx, pggen.GetKeyInProjectParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), KeyID: id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return CatalogueKey{}, ErrNotFound
	}
	if err != nil {
		return CatalogueKey{}, err
	}
	return keyFromPG(row)
}

func (r sqliteCatalogue) PresenceForKey(ctx context.Context, p authz.Proof, keyID string) ([]KeyPresence, error) {
	chain, err := authz.Verify(p, authz.StoreCataloguePresenceForKey, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListKeyPresenceForKey(ctx, sqlitegen.ListKeyPresenceForKeyParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), KeyID: keyID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]KeyPresence, 0, len(rows))
	for _, row := range rows {
		out = append(out, KeyPresence{KeyID: row.KeyID, EnvironmentID: row.EnvironmentID, Rule: row.Rule})
	}
	return out, nil
}

func (r pgCatalogue) PresenceForKey(ctx context.Context, p authz.Proof, keyID string) ([]KeyPresence, error) {
	chain, err := authz.Verify(p, authz.StoreCataloguePresenceForKey, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListKeyPresenceForKey(ctx, pggen.ListKeyPresenceForKeyParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), KeyID: keyID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]KeyPresence, 0, len(rows))
	for _, row := range rows {
		out = append(out, KeyPresence{KeyID: row.KeyID, EnvironmentID: row.EnvironmentID, Rule: row.Rule})
	}
	return out, nil
}

func (r sqlitePending) ListForOwnerInEnvironmentPage(ctx context.Context, p authz.Proof, ownerID, afterKeyID string, limit int) ([]PendingChange, error) {
	chain, err := authz.Verify(p, authz.StorePendingListForOwnerInEnvironmentPage, r.tok)
	if err != nil {
		return nil, err
	}
	env, err := envOf(chain, authz.StorePendingListForOwnerInEnvironmentPage)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListPendingChangesForOwnerInEnvironmentPage(ctx, sqlitegen.ListPendingChangesForOwnerInEnvironmentPageParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		OwnerID: ownerID, AfterKeyID: afterKeyID, PageLimit: int64(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]PendingChange, 0, len(rows))
	for _, row := range rows {
		change, err := pendingFromSQLite(row)
		if err != nil {
			return nil, err
		}
		out = append(out, change)
	}
	return out, nil
}

func (r pgPending) ListForOwnerInEnvironmentPage(ctx context.Context, p authz.Proof, ownerID, afterKeyID string, limit int) ([]PendingChange, error) {
	chain, err := authz.Verify(p, authz.StorePendingListForOwnerInEnvironmentPage, r.tok)
	if err != nil {
		return nil, err
	}
	env, err := envOf(chain, authz.StorePendingListForOwnerInEnvironmentPage)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListPendingChangesForOwnerInEnvironmentPage(ctx, pggen.ListPendingChangesForOwnerInEnvironmentPageParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		OwnerID: ownerID, AfterKeyID: afterKeyID, PageLimit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]PendingChange, 0, len(rows))
	for _, row := range rows {
		change, err := pendingFromPostgres(row)
		if err != nil {
			return nil, err
		}
		out = append(out, change)
	}
	return out, nil
}

func (r sqliteSnapshots) ListPage(ctx context.Context, p authz.Proof, beforeRevision int64, limit int) ([]Snapshot, error) {
	chain, err := authz.Verify(p, authz.StoreSnapshotsListPage, r.tok)
	if err != nil {
		return nil, err
	}
	env, err := envOf(chain, authz.StoreSnapshotsListPage)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListSnapshotsPage(ctx, sqlitegen.ListSnapshotsPageParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		BeforeRevision: beforeRevision, PageLimit: int64(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]Snapshot, 0, len(rows))
	for _, row := range rows {
		snap, err := revisionSnapshotFromSQLite(row)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, nil
}

func (r pgSnapshots) ListPage(ctx context.Context, p authz.Proof, beforeRevision int64, limit int) ([]Snapshot, error) {
	chain, err := authz.Verify(p, authz.StoreSnapshotsListPage, r.tok)
	if err != nil {
		return nil, err
	}
	env, err := envOf(chain, authz.StoreSnapshotsListPage)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListSnapshotsPage(ctx, pggen.ListSnapshotsPageParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		BeforeRevision: beforeRevision, PageLimit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]Snapshot, 0, len(rows))
	for _, row := range rows {
		snap, err := revisionSnapshotFromPG(row)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, nil
}
