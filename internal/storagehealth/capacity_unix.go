//go:build linux || darwin

package storagehealth

import (
	"errors"
	"math"

	"golang.org/x/sys/unix"
)

func Read(path string) (Capacity, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return Capacity{}, err
	}
	if stat.Bsize <= 0 || stat.Blocks > math.MaxUint64/uint64(stat.Bsize) || stat.Bavail > stat.Blocks {
		return Capacity{}, errors.New("invalid filesystem capacity")
	}
	return Capacity{TotalBytes: stat.Blocks * uint64(stat.Bsize), AvailableBytes: stat.Bavail * uint64(stat.Bsize)}, nil
}
