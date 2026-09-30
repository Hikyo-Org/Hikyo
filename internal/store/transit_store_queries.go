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

type transitStoreQueries interface {
	transitListKeys(ctx context.Context, chain domain.Scope) ([]TransitKeyRecord, error)
	transitGetKey(ctx context.Context, chain domain.Scope, name string) (TransitKeyRecord, error)
	transitGetKeyForUse(ctx context.Context, chain domain.Scope, name string) (TransitKeyRecord, error)
	transitAdmissionLock(ctx context.Context, chain domain.Scope) (string, error)
	transitCountKeys(ctx context.Context, chain domain.Scope) (int64, error)
	transitListVersions(ctx context.Context, chain domain.Scope, keyID string) ([]TransitVersionRecord, error)
	transitListCallers(ctx context.Context, chain domain.Scope, keyID string) ([]TransitCaller, error)
	transitVersionMaterial(ctx context.Context, chain domain.Scope, keyID string, version uint32) (TransitVersionMaterial, error)
	transitInsertVersion(ctx context.Context, chain domain.Scope, keyID string, v TransitVersionCreate) error
	transitDeleteCallers(ctx context.Context, chain domain.Scope, keyID string) error
	transitInsertCaller(ctx context.Context, chain domain.Scope, keyID string, caller TransitCaller) error
	transitInsertKey(ctx context.Context, chain domain.Scope, m TransitKeyCreate) error
	transitKeyByID(ctx context.Context, chain domain.Scope, id string) (TransitKeyRecord, error)
	transitCountExistingKey(ctx context.Context, chain domain.Scope, id string) (int64, error)
	transitCompromisedThrough(ctx context.Context, chain domain.Scope, id string) (int64, error)
	transitTrimFloor(ctx context.Context, chain domain.Scope, id string) (int64, error)
	transitAvailableTrimFloor(ctx context.Context, chain domain.Scope, id string) (int64, error)
	transitAppendVersion(ctx context.Context, chain domain.Scope, id string, expectLatest uint32, v TransitVersionCreate) (int64, error)
	transitCountVersions(ctx context.Context, chain domain.Scope, keyID string) (int64, error)
	transitConfigure(ctx context.Context, chain domain.Scope, m TransitKeyConfig) (int64, error)
	transitChangeState(ctx context.Context, chain domain.Scope, m TransitStateChange) (int64, error)
	transitCompromise(ctx context.Context, chain domain.Scope, id string, at time.Time) (int64, error)
	transitFenceTrim(ctx context.Context, chain domain.Scope, id string, at time.Time) (int64, error)
	transitTrimVersions(ctx context.Context, chain domain.Scope, keyID string, through uint32) (int64, error)
	transitListReencrypt(ctx context.Context, chain domain.Scope, cursor string, limit int) ([]ReencryptFieldRow, error)
	transitReencrypt(ctx context.Context, chain domain.Scope, id string, newCT, oldCT []byte) (int64, error)
	transitSelectDeletionDue(ctx context.Context, now time.Time, afterID string, limit int) ([]TransitDueKey, error)
	transitSelectRotationDue(ctx context.Context, afterID string, limit int) ([]TransitDueKey, error)
	transitFencePurge(ctx context.Context, chain domain.Scope, id string, now time.Time) (int64, error)
	transitExternalVersions(ctx context.Context, chain domain.Scope, keyID string) ([]TransitVersionMaterial, error)
	transitDestroy(ctx context.Context, chain domain.Scope, id string, now time.Time) (int64, error)
	transitEraseMaterial(ctx context.Context, chain domain.Scope, keyID string) (int64, error)
	transitCountLive(ctx context.Context) (int64, error)
	transitCountPendingDeletion(ctx context.Context) (int64, error)
	transitGaugeRotationCandidates(ctx context.Context) ([]transitRotationCandidate, error)
}
type sqliteTransitStoreQueries struct {
	queries        *sqlitegen.Queries
	mapConstraints bool
}
type pgTransitStoreQueries struct {
	queries        *pggen.Queries
	mapConstraints bool
}
type transitRotationCandidate struct {
	period    int64
	createdAt *time.Time
}

