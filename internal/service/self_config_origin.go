package service

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Only the runtime worker derives this metadata from authenticated snapshots.
// Apply reads it atomically inside its final transaction, never acquiring the
// worker mutex or minting runtime payload authority on the request stack.
type selfConfigOriginReview struct {
	owner, incarnation, oldSnapshot, candidateSnapshot string
	generation, candidateRevision                      int64
	oldOrigin, candidateOrigin                         string
}

type selfConfigRepairOrigin struct {
	owner, incarnation, origin string
}

// RecordRepairOrigin records the RP of a validated, app-owned repair graph for
// an unconfigured joining node. It grants no active bundle or acknowledgement.
// A network caller cannot replace this evidence with a claimed request origin.
func (s *SelfConfig) RecordRepairOrigin(ctx context.Context, origin string) error {
	if operation.IsNetwork(ctx) {
		return domain.ErrUnauthorized
	}
	if _, ok := selfConfigRPHost(origin); !ok {
		return domain.ErrInvalid
	}
	owner, incarnation, err := s.DB.RecoveryIdentity()
	if err != nil {
		return err
	}
	s.repairOrigin.Store(&selfConfigRepairOrigin{owner: owner, incarnation: incarnation, origin: origin})
	return nil
}

func (s *SelfConfig) prepareOriginReview(ctx context.Context, binding store.SelfConfigBinding, job store.SelfConfigJob, candidate *runtimeconfig.Bundle) error {
	active := s.installed.Load()
	var previous *runtimeconfig.Bundle
	var oldOrigin string
	if active != nil && active.owner == binding.OwnerInstanceID && active.incarnation == binding.Incarnation {
		// A failed installation can leave the usable graph behind the durable
		// target. Retrying that target still retires this graph's RP hostname.
		previous = active.bundle
		oldOrigin = previous.OwnerValues()["HIKYO_EXTERNAL_ORIGIN"]
	} else if repair := s.repairOrigin.Load(); repair != nil && repair.owner == binding.OwnerInstanceID && repair.incarnation == binding.Incarnation {
		oldOrigin = repair.origin
	} else {
		// A restored/suspended instance keeps no active outbound bundle. Read
		// its retained desired policy here so recovery does not depend on
		// stale process environment or on resuming outgoing work first.
		var err error
		previous, err = s.prepareRuntimeSnapshot(ctx, binding, binding.DesiredSnapshotID, binding.DesiredRevision)
		if err != nil {
			return err
		}
		oldOrigin = previous.OwnerValues()["HIKYO_EXTERNAL_ORIGIN"]
	}
	s.originReview.Store(&selfConfigOriginReview{owner: binding.OwnerInstanceID, incarnation: binding.Incarnation,
		oldSnapshot: binding.DesiredSnapshotID, generation: binding.Generation, candidateSnapshot: job.SnapshotID, candidateRevision: job.Revision,
		oldOrigin: oldOrigin, candidateOrigin: candidate.OwnerValues()["HIKYO_EXTERNAL_ORIGIN"]})
	return nil
}

// requireOriginRecovery protects against retiring the RP hostname that makes
// the initiating administrator's passkeys usable. It proves an independent
// local credential path exists, not that DNS, TLS or the new URL is reachable.
// Existing workspace handoff authenticates at an existing owner origin and
// cannot establish that a not-yet-active target origin is reachable.
func (s *SelfConfig) requireOriginRecovery(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, binding store.SelfConfigBinding, job store.SelfConfigJob, intent ReauthIntent, now time.Time) error {
	review := s.originReview.Load()
	if review == nil || review.owner != binding.OwnerInstanceID || review.incarnation != binding.Incarnation || review.oldSnapshot != binding.DesiredSnapshotID || review.generation != binding.Generation || review.candidateSnapshot != job.SnapshotID || review.candidateRevision != job.Revision {
		return domain.ErrConflict
	}
	// An unchanged absent value means this revision leaves node-derived origin
	// selection untouched. A change from or to an unknown origin must not be
	// guessed from a request Host header or current process environment.
	if review.oldOrigin == review.candidateOrigin {
		return nil
	}
	oldHost, oldOK := selfConfigRPHost(review.oldOrigin)
	newHost, newOK := selfConfigRPHost(review.candidateOrigin)
	if !oldOK || !newOK {
		return invalidDetail("Origin changes require explicit current and target origins before applying")
	}
	if oldHost == newHost {
		return nil
	}
	window, err := az.ReauthWindowFor(ctx, caller.SessionID, intent.environmentID)
	if err != nil {
		return err
	}
	if window.FactorClass != "totp" {
		return invalidDetail("Changing the passkey hostname requires fresh TOTP reauthentication and a current local password credential")
	}
	if err := validateSelfConfigFactor(window, now); err != nil {
		return err
	}
	account, err := az.AccountByPrincipal(ctx, caller.Principal)
	if err != nil {
		return err
	}
	epoch, err := az.CredentialEpoch(ctx)
	if err != nil {
		return err
	}
	password, err := az.PasswordCredentialFor(ctx, account.ID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if err != nil || password.CredentialEpoch != epoch || len(password.Verifier) == 0 {
		return invalidDetail("Changing the passkey hostname requires a current local password credential on the applying account")
	}
	factor, err := az.ConfirmedTOTP(ctx, account.ID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if err != nil || !factor.Confirmed || factor.CredentialEpoch != epoch || len(factor.Seed) == 0 {
		return invalidDetail("Changing the passkey hostname requires a current confirmed TOTP factor on the applying account")
	}
	// The normal consumer immediately following this guard still checks the
	// exact owner/revision/schema/generation binding, epoch and single-use CAS.
	return nil
}

func selfConfigRPHost(origin string) (string, bool) {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	return strings.ToLower(u.Hostname()), true
}
