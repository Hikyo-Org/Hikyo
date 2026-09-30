package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
)

type dynamicStoreQueries interface {
	dynamicCreateProvider(ctx context.Context, chain domain.Scope, m DynamicProviderCreate) (int64, error)
	dynamicGetProvider(ctx context.Context, chain domain.Scope, providerID string) (DynamicProviderRecord, error)
	dynamicProviderCredentialCiphertext(ctx context.Context, chain domain.Scope, providerID string) ([]byte, error)
	dynamicListProviders(ctx context.Context, chain domain.Scope) ([]DynamicProviderRecord, error)
	dynamicReplaceProviderCredential(ctx context.Context, chain domain.Scope, m DynamicProviderCredentialMutation) (int64, error)
	dynamicRevokeProviderCredential(ctx context.Context, chain domain.Scope, providerID string) (int64, error)
	dynamicDeleteProvider(ctx context.Context, chain domain.Scope, providerID string) (int64, error)
	dynamicCreateLease(ctx context.Context, chain domain.Scope, m DynamicLeaseCreate) (int64, error)
	dynamicGetLease(ctx context.Context, chain domain.Scope, leaseID string) (DynamicLease, error)
	dynamicListLeasesForEnvironment(ctx context.Context, chain domain.Scope) ([]DynamicLease, error)
	dynamicActiveLeaseIDsForProvider(ctx context.Context, chain domain.Scope, providerID string) ([]DynamicLease, error)
	dynamicFinishMint(ctx context.Context, chain domain.Scope, m DynamicLeaseFinishMint) (int64, error)
	dynamicReencryptProvider(ctx context.Context, chain domain.Scope, id string, newCiphertext, oldCiphertext []byte) (int64, error)
	dynamicListProvidersForReencrypt(ctx context.Context, chain domain.Scope, cursor string, limit int) ([]ReencryptFieldRow, error)
	dynamicEnqueueTransition(ctx context.Context, chain domain.Scope, m DynamicLeaseTransition) (int64, error)
}
type sqliteDynamicStoreQueries struct{ queries *sqlitegen.Queries }
type pgDynamicStoreQueries struct{ queries *pggen.Queries }

func (q sqliteDynamicStoreQueries) dynamicCreateProvider(ctx context.Context, chain domain.Scope, m DynamicProviderCreate) (int64, error) {
	n, err := q.queries.DynamicCreateProvider(ctx, sqlitegen.DynamicCreateProviderParams{ID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Kind: m.Kind, Origin: m.Origin, TlsMode: m.TLSMode, GrantRole: m.GrantRole, Credential: m.CredentialCiphertext, At: runtimeSQLiteStamp(m.At), AuthorityPrincipalID: m.AuthorityPrincipalID})
	return n, constraint(err)
}

func (q pgDynamicStoreQueries) dynamicCreateProvider(ctx context.Context, chain domain.Scope, m DynamicProviderCreate) (int64, error) {
	n, err := q.queries.DynamicCreateProvider(ctx, pggen.DynamicCreateProviderParams{ID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Kind: m.Kind, Origin: m.Origin, TlsMode: m.TLSMode, GrantRole: m.GrantRole, Credential: m.CredentialCiphertext, At: pgRequiredTime(m.At), AuthorityPrincipalID: m.AuthorityPrincipalID})
	return n, constraint(err)
}

