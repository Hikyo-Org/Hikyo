package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Dynamic owns the dynamic-secret surface (#147): PostgreSQL providers
// (project-scoped standing authority) and leases (environment-scoped, short-
// lived credentials). Mint is synchronous because the credential is disclosed
// once and never stored; renew, revoke, expiry, and settle are worker-driven
// and durable across restart.
type Dynamic struct {
	DB              *store.DB
	Auth            *Auth
	Keyring         *crypto.Keyring
	Budget          *Budget
	Runtime         *store.DynamicRuntime
	ProviderFactory dynamic.Factory
	// LeaseDeadline bounds one worker term's provider work; it must be shorter
	// than the claim lease so a slow provider cannot outlive the crash fence.
	LeaseDeadline time.Duration
	Now           func() time.Time
}

func (s *Dynamic) now() time.Time {
	return nowOr(s.Now)
}

var (
	// ErrProviderHasActiveLeases refuses a provider delete that would strand
	// live leases unless the caller asks to revoke them all.
	ErrProviderHasActiveLeases = fmt.Errorf("%w: provider still has active leases; pass revoke_all to revoke them", domain.ErrConflict)
	// ErrProviderUnreachable reports a provider that could not be reached or
	// authenticated at configuration time. Fail loud: a provider that cannot
	// mint is never recorded as usable.
	ErrProviderUnreachable = fmt.Errorf("%w: dynamic provider unreachable or credential refused", domain.ErrInvalid)
)

// validateProviderOrigin rejects any origin that could smuggle a secret or a
// connection override into stored, readable metadata: an embedded password, ANY
// query string (e.g. `?password=`, `?sslmode=`, `?host=`), a fragment, a wrong
// scheme, or a missing user/host/database. The origin is stored verbatim and
// returned by ordinary reads, so it must be secret-free and normalized.
func validateProviderOrigin(origin string) error {
	u, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("%w: origin %q is not a URL", domain.ErrInvalid, origin)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("%w: origin must be a postgres:// URL", domain.ErrInvalid)
	}
	if u.User == nil || u.User.Username() == "" {
		return fmt.Errorf("%w: origin must carry the admin username", domain.ErrInvalid)
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		return fmt.Errorf("%w: origin must not embed a password (it would be stored and readable)", domain.ErrInvalid)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%w: origin must not carry query parameters or a fragment", domain.ErrInvalid)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("%w: origin must carry a host", domain.ErrInvalid)
	}
	if strings.Trim(u.Path, "/") == "" {
		return fmt.Errorf("%w: origin must name a database", domain.ErrInvalid)
	}
	return nil
}

// providerCredentialAAD binds a sealed admin credential to its provider row.
func providerCredentialAAD(orgID, projectID, providerID string) crypto.ProjectFieldAAD {
	return crypto.ProjectFieldAAD{
		OrgID: orgID, ProjectID: projectID,
		OwnerTable: "dynamic_providers", OwnerRowID: providerID, FieldTag: "credential",
	}
}

// connectivityProbe is a harmless role name used to prove a provider is
// reachable and the admin credential authenticates, without any side effect.
const connectivityProbe = "hikyo_connectivitycheck"

// ---- Views ---------------------------------------------------------------

type DynamicProviderView struct {
	store.DynamicProviderRecord
}

type DynamicLeaseView struct {
	store.DynamicLease
}

// MintLeaseResult carries the display-once secret exactly once.
type MintLeaseResult struct {
	Lease     store.DynamicLease
	Username  string
	Password  string
	ExpiresAt time.Time
}

// ---- Providers -----------------------------------------------------------

type CreateDynamicProviderRequest struct {
	Kind       string
	Origin     string
	TLSMode    string
	GrantRole  string
	Credential []byte
}

