package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

var (
	// ErrEnvironmentHasLiveLease refuses an environment delete while a dynamic
	// lease in it is not revoked, expired or failed: deleting the environment
	// would orphan a provider role nothing would revoke. The refusal names the
	// leases.
	ErrEnvironmentHasLiveLease = fmt.Errorf("%w: the environment still has a live dynamic lease", domain.ErrConflict)
	// ErrEnvironmentHasLiveCertificate refuses an environment delete while a
	// PKI certificate in it is still live (unexpired and not failed or expired):
	// its row backs the CRL entry of a revoked serial. The refusal names them.
	ErrEnvironmentHasLiveCertificate = fmt.Errorf("%w: the environment still has a live certificate", domain.ErrConflict)
	// ErrEnvironmentHasCertificateHistory refuses an environment delete while it
	// holds only terminal certificate rows: issuance history is retained (#825).
	ErrEnvironmentHasCertificateHistory = fmt.Errorf("%w: the environment still has certificate issuance history", domain.ErrConflict)
	// ErrEnvironmentHasAdapterTargets refuses an environment delete while it has
	// an adapter target, live or tombstoned, or a configure fence: targets are
	// never deleted and their delivery records are retained.
	ErrEnvironmentHasAdapterTargets = fmt.Errorf("%w: the environment still has adapter targets", domain.ErrConflict)
	// ErrProjectHasActiveProvider refuses a project delete while an active
	// dynamic provider exists.
	ErrProjectHasActiveProvider = fmt.Errorf("%w: the project still has an active dynamic provider", domain.ErrConflict)
)

// liveRowsRefusalError is a named environment-delete refusal that tells the
// caller which rows block it. Ids are not secret.
type liveRowsRefusalError struct {
	base   error
	detail string
}

func (e *liveRowsRefusalError) Error() string      { return "service: " + e.detail }
func (e *liveRowsRefusalError) SafeDetail() string { return e.detail }
func (e *liveRowsRefusalError) Unwrap() error      { return e.base }

// purgeEnvironmentLiveRows removes a deleted environment's terminal dynamic
// leases in the delete's own transaction and refuses by name while a lease or
// certificate row, or an adapter target, remains (#1420).
func purgeEnvironmentLiveRows(ctx context.Context, r store.Repos, proof authz.Proof) error {
	err := r.Dynamic().PurgeEnvironment(ctx, proof)
	if refusal := liveRowsRefusal(err, ErrEnvironmentHasLiveLease, store.ErrDynamicLiveLease,
		"the environment still has a live dynamic lease; revoke it and let it settle first; leases not yet terminal: "); refusal != nil {
		return refusal
	}
	if err != nil {
		return err
	}
	err = r.PKI().GuardEnvironmentDelete(ctx, proof, time.Now().UTC())
	if refusal := liveRowsRefusal(err, ErrEnvironmentHasLiveCertificate, store.ErrPKILiveCertificate,
		"the environment still has a live certificate; revoke it and let it expire first; certificates still live: "); refusal != nil {
		return refusal
	}
	if refusal := liveRowsRefusal(err, ErrEnvironmentHasCertificateHistory, store.ErrPKICertificateHistory,
		"the environment still has certificate issuance history, which is retained and not purged; certificates: "); refusal != nil {
		return refusal
	}
	if err != nil {
		return err
	}
	err = r.Adapters().GuardEnvironmentDelete(ctx, proof)
	if refusal := liveRowsRefusal(err, ErrEnvironmentHasAdapterTargets, store.ErrAdapterTargetsInEnvironment,
		"the environment still has adapter targets (live or tombstoned) or configure fences, whose delivery records are retained; ids: "); refusal != nil {
		return refusal
	}
	return err
}

// purgeProjectProviders removes a deleted project's tombstoned dynamic
// providers in the delete's own transaction, refusing by name while one is
// active (#1420).
func purgeProjectProviders(ctx context.Context, r store.Repos, proof authz.Proof) error {
	err := r.Dynamic().PurgeProjectProviders(ctx, proof)
	if refusal := liveRowsRefusal(err, ErrProjectHasActiveProvider, store.ErrDynamicActiveProvider,
		"the project still has an active dynamic provider; delete it first; providers: "); refusal != nil {
		return refusal
	}
	return err
}

func liveRowsRefusal(err, named, stored error, prefix string) error {
	var live *store.LiveRowsError
	if !errors.As(err, &live) || !errors.Is(err, stored) {
		return nil
	}
	detail := prefix + strings.Join(live.IDs, ", ")
	if live.Truncated {
		detail += fmt.Sprintf(" (first %d shown)", store.LiveRowsLimit)
	}
	return &liveRowsRefusalError{base: named, detail: detail}
}