func (q sqliteDynamicStoreQueries) dynamicGetProvider(ctx context.Context, chain domain.Scope, providerID string) (DynamicProviderRecord, error) {
	c, err := q.queries.DynamicGetProvider(ctx, sqlitegen.DynamicGetProviderParams{ProviderID: providerID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if isNoRows(err) {
		return DynamicProviderRecord{}, ErrNotFound
	}
	if err != nil {
		return DynamicProviderRecord{}, err
	}
	out := DynamicProviderRecord{ID: c.ID, Kind: c.Kind, Origin: c.Origin, TLSMode: c.TlsMode, GrantRole: c.GrantRole, CredentialPresent: c.CredentialPresent == 1, CredentialSetAt: c.CredentialSetAt.String, AuthorityPrincipalID: c.AuthorityPrincipalID, State: c.State, CreatedAt: c.CreatedAt}
	if err := normalizeStoredTimes(&out.CredentialSetAt, &out.CreatedAt); err != nil {
		return DynamicProviderRecord{}, err
	}
	return out, nil
}

func (q pgDynamicStoreQueries) dynamicGetProvider(ctx context.Context, chain domain.Scope, providerID string) (DynamicProviderRecord, error) {
	c, err := q.queries.DynamicGetProvider(ctx, pggen.DynamicGetProviderParams{ProviderID: providerID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if isNoRows(err) {
		return DynamicProviderRecord{}, ErrNotFound
	}
	if err != nil {
		return DynamicProviderRecord{}, err
	}
	out := DynamicProviderRecord{ID: c.ID, Kind: c.Kind, Origin: c.Origin, TLSMode: c.TlsMode, GrantRole: c.GrantRole, CredentialPresent: c.CredentialPresent == 1, CredentialSetAt: pgStoredStamp(c.CredentialSetAt), AuthorityPrincipalID: c.AuthorityPrincipalID, State: c.State, CreatedAt: pgStoredStamp(c.CreatedAt)}
	if err := normalizeStoredTimes(&out.CredentialSetAt, &out.CreatedAt); err != nil {
		return DynamicProviderRecord{}, err
	}
	return out, nil
}

func (q sqliteDynamicStoreQueries) dynamicProviderCredentialCiphertext(ctx context.Context, chain domain.Scope, providerID string) ([]byte, error) {
	value, err := q.queries.DynamicProviderCredentialCiphertext(ctx, sqlitegen.DynamicProviderCredentialCiphertextParams{ProviderID: providerID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return value, err
}

func (q pgDynamicStoreQueries) dynamicProviderCredentialCiphertext(ctx context.Context, chain domain.Scope, providerID string) ([]byte, error) {
	value, err := q.queries.DynamicProviderCredentialCiphertext(ctx, pggen.DynamicProviderCredentialCiphertextParams{ProviderID: providerID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return value, err
}

func (q sqliteDynamicStoreQueries) dynamicListProviders(ctx context.Context, chain domain.Scope) ([]DynamicProviderRecord, error) {
	rows, err := q.queries.DynamicListProviders(ctx, sqlitegen.DynamicListProvidersParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	var result []DynamicProviderRecord
	for _, c := range rows {
		out := DynamicProviderRecord{ID: c.ID, Kind: c.Kind, Origin: c.Origin, TLSMode: c.TlsMode, GrantRole: c.GrantRole, CredentialPresent: c.CredentialPresent == 1, CredentialSetAt: c.CredentialSetAt.String, AuthorityPrincipalID: c.AuthorityPrincipalID, State: c.State, CreatedAt: c.CreatedAt}
		if err := normalizeStoredTimes(&out.CredentialSetAt, &out.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, out)
	}
	return result, nil
}

func (q pgDynamicStoreQueries) dynamicListProviders(ctx context.Context, chain domain.Scope) ([]DynamicProviderRecord, error) {
	rows, err := q.queries.DynamicListProviders(ctx, pggen.DynamicListProvidersParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	var result []DynamicProviderRecord
	for _, c := range rows {
		out := DynamicProviderRecord{ID: c.ID, Kind: c.Kind, Origin: c.Origin, TLSMode: c.TlsMode, GrantRole: c.GrantRole, CredentialPresent: c.CredentialPresent == 1, CredentialSetAt: pgStoredStamp(c.CredentialSetAt), AuthorityPrincipalID: c.AuthorityPrincipalID, State: c.State, CreatedAt: pgStoredStamp(c.CreatedAt)}
		if err := normalizeStoredTimes(&out.CredentialSetAt, &out.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, out)
	}
	return result, nil
}

func (q sqliteDynamicStoreQueries) dynamicReplaceProviderCredential(ctx context.Context, chain domain.Scope, m DynamicProviderCredentialMutation) (int64, error) {
	n, err := q.queries.DynamicReplaceProviderCredential(ctx, sqlitegen.DynamicReplaceProviderCredentialParams{Credential: m.CredentialCiphertext, At: runtimeSQLiteStamp(m.At), ProviderID: m.ProviderID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	return n, constraint(err)
}

func (q pgDynamicStoreQueries) dynamicReplaceProviderCredential(ctx context.Context, chain domain.Scope, m DynamicProviderCredentialMutation) (int64, error) {
	n, err := q.queries.DynamicReplaceProviderCredential(ctx, pggen.DynamicReplaceProviderCredentialParams{Credential: m.CredentialCiphertext, At: pgRequiredTime(m.At), ProviderID: m.ProviderID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	return n, constraint(err)
}

func (q sqliteDynamicStoreQueries) dynamicRevokeProviderCredential(ctx context.Context, chain domain.Scope, providerID string) (int64, error) {
	n, err := q.queries.DynamicRevokeProviderCredential(ctx, sqlitegen.DynamicRevokeProviderCredentialParams{ProviderID: providerID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	return n, constraint(err)
}

func (q pgDynamicStoreQueries) dynamicRevokeProviderCredential(ctx context.Context, chain domain.Scope, providerID string) (int64, error) {
	n, err := q.queries.DynamicRevokeProviderCredential(ctx, pggen.DynamicRevokeProviderCredentialParams{ProviderID: providerID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	return n, constraint(err)
}

func (q sqliteDynamicStoreQueries) dynamicDeleteProvider(ctx context.Context, chain domain.Scope, providerID string) (int64, error) {
	n, err := q.queries.DynamicDeleteProvider(ctx, sqlitegen.DynamicDeleteProviderParams{ProviderID: providerID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	return n, constraint(err)
}

func (q pgDynamicStoreQueries) dynamicDeleteProvider(ctx context.Context, chain domain.Scope, providerID string) (int64, error) {
	n, err := q.queries.DynamicDeleteProvider(ctx, pggen.DynamicDeleteProviderParams{ProviderID: providerID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	return n, constraint(err)
}

func (q sqliteDynamicStoreQueries) dynamicCreateLease(ctx context.Context, chain domain.Scope, m DynamicLeaseCreate) (int64, error) {
	grace := m.At.Add(mintRecoveryGrace)
	n, err := q.queries.DynamicCreateLease(ctx, sqlitegen.DynamicCreateLeaseParams{ID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), ProviderID: m.ProviderID, PrincipalID: m.PrincipalID, PrincipalClass: m.PrincipalClass, ProviderHandle: m.ProviderHandle, MaxTtlSeconds: m.MaxTTLSeconds, At: fixedStamp(m.At), Grace: fixedStamp(grace)})
	return n, constraint(err)
}

func (q pgDynamicStoreQueries) dynamicCreateLease(ctx context.Context, chain domain.Scope, m DynamicLeaseCreate) (int64, error) {
	grace := m.At.Add(mintRecoveryGrace)
	n, err := q.queries.DynamicCreateLease(ctx, pggen.DynamicCreateLeaseParams{ID: m.ID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env), ProviderID: m.ProviderID, PrincipalID: m.PrincipalID, PrincipalClass: m.PrincipalClass, ProviderHandle: m.ProviderHandle, MaxTtlSeconds: m.MaxTTLSeconds, At: pgRequiredTime(m.At), Grace: pgRequiredTime(grace)})
	return n, constraint(err)
}

func (q sqliteDynamicStoreQueries) dynamicGetLease(ctx context.Context, chain domain.Scope, leaseID string) (DynamicLease, error) {
	c, err := q.queries.DynamicGetLease(ctx, sqlitegen.DynamicGetLeaseParams{LeaseID: leaseID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return DynamicLease{}, ErrNotFound
	}
	if err != nil {
		return DynamicLease{}, err
	}
	out := DynamicLease{ID: c.ID, ProviderID: c.ProviderID, EnvironmentID: c.EnvironmentID, PrincipalID: c.PrincipalID, PrincipalClass: c.PrincipalClass, ProviderHandle: c.ProviderHandle, State: c.State, IssuedAt: c.IssuedAt.String, ExpiresAt: c.ExpiresAt.String, MaxTTLSeconds: c.MaxTtlSeconds, LastTransitionAt: c.LastTransitionAt, CreatedAt: c.CreatedAt}
	if err := normalizeStoredTimes(&out.IssuedAt, &out.ExpiresAt, &out.LastTransitionAt, &out.CreatedAt); err != nil {
		return DynamicLease{}, err
	}
	return out, nil
}

func (q pgDynamicStoreQueries) dynamicGetLease(ctx context.Context, chain domain.Scope, leaseID string) (DynamicLease, error) {
	c, err := q.queries.DynamicGetLease(ctx, pggen.DynamicGetLeaseParams{LeaseID: leaseID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if isNoRows(err) {
		return DynamicLease{}, ErrNotFound
	}
	if err != nil {
		return DynamicLease{}, err
	}
	out := DynamicLease{ID: c.ID, ProviderID: c.ProviderID, EnvironmentID: c.EnvironmentID, PrincipalID: c.PrincipalID, PrincipalClass: c.PrincipalClass, ProviderHandle: c.ProviderHandle, State: c.State, IssuedAt: pgStoredStamp(c.IssuedAt), ExpiresAt: pgStoredStamp(c.ExpiresAt), MaxTTLSeconds: c.MaxTtlSeconds, LastTransitionAt: pgStoredStamp(c.LastTransitionAt), CreatedAt: pgStoredStamp(c.CreatedAt)}
	if err := normalizeStoredTimes(&out.IssuedAt, &out.ExpiresAt, &out.LastTransitionAt, &out.CreatedAt); err != nil {
		return DynamicLease{}, err
	}
	return out, nil
}

func (q sqliteDynamicStoreQueries) dynamicListLeasesForEnvironment(ctx context.Context, chain domain.Scope) ([]DynamicLease, error) {
	rows, err := q.queries.DynamicListLeasesForEnvironment(ctx, sqlitegen.DynamicListLeasesForEnvironmentParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	var result []DynamicLease
	for _, c := range rows {
		out := DynamicLease{ID: c.ID, ProviderID: c.ProviderID, EnvironmentID: c.EnvironmentID, PrincipalID: c.PrincipalID, PrincipalClass: c.PrincipalClass, ProviderHandle: c.ProviderHandle, State: c.State, IssuedAt: c.IssuedAt.String, ExpiresAt: c.ExpiresAt.String, MaxTTLSeconds: c.MaxTtlSeconds, LastTransitionAt: c.LastTransitionAt, CreatedAt: c.CreatedAt}
		if err := normalizeStoredTimes(&out.IssuedAt, &out.ExpiresAt, &out.LastTransitionAt, &out.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, out)
	}
	return result, nil
}

func (q pgDynamicStoreQueries) dynamicListLeasesForEnvironment(ctx context.Context, chain domain.Scope) ([]DynamicLease, error) {
	rows, err := q.queries.DynamicListLeasesForEnvironment(ctx, pggen.DynamicListLeasesForEnvironmentParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	if err != nil {
		return nil, err
	}
	var result []DynamicLease
	for _, c := range rows {
		out := DynamicLease{ID: c.ID, ProviderID: c.ProviderID, EnvironmentID: c.EnvironmentID, PrincipalID: c.PrincipalID, PrincipalClass: c.PrincipalClass, ProviderHandle: c.ProviderHandle, State: c.State, IssuedAt: pgStoredStamp(c.IssuedAt), ExpiresAt: pgStoredStamp(c.ExpiresAt), MaxTTLSeconds: c.MaxTtlSeconds, LastTransitionAt: pgStoredStamp(c.LastTransitionAt), CreatedAt: pgStoredStamp(c.CreatedAt)}
		if err := normalizeStoredTimes(&out.IssuedAt, &out.ExpiresAt, &out.LastTransitionAt, &out.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, out)
	}
	return result, nil
}

func (q sqliteDynamicStoreQueries) dynamicActiveLeaseIDsForProvider(ctx context.Context, chain domain.Scope, providerID string) ([]DynamicLease, error) {
	rows, err := q.queries.DynamicActiveLeaseIDsForProvider(ctx, sqlitegen.DynamicActiveLeaseIDsForProviderParams{ProviderID: providerID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	var result []DynamicLease
	for _, c := range rows {
		out := DynamicLease{ID: c.ID, ProviderID: c.ProviderID, EnvironmentID: c.EnvironmentID, PrincipalID: c.PrincipalID, PrincipalClass: c.PrincipalClass, ProviderHandle: c.ProviderHandle, State: c.State, IssuedAt: c.IssuedAt.String, ExpiresAt: c.ExpiresAt.String, MaxTTLSeconds: c.MaxTtlSeconds, LastTransitionAt: c.LastTransitionAt, CreatedAt: c.CreatedAt}
		if err := normalizeStoredTimes(&out.IssuedAt, &out.ExpiresAt, &out.LastTransitionAt, &out.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, out)
	}
	return result, nil
}

func (q pgDynamicStoreQueries) dynamicActiveLeaseIDsForProvider(ctx context.Context, chain domain.Scope, providerID string) ([]DynamicLease, error) {
	rows, err := q.queries.DynamicActiveLeaseIDsForProvider(ctx, pggen.DynamicActiveLeaseIDsForProviderParams{ProviderID: providerID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	var result []DynamicLease
	for _, c := range rows {
		out := DynamicLease{ID: c.ID, ProviderID: c.ProviderID, EnvironmentID: c.EnvironmentID, PrincipalID: c.PrincipalID, PrincipalClass: c.PrincipalClass, ProviderHandle: c.ProviderHandle, State: c.State, IssuedAt: pgStoredStamp(c.IssuedAt), ExpiresAt: pgStoredStamp(c.ExpiresAt), MaxTTLSeconds: c.MaxTtlSeconds, LastTransitionAt: pgStoredStamp(c.LastTransitionAt), CreatedAt: pgStoredStamp(c.CreatedAt)}
		if err := normalizeStoredTimes(&out.IssuedAt, &out.ExpiresAt, &out.LastTransitionAt, &out.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, out)
	}
	return result, nil
}

func (q sqliteDynamicStoreQueries) dynamicFinishMint(ctx context.Context, chain domain.Scope, m DynamicLeaseFinishMint) (int64, error) {
	n, err := q.queries.DynamicFinishMint(ctx, sqlitegen.DynamicFinishMintParams{State: m.State, IssuedAt: sql.NullString{String: fixedStamp(m.IssuedAt), Valid: m.State == "active"}, ExpiresAt: sql.NullString{String: fixedStamp(m.ExpiresAt), Valid: m.State == "active"}, At: fixedStamp(m.At), NextAttemptAt: fixedStamp(m.NextAttemptAt), LeaseID: m.LeaseID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return n, constraint(err)
}

func (q pgDynamicStoreQueries) dynamicFinishMint(ctx context.Context, chain domain.Scope, m DynamicLeaseFinishMint) (int64, error) {
	n, err := q.queries.DynamicFinishMint(ctx, pggen.DynamicFinishMintParams{State: m.State, IssuedAt: pgtype.Timestamptz{Time: CanonTime(m.IssuedAt), Valid: m.State == "active"}, ExpiresAt: pgtype.Timestamptz{Time: CanonTime(m.ExpiresAt), Valid: m.State == "active"}, At: pgRequiredTime(m.At), NextAttemptAt: pgRequiredTime(m.NextAttemptAt), LeaseID: m.LeaseID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
	return n, constraint(err)
}

func (q sqliteDynamicStoreQueries) dynamicReencryptProvider(ctx context.Context, chain domain.Scope, id string, newCiphertext, oldCiphertext []byte) (int64, error) {
	n, err := q.queries.DynamicReencryptProvider(ctx, sqlitegen.DynamicReencryptProviderParams{NewCiphertext: newCiphertext, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ID: id, OldCiphertext: oldCiphertext})
	return n, constraint(err)
}

func (q pgDynamicStoreQueries) dynamicReencryptProvider(ctx context.Context, chain domain.Scope, id string, newCiphertext, oldCiphertext []byte) (int64, error) {
	n, err := q.queries.DynamicReencryptProvider(ctx, pggen.DynamicReencryptProviderParams{NewCiphertext: newCiphertext, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ID: id, OldCiphertext: oldCiphertext})
	return n, constraint(err)
}

func (q sqliteDynamicStoreQueries) dynamicListProvidersForReencrypt(ctx context.Context, chain domain.Scope, cursor string, limit int) ([]ReencryptFieldRow, error) {
	rows, err := q.queries.DynamicListProvidersForReencrypt(ctx, sqlitegen.DynamicListProvidersForReencryptParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Cursor: cursor, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	var result []ReencryptFieldRow
	for _, c := range rows {
		out := ReencryptFieldRow{ID: c.ID, Owner: c.ID, Ciphertext: c.AdminCredentialCiphertext}
		result = append(result, out)
	}
	return result, nil
}

func (q pgDynamicStoreQueries) dynamicListProvidersForReencrypt(ctx context.Context, chain domain.Scope, cursor string, limit int) ([]ReencryptFieldRow, error) {
	rows, err := q.queries.DynamicListProvidersForReencrypt(ctx, pggen.DynamicListProvidersForReencryptParams{ChainOrg: string(chain.Org), ChainProject: string(chain.Project), Cursor: cursor, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	var result []ReencryptFieldRow
	for _, c := range rows {
		out := ReencryptFieldRow{ID: c.ID, Owner: c.ID, Ciphertext: c.AdminCredentialCiphertext}
		result = append(result, out)
	}
	return result, nil
}

func (q sqliteDynamicStoreQueries) dynamicEnqueueTransition(ctx context.Context, chain domain.Scope, m DynamicLeaseTransition) (int64, error) {
	var n int64
	var err error
	switch m.State {
	case "revoking":
		if m.MaxTTLSeconds > 0 {
			n, err = q.queries.DynamicEnqueueRevokingWithTTL(ctx, sqlitegen.DynamicEnqueueRevokingWithTTLParams{State: m.State, MaxTtlSeconds: m.MaxTTLSeconds, At: fixedStamp(m.At), NextAttemptAt: fixedStamp(m.NextAttemptAt), LeaseID: m.LeaseID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
		} else {
			n, err = q.queries.DynamicEnqueueRevoking(ctx, sqlitegen.DynamicEnqueueRevokingParams{State: m.State, At: fixedStamp(m.At), NextAttemptAt: fixedStamp(m.NextAttemptAt), LeaseID: m.LeaseID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
		}
	case "renewing":
		if m.MaxTTLSeconds > 0 {
			n, err = q.queries.DynamicEnqueueRenewingWithTTL(ctx, sqlitegen.DynamicEnqueueRenewingWithTTLParams{State: m.State, MaxTtlSeconds: m.MaxTTLSeconds, At: fixedStamp(m.At), NextAttemptAt: fixedStamp(m.NextAttemptAt), LeaseID: m.LeaseID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
		} else {
			n, err = q.queries.DynamicEnqueueRenewing(ctx, sqlitegen.DynamicEnqueueRenewingParams{State: m.State, At: fixedStamp(m.At), NextAttemptAt: fixedStamp(m.NextAttemptAt), LeaseID: m.LeaseID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
		}
	case "unknown":
		if m.MaxTTLSeconds > 0 {
			n, err = q.queries.DynamicEnqueueUnknownWithTTL(ctx, sqlitegen.DynamicEnqueueUnknownWithTTLParams{State: m.State, MaxTtlSeconds: m.MaxTTLSeconds, At: fixedStamp(m.At), NextAttemptAt: fixedStamp(m.NextAttemptAt), LeaseID: m.LeaseID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
		} else {
			n, err = q.queries.DynamicEnqueueUnknown(ctx, sqlitegen.DynamicEnqueueUnknownParams{State: m.State, At: fixedStamp(m.At), NextAttemptAt: fixedStamp(m.NextAttemptAt), LeaseID: m.LeaseID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
		}
	default:
		return 0, errors.New("store: unknown lease transition target state")
	}
	return n, constraint(err)
}

func (q pgDynamicStoreQueries) dynamicEnqueueTransition(ctx context.Context, chain domain.Scope, m DynamicLeaseTransition) (int64, error) {
	var n int64
	var err error
	switch m.State {
	case "revoking":
		if m.MaxTTLSeconds > 0 {
			n, err = q.queries.DynamicEnqueueRevokingWithTTL(ctx, pggen.DynamicEnqueueRevokingWithTTLParams{State: m.State, MaxTtlSeconds: m.MaxTTLSeconds, At: pgRequiredTime(m.At), NextAttemptAt: pgRequiredTime(m.NextAttemptAt), LeaseID: m.LeaseID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
		} else {
			n, err = q.queries.DynamicEnqueueRevoking(ctx, pggen.DynamicEnqueueRevokingParams{State: m.State, At: pgRequiredTime(m.At), NextAttemptAt: pgRequiredTime(m.NextAttemptAt), LeaseID: m.LeaseID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
		}
	case "renewing":
		if m.MaxTTLSeconds > 0 {
			n, err = q.queries.DynamicEnqueueRenewingWithTTL(ctx, pggen.DynamicEnqueueRenewingWithTTLParams{State: m.State, MaxTtlSeconds: m.MaxTTLSeconds, At: pgRequiredTime(m.At), NextAttemptAt: pgRequiredTime(m.NextAttemptAt), LeaseID: m.LeaseID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
		} else {
			n, err = q.queries.DynamicEnqueueRenewing(ctx, pggen.DynamicEnqueueRenewingParams{State: m.State, At: pgRequiredTime(m.At), NextAttemptAt: pgRequiredTime(m.NextAttemptAt), LeaseID: m.LeaseID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
		}
	case "unknown":
		if m.MaxTTLSeconds > 0 {
			n, err = q.queries.DynamicEnqueueUnknownWithTTL(ctx, pggen.DynamicEnqueueUnknownWithTTLParams{State: m.State, MaxTtlSeconds: m.MaxTTLSeconds, At: pgRequiredTime(m.At), NextAttemptAt: pgRequiredTime(m.NextAttemptAt), LeaseID: m.LeaseID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
		} else {
			n, err = q.queries.DynamicEnqueueUnknown(ctx, pggen.DynamicEnqueueUnknownParams{State: m.State, At: pgRequiredTime(m.At), NextAttemptAt: pgRequiredTime(m.NextAttemptAt), LeaseID: m.LeaseID, ChainOrg: string(chain.Org), ChainProject: string(chain.Project), ChainEnv: string(chain.Env)})
		}
	default:
		return 0, errors.New("store: unknown lease transition target state")
	}
	return n, constraint(err)
}
