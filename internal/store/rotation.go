package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
)

// Scheduled credential rotation request-path store (#148, slice S1). These
// methods are proof-carrying like the dynamic-secret surface: every one verifies
// its proof against its registered store operation and binds the tenant chain
// from the verified proof. No row holds a secret.

// Rotation states. paused is an operator or cascade pause, halted is the
// executor's named stop after a failed authority gate (S2 writes it), disabled is
// the soft delete. Resume leaves paused or halted only through the full resume
// authorization.
const (
	RotationStateActive   = "active"
	RotationStatePaused   = "paused"
	RotationStateHalted   = "halted"
	RotationStateDisabled = "disabled"
	// RotationStateRestoreHeld is what the executor sees in place of the state of a
	// rotation a restore holds (RotationRecord.RestoreHold): not active, so it starts
	// and extends nothing, whatever the stored state is. It is never stored.
	RotationStateRestoreHeld = "restore_hold"
)

var (
	// ErrRotationKeyInUse refuses a rotation whose password or username key is
	// already claimed by another live rotation, whichever role the key plays
	// there.
	ErrRotationKeyInUse = fmt.Errorf("%w: a key is already driven by another live rotation", ErrConflict)
	// ErrRotationLiveInEnvironment refuses an environment delete while a rotation
	// in it is not disabled or has a run that may hold a live role: deleting the
	// environment would orphan a role that nothing would ever revoke.
	ErrRotationLiveInEnvironment = fmt.Errorf("%w: the environment still has a live credential rotation or a run that may hold a live role", ErrConflict)
)

// RotationLiveError is ErrRotationLiveInEnvironment naming what blocks the
// delete: the ids of rotations that are not disabled, and of rotations whose
// runs may still hold a live role. Ids are non-secret.
type RotationLiveError struct {
	LiveRotationIDs   []string
	BlockingRunOwners []string
	// Truncated reports that more rotations block than the limit names: each list
	// was read one past RotationBlockingIDsLimit, so the flag is exact.
	Truncated bool
}

func (e *RotationLiveError) Error() string { return ErrRotationLiveInEnvironment.Error() }
func (e *RotationLiveError) Unwrap() error { return ErrRotationLiveInEnvironment }

// RotationBlockingIDsLimit bounds how many blocking rotation ids a refusal names.
const RotationBlockingIDsLimit = 10

// RotationListMaxLimit bounds one page of a rotation list.
const RotationListMaxLimit = 500

// RotationRecord is one rotation's metadata surface. CredentialExpiresAt is the
// VALID UNTIL of the credential currently delivered into its keys (the latest
// published run for the key, across rotations), or empty when no run has
// published or the role never expires.
type RotationRecord struct {
	ID                     string
	EnvironmentID          string
	ProviderID             string
	KeyID                  string
	UsernameKeyID          string
	IntervalSeconds        int64
	RevocationDelaySeconds int64
	AuthorityPrincipalID   string
	CreatedBy              string
	State                  string
	NextRunAt              string
	Generation             int64
	CreatedAt              string
	UpdatedAt              string
	CredentialExpiresAt    string
	// RestoreHold is set by a restore and cleared by ReleaseHold: a held rotation
	// starts no run and takes no pre-publish step, whatever its state.
	RestoreHold bool
	// HaltReason is the stored reason a halted rotation stopped, exactly as the
	// column holds it (empty unless the state is halted). It is a short code the
	// executor wrote, but an archive may carry any text, so a transport maps it to a
	// fixed word (service.rotationHaltWord) and never passes it on.
	RawHaltReason string
	// LastRun is the newest run by id, the top row of the runs list; nil when the
	// rotation has never run. Set by Get and List, not by the cascade pauses.
	LastRun *RotationLastRun
	// OwedDropRuns counts the rotation's runs that still owe a provider role a drop:
	// phase revoke_refused or quarantine, over its whole history. It is the
	// definition the CLI ATTENTION lines, the browser notice and the
	// hikyo_rotation_runs_{revoke_refused,quarantined} gauges use. Set by Get and List.
	OwedDropRuns int64
	// OwedDropRunsVersionsChanged is the part of OwedDropRuns a restore refused with
	// the published_versions_changed code: the keys may be serving that role again,
	// so it must never be read as "drop it".
	OwedDropRunsVersionsChanged int64
}

// RotationLastRun is the summary of a rotation's newest run: metadata only, with
// the fixed-word error code the runs list already carries.
type RotationLastRun struct {
	ID        string
	Phase     string
	ErrorCode string
	PhaseAt   string
}

// RotationCreate is one rotation configuration. The environment is bound from
// the verified proof chain, never from this struct.
type RotationCreate struct {
	ID                     string
	ProviderID             string
	KeyID                  string
	UsernameKeyID          string
	IntervalSeconds        int64
	RevocationDelaySeconds int64
	AuthorityPrincipalID   string
	NextRunAt              time.Time
	At                     time.Time
}

// RotationSetState is one compare-and-set state change. A zero NextRunAt stores
// NULL (only an active rotation carries a next run).
type RotationSetState struct {
	RotationID         string
	State              string
	NextRunAt          time.Time
	ExpectedGeneration int64
	At                 time.Time
}

// RotationListQuery is one page of a rotation list.
type RotationListQuery struct {
	IncludeDisabled bool
	// KeepOwed lists, without IncludeDisabled, also a deleted rotation that owns a run
	// still owing a provider role a drop. The API's default list sets it; the
	// environment delete's "live rotations" read does not.
	KeepOwed bool
	After    string
	Limit    int
}

// RotationRunRequest is one run-now request: the run the rotation will start (its
// id is chosen by the caller, so the audit row can name it) and whether it skips
// the overlap. The rotation must be active at ExpectedGeneration with no pending
// request and no open run.
type RotationRunRequest struct {
	RotationID         string
	RunID              string
	SkipOverlap        bool
	ExpectedGeneration int64
	At                 time.Time
}

// ErrRotationRunPending is a run-now request refused because the rotation already
// has one waiting for the executor.
var ErrRotationRunPending = fmt.Errorf("%w: a run is already requested", ErrConflict)

// RotationRunQuery is one page of a rotation's runs, newest first; After is the
// last run id of the previous page.
type RotationRunQuery struct {
	RotationID string
	After      string
	Limit      int
}

// RotationRunRecord is one run's operator-visible metadata. It carries no role
// handle, candidate or provider detail: ErrorCode is the fixed-word code of the
// last failure. NotConvergedAdapterTargets is nil until a revoke decided.
type RotationRunRecord struct {
	ID                         string
	Trigger                    string
	Phase                      string
	PublishedRevision          int64
	RevocationDelaySeconds     int64
	StartedAt                  string
	PhaseAt                    string
	ErrorCode                  string
	NotConvergedAdapterTargets *int64
	// CredentialValidUntil is when the role a revoke_refused or quarantine run still
	// owes a drop lapses on its own: the superseded role's for a run that published
	// (its candidate is the serving credential and must never be dropped by hand),
	// the candidate's for a run that never did. Empty for every other phase or when no
	// expiry was recorded.
	CredentialValidUntil string
	// CredentialValidUntilLowerBound reports that an extend of that role has an
	// open or unknown outcome, so the role may outlive CredentialValidUntil: the
	// recorded expiry is "at least", not a fact.
	CredentialValidUntilLowerBound bool
}

// RotationAuthorityRef is the identifiers-and-state view of one live rotation
// that the incident enumeration lists by its recorded authority principal. It
// carries no key, provider or credential detail.
type RotationAuthorityRef struct {
	ID, OrgID, ProjectID, EnvironmentID, State string
}

// RotationReader is the read side.
type RotationReader interface {
	// ListByAuthorityInOrg and ListByAuthorityInInstance page the live (not
	// deleted) rotations whose recorded authority principal is authority, in id
	// order, for the incident enumeration: one org's under an org proof, every
	// org's under an instance proof. The limit is at most domain.MaxIncidentPage.
	ListByAuthorityInOrg(ctx context.Context, p authz.Proof, authority domain.PrincipalID, afterID string, limit int) ([]RotationAuthorityRef, error)
	ListByAuthorityInInstance(ctx context.Context, p authz.Proof, authority domain.PrincipalID, afterID string, limit int) ([]RotationAuthorityRef, error)
	Get(ctx context.Context, p authz.Proof, rotationID string) (RotationRecord, error)
	// Summarize fills, in place, each row's last run and owed-drop counts (the runs
	// that need an operator: revoke_refused and quarantine) with two reads for the
	// whole slice. Get and List leave them empty: only a response needs them.
	Summarize(ctx context.Context, p authz.Proof, rows []RotationRecord) error
	// List returns up to Limit of the environment's rotations whose id sorts after
	// After, in id order; disabled history only with IncludeDisabled. Each carries
	// its delivered-credential expiry, read for the whole page in one query.
	List(ctx context.Context, p authz.Proof, q RotationListQuery) ([]RotationRecord, error)
	// CountOpenRuns counts the rotation's non-terminal runs.
	CountOpenRuns(ctx context.Context, p authz.Proof, rotationID string) (int64, error)
	// ListRuns returns up to Limit of the rotation's runs, newest first.
	ListRuns(ctx context.Context, p authz.Proof, q RotationRunQuery) ([]RotationRunRecord, error)
}

