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

// adapterMoveQueries preserves the verified tenant chain for every move query.
type adapterMoveQueries interface {
	get(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (adapterMoveGetRow, error)
	getLocked(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (adapterMoveGetRow, error)
	targets(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) ([]adapterMoveTargetsRow, error)
	jobs(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) ([]adapterMoveJobsRow, error)
	cancelTarget(ctx context.Context, targetTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (adapterMoveCancelTargetRow, error)
	insertConvergeJob(ctx context.Context, jobID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetTargetID string, moveID string, authorityPrincipalID string, nextGeneration int64, targetTargetID2 string, stamp time.Time, stamp2 time.Time) (int64, error)
	activateCanceledTarget(ctx context.Context, nextGeneration int64, jobID string, targetTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, generation int64) (int64, error)
	restoreAdapter(ctx context.Context, authorityPrincipalID string, moveAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error)
	deleteClaims(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error)
	cancel(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error)
	deleteKeys(ctx context.Context, moveID string, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (int64, error)
	updatePendingTarget(ctx context.Context, targetDestinationKind string, targetDestinationOwner string, targetDestinationName string, targetDestinationEnvironment string, targetDestinationScope string, targetRepositoryID int64, targetVisibility string, selectedJSON []byte, targetNamePrefix string, moveID string, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (int64, error)
	insertKey(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetID string, keyID string) (int64, error)
	replaceOriginCollisions(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, moveAdapterID string, origin string, chainOrg2 domain.OrgID, chainProject2 domain.ProjectID, moveID string, origin2 string) (int, error)
	updatePendingOrigin(ctx context.Context, origin string, pendingCredential []byte, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error)
	resetDestinations(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error)
	pendingKeyIDs(ctx context.Context, moveID string, targetTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) ([]string, error)
	activate(ctx context.Context, authorityPrincipalID string, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error)
	resumeTarget(ctx context.Context, targetTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (int64, error)
	insertActivateJob(ctx context.Context, jobID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetTargetID string, moveID string, authorityPrincipalID string, nextGeneration int64, targetTargetID2 string, stamp time.Time, stamp2 time.Time) (int64, error)
	markResumingTarget(ctx context.Context, nextGeneration int64, jobID string, targetTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, generation int64) (int64, error)
	updateAuthority(ctx context.Context, authorityPrincipalID string, moveAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error)
	beginOriginAdapter(ctx context.Context, stamp time.Time, mutationAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (adapterMoveBeginOriginAdapterRow, error)
	beginOriginCollisions(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, mutationAdapterID string, mutationOrigin string, chainOrg2 domain.OrgID, chainProject2 domain.ProjectID, mutationOrigin2 string) (int, error)
	beginOriginTargets(ctx context.Context, mutationAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) ([]adapterMoveBeginOriginTargetsRow, error)
	insertOrigin(ctx context.Context, mutationMoveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, mutationAdapterID string, mutationOrigin string, mutationPendingCredentialCiphertext []byte, mutationAuthorityPrincipalID string, moveState string, mutationKeepRemote bool, stamp time.Time) (int64, error)
	insertTarget(ctx context.Context, mutationMoveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetID string, targetKind string, targetOwner string, targetName string, targetDestinationEnvironment string, targetDestinationScope string, targetRepositoryID int64, targetVisibility string, selectedJSON []byte, targetPrefix string, orphanedNames string) (int64, error)
	targetKeyIDs(ctx context.Context, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) ([]string, error)
	supersedeJob(ctx context.Context, stamp time.Time, targetActiveJob string, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (int64, error)
	releaseLedger(ctx context.Context, stamp time.Time, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (int64, error)
	insertJob(ctx context.Context, jobID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetID string, jobKind string, mutationMoveID string, mutationAuthorityPrincipalID string, generation int64, targetID2 string, stamp time.Time, stamp2 time.Time) (int64, error)
	markMovingTarget(ctx context.Context, generation int64, jobID string, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetGeneration int64) (int64, error)
	markAdapterMoving(ctx context.Context, mutationAuthorityPrincipalID string, mutationAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error)
	beginTarget(ctx context.Context, stamp time.Time, mutationTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (adapterMoveBeginTargetRow, error)
	insertTargetMove(ctx context.Context, mutationMoveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, currentAdapterID string, mutationTargetID string, mutationAuthorityPrincipalID string, moveState string, mutationKeepRemote bool, stamp time.Time) (int64, error)
	setActiveAuthority(ctx context.Context, mutationAuthorityPrincipalID string, currentAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error)
	configuredCollision(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, targetID string, origin string, targetDestinationKind string, targetDestinationOwner string, targetDestinationName string, targetDestinationEnvironment string, targetDestinationScope string, pendingEffective string, sentinelName string, pendingSurface string, pendingEffective2 string) (int, error)
	insertClaim(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetID string, keyID string, origin string, targetDestinationKind string, targetDestinationOwner string, targetDestinationName string, targetDestinationEnvironment string, targetDestinationScope string, pendingSurface string, pendingEffective string, normalizedName string) (int64, error)
	aWSConfiguredNames(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, targetID string, origin string, targetDestinationOwner string) ([]adapterMoveAWSConfiguredNamesRow, error)
	aWSPendingNames(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, origin string, targetDestinationOwner string, targetID string) ([]adapterMoveAWSPendingNamesRow, error)
	insertAWSClaim(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetID string, keyID string, origin string, targetDestinationKind string, targetDestinationOwner string, targetDestinationName string, targetDestinationEnvironment string, surface string, claimEffectiveName string, normalizedName string) (int64, error)
	flags(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, targetAdapterID string, targetID string) (adapterMoveFlagsRow, error)
}

type sqliteAdapterMoveQueries struct {
	queries        *sqlitegen.Queries
	mapConstraints bool
}
type pgAdapterMoveQueries struct {
	queries        *pggen.Queries
	mapConstraints bool
}

func (d sqliteAdoptDB) adapterMoveQueries() adapterMoveQueries {
	return sqliteAdapterMoveQueries{queries: sqlitegen.New(d.db), mapConstraints: true}
}
func (d pgAdoptDB) adapterMoveQueries() adapterMoveQueries {
	return pgAdapterMoveQueries{queries: pggen.New(d.db), mapConstraints: true}
}
func (d sqliteAdapterTx) adapterMoveQueries() adapterMoveQueries {
	return sqliteAdapterMoveQueries{queries: sqlitegen.New(d.tx)}
}
func (d pgAdapterTx) adapterMoveQueries() adapterMoveQueries {
	return pgAdapterMoveQueries{queries: pggen.New(d.tx)}
}

// Request repositories map constraints; runtime transactions retain the driver error.
func adapterMoveExecError(err error, mapConstraints bool) error {
	if mapConstraints {
		return constraint(err)
	}
	return err
}

func (q sqliteAdapterMoveQueries) get(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (adapterMoveGetRow, error) {
	row, err := q.queries.AdapterMoveGet(ctx, sqlitegen.AdapterMoveGetParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	if err != nil {
		return adapterMoveGetRow{}, err
	}
	created := row.CreatedAt
	if err := normalizeStoredTimes(&created); err != nil {
		return adapterMoveGetRow{}, err
	}
	return adapterMoveGetRow{ID: row.ID, AdapterID: row.AdapterID, Kind: row.Kind, State: row.State, Keep: row.KeepRemote != 0, PendingOrigin: row.PendingOrigin, Created: created, AuthorityPrincipalID: row.AuthorityPrincipalID}, nil
}

func (q sqliteAdapterMoveQueries) getLocked(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (adapterMoveGetRow, error) {
	row, err := q.queries.AdapterMoveGetLocked(ctx, sqlitegen.AdapterMoveGetLockedParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	if err != nil {
		return adapterMoveGetRow{}, err
	}
	created := row.CreatedAt
	if err := normalizeStoredTimes(&created); err != nil {
		return adapterMoveGetRow{}, err
	}
	return adapterMoveGetRow{ID: row.ID, AdapterID: row.AdapterID, Kind: row.Kind, State: row.State, Keep: row.KeepRemote != 0, PendingOrigin: row.PendingOrigin, Created: created, AuthorityPrincipalID: row.AuthorityPrincipalID}, nil
}

func (q sqliteAdapterMoveQueries) targets(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) ([]adapterMoveTargetsRow, error) {
	row, err := q.queries.AdapterMoveTargets(ctx, sqlitegen.AdapterMoveTargetsParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	if err != nil {
		return nil, err
	}
	return mapRows(row, func(row sqlitegen.AdapterMoveTargetsRow) (adapterMoveTargetsRow, error) {
		return adapterMoveTargetsRow{TargetID: row.TargetID, EnvironmentID: row.EnvironmentID, DestinationKind: row.DestinationKind, DestinationOwner: row.DestinationOwner, DestinationName: row.DestinationName, DestinationEnvironment: row.DestinationEnvironment, DestinationScope: row.DestinationScope, DestinationID: row.DestinationID, RepositoryID: row.RepositoryID, Visibility: row.Visibility, SelectedJSON: []byte(row.SelectedRepositoryIds), NamePrefix: row.NamePrefix, OrphanJSON: []byte(row.OrphanedNames)}, nil
	})
}

func (q sqliteAdapterMoveQueries) jobs(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) ([]adapterMoveJobsRow, error) {
	row, err := q.queries.AdapterMoveJobs(ctx, sqlitegen.AdapterMoveJobsParams{MoveID: sql.NullString{String: moveID, Valid: true}, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	if err != nil {
		return nil, err
	}
	return mapRows(row, func(row sqlitegen.AdapterMoveJobsRow) (adapterMoveJobsRow, error) {
		return adapterMoveJobsRow{ID: row.ID, TargetID: row.TargetID, Kind: row.Kind, State: row.State}, nil
	})
}

func (q sqliteAdapterMoveQueries) cancelTarget(ctx context.Context, targetTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (adapterMoveCancelTargetRow, error) {
	row, err := q.queries.AdapterMoveCancelTarget(ctx, sqlitegen.AdapterMoveCancelTargetParams{TargetTargetID: targetTargetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID})
	if err != nil {
		return adapterMoveCancelTargetRow{}, err
	}
	return adapterMoveCancelTargetRow{Generation: row.Generation, ProviderBusy: int(row.ProviderBusy)}, nil
}

func (q sqliteAdapterMoveQueries) insertConvergeJob(ctx context.Context, jobID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetTargetID string, moveID string, authorityPrincipalID string, nextGeneration int64, targetTargetID2 string, stamp time.Time, stamp2 time.Time) (int64, error) {
	n, err := q.queries.AdapterMoveInsertConvergeJob(ctx, sqlitegen.AdapterMoveInsertConvergeJobParams{JobID: jobID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, TargetTargetID: targetTargetID, MoveID: sql.NullString{String: moveID, Valid: true}, AuthorityPrincipalID: authorityPrincipalID, NextGeneration: nextGeneration, TargetTargetId2: targetTargetID2, NextAttemptAt: fixedStamp(stamp), CreatedAt: fixedStamp(stamp2)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) activateCanceledTarget(ctx context.Context, nextGeneration int64, jobID string, targetTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, generation int64) (int64, error) {
	n, err := q.queries.AdapterMoveActivateCanceledTarget(ctx, sqlitegen.AdapterMoveActivateCanceledTargetParams{NextGeneration: nextGeneration, JobID: sql.NullString{String: jobID, Valid: true}, TargetTargetID: targetTargetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, Generation: generation})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) restoreAdapter(ctx context.Context, authorityPrincipalID string, moveAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveRestoreAdapter(ctx, sqlitegen.AdapterMoveRestoreAdapterParams{AuthorityPrincipalID: authorityPrincipalID, MoveAdapterID: moveAdapterID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) deleteClaims(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveDeleteClaims(ctx, sqlitegen.AdapterMoveDeleteClaimsParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) cancel(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveCancel(ctx, sqlitegen.AdapterMoveCancelParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) deleteKeys(ctx context.Context, moveID string, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (int64, error) {
	n, err := q.queries.AdapterMoveDeleteKeys(ctx, sqlitegen.AdapterMoveDeleteKeysParams{MoveID: moveID, TargetID: targetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) updatePendingTarget(ctx context.Context, targetDestinationKind string, targetDestinationOwner string, targetDestinationName string, targetDestinationEnvironment string, targetDestinationScope string, targetRepositoryID int64, targetVisibility string, selectedJSON []byte, targetNamePrefix string, moveID string, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (int64, error) {
	n, err := q.queries.AdapterMoveUpdatePendingTarget(ctx, sqlitegen.AdapterMoveUpdatePendingTargetParams{TargetDestinationKind: targetDestinationKind, TargetDestinationOwner: targetDestinationOwner, TargetDestinationName: targetDestinationName, TargetDestinationEnvironment: targetDestinationEnvironment, TargetDestinationScope: targetDestinationScope, TargetRepositoryID: targetRepositoryID, TargetVisibility: targetVisibility, SelectedRepositoryIds: string(selectedJSON), TargetNamePrefix: targetNamePrefix, MoveID: moveID, TargetID: targetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) insertKey(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetID string, keyID string) (int64, error) {
	n, err := q.queries.AdapterMoveInsertKey(ctx, sqlitegen.AdapterMoveInsertKeyParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, TargetID: targetID, KeyID: keyID})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) replaceOriginCollisions(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, moveAdapterID string, origin string, chainOrg2 domain.OrgID, chainProject2 domain.ProjectID, moveID string, origin2 string) (int, error) {
	row, err := q.queries.AdapterMoveReplaceOriginCollisions(ctx, sqlitegen.AdapterMoveReplaceOriginCollisionsParams{ChainOrg: string(chainOrg), ChainProject: string(chainProject), MoveAdapterID: moveAdapterID, Origin: origin, ChainOrg2: string(chainOrg2), ChainProject2: string(chainProject2), MoveID: moveID, Origin2: sql.NullString{String: origin2, Valid: true}})
	if err != nil {
		return 0, err
	}
	return int(row), nil
}

func (q sqliteAdapterMoveQueries) updatePendingOrigin(ctx context.Context, origin string, pendingCredential []byte, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveUpdatePendingOrigin(ctx, sqlitegen.AdapterMoveUpdatePendingOriginParams{Origin: sql.NullString{String: origin, Valid: true}, PendingCredential: pendingCredential, MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) resetDestinations(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveResetDestinations(ctx, sqlitegen.AdapterMoveResetDestinationsParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) pendingKeyIDs(ctx context.Context, moveID string, targetTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) ([]string, error) {
	row, err := q.queries.AdapterMovePendingKeyIDs(ctx, sqlitegen.AdapterMovePendingKeyIDsParams{MoveID: moveID, TargetTargetID: targetTargetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID})
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (q sqliteAdapterMoveQueries) activate(ctx context.Context, authorityPrincipalID string, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveActivate(ctx, sqlitegen.AdapterMoveActivateParams{AuthorityPrincipalID: authorityPrincipalID, MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) resumeTarget(ctx context.Context, targetTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (int64, error) {
	row, err := q.queries.AdapterMoveResumeTarget(ctx, sqlitegen.AdapterMoveResumeTargetParams{TargetTargetID: targetTargetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID})
	if err != nil {
		return 0, err
	}
	return row, nil
}

func (q sqliteAdapterMoveQueries) insertActivateJob(ctx context.Context, jobID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetTargetID string, moveID string, authorityPrincipalID string, nextGeneration int64, targetTargetID2 string, stamp time.Time, stamp2 time.Time) (int64, error) {
	n, err := q.queries.AdapterMoveInsertActivateJob(ctx, sqlitegen.AdapterMoveInsertActivateJobParams{JobID: jobID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, TargetTargetID: targetTargetID, MoveID: sql.NullString{String: moveID, Valid: true}, AuthorityPrincipalID: authorityPrincipalID, NextGeneration: nextGeneration, TargetTargetId2: targetTargetID2, NextAttemptAt: fixedStamp(stamp), CreatedAt: fixedStamp(stamp2)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) markResumingTarget(ctx context.Context, nextGeneration int64, jobID string, targetTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, generation int64) (int64, error) {
	n, err := q.queries.AdapterMoveMarkResumingTarget(ctx, sqlitegen.AdapterMoveMarkResumingTargetParams{NextGeneration: nextGeneration, JobID: sql.NullString{String: jobID, Valid: true}, TargetTargetID: targetTargetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, Generation: generation})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) updateAuthority(ctx context.Context, authorityPrincipalID string, moveAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveUpdateAuthority(ctx, sqlitegen.AdapterMoveUpdateAuthorityParams{AuthorityPrincipalID: authorityPrincipalID, MoveAdapterID: moveAdapterID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) beginOriginAdapter(ctx context.Context, stamp time.Time, mutationAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (adapterMoveBeginOriginAdapterRow, error) {
	row, err := q.queries.AdapterMoveBeginOriginAdapter(ctx, sqlitegen.AdapterMoveBeginOriginAdapterParams{ObservedAt: sql.NullString{String: fixedStamp(stamp), Valid: true}, MutationAdapterID: mutationAdapterID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	if err != nil {
		return adapterMoveBeginOriginAdapterRow{}, err
	}
	return adapterMoveBeginOriginAdapterRow{CurrentOrigin: row.CurrentOrigin, ProviderBusy: int(row.ProviderBusy)}, nil
}

func (q sqliteAdapterMoveQueries) beginOriginCollisions(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, mutationAdapterID string, mutationOrigin string, chainOrg2 domain.OrgID, chainProject2 domain.ProjectID, mutationOrigin2 string) (int, error) {
	row, err := q.queries.AdapterMoveBeginOriginCollisions(ctx, sqlitegen.AdapterMoveBeginOriginCollisionsParams{ChainOrg: string(chainOrg), ChainProject: string(chainProject), MutationAdapterID: mutationAdapterID, MutationOrigin: mutationOrigin, ChainOrg2: string(chainOrg2), ChainProject2: string(chainProject2), MutationOrigin2: sql.NullString{String: mutationOrigin2, Valid: true}})
	if err != nil {
		return 0, err
	}
	return int(row), nil
}

func (q sqliteAdapterMoveQueries) beginOriginTargets(ctx context.Context, mutationAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) ([]adapterMoveBeginOriginTargetsRow, error) {
	row, err := q.queries.AdapterMoveBeginOriginTargets(ctx, sqlitegen.AdapterMoveBeginOriginTargetsParams{MutationAdapterID: mutationAdapterID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	if err != nil {
		return nil, err
	}
	return mapRows(row, func(row sqlitegen.AdapterMoveBeginOriginTargetsRow) (adapterMoveBeginOriginTargetsRow, error) {
		return adapterMoveBeginOriginTargetsRow{Id: row.ID, EnvironmentID: row.EnvironmentID, Kind: row.Kind, Owner: row.Owner, Name: row.Name, DestinationEnvironment: row.DestinationEnvironment, DestinationScope: row.DestinationScope, DestinationID: row.DestinationID, RepositoryID: row.RepositoryID, Visibility: row.Visibility, SelectedRaw: []byte(row.SelectedRepositoryIds), Prefix: row.Prefix, Generation: row.Generation, ActiveJob: row.ActiveJob, OrphanRaw: []byte(row.OrphanedNames)}, nil
	})
}

func (q sqliteAdapterMoveQueries) insertOrigin(ctx context.Context, mutationMoveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, mutationAdapterID string, mutationOrigin string, mutationPendingCredentialCiphertext []byte, mutationAuthorityPrincipalID string, moveState string, mutationKeepRemote bool, stamp time.Time) (int64, error) {
	n, err := q.queries.AdapterMoveInsertOrigin(ctx, sqlitegen.AdapterMoveInsertOriginParams{MutationMoveID: mutationMoveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), MutationAdapterID: mutationAdapterID, MutationOrigin: sql.NullString{String: mutationOrigin, Valid: true}, MutationPendingCredentialCiphertext: mutationPendingCredentialCiphertext, MutationAuthorityPrincipalID: mutationAuthorityPrincipalID, MoveState: moveState, KeepRemote: boolToInt(mutationKeepRemote), CreatedAt: fixedStamp(stamp)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) insertTarget(ctx context.Context, mutationMoveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetID string, targetKind string, targetOwner string, targetName string, targetDestinationEnvironment string, targetDestinationScope string, targetRepositoryID int64, targetVisibility string, selectedJSON []byte, targetPrefix string, orphanedNames string) (int64, error) {
	n, err := q.queries.AdapterMoveInsertTarget(ctx, sqlitegen.AdapterMoveInsertTargetParams{MutationMoveID: mutationMoveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, TargetID: targetID, TargetKind: targetKind, TargetOwner: targetOwner, TargetName: targetName, TargetDestinationEnvironment: targetDestinationEnvironment, TargetDestinationScope: targetDestinationScope, TargetRepositoryID: targetRepositoryID, TargetVisibility: targetVisibility, SelectedRepositoryIds: string(selectedJSON), TargetPrefix: targetPrefix, OrphanedNames: orphanedNames})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) targetKeyIDs(ctx context.Context, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) ([]string, error) {
	row, err := q.queries.AdapterMoveTargetKeyIDs(ctx, sqlitegen.AdapterMoveTargetKeyIDsParams{TargetID: targetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID})
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (q sqliteAdapterMoveQueries) supersedeJob(ctx context.Context, stamp time.Time, targetActiveJob string, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (int64, error) {
	n, err := q.queries.AdapterMoveSupersedeJob(ctx, sqlitegen.AdapterMoveSupersedeJobParams{FinishedAt: sql.NullString{String: fixedStamp(stamp), Valid: true}, TargetActiveJob: targetActiveJob, TargetID: targetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) releaseLedger(ctx context.Context, stamp time.Time, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (int64, error) {
	n, err := q.queries.AdapterMoveReleaseLedger(ctx, sqlitegen.AdapterMoveReleaseLedgerParams{UpdatedAt: fixedStamp(stamp), TargetID: targetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) insertJob(ctx context.Context, jobID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetID string, jobKind string, mutationMoveID string, mutationAuthorityPrincipalID string, generation int64, targetID2 string, stamp time.Time, stamp2 time.Time) (int64, error) {
	n, err := q.queries.AdapterMoveInsertJob(ctx, sqlitegen.AdapterMoveInsertJobParams{JobID: jobID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, TargetID: targetID, JobKind: jobKind, MutationMoveID: sql.NullString{String: mutationMoveID, Valid: true}, MutationAuthorityPrincipalID: mutationAuthorityPrincipalID, Generation: generation, TargetId2: targetID2, NextAttemptAt: fixedStamp(stamp), CreatedAt: fixedStamp(stamp2)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) markMovingTarget(ctx context.Context, generation int64, jobID string, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetGeneration int64) (int64, error) {
	n, err := q.queries.AdapterMoveMarkMovingTarget(ctx, sqlitegen.AdapterMoveMarkMovingTargetParams{Generation: generation, JobID: sql.NullString{String: jobID, Valid: true}, TargetID: targetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, TargetGeneration: targetGeneration})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) markAdapterMoving(ctx context.Context, mutationAuthorityPrincipalID string, mutationAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveMarkAdapterMoving(ctx, sqlitegen.AdapterMoveMarkAdapterMovingParams{MutationAuthorityPrincipalID: mutationAuthorityPrincipalID, MutationAdapterID: mutationAdapterID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) beginTarget(ctx context.Context, stamp time.Time, mutationTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (adapterMoveBeginTargetRow, error) {
	row, err := q.queries.AdapterMoveBeginTarget(ctx, sqlitegen.AdapterMoveBeginTargetParams{ObservedAt: sql.NullString{String: fixedStamp(stamp), Valid: true}, MutationTargetID: mutationTargetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	if err != nil {
		return adapterMoveBeginTargetRow{}, err
	}
	return adapterMoveBeginTargetRow{AdapterID: row.AdapterID, Origin: row.Origin, EnvironmentID: row.EnvironmentID, Kind: row.Kind, Owner: row.Owner, Name: row.Name, DestinationEnvironment: row.DestinationEnvironment, DestinationScope: row.DestinationScope, DestinationID: row.DestinationID, Prefix: row.Prefix, Generation: row.Generation, ActiveJob: row.ActiveJob, ProviderBusy: int(row.ProviderBusy), OrphanRaw: []byte(row.OrphanedNames)}, nil
}

func (q sqliteAdapterMoveQueries) insertTargetMove(ctx context.Context, mutationMoveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, currentAdapterID string, mutationTargetID string, mutationAuthorityPrincipalID string, moveState string, mutationKeepRemote bool, stamp time.Time) (int64, error) {
	n, err := q.queries.AdapterMoveInsertTargetMove(ctx, sqlitegen.AdapterMoveInsertTargetMoveParams{MutationMoveID: mutationMoveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), CurrentAdapterID: currentAdapterID, MutationTargetID: sql.NullString{String: mutationTargetID, Valid: true}, MutationAuthorityPrincipalID: mutationAuthorityPrincipalID, MoveState: moveState, KeepRemote: boolToInt(mutationKeepRemote), CreatedAt: fixedStamp(stamp)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) setActiveAuthority(ctx context.Context, mutationAuthorityPrincipalID string, currentAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveSetActiveAuthority(ctx, sqlitegen.AdapterMoveSetActiveAuthorityParams{MutationAuthorityPrincipalID: mutationAuthorityPrincipalID, CurrentAdapterID: currentAdapterID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) configuredCollision(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, targetID string, origin string, targetDestinationKind string, targetDestinationOwner string, targetDestinationName string, targetDestinationEnvironment string, targetDestinationScope string, pendingEffective string, sentinelName string, pendingSurface string, pendingEffective2 string) (int, error) {
	row, err := q.queries.AdapterMoveConfiguredCollision(ctx, sqlitegen.AdapterMoveConfiguredCollisionParams{ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetID: targetID, Origin: origin, TargetDestinationKind: targetDestinationKind, TargetDestinationOwner: targetDestinationOwner, TargetDestinationName: targetDestinationName, TargetDestinationEnvironment: targetDestinationEnvironment, TargetDestinationScope: targetDestinationScope, PendingEffective: pendingEffective, SentinelName: sentinelName, PendingSurface: pendingSurface, PendingEffective2: pendingEffective2})
	if err != nil {
		return 0, err
	}
	return int(row), nil
}

func (q sqliteAdapterMoveQueries) insertClaim(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetID string, keyID string, origin string, targetDestinationKind string, targetDestinationOwner string, targetDestinationName string, targetDestinationEnvironment string, targetDestinationScope string, pendingSurface string, pendingEffective string, normalizedName string) (int64, error) {
	n, err := q.queries.AdapterMoveInsertClaim(ctx, sqlitegen.AdapterMoveInsertClaimParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, TargetID: targetID, KeyID: sql.NullString{String: keyID, Valid: keyID != ""}, Origin: origin, TargetDestinationKind: targetDestinationKind, TargetDestinationOwner: targetDestinationOwner, TargetDestinationName: targetDestinationName, TargetDestinationEnvironment: targetDestinationEnvironment, TargetDestinationScope: targetDestinationScope, PendingSurface: pendingSurface, PendingEffective: pendingEffective, NormalizedName: normalizedName})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) aWSConfiguredNames(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, targetID string, origin string, targetDestinationOwner string) ([]adapterMoveAWSConfiguredNamesRow, error) {
	row, err := q.queries.AdapterMoveAWSConfiguredNames(ctx, sqlitegen.AdapterMoveAWSConfiguredNamesParams{ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetID: targetID, Origin: origin, TargetDestinationOwner: targetDestinationOwner})
	if err != nil {
		return nil, err
	}
	return mapRows(row, func(row sqlitegen.AdapterMoveAWSConfiguredNamesRow) (adapterMoveAWSConfiguredNamesRow, error) {
		return adapterMoveAWSConfiguredNamesRow{Kind: row.Kind, Name: row.Name, Prefix: row.Prefix, KeyName: row.KeyName}, nil
	})
}

func (q sqliteAdapterMoveQueries) aWSPendingNames(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, origin string, targetDestinationOwner string, targetID string) ([]adapterMoveAWSPendingNamesRow, error) {
	row, err := q.queries.AdapterMoveAWSPendingNames(ctx, sqlitegen.AdapterMoveAWSPendingNamesParams{ChainOrg: string(chainOrg), ChainProject: string(chainProject), Origin: origin, TargetDestinationOwner: targetDestinationOwner, TargetID: targetID})
	if err != nil {
		return nil, err
	}
	return mapRows(row, func(row sqlitegen.AdapterMoveAWSPendingNamesRow) (adapterMoveAWSPendingNamesRow, error) {
		return adapterMoveAWSPendingNamesRow{OtherTarget: row.OtherTarget, Effective: row.Effective}, nil
	})
}

func (q sqliteAdapterMoveQueries) insertAWSClaim(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetID string, keyID string, origin string, targetDestinationKind string, targetDestinationOwner string, targetDestinationName string, targetDestinationEnvironment string, surface string, claimEffectiveName string, normalizedName string) (int64, error) {
	n, err := q.queries.AdapterMoveInsertAWSClaim(ctx, sqlitegen.AdapterMoveInsertAWSClaimParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, TargetID: targetID, KeyID: sql.NullString{String: keyID, Valid: keyID != ""}, Origin: origin, TargetDestinationKind: targetDestinationKind, TargetDestinationOwner: targetDestinationOwner, TargetDestinationName: targetDestinationName, TargetDestinationEnvironment: targetDestinationEnvironment, Surface: surface, ClaimEffectiveName: claimEffectiveName, NormalizedName: normalizedName})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q sqliteAdapterMoveQueries) flags(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, targetAdapterID string, targetID string) (adapterMoveFlagsRow, error) {
	row, err := q.queries.AdapterMoveFlags(ctx, sqlitegen.AdapterMoveFlagsParams{ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetAdapterID: targetAdapterID, TargetID: targetID})
	if err != nil {
		return adapterMoveFlagsRow{}, err
	}
	return adapterMoveFlagsRow{Provider: row.Provider, Protected: row.VariableProtected != 0, Hidden: row.VariableHidden != 0, Expand: row.VariableExpand != 0}, nil
}

func (q pgAdapterMoveQueries) get(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (adapterMoveGetRow, error) {
	row, err := q.queries.AdapterMoveGet(ctx, pggen.AdapterMoveGetParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	if err != nil {
		return adapterMoveGetRow{}, err
	}
	created := pgStoredStamp(row.CreatedAt)
	if err := normalizeStoredTimes(&created); err != nil {
		return adapterMoveGetRow{}, err
	}
	return adapterMoveGetRow{ID: row.ID, AdapterID: row.AdapterID, Kind: row.Kind, State: row.State, Keep: row.KeepRemote, PendingOrigin: row.PendingOrigin, Created: created, AuthorityPrincipalID: row.AuthorityPrincipalID}, nil
}

func (q pgAdapterMoveQueries) getLocked(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (adapterMoveGetRow, error) {
	row, err := q.queries.AdapterMoveGetLocked(ctx, pggen.AdapterMoveGetLockedParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	if err != nil {
		return adapterMoveGetRow{}, err
	}
	created := pgStoredStamp(row.CreatedAt)
	if err := normalizeStoredTimes(&created); err != nil {
		return adapterMoveGetRow{}, err
	}
	return adapterMoveGetRow{ID: row.ID, AdapterID: row.AdapterID, Kind: row.Kind, State: row.State, Keep: row.KeepRemote, PendingOrigin: row.PendingOrigin, Created: created, AuthorityPrincipalID: row.AuthorityPrincipalID}, nil
}

func (q pgAdapterMoveQueries) targets(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) ([]adapterMoveTargetsRow, error) {
	row, err := q.queries.AdapterMoveTargets(ctx, pggen.AdapterMoveTargetsParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	if err != nil {
		return nil, err
	}
	return mapRows(row, func(row pggen.AdapterMoveTargetsRow) (adapterMoveTargetsRow, error) {
		return adapterMoveTargetsRow{TargetID: row.TargetID, EnvironmentID: row.EnvironmentID, DestinationKind: row.DestinationKind, DestinationOwner: row.DestinationOwner, DestinationName: row.DestinationName, DestinationEnvironment: row.DestinationEnvironment, DestinationScope: row.DestinationScope, DestinationID: row.DestinationID, RepositoryID: row.RepositoryID, Visibility: row.Visibility, SelectedJSON: row.SelectedRepositoryIds, NamePrefix: row.NamePrefix, OrphanJSON: row.OrphanedNames}, nil
	})
}

func (q pgAdapterMoveQueries) jobs(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) ([]adapterMoveJobsRow, error) {
	row, err := q.queries.AdapterMoveJobs(ctx, pggen.AdapterMoveJobsParams{MoveID: pgtype.Text{String: moveID, Valid: true}, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	if err != nil {
		return nil, err
	}
	return mapRows(row, func(row pggen.AdapterMoveJobsRow) (adapterMoveJobsRow, error) {
		return adapterMoveJobsRow{ID: row.ID, TargetID: row.TargetID, Kind: row.Kind, State: row.State}, nil
	})
}

func (q pgAdapterMoveQueries) cancelTarget(ctx context.Context, targetTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (adapterMoveCancelTargetRow, error) {
	row, err := q.queries.AdapterMoveCancelTarget(ctx, pggen.AdapterMoveCancelTargetParams{TargetTargetID: targetTargetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID})
	if err != nil {
		return adapterMoveCancelTargetRow{}, err
	}
	return adapterMoveCancelTargetRow{Generation: row.Generation, ProviderBusy: int(row.ProviderBusy)}, nil
}

func (q pgAdapterMoveQueries) insertConvergeJob(ctx context.Context, jobID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetTargetID string, moveID string, authorityPrincipalID string, nextGeneration int64, targetTargetID2 string, stamp time.Time, stamp2 time.Time) (int64, error) {
	n, err := q.queries.AdapterMoveInsertConvergeJob(ctx, pggen.AdapterMoveInsertConvergeJobParams{JobID: jobID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, TargetTargetID: targetTargetID, MoveID: pgtype.Text{String: moveID, Valid: true}, AuthorityPrincipalID: authorityPrincipalID, NextGeneration: nextGeneration, TargetTargetId2: targetTargetID2, NextAttemptAt: pgRequiredTime(stamp), CreatedAt: pgRequiredTime(stamp2)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) activateCanceledTarget(ctx context.Context, nextGeneration int64, jobID string, targetTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, generation int64) (int64, error) {
	n, err := q.queries.AdapterMoveActivateCanceledTarget(ctx, pggen.AdapterMoveActivateCanceledTargetParams{NextGeneration: nextGeneration, JobID: pgtype.Text{String: jobID, Valid: true}, TargetTargetID: targetTargetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, Generation: generation})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) restoreAdapter(ctx context.Context, authorityPrincipalID string, moveAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveRestoreAdapter(ctx, pggen.AdapterMoveRestoreAdapterParams{AuthorityPrincipalID: authorityPrincipalID, MoveAdapterID: moveAdapterID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) deleteClaims(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveDeleteClaims(ctx, pggen.AdapterMoveDeleteClaimsParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) cancel(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveCancel(ctx, pggen.AdapterMoveCancelParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) deleteKeys(ctx context.Context, moveID string, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (int64, error) {
	n, err := q.queries.AdapterMoveDeleteKeys(ctx, pggen.AdapterMoveDeleteKeysParams{MoveID: moveID, TargetID: targetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) updatePendingTarget(ctx context.Context, targetDestinationKind string, targetDestinationOwner string, targetDestinationName string, targetDestinationEnvironment string, targetDestinationScope string, targetRepositoryID int64, targetVisibility string, selectedJSON []byte, targetNamePrefix string, moveID string, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (int64, error) {
	n, err := q.queries.AdapterMoveUpdatePendingTarget(ctx, pggen.AdapterMoveUpdatePendingTargetParams{TargetDestinationKind: targetDestinationKind, TargetDestinationOwner: targetDestinationOwner, TargetDestinationName: targetDestinationName, TargetDestinationEnvironment: targetDestinationEnvironment, TargetDestinationScope: targetDestinationScope, TargetRepositoryID: targetRepositoryID, TargetVisibility: targetVisibility, SelectedRepositoryIds: selectedJSON, TargetNamePrefix: targetNamePrefix, MoveID: moveID, TargetID: targetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) insertKey(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetID string, keyID string) (int64, error) {
	n, err := q.queries.AdapterMoveInsertKey(ctx, pggen.AdapterMoveInsertKeyParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, TargetID: targetID, KeyID: keyID})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) replaceOriginCollisions(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, moveAdapterID string, origin string, chainOrg2 domain.OrgID, chainProject2 domain.ProjectID, moveID string, origin2 string) (int, error) {
	row, err := q.queries.AdapterMoveReplaceOriginCollisions(ctx, pggen.AdapterMoveReplaceOriginCollisionsParams{ChainOrg: string(chainOrg), ChainProject: string(chainProject), MoveAdapterID: moveAdapterID, Origin: origin, ChainOrg2: string(chainOrg2), ChainProject2: string(chainProject2), MoveID: moveID, Origin2: pgtype.Text{String: origin2, Valid: true}})
	if err != nil {
		return 0, err
	}
	return int(row), nil
}

func (q pgAdapterMoveQueries) updatePendingOrigin(ctx context.Context, origin string, pendingCredential []byte, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveUpdatePendingOrigin(ctx, pggen.AdapterMoveUpdatePendingOriginParams{Origin: pgtype.Text{String: origin, Valid: true}, PendingCredential: pendingCredential, MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) resetDestinations(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveResetDestinations(ctx, pggen.AdapterMoveResetDestinationsParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) pendingKeyIDs(ctx context.Context, moveID string, targetTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) ([]string, error) {
	row, err := q.queries.AdapterMovePendingKeyIDs(ctx, pggen.AdapterMovePendingKeyIDsParams{MoveID: moveID, TargetTargetID: targetTargetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID})
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (q pgAdapterMoveQueries) activate(ctx context.Context, authorityPrincipalID string, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveActivate(ctx, pggen.AdapterMoveActivateParams{AuthorityPrincipalID: authorityPrincipalID, MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) resumeTarget(ctx context.Context, targetTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (int64, error) {
	row, err := q.queries.AdapterMoveResumeTarget(ctx, pggen.AdapterMoveResumeTargetParams{TargetTargetID: targetTargetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID})
	if err != nil {
		return 0, err
	}
	return row, nil
}

func (q pgAdapterMoveQueries) insertActivateJob(ctx context.Context, jobID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetTargetID string, moveID string, authorityPrincipalID string, nextGeneration int64, targetTargetID2 string, stamp time.Time, stamp2 time.Time) (int64, error) {
	n, err := q.queries.AdapterMoveInsertActivateJob(ctx, pggen.AdapterMoveInsertActivateJobParams{JobID: jobID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, TargetTargetID: targetTargetID, MoveID: pgtype.Text{String: moveID, Valid: true}, AuthorityPrincipalID: authorityPrincipalID, NextGeneration: nextGeneration, TargetTargetId2: targetTargetID2, NextAttemptAt: pgRequiredTime(stamp), CreatedAt: pgRequiredTime(stamp2)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) markResumingTarget(ctx context.Context, nextGeneration int64, jobID string, targetTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, generation int64) (int64, error) {
	n, err := q.queries.AdapterMoveMarkResumingTarget(ctx, pggen.AdapterMoveMarkResumingTargetParams{NextGeneration: nextGeneration, JobID: pgtype.Text{String: jobID, Valid: true}, TargetTargetID: targetTargetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, Generation: generation})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) updateAuthority(ctx context.Context, authorityPrincipalID string, moveAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveUpdateAuthority(ctx, pggen.AdapterMoveUpdateAuthorityParams{AuthorityPrincipalID: authorityPrincipalID, MoveAdapterID: moveAdapterID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) beginOriginAdapter(ctx context.Context, stamp time.Time, mutationAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (adapterMoveBeginOriginAdapterRow, error) {
	row, err := q.queries.AdapterMoveBeginOriginAdapter(ctx, pggen.AdapterMoveBeginOriginAdapterParams{ObservedAt: pgRequiredTime(stamp), MutationAdapterID: mutationAdapterID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	if err != nil {
		return adapterMoveBeginOriginAdapterRow{}, err
	}
	return adapterMoveBeginOriginAdapterRow{CurrentOrigin: row.CurrentOrigin, ProviderBusy: int(row.ProviderBusy)}, nil
}

func (q pgAdapterMoveQueries) beginOriginCollisions(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, mutationAdapterID string, mutationOrigin string, chainOrg2 domain.OrgID, chainProject2 domain.ProjectID, mutationOrigin2 string) (int, error) {
	row, err := q.queries.AdapterMoveBeginOriginCollisions(ctx, pggen.AdapterMoveBeginOriginCollisionsParams{ChainOrg: string(chainOrg), ChainProject: string(chainProject), MutationAdapterID: mutationAdapterID, MutationOrigin: mutationOrigin, ChainOrg2: string(chainOrg2), ChainProject2: string(chainProject2), MutationOrigin2: pgtype.Text{String: mutationOrigin2, Valid: true}})
	if err != nil {
		return 0, err
	}
	return int(row), nil
}

func (q pgAdapterMoveQueries) beginOriginTargets(ctx context.Context, mutationAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) ([]adapterMoveBeginOriginTargetsRow, error) {
	row, err := q.queries.AdapterMoveBeginOriginTargets(ctx, pggen.AdapterMoveBeginOriginTargetsParams{MutationAdapterID: mutationAdapterID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	if err != nil {
		return nil, err
	}
	return mapRows(row, func(row pggen.AdapterMoveBeginOriginTargetsRow) (adapterMoveBeginOriginTargetsRow, error) {
		return adapterMoveBeginOriginTargetsRow{Id: row.ID, EnvironmentID: row.EnvironmentID, Kind: row.Kind, Owner: row.Owner, Name: row.Name, DestinationEnvironment: row.DestinationEnvironment, DestinationScope: row.DestinationScope, DestinationID: row.DestinationID, RepositoryID: row.RepositoryID, Visibility: row.Visibility, SelectedRaw: row.SelectedRepositoryIds, Prefix: row.Prefix, Generation: row.Generation, ActiveJob: row.ActiveJob, OrphanRaw: row.OrphanedNames}, nil
	})
}

func (q pgAdapterMoveQueries) insertOrigin(ctx context.Context, mutationMoveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, mutationAdapterID string, mutationOrigin string, mutationPendingCredentialCiphertext []byte, mutationAuthorityPrincipalID string, moveState string, mutationKeepRemote bool, stamp time.Time) (int64, error) {
	n, err := q.queries.AdapterMoveInsertOrigin(ctx, pggen.AdapterMoveInsertOriginParams{MutationMoveID: mutationMoveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), MutationAdapterID: mutationAdapterID, MutationOrigin: pgtype.Text{String: mutationOrigin, Valid: true}, MutationPendingCredentialCiphertext: mutationPendingCredentialCiphertext, MutationAuthorityPrincipalID: mutationAuthorityPrincipalID, MoveState: moveState, KeepRemote: mutationKeepRemote, CreatedAt: pgRequiredTime(stamp)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) insertTarget(ctx context.Context, mutationMoveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetID string, targetKind string, targetOwner string, targetName string, targetDestinationEnvironment string, targetDestinationScope string, targetRepositoryID int64, targetVisibility string, selectedJSON []byte, targetPrefix string, orphanedNames string) (int64, error) {
	n, err := q.queries.AdapterMoveInsertTarget(ctx, pggen.AdapterMoveInsertTargetParams{MutationMoveID: mutationMoveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, TargetID: targetID, TargetKind: targetKind, TargetOwner: targetOwner, TargetName: targetName, TargetDestinationEnvironment: targetDestinationEnvironment, TargetDestinationScope: targetDestinationScope, TargetRepositoryID: targetRepositoryID, TargetVisibility: targetVisibility, SelectedRepositoryIds: selectedJSON, TargetPrefix: targetPrefix, OrphanedNames: []byte(orphanedNames)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) targetKeyIDs(ctx context.Context, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) ([]string, error) {
	row, err := q.queries.AdapterMoveTargetKeyIDs(ctx, pggen.AdapterMoveTargetKeyIDsParams{TargetID: targetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID})
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (q pgAdapterMoveQueries) supersedeJob(ctx context.Context, stamp time.Time, targetActiveJob string, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (int64, error) {
	n, err := q.queries.AdapterMoveSupersedeJob(ctx, pggen.AdapterMoveSupersedeJobParams{FinishedAt: pgRequiredTime(stamp), TargetActiveJob: targetActiveJob, TargetID: targetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) releaseLedger(ctx context.Context, stamp time.Time, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string) (int64, error) {
	n, err := q.queries.AdapterMoveReleaseLedger(ctx, pggen.AdapterMoveReleaseLedgerParams{UpdatedAt: pgRequiredTime(stamp), TargetID: targetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) insertJob(ctx context.Context, jobID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetID string, jobKind string, mutationMoveID string, mutationAuthorityPrincipalID string, generation int64, targetID2 string, stamp time.Time, stamp2 time.Time) (int64, error) {
	n, err := q.queries.AdapterMoveInsertJob(ctx, pggen.AdapterMoveInsertJobParams{JobID: jobID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, TargetID: targetID, JobKind: jobKind, MutationMoveID: pgtype.Text{String: mutationMoveID, Valid: true}, MutationAuthorityPrincipalID: mutationAuthorityPrincipalID, Generation: generation, TargetId2: targetID2, NextAttemptAt: pgRequiredTime(stamp), CreatedAt: pgRequiredTime(stamp2)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) markMovingTarget(ctx context.Context, generation int64, jobID string, targetID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetGeneration int64) (int64, error) {
	n, err := q.queries.AdapterMoveMarkMovingTarget(ctx, pggen.AdapterMoveMarkMovingTargetParams{Generation: generation, JobID: pgtype.Text{String: jobID, Valid: true}, TargetID: targetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, TargetGeneration: targetGeneration})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) markAdapterMoving(ctx context.Context, mutationAuthorityPrincipalID string, mutationAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveMarkAdapterMoving(ctx, pggen.AdapterMoveMarkAdapterMovingParams{MutationAuthorityPrincipalID: mutationAuthorityPrincipalID, MutationAdapterID: mutationAdapterID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) beginTarget(ctx context.Context, stamp time.Time, mutationTargetID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (adapterMoveBeginTargetRow, error) {
	row, err := q.queries.AdapterMoveBeginTarget(ctx, pggen.AdapterMoveBeginTargetParams{ObservedAt: pgRequiredTime(stamp), MutationTargetID: mutationTargetID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	if err != nil {
		return adapterMoveBeginTargetRow{}, err
	}
	return adapterMoveBeginTargetRow{AdapterID: row.AdapterID, Origin: row.Origin, EnvironmentID: row.EnvironmentID, Kind: row.Kind, Owner: row.Owner, Name: row.Name, DestinationEnvironment: row.DestinationEnvironment, DestinationScope: row.DestinationScope, DestinationID: row.DestinationID, Prefix: row.Prefix, Generation: row.Generation, ActiveJob: row.ActiveJob, ProviderBusy: int(row.ProviderBusy), OrphanRaw: row.OrphanedNames}, nil
}

func (q pgAdapterMoveQueries) insertTargetMove(ctx context.Context, mutationMoveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, currentAdapterID string, mutationTargetID string, mutationAuthorityPrincipalID string, moveState string, mutationKeepRemote bool, stamp time.Time) (int64, error) {
	n, err := q.queries.AdapterMoveInsertTargetMove(ctx, pggen.AdapterMoveInsertTargetMoveParams{MutationMoveID: mutationMoveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), CurrentAdapterID: currentAdapterID, MutationTargetID: pgtype.Text{String: mutationTargetID, Valid: true}, MutationAuthorityPrincipalID: mutationAuthorityPrincipalID, MoveState: moveState, KeepRemote: mutationKeepRemote, CreatedAt: pgRequiredTime(stamp)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) setActiveAuthority(ctx context.Context, mutationAuthorityPrincipalID string, currentAdapterID string, chainOrg domain.OrgID, chainProject domain.ProjectID) (int64, error) {
	n, err := q.queries.AdapterMoveSetActiveAuthority(ctx, pggen.AdapterMoveSetActiveAuthorityParams{MutationAuthorityPrincipalID: mutationAuthorityPrincipalID, CurrentAdapterID: currentAdapterID, ChainOrg: string(chainOrg), ChainProject: string(chainProject)})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) configuredCollision(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, targetID string, origin string, targetDestinationKind string, targetDestinationOwner string, targetDestinationName string, targetDestinationEnvironment string, targetDestinationScope string, pendingEffective string, sentinelName string, pendingSurface string, pendingEffective2 string) (int, error) {
	row, err := q.queries.AdapterMoveConfiguredCollision(ctx, pggen.AdapterMoveConfiguredCollisionParams{ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetID: targetID, Origin: origin, TargetDestinationKind: targetDestinationKind, TargetDestinationOwner: targetDestinationOwner, TargetDestinationName: targetDestinationName, TargetDestinationEnvironment: targetDestinationEnvironment, TargetDestinationScope: targetDestinationScope, PendingEffective: pendingEffective, SentinelName: sentinelName, PendingSurface: pendingSurface, PendingEffective2: pendingEffective2})
	if err != nil {
		return 0, err
	}
	return int(row), nil
}

func (q pgAdapterMoveQueries) insertClaim(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetID string, keyID string, origin string, targetDestinationKind string, targetDestinationOwner string, targetDestinationName string, targetDestinationEnvironment string, targetDestinationScope string, pendingSurface string, pendingEffective string, normalizedName string) (int64, error) {
	n, err := q.queries.AdapterMoveInsertClaim(ctx, pggen.AdapterMoveInsertClaimParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, TargetID: targetID, KeyID: pgtype.Text{String: keyID, Valid: keyID != ""}, Origin: origin, TargetDestinationKind: targetDestinationKind, TargetDestinationOwner: targetDestinationOwner, TargetDestinationName: targetDestinationName, TargetDestinationEnvironment: targetDestinationEnvironment, TargetDestinationScope: targetDestinationScope, PendingSurface: pendingSurface, PendingEffective: pendingEffective, NormalizedName: normalizedName})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) aWSConfiguredNames(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, targetID string, origin string, targetDestinationOwner string) ([]adapterMoveAWSConfiguredNamesRow, error) {
	row, err := q.queries.AdapterMoveAWSConfiguredNames(ctx, pggen.AdapterMoveAWSConfiguredNamesParams{ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetID: targetID, Origin: origin, TargetDestinationOwner: targetDestinationOwner})
	if err != nil {
		return nil, err
	}
	return mapRows(row, func(row pggen.AdapterMoveAWSConfiguredNamesRow) (adapterMoveAWSConfiguredNamesRow, error) {
		return adapterMoveAWSConfiguredNamesRow{Kind: row.Kind, Name: row.Name, Prefix: row.Prefix, KeyName: row.KeyName}, nil
	})
}

func (q pgAdapterMoveQueries) aWSPendingNames(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, origin string, targetDestinationOwner string, targetID string) ([]adapterMoveAWSPendingNamesRow, error) {
	row, err := q.queries.AdapterMoveAWSPendingNames(ctx, pggen.AdapterMoveAWSPendingNamesParams{ChainOrg: string(chainOrg), ChainProject: string(chainProject), Origin: origin, TargetDestinationOwner: targetDestinationOwner, TargetID: targetID})
	if err != nil {
		return nil, err
	}
	return mapRows(row, func(row pggen.AdapterMoveAWSPendingNamesRow) (adapterMoveAWSPendingNamesRow, error) {
		return adapterMoveAWSPendingNamesRow{OtherTarget: row.OtherTarget, Effective: row.Effective}, nil
	})
}

func (q pgAdapterMoveQueries) insertAWSClaim(ctx context.Context, moveID string, chainOrg domain.OrgID, chainProject domain.ProjectID, targetEnvironmentID string, targetID string, keyID string, origin string, targetDestinationKind string, targetDestinationOwner string, targetDestinationName string, targetDestinationEnvironment string, surface string, claimEffectiveName string, normalizedName string) (int64, error) {
	n, err := q.queries.AdapterMoveInsertAWSClaim(ctx, pggen.AdapterMoveInsertAWSClaimParams{MoveID: moveID, ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetEnvironmentID: targetEnvironmentID, TargetID: targetID, KeyID: pgtype.Text{String: keyID, Valid: keyID != ""}, Origin: origin, TargetDestinationKind: targetDestinationKind, TargetDestinationOwner: targetDestinationOwner, TargetDestinationName: targetDestinationName, TargetDestinationEnvironment: targetDestinationEnvironment, Surface: surface, ClaimEffectiveName: claimEffectiveName, NormalizedName: normalizedName})
	return n, adapterMoveExecError(err, q.mapConstraints)
}

func (q pgAdapterMoveQueries) flags(ctx context.Context, chainOrg domain.OrgID, chainProject domain.ProjectID, targetAdapterID string, targetID string) (adapterMoveFlagsRow, error) {
	row, err := q.queries.AdapterMoveFlags(ctx, pggen.AdapterMoveFlagsParams{ChainOrg: string(chainOrg), ChainProject: string(chainProject), TargetAdapterID: targetAdapterID, TargetID: targetID})
	if err != nil {
		return adapterMoveFlagsRow{}, err
	}
	return adapterMoveFlagsRow{Provider: row.Provider, Protected: row.VariableProtected, Hidden: row.VariableHidden, Expand: row.VariableExpand}, nil
}
