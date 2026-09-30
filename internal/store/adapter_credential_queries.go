package store

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
)

func checkedAdapterTargetCount(value int64) (int, error) {
	if value < 0 || value > math.MaxInt {
		return 0, fmt.Errorf("store: adapter target count out of range: %d", value)
	}
	return int(value), nil
}
func (q sqliteAdapterStoreQueries) orphans(ctx context.Context, chain domain.Scope, targetID, envID string) ([]string, error) {
	rows, err := q.queries.AdapterOrphans(ctx, sqlitegen.AdapterOrphansParams{TargetID: targetID, EnvironmentID: envID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.AdapterOrphansRow) (string, error) { return c.Surface + ":" + c.EffectiveName, nil })
}
func (q sqliteAdapterStoreQueries) replaceCredentialTarget(ctx context.Context, chain domain.Scope, adapterID string, at time.Time) (string, int, int64, error) {
	c, err := q.queries.AdapterReplaceCredentialTarget(ctx, sqlitegen.AdapterReplaceCredentialTargetParams{AdapterID: adapterID, At: runtimeSQLiteStamp(at), ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return "", 0, 0, err
	}
	count, err := checkedAdapterTargetCount(c.TargetCount)
	return c.AuthorityPrincipalID, count, c.ProviderBusy, err
}
func (q sqliteAdapterStoreQueries) revokeCredentialTarget(ctx context.Context, chain domain.Scope, adapterID string) (string, int, error) {
	c, err := q.queries.AdapterRevokeCredentialTarget(ctx, sqlitegen.AdapterRevokeCredentialTargetParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return "", 0, err
	}
	count, err := checkedAdapterTargetCount(c.TargetCount)
	return c.AuthorityPrincipalID, count, err
}
func (q sqliteAdapterStoreQueries) releaseLedger(ctx context.Context, chain domain.Scope, target adapterTeardownTarget, at time.Time) error {
	return constraint(q.queries.AdapterReleaseLedger(ctx, sqlitegen.AdapterReleaseLedgerParams{UpdatedAt: fixedStamp(at), TargetID: target.targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: target.environmentID}))
}
func (q sqliteAdapterStoreQueries) retainTarget(ctx context.Context, chain domain.Scope, target adapterTeardownTarget, next int64, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterRetainTarget(ctx, sqlitegen.AdapterRetainTargetParams{NextGeneration: next, TargetID: target.targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: target.environmentID, ExpectedGeneration: target.generation, At: runtimeSQLiteStamp(at)}))
}
func (q sqliteAdapterStoreQueries) insertScrubJob(ctx context.Context, chain domain.Scope, target adapterTeardownTarget, result AdapterTeardownResult, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterInsertScrubJob(ctx, sqlitegen.AdapterInsertScrubJobParams{JobID: result.JobID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: target.environmentID, TargetID: target.targetID, AuthorityPrincipalID: target.authority, Generation: result.Generation, DedupKey: target.targetID, NextAttemptAt: fixedStamp(at), CreatedAt: fixedStamp(at)}))
}
func (q sqliteAdapterStoreQueries) scrubTarget(ctx context.Context, chain domain.Scope, target adapterTeardownTarget, result AdapterTeardownResult, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterScrubTarget(ctx, sqlitegen.AdapterScrubTargetParams{NextGeneration: result.Generation, JobID: sql.NullString{String: result.JobID, Valid: true}, TargetID: target.targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: target.environmentID, ExpectedGeneration: target.generation, At: runtimeSQLiteStamp(at)}))
}
func (q sqliteAdapterStoreQueries) markTombstoned(ctx context.Context, chain domain.Scope, adapterID string) (int64, error) {
	return adapterAffected(q.queries.AdapterMarkTombstoned(ctx, sqlitegen.AdapterMarkTombstonedParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
func (q sqliteAdapterStoreQueries) eraseUnusedCredential(ctx context.Context, chain domain.Scope, adapterID string) error {
	return constraint(q.queries.AdapterEraseUnusedCredential(ctx, sqlitegen.AdapterEraseUnusedCredentialParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
func (q sqliteAdapterStoreQueries) replaceCredential(ctx context.Context, chain domain.Scope, m AdapterCredentialMutation) (int64, error) {
	return adapterAffected(q.queries.AdapterReplaceCredential(ctx, sqlitegen.AdapterReplaceCredentialParams{CredentialCiphertext: m.CredentialCiphertext, At: runtimeSQLiteStamp(m.At), AuthorityPrincipalID: m.AuthorityPrincipalID, AdapterID: m.AdapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
func (q sqliteAdapterStoreQueries) replaceCredentialBump(ctx context.Context, chain domain.Scope, adapterID string, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterReplaceCredentialBump(ctx, sqlitegen.AdapterReplaceCredentialBumpParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), At: runtimeSQLiteStamp(at)}))
}
func (q sqliteAdapterStoreQueries) revokeCredential(ctx context.Context, chain domain.Scope, adapterID string) (int64, error) {
	return adapterAffected(q.queries.AdapterRevokeCredential(ctx, sqlitegen.AdapterRevokeCredentialParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
func (q sqliteAdapterStoreQueries) revokeCredentialBump(ctx context.Context, chain domain.Scope, adapterID string) (int64, error) {
	return adapterAffected(q.queries.AdapterRevokeCredentialBump(ctx, sqlitegen.AdapterRevokeCredentialBumpParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
func (q pgAdapterStoreQueries) orphans(ctx context.Context, chain domain.Scope, targetID, envID string) ([]string, error) {
	rows, err := q.queries.AdapterOrphans(ctx, pggen.AdapterOrphansParams{TargetID: targetID, EnvironmentID: envID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.AdapterOrphansRow) (string, error) { return c.Surface + ":" + c.EffectiveName, nil })
}
func (q pgAdapterStoreQueries) replaceCredentialTarget(ctx context.Context, chain domain.Scope, adapterID string, at time.Time) (string, int, int64, error) {
	c, err := q.queries.AdapterReplaceCredentialTarget(ctx, pggen.AdapterReplaceCredentialTargetParams{AdapterID: adapterID, At: pgRequiredTime(at), ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return "", 0, 0, err
	}
	count, err := checkedAdapterTargetCount(c.TargetCount)
	return c.AuthorityPrincipalID, count, c.ProviderBusy, err
}
func (q pgAdapterStoreQueries) revokeCredentialTarget(ctx context.Context, chain domain.Scope, adapterID string) (string, int, error) {
	c, err := q.queries.AdapterRevokeCredentialTarget(ctx, pggen.AdapterRevokeCredentialTargetParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return "", 0, err
	}
	count, err := checkedAdapterTargetCount(c.TargetCount)
	return c.AuthorityPrincipalID, count, err
}
func (q pgAdapterStoreQueries) releaseLedger(ctx context.Context, chain domain.Scope, target adapterTeardownTarget, at time.Time) error {
	return constraint(q.queries.AdapterReleaseLedger(ctx, pggen.AdapterReleaseLedgerParams{UpdatedAt: pgRequiredTime(at), TargetID: target.targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: target.environmentID}))
}
func (q pgAdapterStoreQueries) retainTarget(ctx context.Context, chain domain.Scope, target adapterTeardownTarget, next int64, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterRetainTarget(ctx, pggen.AdapterRetainTargetParams{NextGeneration: next, TargetID: target.targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: target.environmentID, ExpectedGeneration: target.generation, At: pgRequiredTime(at)}))
}
func (q pgAdapterStoreQueries) insertScrubJob(ctx context.Context, chain domain.Scope, target adapterTeardownTarget, result AdapterTeardownResult, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterInsertScrubJob(ctx, pggen.AdapterInsertScrubJobParams{JobID: result.JobID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: target.environmentID, TargetID: target.targetID, AuthorityPrincipalID: target.authority, Generation: result.Generation, DedupKey: target.targetID, NextAttemptAt: pgRequiredTime(at), CreatedAt: pgRequiredTime(at)}))
}
func (q pgAdapterStoreQueries) scrubTarget(ctx context.Context, chain domain.Scope, target adapterTeardownTarget, result AdapterTeardownResult, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterScrubTarget(ctx, pggen.AdapterScrubTargetParams{NextGeneration: result.Generation, JobID: pgtype.Text{String: result.JobID, Valid: true}, TargetID: target.targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: target.environmentID, ExpectedGeneration: target.generation, At: pgRequiredTime(at)}))
}
func (q pgAdapterStoreQueries) markTombstoned(ctx context.Context, chain domain.Scope, adapterID string) (int64, error) {
	return adapterAffected(q.queries.AdapterMarkTombstoned(ctx, pggen.AdapterMarkTombstonedParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
func (q pgAdapterStoreQueries) eraseUnusedCredential(ctx context.Context, chain domain.Scope, adapterID string) error {
	return constraint(q.queries.AdapterEraseUnusedCredential(ctx, pggen.AdapterEraseUnusedCredentialParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
func (q pgAdapterStoreQueries) replaceCredential(ctx context.Context, chain domain.Scope, m AdapterCredentialMutation) (int64, error) {
	return adapterAffected(q.queries.AdapterReplaceCredential(ctx, pggen.AdapterReplaceCredentialParams{CredentialCiphertext: m.CredentialCiphertext, At: pgRequiredTime(m.At), AuthorityPrincipalID: m.AuthorityPrincipalID, AdapterID: m.AdapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
func (q pgAdapterStoreQueries) replaceCredentialBump(ctx context.Context, chain domain.Scope, adapterID string, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterReplaceCredentialBump(ctx, pggen.AdapterReplaceCredentialBumpParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), At: pgRequiredTime(at)}))
}
func (q pgAdapterStoreQueries) revokeCredential(ctx context.Context, chain domain.Scope, adapterID string) (int64, error) {
	return adapterAffected(q.queries.AdapterRevokeCredential(ctx, pggen.AdapterRevokeCredentialParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
func (q pgAdapterStoreQueries) revokeCredentialBump(ctx context.Context, chain domain.Scope, adapterID string) (int64, error) {
	return adapterAffected(q.queries.AdapterRevokeCredentialBump(ctx, pggen.AdapterRevokeCredentialBumpParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)}))
}