// RotationRepo is the write side plus the reads.
type RotationRepo interface {
	RotationReader
	Create(ctx context.Context, p authz.Proof, m RotationCreate) (RotationRecord, error)
	// SetState moves a rotation between active, paused and disabled, bumping its
	// generation. A stale expected generation, or a disabled rotation, is
	// ErrConflict; a missing rotation is ErrNotFound.
	SetState(ctx context.Context, p authz.Proof, m RotationSetState) (RotationRecord, error)
	// RequestRun records a run-now request and makes the rotation due now. A
	// rotation that is not active at the expected generation is ErrConflict; one
	// with a request already waiting is ErrRotationRunPending. The open-run check
	// is the service's: it counts again after this write, which holds the row the
	// executor locks to start a run, and rolls the request back if one opened.
	RequestRun(ctx context.Context, p authz.Proof, m RotationRunRequest) error
	// ReleaseHold clears the restore hold of a held rotation. It reports
	// ErrConflict when the rotation is not held (or is gone). The generation is
	// not bumped, so a run in flight is not invalidated.
	ReleaseHold(ctx context.Context, p authz.Proof, rotationID string, at time.Time) error
	// HaltForIncident halts one active or paused rotation with a named reason,
	// bumping its generation so a run past its gate settles against a stale fence.
	// It reports false, with no error, when the rotation is already halted or
	// deleted, or held by a restore: the incident ledger then reads its state. A rotation outside the proof's environment is ErrNotFound.
	HaltForIncident(ctx context.Context, p authz.Proof, rotationID, reason string, at time.Time) (bool, error)
	// PauseForProvider pauses every active rotation that mints from the provider,
	// in the proof's project, and returns them as paused. It is the provider
	// delete and credential revoke cascade: a rotation never stays active against
	// a provider that can no longer mint.
	PauseForProvider(ctx context.Context, p authz.Proof, providerID string, at time.Time) ([]RotationRecord, error)
	// PauseForEnvironment pauses every active rotation of one environment of the
	// proof's project, or of every environment of the project when environmentID is
	// empty, and returns them as paused. It is the protect and approval-policy
	// cascade: a rotation publishes unattended, so it never stays active in an
	// environment whose publishes now need a ceremony or an approval.
	PauseForEnvironment(ctx context.Context, p authz.Proof, environmentID string, at time.Time) ([]RotationRecord, error)
	// PurgeEnvironment removes every rotation row of the proof's environment as
	// part of the environment delete, once nothing in it is live. It is a
	// *RotationLiveError (ErrRotationLiveInEnvironment) otherwise: a rotation not
	// yet disabled, or a run that may hold a live role (every phase but done, and a
	// failed run that had already published).
	PurgeEnvironment(ctx context.Context, p authz.Proof) error
	// BeginRunPublish is the first write of a run's publish transaction: a fenced
	// touch of the run row under its crash fence. It takes the row lock the whole
	// publish holds, so a takeover worker's fenced claim write waits for the
	// commit or rollback (crash-safety rule 8). A run that is no longer minted, or
	// whose fence was lost, is ErrRotationFenceLost.
	BeginRunPublish(ctx context.Context, p authz.Proof, run RotationRun, now time.Time) error
	// RecordRunPublish records the publish in the same transaction as the revision:
	// the run moves minted to published with its revision and its overlap
	// deadline, the publish effect closes, and the rotation's next run is anchored
	// at the commit time.
	RecordRunPublish(ctx context.Context, p authz.Proof, rec RotationPublishRecord) error
	// AbandonOpenRuns closes every unsettled run of the rotation (quarantine, or
	// done for a published run that owes no revoke) and returns exactly the runs
	// that statement changed, so the service's audit rows match them one to one. A
	// run still in phase published owes a revoke only after delay from at: its
	// stored deadline was read before the publish committed, so it is replaced by
	// at plus the rotation's revocation delay (an observation after the commit).
	AbandonOpenRuns(ctx context.Context, p authz.Proof, rotationID string, at time.Time, delay time.Duration) ([]AbandonedRun, error)
	// ProviderLiveRuns counts runs on a provider that may still hold a live role
	// or owe a revoke, so a provider delete can refuse.
	ProviderLiveRuns(ctx context.Context, p authz.Proof, providerID string) (int64, error)
}

// RotationPublishRecord is one run's publish, written beside the revision it
// published.
type RotationPublishRecord struct {
	Run       RotationRun
	Revision  int64
	Now       time.Time
	NextRunAt time.Time
	// NextAttempt is when the overlap ends: the earliest the revoke may run.
	NextAttempt time.Time
	Outcome     RotationEffectOutcome
}

// AbandonedRun is one run an abandon closed, as the statement that closed it
// returned it. Phase is the phase it was closed into: quarantine, or done for a
// published run that owes no revoke.
type AbandonedRun struct {
	ID               string
	Phase            string
	CandidateHandle  string
	SupersededHandle string
}

type rotationStoreQueries interface {
	create(ctx context.Context, chain domain.Scope, m RotationCreate) (int64, error)
	get(ctx context.Context, chain domain.Scope, rotationID string) (RotationRecord, error)
	list(ctx context.Context, chain domain.Scope, q RotationListQuery) ([]RotationRecord, error)
	setState(ctx context.Context, chain domain.Scope, m RotationSetState) (int64, error)
	halt(ctx context.Context, chain domain.Scope, rotationID, reason string, at time.Time) (int64, error)
	listByAuthorityInOrg(ctx context.Context, chain domain.Scope, authority domain.PrincipalID, afterID string, limit int) ([]RotationAuthorityRef, error)
	listByAuthorityInInstance(ctx context.Context, authority domain.PrincipalID, afterID string, limit int) ([]RotationAuthorityRef, error)
	countOpenRuns(ctx context.Context, chain domain.Scope, rotationID string) (int64, error)
	requestRun(ctx context.Context, chain domain.Scope, m RotationRunRequest) (int64, error)
	releaseHold(ctx context.Context, chain domain.Scope, rotationID string, at time.Time) (int64, error)
	manualPending(ctx context.Context, chain domain.Scope, rotationID string) (bool, error)
	listRuns(ctx context.Context, chain domain.Scope, q RotationRunQuery) ([]RotationRunRecord, error)
	// mintedExpiry is the recorded VALID UNTIL of the role a handle names, read from
	// the run that minted it under the chain (never the rotation, so a recreated
	// rotation finds its predecessor's role), and whether an extend of it is unsettled.
	mintedExpiry(ctx context.Context, chain domain.Scope, handle string) (until string, uncertain bool, err error)
	credentialExpiry(ctx context.Context, chain domain.Scope, keyID string) (string, error)
	// credentialExpiries returns the delivered-credential expiry per key, from the
	// latest published run of each key, in one read.
	credentialExpiries(ctx context.Context, chain domain.Scope, keyIDs []string) (map[string]string, error)
	// lastRuns returns the newest run of each of the rotations that has one, and
	// owedDropCounts the runs owing a drop (and the share a restore refused), each in
	// one read for the whole page.
	lastRuns(ctx context.Context, chain domain.Scope, rotationIDs []string) (map[string]RotationLastRun, error)
	owedDropCounts(ctx context.Context, chain domain.Scope, rotationIDs []string) (map[string][2]int64, error)
	// claim takes one key for a rotation; a taken key is reported by
	// uniqueViolation, never by constraint text.
	claim(ctx context.Context, chain domain.Scope, keyID, rotationID string) (uniqueViolation bool, err error)
	releaseClaims(ctx context.Context, chain domain.Scope, rotationID string) error
	// blockingRunRotations names, up to limit, the distinct rotations that have a
	// run that may hold a live role: any phase but done and failed, or failed after
	// publish.
	blockingRunRotations(ctx context.Context, chain domain.Scope, limit int) ([]string, error)
	purge(ctx context.Context, chain domain.Scope) error
	activeByProvider(ctx context.Context, chain domain.Scope, providerID string) ([]RotationRecord, error)
	activeInEnvironment(ctx context.Context, chain domain.Scope, environmentID string) ([]RotationRecord, error)
	activeInProject(ctx context.Context, chain domain.Scope) ([]RotationRecord, error)
	pauseActive(ctx context.Context, chain domain.Scope, rotationID string, at time.Time) (int64, error)
	abandonRuns(ctx context.Context, chain domain.Scope, rotationID string, at, publishedDue time.Time) ([]AbandonedRun, error)
	providerLiveRuns(ctx context.Context, chain domain.Scope, providerID string) (int64, error)
	runtime() rotationRuntimeQueries
}

