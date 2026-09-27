package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
)

type fakeWindowCounter struct {
	counts map[string]int64
	err    error
}

func (f *fakeWindowCounter) BumpWindow(_ context.Context, bucket, subject string, _ time.Time) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.counts[bucket+"|"+subject]++
	return f.counts[bucket+"|"+subject], nil
}

// Under HA the transit rate limit is the installation-wide shared counter
// (transit ADR D9): it admits up to the per-principal bound, refuses past it
// with the uniform overload, charges once per operation across transaction
// retries, and fails closed when the counter cannot be read.
func TestTransitSharedRateLimit(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 30, 0, time.UTC)
	counter := &fakeWindowCounter{counts: map[string]int64{}}
	s := &Transit{Shared: counter}
	for i := 0; i < BudgetTransitRatePerMin; i++ {
		charged := false
		if err := s.chargeTransit(t.Context(), &charged, "usr_a", "org_a", now); err != nil {
			t.Fatalf("charge %d refused: %v", i, err)
		}
	}
	charged := false
	if err := s.chargeTransit(t.Context(), &charged, "usr_a", "org_a", now); !errors.Is(err, admission.ErrOverloaded) {
		t.Fatalf("charge past the bound = %v, want overload", err)
	}
	// Another principal in the same org still has its own allowance.
	charged = false
	if err := s.chargeTransit(t.Context(), &charged, "usr_b", "org_a", now); err != nil {
		t.Fatalf("second principal refused: %v", err)
	}
	// A retried transaction attempt does not charge twice.
	before := counter.counts["transit-principal|usr_b"]
	if err := s.chargeTransit(t.Context(), &charged, "usr_b", "org_a", now); err != nil || counter.counts["transit-principal|usr_b"] != before {
		t.Fatalf("retry charged again: %v", err)
	}
	down := &Transit{Shared: &fakeWindowCounter{err: errors.New("datastore unreachable")}}
	charged = false
	if err := down.chargeTransit(t.Context(), &charged, "usr_a", "org_a", now); !errors.Is(err, admission.ErrOverloaded) || charged {
		t.Fatalf("unreadable counter = %v (charged=%v), want fail-closed overload", err, charged)
	}
}

// The single-node path charges the per-node transit budget.
func TestTransitBudgetRateLimit(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	b := NewBudget()
	b.now = func() time.Time { return now }
	s := &Transit{Budget: b}
	for i := 0; i < BudgetTransitRatePerMin; i++ {
		charged := false
		if err := s.chargeTransit(t.Context(), &charged, "usr_a", "org_a", now); err != nil {
			t.Fatalf("charge %d refused: %v", i, err)
		}
	}
	charged := false
	if err := s.chargeTransit(t.Context(), &charged, "usr_a", "org_a", now); !errors.Is(err, admission.ErrOverloaded) {
		t.Fatalf("charge past the bound = %v, want overload", err)
	}
}

func TestTransitRotationPeriodRejectsOverflow(t *testing.T) {
	for _, seconds := range []int64{-1, 3599, 18446744074 + 3600, 1<<63 - 1} {
		if err := checkRotationPeriod(seconds); err == nil {
			t.Fatalf("accepted invalid seconds %d", seconds)
		}
	}
	for _, seconds := range []int64{0, int64(MinTransitRotationPeriod / time.Second), int64(MaxTransitRotationPeriod / time.Second)} {
		if err := checkRotationPeriod(seconds); err != nil {
			t.Fatalf("rejected seconds %d: %v", seconds, err)
		}
	}
}

func TestTransitCallerIDGrammar(t *testing.T) {
	for _, id := range []string{"", "usr_alice", "user@example.com", "usr_01993e53-0000-7000-8000-000000000001"} {
		_, err := checkTransitCallers([]TransitCallerEntry{{PrincipalID: id, Operations: []string{"encrypt"}}}, []string{"encrypt"})
		if wantValid := id == "usr_01993e53-0000-7000-8000-000000000001"; (err == nil) != wantValid {
			t.Fatalf("id %q: %v", id, err)
		}
	}
}
