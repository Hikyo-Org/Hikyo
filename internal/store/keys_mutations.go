package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// keyMutationAdapter is the internal seam between key-rotation policy and the
// two SQL dialects. The adapter owns only generated-query shape and driver
// error translation; every security invariant and operation ordering rule is
// owned once by the functions below.
type keyMutationAdapter interface {
	txToken() *authz.TxToken
	acquireHierarchy(context.Context) error
	acquireSelfConfigBinding(context.Context) error
	rootFinalizationBlocked(context.Context) (bool, error)
	acquireScope(context.Context, string) error
	activeMasters(context.Context) ([]activeMasterRow, error)
	insertMasterRow(context.Context, crypto.WrappedKey) error
	insertTier3Row(context.Context, crypto.WrappedKey) error
	retireTier3AtVersion(context.Context, crypto.Purpose, string, string, int64) (int64, error)
	demoteTier3AtVersion(context.Context, crypto.WrappedKey, int64) (int64, error)
	retireMasterAtVersion(context.Context, int64) (int64, error)
	retireMasterWrapper(context.Context, activeMasterRow) (int64, error)
	updateTier3Wrapping(context.Context, crypto.WrappedKey) (int64, error)
	countOpenableTier3NotAtMaster(context.Context, int64) (int64, error)
	activeTier3State(context.Context, crypto.Purpose, string, string, int64) (string, error)
	retireRetiringTier3ForScope(context.Context, crypto.Purpose, string, string) (int64, error)
	insertScopeGenerationRow(context.Context, string) error
}

type activeMasterRow struct {
	version      int64
	rootKeyEpoch int64
}

func verifyKeyMutation(proof authz.Proof, op authz.StoreOp, adapter keyMutationAdapter) error {
	_, err := authz.Verify(proof, op, adapter.txToken())
	return err
}

func acquireHierarchyGeneration(ctx context.Context, proof authz.Proof, adapter keyMutationAdapter) error {
	if err := verifyKeyMutation(proof, authz.StoreKeysAcquireHierarchyGeneration, adapter); err != nil {
		return err
	}
	return adapter.acquireHierarchy(ctx)
}

func insertMaster(ctx context.Context, proof authz.Proof, key crypto.WrappedKey, adapter keyMutationAdapter) error {
	if err := verifyKeyMutation(proof, authz.StoreKeysInsertMaster, adapter); err != nil {
		return err
	}
	return adapter.insertMasterRow(ctx, key)
}

func insertTier3(ctx context.Context, proof authz.Proof, key crypto.WrappedKey, adapter keyMutationAdapter) error {
	if err := verifyKeyMutation(proof, authz.StoreKeysInsertTier3, adapter); err != nil {
		return err
	}
	return adapter.insertTier3Row(ctx, key)
}

func rotateRootScopedTier3(ctx context.Context, proof authz.Proof, key crypto.WrappedKey, adapter keyMutationAdapter, op authz.StoreOp, purpose crypto.Purpose) error {
	if err := verifyKeyMutation(proof, op, adapter); err != nil {
		return err
	}
	// Hierarchy fence first: master rotation cannot slip between retire and
	// insert. CAS on predecessor keeps process memory and datastore aligned.
	if err := adapter.acquireHierarchy(ctx); err != nil {
		return err
	}
	retired, err := adapter.retireTier3AtVersion(ctx, purpose, "", "", int64(key.Version)-1)
	if err != nil {
		return err
	}
	if retired != 1 {
		return ErrRotationSuperseded
	}
	return adapter.insertTier3Row(ctx, key)
}

