package server

import (
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

const maxCredentialLifetimeSeconds int64 = 10 * 365 * 24 * 60 * 60

func credentialLifetime(seconds int64) (time.Duration, error) {
	if seconds < 1 || seconds > maxCredentialLifetimeSeconds {
		return 0, fmt.Errorf("%w: credential lifetime must be within 1 second and 10 years", domain.ErrInvalid)
	}
	return time.Duration(seconds) * time.Second, nil
}
