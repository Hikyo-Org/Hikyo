package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func (r adapterQueries) MoveTarget(ctx context.Context, p authz.Proof, mutation AdapterRouteMoveMutation) (AdapterRouteMoveResult, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersMoveTarget, r.tok)
	if err != nil {
		return AdapterRouteMoveResult{}, err
	}
	return beginAdapterTargetMove(ctx, r.db, chain, mutation)
}

func (r adapterQueries) MoveOrigin(ctx context.Context, p authz.Proof, mutation AdapterOriginMoveMutation) (AdapterRouteMoveBatch, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersMoveOrigin, r.tok)
	if err != nil {
		return AdapterRouteMoveBatch{}, err
	}
	return beginAdapterOriginMove(ctx, r.db, chain, mutation)
}

func (r adapterQueries) Move(ctx context.Context, p authz.Proof, moveID string) (AdapterMove, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersMove, r.tok)
	if err != nil {
		return AdapterMove{}, err
	}
	return readAdapterMove(ctx, r.db, chain, moveID, false)
}

func (r adapterQueries) CancelMove(ctx context.Context, p authz.Proof, moveID, authorityPrincipalID string, at time.Time) (AdapterMove, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersCancelMove, r.tok)
	if err != nil {
		return AdapterMove{}, err
	}
	return cancelAdapterMove(ctx, r.db, chain, moveID, authorityPrincipalID, at)
}

func (r adapterQueries) ReplaceMoveTarget(ctx context.Context, p authz.Proof, moveID string, target AdapterTargetMutation, authorityPrincipalID string, at time.Time) (AdapterMove, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersReplaceMoveTarget, r.tok)
	if err != nil {
		return AdapterMove{}, err
	}
	return replaceAdapterMoveTarget(ctx, r.db, chain, moveID, target, authorityPrincipalID, at)
}

func (r adapterQueries) ReplaceMoveOrigin(ctx context.Context, p authz.Proof, moveID, origin string, pendingCredential []byte, authorityPrincipalID string, at time.Time) (AdapterMove, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersReplaceMoveOrigin, r.tok)
	if err != nil {
		return AdapterMove{}, err
	}
	return replaceAdapterMoveOrigin(ctx, r.db, chain, moveID, origin, pendingCredential, authorityPrincipalID, at)
}

func readAdapterMove(ctx context.Context, db adapterDB, chain domain.Scope, moveID string, lock bool) (AdapterMove, error) {
	var queryResult adapterMoveGetRow
	var err error
	if lock {
		queryResult, err = db.adapterMoveQueries().getLocked(ctx, moveID, chain.Org, chain.Project)
	} else {
		queryResult, err = db.adapterMoveQueries().get(ctx, moveID, chain.Org, chain.Project)
	}
	out := AdapterMove{ID: queryResult.ID, AdapterID: queryResult.AdapterID, Kind: queryResult.Kind, State: queryResult.State, KeepRemote: queryResult.Keep, PendingOrigin: queryResult.PendingOrigin, CreatedAt: queryResult.Created, AuthorityPrincipalID: queryResult.AuthorityPrincipalID}
	if isNoRows(err) {
		return AdapterMove{}, ErrNotFound
	}
	if err != nil {
		return AdapterMove{}, err
	}

	rows, err := db.adapterMoveQueries().targets(ctx, moveID, chain.Org, chain.Project)
	if err != nil {
		return AdapterMove{}, err
	}
	for _, targetQueryRow := range rows {
		target := AdapterMoveTarget{TargetID: targetQueryRow.TargetID, EnvironmentID: targetQueryRow.EnvironmentID, DestinationKind: targetQueryRow.DestinationKind, DestinationOwner: targetQueryRow.DestinationOwner, DestinationName: targetQueryRow.DestinationName, DestinationEnvironment: targetQueryRow.DestinationEnvironment, DestinationScope: targetQueryRow.DestinationScope, DestinationID: targetQueryRow.DestinationID, RepositoryID: targetQueryRow.RepositoryID, Visibility: targetQueryRow.Visibility, NamePrefix: targetQueryRow.NamePrefix}
		selectedJSON := targetQueryRow.SelectedJSON
		orphanJSON := targetQueryRow.OrphanJSON
		if err := json.Unmarshal(orphanJSON, &target.Orphaned); err != nil {
			return AdapterMove{}, err
		}
		if err := json.Unmarshal(selectedJSON, &target.SelectedRepositoryIDs); err != nil {
			return AdapterMove{}, err
		}
		if target.Orphaned == nil {
			target.Orphaned = []string{}
		}
		out.Targets = append(out.Targets, target)
	}

	jobRows, err := db.adapterMoveQueries().jobs(ctx, moveID, chain.Org, chain.Project)
	if err != nil {
		return AdapterMove{}, err
	}
	jobs := map[string][]AdapterMoveJob{}
	for _, jobQueryRow := range jobRows {
		job := AdapterMoveJob{ID: jobQueryRow.ID, TargetID: jobQueryRow.TargetID, Kind: jobQueryRow.Kind, State: jobQueryRow.State}

		jobs[job.TargetID] = append(jobs[job.TargetID], job)
	}
	for i := range out.Targets {
		out.Targets[i].Jobs = jobs[out.Targets[i].TargetID]
		if out.Targets[i].Jobs == nil {
			out.Targets[i].Jobs = []AdapterMoveJob{}
		}
	}
	return out, nil
}

