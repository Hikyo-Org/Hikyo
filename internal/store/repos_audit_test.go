package store

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/audit"
)

// TestAuditFilterMatches pins the equality-field projection applied after the
// authorized page read (see AuditFilter.Matches). Empty fields are unset; a set
// field is an exact match; fields combine with AND. This is what makes browser
// and CLI queries over the same filter select the same events.
func TestAuditFilterMatches(t *testing.T) {
	row := AuditEvent{
		Event: audit.Event{
			Type:          "value.set",
			Actor:         audit.Actor{ID: "usr_alice"},
			Object:        audit.Object{Type: "key", ID: "key_1"},
			Outcome:       audit.OutcomeSuccess,
			CorrelationID: "cor_1",
		},
	}
	cases := []struct {
		name string
		f    AuditFilter
		want bool
	}{
		{"empty filter matches everything", AuditFilter{}, true},
		{"actor match", AuditFilter{Actor: "usr_alice"}, true},
		{"actor mismatch", AuditFilter{Actor: "usr_bob"}, false},
		{"type match", AuditFilter{Type: "value.set"}, true},
		{"type mismatch", AuditFilter{Type: "value.delete"}, false},
		{"outcome match", AuditFilter{Outcome: "success"}, true},
		{"outcome mismatch", AuditFilter{Outcome: "failure"}, false},
		{"object type match", AuditFilter{ObjectType: "key"}, true},
		{"object id mismatch", AuditFilter{ObjectID: "key_2"}, false},
		{"correlation match", AuditFilter{CorrelationID: "cor_1"}, true},
		{"conjunction all match", AuditFilter{Actor: "usr_alice", Type: "value.set", Outcome: "success"}, true},
		{"conjunction one mismatch", AuditFilter{Actor: "usr_alice", Outcome: "failure"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.f.Matches(row); got != tc.want {
				t.Fatalf("Matches = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAuditPageSizeIsClampedToTheCap pins the ops-spec § 10 response cap: an
// audit page never exceeds AuditMaxPageSize rows, regardless of what the caller
// asked for. bounds() is the single chokepoint every engine's page read routes
// through, so the clamp holds for tenant and instance, sqlite and postgres.
func TestAuditPageSizeIsClampedToTheCap(t *testing.T) {
	f := AuditFilter{Limit: AuditMaxPageSize + 500}
	if _, _, err := f.bounds(); err != nil {
		t.Fatalf("bounds() on a valid filter: %v", err)
	}
	if f.Limit != AuditMaxPageSize {
		t.Fatalf("page limit = %d, want it clamped to %d", f.Limit, AuditMaxPageSize)
	}

	// A request already under the cap is left untouched.
	under := AuditFilter{Limit: 25}
	if _, _, err := under.bounds(); err != nil {
		t.Fatalf("bounds() under the cap: %v", err)
	}
	if under.Limit != 25 {
		t.Fatalf("an under-cap limit was altered to %d", under.Limit)
	}

	// The positive-limit invariant still refuses a non-positive page.
	empty := AuditFilter{Limit: 0}
	if _, _, err := empty.bounds(); err == nil {
		t.Fatal("a non-positive page limit must be refused")
	}
}
