package config

import (
	"fmt"
	"io"
	"maps"
	"os"
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
	for _, key := range bootstrapManagedKeys {
		if value := getenv(key); value != "" {
			inputs[key] = value
		}
	}
	cfg, warnings, err := Load(subcommand, args, func(key string) string {
		if key == "HIKYO_UPDATE_CHANNEL" {
			return ""
		}
		return getenv(key)
	}, environ)
	if err != nil {
		return nil, warnings, err
	}
	cfg.ManagedInputs = inputs
	return cfg, warnings, nil
}

// ManagedSeed resolves one-time file imports. Call only for an unmanaged
// instance: an existing binding must not evaluate stale files or seed values.
// Error messages identify the setting, never its contents or filesystem path.
func (c *Config) ManagedSeed() (map[string]string, error) {
	values := maps.Clone(c.ManagedInputs)
	if values == nil {
		values = make(map[string]string)
	}
	if values["HIKYO_UPDATE_CHANNEL"] == "" {
		values["HIKYO_UPDATE_CHANNEL"] = c.UpdateChannel
		if values["HIKYO_UPDATE_CHANNEL"] == "" {
			values["HIKYO_UPDATE_CHANNEL"] = "stable"
		}
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
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 65536 {
		return "", fmt.Errorf("invalid seed file")
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
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil {
		return "", err
	}
	if len(data) > 65536 {
		return "", fmt.Errorf("seed file grew beyond limit")
	}
	return string(data), nil
}
