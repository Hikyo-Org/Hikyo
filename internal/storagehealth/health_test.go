package storagehealth

import "testing"

func TestCapacityIsNotFabricated(t *testing.T) {
	for _, c := range []Capacity{{}, {TotalBytes: 10, AvailableBytes: 11}} {
		if FromCapacity(c).Known {
			t.Fatal("invalid capacity became known")
		}
	}
	for _, free := range []uint64{21, 20, 10, 0} {
		h := FromCapacity(Capacity{100, free})
		if !h.Known || h.UsedPercent != float64(100-free) {
			t.Fatalf("wrong utilization: %+v", h)
		}
	}
}
