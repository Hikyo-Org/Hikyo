package store

import (
	"context"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
)

type adapterStoreQueries interface {
	manifestKeys(context.Context, domain.Scope, []string) ([]adapter.ManifestEntry, error)
	listAdaptersForReencrypt(context.Context, domain.Scope, string, int) ([]ReencryptFieldRow, error)
	listMovesForReencrypt(context.Context, domain.Scope, string, int) ([]ReencryptFieldRow, error)
	reencryptAdapter(context.Context, domain.Scope, string, []byte, []byte) (int64, error)
	reencryptMove(context.Context, domain.Scope, string, []byte, []byte) (int64, error)
	adapterGet(ctx context.Context, chain domain.Scope, adapterID string) (AdapterRecord, error)
	adapterConfiguration(ctx context.Context, chain domain.Scope, adapterID string) (AdapterRecord, []byte, error)
	adapterList(ctx context.Context, chain domain.Scope) ([]AdapterRecord, error)
	adapterTarget(ctx context.Context, chain domain.Scope, targetID string) (AdapterTarget, error)
	adapterListTargets(ctx context.Context, chain domain.Scope, adapterID string) ([]AdapterTarget, error)
	adapterActiveTargetForUpdate(ctx context.Context, chain domain.Scope, targetID string) (AdapterTarget, error)
	adapterTargetKeyIDs(ctx context.Context, chain domain.Scope, targetID string) ([]string, error)
	adapterTargetKeys(ctx context.Context, chain domain.Scope, targetID string) ([]AdapterTargetKey, error)
	adapterProvider(ctx context.Context, chain domain.Scope, adapterID string) (string, error)
	adapterActiveForUpdate(context.Context, domain.Scope, string) (AdapterRecord, error)
	recordCredentialExpiry(context.Context, domain.Scope, string, time.Time) (int64, error)
	beginConfigureEffect(context.Context, domain.Scope, AdapterConfigureFence) error
	finishConfigureEffect(context.Context, domain.Scope, string, string, string, time.Time) (int64, error)
	insertTarget(context.Context, domain.Scope, AdapterTargetMutation, time.Time, []byte) error
	insertTargetKey(context.Context, domain.Scope, AdapterTargetMutation, string) error
	createAdapter(context.Context, domain.Scope, AdapterCreate, time.Time) error
	updateAuthorityExpiry(context.Context, domain.Scope, string, string, time.Time) (int64, error)
	targetActiveJob(context.Context, domain.Scope, string, string) (string, error)
	deleteTargetKeys(context.Context, domain.Scope, string, string) error
	updateTargetConfig(context.Context, domain.Scope, AdapterTargetUpdate, []byte) (int64, error)
	updateActiveAuthority(context.Context, domain.Scope, string, string) (int64, error)
	mapping(context.Context, domain.Scope, string) ([]adapter.ManifestEntry, error)
	planCredential(context.Context, domain.Scope, string) ([]byte, AdapterTransport, error)
	planManifest(context.Context, domain.Scope, string, string) ([]adapter.ManifestEntry, error)
	planLedger(context.Context, domain.Scope, string, string, int64) ([]adapter.LedgerEntry, error)
	targetEnvironments(context.Context, domain.Scope, string) ([]string, error)
	environments(context.Context, domain.Scope, string) ([]string, error)
	conflicts(context.Context, domain.Scope, string) ([]AdapterConflictArtifact, error)
	planTarget(context.Context, domain.Scope, string) (adapterPlanTarget, error)
	insertConflict(context.Context, domain.Scope, string, string, string, string, int64, int64, int64, AdapterConflictEntry, time.Time) (int64, error)
	adoptionTarget(context.Context, domain.Scope, AdapterAdoption) (adapterAdoptionTarget, error)
	adoptionConflictCount(context.Context, domain.Scope, AdapterAdoption, adapterAdoptionTarget, AdapterConflictEntry) (int64, error)
	adoptionInsertLedger(context.Context, domain.Scope, AdapterAdoption, adapterAdoptionTarget, AdapterConflictEntry, string) (int64, error)
	adoptionUpdateHeldLedger(context.Context, domain.Scope, AdapterAdoption, adapterAdoptionTarget, AdapterConflictEntry) (int64, error)
	adoptionReclaimReleasedLedger(context.Context, domain.Scope, AdapterAdoption, adapterAdoptionTarget, AdapterConflictEntry) (int64, error)
	adoptionMarkConflict(context.Context, domain.Scope, AdapterAdoption, adapterAdoptionTarget, AdapterConflictEntry) (int64, error)
	adoptionSupersedeJob(context.Context, domain.Scope, string, string, string, time.Time) (int64, error)
	adoptionInsertJob(context.Context, domain.Scope, string, publishedAdapterTarget, int64, time.Time) (int64, error)
	adoptionUpdateTarget(context.Context, domain.Scope, AdapterAdoption, adapterAdoptionTarget, int64) (int64, error)
	adoptionUpdateAuthority(context.Context, domain.Scope, string, string) (int64, error)
	publishedTargets(context.Context, domain.Scope) ([]publishedAdapterTarget, error)
	manualTarget(context.Context, domain.Scope, string, time.Time) (publishedAdapterTarget, int64, int64, error)
	pauseTarget(context.Context, domain.Scope, string, time.Time) (adapterTeardownTarget, int64, error)
	pauseTargetUpdate(context.Context, domain.Scope, adapterTeardownTarget, int64, time.Time) (int64, error)
	resumeTarget(context.Context, domain.Scope, publishedAdapterTarget) (int64, error)
	resumeRevision(context.Context, domain.Scope, string) (int64, error)
	enqueueTarget(context.Context, domain.Scope, publishedAdapterTarget, string, int64) (int64, error)
	healthCounts(context.Context) (AdapterHealthCounts, error)
	teardownTarget(context.Context, domain.Scope, string, time.Time) (adapterTeardownTarget, error)
	teardownTargets(context.Context, domain.Scope, string, time.Time) ([]adapterTeardownTarget, error)
	teardownAuthority(context.Context, domain.Scope, string) (string, error)
	teardownUnfinishedMoves(context.Context, domain.Scope, string) (int64, error)
	orphans(context.Context, domain.Scope, string, string) ([]string, error)
	releaseLedger(context.Context, domain.Scope, adapterTeardownTarget, time.Time) error
	retainTarget(context.Context, domain.Scope, adapterTeardownTarget, int64, time.Time) (int64, error)
	insertScrubJob(context.Context, domain.Scope, adapterTeardownTarget, AdapterTeardownResult, time.Time) (int64, error)
	scrubTarget(context.Context, domain.Scope, adapterTeardownTarget, AdapterTeardownResult, time.Time) (int64, error)
	markTombstoned(context.Context, domain.Scope, string) (int64, error)
	eraseUnusedCredential(context.Context, domain.Scope, string) error
	replaceCredentialTarget(context.Context, domain.Scope, string, time.Time) (string, int, int64, error)
	replaceCredential(context.Context, domain.Scope, AdapterCredentialMutation) (int64, error)
	replaceCredentialBump(context.Context, domain.Scope, string, time.Time) (int64, error)
	retireCredentialJobs(context.Context, domain.Scope, string, time.Time) (int64, error)
	revokeCredentialTarget(context.Context, domain.Scope, string) (string, int, error)
	revokeCredential(context.Context, domain.Scope, string) (int64, error)
	revokeCredentialBump(context.Context, domain.Scope, string) (int64, error)
}
type sqliteAdapterStoreQueries struct{ queries *sqlitegen.Queries }
type pgAdapterStoreQueries struct{ queries *pggen.Queries }

