package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

type SelfConfigApplyRequest struct {
	Revision, ExpectedGeneration int64
	SchemaVersion                int
	IdempotencyKey               string
	ConfirmRestoredCredentials   bool
	PrepareOnly                  bool
	PlanDigest                   string
}

func (s *SelfConfig) bindingForActor(ctx context.Context, actor Actor, op authz.Operation) (store.SelfConfigBinding, error) {
	var b store.SelfConfigBinding
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpSelfConfigStatus, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		b, err = r.SelfConfig().Binding(ctx, p)
		if err != nil {
			return err
		}
		_, err = az.Authorize(ctx, caller, op, selfConfigScope(b))
		return err
	})
	return b, err
}

// Apply holds the preparation phase for at most 30 seconds. The independent
// runtime workers prepare each fixed participant. Only this final human
// transaction can commit the target; subsequent convergence is asynchronous.
func (s *SelfConfig) Apply(ctx context.Context, actor Actor, req SelfConfigApplyRequest) (SelfConfigStatus, error) {
	if req.Revision < 1 || req.ExpectedGeneration < 1 || req.SchemaVersion != runtimeconfig.SchemaVersion || req.IdempotencyKey == "" || len(req.IdempotencyKey) > 128 || strings.ContainsAny(req.IdempotencyKey, "\r\n") {
		return SelfConfigStatus{}, domain.ErrInvalid
	}
	b, err := s.bindingForActor(ctx, actor, authz.OpSelfConfigApply)
	if err != nil {
		return SelfConfigStatus{}, err
	}
	id, err := newID("scj")
	if err != nil {
		return SelfConfigStatus{}, err
	}
	var job store.SelfConfigJob
	createdAt, err := s.runtimeTimestamp(ctx)
	if err != nil {
		return SelfConfigStatus{}, err
	}
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpSelfConfigApply, selfConfigScope(b), s.now())
		if err != nil {
			return err
		}
		// Request identity survives payload collection and later target changes.
		existing, err := r.SelfConfig().JobByIdempotencyKey(ctx, p, req.IdempotencyKey)
		if err == nil {
			if existing.PrincipalID != string(caller.Principal) || existing.Revision != req.Revision || existing.SchemaVersion != int64(req.SchemaVersion) || existing.ExpectedGeneration != req.ExpectedGeneration || existing.ConfirmRestoredCredentials != req.ConfirmRestoredCredentials {
				return domain.ErrConflict
			}
			job = existing
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		snapshot, err := r.Snapshots().AtRevision(ctx, p, req.Revision)
		if err != nil {
			return err
		}
		if !snapshot.PayloadPresent() {
			return collectedRevisionError(snapshot)
		}
		job, err = r.SelfConfig().BeginJob(ctx, p, store.SelfConfigJob{ID: id, IdempotencyKey: req.IdempotencyKey, PrincipalID: string(caller.Principal), SnapshotID: snapshot.ID, Revision: req.Revision, SchemaVersion: int64(req.SchemaVersion), ExpectedGeneration: req.ExpectedGeneration, CreatedAt: createdAt, LocalNodeID: s.NodeID, ConfirmRestoredCredentials: req.ConfirmRestoredCredentials})
		if err != nil {
			return err
		}
		if job.ID != id {
			return nil
		} // Idempotent requests do not emit another intent.
		// Check new decisions only. A retry of a committed recovery apply keeps
		// its original confirmation even though the binding is now resumed.
		currentBinding, err := r.SelfConfig().Binding(ctx, p)
		if err != nil {
			return err
		}
		if currentBinding.Suspended != req.ConfirmRestoredCredentials {
			return invalidDetail("restored credentials require explicit confirmation before applying")
		}
		ev, err := newAuditEvent(ctx, audit.EventSelfConfigApplyRequested, caller.Principal, audit.Object{Type: "environment", ID: b.EnvironmentID}, audit.OutcomeSuccess, job.ID, audit.Payload{"owner_instance_id": b.OwnerInstanceID, "revision": job.Revision, "generation": job.Generation, "job_id": job.ID})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, ev)
	})
	if err != nil {
		return SelfConfigStatus{}, err
	}
	if job.Status != "preparing" {
		return s.status(ctx, actor, job.ID)
	}
	checkedAt, err := s.runtimeTimestamp(ctx)
	if err != nil {
		return SelfConfigStatus{}, err
	}
	remaining := min(job.CreatedAt.Add(store.SelfConfigPreparationTTL).Sub(checkedAt), selfConfigConvergenceTimeout)
	if remaining <= 0 {
		return s.status(ctx, actor, job.ID)
	}
	// Retries share the durable job's deadline, including final lock waits
	// and transaction retries after all participants have prepared.
	preparationCtx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var prepared bool
		err = tx.Read(preparationCtx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
			_, p, err := authorize(ctx, az, actor, authz.OpSelfConfigApply, selfConfigScope(b), s.now())
			if err != nil {
				return err
			}
			current, err := r.SelfConfig().Job(ctx, p, job.ID)
			if err != nil {
				return err
			}
			job = current
			if job.Status != "preparing" {
				return nil
			}
			nodes, err := r.SelfConfig().Nodes(ctx, p)
			if err != nil {
				return err
			}
			prepared = len(nodes) > 0
			for _, node := range nodes {
				if !node.Prepared || node.SchemaVersion != int64(req.SchemaVersion) || node.ErrorCode != "" {
					prepared = false
				}
			}
			return nil
		})
		if err != nil {
			return SelfConfigStatus{}, err
		}
		if job.Status != "preparing" {
			return s.status(ctx, actor, job.ID)
		}
		if prepared {
			break
		}
		select {
		case <-preparationCtx.Done():
			return s.status(ctx, actor, job.ID)
		case <-ticker.C:
		}
	}
	commitAt, err := s.runtimeTimestamp(preparationCtx)
	if err != nil {
		return SelfConfigStatus{}, err
	}
	if req.PrepareOnly {
		return s.status(ctx, actor, job.ID)
	}
	if req.PlanDigest != "" {
		s.Keyring.LockHierarchyRotation()
		defer s.Keyring.UnlockHierarchyRotation()
	}
	submission, err := s.prepareDeploymentCommit(preparationCtx, actor, b, job, req.PlanDigest)
	if err != nil {
		if errors.Is(err, ErrDeploymentPreparationExpired) {
			return SelfConfigStatus{}, errors.Join(domain.ErrConflict, err, s.abortExpiredDeployment(ctx, actor, b, job))
		}
		return SelfConfigStatus{}, errors.Join(domain.ErrConflict, err)
	}
	commitAt, err = s.runtimeTimestamp(preparationCtx)
	if err != nil {
		return SelfConfigStatus{}, err
	}
	err = tx.Write(preparationCtx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpSelfConfigApply, selfConfigScope(b), s.now())
		if err != nil {
			return err
		}
		current, err := r.SelfConfig().Job(ctx, p, job.ID)
		if err != nil {
			return err
		}
		if current.Status == "applying" || current.Status == "applied" || current.Status == "partial" {
			return nil
		}
		currentBinding, err := r.SelfConfig().Binding(ctx, p)
		if err != nil {
			return err
		}
		if currentBinding.Suspended != req.ConfirmRestoredCredentials {
			return domain.ErrConflict
		}
		rollout, err := r.SelfConfig().Rollout(ctx, p, current.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if err == nil && (rollout.PlanDigest == "" || rollout.PlanDigest != req.PlanDigest) || errors.Is(err, domain.ErrNotFound) && req.PlanDigest != "" {
			return domain.ErrConflict
		}
		intent, err := NewSelfConfigReauthIntent(SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: b.OwnerInstanceID, Revision: req.Revision, SchemaVersion: req.SchemaVersion, ExpectedGeneration: req.ExpectedGeneration, ConfirmRestoredCredentials: req.ConfirmRestoredCredentials, PlanDigest: req.PlanDigest})
		if err != nil {
			return err
		}
		if s.Auth == nil {
			return errors.New("service: self-configuration apply requires reauthentication")
		}
		if err := s.requireOriginRecovery(ctx, az, caller, currentBinding, current, intent, s.now()); err != nil {
			return err
		}
		if err := s.Auth.ConsumeSelfConfigReauth(ctx, az, caller, intent, s.now()); err != nil {
			return err
		}
		if submission != nil {
			if submission.PlanDigest != req.PlanDigest || submission.RowVersion != rollout.RowVersion {
				return domain.ErrConflict
			}
			if err := s.commitDeploymentRoot(ctx, r, az, caller, submission, commitAt); err != nil {
				return err
			}
			if err := r.SelfConfig().PutRollout(ctx, p, submission.SelfConfigRollout); err != nil {
				return err
			}
		}
		committed, err := r.SelfConfig().CommitJob(ctx, p, job.ID, commitAt)
		if err != nil {
			return err
		}
		ev, err := newAuditEvent(ctx, audit.EventSelfConfigTargetCommitted, caller.Principal, audit.Object{Type: "environment", ID: committed.EnvironmentID}, audit.OutcomeSuccess, job.ID, audit.Payload{"owner_instance_id": committed.OwnerInstanceID, "revision": committed.DesiredRevision, "generation": committed.Generation, "job_id": job.ID})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return err
		}
		if currentBinding.Suspended {
			resumed, err := newAuditEvent(ctx, audit.EventSelfConfigResumed, caller.Principal, audit.Object{Type: "environment", ID: committed.EnvironmentID}, audit.OutcomeSuccess, job.ID, audit.Payload{"owner_instance_id": committed.OwnerInstanceID, "revision": committed.DesiredRevision, "generation": committed.Generation, "job_id": job.ID})
			if err != nil {
				return err
			}
			return r.Audit().InsertTenant(ctx, p, resumed)
		}
		return nil
	})
	if err != nil {
		return SelfConfigStatus{}, err
	}
	return s.status(ctx, actor, job.ID)
}
