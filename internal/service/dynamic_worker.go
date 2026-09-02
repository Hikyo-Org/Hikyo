package service

import (
	"context"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

const (
	dynamicRetryFloor = 30 * time.Second
	dynamicRetryCap   = time.Hour
	dynamicLeaseTerm  = 2 * time.Minute
)

func (s *Dynamic) leaseDeadline() time.Duration {
	if s.LeaseDeadline <= 0 {
		return 10 * time.Second
	}
	return s.LeaseDeadline
}

func dynamicRetryDelay(attempt int) time.Duration {
	delay := dynamicRetryFloor
	for i := 1; i < attempt && delay < dynamicRetryCap; i++ {
		delay *= 2
		if delay > dynamicRetryCap {
			delay = dynamicRetryCap
		}
	}
	return delay
}

// RunLeaseSweep claims and settles one due lease transition. It returns true
// when it did work (so the scheduler job can loop until idle). It is the body
// the fenced dynamic-lease scheduler runs under #146's singleton lease, so at
// most one node ever executes a given lease's transition.
func (s *Dynamic) RunLeaseSweep(ctx context.Context, workerID string) (bool, error) {
	if s.Runtime == nil || s.ProviderFactory == nil || s.Keyring == nil {
		return false, errors.New("service: dynamic worker requires runtime, provider factory, and keyring")
	}
	now := s.now()
	lease, ok, err := s.Runtime.ClaimDueLease(ctx, workerID, now, now.Add(dynamicLeaseTerm))
	if err != nil || !ok {
		return ok, err
	}
	kind, err := s.deriveKind(ctx, lease)
	if err != nil {
		// Cannot determine the transition; release for a later attempt.
		return true, s.Runtime.Retry(ctx, lease, s.now().Add(dynamicRetryDelay(lease.Attempt)))
	}
	provider, release, err := s.openProvider(ctx, lease)
	if err != nil {
		if errors.Is(err, store.ErrNoProviderCredential) {
			// The operator revoked or a restore invalidated the admin
			// credential: nothing can be done at the provider until it is
			// re-set. Leave the lease due for a later retry.
			return true, s.Runtime.Retry(ctx, lease, s.now().Add(dynamicRetryDelay(lease.Attempt)))
		}
		return true, s.Runtime.Retry(ctx, lease, s.now().Add(dynamicRetryDelay(lease.Attempt)))
	}
	defer release()

	switch kind {
	case "renew":
		return true, s.settleRenew(ctx, lease, provider)
	case "revoke":
		return true, s.settleDrop(ctx, lease, provider, "revoke", "revoked")
	case "expire":
		return true, s.settleDrop(ctx, lease, provider, "expire", "expired")
	case "mint":
		return true, s.settleMintRecovery(ctx, lease, provider)
	default:
		return true, s.Runtime.Retry(ctx, lease, s.now().Add(dynamicRetryDelay(lease.Attempt)))
	}
}

// deriveKind maps a claimed lease to the transition the worker must perform.
// A minting lease is a crashed synchronous mint; an unknown lease resumes the
// exact transition its latest effect recorded (or a mint if none exists).
func (s *Dynamic) deriveKind(ctx context.Context, lease store.ClaimedLease) (string, error) {
	switch lease.State {
	case "renewing":
		return "renew", nil
	case "revoking":
		return "revoke", nil
	case "minting":
		return "mint", nil
	case "active":
		return "expire", nil
	case "unknown":
		kind, err := s.Runtime.LatestEffectKind(ctx, lease)
		if errors.Is(err, store.ErrNotFound) {
			return "mint", nil
		}
		if err != nil {
			return "", err
		}
		return kind, nil
	default:
		return "", errors.New("service: unclaimable lease state")
	}
}

func (s *Dynamic) openProvider(ctx context.Context, lease store.ClaimedLease) (dynamic.Provider, func(), error) {
	material, err := s.Runtime.LoadProviderMaterial(ctx, lease)
	if err != nil {
		return nil, nil, err
	}
	sealer, err := s.Keyring.ForProject(ctx, lease.OrgID, lease.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	credential, err := sealer.OpenField(providerCredentialAAD(lease.OrgID, lease.ProjectID, material.CredentialOwnerID), material.CredentialCiphertext)
	if err != nil {
		return nil, nil, err
	}
	defer crypto.Zero(credential)
	kind, err := dynamic.ParseKind(material.Kind)
	if err != nil {
		return nil, nil, err
	}
	provider, err := s.ProviderFactory(kind, material.Origin, material.TLSMode, string(credential))
	if err != nil {
		return nil, nil, err
	}
	return provider, provider.Close, nil
}

// settleRenew re-authorizes the lease principal (renew creates access, so it
// re-checks read+reveal and the machine-reveal opt-in) then extends the role.
func (s *Dynamic) settleRenew(ctx context.Context, lease store.ClaimedLease, provider dynamic.Provider) error {
	effectID, err := s.Runtime.RecordIntent(ctx, lease, "renew")
	if err != nil {
		return err
	}
	if err := s.reauthorizeLease(ctx, lease, authz.OpLeaseRenew); err != nil {
		// Authority withdrawn: the renew fails, the credential keeps its
		// current validity and expires naturally.
		return s.Runtime.RecordOutcome(ctx, lease, effectID, "renew", "failure", "active", time.Time{}, time.Time{})
	}
	opCtx, cancel := context.WithTimeout(ctx, s.leaseDeadline())
	defer cancel()
	newExpiry := s.now().Add(time.Duration(lease.MaxTTLSeconds) * time.Second)
	err = provider.ExtendRole(opCtx, lease.ProviderHandle, newExpiry)
	switch {
	case err == nil:
		return s.Runtime.RecordOutcome(ctx, lease, effectID, "renew", "success", "active", lease.IssuedAt, newExpiry)
	case errors.Is(err, dynamic.ErrUnreachable):
		return s.Runtime.RecordOutcomeRetry(ctx, lease, effectID, "renew", "renewing", s.now().Add(dynamicRetryDelay(lease.Attempt)))
	case errors.Is(err, dynamic.ErrAmbiguous):
		return s.Runtime.EnterUnknown(ctx, lease, effectID, "renew", s.now().Add(dynamicRetryDelay(lease.Attempt)))
	default:
		// A definite provider error (e.g. the role vanished): the renew failed;
		// keep the lease active so a later expiry sweep tidies a missing role.
		return s.Runtime.RecordOutcome(ctx, lease, effectID, "renew", "failure", "active", time.Time{}, time.Time{})
	}
}

// settleDrop performs an idempotent DropRole for revoke and expire.
func (s *Dynamic) settleDrop(ctx context.Context, lease store.ClaimedLease, provider dynamic.Provider, kind, terminal string) error {
	effectID, err := s.Runtime.RecordIntent(ctx, lease, kind)
	if err != nil {
		return err
	}
	opCtx, cancel := context.WithTimeout(ctx, s.leaseDeadline())
	defer cancel()
	err = provider.DropRole(opCtx, lease.ProviderHandle)
	keepState := "revoking"
	if kind == "expire" {
		keepState = "active"
	}
	switch {
	case err == nil:
		return s.Runtime.RecordOutcome(ctx, lease, effectID, kind, "success", terminal, time.Time{}, time.Time{})
	case errors.Is(err, dynamic.ErrUnreachable):
		return s.Runtime.RecordOutcomeRetry(ctx, lease, effectID, kind, keepState, s.now().Add(dynamicRetryDelay(lease.Attempt)))
	default:
		// Ambiguous, or a definite error like dependent objects blocking the
		// drop: enter unknown so reconcile (or an operator) settles it, and the
		// hikyo_dynamic_effects_unknown metric surfaces it.
		return s.Runtime.EnterUnknown(ctx, lease, effectID, kind, s.now().Add(dynamicRetryDelay(lease.Attempt)))
	}
}

// settleMintRecovery cleans up a crashed or ambiguous synchronous mint: the
// generated password was never durably delivered, so any role that exists must
// be dropped and the lease marked failed.
func (s *Dynamic) settleMintRecovery(ctx context.Context, lease store.ClaimedLease, provider dynamic.Provider) error {
	effectID, err := s.Runtime.RecordIntent(ctx, lease, "mint")
	if err != nil {
		return err
	}
	opCtx, cancel := context.WithTimeout(ctx, s.leaseDeadline())
	defer cancel()
	if err := provider.DropRole(opCtx, lease.ProviderHandle); err != nil {
		if errors.Is(err, dynamic.ErrUnreachable) {
			return s.Runtime.RecordOutcomeRetry(ctx, lease, effectID, "mint", "minting", s.now().Add(dynamicRetryDelay(lease.Attempt)))
		}
		return s.Runtime.EnterUnknown(ctx, lease, effectID, "mint", s.now().Add(dynamicRetryDelay(lease.Attempt)))
	}
	return s.Runtime.RecordOutcome(ctx, lease, effectID, "mint", "failure", "failed", time.Time{}, time.Time{})
}

// reauthorizeLease re-checks that the lease's recorded principal still holds the
// full disclosure authority a renew needs — read@env (via Authorize) AND
// current reveal + the machine-reveal opt-in (via leaseDisclosureGate). Renewal
// EXTENDS disclosure-backed access, so a principal whose reveal or opt-in was
// withdrawn cannot keep the credential alive; it expires naturally instead.
func (s *Dynamic) reauthorizeLease(ctx context.Context, lease store.ClaimedLease, op authz.Operation) error {
	return tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		caller := authz.Identity{
			Principal: domain.PrincipalID(lease.PrincipalID),
			Class:     domain.PrincipalClass(lease.PrincipalClass),
		}
		scope := domain.Scope{Org: domain.OrgID(lease.OrgID), Project: domain.ProjectID(lease.ProjectID), Env: domain.EnvID(lease.EnvironmentID)}
		projectScope := domain.Scope{Org: scope.Org, Project: scope.Project}
		if _, err := az.Authorize(ctx, caller, op, scope); err != nil {
			return err
		}
		return s.leaseDisclosureGate(ctx, az, caller, scope, projectScope, s.now(), false)
	})
}
