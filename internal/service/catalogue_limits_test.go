package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestConfiguredProjectKeyGrowth(t *testing.T) {
	for _, tc := range []struct {
		current, next int64
		limit         int
		refused       bool
	}{
		{999, 1000, 0, false}, {1000, 1001, 0, true},
		{1000, 1001, 2500, false}, {2500, 2501, 2500, true},
		{2500, 2500, 1000, false}, {2500, 2499, 1000, false}, {2500, 2501, 1000, true},
	} {
		err := checkProjectKeyGrowth(tc.current, tc.next, tc.limit)
		if errors.Is(err, domain.ErrLimitExceeded) != tc.refused {
			t.Errorf("growth %d to %d at limit %d: %v", tc.current, tc.next, tc.limit, err)
		}
	}
}

func TestConfiguredDefinitionsCapsAndResolvedCells(t *testing.T) {
	keys := make([]definitions.CurrentKey, 2500)
	for i := range keys {
		keys[i].ID, keys[i].Name = fmt.Sprintf("key_%d", i), fmt.Sprintf("KEY_%d", i)
	}
	cur := definitions.CurrentState{Keys: keys}
	for i := range 40 {
		cur.Environments = append(cur.Environments, definitions.Environment{ID: fmt.Sprintf("env_%d", i), Name: fmt.Sprintf("env-%d", i)})
	}
	if err := validateFinalDefinitions(cur, definitions.Resolution{}, 1000); err != nil {
		t.Fatalf("lowered cap blocked existing definitions: %v", err)
	}
	if err := validateFinalDefinitions(cur, definitions.Resolution{EnvCreates: []string{"overflow"}}, 2500); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("environment growth bypassed resolved-cell cap: %v", err)
	}
	if err := validateFinalDefinitions(cur, definitions.Resolution{KeyCreates: []definitions.Key{{Name: "OVERFLOW"}}}, 3000); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("key growth bypassed resolved-cell cap: %v", err)
	}
	if config.MaxKeysPerProjectCeiling > definitions.MaxBundleEntries {
		t.Fatal("key ceiling exceeds bundle entry budget")
	}
}

func TestResolveProjectCapacity(t *testing.T) {
	custom, invalid := 5000, -1
	for _, tc := range []struct {
		name                                   string
		override                               *int
		instanceDefault, instanceMaximum, want int
		capped, invalid                        bool
	}{
		{name: "direct construction defaults", want: 1000},
		{name: "inherited default", instanceDefault: 2000, instanceMaximum: 10000, want: 2000},
		{name: "custom", override: &custom, want: 5000},
		{name: "lowered ceiling retains override", override: &custom, instanceMaximum: 3000, want: 3000, capped: true},
		{name: "invalid owner policy", instanceDefault: 4000, instanceMaximum: 3000, invalid: true},
		{name: "invalid stored policy", override: &invalid, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveProjectCapacity(store.ProjectCapacity{KeyLimitOverride: tc.override, Revision: 42}, tc.instanceDefault, tc.instanceMaximum)
			if tc.invalid {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("resolve = %+v, %v", got, err)
				}
				return
			}
			if err != nil || got.EffectiveLimit != tc.want || got.Capped != tc.capped || got.Revision != 42 || got.Inherited != (tc.override == nil) {
				t.Fatalf("resolve = %+v, %v", got, err)
			}
			if tc.override != nil && (got.KeyLimitOverride == nil || *got.KeyLimitOverride != *tc.override) {
				t.Fatalf("saved override lost: %+v", got)
			}
		})
	}
}