func (s *Dynamic) Configure(ctx context.Context, actor Actor, scope domain.Scope, req CreateDynamicProviderRequest) (DynamicProviderView, error) {
	if err := requireProjectScope(scope, "dynamic provider create requires project scope"); err != nil {
		return DynamicProviderView{}, err
	}
	kind, err := dynamic.ParseKind(req.Kind)
	if err != nil {
		return DynamicProviderView{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	tlsMode := req.TLSMode
	if tlsMode == "" {
		tlsMode = "verify-full"
	}
	if tlsMode != "verify-full" {
		return DynamicProviderView{}, fmt.Errorf("%w: only tls_mode verify-full is accepted", domain.ErrInvalid)
	}
	if req.Origin == "" || req.GrantRole == "" || len(req.Credential) == 0 {
		return DynamicProviderView{}, fmt.Errorf("%w: dynamic provider create requires origin, grant_role, and credential", domain.ErrInvalid)
	}
	if err := validateProviderOrigin(req.Origin); err != nil {
		return DynamicProviderView{}, err
	}
	release, err := chargeDefaultAtEntry(ctx, s.DB, s.Budget, actor, authz.OpDynamicProviderConfigure, authz.OpDynamicProviderConfigure, scope, s.now)
	if err != nil {
		return DynamicProviderView{}, err
	}
	defer release()

	providerID, err := newID("dpv")
	if err != nil {
		return DynamicProviderView{}, err
	}
	// Prove reachability + authentication before recording a usable provider.
	if err := s.probeProvider(ctx, kind, req.Origin, tlsMode, string(req.Credential)); err != nil {
		return DynamicProviderView{}, err
	}
	sealer, err := sealerFor(ctx, s.DB, s.Keyring, actor, authz.OpDynamicProviderConfigure, scope)
	if err != nil {
		return DynamicProviderView{}, err
	}
	plain := slices.Clone(req.Credential)
	defer crypto.Zero(plain)
	sealed, err := sealer.SealField(providerCredentialAAD(string(scope.Org), string(scope.Project), providerID), plain)
	if err != nil {
		return DynamicProviderView{}, err
	}
	now := store.CanonTime(s.now())
	var out DynamicProviderView
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpDynamicProviderConfigure, scope, now)
		if err != nil {
			return err
		}
		if err := fenceProject(ctx, r, proof, sealer, scope); err != nil {
			return err
		}
		record, err := r.Dynamic().CreateProvider(ctx, proof, store.DynamicProviderCreate{
			ID: providerID, Kind: string(kind), Origin: req.Origin, TLSMode: tlsMode,
			GrantRole: req.GrantRole, CredentialCiphertext: sealed,
			AuthorityPrincipalID: string(caller.Principal), At: now,
		})
		if err != nil {
			return err
		}
		out = DynamicProviderView{DynamicProviderRecord: record}
		ev, err := domainEvent(ctx, audit.EventDynamicProviderConfigured, caller.Principal,
			audit.Object{Type: "dynamic-provider", ID: providerID},
			audit.Payload{"kind": string(kind), "authority": string(caller.Principal)})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, proof, ev)
	})
	return out, err
}

func (s *Dynamic) List(ctx context.Context, actor Actor, scope domain.Scope) ([]DynamicProviderView, error) {
	if err := requireProjectScope(scope, "dynamic provider list requires project scope"); err != nil {
		return nil, err
	}
	var out []DynamicProviderView
	now := store.CanonTime(s.now())
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpDynamicProviderInspect, scope, now)
		if err != nil {
			return err
		}
		rows, err := r.Dynamic().ListProviders(ctx, proof)
		if err != nil {
			return err
		}
		out = out[:0]
		for _, row := range rows {
			out = append(out, DynamicProviderView{DynamicProviderRecord: row})
		}
		return insertInspected(ctx, r, proof, caller.Principal, audit.EventDynamicProviderInspected, audit.Object{Type: "dynamic-provider", ID: "dynamic-provider-list"}, int64(len(rows)))
	})
	return out, err
}