func rotateDEK(ctx context.Context, proof authz.Proof, key crypto.WrappedKey, adapter keyMutationAdapter) error {
	if err := verifyKeyMutation(proof, authz.StoreKeysRotateDEK, adapter); err != nil {
		return err
	}
	if err := adapter.acquireHierarchy(ctx); err != nil {
		return err
	}
	if err := adapter.acquireScope(ctx, scopeGenerationKey(key.Purpose, key.OrgID, key.ProjectID)); err != nil {
		return err
	}
	if err := assertActiveMaster(ctx, key.MasterKeyVersion, adapter); err != nil {
		return err
	}
	demoted, err := adapter.demoteTier3AtVersion(ctx, key, int64(key.Version)-1)
	if err != nil {
		return err
	}
	if demoted != 1 {
		return ErrRotationSuperseded
	}
	return adapter.insertTier3Row(ctx, key)
}

// assertActiveMaster gives the hierarchy fence teeth for tier-3 rotation. A
// successor wrapped under a master that retired after minting is refused.
func assertActiveMaster(ctx context.Context, masterVersion uint32, adapter keyMutationAdapter) error {
	masters, err := adapter.activeMasters(ctx)
	if err != nil {
		return err
	}
	if len(masters) == 0 {
		return errors.New("store: no active master key — hierarchy missing")
	}
	for _, master := range masters {
		if master.version != int64(masterVersion) {
			return crypto.ErrStaleMaster
		}
	}
	return nil
}

func assertActiveDEKVersion(ctx context.Context, proof authz.Proof, purpose crypto.Purpose, orgID, projectID string, version uint32, adapter keyMutationAdapter) error {
	if err := verifyKeyMutation(proof, authz.StoreKeysAssertActiveDEKVersion, adapter); err != nil {
		return err
	}
	state, err := adapter.activeTier3State(ctx, purpose, orgID, projectID, int64(version))
	if err != nil {
		return err
	}
	if state != "active" {
		return ErrStaleDEK
	}
	return nil
}

func rotateMasterKey(ctx context.Context, proof authz.Proof, newMaster crypto.WrappedKey, rewrapped []crypto.WrappedKey, adapter keyMutationAdapter) error {
	if err := verifyKeyMutation(proof, authz.StoreKeysRotateMasterKey, adapter); err != nil {
		return err
	}
	if err := adapter.acquireHierarchy(ctx); err != nil {
		return err
	}
	masters, err := adapter.activeMasters(ctx)
	if err != nil {
		return err
	}
	// Exactly one active wrapper: zero means missing hierarchy; two means root
	// rotation is dual-wrapped. Master and root rotation are mutually exclusive.
	if len(masters) != 1 {
		return crypto.ErrMasterRotationBlocked
	}
	if int64(newMaster.Version) != masters[0].version+1 {
		return ErrRotationSuperseded
	}
	retired, err := adapter.retireMasterAtVersion(ctx, masters[0].version)
	if err != nil {
		return err
	}
	if retired != 1 {
		return ErrRotationSuperseded
	}
	if err := adapter.insertMasterRow(ctx, newMaster); err != nil {
		return err
	}
	for _, row := range rewrapped {
		updated, err := adapter.updateTier3Wrapping(ctx, row)
		if err != nil {
			return err
		}
		if updated != 1 {
			return fmt.Errorf("store: tier-3 key %s v%d vanished during master rotation", row.ID, row.Version)
		}
	}
	// Checked inside the fence: no openable key may remain under old master.
	stranded, err := adapter.countOpenableTier3NotAtMaster(ctx, int64(newMaster.Version))
	if err != nil {
		return err
	}
	if stranded != 0 {
		return ErrRotationSuperseded
	}
	return nil
}

func retireRetiringTier3(ctx context.Context, proof authz.Proof, purpose crypto.Purpose, orgID, projectID string, adapter keyMutationAdapter) (int64, error) {
	if err := verifyKeyMutation(proof, authz.StoreKeysRetireRetiringTier3, adapter); err != nil {
		return 0, err
	}
	if err := adapter.acquireScope(ctx, scopeGenerationKey(purpose, orgID, projectID)); err != nil {
		return 0, err
	}
	return adapter.retireRetiringTier3ForScope(ctx, purpose, orgID, projectID)
}

