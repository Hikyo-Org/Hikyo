package dynamic

import (
	"math"
	"testing"
	"time"
)

func TestTTLDuration(t *testing.T) {
	for _, seconds := range []int64{math.MinInt64, -1, 0, MaxTTLSeconds + 1, math.MaxInt64} {
		if _, err := TTLDuration(seconds); err == nil {
			t.Errorf("TTLDuration(%d) succeeded", seconds)
		}
	}
	for _, seconds := range []int64{1, 3600, MaxTTLSeconds} {
		got, err := TTLDuration(seconds)
		if err != nil || got <= 0 || int64(got/time.Second) != seconds {
			t.Errorf("TTLDuration(%d) = %v, %v", seconds, got, err)
		}
	}
}
