package server

import (
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
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

// The summary reaches the wire as the view carries it: the halt reason only for a
// halted rotation, the last run only when there is one, and the two owed-drop counts
// always (zero is a fact, not an absence).
func TestCredentialRotationResponseCarriesTheSummary(t *testing.T) {
	var rec service.RotationView
	rec.ID, rec.State = "rot_1", "active"
	rec.CreatedAt, rec.UpdatedAt = "2030-01-02T03:04:05Z", "2030-01-02T03:04:05Z"

	got, err := credentialRotationResponse(rec)
	if err != nil {
		t.Fatal(err)
	}
	if got.HaltReason != nil || got.LastRun != nil || got.OwedDropRuns != 0 || got.OwedDropRunsVersionsChanged != 0 {
		t.Fatalf("a rotation with no summary: %+v", got)
	}

	rec.State, rec.HaltReason = "halted", "gate_failed"
	rec.OwedDropRuns, rec.OwedDropRunsVersionsChanged = 3, 1
	rec.LastRunSummary = &service.RotationLastRunView{ID: "rrn_1", Phase: "revoke_refused", ErrorCode: "published_versions_changed", PhaseAt: time.Date(2030, 2, 3, 4, 5, 6, 0, time.UTC)}
	got, err = credentialRotationResponse(rec)
	if err != nil {
		t.Fatal(err)
	}
	if got.HaltReason == nil || *got.HaltReason != "gate_failed" {
		t.Fatalf("halt reason = %v", got.HaltReason)
	}
	if got.OwedDropRuns != 3 || got.OwedDropRunsVersionsChanged != 1 {
		t.Fatalf("owed = %d/%d", got.OwedDropRuns, got.OwedDropRunsVersionsChanged)
	}
	if got.LastRun == nil || got.LastRun.Id != "rrn_1" || got.LastRun.Phase != apigen.RotationRunPhaseRevokeRefused ||
		got.LastRun.ErrorCode == nil || *got.LastRun.ErrorCode != "published_versions_changed" || !got.LastRun.PhaseAt.Equal(time.Date(2030, 2, 3, 4, 5, 6, 0, time.UTC)) {
		t.Fatalf("last run = %+v", got.LastRun)
	}

	rec.LastRunSummary = &service.RotationLastRunView{ID: "rrn_2", Phase: "done", PhaseAt: time.Date(2030, 2, 3, 4, 5, 6, 0, time.UTC)}
	if got, err = credentialRotationResponse(rec); err != nil || got.LastRun == nil || got.LastRun.ErrorCode != nil {
		t.Fatalf("a run with no error code: %+v, %v", got.LastRun, err)
	}
}