// insertInspected records the manage-identities-gated read of provider
// metadata, mirroring adapter.inspect. A privileged read audits; only a
// bare-read lease inspect is auditedNone.
func (s *Dynamic) Get(ctx context.Context, actor Actor, scope domain.Scope, providerID string) (DynamicProviderView, error) {
	if err := requireProjectScope(scope, "dynamic provider show requires project scope and provider id", providerID); err != nil {
		return DynamicProviderView{}, err
	}
	var out DynamicProviderView
	now := store.CanonTime(s.now())
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpDynamicProviderInspect, scope, now)
		if err != nil {
			return err
		}
		record, err := r.Dynamic().GetProvider(ctx, proof, providerID)
		if err != nil {
			return err
		}
		out = DynamicProviderView{DynamicProviderRecord: record}
		return insertInspected(ctx, r, proof, caller.Principal, audit.EventDynamicProviderInspected, audit.Object{Type: "dynamic-provider", ID: providerID}, 1)
	})
	return out, err
}

// providerMetadata reads a provider's non-secret metadata under the
// credential-set authority (no inspect audit), for the pre-store probe on a
// credential replacement.
func (s *Dynamic) providerMetadata(ctx context.Context, actor Actor, scope domain.Scope, providerID string) (store.DynamicProviderRecord, error) {
	var out store.DynamicProviderRecord
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpDynamicProviderCredentialSet, scope, s.now())
		if err != nil {
			return err
		}
		out, err = r.Dynamic().GetProvider(ctx, proof, providerID)
		return err
	})
	return out, err
}

func (s *Dynamic) ReplaceCredential(ctx context.Context, actor Actor, scope domain.Scope, providerID string, credential []byte) error {
	if scope.Project == "" || scope.Env != "" || providerID == "" || len(credential) == 0 {
		return fmt.Errorf("%w: credential replacement requires project scope, provider id, and non-empty credential", domain.ErrInvalid)
	}
	release, err := chargeDefaultAtEntry(ctx, s.DB, s.Budget, actor, authz.OpDynamicProviderCredentialSet, authz.OpDynamicProviderCredentialSet, scope, s.now)
	if err != nil {
		return err
	}
	defer release()
	// Load the provider's origin/tls to probe the new credential before storing.
	view, err := s.providerMetadata(ctx, actor, scope, providerID)
	if err != nil {
		return err
	}
	kind, err := dynamic.ParseKind(view.Kind)
	if err != nil {
		return err
	}
	if err := s.probeProvider(ctx, kind, view.Origin, view.TLSMode, string(credential)); err != nil {
		return err
	}
	sealer, err := sealerFor(ctx, s.DB, s.Keyring, actor, authz.OpDynamicProviderCredentialSet, scope)
	if err != nil {
		return err
	}
	plain := slices.Clone(credential)
	defer crypto.Zero(plain)
	sealed, err := sealer.SealField(providerCredentialAAD(string(scope.Org), string(scope.Project), providerID), plain)
	if err != nil {
		return err
	}
	now := store.CanonTime(s.now())
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpDynamicProviderCredentialSet, scope, now)
		if err != nil {
			return err
		}
		if err := fenceProject(ctx, r, proof, sealer, scope); err != nil {
			return err
		}
		if err := r.Dynamic().ReplaceProviderCredential(ctx, proof, store.DynamicProviderCredentialMutation{
			ProviderID: providerID, CredentialCiphertext: sealed, At: now,
		}); err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventDynamicProviderCredentialReplace, caller.Principal,
			audit.Object{Type: "dynamic-provider", ID: providerID}, audit.Payload{"credential_present": true})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, proof, ev)
	})
}

func (s *Dynamic) RevokeCredential(ctx context.Context, actor Actor, scope domain.Scope, providerID string) error {
	if err := requireProjectScope(scope, "credential revocation requires project scope and provider id", providerID); err != nil {
		return err
	}
	now := store.CanonTime(s.now())
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpDynamicProviderCredentialRevoke, scope, now)
		if err != nil {
			return err
		}
		if err := r.Dynamic().RevokeProviderCredential(ctx, proof, providerID, now); err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventDynamicProviderCredentialRevoke, caller.Principal,
			audit.Object{Type: "dynamic-provider", ID: providerID}, audit.Payload{"credential_present": false})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, proof, ev)
	})
}

// DeleteResult reports which leases a delete queued for revocation.
type DeleteResult struct {
	ProviderID      string
	RevokedLeaseIDs []string
}

