package bench

import "testing"

// TestSelectPassPicksTheWholePassWithTheLowestP99 pins the estimator the
// artifact names: one whole pass is reported, never a p50 from one pass with a
// p99 from another, and a tie keeps the earliest pass.
func TestSelectPassPicksTheWholePassWithTheLowestP99(t *testing.T) {
	passes := []Pass{{P50Millis: 1.0, P99Millis: 5.2}, {P50Millis: 1.4, P99Millis: 2.8}, {P50Millis: 0.9, P99Millis: 2.8}}
	if got := SelectPass(passes); got != 1 {
		t.Fatalf("SelectPass = %d, want 1 (lowest p99, earliest tie)", got)
	}
	if got := SelectPass(nil); got != -1 {
		t.Fatalf("SelectPass(nil) = %d, want -1", got)
	}
}

func TestPercentileNearestRank(t *testing.T) {
	if got := Percentile([]float64{3, 1, 2}, 50); got != 2 {
		t.Fatalf("p50 = %v, want 2", got)
	}
	if got := Percentile(nil, 99); got != 0 {
		t.Fatalf("empty p99 = %v, want 0", got)
	}
}