func cancelAdapterMove(ctx context.Context, db adapterDB, chain domain.Scope, moveID, authorityPrincipalID string, at time.Time) (AdapterMove, error) {
	if moveID == "" || authorityPrincipalID == "" || at.IsZero() {
		return AdapterMove{}, fmt.Errorf("%w: move cancellation requires move, authority, and timestamp", domain.ErrInvalid)
	}
	move, err := readAdapterMove(ctx, db, chain, moveID, true)
	if err != nil {
		return AdapterMove{}, err
	}
	if move.State != "attention_required" {
		return AdapterMove{}, fmt.Errorf("%w: only an attention-required route move can be canceled", domain.ErrConflict)
	}
	previousAuthority := move.AuthorityPrincipalID
	stamp := at
	for _, target := range move.Targets {

		lookupResult, err := db.adapterMoveQueries().cancelTarget(ctx, target.TargetID, chain.Org, chain.Project, target.EnvironmentID)
		if err != nil {
			return AdapterMove{}, adapter.ErrSuperseded
		}
		if lookupResult.ProviderBusy != 0 {
			return AdapterMove{}, adapter.ErrProviderBusy
		}
		jobID := newAdapterID("job")
		nextGeneration := lookupResult.Generation + 1

		if rows, err := db.adapterMoveQueries().insertConvergeJob(ctx, jobID, chain.Org, chain.Project, target.EnvironmentID, target.TargetID, moveID, authorityPrincipalID, nextGeneration, target.TargetID, stamp, stamp); err != nil || rows != 1 {
			return AdapterMove{}, errors.Join(err, ErrConflict)
		}

		if rows, err := db.adapterMoveQueries().activateCanceledTarget(ctx, nextGeneration, jobID, target.TargetID, chain.Org, chain.Project, target.EnvironmentID, lookupResult.Generation); err != nil || rows != 1 {
			return AdapterMove{}, errors.Join(err, adapter.ErrSuperseded)
		}
	}

	if rows, err := db.adapterMoveQueries().restoreAdapter(ctx, authorityPrincipalID, move.AdapterID, chain.Org, chain.Project); err != nil || rows != 1 {
		return AdapterMove{}, errors.Join(err, adapter.ErrSuperseded)
	}

	if _, err := db.adapterMoveQueries().deleteClaims(ctx, moveID, chain.Org, chain.Project); err != nil {
		return AdapterMove{}, err
	}

	if rows, err := db.adapterMoveQueries().cancel(ctx, moveID, chain.Org, chain.Project); err != nil || rows != 1 {
		return AdapterMove{}, errors.Join(err, adapter.ErrSuperseded)
	}
	out, err := readAdapterMove(ctx, db, chain, moveID, false)
	if err != nil {
		return AdapterMove{}, err
	}
	out.PreviousAuthorityPrincipalID = previousAuthority
	return out, nil
}

func replaceAdapterMoveTarget(ctx context.Context, db adapterDB, chain domain.Scope, moveID string, target AdapterTargetMutation, authorityPrincipalID string, at time.Time) (AdapterMove, error) {
	if err := validatePendingTarget(target); err != nil {
		return AdapterMove{}, err
	}
	if moveID == "" || authorityPrincipalID == "" || at.IsZero() {
		return AdapterMove{}, fmt.Errorf("%w: pending target replacement requires move, authority, and timestamp", domain.ErrInvalid)
	}
	move, err := readAdapterMove(ctx, db, chain, moveID, true)
	if err != nil {
		return AdapterMove{}, err
	}
	if move.State != "attention_required" || move.Kind != "target" || len(move.Targets) != 1 || move.Targets[0].TargetID != target.ID || move.Targets[0].EnvironmentID != target.EnvironmentID || move.AdapterID != target.AdapterID {
		return AdapterMove{}, fmt.Errorf("%w: pending target replacement does not match the attention-required move", domain.ErrConflict)
	}
	if move.Targets[0].DestinationScope != target.DestinationScope {
		return AdapterMove{}, fmt.Errorf("%w: a GitLab environment scope is immutable; remove the target and add a new one", domain.ErrConflict)
	}
	if err := requireUnchangedMoveFlags(ctx, db, chain, target); err != nil {
		return AdapterMove{}, err
	}
	previousAuthority := move.AuthorityPrincipalID

	if _, err := db.adapterMoveQueries().deleteClaims(ctx, moveID, chain.Org, chain.Project); err != nil {
		return AdapterMove{}, err
	}

	if _, err := db.adapterMoveQueries().deleteKeys(ctx, moveID, target.ID, chain.Org, chain.Project, target.EnvironmentID); err != nil {
		return AdapterMove{}, err
	}
	selectedJSON, err := json.Marshal(target.SelectedRepositoryIDs)
	if err != nil {
		return AdapterMove{}, err
	}

	if rows, err := db.adapterMoveQueries().updatePendingTarget(ctx, target.DestinationKind, target.DestinationOwner, target.DestinationName, target.DestinationEnvironment, target.DestinationScope, target.RepositoryID, target.Visibility, selectedJSON, target.NamePrefix, moveID, target.ID, chain.Org, chain.Project, target.EnvironmentID); err != nil || rows != 1 {
		return AdapterMove{}, errors.Join(err, adapter.ErrSuperseded)
	}
	for _, keyID := range target.KeyIDs {

		if rows, err := db.adapterMoveQueries().insertKey(ctx, moveID, chain.Org, chain.Project, target.EnvironmentID, target.ID, keyID); err != nil || rows != 1 {
			return AdapterMove{}, errors.Join(err, ErrConflict)
		}
	}
	if err := reserveAdapterMoveClaims(ctx, db, chain, moveID, move.PendingOrigin, target); err != nil {
		return AdapterMove{}, err
	}
	if err := resumeAdapterMove(ctx, db, chain, move, authorityPrincipalID, at); err != nil {
		return AdapterMove{}, err
	}
	out, err := readAdapterMove(ctx, db, chain, moveID, false)
	if err != nil {
		return AdapterMove{}, err
	}
	out.PreviousAuthorityPrincipalID = previousAuthority
	return out, nil
}

