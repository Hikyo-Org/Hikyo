package runtimeconfig

import (
	"errors"
	"maps"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/config"
)

var ErrNodeNotConfigured = errors.New("managed configuration has no entry for this node")

// EncodeNodeOverrides creates one canonical encrypted project-cell value for
// the independently captured nodes. It never infers one node from another.
func EncodeNodeOverrides(nodes map[string]map[string]string) (string, error) {
	return config.EncodeManagedNodeOverrides(nodes)
}

// HasNodeValues distinguishes a legacy owner-only bundle from one that declares
// managed node configuration. A declared document always requires exact nodes.
func (b *Bundle) HasNodeValues() bool { return b.nodeValues != nil }

// NodeValues returns one independent complete projection, never another node's
// settings. Missing nodes are explicit errors, including in an owner-only bundle.
func (b *Bundle) NodeValues(nodeID string) (map[string]string, error) {
	values, exists := b.nodeValues[nodeID]
	if !exists {
		return nil, ErrNodeNotConfigured
	}
	return maps.Clone(values), nil
}

// ValidateNodeMembership binds a candidate to the exact admitted participants.
// Neither unknown candidate nodes nor omitted admitted nodes are acceptable.
func (b *Bundle) ValidateNodeMembership(admittedNodeIDs []string) error {
	if len(admittedNodeIDs) == 0 || len(admittedNodeIDs) != len(b.nodeValues) {
		return errors.New("managed node membership differs from admitted participants")
	}
	seen := make(map[string]bool, len(admittedNodeIDs))
	for _, id := range admittedNodeIDs {
		if !config.ValidManagedNodeID(id) || seen[id] {
			return errors.New("invalid admitted node membership")
		}
		seen[id] = true
		if _, exists := b.nodeValues[id]; !exists {
			return errors.New("managed node membership differs from admitted participants")
		}
	}
	return nil
}

// PrepareForNode validates the complete candidate using this node's bootstrap
// identity and engine. Resource construction and activation remain app-owned.
func PrepareForNode(values map[string]string, base *config.Config, nodeID string) (*Bundle, error) {
	bundle, err := Prepare(values)
	if err != nil {
		return nil, err
	}
	if topology := bundle.BootstrapSources().Topology; topology.NodeID != "" {
		nodeID = topology.NodeID
	}
	node, err := bundle.NodeValues(nodeID)
	if err != nil {
		return nil, err
	}
	effective, err := config.ApplyManagedOwnerAndNodeValues(base, bundle.ownerValues, node)
	if err != nil {
		return nil, err
	}
	if _, err := admission.New(admission.Config{BudgetMiB: effective.AdmissionBudgetMiB, ArgonMemoryKiB: effective.Argon2MemoryKiB}); err != nil {
		return nil, errors.New("managed Argon2 memory exceeds this node's admission budget")
	}
	return bundle, nil
}
