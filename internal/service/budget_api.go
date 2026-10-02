package service

import (
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"golang.org/x/time/rate"
)

const (
	BudgetAuthenticatedAPIPerMin = 300
	BudgetAuthenticatedAPIBurst  = 600
)

// AdmitAuthenticatedAPI is called only with a live transaction-authenticated
// session, once per network request. It is independent of expensive-path
// budgets and includes reads and valid-but-forbidden operations.
func (b *Budget) AdmitAuthenticatedAPI(sessionID string) error {
	if b == nil {
		return nil
	}
	if sessionID == "" {
		return fmt.Errorf("%w: missing authenticated session", admission.ErrOverloaded)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.developmentDisabled {
		return nil
	}
	at := b.clock()
	if b.humanAPI == nil {
		b.humanAPI = make(map[string]machineFetchBucket)
	}
	bucket, exists := b.humanAPI[sessionID]
	if !exists {
		if len(b.humanAPI) >= budgetMaxTrackedSubjects {
			for id, old := range b.humanAPI {
				if at.Sub(old.lastSeen) >= 2*time.Minute {
					delete(b.humanAPI, id)
				}
			}
			if len(b.humanAPI) >= budgetMaxTrackedSubjects {
				return fmt.Errorf("%w: authenticated API tracking saturated", admission.ErrOverloaded)
			}
		}
		bucket.limiter = rate.NewLimiter(rate.Limit(BudgetAuthenticatedAPIPerMin)/60, BudgetAuthenticatedAPIBurst)
	} else if at.Before(bucket.lastSeen) {
		at = bucket.lastSeen
	}
	bucket.lastSeen = at
	b.humanAPI[sessionID] = bucket
	reservation := bucket.limiter.ReserveN(at, 1)
	if !reservation.OK() {
		return fmt.Errorf("%w: authenticated API reservation refused", admission.ErrOverloaded)
	}
	if wait := reservation.DelayFrom(at); wait > 0 {
		reservation.CancelAt(at)
		return &admission.RateLimitedError{Cause: fmt.Errorf("%w: authenticated API rate exhausted", admission.ErrOverloaded), Wait: wait}
	}
	return nil
}
