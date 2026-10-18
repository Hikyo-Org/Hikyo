package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/audit"
)

func TestAuditActorLookupFailuresAndLifetime(t *testing.T) {
	var c auditActorCache
	calls := 0
	sentinel := errors.New("unavailable")
	lookup := func(context.Context, string) (string, error) { calls++; return "", sentinel }
	for range 2 {
		if _, err := c.lookup(t.Context(), lookup, "actor"); !errors.Is(err, sentinel) {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatal("cached failed lookup")
	}
	for _, kind := range []string{"", "unknown"} {
		lookup = func(context.Context, string) (string, error) { calls++; return kind, nil }
		before := calls
		for range 2 {
			_, _ = c.lookup(t.Context(), lookup, "actor")
		}
		if calls-before != 2 {
			t.Fatal("cached unknown kind")
		}
	}
	lookup = func(context.Context, string) (string, error) { calls++; return "human", nil }
	for range 2 {
		_, _ = c.lookup(t.Context(), lookup, "actor")
	}
	before := calls
	next := auditActorCache{}
	changed := func(context.Context, string) (string, error) { calls++; return "machine", nil }
	if kind, err := next.lookup(t.Context(), changed, "actor"); err != nil || kind != "machine" {
		t.Fatalf("new transaction reused %q: %v", kind, err)
	}
	if calls != before+1 {
		t.Fatal("state escaped transaction owner")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.lookup(ctx, lookup, "actor"); !errors.Is(err, context.Canceled) {
		t.Fatal("reuse ignored cancellation")
	}
}

func TestAuditMatcherSnapshot(t *testing.T) {
	f := AuditFilter{Actor: "actor", Type: "value.*", ObjectType: "k*y", ObjectID: "id**", Outcomes: []string{"success"}, CorrelationID: "cor", Limit: 20}
	before := f.Normalized()
	matcher := f.Compile()
	if !reflect.DeepEqual(before, f.Normalized()) {
		t.Fatal("compile changed normalized filter")
	}
	e := AuditEvent{Event: audit.Event{Type: "value.set", Actor: audit.Actor{ID: "actor"}, Object: audit.Object{Type: "key", ID: "id1"}, Outcome: audit.OutcomeSuccess, CorrelationID: "cor"}}
	f.Type = "grant.*"
	f.Actor = "other"
	f.ObjectType = "other"
	f.ObjectID = "other"
	f.Outcomes[0] = "failure"
	f.CorrelationID = "other"
	if !matcher.Matches(e) || f.Compile().Matches(e) || f.Matches(e) {
		t.Fatal("matcher snapshot leaked source mutations")
	}
}

// Retain the pre-optimization algorithm as an independent semantic oracle and
// benchmark baseline. '*' is the only metacharacter, case remains significant.
func legacyAuditGlob(pattern, s string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == s
	}
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for _, mid := range parts[1 : len(parts)-1] {
		i := strings.Index(s, mid)
		if i < 0 {
			return false
		}
		s = s[i+len(mid):]
	}
	return strings.HasSuffix(s, parts[len(parts)-1])
}
func TestCompiledAuditGlobEquivalence(t *testing.T) {
	words := []string{""}
	level := []string{""}
	for range 4 {
		next := []string{}
		for _, p := range level {
			for _, r := range []string{"a", "A", "*"} {
				next = append(next, p+r)
			}
		}
		words = append(words, next...)
		level = next
	}
	for _, p := range words {
		g := compileAuditGlob(p)
		for _, s := range words {
			if got, want := g.matches(s), legacyAuditGlob(p, s); got != want {
				t.Fatalf("%q %q: %v != %v", p, s, got, want)
			}
		}
	}
}

func legacyAuditMatches(f AuditFilter, e AuditEvent) bool {
	switch {
	case f.Actor != "" && e.Actor.ID != f.Actor:
		return false
	case f.Type != "" && !legacyAuditGlob(f.Type, string(e.Type)):
		return false
	case len(f.Outcomes) > 0 && !slices.Contains(f.Outcomes, string(e.Outcome)):
		return false
	case f.ObjectType != "" && !legacyAuditGlob(f.ObjectType, e.Object.Type):
		return false
	case f.ObjectID != "" && !legacyAuditGlob(f.ObjectID, e.Object.ID):
		return false
	case f.CorrelationID != "" && e.CorrelationID != f.CorrelationID:
		return false
	default:
		return true
	}
}

var auditBenchmarkMatches int

func BenchmarkAuditFilter16000(b *testing.B) {
	for _, wild := range []bool{true, false} {
		b.Run(fmt.Sprintf("wildcard=%v", wild), func(b *testing.B) {
			f := AuditFilter{Type: "value.set", ObjectType: "key", ObjectID: "key_123"}
			if wild {
				f.Type = "v*l*.*t"
				f.ObjectType = "k*y"
				f.ObjectID = "*key*1*3*"
			}
			e := AuditEvent{Event: audit.Event{Type: "value.set", Object: audit.Object{Type: "key", ID: "key_123"}}}
			if !legacyAuditMatches(f, e) || !f.Compile().Matches(e) {
				b.Fatal("fixture must exercise all three matching predicates")
			}
			b.Run("before", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					n := 0
					for range 16000 {
						if legacyAuditMatches(f, e) {
							n++
						}
					}
					auditBenchmarkMatches = n
				}
			})
			b.Run("compiled", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					matcher := f.Compile()
					n := 0
					for range 16000 {
						if matcher.Matches(e) {
							n++
						}
					}
					auditBenchmarkMatches = n
				}
			})
		})
	}
}
