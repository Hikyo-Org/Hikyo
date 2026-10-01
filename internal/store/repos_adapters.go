package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

type adapterQueries struct {
	db  adapterDB
	tok *authz.TxToken
}

func (r sqliteRepos) Adapters() AdapterRepo {
	return adapterQueries{db: sqliteAdoptDB{db: r.db}, tok: r.tok}
}
func (r pgRepos) Adapters() AdapterRepo {
	return adapterQueries{db: pgAdoptDB{db: r.db}, tok: r.tok}
}

func (r adapterQueries) Target(ctx context.Context, p authz.Proof, targetID string) (AdapterTarget, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersTarget, r.tok)
	if err != nil {
		return AdapterTarget{}, err
	}
	target, err := r.db.adapterStoreQueries().adapterTarget(ctx, chain, targetID)
	if err != nil {
		return AdapterTarget{}, err
	}
	target.Findings, err = r.targetFindings(ctx, chain, target)
	return target, err
}

func (r adapterQueries) ListAdaptersForReencrypt(ctx context.Context, p authz.Proof, cursor string, limit int) ([]ReencryptFieldRow, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersListForReencrypt, r.tok)
	if err != nil {
		return nil, err
	}
	return r.db.adapterStoreQueries().listAdaptersForReencrypt(ctx, chain, cursor, limit)
}

func (r adapterQueries) ReencryptAdapter(ctx context.Context, p authz.Proof, id string, newCiphertext, oldCiphertext []byte) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersReencrypt, r.tok)
	if err != nil {
		return false, err
	}
	rows, err := r.db.adapterStoreQueries().reencryptAdapter(ctx, chain, id, newCiphertext, oldCiphertext)
	return rows == 1, err
}

func (r adapterQueries) ListRouteMovesForReencrypt(ctx context.Context, p authz.Proof, cursor string, limit int) ([]ReencryptFieldRow, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersListMovesForReencrypt, r.tok)
	if err != nil {
		return nil, err
	}
	return r.db.adapterStoreQueries().listMovesForReencrypt(ctx, chain, cursor, limit)
}

func (r adapterQueries) ReencryptRouteMove(ctx context.Context, p authz.Proof, id string, newCiphertext, oldCiphertext []byte) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersReencryptMove, r.tok)
	if err != nil {
		return false, err
	}
	rows, err := r.db.adapterStoreQueries().reencryptMove(ctx, chain, id, newCiphertext, oldCiphertext)
	return rows == 1, err
}

func (r adapterQueries) Mapping(ctx context.Context, p authz.Proof, targetID string) ([]adapter.ManifestEntry, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersMapping, r.tok)
	if err != nil {
		return nil, err
	}
	return r.db.adapterStoreQueries().mapping(ctx, chain, targetID)
}

func (r adapterQueries) PlanMaterial(ctx context.Context, p authz.Proof, targetID string) (AdapterPlanMaterial, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersPlanMaterial, r.tok)
	if err != nil {
		return AdapterPlanMaterial{}, err
	}
	target, err := r.db.adapterStoreQueries().adapterTarget(ctx, chain, targetID)
	if err != nil {
		return AdapterPlanMaterial{}, err
	}
	credential, transport, err := r.db.adapterStoreQueries().planCredential(ctx, chain, target.AdapterID)
	if err != nil {
		return AdapterPlanMaterial{}, err
	}
	out := AdapterPlanMaterial{Target: target, Transport: transport, CredentialCiphertext: credential}
	out.Manifest, err = r.db.adapterStoreQueries().planManifest(ctx, chain, targetID, target.EnvironmentID)
	if err != nil {
		return AdapterPlanMaterial{}, err
	}
	out.Ledger, err = r.db.adapterStoreQueries().planLedger(ctx, chain, targetID, target.EnvironmentID)
	if err != nil {
		return AdapterPlanMaterial{}, err
	}
	return out, nil
}

func (r adapterQueries) TargetEnvironments(ctx context.Context, p authz.Proof, targetID string) ([]string, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersTargetEnvironments, r.tok)
	if err != nil {
		return nil, err
	}
	return r.db.adapterStoreQueries().targetEnvironments(ctx, chain, targetID)
}

