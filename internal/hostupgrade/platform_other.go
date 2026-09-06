//go:build !linux

package hostupgrade

import (
	"context"
	"errors"
	"os"
)

func fileOwner(os.FileInfo) int { return -1 }
func runCommand(context.Context, command) ([]byte, error) {
	return nil, errors.New("automatic host upgrades require Linux systemd")
}
