package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// All engine access below this interface remains inside the repository. The
// state machine is shared so SQLite and PostgreSQL cannot drift on activation.
type selfConfigStorage interface {
	currentTime(context.Context) (time.Time, error)
	previousRevision(context.Context) (int64, error)
	completedGeneration(context.Context, int64) (int64, error)
	rollout(context.Context, string) (SelfConfigRollout, error)
	nextRolloutSequence(context.Context, string) (int64, error)
	putRollout(context.Context, SelfConfigRollout) error
	seedInputs(context.Context) ([]SelfConfigSeedInput, error)
	putSeedInput(context.Context, SelfConfigSeedInput) error
	clearSeedInputs(context.Context) error
	recover(context.Context, int64, int64, string, time.Time) error
	lockMembership(context.Context) error
	seedDisagreement(context.Context, SelfConfigBinding) (int64, error)
	binding(context.Context, bool) (SelfConfigBinding, error)
	jobs(context.Context) ([]SelfConfigJob, error)
	job(context.Context, string, bool) (SelfConfigJob, error)
	nodes(context.Context) ([]SelfConfigNode, error)
	topology(context.Context) (string, error)
	fenceTopologyLease(context.Context, time.Time) error
	insertBinding(context.Context, SelfConfigBinding) error
	insertJob(context.Context, SelfConfigJob) error
	updateJob(context.Context, SelfConfigJob, string) error
	commit(context.Context, SelfConfigJob, time.Time) error
	previous(context.Context, string) error
	fence(context.Context, string, time.Time) (int64, error)
	putNode(context.Context, SelfConfigNode) error
	deleteNodes(context.Context) error
	retained(context.Context) ([]string, error)
	retentionSlot(context.Context, string) (string, error)
	retain(context.Context, string, string) error
	release(context.Context, string) error
	participants(context.Context, time.Time) ([]string, error)
	recent(context.Context, time.Time) (int64, error)
	open(context.Context) (int64, error)
	lockSnapshot(context.Context, SelfConfigBinding, string) error
}

type selfConfigRepo struct {
	q   selfConfigStorage
	tok *authz.TxToken
}

func (r selfConfigRepo) verify(ctx context.Context, p authz.Proof, op authz.StoreOp, lock bool) (SelfConfigBinding, error) {
	chain, err := authz.Verify(p, op, r.tok)
	if err != nil {
		return SelfConfigBinding{}, err
	}
	b, err := r.q.binding(ctx, lock)
	if err != nil {
		return SelfConfigBinding{}, err
	}
	if chain.Org != "" && (string(chain.Org) != b.OrgID || string(chain.Project) != b.ProjectID || string(chain.Env) != b.EnvironmentID) {
		return SelfConfigBinding{}, domain.ErrNotFound
	}
	return b, nil
}
func (r selfConfigRepo) Binding(ctx context.Context, p authz.Proof) (SelfConfigBinding, error) {
	return r.verify(ctx, p, authz.StoreSelfConfigBinding, false)
}
func (r selfConfigRepo) Jobs(ctx context.Context, p authz.Proof) ([]SelfConfigJob, error) {
	if _, e := r.verify(ctx, p, authz.StoreSelfConfigJobs, false); e != nil {
		return nil, e
	}
	return r.q.jobs(ctx)
}
func (r selfConfigRepo) Job(ctx context.Context, p authz.Proof, id string) (SelfConfigJob, error) {
	if _, e := r.verify(ctx, p, authz.StoreSelfConfigJob, false); e != nil {
		return SelfConfigJob{}, e
	}
	return r.q.job(ctx, id, false)
}
func (r selfConfigRepo) JobByIdempotencyKey(ctx context.Context, p authz.Proof, key string) (SelfConfigJob, error) {
	if _, err := r.verify(ctx, p, authz.StoreSelfConfigJobByIdempotencyKey, false); err != nil {
		return SelfConfigJob{}, err
	}
	return r.q.job(ctx, key, true)
}
func (r selfConfigRepo) Nodes(ctx context.Context, p authz.Proof) ([]SelfConfigNode, error) {
	if _, e := r.verify(ctx, p, authz.StoreSelfConfigNodes, false); e != nil {
		return nil, e
	}
	return r.q.nodes(ctx)
}
func (r selfConfigRepo) Retained(ctx context.Context, p authz.Proof) ([]string, error) {
	if _, e := r.verify(ctx, p, authz.StoreSelfConfigRetained, false); e != nil {
		return nil, e
	}
	ids, e := r.q.retained(ctx)
	slices.Sort(ids)
	return slices.Compact(ids), e
}

