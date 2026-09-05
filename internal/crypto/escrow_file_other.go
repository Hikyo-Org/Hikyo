//go:build !linux && !darwin

package crypto

import "errors"

func ReadEscrowRootKey(string, string) ([]byte, error) {
	return nil, errors.New("local escrow verification requires a supported Unix custody host")
}
