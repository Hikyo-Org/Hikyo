package store

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// adapterConfigQueries reads only the proof-derived route chain.
type adapterConfigQueries interface {
	configuredNames(ctx context.Context, chain domain.Scope, target AdapterTargetMutation, excludeTargetID string) ([]adapterConfigConfiguredNamesRow, error)
	pendingNames(ctx context.Context, chain domain.Scope, target AdapterTargetMutation, excludeTargetID string) ([]adapterConfigPendingNamesRow, error)
	awsConfiguredNames(ctx context.Context, chain domain.Scope, target AdapterTargetMutation, excludeTargetID string) ([]adapterConfigAWSConfiguredNamesRow, error)
	awsPendingNames(ctx context.Context, chain domain.Scope, target AdapterTargetMutation, excludeTargetID string) ([]adapterConfigAWSPendingNamesRow, error)
	findings(ctx context.Context, chain domain.Scope, target AdapterTarget) ([]AdapterFinding, error)
}
type sqliteAdapterConfigQueries struct{ queries *sqlitegen.Queries }
type pgAdapterConfigQueries struct{ queries *pggen.Queries }

func (d sqliteAdoptDB) adapterConfigQueries() adapterConfigQueries {
	return sqliteAdapterConfigQueries{queries: sqlitegen.New(d.db)}
}
func (d pgAdoptDB) adapterConfigQueries() adapterConfigQueries {
	return pgAdapterConfigQueries{queries: pggen.New(d.db)}
}
func (d sqliteAdapterTx) adapterConfigQueries() adapterConfigQueries {
	return sqliteAdapterConfigQueries{queries: sqlitegen.New(d.tx)}
}
func (d pgAdapterTx) adapterConfigQueries() adapterConfigQueries {
	return pgAdapterConfigQueries{queries: pggen.New(d.tx)}
}
func (q sqliteAdapterConfigQueries) configuredNames(ctx context.Context, chain domain.Scope, target AdapterTargetMutation, excludeTargetID string) ([]adapterConfigConfiguredNamesRow, error) {
	rows, err := q.queries.AdapterConfigConfiguredNames(ctx, sqlitegen.AdapterConfigConfiguredNamesParams{AdapterID: target.AdapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), DestinationKind: target.DestinationKind, DestinationID: target.DestinationID, DestinationScope: target.DestinationScope, ExcludeTargetID: excludeTargetID})
	if err != nil {
		return nil, err
	}
	out := []adapterConfigConfiguredNamesRow{}
	for _, row := range rows {
		out = append(out, adapterConfigConfiguredNamesRow{TargetID: row.TargetID, Prefix: row.Prefix, CanonicalName: row.CanonicalName})
	}
	return out, nil
}
func (q sqliteAdapterConfigQueries) pendingNames(ctx context.Context, chain domain.Scope, target AdapterTargetMutation, excludeTargetID string) ([]adapterConfigPendingNamesRow, error) {
	rows, err := q.queries.AdapterConfigPendingNames(ctx, sqlitegen.AdapterConfigPendingNamesParams{AdapterID: target.AdapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), DestinationKind: target.DestinationKind, DestinationOwner: target.DestinationOwner, DestinationName: target.DestinationName, DestinationEnvironment: target.DestinationEnvironment, DestinationScope: target.DestinationScope, ExcludeTargetID: excludeTargetID})
	if err != nil {
		return nil, err
	}
	out := []adapterConfigPendingNamesRow{}
	for _, row := range rows {
		out = append(out, adapterConfigPendingNamesRow{TargetID: row.TargetID, EffectiveName: row.EffectiveName})
	}
	return out, nil
}
func (q sqliteAdapterConfigQueries) awsConfiguredNames(ctx context.Context, chain domain.Scope, target AdapterTargetMutation, excludeTargetID string) ([]adapterConfigAWSConfiguredNamesRow, error) {
	rows, err := q.queries.AdapterConfigAWSConfiguredNames(ctx, sqlitegen.AdapterConfigAWSConfiguredNamesParams{AdapterID: target.AdapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), DestinationOwner: target.DestinationOwner, ExcludeTargetID: excludeTargetID})
	if err != nil {
		return nil, err
	}
	out := []adapterConfigAWSConfiguredNamesRow{}
	for _, row := range rows {
		out = append(out, adapterConfigAWSConfiguredNamesRow{TargetID: row.TargetID, Kind: row.Kind, Name: row.Name, Prefix: row.Prefix, KeyName: row.KeyName})
	}
	return out, nil
}
func (q sqliteAdapterConfigQueries) awsPendingNames(ctx context.Context, chain domain.Scope, target AdapterTargetMutation, excludeTargetID string) ([]adapterConfigAWSPendingNamesRow, error) {
	rows, err := q.queries.AdapterConfigAWSPendingNames(ctx, sqlitegen.AdapterConfigAWSPendingNamesParams{AdapterID: target.AdapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), DestinationOwner: target.DestinationOwner, ExcludeTargetID: excludeTargetID})
	if err != nil {
		return nil, err
	}
	out := []adapterConfigAWSPendingNamesRow{}
	for _, row := range rows {
		out = append(out, adapterConfigAWSPendingNamesRow{TargetID: row.TargetID, EffectiveName: row.EffectiveName})
	}
	return out, nil
}
func (q sqliteAdapterConfigQueries) findings(ctx context.Context, chain domain.Scope, target AdapterTarget) ([]AdapterFinding, error) {
	rows, err := q.queries.AdapterConfigFindings(ctx, sqlitegen.AdapterConfigFindingsParams{TargetID: target.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: target.EnvironmentID, Generation: target.Generation})
	if err != nil {
		return nil, err
	}
	out := []AdapterFinding{}
	for _, row := range rows {
		out = append(out, AdapterFinding{Surface: row.Surface, EffectiveName: row.EffectiveName, Finding: row.Finding})
	}
	return out, nil
}
func (q pgAdapterConfigQueries) configuredNames(ctx context.Context, chain domain.Scope, target AdapterTargetMutation, excludeTargetID string) ([]adapterConfigConfiguredNamesRow, error) {
	rows, err := q.queries.AdapterConfigConfiguredNames(ctx, pggen.AdapterConfigConfiguredNamesParams{AdapterID: target.AdapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), DestinationKind: target.DestinationKind, DestinationID: target.DestinationID, DestinationScope: target.DestinationScope, ExcludeTargetID: excludeTargetID})
	if err != nil {
		return nil, err
	}
	out := []adapterConfigConfiguredNamesRow{}
	for _, row := range rows {
		out = append(out, adapterConfigConfiguredNamesRow{TargetID: row.TargetID, Prefix: row.Prefix, CanonicalName: row.CanonicalName})
	}
	return out, nil
}
func (q pgAdapterConfigQueries) pendingNames(ctx context.Context, chain domain.Scope, target AdapterTargetMutation, excludeTargetID string) ([]adapterConfigPendingNamesRow, error) {
	rows, err := q.queries.AdapterConfigPendingNames(ctx, pggen.AdapterConfigPendingNamesParams{AdapterID: target.AdapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), DestinationKind: target.DestinationKind, DestinationOwner: target.DestinationOwner, DestinationName: target.DestinationName, DestinationEnvironment: target.DestinationEnvironment, DestinationScope: target.DestinationScope, ExcludeTargetID: excludeTargetID})
	if err != nil {
		return nil, err
	}
	out := []adapterConfigPendingNamesRow{}
	for _, row := range rows {
		out = append(out, adapterConfigPendingNamesRow{TargetID: row.TargetID, EffectiveName: row.EffectiveName})
	}
	return out, nil
}
func (q pgAdapterConfigQueries) awsConfiguredNames(ctx context.Context, chain domain.Scope, target AdapterTargetMutation, excludeTargetID string) ([]adapterConfigAWSConfiguredNamesRow, error) {
	rows, err := q.queries.AdapterConfigAWSConfiguredNames(ctx, pggen.AdapterConfigAWSConfiguredNamesParams{AdapterID: target.AdapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), DestinationOwner: target.DestinationOwner, ExcludeTargetID: excludeTargetID})
	if err != nil {
		return nil, err
	}
	out := []adapterConfigAWSConfiguredNamesRow{}
	for _, row := range rows {
		out = append(out, adapterConfigAWSConfiguredNamesRow{TargetID: row.TargetID, Kind: row.Kind, Name: row.Name, Prefix: row.Prefix, KeyName: row.KeyName})
	}
	return out, nil
}
func (q pgAdapterConfigQueries) awsPendingNames(ctx context.Context, chain domain.Scope, target AdapterTargetMutation, excludeTargetID string) ([]adapterConfigAWSPendingNamesRow, error) {
	rows, err := q.queries.AdapterConfigAWSPendingNames(ctx, pggen.AdapterConfigAWSPendingNamesParams{AdapterID: target.AdapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), DestinationOwner: target.DestinationOwner, ExcludeTargetID: excludeTargetID})
	if err != nil {
		return nil, err
	}
	out := []adapterConfigAWSPendingNamesRow{}
	for _, row := range rows {
		out = append(out, adapterConfigAWSPendingNamesRow{TargetID: row.TargetID, EffectiveName: row.EffectiveName})
	}
	return out, nil
}
func (q pgAdapterConfigQueries) findings(ctx context.Context, chain domain.Scope, target AdapterTarget) ([]AdapterFinding, error) {
	rows, err := q.queries.AdapterConfigFindings(ctx, pggen.AdapterConfigFindingsParams{TargetID: target.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: target.EnvironmentID, Generation: target.Generation})
	if err != nil {
		return nil, err
	}
	out := []AdapterFinding{}
	for _, row := range rows {
		out = append(out, AdapterFinding{Surface: row.Surface, EffectiveName: row.EffectiveName, Finding: row.Finding})
	}
	return out, nil
}