func replaceAdapterMoveOrigin(ctx context.Context, db adapterDB, chain domain.Scope, moveID, origin string, pendingCredential []byte, authorityPrincipalID string, at time.Time) (AdapterMove, error) {
	if moveID == "" || origin == "" || len(pendingCredential) == 0 || authorityPrincipalID == "" || at.IsZero() {
		return AdapterMove{}, fmt.Errorf("%w: pending origin replacement requires move, origin, credential, authority, and timestamp", domain.ErrInvalid)
	}
	move, err := readAdapterMove(ctx, db, chain, moveID, true)
	if err != nil {
		return AdapterMove{}, err
	}
	if move.State != "attention_required" || move.Kind != "origin" {
		return AdapterMove{}, fmt.Errorf("%w: pending origin replacement requires an attention-required origin move", domain.ErrConflict)
	}
	previousAuthority := move.AuthorityPrincipalID
	var collisions int

	collisionQueryResult, err := db.adapterMoveQueries().replaceOriginCollisions(ctx, chain.Org, chain.Project, move.AdapterID, origin, chain.Org, chain.Project, moveID, origin)
	collisions = collisionQueryResult
	if err != nil {
		return AdapterMove{}, err
	}
	if collisions != 0 {
		return AdapterMove{}, fmt.Errorf("%w: adapter origin is already configured or pending", domain.ErrConflict)
	}

	if _, err := db.adapterMoveQueries().deleteClaims(ctx, moveID, chain.Org, chain.Project); err != nil {
		return AdapterMove{}, err
	}

	if rows, err := db.adapterMoveQueries().updatePendingOrigin(ctx, origin, pendingCredential, moveID, chain.Org, chain.Project); err != nil || rows != 1 {
		return AdapterMove{}, errors.Join(err, adapter.ErrSuperseded)
	}

	if _, err := db.adapterMoveQueries().resetDestinations(ctx, moveID, chain.Org, chain.Project); err != nil {
		return AdapterMove{}, err
	}
	for _, target := range move.Targets {

		rows, err := db.adapterMoveQueries().pendingKeyIDs(ctx, moveID, target.TargetID, chain.Org, chain.Project, target.EnvironmentID)
		if err != nil {
			return AdapterMove{}, err
		}
		var keyIDs []string
		for _, keyQueryRow := range rows {
			var keyID string
			keyID = keyQueryRow
			keyIDs = append(keyIDs, keyID)
		}
		if err := reserveAdapterMoveClaims(ctx, db, chain, moveID, origin, AdapterTargetMutation{
			ID: target.TargetID, AdapterID: move.AdapterID, EnvironmentID: target.EnvironmentID,
			DestinationKind: target.DestinationKind, DestinationOwner: target.DestinationOwner,
			DestinationName: target.DestinationName, DestinationEnvironment: target.DestinationEnvironment, DestinationScope: target.DestinationScope,
			RepositoryID: target.RepositoryID, Visibility: target.Visibility, SelectedRepositoryIDs: target.SelectedRepositoryIDs,
			NamePrefix: target.NamePrefix, KeyIDs: keyIDs,
		}); err != nil {
			return AdapterMove{}, err
		}
	}
	move.PendingOrigin = origin
	if err := resumeAdapterMove(ctx, db, chain, move, authorityPrincipalID, at); err != nil {
		return AdapterMove{}, err
	}
	out, err := readAdapterMove(ctx, db, chain, moveID, false)
	if err != nil {
		return AdapterMove{}, err
	}
	out.PreviousAuthorityPrincipalID = previousAuthority
	return out, nil
}