func (r selfConfigRepo) CreateBinding(ctx context.Context, p authz.Proof, b SelfConfigBinding) error {
	if _, e := authz.Verify(p, authz.StoreSelfConfigCreateBinding, r.tok); e != nil {
		return e
	}
	if b.AdoptionKey == "" || b.AdoptedBy == "" || len(b.AdoptionKey) > 128 || b.OwnerInstanceID == "" || b.OrgID == "" || b.ProjectID == "" || b.EnvironmentID == "" || b.DesiredSnapshotID == "" || b.Incarnation == "" || b.SchemaVersion < 1 || b.CreatedAt.IsZero() {
		return domain.ErrInvalid
	}
	if err := r.q.lockMembership(ctx); err != nil {
		return err
	}
	mismatches, err := r.q.seedDisagreement(ctx, b)
	if err != nil {
		return err
	}
	if mismatches != 0 {
		return ErrSelfConfigSeedDisagreement
	}
	if len(b.SeedNodes) != 0 {
		if err := r.verifySeedReferences(ctx, b); err != nil {
			return err
		}
	}
	if err := r.q.lockSnapshot(ctx, b, b.DesiredSnapshotID); err != nil {
		return err
	}
	if err := r.q.insertBinding(ctx, b); err != nil {
		return err
	}
	if err := r.q.retain(ctx, "desired", b.DesiredSnapshotID); err != nil {
		return err
	}
	return r.q.clearSeedInputs(ctx)
}

func (r selfConfigRepo) BeginJob(ctx context.Context, p authz.Proof, want SelfConfigJob) (SelfConfigJob, error) {
	b, err := r.verify(ctx, p, authz.StoreSelfConfigBeginJob, true)
	if err != nil {
		return SelfConfigJob{}, err
	}
	if want.ID == "" || want.IdempotencyKey == "" || len(want.IdempotencyKey) > 128 || want.PrincipalID == "" || want.SnapshotID == "" || want.Revision < 1 || want.SchemaVersion < 1 || want.ExpectedGeneration < 1 || want.CreatedAt.IsZero() {
		return SelfConfigJob{}, domain.ErrInvalid
	}
	old, err := r.q.job(ctx, want.IdempotencyKey, true)
	if err == nil {
		if old.ConfirmRestoredCredentials != want.ConfirmRestoredCredentials || old.PrincipalID != want.PrincipalID || old.SnapshotID != want.SnapshotID || old.Revision != want.Revision || old.SchemaVersion != want.SchemaVersion || old.ExpectedGeneration != want.ExpectedGeneration {
			return SelfConfigJob{}, domain.ErrConflict
		}
		return old, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return SelfConfigJob{}, err
	}
	if b.Generation != want.ExpectedGeneration || b.SchemaVersion != want.SchemaVersion {
		return SelfConfigJob{}, domain.ErrConflict
	}
	n, err := r.q.open(ctx)
	if err != nil {
		return SelfConfigJob{}, err
	}
	if n != 0 {
		// A partial activation may be repaired by a newly reviewed revision.
		// Superseding its job does not change the durable target: only the new
		// job's final exact-MFA commit can do that. Retention remains bounded by
		// desired, previous and the single replacement candidate.
		jobs, err := r.q.jobs(ctx)
		if err != nil {
			return SelfConfigJob{}, err
		}
		var failed *SelfConfigJob
		for _, job := range jobs {
			if job.Status == "preparing" || job.Status == "applying" {
				return SelfConfigJob{}, fmt.Errorf("%w: another configuration apply is unresolved", domain.ErrConflict)
			}
			if job.Status == "partial" && job.Generation == b.Generation {
				rollout, err := r.q.rollout(ctx, job.ID)
				if err != nil && !errors.Is(err, domain.ErrNotFound) {
					return SelfConfigJob{}, err
				}
				if err == nil && rollout.ExternalPhase == "" {
					return SelfConfigJob{}, domain.ErrConflict
				}
				copy := job
				failed = &copy
			}
		}
		if failed == nil {
			return SelfConfigJob{}, domain.ErrConflict
		}
		failed.Status, failed.ErrorCode, failed.UpdatedAt = "superseded", "superseded", want.CreatedAt
		if err := r.q.updateJob(ctx, *failed, "partial"); err != nil {
			return SelfConfigJob{}, err
		}
	}
	n, err = r.q.recent(ctx, want.CreatedAt.Add(-time.Minute))
	if err != nil {
		return SelfConfigJob{}, err
	}
	if n >= 10 {
		return SelfConfigJob{}, fmt.Errorf("%w: configuration apply rate exceeded", domain.ErrConflict)
	}
	if err := r.q.lockSnapshot(ctx, b, want.SnapshotID); err != nil {
		return SelfConfigJob{}, err
	}
	if err := r.q.insertJob(ctx, want); err != nil {
		return SelfConfigJob{}, err
	}
	// The candidate slot is set under the same snapshot lock GC uses. No fourth
	// payload can appear even if the request crashes after preparation starts.
	if err := r.q.retain(ctx, "candidate", want.SnapshotID); err != nil {
		return SelfConfigJob{}, err
	}
	if err := r.q.lockMembership(ctx); err != nil {
		return SelfConfigJob{}, err
	}
	participants, err := r.q.participants(ctx, want.CreatedAt.Add(-30*time.Second))
	if err != nil {
		return SelfConfigJob{}, err
	}
	if raw, topologyErr := r.q.topology(ctx); topologyErr == nil {
		topology, action, parseErr := rolloutTopology(raw)
		if parseErr != nil {
			return SelfConfigJob{}, parseErr
		}
		if topology != nil {
			assigned := topology.After
			if action == "restore" {
				assigned = topology.Before
			}
			if want.LocalNodeID != assigned.NodeID {
				return SelfConfigJob{}, domain.ErrConflict
			}
			participants = []string{assigned.NodeID}
		}
	} else if !isNoRows(topologyErr) && !errors.Is(topologyErr, domain.ErrNotFound) {
		return SelfConfigJob{}, topologyErr
	}
	if len(participants) == 0 {
		if want.LocalNodeID == "" {
			return SelfConfigJob{}, domain.ErrInvalid
		}
		participants = []string{want.LocalNodeID}
	}
	if err := r.q.deleteNodes(ctx); err != nil {
		return SelfConfigJob{}, err
	}
	for _, id := range participants {
		if err := r.q.putNode(ctx, SelfConfigNode{NodeID: id, JobID: want.ID, Incarnation: b.Incarnation, UpdatedAt: want.CreatedAt}); err != nil {
			return SelfConfigJob{}, err
		}
	}
	return r.q.job(ctx, want.ID, false)
}

