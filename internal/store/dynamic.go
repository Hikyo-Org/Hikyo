package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Hikyo-Org/hikyo/internal/authz"
)

// Dynamic-secret request-path store (#147). These methods are proof-carrying:
// every one verifies its proof against its registered store operation and binds
// the tenant chain from the verified proof, exactly like the adapter config
// surface. The worker's lease-transition SQL is the proof-free DynamicRuntime
// below (the domain-specific outbox, like AdapterRuntime).

// DynamicProviderRecord is a provider's metadata surface. There is no
// credential value field: the admin credential is write-only, and its presence
// is all a read exposes.
type DynamicProviderRecord struct {
	ID                   string
	Kind                 string
	Origin               string
	TLSMode              string
	GrantRole            string
	CredentialPresent    bool
	CredentialSetAt      string
	AuthorityPrincipalID string
	State                string
	CreatedAt            string
}

// DynamicProviderCreate is one provider configuration. Credential is already
// sealed by the service; the store never sees plaintext.
type DynamicProviderCreate struct {
	ID                   string
	Kind                 string
	Origin               string
	TLSMode              string
	GrantRole            string
	CredentialCiphertext []byte
	AuthorityPrincipalID string
	At                   time.Time
}

// DynamicProviderCredentialMutation replaces a provider's sealed admin
// credential.
type DynamicProviderCredentialMutation struct {
	ProviderID           string
	CredentialCiphertext []byte
	At                   time.Time
}

// DynamicLease is a lease's metadata surface. There is no secret field, ever:
// the minted password is delivered once at mint and never stored.
type DynamicLease struct {
	ID               string
	ProviderID       string
	EnvironmentID    string
	PrincipalID      string
	PrincipalClass   string
	ProviderHandle   string
	State            string
	IssuedAt         string
	ExpiresAt        string
	MaxTTLSeconds    int64
	LastTransitionAt string
	CreatedAt        string
}

// DynamicLeaseCreate is a mint's durable intent row (state=minting). The lease
// row is itself the job: next_attempt_at is stamped now so a crashed synchronous
// mint is picked up by the worker and settled.
type DynamicLeaseCreate struct {
	ID             string
	ProviderID     string
	PrincipalID    string
	PrincipalClass string
	ProviderHandle string
	MaxTTLSeconds  int64
	At             time.Time
}

// DynamicLeaseFinishMint settles a synchronous mint: active on success, failed
// on a definite provider failure, unknown on an ambiguous outcome.
type DynamicLeaseFinishMint struct {
	LeaseID       string
	State         string
	IssuedAt      time.Time
	ExpiresAt     time.Time
	NextAttemptAt time.Time
	At            time.Time
}

// DynamicLeaseTransition enqueues renew/revoke/reconcile: it sets the transient
// state and stamps next_attempt_at now so the worker claims it. MaxTTLSeconds,
// when non-zero, tightens the per-period ceiling on a renew.
type DynamicLeaseTransition struct {
	LeaseID       string
	State         string
	MaxTTLSeconds int64
	NextAttemptAt time.Time
	At            time.Time
}

// DynamicReader is the read side (inspect/list + reencrypt page).
type DynamicReader interface {
	GetProvider(ctx context.Context, p authz.Proof, providerID string) (DynamicProviderRecord, error)
	// ProviderCredentialCiphertext returns the sealed admin credential for the
	// mint path to open. It is the ONLY method that
	// returns the ciphertext; ordinary reads never do.
	ProviderCredentialCiphertext(ctx context.Context, p authz.Proof, providerID string) ([]byte, error)
	ListProviders(ctx context.Context, p authz.Proof) ([]DynamicProviderRecord, error)
	GetLease(ctx context.Context, p authz.Proof, leaseID string) (DynamicLease, error)
	ListLeasesForEnvironment(ctx context.Context, p authz.Proof) ([]DynamicLease, error)
	ActiveLeaseIDsForProvider(ctx context.Context, p authz.Proof, providerID string) ([]DynamicLease, error)
	ListProvidersForReencrypt(ctx context.Context, p authz.Proof, cursor string, limit int) ([]ReencryptFieldRow, error)
}

