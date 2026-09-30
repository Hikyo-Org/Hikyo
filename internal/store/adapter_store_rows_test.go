package store

import (
	"testing"
	"time"
)

func TestAdapterTargetIgnoresUnusedRetryTimestamp(t *testing.T) {
	for _, state := range []string{"", "running", "succeeded", "queued"} {
		target, err := decodeAdapterTarget(AdapterTarget{ActiveJobState: state}, "", "", "malformed", 0, nil, nil, nil)
		if err != nil || target.RetryAt != nil {
			t.Fatalf("unused retry timestamp for %q: %+v %v", state, target.RetryAt, err)
		}
	}
	if _, err := decodeAdapterTarget(AdapterTarget{ActiveJobState: "queued"}, "", "", "malformed", 1, nil, nil, nil); err == nil {
		t.Fatal("malformed active retry timestamp accepted")
	}
	stamp := "2026-09-30T14:00:00.123456Z"
	target, err := decodeAdapterTarget(AdapterTarget{ActiveJobState: "queued"}, "", "", stamp, 1, nil, nil, nil)
	if err != nil || target.RetryAt == nil || target.RetryAt.Format(time.RFC3339Nano) != stamp {
		t.Fatalf("active retry timestamp: %+v %v", target.RetryAt, err)
	}
}

func TestSQLiteRuntimeStampKeepsFixedMicrosecondWidth(t *testing.T) {
	at := time.Date(2026, 9, 30, 14, 0, 0, 0, time.FixedZone("offset", 3600))
	stamp := runtimeSQLiteStamp(at)
	if !stamp.Valid || stamp.String != "2026-09-30T13:00:00.000000Z" {
		t.Fatalf("noncanonical deadline stamp: %+v", stamp)
	}
}
