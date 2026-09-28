//go:build !linux

package filesync

import "fmt"

// isTmpfs cannot be answered off Linux: the Compose helper reports true there
// so its doctor check stays quiet, but require_tmpfs is a refusal, and a
// refusal that cannot be verified must fail closed.
func isTmpfs(string) (bool, error) {
	return false, fmt.Errorf("%w: require_tmpfs is verifiable on Linux only", ErrUnsupported)
}
