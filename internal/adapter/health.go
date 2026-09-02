package adapter

import "errors"

// TargetHealth is the closed operator-facing status of one sync target (#157).
// It is derived from the stored sync outcome, the pause flag, and the state of
// the target's active outbox job; it is never stored, so the stored outcome
// stays truthful across pause and resume.
type TargetHealth string

const (
	HealthNever      TargetHealth = "never"
	HealthPending    TargetHealth = "pending"
	HealthConverging TargetHealth = "converging"
	HealthConverged  TargetHealth = "converged"
	HealthDegraded   TargetHealth = "degraded"
	HealthFailed     TargetHealth = "failed"
	HealthPaused     TargetHealth = "paused"
)

// DeriveHealth maps the stored sync_status, the pause flag, whether a prior
// converge ever succeeded, and the active job's outbox state onto TargetHealth.
//
//   - paused wins over everything: no push can happen while the flag is set.
//   - converging with a queued (unclaimed) job is pending; a running job is
//     converging.
//   - failed with a prior converged revision is degraded: the destination
//     still holds an older revision rather than nothing.
func DeriveHealth(syncStatus string, paused, hasConverged bool, jobState string) TargetHealth {
	if paused {
		return HealthPaused
	}
	switch syncStatus {
	case "converging":
		if jobState == "queued" {
			return HealthPending
		}
		return HealthConverging
	case "converged":
		return HealthConverged
	case "failed":
		if hasConverged {
			return HealthDegraded
		}
		return HealthFailed
	default:
		return HealthNever
	}
}

// ErrorClass is the closed, bounded cause recorded on a target after a failed
// attempt. It never carries provider response bodies or names.
type ErrorClass string

const (
	ErrorClassAuth              ErrorClass = "auth"
	ErrorClassNetwork           ErrorClass = "network"
	ErrorClassConflict          ErrorClass = "conflict"
	ErrorClassProviderLimit     ErrorClass = "provider_limit"
	ErrorClassProviderAmbiguous ErrorClass = "provider_ambiguous"
	ErrorClassRefused           ErrorClass = "refused"
)

// ClassifyError folds any attempt error into the closed ErrorClass set. Errors
// the seam does not name are transport failures and classify as network.
func ClassifyError(err error) ErrorClass {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrProviderAuth):
		return ErrorClassAuth
	case errors.Is(err, ErrConflict):
		return ErrorClassConflict
	case errors.Is(err, ErrRateLimited), errors.Is(err, ErrQueueFull), errors.Is(err, ErrLedgerFull):
		return ErrorClassProviderLimit
	case errors.Is(err, ErrIndeterminate), errors.Is(err, ErrDestinationID), errors.Is(err, ErrVersionFloor):
		return ErrorClassProviderAmbiguous
	case errors.Is(err, ErrUnauthorized), errors.Is(err, ErrSuperseded):
		return ErrorClassRefused
	}
	if _, ok := ProviderRetryAt(err); ok {
		return ErrorClassProviderLimit
	}
	return ErrorClassNetwork
}

// NeedsAttention reports whether an error class means the destination now
// disagrees with Hikyo's ownership record in a way only an operator can settle:
// an unowned name in the way, or a destination whose identity moved.
func (c ErrorClass) NeedsAttention() bool {
	return c == ErrorClassConflict || c == ErrorClassProviderAmbiguous
}
