package upgrade

import (
	"context"
)

// FenceBackup drains admitted transactions and durably freezes a completed
// source before encrypted export. The caller must authenticate the exact route
// and the installation's enrolled operator before invoking this primitive.
// Like Prepare, this grants neither migration nor runtime authority.
// Repeating the identical intent is safe; a different intent stays fenced.
func (s *Session) FenceBackup(ctx context.Context, expected State, preparation BackupPreparation) (State, error) {
	if expected.Validate() != nil || expected.Pending.Invalidated {
		return State{}, ErrConflict
	}
	previous := expected.Pending
	if previous.Phase == BackupPreparing {
		if previous.Preparation == nil || *previous.Preparation != preparation {
			return State{}, ErrConflict
		}
		return s.Resume(ctx, expected)
	}
	if previous.Phase != Healthy || expected.Maintenance || previous.Hop+1 != previous.RouteLength {
		return State{}, ErrConflict
	}
	if proof := previous.Acceptance.Attestation; proof != nil && proof.OperatorKeyID != preparation.OperatorKeyID {
		return State{}, ErrConflict
	}
	next := expected
	pending := *previous
	pending.Phase, pending.Preparation = BackupPreparing, &preparation
	next.Pending, next.Maintenance = &pending, true
	if next.Validate() != nil {
		return State{}, ErrConflict
	}
	err := s.transaction(ctx, func() error {
		if err := s.compare(ctx, expected); err != nil {
			return err
		}
		return s.persist(ctx, next, false)
	})
	if err != nil {
		return State{}, err
	}
	return next, nil
}
