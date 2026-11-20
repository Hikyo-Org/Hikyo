//go:build darwin

package shell

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// setQuarantine marks a saved file as downloaded, so Gatekeeper and the
// system treat it like a browser download (ADR D11).
func setQuarantine(path string) error {
	value := fmt.Sprintf("0083;%x;Hikyo;", time.Now().Unix())
	return unix.Setxattr(path, "com.apple.quarantine", []byte(value), 0)
}

// Quarantine marks a file a native download saved, like setQuarantine does for
// the ones the shell writes itself.
func Quarantine(path string) error { return setQuarantine(path) }