// DynamicRepo is the write side plus the reads.
type DynamicRepo interface {
	DynamicReader
	CreateProvider(ctx context.Context, p authz.Proof, mutation DynamicProviderCreate) (DynamicProviderRecord, error)
	ReplaceProviderCredential(ctx context.Context, p authz.Proof, mutation DynamicProviderCredentialMutation) error
	RevokeProviderCredential(ctx context.Context, p authz.Proof, providerID string, at time.Time) error
	DeleteProvider(ctx context.Context, p authz.Proof, providerID string, at time.Time) error
	CreateLease(ctx context.Context, p authz.Proof, mutation DynamicLeaseCreate) (DynamicLease, error)
	FinishMint(ctx context.Context, p authz.Proof, mutation DynamicLeaseFinishMint) error
	EnqueueTransition(ctx context.Context, p authz.Proof, mutation DynamicLeaseTransition) (DynamicLease, error)
	ReencryptProvider(ctx context.Context, p authz.Proof, id string, newCiphertext, oldCiphertext []byte) (bool, error)
}

type dynamicQueries struct {
	db  adapterDB
	tok *authz.TxToken
}

func (r sqliteRepos) Dynamic() DynamicRepo {
	return dynamicQueries{db: sqliteAdoptDB{db: r.db}, tok: r.tok}
}
func (r pgRepos) Dynamic() DynamicRepo {
	return dynamicQueries{db: pgAdoptDB{db: r.db}, tok: r.tok}
}

const dynamicProviderColumns = `id,kind,origin,tls_mode,grant_role,CASE WHEN admin_credential_ciphertext IS NULL THEN 0 ELSE 1 END,credential_set_at,authority_principal_id,state,created_at`

func scanDynamicProvider(row interface{ Scan(...any) error }) (DynamicProviderRecord, error) {
	var out DynamicProviderRecord
	var present int
	var credentialSetAt, createdAt adapterStoredTime
	err := row.Scan(&out.ID, &out.Kind, &out.Origin, &out.TLSMode, &out.GrantRole, &present, &credentialSetAt, &out.AuthorityPrincipalID, &out.State, &createdAt)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return DynamicProviderRecord{}, ErrNotFound
	}
	if err != nil {
		return DynamicProviderRecord{}, err
	}
	out.CredentialPresent = present == 1
	out.CredentialSetAt = credentialSetAt.value
	out.CreatedAt = createdAt.value
	return out, nil
}

func (r dynamicQueries) CreateProvider(ctx context.Context, p authz.Proof, m DynamicProviderCreate) (DynamicProviderRecord, error) {
	chain, err := authz.Verify(p, authz.StoreDynamicProvidersCreate, r.tok)
	if err != nil {
		return DynamicProviderRecord{}, err
	}
	stamp := r.db.Stamp(m.At)
	query := r.db.SQL(
		`INSERT INTO dynamic_providers (id,org_id,project_id,kind,origin,tls_mode,grant_role,admin_credential_ciphertext,credential_set_at,authority_principal_id,state,created_at) VALUES (?,?,?,?,?,?,?,?,?,?, 'active', ?)`,
		`INSERT INTO dynamic_providers (id,org_id,project_id,kind,origin,tls_mode,grant_role,admin_credential_ciphertext,credential_set_at,authority_principal_id,state,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10, 'active', $11)`)
	if _, err := r.db.Exec(ctx, query, m.ID, chain.Org, chain.Project, m.Kind, m.Origin, m.TLSMode, m.GrantRole, m.CredentialCiphertext, stamp, m.AuthorityPrincipalID, stamp); err != nil {
		return DynamicProviderRecord{}, err
	}
	return r.GetProvider(ctx, p, m.ID)
}

func (r dynamicQueries) GetProvider(ctx context.Context, p authz.Proof, providerID string) (DynamicProviderRecord, error) {
	chain, err := authz.Verify(p, authz.StoreDynamicProvidersGet, r.tok)
	if err != nil {
		return DynamicProviderRecord{}, err
	}
	query := r.db.SQL(
		`SELECT `+dynamicProviderColumns+` FROM dynamic_providers WHERE id=? AND org_id=? AND project_id=? AND state='active'`,
		`SELECT `+dynamicProviderColumns+` FROM dynamic_providers WHERE id=$1 AND org_id=$2 AND project_id=$3 AND state='active'`)
	return scanDynamicProvider(r.db.QueryRow(ctx, query, providerID, chain.Org, chain.Project))
}

