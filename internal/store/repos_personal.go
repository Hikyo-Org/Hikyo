package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// Personal values (#1235, personal-values ADR). The chain columns are bound
// from the verified proof as everywhere else. The *Own doors also bind the
// owner from the proof (authz.VerifyOwner), so no caller argument can name
// whose ciphertext is read or written. The member doors name an owner, but
// return and delete metadata only.

// PersonalValue is one owner's sealed value for one key in one environment.
type PersonalValue struct {
	ID            string
	EnvironmentID string
	KeyID         string
	Owner         domain.PrincipalID
	Ciphertext    []byte
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// PersonalValueHolder is who holds a value for a key, and when it changed.
type PersonalValueHolder struct {
	Owner     domain.PrincipalID
	UpdatedAt time.Time
}

// PersonalValueCell is the key cascade's view: which owner loses a value where.
type PersonalValueCell struct {
	EnvironmentID string
	Owner         domain.PrincipalID
}

// PersonalValueNotice records why a value disappeared other than by its owner.
type PersonalValueNotice struct {
	KeyName   string
	Owner     domain.PrincipalID
	Cause     string
	CreatedAt time.Time
}

// PersonalValueRepo is the proof-bound personal value surface.
type PersonalValueRepo interface {
	// Put replaces the owner's value for keyID in the proof's environment.
	// The old row is destroyed, never kept as history.
	Put(ctx context.Context, p authz.Proof, id, keyID string, ciphertext []byte, at time.Time) error
	GetOwn(ctx context.Context, p authz.Proof, keyID string) (PersonalValue, error)
	ListOwn(ctx context.Context, p authz.Proof) ([]PersonalValue, error)
	DeleteOwn(ctx context.Context, p authz.Proof, keyID string) (int64, error)
	CountProject(ctx context.Context, p authz.Proof) (int64, error)
	Holders(ctx context.Context, p authz.Proof, keyID string) ([]PersonalValueHolder, error)
	DeleteMember(ctx context.Context, p authz.Proof, keyID string, owner domain.PrincipalID) (int64, error)
	CellsForKey(ctx context.Context, p authz.Proof, keyID string) ([]PersonalValueCell, error)
	DeleteForKey(ctx context.Context, p authz.Proof, keyID string) (int64, error)
	// DeleteForEnvironment removes the proof environment's values and notices
	// and returns the owner of each deleted value, so the caller can audit it.
	DeleteForEnvironment(ctx context.Context, p authz.Proof) ([]domain.PrincipalID, error)
	ListForReencrypt(ctx context.Context, p authz.Proof, cursor string, limit int) ([]ReencryptFieldRow, error)
	Reencrypt(ctx context.Context, p authz.Proof, id string, newCt, oldCt []byte) (bool, error)
	NoticesOwn(ctx context.Context, p authz.Proof) ([]PersonalValueNotice, error)
	DismissNotice(ctx context.Context, p authz.Proof, keyName string) (int64, error)
	NoticesForKeyName(ctx context.Context, p authz.Proof, keyName string) ([]PersonalValueNotice, error)
	// RecordNotice leaves a notice in environmentID, replacing any earlier one.
	RecordNotice(ctx context.Context, p authz.Proof, environmentID, keyName string, owner domain.PrincipalID, cause string, at time.Time) error
}

func ownCell(p authz.Proof, op authz.StoreOp, tok *authz.TxToken) (domain.Scope, string, domain.PrincipalID, error) {
	chain, owner, err := authz.VerifyOwner(p, op, tok)
	if err != nil {
		return domain.Scope{}, "", "", err
	}
	env, err := envOf(chain, op)
	return chain, env, owner, err
}

func envChain(p authz.Proof, op authz.StoreOp, tok *authz.TxToken) (domain.Scope, string, error) {
	chain, err := authz.Verify(p, op, tok)
	if err != nil {
		return domain.Scope{}, "", err
	}
	env, err := envOf(chain, op)
	return chain, env, err
}

// --- sqlite ---

type sqlitePersonalValues struct {
	q   *sqlitegen.Queries
	tok *authz.TxToken
}

func (r sqliteRepos) PersonalValues() PersonalValueRepo {
	return sqlitePersonalValues{q: sqlitegen.New(r.db), tok: r.tok}
}

func (r sqlitePersonalValues) Put(ctx context.Context, p authz.Proof, id, keyID string, ciphertext []byte, at time.Time) error {
	chain, env, owner, err := ownCell(p, authz.StorePersonalValuesPut, r.tok)
	if err != nil {
		return err
	}
	if _, err := r.q.DeletePersonalValueForCell(ctx, sqlitegen.DeletePersonalValueForCellParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		KeyID: keyID, OwnerPrincipalID: string(owner),
	}); err != nil {
		return err
	}
	ts := CanonTime(at).Format(timeFormat)
	return constraint(r.q.InsertPersonalValue(ctx, sqlitegen.InsertPersonalValueParams{
		ID: id, ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		KeyID: keyID, OwnerPrincipalID: string(owner), Ciphertext: ciphertext, CreatedAt: ts, UpdatedAt: ts,
	}))
}

