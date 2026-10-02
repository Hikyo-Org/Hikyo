package service

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
)

func TestAuthenticatedAPIRequestBucket(t *testing.T) {
	c := &clock{t: time.Unix(1700000000, 0)}
	b := newTestBudget(c)
	for range BudgetAuthenticatedAPIBurst {
		if err := b.AdmitAuthenticatedAPI("session"); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.AdmitAuthenticatedAPI("session"); !errors.Is(err, admission.ErrOverloaded) {
		t.Fatalf("burst overflow: %v", err)
	}
	if err := b.AdmitAuthenticatedAPI("other-session"); err != nil {
		t.Fatal(err)
	}
	c.add(100 * time.Millisecond)
	if err := b.AdmitAuthenticatedAPI("session"); !errors.Is(err, admission.ErrOverloaded) {
		t.Fatalf("early refill: %v", err)
	}
	c.add(100 * time.Millisecond)
	if err := b.AdmitAuthenticatedAPI("session"); err != nil {
		t.Fatalf("300/min refill: %v", err)
	}
	c.add(-time.Minute)
	if err := b.AdmitAuthenticatedAPI("session"); !errors.Is(err, admission.ErrOverloaded) {
		t.Fatalf("clock rollback: %v", err)
	}
}

func TestAuthenticatedAPITrackingBound(t *testing.T) {
	c := &clock{t: time.Unix(1700000000, 0)}
	b := newTestBudget(c)
	for i := range budgetMaxTrackedSubjects {
		if err := b.AdmitAuthenticatedAPI(fmt.Sprintf("s%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.AdmitAuthenticatedAPI("overflow"); !errors.Is(err, admission.ErrOverloaded) {
		t.Fatalf("tracking overflow: %v", err)
	}
	c.add(2 * time.Minute)
	if err := b.AdmitAuthenticatedAPI("overflow"); err != nil {
		t.Fatal(err)
	}
	if len(b.humanAPI) != 1 {
		t.Fatal("idle session buckets not reclaimed")
	}
}