func (r dynamicQueries) ProviderCredentialCiphertext(ctx context.Context, p authz.Proof, providerID string) ([]byte, error) {
	chain, err := authz.Verify(p, authz.StoreDynamicProvidersCredentialCiphertext, r.tok)
	if err != nil {
		return nil, err
	}
	query := r.db.SQL(
		`SELECT admin_credential_ciphertext FROM dynamic_providers WHERE id=? AND org_id=? AND project_id=? AND state='active'`,
		`SELECT admin_credential_ciphertext FROM dynamic_providers WHERE id=$1 AND org_id=$2 AND project_id=$3 AND state='active'`)
	var ct []byte
	if err := r.db.QueryRow(ctx, query, providerID, chain.Org, chain.Project).Scan(&ct); err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(ct) == 0 {
		return nil, ErrNoProviderCredential
	}
	return ct, nil
}

func (r dynamicQueries) ListProviders(ctx context.Context, p authz.Proof) ([]DynamicProviderRecord, error) {
	chain, err := authz.Verify(p, authz.StoreDynamicProvidersList, r.tok)
	if err != nil {
		return nil, err
	}
	query := r.db.SQL(
		`SELECT `+dynamicProviderColumns+` FROM dynamic_providers WHERE org_id=? AND project_id=? AND state='active' ORDER BY id`,
		`SELECT `+dynamicProviderColumns+` FROM dynamic_providers WHERE org_id=$1 AND project_id=$2 AND state='active' ORDER BY id`)
	rows, err := r.db.Query(ctx, query, chain.Org, chain.Project)
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	var out []DynamicProviderRecord
	for rows.Next() {
		record, err := scanDynamicProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (r dynamicQueries) ReplaceProviderCredential(ctx context.Context, p authz.Proof, m DynamicProviderCredentialMutation) error {
	chain, err := authz.Verify(p, authz.StoreDynamicProvidersReplaceCredential, r.tok)
	if err != nil {
		return err
	}
	stamp := r.db.Stamp(m.At)
	query := r.db.SQL(
		`UPDATE dynamic_providers SET admin_credential_ciphertext=?,credential_set_at=? WHERE id=? AND org_id=? AND project_id=? AND state='active'`,
		`UPDATE dynamic_providers SET admin_credential_ciphertext=$1,credential_set_at=$2 WHERE id=$3 AND org_id=$4 AND project_id=$5 AND state='active'`)
	rows, err := r.db.Exec(ctx, query, m.CredentialCiphertext, stamp, m.ProviderID, chain.Org, chain.Project)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}

func (r dynamicQueries) RevokeProviderCredential(ctx context.Context, p authz.Proof, providerID string, at time.Time) error {
	chain, err := authz.Verify(p, authz.StoreDynamicProvidersRevokeCredential, r.tok)
	if err != nil {
		return err
	}
	query := r.db.SQL(
		`UPDATE dynamic_providers SET admin_credential_ciphertext=NULL,credential_set_at=NULL WHERE id=? AND org_id=? AND project_id=? AND state='active'`,
		`UPDATE dynamic_providers SET admin_credential_ciphertext=NULL,credential_set_at=NULL WHERE id=$1 AND org_id=$2 AND project_id=$3 AND state='active'`)
	rows, err := r.db.Exec(ctx, query, providerID, chain.Org, chain.Project)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}

func (r dynamicQueries) DeleteProvider(ctx context.Context, p authz.Proof, providerID string, at time.Time) error {
	chain, err := authz.Verify(p, authz.StoreDynamicProvidersDelete, r.tok)
	if err != nil {
		return err
	}
	// The sealed admin credential is DELIBERATELY retained on the tombstoned
	// row: the worker still needs it to drop the roles of the leases this delete
	// queued for revocation (nulling it here would strand every lease, since
	// LoadProviderMaterial would then return no credential and the drops would
	// retry forever). GetProvider filters state='active', so the provider is
	// gone from every ordinary read; only the proof-free worker join reaches it.
	// The reencrypt walk lists by ciphertext presence, not state, so the row
	// stays covered until a restore or an explicit later cleanup nulls it.
	query := r.db.SQL(
		`UPDATE dynamic_providers SET state='tombstoned' WHERE id=? AND org_id=? AND project_id=? AND state='active'`,
		`UPDATE dynamic_providers SET state='tombstoned' WHERE id=$1 AND org_id=$2 AND project_id=$3 AND state='active'`)
	rows, err := r.db.Exec(ctx, query, providerID, chain.Org, chain.Project)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}

// mintRecoveryGrace is how long a `minting` row is left unclaimable by the
// worker, so a healthy synchronous mint settles itself before the recovery
// sweep could drop its freshly created role. Comfortably longer than the
// provider deadline the mint runs under.
const mintRecoveryGrace = 5 * time.Minute

const dynamicLeaseColumns = `id,provider_id,environment_id,principal_id,principal_class,provider_handle,state,issued_at,expires_at,max_ttl_seconds,last_transition_at,created_at`

func scanDynamicLease(row interface{ Scan(...any) error }) (DynamicLease, error) {
	var out DynamicLease
	var issuedAt, expiresAt, lastTransitionAt, createdAt adapterStoredTime
	err := row.Scan(&out.ID, &out.ProviderID, &out.EnvironmentID, &out.PrincipalID, &out.PrincipalClass, &out.ProviderHandle, &out.State, &issuedAt, &expiresAt, &out.MaxTTLSeconds, &lastTransitionAt, &createdAt)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return DynamicLease{}, ErrNotFound
	}
	if err != nil {
		return DynamicLease{}, err
	}
	out.IssuedAt = issuedAt.value
	out.ExpiresAt = expiresAt.value
	out.LastTransitionAt = lastTransitionAt.value
	out.CreatedAt = createdAt.value
	return out, nil
}

func (r dynamicQueries) CreateLease(ctx context.Context, p authz.Proof, m DynamicLeaseCreate) (DynamicLease, error) {
	chain, err := authz.Verify(p, authz.StoreDynamicLeasesCreate, r.tok)
	if err != nil {
		return DynamicLease{}, err
	}
	// The environment is bound from the VERIFIED proof chain (chain.Env), never
	// from a caller argument: mint is env-scoped, so a proof for env A cannot
	// create a lease in env B.
	if chain.Env == "" {
		return DynamicLease{}, ErrNotFound
	}
	stamp := r.db.Stamp(m.At)
	// next_attempt_at is set a grace period ahead, NOT now: the synchronous mint
	// request is still running (it will FinishMint within seconds), and the
	// worker must not claim this `minting` row and mint-recover a role the
	// request is about to disclose. Only a genuinely crashed request — one that
	// never reached FinishMint before the grace elapses — is picked up.
	graceStamp := r.db.Stamp(m.At.Add(mintRecoveryGrace))
	query := r.db.SQL(
		`INSERT INTO dynamic_leases (id,org_id,project_id,environment_id,provider_id,principal_id,principal_class,provider_handle,state,max_ttl_seconds,last_transition_at,attempt_count,next_attempt_at,created_at) VALUES (?,?,?,?,?,?,?,?, 'minting', ?, ?, 0, ?, ?)`,
		`INSERT INTO dynamic_leases (id,org_id,project_id,environment_id,provider_id,principal_id,principal_class,provider_handle,state,max_ttl_seconds,last_transition_at,attempt_count,next_attempt_at,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8, 'minting', $9, $10, 0, $11, $12)`)
	if _, err := r.db.Exec(ctx, query, m.ID, chain.Org, chain.Project, chain.Env, m.ProviderID, m.PrincipalID, m.PrincipalClass, m.ProviderHandle, m.MaxTTLSeconds, stamp, graceStamp, stamp); err != nil {
		return DynamicLease{}, err
	}
	return r.GetLease(ctx, p, m.ID)
}

// GetLease reads one lease by id, bound to the proof's org/project and — when
// the proof is environment-scoped — its environment. A project-scoped proof
// (the provider-delete cascade) reaches any environment in the project; an
// env-scoped proof cannot reach a sibling environment's lease.
func (r dynamicQueries) GetLease(ctx context.Context, p authz.Proof, leaseID string) (DynamicLease, error) {
	chain, err := authz.Verify(p, authz.StoreDynamicLeasesGet, r.tok)
	if err != nil {
		return DynamicLease{}, err
	}
	query := r.db.SQL(
		`SELECT `+dynamicLeaseColumns+` FROM dynamic_leases WHERE id=? AND org_id=? AND project_id=? AND (?='' OR environment_id=?)`,
		`SELECT `+dynamicLeaseColumns+` FROM dynamic_leases WHERE id=$1 AND org_id=$2 AND project_id=$3 AND ($4='' OR environment_id=$5)`)
	return scanDynamicLease(r.db.QueryRow(ctx, query, leaseID, chain.Org, chain.Project, string(chain.Env), string(chain.Env)))
}

func (r dynamicQueries) ListLeasesForEnvironment(ctx context.Context, p authz.Proof) ([]DynamicLease, error) {
	chain, err := authz.Verify(p, authz.StoreDynamicLeasesList, r.tok)
	if err != nil {
		return nil, err
	}
	if chain.Env == "" {
		return nil, ErrNotFound
	}
	query := r.db.SQL(
		`SELECT `+dynamicLeaseColumns+` FROM dynamic_leases WHERE org_id=? AND project_id=? AND environment_id=? ORDER BY id`,
		`SELECT `+dynamicLeaseColumns+` FROM dynamic_leases WHERE org_id=$1 AND project_id=$2 AND environment_id=$3 ORDER BY id`)
	rows, err := r.db.Query(ctx, query, chain.Org, chain.Project, string(chain.Env))
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	var out []DynamicLease
	for rows.Next() {
		lease, err := scanDynamicLease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, lease)
	}
	return out, rows.Err()
}

