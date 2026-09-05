//go:build !linux && !darwin

package devupgrade

import (
	"context"
	"errors"
)

// Open refuses platforms without the implemented ownership, descriptor-relative
// nofollow, exclusive publication and durable directory contract.
func Open(context.Context, string) (Material, error) {
	return Material{}, errors.New("local development upgrade custody requires Linux or macOS filesystem protections")
}