func sqlitePersonal(env, id, keyID, owner string, ct []byte, created, updated string) (PersonalValue, error) {
	c, err := parseTime("personal value", id, created)
	if err != nil {
		return PersonalValue{}, err
	}
	u, err := parseTime("personal value", id, updated)
	if err != nil {
		return PersonalValue{}, err
	}
	return PersonalValue{ID: id, EnvironmentID: env, KeyID: keyID, Owner: domain.PrincipalID(owner), Ciphertext: ct, CreatedAt: c, UpdatedAt: u}, nil
}

func (r sqlitePersonalValues) GetOwn(ctx context.Context, p authz.Proof, keyID string) (PersonalValue, error) {
	chain, env, owner, err := ownCell(p, authz.StorePersonalValuesGetOwn, r.tok)
	if err != nil {
		return PersonalValue{}, err
	}
	row, err := r.q.GetPersonalValueForOwner(ctx, sqlitegen.GetPersonalValueForOwnerParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		KeyID: keyID, OwnerPrincipalID: string(owner),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return PersonalValue{}, domain.ErrNotFound
	}
	if err != nil {
		return PersonalValue{}, err
	}
	return sqlitePersonal(env, row.ID, row.KeyID, row.OwnerPrincipalID, row.Ciphertext, row.CreatedAt, row.UpdatedAt)
}

