package store

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

type adapterOriginRoute struct {
	provider, origin, destinationKind, destinationScope string
	repositoryID, destinationID                         int64
}

type adapterOriginCandidateRequest struct {
	provider, destinationKind, destinationScope, surface, normalizedName, targetID, cursor    string
	authority, authorityRoot, authorityPort, authorityDot, authorityDotRoot, authorityDotPort string
	repositoryID, destinationID                                                               int64
	ipv6                                                                                      bool
}

type adapterOriginCandidateRow struct{ id, targetID, origin string }

type adapterOriginQueries interface {
	currentRoute(context.Context, string, domain.Scope) (adapterOriginRoute, error)
	heldCandidates(context.Context, adapterOriginCandidateRequest) ([]adapterOriginCandidateRow, error)
}

type sqliteAdapterOriginQueries struct{ queries *sqlitegen.Queries }
type pgAdapterOriginQueries struct{ queries *pggen.Queries }

func (d sqliteAdoptDB) adapterOriginQueries() adapterOriginQueries {
	return sqliteAdapterOriginQueries{queries: sqlitegen.New(d.db)}
}
func (d pgAdoptDB) adapterOriginQueries() adapterOriginQueries {
	return pgAdapterOriginQueries{queries: pggen.New(d.db)}
}
func (d sqliteAdapterTx) adapterOriginQueries() adapterOriginQueries {
	return sqliteAdapterOriginQueries{queries: sqlitegen.New(d.tx)}
}
func (d pgAdapterTx) adapterOriginQueries() adapterOriginQueries {
	return pgAdapterOriginQueries{queries: pggen.New(d.tx)}
}

func (q sqliteAdapterOriginQueries) currentRoute(ctx context.Context, targetID string, chain domain.Scope) (adapterOriginRoute, error) {
	r, err := q.queries.AdapterOriginCurrentRoute(ctx, sqlitegen.AdapterOriginCurrentRouteParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return adapterOriginRoute{provider: r.Provider, origin: r.Origin, destinationKind: r.DestinationKind, destinationScope: r.DestinationScope, repositoryID: r.RepositoryID, destinationID: r.DestinationID}, err
}

func (q pgAdapterOriginQueries) currentRoute(ctx context.Context, targetID string, chain domain.Scope) (adapterOriginRoute, error) {
	r, err := q.queries.AdapterOriginCurrentRoute(ctx, pggen.AdapterOriginCurrentRouteParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return adapterOriginRoute{provider: r.Provider, origin: r.Origin, destinationKind: r.DestinationKind, destinationScope: r.DestinationScope, repositoryID: r.RepositoryID, destinationID: r.DestinationID}, err
}

func (q sqliteAdapterOriginQueries) heldCandidates(ctx context.Context, p adapterOriginCandidateRequest) ([]adapterOriginCandidateRow, error) {
	var ipv6 int64
	if p.ipv6 {
		ipv6 = 1
	}
	rows, err := q.queries.AdapterOriginHeldCandidates(ctx, sqlitegen.AdapterOriginHeldCandidatesParams{Provider: p.provider, DestinationKind: p.destinationKind, RepositoryID: p.repositoryID, DestinationID: p.destinationID, DestinationScope: p.destinationScope, Surface: p.surface, NormalizedName: p.normalizedName, Cursor: p.cursor, TargetID: p.targetID, Ipv6: ipv6, Authority: p.authority, AuthorityRoot: p.authorityRoot, AuthorityPort: p.authorityPort, AuthorityDot: p.authorityDot, AuthorityDotRoot: p.authorityDotRoot, AuthorityDotPort: p.authorityDotPort})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(r sqlitegen.AdapterOriginHeldCandidatesRow) (adapterOriginCandidateRow, error) {
		return adapterOriginCandidateRow{id: r.ID, targetID: r.TargetID, origin: r.ProviderOrigin}, nil
	})
}

func (q pgAdapterOriginQueries) heldCandidates(ctx context.Context, p adapterOriginCandidateRequest) ([]adapterOriginCandidateRow, error) {
	var ipv6 int32
	if p.ipv6 {
		ipv6 = 1
	}
	rows, err := q.queries.AdapterOriginHeldCandidates(ctx, pggen.AdapterOriginHeldCandidatesParams{Provider: p.provider, DestinationKind: p.destinationKind, RepositoryID: p.repositoryID, DestinationID: p.destinationID, DestinationScope: p.destinationScope, Surface: p.surface, NormalizedName: p.normalizedName, Cursor: p.cursor, TargetID: p.targetID, Ipv6: ipv6, Authority: p.authority, AuthorityRoot: p.authorityRoot, AuthorityPort: p.authorityPort, AuthorityDot: p.authorityDot, AuthorityDotRoot: p.authorityDotRoot, AuthorityDotPort: p.authorityDotPort})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(r pggen.AdapterOriginHeldCandidatesRow) (adapterOriginCandidateRow, error) {
		return adapterOriginCandidateRow{id: r.ID, targetID: r.TargetID, origin: r.ProviderOrigin}, nil
	})
}
