//go:build !darwin && !linux

package selfupdate

import (
	"errors"
	"os"
)

func openNightlyFile(_ *os.Root, _ string) (*os.File, error) {
	return nil, errors.New("offline bundle assembly requires Linux or macOS filesystem semantics")
}

func publishNightlyDirectory(_, _ string) error {
	return errors.New("offline bundle assembly requires atomic no-replace rename")
}
