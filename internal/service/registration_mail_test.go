package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The registration mailer predicate (#606, mailer-seam 7.3): only a known
// fenced configuration reads "unconfigured"; any other capture failure is a
// fault that must reach the caller, never a silent false.
func TestRegistrationMailPredicate(t *testing.T) {
	configured, err := runtimeconfig.Prepare(map[string]string{
		"HIKYO_MAIL_ADDR": "relay.invalid:465", "HIKYO_MAIL_TLS": "implicit", "HIKYO_MAIL_FROM": "hikyo@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	unconfigured, err := runtimeconfig.Prepare(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	fault := errors.New("metadata read failed")
	for _, tc := range []struct {
		name    string
		bundle  *runtimeconfig.Bundle
		capture error
		want    bool
		wantErr error
	}{
		{name: "configured", bundle: configured, want: true},
		{name: "unconfigured", bundle: unconfigured, want: false},
		{name: "fenced", capture: ErrSelfConfigFenced, want: false},
		{name: "not ready", capture: ErrSelfConfigUnavailable, wantErr: ErrSelfConfigUnavailable},
		{name: "fault", capture: fault, wantErr: fault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			predicate := mailPredicate(func(context.Context) (*runtimeconfig.Bundle, error) {
				return tc.bundle, tc.capture
			})
			got, err := predicate(t.Context())
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("predicate error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("predicate = %t, %v; want %t", got, err, tc.want)
			}
		})
	}
	if _, err := SelfConfigMailConfigured(nil); err == nil {
		t.Fatal("a nil runtime configuration built a predicate")
	}
	if _, err := NewRegistration(RegistrationConfig{DB: nil}); err == nil {
		t.Fatal("a registration without a datastore was built")
	}
	if _, err := NewRegistration(RegistrationConfig{DB: &store.DB{}}); err == nil {
		t.Fatal("a registration without a mailer predicate was built")
	}
}
