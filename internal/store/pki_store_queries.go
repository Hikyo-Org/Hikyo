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

type pkiStoreQueries interface {
	pkiListIssuers(ctx context.Context) ([]PKIIssuer, error)
	pkiListProfiles(ctx context.Context) ([]PKIProfile, error)
	pkiGetIssuer(ctx context.Context, id string) (PKIIssuer, error)
	pkiActiveIssuer(ctx context.Context, name string) (PKIIssuer, error)
	pkiSigningIssuer(ctx context.Context, id string) (PKIIssuer, error)
	pkiGetProfile(ctx context.Context, name string) (PKIProfile, error)
	pkiListBindings(ctx context.Context, profileID string) ([]PKIProfileBinding, error)
	pkiIssuerKey(ctx context.Context, id string) (pkiSealedKey, error)
	pkiCreateIssuer(ctx context.Context, m PKIIssuerCreate) error
	pkiInstallIssuer(ctx context.Context, m PKIIssuerInstall) (int64, error)
	pkiTransitionIssuer(ctx context.Context, id, from, to string, rowVersion int64, at time.Time) (int64, error)
	pkiDestroyIssuerKey(ctx context.Context, id, from, to string, rowVersion int64, at time.Time) (int64, error)
	pkiRevokeParent(ctx context.Context, id string) error
	pkiBumpIssuerRevocation(ctx context.Context, id string) error
	pkiBumpCertificateRevocation(ctx context.Context, id string) error
	pkiDeleteProfileBindings(ctx context.Context, id string) error
	pkiSetIssuerHold(ctx context.Context, id string, hold int64, rowVersion int64, at time.Time) (int64, error)
	pkiCountLiveCertificates(ctx context.Context, issuerID string, now time.Time) (int64, error)
	pkiRevokeLiveCertificates(ctx context.Context, issuerID, reason string, at time.Time) (int64, error)
	pkiRevokedEntries(ctx context.Context, issuerID string, now time.Time) ([]PKIRevokedEntry, error)
	pkiRevokedChildren(ctx context.Context, issuerID string, now time.Time) ([]pkiRevokedChild, error)
	pkiPublishCRL(ctx context.Context, id string, der []byte, previousNumber, number, revocationSeq int64, thisUpdate, nextUpdate time.Time) (int64, error)
	pkiCreateProfile(ctx context.Context, m PKIProfile) error
	pkiUpdateProfile(ctx context.Context, id, policy string, rowVersion int64, at time.Time) (int64, error)
	pkiDeleteProfile(ctx context.Context, id string) (int64, error)
	pkiCreateBinding(ctx context.Context, m PKIProfileBinding) error
	pkiDeleteBinding(ctx context.Context, id, profileID string) (int64, error)
	pkiBoundProfile(ctx context.Context, chain domain.Scope, name string) (PKIProfile, error)
	pkiBoundProfiles(ctx context.Context, chain domain.Scope) ([]PKIProfile, error)
	pkiGetCertificate(ctx context.Context, chain domain.Scope, id string) (PKICertificate, error)
	pkiListCertificates(ctx context.Context, chain domain.Scope) ([]PKICertificate, error)
	pkiFenceIssuance(ctx context.Context, id string) (int64, error)
	pkiCreateCertificate(ctx context.Context, chain domain.Scope, m PKICertificateCreate) error
	pkiFinishCertificate(ctx context.Context, chain domain.Scope, id string, der []byte, at time.Time) (int64, error)
	pkiFailCertificate(ctx context.Context, chain domain.Scope, id string, at time.Time) (int64, error)
	pkiClaimRenewal(ctx context.Context, chain domain.Scope, id, successorID string, at time.Time) (int64, error)
	pkiCompleteRenewal(ctx context.Context, chain domain.Scope, id, successorID string, at time.Time) (int64, error)
	pkiReleaseRenewal(ctx context.Context, chain domain.Scope, id, successorID string, at time.Time) (int64, error)
	pkiRevokeCertificate(ctx context.Context, chain domain.Scope, id, reason string, at time.Time) (int64, error)
	runtimePKIStaleIssuing(ctx context.Context, now time.Time, limit int) ([]pkiSweptRow, error)
	runtimePKIExpired(ctx context.Context, now time.Time, limit int) ([]pkiSweptRow, error)
	runtimePKIMarkUnknown(ctx context.Context, row pkiSweptRow, at time.Time) (int64, error)
	runtimePKIMarkExpired(ctx context.Context, row pkiSweptRow, at time.Time) (int64, error)
	runtimePKIReleaseRenewal(ctx context.Context, row pkiSweptRow, at time.Time) error
	runtimePKITransitionAudit(ctx context.Context, row pkiSweptRow, id, outcome, payload string, at time.Time) error
	runtimePKIDueCRLs(ctx context.Context, halfLife time.Time) ([]PKICRLCandidate, error)
	runtimePKIPublishedAudit(ctx context.Context, id, issuerID, payload string, at time.Time) error
	runtimePKICountLive(ctx context.Context, now time.Time) (int64, error)
	runtimePKICountUnknown(ctx context.Context, now time.Time) (int64, error)
	runtimePKICountHeld(ctx context.Context) (int64, error)
}

type sqlitePKIStoreQueries struct {
	queries        *sqlitegen.Queries
	mapConstraints bool
}
type pgPKIStoreQueries struct {
	queries        *pggen.Queries
	mapConstraints bool
}
type pkiSealedKey struct {
	ciphertext []byte
	version    uint32
}
type pkiRevokedChild struct {
	der       []byte
	updatedAt time.Time
}

