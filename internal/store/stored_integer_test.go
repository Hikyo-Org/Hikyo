package store

import (
	"math"
	"testing"
)

func TestCheckedPGInt32KeepsBoundariesAndRejectsWrapping(t *testing.T) {
	for _, value := range []int64{math.MinInt32, 0, math.MaxInt32} {
		got, err := checkedPGInt32(value)
		if err != nil || int64(got) != value {
			t.Fatalf("%d -> %d, %v", value, got, err)
		}
	}
	for _, value := range []int64{math.MinInt32 - 1, math.MaxInt32 + 1, 1<<32 + 1, math.MaxInt64} {
		if _, err := checkedPGInt32(value); err == nil {
			t.Fatalf("out-of-range %d accepted", value)
		}
	}
}
