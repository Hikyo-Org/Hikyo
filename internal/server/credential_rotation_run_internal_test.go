package server

import (
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The run's expiry and its lower-bound flag reach the wire only together: the
// flag is sent when true, omitted when false, and a malformed expiry fails the read.
func TestRotationRunBodyMapsTheExpiry(t *testing.T) {
	base := store.RotationRunRecord{ID: "rrn_1", Trigger: "schedule", Phase: "revoke_refused", StartedAt: "2030-01-02T03:04:05Z", PhaseAt: "2030-01-02T04:04:05Z"}

	got, err := rotationRunBody(base)
	if err != nil || got.CredentialValidUntil != nil || got.CredentialValidUntilLowerBound != nil {
		t.Fatalf("no expiry: %+v, %v", got, err)
	}

	settled := base
	settled.CredentialValidUntil = "2030-02-03T04:05:06Z"
	got, err = rotationRunBody(settled)
	if err != nil || got.CredentialValidUntil == nil || !got.CredentialValidUntil.Equal(time.Date(2030, 2, 3, 4, 5, 6, 0, time.UTC)) || got.CredentialValidUntilLowerBound != nil {
		t.Fatalf("settled expiry: %+v, %v", got, err)
	}

	uncertain := settled
	uncertain.CredentialValidUntilLowerBound = true
	got, err = rotationRunBody(uncertain)
	if err != nil || got.CredentialValidUntilLowerBound == nil || !*got.CredentialValidUntilLowerBound {
		t.Fatalf("uncertain expiry: %+v, %v", got, err)
	}

	broken := settled
	broken.CredentialValidUntil = "not-a-time"
	if _, err := rotationRunBody(broken); err == nil || !strings.Contains(err.Error(), "credential_valid_until") {
		t.Fatalf("malformed expiry = %v", err)
	}
}
