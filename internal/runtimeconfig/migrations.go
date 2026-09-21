package runtimeconfig

// Migration adds one release-owned setting. Versions and defaults are immutable
// once released. A nil Default means operator input is required; empty string is
// an explicit default, never absence. The declaration remains in Catalogue.
type Migration struct {
	Version int64
	Name    string
	Default *string
}

const MigrationVersion int64 = 2

func Migrations() []Migration {
	disabled := "false"
	optional := "optional"
	return []Migration{
		{Version: 1, Name: "HIKYO_MCP_WRITE_ENABLED", Default: &disabled},
		// #760: an upgraded instance keeps today's posture — a password login on
		// an unenrolled account still mints a session — until an operator opts in.
		// Fresh installs default to `required` in config parsing (greenfield strict).
		{Version: 2, Name: "HIKYO_SECOND_FACTOR", Default: &optional},
	}
}