func (r sqlitePersonalValues) ListOwn(ctx context.Context, p authz.Proof) ([]PersonalValue, error) {
	chain, env, owner, err := ownCell(p, authz.StorePersonalValuesListOwn, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListPersonalValuesForOwner(ctx, sqlitegen.ListPersonalValuesForOwnerParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, OwnerPrincipalID: string(owner),
	})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalValue, 0, len(rows))
	for _, row := range rows {
		v, err := sqlitePersonal(env, row.ID, row.KeyID, row.OwnerPrincipalID, row.Ciphertext, row.CreatedAt, row.UpdatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (r sqlitePersonalValues) DeleteOwn(ctx context.Context, p authz.Proof, keyID string) (int64, error) {
	chain, env, owner, err := ownCell(p, authz.StorePersonalValuesDeleteOwn, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.DeletePersonalValueForCell(ctx, sqlitegen.DeletePersonalValueForCellParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		KeyID: keyID, OwnerPrincipalID: string(owner),
	})
}

func (r sqlitePersonalValues) CountProject(ctx context.Context, p authz.Proof) (int64, error) {
	chain, err := authz.Verify(p, authz.StorePersonalValuesCountProject, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.CountPersonalValuesForProject(ctx, sqlitegen.CountPersonalValuesForProjectParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project),
	})
}

func (r sqlitePersonalValues) Holders(ctx context.Context, p authz.Proof, keyID string) ([]PersonalValueHolder, error) {
	chain, env, err := envChain(p, authz.StorePersonalValuesHolders, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListPersonalValueHolders(ctx, sqlitegen.ListPersonalValueHoldersParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, KeyID: keyID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalValueHolder, 0, len(rows))
	for _, row := range rows {
		u, err := parseTime("personal value holder", row.OwnerPrincipalID, row.UpdatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, PersonalValueHolder{Owner: domain.PrincipalID(row.OwnerPrincipalID), UpdatedAt: u})
	}
	return out, nil
}

func (r sqlitePersonalValues) DeleteMember(ctx context.Context, p authz.Proof, keyID string, owner domain.PrincipalID) (int64, error) {
	chain, env, err := envChain(p, authz.StorePersonalValuesDeleteMember, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.DeletePersonalValueForCell(ctx, sqlitegen.DeletePersonalValueForCellParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		KeyID: keyID, OwnerPrincipalID: string(owner),
	})
}

func (r sqlitePersonalValues) CellsForKey(ctx context.Context, p authz.Proof, keyID string) ([]PersonalValueCell, error) {
	chain, err := authz.Verify(p, authz.StorePersonalValuesCellsForKey, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListPersonalValueCellsForKey(ctx, sqlitegen.ListPersonalValueCellsForKeyParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), KeyID: keyID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalValueCell, len(rows))
	for i, row := range rows {
		out[i] = PersonalValueCell{EnvironmentID: row.EnvironmentID, Owner: domain.PrincipalID(row.OwnerPrincipalID)}
	}
	return out, nil
}

func (r sqlitePersonalValues) DeleteForKey(ctx context.Context, p authz.Proof, keyID string) (int64, error) {
	chain, err := authz.Verify(p, authz.StorePersonalValuesDeleteForKey, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.DeletePersonalValuesForKey(ctx, sqlitegen.DeletePersonalValuesForKeyParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), KeyID: keyID,
	})
}

func (r sqlitePersonalValues) DeleteForEnvironment(ctx context.Context, p authz.Proof) ([]domain.PrincipalID, error) {
	chain, env, err := envChain(p, authz.StorePersonalValuesDeleteForEnv, r.tok)
	if err != nil {
		return nil, err
	}
	if _, err := r.q.DeletePersonalValueNoticesForEnvironment(ctx, sqlitegen.DeletePersonalValueNoticesForEnvironmentParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
	}); err != nil {
		return nil, err
	}
	owners, err := r.q.DeletePersonalValuesForEnvironment(ctx, sqlitegen.DeletePersonalValuesForEnvironmentParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
	})
	out := make([]domain.PrincipalID, len(owners))
	for i, o := range owners {
		out[i] = domain.PrincipalID(o)
	}
	return out, err
}

func (r sqlitePersonalValues) ListForReencrypt(ctx context.Context, p authz.Proof, cursor string, limit int) ([]ReencryptFieldRow, error) {
	chain, err := authz.Verify(p, authz.StorePersonalValuesReencryptList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListPersonalValuesForReencrypt(ctx, sqlitegen.ListPersonalValuesForReencryptParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), Cursor: cursor, PageLimit: int64(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]ReencryptFieldRow, len(rows))
	for i, row := range rows {
		out[i] = ReencryptFieldRow{ID: row.ID, EnvironmentID: row.EnvironmentID, KeyID: row.KeyID, Owner: row.OwnerPrincipalID, Ciphertext: row.Ciphertext}
	}
	return out, nil
}

func (r sqlitePersonalValues) Reencrypt(ctx context.Context, p authz.Proof, id string, newCt, oldCt []byte) (bool, error) {
	chain, err := authz.Verify(p, authz.StorePersonalValuesReencrypt, r.tok)
	if err != nil {
		return false, err
	}
	n, err := r.q.ReencryptPersonalValue(ctx, sqlitegen.ReencryptPersonalValueParams{
		NewCiphertext: newCt, ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ID: id, OldCiphertext: oldCt,
	})
	return n == 1, err
}

func (r sqlitePersonalValues) NoticesOwn(ctx context.Context, p authz.Proof) ([]PersonalValueNotice, error) {
	chain, env, owner, err := ownCell(p, authz.StorePersonalValueNoticesListOwn, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListPersonalValueNoticesForOwner(ctx, sqlitegen.ListPersonalValueNoticesForOwnerParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, OwnerPrincipalID: string(owner),
	})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalValueNotice, 0, len(rows))
	for _, row := range rows {
		c, err := parseTime("personal value notice", row.KeyName, row.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, PersonalValueNotice{KeyName: row.KeyName, Owner: owner, Cause: row.Cause, CreatedAt: c})
	}
	return out, nil
}

