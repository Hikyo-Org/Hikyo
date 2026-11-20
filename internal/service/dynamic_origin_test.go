package service

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestValidateProviderOriginRejectsMultiHost(t *testing.T) {
	for _, origin := range []string{"postgres://u@db.example:5432/app", "postgres://u@[::1]:5432/app"} {
		if err := validateProviderOrigin(origin); err != nil {
			t.Fatalf("single host %q rejected: %v", origin, err)
		}
	}
	for _, origin := range []string{
		"postgres://u@h1:5432,h2:5432/db",
		"postgres://u@h1,h2:5432/db",
		"postgres://u@h1:5432:5433/db",
	} {
		if err := validateProviderOrigin(origin); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("validateProviderOrigin(%q) = %v, want ErrInvalid", origin, err)
		}
	}
}
