package config

const (
	// DefaultKeysPerProject preserves the original small-installation default.
	DefaultKeysPerProject = 1000
	// DefaultMaxKeysPerProject bounds project overrides unless the operator
	// selects a lower instance ceiling.
	DefaultMaxKeysPerProject = 10_000
	// MaxKeysPerProjectCeiling matches the definitions bundle and per-environment
	// validation budgets. Raising this requires a separate capacity review.
	MaxKeysPerProjectCeiling = 10_000
)

func EffectiveDefaultKeysPerProject(limit int) int {
	if limit == 0 {
		return DefaultKeysPerProject
	}
	return limit
}

// EffectiveMaxKeysPerProject supplies the documented default for configurations
// constructed directly by embedded callers. Parsed settings are always nonzero.
func EffectiveMaxKeysPerProject(limit int) int {
	if limit == 0 {
		return DefaultMaxKeysPerProject
	}
	return limit
}