func resumeAdapterMove(ctx context.Context, db adapterDB, chain domain.Scope, move AdapterMove, authorityPrincipalID string, at time.Time) error {
	stamp := at

	if rows, err := db.adapterMoveQueries().activate(ctx, authorityPrincipalID, move.ID, chain.Org, chain.Project); err != nil || rows != 1 {
		return errors.Join(err, adapter.ErrSuperseded)
	}
	for _, target := range move.Targets {
		var generation int64

		lookupResult, err := db.adapterMoveQueries().resumeTarget(ctx, target.TargetID, chain.Org, chain.Project, target.EnvironmentID)
		generation = lookupResult
		if err != nil {
			return adapter.ErrSuperseded
		}
		jobID := newAdapterID("job")
		nextGeneration := generation + 1

		if rows, err := db.adapterMoveQueries().insertActivateJob(ctx, jobID, chain.Org, chain.Project, target.EnvironmentID, target.TargetID, move.ID, authorityPrincipalID, nextGeneration, target.TargetID, stamp, stamp); err != nil || rows != 1 {
			return errors.Join(err, ErrConflict)
		}

		if rows, err := db.adapterMoveQueries().markResumingTarget(ctx, nextGeneration, jobID, target.TargetID, chain.Org, chain.Project, target.EnvironmentID, generation); err != nil || rows != 1 {
			return errors.Join(err, adapter.ErrSuperseded)
		}
	}

	if rows, err := db.adapterMoveQueries().updateAuthority(ctx, authorityPrincipalID, move.AdapterID, chain.Org, chain.Project); err != nil || rows != 1 {
		return errors.Join(err, adapter.ErrSuperseded)
	}
	return nil
}