func (r adapterQueries) Environments(ctx context.Context, p authz.Proof, adapterID string) ([]string, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersEnvironments, r.tok)
	if err != nil {
		return nil, err
	}
	return r.db.adapterStoreQueries().environments(ctx, chain, adapterID)
}

func (r adapterQueries) Conflicts(ctx context.Context, p authz.Proof, targetID string) ([]AdapterConflictArtifact, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersConflicts, r.tok)
	if err != nil {
		return nil, err
	}
	return r.db.adapterStoreQueries().conflicts(ctx, chain, targetID)
}

func (r adapterQueries) RecordPlan(ctx context.Context, p authz.Proof, targetID, artifactID string, expectedGeneration, expectedRepositoryID, expectedDestinationID int64, entries []AdapterConflictEntry, at time.Time) error {
	chain, err := authz.Verify(p, authz.StoreAdaptersRecordPlan, r.tok)
	if err != nil {
		return err
	}
	return recordAdapterPlan(ctx, r.db, chain, targetID, artifactID, expectedGeneration, expectedRepositoryID, expectedDestinationID, entries, at)
}

func recordAdapterPlan(ctx context.Context, db adapterDB, chain domain.Scope, targetID, artifactID string, expectedGeneration, expectedRepositoryID, expectedDestinationID int64, entries []AdapterConflictEntry, at time.Time) error {
	if targetID == "" || artifactID == "" || expectedGeneration <= 0 || expectedDestinationID <= 0 || at.IsZero() {
		return fmt.Errorf("%w: incomplete adapter plan artifact", ErrConflict)
	}
	row, err := db.adapterStoreQueries().planTarget(ctx, chain, targetID)
	if isNoRows(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	environmentID, destinationID, repositoryID, generation := row.environmentID, row.destinationID, row.repositoryID, row.generation
	if generation != expectedGeneration || repositoryID != expectedRepositoryID || destinationID != expectedDestinationID {
		return fmt.Errorf("%w: adapter target changed while planning", ErrConflict)
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		key := entry.Surface + "\x00" + strings.ToUpper(entry.EffectiveName)
		if (entry.Surface != string(adapter.Secret) && entry.Surface != string(adapter.Variable)) || entry.EffectiveName == "" || seen[key] {
			return fmt.Errorf("%w: invalid or duplicate adapter plan conflict", ErrConflict)
		}
		seen[key] = true
		if rows, err := db.adapterStoreQueries().insertConflict(ctx, chain, newAdapterID("acn"), artifactID, environmentID, targetID, destinationID, repositoryID, generation, entry, at); err != nil || rows != 1 {
			if err != nil {
				return err
			}
			return ErrConflict
		}
	}
	return nil
}

func validateAdapterAdoption(adoption AdapterAdoption) error {
	if adoption.TargetID == "" || adoption.ArtifactID == "" || adoption.AuthorityPrincipalID == "" || adoption.JobID == "" || adoption.AuditAt.IsZero() || len(adoption.Entries) == 0 || len(adoption.Entries) != len(adoption.LedgerIDs) {
		return fmt.Errorf("%w: incomplete adapter adoption", ErrConflict)
	}
	seen := make(map[string]bool, len(adoption.Entries))
	for i, entry := range adoption.Entries {
		key := entry.Surface + "\x00" + strings.ToUpper(entry.EffectiveName)
		if (entry.Surface != string(adapter.Secret) && entry.Surface != string(adapter.Variable)) || entry.EffectiveName == "" || adoption.LedgerIDs[i] == "" || seen[key] {
			return fmt.Errorf("%w: invalid or duplicate adapter adoption entry", ErrConflict)
		}
		seen[key] = true
	}
	return nil
}

func (r adapterQueries) Adopt(ctx context.Context, p authz.Proof, adoption AdapterAdoption) (AdapterAdoptionResult, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersAdopt, r.tok)
	if err != nil {
		return AdapterAdoptionResult{}, err
	}
	if err := validateAdapterAdoption(adoption); err != nil {
		return AdapterAdoptionResult{}, err
	}
	return adoptAdapter(ctx, r.db, chain, adoption)
}