func (r selfConfigRepo) CommitJob(ctx context.Context, p authz.Proof, id string, at time.Time) (SelfConfigBinding, error) {
	b, err := r.verify(ctx, p, authz.StoreSelfConfigCommitJob, true)
	if err != nil {
		return SelfConfigBinding{}, err
	}
	j, err := r.q.job(ctx, id, false)
	if err != nil {
		return SelfConfigBinding{}, err
	}
	if j.Status == "applying" || j.Status == "partial" || j.Status == "applied" {
		if b.Generation == j.Generation {
			return b, nil
		}
		return SelfConfigBinding{}, domain.ErrConflict
	}
	at, err = r.q.currentTime(ctx)
	if err != nil {
		return SelfConfigBinding{}, err
	}
	if j.Status != "preparing" || b.Generation != j.ExpectedGeneration || j.CreatedAt.After(at) || at.Sub(j.CreatedAt) >= SelfConfigPreparationTTL {
		return SelfConfigBinding{}, domain.ErrConflict
	}
	nodes, err := r.q.nodes(ctx)
	if err != nil {
		return SelfConfigBinding{}, err
	}
	if len(nodes) == 0 {
		return SelfConfigBinding{}, domain.ErrConflict
	}
	for _, n := range nodes {
		if n.JobID != id || !n.Prepared || n.SchemaVersion != j.SchemaVersion || n.Incarnation != b.Incarnation || n.ErrorCode != "" || n.UpdatedAt.After(at) || at.Sub(n.UpdatedAt) >= 30*time.Second {
			return SelfConfigBinding{}, fmt.Errorf("%w: configuration participants have not prepared", domain.ErrConflict)
		}
	}
	if err := r.q.lockSnapshot(ctx, b, j.SnapshotID); err != nil {
		return SelfConfigBinding{}, err
	}
	// A repair must preserve the last completed target. Its predecessor may
	// have failed installation on every node and is not a recovery reference.
	completed, err := r.q.completedGeneration(ctx, b.Generation)
	if err != nil {
		return SelfConfigBinding{}, err
	}
	if b.Generation == 1 || completed > 0 {
		if err := r.q.retain(ctx, "previous", b.DesiredSnapshotID); err != nil {
			return SelfConfigBinding{}, err
		}
		if err := r.q.previous(ctx, b.DesiredSnapshotID); err != nil {
			return SelfConfigBinding{}, err
		}
	}
	if err := r.q.retain(ctx, "desired", j.SnapshotID); err != nil {
		return SelfConfigBinding{}, err
	}
	if err := r.commitTopologyParticipant(ctx, j, nodes, at); err != nil {
		return SelfConfigBinding{}, err
	}
	if err := r.q.commit(ctx, j, at); err != nil {
		return SelfConfigBinding{}, err
	}
	j.Status = "applying"
	j.UpdatedAt = at
	if err := r.q.updateJob(ctx, j, "preparing"); err != nil {
		return SelfConfigBinding{}, err
	}
	return r.q.binding(ctx, false)
}