func (d sqliteAdoptDB) pkiStoreQueries() pkiStoreQueries {
	return sqlitePKIStoreQueries{queries: sqlitegen.New(d.db), mapConstraints: true}
}
func (d pgAdoptDB) pkiStoreQueries() pkiStoreQueries {
	return pgPKIStoreQueries{queries: pggen.New(d.db), mapConstraints: true}
}
func (d sqliteAdapterTx) pkiStoreQueries() pkiStoreQueries {
	return sqlitePKIStoreQueries{queries: sqlitegen.New(d.tx)}
}
func (d pgAdapterTx) pkiStoreQueries() pkiStoreQueries {
	return pgPKIStoreQueries{queries: pggen.New(d.tx)}
}

func (q sqlitePKIStoreQueries) pkiListIssuers(ctx context.Context) ([]PKIIssuer, error) {
	c, err := q.queries.PKIListIssuers(ctx)
	if err != nil {
		return nil, err
	}
	return mapRows(c, func(c sqlitegen.PKIListIssuersRow) (PKIIssuer, error) {
		return sqlitePKIIssuer(sqlitegen.PKIGetIssuerRow(c))
	})
}

func (q sqlitePKIStoreQueries) pkiListProfiles(ctx context.Context) ([]PKIProfile, error) {
	c, err := q.queries.PKIListProfiles(ctx)
	if err != nil {
		return nil, err
	}
	return mapRows(c, func(c sqlitegen.PkiProfile) (PKIProfile, error) { return sqlitePKIProfile(sqlitegen.PkiProfile(c)) })
}

func (q sqlitePKIStoreQueries) pkiGetIssuer(ctx context.Context, id string) (PKIIssuer, error) {
	c, err := q.queries.PKIGetIssuer(ctx, id)
	if isNoRows(err) {
		return PKIIssuer{}, ErrNotFound
	}
	if err != nil {
		return PKIIssuer{}, err
	}
	item, err := sqlitePKIIssuer(sqlitegen.PKIGetIssuerRow(c))
	return item, err
}

func (q sqlitePKIStoreQueries) pkiActiveIssuer(ctx context.Context, name string) (PKIIssuer, error) {
	c, err := q.queries.PKIActiveIssuer(ctx, name)
	if isNoRows(err) {
		return PKIIssuer{}, ErrNotFound
	}
	if err != nil {
		return PKIIssuer{}, err
	}
	item, err := sqlitePKIIssuer(sqlitegen.PKIGetIssuerRow(c))
	return item, err
}

func (q sqlitePKIStoreQueries) pkiSigningIssuer(ctx context.Context, id string) (PKIIssuer, error) {
	c, err := q.queries.PKISigningIssuer(ctx, id)
	if isNoRows(err) {
		return PKIIssuer{}, ErrNotFound
	}
	if err != nil {
		return PKIIssuer{}, err
	}
	item, err := sqlitePKIIssuer(sqlitegen.PKIGetIssuerRow(c))
	return item, err
}

func (q sqlitePKIStoreQueries) pkiGetProfile(ctx context.Context, name string) (PKIProfile, error) {
	c, err := q.queries.PKIGetProfile(ctx, name)
	if isNoRows(err) {
		return PKIProfile{}, ErrNotFound
	}
	if err != nil {
		return PKIProfile{}, err
	}
	item, err := sqlitePKIProfile(sqlitegen.PkiProfile(c))
	return item, err
}

func (q sqlitePKIStoreQueries) pkiListBindings(ctx context.Context, profileID string) ([]PKIProfileBinding, error) {
	c, err := q.queries.PKIListBindings(ctx, profileID)
	if err != nil {
		return nil, err
	}
	return mapRows(c, sqlitePKIBinding)
}

func (q sqlitePKIStoreQueries) pkiIssuerKey(ctx context.Context, id string) (pkiSealedKey, error) {
	c, err := q.queries.PKIIssuerKey(ctx, id)
	if isNoRows(err) {
		return pkiSealedKey{}, ErrNotFound
	}
	if err != nil {
		return pkiSealedKey{}, err
	}
	version, err := checkedStoredDEKVersion(c.DekVersion.Int64)
	if err != nil {
		return pkiSealedKey{}, err
	}
	return pkiSealedKey{ciphertext: c.EncryptedPrivateKey, version: version}, nil
}

func (q sqlitePKIStoreQueries) pkiCreateIssuer(ctx context.Context, m PKIIssuerCreate) error {
	return q.pkiWriteError(q.queries.PKICreateIssuer(ctx, sqlitegen.PKICreateIssuerParams{ID: m.ID, Name: m.Name, Version: m.Version, Kind: m.Kind, Origin: m.Origin, ParentID: sql.NullString{String: m.ParentID, Valid: m.ParentID != ""}, State: m.State, KeyAlgorithm: m.KeyAlgorithm, KeyFingerprint: m.KeyFingerprint, EncryptedPrivateKey: m.EncryptedPrivateKey, DekVersion: sql.NullInt64{Int64: int64(m.DEKVersion), Valid: true}, CertificateDer: optionalStoredBytes(m.CertificateDER), CsrDer: optionalStoredBytes(m.CSRDER), ChainPem: m.ChainPEM, SubjectCn: m.SubjectCN, SubjectOrg: m.SubjectOrg, NotBefore: sqlitePKITime(m.NotBefore), NotAfter: sqlitePKITime(m.NotAfter), CrlDistributionUrl: m.CRLDistributionURL, CreatedBy: m.CreatedBy, At: fixedStamp(m.At)}))
}

func (q sqlitePKIStoreQueries) pkiInstallIssuer(ctx context.Context, m PKIIssuerInstall) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIInstallIssuer(ctx, sqlitegen.PKIInstallIssuerParams{CertificateDer: m.CertificateDER, ChainPem: m.ChainPEM, NotBefore: runtimeSQLiteStamp(m.NotBefore), NotAfter: runtimeSQLiteStamp(m.NotAfter), At: fixedStamp(m.At), ID: m.ID, RowVersion: m.RowVersion}))
}

func (q sqlitePKIStoreQueries) pkiTransitionIssuer(ctx context.Context, id, from, to string, rowVersion int64, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKITransitionIssuer(ctx, sqlitegen.PKITransitionIssuerParams{NextState: to, At: fixedStamp(at), ID: id, PriorState: from, RowVersion: rowVersion}))
}