type adapterDB interface {
	pkiStoreQueries() pkiStoreQueries
	transitStoreQueries() transitStoreQueries
	adapterMoveQueries() adapterMoveQueries
	adapterConfigQueries() adapterConfigQueries
	adapterRuntimeQueries() adapterRuntimeQueries
	adapterStoreQueries() adapterStoreQueries
	sshQueries() sshRuntimeQueries
	dynamicQueries() dynamicRuntimeQueries
}

type sqliteAdoptDB struct{ db sqlitegen.DBTX }
type pgAdoptDB struct{ db pggen.DBTX }

func adoptAdapter(ctx context.Context, db adapterDB, chain domain.Scope, adoption AdapterAdoption) (AdapterAdoptionResult, error) {
	row, err := db.adapterStoreQueries().adoptionTarget(ctx, chain, adoption)
	adapterID, environmentID, priorJob := row.adapterID, row.environmentID, row.priorJob
	generation, providerBusy := row.generation, row.providerBusy
	if isNoRows(err) {
		return AdapterAdoptionResult{}, ErrNotFound
	}
	if err != nil {
		return AdapterAdoptionResult{}, err
	}
	if providerBusy == 1 {
		return AdapterAdoptionResult{}, adapter.ErrProviderBusy
	}
	for i, entry := range adoption.Entries {
		conflictRows, err := db.adapterStoreQueries().adoptionConflictCount(ctx, chain, adoption, row, entry)
		if err != nil {
			return AdapterAdoptionResult{}, err
		}
		if conflictRows != 1 {
			return AdapterAdoptionResult{}, fmt.Errorf("%w: stale or mismatched adapter conflict artifact", ErrConflict)
		}
		if rows, err := db.adapterStoreQueries().adoptionInsertLedger(ctx, chain, adoption, row, entry, adoption.LedgerIDs[i]); err != nil || rows != 1 {
			if err != nil {
				return AdapterAdoptionResult{}, constraint(err)
			}
			return AdapterAdoptionResult{}, ErrConflict
		}
		if rows, err := db.adapterStoreQueries().adoptionMarkConflict(ctx, chain, adoption, row, entry); err != nil || rows != 1 {
			if err != nil {
				return AdapterAdoptionResult{}, constraint(err)
			}
			return AdapterAdoptionResult{}, ErrConflict
		}
	}
	if priorJob != "" {
		rows, err := db.adapterStoreQueries().adoptionSupersedeJob(ctx, chain, priorJob, adoption.TargetID, environmentID, adoption.AuditAt)
		if err != nil {
			return AdapterAdoptionResult{}, constraint(err)
		}
		if rows != 1 {
			return AdapterAdoptionResult{}, adapter.ErrSuperseded
		}
	}
	nextGeneration := generation + 1
	if rows, err := db.adapterStoreQueries().adoptionInsertJob(ctx, chain, adoption.JobID, publishedAdapterTarget{id: adoption.TargetID, environmentID: environmentID, authority: adoption.AuthorityPrincipalID}, nextGeneration, adoption.AuditAt); err != nil || rows != 1 {
		if err != nil {
			return AdapterAdoptionResult{}, constraint(err)
		}
		return AdapterAdoptionResult{}, ErrConflict
	}
	if rows, err := db.adapterStoreQueries().adoptionUpdateTarget(ctx, chain, adoption, row, nextGeneration); err != nil || rows != 1 {
		if err != nil {
			return AdapterAdoptionResult{}, constraint(err)
		}
		return AdapterAdoptionResult{}, adapter.ErrProviderBusy
	}
	if rows, err := db.adapterStoreQueries().adoptionUpdateAuthority(ctx, chain, adapterID, adoption.AuthorityPrincipalID); err != nil || rows != 1 {
		if err != nil {
			return AdapterAdoptionResult{}, constraint(err)
		}
		return AdapterAdoptionResult{}, ErrNotFound
	}
	return AdapterAdoptionResult{Generation: nextGeneration, JobID: adoption.JobID, SupersededJobID: priorJob}, nil
}

type publishedAdapterTarget struct {
	id, environmentID, authority, activeJob string
	generation                              int64
}

