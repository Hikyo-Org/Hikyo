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
		{"interval below minimum", domain.MinRotationInterval - time.Second, domain.MinRevocationDelay, "minimum rotation interval"},
		{"interval above maximum", domain.MaxRotationInterval + time.Second, domain.MinRevocationDelay, "maximum rotation interval"},
		{"delay below minimum", 24 * time.Hour, domain.MinRevocationDelay - time.Second, "minimum revocation delay"},
		{"delay above maximum", domain.MaxRotationInterval, domain.MaxRevocationDelay + time.Second, "maximum revocation delay"},
		{"delay equal to interval", time.Hour, time.Hour, "shorter than interval_seconds"},
		{"delay longer than interval", 2 * time.Hour, 3 * time.Hour, "shorter than interval_seconds"},
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

func TestRotationBoundConstantsMatchOpsCatalogue(t *testing.T) {
	if domain.MinRotationInterval != time.Hour || domain.MaxRotationInterval != 90*24*time.Hour ||
		domain.MinRevocationDelay != 5*time.Minute || domain.MaxRevocationDelay != 7*24*time.Hour {
		t.Fatalf("rotation bounds drifted from docs/spec/ops-catalogue.md: %v %v %v %v",
			domain.MinRotationInterval, domain.MaxRotationInterval, domain.MinRevocationDelay, domain.MaxRevocationDelay)
	}
}
