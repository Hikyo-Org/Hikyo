package service

import (
	"context"
	"encoding/json"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// renewDeploymentDelivery preserves an already committed exact-MFA decision
// across an executor outage longer than the transport TTL. Neither preparation
// nor another target can obtain renewal. The new signature is sent only after
// its sequence and bytes are durable under the binding and rollout row fences.
func (s *SelfConfig) renewDeploymentDelivery(ctx context.Context, b store.SelfConfigBinding, j store.SelfConfigJob, row store.SelfConfigRollout, command configrollout.SignedCommand) (store.SelfConfigRollout, configrollout.SignedCommand, error) {
	if row.ExternalPhase != "" || command.Command.ExpiresAt.IsZero() || s.now().Before(command.Command.ExpiresAt) {
		return row, command, nil
	}
	var renewed store.SelfConfigRollout
	var signed configrollout.SignedCommand
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		current, err := r.SelfConfig().Binding(ctx, p)
		if err != nil {
			return err
		}
		if !s.deploymentMatches(current) || current.Suspended || current.OwnerInstanceID != b.OwnerInstanceID || current.Incarnation != b.Incarnation || current.Generation != b.Generation || current.DesiredSnapshotID != j.SnapshotID || current.DesiredRevision != j.Revision || current.Generation != j.Generation {
			return domain.ErrConflict
		}
		job, err := r.SelfConfig().Job(ctx, p, j.ID)
		if err != nil {
			return err
		}
		if job.Status != "applying" && job.Status != "partial" || job.SnapshotID != j.SnapshotID || job.Generation != j.Generation || job.ExpectedGeneration != j.ExpectedGeneration {
			return domain.ErrConflict
		}
		stored, err := r.SelfConfig().Rollout(ctx, p, j.ID)
		if err != nil {
			return err
		}
		if stored.RowVersion != row.RowVersion || stored.CommandJSON != row.CommandJSON || stored.PlanDigest != row.PlanDigest || stored.Incarnation != current.Incarnation || stored.EnrollmentID != s.Deployment.Identity().EnrollmentID || stored.ExternalPhase != "" {
			return domain.ErrConflict
		}
		persisted, err := decodeRolloutCommand(stored.CommandJSON)
		if err != nil {
			return err
		}
		if persisted.Command.Intent != rolloutIntent(current, job) || persisted.Command.PlanDigest != stored.PlanDigest {
			return domain.ErrConflict
		}
		switch persisted.Command.Action {
		case configrollout.ActionSubmit, configrollout.ActionObserve, configrollout.ActionRestore:
		default:
			return domain.ErrConflict
		}
		sequence, err := r.SelfConfig().NextRolloutSequence(ctx, p, stored.EnrollmentID)
		if err != nil {
			return err
		}
		signed, err = s.Deployment.RenewCommand(ctx, persisted, uint64(sequence))
		if err != nil {
			return err
		}
		normalized := signed.Command
		normalized.Sequence, normalized.IssuedAt, normalized.ExpiresAt = persisted.Command.Sequence, persisted.Command.IssuedAt, persisted.Command.ExpiresAt
		before, _ := json.Marshal(persisted.Command)
		after, _ := json.Marshal(normalized)
		if string(before) != string(after) || signed.Command.Sequence != uint64(sequence) || !s.now().Before(signed.Command.ExpiresAt) {
			return domain.ErrConflict
		}
		stored.CommandJSON, err = encodeRolloutCommand(signed)
		if err != nil {
			return err
		}
		stored.Sequence = sequence
		if err := r.SelfConfig().PutRollout(ctx, p, stored); err != nil {
			return err
		}
		stored.RowVersion++
		renewed = stored
		return nil
	})
	return renewed, signed, err
}