type rotationQueries struct {
	queries rotationStoreQueries
	tok     *authz.TxToken
}

func (r sqliteRepos) Rotations() RotationRepo {
	return rotationQueries{queries: sqliteRotationQueries{q: sqlitegen.New(r.db)}, tok: r.tok}
}

func (r pgRepos) Rotations() RotationRepo {
	return rotationQueries{queries: pgRotationQueries{q: pggen.New(r.db)}, tok: r.tok}
}

func (q sqliteRotationQueries) runtime() rotationRuntimeQueries {
	return sqliteRotationRuntimeQueries{q: q.q}
}
func (q pgRotationQueries) runtime() rotationRuntimeQueries { return pgRotationRuntimeQueries{q: q.q} }

func (r rotationQueries) envChain(p authz.Proof, op authz.StoreOp) (domain.Scope, error) {
	chain, err := authz.Verify(p, op, r.tok)
	if err != nil {
		return domain.Scope{}, err
	}
	if chain.Env == "" {
		return domain.Scope{}, ErrNotFound
	}
	return chain, nil
}

func (r rotationQueries) Create(ctx context.Context, p authz.Proof, m RotationCreate) (RotationRecord, error) {
	chain, err := r.envChain(p, authz.StoreRotationsCreate)
	if err != nil {
		return RotationRecord{}, err
	}
	if _, err := r.queries.create(ctx, chain, m); err != nil {
		return RotationRecord{}, err
	}
	// The claims are the cross-role exclusion: only a unique violation on a claim
	// insert means "in use", so a foreign key or CHECK failure is never relabelled.
	for _, keyID := range []string{m.KeyID, m.UsernameKeyID} {
		taken, err := r.queries.claim(ctx, chain, keyID, m.ID)
		if taken {
			return RotationRecord{}, ErrRotationKeyInUse
		}
		if err != nil {
			return RotationRecord{}, err
		}
	}
	return r.get(ctx, chain, m.ID)
}

// get reads one rotation with its delivered-credential expiry.
func (r rotationQueries) get(ctx context.Context, chain domain.Scope, rotationID string) (RotationRecord, error) {
	rec, err := r.queries.get(ctx, chain, rotationID)
	if err != nil {
		return RotationRecord{}, err
	}
	rec, err = r.withExpiry(ctx, chain, rec)
	if err != nil {
		return RotationRecord{}, err
	}
	return rec, nil
}

// Summarize fills the last run and the owed-drop counts of the rotations a response
// returns, from two reads for the whole slice whatever its length. It is not part of
// Get, List or any mutation: the executor's gates and the existence checks read a
// rotation on every sweep and must not pay for a summary no transport shows, so the
// service asks for it once, for the rotations it returns, after trimming a page.
func (r rotationQueries) Summarize(ctx context.Context, p authz.Proof, rows []RotationRecord) error {
	chain, err := r.envChain(p, authz.StoreRotationsSummarize)
	if err != nil {
		return err
	}
	return r.summarize(ctx, chain, rows)
}

// summarize is Summarize after the proof: two reads for the slice, whatever its length.
func (r rotationQueries) summarize(ctx context.Context, chain domain.Scope, rows []RotationRecord) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]string, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	last, err := r.queries.lastRuns(ctx, chain, ids)
	if err != nil {
		return err
	}
	owed, err := r.queries.owedDropCounts(ctx, chain, ids)
	if err != nil {
		return err
	}
	for i := range rows {
		if run, ok := last[rows[i].ID]; ok {
			rows[i].LastRun = &run
		}
		counts := owed[rows[i].ID]
		rows[i].OwedDropRuns, rows[i].OwedDropRunsVersionsChanged = counts[0], counts[1]
	}
	return nil
}

func (r rotationQueries) withExpiry(ctx context.Context, chain domain.Scope, rec RotationRecord) (RotationRecord, error) {
	expires, err := r.queries.credentialExpiry(ctx, chain, rec.KeyID)
	if err != nil {
		return RotationRecord{}, err
	}
	rec.CredentialExpiresAt = expires
	return rec, nil
}

func (r rotationQueries) Get(ctx context.Context, p authz.Proof, rotationID string) (RotationRecord, error) {
	chain, err := r.envChain(p, authz.StoreRotationsGet)
	if err != nil {
		return RotationRecord{}, err
	}
	return r.get(ctx, chain, rotationID)
}

func (r rotationQueries) List(ctx context.Context, p authz.Proof, q RotationListQuery) ([]RotationRecord, error) {
	chain, err := r.envChain(p, authz.StoreRotationsList)
	if err != nil {
		return nil, err
	}
	if q.Limit <= 0 || q.Limit > RotationListMaxLimit+1 {
		return nil, fmt.Errorf("%w: rotation list limit out of range", domain.ErrInvalid)
	}
	return r.page(ctx, chain, q)
}

// page is one page of rotations with their delivered-credential expiry, from one
// read each; it carries no run summary (see Summarize).
func (r rotationQueries) page(ctx context.Context, chain domain.Scope, q RotationListQuery) ([]RotationRecord, error) {
	rows, err := r.queries.list(ctx, chain, q)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return rows, nil
	}
	keyIDs := make([]string, len(rows))
	for i := range rows {
		keyIDs[i] = rows[i].KeyID
	}
	expiries, err := r.queries.credentialExpiries(ctx, chain, keyIDs)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].CredentialExpiresAt = expiries[rows[i].KeyID]
	}
	return rows, nil
}

func (r rotationQueries) SetState(ctx context.Context, p authz.Proof, m RotationSetState) (RotationRecord, error) {
	chain, err := r.envChain(p, authz.StoreRotationsSetState)
	if err != nil {
		return RotationRecord{}, err
	}
	rows, err := r.queries.setState(ctx, chain, m)
	if err != nil {
		return RotationRecord{}, err
	}
	if rows != 1 {
		// Missing is NotFound; a stale generation or a disabled rotation is a
		// conflict the caller can see and retry from a fresh read.
		if _, getErr := r.queries.get(ctx, chain, m.RotationID); errors.Is(getErr, ErrNotFound) {
			return RotationRecord{}, ErrNotFound
		}
		return RotationRecord{}, ErrConflict
	}
	if m.State == RotationStateDisabled {
		// A disabled rotation is history: its keys are free for a new rotation.
		if err := r.queries.releaseClaims(ctx, chain, m.RotationID); err != nil {
			return RotationRecord{}, err
		}
	}
	return r.get(ctx, chain, m.RotationID)
}

func (r rotationQueries) ListByAuthorityInOrg(ctx context.Context, p authz.Proof, authority domain.PrincipalID, afterID string, limit int) ([]RotationAuthorityRef, error) {
	chain, err := authz.Verify(p, authz.StoreRotationsListByAuthorityInOrg, r.tok)
	if err != nil {
		return nil, err
	}
	if chain.Org == "" || chain.Project != "" || authority == "" || limit < 1 || limit > domain.MaxIncidentPage {
		return nil, ErrNotFound
	}
	return r.queries.listByAuthorityInOrg(ctx, chain, authority, afterID, limit)
}

func (r rotationQueries) ListByAuthorityInInstance(ctx context.Context, p authz.Proof, authority domain.PrincipalID, afterID string, limit int) ([]RotationAuthorityRef, error) {
	chain, err := authz.Verify(p, authz.StoreRotationsListByAuthorityInInstance, r.tok)
	if err != nil {
		return nil, err
	}
	if chain != (domain.Scope{}) || authority == "" || limit < 1 || limit > domain.MaxIncidentPage {
		return nil, ErrNotFound
	}
	return r.queries.listByAuthorityInInstance(ctx, authority, afterID, limit)
}

func (r rotationQueries) HaltForIncident(ctx context.Context, p authz.Proof, rotationID, reason string, at time.Time) (bool, error) {
	chain, err := r.envChain(p, authz.StoreRotationsHaltForIncident)
	if err != nil {
		return false, err
	}
	n, err := r.queries.halt(ctx, chain, rotationID, reason, at)
	if err != nil {
		return false, err
	}
	if n == 1 {
		return true, nil
	}
	if _, err := r.queries.get(ctx, chain, rotationID); err != nil {
		return false, err
	}
	return false, nil
}