func (r dynamicQueries) ActiveLeaseIDsForProvider(ctx context.Context, p authz.Proof, providerID string) ([]DynamicLease, error) {
	chain, err := authz.Verify(p, authz.StoreDynamicLeasesActiveIDsForProvider, r.tok)
	if err != nil {
		return nil, err
	}
	query := r.db.SQL(
		`SELECT `+dynamicLeaseColumns+` FROM dynamic_leases WHERE provider_id=? AND org_id=? AND project_id=? AND state NOT IN ('revoked','expired','failed') ORDER BY id`,
		`SELECT `+dynamicLeaseColumns+` FROM dynamic_leases WHERE provider_id=$1 AND org_id=$2 AND project_id=$3 AND state NOT IN ('revoked','expired','failed') ORDER BY id`)
	rows, err := r.db.Query(ctx, query, providerID, chain.Org, chain.Project)
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	var out []DynamicLease
	for rows.Next() {
		lease, err := scanDynamicLease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, lease)
	}
	return out, rows.Err()
}

func (r dynamicQueries) FinishMint(ctx context.Context, p authz.Proof, m DynamicLeaseFinishMint) error {
	chain, err := authz.Verify(p, authz.StoreDynamicLeasesFinishMint, r.tok)
	if err != nil {
		return err
	}
	if chain.Env == "" {
		return ErrNotFound
	}
	var issued, expires any
	if m.State == "active" {
		issued = r.db.Stamp(m.IssuedAt)
		expires = r.db.Stamp(m.ExpiresAt)
	}
	// lease_owner IS NULL is load-bearing: if the worker has already claimed this
	// still-minting row (a synchronous request paused past the mint grace), the
	// worker owns the recovery and this settle must NOT clear its fence or
	// disclose. The affected-row count of 0 then makes the request fail closed.
	query := r.db.SQL(
		`UPDATE dynamic_leases SET state=?,issued_at=?,expires_at=?,last_transition_at=?,next_attempt_at=?,lease_owner=NULL,lease_expires_at=NULL WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state='minting' AND lease_owner IS NULL`,
		`UPDATE dynamic_leases SET state=$1,issued_at=$2,expires_at=$3,last_transition_at=$4,next_attempt_at=$5,lease_owner=NULL,lease_expires_at=NULL WHERE id=$6 AND org_id=$7 AND project_id=$8 AND environment_id=$9 AND state='minting' AND lease_owner IS NULL`)
	rows, err := r.db.Exec(ctx, query, m.State, issued, expires, r.db.Stamp(m.At), r.db.Stamp(m.NextAttemptAt), m.LeaseID, chain.Org, chain.Project, string(chain.Env))
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}

