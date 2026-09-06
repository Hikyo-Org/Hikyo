package config

import (
	"encoding/json"
	"errors"
	"strings"
)

// ValidManagedNodeID accepts stable node names without patterns, whitespace,
// traversal syntax or control characters. Membership is a separate service-owned
// check against the exact admitted HA participant set.
func ValidManagedNodeID(id string) bool {
	if len(id) == 0 || len(id) > 128 || strings.Contains(id, "..") {
		return false
	}
	for index, char := range id {
		alphanumeric := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9'
		if !alphanumeric && (index == 0 || char != '-' && char != '_' && char != '.') {
			return false
		}
	}
	return true
}

// ParseManagedNodeOverrides decodes the only supported node document format.
// Every supplied value is a string; JSON null is never an empty-string alias.
// Duplicate fields, unknown outer fields and unknown node settings are refused.
func ParseManagedNodeOverrides(raw string) (map[string]map[string]string, error) {
	refusal := errors.New("HIKYO_NODE_OVERRIDES: invalid versioned node configuration")
	if err := validateManagedJSON(raw); err != nil {
		return nil, refusal
	}
	var outer map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &outer); err != nil || len(outer) != 2 || outer["version"] == nil || outer["nodes"] == nil {
		return nil, refusal
	}
	var document struct {
		Version int                                   `json:"version"`
		Nodes   map[string]map[string]json.RawMessage `json:"nodes"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil || document.Version != 1 || len(document.Nodes) == 0 {
		return nil, refusal
	}
	nodes := make(map[string]map[string]string, len(document.Nodes))
	for id, encoded := range document.Nodes {
		if !ValidManagedNodeID(id) || encoded == nil {
			return nil, refusal
		}
		values := make(map[string]string, len(encoded))
		for key, raw := range encoded {
			trimmed := strings.TrimSpace(string(raw))
			if len(trimmed) == 0 || trimmed[0] != '"' {
				return nil, refusal
			}
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, refusal
			}
			values[key] = value
		}
		if err := ValidateManagedNodeValues(values); err != nil {
			return nil, refusal
		}
		nodes[id] = values
	}
	return nodes, nil
}

// EncodeManagedNodeOverrides produces deterministic JSON from complete node
// projections. Validation uses the same closed parser as published candidates.
func EncodeManagedNodeOverrides(nodes map[string]map[string]string) (string, error) {
	for id, values := range nodes {
		if !ValidManagedNodeID(id) {
			return "", errors.New("HIKYO_NODE_OVERRIDES: invalid node identity")
		}
		if err := ValidateManagedNodeValues(values); err != nil {
			return "", err
		}
	}
	document := struct {
		Version int                          `json:"version"`
		Nodes   map[string]map[string]string `json:"nodes"`
	}{Version: 1, Nodes: nodes}
	raw, err := json.Marshal(document)
	if err != nil {
		return "", errors.New("HIKYO_NODE_OVERRIDES: cannot encode node configuration")
	}
	if _, err := ParseManagedNodeOverrides(string(raw)); err != nil {
		return "", err
	}
	return string(raw), nil
}
