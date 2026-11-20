package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
)

type sqliteRotationRuntimeQueries struct{ q *sqlitegen.Queries }
type pgRotationRuntimeQueries struct{ q *pggen.Queries }

func (d sqliteAdoptDB) rotationRuntimeQueries() rotationRuntimeQueries {
	return sqliteRotationRuntimeQueries{q: sqlitegen.New(d.db)}
}
func (d pgAdoptDB) rotationRuntimeQueries() rotationRuntimeQueries {
	return pgRotationRuntimeQueries{q: pggen.New(d.db)}
}
func (d sqliteAdapterTx) rotationRuntimeQueries() rotationRuntimeQueries {
	return sqliteRotationRuntimeQueries{q: sqlitegen.New(d.tx)}
}
func (d pgAdapterTx) rotationRuntimeQueries() rotationRuntimeQueries {
	return pgRotationRuntimeQueries{q: pggen.New(d.tx)}
}

func sqlText(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
func pgTime(v pgtype.Timestamptz) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return v.Time.UTC()
}
func sqlTime(v sql.NullString) time.Time {
	if !v.Valid || v.String == "" {
		return time.Time{}
	}
	t, err := parseStamp(v.String)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
func sqlTimeReq(s string) time.Time { return sqlTime(sql.NullString{String: s, Valid: true}) }

// ---------------------------------------------------------------- postgres

func (q pgRotationRuntimeQueries) dueRotation(ctx context.Context, now time.Time) (DueRotation, error) {
	r, err := q.q.RotationRtDueRotation(ctx, pgRequiredTime(now))
	return DueRotation{ID: r.ID, OrgID: r.OrgID, ProjectID: r.ProjectID, EnvironmentID: r.EnvironmentID, ProviderID: r.ProviderID, KeyID: r.KeyID, UsernameKeyID: r.UsernameKeyID, IntervalSeconds: r.IntervalSeconds, RevocationDelaySeconds: r.RevocationDelaySeconds, AuthorityPrincipalID: r.AuthorityPrincipalID, Generation: r.Generation, ManualRunID: r.ManualRunID.String, ManualSkipOverlap: r.ManualSkipOverlap}, err
}

func (q pgRotationRuntimeQueries) lockDueRotation(ctx context.Context, d DueRotation, now time.Time) (DueRotation, error) {
	r, err := q.q.RotationRtLockDueRotation(ctx, pggen.RotationRtLockDueRotationParams{RotationID: d.ID, ChainOrg: d.OrgID, ChainProject: d.ProjectID, ChainEnv: d.EnvironmentID, ExpectedGeneration: d.Generation, Now: pgRequiredTime(now)})
	d.KeyID, d.UsernameKeyID, d.ProviderID = r.KeyID, r.UsernameKeyID, r.ProviderID
	d.ManualRunID, d.ManualSkipOverlap = r.ManualRunID.String, r.ManualSkipOverlap
	return d, err
}

func (q pgRotationRuntimeQueries) latestPublished(ctx context.Context, chain domain.Scope, keyID string) (string, time.Time, error) {
	r, err := q.q.RotationRtLatestPublished(ctx, pggen.RotationRtLatestPublishedParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID})
	if isNoRows(err) {
		return "", time.Time{}, nil
	}
	return r.CandidateHandle, pgTime(r.ValidUntilAt), err
}

func (q pgRotationRuntimeQueries) insertRun(ctx context.Context, d DueRotation, s StartRun, superseded string) error {
	return q.q.RotationRtInsertRun(ctx, pggen.RotationRtInsertRunParams{ID: s.RunID, ChainOrg: d.OrgID, ChainProject: d.ProjectID, ChainEnv: d.EnvironmentID, RotationID: d.ID, Generation: d.Generation, RunTrigger: s.Trigger, CandidateHandle: s.CandidateHandle, SupersededHandle: pgText(superseded), CandidateValidUntilAt: pgNullTimestamp(s.CandidateValidUntil), RevocationDelaySeconds: runDelay(d), At: pgRequiredTime(s.Now), Worker: pgText(s.Worker), LeaseUntilAt: pgRequiredTime(s.LeaseUntil)})
}

