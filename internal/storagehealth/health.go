// Package storagehealth measures the filesystem containing a local datastore.
// It never substitutes the application filesystem for a remote database volume.
package storagehealth

import "math"

type Capacity struct {
	TotalBytes     uint64
	AvailableBytes uint64
}

type Health struct {
	Known          bool
	TotalBytes     uint64
	AvailableBytes uint64
	UsedPercent    float64
}

func FromCapacity(c Capacity) Health {
	if c.TotalBytes == 0 || c.AvailableBytes > c.TotalBytes {
		return Health{}
	}
	used := float64(c.TotalBytes-c.AvailableBytes) / float64(c.TotalBytes) * 100
	if math.IsNaN(used) || math.IsInf(used, 0) {
		return Health{}
	}
	return Health{Known: true, TotalBytes: c.TotalBytes, AvailableBytes: c.AvailableBytes, UsedPercent: used}
}