func (r rotationQueries) PauseForProvider(ctx context.Context, p authz.Proof, providerID string, at time.Time) ([]RotationRecord, error) {
	chain, err := authz.Verify(p, authz.StoreRotationsPauseForProvider, r.tok)
	if err != nil {
		return nil, err
	}
	if chain.Project == "" {
		return nil, ErrNotFound
	}
	active, err := r.queries.activeByProvider(ctx, chain, providerID)
	if err != nil {
		return nil, err
	}
	return r.pauseAll(ctx, chain, active, at)
}

func (r rotationQueries) PauseForEnvironment(ctx context.Context, p authz.Proof, environmentID string, at time.Time) ([]RotationRecord, error) {
	chain, err := authz.Verify(p, authz.StoreRotationsPauseForEnvironment, r.tok)
	if err != nil {
		return nil, err
	}
	if chain.Project == "" || (chain.Env != "" && environmentID != string(chain.Env)) {
		return nil, ErrNotFound
	}
	var active []RotationRecord
	if environmentID == "" {
		active, err = r.queries.activeInProject(ctx, chain)
	} else {
		active, err = r.queries.activeInEnvironment(ctx, chain, environmentID)
	}
	if err != nil {
		return nil, err
	}
	return r.pauseAll(ctx, chain, active, at)
}

// pauseAll pauses each still-active rotation of the list in the verified
// project; a rotation another writer already moved is skipped.
func (r rotationQueries) pauseAll(ctx context.Context, chain domain.Scope, active []RotationRecord, at time.Time) ([]RotationRecord, error) {
	paused := make([]RotationRecord, 0, len(active))
	for _, rec := range active {
		rows, err := r.queries.pauseActive(ctx, chain, rec.ID, at)
		if err != nil {
			return nil, err
		}
		if rows != 1 {
			continue
		}
		rec.State = RotationStatePaused
		rec.NextRunAt = ""
		rec.Generation++
		rec.UpdatedAt = CanonTime(at).Format(timeFormat)
		paused = append(paused, rec)
	}
	return paused, nil
}

func (r rotationQueries) PurgeEnvironment(ctx context.Context, p authz.Proof) error {
	chain, err := r.envChain(p, authz.StoreRotationsPurgeEnvironment)
	if err != nil {
		return err
	}
	live, err := r.queries.list(ctx, chain, RotationListQuery{Limit: RotationBlockingIDsLimit + 1})
	if err != nil {
		return err
	}
	blocking, err := r.queries.blockingRunRotations(ctx, chain, RotationBlockingIDsLimit+1)
	if err != nil {
		return err
	}
	if len(live) == 0 && len(blocking) == 0 {
		return r.queries.purge(ctx, chain)
	}
	out := &RotationLiveError{Truncated: len(live) > RotationBlockingIDsLimit || len(blocking) > RotationBlockingIDsLimit}
	for i, rec := range live {
		if i < RotationBlockingIDsLimit {
			out.LiveRotationIDs = append(out.LiveRotationIDs, rec.ID)
		}
	}
	out.BlockingRunOwners = blocking[:min(len(blocking), RotationBlockingIDsLimit)]
	return out
}

func (r rotationQueries) CountOpenRuns(ctx context.Context, p authz.Proof, rotationID string) (int64, error) {
	chain, err := r.envChain(p, authz.StoreRotationsCountOpenRuns)
	if err != nil {
		return 0, err
	}
	return r.queries.countOpenRuns(ctx, chain, rotationID)
}

func (r rotationQueries) RequestRun(ctx context.Context, p authz.Proof, m RotationRunRequest) error {
	chain, err := r.envChain(p, authz.StoreRotationsRequestRun)
	if err != nil {
		return err
	}
	rows, err := r.queries.requestRun(ctx, chain, m)
	if err != nil {
		return err
	}
	if rows == 1 {
		return nil
	}
	pending, err := r.queries.manualPending(ctx, chain, m.RotationID)
	if err != nil {
		return err
	}
	if pending {
		return ErrRotationRunPending
	}
	return ErrConflict
}

func (r rotationQueries) ReleaseHold(ctx context.Context, p authz.Proof, rotationID string, at time.Time) error {
	chain, err := r.envChain(p, authz.StoreRotationsReleaseHold)
	if err != nil {
		return err
	}
	rows, err := r.queries.releaseHold(ctx, chain, rotationID, at)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrConflict
	}
	return nil
}

func (r rotationQueries) ListRuns(ctx context.Context, p authz.Proof, q RotationRunQuery) ([]RotationRunRecord, error) {
	chain, err := r.envChain(p, authz.StoreRotationsListRuns)
	if err != nil {
		return nil, err
	}
	if q.Limit <= 0 || q.Limit > RotationListMaxLimit+1 {
		return nil, fmt.Errorf("%w: rotation runs limit out of range", domain.ErrInvalid)
	}
	return r.queries.listRuns(ctx, chain, q)
}

type sqliteRotationQueries struct{ q *sqlitegen.Queries }
type pgRotationQueries struct{ q *pggen.Queries }

func sqliteOptStamp(t time.Time) sql.NullString {
	if t.IsZero() {
		return sql.NullString{}
	}
	return runtimeSQLiteStamp(t)
}

func pgOptStamp(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgRequiredTime(t)
}

func (q sqliteRotationQueries) create(ctx context.Context, chain domain.Scope, m RotationCreate) (int64, error) {
	n, err := q.q.RotationCreate(ctx, sqlitegen.RotationCreateParams{
		ID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env),
		ProviderID: m.ProviderID, KeyID: m.KeyID, UsernameKeyID: m.UsernameKeyID,
		IntervalSeconds: m.IntervalSeconds, RevocationDelaySeconds: m.RevocationDelaySeconds,
		AuthorityPrincipalID: m.AuthorityPrincipalID, NextRunAt: sqliteOptStamp(m.NextRunAt), At: fixedStamp(m.At),
	})
	return n, constraint(err)
}

func (q pgRotationQueries) create(ctx context.Context, chain domain.Scope, m RotationCreate) (int64, error) {
	n, err := q.q.RotationCreate(ctx, pggen.RotationCreateParams{
		ID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env),
		ProviderID: m.ProviderID, KeyID: m.KeyID, UsernameKeyID: m.UsernameKeyID,
		IntervalSeconds: m.IntervalSeconds, RevocationDelaySeconds: m.RevocationDelaySeconds,
		AuthorityPrincipalID: m.AuthorityPrincipalID, NextRunAt: pgOptStamp(m.NextRunAt), At: pgRequiredTime(m.At),
	})
	return n, constraint(err)
}

func (q sqliteRotationQueries) get(ctx context.Context, chain domain.Scope, rotationID string) (RotationRecord, error) {
	c, err := q.q.RotationGet(ctx, sqlitegen.RotationGetParams{RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return RotationRecord{}, ErrNotFound
	}
	if err != nil {
		return RotationRecord{}, err
	}
	return sqliteRotation(c)
}

func (q pgRotationQueries) get(ctx context.Context, chain domain.Scope, rotationID string) (RotationRecord, error) {
	c, err := q.q.RotationGet(ctx, pggen.RotationGetParams{RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return RotationRecord{}, ErrNotFound
	}
	if err != nil {
		return RotationRecord{}, err
	}
	return pgRotation(c)
}

func (q sqliteRotationQueries) list(ctx context.Context, chain domain.Scope, lq RotationListQuery) ([]RotationRecord, error) {
	var rows []sqlitegen.RotationGetRow
	if lq.IncludeDisabled {
		all, err := q.q.RotationListAll(ctx, sqlitegen.RotationListAllParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), AfterID: lq.After, PageLimit: int64(lq.Limit)})
		if err != nil {
			return nil, err
		}
		for _, c := range all {
			rows = append(rows, sqlitegen.RotationGetRow(c))
		}
	} else if lq.KeepOwed {
		kept, err := q.q.RotationListOrOwed(ctx, sqlitegen.RotationListOrOwedParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), AfterID: lq.After, PageLimit: int64(lq.Limit)})
		if err != nil {
			return nil, err
		}
		for _, c := range kept {
			rows = append(rows, sqlitegen.RotationGetRow(c))
		}
	} else {
		live, err := q.q.RotationList(ctx, sqlitegen.RotationListParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), AfterID: lq.After, PageLimit: int64(lq.Limit)})
		if err != nil {
			return nil, err
		}
		for _, c := range live {
			rows = append(rows, sqlitegen.RotationGetRow(c))
		}
	}
	return mapRows(rows, sqliteRotation)
}