func (r adapterQueries) EnqueuePublished(ctx context.Context, p authz.Proof, at time.Time) ([]AdapterEnqueueResult, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersEnqueuePublished, r.tok)
	if err != nil {
		return nil, err
	}
	targets, err := r.db.adapterStoreQueries().publishedTargets(ctx, chain)
	if err != nil {
		return nil, err
	}
	return enqueuePublishedTargets(ctx, r.db, chain, targets, at)
}

func enqueuePublishedTargets(ctx context.Context, db adapterDB, chain domain.Scope, targets []publishedAdapterTarget, at time.Time) ([]AdapterEnqueueResult, error) {
	out := make([]AdapterEnqueueResult, 0, len(targets))
	for _, target := range targets {
		jobID := newAdapterID("job")
		if target.activeJob != "" {
			rows, err := db.adapterStoreQueries().adoptionSupersedeJob(ctx, chain, target.activeJob, target.id, target.environmentID, at)
			if err != nil {
				return nil, err
			}
			if rows != 1 {
				return nil, adapter.ErrSuperseded
			}
		}
		next := target.generation + 1
		if rows, err := db.adapterStoreQueries().adoptionInsertJob(ctx, chain, jobID, target, next, at); err != nil || rows != 1 {
			if err != nil {
				return nil, err
			}
			return nil, ErrConflict
		}
		rows, err := db.adapterStoreQueries().enqueueTarget(ctx, chain, target, jobID, next)
		if err != nil {
			return nil, err
		}
		if rows != 1 {
			return nil, adapter.ErrSuperseded
		}
		out = append(out, AdapterEnqueueResult{
			TargetID: target.id, JobID: jobID, SupersededJobID: target.activeJob,
			AuthorityPrincipalID: target.authority, Generation: next,
		})
	}
	return out, nil
}

func (r adapterQueries) EnqueueManual(ctx context.Context, p authz.Proof, targetID, authorityPrincipalID string, at time.Time) (AdapterEnqueueResult, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersEnqueueManual, r.tok)
	if err != nil {
		return AdapterEnqueueResult{}, err
	}
	return enqueueManualTarget(ctx, r.db, chain, targetID, authorityPrincipalID, at)
}

func enqueueManualTarget(ctx context.Context, db adapterDB, chain domain.Scope, targetID, authorityPrincipalID string, at time.Time) (AdapterEnqueueResult, error) {
	if targetID == "" || authorityPrincipalID == "" {
		return AdapterEnqueueResult{}, fmt.Errorf("%w: manual adapter sync requires target and authority", domain.ErrInvalid)
	}
	target, providerBusy, paused, err := db.adapterStoreQueries().manualTarget(ctx, chain, targetID, at)
	if isNoRows(err) {
		return AdapterEnqueueResult{}, ErrNotFound
	}
	if err != nil {
		return AdapterEnqueueResult{}, err
	}
	if providerBusy != 0 {
		return AdapterEnqueueResult{}, adapter.ErrProviderBusy
	}
	if paused != 0 {
		return AdapterEnqueueResult{}, ErrAdapterTargetPaused
	}
	target.authority = authorityPrincipalID
	results, err := enqueuePublishedTargets(ctx, db, chain, []publishedAdapterTarget{target}, at)
	if err != nil {
		return AdapterEnqueueResult{}, err
	}
	return results[0], nil
}

// ErrAdapterTargetPaused refuses a push-shaped act on a paused target. Pause
// is loud: resume is the one path back to pushing.
var ErrAdapterTargetPaused = fmt.Errorf("%w: adapter target is paused; resume it to push", domain.ErrConflict)

