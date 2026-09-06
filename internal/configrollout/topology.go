package configrollout

import (
	"context"
	"slices"
	"strconv"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func validTopologyEnrollment(t Target) bool {
	seen := map[string]bool{}
	for _, id := range t.TopologyNodeIDs {
		if !config.ValidManagedNodeID(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return len(seen) == 0 || len(seen) <= 32 && seen[t.StableNodeID]
}
func (k *Kubernetes) allowedNodeID(id string) bool {
	return id == k.target.StableNodeID || slices.Contains(k.target.TopologyNodeIDs, id)
}
func (k *Kubernetes) validTopology(change *domain.SingletonTopologyChange) bool {
	if change == nil {
		return true
	}
	return len(k.target.TopologyNodeIDs) > 0 && validTopologyEnrollment(k.target) && k.allowedNodeID(change.Before.NodeID) && k.allowedNodeID(change.After.NodeID)
}
func (k *Kubernetes) PrepareTopology(ctx context.Context, intent Intent, change domain.SingletonTopologyChange) (*Plan, error) {
	if !k.validTopology(&change) {
		return nil, ErrUnsupported
	}
	return k.prepare(ctx, intent, nil, nil, &change)
}
func topologyEnvironment(t domain.SingletonTopology) []corev1.EnvVar {
	return []corev1.EnvVar{{Name: string(HA), Value: strconv.FormatBool(t.HA)}, {Name: string(NodeID), Value: t.NodeID}}
}
func (k *Kubernetes) prepareTopologyDelta(d *appsv1.Deployment, p *planData) error {
	if p.Topology == nil {
		return nil
	}
	if !k.validTopology(p.Topology) || (p.Bootstrap != nil && p.Topology.Before != p.Topology.After) || len(p.Changes) != 0 || p.Replicas != 1 {
		return ErrUnsupported
	}
	c := container(d, k.target.Container)
	for i, want := range topologyEnvironment(p.Topology.After) {
		delta := envDelta{Name: want.Name, After: want}
		for _, e := range c.Env {
			if e.Name == want.Name {
				if delta.Before != nil {
					return ErrUnsupported
				}
				delta.Before = e.DeepCopy()
			}
		}
		before := topologyEnvironment(p.Topology.Before)[i]
		if delta.Before == nil {
			if want.Name != string(HA) || p.Topology.Before.HA {
				return ErrConflict
			}
		} else if delta.Before.ValueFrom != nil || delta.Before.Value != before.Value {
			return ErrConflict
		}
		if p.Bootstrap == nil {
			p.Delta.Environment = append(p.Delta.Environment, delta)
		}
	}
	return nil
}
func (k *Kubernetes) validTopologyDelta(p planData) bool {
	if p.Topology != nil && p.Bootstrap != nil {
		if !k.validTopology(p.Topology) || p.Topology.Before != p.Topology.After || len(p.Changes) != 0 || p.Replicas != 1 {
			return false
		}
		// Correspondence-only plans have exactly the ordinary source delta.
		// HA/NodeID mutations cannot be hidden among its environment entries.
		p.Topology = nil
		return k.validBootstrapDelta(p)
	}
	if !k.validTopology(p.Topology) || p.Topology == nil || p.Bootstrap != nil || len(p.Changes) != 0 || p.Replicas != 1 || len(p.Delta.Environment) != 2 || p.Delta.RootSource != nil || len(p.Delta.SourceAliases) != 0 {
		return false
	}
	for i, want := range topologyEnvironment(p.Topology.After) {
		e := p.Delta.Environment[i]
		before := topologyEnvironment(p.Topology.Before)[i]
		if e.Name != want.Name || digest(e.After) != digest(want) {
			return false
		}
		if e.Before == nil {
			if want.Name != string(HA) || p.Topology.Before.HA {
				return false
			}
		} else if digest(*e.Before) != digest(before) {
			return false
		}
	}
	return true
}