func (s *Dynamic) Delete(ctx context.Context, actor Actor, scope domain.Scope, providerID string, revokeAll bool) (DeleteResult, error) {
	if err := requireProjectScope(scope, "dynamic provider delete requires project scope and provider id", providerID); err != nil {
		return DeleteResult{}, err
	}
	now := store.CanonTime(s.now())
	var out DeleteResult
	out.ProviderID = providerID
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpDynamicProviderDelete, scope, now)
		if err != nil {
			return err
		}
		if _, err := r.Dynamic().GetProvider(ctx, proof, providerID); err != nil {
			return err
		}
		active, err := r.Dynamic().ActiveLeaseIDsForProvider(ctx, proof, providerID)
		if err != nil {
			return err
		}
		if len(active) > 0 && !revokeAll {
			return ErrProviderHasActiveLeases
		}
		for _, lease := range active {
			enqueued, err := r.Dynamic().EnqueueTransition(ctx, proof, store.DynamicLeaseTransition{
				LeaseID: lease.ID, State: "revoking",
				NextAttemptAt: now, At: now,
			})
			if err != nil {
				if errors.Is(err, domain.ErrConflict) {
					// Already terminal or in flight; nothing to queue.
					continue
				}
				return err
			}
			out.RevokedLeaseIDs = append(out.RevokedLeaseIDs, enqueued.ID)
			intent, err := newAuditEvent(ctx, audit.EventDynamicLeaseTransitionIntent, caller.Principal,
				audit.Object{Type: "dynamic-lease", ID: lease.ID}, audit.OutcomeIntent, "",
				audit.Payload{"kind": "revoke", "provider_handle": lease.ProviderHandle})
			if err != nil {
				return err
			}
			if err := r.Audit().InsertTenant(ctx, proof, intent); err != nil {
				return err
			}
		}
		if err := r.Dynamic().DeleteProvider(ctx, proof, providerID, now); err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventDynamicProviderDeleted, caller.Principal,
			audit.Object{Type: "dynamic-provider", ID: providerID},
			audit.Payload{"revoked_lease_count": int64(len(out.RevokedLeaseIDs))})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, proof, ev)
	})
	return out, err
}

// ---- Leases --------------------------------------------------------------

type MintLeaseRequest struct {
	ProviderID    string
	MaxTTLSeconds int64
}

