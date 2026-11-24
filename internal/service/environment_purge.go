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
	ErrEnvironmentHasLiveCertificate = fmt.Errorf("%w: the environment still has a live certificate", domain.ErrPKICertificateRetention)
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

// liveRowsRefusalsError reports every blocking kind of one environment delete,
// so a caller or test sees the full set: errors.Is matches each kind and the
// detail lists every kind's ids.
type liveRowsRefusalsError struct{ errs []error }

func (e *liveRowsRefusalsError) Error() string { return "service: " + e.SafeDetail() }
func (e *liveRowsRefusalsError) SafeDetail() string {
	parts := make([]string, len(e.errs))
	for i, err := range e.errs {
		var d interface{ SafeDetail() string }
		if errors.As(err, &d) {
			parts[i] = d.SafeDetail()
		} else {
			parts[i] = "the environment still has a live SSH CA; delete it first"
		}
	}
	return strings.Join(parts, "; ")
}
func (e *liveRowsRefusalsError) Unwrap() []error { return e.errs }

// purgeEnvironmentLiveRows removes a deleted environment's terminal dynamic
// leases, terminal certificate history and SSH rows in the delete's own
// transaction. It refuses by name while a lease or live certificate, an adapter
// target or configure fence, or a live SSH CA remains, reporting every blocking
// kind together (#1420); the caller's transaction rolls the partial purge back.
// It returns the certificates purged.
func purgeEnvironmentLiveRows(ctx context.Context, r store.Repos, proof authz.Proof) (int, error) {
	var refusals []error
	err := r.Dynamic().PurgeEnvironment(ctx, proof)
	if refusal := liveRowsRefusal(err, ErrEnvironmentHasLiveLease, store.ErrDynamicLiveLease,
		"the environment still has a live dynamic lease; revoke it and let it settle first; leases not yet terminal: "); refusal != nil {
		refusals = append(refusals, refusal)
	} else if err != nil {
		return 0, err
	}
	purged, err := r.PKI().PurgeEnvironment(ctx, proof, time.Now().UTC())
	if refusal := liveRowsRefusal(err, ErrEnvironmentHasLiveCertificate, store.ErrPKILiveCertificate,
		"the environment still has a live certificate; revoke it and let it expire first; certificates still live: "); refusal != nil {
		refusals = append(refusals, refusal)
	} else if err != nil {
		return 0, err
	}
	err = r.Adapters().GuardEnvironmentDelete(ctx, proof)
	if refusal := liveRowsRefusal(err, ErrEnvironmentHasAdapterTargets, store.ErrAdapterTargetsInEnvironment,
		"the environment still has adapter targets (live or tombstoned) or configure fences, whose delivery records are retained; ids: "); refusal != nil {
		refusals = append(refusals, refusal)
	} else if err != nil {
		return 0, err
	}
	if err := r.SSH().PurgeEnvironment(ctx, proof); errors.Is(err, store.ErrSSHLiveCA) {
		refusals = append(refusals, err)
	} else if err != nil {
		return 0, err
	}
	switch len(refusals) {
	case 0:
		return purged, nil
	case 1:
		return 0, refusals[0]
	}
	return 0, &liveRowsRefusalsError{errs: refusals}
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