func (q sqliteAdapterStoreQueries) manifestKeys(ctx context.Context, chain domain.Scope, keyIDs []string) ([]adapter.ManifestEntry, error) {
	rows, err := q.queries.ListAdapterManifestKeys(ctx, sqlitegen.ListAdapterManifestKeysParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), KeyIds: keyIDs})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(row sqlitegen.ListAdapterManifestKeysRow) (adapter.ManifestEntry, error) {
		return adapter.ManifestEntry{KeyID: row.ID, CanonicalName: row.Name, Classification: adapter.Classification(row.Classification)}, nil
	})
}

func (q pgAdapterStoreQueries) manifestKeys(ctx context.Context, chain domain.Scope, keyIDs []string) ([]adapter.ManifestEntry, error) {
	rows, err := q.queries.ListAdapterManifestKeys(ctx, pggen.ListAdapterManifestKeysParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), KeyIds: keyIDs})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(row pggen.ListAdapterManifestKeysRow) (adapter.ManifestEntry, error) {
		return adapter.ManifestEntry{KeyID: row.ID, CanonicalName: row.Name, Classification: adapter.Classification(row.Classification)}, nil
	})
}

func (q sqliteAdapterStoreQueries) listAdaptersForReencrypt(ctx context.Context, chain domain.Scope, cursor string, limit int) ([]ReencryptFieldRow, error) {
	rows, err := q.queries.AdapterListForReencrypt(ctx, sqlitegen.AdapterListForReencryptParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Cursor: cursor, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.AdapterListForReencryptRow) (ReencryptFieldRow, error) {
		return ReencryptFieldRow{ID: c.ID, Owner: c.ID, Ciphertext: c.CredentialCiphertext}, nil
	})
}

func (q sqliteAdapterStoreQueries) listMovesForReencrypt(ctx context.Context, chain domain.Scope, cursor string, limit int) ([]ReencryptFieldRow, error) {
	rows, err := q.queries.AdapterListMovesForReencrypt(ctx, sqlitegen.AdapterListMovesForReencryptParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Cursor: cursor, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.AdapterListMovesForReencryptRow) (ReencryptFieldRow, error) {
		return ReencryptFieldRow{ID: c.ID, Owner: c.AdapterID, Ciphertext: c.PendingCredentialCiphertext}, nil
	})
}

func (q sqliteAdapterStoreQueries) reencryptAdapter(ctx context.Context, chain domain.Scope, id string, newCiphertext, oldCiphertext []byte) (int64, error) {
	n, err := q.queries.AdapterReencrypt(ctx, sqlitegen.AdapterReencryptParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ID: id, NewCiphertext: newCiphertext, OldCiphertext: oldCiphertext})
	return n, constraint(err)
}

func (q sqliteAdapterStoreQueries) reencryptMove(ctx context.Context, chain domain.Scope, id string, newCiphertext, oldCiphertext []byte) (int64, error) {
	n, err := q.queries.AdapterReencryptMove(ctx, sqlitegen.AdapterReencryptMoveParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ID: id, NewCiphertext: newCiphertext, OldCiphertext: oldCiphertext})
	return n, constraint(err)
}

func (q pgAdapterStoreQueries) listAdaptersForReencrypt(ctx context.Context, chain domain.Scope, cursor string, limit int) ([]ReencryptFieldRow, error) {
	rows, err := q.queries.AdapterListForReencrypt(ctx, pggen.AdapterListForReencryptParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Cursor: cursor, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.AdapterListForReencryptRow) (ReencryptFieldRow, error) {
		return ReencryptFieldRow{ID: c.ID, Owner: c.ID, Ciphertext: c.CredentialCiphertext}, nil
	})
}