func rootKeyRotatePrepare(ctx context.Context, proof authz.Proof, newWrapper crypto.WrappedKey, adapter keyMutationAdapter) error {
	if err := verifyKeyMutation(proof, authz.StoreKeysRootRotatePrepare, adapter); err != nil {
		return err
	}
	if err := adapter.acquireSelfConfigBinding(ctx); err != nil {
		return err
	}
	if err := adapter.acquireHierarchy(ctx); err != nil {
		return err
	}
	masters, err := adapter.activeMasters(ctx)
	if err != nil {
		return err
	}
	if len(masters) != 1 || masters[0].version != int64(newWrapper.Version) || masters[0].rootKeyEpoch+1 != int64(newWrapper.RootKeyEpoch) {
		return crypto.ErrRootRotationBlocked
	}
	return adapter.insertMasterRow(ctx, newWrapper)
}

func rootKeyRotateFinalize(ctx context.Context, proof authz.Proof, adapter keyMutationAdapter) (uint32, error) {
	if err := verifyKeyMutation(proof, authz.StoreKeysRootRotateFinalize, adapter); err != nil {
		return 0, err
	}
	// Binding precedes hierarchy throughout root preparation and Apply.
	// Hold it until commit so target changes cannot race wrapper retirement.
	if err := adapter.acquireSelfConfigBinding(ctx); err != nil {
		return 0, err
	}
	blocked, err := adapter.rootFinalizationBlocked(ctx)
	if err != nil {
		return 0, err
	}
	if blocked {
		return 0, ErrRootFinalizationPendingDeployment
	}
	if err := adapter.acquireHierarchy(ctx); err != nil {
		return 0, err
	}
	masters, err := adapter.activeMasters(ctx)
	if err != nil {
		return 0, err
	}
	if len(masters) != 2 {
		return 0, crypto.ErrNotDualWrapped
	}
	// activeMasters is ordered by root epoch descending: [new, old].
	newEpoch := masters[0].rootKeyEpoch
	retired, err := adapter.retireMasterWrapper(ctx, masters[len(masters)-1])
	if err != nil {
		return 0, err
	}
	if retired != 1 {
		return 0, ErrRotationSuperseded
	}
	return dbVersion("root key epoch", newEpoch)
}

func insertScopeGeneration(ctx context.Context, proof authz.Proof, purpose crypto.Purpose, orgID, projectID string, adapter keyMutationAdapter) error {
	if err := verifyKeyMutation(proof, authz.StoreKeysInsertScopeGeneration, adapter); err != nil {
		return err
	}
	return adapter.insertScopeGenerationRow(ctx, scopeGenerationKey(purpose, orgID, projectID))
}

func (k sqliteKeys) txToken() *authz.TxToken { return k.tok }

func (k sqliteKeys) acquireHierarchy(ctx context.Context) error {
	// SQLite's single write connection plus BEGIN IMMEDIATE serializes writers
	// globally; reading the row keeps the dialect-neutral call shape and proves
	// the hierarchy generation exists.
	_, err := k.q.AcquireHierarchyGeneration(ctx)
	return err
}

func (k sqliteKeys) acquireScope(ctx context.Context, scope string) error {
	_, err := k.q.AcquireScopeGeneration(ctx, scope)
	return err
}

func (k sqliteKeys) activeMasters(ctx context.Context) ([]activeMasterRow, error) {
	rows, err := k.q.GetActiveMasterKeys(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]activeMasterRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, activeMasterRow{version: row.Version, rootKeyEpoch: row.RootKeyEpoch})
	}
	return out, nil
}

