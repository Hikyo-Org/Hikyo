package config

import (
	"encoding/json"
	"errors"
)

const ManagedBootstrapSourcesKey = "HIKYO_BOOTSTRAP_SOURCES"

// ManagedBootstrapSources selects installation-owned aliases. It cannot carry
// a locator, path, credential or root key. Enrollment determines their custody.
type ManagedBootstrapSources struct {
	Version        int    `json:"version"`
	DatabaseSource string `json:"database_source,omitempty"`
	RootSource     string `json:"root_source,omitempty"`
	UpgradeSource  string `json:"upgrade_source,omitempty"`
}

func ParseManagedBootstrapSources(raw string) (ManagedBootstrapSources, error) {
	refusal := errors.New("HIKYO_BOOTSTRAP_SOURCES: invalid source aliases")
	if err := validateManagedJSON(raw); err != nil {
		return ManagedBootstrapSources{}, refusal
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || len(fields) < 2 || len(fields) > 4 {
		return ManagedBootstrapSources{}, refusal
	}
	for key, value := range fields {
		switch key {
		case "version":
			if string(value) != "1" {
				return ManagedBootstrapSources{}, refusal
			}
		case "database_source", "root_source", "upgrade_source":
			var alias string
			if err := json.Unmarshal(value, &alias); err != nil || !ValidManagedNodeID(alias) {
				return ManagedBootstrapSources{}, refusal
			}
		default:
			return ManagedBootstrapSources{}, refusal
		}
	}
	var out ManagedBootstrapSources
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out.Version != 1 || (out.DatabaseSource == "" && out.RootSource == "" && out.UpgradeSource == "") {
		return ManagedBootstrapSources{}, refusal
	}
	return out, nil
}
