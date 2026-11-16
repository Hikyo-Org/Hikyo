package cli

import (
	"strings"
	"testing"
)

func TestDurationSeconds(t *testing.T) {
	for _, tc := range []struct {
		raw       string
		allowZero bool
		want      int64
	}{
		{"90s", false, 90},
		{"30m", false, 1800},
		{"720h", false, 720 * 3600},
		{"1d", false, 86400},
		{"30d", false, 30 * 86400},
		{"1h30m", false, 5400},
		{"0", true, 0},
		{"106751d", false, 106751 * 86400},
	} {
		got, err := durationSeconds("--ttl", tc.raw, tc.allowZero)
		if err != nil || got != tc.want {
			t.Errorf("durationSeconds(%q, %v) = %d, %v; want %d", tc.raw, tc.allowZero, got, err, tc.want)
		}
	}
	for _, raw := range []string{"", "0", "0s", "500ms", "-5m", "d", "1.5d", "30 d", "abc", "9999999d", "106752d", "213505d"} {
		_, err := durationSeconds("--interval", raw, false)
		if err == nil || !strings.Contains(err.Error(), "--interval") {
			t.Errorf("durationSeconds(%q) = %v, want a usage error naming --interval", raw, err)
		}
	}
}

// TestDurationSecondsZeroMessage pins that a flag accepting 0 names it, and one
// that does not never offers it.
func TestDurationSecondsZeroMessage(t *testing.T) {
	_, err := durationSeconds("--delay", "-1s", true)
	if err == nil || !strings.Contains(err.Error(), "0 or a duration of at least one second") {
		t.Errorf("allowZero error = %v, want it to name 0", err)
	}
	_, err = durationSeconds("--ttl", "0", false)
	if err == nil || strings.Contains(err.Error(), "0 or") {
		t.Errorf("non-zero flag error = %v, must not offer 0", err)
	}
}