func beginAdapterOriginMove(ctx context.Context, db adapterDB, chain domain.Scope, mutation AdapterOriginMoveMutation) (AdapterRouteMoveBatch, error) {
	if mutation.AdapterID == "" || mutation.Origin == "" || len(mutation.PendingCredentialCiphertext) == 0 || mutation.AuthorityPrincipalID == "" || mutation.At.IsZero() {
		return AdapterRouteMoveBatch{}, fmt.Errorf("%w: origin move requires adapter, origin, sealed credential, authority, and timestamp", domain.ErrInvalid)
	}
	if mutation.MoveID == "" {
		mutation.MoveID = newAdapterID("arm")
	}
	stamp := mutation.At

	lookupAdapterResult, err := db.adapterMoveQueries().beginOriginAdapter(ctx, stamp, mutation.AdapterID, chain.Org, chain.Project)
	if isNoRows(err) {
		return AdapterRouteMoveBatch{}, ErrNotFound
	}
	if err != nil {
		return AdapterRouteMoveBatch{}, err
	}
	if lookupAdapterResult.ProviderBusy != 0 {
		return AdapterRouteMoveBatch{}, adapter.ErrProviderBusy
	}
	if lookupAdapterResult.CurrentOrigin == mutation.Origin {
		return AdapterRouteMoveBatch{}, fmt.Errorf("%w: adapter origin is unchanged", domain.ErrInvalid)
	}
	var collision int

	collisionQueryResult, err := db.adapterMoveQueries().beginOriginCollisions(ctx, chain.Org, chain.Project, mutation.AdapterID, mutation.Origin, chain.Org, chain.Project, mutation.Origin)
	collision = collisionQueryResult
	if err != nil {
		return AdapterRouteMoveBatch{}, err
	}
	if collision != 0 {
		return AdapterRouteMoveBatch{}, fmt.Errorf("%w: adapter origin is already configured or pending", domain.ErrConflict)
	}
	type originTarget struct {
		id, environmentID, kind, owner, name, destinationEnvironment, destinationScope, visibility, prefix, activeJob string
		destinationID, repositoryID, generation                                                                       int64
		selectedRepositoryIDs                                                                                         []int64
		orphaned                                                                                                      []string
	}

	rows, err := db.adapterMoveQueries().beginOriginTargets(ctx, mutation.AdapterID, chain.Org, chain.Project)
	if err != nil {
		return AdapterRouteMoveBatch{}, err
	}
	var targets []originTarget
	for _, targetQueryRow := range rows {
		target := originTarget{id: targetQueryRow.Id, environmentID: targetQueryRow.EnvironmentID, kind: targetQueryRow.Kind, owner: targetQueryRow.Owner, name: targetQueryRow.Name, destinationEnvironment: targetQueryRow.DestinationEnvironment, destinationScope: targetQueryRow.DestinationScope, destinationID: targetQueryRow.DestinationID, repositoryID: targetQueryRow.RepositoryID, visibility: targetQueryRow.Visibility, prefix: targetQueryRow.Prefix, generation: targetQueryRow.Generation, activeJob: targetQueryRow.ActiveJob}
		selectedRaw := targetQueryRow.SelectedRaw
		orphanRaw := targetQueryRow.OrphanRaw
		if err := json.Unmarshal(orphanRaw, &target.orphaned); err != nil {
			return AdapterRouteMoveBatch{}, err
		}
		if err := json.Unmarshal(selectedRaw, &target.selectedRepositoryIDs); err != nil {
			return AdapterRouteMoveBatch{}, err
		}
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		return AdapterRouteMoveBatch{}, fmt.Errorf("%w: origin move requires at least one active target", domain.ErrInvalid)
	}
	moveState, jobKind := "scrubbing", "scrub"
	if mutation.KeepRemote {
		moveState, jobKind = "activating", "activate"
	}

	if affected, err := db.adapterMoveQueries().insertOrigin(ctx, mutation.MoveID, chain.Org, chain.Project, mutation.AdapterID, mutation.Origin, mutation.PendingCredentialCiphertext, mutation.AuthorityPrincipalID, moveState, mutation.KeepRemote, stamp); err != nil || affected != 1 {
		if err != nil {
			return AdapterRouteMoveBatch{}, err
		}
		return AdapterRouteMoveBatch{}, ErrConflict
	}
	batch := AdapterRouteMoveBatch{MoveID: mutation.MoveID}
	for _, target := range targets {
		pendingOrphans := []string{}
		if mutation.KeepRemote {
			pendingOrphans = target.orphaned
		}
		orphanJSON, _ := json.Marshal(pendingOrphans)
		selectedJSON, _ := json.Marshal(target.selectedRepositoryIDs)

		if affected, err := db.adapterMoveQueries().insertTarget(ctx, mutation.MoveID, chain.Org, chain.Project, target.environmentID, target.id, target.kind, target.owner, target.name, target.destinationEnvironment, target.destinationScope, target.repositoryID, target.visibility, selectedJSON, target.prefix, string(orphanJSON)); err != nil || affected != 1 {
			if err != nil {
				return AdapterRouteMoveBatch{}, err
			}
			return AdapterRouteMoveBatch{}, ErrConflict
		}

		keyRows, err := db.adapterMoveQueries().targetKeyIDs(ctx, target.id, chain.Org, chain.Project, target.environmentID)
		if err != nil {
			return AdapterRouteMoveBatch{}, err
		}
		var keyIDs []string
		for _, keyQueryRow := range keyRows {
			var keyID string
			keyID = keyQueryRow
			keyIDs = append(keyIDs, keyID)
		}
		if len(keyIDs) == 0 {
			return AdapterRouteMoveBatch{}, fmt.Errorf("%w: adapter target has no keys", domain.ErrInvalid)
		}

		for _, keyID := range keyIDs {
			if _, err := db.adapterMoveQueries().insertKey(ctx, mutation.MoveID, chain.Org, chain.Project, target.environmentID, target.id, keyID); err != nil {
				return AdapterRouteMoveBatch{}, err
			}
		}
		if err := reserveAdapterMoveClaims(ctx, db, chain, mutation.MoveID, mutation.Origin, AdapterTargetMutation{
			ID: target.id, AdapterID: mutation.AdapterID, EnvironmentID: target.environmentID,
			DestinationKind: target.kind, DestinationOwner: target.owner, DestinationName: target.name,
			DestinationEnvironment: target.destinationEnvironment, DestinationScope: target.destinationScope, RepositoryID: target.repositoryID,
			Visibility: target.visibility, SelectedRepositoryIDs: target.selectedRepositoryIDs,
			NamePrefix: target.prefix, KeyIDs: keyIDs,
		}); err != nil {
			return AdapterRouteMoveBatch{}, err
		}
		if target.activeJob != "" {

			if affected, err := db.adapterMoveQueries().supersedeJob(ctx, stamp, target.activeJob, target.id, chain.Org, chain.Project, target.environmentID); err != nil || affected != 1 {
				if err != nil {
					return AdapterRouteMoveBatch{}, err
				}
				return AdapterRouteMoveBatch{}, adapter.ErrSuperseded
			}
		}
		if mutation.KeepRemote {

			if _, err := db.adapterMoveQueries().releaseLedger(ctx, stamp, target.id, chain.Org, chain.Project, target.environmentID); err != nil {
				return AdapterRouteMoveBatch{}, err
			}
		}
		jobID := newAdapterID("job")
		generation := target.generation + 1

		if _, err := db.adapterMoveQueries().insertJob(ctx, jobID, chain.Org, chain.Project, target.environmentID, target.id, jobKind, mutation.MoveID, mutation.AuthorityPrincipalID, generation, target.id, stamp, stamp); err != nil {
			return AdapterRouteMoveBatch{}, err
		}

		if affected, err := db.adapterMoveQueries().markMovingTarget(ctx, generation, jobID, target.id, chain.Org, chain.Project, target.environmentID, target.generation); err != nil || affected != 1 {
			if err != nil {
				return AdapterRouteMoveBatch{}, err
			}
			return AdapterRouteMoveBatch{}, adapter.ErrProviderBusy
		}
		result := AdapterRouteMoveResult{MoveID: mutation.MoveID, TargetID: target.id, JobID: jobID, SupersededJobID: target.activeJob, Generation: generation}
		if mutation.KeepRemote {
			result.Orphaned = append([]string(nil), target.orphaned...)
			batch.Orphaned = append(batch.Orphaned, target.orphaned...)
		}
		batch.Targets = append(batch.Targets, result)
	}

	if affected, err := db.adapterMoveQueries().markAdapterMoving(ctx, mutation.AuthorityPrincipalID, mutation.AdapterID, chain.Org, chain.Project); err != nil || affected != 1 {
		if err != nil {
			return AdapterRouteMoveBatch{}, err
		}
		return AdapterRouteMoveBatch{}, ErrNotFound
	}
	return batch, nil
}