func (r sqlitePersonalValues) DismissNotice(ctx context.Context, p authz.Proof, keyName string) (int64, error) {
	chain, env, owner, err := ownCell(p, authz.StorePersonalValueNoticesDismiss, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.DeletePersonalValueNotice(ctx, sqlitegen.DeletePersonalValueNoticeParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		KeyName: keyName, OwnerPrincipalID: string(owner),
	})
}

func (r sqlitePersonalValues) NoticesForKeyName(ctx context.Context, p authz.Proof, keyName string) ([]PersonalValueNotice, error) {
	chain, env, err := envChain(p, authz.StorePersonalValueNoticesForKey, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListPersonalValueNoticesForKeyName(ctx, sqlitegen.ListPersonalValueNoticesForKeyNameParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, KeyName: keyName,
	})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalValueNotice, 0, len(rows))
	for _, row := range rows {
		c, err := parseTime("personal value notice", keyName, row.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, PersonalValueNotice{KeyName: keyName, Owner: domain.PrincipalID(row.OwnerPrincipalID), Cause: row.Cause, CreatedAt: c})
	}
	return out, nil
}

func (r sqlitePersonalValues) RecordNotice(ctx context.Context, p authz.Proof, environmentID, keyName string, owner domain.PrincipalID, cause string, at time.Time) error {
	chain, err := authz.Verify(p, authz.StorePersonalValueNoticesRecord, r.tok)
	if err != nil {
		return err
	}
	if _, err := r.q.DeletePersonalValueNoticeAt(ctx, sqlitegen.DeletePersonalValueNoticeAtParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), EnvironmentID: environmentID,
		KeyName: keyName, OwnerPrincipalID: string(owner),
	}); err != nil {
		return err
	}
	return constraint(r.q.InsertPersonalValueNotice(ctx, sqlitegen.InsertPersonalValueNoticeParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), EnvironmentID: environmentID,
		KeyName: keyName, OwnerPrincipalID: string(owner), Cause: cause, CreatedAt: CanonTime(at).Format(timeFormat),
	}))
}

// --- postgres ---

type pgPersonalValues struct {
	q   *pggen.Queries
	tok *authz.TxToken
}

func (r pgRepos) PersonalValues() PersonalValueRepo {
	return pgPersonalValues{q: pggen.New(r.db), tok: r.tok}
}

func (r pgPersonalValues) Put(ctx context.Context, p authz.Proof, id, keyID string, ciphertext []byte, at time.Time) error {
	chain, env, owner, err := ownCell(p, authz.StorePersonalValuesPut, r.tok)
	if err != nil {
		return err
	}
	if _, err := r.q.DeletePersonalValueForCell(ctx, pggen.DeletePersonalValueForCellParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		KeyID: keyID, OwnerPrincipalID: string(owner),
	}); err != nil {
		return err
	}
	return constraint(r.q.InsertPersonalValue(ctx, pggen.InsertPersonalValueParams{
		ID: id, ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		KeyID: keyID, OwnerPrincipalID: string(owner), Ciphertext: ciphertext, CreatedAt: pgTimestamp(at), UpdatedAt: pgTimestamp(at),
	}))
}

func (r pgPersonalValues) GetOwn(ctx context.Context, p authz.Proof, keyID string) (PersonalValue, error) {
	chain, env, owner, err := ownCell(p, authz.StorePersonalValuesGetOwn, r.tok)
	if err != nil {
		return PersonalValue{}, err
	}
	row, err := r.q.GetPersonalValueForOwner(ctx, pggen.GetPersonalValueForOwnerParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		KeyID: keyID, OwnerPrincipalID: string(owner),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PersonalValue{}, domain.ErrNotFound
	}
	if err != nil {
		return PersonalValue{}, err
	}
	return PersonalValue{ID: row.ID, EnvironmentID: env, KeyID: row.KeyID, Owner: domain.PrincipalID(row.OwnerPrincipalID),
		Ciphertext: row.Ciphertext, CreatedAt: pgTimeToGo(row.CreatedAt), UpdatedAt: pgTimeToGo(row.UpdatedAt)}, nil
}

