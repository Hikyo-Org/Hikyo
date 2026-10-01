package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
)

type dynamicRuntimeQueries interface {
	dynamicClaimLease(ctx context.Context, lease ClaimedLease, until time.Time) error
	dynamicLatestEffectKind(ctx context.Context, lease ClaimedLease) (string, error)
	dynamicInsertEffect(ctx context.Context, lease ClaimedLease, id, kind, auditID string, at time.Time) error
	dynamicCloseEffect(ctx context.Context, lease ClaimedLease, id, outcome, auditID string, at time.Time) error
	dynamicSettleLease(ctx context.Context, lease ClaimedLease, state string, issuedAt, expiresAt, now, nextAttempt time.Time) (int64, error)
	dynamicRetryLease(ctx context.Context, lease ClaimedLease, nextAttempt, now time.Time) (int64, error)
	dynamicAssertLease(ctx context.Context, lease ClaimedLease, now time.Time) (int64, error)
	dynamicInsertLeaseAudit(ctx context.Context, lease ClaimedLease, id, typ, outcome string, at time.Time, payload []byte) error
	dynamicClaimDueLease(ctx context.Context, now time.Time) (ClaimedLease, error)
	dynamicLoadProviderMaterial(ctx context.Context, lease ClaimedLease, now time.Time) (LeaseProviderMaterial, error)
	dynamicGauges(ctx context.Context) (active, unknown int64, err error)
}

type sqliteDynamicRuntimeQueries struct{ queries *sqlitegen.Queries }
type pgDynamicRuntimeQueries struct{ queries *pggen.Queries }

func (q pgDynamicRuntimeQueries) dynamicClaimLease(ctx context.Context, lease ClaimedLease, until time.Time) error {
	attempt, err := checkedPGInt32(int64(lease.Attempt))
	if err != nil {
		return err
	}

	return q.queries.RuntimeDynamicClaimLease(ctx, pggen.RuntimeDynamicClaimLeaseParams{ChainOrg: lease.OrgID, ChainProject: lease.ProjectID, ChainEnv: lease.EnvironmentID, Attempt: attempt, Worker: pgtype.Text{String: lease.LeaseOwner, Valid: true}, Until: pgRequiredTime(until), ClaimToken: lease.ClaimToken, ID: lease.ID})
}

func (q sqliteDynamicRuntimeQueries) dynamicClaimLease(ctx context.Context, lease ClaimedLease, until time.Time) error {

	return q.queries.RuntimeDynamicClaimLease(ctx, sqlitegen.RuntimeDynamicClaimLeaseParams{ChainOrg: lease.OrgID, ChainProject: lease.ProjectID, ChainEnv: lease.EnvironmentID, Attempt: int64(lease.Attempt), Worker: sql.NullString{String: lease.LeaseOwner, Valid: true}, Until: runtimeSQLiteStamp(until), ClaimToken: lease.ClaimToken, ID: lease.ID})
}

func (q pgDynamicRuntimeQueries) dynamicLatestEffectKind(ctx context.Context, lease ClaimedLease) (string, error) {

	return q.queries.RuntimeDynamicLatestEffectKind(ctx, pggen.RuntimeDynamicLatestEffectKindParams{ChainProject: lease.ProjectID, ChainEnv: lease.EnvironmentID, LeaseID: lease.ID, ChainOrg: lease.OrgID})
}

func (q sqliteDynamicRuntimeQueries) dynamicLatestEffectKind(ctx context.Context, lease ClaimedLease) (string, error) {

	return q.queries.RuntimeDynamicLatestEffectKind(ctx, sqlitegen.RuntimeDynamicLatestEffectKindParams{ChainProject: lease.ProjectID, ChainEnv: lease.EnvironmentID, LeaseID: lease.ID, ChainOrg: lease.OrgID})
}

func (q pgDynamicRuntimeQueries) dynamicInsertEffect(ctx context.Context, lease ClaimedLease, id, kind, auditID string, at time.Time) error {

	return q.queries.RuntimeDynamicInsertEffect(ctx, pggen.RuntimeDynamicInsertEffectParams{ID: id, ChainOrg: lease.OrgID, ChainProject: lease.ProjectID, ChainEnv: lease.EnvironmentID, LeaseID: lease.ID, Kind: kind, IntentAuditID: auditID, At: pgRequiredTime(at)})
}

