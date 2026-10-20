package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// TestRenderTotalRefusesAnOversizedTarget pins the ops-spec § 8 per-target
// render cap: a resolved environment whose delivered bytes exceed
// MaxRenderBytesPerTarget is refused by name at publish. Absent cells and the
// under-cap case are left alone.
func TestRenderTotalRefusesAnOversizedTarget(t *testing.T) {
	big := resolvedCell{key: store.CatalogueKey{Name: "BIG"}, set: true,
		value: strings.Repeat("x", MaxRenderBytesPerTarget+1)}
	if err := checkRenderTotal([]resolvedCell{big}, "env_prod"); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("an over-cap target must be refused with ErrLimitExceeded: %v", err)
	}

	// Two just-over-half-cap set cells cross the bound in aggregate — the cap is
	// on the summed VALUE bytes, not any single value.
	overHalf := strings.Repeat("x", MaxRenderBytesPerTarget/2+1)
	pair := []resolvedCell{
		{key: store.CatalogueKey{Name: "A"}, set: true, value: overHalf},
		{key: store.CatalogueKey{Name: "B"}, set: true, value: overHalf},
	}
	if err := checkRenderTotal(pair, "env_prod"); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("two over-half values must sum past the target cap: %v", err)
	}

	// Exactly-at-the-cap value bytes are accepted — matching Kubernetes, which
	// charges value bytes only (key names are not counted).
	exact := []resolvedCell{
		{key: store.CatalogueKey{Name: "A-LONG-KEY-NAME"}, set: true, value: strings.Repeat("x", MaxRenderBytesPerTarget)},
	}
	if err := checkRenderTotal(exact, "env_prod"); err != nil {
		t.Fatalf("a target whose value bytes equal the cap must publish (names uncounted): %v", err)
	}

	// An absent cell of any nominal size is not delivered, so it is not charged.
	ok := []resolvedCell{
		{key: store.CatalogueKey{Name: "SET"}, set: true, value: "small"},
		{key: store.CatalogueKey{Name: "ABSENT"}, set: false, value: strings.Repeat("x", MaxRenderBytesPerTarget)},
	}
	if err := checkRenderTotal(ok, "env_prod"); err != nil {
		t.Fatalf("an under-cap target (absent cells uncharged) must publish: %v", err)
	}
}

// The default fits by construction; larger configured caps require admission.
func TestResolvedCellBudgetComposesByConstruction(t *testing.T) {
	product := MaxEnvironmentsPerProject * config.DefaultKeysPerProject
	if product > MaxResolvedCells {
		t.Fatal("default maxima exceed resolved-cell budget")
	}
	for _, tc := range []struct {
		keys, envs int64
		refused    bool
	}{
		{1000, 50, false}, {2000, 50, false}, {2001, 50, true},
		{10000, 10, false}, {10000, 11, true}, {10000, 0, false},
	} {
		err := checkResolvedCells(tc.keys, tc.envs)
		if errors.Is(err, domain.ErrLimitExceeded) != tc.refused {
			t.Errorf("keys=%d envs=%d: %v", tc.keys, tc.envs, err)
		}
	}
}