func (s *Dynamic) MintLease(ctx context.Context, actor Actor, scope domain.Scope, req MintLeaseRequest) (MintLeaseResult, error) {
	if scope.Project == "" || scope.Env == "" || req.ProviderID == "" {
		return MintLeaseResult{}, fmt.Errorf("%w: lease mint requires environment scope and provider id", domain.ErrInvalid)
	}
	if req.MaxTTLSeconds <= 0 {
		return MintLeaseResult{}, fmt.Errorf("%w: lease mint requires a positive max_ttl_seconds", domain.ErrInvalid)
	}
	release, err := chargeDefaultAtEntry(ctx, s.DB, s.Budget, actor, authz.OpLeaseMint, authz.OpLeaseMint, scope, s.now)
	if err != nil {
		return MintLeaseResult{}, err
	}
	defer release()

	projectScope := domain.Scope{Org: scope.Org, Project: scope.Project}
	leaseID, err := newID("dls")
	if err != nil {
		return MintLeaseResult{}, err
	}
	roleName := dynamic.RoleName(leaseID)
	password, err := dynamic.GeneratePassword()
	if err != nil {
		return MintLeaseResult{}, err
	}
	// Phase 1 (tx): authorize, gate disclosure authority, record the mint intent
	// (lease row minting + audit intent), and read the SEALED admin credential.
	// The credential is NOT decrypted inside the transaction: a tx error or
	// replay would otherwise leave plaintext buffers unzeroed. It is opened once,
	// after commit, with its zeroing deferred immediately.
	var (
		providerKind, providerOrigin, providerTLS, grantRole string
		ciphertext                                           []byte
		principalClass                                       string
	)
	now := store.CanonTime(s.now())
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpLeaseMint, scope, now)
		if err != nil {
			return err
		}
		if err := s.leaseDisclosureGate(ctx, az, caller, scope, projectScope, now, true); err != nil {
			return err
		}
		provider, err := r.Dynamic().GetProvider(ctx, proof, req.ProviderID)
		if err != nil {
			return err
		}
		providerKind, providerOrigin, providerTLS, grantRole = provider.Kind, provider.Origin, provider.TLSMode, provider.GrantRole
		principalClass = string(caller.Class)
		ciphertext, err = r.Dynamic().ProviderCredentialCiphertext(ctx, proof, req.ProviderID)
		if err != nil {
			return err
		}
		if _, err := r.Dynamic().CreateLease(ctx, proof, store.DynamicLeaseCreate{
			ID: leaseID, ProviderID: req.ProviderID,
			PrincipalID: string(caller.Principal), PrincipalClass: principalClass,
			ProviderHandle: roleName, MaxTTLSeconds: req.MaxTTLSeconds, At: now,
		}); err != nil {
			return err
		}
		intent, err := newAuditEvent(ctx, audit.EventDynamicLeaseTransitionIntent, caller.Principal,
			audit.Object{Type: "dynamic-lease", ID: leaseID}, audit.OutcomeIntent, "",
			audit.Payload{"kind": "mint", "provider_handle": roleName})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, proof, intent)
	})
	if err != nil {
		return MintLeaseResult{}, err
	}
	sealer, err := s.Keyring.ForProject(ctx, string(scope.Org), string(scope.Project))
	if err != nil {
		return MintLeaseResult{}, err
	}
	adminCredential, err := sealer.OpenField(providerCredentialAAD(string(scope.Org), string(scope.Project), req.ProviderID), ciphertext)
	if err != nil {
		return MintLeaseResult{}, err
	}
	defer crypto.Zero(adminCredential)

	// Phase 2: mint at the provider (outside any transaction).
	kind, err := dynamic.ParseKind(providerKind)
	if err != nil {
		return MintLeaseResult{}, err
	}
	provider, err := s.ProviderFactory(kind, providerOrigin, providerTLS, string(adminCredential))
	if err != nil {
		return MintLeaseResult{}, err
	}
	defer provider.Close()
	expiresAt := s.now().Add(time.Duration(req.MaxTTLSeconds) * time.Second)
	createErr := provider.CreateRole(ctx, dynamic.CreateRoleRequest{
		Name: roleName, Password: password, GrantRole: grantRole, ValidUntil: expiresAt,
	})

	// Phase 3: settle the mint in ONE transaction that also re-checks disclosure
	// authority, so there is no window between the recheck and the disclosure.
	// Provider outcome fixes the base state; a successful mint whose authority
	// was withdrawn mid-flight is NEVER disclosed — instead the created role is
	// handed to the worker to drop (state `revoking`), atomically.
	baseState, baseOutcome := "active", "success"
	switch {
	case createErr == nil:
	case errors.Is(createErr, dynamic.ErrUnreachable), errors.Is(createErr, dynamic.ErrRefused):
		baseState, baseOutcome = "failed", "failure"
	case errors.Is(createErr, dynamic.ErrAmbiguous):
		baseState, baseOutcome = "unknown", "unknown"
	default:
		baseState, baseOutcome = "unknown", "unknown"
	}
	issuedAt := store.CanonTime(s.now())
	settleNow := store.CanonTime(s.now())
	var (
		mintedLease store.DynamicLease
		disclosed   bool
	)
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpLeaseMint, scope, settleNow)
		if err != nil {
			return err
		}
		finishState, mintOutcome := baseState, baseOutcome
		disclosed = createErr == nil
		if createErr == nil {
			// Re-check disclosure authority WITHOUT consuming another ceremony,
			// in the same tx as the disclosure. If it was withdrawn, do not
			// disclose: settle to `revoking` so the worker drops the role that
			// was created, and the password never leaves this process.
			if err := s.leaseDisclosureGate(ctx, az, caller, scope, projectScope, settleNow, false); err != nil {
				finishState, mintOutcome, disclosed = "revoking", "failure", false
			}
		}
		fin := store.DynamicLeaseFinishMint{LeaseID: leaseID, State: finishState, At: settleNow}
		switch finishState {
		case "active":
			fin.IssuedAt, fin.ExpiresAt, fin.NextAttemptAt = issuedAt, store.CanonTime(expiresAt), store.CanonTime(expiresAt)
		case "revoking":
			// Due immediately so the worker drops the never-disclosed role.
			fin.NextAttemptAt = settleNow
		default:
			fin.NextAttemptAt = settleNow
		}
		if err := r.Dynamic().FinishMint(ctx, proof, fin); err != nil {
			return err
		}
		outcome, err := newAuditEvent(ctx, audit.EventDynamicLeaseTransitionOutcome, caller.Principal,
			audit.Object{Type: "dynamic-lease", ID: leaseID}, audit.Outcome(mintOutcome), "",
			audit.Payload{"kind": "mint", "provider_handle": roleName})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, proof, outcome); err != nil {
			return err
		}
		if disclosed {
			ev, err := domainEvent(ctx, audit.EventDynamicLeaseDisclosed, caller.Principal,
				audit.Object{Type: "dynamic-lease", ID: leaseID}, audit.Payload{
					"provider_handle": roleName, "principal_class": principalClass,
					"expires_at": expiresAt.UTC().Format(time.RFC3339),
				})
			if err != nil {
				return err
			}
			if err := r.Audit().InsertTenant(ctx, proof, ev); err != nil {
				return err
			}
		}
		// Read the settled lease inside this SAME transaction, so the response
		// never depends on a fallible post-commit read that could lose the
		// display-once password after the role is already live.
		mintedLease, err = r.Dynamic().GetLease(ctx, proof, leaseID)
		return err
	})
	if err != nil {
		return MintLeaseResult{}, err
	}
	if !disclosed {
		return MintLeaseResult{}, fmt.Errorf("%w: minting the credential failed", domain.ErrConflict)
	}
	return MintLeaseResult{Lease: mintedLease, Username: roleName, Password: password, ExpiresAt: expiresAt}, nil
}

