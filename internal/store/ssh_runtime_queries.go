package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
)

type sshRuntimeQueries interface {
	sshLiveCertificates(ctx context.Context, now time.Time, afterID string, limit int) ([]SSHSweepCandidate, error)
	sshRevokeForAuthority(ctx context.Context, c SSHSweepCandidate, at time.Time) (int64, error)
	sshRevocationAudit(ctx context.Context, c SSHSweepCandidate, at time.Time, id, payload string) error
	sshGauges(ctx context.Context, now time.Time) (active, krlEntries int64, err error)
}

type sqliteSshRuntimeQueries struct{ queries *sqlitegen.Queries }
type pgSshRuntimeQueries struct{ queries *pggen.Queries }

func (q pgSshRuntimeQueries) sshLiveCertificates(ctx context.Context, now time.Time, afterID string, limit int) ([]SSHSweepCandidate, error) {
	var out []SSHSweepCandidate

	rows, err := q.queries.RuntimeSSHListLiveCertificates(ctx, pggen.RuntimeSSHListLiveCertificatesParams{Now: pgRequiredTime(now), AfterID: afterID, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	for _, c := range rows {
		out = append(out, SSHSweepCandidate{ID: c.ID, OrgID: c.OrgID, ProjectID: c.ProjectID, EnvironmentID: c.EnvironmentID, ProfileID: c.ProfileID, Serial: c.Serial, RequesterPrincipalID: c.RequesterPrincipalID, RequesterClass: c.RequesterClass, RequesterListed: c.RequesterListed == 1})
	}

	return out, nil
}

func (q sqliteSshRuntimeQueries) sshLiveCertificates(ctx context.Context, now time.Time, afterID string, limit int) ([]SSHSweepCandidate, error) {
	var out []SSHSweepCandidate

	rows, err := q.queries.RuntimeSSHListLiveCertificates(ctx, sqlitegen.RuntimeSSHListLiveCertificatesParams{Now: fixedStamp(now), AfterID: afterID, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	for _, c := range rows {
		out = append(out, SSHSweepCandidate{ID: c.ID, OrgID: c.OrgID, ProjectID: c.ProjectID, EnvironmentID: c.EnvironmentID, ProfileID: c.ProfileID, Serial: c.Serial, RequesterPrincipalID: c.RequesterPrincipalID, RequesterClass: c.RequesterClass, RequesterListed: c.RequesterListed == 1})
	}

	return out, nil
}

func (q pgSshRuntimeQueries) sshRevokeForAuthority(ctx context.Context, c SSHSweepCandidate, at time.Time) (int64, error) {

	return q.queries.RuntimeSSHRevokeForAuthority(ctx, pggen.RuntimeSSHRevokeForAuthorityParams{At: pgRequiredTime(at), ID: c.ID, ChainOrg: c.OrgID, ChainProject: c.ProjectID, ChainEnv: c.EnvironmentID})
}

func (q sqliteSshRuntimeQueries) sshRevokeForAuthority(ctx context.Context, c SSHSweepCandidate, at time.Time) (int64, error) {

	return q.queries.RuntimeSSHRevokeForAuthority(ctx, sqlitegen.RuntimeSSHRevokeForAuthorityParams{At: runtimeSQLiteStamp(at), ID: c.ID, ChainOrg: c.OrgID, ChainProject: c.ProjectID, ChainEnv: c.EnvironmentID})
}

func (q pgSshRuntimeQueries) sshRevocationAudit(ctx context.Context, c SSHSweepCandidate, at time.Time, id, payload string) error {

	return q.queries.RuntimeSSHInsertAuthorityRevocationAudit(ctx, pggen.RuntimeSSHInsertAuthorityRevocationAuditParams{ID: id, At: pgRequiredTime(at), AuthorityID: pgtype.Text{String: c.RequesterPrincipalID, Valid: true}, ChainOrg: c.OrgID, ChainProject: pgtype.Text{String: c.ProjectID, Valid: true}, ChainEnv: pgtype.Text{String: c.EnvironmentID, Valid: true}, CertificateID: pgtype.Text{String: c.ID, Valid: true}, Payload: payload})
}

func (q sqliteSshRuntimeQueries) sshRevocationAudit(ctx context.Context, c SSHSweepCandidate, at time.Time, id, payload string) error {

	return q.queries.RuntimeSSHInsertAuthorityRevocationAudit(ctx, sqlitegen.RuntimeSSHInsertAuthorityRevocationAuditParams{ID: id, At: fixedStamp(at), AuthorityID: sql.NullString{String: c.RequesterPrincipalID, Valid: true}, ChainOrg: c.OrgID, ChainProject: sql.NullString{String: c.ProjectID, Valid: true}, ChainEnv: sql.NullString{String: c.EnvironmentID, Valid: true}, CertificateID: sql.NullString{String: c.ID, Valid: true}, Payload: payload})
}

func (q pgSshRuntimeQueries) sshGauges(ctx context.Context, now time.Time) (active, krlEntries int64, err error) {

	stamp := pgRequiredTime(now)
	active, err = q.queries.RuntimeSSHCountActive(ctx, stamp)
	if err == nil {
		krlEntries, err = q.queries.RuntimeSSHCountKRLEntries(ctx, stamp)
	}

	return
}

func (q sqliteSshRuntimeQueries) sshGauges(ctx context.Context, now time.Time) (active, krlEntries int64, err error) {

	stamp := fixedStamp(now)
	active, err = q.queries.RuntimeSSHCountActive(ctx, stamp)
	if err == nil {
		krlEntries, err = q.queries.RuntimeSSHCountKRLEntries(ctx, stamp)
	}

	return
}
