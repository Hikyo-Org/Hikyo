package service

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestValidateProviderOriginRejectsMultiHost(t *testing.T) {
	if err := validateProviderOrigin("postgres://u@db.example:5432/app"); err != nil {
		t.Fatalf("single host rejected: %v", err)
	}
	for _, origin := range []string{
		"postgres://u@h1:5432,h2:5432/db",
		"postgres://u@h1,h2:5432/db",
	} {
		if err := validateProviderOrigin(origin); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("validateProviderOrigin(%q) = %v, want ErrInvalid", origin, err)
		}
	}
}