func (q pgRotationQueries) list(ctx context.Context, chain domain.Scope, lq RotationListQuery) ([]RotationRecord, error) {
	pageLimit, err := checkedPGInt32(int64(lq.Limit))
	if err != nil {
		return nil, err
	}
	var rows []pggen.RotationGetRow
	if lq.IncludeDisabled {
		all, err := q.q.RotationListAll(ctx, pggen.RotationListAllParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), AfterID: lq.After, PageLimit: pageLimit})
		if err != nil {
			return nil, err
		}
		for _, c := range all {
			rows = append(rows, pggen.RotationGetRow(c))
		}
	} else if lq.KeepOwed {
		kept, err := q.q.RotationListOrOwed(ctx, pggen.RotationListOrOwedParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), AfterID: lq.After, PageLimit: pageLimit})
		if err != nil {
			return nil, err
		}
		for _, c := range kept {
			rows = append(rows, pggen.RotationGetRow(c))
		}
	} else {
		live, err := q.q.RotationList(ctx, pggen.RotationListParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), AfterID: lq.After, PageLimit: pageLimit})
		if err != nil {
			return nil, err
		}
		for _, c := range live {
			rows = append(rows, pggen.RotationGetRow(c))
		}
	}
	return mapRows(rows, pgRotation)
}

func (q sqliteRotationQueries) setState(ctx context.Context, chain domain.Scope, m RotationSetState) (int64, error) {
	n, err := q.q.RotationSetState(ctx, sqlitegen.RotationSetStateParams{
		State: m.State, NextRunAt: sqliteOptStamp(m.NextRunAt), At: fixedStamp(m.At), RotationID: m.RotationID,
		ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), ExpectedGeneration: m.ExpectedGeneration,
	})
	return n, constraint(err)
}

func (q pgRotationQueries) setState(ctx context.Context, chain domain.Scope, m RotationSetState) (int64, error) {
	n, err := q.q.RotationSetState(ctx, pggen.RotationSetStateParams{
		State: m.State, NextRunAt: pgOptStamp(m.NextRunAt), At: pgRequiredTime(m.At), RotationID: m.RotationID,
		ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), ExpectedGeneration: m.ExpectedGeneration,
	})
	return n, constraint(err)
}

func (q sqliteRotationQueries) halt(ctx context.Context, chain domain.Scope, rotationID, reason string, at time.Time) (int64, error) {
	n, err := q.q.RotationRtHaltForIncident(ctx, sqlitegen.RotationRtHaltForIncidentParams{HaltReason: sqlText(reason), Now: fixedStamp(at), RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return n, constraint(err)
}

func (q pgRotationQueries) halt(ctx context.Context, chain domain.Scope, rotationID, reason string, at time.Time) (int64, error) {
	n, err := q.q.RotationRtHaltForIncident(ctx, pggen.RotationRtHaltForIncidentParams{HaltReason: pgText(reason), Now: pgRequiredTime(at), RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return n, constraint(err)
}

func (q sqliteRotationQueries) listByAuthorityInOrg(ctx context.Context, chain domain.Scope, authority domain.PrincipalID, afterID string, limit int) ([]RotationAuthorityRef, error) {
	rows, err := q.q.RotationListByAuthorityInOrg(ctx, sqlitegen.RotationListByAuthorityInOrgParams{ChainOrg: string(chain.Org), AuthorityPrincipalID: string(authority), AfterID: afterID, RowLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.RotationListByAuthorityInOrgRow) (RotationAuthorityRef, error) {
		return RotationAuthorityRef{ID: c.ID, OrgID: string(chain.Org), ProjectID: c.ProjectID, EnvironmentID: c.EnvironmentID, State: c.State}, nil
	})
}

func (q pgRotationQueries) listByAuthorityInOrg(ctx context.Context, chain domain.Scope, authority domain.PrincipalID, afterID string, limit int) ([]RotationAuthorityRef, error) {
	if limit < 1 || limit > domain.MaxIncidentPage {
		return nil, domain.ErrInvalid
	}
	rows, err := q.q.RotationListByAuthorityInOrg(ctx, pggen.RotationListByAuthorityInOrgParams{ChainOrg: string(chain.Org), AuthorityPrincipalID: string(authority), AfterID: afterID, RowLimit: int32(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.RotationListByAuthorityInOrgRow) (RotationAuthorityRef, error) {
		return RotationAuthorityRef{ID: c.ID, OrgID: string(chain.Org), ProjectID: c.ProjectID, EnvironmentID: c.EnvironmentID, State: c.State}, nil
	})
}

func (q sqliteRotationQueries) listByAuthorityInInstance(ctx context.Context, authority domain.PrincipalID, afterID string, limit int) ([]RotationAuthorityRef, error) {
	rows, err := q.q.RotationListByAuthorityInInstance(ctx, sqlitegen.RotationListByAuthorityInInstanceParams{AuthorityPrincipalID: string(authority), AfterID: afterID, RowLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.RotationListByAuthorityInInstanceRow) (RotationAuthorityRef, error) {
		return RotationAuthorityRef(c), nil
	})
}

func (q pgRotationQueries) listByAuthorityInInstance(ctx context.Context, authority domain.PrincipalID, afterID string, limit int) ([]RotationAuthorityRef, error) {
	if limit < 1 || limit > domain.MaxIncidentPage {
		return nil, domain.ErrInvalid
	}
	rows, err := q.q.RotationListByAuthorityInInstance(ctx, pggen.RotationListByAuthorityInInstanceParams{AuthorityPrincipalID: string(authority), AfterID: afterID, RowLimit: int32(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.RotationListByAuthorityInInstanceRow) (RotationAuthorityRef, error) {
		return RotationAuthorityRef(c), nil
	})
}

func (q sqliteRotationQueries) countOpenRuns(ctx context.Context, chain domain.Scope, rotationID string) (int64, error) {
	return q.q.RotationCountOpenRuns(ctx, sqlitegen.RotationCountOpenRunsParams{RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q pgRotationQueries) countOpenRuns(ctx context.Context, chain domain.Scope, rotationID string) (int64, error) {
	return q.q.RotationCountOpenRuns(ctx, pggen.RotationCountOpenRunsParams{RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q sqliteRotationQueries) credentialExpiry(ctx context.Context, chain domain.Scope, keyID string) (string, error) {
	v, err := q.q.RotationCredentialExpiry(ctx, sqlitegen.RotationCredentialExpiryParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID})
	if isNoRows(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	out := v.ExpiresAt.String
	if err := normalizeStoredTimes(&out); err != nil {
		return "", err
	}
	return out, nil
}

func (q pgRotationQueries) credentialExpiry(ctx context.Context, chain domain.Scope, keyID string) (string, error) {
	v, err := q.q.RotationCredentialExpiry(ctx, pggen.RotationCredentialExpiryParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID})
	if isNoRows(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	out := pgStoredStamp(v.ExpiresAt)
	if err := normalizeStoredTimes(&out); err != nil {
		return "", err
	}
	return out, nil
}

// expiryPick is the newest published run seen for one key across both key roles.
type expiryPick struct {
	revision int64
	runID    string
	stamp    string
}

// keepNewest records a candidate for a key unless a newer published run is already
// held: the single-key lookup orders by published_revision then run id, and the
// merge of the two role queries must agree with it.
func keepNewest(picks map[string]expiryPick, key string, c expiryPick) {
	if cur, ok := picks[key]; ok && (cur.revision > c.revision || (cur.revision == c.revision && cur.runID >= c.runID)) {
		return
	}
	picks[key] = c
}

func expiryStamps(picks map[string]expiryPick) map[string]string {
	out := make(map[string]string, len(picks))
	for key, pick := range picks {
		out[key] = pick.stamp
	}
	return out
}

func (q sqliteRotationQueries) credentialExpiries(ctx context.Context, chain domain.Scope, keyIDs []string) (map[string]string, error) {
	picks := map[string]expiryPick{}
	password, err := q.q.RotationCredentialExpiries(ctx, sqlitegen.RotationCredentialExpiriesParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyIds: keyIDs})
	if err != nil {
		return nil, err
	}
	username, err := q.q.RotationCredentialExpiriesByUsernameKey(ctx, sqlitegen.RotationCredentialExpiriesByUsernameKeyParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyIds: keyIDs})
	if err != nil {
		return nil, err
	}
	add := func(key, runID string, revision sql.NullInt64, expires sql.NullString) error {
		stamp := expires.String
		if err := normalizeStoredTimes(&stamp); err != nil {
			return err
		}
		keepNewest(picks, key, expiryPick{revision: revision.Int64, runID: runID, stamp: stamp})
		return nil
	}
	for _, row := range password {
		if err := add(row.KeyID, row.RunID, row.PublishedRevision, row.ExpiresAt); err != nil {
			return nil, err
		}
	}
	for _, row := range username {
		if err := add(row.KeyID, row.RunID, row.PublishedRevision, row.ExpiresAt); err != nil {
			return nil, err
		}
	}
	return expiryStamps(picks), nil
}

func (q pgRotationQueries) credentialExpiries(ctx context.Context, chain domain.Scope, keyIDs []string) (map[string]string, error) {
	picks := map[string]expiryPick{}
	password, err := q.q.RotationCredentialExpiries(ctx, pggen.RotationCredentialExpiriesParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyIds: keyIDs})
	if err != nil {
		return nil, err
	}
	username, err := q.q.RotationCredentialExpiriesByUsernameKey(ctx, pggen.RotationCredentialExpiriesByUsernameKeyParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyIds: keyIDs})
	if err != nil {
		return nil, err
	}
	add := func(key, runID string, revision pgtype.Int8, expires pgtype.Timestamptz) error {
		stamp := pgStoredStamp(expires)
		if err := normalizeStoredTimes(&stamp); err != nil {
			return err
		}
		keepNewest(picks, key, expiryPick{revision: revision.Int64, runID: runID, stamp: stamp})
		return nil
	}
	for _, row := range password {
		if err := add(row.KeyID, row.RunID, row.PublishedRevision, row.ExpiresAt); err != nil {
			return nil, err
		}
	}
	for _, row := range username {
		if err := add(row.KeyID, row.RunID, row.PublishedRevision, row.ExpiresAt); err != nil {
			return nil, err
		}
	}
	return expiryStamps(picks), nil
}

func (q sqliteRotationQueries) lastRuns(ctx context.Context, chain domain.Scope, ids []string) (map[string]RotationLastRun, error) {
	rows, err := q.q.RotationLastRuns(ctx, sqlitegen.RotationLastRunsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), RotationIds: ids})
	if err != nil {
		return nil, err
	}
	out := make(map[string]RotationLastRun, len(rows))
	for _, row := range rows {
		run := RotationLastRun{ID: row.ID, Phase: row.Phase, ErrorCode: row.LastError.String, PhaseAt: row.PhaseAt}
		if err := normalizeStoredTimes(&run.PhaseAt); err != nil {
			return nil, err
		}
		out[row.RotationID] = run
	}
	return out, nil
}

func (q pgRotationQueries) lastRuns(ctx context.Context, chain domain.Scope, ids []string) (map[string]RotationLastRun, error) {
	rows, err := q.q.RotationLastRuns(ctx, pggen.RotationLastRunsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), RotationIds: ids})
	if err != nil {
		return nil, err
	}
	out := make(map[string]RotationLastRun, len(rows))
	for _, row := range rows {
		run := RotationLastRun{ID: row.ID, Phase: row.Phase, ErrorCode: row.LastError.String, PhaseAt: pgStoredStamp(row.PhaseAt)}
		if err := normalizeStoredTimes(&run.PhaseAt); err != nil {
			return nil, err
		}
		out[row.RotationID] = run
	}
	return out, nil
}

func (q sqliteRotationQueries) owedDropCounts(ctx context.Context, chain domain.Scope, ids []string) (map[string][2]int64, error) {
	rows, err := q.q.RotationOwedDropCounts(ctx, sqlitegen.RotationOwedDropCountsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), RotationIds: ids})
	if err != nil {
		return nil, err
	}
	out := make(map[string][2]int64, len(rows))
	for _, row := range rows {
		out[row.RotationID] = [2]int64{row.Owed, row.VersionsChanged}
	}
	return out, nil
}

