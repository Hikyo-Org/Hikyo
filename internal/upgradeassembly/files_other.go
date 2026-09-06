//go:build !darwin && !linux

package upgradeassembly

import (
	"errors"
	"os"
)

func openDocument(_ *os.Root, _ string) (*os.File, error) {
	return nil, errors.New("offline bundle assembly requires Linux or macOS filesystem semantics")
}

func publishDirectory(_, _ string) error {
	return errors.New("offline bundle assembly requires atomic no-replace rename")
}
