package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func (r selfConfigRepo) Rollout(ctx context.Context, p authz.Proof, jobID string) (SelfConfigRollout, error) {
	if _, err := r.verify(ctx, p, authz.StoreSelfConfigRollout, false); err != nil {
		return SelfConfigRollout{}, err
	}
	return r.q.rollout(ctx, jobID)
}

func (r selfConfigRepo) NextRolloutSequence(ctx context.Context, p authz.Proof, enrollmentID string) (int64, error) {
	if _, err := r.verify(ctx, p, authz.StoreSelfConfigNextRolloutSequence, true); err != nil {
		return 0, err
	}
	if enrollmentID == "" || len(enrollmentID) > 128 {
		return 0, domain.ErrInvalid
	}
	return r.q.nextRolloutSequence(ctx, enrollmentID)
}

func (r selfConfigRepo) PutRollout(ctx context.Context, p authz.Proof, want SelfConfigRollout) error {
	b, err := r.verify(ctx, p, authz.StoreSelfConfigPutRollout, true)
	if err != nil {
		return err
	}
	if want.JobID == "" || want.EnrollmentID == "" || len(want.EnrollmentID) > 128 || want.Incarnation != b.Incarnation || want.Sequence < 1 || want.RowVersion < 0 || len(want.CommandJSON) > 64<<10 || !json.Valid([]byte(want.CommandJSON)) || len(want.ResponseJSON) > 64<<10 || (want.ResponseJSON != "" && !json.Valid([]byte(want.ResponseJSON))) {
		return domain.ErrInvalid
	}
	if want.ExternalPhase != "" && want.ExternalPhase != "applied" && want.ExternalPhase != "restored" {
		return domain.ErrInvalid
	}
	if want.PlanDigest != "" {
		decoded, err := hex.DecodeString(want.PlanDigest)
		if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != want.PlanDigest {
			return domain.ErrInvalid
		}
	}
	job, err := r.q.job(ctx, want.JobID, false)
	if err != nil {
		return err
	}
	if job.Status != "preparing" && job.Status != "applying" && job.Status != "partial" {
		return domain.ErrConflict
	}
	previous, err := r.q.rollout(ctx, want.JobID)
	if errors.Is(err, domain.ErrNotFound) {
		if want.RowVersion != 0 || job.Status != "preparing" || job.ExpectedGeneration != b.Generation || want.PlanDigest != "" || want.ExternalPhase != "" {
			return domain.ErrConflict
		}
	} else if err != nil {
		return err
	} else {
		if previous.ExternalPhase != "" && previous.ExternalPhase != want.ExternalPhase {
			return domain.ErrConflict
		}
		if previous.EnrollmentID != want.EnrollmentID || previous.Incarnation != want.Incarnation || previous.RowVersion != want.RowVersion || want.Sequence < previous.Sequence || (previous.PlanDigest != "" && previous.PlanDigest != want.PlanDigest) {
			return domain.ErrConflict
		}
		if want.Sequence == previous.Sequence && want.CommandJSON != previous.CommandJSON {
			return domain.ErrConflict
		}
	}
	return r.q.putRollout(ctx, want)
}
