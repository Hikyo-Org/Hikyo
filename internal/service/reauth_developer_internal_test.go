package service

import (
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestDeveloperCredentialReauthRequiresConcretePositiveLifetime(t *testing.T) {
	for _, ttl := range []time.Duration{0, -time.Second, time.Nanosecond, 8*time.Hour + time.Second} {
		if _, err := NewPersonalDeveloperCredentialReauthIntent("env_consent", nil, ttl, true, false); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("lifetime %s: %v, want invalid", ttl, err)
		}
	}
	first, err := NewPersonalDeveloperCredentialReauthIntent("env_consent", nil, time.Hour, true, false)
	if err != nil {
		t.Fatal(err)
	}
	longer, err := NewPersonalDeveloperCredentialReauthIntent("env_consent", nil, 2*time.Hour, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.developerBinding == longer.developerBinding || first.keySet == longer.keySet {
		t.Fatal("different confirmed lifetimes share a reauthentication binding")
	}
}
