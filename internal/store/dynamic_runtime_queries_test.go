package store

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
)

func TestPostgresDynamicClaimRefusesAttemptOverflowBeforeWrite(t *testing.T) {
	// A nil generated DB makes any attempted write fail the test. The old
	// unchecked conversion could wrap an exhausted attempt back into range.
	q := pgDynamicRuntimeQueries{queries: pggen.New(nil)}
	for _, attempt := range []int{math.MaxInt32 + 1, math.MinInt32 - 1} {
		err := q.dynamicClaimLease(context.Background(), ClaimedLease{Attempt: attempt}, time.Now())
		if err == nil || !strings.Contains(err.Error(), "PostgreSQL integer out of range") {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
}