func (q pgRotationRuntimeQueries) setNextRun(ctx context.Context, c domain.Scope, rotationID string, next, now time.Time) (int64, error) {
	return q.q.RotationRtSetNextRun(ctx, pggen.RotationRtSetNextRunParams{NextRunAt: pgRequiredTime(next), Now: pgRequiredTime(now), RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
}

func (q pgRotationRuntimeQueries) deferNextRun(ctx context.Context, c domain.Scope, rotationID string, next, now time.Time) (int64, error) {
	return q.q.RotationRtDeferNextRun(ctx, pggen.RotationRtDeferNextRunParams{NextRunAt: pgRequiredTime(next), Now: pgRequiredTime(now), RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
}

func (q pgRotationRuntimeQueries) haltRotation(ctx context.Context, c domain.Scope, rotationID, reason string, now time.Time) (int64, error) {
	return q.q.RotationRtHaltRotation(ctx, pggen.RotationRtHaltRotationParams{HaltReason: pgText(reason), Now: pgRequiredTime(now), RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
}

func (q pgRotationRuntimeQueries) insertEffect(ctx context.Context, c domain.Scope, runID string, e RotationEffectIntent, at time.Time) error {
	return q.q.RotationRtInsertEffect(ctx, pggen.RotationRtInsertEffectParams{ID: e.EffectID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), RunID: runID, Kind: e.Kind, IntentAuditID: e.Audit.ID, At: pgRequiredTime(at)})
}

func (q pgRotationRuntimeQueries) closeEffect(ctx context.Context, c domain.Scope, runID string, o RotationEffectOutcome, at time.Time) error {
	return q.q.RotationRtCloseEffect(ctx, pggen.RotationRtCloseEffectParams{Outcome: pgText(o.Outcome), OutcomeAuditID: pgText(o.Audit.ID), At: pgRequiredTime(at), ID: o.EffectID, RunID: runID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
}

func (q pgRotationRuntimeQueries) insertAudit(ctx context.Context, c domain.Scope, authority, rotationID, runID string, a RotationAudit, at time.Time) error {
	return q.q.RotationRtInsertAudit(ctx, pggen.RotationRtInsertAuditParams{ID: a.ID, Type: a.Type, At: pgRequiredTime(at), AuthorityID: pgText(authority), ChainOrg: string(c.Org), ChainProject: pgText(string(c.Project)), ChainEnv: pgText(string(c.Env)), RotationID: pgText(rotationID), Outcome: a.Outcome, RunID: pgText(runID), Payload: string(a.Payload)})
}

func (q pgRotationRuntimeQueries) claimDueRun(ctx context.Context, now, leaseCut time.Time) (RotationRun, error) {
	r, err := q.q.RotationRtClaimDueRun(ctx, pggen.RotationRtClaimDueRunParams{Now: pgRequiredTime(now), LeaseCut: pgRequiredTime(leaseCut)})
	return RotationRun{ID: r.ID, OrgID: r.OrgID, ProjectID: r.ProjectID, EnvironmentID: r.EnvironmentID, RotationID: r.RotationID, RunGeneration: r.RunGeneration, Trigger: r.RunTrigger, Phase: r.Phase, CandidateHandle: r.CandidateHandle, SupersededHandle: r.SupersededHandle.String, CandidateValidUntil: pgTime(r.CandidateValidUntilAt), RevokeNotBefore: pgTime(r.RevokeNotBeforeAt), PublishedRevision: r.PublishedRevision.Int64, RevocationDelaySeconds: r.RevocationDelaySeconds, StartedAt: pgTime(r.StartedAt), PhaseAt: pgTime(r.PhaseAt), NextAttemptAt: pgTime(r.NextAttemptAt), AttemptCount: r.AttemptCount, LastError: r.LastError.String, ClaimToken: r.LeaseClaimToken, ProviderID: r.ProviderID, KeyID: r.KeyID, UsernameKeyID: r.UsernameKeyID, IntervalSeconds: r.IntervalSeconds, RotationDelaySeconds: r.RotationDelaySeconds, AuthorityPrincipalID: r.AuthorityPrincipalID, RotationState: heldState(r.RotationState, r.RotationRestoreHold), RotationGeneration: r.RotationGeneration}, err
}

func (q pgRotationRuntimeQueries) claimRun(ctx context.Context, run RotationRun, until time.Time) error {
	return q.q.RotationRtClaimRun(ctx, pggen.RotationRtClaimRunParams{Attempt: run.AttemptCount, Worker: pgText(run.LeaseOwner), UntilAt: pgRequiredTime(until), ClaimToken: run.ClaimToken, ID: run.ID, ChainOrg: run.OrgID, ChainProject: run.ProjectID, ChainEnv: run.EnvironmentID})
}

func (q pgRotationRuntimeQueries) assertRun(ctx context.Context, run RotationRun, now time.Time) (int64, error) {
	return q.q.RotationRtAssertRun(ctx, pggen.RotationRtAssertRunParams{ID: run.ID, ChainOrg: run.OrgID, ChainProject: run.ProjectID, ChainEnv: run.EnvironmentID, Worker: pgText(run.LeaseOwner), Now: pgRequiredTime(now), ClaimToken: run.ClaimToken})
}

func (q pgRotationRuntimeQueries) settleRun(ctx context.Context, run RotationRun, s RotationSettle) (int64, error) {
	keep := int64(0)
	if s.KeepLease {
		keep = 1
	}
	checked := int64(0)
	if s.RestoreChecked {
		checked = 1
	}
	return q.q.RotationRtSettleRun(ctx, pggen.RotationRtSettleRunParams{Phase: s.Phase, Now: pgRequiredTime(s.Now), LastError: pgText(s.LastError), NextAttemptAt: pgRequiredTime(s.NextAttempt), ValidUntilAt: pgNullTimestamp(s.ValidUntil), RevokeNotBeforeAt: pgNullTimestamp(s.RevokeNotBefore), PublishedRevision: pgtype.Int8{Int64: s.PublishedRevision, Valid: s.PublishedRevision != 0}, NotConvergedAdapterTargets: pgNullInt8(s.NotConverged), KeepLease: keep, RestoreChecked: checked, ID: run.ID, ChainOrg: run.OrgID, ChainProject: run.ProjectID, ChainEnv: run.EnvironmentID, Worker: pgText(run.LeaseOwner), ClaimToken: run.ClaimToken, FromPhases: s.FromPhases})
}

func (q pgRotationRuntimeQueries) getRun(ctx context.Context, c domain.Scope, runID string) (RotationRun, error) {
	r, err := q.q.RotationRtGetRun(ctx, pggen.RotationRtGetRunParams{RunID: runID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
	return RotationRun{ID: r.ID, OrgID: r.OrgID, ProjectID: r.ProjectID, EnvironmentID: r.EnvironmentID, RotationID: r.RotationID, RunGeneration: r.RunGeneration, Trigger: r.RunTrigger, Phase: r.Phase, CandidateHandle: r.CandidateHandle, SupersededHandle: r.SupersededHandle.String, CandidateValidUntil: pgTime(r.CandidateValidUntilAt), RevokeNotBefore: pgTime(r.RevokeNotBeforeAt), PublishedRevision: r.PublishedRevision.Int64, RevocationDelaySeconds: r.RevocationDelaySeconds, StartedAt: pgTime(r.StartedAt), PhaseAt: pgTime(r.PhaseAt), NextAttemptAt: pgTime(r.NextAttemptAt), AttemptCount: r.AttemptCount, LastError: r.LastError.String, ClaimToken: r.LeaseClaimToken, ProviderID: r.ProviderID, KeyID: r.KeyID, UsernameKeyID: r.UsernameKeyID, IntervalSeconds: r.IntervalSeconds, RotationDelaySeconds: r.RotationDelaySeconds, AuthorityPrincipalID: r.AuthorityPrincipalID, RotationState: heldState(r.RotationState, r.RotationRestoreHold), RotationGeneration: r.RotationGeneration}, err
}

func (q pgRotationRuntimeQueries) failedAttempts(ctx context.Context, c domain.Scope, rotationID string, generation int64, since time.Time) (int64, error) {
	return q.q.RotationRtFailedAttempts(ctx, pggen.RotationRtFailedAttemptsParams{RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), SinceAt: pgRequiredTime(since), Generation: generation})
}

func (q pgRotationRuntimeQueries) infraFaults(ctx context.Context, c domain.Scope, rotationID string, generation int64, since time.Time) (int64, error) {
	return q.q.RotationRtInfraFaults(ctx, pggen.RotationRtInfraFaultsParams{RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), SinceAt: pgRequiredTime(since), Generation: generation})
}

func (q pgRotationRuntimeQueries) publishedAt(ctx context.Context, c domain.Scope, revision int64) (time.Time, error) {
	v, err := q.q.RotationRtSnapshotPublishedAt(ctx, pggen.RotationRtSnapshotPublishedAtParams{ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), Revision: revision})
	return pgTime(v), err
}

func (q pgRotationRuntimeQueries) currentEntry(ctx context.Context, c domain.Scope, keyID string) (string, error) {
	return q.q.RotationRtCurrentEntry(ctx, pggen.RotationRtCurrentEntryParams{ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), KeyID: keyID})
}

func (q pgRotationRuntimeQueries) publishedEntries(ctx context.Context, c domain.Scope, keyID string, from int64) ([]string, error) {
	return q.q.RotationRtPublishedEntries(ctx, pggen.RotationRtPublishedEntriesParams{ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), KeyID: keyID, FromRevision: pgtype.Int8{Int64: from, Valid: true}})
}

func (q pgRotationRuntimeQueries) laterPublished(ctx context.Context, c domain.Scope, keyID string, revision int64) (int64, error) {
	return q.q.RotationRtLaterPublished(ctx, pggen.RotationRtLaterPublishedParams{ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), KeyID: keyID, Revision: pgtype.Int8{Int64: revision, Valid: true}})
}

func (q pgRotationRuntimeQueries) restoreHeld(ctx context.Context, c domain.Scope, rotationID string) (int64, error) {
	return q.q.RotationRtRestoreHeld(ctx, pggen.RotationRtRestoreHeldParams{RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
}

func (q pgRotationRuntimeQueries) dueRun(ctx context.Context, run RotationRun, now time.Time) error {
	_, err := q.q.RotationRtDueRun(ctx, pggen.RotationRtDueRunParams{Now: pgRequiredTime(now), ID: run.ID, ChainOrg: run.OrgID, ChainProject: run.ProjectID, ChainEnv: run.EnvironmentID})
	return err
}

func (q pgRotationRuntimeQueries) providerMaterial(ctx context.Context, run RotationRun, now time.Time) (LeaseProviderMaterial, error) {
	c, err := q.q.RotationRtLoadProviderMaterial(ctx, pggen.RotationRtLoadProviderMaterialParams{RunID: run.ID, ChainOrg: run.OrgID, ChainProject: run.ProjectID, ChainEnv: run.EnvironmentID, Worker: pgText(run.LeaseOwner), Now: pgRequiredTime(now), ClaimToken: run.ClaimToken})
	return LeaseProviderMaterial{Kind: c.Kind, Origin: c.Origin, TLSMode: c.TlsMode, GrantRole: c.GrantRole, CredentialOwnerID: c.ID, CredentialCiphertext: append([]byte(nil), c.AdminCredentialCiphertext...)}, err
}

func (q pgRotationRuntimeQueries) supersededRole(ctx context.Context, run RotationRun, now time.Time) (SupersededRole, error) {
	c, err := q.q.RotationRtLoadSupersededRole(ctx, pggen.RotationRtLoadSupersededRoleParams{RunID: run.ID, ChainOrg: run.OrgID, ChainProject: run.ProjectID, ChainEnv: run.EnvironmentID, Worker: pgText(run.LeaseOwner), Now: pgRequiredTime(now), ClaimToken: run.ClaimToken})
	return SupersededRole{
		Material:   LeaseProviderMaterial{Kind: c.Kind, Origin: c.Origin, TLSMode: c.TlsMode, GrantRole: c.GrantRole, CredentialOwnerID: c.ID, CredentialCiphertext: append([]byte(nil), c.AdminCredentialCiphertext...)},
		ValidUntil: pgTime(c.ValidUntilAt), ExtendUncertain: c.ExtendUncertain > 0,
	}, err
}

func (q pgRotationRuntimeQueries) openRunCount(ctx context.Context, c domain.Scope, rotationID string) (int64, error) {
	return q.q.RotationRtOpenRunCount(ctx, pggen.RotationRtOpenRunCountParams{RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
}

func (q pgRotationRuntimeQueries) orgLeaseCount(ctx context.Context, org domain.OrgID, now time.Time) (int64, error) {
	return q.q.RotationRtOrgLeaseCount(ctx, pggen.RotationRtOrgLeaseCountParams{ChainOrg: string(org), Now: pgRequiredTime(now)})
}

func (q pgRotationRuntimeQueries) openEffect(ctx context.Context, c domain.Scope, runID string) (string, string, error) {
	r, err := q.q.RotationRtOpenEffect(ctx, pggen.RotationRtOpenEffectParams{RunID: runID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
	return r.ID, r.Kind, err
}

func (q pgRotationRuntimeQueries) clearManual(ctx context.Context, c domain.Scope, rotationID string) error {
	return q.q.RotationRtClearManual(ctx, pggen.RotationRtClearManualParams{RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
}

func (q pgRotationRuntimeQueries) notConverged(ctx context.Context, run RotationRun) (int64, error) {
	return q.q.RotationRtNotConverged(ctx, pggen.RotationRtNotConvergedParams{ChainOrg: run.OrgID, ChainProject: run.ProjectID, ChainEnv: run.EnvironmentID, KeyID: run.KeyID, UsernameKeyID: run.UsernameKeyID, Revision: pgtype.Int8{Int64: run.PublishedRevision, Valid: true}})
}

// runDelay is the revocation delay a run snapshots: zero for a run-now that skips
// the overlap, the rotation's own delay otherwise.
func runDelay(d DueRotation) int64 {
	if d.ManualSkipOverlap {
		return 0
	}
	return d.RevocationDelaySeconds
}

func pgNullInt8(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

// ------------------------------------------------------------------ sqlite

// sqlNullInt is the SQLite nullable integer of an optional value; the adapter
// store's observed provider version uses it too.
func sqlNullInt(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func (q sqliteRotationRuntimeQueries) dueRotation(ctx context.Context, now time.Time) (DueRotation, error) {
	r, err := q.q.RotationRtDueRotation(ctx, sql.NullString{String: fixedStamp(now), Valid: true})
	return DueRotation{ID: r.ID, OrgID: r.OrgID, ProjectID: r.ProjectID, EnvironmentID: r.EnvironmentID, ProviderID: r.ProviderID, KeyID: r.KeyID, UsernameKeyID: r.UsernameKeyID, IntervalSeconds: r.IntervalSeconds, RevocationDelaySeconds: r.RevocationDelaySeconds, AuthorityPrincipalID: r.AuthorityPrincipalID, Generation: r.Generation, ManualRunID: r.ManualRunID.String, ManualSkipOverlap: r.ManualSkipOverlap != 0}, err
}

func (q sqliteRotationRuntimeQueries) lockDueRotation(ctx context.Context, d DueRotation, now time.Time) (DueRotation, error) {
	r, err := q.q.RotationRtLockDueRotation(ctx, sqlitegen.RotationRtLockDueRotationParams{RotationID: d.ID, ChainOrg: d.OrgID, ChainProject: d.ProjectID, ChainEnv: d.EnvironmentID, ExpectedGeneration: d.Generation, Now: sql.NullString{String: fixedStamp(now), Valid: true}})
	d.KeyID, d.UsernameKeyID, d.ProviderID = r.KeyID, r.UsernameKeyID, r.ProviderID
	d.ManualRunID, d.ManualSkipOverlap = r.ManualRunID.String, r.ManualSkipOverlap != 0
	return d, err
}

func (q sqliteRotationRuntimeQueries) latestPublished(ctx context.Context, chain domain.Scope, keyID string) (string, time.Time, error) {
	r, err := q.q.RotationRtLatestPublished(ctx, sqlitegen.RotationRtLatestPublishedParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), KeyID: keyID})
	if isNoRows(err) {
		return "", time.Time{}, nil
	}
	return r.CandidateHandle, sqlTime(r.ValidUntilAt), err
}

func (q sqliteRotationRuntimeQueries) insertRun(ctx context.Context, d DueRotation, s StartRun, superseded string) error {
	return q.q.RotationRtInsertRun(ctx, sqlitegen.RotationRtInsertRunParams{ID: s.RunID, ChainOrg: d.OrgID, ChainProject: d.ProjectID, ChainEnv: d.EnvironmentID, RotationID: d.ID, Generation: d.Generation, RunTrigger: s.Trigger, CandidateHandle: s.CandidateHandle, SupersededHandle: sqlText(superseded), CandidateValidUntilAt: nullTimeString(s.CandidateValidUntil), RevocationDelaySeconds: runDelay(d), At: fixedStamp(s.Now), Worker: sqlText(s.Worker), LeaseUntilAt: sql.NullString{String: fixedStamp(s.LeaseUntil), Valid: true}})
}

func (q sqliteRotationRuntimeQueries) setNextRun(ctx context.Context, c domain.Scope, rotationID string, next, now time.Time) (int64, error) {
	return q.q.RotationRtSetNextRun(ctx, sqlitegen.RotationRtSetNextRunParams{NextRunAt: sql.NullString{String: fixedStamp(next), Valid: true}, Now: fixedStamp(now), RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
}

func (q sqliteRotationRuntimeQueries) deferNextRun(ctx context.Context, c domain.Scope, rotationID string, next, now time.Time) (int64, error) {
	return q.q.RotationRtDeferNextRun(ctx, sqlitegen.RotationRtDeferNextRunParams{NextRunAt: sql.NullString{String: fixedStamp(next), Valid: true}, Now: fixedStamp(now), RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
}

func (q sqliteRotationRuntimeQueries) haltRotation(ctx context.Context, c domain.Scope, rotationID, reason string, now time.Time) (int64, error) {
	return q.q.RotationRtHaltRotation(ctx, sqlitegen.RotationRtHaltRotationParams{HaltReason: sqlText(reason), Now: fixedStamp(now), RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
}

func (q sqliteRotationRuntimeQueries) insertEffect(ctx context.Context, c domain.Scope, runID string, e RotationEffectIntent, at time.Time) error {
	return q.q.RotationRtInsertEffect(ctx, sqlitegen.RotationRtInsertEffectParams{ID: e.EffectID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), RunID: runID, Kind: e.Kind, IntentAuditID: e.Audit.ID, At: fixedStamp(at)})
}

func (q sqliteRotationRuntimeQueries) closeEffect(ctx context.Context, c domain.Scope, runID string, o RotationEffectOutcome, at time.Time) error {
	return q.q.RotationRtCloseEffect(ctx, sqlitegen.RotationRtCloseEffectParams{Outcome: sqlText(o.Outcome), OutcomeAuditID: sqlText(o.Audit.ID), At: sql.NullString{String: fixedStamp(at), Valid: true}, ID: o.EffectID, RunID: runID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
}

func (q sqliteRotationRuntimeQueries) insertAudit(ctx context.Context, c domain.Scope, authority, rotationID, runID string, a RotationAudit, at time.Time) error {
	return q.q.RotationRtInsertAudit(ctx, sqlitegen.RotationRtInsertAuditParams{ID: a.ID, Type: a.Type, At: fixedStamp(at), AuthorityID: sqlText(authority), ChainOrg: string(c.Org), ChainProject: sqlText(string(c.Project)), ChainEnv: sqlText(string(c.Env)), RotationID: sqlText(rotationID), Outcome: a.Outcome, RunID: sqlText(runID), Payload: string(a.Payload)})
}

func (q sqliteRotationRuntimeQueries) claimDueRun(ctx context.Context, now, leaseCut time.Time) (RotationRun, error) {
	r, err := q.q.RotationRtClaimDueRun(ctx, sqlitegen.RotationRtClaimDueRunParams{Now: fixedStamp(now), LeaseCut: fixedStamp(leaseCut)})
	return RotationRun{ID: r.ID, OrgID: r.OrgID, ProjectID: r.ProjectID, EnvironmentID: r.EnvironmentID, RotationID: r.RotationID, RunGeneration: r.RunGeneration, Trigger: r.RunTrigger, Phase: r.Phase, CandidateHandle: r.CandidateHandle, SupersededHandle: r.SupersededHandle.String, CandidateValidUntil: sqlTime(r.CandidateValidUntilAt), RevokeNotBefore: sqlTime(r.RevokeNotBeforeAt), PublishedRevision: r.PublishedRevision.Int64, RevocationDelaySeconds: r.RevocationDelaySeconds, StartedAt: sqlTimeReq(r.StartedAt), PhaseAt: sqlTimeReq(r.PhaseAt), NextAttemptAt: sqlTimeReq(r.NextAttemptAt), AttemptCount: r.AttemptCount, LastError: r.LastError.String, ClaimToken: r.LeaseClaimToken, ProviderID: r.ProviderID, KeyID: r.KeyID, UsernameKeyID: r.UsernameKeyID, IntervalSeconds: r.IntervalSeconds, RotationDelaySeconds: r.RotationDelaySeconds, AuthorityPrincipalID: r.AuthorityPrincipalID, RotationState: heldState(r.RotationState, r.RotationRestoreHold != 0), RotationGeneration: r.RotationGeneration}, err
}

func (q sqliteRotationRuntimeQueries) claimRun(ctx context.Context, run RotationRun, until time.Time) error {
	return q.q.RotationRtClaimRun(ctx, sqlitegen.RotationRtClaimRunParams{Attempt: run.AttemptCount, Worker: sqlText(run.LeaseOwner), UntilAt: sql.NullString{String: fixedStamp(until), Valid: true}, ClaimToken: run.ClaimToken, ID: run.ID, ChainOrg: run.OrgID, ChainProject: run.ProjectID, ChainEnv: run.EnvironmentID})
}

func (q sqliteRotationRuntimeQueries) assertRun(ctx context.Context, run RotationRun, now time.Time) (int64, error) {
	return q.q.RotationRtAssertRun(ctx, sqlitegen.RotationRtAssertRunParams{ID: run.ID, ChainOrg: run.OrgID, ChainProject: run.ProjectID, ChainEnv: run.EnvironmentID, Worker: sqlText(run.LeaseOwner), Now: sql.NullString{String: fixedStamp(now), Valid: true}, ClaimToken: run.ClaimToken})
}

func (q sqliteRotationRuntimeQueries) settleRun(ctx context.Context, run RotationRun, s RotationSettle) (int64, error) {
	keep := int64(0)
	if s.KeepLease {
		keep = 1
	}
	checked := int64(0)
	if s.RestoreChecked {
		checked = 1
	}
	return q.q.RotationRtSettleRun(ctx, sqlitegen.RotationRtSettleRunParams{Phase: s.Phase, Now: fixedStamp(s.Now), LastError: sqlText(s.LastError), NextAttemptAt: fixedStamp(s.NextAttempt), ValidUntilAt: nullTimeString(s.ValidUntil), RevokeNotBeforeAt: nullTimeString(s.RevokeNotBefore), PublishedRevision: sql.NullInt64{Int64: s.PublishedRevision, Valid: s.PublishedRevision != 0}, NotConvergedAdapterTargets: sqlNullInt(s.NotConverged), KeepLease: keep, RestoreChecked: checked, ID: run.ID, ChainOrg: run.OrgID, ChainProject: run.ProjectID, ChainEnv: run.EnvironmentID, Worker: sqlText(run.LeaseOwner), ClaimToken: run.ClaimToken, FromPhases: s.FromPhases})
}

func (q sqliteRotationRuntimeQueries) getRun(ctx context.Context, c domain.Scope, runID string) (RotationRun, error) {
	r, err := q.q.RotationRtGetRun(ctx, sqlitegen.RotationRtGetRunParams{RunID: runID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
	return RotationRun{ID: r.ID, OrgID: r.OrgID, ProjectID: r.ProjectID, EnvironmentID: r.EnvironmentID, RotationID: r.RotationID, RunGeneration: r.RunGeneration, Trigger: r.RunTrigger, Phase: r.Phase, CandidateHandle: r.CandidateHandle, SupersededHandle: r.SupersededHandle.String, CandidateValidUntil: sqlTime(r.CandidateValidUntilAt), RevokeNotBefore: sqlTime(r.RevokeNotBeforeAt), PublishedRevision: r.PublishedRevision.Int64, RevocationDelaySeconds: r.RevocationDelaySeconds, StartedAt: sqlTimeReq(r.StartedAt), PhaseAt: sqlTimeReq(r.PhaseAt), NextAttemptAt: sqlTimeReq(r.NextAttemptAt), AttemptCount: r.AttemptCount, LastError: r.LastError.String, ClaimToken: r.LeaseClaimToken, ProviderID: r.ProviderID, KeyID: r.KeyID, UsernameKeyID: r.UsernameKeyID, IntervalSeconds: r.IntervalSeconds, RotationDelaySeconds: r.RotationDelaySeconds, AuthorityPrincipalID: r.AuthorityPrincipalID, RotationState: heldState(r.RotationState, r.RotationRestoreHold != 0), RotationGeneration: r.RotationGeneration}, err
}

func (q sqliteRotationRuntimeQueries) failedAttempts(ctx context.Context, c domain.Scope, rotationID string, generation int64, since time.Time) (int64, error) {
	return q.q.RotationRtFailedAttempts(ctx, sqlitegen.RotationRtFailedAttemptsParams{RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), SinceAt: fixedStamp(since), Generation: generation})
}

func (q sqliteRotationRuntimeQueries) infraFaults(ctx context.Context, c domain.Scope, rotationID string, generation int64, since time.Time) (int64, error) {
	return q.q.RotationRtInfraFaults(ctx, sqlitegen.RotationRtInfraFaultsParams{RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), SinceAt: fixedStamp(since), Generation: generation})
}

func (q sqliteRotationRuntimeQueries) publishedAt(ctx context.Context, c domain.Scope, revision int64) (time.Time, error) {
	v, err := q.q.RotationRtSnapshotPublishedAt(ctx, sqlitegen.RotationRtSnapshotPublishedAtParams{ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), Revision: revision})
	return sqlTimeReq(v), err
}

func (q sqliteRotationRuntimeQueries) currentEntry(ctx context.Context, c domain.Scope, keyID string) (string, error) {
	return q.q.RotationRtCurrentEntry(ctx, sqlitegen.RotationRtCurrentEntryParams{ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), KeyID: keyID})
}

func (q sqliteRotationRuntimeQueries) publishedEntries(ctx context.Context, c domain.Scope, keyID string, from int64) ([]string, error) {
	return q.q.RotationRtPublishedEntries(ctx, sqlitegen.RotationRtPublishedEntriesParams{ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), KeyID: keyID, FromRevision: sql.NullInt64{Int64: from, Valid: true}})
}

func (q sqliteRotationRuntimeQueries) laterPublished(ctx context.Context, c domain.Scope, keyID string, revision int64) (int64, error) {
	return q.q.RotationRtLaterPublished(ctx, sqlitegen.RotationRtLaterPublishedParams{ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), KeyID: keyID, Revision: sql.NullInt64{Int64: revision, Valid: true}})
}

func (q sqliteRotationRuntimeQueries) restoreHeld(ctx context.Context, c domain.Scope, rotationID string) (int64, error) {
	return q.q.RotationRtRestoreHeld(ctx, sqlitegen.RotationRtRestoreHeldParams{RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
}

func (q sqliteRotationRuntimeQueries) dueRun(ctx context.Context, run RotationRun, now time.Time) error {
	_, err := q.q.RotationRtDueRun(ctx, sqlitegen.RotationRtDueRunParams{Now: fixedStamp(now), ID: run.ID, ChainOrg: run.OrgID, ChainProject: run.ProjectID, ChainEnv: run.EnvironmentID})
	return err
}

func (q sqliteRotationRuntimeQueries) providerMaterial(ctx context.Context, run RotationRun, now time.Time) (LeaseProviderMaterial, error) {
	c, err := q.q.RotationRtLoadProviderMaterial(ctx, sqlitegen.RotationRtLoadProviderMaterialParams{RunID: run.ID, ChainOrg: run.OrgID, ChainProject: run.ProjectID, ChainEnv: run.EnvironmentID, Worker: sqlText(run.LeaseOwner), Now: sql.NullString{String: fixedStamp(now), Valid: true}, ClaimToken: run.ClaimToken})
	return LeaseProviderMaterial{Kind: c.Kind, Origin: c.Origin, TLSMode: c.TlsMode, GrantRole: c.GrantRole, CredentialOwnerID: c.ID, CredentialCiphertext: append([]byte(nil), c.AdminCredentialCiphertext...)}, err
}

func (q sqliteRotationRuntimeQueries) supersededRole(ctx context.Context, run RotationRun, now time.Time) (SupersededRole, error) {
	c, err := q.q.RotationRtLoadSupersededRole(ctx, sqlitegen.RotationRtLoadSupersededRoleParams{RunID: run.ID, ChainOrg: run.OrgID, ChainProject: run.ProjectID, ChainEnv: run.EnvironmentID, Worker: sqlText(run.LeaseOwner), Now: sql.NullString{String: fixedStamp(now), Valid: true}, ClaimToken: run.ClaimToken})
	return SupersededRole{
		Material:   LeaseProviderMaterial{Kind: c.Kind, Origin: c.Origin, TLSMode: c.TlsMode, GrantRole: c.GrantRole, CredentialOwnerID: c.ID, CredentialCiphertext: append([]byte(nil), c.AdminCredentialCiphertext...)},
		ValidUntil: sqlTime(c.ValidUntilAt), ExtendUncertain: c.ExtendUncertain > 0,
	}, err
}

func (q sqliteRotationRuntimeQueries) openRunCount(ctx context.Context, c domain.Scope, rotationID string) (int64, error) {
	return q.q.RotationRtOpenRunCount(ctx, sqlitegen.RotationRtOpenRunCountParams{RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
}

func (q sqliteRotationRuntimeQueries) orgLeaseCount(ctx context.Context, org domain.OrgID, now time.Time) (int64, error) {
	return q.q.RotationRtOrgLeaseCount(ctx, sqlitegen.RotationRtOrgLeaseCountParams{ChainOrg: string(org), Now: sql.NullString{String: fixedStamp(now), Valid: true}})
}

func (q sqliteRotationRuntimeQueries) openEffect(ctx context.Context, c domain.Scope, runID string) (string, string, error) {
	r, err := q.q.RotationRtOpenEffect(ctx, sqlitegen.RotationRtOpenEffectParams{RunID: runID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
	return r.ID, r.Kind, err
}

func (q pgRotationRuntimeQueries) ownPublished(ctx context.Context, c domain.Scope, rotationID string, generation int64) (int64, error) {
	v, err := q.q.RotationRtOwnPublished(ctx, pggen.RotationRtOwnPublishedParams{RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), Generation: generation})
	return v.Int64, err
}

func (q pgRotationRuntimeQueries) cycleStart(ctx context.Context, c domain.Scope, rotationID string, generation int64) (time.Time, error) {
	v, err := q.q.RotationRtCycleStart(ctx, pggen.RotationRtCycleStartParams{RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), Generation: generation})
	return pgTime(v), err
}

func (q sqliteRotationRuntimeQueries) ownPublished(ctx context.Context, c domain.Scope, rotationID string, generation int64) (int64, error) {
	v, err := q.q.RotationRtOwnPublished(ctx, sqlitegen.RotationRtOwnPublishedParams{RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), Generation: generation})
	return v.Int64, err
}

func (q sqliteRotationRuntimeQueries) cycleStart(ctx context.Context, c domain.Scope, rotationID string, generation int64) (time.Time, error) {
	v, err := q.q.RotationRtCycleStart(ctx, sqlitegen.RotationRtCycleStartParams{RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env), Generation: generation})
	return sqlTimeReq(v), err
}

func (q sqliteRotationRuntimeQueries) clearManual(ctx context.Context, c domain.Scope, rotationID string) error {
	return q.q.RotationRtClearManual(ctx, sqlitegen.RotationRtClearManualParams{RotationID: rotationID, ChainOrg: string(c.Org), ChainProject: string(c.Project), ChainEnv: string(c.Env)})
}

func (q sqliteRotationRuntimeQueries) notConverged(ctx context.Context, run RotationRun) (int64, error) {
	return q.q.RotationRtNotConverged(ctx, sqlitegen.RotationRtNotConvergedParams{ChainOrg: run.OrgID, ChainProject: run.ProjectID, ChainEnv: run.EnvironmentID, KeyID: run.KeyID, UsernameKeyID: run.UsernameKeyID, Revision: sql.NullInt64{Int64: run.PublishedRevision, Valid: true}})
}

func (q pgRotationRuntimeQueries) gauges(ctx context.Context, now time.Time) (RotationGauges, error) {
	r, err := q.q.RotationRtGauges(ctx, pggen.RotationRtGaugesParams{OverdueAt: pgRequiredTime(now.Add(-RotationOverdueGrace)), NowAt: pgRequiredTime(now), ExpiringAt: pgRequiredTime(now.Add(RotationExpiringWithin))})
	return RotationGauges{Quarantined: r.Quarantined, RevokeRefused: r.RevokeRefused, UnknownOutcome: r.UnknownOutcome, Halted: r.Halted, RestoreHeld: r.RestoreHeld, Overdue: r.Overdue, Expiring: r.Expiring}, err
}

func (q sqliteRotationRuntimeQueries) gauges(ctx context.Context, now time.Time) (RotationGauges, error) {
	r, err := q.q.RotationRtGauges(ctx, sqlitegen.RotationRtGaugesParams{OverdueAt: fixedStamp(now.Add(-RotationOverdueGrace)), NowAt: fixedStamp(now), ExpiringAt: fixedStamp(now.Add(RotationExpiringWithin))})
	return RotationGauges{Quarantined: r.Quarantined, RevokeRefused: r.RevokeRefused, UnknownOutcome: r.UnknownOutcome, Halted: r.Halted, RestoreHeld: r.RestoreHeld, Overdue: r.Overdue, Expiring: r.Expiring}, err
}

// heldState is the state the executor judges: a rotation held by a restore is
// not active, so every state gate that pauses a run (no new start, no pre-publish
// step, no extend) holds it too, while the post-publish revoke goes on.
func heldState(state string, held bool) string {
	if held {
		return RotationStateRestoreHeld
	}
	return state
}
