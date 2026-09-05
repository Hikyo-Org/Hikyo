//go:build !linux && !darwin

package storagehealth

import "errors"

func Read(string) (Capacity, error) {
	return Capacity{}, errors.New("filesystem capacity unavailable on this platform")
}
