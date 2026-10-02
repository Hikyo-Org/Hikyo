//go:build !windows

package securefile

import "os"

func createAtomicTemp(dir string, _ os.FileMode) (*os.File, error) {
	return os.CreateTemp(dir, ".secure-write-*")
}
