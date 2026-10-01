package store

import (
	"context"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

func decodeSSHProfileLists(out *SSHProfile, principals, sources, extensions, algorithms string) error {
	for _, f := range []struct {
		src string
		dst *[]string
	}{{principals, &out.Principals}, {sources, &out.SourceAddresses}, {extensions, &out.Extensions}, {algorithms, &out.KeyAlgorithms}} {
		var err error
		*f.dst, err = decodeSSHList(f.src)
		if err != nil {
			return err
		}
	}
	return nil
}

func (q sqliteSSHStoreQueries) sshCAKeys(ctx context.Context, chain domain.Scope) ([]SSHCAKey, error) {
	rows, err := q.queries.SSHCAKeys(ctx, sqlitegen.SSHCAKeysParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.SSHCAKeysRow) (SSHCAKey, error) {
		out := SSHCAKey{ID: c.ID, CAID: c.CaID, Algorithm: c.Algorithm, PublicKey: c.PublicKey, Fingerprint: c.Fingerprint, Origin: c.Origin, State: c.State, CreatedAt: c.CreatedAt, RetiringAt: c.RetiringAt.String, RetireAfter: c.RetireAfter.String, RetiredAt: c.RetiredAt.String}
		if err := normalizeStoredTimes(&out.CreatedAt, &out.RetiringAt, &out.RetireAfter, &out.RetiredAt); err != nil {
			return SSHCAKey{}, err
		}
		return out, nil
	})
}

func (q pgSSHStoreQueries) sshCAKeys(ctx context.Context, chain domain.Scope) ([]SSHCAKey, error) {
	rows, err := q.queries.SSHCAKeys(ctx, pggen.SSHCAKeysParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.SSHCAKeysRow) (SSHCAKey, error) {
		out := SSHCAKey{ID: c.ID, CAID: c.CaID, Algorithm: c.Algorithm, PublicKey: c.PublicKey, Fingerprint: c.Fingerprint, Origin: c.Origin, State: c.State, CreatedAt: pgStoredStamp(c.CreatedAt), RetiringAt: pgStoredStamp(c.RetiringAt), RetireAfter: pgStoredStamp(c.RetireAfter), RetiredAt: pgStoredStamp(c.RetiredAt)}
		if err := normalizeStoredTimes(&out.CreatedAt, &out.RetiringAt, &out.RetireAfter, &out.RetiredAt); err != nil {
			return SSHCAKey{}, err
		}
		return out, nil
	})
}

func (q sqliteSSHStoreQueries) sshGetCA(ctx context.Context, chain domain.Scope, caID string) (SSHCA, error) {
	c, err := q.queries.SSHGetCA(ctx, sqlitegen.SSHGetCAParams{CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return SSHCA{}, ErrNotFound
	}
	if err != nil {
		return SSHCA{}, err
	}
	return sqliteSshCA(c)
}

func (q pgSSHStoreQueries) sshGetCA(ctx context.Context, chain domain.Scope, caID string) (SSHCA, error) {
	c, err := q.queries.SSHGetCA(ctx, pggen.SSHGetCAParams{CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return SSHCA{}, ErrNotFound
	}
	if err != nil {
		return SSHCA{}, err
	}
	return pgSshCA(c)
}

func (q sqliteSSHStoreQueries) sshListCAs(ctx context.Context, chain domain.Scope) ([]SSHCA, error) {
	rows, err := q.queries.SSHListCAs(ctx, sqlitegen.SSHListCAsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.SSHListCAsRow) (SSHCA, error) {
		return sqliteSshCA(sqlitegen.SSHGetCARow(c))
	})
}

func (q pgSSHStoreQueries) sshListCAs(ctx context.Context, chain domain.Scope) ([]SSHCA, error) {
	rows, err := q.queries.SSHListCAs(ctx, pggen.SSHListCAsParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.SSHListCAsRow) (SSHCA, error) {
		return pgSshCA(pggen.SSHGetCARow(c))
	})
}

