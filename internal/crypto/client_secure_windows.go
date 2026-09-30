//go:build !unix

package crypto

import "errors"

// Non-Unix clients fail closed until this implementation can enforce an
// owner-only ACL, reject reparse points, and validate opened-handle identity.
// Ordinary mode bits do not provide those custody guarantees on Windows.
func loadOrCreateMasterKey(string) ([]byte, error) {
	return nil, errors.New("crypto: local snapshot key custody is unavailable on this platform")
}