func (q sqliteDynamicRuntimeQueries) dynamicInsertEffect(ctx context.Context, lease ClaimedLease, id, kind, auditID string, at time.Time) error {

	return q.queries.RuntimeDynamicInsertEffect(ctx, sqlitegen.RuntimeDynamicInsertEffectParams{ID: id, ChainOrg: lease.OrgID, ChainProject: lease.ProjectID, ChainEnv: lease.EnvironmentID, LeaseID: lease.ID, Kind: kind, IntentAuditID: auditID, At: fixedStamp(at)})
}

func (q pgDynamicRuntimeQueries) dynamicCloseEffect(ctx context.Context, lease ClaimedLease, id, outcome, auditID string, at time.Time) error {

	return q.queries.RuntimeDynamicCloseEffect(ctx, pggen.RuntimeDynamicCloseEffectParams{ChainProject: lease.ProjectID, ChainEnv: lease.EnvironmentID, Outcome: pgtype.Text{String: outcome, Valid: true}, OutcomeAuditID: pgtype.Text{String: auditID, Valid: true}, At: pgRequiredTime(at), ID: id, ChainOrg: lease.OrgID})
}

func (q sqliteDynamicRuntimeQueries) dynamicCloseEffect(ctx context.Context, lease ClaimedLease, id, outcome, auditID string, at time.Time) error {

	return q.queries.RuntimeDynamicCloseEffect(ctx, sqlitegen.RuntimeDynamicCloseEffectParams{ChainProject: lease.ProjectID, ChainEnv: lease.EnvironmentID, Outcome: sql.NullString{String: outcome, Valid: true}, OutcomeAuditID: sql.NullString{String: auditID, Valid: true}, At: runtimeSQLiteStamp(at), ID: id, ChainOrg: lease.OrgID})
}

func (q pgDynamicRuntimeQueries) dynamicSettleLease(ctx context.Context, lease ClaimedLease, state string, issuedAt, expiresAt, now, nextAttempt time.Time) (int64, error) {

	return q.queries.RuntimeDynamicSettleLease(ctx, pggen.RuntimeDynamicSettleLeaseParams{ChainProject: lease.ProjectID, ChainEnv: lease.EnvironmentID, State: state, IssuedAt: pgNullTimestamp(issuedAt), ExpiresAt: pgNullTimestamp(expiresAt), Now: pgRequiredTime(now), NextAttempt: pgRequiredTime(nextAttempt), ID: lease.ID, ChainOrg: lease.OrgID, Worker: pgtype.Text{String: lease.LeaseOwner, Valid: true}, ClaimToken: lease.ClaimToken})
}

func (q sqliteDynamicRuntimeQueries) dynamicSettleLease(ctx context.Context, lease ClaimedLease, state string, issuedAt, expiresAt, now, nextAttempt time.Time) (int64, error) {

	return q.queries.RuntimeDynamicSettleLease(ctx, sqlitegen.RuntimeDynamicSettleLeaseParams{ChainProject: lease.ProjectID, ChainEnv: lease.EnvironmentID, State: state, IssuedAt: nullTimeString(issuedAt), ExpiresAt: nullTimeString(expiresAt), Now: fixedStamp(now), NextAttempt: fixedStamp(nextAttempt), ID: lease.ID, ChainOrg: lease.OrgID, Worker: sql.NullString{String: lease.LeaseOwner, Valid: true}, ClaimToken: lease.ClaimToken})
}

func (q pgDynamicRuntimeQueries) dynamicRetryLease(ctx context.Context, lease ClaimedLease, nextAttempt, now time.Time) (int64, error) {

	return q.queries.RuntimeDynamicRetryLease(ctx, pggen.RuntimeDynamicRetryLeaseParams{ChainProject: lease.ProjectID, ChainEnv: lease.EnvironmentID, NextAttempt: pgRequiredTime(nextAttempt), ID: lease.ID, ChainOrg: lease.OrgID, Worker: pgtype.Text{String: lease.LeaseOwner, Valid: true}, Now: pgRequiredTime(now), ClaimToken: lease.ClaimToken})
}