func (q sqlitePKIStoreQueries) pkiDestroyIssuerKey(ctx context.Context, id, from, to string, rowVersion int64, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIDestroyIssuerKey(ctx, sqlitegen.PKIDestroyIssuerKeyParams{NextState: to, At: fixedStamp(at), ID: id, PriorState: from, RowVersion: rowVersion}))
}

func (q sqlitePKIStoreQueries) pkiRevokeParent(ctx context.Context, id string) error {
	return q.pkiWriteError(q.queries.PKIRevokeParent(ctx, id))
}

func (q sqlitePKIStoreQueries) pkiBumpIssuerRevocation(ctx context.Context, id string) error {
	return q.pkiWriteError(q.queries.PKIBumpIssuerRevocation(ctx, id))
}

func (q sqlitePKIStoreQueries) pkiBumpCertificateRevocation(ctx context.Context, id string) error {
	return q.pkiWriteError(q.queries.PKIBumpCertificateRevocation(ctx, id))
}

func (q sqlitePKIStoreQueries) pkiDeleteProfileBindings(ctx context.Context, id string) error {
	return q.pkiWriteError(q.queries.PKIDeleteProfileBindings(ctx, id))
}

func (q sqlitePKIStoreQueries) pkiSetIssuerHold(ctx context.Context, id string, hold int64, rowVersion int64, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKISetIssuerHold(ctx, sqlitegen.PKISetIssuerHoldParams{Hold: hold, At: fixedStamp(at), ID: id, RowVersion: rowVersion}))
}

func (q sqlitePKIStoreQueries) pkiCountLiveCertificates(ctx context.Context, issuerID string, now time.Time) (int64, error) {
	return q.queries.PKICountLiveCertificates(ctx, sqlitegen.PKICountLiveCertificatesParams{IssuerID: issuerID, Now: fixedStamp(now)})
}

func (q sqlitePKIStoreQueries) pkiRevokeLiveCertificates(ctx context.Context, issuerID, reason string, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIRevokeLiveCertificates(ctx, sqlitegen.PKIRevokeLiveCertificatesParams{At: runtimeSQLiteStamp(at), Reason: sql.NullString{String: reason, Valid: true}, IssuerID: issuerID}))
}

func (q sqlitePKIStoreQueries) pkiRevokedEntries(ctx context.Context, issuerID string, now time.Time) ([]PKIRevokedEntry, error) {
	c, err := q.queries.PKIRevokedEntries(ctx, sqlitegen.PKIRevokedEntriesParams{IssuerID: issuerID, Now: fixedStamp(now)})
	if err != nil {
		return nil, err
	}
	return mapRows(c, sqlitePKIRevocation)
}

func (q sqlitePKIStoreQueries) pkiRevokedChildren(ctx context.Context, issuerID string, now time.Time) ([]pkiRevokedChild, error) {
	c, err := q.queries.PKIRevokedChildren(ctx, sqlitegen.PKIRevokedChildrenParams{IssuerID: sql.NullString{String: issuerID, Valid: true}, Now: runtimeSQLiteStamp(now)})
	if err != nil {
		return nil, err
	}
	return mapRows(c, sqlitePKIChild)
}

func (q sqlitePKIStoreQueries) pkiPublishCRL(ctx context.Context, id string, der []byte, previousNumber, number, revocationSeq int64, thisUpdate, nextUpdate time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIPublishCRL(ctx, sqlitegen.PKIPublishCRLParams{Der: der, Number: number, RevocationSeq: revocationSeq, ThisUpdate: runtimeSQLiteStamp(thisUpdate), NextUpdate: runtimeSQLiteStamp(nextUpdate), ID: id, PreviousNumber: previousNumber}))
}

func (q sqlitePKIStoreQueries) pkiCreateProfile(ctx context.Context, m PKIProfile) error {
	return q.pkiWriteError(q.queries.PKICreateProfile(ctx, sqlitegen.PKICreateProfileParams{ID: m.ID, Name: m.Name, Policy: m.Policy, CreatedBy: m.CreatedBy, At: fixedStamp(m.CreatedAt)}))
}

func (q sqlitePKIStoreQueries) pkiUpdateProfile(ctx context.Context, id, policy string, rowVersion int64, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIUpdateProfile(ctx, sqlitegen.PKIUpdateProfileParams{Policy: policy, At: fixedStamp(at), ID: id, RowVersion: rowVersion}))
}

func (q sqlitePKIStoreQueries) pkiDeleteProfile(ctx context.Context, id string) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIDeleteProfile(ctx, id))
}

func (q sqlitePKIStoreQueries) pkiCreateBinding(ctx context.Context, m PKIProfileBinding) error {
	return q.pkiWriteError(q.queries.PKICreateBinding(ctx, sqlitegen.PKICreateBindingParams{ID: m.ID, ProfileID: m.ProfileID, OrgID: m.OrgID, ProjectID: m.ProjectID, EnvironmentID: sql.NullString{String: m.EnvironmentID, Valid: m.EnvironmentID != ""}, CreatedBy: m.CreatedBy, At: fixedStamp(m.CreatedAt)}))
}

func (q sqlitePKIStoreQueries) pkiDeleteBinding(ctx context.Context, id, profileID string) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIDeleteBinding(ctx, sqlitegen.PKIDeleteBindingParams{ID: id, ProfileID: profileID}))
}

func (q sqlitePKIStoreQueries) pkiBoundProfile(ctx context.Context, chain domain.Scope, name string) (PKIProfile, error) {
	c, err := q.queries.PKIBoundProfile(ctx, sqlitegen.PKIBoundProfileParams{Name: name, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: sql.NullString{String: string(chain.Env), Valid: string(chain.Env) != ""}})
	if isNoRows(err) {
		return PKIProfile{}, ErrNotFound
	}
	if err != nil {
		return PKIProfile{}, err
	}
	item, err := sqlitePKIProfile(sqlitegen.PkiProfile(c))
	return item, err
}