func (q pgRotationQueries) owedDropCounts(ctx context.Context, chain domain.Scope, ids []string) (map[string][2]int64, error) {
	rows, err := q.q.RotationOwedDropCounts(ctx, pggen.RotationOwedDropCountsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), RotationIds: ids})
	if err != nil {
		return nil, err
	}
	out := make(map[string][2]int64, len(rows))
	for _, row := range rows {
		out[row.RotationID] = [2]int64{row.Owed, row.VersionsChanged}
	}
	return out, nil
}

func (q sqliteRotationQueries) claim(ctx context.Context, chain domain.Scope, keyID, rotationID string) (bool, error) {
	err := q.q.RotationClaimInsert(ctx, sqlitegen.RotationClaimInsertParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID, RotationID: rotationID})
	if sqliteUniqueViolation(err) {
		return true, nil
	}
	return false, constraint(err)
}

func (q pgRotationQueries) claim(ctx context.Context, chain domain.Scope, keyID, rotationID string) (bool, error) {
	err := q.q.RotationClaimInsert(ctx, pggen.RotationClaimInsertParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID, RotationID: rotationID})
	if pgUniqueViolation(err) {
		return true, nil
	}
	return false, constraint(err)
}

func (q sqliteRotationQueries) releaseClaims(ctx context.Context, chain domain.Scope, rotationID string) error {
	return q.q.RotationClaimsRelease(ctx, sqlitegen.RotationClaimsReleaseParams{RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q pgRotationQueries) releaseClaims(ctx context.Context, chain domain.Scope, rotationID string) error {
	return q.q.RotationClaimsRelease(ctx, pggen.RotationClaimsReleaseParams{RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q sqliteRotationQueries) blockingRunRotations(ctx context.Context, chain domain.Scope, limit int) ([]string, error) {
	open, err := q.q.RotationBlockingRunRotations(ctx, sqlitegen.RotationBlockingRunRotationsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	failed, err := q.q.RotationPublishedFailedRunRotations(ctx, sqlitegen.RotationPublishedFailedRunRotationsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	unconfirmed, err := q.q.RotationUnconfirmedFailedRunRotations(ctx, sqlitegen.RotationUnconfirmedFailedRunRotationsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return uniqueFirst(append(append(open, failed...), unconfirmed...), limit), nil
}

func (q pgRotationQueries) blockingRunRotations(ctx context.Context, chain domain.Scope, limit int) ([]string, error) {
	pageLimit, err := checkedPGInt32(int64(limit))
	if err != nil {
		return nil, err
	}
	open, err := q.q.RotationBlockingRunRotations(ctx, pggen.RotationBlockingRunRotationsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), PageLimit: pageLimit})
	if err != nil {
		return nil, err
	}
	failed, err := q.q.RotationPublishedFailedRunRotations(ctx, pggen.RotationPublishedFailedRunRotationsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), PageLimit: pageLimit})
	if err != nil {
		return nil, err
	}
	unconfirmed, err := q.q.RotationUnconfirmedFailedRunRotations(ctx, pggen.RotationUnconfirmedFailedRunRotationsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), PageLimit: pageLimit})
	if err != nil {
		return nil, err
	}
	return uniqueFirst(append(append(open, failed...), unconfirmed...), limit), nil
}

// uniqueFirst keeps the first limit distinct ids in id order. Each source list is
// the smallest ids of its own predicate, so the smallest ids of their union are
// within them and the cut is the same on both engines.
func uniqueFirst(ids []string, limit int) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out[:min(len(out), limit)]
}

// purge deletes children before parents, the order the foreign keys require.
func (q sqliteRotationQueries) purge(ctx context.Context, chain domain.Scope) error {
	if err := q.q.RotationPurgeEffects(ctx, sqlitegen.RotationPurgeEffectsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}); err != nil {
		return constraint(err)
	}
	if err := q.q.RotationPurgeRuns(ctx, sqlitegen.RotationPurgeRunsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}); err != nil {
		return constraint(err)
	}
	if err := q.q.RotationPurgeClaims(ctx, sqlitegen.RotationPurgeClaimsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}); err != nil {
		return constraint(err)
	}
	return constraint(q.q.RotationPurge(ctx, sqlitegen.RotationPurgeParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q pgRotationQueries) purge(ctx context.Context, chain domain.Scope) error {
	if err := q.q.RotationPurgeEffects(ctx, pggen.RotationPurgeEffectsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}); err != nil {
		return constraint(err)
	}
	if err := q.q.RotationPurgeRuns(ctx, pggen.RotationPurgeRunsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}); err != nil {
		return constraint(err)
	}
	if err := q.q.RotationPurgeClaims(ctx, pggen.RotationPurgeClaimsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}); err != nil {
		return constraint(err)
	}
	return constraint(q.q.RotationPurge(ctx, pggen.RotationPurgeParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q sqliteRotationQueries) activeByProvider(ctx context.Context, chain domain.Scope, providerID string) ([]RotationRecord, error) {
	rows, err := q.q.RotationListActiveByProvider(ctx, sqlitegen.RotationListActiveByProviderParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ProviderID: providerID})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.RotationListActiveByProviderRow) (RotationRecord, error) {
		return sqliteRotation(sqlitegen.RotationGetRow(c))
	})
}

