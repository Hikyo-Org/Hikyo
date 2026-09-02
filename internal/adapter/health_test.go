package adapter

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestDeriveHealthIsClosedAndPauseWins(t *testing.T) {
	tests := []struct {
		status    string
		paused    bool
		converged bool
		job       string
		want      TargetHealth
	}{
		{"never", false, false, "", HealthNever},
		{"converging", false, false, "queued", HealthPending},
		{"converging", false, false, "running", HealthConverging},
		{"converging", false, true, "", HealthConverging},
		{"converged", false, true, "", HealthConverged},
		{"failed", false, false, "", HealthFailed},
		{"failed", false, true, "queued", HealthDegraded},
		{"failed", true, true, "queued", HealthPaused},
		{"converged", true, true, "", HealthPaused},
		{"never", true, false, "", HealthPaused},
		{"bogus", false, false, "", HealthNever},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/paused=%v/converged=%v/job=%s", tt.status, tt.paused, tt.converged, tt.job), func(t *testing.T) {
			if got := DeriveHealth(tt.status, tt.paused, tt.converged, tt.job); got != tt.want {
				t.Fatalf("DeriveHealth = %q, want %q", got, tt.want)
			}
		})
	}
}

type classifyRetryErr struct{ at time.Time }

func (e classifyRetryErr) Error() string      { return "slow down" }
func (e classifyRetryErr) RetryAt() time.Time { return e.at }

func TestClassifyErrorIsBoundedAndNamesNoProviderDetail(t *testing.T) {
	tests := []struct {
		err  error
		want ErrorClass
	}{
		{nil, ""},
		{ErrProviderAuth, ErrorClassAuth},
		{fmt.Errorf("wrapped: %w", ErrProviderAuth), ErrorClassAuth},
		{ErrConflict, ErrorClassConflict},
		{ErrRateLimited, ErrorClassProviderLimit},
		{classifyRetryErr{at: time.Now()}, ErrorClassProviderLimit},
		{ErrQueueFull, ErrorClassProviderLimit},
		{ErrIndeterminate, ErrorClassProviderAmbiguous},
		{ErrDestinationID, ErrorClassProviderAmbiguous},
		{ErrUnauthorized, ErrorClassRefused},
		{ErrSuperseded, ErrorClassRefused},
		{errors.New("dial tcp: connection reset by peer"), ErrorClassNetwork},
	}
	for _, tt := range tests {
		if got := ClassifyError(tt.err); got != tt.want {
			t.Errorf("ClassifyError(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
	if !ErrorClassConflict.NeedsAttention() || !ErrorClassProviderAmbiguous.NeedsAttention() {
		t.Fatal("conflict and provider_ambiguous must demand operator attention")
	}
	if ErrorClassNetwork.NeedsAttention() || ErrorClassAuth.NeedsAttention() {
		t.Fatal("retryable classes must not demand operator attention")
	}
}