func (q pgAdapterStoreQueries) listMovesForReencrypt(ctx context.Context, chain domain.Scope, cursor string, limit int) ([]ReencryptFieldRow, error) {
	rows, err := q.queries.AdapterListMovesForReencrypt(ctx, pggen.AdapterListMovesForReencryptParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Cursor: cursor, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.AdapterListMovesForReencryptRow) (ReencryptFieldRow, error) {
		return ReencryptFieldRow{ID: c.ID, Owner: c.AdapterID, Ciphertext: c.PendingCredentialCiphertext}, nil
	})
}

func (q pgAdapterStoreQueries) reencryptAdapter(ctx context.Context, chain domain.Scope, id string, newCiphertext, oldCiphertext []byte) (int64, error) {
	n, err := q.queries.AdapterReencrypt(ctx, pggen.AdapterReencryptParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ID: id, NewCiphertext: newCiphertext, OldCiphertext: oldCiphertext})
	return n, constraint(err)
}

func (q pgAdapterStoreQueries) reencryptMove(ctx context.Context, chain domain.Scope, id string, newCiphertext, oldCiphertext []byte) (int64, error) {
	n, err := q.queries.AdapterReencryptMove(ctx, pggen.AdapterReencryptMoveParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ID: id, NewCiphertext: newCiphertext, OldCiphertext: oldCiphertext})
	return n, constraint(err)
}