func (r pgPersonalValues) ListOwn(ctx context.Context, p authz.Proof) ([]PersonalValue, error) {
	chain, env, owner, err := ownCell(p, authz.StorePersonalValuesListOwn, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListPersonalValuesForOwner(ctx, pggen.ListPersonalValuesForOwnerParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, OwnerPrincipalID: string(owner),
	})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalValue, len(rows))
	for i, row := range rows {
		out[i] = PersonalValue{ID: row.ID, EnvironmentID: env, KeyID: row.KeyID, Owner: domain.PrincipalID(row.OwnerPrincipalID),
			Ciphertext: row.Ciphertext, CreatedAt: pgTimeToGo(row.CreatedAt), UpdatedAt: pgTimeToGo(row.UpdatedAt)}
	}
	return out, nil
}

func (r pgPersonalValues) DeleteOwn(ctx context.Context, p authz.Proof, keyID string) (int64, error) {
	chain, env, owner, err := ownCell(p, authz.StorePersonalValuesDeleteOwn, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.DeletePersonalValueForCell(ctx, pggen.DeletePersonalValueForCellParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		KeyID: keyID, OwnerPrincipalID: string(owner),
	})
}

func (r pgPersonalValues) CountProject(ctx context.Context, p authz.Proof) (int64, error) {
	chain, err := authz.Verify(p, authz.StorePersonalValuesCountProject, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.CountPersonalValuesForProject(ctx, pggen.CountPersonalValuesForProjectParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project),
	})
}

func (r pgPersonalValues) Holders(ctx context.Context, p authz.Proof, keyID string) ([]PersonalValueHolder, error) {
	chain, env, err := envChain(p, authz.StorePersonalValuesHolders, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListPersonalValueHolders(ctx, pggen.ListPersonalValueHoldersParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, KeyID: keyID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalValueHolder, len(rows))
	for i, row := range rows {
		out[i] = PersonalValueHolder{Owner: domain.PrincipalID(row.OwnerPrincipalID), UpdatedAt: pgTimeToGo(row.UpdatedAt)}
	}
	return out, nil
}

func (r pgPersonalValues) DeleteMember(ctx context.Context, p authz.Proof, keyID string, owner domain.PrincipalID) (int64, error) {
	chain, env, err := envChain(p, authz.StorePersonalValuesDeleteMember, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.DeletePersonalValueForCell(ctx, pggen.DeletePersonalValueForCellParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		KeyID: keyID, OwnerPrincipalID: string(owner),
	})
}

func (r pgPersonalValues) CellsForKey(ctx context.Context, p authz.Proof, keyID string) ([]PersonalValueCell, error) {
	chain, err := authz.Verify(p, authz.StorePersonalValuesCellsForKey, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListPersonalValueCellsForKey(ctx, pggen.ListPersonalValueCellsForKeyParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), KeyID: keyID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalValueCell, len(rows))
	for i, row := range rows {
		out[i] = PersonalValueCell{EnvironmentID: row.EnvironmentID, Owner: domain.PrincipalID(row.OwnerPrincipalID)}
	}
	return out, nil
}

func (r pgPersonalValues) DeleteForKey(ctx context.Context, p authz.Proof, keyID string) (int64, error) {
	chain, err := authz.Verify(p, authz.StorePersonalValuesDeleteForKey, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.DeletePersonalValuesForKey(ctx, pggen.DeletePersonalValuesForKeyParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), KeyID: keyID,
	})
}

func (r pgPersonalValues) DeleteForEnvironment(ctx context.Context, p authz.Proof) ([]domain.PrincipalID, error) {
	chain, env, err := envChain(p, authz.StorePersonalValuesDeleteForEnv, r.tok)
	if err != nil {
		return nil, err
	}
	if _, err := r.q.DeletePersonalValueNoticesForEnvironment(ctx, pggen.DeletePersonalValueNoticesForEnvironmentParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
	}); err != nil {
		return nil, err
	}
	owners, err := r.q.DeletePersonalValuesForEnvironment(ctx, pggen.DeletePersonalValuesForEnvironmentParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
	})
	out := make([]domain.PrincipalID, len(owners))
	for i, o := range owners {
		out[i] = domain.PrincipalID(o)
	}
	return out, err
}

