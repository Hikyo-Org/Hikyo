package store

import (
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// Typed generated projections preserve the public row shape and timestamp errors.
func pkiReadTime(value string) (time.Time, error) {
	parsed, err := readStoredTime(value)
	if err != nil || parsed == nil {
		return time.Time{}, err
	}
	return *parsed, nil
}

func sqlitePKIIssuer(c sqlitegen.PKIGetIssuerRow) (PKIIssuer, error) {
	out := PKIIssuer{ID: c.ID, Name: c.Name, Version: int64(c.Version), Kind: c.Kind, Origin: c.Origin, ParentID: c.ParentID, State: c.State, KeyAlgorithm: c.KeyAlgorithm, KeyFingerprint: c.KeyFingerprint, KeyPresent: c.KeyPresent == 1, CertificateDER: c.CertificateDer, CSRDER: c.CsrDer, ChainPEM: c.ChainPem, SubjectCN: c.SubjectCn, SubjectOrg: c.SubjectOrg, CRLDistributionURL: c.CrlDistributionUrl, RestoreHold: c.RestoreHold == 1, IssuedCount: c.IssuedCount, CRLDER: c.CrlDer, CRLNumber: c.CrlNumber, RevocationSeq: c.RevocationSeq, RowVersion: c.RowVersion, CreatedBy: c.CreatedBy}
	var err error
	out.NotBefore, err = pkiReadTime(c.NotBefore.String)
	if err != nil {
		return out, err
	}
	out.NotAfter, err = pkiReadTime(c.NotAfter.String)
	if err != nil {
		return out, err
	}
	out.CRLThisUpdate, err = pkiReadTime(c.CrlThisUpdate.String)
	if err != nil {
		return out, err
	}
	out.CRLNextUpdate, err = pkiReadTime(c.CrlNextUpdate.String)
	if err != nil {
		return out, err
	}
	out.CreatedAt, err = pkiReadTime(c.CreatedAt)
	if err != nil {
		return out, err
	}
	out.UpdatedAt, err = pkiReadTime(c.UpdatedAt)
	if err != nil {
		return out, err
	}
	return out, nil
}

func sqlitePKIProfile(c sqlitegen.PkiProfile) (PKIProfile, error) {
	out := PKIProfile{ID: c.ID, Name: c.Name, Policy: c.Policy, RowVersion: c.RowVersion, CreatedBy: c.CreatedBy}
	var err error
	out.CreatedAt, err = pkiReadTime(c.CreatedAt)
	if err != nil {
		return out, err
	}
	out.UpdatedAt, err = pkiReadTime(c.UpdatedAt)
	if err != nil {
		return out, err
	}
	return out, nil
}

func sqlitePKICertificate(c sqlitegen.PKIGetCertificateRow) (PKICertificate, error) {
	out := PKICertificate{ID: c.ID, EnvironmentID: c.EnvironmentID, ProfileID: c.ProfileID, ProfileName: c.ProfileName, IssuerID: c.IssuerID, Serial: c.Serial, State: c.State, KeySource: c.KeySource, KeyAlgorithm: c.KeyAlgorithm, KeyFingerprint: c.KeyFingerprint, CommonName: c.CommonName, SANs: c.Sans, CertificateDER: c.CertificateDer, PrincipalID: c.PrincipalID, PrincipalClass: c.PrincipalClass, RenewedFrom: c.RenewedFrom, RenewedBy: c.RenewedBy, RevocationReason: c.RevocationReason, RowVersion: c.RowVersion}
	var err error
	out.NotBefore, err = pkiReadTime(c.NotBefore)
	if err != nil {
		return out, err
	}
	out.NotAfter, err = pkiReadTime(c.NotAfter)
	if err != nil {
		return out, err
	}
	out.RevokedAt, err = pkiReadTime(c.RevokedAt.String)
	if err != nil {
		return out, err
	}
	out.IssuingDeadline, err = pkiReadTime(c.IssuingDeadline)
	if err != nil {
		return out, err
	}
	out.CreatedAt, err = pkiReadTime(c.CreatedAt)
	if err != nil {
		return out, err
	}
	out.UpdatedAt, err = pkiReadTime(c.UpdatedAt)
	if err != nil {
		return out, err
	}
	return out, nil
}

func sqlitePKIBinding(c sqlitegen.PKIListBindingsRow) (PKIProfileBinding, error) {
	out := PKIProfileBinding{ID: c.ID, ProfileID: c.ProfileID, OrgID: c.OrgID, ProjectID: c.ProjectID, EnvironmentID: c.EnvironmentID, CreatedBy: c.CreatedBy}
	var err error
	out.CreatedAt, err = pkiReadTime(c.CreatedAt)
	if err != nil {
		return out, err
	}
	return out, nil
}

func sqlitePKIRevocation(c sqlitegen.PKIRevokedEntriesRow) (PKIRevokedEntry, error) {
	out := PKIRevokedEntry{Serial: c.Serial, Reason: c.Reason}
	var err error
	out.RevokedAt, err = pkiReadTime(c.RevokedAt)
	if err != nil {
		return out, err
	}
	return out, nil
}

func sqlitePKIChild(c sqlitegen.PKIRevokedChildrenRow) (pkiRevokedChild, error) {
	out := pkiRevokedChild{der: c.CertificateDer}
	var err error
	out.updatedAt, err = pkiReadTime(c.UpdatedAt)
	if err != nil {
		return out, err
	}
	return out, nil
}

func sqlitePKISwept(c sqlitegen.RuntimePKIStaleIssuingRow) (pkiSweptRow, error) {
	out := pkiSweptRow{id: c.ID, org: c.OrgID, project: c.ProjectID, env: c.EnvironmentID, serial: c.Serial, principal: c.PrincipalID, issuerName: c.IssuerName, renewedFrom: c.RenewedFrom}
	return out, nil
}

func sqlitePKICandidate(c sqlitegen.RuntimePKIDueCRLsRow) (PKICRLCandidate, error) {
	version, err := checkedStoredDEKVersion(c.DekVersion.Int64)
	if err != nil {
		return PKICRLCandidate{}, err
	}
	out := PKICRLCandidate{IssuerID: c.ID, Name: c.Name, Version: int64(c.Version), CertificateDER: c.CertificateDer, EncryptedPrivateKey: c.EncryptedPrivateKey, DEKVersion: version, CRLNumber: c.CrlNumber, RevocationSeq: c.RevocationSeq}
	return out, nil
}

func pgPKIIssuer(c pggen.PKIGetIssuerRow) (PKIIssuer, error) {
	out := PKIIssuer{ID: c.ID, Name: c.Name, Version: int64(c.Version), Kind: c.Kind, Origin: c.Origin, ParentID: c.ParentID, State: c.State, KeyAlgorithm: c.KeyAlgorithm, KeyFingerprint: c.KeyFingerprint, KeyPresent: c.KeyPresent == 1, CertificateDER: c.CertificateDer, CSRDER: c.CsrDer, ChainPEM: c.ChainPem, SubjectCN: c.SubjectCn, SubjectOrg: c.SubjectOrg, CRLDistributionURL: c.CrlDistributionUrl, RestoreHold: c.RestoreHold == 1, IssuedCount: c.IssuedCount, CRLDER: c.CrlDer, CRLNumber: c.CrlNumber, RevocationSeq: c.RevocationSeq, RowVersion: c.RowVersion, CreatedBy: c.CreatedBy}
	var err error
	out.NotBefore, err = pkiReadTime(pgStoredStamp(c.NotBefore))
	if err != nil {
		return out, err
	}
	out.NotAfter, err = pkiReadTime(pgStoredStamp(c.NotAfter))
	if err != nil {
		return out, err
	}
	out.CRLThisUpdate, err = pkiReadTime(pgStoredStamp(c.CrlThisUpdate))
	if err != nil {
		return out, err
	}
	out.CRLNextUpdate, err = pkiReadTime(pgStoredStamp(c.CrlNextUpdate))
	if err != nil {
		return out, err
	}
	out.CreatedAt, err = pkiReadTime(pgStoredStamp(c.CreatedAt))
	if err != nil {
		return out, err
	}
	out.UpdatedAt, err = pkiReadTime(pgStoredStamp(c.UpdatedAt))
	if err != nil {
		return out, err
	}
	return out, nil
}

func pgPKIProfile(c pggen.PkiProfile) (PKIProfile, error) {
	out := PKIProfile{ID: c.ID, Name: c.Name, Policy: c.Policy, RowVersion: c.RowVersion, CreatedBy: c.CreatedBy}
	var err error
	out.CreatedAt, err = pkiReadTime(pgStoredStamp(c.CreatedAt))
	if err != nil {
		return out, err
	}
	out.UpdatedAt, err = pkiReadTime(pgStoredStamp(c.UpdatedAt))
	if err != nil {
		return out, err
	}
	return out, nil
}

func pgPKICertificate(c pggen.PKIGetCertificateRow) (PKICertificate, error) {
	out := PKICertificate{ID: c.ID, EnvironmentID: c.EnvironmentID, ProfileID: c.ProfileID, ProfileName: c.ProfileName, IssuerID: c.IssuerID, Serial: c.Serial, State: c.State, KeySource: c.KeySource, KeyAlgorithm: c.KeyAlgorithm, KeyFingerprint: c.KeyFingerprint, CommonName: c.CommonName, SANs: c.Sans, CertificateDER: c.CertificateDer, PrincipalID: c.PrincipalID, PrincipalClass: c.PrincipalClass, RenewedFrom: c.RenewedFrom, RenewedBy: c.RenewedBy, RevocationReason: c.RevocationReason, RowVersion: c.RowVersion}
	var err error
	out.NotBefore, err = pkiReadTime(pgStoredStamp(c.NotBefore))
	if err != nil {
		return out, err
	}
	out.NotAfter, err = pkiReadTime(pgStoredStamp(c.NotAfter))
	if err != nil {
		return out, err
	}
	out.RevokedAt, err = pkiReadTime(pgStoredStamp(c.RevokedAt))
	if err != nil {
		return out, err
	}
	out.IssuingDeadline, err = pkiReadTime(pgStoredStamp(c.IssuingDeadline))
	if err != nil {
		return out, err
	}
	out.CreatedAt, err = pkiReadTime(pgStoredStamp(c.CreatedAt))
	if err != nil {
		return out, err
	}
	out.UpdatedAt, err = pkiReadTime(pgStoredStamp(c.UpdatedAt))
	if err != nil {
		return out, err
	}
	return out, nil
}

func pgPKIBinding(c pggen.PKIListBindingsRow) (PKIProfileBinding, error) {
	out := PKIProfileBinding{ID: c.ID, ProfileID: c.ProfileID, OrgID: c.OrgID, ProjectID: c.ProjectID, EnvironmentID: c.EnvironmentID, CreatedBy: c.CreatedBy}
	var err error
	out.CreatedAt, err = pkiReadTime(pgStoredStamp(c.CreatedAt))
	if err != nil {
		return out, err
	}
	return out, nil
}

func pgPKIRevocation(c pggen.PKIRevokedEntriesRow) (PKIRevokedEntry, error) {
	out := PKIRevokedEntry{Serial: c.Serial, Reason: c.Reason}
	var err error
	out.RevokedAt, err = pkiReadTime(pgStoredStamp(c.RevokedAt))
	if err != nil {
		return out, err
	}
	return out, nil
}

func pgPKIChild(c pggen.PKIRevokedChildrenRow) (pkiRevokedChild, error) {
	out := pkiRevokedChild{der: c.CertificateDer}
	var err error
	out.updatedAt, err = pkiReadTime(pgStoredStamp(c.UpdatedAt))
	if err != nil {
		return out, err
	}
	return out, nil
}

func pgPKISwept(c pggen.RuntimePKIStaleIssuingRow) (pkiSweptRow, error) {
	out := pkiSweptRow{id: c.ID, org: c.OrgID, project: c.ProjectID, env: c.EnvironmentID, serial: c.Serial, principal: c.PrincipalID, issuerName: c.IssuerName, renewedFrom: c.RenewedFrom}
	return out, nil
}

func pgPKICandidate(c pggen.RuntimePKIDueCRLsRow) (PKICRLCandidate, error) {
	version, err := checkedStoredDEKVersion(c.DekVersion.Int64)
	if err != nil {
		return PKICRLCandidate{}, err
	}
	out := PKICRLCandidate{IssuerID: c.ID, Name: c.Name, Version: int64(c.Version), CertificateDER: c.CertificateDer, EncryptedPrivateKey: c.EncryptedPrivateKey, DEKVersion: version, CRLNumber: c.CrlNumber, RevocationSeq: c.RevocationSeq}
	return out, nil
}