func (q sqliteAdapterStoreQueries) adapterGet(ctx context.Context, chain domain.Scope, adapterID string) (AdapterRecord, error) {
	c, err := q.queries.AdapterGet(ctx, sqlitegen.AdapterGetParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if isNoRows(err) {
		return AdapterRecord{}, ErrNotFound
	}
	if err != nil {
		return AdapterRecord{}, err
	}
	return adapterRecordSQLite(sqlitegen.AdapterGetRow(c))
}

func (q sqliteAdapterStoreQueries) adapterConfiguration(ctx context.Context, chain domain.Scope, adapterID string) (AdapterRecord, []byte, error) {
	c, err := q.queries.AdapterConfiguration(ctx, sqlitegen.AdapterConfigurationParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if isNoRows(err) {
		return AdapterRecord{}, nil, ErrNotFound
	}
	if err != nil {
		return AdapterRecord{}, nil, err
	}
	record, err := adapterRecordSQLite(sqlitegen.AdapterGetRow{ID: c.ID, Provider: c.Provider, Origin: c.Origin, CredentialPresent: c.CredentialPresent, CredentialSetAt: c.CredentialSetAt, CredentialExpiresAt: c.CredentialExpiresAt, AuthorityPrincipalID: c.AuthorityPrincipalID, State: c.State, CreatedAt: c.CreatedAt, SpkiPin: c.SpkiPin, CaBundlePem: c.CaBundlePem, AllowPersonalToken: c.AllowPersonalToken})
	return record, c.CredentialCiphertext, err
}

func (q sqliteAdapterStoreQueries) adapterList(ctx context.Context, chain domain.Scope) ([]AdapterRecord, error) {
	rows, err := q.queries.AdapterList(ctx, sqlitegen.AdapterListParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.AdapterListRow) (AdapterRecord, error) {
		return adapterRecordSQLite(sqlitegen.AdapterGetRow(c))
	})
}

func (q sqliteAdapterStoreQueries) adapterTarget(ctx context.Context, chain domain.Scope, targetID string) (AdapterTarget, error) {
	c, err := q.queries.AdapterGetTarget(ctx, sqlitegen.AdapterGetTargetParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if isNoRows(err) {
		return AdapterTarget{}, ErrNotFound
	}
	if err != nil {
		return AdapterTarget{}, err
	}
	return adapterTargetSQLite(sqlitegen.AdapterGetTargetRow(c))
}

func (q sqliteAdapterStoreQueries) adapterListTargets(ctx context.Context, chain domain.Scope, adapterID string) ([]AdapterTarget, error) {
	rows, err := q.queries.AdapterListTargets(ctx, sqlitegen.AdapterListTargetsParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.AdapterListTargetsRow) (AdapterTarget, error) {
		return adapterTargetSQLite(sqlitegen.AdapterGetTargetRow(c))
	})
}

func (q sqliteAdapterStoreQueries) adapterActiveTargetForUpdate(ctx context.Context, chain domain.Scope, targetID string) (AdapterTarget, error) {
	c, err := q.queries.AdapterGetActiveTargetForUpdate(ctx, sqlitegen.AdapterGetActiveTargetForUpdateParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if isNoRows(err) {
		return AdapterTarget{}, ErrNotFound
	}
	if err != nil {
		return AdapterTarget{}, err
	}
	return adapterTargetSQLite(sqlitegen.AdapterGetTargetRow(c))
}

func (q sqliteAdapterStoreQueries) adapterTargetKeyIDs(ctx context.Context, chain domain.Scope, targetID string) ([]string, error) {
	return q.queries.AdapterTargetKeyIDs(ctx, sqlitegen.AdapterTargetKeyIDsParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
}

func (q sqliteAdapterStoreQueries) adapterTargetKeys(ctx context.Context, chain domain.Scope, targetID string) ([]AdapterTargetKey, error) {
	rows, err := q.queries.AdapterTargetKeys(ctx, sqlitegen.AdapterTargetKeysParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	out := []AdapterTargetKey{}
	for _, c := range rows {
		out = append(out, AdapterTargetKey{ID: c.ID, Name: c.Name, Classification: c.Classification})
	}
	return out, nil
}

func (q sqliteAdapterStoreQueries) adapterProvider(ctx context.Context, chain domain.Scope, adapterID string) (string, error) {
	rows, err := q.queries.AdapterProvider(ctx, sqlitegen.AdapterProviderParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", ErrNotFound
	}
	if len(rows) != 1 {
		return "", fmt.Errorf("store: adapter provider lookup was not unique")
	}
	return rows[0], nil
}

func (q pgAdapterStoreQueries) adapterGet(ctx context.Context, chain domain.Scope, adapterID string) (AdapterRecord, error) {
	c, err := q.queries.AdapterGet(ctx, pggen.AdapterGetParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if isNoRows(err) {
		return AdapterRecord{}, ErrNotFound
	}
	if err != nil {
		return AdapterRecord{}, err
	}
	return adapterRecordPG(pggen.AdapterGetRow(c))
}

func (q pgAdapterStoreQueries) adapterConfiguration(ctx context.Context, chain domain.Scope, adapterID string) (AdapterRecord, []byte, error) {
	c, err := q.queries.AdapterConfiguration(ctx, pggen.AdapterConfigurationParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if isNoRows(err) {
		return AdapterRecord{}, nil, ErrNotFound
	}
	if err != nil {
		return AdapterRecord{}, nil, err
	}
	record, err := adapterRecordPG(pggen.AdapterGetRow{ID: c.ID, Provider: c.Provider, Origin: c.Origin, CredentialPresent: c.CredentialPresent, CredentialSetAt: c.CredentialSetAt, CredentialExpiresAt: c.CredentialExpiresAt, AuthorityPrincipalID: c.AuthorityPrincipalID, State: c.State, CreatedAt: c.CreatedAt, SpkiPin: c.SpkiPin, CaBundlePem: c.CaBundlePem, AllowPersonalToken: c.AllowPersonalToken})
	return record, c.CredentialCiphertext, err
}

func (q pgAdapterStoreQueries) adapterList(ctx context.Context, chain domain.Scope) ([]AdapterRecord, error) {
	rows, err := q.queries.AdapterList(ctx, pggen.AdapterListParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.AdapterListRow) (AdapterRecord, error) {
		return adapterRecordPG(pggen.AdapterGetRow(c))
	})
}

func (q pgAdapterStoreQueries) adapterTarget(ctx context.Context, chain domain.Scope, targetID string) (AdapterTarget, error) {
	c, err := q.queries.AdapterGetTarget(ctx, pggen.AdapterGetTargetParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if isNoRows(err) {
		return AdapterTarget{}, ErrNotFound
	}
	if err != nil {
		return AdapterTarget{}, err
	}
	return adapterTargetPG(pggen.AdapterGetTargetRow(c))
}

func (q pgAdapterStoreQueries) adapterListTargets(ctx context.Context, chain domain.Scope, adapterID string) ([]AdapterTarget, error) {
	rows, err := q.queries.AdapterListTargets(ctx, pggen.AdapterListTargetsParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.AdapterListTargetsRow) (AdapterTarget, error) {
		return adapterTargetPG(pggen.AdapterGetTargetRow(c))
	})
}

func (q pgAdapterStoreQueries) adapterActiveTargetForUpdate(ctx context.Context, chain domain.Scope, targetID string) (AdapterTarget, error) {
	c, err := q.queries.AdapterGetActiveTargetForUpdate(ctx, pggen.AdapterGetActiveTargetForUpdateParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if isNoRows(err) {
		return AdapterTarget{}, ErrNotFound
	}
	if err != nil {
		return AdapterTarget{}, err
	}
	return adapterTargetPG(pggen.AdapterGetTargetRow(c))
}

func (q pgAdapterStoreQueries) adapterTargetKeyIDs(ctx context.Context, chain domain.Scope, targetID string) ([]string, error) {
	return q.queries.AdapterTargetKeyIDs(ctx, pggen.AdapterTargetKeyIDsParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
}

func (q pgAdapterStoreQueries) adapterTargetKeys(ctx context.Context, chain domain.Scope, targetID string) ([]AdapterTargetKey, error) {
	rows, err := q.queries.AdapterTargetKeys(ctx, pggen.AdapterTargetKeysParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	out := []AdapterTargetKey{}
	for _, c := range rows {
		out = append(out, AdapterTargetKey{ID: c.ID, Name: c.Name, Classification: c.Classification})
	}
	return out, nil
}

func (q pgAdapterStoreQueries) adapterProvider(ctx context.Context, chain domain.Scope, adapterID string) (string, error) {
	rows, err := q.queries.AdapterProvider(ctx, pggen.AdapterProviderParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", ErrNotFound
	}
	if len(rows) != 1 {
		return "", fmt.Errorf("store: adapter provider lookup was not unique")
	}
	return rows[0], nil
}

func (q sqliteAdapterStoreQueries) adapterActiveForUpdate(ctx context.Context, chain domain.Scope, adapterID string) (AdapterRecord, error) {
	c, err := q.queries.AdapterGetActiveForUpdate(ctx, sqlitegen.AdapterGetActiveForUpdateParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if isNoRows(err) {
		return AdapterRecord{}, ErrNotFound
	}
	if err != nil {
		return AdapterRecord{}, err
	}
	return adapterRecordSQLite(sqlitegen.AdapterGetRow(c))
}
func (q sqliteAdapterStoreQueries) recordCredentialExpiry(ctx context.Context, chain domain.Scope, adapterID string, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterRecordCredentialExpiry(ctx, sqlitegen.AdapterRecordCredentialExpiryParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ExpiresAt: runtimeSQLiteStamp(at)}))
}
func (q sqliteAdapterStoreQueries) beginConfigureEffect(ctx context.Context, chain domain.Scope, fence AdapterConfigureFence) error {
	return constraint(q.queries.AdapterBeginConfigureEffect(ctx, sqlitegen.AdapterBeginConfigureEffectParams{TargetID: fence.TargetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: fence.EnvironmentID, DestinationKind: fence.DestinationKind, DestinationOwner: fence.DestinationOwner, DestinationName: fence.DestinationName, DestinationEnvironment: fence.DestinationEnvironment, Generation: fence.Generation, EffectID: fence.EffectID, LeaseExpiresAt: fixedStamp(fence.LeaseExpiresAt), CreatedAt: fixedStamp(fence.At)}))
}
func (q sqliteAdapterStoreQueries) finishConfigureEffect(ctx context.Context, chain domain.Scope, targetID, effectID, outcome string, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterFinishConfigureEffect(ctx, sqlitegen.AdapterFinishConfigureEffectParams{TargetID: targetID, EffectID: effectID, Outcome: outcome, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), CompletedAt: runtimeSQLiteStamp(at)}))
}

func (q pgAdapterStoreQueries) adapterActiveForUpdate(ctx context.Context, chain domain.Scope, adapterID string) (AdapterRecord, error) {
	c, err := q.queries.AdapterGetActiveForUpdate(ctx, pggen.AdapterGetActiveForUpdateParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if isNoRows(err) {
		return AdapterRecord{}, ErrNotFound
	}
	if err != nil {
		return AdapterRecord{}, err
	}
	return adapterRecordPG(pggen.AdapterGetRow(c))
}
func (q pgAdapterStoreQueries) recordCredentialExpiry(ctx context.Context, chain domain.Scope, adapterID string, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterRecordCredentialExpiry(ctx, pggen.AdapterRecordCredentialExpiryParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ExpiresAt: pgRequiredTime(at)}))
}
func (q pgAdapterStoreQueries) beginConfigureEffect(ctx context.Context, chain domain.Scope, fence AdapterConfigureFence) error {
	return constraint(q.queries.AdapterBeginConfigureEffect(ctx, pggen.AdapterBeginConfigureEffectParams{TargetID: fence.TargetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: fence.EnvironmentID, DestinationKind: fence.DestinationKind, DestinationOwner: fence.DestinationOwner, DestinationName: fence.DestinationName, DestinationEnvironment: fence.DestinationEnvironment, Generation: fence.Generation, EffectID: fence.EffectID, LeaseExpiresAt: pgRequiredTime(fence.LeaseExpiresAt), CreatedAt: pgRequiredTime(fence.At)}))
}
func (q pgAdapterStoreQueries) finishConfigureEffect(ctx context.Context, chain domain.Scope, targetID, effectID, outcome string, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterFinishConfigureEffect(ctx, pggen.AdapterFinishConfigureEffectParams{TargetID: targetID, EffectID: effectID, Outcome: outcome, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), CompletedAt: pgRequiredTime(at)}))
}

func (q sqliteAdapterStoreQueries) insertTarget(ctx context.Context, chain domain.Scope, m AdapterTargetMutation, at time.Time, selected []byte) error {
	return constraint(q.queries.AdapterInsertTarget(ctx, sqlitegen.AdapterInsertTargetParams{TargetID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: m.EnvironmentID, AdapterID: m.AdapterID, DestinationKind: m.DestinationKind, DestinationOwner: m.DestinationOwner, DestinationName: m.DestinationName, DestinationEnvironment: m.DestinationEnvironment, DestinationID: m.DestinationID, RepositoryID: m.RepositoryID, Visibility: m.Visibility, SelectedRepositoryIds: string(selected), NamePrefix: m.NamePrefix, CreatedAt: fixedStamp(at), DestinationScope: m.DestinationScope, VariableProtected: boolToInt(m.VariableProtected), VariableHidden: boolToInt(m.VariableHidden), VariableExpand: boolToInt(m.VariableExpand)}))
}
func (q sqliteAdapterStoreQueries) insertTargetKey(ctx context.Context, chain domain.Scope, m AdapterTargetMutation, keyID string) error {
	return constraint(q.queries.AdapterInsertTargetKey(ctx, sqlitegen.AdapterInsertTargetKeyParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: m.EnvironmentID, TargetID: m.ID, AdapterID: m.AdapterID, KeyID: keyID}))
}
func (q sqliteAdapterStoreQueries) createAdapter(ctx context.Context, chain domain.Scope, m AdapterCreate, at time.Time) error {
	return constraint(q.queries.AdapterCreate(ctx, sqlitegen.AdapterCreateParams{AdapterID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Provider: m.Provider, Origin: m.Origin, CredentialCiphertext: m.CredentialCiphertext, CredentialSetAt: runtimeSQLiteStamp(at), CredentialExpiresAt: nullTimeString(m.CredentialExpiresAt), AuthorityPrincipalID: m.AuthorityPrincipalID, CreatedAt: fixedStamp(at), SpkiPin: m.SPKIPin, CaBundlePem: m.CABundlePEM, AllowPersonalToken: boolToInt(m.AllowPersonalToken)}))
}
func (q sqliteAdapterStoreQueries) updateAuthorityExpiry(ctx context.Context, chain domain.Scope, adapterID, authority string, expires time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterUpdateAuthorityExpiry(ctx, sqlitegen.AdapterUpdateAuthorityExpiryParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), AuthorityPrincipalID: authority, CredentialExpiresAt: nullTimeString(expires)}))
}
func (q sqliteAdapterStoreQueries) targetActiveJob(ctx context.Context, chain domain.Scope, targetID, envID string) (string, error) {
	return q.queries.AdapterTargetActiveJob(ctx, sqlitegen.AdapterTargetActiveJobParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: envID})
}
func (q sqliteAdapterStoreQueries) deleteTargetKeys(ctx context.Context, chain domain.Scope, targetID, envID string) error {
	return constraint(q.queries.AdapterDeleteTargetKeys(ctx, sqlitegen.AdapterDeleteTargetKeysParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: envID}))
}
func (q sqliteAdapterStoreQueries) updateTargetConfig(ctx context.Context, chain domain.Scope, m AdapterTargetUpdate, selected []byte) (int64, error) {
	return adapterAffected(q.queries.AdapterUpdateTargetConfig(ctx, sqlitegen.AdapterUpdateTargetConfigParams{TargetID: m.Target.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Visibility: m.Target.Visibility, SelectedRepositoryIds: string(selected), NamePrefix: m.Target.NamePrefix, VariableProtected: boolToInt(m.Target.VariableProtected), VariableHidden: boolToInt(m.Target.VariableHidden), VariableExpand: boolToInt(m.Target.VariableExpand), ExpectedGeneration: m.ExpectedGeneration}))
}
func (q sqliteAdapterStoreQueries) updateActiveAuthority(ctx context.Context, chain domain.Scope, adapterID, authority string) (int64, error) {
	return adapterAffected(q.queries.AdapterUpdateActiveAuthority(ctx, sqlitegen.AdapterUpdateActiveAuthorityParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), AuthorityPrincipalID: authority}))
}