func (r dynamicQueries) EnqueueTransition(ctx context.Context, p authz.Proof, m DynamicLeaseTransition) (DynamicLease, error) {
	chain, err := authz.Verify(p, authz.StoreDynamicLeasesEnqueueTransition, r.tok)
	if err != nil {
		return DynamicLease{}, err
	}
	// Only a settled/active lease can be pushed into a new transient state;
	// a lease already in flight is left to the worker. Revoke is allowed from
	// any non-terminal state so a compromised workload's lease can always be
	// torn down.
	var allowed string
	switch m.State {
	case "revoking":
		allowed = `state NOT IN ('revoked','expired','failed','revoking')`
	case "renewing":
		allowed = `state='active'`
	case "unknown":
		// Reconcile only re-triggers a lease that is ALREADY uncertain; it never
		// forces a healthy lease into re-probing (which mint-recovery would then
		// drop). A reconcile of a settled or active lease is a no-op conflict.
		allowed = `state='unknown'`
	default:
		return DynamicLease{}, errors.New("store: unknown lease transition target state")
	}
	// Environment is bound from the proof: an env-scoped proof (renew/revoke/
	// settle) may only touch a lease in its own environment; a project-scoped
	// proof (the provider-delete cascade) has chain.Env == "" and reaches every
	// environment in the project. The `(chain.Env='' OR environment_id=chain.Env)`
	// predicate expresses both from the verified chain, never a caller argument.
	env := string(chain.Env)
	var query string
	var args []any
	if m.MaxTTLSeconds > 0 {
		query = r.db.SQL(
			`UPDATE dynamic_leases SET state=?,max_ttl_seconds=?,last_transition_at=?,next_attempt_at=?,attempt_count=0,lease_owner=NULL,lease_expires_at=NULL WHERE id=? AND org_id=? AND project_id=? AND (?='' OR environment_id=?) AND `+allowed,
			`UPDATE dynamic_leases SET state=$1,max_ttl_seconds=$2,last_transition_at=$3,next_attempt_at=$4,attempt_count=0,lease_owner=NULL,lease_expires_at=NULL WHERE id=$5 AND org_id=$6 AND project_id=$7 AND ($8='' OR environment_id=$9) AND `+allowed)
		args = []any{m.State, m.MaxTTLSeconds, r.db.Stamp(m.At), r.db.Stamp(m.NextAttemptAt), m.LeaseID, chain.Org, chain.Project, env, env}
	} else {
		query = r.db.SQL(
			`UPDATE dynamic_leases SET state=?,last_transition_at=?,next_attempt_at=?,attempt_count=0,lease_owner=NULL,lease_expires_at=NULL WHERE id=? AND org_id=? AND project_id=? AND (?='' OR environment_id=?) AND `+allowed,
			`UPDATE dynamic_leases SET state=$1,last_transition_at=$2,next_attempt_at=$3,attempt_count=0,lease_owner=NULL,lease_expires_at=NULL WHERE id=$4 AND org_id=$5 AND project_id=$6 AND ($7='' OR environment_id=$8) AND `+allowed)
		args = []any{m.State, r.db.Stamp(m.At), r.db.Stamp(m.NextAttemptAt), m.LeaseID, chain.Org, chain.Project, env, env}
	}
	rows, err := r.db.Exec(ctx, query, args...)
	if err != nil {
		return DynamicLease{}, err
	}
	if rows != 1 {
		// The lease is missing or not in a state this transition may enter.
		if _, getErr := r.GetLease(ctx, p, m.LeaseID); errors.Is(getErr, ErrNotFound) {
			return DynamicLease{}, ErrNotFound
		}
		return DynamicLease{}, ErrConflict
	}
	return r.GetLease(ctx, p, m.LeaseID)
}