// leaseDisclosureGate enforces disclosure authority for a lease mint or renew,
// per caller class, since the op formula is read@env only (machine-holdable).
// Both classes must hold `reveal` over the environment; a machine additionally
// needs the project's machine-reveal opt-in (the same conjunct delivery
// applies); a human additionally consumes a fresh mint ceremony when
// consumeCeremony is set. A missing grant answers the uniform nonexistent
// response so the surface is not an authority oracle.
func (s *Dynamic) leaseDisclosureGate(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, scope, projectScope domain.Scope, now time.Time, consumeCeremony bool) error {
	grants, err := az.GrantRowsForPrincipal(ctx, caller.Principal)
	if err != nil {
		return err
	}
	if !holds(grants, domain.CapReveal, scope) {
		return domain.ErrNotFound
	}
	if domain.IsServiceAccountKind(caller.Class) {
		enabled, _, err := az.MachineRevealOptIn(ctx, caller, scope.Project)
		if err != nil {
			return err
		}
		if !enabled {
			return domain.ErrNotFound
		}
		return nil
	}
	if !consumeCeremony {
		return nil
	}
	if s.Auth == nil {
		return errors.New("service: dynamic secrets have no reauthentication seam wired")
	}
	intent, err := NewMintReauthIntent(string(scope.Env), nil)
	if err != nil {
		return err
	}
	if err := s.Auth.ConsumeReauthWindow(ctx, az, caller.SessionID, intent, now); err != nil {
		switch {
		case errors.Is(err, ErrNoReauthWindow), errors.Is(err, ErrReauthWindowExpired),
			errors.Is(err, ErrReauthUnitMismatch), errors.Is(err, ErrReauthWindowSpent):
			return fmt.Errorf("%w (lease mint)", ErrReauthRequired)
		default:
			return err
		}
	}
	return nil
}

func (s *Dynamic) ListLeases(ctx context.Context, actor Actor, scope domain.Scope) ([]DynamicLeaseView, error) {
	if scope.Project == "" || scope.Env == "" {
		return nil, fmt.Errorf("%w: lease list requires environment scope", domain.ErrInvalid)
	}
	var out []DynamicLeaseView
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpLeaseInspect, scope, s.now())
		if err != nil {
			return err
		}
		rows, err := r.Dynamic().ListLeasesForEnvironment(ctx, proof)
		if err != nil {
			return err
		}
		for _, row := range rows {
			out = append(out, DynamicLeaseView{DynamicLease: row})
		}
		return nil
	})
	return out, err
}

