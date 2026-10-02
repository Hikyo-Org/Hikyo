package service

import (
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"golang.org/x/time/rate"
)

type machineFetchBucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// chargeMachineFetchOnce runs only after transaction-local authentication and
// authorization. Retry closures share charged, so one fetch spends one token.
func (b *Budget) chargeMachineFetchOnce(charged *bool, principal domain.PrincipalID) error {
	if b == nil || *charged {
		return nil
	}
	if principal == "" {
		return fmt.Errorf("%w: missing machine fetch principal", admission.ErrOverloaded)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.developmentDisabled {
		return nil
	}
	at := b.clock()
	if b.machineFetch == nil {
		b.machineFetch = make(map[domain.PrincipalID]machineFetchBucket)
	}
	bucket, exists := b.machineFetch[principal]
	if !exists {
		// Two idle minutes refill an empty bucket completely. Removing only
		// such buckets cannot forgive an active principal's debt.
		if len(b.machineFetch) >= budgetMaxTrackedSubjects {
			for id, old := range b.machineFetch {
				if at.Sub(old.lastSeen) >= 2*time.Minute {
					delete(b.machineFetch, id)
				}
			}
			if len(b.machineFetch) >= budgetMaxTrackedSubjects {
				return fmt.Errorf("%w: machine fetch tracking saturated", admission.ErrOverloaded)
			}
		}
		bucket.limiter = rate.NewLimiter(rate.Limit(BudgetMachineFetchPrincipalPerMin)/60, BudgetMachineFetchPrincipalBurst)
	} else if at.Before(bucket.lastSeen) {
		at = bucket.lastSeen // Clock rollback must not reset/refill the bucket.
	}
	bucket.lastSeen = at
	b.machineFetch[principal] = bucket
	reservation := bucket.limiter.ReserveN(at, 1)
	if !reservation.OK() {
		return fmt.Errorf("%w: machine fetch reservation refused", admission.ErrOverloaded)
	}
	if wait := reservation.DelayFrom(at); wait > 0 {
		reservation.CancelAt(at)
		return &admission.RateLimitedError{Cause: fmt.Errorf("%w: machine fetch principal rate exhausted", admission.ErrOverloaded), Wait: wait}
	}
	*charged = true
	return nil
}