func (r adapterQueries) PauseTarget(ctx context.Context, p authz.Proof, targetID string, at time.Time) (AdapterPauseResult, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersPauseTarget, r.tok)
	if err != nil {
		return AdapterPauseResult{}, err
	}
	if targetID == "" {
		return AdapterPauseResult{}, fmt.Errorf("%w: pause requires a target", domain.ErrInvalid)
	}
	target, paused, err := r.db.adapterStoreQueries().pauseTarget(ctx, chain, targetID, at)
	if isNoRows(err) {
		return AdapterPauseResult{}, ErrNotFound
	}
	if err != nil {
		return AdapterPauseResult{}, err
	}
	result := AdapterPauseResult{TargetID: target.targetID, AuthorityPrincipalID: target.authority, Generation: target.generation}
	if paused != 0 {
		result.AlreadyPaused = true
		return result, nil
	}
	if target.providerBusy == 1 {
		return AdapterPauseResult{}, adapter.ErrProviderBusy
	}
	if target.activeJob != "" {
		rows, err := r.db.adapterStoreQueries().adoptionSupersedeJob(ctx, chain, target.activeJob, target.targetID, target.environmentID, at)
		if err != nil {
			return AdapterPauseResult{}, err
		}
		if rows == 1 {
			result.SupersededJobID = target.activeJob
		}
	}
	// The generation bump fences a worker still inside the superseded job:
	// its next Gate, Prepare, or Finish sees a generation it does not hold.
	result.Generation = target.generation + 1
	rows, err := r.db.adapterStoreQueries().pauseTargetUpdate(ctx, chain, target, result.Generation, at)
	if err != nil {
		return AdapterPauseResult{}, err
	}
	if rows != 1 {
		return AdapterPauseResult{}, adapter.ErrProviderBusy
	}
	return result, nil
}

func (r adapterQueries) ResumeTarget(ctx context.Context, p authz.Proof, targetID, authorityPrincipalID string, at time.Time) (AdapterResumeResult, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersResumeTarget, r.tok)
	if err != nil {
		return AdapterResumeResult{}, err
	}
	if targetID == "" || authorityPrincipalID == "" {
		return AdapterResumeResult{}, fmt.Errorf("%w: resume requires target and authority", domain.ErrInvalid)
	}
	target, providerBusy, paused, err := r.db.adapterStoreQueries().manualTarget(ctx, chain, targetID, at)
	if isNoRows(err) {
		return AdapterResumeResult{}, ErrNotFound
	}
	if err != nil {
		return AdapterResumeResult{}, err
	}
	if paused == 0 {
		return AdapterResumeResult{}, fmt.Errorf("%w: adapter target is not paused", domain.ErrConflict)
	}
	if providerBusy != 0 {
		return AdapterResumeResult{}, adapter.ErrProviderBusy
	}
	rows, err := r.db.adapterStoreQueries().resumeTarget(ctx, chain, target)
	if err != nil {
		return AdapterResumeResult{}, err
	}
	if rows != 1 {
		return AdapterResumeResult{}, adapter.ErrSuperseded
	}
	target.authority = authorityPrincipalID
	results, err := enqueuePublishedTargets(ctx, r.db, chain, []publishedAdapterTarget{target}, at)
	if err != nil {
		return AdapterResumeResult{}, err
	}
	out := AdapterResumeResult{Enqueue: results[0]}
	out.Revision, err = r.db.adapterStoreQueries().resumeRevision(ctx, chain, target.environmentID)
	if err != nil {
		return AdapterResumeResult{}, err
	}
	return out, nil
}

// HealthCounts is the label-free operator read (#157). It spans every tenant
// on purpose: the gauges and doctor report how many targets need a human, not
// which. Only active targets count; a tombstoned target's orphans were
// reported loudly at removal time.
func (r adapterQueries) HealthCounts(ctx context.Context, p authz.Proof) (AdapterHealthCounts, error) {
	if _, err := authz.Verify(p, authz.StoreAdaptersHealthCounts, r.tok); err != nil {
		return AdapterHealthCounts{}, err
	}
	return r.db.adapterStoreQueries().healthCounts(ctx)
}

type adapterTeardownTarget struct {
	adapterID, targetID, environmentID, authority, activeJob string
	generation                                               int64
	providerBusy                                             int
	orphaned                                                 []string
}

func (r adapterQueries) TeardownTarget(ctx context.Context, p authz.Proof, targetID string, keepRemote bool, at time.Time) (AdapterTeardownResult, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersTeardownTarget, r.tok)
	if err != nil {
		return AdapterTeardownResult{}, err
	}
	target, err := r.db.adapterStoreQueries().teardownTarget(ctx, chain, targetID, at)
	if isNoRows(err) {
		return AdapterTeardownResult{}, ErrNotFound
	}
	if err != nil {
		return AdapterTeardownResult{}, err
	}
	target.orphaned, err = adapterOrphans(ctx, r.db, chain, target.targetID, target.environmentID)
	if err != nil {
		return AdapterTeardownResult{}, err
	}
	return teardownAdapterTarget(ctx, r.db, chain, target, keepRemote, at)
}

