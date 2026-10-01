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

type sshStoreQueries interface {
	sshCreateCA(ctx context.Context, chain domain.Scope, m SSHCACreate) (int64, error)
	sshInsertCAKey(ctx context.Context, chain domain.Scope, m SSHCAKeyCreate) (int64, error)
	sshRetireActiveCAKey(ctx context.Context, chain domain.Scope, keyID string, at, retireAfter time.Time) (int64, error)
	sshRetireCAKey(ctx context.Context, chain domain.Scope, keyID, caID string, at time.Time) (int64, error)
	sshDeleteCA(ctx context.Context, chain domain.Scope, caID string) (int64, error)
	sshRetireAllCAKeys(ctx context.Context, chain domain.Scope, caID string, at time.Time) (int64, error)
	sshDeleteProfile(ctx context.Context, chain domain.Scope, profileID string, at time.Time) (int64, error)
	sshRevokeCertificate(ctx context.Context, chain domain.Scope, certID, reason string, at time.Time) (int64, error)
	sshReencryptCAKey(ctx context.Context, chain domain.Scope, id string, newCiphertext, oldCiphertext []byte) (int64, error)
	sshCreateProfile(ctx context.Context, chain domain.Scope, m SSHProfileWrite, lists [4]string) (int64, error)
	sshUpdateProfile(ctx context.Context, chain domain.Scope, m SSHProfileWrite, lists [4]string) (int64, error)
	sshDeleteRequesters(ctx context.Context, chain domain.Scope, profileID string) (int64, error)
	sshInsertRequester(ctx context.Context, chain domain.Scope, profileID, principal string, at time.Time) (int64, error)
	sshInsertCertificate(ctx context.Context, chain domain.Scope, m SSHCertificateCreate, principals string) (int64, error)
	sshCAKeys(ctx context.Context, chain domain.Scope) ([]SSHCAKey, error)
	sshGetCA(ctx context.Context, chain domain.Scope, caID string) (SSHCA, error)
	sshListCAs(ctx context.Context, chain domain.Scope) ([]SSHCA, error)
	sshActiveCAKey(ctx context.Context, chain domain.Scope, caID string) (SSHActiveKey, error)
	sshActiveCAKeyID(ctx context.Context, chain domain.Scope, caID string) (string, error)
	sshCAKeyExists(ctx context.Context, chain domain.Scope, keyID string, caID string) (int64, error)
	sshCountLiveProfiles(ctx context.Context, chain domain.Scope, caID string) (int64, error)
	sshActiveKeyLastExpiry(ctx context.Context, chain domain.Scope, caID string, now time.Time) (*time.Time, error)
	sshRequesters(ctx context.Context, chain domain.Scope) (map[string][]string, error)
	sshGetProfile(ctx context.Context, chain domain.Scope, profileID string) (SSHProfile, error)
	sshListProfiles(ctx context.Context, chain domain.Scope) ([]SSHProfile, error)
	sshIsRequester(ctx context.Context, chain domain.Scope, profileID string, principalID string) (int64, error)
	sshGetCertificate(ctx context.Context, chain domain.Scope, certID string) (SSHCertificate, error)
	sshListCertificates(ctx context.Context, chain domain.Scope, limit int) ([]SSHCertificate, error)
	sshProfileRevocationCandidates(ctx context.Context, chain domain.Scope, profileID string, at time.Time) ([]SSHRevokedCertificate, error)
	sshRevokedSerials(ctx context.Context, chain domain.Scope, caID string, now time.Time, limit int) ([]SSHRevokedSerial, error)
	sshListKeysForReencrypt(ctx context.Context, chain domain.Scope, cursor string, limit int) ([]ReencryptFieldRow, error)
	sshCountLiveCAs(context.Context, domain.Scope) (int64, error)
	sshPurgeEnvironment(context.Context, domain.Scope) error
}
type sqliteSSHStoreQueries struct{ queries *sqlitegen.Queries }
type pgSSHStoreQueries struct{ queries *pggen.Queries }

func (q sqliteSSHStoreQueries) sshCreateCA(ctx context.Context, chain domain.Scope, m SSHCACreate) (int64, error) {
	rows, err := q.queries.SSHCreateCA(ctx, sqlitegen.SSHCreateCAParams{ID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), Name: m.Name, AuthorityPrincipalID: m.AuthorityPrincipalID, At: fixedStamp(m.At)})
	return rows, constraint(err)
}

func (q pgSSHStoreQueries) sshCreateCA(ctx context.Context, chain domain.Scope, m SSHCACreate) (int64, error) {
	rows, err := q.queries.SSHCreateCA(ctx, pggen.SSHCreateCAParams{ID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), Name: m.Name, AuthorityPrincipalID: m.AuthorityPrincipalID, At: pgRequiredTime(m.At)})
	return rows, constraint(err)
}

