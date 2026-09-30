package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
)

type adapterRuntimeQueries interface {
	adapterWorkerLoadExecutionQuery(ctx context.Context, jobID string, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, leaseOwner string) (adapterWorkerLoadExecutionQueryRow, error)
	adapterWorkerLoadExecutionLedgerQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) ([]adapterWorkerLoadExecutionLedgerQueryRow, error)
	adapterWorkerLoadExecutionSnapshotQuery(ctx context.Context, chainOrg string, chainProject string, chainEnv string) (adapterWorkerLoadExecutionSnapshotQueryRow, error)
	adapterWorkerLoadExecutionEntryQuery(ctx context.Context, targetID string, snapshotID string, chainOrg string, chainProject string, chainEnv string) ([]adapterWorkerLoadExecutionEntryQueryRow, error)
	adapterWorkerLoadActivationQuery(ctx context.Context, jobID string, routeMoveID string, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, leaseOwner string) (adapterWorkerLoadActivationQueryRow, error)
	adapterWorkerTryEnqueueLookup(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, now time.Time) (int64, error)
	adapterWorkerTryEnqueueExistsQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error)
	adapterWorkerTryEnqueueDepthQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error)
	adapterWorkerTryEnqueuePreviousQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (string, error)
	adapterWorkerTryEnqueueSupersede(ctx context.Context, at time.Time, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error)
	adapterWorkerTryEnqueueInsert(ctx context.Context, jobID string, chainOrg string, chainProject string, chainEnv string, targetID string, kind string, authorityPrincipal string, generation int64, at time.Time) (int64, error)
	adapterWorkerTryEnqueueUpdate(ctx context.Context, generation int64, state string, jobID string, targetID string, chainOrg string, chainProject string, chainEnv string, expectedGeneration int64, at time.Time) (int64, error)
	adapterWorkerClaimDueSelectQuery(ctx context.Context, now time.Time) (adapterWorkerClaimDueSelectQueryRow, error)
	adapterWorkerClaimDueUpdate(ctx context.Context, attempt int64, worker string, leaseUntil time.Time, jobID string) (int64, error)
	adapterWorkerCloseIndeterminateEffectsQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, at time.Time) ([]adapterWorkerCloseIndeterminateEffectsQueryRow, error)
	adapterWorkerCloseIndeterminateEffectsUpdate(ctx context.Context, outcomeAuditID string, at time.Time, effectID string, chainOrg string, chainProject string, chainEnv string, targetID string) (int64, error)
	adapterWorkerCloseIndeterminateEffectsRelease(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, effectID string) (int64, error)
	adapterWorkerGateQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, jobID string, leaseOwner string, now time.Time) (int64, error)
	adapterWorkerReservePendingQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, surface string, normalized string) (int64, error)
	adapterWorkerReserveSelectQuery(ctx context.Context, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalized string) (string, error)
	adapterWorkerReserveCurrentRoute(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (adapterWorkerReserveCurrentRouteRow, error)
	adapterWorkerReserveReactivate(ctx context.Context, effectiveName string, origin string, destinationKind string, repositoryID int64, destinationID int64, destinationScope string, now time.Time, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalized string) (int64, error)
	adapterWorkerReserveCountQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error)
	adapterWorkerReserveLookup(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (adapterWorkerReserveLookupRow, error)
	adapterWorkerReserveInsert(ctx context.Context, newID string, chainOrg string, chainProject string, chainEnv string, targetID string, origin string, destinationKind string, repositoryID int64, destinationID int64, destinationScope string, surface string, effectiveName string, normalized string, reserved string, now time.Time) (int64, error)
	adapterWorkerPrepareProviderLease(ctx context.Context, jobID string, effectID string, leaseUntil time.Time, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, nowStamp time.Time) (int64, error)
	adapterWorkerPrepareLease(ctx context.Context, leaseUntil time.Time, jobID string, leaseOwner string) (int64, error)
	adapterWorkerPrepareUpdate(ctx context.Context, now time.Time, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalizedName string) (int64, error)
	adapterWorkerPrepareInsert(ctx context.Context, effectID string, chainOrg string, chainProject string, chainEnv string, targetID string, jobID string, surface string, effectiveName string, disposition string, intentID string, now time.Time) (int64, error)
	adapterWorkerFinishUpdateEffect(ctx context.Context, outcomeAuditID string, outcome string, finding string, now time.Time, effectID string) (int64, error)
	adapterWorkerFinishRemove(ctx context.Context, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalizedName string) (int64, error)
	adapterWorkerFinishUpdate(ctx context.Context, state string, missing bool, now time.Time, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalizedName string) (int64, error)
	adapterWorkerFinishReleaseLease(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, jobID string, effectID string) (int64, error)
	adapterWorkerRefuseRemove(ctx context.Context, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalizedName string) (int64, error)
	adapterWorkerReleaseReservationRemove(ctx context.Context, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalizedName string, generation int64, jobID string, leaseOwner string, now time.Time) (int64, error)
	adapterWorkerInsertConflictDedup(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, surface string, effectiveName string) (int64, error)
	adapterWorkerInsertConflictInsert(ctx context.Context, newID string, artifactID string, chainOrg string, chainProject string, chainEnv string, targetID string, jobID string, generation int64, surface string, effectiveName string, now time.Time) (int64, error)
	adapterWorkerInsertAdapterJobAuditWithIDQuery(ctx context.Context, effectID string, typ string, at time.Time, authorityPrincipal string, chainOrg string, chainProject string, chainEnv string, targetID string, outcome string, jobID string, payload string) (int64, error)
	adapterWorkerActivateFinish(ctx context.Context, at time.Time, jobID string, routeMoveID string, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, leaseOwner string) (int64, error)
	adapterWorkerActivateLookup(ctx context.Context, routeMoveID string, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64) (adapterWorkerActivateLookupRow, error)
	adapterWorkerActivateCollisionQuery(ctx context.Context, routeMoveID string, targetID string, pendingOrigin string, kind string, repositoryID int64, destinationID int64, sentinelName string) (int64, error)
	adapterWorkerActivateSetResolved(ctx context.Context, destinationID int64, repositoryID int64, routeMoveID string, targetID string, chainOrg string, chainProject string, pendingEnvironment string) (int64, error)
	adapterWorkerActivateDeleteKeys(ctx context.Context, targetID string, chainOrg string, chainProject string, currentEnvironment string) (int64, error)
	adapterWorkerActivateInsertKeys(ctx context.Context, adapterID string, routeMoveID string, targetID string, chainOrg string, chainProject string, pendingEnvironment string) (int64, error)
	adapterWorkerActivateInsertJob(ctx context.Context, convergeID string, chainOrg string, chainProject string, pendingEnvironment string, targetID string, routeMoveID string, authorityPrincipal string, generation int64, at time.Time) (int64, error)
	adapterWorkerActivateApplyTarget(ctx context.Context, kind string, owner string, name string, destinationEnvironment string, destinationID int64, repositoryID int64, visibility string, selectedRaw []byte, prefix string, generation int64, expectedGeneration int64, convergeID string, targetID string, chainOrg string, chainProject string, currentEnvironment string) (int64, error)
	adapterWorkerActivateUpdateExpiry(ctx context.Context, credentialExpiresAt time.Time, adapterID string, chainOrg string, chainProject string) (int64, error)
	adapterWorkerActivateDeleteClaims(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error)
	adapterWorkerActivateCompleteMove(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, targetID string) (int64, error)
	adapterWorkerActivateOriginRouteMoveMarkProbe(ctx context.Context, targetID string, adapterID string, chainOrg string, chainProject string, chainEnv string, generation int64, jobID string) (int64, error)
	adapterWorkerActivateOriginRouteMoveCountUnresolved(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error)
	adapterWorkerActivateOriginRouteMoveLoadPending(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, adapterID string) (adapterWorkerActivateOriginRouteMoveLoadPendingRow, error)
	adapterWorkerActivateOriginRouteMoveTargetQuery(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, adapterID string) ([]adapterWorkerActivateOriginRouteMoveTargetQueryRow, error)
	adapterWorkerActivateOriginRouteMoveDeleteKeys(ctx context.Context, effectID string, chainOrg string, chainProject string, environment string) (int64, error)
	adapterWorkerActivateOriginRouteMoveInsertKeys(ctx context.Context, adapterID string, routeMoveID string, effectID string, chainOrg string, chainProject string, environment string) (int64, error)
	adapterWorkerActivateOriginRouteMoveInsertJob(ctx context.Context, convergeID string, chainOrg string, chainProject string, environment string, effectID string, routeMoveID string, authorityPrincipal string, generation int64, at time.Time) (int64, error)
	adapterWorkerActivateOriginRouteMoveApplyTarget(ctx context.Context, kind string, owner string, name string, destinationEnvironment string, destinationID int64, repositoryID int64, visibility string, selectedRaw []byte, prefix string, generation int64, expectedGeneration int64, convergeID string, effectID string, adapterID string, chainOrg string, chainProject string, environment string) (int64, error)
	adapterWorkerActivateOriginRouteMoveActivateAdapter(ctx context.Context, pendingOrigin string, pendingCredential []byte, at time.Time, expires *time.Time, adapterID string, chainOrg string, chainProject string) (int64, error)
	adapterWorkerActivateOriginRouteMoveDeleteClaims(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error)
	adapterWorkerActivateOriginRouteMoveCompleteMove(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, adapterID string) (int64, error)
	adapterWorkerFinishJobQuery(ctx context.Context, due time.Time, job adapter.Job) (int64, error)
	adapterWorkerCompleteJob(ctx context.Context, state string, at time.Time, job adapter.Job) (int64, error)
	adapterWorkerFinishJobLookup(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (string, error)
	adapterWorkerFinishJobMarkTarget(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64) (int64, error)
	adapterWorkerFinishJobErase(ctx context.Context, adapterID string, chainOrg string, chainProject string) (int64, error)
	adapterWorkerFinishJobAttention(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error)
	adapterWorkerFinishJobSupersede(ctx context.Context, at time.Time, routeMoveID string, chainOrg string, chainProject string, jobID string) (int64, error)
	adapterWorkerFinishJobMarkTargets(ctx context.Context, chainOrg string, chainProject string, routeMoveID string) (int64, error)
	adapterWorkerRecordJobOutcome(ctx context.Context, targetStatus string, convergedRevision int64, convergedRev sql.NullInt64, failureJSON []byte, warningJSON []byte, revision int64, rev sql.NullInt64, attemptedAt time.Time, errorClass sql.NullString, attentionMode int64, retainActiveJob int64, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64) (int64, error)
	adapterWorkerRaiseDriftAttentionQuery(ctx context.Context, targetID string, org string, project string, environmentID string) (int64, error)
	adapterWorkerFinishRouteMoveScrubLookupMove(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, targetID string, chainEnv string) (string, error)
	adapterWorkerFinishRouteMoveScrubActiveLedgerQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error)
	adapterWorkerFinishRouteMoveScrubPersistOrphans(ctx context.Context, orphanJSON []byte, routeMoveID string, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error)
	adapterWorkerFinishRouteMoveScrubMove(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, targetID string) (int64, error)
	adapterWorkerFinishRouteMoveScrubInsert(ctx context.Context, activateID string, chainOrg string, chainProject string, chainEnv string, targetID string, routeMoveID string, authorityPrincipal string, generation int64, at time.Time) (int64, error)
	adapterWorkerFinishRouteMoveScrubMark(ctx context.Context, failureJSON []byte, activateID string, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64) (int64, error)
	adapterWorkerFinishOriginRouteMoveScrubMarkDone(ctx context.Context, failureJSON []byte, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, jobID string) (int64, error)
	adapterWorkerFinishOriginRouteMoveScrubCountPending(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error)
	adapterWorkerFinishOriginRouteMoveScrubActivateMove(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error)
	adapterWorkerFinishOriginRouteMoveScrubTargetQuery(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) ([]adapterWorkerFinishOriginRouteMoveScrubTargetQueryRow, error)
	adapterWorkerFinishOriginRouteMoveScrubInsert(ctx context.Context, activateID string, chainOrg string, chainProject string, environment string, effectID string, routeMoveID string, authority string, generation int64, at time.Time) (int64, error)
	adapterWorkerFinishOriginRouteMoveScrubMark(ctx context.Context, activateID string, effectID string, chainOrg string, chainProject string, environment string, generation int64) (int64, error)
	adapterWorkerFinishDeadCredentialScrubLookup(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (string, error)
	adapterWorkerFinishDeadCredentialScrubReleaseLedger(ctx context.Context, at time.Time, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error)
	adapterWorkerFinishDeadCredentialScrubMarkTarget(ctx context.Context, failureJSON []byte, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64) (int64, error)
	adapterWorkerFinishDeadCredentialScrubErase(ctx context.Context, adapterID string, chainOrg string, chainProject string) (int64, error)
	adapterWorkerCheckProviderSwitchQuery(ctx context.Context) (int64, error)
}
type adapterWorkerLoadExecutionQueryRow struct {
	Provider               string
	Origin                 string
	ID                     string
	CredentialCiphertext   []byte
	DestinationKind        string
	DestinationOwner       string
	DestinationName        string
	DestinationEnvironment string
	DestinationID          int64
	RepositoryID           int64
	Visibility             string
	SelectedRepositoryIds  []byte
	NamePrefix             string
	Generation             int64
	SpkiPin                string
	CaBundlePem            string
	AllowPersonalToken     int64
	DestinationScope       string
	VariableProtected      int64
	VariableHidden         int64
	VariableExpand         int64
}
type adapterWorkerLoadExecutionLedgerQueryRow struct {
	Surface       string
	EffectiveName string
	State         string
	Missing       bool
}
type adapterWorkerLoadExecutionSnapshotQueryRow struct {
	ID                string
	Revision          int64
	ParameterContract string
}
type adapterWorkerLoadExecutionEntryQueryRow struct {
	ID             string
	SnapshotID     string
	KeyID          string
	KeyName        string
	Classification string
	Ciphertext     []byte
}
type adapterWorkerLoadActivationQueryRow struct {
	Provider                    string
	PendingOrigin               string
	ID                          string
	PendingCredentialCiphertext []byte
	EnvironmentID               string
	DestinationKind             string
	DestinationOwner            string
	DestinationName             string
	DestinationEnvironment      string
	DestinationID               int64
	RepositoryID                int64
	Visibility                  string
	SelectedRepositoryIds       []byte
	NamePrefix                  string
	Generation                  int64
	SpkiPin                     string
	CaBundlePem                 string
	AllowPersonalToken          int64
	DestinationScope            string
	VariableProtected           int64
	VariableHidden              int64
	VariableExpand              int64
}
type adapterWorkerClaimDueSelectQueryRow struct {
	ID                   string
	OrgID                string
	ProjectID            string
	EnvironmentID        string
	TargetID             string
	Kind                 string
	RouteMoveID          string
	AuthorityPrincipalID string
	Generation           int64
	AttemptCount         int64
	CreatedAt            string
}
type adapterWorkerCloseIndeterminateEffectsQueryRow struct {
	ID                   string
	JobID                string
	Surface              string
	EffectiveName        string
	Disposition          string
	AuthorityPrincipalID string
}
type adapterWorkerReserveCurrentRouteRow struct {
	Origin           string
	DestinationKind  string
	DestinationID    int64
	RepositoryID     int64
	DestinationScope string
}
type adapterWorkerReserveLookupRow struct {
	Origin           string
	DestinationKind  string
	DestinationID    int64
	RepositoryID     int64
	DestinationScope string
}
type adapterWorkerActivateLookupRow struct {
	AdapterID              string
	EnvironmentID          string
	EnvironmentID_2        string
	DestinationKind        string
	DestinationOwner       string
	DestinationName        string
	DestinationEnvironment string
	Visibility             string
	SelectedRepositoryIds  []byte
	NamePrefix             string
	Kind                   string
	PendingOrigin          string
}
type adapterWorkerActivateOriginRouteMoveLoadPendingRow struct {
	PendingOrigin               sql.NullString
	PendingCredentialCiphertext []byte
}
type adapterWorkerActivateOriginRouteMoveTargetQueryRow struct {
	ID                     string
	EnvironmentID          string
	Generation             int64
	DestinationKind        string
	DestinationOwner       string
	DestinationName        string
	DestinationEnvironment string
	DestinationID          int64
	RepositoryID           int64
	Visibility             string
	SelectedRepositoryIds  []byte
	NamePrefix             string
}
type adapterWorkerFinishOriginRouteMoveScrubTargetQueryRow struct {
	ID                   string
	EnvironmentID        string
	Generation           int64
	AuthorityPrincipalID string
}
type sqliteAdapterRuntimeQueries struct{ queries *sqlitegen.Queries }

func (q sqliteAdapterRuntimeQueries) adapterWorkerLoadExecutionQuery(ctx context.Context, jobID string, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, leaseOwner string) (adapterWorkerLoadExecutionQueryRow, error) {
	value, err := q.queries.AdapterWorkerLoadExecutionQuery(ctx, sqlitegen.AdapterWorkerLoadExecutionQueryParams{JobID: jobID, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation, LeaseOwner: sql.NullString{String: leaseOwner, Valid: true}})
	if err != nil {
		return adapterWorkerLoadExecutionQueryRow{}, err
	}
	return adapterWorkerLoadExecutionQueryRow{Provider: value.Provider, Origin: value.Origin, ID: value.ID, CredentialCiphertext: value.CredentialCiphertext, DestinationKind: value.DestinationKind, DestinationOwner: value.DestinationOwner, DestinationName: value.DestinationName, DestinationEnvironment: value.DestinationEnvironment, DestinationID: value.DestinationID, RepositoryID: value.RepositoryID, Visibility: value.Visibility, SelectedRepositoryIds: []byte(value.SelectedRepositoryIds), NamePrefix: value.NamePrefix, Generation: value.Generation, SpkiPin: value.SpkiPin, CaBundlePem: value.CaBundlePem, AllowPersonalToken: value.AllowPersonalToken, DestinationScope: value.DestinationScope, VariableProtected: value.VariableProtected, VariableHidden: value.VariableHidden, VariableExpand: value.VariableExpand}, nil
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerLoadExecutionLedgerQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) ([]adapterWorkerLoadExecutionLedgerQueryRow, error) {
	value, err := q.queries.AdapterWorkerLoadExecutionLedgerQuery(ctx, sqlitegen.AdapterWorkerLoadExecutionLedgerQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
	if err != nil {
		return nil, err
	}
	return mapRows(value, func(c sqlitegen.AdapterWorkerLoadExecutionLedgerQueryRow) (adapterWorkerLoadExecutionLedgerQueryRow, error) {
		return adapterWorkerLoadExecutionLedgerQueryRow{Surface: c.Surface, EffectiveName: c.EffectiveName, State: c.State, Missing: (c.Missing != 0)}, nil
	})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerLoadExecutionSnapshotQuery(ctx context.Context, chainOrg string, chainProject string, chainEnv string) (adapterWorkerLoadExecutionSnapshotQueryRow, error) {
	value, err := q.queries.AdapterWorkerLoadExecutionSnapshotQuery(ctx, sqlitegen.AdapterWorkerLoadExecutionSnapshotQueryParams{ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
	if err != nil {
		return adapterWorkerLoadExecutionSnapshotQueryRow{}, err
	}
	return adapterWorkerLoadExecutionSnapshotQueryRow(value), nil
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerLoadExecutionEntryQuery(ctx context.Context, targetID string, snapshotID string, chainOrg string, chainProject string, chainEnv string) ([]adapterWorkerLoadExecutionEntryQueryRow, error) {
	value, err := q.queries.AdapterWorkerLoadExecutionEntryQuery(ctx, sqlitegen.AdapterWorkerLoadExecutionEntryQueryParams{TargetID: targetID, SnapshotID: snapshotID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
	if err != nil {
		return nil, err
	}
	return mapRows(value, func(c sqlitegen.AdapterWorkerLoadExecutionEntryQueryRow) (adapterWorkerLoadExecutionEntryQueryRow, error) {
		return adapterWorkerLoadExecutionEntryQueryRow(c), nil
	})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerLoadActivationQuery(ctx context.Context, jobID string, routeMoveID string, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, leaseOwner string) (adapterWorkerLoadActivationQueryRow, error) {
	value, err := q.queries.AdapterWorkerLoadActivationQuery(ctx, sqlitegen.AdapterWorkerLoadActivationQueryParams{JobID: jobID, RouteMoveID: sql.NullString{String: routeMoveID, Valid: true}, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation, LeaseOwner: sql.NullString{String: leaseOwner, Valid: true}})
	if err != nil {
		return adapterWorkerLoadActivationQueryRow{}, err
	}
	return adapterWorkerLoadActivationQueryRow{Provider: value.Provider, PendingOrigin: value.PendingOrigin, ID: value.ID, PendingCredentialCiphertext: value.PendingCredentialCiphertext, EnvironmentID: value.EnvironmentID, DestinationKind: value.DestinationKind, DestinationOwner: value.DestinationOwner, DestinationName: value.DestinationName, DestinationEnvironment: value.DestinationEnvironment, DestinationID: value.DestinationID, RepositoryID: value.RepositoryID, Visibility: value.Visibility, SelectedRepositoryIds: []byte(value.SelectedRepositoryIds), NamePrefix: value.NamePrefix, Generation: value.Generation, SpkiPin: value.SpkiPin, CaBundlePem: value.CaBundlePem, AllowPersonalToken: value.AllowPersonalToken, DestinationScope: value.DestinationScope, VariableProtected: value.VariableProtected, VariableHidden: value.VariableHidden, VariableExpand: value.VariableExpand}, nil
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerTryEnqueueLookup(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, now time.Time) (int64, error) {
	return q.queries.AdapterWorkerTryEnqueueLookup(ctx, sqlitegen.AdapterWorkerTryEnqueueLookupParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Now: runtimeSQLiteStamp(now)})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerTryEnqueueExistsQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error) {
	return q.queries.AdapterWorkerTryEnqueueExistsQuery(ctx, sqlitegen.AdapterWorkerTryEnqueueExistsQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerTryEnqueueDepthQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error) {
	return q.queries.AdapterWorkerTryEnqueueDepthQuery(ctx, sqlitegen.AdapterWorkerTryEnqueueDepthQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerTryEnqueuePreviousQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (string, error) {
	return q.queries.AdapterWorkerTryEnqueuePreviousQuery(ctx, sqlitegen.AdapterWorkerTryEnqueuePreviousQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerTryEnqueueSupersede(ctx context.Context, at time.Time, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error) {
	return q.queries.AdapterWorkerTryEnqueueSupersede(ctx, sqlitegen.AdapterWorkerTryEnqueueSupersedeParams{At: runtimeSQLiteStamp(at), TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerTryEnqueueInsert(ctx context.Context, jobID string, chainOrg string, chainProject string, chainEnv string, targetID string, kind string, authorityPrincipal string, generation int64, at time.Time) (int64, error) {
	return q.queries.AdapterWorkerTryEnqueueInsert(ctx, sqlitegen.AdapterWorkerTryEnqueueInsertParams{JobID: jobID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Kind: kind, AuthorityPrincipal: authorityPrincipal, Generation: generation, At: fixedStamp(at)})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerTryEnqueueUpdate(ctx context.Context, generation int64, state string, jobID string, targetID string, chainOrg string, chainProject string, chainEnv string, expectedGeneration int64, at time.Time) (int64, error) {
	return q.queries.AdapterWorkerTryEnqueueUpdate(ctx, sqlitegen.AdapterWorkerTryEnqueueUpdateParams{Generation: generation, State: state, JobID: sql.NullString{String: jobID, Valid: true}, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, ExpectedGeneration: expectedGeneration, At: runtimeSQLiteStamp(at)})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerClaimDueSelectQuery(ctx context.Context, now time.Time) (adapterWorkerClaimDueSelectQueryRow, error) {
	value, err := q.queries.AdapterWorkerClaimDueSelectQuery(ctx, fixedStamp(now))
	if err != nil {
		return adapterWorkerClaimDueSelectQueryRow{}, err
	}
	return adapterWorkerClaimDueSelectQueryRow(value), nil
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerClaimDueUpdate(ctx context.Context, attempt int64, worker string, leaseUntil time.Time, jobID string) (int64, error) {
	return q.queries.AdapterWorkerClaimDueUpdate(ctx, sqlitegen.AdapterWorkerClaimDueUpdateParams{Attempt: attempt, Worker: sql.NullString{String: worker, Valid: true}, LeaseUntil: runtimeSQLiteStamp(leaseUntil), JobID: jobID})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerCloseIndeterminateEffectsQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, at time.Time) ([]adapterWorkerCloseIndeterminateEffectsQueryRow, error) {
	value, err := q.queries.AdapterWorkerCloseIndeterminateEffectsQuery(ctx, sqlitegen.AdapterWorkerCloseIndeterminateEffectsQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, At: runtimeSQLiteStamp(at)})
	if err != nil {
		return nil, err
	}
	return mapRows(value, func(c sqlitegen.AdapterWorkerCloseIndeterminateEffectsQueryRow) (adapterWorkerCloseIndeterminateEffectsQueryRow, error) {
		return adapterWorkerCloseIndeterminateEffectsQueryRow(c), nil
	})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerCloseIndeterminateEffectsUpdate(ctx context.Context, outcomeAuditID string, at time.Time, effectID string, chainOrg string, chainProject string, chainEnv string, targetID string) (int64, error) {
	return q.queries.AdapterWorkerCloseIndeterminateEffectsUpdate(ctx, sqlitegen.AdapterWorkerCloseIndeterminateEffectsUpdateParams{OutcomeAuditID: sql.NullString{String: outcomeAuditID, Valid: true}, At: runtimeSQLiteStamp(at), EffectID: effectID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerCloseIndeterminateEffectsRelease(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, effectID string) (int64, error) {
	return q.queries.AdapterWorkerCloseIndeterminateEffectsRelease(ctx, sqlitegen.AdapterWorkerCloseIndeterminateEffectsReleaseParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, EffectID: sql.NullString{String: effectID, Valid: true}})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerGateQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, jobID string, leaseOwner string, now time.Time) (int64, error) {
	return q.queries.AdapterWorkerGateQuery(ctx, sqlitegen.AdapterWorkerGateQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation, JobID: jobID, LeaseOwner: sql.NullString{String: leaseOwner, Valid: true}, Now: runtimeSQLiteStamp(now)})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerReservePendingQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, surface string, normalized string) (int64, error) {
	return q.queries.AdapterWorkerReservePendingQuery(ctx, sqlitegen.AdapterWorkerReservePendingQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Surface: surface, Normalized: normalized})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerReserveSelectQuery(ctx context.Context, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalized string) (string, error) {
	return q.queries.AdapterWorkerReserveSelectQuery(ctx, sqlitegen.AdapterWorkerReserveSelectQueryParams{ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Surface: surface, Normalized: normalized})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerReserveCurrentRoute(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (adapterWorkerReserveCurrentRouteRow, error) {
	value, err := q.queries.AdapterWorkerReserveCurrentRoute(ctx, sqlitegen.AdapterWorkerReserveCurrentRouteParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
	if err != nil {
		return adapterWorkerReserveCurrentRouteRow{}, err
	}
	return adapterWorkerReserveCurrentRouteRow(value), nil
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerReserveReactivate(ctx context.Context, effectiveName string, origin string, destinationKind string, repositoryID int64, destinationID int64, destinationScope string, now time.Time, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalized string) (int64, error) {
	return q.queries.AdapterWorkerReserveReactivate(ctx, sqlitegen.AdapterWorkerReserveReactivateParams{EffectiveName: effectiveName, Origin: origin, DestinationKind: destinationKind, RepositoryID: repositoryID, DestinationID: destinationID, DestinationScope: destinationScope, Now: fixedStamp(now), ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Surface: surface, Normalized: normalized})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerReserveCountQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error) {
	return q.queries.AdapterWorkerReserveCountQuery(ctx, sqlitegen.AdapterWorkerReserveCountQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerReserveLookup(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (adapterWorkerReserveLookupRow, error) {
	value, err := q.queries.AdapterWorkerReserveLookup(ctx, sqlitegen.AdapterWorkerReserveLookupParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
	if err != nil {
		return adapterWorkerReserveLookupRow{}, err
	}
	return adapterWorkerReserveLookupRow(value), nil
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerReserveInsert(ctx context.Context, newID string, chainOrg string, chainProject string, chainEnv string, targetID string, origin string, destinationKind string, repositoryID int64, destinationID int64, destinationScope string, surface string, effectiveName string, normalized string, reserved string, now time.Time) (int64, error) {
	return q.queries.AdapterWorkerReserveInsert(ctx, sqlitegen.AdapterWorkerReserveInsertParams{NewID: newID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Origin: origin, DestinationKind: destinationKind, RepositoryID: repositoryID, DestinationID: destinationID, DestinationScope: destinationScope, Surface: surface, EffectiveName: effectiveName, Normalized: normalized, Reserved: reserved, Now: fixedStamp(now)})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerPrepareProviderLease(ctx context.Context, jobID string, effectID string, leaseUntil time.Time, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, nowStamp time.Time) (int64, error) {
	return q.queries.AdapterWorkerPrepareProviderLease(ctx, sqlitegen.AdapterWorkerPrepareProviderLeaseParams{JobID: sql.NullString{String: jobID, Valid: true}, EffectID: sql.NullString{String: effectID, Valid: true}, LeaseUntil: runtimeSQLiteStamp(leaseUntil), TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation, NowStamp: runtimeSQLiteStamp(nowStamp)})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerPrepareLease(ctx context.Context, leaseUntil time.Time, jobID string, leaseOwner string) (int64, error) {
	return q.queries.AdapterWorkerPrepareLease(ctx, sqlitegen.AdapterWorkerPrepareLeaseParams{LeaseUntil: runtimeSQLiteStamp(leaseUntil), JobID: jobID, LeaseOwner: sql.NullString{String: leaseOwner, Valid: true}})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerPrepareUpdate(ctx context.Context, now time.Time, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalizedName string) (int64, error) {
	return q.queries.AdapterWorkerPrepareUpdate(ctx, sqlitegen.AdapterWorkerPrepareUpdateParams{Now: fixedStamp(now), ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Surface: surface, NormalizedName: normalizedName})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerPrepareInsert(ctx context.Context, effectID string, chainOrg string, chainProject string, chainEnv string, targetID string, jobID string, surface string, effectiveName string, disposition string, intentID string, now time.Time) (int64, error) {
	return q.queries.AdapterWorkerPrepareInsert(ctx, sqlitegen.AdapterWorkerPrepareInsertParams{EffectID: effectID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, JobID: jobID, Surface: surface, EffectiveName: effectiveName, Disposition: disposition, IntentID: intentID, Now: fixedStamp(now)})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishUpdateEffect(ctx context.Context, outcomeAuditID string, outcome string, finding string, now time.Time, effectID string) (int64, error) {
	return q.queries.AdapterWorkerFinishUpdateEffect(ctx, sqlitegen.AdapterWorkerFinishUpdateEffectParams{OutcomeAuditID: sql.NullString{String: outcomeAuditID, Valid: true}, Outcome: sql.NullString{String: outcome, Valid: true}, Finding: finding, Now: runtimeSQLiteStamp(now), EffectID: effectID})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishRemove(ctx context.Context, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalizedName string) (int64, error) {
	return q.queries.AdapterWorkerFinishRemove(ctx, sqlitegen.AdapterWorkerFinishRemoveParams{ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Surface: surface, NormalizedName: normalizedName})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishUpdate(ctx context.Context, state string, missing bool, now time.Time, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalizedName string) (int64, error) {
	return q.queries.AdapterWorkerFinishUpdate(ctx, sqlitegen.AdapterWorkerFinishUpdateParams{State: state, Missing: boolInt(missing), Now: fixedStamp(now), ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Surface: surface, NormalizedName: normalizedName})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishReleaseLease(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, jobID string, effectID string) (int64, error) {
	return q.queries.AdapterWorkerFinishReleaseLease(ctx, sqlitegen.AdapterWorkerFinishReleaseLeaseParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, JobID: sql.NullString{String: jobID, Valid: true}, EffectID: sql.NullString{String: effectID, Valid: true}})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerRefuseRemove(ctx context.Context, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalizedName string) (int64, error) {
	return q.queries.AdapterWorkerRefuseRemove(ctx, sqlitegen.AdapterWorkerRefuseRemoveParams{ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Surface: surface, NormalizedName: normalizedName})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerReleaseReservationRemove(ctx context.Context, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalizedName string, generation int64, jobID string, leaseOwner string, now time.Time) (int64, error) {
	return q.queries.AdapterWorkerReleaseReservationRemove(ctx, sqlitegen.AdapterWorkerReleaseReservationRemoveParams{ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Surface: surface, NormalizedName: normalizedName, Generation: generation, JobID: jobID, LeaseOwner: sql.NullString{String: leaseOwner, Valid: true}, Now: runtimeSQLiteStamp(now)})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerInsertConflictDedup(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, surface string, effectiveName string) (int64, error) {
	return q.queries.AdapterWorkerInsertConflictDedup(ctx, sqlitegen.AdapterWorkerInsertConflictDedupParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation, Surface: surface, EffectiveName: effectiveName})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerInsertConflictInsert(ctx context.Context, newID string, artifactID string, chainOrg string, chainProject string, chainEnv string, targetID string, jobID string, generation int64, surface string, effectiveName string, now time.Time) (int64, error) {
	return q.queries.AdapterWorkerInsertConflictInsert(ctx, sqlitegen.AdapterWorkerInsertConflictInsertParams{NewID: newID, ArtifactID: artifactID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, JobID: sql.NullString{String: jobID, Valid: true}, Generation: generation, Surface: surface, EffectiveName: effectiveName, Now: fixedStamp(now)})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerInsertAdapterJobAuditWithIDQuery(ctx context.Context, effectID string, typ string, at time.Time, authorityPrincipal string, chainOrg string, chainProject string, chainEnv string, targetID string, outcome string, jobID string, payload string) (int64, error) {
	return q.queries.AdapterWorkerInsertAdapterJobAuditWithIDQuery(ctx, sqlitegen.AdapterWorkerInsertAdapterJobAuditWithIDQueryParams{EffectID: effectID, Typ: typ, At: fixedStamp(at), AuthorityPrincipal: sql.NullString{String: authorityPrincipal, Valid: true}, ChainOrg: chainOrg, ChainProject: sql.NullString{String: chainProject, Valid: true}, ChainEnv: sql.NullString{String: chainEnv, Valid: true}, TargetID: sql.NullString{String: targetID, Valid: true}, Outcome: outcome, JobID: sql.NullString{String: jobID, Valid: true}, Payload: payload})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateFinish(ctx context.Context, at time.Time, jobID string, routeMoveID string, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, leaseOwner string) (int64, error) {
	return q.queries.AdapterWorkerActivateFinish(ctx, sqlitegen.AdapterWorkerActivateFinishParams{At: runtimeSQLiteStamp(at), JobID: jobID, RouteMoveID: sql.NullString{String: routeMoveID, Valid: true}, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation, LeaseOwner: sql.NullString{String: leaseOwner, Valid: true}})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateLookup(ctx context.Context, routeMoveID string, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64) (adapterWorkerActivateLookupRow, error) {
	value, err := q.queries.AdapterWorkerActivateLookup(ctx, sqlitegen.AdapterWorkerActivateLookupParams{RouteMoveID: routeMoveID, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation})
	if err != nil {
		return adapterWorkerActivateLookupRow{}, err
	}
	return adapterWorkerActivateLookupRow{AdapterID: value.AdapterID, EnvironmentID: value.EnvironmentID, EnvironmentID_2: value.EnvironmentID_2, DestinationKind: value.DestinationKind, DestinationOwner: value.DestinationOwner, DestinationName: value.DestinationName, DestinationEnvironment: value.DestinationEnvironment, Visibility: value.Visibility, SelectedRepositoryIds: []byte(value.SelectedRepositoryIds), NamePrefix: value.NamePrefix, Kind: value.Kind, PendingOrigin: value.PendingOrigin}, nil
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateCollisionQuery(ctx context.Context, routeMoveID string, targetID string, pendingOrigin string, kind string, repositoryID int64, destinationID int64, sentinelName string) (int64, error) {
	return q.queries.AdapterWorkerActivateCollisionQuery(ctx, sqlitegen.AdapterWorkerActivateCollisionQueryParams{RouteMoveID: routeMoveID, TargetID: targetID, PendingOrigin: pendingOrigin, Kind: kind, RepositoryID: repositoryID, DestinationID: destinationID, SentinelName: sentinelName})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateSetResolved(ctx context.Context, destinationID int64, repositoryID int64, routeMoveID string, targetID string, chainOrg string, chainProject string, pendingEnvironment string) (int64, error) {
	return q.queries.AdapterWorkerActivateSetResolved(ctx, sqlitegen.AdapterWorkerActivateSetResolvedParams{DestinationID: destinationID, RepositoryID: repositoryID, RouteMoveID: routeMoveID, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, PendingEnvironment: pendingEnvironment})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateDeleteKeys(ctx context.Context, targetID string, chainOrg string, chainProject string, currentEnvironment string) (int64, error) {
	return q.queries.AdapterWorkerActivateDeleteKeys(ctx, sqlitegen.AdapterWorkerActivateDeleteKeysParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, CurrentEnvironment: currentEnvironment})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateInsertKeys(ctx context.Context, adapterID string, routeMoveID string, targetID string, chainOrg string, chainProject string, pendingEnvironment string) (int64, error) {
	return q.queries.AdapterWorkerActivateInsertKeys(ctx, sqlitegen.AdapterWorkerActivateInsertKeysParams{AdapterID: adapterID, RouteMoveID: routeMoveID, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, PendingEnvironment: pendingEnvironment})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateInsertJob(ctx context.Context, convergeID string, chainOrg string, chainProject string, pendingEnvironment string, targetID string, routeMoveID string, authorityPrincipal string, generation int64, at time.Time) (int64, error) {
	return q.queries.AdapterWorkerActivateInsertJob(ctx, sqlitegen.AdapterWorkerActivateInsertJobParams{ConvergeID: convergeID, ChainOrg: chainOrg, ChainProject: chainProject, PendingEnvironment: pendingEnvironment, TargetID: targetID, RouteMoveID: sql.NullString{String: routeMoveID, Valid: true}, AuthorityPrincipal: authorityPrincipal, Generation: generation, At: fixedStamp(at)})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateApplyTarget(ctx context.Context, kind string, owner string, name string, destinationEnvironment string, destinationID int64, repositoryID int64, visibility string, selectedRaw []byte, prefix string, generation int64, expectedGeneration int64, convergeID string, targetID string, chainOrg string, chainProject string, currentEnvironment string) (int64, error) {
	return q.queries.AdapterWorkerActivateApplyTarget(ctx, sqlitegen.AdapterWorkerActivateApplyTargetParams{Kind: kind, Owner: owner, Name: name, DestinationEnvironment: destinationEnvironment, DestinationID: destinationID, RepositoryID: repositoryID, Visibility: visibility, SelectedRaw: string(selectedRaw), Prefix: prefix, Generation: generation, ExpectedGeneration: expectedGeneration, ConvergeID: sql.NullString{String: convergeID, Valid: true}, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, CurrentEnvironment: currentEnvironment})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateUpdateExpiry(ctx context.Context, credentialExpiresAt time.Time, adapterID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerActivateUpdateExpiry(ctx, sqlitegen.AdapterWorkerActivateUpdateExpiryParams{CredentialExpiresAt: runtimeSQLiteStamp(credentialExpiresAt), AdapterID: adapterID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateDeleteClaims(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerActivateDeleteClaims(ctx, sqlitegen.AdapterWorkerActivateDeleteClaimsParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateCompleteMove(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, targetID string) (int64, error) {
	return q.queries.AdapterWorkerActivateCompleteMove(ctx, sqlitegen.AdapterWorkerActivateCompleteMoveParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject, TargetID: sql.NullString{String: targetID, Valid: true}})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveMarkProbe(ctx context.Context, targetID string, adapterID string, chainOrg string, chainProject string, chainEnv string, generation int64, jobID string) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveMarkProbe(ctx, sqlitegen.AdapterWorkerActivateOriginRouteMoveMarkProbeParams{TargetID: targetID, AdapterID: adapterID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation, JobID: sql.NullString{String: jobID, Valid: true}})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveCountUnresolved(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveCountUnresolved(ctx, sqlitegen.AdapterWorkerActivateOriginRouteMoveCountUnresolvedParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveLoadPending(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, adapterID string) (adapterWorkerActivateOriginRouteMoveLoadPendingRow, error) {
	value, err := q.queries.AdapterWorkerActivateOriginRouteMoveLoadPending(ctx, sqlitegen.AdapterWorkerActivateOriginRouteMoveLoadPendingParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject, AdapterID: adapterID})
	if err != nil {
		return adapterWorkerActivateOriginRouteMoveLoadPendingRow{}, err
	}
	return adapterWorkerActivateOriginRouteMoveLoadPendingRow(value), nil
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveTargetQuery(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, adapterID string) ([]adapterWorkerActivateOriginRouteMoveTargetQueryRow, error) {
	value, err := q.queries.AdapterWorkerActivateOriginRouteMoveTargetQuery(ctx, sqlitegen.AdapterWorkerActivateOriginRouteMoveTargetQueryParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject, AdapterID: adapterID})
	if err != nil {
		return nil, err
	}
	return mapRows(value, func(c sqlitegen.AdapterWorkerActivateOriginRouteMoveTargetQueryRow) (adapterWorkerActivateOriginRouteMoveTargetQueryRow, error) {
		return adapterWorkerActivateOriginRouteMoveTargetQueryRow{ID: c.ID, EnvironmentID: c.EnvironmentID, Generation: c.Generation, DestinationKind: c.DestinationKind, DestinationOwner: c.DestinationOwner, DestinationName: c.DestinationName, DestinationEnvironment: c.DestinationEnvironment, DestinationID: c.DestinationID, RepositoryID: c.RepositoryID, Visibility: c.Visibility, SelectedRepositoryIds: []byte(c.SelectedRepositoryIds), NamePrefix: c.NamePrefix}, nil
	})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveDeleteKeys(ctx context.Context, effectID string, chainOrg string, chainProject string, environment string) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveDeleteKeys(ctx, sqlitegen.AdapterWorkerActivateOriginRouteMoveDeleteKeysParams{EffectID: effectID, ChainOrg: chainOrg, ChainProject: chainProject, Environment: environment})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveInsertKeys(ctx context.Context, adapterID string, routeMoveID string, effectID string, chainOrg string, chainProject string, environment string) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveInsertKeys(ctx, sqlitegen.AdapterWorkerActivateOriginRouteMoveInsertKeysParams{AdapterID: adapterID, RouteMoveID: routeMoveID, EffectID: effectID, ChainOrg: chainOrg, ChainProject: chainProject, Environment: environment})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveInsertJob(ctx context.Context, convergeID string, chainOrg string, chainProject string, environment string, effectID string, routeMoveID string, authorityPrincipal string, generation int64, at time.Time) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveInsertJob(ctx, sqlitegen.AdapterWorkerActivateOriginRouteMoveInsertJobParams{ConvergeID: convergeID, ChainOrg: chainOrg, ChainProject: chainProject, Environment: environment, EffectID: effectID, RouteMoveID: sql.NullString{String: routeMoveID, Valid: true}, AuthorityPrincipal: authorityPrincipal, Generation: generation, At: fixedStamp(at)})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveApplyTarget(ctx context.Context, kind string, owner string, name string, destinationEnvironment string, destinationID int64, repositoryID int64, visibility string, selectedRaw []byte, prefix string, generation int64, expectedGeneration int64, convergeID string, effectID string, adapterID string, chainOrg string, chainProject string, environment string) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveApplyTarget(ctx, sqlitegen.AdapterWorkerActivateOriginRouteMoveApplyTargetParams{Kind: kind, Owner: owner, Name: name, DestinationEnvironment: destinationEnvironment, DestinationID: destinationID, RepositoryID: repositoryID, Visibility: visibility, SelectedRaw: string(selectedRaw), Prefix: prefix, Generation: generation, ExpectedGeneration: expectedGeneration, ConvergeID: sql.NullString{String: convergeID, Valid: true}, EffectID: effectID, AdapterID: adapterID, ChainOrg: chainOrg, ChainProject: chainProject, Environment: environment})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveActivateAdapter(ctx context.Context, pendingOrigin string, pendingCredential []byte, at time.Time, expires *time.Time, adapterID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveActivateAdapter(ctx, sqlitegen.AdapterWorkerActivateOriginRouteMoveActivateAdapterParams{PendingOrigin: pendingOrigin, PendingCredential: pendingCredential, At: runtimeSQLiteStamp(at), Expires: sqliteStampPtr(expires), AdapterID: adapterID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveDeleteClaims(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveDeleteClaims(ctx, sqlitegen.AdapterWorkerActivateOriginRouteMoveDeleteClaimsParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveCompleteMove(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, adapterID string) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveCompleteMove(ctx, sqlitegen.AdapterWorkerActivateOriginRouteMoveCompleteMoveParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject, AdapterID: adapterID})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishJobQuery(ctx context.Context, due time.Time, job adapter.Job) (int64, error) {
	return q.queries.AdapterWorkerFinishJobQuery(ctx, sqlitegen.AdapterWorkerFinishJobQueryParams{Due: fixedStamp(due), JobID: job.ID, LeaseOwner: sql.NullString{String: job.LeaseOwner, Valid: true}, ChainOrg: job.OrgID, ChainProject: job.ProjectID, ChainEnv: job.EnvironmentID, TargetID: job.TargetID, Generation: job.Generation, AuthorityPrincipal: job.AuthorityPrincipal, Kind: string(job.Kind), RouteMoveID: job.RouteMoveID})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerCompleteJob(ctx context.Context, state string, at time.Time, job adapter.Job) (int64, error) {
	return q.queries.AdapterWorkerCompleteJob(ctx, sqlitegen.AdapterWorkerCompleteJobParams{State: state, At: runtimeSQLiteStamp(at), JobID: job.ID, LeaseOwner: sql.NullString{String: job.LeaseOwner, Valid: true}, ChainOrg: job.OrgID, ChainProject: job.ProjectID, ChainEnv: job.EnvironmentID, TargetID: job.TargetID, Generation: job.Generation, AuthorityPrincipal: job.AuthorityPrincipal, Kind: string(job.Kind), RouteMoveID: job.RouteMoveID})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishJobLookup(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (string, error) {
	return q.queries.AdapterWorkerFinishJobLookup(ctx, sqlitegen.AdapterWorkerFinishJobLookupParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishJobMarkTarget(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64) (int64, error) {
	return q.queries.AdapterWorkerFinishJobMarkTarget(ctx, sqlitegen.AdapterWorkerFinishJobMarkTargetParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishJobErase(ctx context.Context, adapterID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerFinishJobErase(ctx, sqlitegen.AdapterWorkerFinishJobEraseParams{AdapterID: adapterID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishJobAttention(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerFinishJobAttention(ctx, sqlitegen.AdapterWorkerFinishJobAttentionParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishJobSupersede(ctx context.Context, at time.Time, routeMoveID string, chainOrg string, chainProject string, jobID string) (int64, error) {
	return q.queries.AdapterWorkerFinishJobSupersede(ctx, sqlitegen.AdapterWorkerFinishJobSupersedeParams{At: runtimeSQLiteStamp(at), RouteMoveID: sql.NullString{String: routeMoveID, Valid: true}, ChainOrg: chainOrg, ChainProject: chainProject, JobID: jobID})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishJobMarkTargets(ctx context.Context, chainOrg string, chainProject string, routeMoveID string) (int64, error) {
	return q.queries.AdapterWorkerFinishJobMarkTargets(ctx, sqlitegen.AdapterWorkerFinishJobMarkTargetsParams{ChainOrg: chainOrg, ChainProject: chainProject, RouteMoveID: routeMoveID})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerRecordJobOutcome(ctx context.Context, targetStatus string, convergedRevision int64, convergedRev sql.NullInt64, failureJSON []byte, warningJSON []byte, revision int64, rev sql.NullInt64, attemptedAt time.Time, errorClass sql.NullString, attentionMode int64, retainActiveJob int64, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64) (int64, error) {
	return q.queries.AdapterWorkerRecordJobOutcome(ctx, sqlitegen.AdapterWorkerRecordJobOutcomeParams{TargetStatus: targetStatus, ConvergedRevision: convergedRevision, ConvergedRev: convergedRev, FailureJSON: string(failureJSON), WarningJSON: string(warningJSON), Revision: revision, Rev: rev, AttemptedAt: runtimeSQLiteStamp(attemptedAt), ErrorClass: errorClass, AttentionMode: attentionMode, RetainActiveJob: retainActiveJob, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerRaiseDriftAttentionQuery(ctx context.Context, targetID string, org string, project string, environmentID string) (int64, error) {
	return q.queries.AdapterWorkerRaiseDriftAttentionQuery(ctx, sqlitegen.AdapterWorkerRaiseDriftAttentionQueryParams{TargetID: targetID, Org: org, Project: project, EnvironmentID: environmentID})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishRouteMoveScrubLookupMove(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, targetID string, chainEnv string) (string, error) {
	return q.queries.AdapterWorkerFinishRouteMoveScrubLookupMove(ctx, sqlitegen.AdapterWorkerFinishRouteMoveScrubLookupMoveParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject, TargetID: targetID, ChainEnv: chainEnv})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishRouteMoveScrubActiveLedgerQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error) {
	return q.queries.AdapterWorkerFinishRouteMoveScrubActiveLedgerQuery(ctx, sqlitegen.AdapterWorkerFinishRouteMoveScrubActiveLedgerQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishRouteMoveScrubPersistOrphans(ctx context.Context, orphanJSON []byte, routeMoveID string, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error) {
	return q.queries.AdapterWorkerFinishRouteMoveScrubPersistOrphans(ctx, sqlitegen.AdapterWorkerFinishRouteMoveScrubPersistOrphansParams{OrphanJSON: string(orphanJSON), RouteMoveID: routeMoveID, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishRouteMoveScrubMove(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, targetID string) (int64, error) {
	return q.queries.AdapterWorkerFinishRouteMoveScrubMove(ctx, sqlitegen.AdapterWorkerFinishRouteMoveScrubMoveParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject, TargetID: sql.NullString{String: targetID, Valid: true}})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishRouteMoveScrubInsert(ctx context.Context, activateID string, chainOrg string, chainProject string, chainEnv string, targetID string, routeMoveID string, authorityPrincipal string, generation int64, at time.Time) (int64, error) {
	return q.queries.AdapterWorkerFinishRouteMoveScrubInsert(ctx, sqlitegen.AdapterWorkerFinishRouteMoveScrubInsertParams{ActivateID: activateID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, RouteMoveID: sql.NullString{String: routeMoveID, Valid: true}, AuthorityPrincipal: authorityPrincipal, Generation: generation, At: fixedStamp(at)})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishRouteMoveScrubMark(ctx context.Context, failureJSON []byte, activateID string, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64) (int64, error) {
	return q.queries.AdapterWorkerFinishRouteMoveScrubMark(ctx, sqlitegen.AdapterWorkerFinishRouteMoveScrubMarkParams{FailureJSON: string(failureJSON), ActivateID: sql.NullString{String: activateID, Valid: true}, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishOriginRouteMoveScrubMarkDone(ctx context.Context, failureJSON []byte, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, jobID string) (int64, error) {
	return q.queries.AdapterWorkerFinishOriginRouteMoveScrubMarkDone(ctx, sqlitegen.AdapterWorkerFinishOriginRouteMoveScrubMarkDoneParams{FailureJSON: string(failureJSON), TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation, JobID: sql.NullString{String: jobID, Valid: true}})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishOriginRouteMoveScrubCountPending(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerFinishOriginRouteMoveScrubCountPending(ctx, sqlitegen.AdapterWorkerFinishOriginRouteMoveScrubCountPendingParams{RouteMoveID: sql.NullString{String: routeMoveID, Valid: true}, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishOriginRouteMoveScrubActivateMove(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerFinishOriginRouteMoveScrubActivateMove(ctx, sqlitegen.AdapterWorkerFinishOriginRouteMoveScrubActivateMoveParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishOriginRouteMoveScrubTargetQuery(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) ([]adapterWorkerFinishOriginRouteMoveScrubTargetQueryRow, error) {
	value, err := q.queries.AdapterWorkerFinishOriginRouteMoveScrubTargetQuery(ctx, sqlitegen.AdapterWorkerFinishOriginRouteMoveScrubTargetQueryParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject})
	if err != nil {
		return nil, err
	}
	return mapRows(value, func(c sqlitegen.AdapterWorkerFinishOriginRouteMoveScrubTargetQueryRow) (adapterWorkerFinishOriginRouteMoveScrubTargetQueryRow, error) {
		return adapterWorkerFinishOriginRouteMoveScrubTargetQueryRow(c), nil
	})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishOriginRouteMoveScrubInsert(ctx context.Context, activateID string, chainOrg string, chainProject string, environment string, effectID string, routeMoveID string, authority string, generation int64, at time.Time) (int64, error) {
	return q.queries.AdapterWorkerFinishOriginRouteMoveScrubInsert(ctx, sqlitegen.AdapterWorkerFinishOriginRouteMoveScrubInsertParams{ActivateID: activateID, ChainOrg: chainOrg, ChainProject: chainProject, Environment: environment, EffectID: effectID, RouteMoveID: sql.NullString{String: routeMoveID, Valid: true}, Authority: authority, Generation: generation, At: fixedStamp(at)})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishOriginRouteMoveScrubMark(ctx context.Context, activateID string, effectID string, chainOrg string, chainProject string, environment string, generation int64) (int64, error) {
	return q.queries.AdapterWorkerFinishOriginRouteMoveScrubMark(ctx, sqlitegen.AdapterWorkerFinishOriginRouteMoveScrubMarkParams{ActivateID: sql.NullString{String: activateID, Valid: true}, EffectID: effectID, ChainOrg: chainOrg, ChainProject: chainProject, Environment: environment, Generation: generation})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishDeadCredentialScrubLookup(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (string, error) {
	return q.queries.AdapterWorkerFinishDeadCredentialScrubLookup(ctx, sqlitegen.AdapterWorkerFinishDeadCredentialScrubLookupParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishDeadCredentialScrubReleaseLedger(ctx context.Context, at time.Time, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error) {
	return q.queries.AdapterWorkerFinishDeadCredentialScrubReleaseLedger(ctx, sqlitegen.AdapterWorkerFinishDeadCredentialScrubReleaseLedgerParams{At: fixedStamp(at), TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishDeadCredentialScrubMarkTarget(ctx context.Context, failureJSON []byte, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64) (int64, error) {
	return q.queries.AdapterWorkerFinishDeadCredentialScrubMarkTarget(ctx, sqlitegen.AdapterWorkerFinishDeadCredentialScrubMarkTargetParams{FailureJSON: string(failureJSON), TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerFinishDeadCredentialScrubErase(ctx context.Context, adapterID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerFinishDeadCredentialScrubErase(ctx, sqlitegen.AdapterWorkerFinishDeadCredentialScrubEraseParams{AdapterID: adapterID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q sqliteAdapterRuntimeQueries) adapterWorkerCheckProviderSwitchQuery(ctx context.Context) (int64, error) {
	return q.queries.AdapterWorkerCheckProviderSwitchQuery(ctx)
}

type pgAdapterRuntimeQueries struct{ queries *pggen.Queries }

func (q pgAdapterRuntimeQueries) adapterWorkerLoadExecutionQuery(ctx context.Context, jobID string, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, leaseOwner string) (adapterWorkerLoadExecutionQueryRow, error) {
	value, err := q.queries.AdapterWorkerLoadExecutionQuery(ctx, pggen.AdapterWorkerLoadExecutionQueryParams{JobID: jobID, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation, LeaseOwner: pgtype.Text{String: leaseOwner, Valid: true}})
	if err != nil {
		return adapterWorkerLoadExecutionQueryRow{}, err
	}
	return adapterWorkerLoadExecutionQueryRow{Provider: value.Provider, Origin: value.Origin, ID: value.ID, CredentialCiphertext: value.CredentialCiphertext, DestinationKind: value.DestinationKind, DestinationOwner: value.DestinationOwner, DestinationName: value.DestinationName, DestinationEnvironment: value.DestinationEnvironment, DestinationID: value.DestinationID, RepositoryID: value.RepositoryID, Visibility: value.Visibility, SelectedRepositoryIds: value.SelectedRepositoryIds, NamePrefix: value.NamePrefix, Generation: value.Generation, SpkiPin: value.SpkiPin, CaBundlePem: value.CaBundlePem, AllowPersonalToken: int64(value.AllowPersonalToken), DestinationScope: value.DestinationScope, VariableProtected: int64(value.VariableProtected), VariableHidden: int64(value.VariableHidden), VariableExpand: int64(value.VariableExpand)}, nil
}
func (q pgAdapterRuntimeQueries) adapterWorkerLoadExecutionLedgerQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) ([]adapterWorkerLoadExecutionLedgerQueryRow, error) {
	value, err := q.queries.AdapterWorkerLoadExecutionLedgerQuery(ctx, pggen.AdapterWorkerLoadExecutionLedgerQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
	if err != nil {
		return nil, err
	}
	return mapRows(value, func(c pggen.AdapterWorkerLoadExecutionLedgerQueryRow) (adapterWorkerLoadExecutionLedgerQueryRow, error) {
		return adapterWorkerLoadExecutionLedgerQueryRow(c), nil
	})
}
func (q pgAdapterRuntimeQueries) adapterWorkerLoadExecutionSnapshotQuery(ctx context.Context, chainOrg string, chainProject string, chainEnv string) (adapterWorkerLoadExecutionSnapshotQueryRow, error) {
	value, err := q.queries.AdapterWorkerLoadExecutionSnapshotQuery(ctx, pggen.AdapterWorkerLoadExecutionSnapshotQueryParams{ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
	if err != nil {
		return adapterWorkerLoadExecutionSnapshotQueryRow{}, err
	}
	return adapterWorkerLoadExecutionSnapshotQueryRow(value), nil
}
func (q pgAdapterRuntimeQueries) adapterWorkerLoadExecutionEntryQuery(ctx context.Context, targetID string, snapshotID string, chainOrg string, chainProject string, chainEnv string) ([]adapterWorkerLoadExecutionEntryQueryRow, error) {
	value, err := q.queries.AdapterWorkerLoadExecutionEntryQuery(ctx, pggen.AdapterWorkerLoadExecutionEntryQueryParams{TargetID: targetID, SnapshotID: snapshotID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
	if err != nil {
		return nil, err
	}
	return mapRows(value, func(c pggen.AdapterWorkerLoadExecutionEntryQueryRow) (adapterWorkerLoadExecutionEntryQueryRow, error) {
		return adapterWorkerLoadExecutionEntryQueryRow(c), nil
	})
}
func (q pgAdapterRuntimeQueries) adapterWorkerLoadActivationQuery(ctx context.Context, jobID string, routeMoveID string, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, leaseOwner string) (adapterWorkerLoadActivationQueryRow, error) {
	value, err := q.queries.AdapterWorkerLoadActivationQuery(ctx, pggen.AdapterWorkerLoadActivationQueryParams{JobID: jobID, RouteMoveID: pgtype.Text{String: routeMoveID, Valid: true}, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation, LeaseOwner: pgtype.Text{String: leaseOwner, Valid: true}})
	if err != nil {
		return adapterWorkerLoadActivationQueryRow{}, err
	}
	return adapterWorkerLoadActivationQueryRow{Provider: value.Provider, PendingOrigin: value.PendingOrigin, ID: value.ID, PendingCredentialCiphertext: value.PendingCredentialCiphertext, EnvironmentID: value.EnvironmentID, DestinationKind: value.DestinationKind, DestinationOwner: value.DestinationOwner, DestinationName: value.DestinationName, DestinationEnvironment: value.DestinationEnvironment, DestinationID: value.DestinationID, RepositoryID: value.RepositoryID, Visibility: value.Visibility, SelectedRepositoryIds: value.SelectedRepositoryIds, NamePrefix: value.NamePrefix, Generation: value.Generation, SpkiPin: value.SpkiPin, CaBundlePem: value.CaBundlePem, AllowPersonalToken: int64(value.AllowPersonalToken), DestinationScope: value.DestinationScope, VariableProtected: int64(value.VariableProtected), VariableHidden: int64(value.VariableHidden), VariableExpand: int64(value.VariableExpand)}, nil
}
func (q pgAdapterRuntimeQueries) adapterWorkerTryEnqueueLookup(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, now time.Time) (int64, error) {
	return q.queries.AdapterWorkerTryEnqueueLookup(ctx, pggen.AdapterWorkerTryEnqueueLookupParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Now: pgRequiredTime(now)})
}
func (q pgAdapterRuntimeQueries) adapterWorkerTryEnqueueExistsQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error) {
	return q.queries.AdapterWorkerTryEnqueueExistsQuery(ctx, pggen.AdapterWorkerTryEnqueueExistsQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q pgAdapterRuntimeQueries) adapterWorkerTryEnqueueDepthQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error) {
	return q.queries.AdapterWorkerTryEnqueueDepthQuery(ctx, pggen.AdapterWorkerTryEnqueueDepthQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q pgAdapterRuntimeQueries) adapterWorkerTryEnqueuePreviousQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (string, error) {
	return q.queries.AdapterWorkerTryEnqueuePreviousQuery(ctx, pggen.AdapterWorkerTryEnqueuePreviousQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q pgAdapterRuntimeQueries) adapterWorkerTryEnqueueSupersede(ctx context.Context, at time.Time, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error) {
	return q.queries.AdapterWorkerTryEnqueueSupersede(ctx, pggen.AdapterWorkerTryEnqueueSupersedeParams{At: pgRequiredTime(at), TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q pgAdapterRuntimeQueries) adapterWorkerTryEnqueueInsert(ctx context.Context, jobID string, chainOrg string, chainProject string, chainEnv string, targetID string, kind string, authorityPrincipal string, generation int64, at time.Time) (int64, error) {
	return q.queries.AdapterWorkerTryEnqueueInsert(ctx, pggen.AdapterWorkerTryEnqueueInsertParams{JobID: jobID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Kind: kind, AuthorityPrincipal: authorityPrincipal, Generation: generation, At: pgRequiredTime(at)})
}
func (q pgAdapterRuntimeQueries) adapterWorkerTryEnqueueUpdate(ctx context.Context, generation int64, state string, jobID string, targetID string, chainOrg string, chainProject string, chainEnv string, expectedGeneration int64, at time.Time) (int64, error) {
	return q.queries.AdapterWorkerTryEnqueueUpdate(ctx, pggen.AdapterWorkerTryEnqueueUpdateParams{Generation: generation, State: state, JobID: pgtype.Text{String: jobID, Valid: true}, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, ExpectedGeneration: expectedGeneration, At: pgRequiredTime(at)})
}
func (q pgAdapterRuntimeQueries) adapterWorkerClaimDueSelectQuery(ctx context.Context, now time.Time) (adapterWorkerClaimDueSelectQueryRow, error) {
	value, err := q.queries.AdapterWorkerClaimDueSelectQuery(ctx, pgRequiredTime(now))
	if err != nil {
		return adapterWorkerClaimDueSelectQueryRow{}, err
	}
	return adapterWorkerClaimDueSelectQueryRow{ID: value.ID, OrgID: value.OrgID, ProjectID: value.ProjectID, EnvironmentID: value.EnvironmentID, TargetID: value.TargetID, Kind: value.Kind, RouteMoveID: value.RouteMoveID, AuthorityPrincipalID: value.AuthorityPrincipalID, Generation: value.Generation, AttemptCount: int64(value.AttemptCount), CreatedAt: pgStoredStamp(value.CreatedAt)}, nil
}
func (q pgAdapterRuntimeQueries) adapterWorkerClaimDueUpdate(ctx context.Context, attempt int64, worker string, leaseUntil time.Time, jobID string) (int64, error) {
	checkedAttempt, err := checkedPGInt32(int64(attempt))
	if err != nil {
		return 0, err
	}
	return q.queries.AdapterWorkerClaimDueUpdate(ctx, pggen.AdapterWorkerClaimDueUpdateParams{Attempt: checkedAttempt, Worker: pgtype.Text{String: worker, Valid: true}, LeaseUntil: pgRequiredTime(leaseUntil), JobID: jobID})
}
func (q pgAdapterRuntimeQueries) adapterWorkerCloseIndeterminateEffectsQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, at time.Time) ([]adapterWorkerCloseIndeterminateEffectsQueryRow, error) {
	value, err := q.queries.AdapterWorkerCloseIndeterminateEffectsQuery(ctx, pggen.AdapterWorkerCloseIndeterminateEffectsQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, At: pgRequiredTime(at)})
	if err != nil {
		return nil, err
	}
	return mapRows(value, func(c pggen.AdapterWorkerCloseIndeterminateEffectsQueryRow) (adapterWorkerCloseIndeterminateEffectsQueryRow, error) {
		return adapterWorkerCloseIndeterminateEffectsQueryRow(c), nil
	})
}
func (q pgAdapterRuntimeQueries) adapterWorkerCloseIndeterminateEffectsUpdate(ctx context.Context, outcomeAuditID string, at time.Time, effectID string, chainOrg string, chainProject string, chainEnv string, targetID string) (int64, error) {
	return q.queries.AdapterWorkerCloseIndeterminateEffectsUpdate(ctx, pggen.AdapterWorkerCloseIndeterminateEffectsUpdateParams{OutcomeAuditID: pgtype.Text{String: outcomeAuditID, Valid: true}, At: pgRequiredTime(at), EffectID: effectID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID})
}
func (q pgAdapterRuntimeQueries) adapterWorkerCloseIndeterminateEffectsRelease(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, effectID string) (int64, error) {
	return q.queries.AdapterWorkerCloseIndeterminateEffectsRelease(ctx, pggen.AdapterWorkerCloseIndeterminateEffectsReleaseParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, EffectID: pgtype.Text{String: effectID, Valid: true}})
}
func (q pgAdapterRuntimeQueries) adapterWorkerGateQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, jobID string, leaseOwner string, now time.Time) (int64, error) {
	return q.queries.AdapterWorkerGateQuery(ctx, pggen.AdapterWorkerGateQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation, JobID: jobID, LeaseOwner: pgtype.Text{String: leaseOwner, Valid: true}, Now: pgRequiredTime(now)})
}
func (q pgAdapterRuntimeQueries) adapterWorkerReservePendingQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, surface string, normalized string) (int64, error) {
	return q.queries.AdapterWorkerReservePendingQuery(ctx, pggen.AdapterWorkerReservePendingQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Surface: surface, Normalized: normalized})
}
func (q pgAdapterRuntimeQueries) adapterWorkerReserveSelectQuery(ctx context.Context, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalized string) (string, error) {
	return q.queries.AdapterWorkerReserveSelectQuery(ctx, pggen.AdapterWorkerReserveSelectQueryParams{ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Surface: surface, Normalized: normalized})
}
func (q pgAdapterRuntimeQueries) adapterWorkerReserveCurrentRoute(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (adapterWorkerReserveCurrentRouteRow, error) {
	value, err := q.queries.AdapterWorkerReserveCurrentRoute(ctx, pggen.AdapterWorkerReserveCurrentRouteParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
	if err != nil {
		return adapterWorkerReserveCurrentRouteRow{}, err
	}
	return adapterWorkerReserveCurrentRouteRow(value), nil
}
func (q pgAdapterRuntimeQueries) adapterWorkerReserveReactivate(ctx context.Context, effectiveName string, origin string, destinationKind string, repositoryID int64, destinationID int64, destinationScope string, now time.Time, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalized string) (int64, error) {
	return q.queries.AdapterWorkerReserveReactivate(ctx, pggen.AdapterWorkerReserveReactivateParams{EffectiveName: effectiveName, Origin: origin, DestinationKind: destinationKind, RepositoryID: repositoryID, DestinationID: destinationID, DestinationScope: destinationScope, Now: pgRequiredTime(now), ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Surface: surface, Normalized: normalized})
}
func (q pgAdapterRuntimeQueries) adapterWorkerReserveCountQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error) {
	return q.queries.AdapterWorkerReserveCountQuery(ctx, pggen.AdapterWorkerReserveCountQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q pgAdapterRuntimeQueries) adapterWorkerReserveLookup(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (adapterWorkerReserveLookupRow, error) {
	value, err := q.queries.AdapterWorkerReserveLookup(ctx, pggen.AdapterWorkerReserveLookupParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
	if err != nil {
		return adapterWorkerReserveLookupRow{}, err
	}
	return adapterWorkerReserveLookupRow(value), nil
}
func (q pgAdapterRuntimeQueries) adapterWorkerReserveInsert(ctx context.Context, newID string, chainOrg string, chainProject string, chainEnv string, targetID string, origin string, destinationKind string, repositoryID int64, destinationID int64, destinationScope string, surface string, effectiveName string, normalized string, reserved string, now time.Time) (int64, error) {
	return q.queries.AdapterWorkerReserveInsert(ctx, pggen.AdapterWorkerReserveInsertParams{NewID: newID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Origin: origin, DestinationKind: destinationKind, RepositoryID: repositoryID, DestinationID: destinationID, DestinationScope: destinationScope, Surface: surface, EffectiveName: effectiveName, Normalized: normalized, Reserved: reserved, Now: pgRequiredTime(now)})
}
func (q pgAdapterRuntimeQueries) adapterWorkerPrepareProviderLease(ctx context.Context, jobID string, effectID string, leaseUntil time.Time, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, nowStamp time.Time) (int64, error) {
	return q.queries.AdapterWorkerPrepareProviderLease(ctx, pggen.AdapterWorkerPrepareProviderLeaseParams{JobID: pgtype.Text{String: jobID, Valid: true}, EffectID: pgtype.Text{String: effectID, Valid: true}, LeaseUntil: pgRequiredTime(leaseUntil), TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation, NowStamp: pgRequiredTime(nowStamp)})
}
func (q pgAdapterRuntimeQueries) adapterWorkerPrepareLease(ctx context.Context, leaseUntil time.Time, jobID string, leaseOwner string) (int64, error) {
	return q.queries.AdapterWorkerPrepareLease(ctx, pggen.AdapterWorkerPrepareLeaseParams{LeaseUntil: pgRequiredTime(leaseUntil), JobID: jobID, LeaseOwner: pgtype.Text{String: leaseOwner, Valid: true}})
}
func (q pgAdapterRuntimeQueries) adapterWorkerPrepareUpdate(ctx context.Context, now time.Time, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalizedName string) (int64, error) {
	return q.queries.AdapterWorkerPrepareUpdate(ctx, pggen.AdapterWorkerPrepareUpdateParams{Now: pgRequiredTime(now), ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Surface: surface, NormalizedName: normalizedName})
}
func (q pgAdapterRuntimeQueries) adapterWorkerPrepareInsert(ctx context.Context, effectID string, chainOrg string, chainProject string, chainEnv string, targetID string, jobID string, surface string, effectiveName string, disposition string, intentID string, now time.Time) (int64, error) {
	return q.queries.AdapterWorkerPrepareInsert(ctx, pggen.AdapterWorkerPrepareInsertParams{EffectID: effectID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, JobID: jobID, Surface: surface, EffectiveName: effectiveName, Disposition: disposition, IntentID: intentID, Now: pgRequiredTime(now)})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishUpdateEffect(ctx context.Context, outcomeAuditID string, outcome string, finding string, now time.Time, effectID string) (int64, error) {
	return q.queries.AdapterWorkerFinishUpdateEffect(ctx, pggen.AdapterWorkerFinishUpdateEffectParams{OutcomeAuditID: pgtype.Text{String: outcomeAuditID, Valid: true}, Outcome: pgtype.Text{String: outcome, Valid: true}, Finding: finding, Now: pgRequiredTime(now), EffectID: effectID})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishRemove(ctx context.Context, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalizedName string) (int64, error) {
	return q.queries.AdapterWorkerFinishRemove(ctx, pggen.AdapterWorkerFinishRemoveParams{ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Surface: surface, NormalizedName: normalizedName})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishUpdate(ctx context.Context, state string, missing bool, now time.Time, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalizedName string) (int64, error) {
	return q.queries.AdapterWorkerFinishUpdate(ctx, pggen.AdapterWorkerFinishUpdateParams{State: state, Missing: missing, Now: pgRequiredTime(now), ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Surface: surface, NormalizedName: normalizedName})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishReleaseLease(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, jobID string, effectID string) (int64, error) {
	return q.queries.AdapterWorkerFinishReleaseLease(ctx, pggen.AdapterWorkerFinishReleaseLeaseParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, JobID: pgtype.Text{String: jobID, Valid: true}, EffectID: pgtype.Text{String: effectID, Valid: true}})
}
func (q pgAdapterRuntimeQueries) adapterWorkerRefuseRemove(ctx context.Context, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalizedName string) (int64, error) {
	return q.queries.AdapterWorkerRefuseRemove(ctx, pggen.AdapterWorkerRefuseRemoveParams{ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Surface: surface, NormalizedName: normalizedName})
}
func (q pgAdapterRuntimeQueries) adapterWorkerReleaseReservationRemove(ctx context.Context, chainOrg string, chainProject string, chainEnv string, targetID string, surface string, normalizedName string, generation int64, jobID string, leaseOwner string, now time.Time) (int64, error) {
	return q.queries.AdapterWorkerReleaseReservationRemove(ctx, pggen.AdapterWorkerReleaseReservationRemoveParams{ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, Surface: surface, NormalizedName: normalizedName, Generation: generation, JobID: jobID, LeaseOwner: pgtype.Text{String: leaseOwner, Valid: true}, Now: pgRequiredTime(now)})
}
func (q pgAdapterRuntimeQueries) adapterWorkerInsertConflictDedup(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, surface string, effectiveName string) (int64, error) {
	return q.queries.AdapterWorkerInsertConflictDedup(ctx, pggen.AdapterWorkerInsertConflictDedupParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation, Surface: surface, EffectiveName: effectiveName})
}
func (q pgAdapterRuntimeQueries) adapterWorkerInsertConflictInsert(ctx context.Context, newID string, artifactID string, chainOrg string, chainProject string, chainEnv string, targetID string, jobID string, generation int64, surface string, effectiveName string, now time.Time) (int64, error) {
	return q.queries.AdapterWorkerInsertConflictInsert(ctx, pggen.AdapterWorkerInsertConflictInsertParams{NewID: newID, ArtifactID: artifactID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, JobID: pgtype.Text{String: jobID, Valid: true}, Generation: generation, Surface: surface, EffectiveName: effectiveName, Now: pgRequiredTime(now)})
}
func (q pgAdapterRuntimeQueries) adapterWorkerInsertAdapterJobAuditWithIDQuery(ctx context.Context, effectID string, typ string, at time.Time, authorityPrincipal string, chainOrg string, chainProject string, chainEnv string, targetID string, outcome string, jobID string, payload string) (int64, error) {
	return q.queries.AdapterWorkerInsertAdapterJobAuditWithIDQuery(ctx, pggen.AdapterWorkerInsertAdapterJobAuditWithIDQueryParams{EffectID: effectID, Typ: typ, At: pgRequiredTime(at), AuthorityPrincipal: pgtype.Text{String: authorityPrincipal, Valid: true}, ChainOrg: chainOrg, ChainProject: pgtype.Text{String: chainProject, Valid: true}, ChainEnv: pgtype.Text{String: chainEnv, Valid: true}, TargetID: pgtype.Text{String: targetID, Valid: true}, Outcome: outcome, JobID: pgtype.Text{String: jobID, Valid: true}, Payload: payload})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateFinish(ctx context.Context, at time.Time, jobID string, routeMoveID string, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, leaseOwner string) (int64, error) {
	return q.queries.AdapterWorkerActivateFinish(ctx, pggen.AdapterWorkerActivateFinishParams{At: pgRequiredTime(at), JobID: jobID, RouteMoveID: pgtype.Text{String: routeMoveID, Valid: true}, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation, LeaseOwner: pgtype.Text{String: leaseOwner, Valid: true}})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateLookup(ctx context.Context, routeMoveID string, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64) (adapterWorkerActivateLookupRow, error) {
	value, err := q.queries.AdapterWorkerActivateLookup(ctx, pggen.AdapterWorkerActivateLookupParams{RouteMoveID: routeMoveID, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation})
	if err != nil {
		return adapterWorkerActivateLookupRow{}, err
	}
	return adapterWorkerActivateLookupRow(value), nil
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateCollisionQuery(ctx context.Context, routeMoveID string, targetID string, pendingOrigin string, kind string, repositoryID int64, destinationID int64, sentinelName string) (int64, error) {
	value, err := q.queries.AdapterWorkerActivateCollisionQuery(ctx, pggen.AdapterWorkerActivateCollisionQueryParams{RouteMoveID: routeMoveID, TargetID: targetID, PendingOrigin: pendingOrigin, Kind: kind, RepositoryID: repositoryID, DestinationID: destinationID, SentinelName: sentinelName})
	return int64(value), err
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateSetResolved(ctx context.Context, destinationID int64, repositoryID int64, routeMoveID string, targetID string, chainOrg string, chainProject string, pendingEnvironment string) (int64, error) {
	return q.queries.AdapterWorkerActivateSetResolved(ctx, pggen.AdapterWorkerActivateSetResolvedParams{DestinationID: destinationID, RepositoryID: repositoryID, RouteMoveID: routeMoveID, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, PendingEnvironment: pendingEnvironment})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateDeleteKeys(ctx context.Context, targetID string, chainOrg string, chainProject string, currentEnvironment string) (int64, error) {
	return q.queries.AdapterWorkerActivateDeleteKeys(ctx, pggen.AdapterWorkerActivateDeleteKeysParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, CurrentEnvironment: currentEnvironment})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateInsertKeys(ctx context.Context, adapterID string, routeMoveID string, targetID string, chainOrg string, chainProject string, pendingEnvironment string) (int64, error) {
	return q.queries.AdapterWorkerActivateInsertKeys(ctx, pggen.AdapterWorkerActivateInsertKeysParams{AdapterID: adapterID, RouteMoveID: routeMoveID, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, PendingEnvironment: pendingEnvironment})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateInsertJob(ctx context.Context, convergeID string, chainOrg string, chainProject string, pendingEnvironment string, targetID string, routeMoveID string, authorityPrincipal string, generation int64, at time.Time) (int64, error) {
	return q.queries.AdapterWorkerActivateInsertJob(ctx, pggen.AdapterWorkerActivateInsertJobParams{ConvergeID: convergeID, ChainOrg: chainOrg, ChainProject: chainProject, PendingEnvironment: pendingEnvironment, TargetID: targetID, RouteMoveID: pgtype.Text{String: routeMoveID, Valid: true}, AuthorityPrincipal: authorityPrincipal, Generation: generation, At: pgRequiredTime(at)})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateApplyTarget(ctx context.Context, kind string, owner string, name string, destinationEnvironment string, destinationID int64, repositoryID int64, visibility string, selectedRaw []byte, prefix string, generation int64, expectedGeneration int64, convergeID string, targetID string, chainOrg string, chainProject string, currentEnvironment string) (int64, error) {
	return q.queries.AdapterWorkerActivateApplyTarget(ctx, pggen.AdapterWorkerActivateApplyTargetParams{Kind: kind, Owner: owner, Name: name, DestinationEnvironment: destinationEnvironment, DestinationID: destinationID, RepositoryID: repositoryID, Visibility: visibility, SelectedRaw: selectedRaw, Prefix: prefix, Generation: generation, ExpectedGeneration: expectedGeneration, ConvergeID: pgtype.Text{String: convergeID, Valid: true}, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, CurrentEnvironment: currentEnvironment})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateUpdateExpiry(ctx context.Context, credentialExpiresAt time.Time, adapterID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerActivateUpdateExpiry(ctx, pggen.AdapterWorkerActivateUpdateExpiryParams{CredentialExpiresAt: pgRequiredTime(credentialExpiresAt), AdapterID: adapterID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateDeleteClaims(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerActivateDeleteClaims(ctx, pggen.AdapterWorkerActivateDeleteClaimsParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateCompleteMove(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, targetID string) (int64, error) {
	return q.queries.AdapterWorkerActivateCompleteMove(ctx, pggen.AdapterWorkerActivateCompleteMoveParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject, TargetID: pgtype.Text{String: targetID, Valid: true}})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveMarkProbe(ctx context.Context, targetID string, adapterID string, chainOrg string, chainProject string, chainEnv string, generation int64, jobID string) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveMarkProbe(ctx, pggen.AdapterWorkerActivateOriginRouteMoveMarkProbeParams{TargetID: targetID, AdapterID: adapterID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation, JobID: pgtype.Text{String: jobID, Valid: true}})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveCountUnresolved(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveCountUnresolved(ctx, pggen.AdapterWorkerActivateOriginRouteMoveCountUnresolvedParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveLoadPending(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, adapterID string) (adapterWorkerActivateOriginRouteMoveLoadPendingRow, error) {
	value, err := q.queries.AdapterWorkerActivateOriginRouteMoveLoadPending(ctx, pggen.AdapterWorkerActivateOriginRouteMoveLoadPendingParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject, AdapterID: adapterID})
	if err != nil {
		return adapterWorkerActivateOriginRouteMoveLoadPendingRow{}, err
	}
	return adapterWorkerActivateOriginRouteMoveLoadPendingRow{PendingOrigin: sql.NullString{String: value.PendingOrigin.String, Valid: value.PendingOrigin.Valid}, PendingCredentialCiphertext: value.PendingCredentialCiphertext}, nil
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveTargetQuery(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, adapterID string) ([]adapterWorkerActivateOriginRouteMoveTargetQueryRow, error) {
	value, err := q.queries.AdapterWorkerActivateOriginRouteMoveTargetQuery(ctx, pggen.AdapterWorkerActivateOriginRouteMoveTargetQueryParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject, AdapterID: adapterID})
	if err != nil {
		return nil, err
	}
	return mapRows(value, func(c pggen.AdapterWorkerActivateOriginRouteMoveTargetQueryRow) (adapterWorkerActivateOriginRouteMoveTargetQueryRow, error) {
		return adapterWorkerActivateOriginRouteMoveTargetQueryRow(c), nil
	})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveDeleteKeys(ctx context.Context, effectID string, chainOrg string, chainProject string, environment string) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveDeleteKeys(ctx, pggen.AdapterWorkerActivateOriginRouteMoveDeleteKeysParams{EffectID: effectID, ChainOrg: chainOrg, ChainProject: chainProject, Environment: environment})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveInsertKeys(ctx context.Context, adapterID string, routeMoveID string, effectID string, chainOrg string, chainProject string, environment string) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveInsertKeys(ctx, pggen.AdapterWorkerActivateOriginRouteMoveInsertKeysParams{AdapterID: adapterID, RouteMoveID: routeMoveID, EffectID: effectID, ChainOrg: chainOrg, ChainProject: chainProject, Environment: environment})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveInsertJob(ctx context.Context, convergeID string, chainOrg string, chainProject string, environment string, effectID string, routeMoveID string, authorityPrincipal string, generation int64, at time.Time) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveInsertJob(ctx, pggen.AdapterWorkerActivateOriginRouteMoveInsertJobParams{ConvergeID: convergeID, ChainOrg: chainOrg, ChainProject: chainProject, Environment: environment, EffectID: effectID, RouteMoveID: pgtype.Text{String: routeMoveID, Valid: true}, AuthorityPrincipal: authorityPrincipal, Generation: generation, At: pgRequiredTime(at)})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveApplyTarget(ctx context.Context, kind string, owner string, name string, destinationEnvironment string, destinationID int64, repositoryID int64, visibility string, selectedRaw []byte, prefix string, generation int64, expectedGeneration int64, convergeID string, effectID string, adapterID string, chainOrg string, chainProject string, environment string) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveApplyTarget(ctx, pggen.AdapterWorkerActivateOriginRouteMoveApplyTargetParams{Kind: kind, Owner: owner, Name: name, DestinationEnvironment: destinationEnvironment, DestinationID: destinationID, RepositoryID: repositoryID, Visibility: visibility, SelectedRaw: selectedRaw, Prefix: prefix, Generation: generation, ExpectedGeneration: expectedGeneration, ConvergeID: pgtype.Text{String: convergeID, Valid: true}, EffectID: effectID, AdapterID: adapterID, ChainOrg: chainOrg, ChainProject: chainProject, Environment: environment})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveActivateAdapter(ctx context.Context, pendingOrigin string, pendingCredential []byte, at time.Time, expires *time.Time, adapterID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveActivateAdapter(ctx, pggen.AdapterWorkerActivateOriginRouteMoveActivateAdapterParams{PendingOrigin: pendingOrigin, PendingCredential: pendingCredential, At: pgRequiredTime(at), Expires: pgStampPtr(expires), AdapterID: adapterID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveDeleteClaims(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveDeleteClaims(ctx, pggen.AdapterWorkerActivateOriginRouteMoveDeleteClaimsParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q pgAdapterRuntimeQueries) adapterWorkerActivateOriginRouteMoveCompleteMove(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, adapterID string) (int64, error) {
	return q.queries.AdapterWorkerActivateOriginRouteMoveCompleteMove(ctx, pggen.AdapterWorkerActivateOriginRouteMoveCompleteMoveParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject, AdapterID: adapterID})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishJobQuery(ctx context.Context, due time.Time, job adapter.Job) (int64, error) {
	return q.queries.AdapterWorkerFinishJobQuery(ctx, pggen.AdapterWorkerFinishJobQueryParams{Due: pgRequiredTime(due), JobID: job.ID, LeaseOwner: pgtype.Text{String: job.LeaseOwner, Valid: true}, ChainOrg: job.OrgID, ChainProject: job.ProjectID, ChainEnv: job.EnvironmentID, TargetID: job.TargetID, Generation: job.Generation, AuthorityPrincipal: job.AuthorityPrincipal, Kind: string(job.Kind), RouteMoveID: job.RouteMoveID})
}
func (q pgAdapterRuntimeQueries) adapterWorkerCompleteJob(ctx context.Context, state string, at time.Time, job adapter.Job) (int64, error) {
	return q.queries.AdapterWorkerCompleteJob(ctx, pggen.AdapterWorkerCompleteJobParams{State: state, At: pgRequiredTime(at), JobID: job.ID, LeaseOwner: pgtype.Text{String: job.LeaseOwner, Valid: true}, ChainOrg: job.OrgID, ChainProject: job.ProjectID, ChainEnv: job.EnvironmentID, TargetID: job.TargetID, Generation: job.Generation, AuthorityPrincipal: job.AuthorityPrincipal, Kind: string(job.Kind), RouteMoveID: job.RouteMoveID})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishJobLookup(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (string, error) {
	return q.queries.AdapterWorkerFinishJobLookup(ctx, pggen.AdapterWorkerFinishJobLookupParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishJobMarkTarget(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64) (int64, error) {
	return q.queries.AdapterWorkerFinishJobMarkTarget(ctx, pggen.AdapterWorkerFinishJobMarkTargetParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishJobErase(ctx context.Context, adapterID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerFinishJobErase(ctx, pggen.AdapterWorkerFinishJobEraseParams{AdapterID: adapterID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishJobAttention(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerFinishJobAttention(ctx, pggen.AdapterWorkerFinishJobAttentionParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishJobSupersede(ctx context.Context, at time.Time, routeMoveID string, chainOrg string, chainProject string, jobID string) (int64, error) {
	return q.queries.AdapterWorkerFinishJobSupersede(ctx, pggen.AdapterWorkerFinishJobSupersedeParams{At: pgRequiredTime(at), RouteMoveID: pgtype.Text{String: routeMoveID, Valid: true}, ChainOrg: chainOrg, ChainProject: chainProject, JobID: jobID})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishJobMarkTargets(ctx context.Context, chainOrg string, chainProject string, routeMoveID string) (int64, error) {
	return q.queries.AdapterWorkerFinishJobMarkTargets(ctx, pggen.AdapterWorkerFinishJobMarkTargetsParams{ChainOrg: chainOrg, ChainProject: chainProject, RouteMoveID: routeMoveID})
}
func (q pgAdapterRuntimeQueries) adapterWorkerRecordJobOutcome(ctx context.Context, targetStatus string, convergedRevision int64, convergedRev sql.NullInt64, failureJSON []byte, warningJSON []byte, revision int64, rev sql.NullInt64, attemptedAt time.Time, errorClass sql.NullString, attentionMode int64, retainActiveJob int64, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64) (int64, error) {
	checkedAttentionMode, err := checkedPGInt32(int64(attentionMode))
	if err != nil {
		return 0, err
	}
	checkedRetainActiveJob, err := checkedPGInt32(int64(retainActiveJob))
	if err != nil {
		return 0, err
	}
	return q.queries.AdapterWorkerRecordJobOutcome(ctx, pggen.AdapterWorkerRecordJobOutcomeParams{TargetStatus: targetStatus, ConvergedRevision: convergedRevision, ConvergedRev: pgtype.Int8{Int64: convergedRev.Int64, Valid: convergedRev.Valid}, FailureJSON: failureJSON, WarningJSON: warningJSON, Revision: revision, Rev: pgtype.Int8{Int64: rev.Int64, Valid: rev.Valid}, AttemptedAt: pgRequiredTime(attemptedAt), ErrorClass: pgtype.Text{String: errorClass.String, Valid: errorClass.Valid}, AttentionMode: checkedAttentionMode, RetainActiveJob: checkedRetainActiveJob, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation})
}
func (q pgAdapterRuntimeQueries) adapterWorkerRaiseDriftAttentionQuery(ctx context.Context, targetID string, org string, project string, environmentID string) (int64, error) {
	return q.queries.AdapterWorkerRaiseDriftAttentionQuery(ctx, pggen.AdapterWorkerRaiseDriftAttentionQueryParams{TargetID: targetID, Org: org, Project: project, EnvironmentID: environmentID})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishRouteMoveScrubLookupMove(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, targetID string, chainEnv string) (string, error) {
	return q.queries.AdapterWorkerFinishRouteMoveScrubLookupMove(ctx, pggen.AdapterWorkerFinishRouteMoveScrubLookupMoveParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject, TargetID: targetID, ChainEnv: chainEnv})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishRouteMoveScrubActiveLedgerQuery(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error) {
	return q.queries.AdapterWorkerFinishRouteMoveScrubActiveLedgerQuery(ctx, pggen.AdapterWorkerFinishRouteMoveScrubActiveLedgerQueryParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishRouteMoveScrubPersistOrphans(ctx context.Context, orphanJSON []byte, routeMoveID string, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error) {
	return q.queries.AdapterWorkerFinishRouteMoveScrubPersistOrphans(ctx, pggen.AdapterWorkerFinishRouteMoveScrubPersistOrphansParams{OrphanJSON: orphanJSON, RouteMoveID: routeMoveID, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishRouteMoveScrubMove(ctx context.Context, routeMoveID string, chainOrg string, chainProject string, targetID string) (int64, error) {
	return q.queries.AdapterWorkerFinishRouteMoveScrubMove(ctx, pggen.AdapterWorkerFinishRouteMoveScrubMoveParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject, TargetID: pgtype.Text{String: targetID, Valid: true}})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishRouteMoveScrubInsert(ctx context.Context, activateID string, chainOrg string, chainProject string, chainEnv string, targetID string, routeMoveID string, authorityPrincipal string, generation int64, at time.Time) (int64, error) {
	return q.queries.AdapterWorkerFinishRouteMoveScrubInsert(ctx, pggen.AdapterWorkerFinishRouteMoveScrubInsertParams{ActivateID: activateID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, TargetID: targetID, RouteMoveID: pgtype.Text{String: routeMoveID, Valid: true}, AuthorityPrincipal: authorityPrincipal, Generation: generation, At: pgRequiredTime(at)})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishRouteMoveScrubMark(ctx context.Context, failureJSON []byte, activateID string, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64) (int64, error) {
	return q.queries.AdapterWorkerFinishRouteMoveScrubMark(ctx, pggen.AdapterWorkerFinishRouteMoveScrubMarkParams{FailureJSON: failureJSON, ActivateID: pgtype.Text{String: activateID, Valid: true}, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishOriginRouteMoveScrubMarkDone(ctx context.Context, failureJSON []byte, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64, jobID string) (int64, error) {
	return q.queries.AdapterWorkerFinishOriginRouteMoveScrubMarkDone(ctx, pggen.AdapterWorkerFinishOriginRouteMoveScrubMarkDoneParams{FailureJSON: failureJSON, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation, JobID: pgtype.Text{String: jobID, Valid: true}})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishOriginRouteMoveScrubCountPending(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerFinishOriginRouteMoveScrubCountPending(ctx, pggen.AdapterWorkerFinishOriginRouteMoveScrubCountPendingParams{RouteMoveID: pgtype.Text{String: routeMoveID, Valid: true}, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishOriginRouteMoveScrubActivateMove(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerFinishOriginRouteMoveScrubActivateMove(ctx, pggen.AdapterWorkerFinishOriginRouteMoveScrubActivateMoveParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishOriginRouteMoveScrubTargetQuery(ctx context.Context, routeMoveID string, chainOrg string, chainProject string) ([]adapterWorkerFinishOriginRouteMoveScrubTargetQueryRow, error) {
	value, err := q.queries.AdapterWorkerFinishOriginRouteMoveScrubTargetQuery(ctx, pggen.AdapterWorkerFinishOriginRouteMoveScrubTargetQueryParams{RouteMoveID: routeMoveID, ChainOrg: chainOrg, ChainProject: chainProject})
	if err != nil {
		return nil, err
	}
	return mapRows(value, func(c pggen.AdapterWorkerFinishOriginRouteMoveScrubTargetQueryRow) (adapterWorkerFinishOriginRouteMoveScrubTargetQueryRow, error) {
		return adapterWorkerFinishOriginRouteMoveScrubTargetQueryRow(c), nil
	})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishOriginRouteMoveScrubInsert(ctx context.Context, activateID string, chainOrg string, chainProject string, environment string, effectID string, routeMoveID string, authority string, generation int64, at time.Time) (int64, error) {
	return q.queries.AdapterWorkerFinishOriginRouteMoveScrubInsert(ctx, pggen.AdapterWorkerFinishOriginRouteMoveScrubInsertParams{ActivateID: activateID, ChainOrg: chainOrg, ChainProject: chainProject, Environment: environment, EffectID: effectID, RouteMoveID: pgtype.Text{String: routeMoveID, Valid: true}, Authority: authority, Generation: generation, At: pgRequiredTime(at)})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishOriginRouteMoveScrubMark(ctx context.Context, activateID string, effectID string, chainOrg string, chainProject string, environment string, generation int64) (int64, error) {
	return q.queries.AdapterWorkerFinishOriginRouteMoveScrubMark(ctx, pggen.AdapterWorkerFinishOriginRouteMoveScrubMarkParams{ActivateID: pgtype.Text{String: activateID, Valid: true}, EffectID: effectID, ChainOrg: chainOrg, ChainProject: chainProject, Environment: environment, Generation: generation})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishDeadCredentialScrubLookup(ctx context.Context, targetID string, chainOrg string, chainProject string, chainEnv string) (string, error) {
	return q.queries.AdapterWorkerFinishDeadCredentialScrubLookup(ctx, pggen.AdapterWorkerFinishDeadCredentialScrubLookupParams{TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishDeadCredentialScrubReleaseLedger(ctx context.Context, at time.Time, targetID string, chainOrg string, chainProject string, chainEnv string) (int64, error) {
	return q.queries.AdapterWorkerFinishDeadCredentialScrubReleaseLedger(ctx, pggen.AdapterWorkerFinishDeadCredentialScrubReleaseLedgerParams{At: pgRequiredTime(at), TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishDeadCredentialScrubMarkTarget(ctx context.Context, failureJSON []byte, targetID string, chainOrg string, chainProject string, chainEnv string, generation int64) (int64, error) {
	return q.queries.AdapterWorkerFinishDeadCredentialScrubMarkTarget(ctx, pggen.AdapterWorkerFinishDeadCredentialScrubMarkTargetParams{FailureJSON: failureJSON, TargetID: targetID, ChainOrg: chainOrg, ChainProject: chainProject, ChainEnv: chainEnv, Generation: generation})
}
func (q pgAdapterRuntimeQueries) adapterWorkerFinishDeadCredentialScrubErase(ctx context.Context, adapterID string, chainOrg string, chainProject string) (int64, error) {
	return q.queries.AdapterWorkerFinishDeadCredentialScrubErase(ctx, pggen.AdapterWorkerFinishDeadCredentialScrubEraseParams{AdapterID: adapterID, ChainOrg: chainOrg, ChainProject: chainProject})
}
func (q pgAdapterRuntimeQueries) adapterWorkerCheckProviderSwitchQuery(ctx context.Context) (int64, error) {
	value, err := q.queries.AdapterWorkerCheckProviderSwitchQuery(ctx)
	return int64(value), err
}

func (s sqliteAdoptDB) adapterRuntimeQueries() adapterRuntimeQueries {
	return sqliteAdapterRuntimeQueries{queries: sqlitegen.New(s.db)}
}
func (s sqliteAdapterTx) adapterRuntimeQueries() adapterRuntimeQueries {
	return sqliteAdapterRuntimeQueries{queries: sqlitegen.New(s.tx)}
}
func (s pgAdoptDB) adapterRuntimeQueries() adapterRuntimeQueries {
	return pgAdapterRuntimeQueries{queries: pggen.New(s.db)}
}
func (s pgAdapterTx) adapterRuntimeQueries() adapterRuntimeQueries {
	return pgAdapterRuntimeQueries{queries: pggen.New(s.tx)}
}
