package store

import (
	"math"
	"testing"
)

func TestRestoreSequencePositionsReserveCapacity(t *testing.T) {
	for _, tt := range []struct {
		bound pgSequenceBound
		value int64
		valid bool
	}{
		{pgSequenceBound{1, math.MaxInt64}, 1, true},
		{pgSequenceBound{1, math.MaxInt64}, 1000000000, true},
		{pgSequenceBound{1, math.MaxInt64}, math.MaxInt64, false},
		{pgSequenceBound{1, math.MaxInt64}, math.MaxInt64 - 1, false},
		{pgSequenceBound{1, math.MaxInt64}, math.MaxInt64/2 + 1, true},
		{pgSequenceBound{1, math.MaxInt64}, math.MaxInt64/2 + 2, false},
		{pgSequenceBound{2, 42}, 22, true},
		{pgSequenceBound{2, 42}, 23, false},
		{pgSequenceBound{math.MinInt64, math.MaxInt64}, -1, true},
		{pgSequenceBound{math.MinInt64, math.MaxInt64}, 0, false},
		{pgSequenceBound{2, 42}, 1, false},
	} {
		if got := validRestoredSequencePosition(tt.bound, tt.value); got != tt.valid {
			t.Fatalf("range=%+v value=%d valid=%v; want %v", tt.bound, tt.value, got, tt.valid)
		}
	}
}
