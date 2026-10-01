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

// checkedStoredDEKVersion rejects corrupted storage before narrowing a key version.
func checkedStoredDEKVersion(value int64) (uint32, error) {
	return checkedStoredUint32("dek_version", value)
}

// storedUint32Fits is the common storage bound for key and row versions.
func storedUint32Fits(value int64) bool { return value >= 0 && value <= math.MaxUint32 }

// checkedStoredUint32 preserves the existing field-first corruption diagnostic.
func checkedStoredUint32(field string, value int64) (uint32, error) {
	if !storedUint32Fits(value) {
		return 0, fmt.Errorf("store: %s out of range: %d", field, value)
	}
	return uint32(value), nil
}
