package config

import (
	"encoding/json"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

const ManagedBootstrapSourcesKey = "HIKYO_BOOTSTRAP_SOURCES"

// ManagedBootstrapSources selects installation-owned aliases. It cannot carry
// a locator, path, credential or root key. Enrollment determines their custody.
type ManagedBootstrapSources struct {
	Topology       domain.SingletonTopology `json:"topology,omitempty"`
	Version        int                      `json:"version"`
	DatabaseSource string                   `json:"database_source,omitempty"`
	RootSource     string                   `json:"root_source,omitempty"`
	UpgradeSource  string                   `json:"upgrade_source,omitempty"`
}

func ParseManagedBootstrapSources(raw string) (ManagedBootstrapSources, error) {
	refusal := errors.New("HIKYO_BOOTSTRAP_SOURCES: invalid source aliases")
	if err := validateManagedJSON(raw); err != nil {
		return ManagedBootstrapSources{}, refusal
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || len(fields) < 2 || len(fields) > 5 {
		return ManagedBootstrapSources{}, refusal
	}
	for key, value := range fields {
		switch key {
		case "version":
			if string(value) != "1" {
				return ManagedBootstrapSources{}, refusal
			}
		case "topology":
			var topologyFields map[string]json.RawMessage
			var topology domain.SingletonTopology
			if json.Unmarshal(value, &topologyFields) != nil || len(topologyFields) != 2 || topologyFields["ha"] == nil || topologyFields["node_id"] == nil || json.Unmarshal(value, &topology) != nil || !ValidManagedNodeID(topology.NodeID) || (string(topologyFields["ha"]) != "true" && string(topologyFields["ha"]) != "false") {
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
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out.Version != 1 || (out.DatabaseSource == "" && out.RootSource == "" && out.Topology.NodeID == "" && out.UpgradeSource == "") {
		return ManagedBootstrapSources{}, refusal
	}
	return out, nil
}

func (s ManagedBootstrapSources) MarshalJSON() ([]byte, error) {
	type sources struct {
		UpgradeSource  string                    `json:"upgrade_source,omitempty"`
		Version        int                       `json:"version"`
		DatabaseSource string                    `json:"database_source,omitempty"`
		RootSource     string                    `json:"root_source,omitempty"`
		Topology       *domain.SingletonTopology `json:"topology,omitempty"`
	}
	out := sources{UpgradeSource: s.UpgradeSource, Version: s.Version, DatabaseSource: s.DatabaseSource, RootSource: s.RootSource}
	if s.Topology.NodeID != "" {
		topology := s.Topology
		out.Topology = &topology
	}
	return json.Marshal(out)
}
