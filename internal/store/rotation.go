package store

import (
	"context"
	"database/sql"
	"errors"
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

// RotationRecord is one rotation's metadata surface.
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
	List(ctx context.Context, p authz.Proof) ([]RotationRecord, error)
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
}

type rotationStoreQueries interface {
	create(ctx context.Context, chain domain.Scope, m RotationCreate) (int64, error)
	get(ctx context.Context, chain domain.Scope, rotationID string) (RotationRecord, error)
	list(ctx context.Context, chain domain.Scope) ([]RotationRecord, error)
	setState(ctx context.Context, chain domain.Scope, m RotationSetState) (int64, error)
	countOpenRuns(ctx context.Context, chain domain.Scope, rotationID string) (int64, error)
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
	return r.queries.get(ctx, chain, m.ID)
}

func (r rotationQueries) Get(ctx context.Context, p authz.Proof, rotationID string) (RotationRecord, error) {
	chain, err := r.envChain(p, authz.StoreRotationsGet)
	if err != nil {
		return RotationRecord{}, err
	}
	return r.queries.get(ctx, chain, rotationID)
}

func (r rotationQueries) List(ctx context.Context, p authz.Proof) ([]RotationRecord, error) {
	chain, err := r.envChain(p, authz.StoreRotationsList)
	if err != nil {
		return nil, err
	}
	return r.queries.list(ctx, chain)
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
	return r.queries.get(ctx, chain, m.RotationID)
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
		ProviderID: m.ProviderID, KeyID: m.KeyID, UsernameKeyID: sql.NullString{String: m.UsernameKeyID, Valid: m.UsernameKeyID != ""},
		IntervalSeconds: m.IntervalSeconds, RevocationDelaySeconds: m.RevocationDelaySeconds,
		AuthorityPrincipalID: m.AuthorityPrincipalID, NextRunAt: sqliteOptStamp(m.NextRunAt), At: fixedStamp(m.At),
	})
	return n, constraint(err)
}

func (q pgRotationQueries) create(ctx context.Context, chain domain.Scope, m RotationCreate) (int64, error) {
	n, err := q.q.RotationCreate(ctx, pggen.RotationCreateParams{
		ID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env),
		ProviderID: m.ProviderID, KeyID: m.KeyID, UsernameKeyID: pgtype.Text{String: m.UsernameKeyID, Valid: m.UsernameKeyID != ""},
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

func (q sqliteRotationQueries) list(ctx context.Context, chain domain.Scope) ([]RotationRecord, error) {
	rows, err := q.q.RotationList(ctx, sqlitegen.RotationListParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.RotationListRow) (RotationRecord, error) {
		return sqliteRotation(sqlitegen.RotationGetRow(c))
	})
}

func (q pgRotationQueries) list(ctx context.Context, chain domain.Scope) ([]RotationRecord, error) {
	rows, err := q.q.RotationList(ctx, pggen.RotationListParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.RotationListRow) (RotationRecord, error) {
		return pgRotation(pggen.RotationGetRow(c))
	})
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

func sqliteRotation(c sqlitegen.RotationGetRow) (RotationRecord, error) {
	out := RotationRecord{
		ID: c.ID, EnvironmentID: c.EnvironmentID, ProviderID: c.ProviderID, KeyID: c.KeyID, UsernameKeyID: c.UsernameKeyID.String,
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
		ID: c.ID, EnvironmentID: c.EnvironmentID, ProviderID: c.ProviderID, KeyID: c.KeyID, UsernameKeyID: c.UsernameKeyID.String,
		IntervalSeconds: c.IntervalSeconds, RevocationDelaySeconds: c.RevocationDelaySeconds,
		AuthorityPrincipalID: c.AuthorityPrincipalID, CreatedBy: c.CreatedBy, State: c.State,
		NextRunAt: pgStoredStamp(c.NextRunAt), Generation: c.Generation, CreatedAt: pgStoredStamp(c.CreatedAt), UpdatedAt: pgStoredStamp(c.UpdatedAt),
	}
	if err := normalizeStoredTimes(&out.NextRunAt, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return RotationRecord{}, err
	}
	return out, nil
}
