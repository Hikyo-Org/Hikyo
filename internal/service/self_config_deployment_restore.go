package service

import (
	"context"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

type SelfConfigDeploymentRestoreRequest struct {
	Revision, ExpectedGeneration int64
	SchemaVersion                int
	PlanDigest                   string
}

// RestoreDeployment authorizes only the retained external deployment rollback.
// The desired runtime target stays fenced until a separately authorized repair.
// One original job has one restore command; retries return that durable decision.
func (s *SelfConfig) RestoreDeployment(ctx context.Context, actor Actor, req SelfConfigDeploymentRestoreRequest) (SelfConfigStatus, error) {
	if req.Revision < 1 || req.ExpectedGeneration < 1 || req.SchemaVersion != runtimeconfig.SchemaVersion {
		return SelfConfigStatus{}, domain.ErrInvalid
	}
	b, err := s.bindingForActor(ctx, actor, authz.OpSelfConfigApply)
	if err != nil {
		return SelfConfigStatus{}, err
	}
	intent, err := NewSelfConfigReauthIntent(SelfConfigReauthTarget{Action: "rollout-restore", OwnerInstanceID: b.OwnerInstanceID, Revision: req.Revision, ExpectedGeneration: req.ExpectedGeneration, SchemaVersion: req.SchemaVersion, PlanDigest: req.PlanDigest})
	if err != nil {
		return SelfConfigStatus{}, err
	}
	var job store.SelfConfigJob
	var row store.SelfConfigRollout
	var sequence int64
	var repeated bool
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		_, p, err := authorize(ctx, az, actor, authz.OpSelfConfigApply, selfConfigScope(b), s.now())
		if err != nil {
			return err
		}
		current, err := r.SelfConfig().Binding(ctx, p)
		if err != nil {
			return err
		}
		if !restoreBindingMatches(current, b, req) {
			return domain.ErrConflict
		}
		jobs, err := r.SelfConfig().Jobs(ctx, p)
		if err != nil {
			return err
		}
		for _, candidate := range jobs {
			if candidate.Generation == req.ExpectedGeneration && candidate.Revision == req.Revision && candidate.Status == "partial" {
				job = candidate
				break
			}
		}
		if job.ID == "" {
			return domain.ErrConflict
		}
		row, err = r.SelfConfig().Rollout(ctx, p, job.ID)
		if err != nil {
			return err
		}
		if row.PlanDigest != req.PlanDigest || row.Incarnation != b.Incarnation {
			return domain.ErrConflict
		}
		command, err := decodeRolloutCommand(row.CommandJSON)
		if err != nil {
			return err
		}
		if command.Command.Intent != rolloutIntent(b, job) || command.Command.PlanDigest != req.PlanDigest || command.Command.Action == configrollout.ActionPrepare {
			return domain.ErrConflict
		}
		repeated = command.Command.Action == configrollout.ActionRestore
		if repeated {
			return nil
		}
		if row.ExternalPhase != "" {
			return domain.ErrConflict
		}
		sequence, err = r.SelfConfig().NextRolloutSequence(ctx, p, row.EnrollmentID)
		return err
	})
	if err != nil {
		return SelfConfigStatus{}, err
	}
	if repeated {
		return s.status(ctx, actor, job.ID)
	}
	if !s.deploymentMatches(b) || row.EnrollmentID != s.Deployment.Identity().EnrollmentID {
		return SelfConfigStatus{}, configrollout.ErrUnsupported
	}
	command, err := decodeRolloutCommand(row.CommandJSON)
	if err != nil {
		return SelfConfigStatus{}, err
	}
	signed, err := s.Deployment.DecisionCommand(ctx, command, configrollout.ActionRestore, uint64(sequence), req.PlanDigest, nil)
	if err != nil {
		return SelfConfigStatus{}, err
	}
	raw, err := encodeRolloutCommand(signed)
	if err != nil {
		return SelfConfigStatus{}, err
	}
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpSelfConfigApply, selfConfigScope(b), s.now())
		if err != nil {
			return err
		}
		current, err := r.SelfConfig().Binding(ctx, p)
		if err != nil {
			return err
		}
		if !restoreBindingMatches(current, b, req) {
			return domain.ErrConflict
		}
		latest, err := r.SelfConfig().Job(ctx, p, job.ID)
		if err != nil {
			return err
		}
		if latest.Status != "partial" || latest.Generation != job.Generation || latest.SnapshotID != job.SnapshotID {
			return domain.ErrConflict
		}
		stored, err := r.SelfConfig().Rollout(ctx, p, job.ID)
		if err != nil {
			return err
		}
		if stored.Incarnation != b.Incarnation || stored.PlanDigest != req.PlanDigest {
			return domain.ErrConflict
		}
		existing, err := decodeRolloutCommand(stored.CommandJSON)
		if err != nil {
			return err
		}
		if existing.Command.Action == configrollout.ActionRestore {
			return nil
		}
		if stored.RowVersion != row.RowVersion || stored.ExternalPhase != "" {
			return domain.ErrConflict
		}
		if s.Auth == nil {
			return ErrReauthUnitMismatch
		}
		if err := s.Auth.ConsumeSelfConfigReauth(ctx, az, caller, intent, s.now()); err != nil {
			return err
		}
		row.CommandJSON = raw
		row.Sequence = sequence
		if err := r.SelfConfig().PutRollout(ctx, p, row); err != nil {
			return err
		}
		event, err := newAuditEvent(ctx, audit.EventSelfConfigDeploymentRestoreRequested, caller.Principal, audit.Object{Type: "environment", ID: b.EnvironmentID}, audit.OutcomeSuccess, job.ID, audit.Payload{"owner_instance_id": b.OwnerInstanceID, "revision": job.Revision, "generation": b.Generation, "job_id": job.ID, "plan_digest": row.PlanDigest})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, event)
	})
	if err != nil {
		return SelfConfigStatus{}, err
	}
	return s.status(ctx, actor, job.ID)
}

func restoreBindingMatches(current, expected store.SelfConfigBinding, req SelfConfigDeploymentRestoreRequest) bool {
	return !current.Suspended && current.OwnerInstanceID == expected.OwnerInstanceID && current.Incarnation == expected.Incarnation && current.OrgID == expected.OrgID && current.ProjectID == expected.ProjectID && current.EnvironmentID == expected.EnvironmentID && current.Generation == req.ExpectedGeneration && current.DesiredRevision == req.Revision && current.SchemaVersion == int64(req.SchemaVersion)
}
