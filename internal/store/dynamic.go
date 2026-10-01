package store

import (
	"context"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
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
	queries dynamicStoreQueries
	tok     *authz.TxToken
}

func (r sqliteRepos) Dynamic() DynamicRepo {
	return dynamicQueries{queries: sqliteDynamicStoreQueries{queries: sqlitegen.New(r.db)}, tok: r.tok}
}
func (r pgRepos) Dynamic() DynamicRepo {
	return dynamicQueries{queries: pgDynamicStoreQueries{queries: pggen.New(r.db)}, tok: r.tok}
}

func (r dynamicQueries) CreateProvider(ctx context.Context, p authz.Proof, m DynamicProviderCreate) (DynamicProviderRecord, error) {
	chain, err := authz.Verify(p, authz.StoreDynamicProvidersCreate, r.tok)
	if err != nil {
		return DynamicProviderRecord{}, err
	}
	if _, err := r.queries.dynamicCreateProvider(ctx, chain, m); err != nil {
		return DynamicProviderRecord{}, err
	}
	return r.GetProvider(ctx, p, m.ID)
}

func (r dynamicQueries) GetProvider(ctx context.Context, p authz.Proof, providerID string) (DynamicProviderRecord, error) {
	chain, err := authz.Verify(p, authz.StoreDynamicProvidersGet, r.tok)
	if err != nil {
		return DynamicProviderRecord{}, err
	}
	return r.queries.dynamicGetProvider(ctx, chain, providerID)
}

func (r dynamicQueries) ProviderCredentialCiphertext(ctx context.Context, p authz.Proof, providerID string) ([]byte, error) {
	chain, err := authz.Verify(p, authz.StoreDynamicProvidersCredentialCiphertext, r.tok)
	if err != nil {
		return nil, err
	}
	ct, err := r.queries.dynamicProviderCredentialCiphertext(ctx, chain, providerID)
	if err != nil {
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
	return r.queries.dynamicListProviders(ctx, chain)
}

func (r dynamicQueries) ReplaceProviderCredential(ctx context.Context, p authz.Proof, m DynamicProviderCredentialMutation) error {
	chain, err := authz.Verify(p, authz.StoreDynamicProvidersReplaceCredential, r.tok)
	if err != nil {
		return err
	}
	rows, err := r.queries.dynamicReplaceProviderCredential(ctx, chain, m)
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
	rows, err := r.queries.dynamicRevokeProviderCredential(ctx, chain, providerID)
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
	rows, err := r.queries.dynamicDeleteProvider(ctx, chain, providerID)
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
	// next_attempt_at is set a grace period ahead, NOT now: the synchronous mint
	// request is still running (it will FinishMint within seconds), and the
	// worker must not claim this `minting` row and mint-recover a role the
	// request is about to disclose. Only a genuinely crashed request — one that
	// never reached FinishMint before the grace elapses — is picked up.
	if _, err := r.queries.dynamicCreateLease(ctx, chain, m); err != nil {
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
	return r.queries.dynamicGetLease(ctx, chain, leaseID)
}

func (r dynamicQueries) ListLeasesForEnvironment(ctx context.Context, p authz.Proof) ([]DynamicLease, error) {
	chain, err := authz.Verify(p, authz.StoreDynamicLeasesList, r.tok)
	if err != nil {
		return nil, err
	}
	if chain.Env == "" {
		return nil, ErrNotFound
	}
	return r.queries.dynamicListLeasesForEnvironment(ctx, chain)
}

func (r dynamicQueries) ActiveLeaseIDsForProvider(ctx context.Context, p authz.Proof, providerID string) ([]DynamicLease, error) {
	chain, err := authz.Verify(p, authz.StoreDynamicLeasesActiveIDsForProvider, r.tok)
	if err != nil {
		return nil, err
	}
	return r.queries.dynamicActiveLeaseIDsForProvider(ctx, chain, providerID)
}

func (r dynamicQueries) FinishMint(ctx context.Context, p authz.Proof, m DynamicLeaseFinishMint) error {
	chain, err := authz.Verify(p, authz.StoreDynamicLeasesFinishMint, r.tok)
	if err != nil {
		return err
	}
	if chain.Env == "" {
		return ErrNotFound
	}

	// lease_owner IS NULL is load-bearing: if the worker has already claimed this
	// still-minting row (a synchronous request paused past the mint grace), the
	// worker owns the recovery and this settle must NOT clear its fence or
	// disclose. The affected-row count of 0 then makes the request fail closed.
	rows, err := r.queries.dynamicFinishMint(ctx, chain, m)
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
	switch m.State {
	case "revoking", "renewing", "unknown":
	default:
		return DynamicLease{}, errors.New("store: unknown lease transition target state")
	}

	// Environment is bound from the proof: an env-scoped proof (renew/revoke/
	// settle) may only touch a lease in its own environment; a project-scoped
	// proof (the provider-delete cascade) has chain.Env == "" and reaches every
	// environment in the project. The `(chain.Env='' OR environment_id=chain.Env)`
	// predicate expresses both from the verified chain, never a caller argument.
	rows, err := r.queries.dynamicEnqueueTransition(ctx, chain, m)
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
	rows, err := r.queries.dynamicReencryptProvider(ctx, chain, id, newCiphertext, oldCiphertext)
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
	return r.queries.dynamicListProvidersForReencrypt(ctx, chain, cursor, limit)
}