func (q sqlitePKIStoreQueries) pkiBoundProfiles(ctx context.Context, chain domain.Scope) ([]PKIProfile, error) {
	c, err := q.queries.PKIBoundProfiles(ctx, sqlitegen.PKIBoundProfilesParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: sql.NullString{String: string(chain.Env), Valid: string(chain.Env) != ""}})
	if err != nil {
		return nil, err
	}
	return mapRows(c, func(c sqlitegen.PkiProfile) (PKIProfile, error) { return sqlitePKIProfile(sqlitegen.PkiProfile(c)) })
}

func (q sqlitePKIStoreQueries) pkiGetCertificate(ctx context.Context, chain domain.Scope, id string) (PKICertificate, error) {
	c, err := q.queries.PKIGetCertificate(ctx, sqlitegen.PKIGetCertificateParams{ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return PKICertificate{}, ErrNotFound
	}
	if err != nil {
		return PKICertificate{}, err
	}
	item, err := sqlitePKICertificate(sqlitegen.PKIGetCertificateRow(c))
	return item, err
}

func (q sqlitePKIStoreQueries) pkiListCertificates(ctx context.Context, chain domain.Scope) ([]PKICertificate, error) {
	c, err := q.queries.PKIListCertificates(ctx, sqlitegen.PKIListCertificatesParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	return mapRows(c, func(c sqlitegen.PKIListCertificatesRow) (PKICertificate, error) {
		return sqlitePKICertificate(sqlitegen.PKIGetCertificateRow(c))
	})
}

func (q sqlitePKIStoreQueries) pkiFenceIssuance(ctx context.Context, id string) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIFenceIssuance(ctx, id))
}

func (q sqlitePKIStoreQueries) pkiCreateCertificate(ctx context.Context, chain domain.Scope, m PKICertificateCreate) error {
	return q.pkiWriteError(q.queries.PKICreateCertificate(ctx, sqlitegen.PKICreateCertificateParams{ID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), ProfileID: m.ProfileID, ProfileName: m.ProfileName, IssuerID: m.IssuerID, Serial: m.Serial, KeySource: m.KeySource, KeyAlgorithm: m.KeyAlgorithm, KeyFingerprint: m.KeyFingerprint, CommonName: m.CommonName, Sans: m.SANs, NotBefore: fixedStamp(m.NotBefore), NotAfter: fixedStamp(m.NotAfter), PrincipalID: m.PrincipalID, PrincipalClass: m.PrincipalClass, RenewedFrom: sql.NullString{String: m.RenewedFrom, Valid: m.RenewedFrom != ""}, IssuingDeadline: fixedStamp(m.IssuingDeadline), At: fixedStamp(m.At)}))
}

