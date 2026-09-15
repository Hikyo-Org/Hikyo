package store

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/audit"
)

// TestAuditFilterMatches pins the field projection applied after the authorized
// page read (see AuditFilter.Matches). Empty fields are unset; Actor and
// CorrelationID match exactly; Type/ObjectType/ObjectID match as `*` globs;
// Outcomes matches set membership; fields combine with AND. This is what makes
// browser and CLI queries over the same filter select the same events.
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
		{"outcome match", AuditFilter{Outcomes: []string{"success"}}, true},
		{"outcome mismatch", AuditFilter{Outcomes: []string{"failure"}}, false},
		{"outcome multi match", AuditFilter{Outcomes: []string{"failure", "success"}}, true},
		{"outcome multi mismatch", AuditFilter{Outcomes: []string{"failure", "error"}}, false},
		{"object type match", AuditFilter{ObjectType: "key"}, true},
		{"object type glob", AuditFilter{ObjectType: "ke*"}, true},
		{"object id mismatch", AuditFilter{ObjectID: "key_2"}, false},
		{"object id glob match", AuditFilter{ObjectID: "key_*"}, true},
		{"correlation match", AuditFilter{CorrelationID: "cor_1"}, true},
		{"conjunction all match", AuditFilter{Actor: "usr_alice", Type: "value.set", Outcomes: []string{"success"}}, true},
		{"conjunction one mismatch", AuditFilter{Actor: "usr_alice", Outcomes: []string{"failure"}}, false},
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

// TestNormalizedOutcomeAndActorName pins the payload types the audit registry
// asserts: filter_outcomes must be a []string (KindStringList does v.([]string),
// not []any), a single outcome collapses to the scalar filter_outcome for
// back-compat, and filter_actor_name is a plain string.
func TestNormalizedOutcomeAndActorName(t *testing.T) {
	// Multi-outcome + actor_name: the list field and the string field.
	multi := AuditFilter{Limit: 10, Outcomes: []string{"success", "failure"}, ActorName: "al*"}.Normalized()
	if _, ok := multi["filter_outcome"]; ok {
		t.Error("multiple outcomes must not also emit the scalar filter_outcome")
	}
	got, ok := multi["filter_outcomes"].([]string)
	if !ok {
		t.Fatalf("filter_outcomes = %T, want []string (KindStringList asserts []string)", multi["filter_outcomes"])
	}
	if len(got) != 2 || got[0] != "success" || got[1] != "failure" {
		t.Errorf("filter_outcomes = %v, want [success failure]", got)
	}
	if name, ok := multi["filter_actor_name"].(string); !ok || name != "al*" {
		t.Errorf("filter_actor_name = %v (%T), want %q string", multi["filter_actor_name"], multi["filter_actor_name"], "al*")
	}

	// One outcome stays the scalar filter_outcome (unchanged pre-wildcard payload).
	single := AuditFilter{Limit: 10, Outcomes: []string{"success"}}.Normalized()
	if _, ok := single["filter_outcomes"]; ok {
		t.Error("a single outcome must not emit the filter_outcomes list")
	}
	if v, ok := single["filter_outcome"].(string); !ok || v != "success" {
		t.Errorf("filter_outcome = %v, want scalar %q", single["filter_outcome"], "success")
	}
}

// TestMatchGlob pins the `*`-wildcard matcher behind the free-text audit
// filters: no-star patterns stay exact (back-compat), stars match any run
// including empty, and interior segments must appear in order.
func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"", "", true},
		{"", "x", false},
		{"key_1", "key_1", true},
		{"key_1", "key_2", false},
		{"*", "", true},
		{"*", "anything", true},
		{"key_*", "key_1", true},
		{"key_*", "key_", true},
		{"key_*", "val_1", false},
		{"*.set", "value.set", true},
		{"*.set", "value.delete", false},
		{"value.*", "value.set", true},
		{"a*b*c", "axxbyyc", true},
		{"a*b*c", "axxc", false},
		{"a*b*c", "abc", true},
		{"*mid*", "leftmidright", true},
		{"*mid*", "leftright", false},
	}
	for _, tc := range cases {
		if got := matchGlob(tc.pattern, tc.s); got != tc.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.pattern, tc.s, got, tc.want)
		}
	}
}