func validatePendingTarget(m AdapterTargetMutation) error {
	if m.ID == "" || m.AdapterID == "" || m.EnvironmentID == "" || m.DestinationOwner == "" || len(m.KeyIDs) == 0 {
		return fmt.Errorf("%w: pending adapter target requires ids, environment, destination, and keys", domain.ErrInvalid)
	}
	switch m.DestinationKind {
	case string(adapter.Repository):
		if m.DestinationName == "" || m.DestinationEnvironment != "" || m.Visibility != "" || len(m.SelectedRepositoryIDs) != 0 {
			return fmt.Errorf("%w: repository target requires repository name", domain.ErrInvalid)
		}
	case string(adapter.Organization):
		if m.DestinationName != "" || m.DestinationEnvironment != "" {
			return fmt.Errorf("%w: organization target does not take repository name", domain.ErrInvalid)
		}
		if m.Visibility == "selected" && len(m.SelectedRepositoryIDs) == 0 {
			return fmt.Errorf("%w: selected visibility requires repository ids", domain.ErrInvalid)
		}
		if m.Visibility != "" && m.Visibility != "all" && m.Visibility != "private" && m.Visibility != "selected" {
			return fmt.Errorf("%w: invalid organization visibility", domain.ErrInvalid)
		}
	case string(adapter.Environment):
		if m.DestinationName == "" || m.DestinationEnvironment == "" || m.Visibility != "" || len(m.SelectedRepositoryIDs) != 0 {
			return fmt.Errorf("%w: environment target requires repository and environment", domain.ErrInvalid)
		}
	case string(adapter.JSONObject), string(adapter.PerKey):
		if err := adapter.ValidateAWSSecretsManagerDestination(targetDestination(m)); err != nil {
			return fmt.Errorf("%w: %v", domain.ErrInvalid, err)
		}
	case string(adapter.WorkersScript), string(adapter.PagesProject):
		if err := validateCloudflareTarget(m); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: unsupported adapter destination kind", domain.ErrInvalid)
	}
	seen := make(map[string]bool, len(m.KeyIDs))
	for _, keyID := range m.KeyIDs {
		if keyID == "" || seen[keyID] {
			return fmt.Errorf("%w: adapter target key ids must be non-empty and unique", domain.ErrInvalid)
		}
		seen[keyID] = true
	}
	return nil
}

