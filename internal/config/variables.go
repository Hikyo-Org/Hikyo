package config

import "slices"

// VariableAudience identifies the consumer of a recognized environment key.
type VariableAudience string

const (
	VariableServer  VariableAudience = "server"
	VariableClient  VariableAudience = "client"
	VariableCommand VariableAudience = "command"
	VariableRetired VariableAudience = "retired"
)

// VariableScope distinguishes shared logical-owner values from local node inputs.
// A node-scoped file import may produce an owner-scoped managed value.
type VariableScope string

const (
	VariableOwner VariableScope = "owner"
	VariableNode  VariableScope = "node"
)

// VariableActivation describes the lifecycle a setting belongs to. It does not
// claim that the current build implements Apply for that lifecycle.
type VariableActivation string

const (
	VariableLive       VariableActivation = "live"
	VariableComponent  VariableActivation = "component"
	VariableAppReload  VariableActivation = "app-reload"
	VariableBootstrap  VariableActivation = "bootstrap"
	VariableDeployment VariableActivation = "deployment"
	VariableNone       VariableActivation = "none"
)

// VariableImport describes the source boundary, not permission to import a key.
// The active managed catalogue must independently admit each value. External
// inputs require their dedicated operator workflow; a generic importer must
// never read their contents.
type VariableImport string

const (
	VariableValue       VariableImport = "value"
	VariableFileContent VariableImport = "file-content"
	VariableExternal    VariableImport = "external"
)

// VariableDescriptor contains metadata only, never a configured value. Secret
// classifies the value or explicitly imported contents, including credentials
// embedded in compound values. For external file references it classifies only
// the path metadata. Root-key file references remain external; their private
// bytes have ReferencedContentSecret=true. TLS key sources import secret bytes
// once into the reviewed node document and therefore also have Secret=true.
// FileContentKey is set only for supported one-time content imports; the source
// path itself is never the imported value. Other filesystem references remain
// external until a dedicated content or deployment workflow owns them.
type VariableDescriptor struct {
	ManagedField            string             `json:"managedField,omitempty"`
	Key                     string             `json:"key"`
	Audience                VariableAudience   `json:"audience"`
	Scope                   VariableScope      `json:"scope"`
	Activation              VariableActivation `json:"activation"`
	Secret                  bool               `json:"secret"`
	Import                  VariableImport     `json:"import"`
	FileContentKey          string             `json:"fileContentKey,omitempty"`
	ReferencedContentSecret bool               `json:"referencedContentSecret,omitempty"`
	DevelopmentOnly         bool               `json:"developmentOnly,omitempty"`
}

// VariableInventory returns an independent, key-sorted copy of the complete
// recognized environment inventory. Its descriptors are values without mutable
// children, so callers cannot mutate the package's admission metadata.
func VariableInventory() []VariableDescriptor {
	return slices.Clone(variableInventory)
}