func validSelfConfigError(code string) bool {
	return slices.Contains([]string{"", "invalid_config", "incompatible_schema", "preparation_failed", "activation_failed", "preparation_timeout", "convergence_timeout", "restored", "transport_failed", "superseded"}, code)
}

func (r selfConfigRepo) FinishJob(ctx context.Context, p authz.Proof, id, status, code string, at time.Time) error {
	b, err := r.verify(ctx, p, authz.StoreSelfConfigFinishJob, true)
	if err != nil {
		return err
	}
	if !validSelfConfigError(code) || at.IsZero() {
		return domain.ErrInvalid
	}
	j, err := r.q.job(ctx, id, false)
	if err != nil {
		return err
	}
	previous := j.Status
	switch status {
	case "aborted":
		if j.Status != "preparing" {
			return domain.ErrConflict
		}
	case "partial":
		if j.Status != "applying" && j.Status != "partial" {
			return domain.ErrConflict
		}
	case "applied":
		if j.Status != "applying" && j.Status != "partial" {
			return domain.ErrConflict
		}
		nodes, err := r.q.nodes(ctx)
		if err != nil {
			return err
		}
		if len(nodes) == 0 {
			return domain.ErrConflict
		}
		for _, n := range nodes {
			if n.JobID != id || n.ActiveGeneration != j.Generation || n.ActiveRevision != j.Revision || n.Incarnation != b.Incarnation || n.ErrorCode != "" {
				return domain.ErrConflict
			}
		}
	default:
		return domain.ErrInvalid
	}
	j.Status = status
	j.ErrorCode = code
	j.UpdatedAt = at
	if err := r.q.updateJob(ctx, j, previous); err != nil {
		return err
	}
	if status == "applied" || status == "aborted" {
		return r.q.release(ctx, "candidate")
	}
	return nil
}

func (r selfConfigRepo) PutNode(ctx context.Context, p authz.Proof, n SelfConfigNode) error {
	b, err := r.verify(ctx, p, authz.StoreSelfConfigPutNode, true)
	if err != nil {
		return err
	}
	if n.NodeID == "" || n.Incarnation != b.Incarnation || !validSelfConfigError(n.ErrorCode) || n.UpdatedAt.IsZero() {
		return domain.ErrInvalid
	}
	if n.JobID == "" {
		open, err := r.q.open(ctx)
		if err != nil {
			return err
		}
		refused := n.ActiveGeneration == 0 && n.ActiveRevision == 0 && !n.Prepared && (n.ErrorCode == "invalid_config" || n.ErrorCode == "incompatible_schema" || n.ErrorCode == "activation_failed")
		acknowledged := n.ActiveGeneration == b.Generation && n.ActiveRevision == b.DesiredRevision && n.SchemaVersion == b.SchemaVersion && n.ErrorCode == ""
		if open != 0 || (!refused && !acknowledged) {
			return domain.ErrConflict
		}
		admitted, err := r.q.participants(ctx, n.UpdatedAt.Add(-30*time.Second))
		if err != nil {
			return err
		}
		if raw, topologyErr := r.q.topology(ctx); topologyErr == nil {
			topology, action, err := rolloutTopology(raw)
			if err != nil {
				return err
			}
			if topology != nil {
				if (action == "restore" && topology.Before != topology.After) || n.NodeID != topology.After.NodeID {
					return domain.ErrConflict
				}
				admitted = []string{topology.After.NodeID}
			}
		} else if !isNoRows(topologyErr) && !errors.Is(topologyErr, domain.ErrNotFound) {
			return topologyErr
		}
		if len(admitted) > 0 && !slices.Contains(admitted, n.NodeID) {
			return domain.ErrConflict
		}
		return r.q.putNode(ctx, n)
	}
	nodes, err := r.q.nodes(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, existing := range nodes {
		if existing.NodeID == n.NodeID && existing.JobID == n.JobID {
			found = true
		}
	}
	if !found {
		return domain.ErrConflict
	} // late joiners cannot change fixed membership
	j, err := r.q.job(ctx, n.JobID, false)
	if err != nil {
		return err
	}
	if n.Prepared && n.SchemaVersion != j.SchemaVersion {
		return domain.ErrConflict
	}
	if n.ActiveGeneration != 0 && (n.ActiveGeneration != b.Generation || n.ActiveRevision != b.DesiredRevision) {
		return domain.ErrConflict
	}
	return r.q.putNode(ctx, n)
}

func (r selfConfigRepo) FenceRestored(ctx context.Context, p authz.Proof, incarnation string, at time.Time) error {
	if _, err := r.verify(ctx, p, authz.StoreSelfConfigFenceRestored, true); err != nil {
		return err
	}
	if incarnation == "" || at.IsZero() {
		return domain.ErrInvalid
	}
	n, err := r.q.fence(ctx, incarnation, at)
	if err != nil || n == 0 {
		return err
	}
	if err := r.q.deleteNodes(ctx); err != nil {
		return err
	}
	jobs, err := r.q.jobs(ctx)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.Status == "preparing" || j.Status == "applying" || j.Status == "partial" {
			old := j.Status
			j.Status = "aborted"
			j.ErrorCode = "restored"
			j.UpdatedAt = at
			if err := r.q.updateJob(ctx, j, old); err != nil {
				return err
			}
		}
	}
	return r.q.release(ctx, "candidate")
}