func (r dynamicQueries) ReencryptProvider(ctx context.Context, p authz.Proof, id string, newCiphertext, oldCiphertext []byte) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreDynamicProvidersReencrypt, r.tok)
	if err != nil {
		return false, err
	}
	query := r.db.SQL(
		`UPDATE dynamic_providers SET admin_credential_ciphertext=? WHERE org_id=? AND project_id=? AND id=? AND admin_credential_ciphertext=?`,
		`UPDATE dynamic_providers SET admin_credential_ciphertext=$1 WHERE org_id=$2 AND project_id=$3 AND id=$4 AND admin_credential_ciphertext=$5`)
	rows, err := r.db.Exec(ctx, query, newCiphertext, chain.Org, chain.Project, id, oldCiphertext)
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (r dynamicQueries) ListProvidersForReencrypt(ctx context.Context, p authz.Proof, cursor string, limit int) ([]ReencryptFieldRow, error) {
	chain, err := authz.Verify(p, authz.StoreDynamicProvidersListForReencrypt, r.tok)
	if err != nil {
		return nil, err
	}
	query := r.db.SQL(
		`SELECT id, admin_credential_ciphertext FROM dynamic_providers WHERE org_id=? AND project_id=? AND id>? AND admin_credential_ciphertext IS NOT NULL ORDER BY id LIMIT ?`,
		`SELECT id, admin_credential_ciphertext FROM dynamic_providers WHERE org_id=$1 AND project_id=$2 AND id>$3 AND admin_credential_ciphertext IS NOT NULL ORDER BY id LIMIT $4`)
	rows, err := r.db.Query(ctx, query, chain.Org, chain.Project, cursor, limit)
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	var out []ReencryptFieldRow
	for rows.Next() {
		var id string
		var ct []byte
		if err := rows.Scan(&id, &ct); err != nil {
			return nil, err
		}
		out = append(out, ReencryptFieldRow{ID: id, Owner: id, Ciphertext: ct})
	}
	return out, rows.Err()
}
