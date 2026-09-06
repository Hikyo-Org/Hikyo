package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5"
	"time"
)

func (r sqliteSelfConfigStorage) rollout(ctx context.Context, id string) (SelfConfigRollout, error) {
	row, err := r.q.GetSelfConfigRollout(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return SelfConfigRollout{}, ErrNotFound
	}
	if err != nil {
		return SelfConfigRollout{}, err
	}
	return SelfConfigRollout{JobID: row.JobID, EnrollmentID: row.EnrollmentID, Incarnation: row.Incarnation, PlanDigest: row.PlanDigest, CommandJSON: row.CommandJson, ResponseJSON: row.ResponseJson, ExternalPhase: row.ExternalPhase, Sequence: row.Sequence, RowVersion: row.RowVersion}, nil
}
func (r sqliteSelfConfigStorage) nextRolloutSequence(ctx context.Context, id string) (int64, error) {
	return r.q.NextSelfConfigRolloutSequence(ctx, id)
}
func (r sqliteSelfConfigStorage) putRollout(ctx context.Context, v SelfConfigRollout) error {
	if v.RowVersion == 0 {
		return constraint(affected(r.q.InsertSelfConfigRollout(ctx, sqlitegen.InsertSelfConfigRolloutParams{JobID: v.JobID, EnrollmentID: v.EnrollmentID, Incarnation: v.Incarnation, PlanDigest: v.PlanDigest, CommandJson: v.CommandJSON, ResponseJson: v.ResponseJSON, ExternalPhase: v.ExternalPhase, Sequence: v.Sequence})))
	}
	return affected(r.q.UpdateSelfConfigRollout(ctx, sqlitegen.UpdateSelfConfigRolloutParams{JobID: v.JobID, EnrollmentID: v.EnrollmentID, Incarnation: v.Incarnation, PlanDigest: v.PlanDigest, CommandJson: v.CommandJSON, ResponseJson: v.ResponseJSON, ExternalPhase: v.ExternalPhase, Sequence: v.Sequence, ExpectedVersion: v.RowVersion}))
}

func (r pgSelfConfigStorage) rollout(ctx context.Context, id string) (SelfConfigRollout, error) {
	row, err := r.q.GetSelfConfigRollout(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return SelfConfigRollout{}, ErrNotFound
	}
	if err != nil {
		return SelfConfigRollout{}, err
	}
	return SelfConfigRollout{JobID: row.JobID, EnrollmentID: row.EnrollmentID, Incarnation: row.Incarnation, PlanDigest: row.PlanDigest, CommandJSON: row.CommandJson, ResponseJSON: row.ResponseJson, ExternalPhase: row.ExternalPhase, Sequence: row.Sequence, RowVersion: row.RowVersion}, nil
}
func (r pgSelfConfigStorage) nextRolloutSequence(ctx context.Context, id string) (int64, error) {
	return r.q.NextSelfConfigRolloutSequence(ctx, id)
}
func (r pgSelfConfigStorage) putRollout(ctx context.Context, v SelfConfigRollout) error {
	if v.RowVersion == 0 {
		return constraint(affected(r.q.InsertSelfConfigRollout(ctx, pggen.InsertSelfConfigRolloutParams{JobID: v.JobID, EnrollmentID: v.EnrollmentID, Incarnation: v.Incarnation, PlanDigest: v.PlanDigest, CommandJson: v.CommandJSON, ResponseJson: v.ResponseJSON, ExternalPhase: v.ExternalPhase, Sequence: v.Sequence})))
	}
	return affected(r.q.UpdateSelfConfigRollout(ctx, pggen.UpdateSelfConfigRolloutParams{JobID: v.JobID, EnrollmentID: v.EnrollmentID, Incarnation: v.Incarnation, PlanDigest: v.PlanDigest, CommandJson: v.CommandJSON, ResponseJson: v.ResponseJSON, ExternalPhase: v.ExternalPhase, Sequence: v.Sequence, ExpectedVersion: v.RowVersion}))
}

func (r sqliteSelfConfigStorage) completedGeneration(ctx context.Context, generation int64) (int64, error) {
	return r.q.CountSelfConfigCompletedGeneration(ctx, generation)
}

func (r pgSelfConfigStorage) completedGeneration(ctx context.Context, generation int64) (int64, error) {
	return r.q.CountSelfConfigCompletedGeneration(ctx, generation)
}

func (r sqliteSelfConfigStorage) previousRevision(ctx context.Context) (int64, error) {
	v, err := r.q.GetSelfConfigPreviousRevision(ctx)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return v, err
}

func (r pgSelfConfigStorage) previousRevision(ctx context.Context) (int64, error) {
	v, err := r.q.GetSelfConfigPreviousRevision(ctx)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return v, err
}

// Read time after acquiring the transaction fence. SQLite worker timestamps
// use the host clock, so retain its microseconds rather than SQLite strftime's
// millisecond precision. PostgreSQL workers use the datastore clock; reading
// clock_timestamp here also includes time spent waiting for the writer fence.
func (r sqliteSelfConfigStorage) currentTime(context.Context) (time.Time, error) {
	return CanonTime(time.Now()), nil
}
func (r pgSelfConfigStorage) currentTime(ctx context.Context) (time.Time, error) {
	stamp, err := r.q.GetSelfConfigClock(ctx)
	if err != nil {
		return time.Time{}, err
	}
	return parseTime("self config clock", "observed_at", stamp)
}