var variableInventory = []VariableDescriptor{
	{Key: "HIKYO_ADAPTER_EGRESS_POLICY_FILE", Audience: VariableServer, Scope: VariableNode, Activation: VariableAppReload, Secret: false, Import: VariableFileContent, FileContentKey: "HIKYO_ADAPTER_EGRESS_POLICY_JSON"},
	{Key: "HIKYO_ADMISSION_BUDGET_MIB", Audience: VariableServer, Scope: VariableNode, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_ARGON2_MEMORY_KIB", Audience: VariableServer, Scope: VariableOwner, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_ARGON2_PARALLELISM", Audience: VariableServer, Scope: VariableOwner, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_ARGON2_TIME", Audience: VariableServer, Scope: VariableOwner, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_AUDIT_ACCESS_RETAIN_DAYS", Audience: VariableServer, Scope: VariableOwner, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_AUDIT_SECURITY_RETAIN_DAYS", Audience: VariableServer, Scope: VariableOwner, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_BACKUP_DIR", Audience: VariableServer, Scope: VariableNode, Activation: VariableAppReload, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_BACKUP_INTERVAL", Audience: VariableServer, Scope: VariableOwner, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_BACKUP_RECIPIENTS", Audience: VariableServer, Scope: VariableOwner, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_BACKUP_RETAIN_COUNT", Audience: VariableServer, Scope: VariableOwner, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_BACKUP_RETAIN_DAYS", Audience: VariableServer, Scope: VariableOwner, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_BACKUP_RPO", Audience: VariableServer, Scope: VariableOwner, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_BACKUP_RTO_TARGET", Audience: VariableServer, Scope: VariableOwner, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_COMPOSE_DOCKER", Audience: VariableClient, Scope: VariableNode, Activation: VariableNone, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_CONTEXT", Audience: VariableClient, Scope: VariableNode, Activation: VariableNone, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_DB", Audience: VariableServer, Scope: VariableNode, Activation: VariableBootstrap, Secret: true, Import: VariableExternal},
	{Key: "HIKYO_DEV_ADAPTER_FAKE_PROVIDER", Audience: VariableServer, Scope: VariableNode, Activation: VariableAppReload, Secret: false, Import: VariableValue, DevelopmentOnly: true},
	{Key: "HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE", Audience: VariableServer, Scope: VariableNode, Activation: VariableAppReload, Secret: false, Import: VariableValue, DevelopmentOnly: true},
	{Key: "HIKYO_DEV_SERVICE_BUDGETS_DISABLED", Audience: VariableServer, Scope: VariableNode, Activation: VariableAppReload, Secret: false, Import: VariableValue, DevelopmentOnly: true},
	{Key: "HIKYO_DIRECTORY_PROXY", Audience: VariableServer, Scope: VariableOwner, Activation: VariableAppReload, Secret: true, Import: VariableValue},
	{Key: "HIKYO_DYNAMIC_EGRESS_POLICY_FILE", Audience: VariableServer, Scope: VariableNode, Activation: VariableAppReload, Secret: false, Import: VariableFileContent, FileContentKey: "HIKYO_DYNAMIC_EGRESS_POLICY_JSON"},
	{Key: "HIKYO_ENV", Audience: VariableClient, Scope: VariableNode, Activation: VariableNone, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_EXTERNAL_ORIGIN", Audience: VariableServer, Scope: VariableOwner, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_HA", ManagedField: "HIKYO_BOOTSTRAP_SOURCES.topology.ha", Audience: VariableServer, Scope: VariableOwner, Activation: VariableDeployment, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_INSTANCE", Audience: VariableClient, Scope: VariableNode, Activation: VariableNone, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_LISTEN", Audience: VariableServer, Scope: VariableNode, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_MAIL_ADDR", Audience: VariableServer, Scope: VariableOwner, Activation: VariableComponent, Secret: false, Import: VariableValue},
	{Key: "HIKYO_MAIL_ALLOWED_CIDRS", Audience: VariableServer, Scope: VariableOwner, Activation: VariableComponent, Secret: false, Import: VariableValue},
	{Key: "HIKYO_MAIL_CA_FILE", Audience: VariableServer, Scope: VariableNode, Activation: VariableComponent, Secret: false, Import: VariableFileContent, FileContentKey: "HIKYO_MAIL_CA_PEM"},
	{Key: "HIKYO_MAIL_EHLO", Audience: VariableServer, Scope: VariableOwner, Activation: VariableComponent, Secret: false, Import: VariableValue},
	{Key: "HIKYO_MAIL_FROM", Audience: VariableServer, Scope: VariableOwner, Activation: VariableComponent, Secret: false, Import: VariableValue},
	{Key: "HIKYO_MAIL_PASSWORD", Audience: VariableServer, Scope: VariableOwner, Activation: VariableComponent, Secret: true, Import: VariableValue},
	{Key: "HIKYO_MAIL_PASSWORD_FILE", Audience: VariableServer, Scope: VariableNode, Activation: VariableComponent, Secret: true, Import: VariableFileContent, FileContentKey: "HIKYO_MAIL_PASSWORD", ReferencedContentSecret: true},
	{Key: "HIKYO_MAIL_TLS", Audience: VariableServer, Scope: VariableOwner, Activation: VariableComponent, Secret: false, Import: VariableValue},
	{Key: "HIKYO_MAIL_USER", Audience: VariableServer, Scope: VariableOwner, Activation: VariableComponent, Secret: false, Import: VariableValue},
	{Key: "HIKYO_MCP_ALLOWED_ORIGINS", Audience: VariableServer, Scope: VariableOwner, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_MCP_ENABLED", Audience: VariableServer, Scope: VariableOwner, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_NEW_ROOT_KEY_FILE", Audience: VariableServer, Scope: VariableNode, Activation: VariableAppReload, Secret: false, Import: VariableExternal, ReferencedContentSecret: true},
	{Key: "HIKYO_NODE_ID", ManagedField: "HIKYO_BOOTSTRAP_SOURCES.topology.node_id", Audience: VariableServer, Scope: VariableNode, Activation: VariableDeployment, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_OIDC_EGRESS_POLICY_FILE", Audience: VariableServer, Scope: VariableNode, Activation: VariableAppReload, Secret: false, Import: VariableFileContent, FileContentKey: "HIKYO_OIDC_EGRESS_POLICY_JSON"},
	{Key: "HIKYO_OPERATIONAL_LISTEN", Audience: VariableServer, Scope: VariableNode, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_ORG", Audience: VariableClient, Scope: VariableNode, Activation: VariableNone, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_PG_POOL_MAX", Audience: VariableServer, Scope: VariableNode, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_PROJECT", Audience: VariableClient, Scope: VariableNode, Activation: VariableNone, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_REAUTH_WINDOW_SECONDS", Audience: VariableServer, Scope: VariableOwner, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_ROOT_KEY", Audience: VariableServer, Scope: VariableNode, Activation: VariableBootstrap, Secret: true, Import: VariableExternal},
	{Key: "HIKYO_ROOT_KEY_FILE", Audience: VariableServer, Scope: VariableNode, Activation: VariableBootstrap, Secret: false, Import: VariableExternal, ReferencedContentSecret: true},
	{Key: "HIKYO_STATE_DIR", Audience: VariableClient, Scope: VariableNode, Activation: VariableNone, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_TLS_CERT_FILE", Audience: VariableServer, Scope: VariableNode, Activation: VariableAppReload, Secret: false, Import: VariableFileContent, FileContentKey: "HIKYO_TLS_CERT_PEM"},
	{Key: "HIKYO_TLS_KEY_FILE", Audience: VariableServer, Scope: VariableNode, Activation: VariableAppReload, Secret: true, Import: VariableFileContent, FileContentKey: "HIKYO_TLS_KEY_PEM", ReferencedContentSecret: true},
	{Key: "HIKYO_TOKEN", Audience: VariableClient, Scope: VariableNode, Activation: VariableNone, Secret: true, Import: VariableExternal},
	{Key: "HIKYO_TRUSTED_PROXY_CIDRS", Audience: VariableServer, Scope: VariableNode, Activation: VariableAppReload, Secret: false, Import: VariableValue},
	{Key: "HIKYO_TRUST_BUNDLE", Audience: VariableClient, Scope: VariableNode, Activation: VariableNone, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_UPDATER_SOCKET", Audience: VariableRetired, Scope: VariableNode, Activation: VariableNone, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_UPDATE_CHANNEL", Audience: VariableServer, Scope: VariableOwner, Activation: VariableLive, Secret: false, Import: VariableValue},
	{Key: "HIKYO_UPGRADE_BACKUP", Audience: VariableServer, Scope: VariableNode, Activation: VariableBootstrap, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_UPGRADE_BUNDLE", Audience: VariableServer, Scope: VariableNode, Activation: VariableBootstrap, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_UPGRADE_EVIDENCE", Audience: VariableServer, Scope: VariableNode, Activation: VariableBootstrap, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_UPGRADE_LEGACY_WRITERS_STOPPED", Audience: VariableServer, Scope: VariableNode, Activation: VariableBootstrap, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_UPGRADE_OPERATOR_INSTANCE", Audience: VariableCommand, Scope: VariableNode, Activation: VariableNone, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY", Audience: VariableServer, Scope: VariableNode, Activation: VariableBootstrap, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_UPGRADE_STATE_DIR", Audience: VariableServer, Scope: VariableNode, Activation: VariableBootstrap, Secret: false, Import: VariableExternal},
	{Key: "HIKYO_UPGRADE_TARGET_MANIFEST", Audience: VariableServer, Scope: VariableNode, Activation: VariableBootstrap, Secret: false, Import: VariableExternal},
	{Key: "XDG_STATE_HOME", Audience: VariableClient, Scope: VariableNode, Activation: VariableNone, Secret: false, Import: VariableExternal},
}
