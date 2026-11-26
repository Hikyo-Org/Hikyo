package releasetrust_test

import (
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

// The 30-day bound is enforced at admission, not only where a statement is signed: a
// bridge whose not_after lies beyond MaxBridgeWindow plus the documented skew is
// unusable even though it verifies.
func TestBridgeBeyondTheMaximumWindowIsNotUsable(t *testing.T) {
	now := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	limit := now.Add(releasetrust.MaxBridgeWindow + releasetrust.BridgeWindowSkew)
	for _, tc := range []struct {
		name    string
		at      time.Time
		expired bool
	}{
		{"inside the window", now.Add(releasetrust.MaxBridgeWindow), false},
		{"at the skew limit", limit, false},
		{"one second past the skew limit", limit.Add(time.Second), true},
		{"years out", now.Add(5 * 365 * 24 * time.Hour), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := testfixture.New(t)
			material := f.AddBridge(t, v2Statement(releasetrust.BridgeSchemaV2, tc.at.Format(time.RFC3339)))
			bridge, err := releasetrust.VerifyBridge(f.Snapshot(t), material)
			if err != nil || bridge.Expired(now) != tc.expired {
				t.Fatalf("Expired = %v (err %v), want %v", bridge.Expired(now), err, tc.expired)
			}
		})
	}
}
