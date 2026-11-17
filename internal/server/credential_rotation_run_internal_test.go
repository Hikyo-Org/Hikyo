package server

import (
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
)

// The run's expiry and its lower-bound flag reach the wire only together: the
// flag is sent when true and omitted when false, and neither without an expiry.
func TestRotationRunBodyMapsTheExpiry(t *testing.T) {
	base := service.RotationRunView{ID: "rrn_1", Trigger: "schedule", Phase: "revoke_refused"}

	got := rotationRunBody(base)
	if got.CredentialValidUntil != nil || got.CredentialValidUntilLowerBound != nil {
		t.Fatalf("no expiry: %+v", got)
	}

	settled := base
	settled.CredentialValidUntil = time.Date(2030, 2, 3, 4, 5, 6, 0, time.UTC)
	got = rotationRunBody(settled)
	if got.CredentialValidUntil == nil || !got.CredentialValidUntil.Equal(settled.CredentialValidUntil) || got.CredentialValidUntilLowerBound != nil {
		t.Fatalf("settled expiry: %+v", got)
	}

	uncertain := settled
	uncertain.CredentialValidUntilLowerBound = true
	got = rotationRunBody(uncertain)
	if got.CredentialValidUntilLowerBound == nil || !*got.CredentialValidUntilLowerBound {
		t.Fatalf("uncertain expiry: %+v", got)
	}
}
