//go:build windows

package upgradebundle

import (
	"errors"
	"os"
)

func openDocument(_ *os.Root, _ string) (*os.File, error) {
	return nil, errors.New("offline upgrade custody requires a supported Unix host")
}

func openDirectory(_ *os.Root, _ string) (*os.File, error) {
	return nil, errors.New("offline upgrade custody requires a supported Unix host")
}