func (q pgAdapterStoreQueries) insertTarget(ctx context.Context, chain domain.Scope, m AdapterTargetMutation, at time.Time, selected []byte) error {
	return constraint(q.queries.AdapterInsertTarget(ctx, pggen.AdapterInsertTargetParams{TargetID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: m.EnvironmentID, AdapterID: m.AdapterID, DestinationKind: m.DestinationKind, DestinationOwner: m.DestinationOwner, DestinationName: m.DestinationName, DestinationEnvironment: m.DestinationEnvironment, DestinationID: m.DestinationID, RepositoryID: m.RepositoryID, Visibility: m.Visibility, SelectedRepositoryIds: selected, NamePrefix: m.NamePrefix, CreatedAt: pgRequiredTime(at), DestinationScope: m.DestinationScope, VariableProtected: m.VariableProtected, VariableHidden: m.VariableHidden, VariableExpand: m.VariableExpand}))
}
func (q pgAdapterStoreQueries) insertTargetKey(ctx context.Context, chain domain.Scope, m AdapterTargetMutation, keyID string) error {
	return constraint(q.queries.AdapterInsertTargetKey(ctx, pggen.AdapterInsertTargetKeyParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: m.EnvironmentID, TargetID: m.ID, AdapterID: m.AdapterID, KeyID: keyID}))
}
func (q pgAdapterStoreQueries) createAdapter(ctx context.Context, chain domain.Scope, m AdapterCreate, at time.Time) error {
	return constraint(q.queries.AdapterCreate(ctx, pggen.AdapterCreateParams{AdapterID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Provider: m.Provider, Origin: m.Origin, CredentialCiphertext: m.CredentialCiphertext, CredentialSetAt: pgRequiredTime(at), CredentialExpiresAt: pgNullTimestamp(m.CredentialExpiresAt), AuthorityPrincipalID: m.AuthorityPrincipalID, CreatedAt: pgRequiredTime(at), SpkiPin: m.SPKIPin, CaBundlePem: m.CABundlePEM, AllowPersonalToken: m.AllowPersonalToken}))
}
func (q pgAdapterStoreQueries) updateAuthorityExpiry(ctx context.Context, chain domain.Scope, adapterID, authority string, expires time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterUpdateAuthorityExpiry(ctx, pggen.AdapterUpdateAuthorityExpiryParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), AuthorityPrincipalID: authority, CredentialExpiresAt: pgNullTimestamp(expires)}))
}
func (q pgAdapterStoreQueries) targetActiveJob(ctx context.Context, chain domain.Scope, targetID, envID string) (string, error) {
	return q.queries.AdapterTargetActiveJob(ctx, pggen.AdapterTargetActiveJobParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: envID})
}
func (q pgAdapterStoreQueries) deleteTargetKeys(ctx context.Context, chain domain.Scope, targetID, envID string) error {
	return constraint(q.queries.AdapterDeleteTargetKeys(ctx, pggen.AdapterDeleteTargetKeysParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: envID}))
}
func (q pgAdapterStoreQueries) updateTargetConfig(ctx context.Context, chain domain.Scope, m AdapterTargetUpdate, selected []byte) (int64, error) {
	return adapterAffected(q.queries.AdapterUpdateTargetConfig(ctx, pggen.AdapterUpdateTargetConfigParams{TargetID: m.Target.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Visibility: m.Target.Visibility, SelectedRepositoryIds: selected, NamePrefix: m.Target.NamePrefix, VariableProtected: m.Target.VariableProtected, VariableHidden: m.Target.VariableHidden, VariableExpand: m.Target.VariableExpand, ExpectedGeneration: m.ExpectedGeneration}))
}
func (q pgAdapterStoreQueries) updateActiveAuthority(ctx context.Context, chain domain.Scope, adapterID, authority string) (int64, error) {
	return adapterAffected(q.queries.AdapterUpdateActiveAuthority(ctx, pggen.AdapterUpdateActiveAuthorityParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), AuthorityPrincipalID: authority}))
}

