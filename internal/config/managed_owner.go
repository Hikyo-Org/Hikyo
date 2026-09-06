package config

import (
	"errors"
	"maps"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ManagedOwnerKeys returns the application settings shared by one logical
// owner. Bootstrap and node settings require separate activation plans.
func ManagedOwnerKeys() []string {
	var keys []string
	for _, descriptor := range VariableInventory() {
		if descriptor.Audience == VariableServer && descriptor.Scope == VariableOwner && descriptor.Activation == VariableAppReload {
			keys = append(keys, descriptor.Key)
		}
	}
	return keys
}

// ManagedOwnerValues exports effective application settings, including defaults.
// Mail and release-channel values have their own runtime components and are not
// included. A disabled backup policy omits schedule knobs that require a policy.
func (c *Config) ManagedOwnerValues() map[string]string {
	values := map[string]string{
		"HIKYO_ARGON2_MEMORY_KIB":          strconv.FormatUint(uint64(c.Argon2MemoryKiB), 10),
		"HIKYO_ARGON2_TIME":                strconv.FormatUint(uint64(c.Argon2Time), 10),
		"HIKYO_ARGON2_PARALLELISM":         strconv.FormatUint(uint64(c.Argon2Parallelism), 10),
		"HIKYO_AUDIT_ACCESS_RETAIN_DAYS":   strconv.Itoa(c.AuditAccessRetainDays),
		"HIKYO_AUDIT_SECURITY_RETAIN_DAYS": strconv.Itoa(c.AuditSecurityRetainDays),
		"HIKYO_REAUTH_WINDOW_SECONDS":      strconv.FormatInt(int64(c.ReauthWindow/time.Second), 10),
		"HIKYO_MCP_ENABLED":                strconv.FormatBool(c.MCPEnabled),
		"HIKYO_BACKUP_RTO_TARGET":          c.BackupRTOTarget.String(),
	}
	for key, value := range map[string]string{
		"HIKYO_EXTERNAL_ORIGIN":     c.ExternalOrigin,
		"HIKYO_DIRECTORY_PROXY":     c.DirectoryProxy,
		"HIKYO_MCP_ALLOWED_ORIGINS": strings.Join(c.MCPAllowedOrigins, ","),
		"HIKYO_BACKUP_RECIPIENTS":   strings.Join(c.BackupRecipients, ","),
	} {
		if value != "" {
			values[key] = value
		}
	}
	if c.BackupScheduled() {
		values["HIKYO_BACKUP_INTERVAL"] = c.BackupInterval.String()
		values["HIKYO_BACKUP_RPO"] = c.BackupRPO.String()
		values["HIKYO_BACKUP_RETAIN_COUNT"] = strconv.Itoa(c.BackupRetainCount)
		values["HIKYO_BACKUP_RETAIN_DAYS"] = strconv.Itoa(c.BackupRetainDays)
	}
	return values
}

// ApplyManagedOwnerValues validates a complete owner configuration with the
// actual node's deployment context. Missing owner keys use documented defaults,
// never stale process environment. Validation reuses the startup parser with
// explicit synthetic datastore metadata and no file-source inputs: it does not
// open databases, files, listeners, or network connections. The returned config
// preserves node/bootstrap fields and owns independent copies of mutable data.
func ApplyManagedOwnerValues(base *Config, values map[string]string) (*Config, error) {
	if base == nil {
		return nil, errors.New("managed owner configuration requires explicit node context")
	}
	return applyManagedOwnerValues(base, values, true)
}

// ValidateManagedOwnerValues checks syntax and cross-owner dependencies without
// claiming a node can activate the configuration. Listener trust, development
// origin permission and backup destination checks require ApplyManagedOwnerValues
// with the actual node. Cryptographic policy and capacity checks belong to their
// owning runtime components; config remains a parsing-only leaf package.
func ValidateManagedOwnerValues(values map[string]string) error {
	_, err := applyManagedOwnerValues(&Config{}, values, false)
	return err
}

// ManagedOwnerPolicy contains parsed inputs for the runtime cryptographic policy
// validators. It has no bootstrap context and cannot construct an application.
type ManagedOwnerPolicy struct {
	Argon2MemoryKiB   uint32
	Argon2Time        uint32
	Argon2Parallelism uint8
	BackupRecipients  []string
}

// ParseManagedOwnerPolicy returns independently parsed policy inputs so runtime
// owners can enforce their cryptographic floors and recipient rules.
func ParseManagedOwnerPolicy(values map[string]string) (ManagedOwnerPolicy, error) {
	parsed, err := applyManagedOwnerValues(&Config{}, values, false)
	if err != nil {
		return ManagedOwnerPolicy{}, err
	}
	return ManagedOwnerPolicy{Argon2MemoryKiB: parsed.Argon2MemoryKiB, Argon2Time: parsed.Argon2Time, Argon2Parallelism: parsed.Argon2Parallelism, BackupRecipients: slices.Clone(parsed.BackupRecipients)}, nil
}

func applyManagedOwnerValues(base *Config, values map[string]string, validateNode bool) (*Config, error) {
	keys := ManagedOwnerKeys()
	for key, value := range values {
		if !slices.Contains(keys, key) {
			return nil, errors.New("managed owner configuration contains an unsupported key")
		}
		if value == "" {
			return nil, errors.New("managed owner configuration values must be absent or nonempty")
		}
	}
	inputs := maps.Clone(values)
	if inputs == nil {
		inputs = make(map[string]string)
	}
	// A fixed parser-only datastore prevents any path/DSN from becoming owner
	// input. Load parses this descriptor but never opens it.
	inputs["HIKYO_DB"] = "sqlite:managed-owner-validation"
	inputs["HIKYO_LISTEN"] = base.Listen
	inputs["HIKYO_OPERATIONAL_LISTEN"] = base.OperationalListen
	inputs["HIKYO_TRUSTED_PROXY_CIDRS"] = strings.Join(base.TrustedProxyCIDRs, ",")
	inputs["HIKYO_TLS_CERT_FILE"] = base.TLSCertFile
	inputs["HIKYO_TLS_KEY_FILE"] = base.TLSKeyFile
	// The parser checks TLS presence, not files. Managed content has already
	// passed key-pair validation; these markers are never returned or read.
	if base.TLSCertPEM != "" && base.TLSKeyPEM != "" {
		inputs["HIKYO_TLS_CERT_FILE"], inputs["HIKYO_TLS_KEY_FILE"] = "managed-content", "managed-content"
	}
	inputs["HIKYO_BACKUP_DIR"] = base.BackupDir
	if base.AdmissionBudgetMiB != 0 {
		inputs["HIKYO_ADMISSION_BUDGET_MIB"] = strconv.Itoa(base.AdmissionBudgetMiB)
	}
	var args []string
	if base.Dev {
		args = []string{"--dev"}
	}
	if !validateNode {
		// The startup parser also checks deployment-dependent constraints. These
		// parser-only inputs select their least restrictive cases; no node config
		// produced by this path can escape ValidateManagedOwnerValues.
		args = []string{"--dev"}
		inputs["HIKYO_BACKUP_DIR"] = "managed-owner-validation"
		inputs["HIKYO_REAUTH_WINDOW_SECONDS"] = values["HIKYO_REAUTH_WINDOW_SECONDS"]
		if inputs["HIKYO_REAUTH_WINDOW_SECONDS"] == "" {
			inputs["HIKYO_REAUTH_WINDOW_SECONDS"] = "0"
		}
	}
	parsed, _, err := Load("server", args, func(key string) string { return inputs[key] }, nil)
	if err != nil {
		return nil, errors.New("managed owner configuration is invalid for this node")
	}
	result := *base
	result.ManagedInputs = maps.Clone(base.ManagedInputs)
	result.ManagedNodeInputs = maps.Clone(base.ManagedNodeInputs)
	result.AdapterEgressPolicy = clonePrefixPolicy(base.AdapterEgressPolicy)
	result.OIDCEgressPolicy = clonePrefixPolicy(base.OIDCEgressPolicy)
	result.DynamicEgressPolicy = clonePrefixPolicy(base.DynamicEgressPolicy)
	result.Argon2MemoryKiB, result.Argon2Time, result.Argon2Parallelism = parsed.Argon2MemoryKiB, parsed.Argon2Time, parsed.Argon2Parallelism
	result.ReauthWindow = parsed.ReauthWindow
	result.TrustedProxyCIDRs = slices.Clone(parsed.TrustedProxyCIDRs)
	result.ExternalOrigin, result.DirectoryProxy = parsed.ExternalOrigin, parsed.DirectoryProxy
	result.MCPEnabled, result.MCPAllowedOrigins = parsed.MCPEnabled, slices.Clone(parsed.MCPAllowedOrigins)
	result.BackupRecipients = slices.Clone(parsed.BackupRecipients)
	result.BackupInterval, result.BackupRPO = parsed.BackupInterval, parsed.BackupRPO
	result.BackupRetainCount, result.BackupRetainDays = parsed.BackupRetainCount, parsed.BackupRetainDays
	result.BackupRTOTarget = parsed.BackupRTOTarget
	result.AuditAccessRetainDays, result.AuditSecurityRetainDays = parsed.AuditAccessRetainDays, parsed.AuditSecurityRetainDays
	return &result, nil
}

func clonePrefixPolicy(policy map[string][]netip.Prefix) map[string][]netip.Prefix {
	if policy == nil {
		return nil
	}
	result := make(map[string][]netip.Prefix, len(policy))
	for key, values := range policy {
		result[key] = slices.Clone(values)
	}
	return result
}