func (k sqliteKeys) insertMasterRow(ctx context.Context, key crypto.WrappedKey) error {
	err := k.q.InsertMasterKey(ctx, sqlitegen.InsertMasterKeyParams{
		Version: int64(key.Version), RootKeyEpoch: int64(key.RootKeyEpoch), Blob: key.Blob,
		CreatedAt: CanonTime(key.CreatedAt).Format(timeFormat),
	})
	if sqliteUniqueViolation(err) {
		return crypto.ErrKeyExists
	}
	return err
}

func (k sqliteKeys) insertTier3Row(ctx context.Context, key crypto.WrappedKey) error {
	err := k.q.InsertTier3Key(ctx, sqlitegen.InsertTier3KeyParams{
		ID: key.ID, Purpose: string(key.Purpose), OrgID: key.OrgID, ProjectID: key.ProjectID,
		Version: int64(key.Version), MasterKeyVersion: int64(key.MasterKeyVersion), Blob: key.Blob,
		CreatedAt: CanonTime(key.CreatedAt).Format(timeFormat),
	})
	if sqliteUniqueViolation(err) {
		return crypto.ErrKeyExists
	}
	return err
}

func (k sqliteKeys) retireTier3AtVersion(ctx context.Context, purpose crypto.Purpose, orgID, projectID string, version int64) (int64, error) {
	return k.q.RetireTier3KeyAtVersion(ctx, sqlitegen.RetireTier3KeyAtVersionParams{
		Purpose: string(purpose), OrgID: orgID, ProjectID: projectID, Version: version,
	})
}

func (k sqliteKeys) demoteTier3AtVersion(ctx context.Context, key crypto.WrappedKey, version int64) (int64, error) {
	return k.q.DemoteActiveTier3ToRetiring(ctx, sqlitegen.DemoteActiveTier3ToRetiringParams{
		Purpose: string(key.Purpose), OrgID: key.OrgID, ProjectID: key.ProjectID, Version: version,
	})
}

func (k sqliteKeys) retireMasterAtVersion(ctx context.Context, version int64) (int64, error) {
	return k.q.RetireMasterAtVersion(ctx, version)
}

func (k sqliteKeys) retireMasterWrapper(ctx context.Context, master activeMasterRow) (int64, error) {
	return k.q.RetireMasterWrapperAtEpoch(ctx, sqlitegen.RetireMasterWrapperAtEpochParams{
		Version: master.version, RootKeyEpoch: master.rootKeyEpoch,
	})
}

func (k sqliteKeys) updateTier3Wrapping(ctx context.Context, key crypto.WrappedKey) (int64, error) {
	return k.q.UpdateTier3Wrapping(ctx, sqlitegen.UpdateTier3WrappingParams{
		Blob: key.Blob, MasterKeyVersion: int64(key.MasterKeyVersion), ID: key.ID, Version: int64(key.Version),
	})
}

func (k sqliteKeys) countOpenableTier3NotAtMaster(ctx context.Context, version int64) (int64, error) {
	return k.q.CountOpenableTier3NotAtMaster(ctx, version)
}

func (k sqliteKeys) activeTier3State(ctx context.Context, purpose crypto.Purpose, orgID, projectID string, version int64) (string, error) {
	state, err := k.q.AssertActiveTier3Version(ctx, sqlitegen.AssertActiveTier3VersionParams{
		Purpose: string(purpose), OrgID: orgID, ProjectID: projectID, Version: version,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrStaleDEK
	}
	return state, err
}

func (k sqliteKeys) retireRetiringTier3ForScope(ctx context.Context, purpose crypto.Purpose, orgID, projectID string) (int64, error) {
	return k.q.RetireRetiringTier3ForScope(ctx, sqlitegen.RetireRetiringTier3ForScopeParams{
		Purpose: string(purpose), OrgID: orgID, ProjectID: projectID,
	})
}

func (k sqliteKeys) insertScopeGenerationRow(ctx context.Context, scope string) error {
	err := k.q.InsertKeyGeneration(ctx, scope)
	if sqliteUniqueViolation(err) {
		return crypto.ErrKeyExists
	}
	return err
}

func (k pgKeys) txToken() *authz.TxToken { return k.tok }

func (k pgKeys) acquireHierarchy(ctx context.Context) error {
	// SELECT ... FOR UPDATE locks the hierarchy generation, serializing tier-3
	// key creation against master and root rotation in this transaction.
	_, err := k.q.AcquireHierarchyGeneration(ctx)
	return err
}

func (k pgKeys) acquireScope(ctx context.Context, scope string) error {
	_, err := k.q.AcquireScopeGeneration(ctx, scope)
	return err
}

func (k pgKeys) activeMasters(ctx context.Context) ([]activeMasterRow, error) {
	rows, err := k.q.GetActiveMasterKeys(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]activeMasterRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, activeMasterRow{version: row.Version, rootKeyEpoch: row.RootKeyEpoch})
	}
	return out, nil
}