func (q sqlitePKIStoreQueries) pkiFinishCertificate(ctx context.Context, chain domain.Scope, id string, der []byte, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIFinishCertificate(ctx, sqlitegen.PKIFinishCertificateParams{Der: der, At: fixedStamp(at), ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q sqlitePKIStoreQueries) pkiFailCertificate(ctx context.Context, chain domain.Scope, id string, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIFailCertificate(ctx, sqlitegen.PKIFailCertificateParams{At: fixedStamp(at), ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q sqlitePKIStoreQueries) pkiClaimRenewal(ctx context.Context, chain domain.Scope, id, successorID string, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIClaimRenewal(ctx, sqlitegen.PKIClaimRenewalParams{SuccessorID: sql.NullString{String: successorID, Valid: true}, At: fixedStamp(at), ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q sqlitePKIStoreQueries) pkiCompleteRenewal(ctx context.Context, chain domain.Scope, id, successorID string, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKICompleteRenewal(ctx, sqlitegen.PKICompleteRenewalParams{At: fixedStamp(at), ID: id, SuccessorID: sql.NullString{String: successorID, Valid: true}, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q sqlitePKIStoreQueries) pkiReleaseRenewal(ctx context.Context, chain domain.Scope, id, successorID string, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIReleaseRenewal(ctx, sqlitegen.PKIReleaseRenewalParams{At: fixedStamp(at), ID: id, SuccessorID: sql.NullString{String: successorID, Valid: true}, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q sqlitePKIStoreQueries) pkiRevokeCertificate(ctx context.Context, chain domain.Scope, id, reason string, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIRevokeCertificate(ctx, sqlitegen.PKIRevokeCertificateParams{At: runtimeSQLiteStamp(at), Reason: sql.NullString{String: reason, Valid: true}, ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q sqlitePKIStoreQueries) runtimePKIStaleIssuing(ctx context.Context, now time.Time, limit int) ([]pkiSweptRow, error) {
	c, err := q.queries.RuntimePKIStaleIssuing(ctx, sqlitegen.RuntimePKIStaleIssuingParams{Now: fixedStamp(now), PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(c, sqlitePKISwept)
}

func (q sqlitePKIStoreQueries) runtimePKIExpired(ctx context.Context, now time.Time, limit int) ([]pkiSweptRow, error) {
	c, err := q.queries.RuntimePKIExpired(ctx, sqlitegen.RuntimePKIExpiredParams{Now: fixedStamp(now), PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(c, func(c sqlitegen.RuntimePKIExpiredRow) (pkiSweptRow, error) {
		return sqlitePKISwept(sqlitegen.RuntimePKIStaleIssuingRow(c))
	})
}

func (q sqlitePKIStoreQueries) runtimePKIMarkUnknown(ctx context.Context, row pkiSweptRow, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.RuntimePKIMarkUnknown(ctx, sqlitegen.RuntimePKIMarkUnknownParams{At: fixedStamp(at), ID: row.id, OrgID: row.org}))
}

func (q sqlitePKIStoreQueries) runtimePKIMarkExpired(ctx context.Context, row pkiSweptRow, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.RuntimePKIMarkExpired(ctx, sqlitegen.RuntimePKIMarkExpiredParams{At: fixedStamp(at), ID: row.id, OrgID: row.org}))
}

func (q sqlitePKIStoreQueries) runtimePKIReleaseRenewal(ctx context.Context, row pkiSweptRow, at time.Time) error {
	return q.pkiWriteError(q.queries.RuntimePKIReleaseRenewal(ctx, sqlitegen.RuntimePKIReleaseRenewalParams{At: fixedStamp(at), ID: row.renewedFrom, OrgID: row.org, SuccessorID: sql.NullString{String: row.id, Valid: true}}))
}

func (q sqlitePKIStoreQueries) runtimePKITransitionAudit(ctx context.Context, row pkiSweptRow, id, outcome, payload string, at time.Time) error {
	return q.pkiWriteError(q.queries.RuntimePKITransitionAudit(ctx, sqlitegen.RuntimePKITransitionAuditParams{ID: id, At: fixedStamp(at), AuthorityID: sql.NullString{String: row.principal, Valid: true}, OrgID: row.org, ProjectID: sql.NullString{String: row.project, Valid: true}, EnvID: sql.NullString{String: row.env, Valid: true}, CertificateID: sql.NullString{String: row.id, Valid: true}, Outcome: outcome, Payload: payload}))
}

func (q sqlitePKIStoreQueries) runtimePKIDueCRLs(ctx context.Context, halfLife time.Time) ([]PKICRLCandidate, error) {
	c, err := q.queries.RuntimePKIDueCRLs(ctx, runtimeSQLiteStamp(halfLife))
	if err != nil {
		return nil, err
	}
	return mapRows(c, sqlitePKICandidate)
}

func (q sqlitePKIStoreQueries) runtimePKIPublishedAudit(ctx context.Context, id, issuerID, payload string, at time.Time) error {
	return q.pkiWriteError(q.queries.RuntimePKIPublishedAudit(ctx, sqlitegen.RuntimePKIPublishedAuditParams{ID: id, At: fixedStamp(at), IssuerID: sql.NullString{String: issuerID, Valid: true}, Payload: payload}))
}

func (q sqlitePKIStoreQueries) runtimePKICountLive(ctx context.Context, now time.Time) (int64, error) {
	return q.queries.RuntimePKICountLive(ctx, fixedStamp(now))
}

func (q sqlitePKIStoreQueries) runtimePKICountUnknown(ctx context.Context, now time.Time) (int64, error) {
	return q.queries.RuntimePKICountUnknown(ctx, fixedStamp(now))
}

func (q sqlitePKIStoreQueries) runtimePKICountHeld(ctx context.Context) (int64, error) {
	return q.queries.RuntimePKICountHeld(ctx)
}

func (q pgPKIStoreQueries) pkiListIssuers(ctx context.Context) ([]PKIIssuer, error) {
	c, err := q.queries.PKIListIssuers(ctx)
	if err != nil {
		return nil, err
	}
	return mapRows(c, func(c pggen.PKIListIssuersRow) (PKIIssuer, error) { return pgPKIIssuer(pggen.PKIGetIssuerRow(c)) })
}

func (q pgPKIStoreQueries) pkiListProfiles(ctx context.Context) ([]PKIProfile, error) {
	c, err := q.queries.PKIListProfiles(ctx)
	if err != nil {
		return nil, err
	}
	return mapRows(c, func(c pggen.PkiProfile) (PKIProfile, error) { return pgPKIProfile(pggen.PkiProfile(c)) })
}

func (q pgPKIStoreQueries) pkiGetIssuer(ctx context.Context, id string) (PKIIssuer, error) {
	c, err := q.queries.PKIGetIssuer(ctx, id)
	if isNoRows(err) {
		return PKIIssuer{}, ErrNotFound
	}
	if err != nil {
		return PKIIssuer{}, err
	}
	item, err := pgPKIIssuer(pggen.PKIGetIssuerRow(c))
	return item, err
}

func (q pgPKIStoreQueries) pkiActiveIssuer(ctx context.Context, name string) (PKIIssuer, error) {
	c, err := q.queries.PKIActiveIssuer(ctx, name)
	if isNoRows(err) {
		return PKIIssuer{}, ErrNotFound
	}
	if err != nil {
		return PKIIssuer{}, err
	}
	item, err := pgPKIIssuer(pggen.PKIGetIssuerRow(c))
	return item, err
}

func (q pgPKIStoreQueries) pkiSigningIssuer(ctx context.Context, id string) (PKIIssuer, error) {
	c, err := q.queries.PKISigningIssuer(ctx, id)
	if isNoRows(err) {
		return PKIIssuer{}, ErrNotFound
	}
	if err != nil {
		return PKIIssuer{}, err
	}
	item, err := pgPKIIssuer(pggen.PKIGetIssuerRow(c))
	return item, err
}

func (q pgPKIStoreQueries) pkiGetProfile(ctx context.Context, name string) (PKIProfile, error) {
	c, err := q.queries.PKIGetProfile(ctx, name)
	if isNoRows(err) {
		return PKIProfile{}, ErrNotFound
	}
	if err != nil {
		return PKIProfile{}, err
	}
	item, err := pgPKIProfile(pggen.PkiProfile(c))
	return item, err
}

func (q pgPKIStoreQueries) pkiListBindings(ctx context.Context, profileID string) ([]PKIProfileBinding, error) {
	c, err := q.queries.PKIListBindings(ctx, profileID)
	if err != nil {
		return nil, err
	}
	return mapRows(c, pgPKIBinding)
}

func (q pgPKIStoreQueries) pkiIssuerKey(ctx context.Context, id string) (pkiSealedKey, error) {
	c, err := q.queries.PKIIssuerKey(ctx, id)
	if isNoRows(err) {
		return pkiSealedKey{}, ErrNotFound
	}
	if err != nil {
		return pkiSealedKey{}, err
	}
	version, err := checkedStoredDEKVersion(c.DekVersion.Int64)
	if err != nil {
		return pkiSealedKey{}, err
	}
	return pkiSealedKey{ciphertext: c.EncryptedPrivateKey, version: version}, nil
}

func (q pgPKIStoreQueries) pkiCreateIssuer(ctx context.Context, m PKIIssuerCreate) error {
	checkedMVersion, err := checkedPGInt32(int64(m.Version))
	if err != nil {
		return err
	}

	return q.pkiWriteError(q.queries.PKICreateIssuer(ctx, pggen.PKICreateIssuerParams{ID: m.ID, Name: m.Name, Version: checkedMVersion, Kind: m.Kind, Origin: m.Origin, ParentID: pgtype.Text{String: m.ParentID, Valid: m.ParentID != ""}, State: m.State, KeyAlgorithm: m.KeyAlgorithm, KeyFingerprint: m.KeyFingerprint, EncryptedPrivateKey: m.EncryptedPrivateKey, DekVersion: pgtype.Int8{Int64: int64(m.DEKVersion), Valid: true}, CertificateDer: optionalStoredBytes(m.CertificateDER), CsrDer: optionalStoredBytes(m.CSRDER), ChainPem: m.ChainPEM, SubjectCn: m.SubjectCN, SubjectOrg: m.SubjectOrg, NotBefore: pgPKITime(m.NotBefore), NotAfter: pgPKITime(m.NotAfter), CrlDistributionUrl: m.CRLDistributionURL, CreatedBy: m.CreatedBy, At: pgRequiredTime(m.At)}))
}

func (q pgPKIStoreQueries) pkiInstallIssuer(ctx context.Context, m PKIIssuerInstall) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIInstallIssuer(ctx, pggen.PKIInstallIssuerParams{CertificateDer: m.CertificateDER, ChainPem: m.ChainPEM, NotBefore: pgRequiredTime(m.NotBefore), NotAfter: pgRequiredTime(m.NotAfter), At: pgRequiredTime(m.At), ID: m.ID, RowVersion: m.RowVersion}))
}

func (q pgPKIStoreQueries) pkiTransitionIssuer(ctx context.Context, id, from, to string, rowVersion int64, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKITransitionIssuer(ctx, pggen.PKITransitionIssuerParams{NextState: to, At: pgRequiredTime(at), ID: id, PriorState: from, RowVersion: rowVersion}))
}

func (q pgPKIStoreQueries) pkiDestroyIssuerKey(ctx context.Context, id, from, to string, rowVersion int64, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIDestroyIssuerKey(ctx, pggen.PKIDestroyIssuerKeyParams{NextState: to, At: pgRequiredTime(at), ID: id, PriorState: from, RowVersion: rowVersion}))
}

func (q pgPKIStoreQueries) pkiRevokeParent(ctx context.Context, id string) error {
	return q.pkiWriteError(q.queries.PKIRevokeParent(ctx, id))
}

func (q pgPKIStoreQueries) pkiBumpIssuerRevocation(ctx context.Context, id string) error {
	return q.pkiWriteError(q.queries.PKIBumpIssuerRevocation(ctx, id))
}

func (q pgPKIStoreQueries) pkiBumpCertificateRevocation(ctx context.Context, id string) error {
	return q.pkiWriteError(q.queries.PKIBumpCertificateRevocation(ctx, id))
}

func (q pgPKIStoreQueries) pkiDeleteProfileBindings(ctx context.Context, id string) error {
	return q.pkiWriteError(q.queries.PKIDeleteProfileBindings(ctx, id))
}

func (q pgPKIStoreQueries) pkiSetIssuerHold(ctx context.Context, id string, hold int64, rowVersion int64, at time.Time) (int64, error) {
	checkedHold, err := checkedPGInt32(int64(hold))
	if err != nil {
		return 0, err
	}

	return q.pkiWriteRows(q.queries.PKISetIssuerHold(ctx, pggen.PKISetIssuerHoldParams{Hold: checkedHold, At: pgRequiredTime(at), ID: id, RowVersion: rowVersion}))
}

func (q pgPKIStoreQueries) pkiCountLiveCertificates(ctx context.Context, issuerID string, now time.Time) (int64, error) {
	return q.queries.PKICountLiveCertificates(ctx, pggen.PKICountLiveCertificatesParams{IssuerID: issuerID, Now: pgRequiredTime(now)})
}

func (q pgPKIStoreQueries) pkiRevokeLiveCertificates(ctx context.Context, issuerID, reason string, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIRevokeLiveCertificates(ctx, pggen.PKIRevokeLiveCertificatesParams{At: pgRequiredTime(at), Reason: pgtype.Text{String: reason, Valid: true}, IssuerID: issuerID}))
}

func (q pgPKIStoreQueries) pkiRevokedEntries(ctx context.Context, issuerID string, now time.Time) ([]PKIRevokedEntry, error) {
	c, err := q.queries.PKIRevokedEntries(ctx, pggen.PKIRevokedEntriesParams{IssuerID: issuerID, Now: pgRequiredTime(now)})
	if err != nil {
		return nil, err
	}
	return mapRows(c, pgPKIRevocation)
}

func (q pgPKIStoreQueries) pkiRevokedChildren(ctx context.Context, issuerID string, now time.Time) ([]pkiRevokedChild, error) {
	c, err := q.queries.PKIRevokedChildren(ctx, pggen.PKIRevokedChildrenParams{IssuerID: pgtype.Text{String: issuerID, Valid: true}, Now: pgRequiredTime(now)})
	if err != nil {
		return nil, err
	}
	return mapRows(c, pgPKIChild)
}

func (q pgPKIStoreQueries) pkiPublishCRL(ctx context.Context, id string, der []byte, previousNumber, number, revocationSeq int64, thisUpdate, nextUpdate time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIPublishCRL(ctx, pggen.PKIPublishCRLParams{Der: der, Number: number, RevocationSeq: revocationSeq, ThisUpdate: pgRequiredTime(thisUpdate), NextUpdate: pgRequiredTime(nextUpdate), ID: id, PreviousNumber: previousNumber}))
}

func (q pgPKIStoreQueries) pkiCreateProfile(ctx context.Context, m PKIProfile) error {
	return q.pkiWriteError(q.queries.PKICreateProfile(ctx, pggen.PKICreateProfileParams{ID: m.ID, Name: m.Name, Policy: m.Policy, CreatedBy: m.CreatedBy, At: pgRequiredTime(m.CreatedAt)}))
}

func (q pgPKIStoreQueries) pkiUpdateProfile(ctx context.Context, id, policy string, rowVersion int64, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIUpdateProfile(ctx, pggen.PKIUpdateProfileParams{Policy: policy, At: pgRequiredTime(at), ID: id, RowVersion: rowVersion}))
}

func (q pgPKIStoreQueries) pkiDeleteProfile(ctx context.Context, id string) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIDeleteProfile(ctx, id))
}

func (q pgPKIStoreQueries) pkiCreateBinding(ctx context.Context, m PKIProfileBinding) error {
	return q.pkiWriteError(q.queries.PKICreateBinding(ctx, pggen.PKICreateBindingParams{ID: m.ID, ProfileID: m.ProfileID, OrgID: m.OrgID, ProjectID: m.ProjectID, EnvironmentID: pgtype.Text{String: m.EnvironmentID, Valid: m.EnvironmentID != ""}, CreatedBy: m.CreatedBy, At: pgRequiredTime(m.CreatedAt)}))
}

func (q pgPKIStoreQueries) pkiDeleteBinding(ctx context.Context, id, profileID string) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIDeleteBinding(ctx, pggen.PKIDeleteBindingParams{ID: id, ProfileID: profileID}))
}

func (q pgPKIStoreQueries) pkiBoundProfile(ctx context.Context, chain domain.Scope, name string) (PKIProfile, error) {
	c, err := q.queries.PKIBoundProfile(ctx, pggen.PKIBoundProfileParams{Name: name, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: pgtype.Text{String: string(chain.Env), Valid: string(chain.Env) != ""}})
	if isNoRows(err) {
		return PKIProfile{}, ErrNotFound
	}
	if err != nil {
		return PKIProfile{}, err
	}
	item, err := pgPKIProfile(pggen.PkiProfile(c))
	return item, err
}

func (q pgPKIStoreQueries) pkiBoundProfiles(ctx context.Context, chain domain.Scope) ([]PKIProfile, error) {
	c, err := q.queries.PKIBoundProfiles(ctx, pggen.PKIBoundProfilesParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: pgtype.Text{String: string(chain.Env), Valid: string(chain.Env) != ""}})
	if err != nil {
		return nil, err
	}
	return mapRows(c, func(c pggen.PkiProfile) (PKIProfile, error) { return pgPKIProfile(pggen.PkiProfile(c)) })
}

func (q pgPKIStoreQueries) pkiGetCertificate(ctx context.Context, chain domain.Scope, id string) (PKICertificate, error) {
	c, err := q.queries.PKIGetCertificate(ctx, pggen.PKIGetCertificateParams{ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return PKICertificate{}, ErrNotFound
	}
	if err != nil {
		return PKICertificate{}, err
	}
	item, err := pgPKICertificate(pggen.PKIGetCertificateRow(c))
	return item, err
}

func (q pgPKIStoreQueries) pkiListCertificates(ctx context.Context, chain domain.Scope) ([]PKICertificate, error) {
	c, err := q.queries.PKIListCertificates(ctx, pggen.PKIListCertificatesParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	return mapRows(c, func(c pggen.PKIListCertificatesRow) (PKICertificate, error) {
		return pgPKICertificate(pggen.PKIGetCertificateRow(c))
	})
}

func (q pgPKIStoreQueries) pkiFenceIssuance(ctx context.Context, id string) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIFenceIssuance(ctx, id))
}

func (q pgPKIStoreQueries) pkiCreateCertificate(ctx context.Context, chain domain.Scope, m PKICertificateCreate) error {
	return q.pkiWriteError(q.queries.PKICreateCertificate(ctx, pggen.PKICreateCertificateParams{ID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), ProfileID: m.ProfileID, ProfileName: m.ProfileName, IssuerID: m.IssuerID, Serial: m.Serial, KeySource: m.KeySource, KeyAlgorithm: m.KeyAlgorithm, KeyFingerprint: m.KeyFingerprint, CommonName: m.CommonName, Sans: m.SANs, NotBefore: pgRequiredTime(m.NotBefore), NotAfter: pgRequiredTime(m.NotAfter), PrincipalID: m.PrincipalID, PrincipalClass: m.PrincipalClass, RenewedFrom: pgtype.Text{String: m.RenewedFrom, Valid: m.RenewedFrom != ""}, IssuingDeadline: pgRequiredTime(m.IssuingDeadline), At: pgRequiredTime(m.At)}))
}

func (q pgPKIStoreQueries) pkiFinishCertificate(ctx context.Context, chain domain.Scope, id string, der []byte, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIFinishCertificate(ctx, pggen.PKIFinishCertificateParams{Der: der, At: pgRequiredTime(at), ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q pgPKIStoreQueries) pkiFailCertificate(ctx context.Context, chain domain.Scope, id string, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIFailCertificate(ctx, pggen.PKIFailCertificateParams{At: pgRequiredTime(at), ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q pgPKIStoreQueries) pkiClaimRenewal(ctx context.Context, chain domain.Scope, id, successorID string, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIClaimRenewal(ctx, pggen.PKIClaimRenewalParams{SuccessorID: pgtype.Text{String: successorID, Valid: true}, At: pgRequiredTime(at), ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q pgPKIStoreQueries) pkiCompleteRenewal(ctx context.Context, chain domain.Scope, id, successorID string, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKICompleteRenewal(ctx, pggen.PKICompleteRenewalParams{At: pgRequiredTime(at), ID: id, SuccessorID: pgtype.Text{String: successorID, Valid: true}, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q pgPKIStoreQueries) pkiReleaseRenewal(ctx context.Context, chain domain.Scope, id, successorID string, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIReleaseRenewal(ctx, pggen.PKIReleaseRenewalParams{At: pgRequiredTime(at), ID: id, SuccessorID: pgtype.Text{String: successorID, Valid: true}, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q pgPKIStoreQueries) pkiRevokeCertificate(ctx context.Context, chain domain.Scope, id, reason string, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.PKIRevokeCertificate(ctx, pggen.PKIRevokeCertificateParams{At: pgRequiredTime(at), Reason: pgtype.Text{String: reason, Valid: true}, ID: id, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}))
}

func (q pgPKIStoreQueries) runtimePKIStaleIssuing(ctx context.Context, now time.Time, limit int) ([]pkiSweptRow, error) {
	c, err := q.queries.RuntimePKIStaleIssuing(ctx, pggen.RuntimePKIStaleIssuingParams{Now: pgRequiredTime(now), PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(c, pgPKISwept)
}

func (q pgPKIStoreQueries) runtimePKIExpired(ctx context.Context, now time.Time, limit int) ([]pkiSweptRow, error) {
	c, err := q.queries.RuntimePKIExpired(ctx, pggen.RuntimePKIExpiredParams{Now: pgRequiredTime(now), PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(c, func(c pggen.RuntimePKIExpiredRow) (pkiSweptRow, error) {
		return pgPKISwept(pggen.RuntimePKIStaleIssuingRow(c))
	})
}

func (q pgPKIStoreQueries) runtimePKIMarkUnknown(ctx context.Context, row pkiSweptRow, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.RuntimePKIMarkUnknown(ctx, pggen.RuntimePKIMarkUnknownParams{At: pgRequiredTime(at), ID: row.id, OrgID: row.org}))
}

func (q pgPKIStoreQueries) runtimePKIMarkExpired(ctx context.Context, row pkiSweptRow, at time.Time) (int64, error) {
	return q.pkiWriteRows(q.queries.RuntimePKIMarkExpired(ctx, pggen.RuntimePKIMarkExpiredParams{At: pgRequiredTime(at), ID: row.id, OrgID: row.org}))
}

func (q pgPKIStoreQueries) runtimePKIReleaseRenewal(ctx context.Context, row pkiSweptRow, at time.Time) error {
	return q.pkiWriteError(q.queries.RuntimePKIReleaseRenewal(ctx, pggen.RuntimePKIReleaseRenewalParams{At: pgRequiredTime(at), ID: row.renewedFrom, OrgID: row.org, SuccessorID: pgtype.Text{String: row.id, Valid: true}}))
}

func (q pgPKIStoreQueries) runtimePKITransitionAudit(ctx context.Context, row pkiSweptRow, id, outcome, payload string, at time.Time) error {
	return q.pkiWriteError(q.queries.RuntimePKITransitionAudit(ctx, pggen.RuntimePKITransitionAuditParams{ID: id, At: pgRequiredTime(at), AuthorityID: pgtype.Text{String: row.principal, Valid: true}, OrgID: row.org, ProjectID: pgtype.Text{String: row.project, Valid: true}, EnvID: pgtype.Text{String: row.env, Valid: true}, CertificateID: pgtype.Text{String: row.id, Valid: true}, Outcome: outcome, Payload: payload}))
}

func (q pgPKIStoreQueries) runtimePKIDueCRLs(ctx context.Context, halfLife time.Time) ([]PKICRLCandidate, error) {
	c, err := q.queries.RuntimePKIDueCRLs(ctx, pgRequiredTime(halfLife))
	if err != nil {
		return nil, err
	}
	return mapRows(c, pgPKICandidate)
}

func (q pgPKIStoreQueries) runtimePKIPublishedAudit(ctx context.Context, id, issuerID, payload string, at time.Time) error {
	return q.pkiWriteError(q.queries.RuntimePKIPublishedAudit(ctx, pggen.RuntimePKIPublishedAuditParams{ID: id, At: pgRequiredTime(at), IssuerID: pgtype.Text{String: issuerID, Valid: true}, Payload: payload}))
}

func (q pgPKIStoreQueries) runtimePKICountLive(ctx context.Context, now time.Time) (int64, error) {
	return q.queries.RuntimePKICountLive(ctx, pgRequiredTime(now))
}

func (q pgPKIStoreQueries) runtimePKICountUnknown(ctx context.Context, now time.Time) (int64, error) {
	return q.queries.RuntimePKICountUnknown(ctx, pgRequiredTime(now))
}

func (q pgPKIStoreQueries) runtimePKICountHeld(ctx context.Context) (int64, error) {
	return q.queries.RuntimePKICountHeld(ctx)
}

func pgPKITime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: CanonTime(t), Valid: !t.IsZero()}
}
func sqlitePKITime(t time.Time) sql.NullString {
	return sql.NullString{String: fixedStamp(t), Valid: !t.IsZero()}
}
func (q sqlitePKIStoreQueries) pkiWriteError(err error) error {
	if q.mapConstraints {
		return constraint(err)
	}
	return err
}
func (q sqlitePKIStoreQueries) pkiWriteRows(n int64, err error) (int64, error) {
	return n, q.pkiWriteError(err)
}

func (q pgPKIStoreQueries) pkiWriteError(err error) error {
	if q.mapConstraints {
		return constraint(err)
	}
	return err
}
func (q pgPKIStoreQueries) pkiWriteRows(n int64, err error) (int64, error) {
	return n, q.pkiWriteError(err)
}
