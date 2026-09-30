package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
)

type adapterAdoptionTarget struct {
	adapterID, environmentID, origin, destinationKind, priorJob string
	repositoryID, destinationID, generation, providerBusy       int64
}

func (q sqliteAdapterStoreQueries) adoptionTarget(ctx context.Context, chain domain.Scope, adoption AdapterAdoption) (adapterAdoptionTarget, error) {
	c, err := q.queries.AdapterAdoptionTarget(ctx, sqlitegen.AdapterAdoptionTargetParams{At: runtimeSQLiteStamp(adoption.AuditAt), TargetID: adoption.TargetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	return adapterAdoptionTarget{adapterID: c.AdapterID, environmentID: c.EnvironmentID, origin: c.Origin, destinationKind: c.DestinationKind, priorJob: c.PriorJob, repositoryID: c.RepositoryID, destinationID: c.DestinationID, generation: c.Generation, providerBusy: c.ProviderBusy}, err
}
func (q sqliteAdapterStoreQueries) adoptionConflictCount(ctx context.Context, chain domain.Scope, adoption AdapterAdoption, row adapterAdoptionTarget, entry AdapterConflictEntry) (int64, error) {
	return q.queries.AdapterAdoptionConflictCount(ctx, sqlitegen.AdapterAdoptionConflictCountParams{ArtifactID: adoption.ArtifactID, TargetID: adoption.TargetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: row.environmentID, RepositoryID: row.repositoryID, DestinationID: row.destinationID, Generation: row.generation, Surface: entry.Surface, EffectiveName: entry.EffectiveName})
}
func (q sqliteAdapterStoreQueries) adoptionInsertLedger(ctx context.Context, chain domain.Scope, adoption AdapterAdoption, row adapterAdoptionTarget, entry AdapterConflictEntry, ledgerID string) (int64, error) {
	return adapterAffected(q.queries.AdapterAdoptionInsertLedger(ctx, sqlitegen.AdapterAdoptionInsertLedgerParams{LedgerID: ledgerID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: row.environmentID, TargetID: adoption.TargetID, ProviderOrigin: row.origin, DestinationKind: row.destinationKind, RepositoryID: row.repositoryID, DestinationID: row.destinationID, Surface: entry.Surface, EffectiveName: entry.EffectiveName, NormalizedName: strings.ToUpper(entry.EffectiveName), UpdatedAt: fixedStamp(adoption.AuditAt)}))
}
func (q sqliteAdapterStoreQueries) adoptionMarkConflict(ctx context.Context, chain domain.Scope, adoption AdapterAdoption, row adapterAdoptionTarget, entry AdapterConflictEntry) (int64, error) {
	return adapterAffected(q.queries.AdapterAdoptionMarkConflict(ctx, sqlitegen.AdapterAdoptionMarkConflictParams{AdoptedAt: runtimeSQLiteStamp(adoption.AuditAt), ArtifactID: adoption.ArtifactID, TargetID: adoption.TargetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: row.environmentID, Surface: entry.Surface, EffectiveName: entry.EffectiveName}))
}
func (q sqliteAdapterStoreQueries) adoptionSupersedeJob(ctx context.Context, chain domain.Scope, jobID, targetID, envID string, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterAdoptionSupersedeJob(ctx, sqlitegen.AdapterAdoptionSupersedeJobParams{FinishedAt: runtimeSQLiteStamp(at), JobID: jobID, TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: envID}))
}
func (q sqliteAdapterStoreQueries) adoptionInsertJob(ctx context.Context, chain domain.Scope, jobID string, target publishedAdapterTarget, generation int64, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterAdoptionInsertJob(ctx, sqlitegen.AdapterAdoptionInsertJobParams{JobID: jobID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: target.environmentID, TargetID: target.id, AuthorityPrincipalID: target.authority, Generation: generation, DedupKey: target.id, NextAttemptAt: fixedStamp(at), CreatedAt: fixedStamp(at)}))
}
func (q sqliteAdapterStoreQueries) adoptionUpdateTarget(ctx context.Context, chain domain.Scope, adoption AdapterAdoption, row adapterAdoptionTarget, next int64) (int64, error) {
	return adapterAffected(q.queries.AdapterAdoptionUpdateTarget(ctx, sqlitegen.AdapterAdoptionUpdateTargetParams{NextGeneration: next, JobID: sql.NullString{String: adoption.JobID, Valid: true}, TargetID: adoption.TargetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: row.environmentID, ExpectedGeneration: row.generation, At: runtimeSQLiteStamp(adoption.AuditAt)}))
}
func (q sqliteAdapterStoreQueries) adoptionUpdateAuthority(ctx context.Context, chain domain.Scope, adapterID, authority string) (int64, error) {
	return adapterAffected(q.queries.AdapterAdoptionUpdateAuthority(ctx, sqlitegen.AdapterAdoptionUpdateAuthorityParams{AuthorityPrincipalID: authority, AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
func (q pgAdapterStoreQueries) adoptionTarget(ctx context.Context, chain domain.Scope, adoption AdapterAdoption) (adapterAdoptionTarget, error) {
	c, err := q.queries.AdapterAdoptionTarget(ctx, pggen.AdapterAdoptionTargetParams{At: pgRequiredTime(adoption.AuditAt), TargetID: adoption.TargetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	return adapterAdoptionTarget{adapterID: c.AdapterID, environmentID: c.EnvironmentID, origin: c.Origin, destinationKind: c.DestinationKind, priorJob: c.PriorJob, repositoryID: c.RepositoryID, destinationID: c.DestinationID, generation: c.Generation, providerBusy: c.ProviderBusy}, err
}
func (q pgAdapterStoreQueries) adoptionConflictCount(ctx context.Context, chain domain.Scope, adoption AdapterAdoption, row adapterAdoptionTarget, entry AdapterConflictEntry) (int64, error) {
	return q.queries.AdapterAdoptionConflictCount(ctx, pggen.AdapterAdoptionConflictCountParams{ArtifactID: adoption.ArtifactID, TargetID: adoption.TargetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: row.environmentID, RepositoryID: row.repositoryID, DestinationID: row.destinationID, Generation: row.generation, Surface: entry.Surface, EffectiveName: entry.EffectiveName})
}
func (q pgAdapterStoreQueries) adoptionInsertLedger(ctx context.Context, chain domain.Scope, adoption AdapterAdoption, row adapterAdoptionTarget, entry AdapterConflictEntry, ledgerID string) (int64, error) {
	return adapterAffected(q.queries.AdapterAdoptionInsertLedger(ctx, pggen.AdapterAdoptionInsertLedgerParams{LedgerID: ledgerID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: row.environmentID, TargetID: adoption.TargetID, ProviderOrigin: row.origin, DestinationKind: row.destinationKind, RepositoryID: row.repositoryID, DestinationID: row.destinationID, Surface: entry.Surface, EffectiveName: entry.EffectiveName, NormalizedName: strings.ToUpper(entry.EffectiveName), UpdatedAt: pgRequiredTime(adoption.AuditAt)}))
}
func (q pgAdapterStoreQueries) adoptionMarkConflict(ctx context.Context, chain domain.Scope, adoption AdapterAdoption, row adapterAdoptionTarget, entry AdapterConflictEntry) (int64, error) {
	return adapterAffected(q.queries.AdapterAdoptionMarkConflict(ctx, pggen.AdapterAdoptionMarkConflictParams{AdoptedAt: pgRequiredTime(adoption.AuditAt), ArtifactID: adoption.ArtifactID, TargetID: adoption.TargetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: row.environmentID, Surface: entry.Surface, EffectiveName: entry.EffectiveName}))
}
func (q pgAdapterStoreQueries) adoptionSupersedeJob(ctx context.Context, chain domain.Scope, jobID, targetID, envID string, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterAdoptionSupersedeJob(ctx, pggen.AdapterAdoptionSupersedeJobParams{FinishedAt: pgRequiredTime(at), JobID: jobID, TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: envID}))
}
func (q pgAdapterStoreQueries) adoptionInsertJob(ctx context.Context, chain domain.Scope, jobID string, target publishedAdapterTarget, generation int64, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterAdoptionInsertJob(ctx, pggen.AdapterAdoptionInsertJobParams{JobID: jobID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: target.environmentID, TargetID: target.id, AuthorityPrincipalID: target.authority, Generation: generation, DedupKey: target.id, NextAttemptAt: pgRequiredTime(at), CreatedAt: pgRequiredTime(at)}))
}
func (q pgAdapterStoreQueries) adoptionUpdateTarget(ctx context.Context, chain domain.Scope, adoption AdapterAdoption, row adapterAdoptionTarget, next int64) (int64, error) {
	return adapterAffected(q.queries.AdapterAdoptionUpdateTarget(ctx, pggen.AdapterAdoptionUpdateTargetParams{NextGeneration: next, JobID: pgtype.Text{String: adoption.JobID, Valid: true}, TargetID: adoption.TargetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: row.environmentID, ExpectedGeneration: row.generation, At: pgRequiredTime(adoption.AuditAt)}))
}
func (q pgAdapterStoreQueries) adoptionUpdateAuthority(ctx context.Context, chain domain.Scope, adapterID, authority string) (int64, error) {
	return adapterAffected(q.queries.AdapterAdoptionUpdateAuthority(ctx, pggen.AdapterAdoptionUpdateAuthorityParams{AuthorityPrincipalID: authority, AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
