package config

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
)

// bootstrapManagedKeys includes startup file references as well as the
// managed value names. File contents become ordinary encrypted project cells
// during adoption; paths never become runtime configuration.
var bootstrapManagedKeys = []string{
	"HIKYO_MAIL_ADDR", "HIKYO_MAIL_TLS", "HIKYO_MAIL_USER",
	"HIKYO_MAIL_PASSWORD", "HIKYO_MAIL_PASSWORD_FILE", "HIKYO_MAIL_FROM",
	"HIKYO_MAIL_EHLO", "HIKYO_MAIL_ALLOWED_CIDRS", "HIKYO_MAIL_CA_FILE",
	"HIKYO_UPDATE_CHANNEL",
}

// LoadBootstrap parses the external settings needed to open the instance.
// Managed settings are validated later, after their authority is known. Load
// remains available to callers that explicitly validate a standalone seed.
func LoadBootstrap(subcommand string, args []string, getenv func(string) string, environ []string) (*Config, []string, error) {
	inputs := make(map[string]string)
	seedKeys := append(slices.Clone(bootstrapManagedKeys), ManagedOwnerKeys()...)
	nodeInputs := make(map[string]string)
	for _, key := range managedNodeInputKeys() {
		if value := getenv(key); value != "" {
			nodeInputs[key] = value
		}
	}
	for _, key := range seedKeys {
		if value := getenv(key); value != "" {
			inputs[key] = value
		}
	}
	cfg, warnings, err := load(subcommand, args, func(key string) string {
		if slices.Contains(seedKeys, key) || slices.Contains(managedNodeInputKeys(), key) {
			return ""
		}
		return getenv(key)
	}, environ, true)
	if err != nil {
		return nil, warnings, err
	}
	cfg.ManagedInputs = inputs
	for key, value := range cfg.ManagedNodeInputs {
		nodeInputs[key] = value
	}
	cfg.ManagedNodeInputs = nodeInputs
	return cfg, warnings, nil
}

// ManagedSeed resolves one-time file imports. Call only for an unmanaged
// instance: an existing binding must not evaluate stale files or seed values.
// Error messages identify the setting, never its contents or filesystem path.
func (c *Config) ManagedSeed() (map[string]string, error) {
	node, err := c.SeedNodeValues()
	if err != nil {
		return nil, err
	}
	return c.ManagedSeedForNode(node)
}

// ManagedSeedForNode reuses an exact already-captured node seed. HA aggregation
// calls SeedNodeValues once, then this method, so file edits cannot split one
// node's owner seed and encrypted node projection across different bytes.
func (c *Config) ManagedSeedForNode(nodeValues map[string]string) (map[string]string, error) {
	if _, present := nodeValues["HIKYO_TRUSTED_PROXY_CIDRS"]; !present {
		return nil, errors.New("node seed requires an explicit trusted proxy policy")
	}
	base, err := parseManagedNodeValues(c, nodeValues)
	if err != nil {
		return nil, err
	}

	values := maps.Clone(c.ManagedInputs)
	delete(values, "HIKYO_TRUSTED_PROXY_CIDRS")
	if values == nil {
		values = make(map[string]string)
	}
	if values["HIKYO_UPDATE_CHANNEL"] == "" {
		values["HIKYO_UPDATE_CHANNEL"] = c.UpdateChannel
		if values["HIKYO_UPDATE_CHANNEL"] == "" {
			values["HIKYO_UPDATE_CHANNEL"] = "stable"
		}
	}
	ownerValues := c.ManagedOwnerValues()
	if c.ManagedInputs != nil {
		delete(ownerValues, "HIKYO_EXTERNAL_ORIGIN")
	}
	// Manually constructed configurations used by host workflows may have zero
	// owner fields. Resolve absent defaults through the same parser first.
	if c.Argon2MemoryKiB == 0 {
		defaults, err := ApplyManagedOwnerValues(base, nil)
		if err != nil {
			return nil, err
		}
		ownerValues = defaults.ManagedOwnerValues()
	}
	for _, key := range ManagedOwnerKeys() {
		if value, present := values[key]; present {
			ownerValues[key] = value
		}
		delete(values, key)
	}
	effective, err := ApplyManagedOwnerAndNodeValues(base, ownerValues, nodeValues)
	if err != nil {
		return nil, err
	}
	for key, value := range effective.ManagedOwnerValues() {
		values[key] = value
	}
	for _, source := range []struct{ file, value string }{
		{"HIKYO_MAIL_PASSWORD_FILE", "HIKYO_MAIL_PASSWORD"},
		{"HIKYO_MAIL_CA_FILE", "HIKYO_MAIL_CA_PEM"},
	} {
		path := values[source.file]
		delete(values, source.file)
		if path == "" {
			continue
		}
		if values[source.value] != "" {
			return nil, fmt.Errorf("%s and %s cannot both be configured", source.file, source.value)
		}
		content, err := readManagedSeedFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: cannot import a regular file of at most 65536 bytes", source.file)
		}
		values[source.value] = content
	}
	return values, nil
}

func readManagedSeedFile(path string) (string, error) {
	return readManagedSeedFileMode(path, false)
}

// Check permissions on the opened descriptor as well as the initial path so
// replacing a startup TLS key between stat and open cannot weaken its policy.
func readManagedSeedFileMode(path string, privateKey bool) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 65536 {
		return "", fmt.Errorf("invalid seed file")
	}
	if privateKey && info.Mode().Perm() != 0400 && info.Mode().Perm() != 0600 {
		return "", errors.New("TLS key file requires mode 0400 or 0600")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 65536 {
		return "", fmt.Errorf("invalid seed file")
	}
	if privateKey && info.Mode().Perm() != 0400 && info.Mode().Perm() != 0600 {
		return "", errors.New("TLS key file requires mode 0400 or 0600")
	}
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil {
		return "", err
	}
	if len(data) > 65536 {
		return "", fmt.Errorf("seed file grew beyond limit")
	}
	return string(data), nil
}