func (r adapterQueries) TeardownAdapter(ctx context.Context, p authz.Proof, adapterID string, keepRemote bool, at time.Time) (AdapterTeardownBatch, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersTeardownAdapter, r.tok)
	if err != nil {
		return AdapterTeardownBatch{}, err
	}
	authority, err := r.db.adapterStoreQueries().teardownAuthority(ctx, chain, adapterID)
	if isNoRows(err) {
		return AdapterTeardownBatch{}, ErrNotFound
	}
	if err != nil {
		return AdapterTeardownBatch{}, err
	}
	targets, err := r.db.adapterStoreQueries().teardownTargets(ctx, chain, adapterID, at)
	if err != nil {
		return AdapterTeardownBatch{}, err
	}
	for i := range targets {
		targets[i].orphaned, err = adapterOrphans(ctx, r.db, chain, targets[i].targetID, targets[i].environmentID)
		if err != nil {
			return AdapterTeardownBatch{}, err
		}
	}
	return teardownWholeAdapter(ctx, r.db, chain, adapterID, authority, targets, keepRemote, at)
}

func adapterOrphans(ctx context.Context, db adapterDB, chain domain.Scope, targetID, environmentID string) ([]string, error) {
	return db.adapterStoreQueries().orphans(ctx, chain, targetID, environmentID)
}

func teardownAdapterTarget(ctx context.Context, db adapterDB, chain domain.Scope, target adapterTeardownTarget, keepRemote bool, at time.Time) (AdapterTeardownResult, error) {
	if target.providerBusy == 1 {
		return AdapterTeardownResult{}, adapter.ErrProviderBusy
	}
	result := AdapterTeardownResult{
		TargetID: target.targetID, AuthorityPrincipalID: target.authority,
		SupersededJobID: target.activeJob, Generation: target.generation + 1,
	}
	if target.activeJob != "" {
		rows, err := db.adapterStoreQueries().adoptionSupersedeJob(ctx, chain, target.activeJob, target.targetID, target.environmentID, at)
		if err != nil {
			return AdapterTeardownResult{}, err
		}
		if rows != 1 {
			return AdapterTeardownResult{}, adapter.ErrSuperseded
		}
	}
	if keepRemote {
		if err := db.adapterStoreQueries().releaseLedger(ctx, chain, target, at); err != nil {
			return AdapterTeardownResult{}, err
		}
		rows, err := db.adapterStoreQueries().retainTarget(ctx, chain, target, result.Generation, at)
		if err != nil {
			return AdapterTeardownResult{}, err
		}
		if rows != 1 {
			return AdapterTeardownResult{}, adapter.ErrProviderBusy
		}
		result.Orphaned = append([]string(nil), target.orphaned...)
		return result, nil
	}
	result.JobID = newAdapterID("job")
	if rows, err := db.adapterStoreQueries().insertScrubJob(ctx, chain, target, result, at); err != nil || rows != 1 {
		if err != nil {
			return AdapterTeardownResult{}, err
		}
		return AdapterTeardownResult{}, ErrConflict
	}
	rows, err := db.adapterStoreQueries().scrubTarget(ctx, chain, target, result, at)
	if err != nil {
		return AdapterTeardownResult{}, err
	}
	if rows != 1 {
		return AdapterTeardownResult{}, adapter.ErrProviderBusy
	}
	return result, nil
}

func teardownWholeAdapter(ctx context.Context, db adapterDB, chain domain.Scope, adapterID, authority string, targets []adapterTeardownTarget, keepRemote bool, at time.Time) (AdapterTeardownBatch, error) {
	results := make([]AdapterTeardownResult, 0, len(targets))
	for _, target := range targets {
		result, err := teardownAdapterTarget(ctx, db, chain, target, keepRemote, at)
		if err != nil {
			return AdapterTeardownBatch{}, err
		}
		results = append(results, result)
	}
	rows, err := db.adapterStoreQueries().markTombstoned(ctx, chain, adapterID)
	if err != nil {
		return AdapterTeardownBatch{}, err
	}
	if rows != 1 {
		return AdapterTeardownBatch{}, ErrNotFound
	}
	if err := db.adapterStoreQueries().eraseUnusedCredential(ctx, chain, adapterID); err != nil {
		return AdapterTeardownBatch{}, err
	}
	return AdapterTeardownBatch{AuthorityPrincipalID: authority, Targets: results}, nil
}