type adapterPlanTarget struct {
	environmentID                           string
	destinationID, repositoryID, generation int64
}

func (q sqliteAdapterStoreQueries) mapping(ctx context.Context, chain domain.Scope, targetID string) ([]adapter.ManifestEntry, error) {
	rows, err := q.queries.AdapterMapping(ctx, sqlitegen.AdapterMappingParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.AdapterMappingRow) (adapter.ManifestEntry, error) {
		return adapter.ManifestEntry{KeyID: c.KeyID, CanonicalName: c.KeyName, Classification: adapter.Classification(c.Classification)}, nil
	})
}

func (q sqliteAdapterStoreQueries) planManifest(ctx context.Context, chain domain.Scope, targetID string, envID string) ([]adapter.ManifestEntry, error) {
	rows, err := q.queries.AdapterPlanManifest(ctx, sqlitegen.AdapterPlanManifestParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: envID})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.AdapterPlanManifestRow) (adapter.ManifestEntry, error) {
		return adapter.ManifestEntry{KeyID: c.KeyID, CanonicalName: c.KeyName, Classification: adapter.Classification(c.Classification)}, nil
	})
}

func (q sqliteAdapterStoreQueries) planCredential(ctx context.Context, chain domain.Scope, adapterID string) ([]byte, AdapterTransport, error) {
	c, err := q.queries.AdapterPlanCredential(ctx, sqlitegen.AdapterPlanCredentialParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	return c.CredentialCiphertext, AdapterTransport{SPKIPin: c.SpkiPin, CABundlePEM: c.CaBundlePem, AllowPersonalToken: c.AllowPersonalToken == 1}, err
}
func (q sqliteAdapterStoreQueries) planLedger(ctx context.Context, chain domain.Scope, targetID, envID string, generation int64) ([]adapter.LedgerEntry, error) {
	rows, err := q.queries.AdapterPlanLedger(ctx, sqlitegen.AdapterPlanLedgerParams{TargetID: targetID, EnvironmentID: envID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Generation: generation})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.AdapterPlanLedgerRow) (adapter.LedgerEntry, error) {
		return adapter.LedgerEntry{Surface: adapter.Surface(c.Surface), EffectiveName: c.EffectiveName, State: adapter.LedgerState(c.State), Missing: c.Missing != 0, AdoptionPending: c.AdoptionPending != 0, AdoptionVersion: adapterVersionWitness(c.AdoptionVersion.Int64, c.AdoptionVersion.Valid)}, nil
	})
}
func (q sqliteAdapterStoreQueries) targetEnvironments(ctx context.Context, chain domain.Scope, targetID string) ([]string, error) {
	return q.queries.AdapterTargetEnvironments(ctx, sqlitegen.AdapterTargetEnvironmentsParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
}
func (q sqliteAdapterStoreQueries) environments(ctx context.Context, chain domain.Scope, adapterID string) ([]string, error) {
	return q.queries.AdapterEnvironments(ctx, sqlitegen.AdapterEnvironmentsParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
}
func (q sqliteAdapterStoreQueries) conflicts(ctx context.Context, chain domain.Scope, targetID string) ([]AdapterConflictArtifact, error) {
	rows, err := q.queries.AdapterConflicts(ctx, sqlitegen.AdapterConflictsParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]int)
	var out []AdapterConflictArtifact
	for _, c := range rows {
		created, err := parseTime("adapter conflict", c.ArtifactID, c.CreatedAt)
		if err != nil {
			return nil, err
		}
		entry := AdapterConflictEntry{Surface: c.Surface, EffectiveName: c.EffectiveName}
		if index, ok := byID[c.ArtifactID]; ok {
			out[index].Entries = append(out[index].Entries, entry)
		} else {
			byID[c.ArtifactID] = len(out)
			out = append(out, AdapterConflictArtifact{ID: c.ArtifactID, TargetID: c.TargetID, JobID: c.JobID.String, DestinationID: c.DestinationID, RepositoryID: c.RepositoryID, TargetGeneration: c.TargetGeneration, CreatedAt: created, Entries: []AdapterConflictEntry{entry}})
		}
	}
	return out, nil
}
func (q sqliteAdapterStoreQueries) planTarget(ctx context.Context, chain domain.Scope, targetID string) (adapterPlanTarget, error) {
	c, err := q.queries.AdapterPlanTarget(ctx, sqlitegen.AdapterPlanTargetParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	return adapterPlanTarget{environmentID: c.EnvironmentID, destinationID: c.DestinationID, repositoryID: c.RepositoryID, generation: c.Generation}, err
}
func (q sqliteAdapterStoreQueries) insertConflict(ctx context.Context, chain domain.Scope, id, artifactID, envID, targetID string, destinationID, repositoryID, generation int64, entry AdapterConflictEntry, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterInsertConflict(ctx, sqlitegen.AdapterInsertConflictParams{ID: id, ArtifactID: artifactID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: envID, TargetID: targetID, DestinationID: destinationID, RepositoryID: repositoryID, TargetGeneration: generation, Surface: entry.Surface, EffectiveName: entry.EffectiveName, ObservedProviderVersion: adapterOptionalVersion(entry.ObservedProviderVersion), CreatedAt: fixedStamp(at)}))
}

func (q pgAdapterStoreQueries) mapping(ctx context.Context, chain domain.Scope, targetID string) ([]adapter.ManifestEntry, error) {
	rows, err := q.queries.AdapterMapping(ctx, pggen.AdapterMappingParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.AdapterMappingRow) (adapter.ManifestEntry, error) {
		return adapter.ManifestEntry{KeyID: c.KeyID, CanonicalName: c.KeyName, Classification: adapter.Classification(c.Classification)}, nil
	})
}