func (q sqliteSSHStoreQueries) sshActiveCAKey(ctx context.Context, chain domain.Scope, caID string) (SSHActiveKey, error) {
	c, err := q.queries.SSHActiveCAKey(ctx, sqlitegen.SSHActiveCAKeyParams{CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return SSHActiveKey{}, ErrNotFound
	}
	if err != nil {
		return SSHActiveKey{}, err
	}
	out := SSHActiveKey{KeyID: c.ID, Algorithm: c.Algorithm, PublicKey: c.PublicKey, Ciphertext: c.PrivateKeyCiphertext}
	return out, nil
}

func (q pgSSHStoreQueries) sshActiveCAKey(ctx context.Context, chain domain.Scope, caID string) (SSHActiveKey, error) {
	c, err := q.queries.SSHActiveCAKey(ctx, pggen.SSHActiveCAKeyParams{CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return SSHActiveKey{}, ErrNotFound
	}
	if err != nil {
		return SSHActiveKey{}, err
	}
	out := SSHActiveKey{KeyID: c.ID, Algorithm: c.Algorithm, PublicKey: c.PublicKey, Ciphertext: c.PrivateKeyCiphertext}
	return out, nil
}

func (q sqliteSSHStoreQueries) sshActiveCAKeyID(ctx context.Context, chain domain.Scope, caID string) (string, error) {
	return q.queries.SSHActiveCAKeyID(ctx, sqlitegen.SSHActiveCAKeyIDParams{CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q pgSSHStoreQueries) sshActiveCAKeyID(ctx context.Context, chain domain.Scope, caID string) (string, error) {
	return q.queries.SSHActiveCAKeyID(ctx, pggen.SSHActiveCAKeyIDParams{CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q sqliteSSHStoreQueries) sshCAKeyExists(ctx context.Context, chain domain.Scope, keyID string, caID string) (int64, error) {
	return q.queries.SSHCAKeyExists(ctx, sqlitegen.SSHCAKeyExistsParams{KeyID: keyID, CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q pgSSHStoreQueries) sshCAKeyExists(ctx context.Context, chain domain.Scope, keyID string, caID string) (int64, error) {
	return q.queries.SSHCAKeyExists(ctx, pggen.SSHCAKeyExistsParams{KeyID: keyID, CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q sqliteSSHStoreQueries) sshCountLiveProfiles(ctx context.Context, chain domain.Scope, caID string) (int64, error) {
	return q.queries.SSHCountLiveProfiles(ctx, sqlitegen.SSHCountLiveProfilesParams{CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q pgSSHStoreQueries) sshCountLiveProfiles(ctx context.Context, chain domain.Scope, caID string) (int64, error) {
	return q.queries.SSHCountLiveProfiles(ctx, pggen.SSHCountLiveProfilesParams{CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q sqliteSSHStoreQueries) sshActiveKeyLastExpiry(ctx context.Context, chain domain.Scope, caID string, now time.Time) (*time.Time, error) {
	value, err := q.queries.SSHActiveKeyLastExpiry(ctx, sqlitegen.SSHActiveKeyLastExpiryParams{CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), Now: fixedStamp(now)})
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return readStoredTime(value)
}

func (q pgSSHStoreQueries) sshActiveKeyLastExpiry(ctx context.Context, chain domain.Scope, caID string, now time.Time) (*time.Time, error) {
	value, err := q.queries.SSHActiveKeyLastExpiry(ctx, pggen.SSHActiveKeyLastExpiryParams{CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), Now: pgRequiredTime(now)})
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return readStoredTime(pgStoredStamp(value))
}

func (q sqliteSSHStoreQueries) sshRequesters(ctx context.Context, chain domain.Scope) (map[string][]string, error) {
	rows, err := q.queries.SSHRequesters(ctx, sqlitegen.SSHRequestersParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, c := range rows {
		out[c.ProfileID] = append(out[c.ProfileID], c.PrincipalID)
	}
	return out, nil
}

func (q pgSSHStoreQueries) sshRequesters(ctx context.Context, chain domain.Scope) (map[string][]string, error) {
	rows, err := q.queries.SSHRequesters(ctx, pggen.SSHRequestersParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, c := range rows {
		out[c.ProfileID] = append(out[c.ProfileID], c.PrincipalID)
	}
	return out, nil
}

func (q sqliteSSHStoreQueries) sshGetProfile(ctx context.Context, chain domain.Scope, profileID string) (SSHProfile, error) {
	c, err := q.queries.SSHGetProfile(ctx, sqlitegen.SSHGetProfileParams{ProfileID: profileID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return SSHProfile{}, ErrNotFound
	}
	if err != nil {
		return SSHProfile{}, err
	}
	return sqliteSshProfile(c)
}

func (q pgSSHStoreQueries) sshGetProfile(ctx context.Context, chain domain.Scope, profileID string) (SSHProfile, error) {
	c, err := q.queries.SSHGetProfile(ctx, pggen.SSHGetProfileParams{ProfileID: profileID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return SSHProfile{}, ErrNotFound
	}
	if err != nil {
		return SSHProfile{}, err
	}
	return pgSshProfile(c)
}

func (q sqliteSSHStoreQueries) sshListProfiles(ctx context.Context, chain domain.Scope) ([]SSHProfile, error) {
	rows, err := q.queries.SSHListProfiles(ctx, sqlitegen.SSHListProfilesParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.SSHListProfilesRow) (SSHProfile, error) {
		return sqliteSshProfile(sqlitegen.SSHGetProfileRow(c))
	})
}

func (q pgSSHStoreQueries) sshListProfiles(ctx context.Context, chain domain.Scope) ([]SSHProfile, error) {
	rows, err := q.queries.SSHListProfiles(ctx, pggen.SSHListProfilesParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.SSHListProfilesRow) (SSHProfile, error) {
		return pgSshProfile(pggen.SSHGetProfileRow(c))
	})
}

func (q sqliteSSHStoreQueries) sshIsRequester(ctx context.Context, chain domain.Scope, profileID string, principalID string) (int64, error) {
	return q.queries.SSHIsRequester(ctx, sqlitegen.SSHIsRequesterParams{ProfileID: profileID, PrincipalID: principalID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q pgSSHStoreQueries) sshIsRequester(ctx context.Context, chain domain.Scope, profileID string, principalID string) (int64, error) {
	return q.queries.SSHIsRequester(ctx, pggen.SSHIsRequesterParams{ProfileID: profileID, PrincipalID: principalID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
}

func (q sqliteSSHStoreQueries) sshGetCertificate(ctx context.Context, chain domain.Scope, certID string) (SSHCertificate, error) {
	c, err := q.queries.SSHGetCertificate(ctx, sqlitegen.SSHGetCertificateParams{CertificateID: certID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return SSHCertificate{}, ErrNotFound
	}
	if err != nil {
		return SSHCertificate{}, err
	}
	return sqliteSshCertificate(c)
}

func (q pgSSHStoreQueries) sshGetCertificate(ctx context.Context, chain domain.Scope, certID string) (SSHCertificate, error) {
	c, err := q.queries.SSHGetCertificate(ctx, pggen.SSHGetCertificateParams{CertificateID: certID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return SSHCertificate{}, ErrNotFound
	}
	if err != nil {
		return SSHCertificate{}, err
	}
	return pgSshCertificate(c)
}

func (q sqliteSSHStoreQueries) sshListCertificates(ctx context.Context, chain domain.Scope, limit int) ([]SSHCertificate, error) {
	rows, err := q.queries.SSHListCertificates(ctx, sqlitegen.SSHListCertificatesParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.SSHListCertificatesRow) (SSHCertificate, error) {
		return sqliteSshCertificate(sqlitegen.SSHGetCertificateRow(c))
	})
}

func (q pgSSHStoreQueries) sshListCertificates(ctx context.Context, chain domain.Scope, limit int) ([]SSHCertificate, error) {
	rows, err := q.queries.SSHListCertificates(ctx, pggen.SSHListCertificatesParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.SSHListCertificatesRow) (SSHCertificate, error) {
		return pgSshCertificate(pggen.SSHGetCertificateRow(c))
	})
}

func (q sqliteSSHStoreQueries) sshProfileRevocationCandidates(ctx context.Context, chain domain.Scope, profileID string, at time.Time) ([]SSHRevokedCertificate, error) {
	rows, err := q.queries.SSHProfileRevocationCandidates(ctx, sqlitegen.SSHProfileRevocationCandidatesParams{ProfileID: profileID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), At: fixedStamp(at)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.SSHProfileRevocationCandidatesRow) (SSHRevokedCertificate, error) {
		out := SSHRevokedCertificate{ID: c.ID, Serial: c.Serial, RequesterPrincipalID: c.RequesterPrincipalID}
		return out, nil
	})
}

func (q pgSSHStoreQueries) sshProfileRevocationCandidates(ctx context.Context, chain domain.Scope, profileID string, at time.Time) ([]SSHRevokedCertificate, error) {
	rows, err := q.queries.SSHProfileRevocationCandidates(ctx, pggen.SSHProfileRevocationCandidatesParams{ProfileID: profileID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), At: pgRequiredTime(at)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.SSHProfileRevocationCandidatesRow) (SSHRevokedCertificate, error) {
		out := SSHRevokedCertificate{ID: c.ID, Serial: c.Serial, RequesterPrincipalID: c.RequesterPrincipalID}
		return out, nil
	})
}

func (q sqliteSSHStoreQueries) sshRevokedSerials(ctx context.Context, chain domain.Scope, caID string, now time.Time, limit int) ([]SSHRevokedSerial, error) {
	rows, err := q.queries.SSHRevokedSerials(ctx, sqlitegen.SSHRevokedSerialsParams{CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), Now: fixedStamp(now), PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.SSHRevokedSerialsRow) (SSHRevokedSerial, error) {
		out := SSHRevokedSerial{CAKeyPublicKey: c.PublicKey, Serial: c.Serial}
		return out, nil
	})
}

func (q pgSSHStoreQueries) sshRevokedSerials(ctx context.Context, chain domain.Scope, caID string, now time.Time, limit int) ([]SSHRevokedSerial, error) {
	rows, err := q.queries.SSHRevokedSerials(ctx, pggen.SSHRevokedSerialsParams{CaID: caID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), Now: pgRequiredTime(now), PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.SSHRevokedSerialsRow) (SSHRevokedSerial, error) {
		out := SSHRevokedSerial{CAKeyPublicKey: c.PublicKey, Serial: c.Serial}
		return out, nil
	})
}

func (q sqliteSSHStoreQueries) sshListKeysForReencrypt(ctx context.Context, chain domain.Scope, cursor string, limit int) ([]ReencryptFieldRow, error) {
	rows, err := q.queries.SSHListKeysForReencrypt(ctx, sqlitegen.SSHListKeysForReencryptParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Cursor: cursor, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c sqlitegen.SSHListKeysForReencryptRow) (ReencryptFieldRow, error) {
		out := ReencryptFieldRow{ID: c.ID, Owner: c.ID, Ciphertext: c.PrivateKeyCiphertext}
		return out, nil
	})
}

func (q pgSSHStoreQueries) sshListKeysForReencrypt(ctx context.Context, chain domain.Scope, cursor string, limit int) ([]ReencryptFieldRow, error) {
	rows, err := q.queries.SSHListKeysForReencrypt(ctx, pggen.SSHListKeysForReencryptParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Cursor: cursor, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(c pggen.SSHListKeysForReencryptRow) (ReencryptFieldRow, error) {
		out := ReencryptFieldRow{ID: c.ID, Owner: c.ID, Ciphertext: c.PrivateKeyCiphertext}
		return out, nil
	})
}

func sqliteSshCA(c sqlitegen.SSHGetCARow) (SSHCA, error) {
	out := SSHCA{ID: c.ID, Name: c.Name, State: c.State, AuthorityPrincipalID: c.AuthorityPrincipalID, CreatedAt: c.CreatedAt}
	if err := normalizeStoredTimes(&out.CreatedAt); err != nil {
		return SSHCA{}, err
	}
	return out, nil
}

func pgSshCA(c pggen.SSHGetCARow) (SSHCA, error) {
	out := SSHCA{ID: c.ID, Name: c.Name, State: c.State, AuthorityPrincipalID: c.AuthorityPrincipalID, CreatedAt: pgStoredStamp(c.CreatedAt)}
	if err := normalizeStoredTimes(&out.CreatedAt); err != nil {
		return SSHCA{}, err
	}
	return out, nil
}

func sqliteSshProfile(c sqlitegen.SSHGetProfileRow) (SSHProfile, error) {
	out := SSHProfile{ID: c.ID, CAID: c.CaID, Name: c.Name, ForceCommand: c.ForceCommand, DefaultTTLSeconds: c.DefaultTtlSeconds, MaxTTLSeconds: c.MaxTtlSeconds, State: c.State, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
	if err := normalizeStoredTimes(&out.CreatedAt, &out.UpdatedAt); err != nil {
		return SSHProfile{}, err
	}
	if err := decodeSSHProfileLists(&out, c.Principals, c.SourceAddresses, c.Extensions, c.KeyAlgorithms); err != nil {
		return SSHProfile{}, err
	}
	return out, nil
}

func pgSshProfile(c pggen.SSHGetProfileRow) (SSHProfile, error) {
	out := SSHProfile{ID: c.ID, CAID: c.CaID, Name: c.Name, ForceCommand: c.ForceCommand, DefaultTTLSeconds: c.DefaultTtlSeconds, MaxTTLSeconds: c.MaxTtlSeconds, State: c.State, CreatedAt: pgStoredStamp(c.CreatedAt), UpdatedAt: pgStoredStamp(c.UpdatedAt)}
	if err := normalizeStoredTimes(&out.CreatedAt, &out.UpdatedAt); err != nil {
		return SSHProfile{}, err
	}
	if err := decodeSSHProfileLists(&out, c.Principals, c.SourceAddresses, c.Extensions, c.KeyAlgorithms); err != nil {
		return SSHProfile{}, err
	}
	return out, nil
}

func sqliteSshCertificate(c sqlitegen.SSHGetCertificateRow) (SSHCertificate, error) {
	var err error
	out := SSHCertificate{ID: c.ID, CAID: c.CaID, CAKeyID: c.CaKeyID, ProfileID: c.ProfileID, Serial: c.Serial, KeyID: c.KeyID, PublicKeyFingerprint: c.PublicKeyFingerprint, KeyAlgorithm: c.KeyAlgorithm, KeyOrigin: c.KeyOrigin, ValidAfter: c.ValidAfter, ValidBefore: c.ValidBefore, RequesterPrincipalID: c.RequesterPrincipalID, RequesterClass: c.RequesterClass, State: c.State, RevokedAt: c.RevokedAt.String, RevocationReason: c.RevocationReason.String, CreatedAt: c.CreatedAt, CAKeyState: c.CaKeyState, CAKeyRetireAfter: c.CaKeyRetireAfter.String, CAState: c.CaState}
	if err := normalizeStoredTimes(&out.ValidAfter, &out.ValidBefore, &out.RevokedAt, &out.CreatedAt, &out.CAKeyRetireAfter); err != nil {
		return SSHCertificate{}, err
	}
	out.Principals, err = decodeSSHList(c.Principals)
	if err != nil {
		return SSHCertificate{}, err
	}
	return out, nil
}

func pgSshCertificate(c pggen.SSHGetCertificateRow) (SSHCertificate, error) {
	var err error
	out := SSHCertificate{ID: c.ID, CAID: c.CaID, CAKeyID: c.CaKeyID, ProfileID: c.ProfileID, Serial: c.Serial, KeyID: c.KeyID, PublicKeyFingerprint: c.PublicKeyFingerprint, KeyAlgorithm: c.KeyAlgorithm, KeyOrigin: c.KeyOrigin, ValidAfter: pgStoredStamp(c.ValidAfter), ValidBefore: pgStoredStamp(c.ValidBefore), RequesterPrincipalID: c.RequesterPrincipalID, RequesterClass: c.RequesterClass, State: c.State, RevokedAt: pgStoredStamp(c.RevokedAt), RevocationReason: c.RevocationReason.String, CreatedAt: pgStoredStamp(c.CreatedAt), CAKeyState: c.CaKeyState, CAKeyRetireAfter: pgStoredStamp(c.CaKeyRetireAfter), CAState: c.CaState}
	if err := normalizeStoredTimes(&out.ValidAfter, &out.ValidBefore, &out.RevokedAt, &out.CreatedAt, &out.CAKeyRetireAfter); err != nil {
		return SSHCertificate{}, err
	}
	out.Principals, err = decodeSSHList(c.Principals)
	if err != nil {
		return SSHCertificate{}, err
	}
	return out, nil
}