func (q pgRotationQueries) activeByProvider(ctx context.Context, chain domain.Scope, providerID string) ([]RotationRecord, error) {
	rows, err := q.q.RotationListActiveByProvider(ctx, pggen.RotationListActiveByProviderParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ProviderID: providerID})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.RotationListActiveByProviderRow) (RotationRecord, error) {
		return pgRotation(pggen.RotationGetRow(c))
	})
}

func (q sqliteRotationQueries) activeInEnvironment(ctx context.Context, chain domain.Scope, environmentID string) ([]RotationRecord, error) {
	rows, err := q.q.RotationListActiveInEnvironment(ctx, sqlitegen.RotationListActiveInEnvironmentParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: environmentID})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.RotationListActiveInEnvironmentRow) (RotationRecord, error) {
		return sqliteRotation(sqlitegen.RotationGetRow(c))
	})
}

func (q pgRotationQueries) activeInEnvironment(ctx context.Context, chain domain.Scope, environmentID string) ([]RotationRecord, error) {
	rows, err := q.q.RotationListActiveInEnvironment(ctx, pggen.RotationListActiveInEnvironmentParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: environmentID})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.RotationListActiveInEnvironmentRow) (RotationRecord, error) {
		return pgRotation(pggen.RotationGetRow(c))
	})
}

func (q sqliteRotationQueries) activeInProject(ctx context.Context, chain domain.Scope) ([]RotationRecord, error) {
	rows, err := q.q.RotationListActiveInProject(ctx, sqlitegen.RotationListActiveInProjectParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.RotationListActiveInProjectRow) (RotationRecord, error) {
		return sqliteRotation(sqlitegen.RotationGetRow(c))
	})
}

func (q pgRotationQueries) activeInProject(ctx context.Context, chain domain.Scope) ([]RotationRecord, error) {
	rows, err := q.q.RotationListActiveInProject(ctx, pggen.RotationListActiveInProjectParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.RotationListActiveInProjectRow) (RotationRecord, error) {
		return pgRotation(pggen.RotationGetRow(c))
	})
}