func (s *Dynamic) GetLease(ctx context.Context, actor Actor, scope domain.Scope, leaseID string) (DynamicLeaseView, error) {
	if scope.Project == "" || scope.Env == "" || leaseID == "" {
		return DynamicLeaseView{}, fmt.Errorf("%w: lease show requires environment scope and lease id", domain.ErrInvalid)
	}
	var out DynamicLeaseView
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpLeaseInspect, scope, s.now())
		if err != nil {
			return err
		}
		lease, err := r.Dynamic().GetLease(ctx, proof, leaseID)
		if err != nil {
			return err
		}
		out = DynamicLeaseView{DynamicLease: lease}
		return nil
	})
	return out, err
}

func (s *Dynamic) RenewLease(ctx context.Context, actor Actor, scope domain.Scope, leaseID string, maxTTLSeconds int64) (DynamicLeaseView, error) {
	return s.enqueueLeaseTransition(ctx, actor, scope, leaseID, authz.OpLeaseRenew, "renewing", "renew", maxTTLSeconds)
}

func (s *Dynamic) RevokeLease(ctx context.Context, actor Actor, scope domain.Scope, leaseID string) (DynamicLeaseView, error) {
	return s.enqueueLeaseTransition(ctx, actor, scope, leaseID, authz.OpLeaseRevoke, "revoking", "revoke", 0)
}

func (s *Dynamic) SettleLease(ctx context.Context, actor Actor, scope domain.Scope, leaseID string) (DynamicLeaseView, error) {
	return s.enqueueLeaseTransition(ctx, actor, scope, leaseID, authz.OpLeaseSettle, "unknown", "settle", 0)
}

func (s *Dynamic) enqueueLeaseTransition(ctx context.Context, actor Actor, scope domain.Scope, leaseID string, op authz.Operation, state, kind string, maxTTLSeconds int64) (DynamicLeaseView, error) {
	if scope.Project == "" || scope.Env == "" || leaseID == "" {
		return DynamicLeaseView{}, fmt.Errorf("%w: lease %s requires environment scope and lease id", domain.ErrInvalid, kind)
	}
	now := store.CanonTime(s.now())
	var out DynamicLeaseView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, op, scope, now)
		if err != nil {
			return err
		}
		lease, err := r.Dynamic().EnqueueTransition(ctx, proof, store.DynamicLeaseTransition{
			LeaseID: leaseID, State: state,
			MaxTTLSeconds: maxTTLSeconds, NextAttemptAt: now, At: now,
		})
		if err != nil {
			return err
		}
		out = DynamicLeaseView{DynamicLease: lease}
		if op == authz.OpLeaseSettle {
			ev, err := domainEvent(ctx, audit.EventDynamicLeaseSettleRequested, caller.Principal,
				audit.Object{Type: "dynamic-lease", ID: leaseID}, audit.Payload{"provider_handle": lease.ProviderHandle})
			if err != nil {
				return err
			}
			return r.Audit().InsertTenant(ctx, proof, ev)
		}
		intent, err := newAuditEvent(ctx, audit.EventDynamicLeaseTransitionIntent, caller.Principal,
			audit.Object{Type: "dynamic-lease", ID: leaseID}, audit.OutcomeIntent, "",
			audit.Payload{"kind": kind, "provider_handle": lease.ProviderHandle})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, proof, intent)
	})
	return out, err
}

func (s *Dynamic) probeProvider(ctx context.Context, kind dynamic.Kind, origin, tlsMode, credential string) error {
	if s.ProviderFactory == nil {
		return errors.New("service: dynamic provider factory is not configured")
	}
	provider, err := s.ProviderFactory(kind, origin, tlsMode, credential)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	defer provider.Close()
	if _, err := provider.RoleStatus(ctx, connectivityProbe); err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	return nil
}