// RecoverTarget requires an exact host-local proof and quiesced consumers. It
// never clears restore suspension: credential confirmation remains explicit.
func (r selfConfigRepo) RecoverTarget(ctx context.Context, p authz.Proof, expectedGeneration, revision int64, snapshotID string, at time.Time) (SelfConfigBinding, error) {
	b, err := r.verify(ctx, p, authz.StoreSelfConfigRecoverTarget, true)
	if err != nil {
		return SelfConfigBinding{}, err
	}
	if err := authz.VerifySelfConfigSnapshot(p, snapshotID); err != nil {
		return SelfConfigBinding{}, err
	}
	if expectedGeneration != b.Generation || revision < 1 || at.IsZero() {
		return SelfConfigBinding{}, domain.ErrConflict
	}
	if err := r.q.lockMembership(ctx); err != nil {
		return SelfConfigBinding{}, err
	}
	live, err := r.q.participants(ctx, at.Add(-30*time.Second))
	if err != nil {
		return SelfConfigBinding{}, err
	}
	if len(live) > 0 {
		return SelfConfigBinding{}, fmt.Errorf("%w: recovery requires quiesced HA nodes", domain.ErrConflict)
	}
	nodes, err := r.q.nodes(ctx)
	if err != nil {
		return SelfConfigBinding{}, err
	}
	for _, node := range nodes {
		if !node.UpdatedAt.Before(at.Add(-30 * time.Second)) {
			return SelfConfigBinding{}, fmt.Errorf("%w: recovery requires quiesced runtime consumers", domain.ErrConflict)
		}
	}
	// Quiescence permits the host operator to supersede a job whose invalid
	// desired revision cannot converge. Keep its history, release its candidate
	// below, and advance the selected target in this same transaction.
	jobs, err := r.q.jobs(ctx)
	if err != nil {
		return SelfConfigBinding{}, err
	}
	for _, j := range jobs {
		if j.Status == "preparing" || j.Status == "applying" || j.Status == "partial" {
			old := j.Status
			j.Status = "aborted"
			j.ErrorCode = "recovered"
			j.UpdatedAt = at
			if err := r.q.updateJob(ctx, j, old); err != nil {
				return SelfConfigBinding{}, err
			}
		}
	}
	if err := r.q.lockSnapshot(ctx, b, snapshotID); err != nil {
		return SelfConfigBinding{}, err
	}
	if err := r.q.recover(ctx, expectedGeneration, revision, snapshotID, at); err != nil {
		return SelfConfigBinding{}, err
	}
	if err := r.q.retain(ctx, "previous", b.DesiredSnapshotID); err != nil {
		return SelfConfigBinding{}, err
	}
	if err := r.q.previous(ctx, b.DesiredSnapshotID); err != nil {
		return SelfConfigBinding{}, err
	}
	if err := r.q.retain(ctx, "desired", snapshotID); err != nil {
		return SelfConfigBinding{}, err
	}
	if err := r.q.release(ctx, "candidate"); err != nil {
		return SelfConfigBinding{}, err
	}
	if err := r.q.deleteNodes(ctx); err != nil {
		return SelfConfigBinding{}, err
	}
	return r.q.binding(ctx, false)
}

func (r selfConfigRepo) PreviousRevision(ctx context.Context, p authz.Proof) (int64, error) {
	if _, err := r.verify(ctx, p, authz.StoreSelfConfigPreviousRevision, false); err != nil {
		return 0, err
	}
	return r.q.previousRevision(ctx)
}