func (d sqliteAdoptDB) transitStoreQueries() transitStoreQueries {
	return sqliteTransitStoreQueries{queries: sqlitegen.New(d.db), mapConstraints: true}
}
func (d pgAdoptDB) transitStoreQueries() transitStoreQueries {
	return pgTransitStoreQueries{queries: pggen.New(d.db), mapConstraints: true}
}
func (d sqliteAdapterTx) transitStoreQueries() transitStoreQueries {
	return sqliteTransitStoreQueries{queries: sqlitegen.New(d.tx)}
}
func (d pgAdapterTx) transitStoreQueries() transitStoreQueries {
	return pgTransitStoreQueries{queries: pggen.New(d.tx)}
}

func (q sqliteTransitStoreQueries) transitListKeys(ctx context.Context, chain domain.Scope) ([]TransitKeyRecord, error) {
	c, err := q.queries.TransitListKeys(ctx, sqlitegen.TransitListKeysParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	var out []TransitKeyRecord
	for _, c := range c {
		item, err := sqliteTransitKey(sqlitegen.TransitKeyByIDRow(c))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (q sqliteTransitStoreQueries) transitGetKey(ctx context.Context, chain domain.Scope, name string) (TransitKeyRecord, error) {
	c, err := q.queries.TransitGetKey(ctx, sqlitegen.TransitGetKeyParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), Name: name})
	if isNoRows(err) {
		return TransitKeyRecord{}, ErrNotFound
	}
	if err != nil {
		return TransitKeyRecord{}, err
	}
	item, err := sqliteTransitKey(sqlitegen.TransitKeyByIDRow(c))
	if err != nil {
		return TransitKeyRecord{}, err
	}
	return item, nil
}

func (q sqliteTransitStoreQueries) transitGetKeyForUse(ctx context.Context, chain domain.Scope, name string) (TransitKeyRecord, error) {
	c, err := q.queries.TransitGetKeyForUse(ctx, sqlitegen.TransitGetKeyForUseParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), Name: name})
	if isNoRows(err) {
		return TransitKeyRecord{}, ErrNotFound
	}
	if err != nil {
		return TransitKeyRecord{}, err
	}
	item, err := sqliteTransitKey(sqlitegen.TransitKeyByIDRow(c))
	if err != nil {
		return TransitKeyRecord{}, err
	}
	return item, nil
}

