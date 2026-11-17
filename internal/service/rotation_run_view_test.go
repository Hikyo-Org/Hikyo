package service

import (
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

// A stored run's stamps parse into the view, the expiry and its lower-bound flag
// travel together, and a malformed stamp fails the read by name.
func TestRotationRunViewParsesTheStamps(t *testing.T) {
	base := store.RotationRunRecord{ID: "rrn_1", Trigger: "schedule", Phase: "revoke_refused", StartedAt: "2030-01-02T03:04:05Z", PhaseAt: "2030-01-02T04:04:05Z", CredentialValidUntilLowerBound: true}

	got, err := rotationRunView(base)
	if err != nil || !got.CredentialValidUntil.IsZero() || got.CredentialValidUntilLowerBound {
		t.Fatalf("no expiry (the flag never travels alone): %+v, %v", got, err)
	}
	if !got.StartedAt.Equal(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)) || !got.PhaseAt.Equal(time.Date(2030, 1, 2, 4, 4, 5, 0, time.UTC)) {
		t.Fatalf("stamps = %v, %v", got.StartedAt, got.PhaseAt)
	}

	withExpiry := base
	withExpiry.CredentialValidUntil = "2030-02-03T04:05:06Z"
	got, err = rotationRunView(withExpiry)
	if err != nil || !got.CredentialValidUntil.Equal(time.Date(2030, 2, 3, 4, 5, 6, 0, time.UTC)) || !got.CredentialValidUntilLowerBound {
		t.Fatalf("expiry: %+v, %v", got, err)
	}

	for field, mutate := range map[string]func(*store.RotationRunRecord){
		"started_at":             func(r *store.RotationRunRecord) { r.StartedAt = "nope" },
		"phase_at":               func(r *store.RotationRunRecord) { r.PhaseAt = "nope" },
		"credential_valid_until": func(r *store.RotationRunRecord) { r.CredentialValidUntil = "nope" },
	} {
		broken := withExpiry
		mutate(&broken)
		if _, err := rotationRunView(broken); err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("malformed %s = %v", field, err)
		}
	}
}
