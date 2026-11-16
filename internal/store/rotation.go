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
	After           string
	Limit           int
}

// RotationReader is the read side.
type RotationReader interface {
	Get(ctx context.Context, p authz.Proof, rotationID string) (RotationRecord, error)
	// List returns up to Limit of the environment's rotations whose id sorts after
	// After, in id order; disabled history only with IncludeDisabled. Each carries
	// its delivered-credential expiry, read for the whole page in one query.
	List(ctx context.Context, p authz.Proof, q RotationListQuery) ([]RotationRecord, error)
	// CountOpenRuns counts the rotation's non-terminal runs.
	CountOpenRuns(ctx context.Context, p authz.Proof, rotationID string) (int64, error)
}

// RotationRepo is the write side plus the reads.
type RotationRepo interface {
	RotationReader
	Create(ctx context.Context, p authz.Proof, m RotationCreate) (RotationRecord, error)
	// SetState moves a rotation between active, paused and disabled, bumping its
	// generation. A stale expected generation, or a disabled rotation, is
	// ErrConflict; a missing rotation is ErrNotFound.
	SetState(ctx context.Context, p authz.Proof, m RotationSetState) (RotationRecord, error)
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
	// that statement changed, so the service's audit rows match them one to one.
	AbandonOpenRuns(ctx context.Context, p authz.Proof, rotationID string, at time.Time) ([]AbandonedRun, error)
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
	countOpenRuns(ctx context.Context, chain domain.Scope, rotationID string) (int64, error)
	credentialExpiry(ctx context.Context, chain domain.Scope, keyID string) (string, error)
	// credentialExpiries returns the delivered-credential expiry per key, from the
	// latest published run of each key, in one read.
	credentialExpiries(ctx context.Context, chain domain.Scope, keyIDs []string) (map[string]string, error)
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
	abandonRuns(ctx context.Context, chain domain.Scope, rotationID string, at time.Time) ([]AbandonedRun, error)
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
	return r.withExpiry(ctx, chain, rec)
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

func (q sqliteRotationQueries) credentialExpiries(ctx context.Context, chain domain.Scope, keyIDs []string) (map[string]string, error) {
	rows, err := q.q.RotationCredentialExpiries(ctx, sqlitegen.RotationCredentialExpiriesParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyIds: keyIDs})
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, row := range rows {
		stamp := row.ExpiresAt.String
		if err := normalizeStoredTimes(&stamp); err != nil {
			return nil, err
		}
		out[row.KeyID] = stamp
	}
	return out, nil
}

func (q pgRotationQueries) credentialExpiries(ctx context.Context, chain domain.Scope, keyIDs []string) (map[string]string, error) {
	rows, err := q.q.RotationCredentialExpiries(ctx, pggen.RotationCredentialExpiriesParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyIds: keyIDs})
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, row := range rows {
		stamp := pgStoredStamp(row.ExpiresAt)
		if err := normalizeStoredTimes(&stamp); err != nil {
			return nil, err
		}
		out[row.KeyID] = stamp
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
		NextRunAt: c.NextRunAt.String, Generation: c.Generation, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
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
		NextRunAt: pgStoredStamp(c.NextRunAt), Generation: c.Generation, CreatedAt: pgStoredStamp(c.CreatedAt), UpdatedAt: pgStoredStamp(c.UpdatedAt),
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
	_, err = rt.setNextRun(ctx, chain, run.RotationID, rec.NextRunAt, rec.Now)
	return err
}

func (r rotationQueries) AbandonOpenRuns(ctx context.Context, p authz.Proof, rotationID string, at time.Time) ([]AbandonedRun, error) {
	chain, err := r.envChain(p, authz.StoreRotationsAbandonOpenRuns)
	if err != nil {
		return nil, err
	}
	return r.queries.abandonRuns(ctx, chain, rotationID, at)
}

func (r rotationQueries) ProviderLiveRuns(ctx context.Context, p authz.Proof, providerID string) (int64, error) {
	chain, err := authz.Verify(p, authz.StoreRotationsProviderLiveRuns, r.tok)
	if err != nil {
		return 0, err
	}
	return r.queries.providerLiveRuns(ctx, chain, providerID)
}

func (q sqliteRotationQueries) abandonRuns(ctx context.Context, chain domain.Scope, rotationID string, at time.Time) ([]AbandonedRun, error) {
	rows, err := q.q.RotationAbandonOpenRuns(ctx, sqlitegen.RotationAbandonOpenRunsParams{At: fixedStamp(at), RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(row sqlitegen.RotationAbandonOpenRunsRow) (AbandonedRun, error) {
		return AbandonedRun{ID: row.ID, Phase: row.Phase, CandidateHandle: row.CandidateHandle, SupersededHandle: row.SupersededHandle.String}, nil
	})
}

func (q pgRotationQueries) abandonRuns(ctx context.Context, chain domain.Scope, rotationID string, at time.Time) ([]AbandonedRun, error) {
	rows, err := q.q.RotationAbandonOpenRuns(ctx, pggen.RotationAbandonOpenRunsParams{At: pgRequiredTime(at), RotationID: rotationID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
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