func (q sqliteTransitStoreQueries) transitAdmissionLock(ctx context.Context, chain domain.Scope) (string, error) {
	return q.queries.TransitAdmissionLock(ctx, sqlitegen.TransitAdmissionLockParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q sqliteTransitStoreQueries) transitCountKeys(ctx context.Context, chain domain.Scope) (int64, error) {
	return q.queries.TransitCountKeys(ctx, sqlitegen.TransitCountKeysParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q sqliteTransitStoreQueries) transitListVersions(ctx context.Context, chain domain.Scope, keyID string) ([]TransitVersionRecord, error) {
	c, err := q.queries.TransitListVersions(ctx, sqlitegen.TransitListVersionsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID})
	if err != nil {
		return nil, err
	}
	var out []TransitVersionRecord
	for _, c := range c {
		item, err := sqliteTransitVersion(sqlitegen.TransitListVersionsRow(c))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (q sqliteTransitStoreQueries) transitListCallers(ctx context.Context, chain domain.Scope, keyID string) ([]TransitCaller, error) {
	c, err := q.queries.TransitListCallers(ctx, sqlitegen.TransitListCallersParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID})
	if err != nil {
		return nil, err
	}
	out := []TransitCaller{}
	for _, c := range c {
		item, err := sqliteTransitCaller(sqlitegen.TransitListCallersRow(c))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (q sqliteTransitStoreQueries) transitVersionMaterial(ctx context.Context, chain domain.Scope, keyID string, version uint32) (TransitVersionMaterial, error) {
	c, err := q.queries.TransitVersionMaterial(ctx, sqlitegen.TransitVersionMaterialParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID, Version: int64(version)})
	if isNoRows(err) {
		return TransitVersionMaterial{}, ErrNotFound
	}
	if err != nil {
		return TransitVersionMaterial{}, err
	}
	item, err := sqliteTransitMaterial(sqlitegen.TransitVersionMaterialRow(c))
	if err != nil {
		return TransitVersionMaterial{}, err
	}
	return item, nil
}

func (q sqliteTransitStoreQueries) transitInsertVersion(ctx context.Context, chain domain.Scope, keyID string, v TransitVersionCreate) error {
	return q.transitWriteError(q.queries.TransitInsertVersion(ctx, sqlitegen.TransitInsertVersionParams{ID: v.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID, Version: int64(v.Version), MaterialCiphertext: optionalStoredBytes(v.Sealed), ExternalRef: sql.NullString{String: v.ExternalRef, Valid: v.ExternalRef != ""}, PublicKey: optionalStoredBytes(v.PublicKey), At: fixedStamp(v.At)}))
}

func (q sqliteTransitStoreQueries) transitDeleteCallers(ctx context.Context, chain domain.Scope, keyID string) error {
	return q.transitWriteError(q.queries.TransitDeleteCallers(ctx, sqlitegen.TransitDeleteCallersParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID}))
}

func (q sqliteTransitStoreQueries) transitInsertCaller(ctx context.Context, chain domain.Scope, keyID string, caller TransitCaller) error {
	return q.transitWriteError(q.queries.TransitInsertCaller(ctx, sqlitegen.TransitInsertCallerParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID, PrincipalID: caller.PrincipalID, Operations: encodeTransitOps(caller.Operations)}))
}

func (q sqliteTransitStoreQueries) transitInsertKey(ctx context.Context, chain domain.Scope, m TransitKeyCreate) error {
	return q.transitWriteError(q.queries.TransitInsertKey(ctx, sqlitegen.TransitInsertKeyParams{ID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), Name: m.Name, Algorithm: m.Algorithm, Custody: m.Custody, AllowedOperations: encodeTransitOps(m.AllowedOperations), RotationPeriodSeconds: m.RotationPeriodSeconds, CreatedBy: m.CreatedBy, At: fixedStamp(m.At)}))
}

