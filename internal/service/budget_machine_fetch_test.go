package service

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestMachineFetchPrincipalBucket(t *testing.T) {
	c := &clock{t: time.Unix(1700000000, 0)}
	b := newTestBudget(c)
	charge := func(id domain.PrincipalID) error { charged := false; return b.chargeMachineFetchOnce(&charged, id) }
	for range BudgetMachineFetchPrincipalBurst {
		charged := false
		if err := b.chargeMachineFetchOnce(&charged, "shared"); err != nil {
			t.Fatal(err)
		}
		if err := b.chargeMachineFetchOnce(&charged, "shared"); err != nil {
			t.Fatal("retry charged twice", err)
		}
	}
	if err := charge("shared"); !errors.Is(err, admission.ErrOverloaded) {
		t.Fatalf("burst overflow: %v", err)
	}
	if err := charge("other"); err != nil {
		t.Fatal("independent principal", err)
	}
	c.add(time.Second)
	if err := charge("shared"); !errors.Is(err, admission.ErrOverloaded) {
		t.Fatalf("early refill: %v", err)
	}
	c.add(time.Second)
	if err := charge("shared"); err != nil {
		t.Fatal("two-second refill", err)
	}
	if err := charge("shared"); !errors.Is(err, admission.ErrOverloaded) {
		t.Fatalf("refill overflow: %v", err)
	}
	c.add(-time.Minute)
	if err := charge("shared"); !errors.Is(err, admission.ErrOverloaded) {
		t.Fatalf("clock rollback: %v", err)
	}
}

func TestMachineFetchPrincipalTrackingBound(t *testing.T) {
	c := &clock{t: time.Unix(1700000000, 0)}
	b := newTestBudget(c)
	for i := range budgetMaxTrackedSubjects {
		charged := false
		if err := b.chargeMachineFetchOnce(&charged, domain.PrincipalID(fmt.Sprintf("p%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	charged := false
	if err := b.chargeMachineFetchOnce(&charged, "overflow"); !errors.Is(err, admission.ErrOverloaded) {
		t.Fatalf("tracking bound: %v", err)
	}
	c.add(2 * time.Minute)
	if err := b.chargeMachineFetchOnce(&charged, "overflow"); err != nil {
		t.Fatal("idle eviction", err)
	}
	if len(b.machineFetch) != 1 {
		t.Fatalf("stale buckets remain: %d", len(b.machineFetch))
	}
}
