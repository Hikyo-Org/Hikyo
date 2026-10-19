package config

import (
	"strconv"
	"strings"
	"testing"
)

func TestProjectKeyLimitConfiguration(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
	}{
		{"", 10000}, {"1", 1}, {"2500", 2500}, {"10000", 10000},
		{"0", 0}, {"-1", 0}, {"1.5", 0}, {"10001", 0}, {"unlimited", 0},
	} {
		t.Run("value="+tc.raw, func(t *testing.T) {
			cfg, _, err := Load("server", []string{"--dev"}, env("HIKYO_MAX_KEYS_PER_PROJECT", tc.raw, "HIKYO_DEFAULT_KEYS_PER_PROJECT", "1"), nil)
			if tc.want == 0 {
				if err == nil || !strings.Contains(err.Error(), "HIKYO_MAX_KEYS_PER_PROJECT") {
					t.Fatalf("invalid limit accepted or unnamed: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MaxKeysPerProject != tc.want {
				t.Fatalf("limit=%d, want %d", cfg.MaxKeysPerProject, tc.want)
			}
			managed, err := ApplyManagedOwnerValues(cfg, map[string]string{"HIKYO_MAX_KEYS_PER_PROJECT": strconv.Itoa(tc.want), "HIKYO_DEFAULT_KEYS_PER_PROJECT": "1"})
			if err != nil || managed.MaxKeysPerProject != tc.want {
				t.Fatalf("managed round trip: %v", err)
			}
			if managed.ManagedOwnerValues()["HIKYO_MAX_KEYS_PER_PROJECT"] != strconv.Itoa(tc.want) {
				t.Fatal("limit missing from managed seed")
			}
			defaults, err := ApplyManagedOwnerValues(managed, nil)
			if err != nil || defaults.MaxKeysPerProject != DefaultMaxKeysPerProject {
				t.Fatalf("absence retained stale override: %v", err)
			}
		})
	}
}

func TestProjectKeyDefaultCannotExceedInstanceMaximum(t *testing.T) {
	for _, values := range []map[string]string{
		{"HIKYO_DEFAULT_KEYS_PER_PROJECT": "0"},
		{"HIKYO_DEFAULT_KEYS_PER_PROJECT": "10001"},
		{"HIKYO_DEFAULT_KEYS_PER_PROJECT": "2500", "HIKYO_MAX_KEYS_PER_PROJECT": "2000"},
	} {
		if err := ValidateManagedOwnerValues(values); err == nil {
			t.Errorf("invalid policy accepted: %v", values)
		}
	}
	cfg, _, err := Load("server", []string{"--dev"}, env(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultKeysPerProject != 1000 || cfg.MaxKeysPerProject != 10000 {
		t.Fatal("unexpected default and ceiling")
	}
	updated, err := ApplyManagedOwnerValues(cfg, map[string]string{"HIKYO_DEFAULT_KEYS_PER_PROJECT": "2000", "HIKYO_MAX_KEYS_PER_PROJECT": "5000"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DefaultKeysPerProject != 2000 || updated.MaxKeysPerProject != 5000 {
		t.Fatal("managed policy was not captured")
	}
}