func (r pgPersonalValues) ListForReencrypt(ctx context.Context, p authz.Proof, cursor string, limit int) ([]ReencryptFieldRow, error) {
	chain, err := authz.Verify(p, authz.StorePersonalValuesReencryptList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListPersonalValuesForReencrypt(ctx, pggen.ListPersonalValuesForReencryptParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), Cursor: cursor, PageLimit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]ReencryptFieldRow, len(rows))
	for i, row := range rows {
		out[i] = ReencryptFieldRow{ID: row.ID, EnvironmentID: row.EnvironmentID, KeyID: row.KeyID, Owner: row.OwnerPrincipalID, Ciphertext: row.Ciphertext}
	}
	return out, nil
}

func (r pgPersonalValues) Reencrypt(ctx context.Context, p authz.Proof, id string, newCt, oldCt []byte) (bool, error) {
	chain, err := authz.Verify(p, authz.StorePersonalValuesReencrypt, r.tok)
	if err != nil {
		return false, err
	}
	n, err := r.q.ReencryptPersonalValue(ctx, pggen.ReencryptPersonalValueParams{
		NewCiphertext: newCt, ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ID: id, OldCiphertext: oldCt,
	})
	return n == 1, err
}

func (r pgPersonalValues) NoticesOwn(ctx context.Context, p authz.Proof) ([]PersonalValueNotice, error) {
	chain, env, owner, err := ownCell(p, authz.StorePersonalValueNoticesListOwn, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListPersonalValueNoticesForOwner(ctx, pggen.ListPersonalValueNoticesForOwnerParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, OwnerPrincipalID: string(owner),
	})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalValueNotice, len(rows))
	for i, row := range rows {
		out[i] = PersonalValueNotice{KeyName: row.KeyName, Owner: owner, Cause: row.Cause, CreatedAt: pgTimeToGo(row.CreatedAt)}
	}
	return out, nil
}

func (r pgPersonalValues) DismissNotice(ctx context.Context, p authz.Proof, keyName string) (int64, error) {
	chain, env, owner, err := ownCell(p, authz.StorePersonalValueNoticesDismiss, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.DeletePersonalValueNotice(ctx, pggen.DeletePersonalValueNoticeParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		KeyName: keyName, OwnerPrincipalID: string(owner),
	})
}

func (r pgPersonalValues) NoticesForKeyName(ctx context.Context, p authz.Proof, keyName string) ([]PersonalValueNotice, error) {
	chain, env, err := envChain(p, authz.StorePersonalValueNoticesForKey, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListPersonalValueNoticesForKeyName(ctx, pggen.ListPersonalValueNoticesForKeyNameParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, KeyName: keyName,
	})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalValueNotice, len(rows))
	for i, row := range rows {
		out[i] = PersonalValueNotice{KeyName: keyName, Owner: domain.PrincipalID(row.OwnerPrincipalID), Cause: row.Cause, CreatedAt: pgTimeToGo(row.CreatedAt)}
	}
	return out, nil
}

func (r pgPersonalValues) RecordNotice(ctx context.Context, p authz.Proof, environmentID, keyName string, owner domain.PrincipalID, cause string, at time.Time) error {
	chain, err := authz.Verify(p, authz.StorePersonalValueNoticesRecord, r.tok)
	if err != nil {
		return err
	}
	if _, err := r.q.DeletePersonalValueNoticeAt(ctx, pggen.DeletePersonalValueNoticeAtParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), EnvironmentID: environmentID,
		KeyName: keyName, OwnerPrincipalID: string(owner),
	}); err != nil {
		return err
	}
	return constraint(r.q.InsertPersonalValueNotice(ctx, pggen.InsertPersonalValueNoticeParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), EnvironmentID: environmentID,
		KeyName: keyName, OwnerPrincipalID: string(owner), Cause: cause, CreatedAt: pgTimestamp(at),
	}))
}