func (q sqliteDynamicRuntimeQueries) dynamicRetryLease(ctx context.Context, lease ClaimedLease, nextAttempt, now time.Time) (int64, error) {

	return q.queries.RuntimeDynamicRetryLease(ctx, sqlitegen.RuntimeDynamicRetryLeaseParams{ChainProject: lease.ProjectID, ChainEnv: lease.EnvironmentID, NextAttempt: fixedStamp(nextAttempt), ID: lease.ID, ChainOrg: lease.OrgID, Worker: sql.NullString{String: lease.LeaseOwner, Valid: true}, Now: runtimeSQLiteStamp(now), ClaimToken: lease.ClaimToken})
}

func (q pgDynamicRuntimeQueries) dynamicAssertLease(ctx context.Context, lease ClaimedLease, now time.Time) (int64, error) {

	return q.queries.RuntimeDynamicAssertLease(ctx, pggen.RuntimeDynamicAssertLeaseParams{ChainProject: lease.ProjectID, ChainEnv: lease.EnvironmentID, ID: lease.ID, ChainOrg: lease.OrgID, Worker: pgtype.Text{String: lease.LeaseOwner, Valid: true}, Now: pgRequiredTime(now), ClaimToken: lease.ClaimToken})
}

func (q sqliteDynamicRuntimeQueries) dynamicAssertLease(ctx context.Context, lease ClaimedLease, now time.Time) (int64, error) {

	return q.queries.RuntimeDynamicAssertLease(ctx, sqlitegen.RuntimeDynamicAssertLeaseParams{ChainProject: lease.ProjectID, ChainEnv: lease.EnvironmentID, ID: lease.ID, ChainOrg: lease.OrgID, Worker: sql.NullString{String: lease.LeaseOwner, Valid: true}, Now: runtimeSQLiteStamp(now), ClaimToken: lease.ClaimToken})
}

func (q pgDynamicRuntimeQueries) dynamicInsertLeaseAudit(ctx context.Context, lease ClaimedLease, id, typ, outcome string, at time.Time, payload []byte) error {

	return q.queries.RuntimeDynamicInsertLeaseAudit(ctx, pggen.RuntimeDynamicInsertLeaseAuditParams{ID: id, Type: typ, At: pgRequiredTime(at), AuthorityID: pgtype.Text{String: lease.PrincipalID, Valid: true}, ChainOrg: lease.OrgID, ChainProject: pgtype.Text{String: lease.ProjectID, Valid: true}, ChainEnv: pgtype.Text{String: lease.EnvironmentID, Valid: true}, LeaseID: pgtype.Text{String: lease.ID, Valid: true}, Outcome: outcome, Payload: string(payload)})
}

func (q sqliteDynamicRuntimeQueries) dynamicInsertLeaseAudit(ctx context.Context, lease ClaimedLease, id, typ, outcome string, at time.Time, payload []byte) error {

	return q.queries.RuntimeDynamicInsertLeaseAudit(ctx, sqlitegen.RuntimeDynamicInsertLeaseAuditParams{ID: id, Type: typ, At: fixedStamp(at), AuthorityID: sql.NullString{String: lease.PrincipalID, Valid: true}, ChainOrg: lease.OrgID, ChainProject: sql.NullString{String: lease.ProjectID, Valid: true}, ChainEnv: sql.NullString{String: lease.EnvironmentID, Valid: true}, LeaseID: sql.NullString{String: lease.ID, Valid: true}, Outcome: outcome, Payload: string(payload)})
}