func (q sqliteTransitStoreQueries) transitKeyByID(ctx context.Context, chain domain.Scope, id string) (TransitKeyRecord, error) {
	c, err := q.queries.TransitKeyByID(ctx, sqlitegen.TransitKeyByIDParams{ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return TransitKeyRecord{}, ErrNotFound
	}
	if err != nil {
		return TransitKeyRecord{}, err
	}
	item, err := sqliteTransitKey(sqlitegen.TransitKeyByIDRow(c))
	if err != nil {
		return TransitKeyRecord{}, err
	}
	return item, nil
}

func (q sqliteTransitStoreQueries) transitCountExistingKey(ctx context.Context, chain domain.Scope, id string) (int64, error) {
	return q.queries.TransitCountExistingKey(ctx, sqlitegen.TransitCountExistingKeyParams{ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q sqliteTransitStoreQueries) transitCompromisedThrough(ctx context.Context, chain domain.Scope, id string) (int64, error) {
	return q.queries.TransitCompromisedThrough(ctx, sqlitegen.TransitCompromisedThroughParams{ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q sqliteTransitStoreQueries) transitTrimFloor(ctx context.Context, chain domain.Scope, id string) (int64, error) {
	return q.queries.TransitTrimFloor(ctx, sqlitegen.TransitTrimFloorParams{ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q sqliteTransitStoreQueries) transitAvailableTrimFloor(ctx context.Context, chain domain.Scope, id string) (int64, error) {
	return q.queries.TransitAvailableTrimFloor(ctx, sqlitegen.TransitAvailableTrimFloorParams{ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q sqliteTransitStoreQueries) transitAppendVersion(ctx context.Context, chain domain.Scope, id string, expectLatest uint32, v TransitVersionCreate) (int64, error) {
	return q.transitWriteRows(q.queries.TransitAppendVersion(ctx, sqlitegen.TransitAppendVersionParams{Version: int64(v.Version), At: fixedStamp(v.At), ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), ExpectLatest: int64(expectLatest)}))
}

func (q sqliteTransitStoreQueries) transitCountVersions(ctx context.Context, chain domain.Scope, keyID string) (int64, error) {
	return q.queries.TransitCountVersions(ctx, sqlitegen.TransitCountVersionsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID})
}

func (q sqliteTransitStoreQueries) transitConfigure(ctx context.Context, chain domain.Scope, m TransitKeyConfig) (int64, error) {
	return q.transitWriteRows(q.queries.TransitConfigure(ctx, sqlitegen.TransitConfigureParams{MinEncrypt: int64(m.MinEncryptVersion), MinDecrypt: int64(m.MinDecryptVersion), RotationPeriod: m.RotationPeriodSeconds, At: fixedStamp(m.At), ID: m.KeyID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q sqliteTransitStoreQueries) transitChangeState(ctx context.Context, chain domain.Scope, m TransitStateChange) (int64, error) {
	return q.transitWriteRows(q.queries.TransitChangeState(ctx, sqlitegen.TransitChangeStateParams{NextState: m.To, DeletionAfter: sql.NullString{String: fixedStamp(m.DeletionAfter), Valid: m.To == "pending-deletion"}, At: fixedStamp(m.At), ID: m.KeyID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), FromStates: m.From}))
}

func (q sqliteTransitStoreQueries) transitCompromise(ctx context.Context, chain domain.Scope, id string, at time.Time) (int64, error) {
	return q.transitWriteRows(q.queries.TransitCompromise(ctx, sqlitegen.TransitCompromiseParams{At: fixedStamp(at), ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q sqliteTransitStoreQueries) transitFenceTrim(ctx context.Context, chain domain.Scope, id string, at time.Time) (int64, error) {
	return q.transitWriteRows(q.queries.TransitFenceTrim(ctx, sqlitegen.TransitFenceTrimParams{At: fixedStamp(at), ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q sqliteTransitStoreQueries) transitTrimVersions(ctx context.Context, chain domain.Scope, keyID string, through uint32) (int64, error) {
	return q.transitWriteRows(q.queries.TransitTrimVersions(ctx, sqlitegen.TransitTrimVersionsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID, Through: int64(through)}))
}

func (q sqliteTransitStoreQueries) transitListReencrypt(ctx context.Context, chain domain.Scope, cursor string, limit int) ([]ReencryptFieldRow, error) {
	c, err := q.queries.TransitListReencrypt(ctx, sqlitegen.TransitListReencryptParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Cursor: cursor, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	var out []ReencryptFieldRow
	for _, c := range c {
		item, err := sqliteTransitReencrypt(sqlitegen.TransitListReencryptRow(c))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (q sqliteTransitStoreQueries) transitReencrypt(ctx context.Context, chain domain.Scope, id string, newCT, oldCT []byte) (int64, error) {
	return q.transitWriteRows(q.queries.TransitReencrypt(ctx, sqlitegen.TransitReencryptParams{NewCt: newCT, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ID: id, OldCt: oldCT}))
}

func (q sqliteTransitStoreQueries) transitSelectDeletionDue(ctx context.Context, now time.Time, afterID string, limit int) ([]TransitDueKey, error) {
	c, err := q.queries.TransitSelectDeletionDue(ctx, sqlitegen.TransitSelectDeletionDueParams{Now: runtimeSQLiteStamp(now), AfterID: afterID, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	var out []TransitDueKey
	for _, c := range c {
		item, err := sqliteTransitDeletion(sqlitegen.TransitSelectDeletionDueRow(c))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (q sqliteTransitStoreQueries) transitSelectRotationDue(ctx context.Context, afterID string, limit int) ([]TransitDueKey, error) {
	c, err := q.queries.TransitSelectRotationDue(ctx, sqlitegen.TransitSelectRotationDueParams{AfterID: afterID, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	var out []TransitDueKey
	for _, c := range c {
		item, err := sqliteTransitRotation(sqlitegen.TransitSelectRotationDueRow(c))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (q sqliteTransitStoreQueries) transitFencePurge(ctx context.Context, chain domain.Scope, id string, now time.Time) (int64, error) {
	return q.transitWriteRows(q.queries.TransitFencePurge(ctx, sqlitegen.TransitFencePurgeParams{ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), Now: runtimeSQLiteStamp(now)}))
}

func (q sqliteTransitStoreQueries) transitExternalVersions(ctx context.Context, chain domain.Scope, keyID string) ([]TransitVersionMaterial, error) {
	c, err := q.queries.TransitExternalVersions(ctx, sqlitegen.TransitExternalVersionsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID})
	if err != nil {
		return nil, err
	}
	var out []TransitVersionMaterial
	for _, c := range c {
		item, err := sqliteTransitExternal(sqlitegen.TransitExternalVersionsRow(c))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (q sqliteTransitStoreQueries) transitDestroy(ctx context.Context, chain domain.Scope, id string, now time.Time) (int64, error) {
	return q.transitWriteRows(q.queries.TransitDestroy(ctx, sqlitegen.TransitDestroyParams{Now: fixedStamp(now), ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q sqliteTransitStoreQueries) transitEraseMaterial(ctx context.Context, chain domain.Scope, keyID string) (int64, error) {
	return q.transitWriteRows(q.queries.TransitEraseMaterial(ctx, sqlitegen.TransitEraseMaterialParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID}))
}

func (q sqliteTransitStoreQueries) transitCountLive(ctx context.Context) (int64, error) {
	return q.queries.TransitCountLive(ctx)
}

func (q sqliteTransitStoreQueries) transitCountPendingDeletion(ctx context.Context) (int64, error) {
	return q.queries.TransitCountPendingDeletion(ctx)
}

func (q sqliteTransitStoreQueries) transitGaugeRotationCandidates(ctx context.Context) ([]transitRotationCandidate, error) {
	c, err := q.queries.TransitGaugeRotationCandidates(ctx)
	if err != nil {
		return nil, err
	}
	var out []transitRotationCandidate
	for _, c := range c {
		item, err := sqliteTransitGauge(sqlitegen.TransitGaugeRotationCandidatesRow(c))
		if err != nil {
			return out, err
		}
		out = append(out, item)
	}
	return out, nil
}
func (q sqliteTransitStoreQueries) transitWriteError(err error) error {
	if q.mapConstraints {
		return constraint(err)
	}
	return err
}
func (q sqliteTransitStoreQueries) transitWriteRows(n int64, err error) (int64, error) {
	return n, q.transitWriteError(err)
}

func (q pgTransitStoreQueries) transitListKeys(ctx context.Context, chain domain.Scope) ([]TransitKeyRecord, error) {
	c, err := q.queries.TransitListKeys(ctx, pggen.TransitListKeysParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	var out []TransitKeyRecord
	for _, c := range c {
		item, err := pgTransitKey(pggen.TransitKeyByIDRow(c))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (q pgTransitStoreQueries) transitGetKey(ctx context.Context, chain domain.Scope, name string) (TransitKeyRecord, error) {
	c, err := q.queries.TransitGetKey(ctx, pggen.TransitGetKeyParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), Name: name})
	if isNoRows(err) {
		return TransitKeyRecord{}, ErrNotFound
	}
	if err != nil {
		return TransitKeyRecord{}, err
	}
	item, err := pgTransitKey(pggen.TransitKeyByIDRow(c))
	if err != nil {
		return TransitKeyRecord{}, err
	}
	return item, nil
}

func (q pgTransitStoreQueries) transitGetKeyForUse(ctx context.Context, chain domain.Scope, name string) (TransitKeyRecord, error) {
	c, err := q.queries.TransitGetKeyForUse(ctx, pggen.TransitGetKeyForUseParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), Name: name})
	if isNoRows(err) {
		return TransitKeyRecord{}, ErrNotFound
	}
	if err != nil {
		return TransitKeyRecord{}, err
	}
	item, err := pgTransitKey(pggen.TransitKeyByIDRow(c))
	if err != nil {
		return TransitKeyRecord{}, err
	}
	return item, nil
}

func (q pgTransitStoreQueries) transitAdmissionLock(ctx context.Context, chain domain.Scope) (string, error) {
	return q.queries.TransitAdmissionLock(ctx, pggen.TransitAdmissionLockParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q pgTransitStoreQueries) transitCountKeys(ctx context.Context, chain domain.Scope) (int64, error) {
	return q.queries.TransitCountKeys(ctx, pggen.TransitCountKeysParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q pgTransitStoreQueries) transitListVersions(ctx context.Context, chain domain.Scope, keyID string) ([]TransitVersionRecord, error) {
	c, err := q.queries.TransitListVersions(ctx, pggen.TransitListVersionsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID})
	if err != nil {
		return nil, err
	}
	var out []TransitVersionRecord
	for _, c := range c {
		item, err := pgTransitVersion(pggen.TransitListVersionsRow(c))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (q pgTransitStoreQueries) transitListCallers(ctx context.Context, chain domain.Scope, keyID string) ([]TransitCaller, error) {
	c, err := q.queries.TransitListCallers(ctx, pggen.TransitListCallersParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID})
	if err != nil {
		return nil, err
	}
	out := []TransitCaller{}
	for _, c := range c {
		item, err := pgTransitCaller(pggen.TransitListCallersRow(c))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (q pgTransitStoreQueries) transitVersionMaterial(ctx context.Context, chain domain.Scope, keyID string, version uint32) (TransitVersionMaterial, error) {
	checkedVersion, err := checkedPGInt32(int64(version))
	if err != nil {
		return TransitVersionMaterial{}, err
	}

	c, err := q.queries.TransitVersionMaterial(ctx, pggen.TransitVersionMaterialParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID, Version: checkedVersion})
	if isNoRows(err) {
		return TransitVersionMaterial{}, ErrNotFound
	}
	if err != nil {
		return TransitVersionMaterial{}, err
	}
	item, err := pgTransitMaterial(pggen.TransitVersionMaterialRow(c))
	if err != nil {
		return TransitVersionMaterial{}, err
	}
	return item, nil
}

func (q pgTransitStoreQueries) transitInsertVersion(ctx context.Context, chain domain.Scope, keyID string, v TransitVersionCreate) error {
	checkedVVersion, err := checkedPGInt32(int64(v.Version))
	if err != nil {
		return err
	}

	return q.transitWriteError(q.queries.TransitInsertVersion(ctx, pggen.TransitInsertVersionParams{ID: v.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID, Version: checkedVVersion, MaterialCiphertext: optionalStoredBytes(v.Sealed), ExternalRef: pgtype.Text{String: v.ExternalRef, Valid: v.ExternalRef != ""}, PublicKey: optionalStoredBytes(v.PublicKey), At: pgRequiredTime(v.At)}))
}

func (q pgTransitStoreQueries) transitDeleteCallers(ctx context.Context, chain domain.Scope, keyID string) error {
	return q.transitWriteError(q.queries.TransitDeleteCallers(ctx, pggen.TransitDeleteCallersParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID}))
}

func (q pgTransitStoreQueries) transitInsertCaller(ctx context.Context, chain domain.Scope, keyID string, caller TransitCaller) error {
	return q.transitWriteError(q.queries.TransitInsertCaller(ctx, pggen.TransitInsertCallerParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID, PrincipalID: caller.PrincipalID, Operations: encodeTransitOps(caller.Operations)}))
}

func (q pgTransitStoreQueries) transitInsertKey(ctx context.Context, chain domain.Scope, m TransitKeyCreate) error {
	return q.transitWriteError(q.queries.TransitInsertKey(ctx, pggen.TransitInsertKeyParams{ID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), Name: m.Name, Algorithm: m.Algorithm, Custody: m.Custody, AllowedOperations: encodeTransitOps(m.AllowedOperations), RotationPeriodSeconds: m.RotationPeriodSeconds, CreatedBy: m.CreatedBy, At: pgRequiredTime(m.At)}))
}

func (q pgTransitStoreQueries) transitKeyByID(ctx context.Context, chain domain.Scope, id string) (TransitKeyRecord, error) {
	c, err := q.queries.TransitKeyByID(ctx, pggen.TransitKeyByIDParams{ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return TransitKeyRecord{}, ErrNotFound
	}
	if err != nil {
		return TransitKeyRecord{}, err
	}
	item, err := pgTransitKey(pggen.TransitKeyByIDRow(c))
	if err != nil {
		return TransitKeyRecord{}, err
	}
	return item, nil
}

func (q pgTransitStoreQueries) transitCountExistingKey(ctx context.Context, chain domain.Scope, id string) (int64, error) {
	return q.queries.TransitCountExistingKey(ctx, pggen.TransitCountExistingKeyParams{ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q pgTransitStoreQueries) transitCompromisedThrough(ctx context.Context, chain domain.Scope, id string) (int64, error) {
	value, err := q.queries.TransitCompromisedThrough(ctx, pggen.TransitCompromisedThroughParams{ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return int64(value), err
}

func (q pgTransitStoreQueries) transitTrimFloor(ctx context.Context, chain domain.Scope, id string) (int64, error) {
	value, err := q.queries.TransitTrimFloor(ctx, pggen.TransitTrimFloorParams{ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return int64(value), err
}

func (q pgTransitStoreQueries) transitAvailableTrimFloor(ctx context.Context, chain domain.Scope, id string) (int64, error) {
	value, err := q.queries.TransitAvailableTrimFloor(ctx, pggen.TransitAvailableTrimFloorParams{ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return int64(value), err
}

func (q pgTransitStoreQueries) transitAppendVersion(ctx context.Context, chain domain.Scope, id string, expectLatest uint32, v TransitVersionCreate) (int64, error) {
	version, err := checkedPGInt32(int64(v.Version))
	if err != nil {
		return 0, err
	}
	expected, err := checkedPGInt32(int64(expectLatest))
	if err != nil {
		return 0, err
	}
	return q.transitWriteRows(q.queries.TransitAppendVersion(ctx, pggen.TransitAppendVersionParams{Version: version, At: pgRequiredTime(v.At), ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), ExpectLatest: expected}))
}

func (q pgTransitStoreQueries) transitCountVersions(ctx context.Context, chain domain.Scope, keyID string) (int64, error) {
	return q.queries.TransitCountVersions(ctx, pggen.TransitCountVersionsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID})
}

func (q pgTransitStoreQueries) transitConfigure(ctx context.Context, chain domain.Scope, m TransitKeyConfig) (int64, error) {
	minEncrypt, err := checkedPGInt32(int64(m.MinEncryptVersion))
	if err != nil {
		return 0, err
	}
	minDecrypt, err := checkedPGInt32(int64(m.MinDecryptVersion))
	if err != nil {
		return 0, err
	}
	return q.transitWriteRows(q.queries.TransitConfigure(ctx, pggen.TransitConfigureParams{MinEncrypt: minEncrypt, MinDecrypt: minDecrypt, RotationPeriod: m.RotationPeriodSeconds, At: pgRequiredTime(m.At), ID: m.KeyID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q pgTransitStoreQueries) transitChangeState(ctx context.Context, chain domain.Scope, m TransitStateChange) (int64, error) {
	return q.transitWriteRows(q.queries.TransitChangeState(ctx, pggen.TransitChangeStateParams{NextState: m.To, DeletionAfter: pgtype.Timestamptz{Time: CanonTime(m.DeletionAfter), Valid: m.To == "pending-deletion"}, At: pgRequiredTime(m.At), ID: m.KeyID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), FromStates: m.From}))
}

func (q pgTransitStoreQueries) transitCompromise(ctx context.Context, chain domain.Scope, id string, at time.Time) (int64, error) {
	return q.transitWriteRows(q.queries.TransitCompromise(ctx, pggen.TransitCompromiseParams{At: pgRequiredTime(at), ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q pgTransitStoreQueries) transitFenceTrim(ctx context.Context, chain domain.Scope, id string, at time.Time) (int64, error) {
	return q.transitWriteRows(q.queries.TransitFenceTrim(ctx, pggen.TransitFenceTrimParams{At: pgRequiredTime(at), ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q pgTransitStoreQueries) transitTrimVersions(ctx context.Context, chain domain.Scope, keyID string, through uint32) (int64, error) {
	checkedThrough, err := checkedPGInt32(int64(through))
	if err != nil {
		return 0, err
	}

	return q.transitWriteRows(q.queries.TransitTrimVersions(ctx, pggen.TransitTrimVersionsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID, Through: checkedThrough}))
}

func (q pgTransitStoreQueries) transitListReencrypt(ctx context.Context, chain domain.Scope, cursor string, limit int) ([]ReencryptFieldRow, error) {
	c, err := q.queries.TransitListReencrypt(ctx, pggen.TransitListReencryptParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Cursor: cursor, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	var out []ReencryptFieldRow
	for _, c := range c {
		item, err := pgTransitReencrypt(pggen.TransitListReencryptRow(c))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (q pgTransitStoreQueries) transitReencrypt(ctx context.Context, chain domain.Scope, id string, newCT, oldCT []byte) (int64, error) {
	return q.transitWriteRows(q.queries.TransitReencrypt(ctx, pggen.TransitReencryptParams{NewCt: newCT, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ID: id, OldCt: oldCT}))
}

func (q pgTransitStoreQueries) transitSelectDeletionDue(ctx context.Context, now time.Time, afterID string, limit int) ([]TransitDueKey, error) {
	c, err := q.queries.TransitSelectDeletionDue(ctx, pggen.TransitSelectDeletionDueParams{Now: pgRequiredTime(now), AfterID: afterID, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	var out []TransitDueKey
	for _, c := range c {
		item, err := pgTransitDeletion(pggen.TransitSelectDeletionDueRow(c))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (q pgTransitStoreQueries) transitSelectRotationDue(ctx context.Context, afterID string, limit int) ([]TransitDueKey, error) {
	c, err := q.queries.TransitSelectRotationDue(ctx, pggen.TransitSelectRotationDueParams{AfterID: afterID, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	var out []TransitDueKey
	for _, c := range c {
		item, err := pgTransitRotation(pggen.TransitSelectRotationDueRow(c))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (q pgTransitStoreQueries) transitFencePurge(ctx context.Context, chain domain.Scope, id string, now time.Time) (int64, error) {
	return q.transitWriteRows(q.queries.TransitFencePurge(ctx, pggen.TransitFencePurgeParams{ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), Now: pgRequiredTime(now)}))
}

func (q pgTransitStoreQueries) transitExternalVersions(ctx context.Context, chain domain.Scope, keyID string) ([]TransitVersionMaterial, error) {
	c, err := q.queries.TransitExternalVersions(ctx, pggen.TransitExternalVersionsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID})
	if err != nil {
		return nil, err
	}
	var out []TransitVersionMaterial
	for _, c := range c {
		item, err := pgTransitExternal(pggen.TransitExternalVersionsRow(c))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (q pgTransitStoreQueries) transitDestroy(ctx context.Context, chain domain.Scope, id string, now time.Time) (int64, error) {
	return q.transitWriteRows(q.queries.TransitDestroy(ctx, pggen.TransitDestroyParams{Now: pgRequiredTime(now), ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q pgTransitStoreQueries) transitEraseMaterial(ctx context.Context, chain domain.Scope, keyID string) (int64, error) {
	return q.transitWriteRows(q.queries.TransitEraseMaterial(ctx, pggen.TransitEraseMaterialParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID}))
}

func (q pgTransitStoreQueries) transitCountLive(ctx context.Context) (int64, error) {
	return q.queries.TransitCountLive(ctx)
}

func (q pgTransitStoreQueries) transitCountPendingDeletion(ctx context.Context) (int64, error) {
	return q.queries.TransitCountPendingDeletion(ctx)
}

func (q pgTransitStoreQueries) transitGaugeRotationCandidates(ctx context.Context) ([]transitRotationCandidate, error) {
	c, err := q.queries.TransitGaugeRotationCandidates(ctx)
	if err != nil {
		return nil, err
	}
	var out []transitRotationCandidate
	for _, c := range c {
		item, err := pgTransitGauge(pggen.TransitGaugeRotationCandidatesRow(c))
		if err != nil {
			return out, err
		}
		out = append(out, item)
	}
	return out, nil
}
func (q pgTransitStoreQueries) transitWriteError(err error) error {
	if q.mapConstraints {
		return constraint(err)
	}
	return err
}
func (q pgTransitStoreQueries) transitWriteRows(n int64, err error) (int64, error) {
	return n, q.transitWriteError(err)
}
