package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
)

func (q sqliteAdapterStoreQueries) publishedTargets(ctx context.Context, chain domain.Scope) ([]publishedAdapterTarget, error) {
	rows, err := q.queries.AdapterPublishedTargets(ctx, sqlitegen.AdapterPublishedTargetsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	var out []publishedAdapterTarget
	for _, c := range rows {
		out = append(out, publishedAdapterTarget{id: c.ID, environmentID: c.EnvironmentID, authority: c.AuthorityPrincipalID, generation: c.Generation, activeJob: c.ActiveJobID})
	}
	return out, nil
}
func (q sqliteAdapterStoreQueries) manualTarget(ctx context.Context, chain domain.Scope, targetID string, at time.Time) (publishedAdapterTarget, int64, int64, error) {
	c, err := q.queries.AdapterManualTarget(ctx, sqlitegen.AdapterManualTargetParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), At: runtimeSQLiteStamp(at)})
	return publishedAdapterTarget{id: c.ID, environmentID: c.EnvironmentID, generation: c.Generation, activeJob: c.ActiveJobID}, c.ProviderBusy, c.Paused, err
}
func (q sqliteAdapterStoreQueries) pauseTarget(ctx context.Context, chain domain.Scope, targetID string, at time.Time) (adapterTeardownTarget, int64, error) {
	c, err := q.queries.AdapterPauseTarget(ctx, sqlitegen.AdapterPauseTargetParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), At: runtimeSQLiteStamp(at)})
	return adapterTeardownTarget{adapterID: c.AdapterID, targetID: c.ID, environmentID: c.EnvironmentID, authority: c.AuthorityPrincipalID, generation: c.Generation, activeJob: c.ActiveJobID, providerBusy: int(c.ProviderBusy)}, c.Paused, err
}
func (q sqliteAdapterStoreQueries) healthCounts(ctx context.Context) (AdapterHealthCounts, error) {
	c, err := q.queries.AdapterHealthCounts(ctx)
	return AdapterHealthCounts{TargetsFailed: c.TargetsFailed, TargetsPaused: c.TargetsPaused, TargetsAttention: c.TargetsAttention, JobsQueued: c.JobsQueued}, err
}
func (q sqliteAdapterStoreQueries) teardownTarget(ctx context.Context, chain domain.Scope, targetID string, at time.Time) (adapterTeardownTarget, error) {
	c, err := q.queries.AdapterTeardownTarget(ctx, sqlitegen.AdapterTeardownTargetParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), At: runtimeSQLiteStamp(at)})
	return adapterTeardownTarget{adapterID: c.AdapterID, targetID: c.ID, environmentID: c.EnvironmentID, authority: c.AuthorityPrincipalID, generation: c.Generation, activeJob: c.ActiveJobID, providerBusy: int(c.ProviderBusy)}, err
}
func (q sqliteAdapterStoreQueries) teardownTargets(ctx context.Context, chain domain.Scope, adapterID string, at time.Time) ([]adapterTeardownTarget, error) {
	rows, err := q.queries.AdapterTeardownTargets(ctx, sqlitegen.AdapterTeardownTargetsParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), At: runtimeSQLiteStamp(at)})
	if err != nil {
		return nil, err
	}
	var out []adapterTeardownTarget
	for _, c := range rows {
		out = append(out, adapterTeardownTarget{adapterID: c.AdapterID, targetID: c.ID, environmentID: c.EnvironmentID, authority: c.AuthorityPrincipalID, generation: c.Generation, activeJob: c.ActiveJobID, providerBusy: int(c.ProviderBusy)})
	}
	return out, nil
}
func (q sqliteAdapterStoreQueries) teardownAuthority(ctx context.Context, chain domain.Scope, adapterID string) (string, error) {
	return q.queries.AdapterTeardownAuthority(ctx, sqlitegen.AdapterTeardownAuthorityParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
}
func (q sqliteAdapterStoreQueries) resumeRevision(ctx context.Context, chain domain.Scope, envID string) (int64, error) {
	return q.queries.AdapterResumeRevision(ctx, sqlitegen.AdapterResumeRevisionParams{EnvironmentID: envID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
}
func (q sqliteAdapterStoreQueries) resumeTarget(ctx context.Context, chain domain.Scope, target publishedAdapterTarget) (int64, error) {
	return adapterAffected(q.queries.AdapterResumeTarget(ctx, sqlitegen.AdapterResumeTargetParams{TargetID: target.id, EnvironmentID: target.environmentID, ExpectedGeneration: target.generation, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
func (q sqliteAdapterStoreQueries) pauseTargetUpdate(ctx context.Context, chain domain.Scope, target adapterTeardownTarget, next int64, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterPauseTargetUpdate(ctx, sqlitegen.AdapterPauseTargetUpdateParams{TargetID: target.targetID, EnvironmentID: target.environmentID, ExpectedGeneration: target.generation, NextGeneration: next, PausedAt: runtimeSQLiteStamp(at), At: runtimeSQLiteStamp(at), ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
func (q sqliteAdapterStoreQueries) enqueueTarget(ctx context.Context, chain domain.Scope, target publishedAdapterTarget, jobID string, next int64) (int64, error) {
	return adapterAffected(q.queries.AdapterEnqueueTarget(ctx, sqlitegen.AdapterEnqueueTargetParams{TargetID: target.id, EnvironmentID: target.environmentID, ExpectedGeneration: target.generation, NextGeneration: next, JobID: sql.NullString{String: jobID, Valid: true}, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
func (q pgAdapterStoreQueries) publishedTargets(ctx context.Context, chain domain.Scope) ([]publishedAdapterTarget, error) {
	rows, err := q.queries.AdapterPublishedTargets(ctx, pggen.AdapterPublishedTargetsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	var out []publishedAdapterTarget
	for _, c := range rows {
		out = append(out, publishedAdapterTarget{id: c.ID, environmentID: c.EnvironmentID, authority: c.AuthorityPrincipalID, generation: c.Generation, activeJob: c.ActiveJobID})
	}
	return out, nil
}
func (q pgAdapterStoreQueries) manualTarget(ctx context.Context, chain domain.Scope, targetID string, at time.Time) (publishedAdapterTarget, int64, int64, error) {
	c, err := q.queries.AdapterManualTarget(ctx, pggen.AdapterManualTargetParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), At: pgRequiredTime(at)})
	return publishedAdapterTarget{id: c.ID, environmentID: c.EnvironmentID, generation: c.Generation, activeJob: c.ActiveJobID}, c.ProviderBusy, c.Paused, err
}
func (q pgAdapterStoreQueries) pauseTarget(ctx context.Context, chain domain.Scope, targetID string, at time.Time) (adapterTeardownTarget, int64, error) {
	c, err := q.queries.AdapterPauseTarget(ctx, pggen.AdapterPauseTargetParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), At: pgRequiredTime(at)})
	return adapterTeardownTarget{adapterID: c.AdapterID, targetID: c.ID, environmentID: c.EnvironmentID, authority: c.AuthorityPrincipalID, generation: c.Generation, activeJob: c.ActiveJobID, providerBusy: int(c.ProviderBusy)}, c.Paused, err
}
func (q pgAdapterStoreQueries) healthCounts(ctx context.Context) (AdapterHealthCounts, error) {
	c, err := q.queries.AdapterHealthCounts(ctx)
	return AdapterHealthCounts{TargetsFailed: c.TargetsFailed, TargetsPaused: c.TargetsPaused, TargetsAttention: c.TargetsAttention, JobsQueued: c.JobsQueued}, err
}
func (q pgAdapterStoreQueries) teardownTarget(ctx context.Context, chain domain.Scope, targetID string, at time.Time) (adapterTeardownTarget, error) {
	c, err := q.queries.AdapterTeardownTarget(ctx, pggen.AdapterTeardownTargetParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), At: pgRequiredTime(at)})
	return adapterTeardownTarget{adapterID: c.AdapterID, targetID: c.ID, environmentID: c.EnvironmentID, authority: c.AuthorityPrincipalID, generation: c.Generation, activeJob: c.ActiveJobID, providerBusy: int(c.ProviderBusy)}, err
}
func (q pgAdapterStoreQueries) teardownTargets(ctx context.Context, chain domain.Scope, adapterID string, at time.Time) ([]adapterTeardownTarget, error) {
	rows, err := q.queries.AdapterTeardownTargets(ctx, pggen.AdapterTeardownTargetsParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), At: pgRequiredTime(at)})
	if err != nil {
		return nil, err
	}
	var out []adapterTeardownTarget
	for _, c := range rows {
		out = append(out, adapterTeardownTarget{adapterID: c.AdapterID, targetID: c.ID, environmentID: c.EnvironmentID, authority: c.AuthorityPrincipalID, generation: c.Generation, activeJob: c.ActiveJobID, providerBusy: int(c.ProviderBusy)})
	}
	return out, nil
}
func (q pgAdapterStoreQueries) teardownAuthority(ctx context.Context, chain domain.Scope, adapterID string) (string, error) {
	return q.queries.AdapterTeardownAuthority(ctx, pggen.AdapterTeardownAuthorityParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
}
func (q pgAdapterStoreQueries) resumeRevision(ctx context.Context, chain domain.Scope, envID string) (int64, error) {
	return q.queries.AdapterResumeRevision(ctx, pggen.AdapterResumeRevisionParams{EnvironmentID: envID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
}
func (q pgAdapterStoreQueries) resumeTarget(ctx context.Context, chain domain.Scope, target publishedAdapterTarget) (int64, error) {
	return adapterAffected(q.queries.AdapterResumeTarget(ctx, pggen.AdapterResumeTargetParams{TargetID: target.id, EnvironmentID: target.environmentID, ExpectedGeneration: target.generation, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
func (q pgAdapterStoreQueries) pauseTargetUpdate(ctx context.Context, chain domain.Scope, target adapterTeardownTarget, next int64, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterPauseTargetUpdate(ctx, pggen.AdapterPauseTargetUpdateParams{TargetID: target.targetID, EnvironmentID: target.environmentID, ExpectedGeneration: target.generation, NextGeneration: next, PausedAt: pgRequiredTime(at), At: pgRequiredTime(at), ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
func (q pgAdapterStoreQueries) enqueueTarget(ctx context.Context, chain domain.Scope, target publishedAdapterTarget, jobID string, next int64) (int64, error) {
	return adapterAffected(q.queries.AdapterEnqueueTarget(ctx, pggen.AdapterEnqueueTargetParams{TargetID: target.id, EnvironmentID: target.environmentID, ExpectedGeneration: target.generation, NextGeneration: next, JobID: pgtype.Text{String: jobID, Valid: true}, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