func beginAdapterTargetMove(ctx context.Context, db adapterDB, chain domain.Scope, mutation AdapterRouteMoveMutation) (AdapterRouteMoveResult, error) {
	if err := validatePendingTarget(mutation.Target); err != nil {
		return AdapterRouteMoveResult{}, err
	}
	if mutation.ExpectedGeneration <= 0 || mutation.AuthorityPrincipalID == "" || mutation.At.IsZero() {
		return AdapterRouteMoveResult{}, fmt.Errorf("%w: target move requires generation, authority, and timestamp", domain.ErrInvalid)
	}
	if mutation.MoveID == "" {
		mutation.MoveID = newAdapterID("arm")
	}
	stamp := mutation.At

	lookupResult, err := db.adapterMoveQueries().beginTarget(ctx, stamp, mutation.Target.ID, chain.Org, chain.Project)
	if isNoRows(err) {
		return AdapterRouteMoveResult{}, ErrNotFound
	}
	if err != nil {
		return AdapterRouteMoveResult{}, err
	}
	if lookupResult.ProviderBusy != 0 {
		return AdapterRouteMoveResult{}, adapter.ErrProviderBusy
	}
	if lookupResult.Generation != mutation.ExpectedGeneration {
		return AdapterRouteMoveResult{}, adapter.ErrSuperseded
	}
	if lookupResult.AdapterID != mutation.Target.AdapterID {
		return AdapterRouteMoveResult{}, fmt.Errorf("%w: target does not belong to adapter", domain.ErrConflict)
	}
	if lookupResult.EnvironmentID != mutation.Target.EnvironmentID {
		return AdapterRouteMoveResult{}, fmt.Errorf("%w: moving a target between environments requires a replacement target identity", domain.ErrConflict)
	}
	if err := requireUnchangedMoveFlags(ctx, db, chain, mutation.Target); err != nil {
		return AdapterRouteMoveResult{}, err
	}
	if lookupResult.DestinationScope != mutation.Target.DestinationScope {
		return AdapterRouteMoveResult{}, fmt.Errorf("%w: a GitLab environment scope is immutable; remove the target and add a new one", domain.ErrConflict)
	}
	if lookupResult.Kind == mutation.Target.DestinationKind && lookupResult.Owner == mutation.Target.DestinationOwner && lookupResult.Name == mutation.Target.DestinationName && lookupResult.DestinationEnvironment == mutation.Target.DestinationEnvironment {
		return AdapterRouteMoveResult{}, fmt.Errorf("%w: target update does not move its route", domain.ErrInvalid)
	}
	var orphaned []string
	if err := json.Unmarshal(lookupResult.OrphanRaw, &orphaned); err != nil {
		return AdapterRouteMoveResult{}, fmt.Errorf("store: adapter move orphan list: %w", err)
	}
	moveState, jobKind := "scrubbing", "scrub"
	if mutation.KeepRemote {
		moveState, jobKind = "activating", "activate"
	}

	if rows, err := db.adapterMoveQueries().insertTargetMove(ctx, mutation.MoveID, chain.Org, chain.Project, lookupResult.AdapterID, mutation.Target.ID, mutation.AuthorityPrincipalID, moveState, mutation.KeepRemote, stamp); err != nil || rows != 1 {
		if err != nil {
			return AdapterRouteMoveResult{}, err
		}
		return AdapterRouteMoveResult{}, ErrConflict
	}
	pendingOrphans := []string{}
	if mutation.KeepRemote {
		pendingOrphans = orphaned
	}
	pendingOrphanJSON, _ := json.Marshal(pendingOrphans)
	selectedJSON, _ := json.Marshal(mutation.Target.SelectedRepositoryIDs)

	if rows, err := db.adapterMoveQueries().insertTarget(ctx, mutation.MoveID, chain.Org, chain.Project, mutation.Target.EnvironmentID, mutation.Target.ID, mutation.Target.DestinationKind, mutation.Target.DestinationOwner, mutation.Target.DestinationName, mutation.Target.DestinationEnvironment, mutation.Target.DestinationScope, mutation.Target.RepositoryID, mutation.Target.Visibility, selectedJSON, mutation.Target.NamePrefix, string(pendingOrphanJSON)); err != nil || rows != 1 {
		if err != nil {
			return AdapterRouteMoveResult{}, err
		}
		return AdapterRouteMoveResult{}, ErrConflict
	}
	for _, keyID := range mutation.Target.KeyIDs {

		if rows, err := db.adapterMoveQueries().insertKey(ctx, mutation.MoveID, chain.Org, chain.Project, mutation.Target.EnvironmentID, mutation.Target.ID, keyID); err != nil || rows != 1 {
			if err != nil {
				return AdapterRouteMoveResult{}, err
			}
			return AdapterRouteMoveResult{}, ErrConflict
		}
	}
	if err := reserveAdapterMoveClaims(ctx, db, chain, mutation.MoveID, lookupResult.Origin, mutation.Target); err != nil {
		return AdapterRouteMoveResult{}, err
	}
	if lookupResult.ActiveJob != "" {

		if rows, err := db.adapterMoveQueries().supersedeJob(ctx, stamp, lookupResult.ActiveJob, mutation.Target.ID, chain.Org, chain.Project, lookupResult.EnvironmentID); err != nil || rows != 1 {
			if err != nil {
				return AdapterRouteMoveResult{}, err
			}
			return AdapterRouteMoveResult{}, adapter.ErrSuperseded
		}
	}
	if mutation.KeepRemote {

		if _, err := db.adapterMoveQueries().releaseLedger(ctx, stamp, mutation.Target.ID, chain.Org, chain.Project, lookupResult.EnvironmentID); err != nil {
			return AdapterRouteMoveResult{}, err
		}
	}
	jobID := newAdapterID("job")
	generation := lookupResult.Generation + 1

	if rows, err := db.adapterMoveQueries().insertJob(ctx, jobID, chain.Org, chain.Project, lookupResult.EnvironmentID, mutation.Target.ID, jobKind, mutation.MoveID, mutation.AuthorityPrincipalID, generation, mutation.Target.ID, stamp, stamp); err != nil || rows != 1 {
		if err != nil {
			return AdapterRouteMoveResult{}, err
		}
		return AdapterRouteMoveResult{}, ErrConflict
	}

	if rows, err := db.adapterMoveQueries().markMovingTarget(ctx, generation, jobID, mutation.Target.ID, chain.Org, chain.Project, lookupResult.EnvironmentID, lookupResult.Generation); err != nil || rows != 1 {
		if err != nil {
			return AdapterRouteMoveResult{}, err
		}
		return AdapterRouteMoveResult{}, adapter.ErrProviderBusy
	}

	if rows, err := db.adapterMoveQueries().setActiveAuthority(ctx, mutation.AuthorityPrincipalID, lookupResult.AdapterID, chain.Org, chain.Project); err != nil || rows != 1 {
		if err != nil {
			return AdapterRouteMoveResult{}, err
		}
		return AdapterRouteMoveResult{}, ErrNotFound
	}
	result := AdapterRouteMoveResult{MoveID: mutation.MoveID, TargetID: mutation.Target.ID, JobID: jobID, SupersededJobID: lookupResult.ActiveJob, Generation: generation}
	if mutation.KeepRemote {
		result.Orphaned = orphaned
	}
	return result, nil
}

