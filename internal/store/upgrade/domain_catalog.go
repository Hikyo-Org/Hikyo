package upgrade

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// DomainCatalogSQLite is the shared release-build/boot/backup fingerprint seam.
// The caller owns a consistent snapshot. Only the independently validated exact
// control schema is omitted; arbitrary upgrade-prefixed objects remain drift.
func DomainCatalogSQLite(ctx context.Context, q SQLSnapshotQueries) (Catalog, error) {
	catalog, err := inspectCatalog(ctx, q, releaseidentity.SQLite)
	if err != nil {
		return Catalog{}, err
	}
	return domainCatalog(catalog)
}

func DomainCatalogPostgres(ctx context.Context, q PGSnapshotQueries) (Catalog, error) {
	catalog, err := inspectCatalogWith(ctx, func(ctx context.Context, query string, args ...any) (catalogRows, error) {
		return q.Query(ctx, query, args...)
	}, releaseidentity.Postgres)
	if err != nil {
		return Catalog{}, err
	}
	return domainCatalog(catalog)
}

func domainCatalog(catalog Catalog) (Catalog, error) {
	if controlPresent(catalog) {
		return withoutControl(catalog)
	}
	return catalog, nil
}

func (s *Session) DomainCatalog(ctx context.Context) (Catalog, error) {
	if err := s.check(); err != nil {
		return Catalog{}, err
	}
	catalog, err := inspectCatalog(ctx, s.conn, s.engine)
	if err != nil {
		return Catalog{}, err
	}
	return domainCatalog(catalog)
}
