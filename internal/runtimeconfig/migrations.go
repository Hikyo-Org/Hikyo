package runtimeconfig

// Migration adds one release-owned setting. Versions and defaults are immutable
// once released. A nil Default means operator input is required; empty string is
// an explicit default, never absence. The declaration remains in Catalogue.
type Migration struct {
	Version int64
	Name    string
	Default *string
}

const MigrationVersion int64 = 1

func Migrations() []Migration {
	disabled := "false"
	return []Migration{{Version: 1, Name: "HIKYO_MCP_WRITE_ENABLED", Default: &disabled}}
}