func (q pgDynamicRuntimeQueries) dynamicClaimDueLease(ctx context.Context, now time.Time) (ClaimedLease, error) {

	c, err := q.queries.RuntimeDynamicClaimDueLease(ctx, pgRequiredTime(now))
	return ClaimedLease{ID: c.ID, OrgID: c.OrgID, ProjectID: c.ProjectID, EnvironmentID: c.EnvironmentID, ProviderID: c.ProviderID, ProviderHandle: c.ProviderHandle, PrincipalID: c.PrincipalID, PrincipalClass: c.PrincipalClass, State: c.State, MaxTTLSeconds: c.MaxTtlSeconds, IssuedAt: c.IssuedAt.Time.UTC(), ExpiresAt: c.ExpiresAt.Time.UTC(), Attempt: int(c.AttemptCount), ClaimToken: c.LeaseClaimToken}, err
}

func (q sqliteDynamicRuntimeQueries) dynamicClaimDueLease(ctx context.Context, now time.Time) (ClaimedLease, error) {

	c, err := q.queries.RuntimeDynamicClaimDueLease(ctx, fixedStamp(now))
	return ClaimedLease{ID: c.ID, OrgID: c.OrgID, ProjectID: c.ProjectID, EnvironmentID: c.EnvironmentID, ProviderID: c.ProviderID, ProviderHandle: c.ProviderHandle, PrincipalID: c.PrincipalID, PrincipalClass: c.PrincipalClass, State: c.State, MaxTTLSeconds: c.MaxTtlSeconds, IssuedAt: dynamicClaimTime(c.IssuedAt.String), ExpiresAt: dynamicClaimTime(c.ExpiresAt.String), Attempt: int(c.AttemptCount), ClaimToken: c.LeaseClaimToken}, err
}

func (q pgDynamicRuntimeQueries) dynamicLoadProviderMaterial(ctx context.Context, lease ClaimedLease, now time.Time) (LeaseProviderMaterial, error) {

	c, err := q.queries.RuntimeDynamicLoadProviderMaterial(ctx, pggen.RuntimeDynamicLoadProviderMaterialParams{ChainEnv: lease.EnvironmentID, LeaseID: lease.ID, ChainOrg: lease.OrgID, ChainProject: lease.ProjectID, Worker: pgtype.Text{String: lease.LeaseOwner, Valid: true}, Now: pgRequiredTime(now), ClaimToken: lease.ClaimToken})
	return LeaseProviderMaterial{Kind: c.Kind, Origin: c.Origin, TLSMode: c.TlsMode, GrantRole: c.GrantRole, CredentialOwnerID: c.ID, CredentialCiphertext: append([]byte(nil), c.AdminCredentialCiphertext...)}, err
}

func (q sqliteDynamicRuntimeQueries) dynamicLoadProviderMaterial(ctx context.Context, lease ClaimedLease, now time.Time) (LeaseProviderMaterial, error) {

	c, err := q.queries.RuntimeDynamicLoadProviderMaterial(ctx, sqlitegen.RuntimeDynamicLoadProviderMaterialParams{ChainEnv: lease.EnvironmentID, LeaseID: lease.ID, ChainOrg: lease.OrgID, ChainProject: lease.ProjectID, Worker: sql.NullString{String: lease.LeaseOwner, Valid: true}, Now: runtimeSQLiteStamp(now), ClaimToken: lease.ClaimToken})
	return LeaseProviderMaterial{Kind: c.Kind, Origin: c.Origin, TLSMode: c.TlsMode, GrantRole: c.GrantRole, CredentialOwnerID: c.ID, CredentialCiphertext: append([]byte(nil), c.AdminCredentialCiphertext...)}, err
}

func (q pgDynamicRuntimeQueries) dynamicGauges(ctx context.Context) (active, unknown int64, err error) {

	active, err = q.queries.RuntimeDynamicCountActiveLeases(ctx)
	if err == nil {
		unknown, err = q.queries.RuntimeDynamicCountUnknownEffects(ctx)
	}

	return
}

func (q sqliteDynamicRuntimeQueries) dynamicGauges(ctx context.Context) (active, unknown int64, err error) {

	active, err = q.queries.RuntimeDynamicCountActiveLeases(ctx)
	if err == nil {
		unknown, err = q.queries.RuntimeDynamicCountUnknownEffects(ctx)
	}

	return
}
