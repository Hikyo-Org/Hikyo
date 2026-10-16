//go:build !darwin && !linux && !windows

package cli

import "fmt"

func loginSocketOptions(uintptr) error {
	return fmt.Errorf("strict loopback socket binding is unavailable on this platform; use --device")
}
