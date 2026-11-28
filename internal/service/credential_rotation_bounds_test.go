package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// TestRotationBoundsAreNamedRefusals pins the four ops-catalogue bounds and the
// delay-shorter-than-interval rule: each hit is domain.ErrInvalid naming the
// bound, and the exact limits are accepted.
func TestRotationBoundsAreNamedRefusals(t *testing.T) {
	sec := func(d time.Duration) int64 { return int64(d / time.Second) }
	for _, tc := range []struct {
		name            string
		interval, delay time.Duration
		wantErr         string
	}{
		{"minimum interval and delay accepted", domain.MinRotationInterval, domain.MinRevocationDelay, ""},
		{"maximum interval and delay accepted", domain.MaxRotationInterval, domain.MaxRevocationDelay, ""},
		{"interval below minimum", domain.MinRotationInterval - time.Second, domain.MinRevocationDelay, "--interval (interval_seconds) 3599s is below the 1h minimum rotation interval"},
		{"interval above maximum", domain.MaxRotationInterval + time.Second, domain.MinRevocationDelay, "above the 90d maximum rotation interval"},
		{"delay below minimum", 24 * time.Hour, domain.MinRevocationDelay - time.Second, "--revocation-delay (revocation_delay_seconds) 299s is below the 5m minimum revocation delay"},
		{"delay above maximum", domain.MaxRotationInterval, domain.MaxRevocationDelay + time.Second, "above the 7d maximum revocation delay"},
		{"delay equal to interval", time.Hour, time.Hour, "shorter than --interval"},
		{"delay longer than interval", 2 * time.Hour, 3 * time.Hour, "shorter than --interval"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRotationBounds(sec(tc.interval), sec(tc.delay))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateRotationBounds = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateRotationBounds = %v, want ErrInvalid naming %q", err, tc.wantErr)
			}
		})
	}
}

// TestRotationBoundsRefuseHugeSecondsWithoutOverflow pins that a value above the
// time.Duration range (about 9.2e9 seconds) is rendered from seconds, not wrapped
// into a negative or small duration.
func TestRotationBoundsRefuseHugeSecondsWithoutOverflow(t *testing.T) {
	const huge = int64(9_300_000_001)
	if got := rotationDuration(huge); got != "9300000001s" {
		t.Fatalf("rotationDuration(%d) = %q, want 9300000001s", huge, got)
	}
	if got := rotationDuration(86400 * 108000); got != "108000d" {
		t.Fatalf("rotationDuration of a huge whole-day value = %q, want 108000d", got)
	}
	err := ValidateRotationBounds(huge, int64(domain.MinRevocationDelay/time.Second))
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "9300000001s is above the 90d maximum rotation interval") {
		t.Fatalf("ValidateRotationBounds(huge interval) = %v, want ErrInvalid naming the raw seconds", err)
	}
	err = ValidateRotationBounds(int64(domain.MaxRotationInterval/time.Second), huge)
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "9300000001s is above the 7d maximum revocation delay") {
		t.Fatalf("ValidateRotationBounds(huge delay) = %v, want ErrInvalid naming the raw seconds", err)
	}
}