func (r adapterQueries) ReplaceCredential(ctx context.Context, p authz.Proof, mutation AdapterCredentialMutation) (AdapterCredentialResult, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersReplaceCredential, r.tok)
	if err != nil {
		return AdapterCredentialResult{}, err
	}
	if mutation.AdapterID == "" || mutation.AuthorityPrincipalID == "" || len(mutation.CredentialCiphertext) == 0 {
		return AdapterCredentialResult{}, fmt.Errorf("%w: credential replacement requires adapter, sealed credential, and authority", domain.ErrInvalid)
	}
	return replaceAdapterCredential(ctx, r.db, chain, mutation)
}

func replaceAdapterCredential(ctx context.Context, db adapterDB, chain domain.Scope, mutation AdapterCredentialMutation) (AdapterCredentialResult, error) {
	previous, targetCount, providerBusy, err := db.adapterStoreQueries().replaceCredentialTarget(ctx, chain, mutation.AdapterID, mutation.At)
	if isNoRows(err) {
		return AdapterCredentialResult{}, ErrNotFound
	}
	if err != nil {
		return AdapterCredentialResult{}, err
	}
	if providerBusy != 0 {
		return AdapterCredentialResult{}, adapter.ErrProviderBusy
	}
	rows, err := db.adapterStoreQueries().replaceCredential(ctx, chain, mutation)
	if err != nil {
		return AdapterCredentialResult{}, err
	}
	if rows != 1 {
		return AdapterCredentialResult{}, ErrNotFound
	}
	rows, err = db.adapterStoreQueries().replaceCredentialBump(ctx, chain, mutation.AdapterID, mutation.At)
	if err != nil {
		return AdapterCredentialResult{}, err
	}
	if rows != int64(targetCount) {
		return AdapterCredentialResult{}, adapter.ErrProviderBusy
	}
	return AdapterCredentialResult{PreviousAuthorityPrincipalID: previous, AuthorityPrincipalID: mutation.AuthorityPrincipalID, TargetCount: targetCount}, nil
}

func (r adapterQueries) RevokeCredential(ctx context.Context, p authz.Proof, adapterID string, at time.Time) (AdapterCredentialResult, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersRevokeCredential, r.tok)
	if err != nil {
		return AdapterCredentialResult{}, err
	}
	return revokeAdapterCredential(ctx, r.db, chain, adapterID, at)
}

func revokeAdapterCredential(ctx context.Context, db adapterDB, chain domain.Scope, adapterID string, at time.Time) (AdapterCredentialResult, error) {
	if adapterID == "" {
		return AdapterCredentialResult{}, fmt.Errorf("%w: credential revocation requires adapter id", domain.ErrInvalid)
	}
	authority, targetCount, err := db.adapterStoreQueries().revokeCredentialTarget(ctx, chain, adapterID)
	if isNoRows(err) {
		return AdapterCredentialResult{}, ErrNotFound
	}
	if err != nil {
		return AdapterCredentialResult{}, err
	}
	rows, err := db.adapterStoreQueries().revokeCredential(ctx, chain, adapterID)
	if err != nil {
		return AdapterCredentialResult{}, err
	}
	if rows != 1 {
		return AdapterCredentialResult{}, ErrNotFound
	}
	rows, err = db.adapterStoreQueries().revokeCredentialBump(ctx, chain, adapterID)
	if err != nil {
		return AdapterCredentialResult{}, err
	}
	if rows != int64(targetCount) {
		return AdapterCredentialResult{}, ErrConflict
	}
	return AdapterCredentialResult{PreviousAuthorityPrincipalID: authority, AuthorityPrincipalID: authority, TargetCount: targetCount}, nil
}
