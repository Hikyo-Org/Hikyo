package config

import (
	"fmt"
	"strconv"
	"strings"
)

var managedDevelopmentKeys = []string{
	"HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE",
	"HIKYO_DEV_SERVICE_BUDGETS_DISABLED",
	"HIKYO_DEV_ADAPTER_FAKE_PROVIDER",
}

// parseDevelopmentOverrides is shared by startup and node activation. The
// deployment's development context is fixed externally; values cannot set it.
func parseDevelopmentOverrides(cfg *Config, getenv func(string) string) error {
	cfg.DevAdmissionPerIPPerMinute = 0
	cfg.DevServiceBudgetsDisabled = false
	cfg.DevAdapterFakeProvider = false
	for _, key := range managedDevelopmentKeys {
		raw := strings.TrimSpace(getenv(key))
		if raw == "" {
			continue
		}
		if !cfg.Dev {
			return fmt.Errorf("%s is a development-mode override and this is not a development server: remove it, or pass --dev if this is an evaluation instance", key)
		}
		if key == "HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE" {
			value, err := strconv.ParseInt(raw, 10, 32)
			if err != nil || value < 1 {
				return fmt.Errorf("%s: %q is not a positive portable integer", key, raw)
			}
			cfg.DevAdmissionPerIPPerMinute = int(value)
			continue
		}
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("%s: %q is not a boolean", key, raw)
		}
		if key == "HIKYO_DEV_SERVICE_BUDGETS_DISABLED" {
			cfg.DevServiceBudgetsDisabled = value
		} else {
			cfg.DevAdapterFakeProvider = value
		}
	}
	return nil
}
