package runtimeconfig

// Migration adds one release-owned setting. Versions and defaults are immutable
// once released. A nil Default means operator input is required unless Optional
// explicitly permits an absent value. Empty string is an explicit default,
// never absence. The declaration remains in Catalogue.
type Migration struct {
	Version int64
	Name    string
	Default *string
	// Optional adds only the declaration when no value exists. It cannot be
	// combined with Default, and never removes an existing operator value.
	Optional bool
}

const MigrationVersion int64 = 5

func Migrations() []Migration {
	disabled := "false"
	optional := "optional"
	keyLimit := "10000"
	defaultKeys := "1000"
	return []Migration{
		{Version: 1, Name: "HIKYO_MCP_WRITE_ENABLED", Default: &disabled},
		// #760: an upgraded instance keeps today's posture — a password login on
		// an unenrolled account still mints a session — until an operator opts in.
		// Fresh installs default to `required` in config parsing (greenfield strict).
		{Version: 2, Name: "HIKYO_SECOND_FACTOR", Default: &optional},
		{Version: 3, Name: "HIKYO_MAX_KEYS_PER_PROJECT", Default: &keyLimit},
		{Version: 4, Name: "HIKYO_DEFAULT_KEYS_PER_PROJECT", Default: &defaultKeys},
		// Absence selects automatic public TLS discovery. A fabricated pin or
		// empty value would change that posture and must never be introduced.
		{Version: 5, Name: "HIKYO_PAIRING_SPKI_PIN", Optional: true},
	}
}
