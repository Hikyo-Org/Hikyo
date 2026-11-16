package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// Rotation states.
const (
	RotationStateActive   = "active"
	RotationStatePaused   = "paused"
	RotationStateDisabled = "disabled"
)

var (
	// ErrRotationKeyInUse refuses a rotation whose password or username key is
	// already claimed by another live rotation, whichever role the key plays
	// there.
	ErrRotationKeyInUse = fmt.Errorf("%w: a key is already driven by another live rotation", ErrConflict)
	// ErrRotationLiveInEnvironment refuses an environment delete while a rotation
	// in it is not disabled or has a run in flight: deleting the environment
	// would orphan a live superseded role that nothing would ever revoke.
	ErrRotationLiveInEnvironment = fmt.Errorf("%w: the environment still has a live credential rotation; delete the rotation first", ErrConflict)
)

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

// RotationReader is the read side.
type RotationReader interface {
	Get(ctx context.Context, p authz.Proof, rotationID string) (RotationRecord, error)
	// List returns the environment's rotations; disabled history only with
	// includeDisabled.
	List(ctx context.Context, p authz.Proof, includeDisabled bool) ([]RotationRecord, error)
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
	// PurgeEnvironment removes every rotation row of the proof's environment as
	// part of the environment delete, once nothing in it is live. It is
	// ErrRotationLiveInEnvironment otherwise.
	PurgeEnvironment(ctx context.Context, p authz.Proof) error
}

type rotationStoreQueries interface {
	create(ctx context.Context, chain domain.Scope, m RotationCreate) (int64, error)
	get(ctx context.Context, chain domain.Scope, rotationID string) (RotationRecord, error)
	list(ctx context.Context, chain domain.Scope, includeDisabled bool) ([]RotationRecord, error)
	setState(ctx context.Context, chain domain.Scope, m RotationSetState) (int64, error)
	countOpenRuns(ctx context.Context, chain domain.Scope, rotationID string) (int64, error)
	credentialExpiry(ctx context.Context, chain domain.Scope, keyID string) (string, error)
	// claim takes one key for a rotation; a taken key is reported by
	// uniqueViolation, never by constraint text.
	claim(ctx context.Context, chain domain.Scope, keyID, rotationID string) (uniqueViolation bool, err error)
	releaseClaims(ctx context.Context, chain domain.Scope, rotationID string) error
	liveCounts(ctx context.Context, chain domain.Scope) (rotations, openRuns int64, err error)
	purge(ctx context.Context, chain domain.Scope) error
	activeByProvider(ctx context.Context, chain domain.Scope, providerID string) ([]RotationRecord, error)
	pauseActive(ctx context.Context, chain domain.Scope, rotationID string, at time.Time) (int64, error)
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

func (r rotationQueries) List(ctx context.Context, p authz.Proof, includeDisabled bool) ([]RotationRecord, error) {
	chain, err := r.envChain(p, authz.StoreRotationsList)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.list(ctx, chain, includeDisabled)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i], err = r.withExpiry(ctx, chain, rows[i]); err != nil {
			return nil, err
		}
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
	live, open, err := r.queries.liveCounts(ctx, chain)
	if err != nil {
		return err
	}
	if live > 0 || open > 0 {
		return ErrRotationLiveInEnvironment
	}
	return r.queries.purge(ctx, chain)
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

func (q sqliteRotationQueries) list(ctx context.Context, chain domain.Scope, includeDisabled bool) ([]RotationRecord, error) {
	var rows []sqlitegen.RotationGetRow
	if includeDisabled {
		all, err := q.q.RotationListAll(ctx, sqlitegen.RotationListAllParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
		if err != nil {
			return nil, err
		}
		for _, c := range all {
			rows = append(rows, sqlitegen.RotationGetRow(c))
		}
	} else {
		live, err := q.q.RotationList(ctx, sqlitegen.RotationListParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
		if err != nil {
			return nil, err
		}
		for _, c := range live {
			rows = append(rows, sqlitegen.RotationGetRow(c))
		}
	}
	return mapRows(rows, sqliteRotation)
}

func (q pgRotationQueries) list(ctx context.Context, chain domain.Scope, includeDisabled bool) ([]RotationRecord, error) {
	var rows []pggen.RotationGetRow
	if includeDisabled {
		all, err := q.q.RotationListAll(ctx, pggen.RotationListAllParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
		if err != nil {
			return nil, err
		}
		for _, c := range all {
			rows = append(rows, pggen.RotationGetRow(c))
		}
	} else {
		live, err := q.q.RotationList(ctx, pggen.RotationListParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
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

func (q sqliteRotationQueries) liveCounts(ctx context.Context, chain domain.Scope) (int64, int64, error) {
	live, err := q.q.RotationCountLiveInEnvironment(ctx, sqlitegen.RotationCountLiveInEnvironmentParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return 0, 0, err
	}
	open, err := q.q.RotationCountOpenRunsInEnvironment(ctx, sqlitegen.RotationCountOpenRunsInEnvironmentParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return live, open, err
}

func (q pgRotationQueries) liveCounts(ctx context.Context, chain domain.Scope) (int64, int64, error) {
	live, err := q.q.RotationCountLiveInEnvironment(ctx, pggen.RotationCountLiveInEnvironmentParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return 0, 0, err
	}
	open, err := q.q.RotationCountOpenRunsInEnvironment(ctx, pggen.RotationCountOpenRunsInEnvironmentParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return live, open, err
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
