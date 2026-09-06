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

type sqliteSelfConfigStorage struct{ q *sqlitegen.Queries }

func sqliteSelfConfigBinding(row sqlitegen.SelfConfigBinding) (SelfConfigBinding, error) {
	out := SelfConfigBinding{AdoptionKey: row.AdoptionKey, AdoptedBy: row.AdoptedBy, OwnerInstanceID: row.OwnerInstanceID,
		OrgID:              row.OrgID,
		ProjectID:          row.ProjectID,
		EnvironmentID:      row.EnvironmentID,
		SchemaVersion:      row.SchemaVersion,
		Generation:         row.Generation,
		DesiredRevision:    row.DesiredRevision,
		DesiredSnapshotID:  row.DesiredSnapshotID,
		PreviousSnapshotID: row.PreviousSnapshotID,
		Incarnation:        row.Incarnation,
		Suspended:          row.Suspended == 1,
	}
	parsedCreatedAt, err := parseTime("self config", "CreatedAt", row.CreatedAt)
	if err != nil {
		return SelfConfigBinding{}, err
	}
	out.CreatedAt = parsedCreatedAt
	parsedUpdatedAt, err := parseTime("self config", "UpdatedAt", row.UpdatedAt)
	if err != nil {
		return SelfConfigBinding{}, err
	}
	out.UpdatedAt = parsedUpdatedAt
	return out, nil
}
func sqliteSelfConfigJob(row sqlitegen.SelfConfigJob) (SelfConfigJob, error) {
	out := SelfConfigJob{ID: row.ID,
		IdempotencyKey:             row.IdempotencyKey,
		ConfirmRestoredCredentials: row.ConfirmRestoredCredentials == 1,
		PrincipalID:                row.PrincipalID,
		SnapshotID:                 row.SnapshotID,
		Revision:                   row.Revision,
		SchemaVersion:              row.SchemaVersion,
		ExpectedGeneration:         row.ExpectedGeneration,
		Generation:                 row.Generation,
		Status:                     row.Status,
		ErrorCode:                  row.ErrorCode,
	}
	parsedCreatedAt, err := parseTime("self config", "CreatedAt", row.CreatedAt)
	if err != nil {
		return SelfConfigJob{}, err
	}
	out.CreatedAt = parsedCreatedAt
	parsedUpdatedAt, err := parseTime("self config", "UpdatedAt", row.UpdatedAt)
	if err != nil {
		return SelfConfigJob{}, err
	}
	out.UpdatedAt = parsedUpdatedAt
	return out, nil
}
func sqliteSelfConfigNode(row sqlitegen.SelfConfigNode) (SelfConfigNode, error) {
	out := SelfConfigNode{NodeID: row.NodeID,
		JobID:            row.JobID,
		SchemaVersion:    row.SchemaVersion,
		ActiveGeneration: row.ActiveGeneration,
		ActiveRevision:   row.ActiveRevision,
		Incarnation:      row.Incarnation,
		ErrorCode:        row.ErrorCode,
		Prepared:         row.Prepared == 1,
	}
	parsedUpdatedAt, err := parseTime("self config", "UpdatedAt", row.UpdatedAt)
	if err != nil {
		return SelfConfigNode{}, err
	}
	out.UpdatedAt = parsedUpdatedAt
	return out, nil
}
func (r sqliteSelfConfigStorage) binding(ctx context.Context, lock bool) (SelfConfigBinding, error) {
	var row sqlitegen.SelfConfigBinding
	var err error
	if lock {
		row, err = r.q.LockSelfConfigBinding(ctx)
	} else {
		row, err = r.q.GetSelfConfigBinding(ctx)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return SelfConfigBinding{}, ErrNotFound
	}
	if err != nil {
		return SelfConfigBinding{}, err
	}
	return sqliteSelfConfigBinding(row)
}
func (r sqliteSelfConfigStorage) job(ctx context.Context, id string, byKey bool) (SelfConfigJob, error) {
	var row sqlitegen.SelfConfigJob
	var err error
	if byKey {
		row, err = r.q.GetSelfConfigJobByKey(ctx, id)
	} else {
		row, err = r.q.GetSelfConfigJob(ctx, id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return SelfConfigJob{}, ErrNotFound
	}
	if err != nil {
		return SelfConfigJob{}, err
	}
	return sqliteSelfConfigJob(row)
}
func (r sqliteSelfConfigStorage) jobs(ctx context.Context) ([]SelfConfigJob, error) {
	rows, err := r.q.ListSelfConfigJobs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SelfConfigJob, 0, len(rows))
	for _, row := range rows {
		v, e := sqliteSelfConfigJob(row)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
func (r sqliteSelfConfigStorage) nodes(ctx context.Context) ([]SelfConfigNode, error) {
	rows, err := r.q.ListSelfConfigNodes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SelfConfigNode, 0, len(rows))
	for _, row := range rows {
		v, e := sqliteSelfConfigNode(row)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
func (r sqliteSelfConfigStorage) insertBinding(ctx context.Context, b SelfConfigBinding) error {
	return constraint(affected(r.q.CreateSelfConfigBinding(ctx, sqlitegen.CreateSelfConfigBindingParams{AdoptionKey: b.AdoptionKey, AdoptedBy: b.AdoptedBy, OwnerInstanceID: b.OwnerInstanceID, OrgID: b.OrgID, ProjectID: b.ProjectID, EnvironmentID: b.EnvironmentID, SnapshotID: b.DesiredSnapshotID, SchemaVersion: b.SchemaVersion, Incarnation: b.Incarnation, CreatedAt: CanonTime(b.CreatedAt).Format(timeFormat)})))
}
func (r sqliteSelfConfigStorage) insertJob(ctx context.Context, j SelfConfigJob) error {
	return constraint(affected(r.q.InsertSelfConfigJob(ctx, sqlitegen.InsertSelfConfigJobParams{ConfirmRestoredCredentials: boolInt(j.ConfirmRestoredCredentials), ID: j.ID, IdempotencyKey: j.IdempotencyKey, PrincipalID: j.PrincipalID, SnapshotID: j.SnapshotID, Revision: j.Revision, SchemaVersion: j.SchemaVersion, ExpectedGeneration: j.ExpectedGeneration, CreatedAt: CanonTime(j.CreatedAt).Format(timeFormat)})))
}
func (r sqliteSelfConfigStorage) updateJob(ctx context.Context, j SelfConfigJob, previous string) error {
	return affected(r.q.UpdateSelfConfigJob(ctx, sqlitegen.UpdateSelfConfigJobParams{ID: j.ID, Status: j.Status, PreviousStatus: previous, ErrorCode: j.ErrorCode, UpdatedAt: CanonTime(j.UpdatedAt).Format(timeFormat)}))
}
func (r sqliteSelfConfigStorage) commit(ctx context.Context, j SelfConfigJob, at time.Time) error {
	return affected(r.q.CommitSelfConfigTarget(ctx, sqlitegen.CommitSelfConfigTargetParams{SnapshotID: j.SnapshotID, Revision: j.Revision, ExpectedGeneration: j.ExpectedGeneration, UpdatedAt: CanonTime(at).Format(timeFormat)}))
}
func (r sqliteSelfConfigStorage) previous(ctx context.Context, id string) error {
	return r.q.SetSelfConfigPrevious(ctx, id)
}
func (r sqliteSelfConfigStorage) fence(ctx context.Context, incarnation string, at time.Time) (int64, error) {
	return r.q.FenceSelfConfigRestored(ctx, sqlitegen.FenceSelfConfigRestoredParams{Incarnation: incarnation, UpdatedAt: CanonTime(at).Format(timeFormat)})
}
func (r sqliteSelfConfigStorage) putNode(ctx context.Context, n SelfConfigNode) error {
	return r.q.PutSelfConfigNode(ctx, sqlitegen.PutSelfConfigNodeParams{NodeID: n.NodeID, JobID: n.JobID, SchemaVersion: n.SchemaVersion, Prepared: boolInt(n.Prepared), ActiveGeneration: n.ActiveGeneration, ActiveRevision: n.ActiveRevision, Incarnation: n.Incarnation, ErrorCode: n.ErrorCode, UpdatedAt: CanonTime(n.UpdatedAt).Format(timeFormat)})
}
func (r sqliteSelfConfigStorage) deleteNodes(ctx context.Context) error {
	return r.q.DeleteSelfConfigNodes(ctx)
}
func (r sqliteSelfConfigStorage) retained(ctx context.Context) ([]string, error) {
	return r.q.ListSelfConfigRetained(ctx)
}
func (r sqliteSelfConfigStorage) retentionSlot(ctx context.Context, slot string) (string, error) {
	return r.q.GetSelfConfigRetentionSlot(ctx, slot)
}
func (r sqliteSelfConfigStorage) retain(ctx context.Context, slot, id string) error {
	return r.q.SetSelfConfigRetention(ctx, sqlitegen.SetSelfConfigRetentionParams{Slot: slot, SnapshotID: id})
}
func (r sqliteSelfConfigStorage) release(ctx context.Context, slot string) error {
	return r.q.DeleteSelfConfigRetention(ctx, slot)
}
func (r sqliteSelfConfigStorage) participants(ctx context.Context, since time.Time) ([]string, error) {
	return r.q.ListSelfConfigParticipants(ctx, CanonTime(since).Format(timeFormat))
}
func (r sqliteSelfConfigStorage) recent(ctx context.Context, since time.Time) (int64, error) {
	return r.q.CountRecentSelfConfigJobs(ctx, CanonTime(since).Format(timeFormat))
}
func (r sqliteSelfConfigStorage) open(ctx context.Context) (int64, error) {
	return r.q.CountSelfConfigOpenJobs(ctx)
}
func (r sqliteSelfConfigStorage) lockSnapshot(ctx context.Context, b SelfConfigBinding, id string) error {
	_, err := r.q.LockSnapshotForRetentionConsequence(ctx, sqlitegen.LockSnapshotForRetentionConsequenceParams{OrgID: b.OrgID, ProjectID: b.ProjectID, EnvironmentID: b.EnvironmentID, ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

type pgSelfConfigStorage struct{ q *pggen.Queries }

func pgSelfConfigBinding(row pggen.SelfConfigBinding) (SelfConfigBinding, error) {
	out := SelfConfigBinding{AdoptionKey: row.AdoptionKey, AdoptedBy: row.AdoptedBy, OwnerInstanceID: row.OwnerInstanceID,
		OrgID:              row.OrgID,
		ProjectID:          row.ProjectID,
		EnvironmentID:      row.EnvironmentID,
		SchemaVersion:      row.SchemaVersion,
		Generation:         row.Generation,
		DesiredRevision:    row.DesiredRevision,
		DesiredSnapshotID:  row.DesiredSnapshotID,
		PreviousSnapshotID: row.PreviousSnapshotID,
		Incarnation:        row.Incarnation,
		Suspended:          row.Suspended,
	}
	if !row.CreatedAt.Valid {
		return SelfConfigBinding{}, ErrConflict
	}
	out.CreatedAt = row.CreatedAt.Time.UTC()
	if !row.UpdatedAt.Valid {
		return SelfConfigBinding{}, ErrConflict
	}
	out.UpdatedAt = row.UpdatedAt.Time.UTC()
	return out, nil
}
func pgSelfConfigJob(row pggen.SelfConfigJob) (SelfConfigJob, error) {
	out := SelfConfigJob{ID: row.ID,
		IdempotencyKey:             row.IdempotencyKey,
		ConfirmRestoredCredentials: row.ConfirmRestoredCredentials,
		PrincipalID:                row.PrincipalID,
		SnapshotID:                 row.SnapshotID,
		Revision:                   row.Revision,
		SchemaVersion:              row.SchemaVersion,
		ExpectedGeneration:         row.ExpectedGeneration,
		Generation:                 row.Generation,
		Status:                     row.Status,
		ErrorCode:                  row.ErrorCode,
	}
	if !row.CreatedAt.Valid {
		return SelfConfigJob{}, ErrConflict
	}
	out.CreatedAt = row.CreatedAt.Time.UTC()
	if !row.UpdatedAt.Valid {
		return SelfConfigJob{}, ErrConflict
	}
	out.UpdatedAt = row.UpdatedAt.Time.UTC()
	return out, nil
}
func pgSelfConfigNode(row pggen.SelfConfigNode) (SelfConfigNode, error) {
	out := SelfConfigNode{NodeID: row.NodeID,
		JobID:            row.JobID,
		SchemaVersion:    row.SchemaVersion,
		ActiveGeneration: row.ActiveGeneration,
		ActiveRevision:   row.ActiveRevision,
		Incarnation:      row.Incarnation,
		ErrorCode:        row.ErrorCode,
		Prepared:         row.Prepared,
	}
	if !row.UpdatedAt.Valid {
		return SelfConfigNode{}, ErrConflict
	}
	out.UpdatedAt = row.UpdatedAt.Time.UTC()
	return out, nil
}
func (r pgSelfConfigStorage) binding(ctx context.Context, lock bool) (SelfConfigBinding, error) {
	var row pggen.SelfConfigBinding
	var err error
	if lock {
		row, err = r.q.LockSelfConfigBinding(ctx)
	} else {
		row, err = r.q.GetSelfConfigBinding(ctx)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return SelfConfigBinding{}, ErrNotFound
	}
	if err != nil {
		return SelfConfigBinding{}, err
	}
	return pgSelfConfigBinding(row)
}
func (r pgSelfConfigStorage) job(ctx context.Context, id string, byKey bool) (SelfConfigJob, error) {
	var row pggen.SelfConfigJob
	var err error
	if byKey {
		row, err = r.q.GetSelfConfigJobByKey(ctx, id)
	} else {
		row, err = r.q.GetSelfConfigJob(ctx, id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return SelfConfigJob{}, ErrNotFound
	}
	if err != nil {
		return SelfConfigJob{}, err
	}
	return pgSelfConfigJob(row)
}
func (r pgSelfConfigStorage) jobs(ctx context.Context) ([]SelfConfigJob, error) {
	rows, err := r.q.ListSelfConfigJobs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SelfConfigJob, 0, len(rows))
	for _, row := range rows {
		v, e := pgSelfConfigJob(row)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
func (r pgSelfConfigStorage) nodes(ctx context.Context) ([]SelfConfigNode, error) {
	rows, err := r.q.ListSelfConfigNodes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SelfConfigNode, 0, len(rows))
	for _, row := range rows {
		v, e := pgSelfConfigNode(row)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
func (r pgSelfConfigStorage) insertBinding(ctx context.Context, b SelfConfigBinding) error {
	return constraint(affected(r.q.CreateSelfConfigBinding(ctx, pggen.CreateSelfConfigBindingParams{AdoptionKey: b.AdoptionKey, AdoptedBy: b.AdoptedBy, OwnerInstanceID: b.OwnerInstanceID, OrgID: b.OrgID, ProjectID: b.ProjectID, EnvironmentID: b.EnvironmentID, SnapshotID: b.DesiredSnapshotID, SchemaVersion: b.SchemaVersion, Incarnation: b.Incarnation, CreatedAt: pgTimestamp(b.CreatedAt)})))
}
func (r pgSelfConfigStorage) insertJob(ctx context.Context, j SelfConfigJob) error {
	return constraint(affected(r.q.InsertSelfConfigJob(ctx, pggen.InsertSelfConfigJobParams{ConfirmRestoredCredentials: j.ConfirmRestoredCredentials, ID: j.ID, IdempotencyKey: j.IdempotencyKey, PrincipalID: j.PrincipalID, SnapshotID: j.SnapshotID, Revision: j.Revision, SchemaVersion: j.SchemaVersion, ExpectedGeneration: j.ExpectedGeneration, CreatedAt: pgTimestamp(j.CreatedAt)})))
}
func (r pgSelfConfigStorage) updateJob(ctx context.Context, j SelfConfigJob, previous string) error {
	return affected(r.q.UpdateSelfConfigJob(ctx, pggen.UpdateSelfConfigJobParams{ID: j.ID, Status: j.Status, PreviousStatus: previous, ErrorCode: j.ErrorCode, UpdatedAt: pgTimestamp(j.UpdatedAt)}))
}
func (r pgSelfConfigStorage) commit(ctx context.Context, j SelfConfigJob, at time.Time) error {
	return affected(r.q.CommitSelfConfigTarget(ctx, pggen.CommitSelfConfigTargetParams{SnapshotID: j.SnapshotID, Revision: j.Revision, ExpectedGeneration: j.ExpectedGeneration, UpdatedAt: pgTimestamp(at)}))
}
func (r pgSelfConfigStorage) previous(ctx context.Context, id string) error {
	return r.q.SetSelfConfigPrevious(ctx, id)
}
func (r pgSelfConfigStorage) fence(ctx context.Context, incarnation string, at time.Time) (int64, error) {
	return r.q.FenceSelfConfigRestored(ctx, pggen.FenceSelfConfigRestoredParams{Incarnation: incarnation, UpdatedAt: pgTimestamp(at)})
}
func (r pgSelfConfigStorage) putNode(ctx context.Context, n SelfConfigNode) error {
	return r.q.PutSelfConfigNode(ctx, pggen.PutSelfConfigNodeParams{NodeID: n.NodeID, JobID: n.JobID, SchemaVersion: n.SchemaVersion, Prepared: n.Prepared, ActiveGeneration: n.ActiveGeneration, ActiveRevision: n.ActiveRevision, Incarnation: n.Incarnation, ErrorCode: n.ErrorCode, UpdatedAt: pgTimestamp(n.UpdatedAt)})
}
func (r pgSelfConfigStorage) deleteNodes(ctx context.Context) error {
	return r.q.DeleteSelfConfigNodes(ctx)
}
func (r pgSelfConfigStorage) retained(ctx context.Context) ([]string, error) {
	return r.q.ListSelfConfigRetained(ctx)
}
func (r pgSelfConfigStorage) retentionSlot(ctx context.Context, slot string) (string, error) {
	return r.q.GetSelfConfigRetentionSlot(ctx, slot)
}
func (r pgSelfConfigStorage) retain(ctx context.Context, slot, id string) error {
	return r.q.SetSelfConfigRetention(ctx, pggen.SetSelfConfigRetentionParams{Slot: slot, SnapshotID: id})
}
func (r pgSelfConfigStorage) release(ctx context.Context, slot string) error {
	return r.q.DeleteSelfConfigRetention(ctx, slot)
}
func (r pgSelfConfigStorage) participants(ctx context.Context, since time.Time) ([]string, error) {
	return r.q.ListSelfConfigParticipants(ctx, pgTimestamp(since))
}
func (r pgSelfConfigStorage) recent(ctx context.Context, since time.Time) (int64, error) {
	return r.q.CountRecentSelfConfigJobs(ctx, pgTimestamp(since))
}
func (r pgSelfConfigStorage) open(ctx context.Context) (int64, error) {
	return r.q.CountSelfConfigOpenJobs(ctx)
}
func (r pgSelfConfigStorage) lockSnapshot(ctx context.Context, b SelfConfigBinding, id string) error {
	_, err := r.q.LockSnapshotForRetentionConsequence(ctx, pggen.LockSnapshotForRetentionConsequenceParams{ChainOrgID: b.OrgID, ChainProjectID: b.ProjectID, ChainEnvID: b.EnvironmentID, SnapshotID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (r sqliteSelfConfigStorage) lockMembership(ctx context.Context) error {
	return r.q.LockSelfConfigMembership(ctx)
}
func (r pgSelfConfigStorage) lockMembership(ctx context.Context) error {
	return r.q.LockSelfConfigMembership(ctx)
}
func (r sqliteSelfConfigStorage) seedDisagreement(ctx context.Context, b SelfConfigBinding) (int64, error) {
	return r.q.CountSelfConfigSeedDisagreement(ctx, sqlitegen.CountSelfConfigSeedDisagreementParams{SinceAt: CanonTime(b.CreatedAt.Add(-30 * time.Second)).Format(timeFormat), SchemaVersion: b.SchemaVersion, Fingerprint: b.SeedFingerprint})
}
func (r pgSelfConfigStorage) seedDisagreement(ctx context.Context, b SelfConfigBinding) (int64, error) {
	return r.q.CountSelfConfigSeedDisagreement(ctx, pggen.CountSelfConfigSeedDisagreementParams{SinceAt: pgTimestamp(b.CreatedAt.Add(-30 * time.Second)), SchemaVersion: b.SchemaVersion, Fingerprint: b.SeedFingerprint})
}

func (r sqliteSelfConfigStorage) recover(ctx context.Context, expected, revision int64, id string, at time.Time) error {
	return affected(r.q.RecoverSelfConfigTarget(ctx, sqlitegen.RecoverSelfConfigTargetParams{SnapshotID: id, Revision: revision, ExpectedGeneration: expected, UpdatedAt: CanonTime(at).Format(timeFormat)}))
}
func (r pgSelfConfigStorage) recover(ctx context.Context, expected, revision int64, id string, at time.Time) error {
	return affected(r.q.RecoverSelfConfigTarget(ctx, pggen.RecoverSelfConfigTargetParams{SnapshotID: id, Revision: revision, ExpectedGeneration: expected, UpdatedAt: pgTimestamp(at)}))
}