func reserveAdapterMoveClaims(ctx context.Context, db adapterDB, chain domain.Scope, moveID, origin string, target AdapterTargetMutation) error {
	provider, err := adapterProvider(ctx, db, chain, target.AdapterID)
	if err != nil {
		return err
	}
	if provider == string(adapter.AWSSecretsManagerProvider) || isAWSDestinationKind(target.DestinationKind) {
		return reserveAWSMoveClaims(ctx, db, chain, moveID, origin, provider, target)
	}
	keys, err := db.adapterStoreQueries().manifestKeys(ctx, chain, target.KeyIDs)
	if err != nil {
		return err
	}
	type claim struct{ keyID, surface, effective string }
	claims := []claim{{surface: string(adapter.Secret), effective: target.NamePrefix + adapter.SentinelName}, {surface: string(adapter.Variable), effective: target.NamePrefix + adapter.SentinelName}}
	for _, key := range keys {
		keyID, name, classification := key.KeyID, key.CanonicalName, string(key.Classification)
		surface := adapter.Secret
		if adapter.Classification(classification) == adapter.ConfigClassification && provider != string(adapter.CloudflareProvider) && provider != string(adapter.VaultKVProvider) {
			surface = adapter.Variable
		}
		claims = append(claims, claim{keyID: keyID, surface: string(surface), effective: target.NamePrefix + name})
	}

	if len(claims) != len(target.KeyIDs)+2 {
		return ErrNotFound
	}
	for _, pending := range claims {
		var configured int

		configuredCollisionResult, err := db.adapterMoveQueries().configuredCollision(ctx, chain.Org, chain.Project, target.ID, origin, target.DestinationKind, target.DestinationOwner, target.DestinationName, target.DestinationEnvironment, target.DestinationScope, pending.effective, adapter.SentinelName, pending.surface, pending.effective)
		configured = configuredCollisionResult
		if err != nil {
			return err
		}
		if configured != 0 {
			return fmt.Errorf("%w: effective name %q is already configured on the pending destination", domain.ErrConflict, pending.effective)
		}

		var keyID string
		if pending.keyID != "" {
			keyID = pending.keyID
		}
		if _, err := db.adapterMoveQueries().insertClaim(ctx, moveID, chain.Org, chain.Project, target.EnvironmentID, target.ID, keyID, origin, target.DestinationKind, target.DestinationOwner, target.DestinationName, target.DestinationEnvironment, target.DestinationScope, pending.surface, pending.effective, strings.ToUpper(pending.effective)); err != nil {
			if constraint(err) != nil {
				return fmt.Errorf("%w: pending effective name %q is already claimed", domain.ErrConflict, pending.effective)
			}
			return err
		}
	}
	return nil
}

// reserveAWSMoveClaims reserves the pending route's AWS secret names. The
// pending route may name a different account or region, so the configured
// check runs against the pending origin and account, not the current ones.
func reserveAWSMoveClaims(ctx context.Context, db adapterDB, chain domain.Scope, moveID, origin, provider string, target AdapterTargetMutation) error {
	_, manifest, err := targetProviderManifest(ctx, db, chain, target)
	if err != nil {
		return err
	}
	if err := adapter.ValidateTargetManifest(provider, targetDestination(target), target.NamePrefix, manifest, false); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	claims := adapter.ClaimedNames(provider, targetDestination(target), target.NamePrefix, manifest)
	desired := make(map[string]bool, len(claims))
	for _, claim := range claims {
		desired[strings.ToUpper(claim.EffectiveName)] = true
	}

	rows, err := db.adapterMoveQueries().aWSConfiguredNames(ctx, chain.Org, chain.Project, target.ID, origin, target.DestinationOwner)
	if err != nil {
		return err
	}
	for _, configuredRow := range rows {
		claimed := configuredRow.Name
		if configuredRow.Kind == string(adapter.PerKey) {
			if configuredRow.KeyName == "" {
				continue
			}
			claimed = configuredRow.Name + configuredRow.Prefix + configuredRow.KeyName
		}
		if desired[strings.ToUpper(claimed)] {
			return fmt.Errorf("%w: effective name %q is already configured on the pending destination", domain.ErrConflict, claimed)
		}
	}

	pendingRows, err := db.adapterMoveQueries().aWSPendingNames(ctx, chain.Org, chain.Project, origin, target.DestinationOwner, target.ID)
	if err != nil {
		return err
	}
	for _, pendingRow := range pendingRows {
		if desired[strings.ToUpper(pendingRow.Effective)] {
			return fmt.Errorf("%w: effective name %q is reserved by pending target %q on this destination", domain.ErrConflict, pendingRow.Effective, pendingRow.OtherTarget)
		}
	}
	for _, claim := range claims {

		var keyID string
		if claim.KeyID != "" {
			keyID = claim.KeyID
		}
		if _, err := db.adapterMoveQueries().insertAWSClaim(ctx, moveID, chain.Org, chain.Project, target.EnvironmentID, target.ID, keyID, origin, target.DestinationKind, target.DestinationOwner, target.DestinationName, target.DestinationEnvironment, string(claim.Surface), claim.EffectiveName, strings.ToUpper(claim.EffectiveName)); err != nil {
			if constraint(err) != nil {
				return fmt.Errorf("%w: pending effective name %q is already claimed", domain.ErrConflict, claim.EffectiveName)
			}
			return err
		}
	}
	return nil
}

// Move storage preserves flags at activation, so accepting changed flags here
// would promise state that cannot be committed. Check both creation and resume.
func requireUnchangedMoveFlags(ctx context.Context, db adapterDB, chain domain.Scope, target AdapterTargetMutation) error {

	queryResult, err := db.adapterMoveQueries().flags(ctx, chain.Org, chain.Project, target.AdapterID, target.ID)
	if err != nil {
		return err
	}
	if queryResult.Provider == string(adapter.GitLabProvider) && (queryResult.Protected != target.VariableProtected || queryResult.Hidden != target.VariableHidden || queryResult.Expand != target.VariableExpand) {
		return fmt.Errorf("%w: update variable flags separately before or after moving the destination", domain.ErrInvalid)
	}
	return nil
}