func (k pgKeys) insertMasterRow(ctx context.Context, key crypto.WrappedKey) error {
	err := k.q.InsertMasterKey(ctx, pggen.InsertMasterKeyParams{
		Version: int64(key.Version), RootKeyEpoch: int64(key.RootKeyEpoch), Blob: key.Blob,
		CreatedAt: pgtype.Timestamptz{Time: CanonTime(key.CreatedAt), Valid: true},
	})
	if pgUniqueViolation(err) {
		return crypto.ErrKeyExists
	}
	return err
}

func (k pgKeys) insertTier3Row(ctx context.Context, key crypto.WrappedKey) error {
	err := k.q.InsertTier3Key(ctx, pggen.InsertTier3KeyParams{
		ID: key.ID, Purpose: string(key.Purpose), OrgID: key.OrgID, ProjectID: key.ProjectID,
		Version: int64(key.Version), MasterKeyVersion: int64(key.MasterKeyVersion), Blob: key.Blob,
		CreatedAt: pgtype.Timestamptz{Time: CanonTime(key.CreatedAt), Valid: true},
	})
	if pgUniqueViolation(err) {
		return crypto.ErrKeyExists
	}
	return err
}

func (k pgKeys) retireTier3AtVersion(ctx context.Context, purpose crypto.Purpose, orgID, projectID string, version int64) (int64, error) {
	return k.q.RetireTier3KeyAtVersion(ctx, pggen.RetireTier3KeyAtVersionParams{
		Purpose: string(purpose), OrgID: orgID, ProjectID: projectID, Version: version,
	})
}

func (k pgKeys) demoteTier3AtVersion(ctx context.Context, key crypto.WrappedKey, version int64) (int64, error) {
	return k.q.DemoteActiveTier3ToRetiring(ctx, pggen.DemoteActiveTier3ToRetiringParams{
		Purpose: string(key.Purpose), OrgID: key.OrgID, ProjectID: key.ProjectID, Version: version,
	})
}

func (k pgKeys) retireMasterAtVersion(ctx context.Context, version int64) (int64, error) {
	return k.q.RetireMasterAtVersion(ctx, version)
}

func (k pgKeys) retireMasterWrapper(ctx context.Context, master activeMasterRow) (int64, error) {
	return k.q.RetireMasterWrapperAtEpoch(ctx, pggen.RetireMasterWrapperAtEpochParams{
		Version: master.version, RootKeyEpoch: master.rootKeyEpoch,
	})
}

func (k pgKeys) updateTier3Wrapping(ctx context.Context, key crypto.WrappedKey) (int64, error) {
	return k.q.UpdateTier3Wrapping(ctx, pggen.UpdateTier3WrappingParams{
		Blob: key.Blob, MasterKeyVersion: int64(key.MasterKeyVersion), ID: key.ID, Version: int64(key.Version),
	})
}

func (k pgKeys) countOpenableTier3NotAtMaster(ctx context.Context, version int64) (int64, error) {
	return k.q.CountOpenableTier3NotAtMaster(ctx, version)
}