func (q sqliteSSHStoreQueries) sshInsertCAKey(ctx context.Context, chain domain.Scope, m SSHCAKeyCreate) (int64, error) {
	rows, err := q.queries.SSHInsertCAKey(ctx, sqlitegen.SSHInsertCAKeyParams{ID: m.ID, Algorithm: m.Algorithm, PublicKey: m.PublicKey, Fingerprint: m.Fingerprint, Origin: m.Origin, Ciphertext: m.Ciphertext, At: fixedStamp(m.At), CaID: m.CAID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q pgSSHStoreQueries) sshInsertCAKey(ctx context.Context, chain domain.Scope, m SSHCAKeyCreate) (int64, error) {
	rows, err := q.queries.SSHInsertCAKey(ctx, pggen.SSHInsertCAKeyParams{ID: m.ID, Algorithm: m.Algorithm, PublicKey: m.PublicKey, Fingerprint: m.Fingerprint, Origin: m.Origin, Ciphertext: m.Ciphertext, At: pgRequiredTime(m.At), CaID: m.CAID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q sqliteSSHStoreQueries) sshRetireActiveCAKey(ctx context.Context, chain domain.Scope, keyID string, at, retireAfter time.Time) (int64, error) {
	rows, err := q.queries.SSHRetireActiveCAKey(ctx, sqlitegen.SSHRetireActiveCAKeyParams{At: runtimeSQLiteStamp(at), RetireAfter: runtimeSQLiteStamp(retireAfter), KeyID: keyID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q pgSSHStoreQueries) sshRetireActiveCAKey(ctx context.Context, chain domain.Scope, keyID string, at, retireAfter time.Time) (int64, error) {
	rows, err := q.queries.SSHRetireActiveCAKey(ctx, pggen.SSHRetireActiveCAKeyParams{At: pgRequiredTime(at), RetireAfter: pgRequiredTime(retireAfter), KeyID: keyID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q sqliteSSHStoreQueries) sshRetireCAKey(ctx context.Context, chain domain.Scope, keyID, caID string, at time.Time) (int64, error) {
	rows, err := q.queries.SSHRetireCAKey(ctx, sqlitegen.SSHRetireCAKeyParams{At: runtimeSQLiteStamp(at), KeyID: keyID, CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q pgSSHStoreQueries) sshRetireCAKey(ctx context.Context, chain domain.Scope, keyID, caID string, at time.Time) (int64, error) {
	rows, err := q.queries.SSHRetireCAKey(ctx, pggen.SSHRetireCAKeyParams{At: pgRequiredTime(at), KeyID: keyID, CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q sqliteSSHStoreQueries) sshDeleteCA(ctx context.Context, chain domain.Scope, caID string) (int64, error) {
	rows, err := q.queries.SSHDeleteCA(ctx, sqlitegen.SSHDeleteCAParams{CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q pgSSHStoreQueries) sshDeleteCA(ctx context.Context, chain domain.Scope, caID string) (int64, error) {
	rows, err := q.queries.SSHDeleteCA(ctx, pggen.SSHDeleteCAParams{CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q sqliteSSHStoreQueries) sshRetireAllCAKeys(ctx context.Context, chain domain.Scope, caID string, at time.Time) (int64, error) {
	rows, err := q.queries.SSHRetireAllCAKeys(ctx, sqlitegen.SSHRetireAllCAKeysParams{At: runtimeSQLiteStamp(at), CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q pgSSHStoreQueries) sshRetireAllCAKeys(ctx context.Context, chain domain.Scope, caID string, at time.Time) (int64, error) {
	rows, err := q.queries.SSHRetireAllCAKeys(ctx, pggen.SSHRetireAllCAKeysParams{At: pgRequiredTime(at), CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q sqliteSSHStoreQueries) sshDeleteProfile(ctx context.Context, chain domain.Scope, profileID string, at time.Time) (int64, error) {
	rows, err := q.queries.SSHDeleteProfile(ctx, sqlitegen.SSHDeleteProfileParams{At: fixedStamp(at), ProfileID: profileID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q pgSSHStoreQueries) sshDeleteProfile(ctx context.Context, chain domain.Scope, profileID string, at time.Time) (int64, error) {
	rows, err := q.queries.SSHDeleteProfile(ctx, pggen.SSHDeleteProfileParams{At: pgRequiredTime(at), ProfileID: profileID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q sqliteSSHStoreQueries) sshRevokeCertificate(ctx context.Context, chain domain.Scope, certID, reason string, at time.Time) (int64, error) {
	rows, err := q.queries.SSHRevokeCertificate(ctx, sqlitegen.SSHRevokeCertificateParams{At: runtimeSQLiteStamp(at), Reason: sql.NullString{String: reason, Valid: true}, CertificateID: certID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q pgSSHStoreQueries) sshRevokeCertificate(ctx context.Context, chain domain.Scope, certID, reason string, at time.Time) (int64, error) {
	rows, err := q.queries.SSHRevokeCertificate(ctx, pggen.SSHRevokeCertificateParams{At: pgRequiredTime(at), Reason: pgtype.Text{String: reason, Valid: true}, CertificateID: certID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q sqliteSSHStoreQueries) sshReencryptCAKey(ctx context.Context, chain domain.Scope, id string, newCiphertext, oldCiphertext []byte) (int64, error) {
	rows, err := q.queries.SSHReencryptCAKey(ctx, sqlitegen.SSHReencryptCAKeyParams{NewCiphertext: newCiphertext, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ID: id, OldCiphertext: oldCiphertext})
	return rows, constraint(err)
}

func (q pgSSHStoreQueries) sshReencryptCAKey(ctx context.Context, chain domain.Scope, id string, newCiphertext, oldCiphertext []byte) (int64, error) {
	rows, err := q.queries.SSHReencryptCAKey(ctx, pggen.SSHReencryptCAKeyParams{NewCiphertext: newCiphertext, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ID: id, OldCiphertext: oldCiphertext})
	return rows, constraint(err)
}

func (q sqliteSSHStoreQueries) sshCreateProfile(ctx context.Context, chain domain.Scope, m SSHProfileWrite, lists [4]string) (int64, error) {
	rows, err := q.queries.SSHCreateProfile(ctx, sqlitegen.SSHCreateProfileParams{ID: m.ID, Name: m.Name, Principals: lists[0], ForceCommand: m.ForceCommand, SourceAddresses: lists[1], Extensions: lists[2], KeyAlgorithms: lists[3], DefaultTtlSeconds: m.DefaultTTLSeconds, MaxTtlSeconds: m.MaxTTLSeconds, State: profileState(m.Enabled), At: fixedStamp(m.At), CaID: m.CAID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q pgSSHStoreQueries) sshCreateProfile(ctx context.Context, chain domain.Scope, m SSHProfileWrite, lists [4]string) (int64, error) {
	rows, err := q.queries.SSHCreateProfile(ctx, pggen.SSHCreateProfileParams{ID: m.ID, Name: m.Name, Principals: lists[0], ForceCommand: m.ForceCommand, SourceAddresses: lists[1], Extensions: lists[2], KeyAlgorithms: lists[3], DefaultTtlSeconds: m.DefaultTTLSeconds, MaxTtlSeconds: m.MaxTTLSeconds, State: profileState(m.Enabled), At: pgRequiredTime(m.At), CaID: m.CAID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q sqliteSSHStoreQueries) sshUpdateProfile(ctx context.Context, chain domain.Scope, m SSHProfileWrite, lists [4]string) (int64, error) {
	rows, err := q.queries.SSHUpdateProfile(ctx, sqlitegen.SSHUpdateProfileParams{Name: m.Name, Principals: lists[0], ForceCommand: m.ForceCommand, SourceAddresses: lists[1], Extensions: lists[2], KeyAlgorithms: lists[3], DefaultTtlSeconds: m.DefaultTTLSeconds, MaxTtlSeconds: m.MaxTTLSeconds, State: profileState(m.Enabled), At: fixedStamp(m.At), ID: m.ID, CaID: m.CAID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q pgSSHStoreQueries) sshUpdateProfile(ctx context.Context, chain domain.Scope, m SSHProfileWrite, lists [4]string) (int64, error) {
	rows, err := q.queries.SSHUpdateProfile(ctx, pggen.SSHUpdateProfileParams{Name: m.Name, Principals: lists[0], ForceCommand: m.ForceCommand, SourceAddresses: lists[1], Extensions: lists[2], KeyAlgorithms: lists[3], DefaultTtlSeconds: m.DefaultTTLSeconds, MaxTtlSeconds: m.MaxTTLSeconds, State: profileState(m.Enabled), At: pgRequiredTime(m.At), ID: m.ID, CaID: m.CAID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q sqliteSSHStoreQueries) sshDeleteRequesters(ctx context.Context, chain domain.Scope, profileID string) (int64, error) {
	rows, err := q.queries.SSHDeleteRequesters(ctx, sqlitegen.SSHDeleteRequestersParams{ProfileID: profileID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q pgSSHStoreQueries) sshDeleteRequesters(ctx context.Context, chain domain.Scope, profileID string) (int64, error) {
	rows, err := q.queries.SSHDeleteRequesters(ctx, pggen.SSHDeleteRequestersParams{ProfileID: profileID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q sqliteSSHStoreQueries) sshInsertRequester(ctx context.Context, chain domain.Scope, profileID, principal string, at time.Time) (int64, error) {
	rows, err := q.queries.SSHInsertRequester(ctx, sqlitegen.SSHInsertRequesterParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), ProfileID: profileID, Principal: principal, At: fixedStamp(at)})
	return rows, constraint(err)
}

func (q pgSSHStoreQueries) sshInsertRequester(ctx context.Context, chain domain.Scope, profileID, principal string, at time.Time) (int64, error) {
	rows, err := q.queries.SSHInsertRequester(ctx, pggen.SSHInsertRequesterParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), ProfileID: profileID, Principal: principal, At: pgRequiredTime(at)})
	return rows, constraint(err)
}

func (q sqliteSSHStoreQueries) sshInsertCertificate(ctx context.Context, chain domain.Scope, m SSHCertificateCreate, principals string) (int64, error) {
	rows, err := q.queries.SSHInsertCertificate(ctx, sqlitegen.SSHInsertCertificateParams{ID: m.ID, Serial: m.Serial, KeyID: m.KeyID, Principals: principals, PublicKeyFingerprint: m.PublicKeyFingerprint, KeyAlgorithm: m.KeyAlgorithm, KeyOrigin: m.KeyOrigin, ValidAfter: fixedStamp(m.ValidAfter), ValidBefore: fixedStamp(m.ValidBefore), RequesterPrincipalID: m.RequesterPrincipalID, RequesterClass: m.RequesterClass, At: fixedStamp(m.At), CaKeyID: m.CAKeyID, CaID: m.CAID, ProfileID: m.ProfileID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q pgSSHStoreQueries) sshInsertCertificate(ctx context.Context, chain domain.Scope, m SSHCertificateCreate, principals string) (int64, error) {
	rows, err := q.queries.SSHInsertCertificate(ctx, pggen.SSHInsertCertificateParams{ID: m.ID, Serial: m.Serial, KeyID: m.KeyID, Principals: principals, PublicKeyFingerprint: m.PublicKeyFingerprint, KeyAlgorithm: m.KeyAlgorithm, KeyOrigin: m.KeyOrigin, ValidAfter: pgRequiredTime(m.ValidAfter), ValidBefore: pgRequiredTime(m.ValidBefore), RequesterPrincipalID: m.RequesterPrincipalID, RequesterClass: m.RequesterClass, At: pgRequiredTime(m.At), CaKeyID: m.CAKeyID, CaID: m.CAID, ProfileID: m.ProfileID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return rows, constraint(err)
}

func (q sqliteSSHStoreQueries) sshCountLiveCAs(ctx context.Context, chain domain.Scope) (int64, error) {
	return q.queries.SSHCountLiveCAs(ctx, sqlitegen.SSHCountLiveCAsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q sqliteSSHStoreQueries) sshPurgeEnvironment(ctx context.Context, chain domain.Scope) error {
	if err := q.queries.SSHPurgeCertificates(ctx, sqlitegen.SSHPurgeCertificatesParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}); err != nil {
		return constraint(err)
	}
	if err := q.queries.SSHPurgeRequesters(ctx, sqlitegen.SSHPurgeRequestersParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}); err != nil {
		return constraint(err)
	}
	if err := q.queries.SSHPurgeProfiles(ctx, sqlitegen.SSHPurgeProfilesParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}); err != nil {
		return constraint(err)
	}
	if err := q.queries.SSHPurgeCAKeys(ctx, sqlitegen.SSHPurgeCAKeysParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}); err != nil {
		return constraint(err)
	}
	if err := q.queries.SSHPurgeCAs(ctx, sqlitegen.SSHPurgeCAsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}); err != nil {
		return constraint(err)
	}
	return nil
}

func (q pgSSHStoreQueries) sshCountLiveCAs(ctx context.Context, chain domain.Scope) (int64, error) {
	return q.queries.SSHCountLiveCAs(ctx, pggen.SSHCountLiveCAsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q pgSSHStoreQueries) sshPurgeEnvironment(ctx context.Context, chain domain.Scope) error {
	if err := q.queries.SSHPurgeCertificates(ctx, pggen.SSHPurgeCertificatesParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}); err != nil {
		return constraint(err)
	}
	if err := q.queries.SSHPurgeRequesters(ctx, pggen.SSHPurgeRequestersParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}); err != nil {
		return constraint(err)
	}
	if err := q.queries.SSHPurgeProfiles(ctx, pggen.SSHPurgeProfilesParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}); err != nil {
		return constraint(err)
	}
	if err := q.queries.SSHPurgeCAKeys(ctx, pggen.SSHPurgeCAKeysParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}); err != nil {
		return constraint(err)
	}
	if err := q.queries.SSHPurgeCAs(ctx, pggen.SSHPurgeCAsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)}); err != nil {
		return constraint(err)
	}
	return nil
}