func (q sqliteRotationQueries) pauseActive(ctx context.Context, chain domain.Scope, rotationID string, at time.Time) (int64, error) {
	n, err := q.q.RotationPauseActive(ctx, sqlitegen.RotationPauseActiveParams{At: fixedStamp(at), RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	return n, constraint(err)
}

func (q pgRotationQueries) pauseActive(ctx context.Context, chain domain.Scope, rotationID string, at time.Time) (int64, error) {
	n, err := q.q.RotationPauseActive(ctx, pggen.RotationPauseActiveParams{At: pgRequiredTime(at), RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	return n, constraint(err)
}

func sqliteRotation(c sqlitegen.RotationGetRow) (RotationRecord, error) {
	out := RotationRecord{
		ID: c.ID, EnvironmentID: c.EnvironmentID, ProviderID: c.ProviderID, KeyID: c.KeyID, UsernameKeyID: c.UsernameKeyID,
		IntervalSeconds: c.IntervalSeconds, RevocationDelaySeconds: c.RevocationDelaySeconds,
		AuthorityPrincipalID: c.AuthorityPrincipalID, CreatedBy: c.CreatedBy, State: c.State,
		NextRunAt: c.NextRunAt.String, Generation: c.Generation, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, RestoreHold: c.RestoreHold != 0, RawHaltReason: c.HaltReason.String,
	}
	if err := normalizeStoredTimes(&out.NextRunAt, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return RotationRecord{}, err
	}
	return out, nil
}

func pgRotation(c pggen.RotationGetRow) (RotationRecord, error) {
	out := RotationRecord{
		ID: c.ID, EnvironmentID: c.EnvironmentID, ProviderID: c.ProviderID, KeyID: c.KeyID, UsernameKeyID: c.UsernameKeyID,
		IntervalSeconds: c.IntervalSeconds, RevocationDelaySeconds: c.RevocationDelaySeconds,
		AuthorityPrincipalID: c.AuthorityPrincipalID, CreatedBy: c.CreatedBy, State: c.State,
		NextRunAt: pgStoredStamp(c.NextRunAt), Generation: c.Generation, CreatedAt: pgStoredStamp(c.CreatedAt), UpdatedAt: pgStoredStamp(c.UpdatedAt), RestoreHold: c.RestoreHold, RawHaltReason: c.HaltReason.String,
	}
	if err := normalizeStoredTimes(&out.NextRunAt, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return RotationRecord{}, err
	}
	return out, nil
}

func (r rotationQueries) BeginRunPublish(ctx context.Context, p authz.Proof, run RotationRun, now time.Time) error {
	chain, err := r.envChain(p, authz.StoreRotationsBeginRunPublish)
	if err != nil {
		return err
	}
	if chain != run.Chain() {
		return ErrNotFound
	}
	rows, err := r.queries.runtime().settleRun(ctx, run, RotationSettle{Now: now, FromPhases: []string{"minted"}, Phase: "minted", NextAttempt: run.NextAttemptAt, KeepLease: true})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrRotationFenceLost
	}
	return nil
}

func (r rotationQueries) RecordRunPublish(ctx context.Context, p authz.Proof, rec RotationPublishRecord) error {
	chain, err := r.envChain(p, authz.StoreRotationsRecordRunPublish)
	if err != nil {
		return err
	}
	run := rec.Run
	if chain != run.Chain() {
		return ErrNotFound
	}
	rt := r.queries.runtime()
	rows, err := rt.settleRun(ctx, run, RotationSettle{Now: rec.Now, FromPhases: []string{"minted"}, Phase: "published", NextAttempt: rec.NextAttempt, PublishedRevision: rec.Revision, KeepLease: true})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrRotationFenceLost
	}
	if err := rt.insertAudit(ctx, chain, run.AuthorityPrincipalID, run.RotationID, run.ID, rec.Outcome.Audit, rec.Now); err != nil {
		return err
	}
	if err := rt.closeEffect(ctx, chain, run.ID, rec.Outcome, rec.Now); err != nil {
		return err
	}
	// Zero rows is correct: the rotation was paused or deleted while the run was
	// in flight. The publish still stands, so the run still revokes; only the
	// schedule is not re-armed.
	// This write is also the publish's guard against a restore hold that lands after
	// the begin check: the schedule write targets only an active, unheld row, so under
	// PG serializable a concurrent hold on that row conflicts with it and the publish
	// transaction aborts (the run then settles as held), while sqlite's single write
	// connection cannot interleave a hold at all.
	// TestRotationHoldDuringThePublishTransactionRollsItBack fails without it.
	// It is the same write that makes a concurrent incident halt abort the publish
	// on PostgreSQL: the halt updates this rotation row, so the two conflict under
	// serializable and the rerun finds the rotation halted. Removing it lets
	// TestRotationIncidentHaltDuringThePublishTransactionRollsItBack fail.
	_, err = rt.setNextRun(ctx, chain, run.RotationID, rec.NextRunAt, rec.Now)
	return err
}

func (r rotationQueries) AbandonOpenRuns(ctx context.Context, p authz.Proof, rotationID string, at time.Time, delay time.Duration) ([]AbandonedRun, error) {
	chain, err := r.envChain(p, authz.StoreRotationsAbandonOpenRuns)
	if err != nil {
		return nil, err
	}
	return r.queries.abandonRuns(ctx, chain, rotationID, at, at.Add(delay))
}

func (r rotationQueries) ProviderLiveRuns(ctx context.Context, p authz.Proof, providerID string) (int64, error) {
	chain, err := authz.Verify(p, authz.StoreRotationsProviderLiveRuns, r.tok)
	if err != nil {
		return 0, err
	}
	return r.queries.providerLiveRuns(ctx, chain, providerID)
}

func (q sqliteRotationQueries) abandonRuns(ctx context.Context, chain domain.Scope, rotationID string, at, publishedDue time.Time) ([]AbandonedRun, error) {
	rows, err := q.q.RotationAbandonOpenRuns(ctx, sqlitegen.RotationAbandonOpenRunsParams{At: fixedStamp(at), PublishedDueAt: fixedStamp(publishedDue), RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(row sqlitegen.RotationAbandonOpenRunsRow) (AbandonedRun, error) {
		return AbandonedRun{ID: row.ID, Phase: row.Phase, CandidateHandle: row.CandidateHandle, SupersededHandle: row.SupersededHandle.String}, nil
	})
}

func (q pgRotationQueries) abandonRuns(ctx context.Context, chain domain.Scope, rotationID string, at, publishedDue time.Time) ([]AbandonedRun, error) {
	rows, err := q.q.RotationAbandonOpenRuns(ctx, pggen.RotationAbandonOpenRunsParams{At: pgRequiredTime(at), PublishedDueAt: pgRequiredTime(publishedDue), RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(row pggen.RotationAbandonOpenRunsRow) (AbandonedRun, error) {
		return AbandonedRun{ID: row.ID, Phase: row.Phase, CandidateHandle: row.CandidateHandle, SupersededHandle: row.SupersededHandle.String}, nil
	})
}

func (q sqliteRotationQueries) providerLiveRuns(ctx context.Context, chain domain.Scope, providerID string) (int64, error) {
	return q.q.RotationProviderLiveRuns(ctx, sqlitegen.RotationProviderLiveRunsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ProviderID: providerID})
}

func (q pgRotationQueries) providerLiveRuns(ctx context.Context, chain domain.Scope, providerID string) (int64, error) {
	return q.q.RotationProviderLiveRuns(ctx, pggen.RotationProviderLiveRunsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ProviderID: providerID})
}

func (q sqliteRotationQueries) releaseHold(ctx context.Context, chain domain.Scope, rotationID string, at time.Time) (int64, error) {
	return q.q.RotationReleaseHold(ctx, sqlitegen.RotationReleaseHoldParams{At: fixedStamp(at), RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q pgRotationQueries) releaseHold(ctx context.Context, chain domain.Scope, rotationID string, at time.Time) (int64, error) {
	return q.q.RotationReleaseHold(ctx, pggen.RotationReleaseHoldParams{At: pgRequiredTime(at), RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q sqliteRotationQueries) requestRun(ctx context.Context, chain domain.Scope, m RotationRunRequest) (int64, error) {
	skip := int64(0)
	if m.SkipOverlap {
		skip = 1
	}
	return q.q.RotationRequestRun(ctx, sqlitegen.RotationRequestRunParams{RunID: sqlText(m.RunID), SkipOverlap: skip, Now: sql.NullString{String: fixedStamp(m.At), Valid: true}, RotationID: m.RotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), ExpectedGeneration: m.ExpectedGeneration})
}

func (q pgRotationQueries) requestRun(ctx context.Context, chain domain.Scope, m RotationRunRequest) (int64, error) {
	return q.q.RotationRequestRun(ctx, pggen.RotationRequestRunParams{RunID: pgText(m.RunID), SkipOverlap: m.SkipOverlap, Now: pgRequiredTime(m.At), RotationID: m.RotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), ExpectedGeneration: m.ExpectedGeneration})
}

func (q sqliteRotationQueries) manualPending(ctx context.Context, chain domain.Scope, rotationID string) (bool, error) {
	n, err := q.q.RotationManualPending(ctx, sqlitegen.RotationManualPendingParams{RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return n > 0, err
}

func (q pgRotationQueries) manualPending(ctx context.Context, chain domain.Scope, rotationID string) (bool, error) {
	n, err := q.q.RotationManualPending(ctx, pggen.RotationManualPendingParams{RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return n > 0, err
}

// resolveRunExpiry fills the expiry of the role a revoke_refused or quarantine run
// still owes a drop: the candidate's when the run never published, else the
// superseded role's (a published run's candidate is the serving credential, so an
// operator is never pointed at it).
func resolveRunExpiry(ctx context.Context, q rotationStoreQueries, chain domain.Scope, rec *RotationRunRecord, candidateUntil, supersededHandle string) error {
	if rec.Phase != "revoke_refused" && rec.Phase != "quarantine" {
		return nil
	}
	if rec.PublishedRevision == 0 {
		rec.CredentialValidUntil = candidateUntil
		return nil
	}
	if supersededHandle == "" {
		return nil
	}
	until, uncertain, err := q.mintedExpiry(ctx, chain, supersededHandle)
	if err != nil {
		return err
	}
	rec.CredentialValidUntil, rec.CredentialValidUntilLowerBound = until, uncertain
	return nil
}

func (q sqliteRotationQueries) listRuns(ctx context.Context, chain domain.Scope, lq RotationRunQuery) ([]RotationRunRecord, error) {
	var rows []sqlitegen.RotationListRunsRow
	var err error
	if lq.After == "" {
		rows, err = q.q.RotationListRuns(ctx, sqlitegen.RotationListRunsParams{RotationID: lq.RotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), PageLimit: int64(lq.Limit)})
	} else {
		var after []sqlitegen.RotationListRunsAfterRow
		after, err = q.q.RotationListRunsAfter(ctx, sqlitegen.RotationListRunsAfterParams{RotationID: lq.RotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), AfterID: lq.After, PageLimit: int64(lq.Limit)})
		for _, r := range after {
			rows = append(rows, sqlitegen.RotationListRunsRow(r))
		}
	}
	if err != nil {
		return nil, err
	}
	out := make([]RotationRunRecord, 0, len(rows))
	for _, r := range rows {
		rec := RotationRunRecord{ID: r.ID, Trigger: r.RunTrigger, Phase: r.Phase, PublishedRevision: r.PublishedRevision.Int64, RevocationDelaySeconds: r.RevocationDelaySeconds, StartedAt: r.StartedAt, PhaseAt: r.PhaseAt, ErrorCode: r.LastError.String}
		if err := resolveRunExpiry(ctx, q, chain, &rec, r.CandidateValidUntil.String, r.SupersededHandle.String); err != nil {
			return nil, err
		}
		if err := normalizeStoredTimes(&rec.StartedAt, &rec.PhaseAt, &rec.CredentialValidUntil); err != nil {
			return nil, err
		}
		if r.NotConvergedAdapterTargets.Valid {
			rec.NotConvergedAdapterTargets = &r.NotConvergedAdapterTargets.Int64
		}
		out = append(out, rec)
	}
	return out, nil
}

func (q pgRotationQueries) listRuns(ctx context.Context, chain domain.Scope, lq RotationRunQuery) ([]RotationRunRecord, error) {
	pageLimit, err := checkedPGInt32(int64(lq.Limit))
	if err != nil {
		return nil, err
	}
	var rows []pggen.RotationListRunsRow
	if lq.After == "" {
		rows, err = q.q.RotationListRuns(ctx, pggen.RotationListRunsParams{RotationID: lq.RotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), PageLimit: pageLimit})
	} else {
		var after []pggen.RotationListRunsAfterRow
		after, err = q.q.RotationListRunsAfter(ctx, pggen.RotationListRunsAfterParams{RotationID: lq.RotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), AfterID: lq.After, PageLimit: pageLimit})
		for _, r := range after {
			rows = append(rows, pggen.RotationListRunsRow(r))
		}
	}
	if err != nil {
		return nil, err
	}
	out := make([]RotationRunRecord, 0, len(rows))
	for _, r := range rows {
		rec := RotationRunRecord{ID: r.ID, Trigger: r.RunTrigger, Phase: r.Phase, PublishedRevision: r.PublishedRevision.Int64, RevocationDelaySeconds: r.RevocationDelaySeconds, StartedAt: pgStoredStamp(r.StartedAt), PhaseAt: pgStoredStamp(r.PhaseAt), ErrorCode: r.LastError.String}
		if err := resolveRunExpiry(ctx, q, chain, &rec, pgStoredStamp(r.CandidateValidUntil), r.SupersededHandle.String); err != nil {
			return nil, err
		}
		if err := normalizeStoredTimes(&rec.StartedAt, &rec.PhaseAt, &rec.CredentialValidUntil); err != nil {
			return nil, err
		}
		if r.NotConvergedAdapterTargets.Valid {
			rec.NotConvergedAdapterTargets = &r.NotConvergedAdapterTargets.Int64
		}
		out = append(out, rec)
	}
	return out, nil
}

func (q sqliteRotationQueries) mintedExpiry(ctx context.Context, chain domain.Scope, handle string) (string, bool, error) {
	minter, err := q.q.RotationRunMinterByHandle(ctx, sqlitegen.RotationRunMinterByHandleParams{Handle: handle, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	n, err := q.q.RotationRunExtendUncertain(ctx, sqlitegen.RotationRunExtendUncertainParams{RunID: minter.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return minter.CandidateValidUntil.String, n > 0, err
}

func (q pgRotationQueries) mintedExpiry(ctx context.Context, chain domain.Scope, handle string) (string, bool, error) {
	minter, err := q.q.RotationRunMinterByHandle(ctx, pggen.RotationRunMinterByHandleParams{Handle: handle, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	n, err := q.q.RotationRunExtendUncertain(ctx, pggen.RotationRunExtendUncertainParams{RunID: minter.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return pgStoredStamp(minter.CandidateValidUntil), n > 0, err
}
