package store

import (
	"math"
	"strings"
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

func TestStoredDEKVersionRefusesWrapping(t *testing.T) {
	for _, v := range []int64{0, 1, math.MaxUint32} {
		got, err := checkedStoredDEKVersion(v)
		if err != nil || int64(got) != v {
			t.Fatalf("%d -> %d: %v", v, got, err)
		}
	}
	for _, v := range []int64{-1, math.MaxUint32 + 1, math.MaxInt64} {
		if _, err := checkedStoredDEKVersion(v); err == nil {
			t.Fatalf("corrupt version %d accepted", v)
		}
	}
}

func TestStoredReencryptVersionsRefuseWrapping(t *testing.T) {
	for _, v := range []int64{0, 1, math.MaxUint32} {
		got, err := storedReencryptInstanceRow("row", []byte("ciphertext"), v, v)
		if err != nil || got.ID != "row" || string(got.Ciphertext) != "ciphertext" || int64(got.DEKVersion) != v || int64(got.RowVersion) != v {
			t.Fatalf("valid versions %d: %v", v, err)
		}
	}
	for _, v := range []int64{-1, math.MaxUint32 + 1, math.MaxInt64} {
		if _, err := storedReencryptInstanceRow("row", nil, v, 1); err == nil || !strings.Contains(err.Error(), "dek_version") {
			t.Fatalf("corrupt DEK %d: %v", v, err)
		}
		if _, err := storedReencryptInstanceRow("row", nil, 1, v); err == nil || !strings.Contains(err.Error(), "row_version") {
			t.Fatalf("corrupt row %d: %v", v, err)
		}
	}
}
