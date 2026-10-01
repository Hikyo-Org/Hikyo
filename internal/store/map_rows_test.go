package store

import (
	"errors"
	"slices"
	"testing"
)

func TestMapRowsPreservesNilAndDiscardsPartialResults(t *testing.T) {
	invalid := errors.New("invalid stored row")
	convert := func(value int) (int, error) {
		if value < 0 {
			return 0, invalid
		}
		return value * 2, nil
	}
	for _, empty := range [][]int{nil, {}} {
		got, err := mapRows(empty, convert)
		if got != nil || err != nil {
			t.Fatalf("empty query changed its nil result: %v, %v", got, err)
		}
	}
	got, err := mapRows([]int{3, 1, 2}, convert)
	if err != nil || !slices.Equal(got, []int{6, 2, 4}) {
		t.Fatalf("query order changed: %v, %v", got, err)
	}
	got, err = mapRows([]int{1, -1, 2}, convert)
	if got != nil || !errors.Is(err, invalid) {
		t.Fatalf("partial invalid query escaped: %v, %v", got, err)
	}
}