func (q pgAdapterStoreQueries) planManifest(ctx context.Context, chain domain.Scope, targetID string, envID string) ([]adapter.ManifestEntry, error) {
	rows, err := q.queries.AdapterPlanManifest(ctx, pggen.AdapterPlanManifestParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: envID})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.AdapterPlanManifestRow) (adapter.ManifestEntry, error) {
		return adapter.ManifestEntry{KeyID: c.KeyID, CanonicalName: c.KeyName, Classification: adapter.Classification(c.Classification)}, nil
	})
}

func (q pgAdapterStoreQueries) planCredential(ctx context.Context, chain domain.Scope, adapterID string) ([]byte, AdapterTransport, error) {
	c, err := q.queries.AdapterPlanCredential(ctx, pggen.AdapterPlanCredentialParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	return c.CredentialCiphertext, AdapterTransport{SPKIPin: c.SpkiPin, CABundlePEM: c.CaBundlePem, AllowPersonalToken: c.AllowPersonalToken == 1}, err
}
func (q pgAdapterStoreQueries) planLedger(ctx context.Context, chain domain.Scope, targetID, envID string, generation int64) ([]adapter.LedgerEntry, error) {
	rows, err := q.queries.AdapterPlanLedger(ctx, pggen.AdapterPlanLedgerParams{TargetID: targetID, EnvironmentID: envID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Generation: generation})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.AdapterPlanLedgerRow) (adapter.LedgerEntry, error) {
		return adapter.LedgerEntry{Surface: adapter.Surface(c.Surface), EffectiveName: c.EffectiveName, State: adapter.LedgerState(c.State), Missing: c.Missing, AdoptionPending: c.AdoptionPending != 0, AdoptionVersion: adapterVersionWitness(c.AdoptionVersion.Int64, c.AdoptionVersion.Valid)}, nil
	})
}
func (q pgAdapterStoreQueries) targetEnvironments(ctx context.Context, chain domain.Scope, targetID string) ([]string, error) {
	return q.queries.AdapterTargetEnvironments(ctx, pggen.AdapterTargetEnvironmentsParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
}
func (q pgAdapterStoreQueries) environments(ctx context.Context, chain domain.Scope, adapterID string) ([]string, error) {
	return q.queries.AdapterEnvironments(ctx, pggen.AdapterEnvironmentsParams{AdapterID: adapterID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
}
func (q pgAdapterStoreQueries) conflicts(ctx context.Context, chain domain.Scope, targetID string) ([]AdapterConflictArtifact, error) {
	rows, err := q.queries.AdapterConflicts(ctx, pggen.AdapterConflictsParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]int)
	var out []AdapterConflictArtifact
	for _, c := range rows {
		created, err := c.CreatedAt.Time.UTC(), error(nil)
		if err != nil {
			return nil, err
		}
		entry := AdapterConflictEntry{Surface: c.Surface, EffectiveName: c.EffectiveName}
		if index, ok := byID[c.ArtifactID]; ok {
			out[index].Entries = append(out[index].Entries, entry)
		} else {
			byID[c.ArtifactID] = len(out)
			out = append(out, AdapterConflictArtifact{ID: c.ArtifactID, TargetID: c.TargetID, JobID: c.JobID.String, DestinationID: c.DestinationID, RepositoryID: c.RepositoryID, TargetGeneration: c.TargetGeneration, CreatedAt: created, Entries: []AdapterConflictEntry{entry}})
		}
	}
	return out, nil
}
func (q pgAdapterStoreQueries) planTarget(ctx context.Context, chain domain.Scope, targetID string) (adapterPlanTarget, error) {
	c, err := q.queries.AdapterPlanTarget(ctx, pggen.AdapterPlanTargetParams{TargetID: targetID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	return adapterPlanTarget{environmentID: c.EnvironmentID, destinationID: c.DestinationID, repositoryID: c.RepositoryID, generation: c.Generation}, err
}
func (q pgAdapterStoreQueries) insertConflict(ctx context.Context, chain domain.Scope, id, artifactID, envID, targetID string, destinationID, repositoryID, generation int64, entry AdapterConflictEntry, at time.Time) (int64, error) {
	return adapterAffected(q.queries.AdapterInsertConflict(ctx, pggen.AdapterInsertConflictParams{ID: id, ArtifactID: artifactID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), EnvironmentID: envID, TargetID: targetID, DestinationID: destinationID, RepositoryID: repositoryID, TargetGeneration: generation, Surface: entry.Surface, EffectiveName: entry.EffectiveName, ObservedProviderVersion: pgtype.Int8{Int64: adapterOptionalVersion(entry.ObservedProviderVersion).Int64, Valid: adapterOptionalVersion(entry.ObservedProviderVersion).Valid}, CreatedAt: pgRequiredTime(at)}))
}

func adapterAffected(rows int64, err error) (int64, error) { return rows, constraint(err) }
