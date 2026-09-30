package store

import (
	"fmt"
	"math"
)

// checkedPGInt32 preserves the PostgreSQL driver's refusal of values outside
// INTEGER storage, before a generated parameter can narrow and wrap them.
func checkedPGInt32(value int64) (int32, error) {
	if value < math.MinInt32 || value > math.MaxInt32 {
		return 0, fmt.Errorf("store: PostgreSQL integer out of range: %d", value)
	}
	return int32(value), nil
}
