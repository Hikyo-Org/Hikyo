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
)

// environmentLiveError is a named environment-delete refusal that tells the
// caller which rows block it. Ids are not secret.
type environmentLiveError struct {
	base   error
	detail string
}

func (e *environmentLiveError) Error() string      { return "service: " + e.detail }
func (e *environmentLiveError) SafeDetail() string { return e.detail }
func (e *environmentLiveError) Unwrap() error      { return e.base }

// purgeEnvironmentLiveRows removes a deleted environment's terminal dynamic
// leases and expired certificates in the delete's own transaction, refusing by
// name while any is live (#1420).
func purgeEnvironmentLiveRows(ctx context.Context, r store.Repos, proof authz.Proof) error {
	err := r.Dynamic().PurgeEnvironment(ctx, proof)
	if refusal := liveRowsRefusal(err, ErrEnvironmentHasLiveLease, store.ErrDynamicLiveLease,
		"the environment still has a live dynamic lease; revoke it and let it settle first; leases not yet terminal: "); refusal != nil {
		return refusal
	}
	if err != nil {
		return err
	}
	err = r.PKI().PurgeEnvironmentCertificates(ctx, proof, time.Now().UTC())
	if refusal := liveRowsRefusal(err, ErrEnvironmentHasLiveCertificate, store.ErrPKILiveCertificate,
		"the environment still has a live certificate; revoke it and let it expire first; certificates still live: "); refusal != nil {
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
	return &environmentLiveError{base: named, detail: detail}
}