func (k pgKeys) activeTier3State(ctx context.Context, purpose crypto.Purpose, orgID, projectID string, version int64) (string, error) {
	state, err := k.q.AssertActiveTier3Version(ctx, pggen.AssertActiveTier3VersionParams{
		Purpose: string(purpose), OrgID: orgID, ProjectID: projectID, Version: version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrStaleDEK
	}
	return state, err
}

func (k pgKeys) retireRetiringTier3ForScope(ctx context.Context, purpose crypto.Purpose, orgID, projectID string) (int64, error) {
	return k.q.RetireRetiringTier3ForScope(ctx, pggen.RetireRetiringTier3ForScopeParams{
		Purpose: string(purpose), OrgID: orgID, ProjectID: projectID,
	})
}

func (k pgKeys) insertScopeGenerationRow(ctx context.Context, scope string) error {
	err := k.q.InsertKeyGeneration(ctx, scope)
	if pgUniqueViolation(err) {
		return crypto.ErrKeyExists
	}
	return err
}

// insertInitialProjectDEK is an initial-only scoped insertion. It cannot alter
// existing key versions or write another project's key, and it shares the
// master rotation fence with normal key creation.
func insertInitialProjectDEK(ctx context.Context, p authz.Proof, key crypto.WrappedKey, adapter keyMutationAdapter) error {
	chain, err := authz.Verify(p, authz.StoreKeysInsertInitialProjectDEK, adapter.txToken())
	if err != nil {
		return err
	}
	if chain.Project == "" || key.Purpose != crypto.PurposeProject || key.OrgID != string(chain.Org) || key.ProjectID != string(chain.Project) || key.Version != 1 {
		return ErrConflict
	}
	if err := adapter.acquireHierarchy(ctx); err != nil {
		return err
	}
	if err := assertActiveMaster(ctx, key.MasterKeyVersion, adapter); err != nil {
		return err
	}
	if err := adapter.insertTier3Row(ctx, key); err != nil {
		return err
	}
	return adapter.insertScopeGenerationRow(ctx, scopeGenerationKey(key.Purpose, key.OrgID, key.ProjectID))
}

func assertRootKeyEpoch(ctx context.Context, p authz.Proof, epoch uint32, adapter keyMutationAdapter) error {
	if err := verifyKeyMutation(p, authz.StoreKeysAssertRootKeyEpoch, adapter); err != nil {
		return err
	}
	if err := adapter.acquireSelfConfigBinding(ctx); err != nil {
		return err
	}
	if err := adapter.acquireHierarchy(ctx); err != nil {
		return err
	}
	masters, err := adapter.activeMasters(ctx)
	if err != nil {
		return err
	}
	if len(masters) != 2 {
		return crypto.ErrNotDualWrapped
	}
	var latest int64
	for _, master := range masters {
		if master.rootKeyEpoch > latest {
			latest = master.rootKeyEpoch
		}
	}
	if epoch == 0 || latest != int64(epoch) {
		return crypto.ErrRootRotationBlocked
	}
	return nil
}

// An unmanaged installation has no binding row. Existing bindings serialize
// root lifecycle operations with target commits and deployment completion.
func (k sqliteKeys) acquireSelfConfigBinding(ctx context.Context) error {
	_, err := k.q.LockSelfConfigBinding(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}
func (k pgKeys) acquireSelfConfigBinding(ctx context.Context) error {
	_, err := k.q.LockSelfConfigBinding(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}
func (k sqliteKeys) rootFinalizationBlocked(ctx context.Context) (bool, error) {
	count, err := k.q.CountSelfConfigRootFinalizationBlockers(ctx)
	return count != 0, err
}
func (k pgKeys) rootFinalizationBlocked(ctx context.Context) (bool, error) {
	count, err := k.q.CountSelfConfigRootFinalizationBlockers(ctx)
	return count != 0, err
}
