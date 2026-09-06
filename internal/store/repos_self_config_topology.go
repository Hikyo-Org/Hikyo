package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// rolloutTopology reads only the closed, nonsecret membership correspondence.
// It persists in every command stage, including Observe and Restore.
func rolloutTopology(raw string) (*domain.SingletonTopologyChange, string, error) {
	var envelope struct {
		Command struct {
			Action   string                          `json:"action"`
			Topology *domain.SingletonTopologyChange `json:"topology"`
		} `json:"command"`
	}
	if json.Unmarshal([]byte(raw), &envelope) != nil {
		return nil, "", domain.ErrConflict
	}
	t := envelope.Command.Topology
	if t != nil && (t.Before.NodeID == "" || t.After.NodeID == "") {
		return nil, "", domain.ErrConflict
	}
	return t, envelope.Command.Action, nil
}
func (r selfConfigRepo) commitTopologyParticipant(ctx context.Context, j SelfConfigJob, nodes []SelfConfigNode, at time.Time) error {
	row, err := r.q.rollout(ctx, j.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	topology, action, err := rolloutTopology(row.CommandJSON)
	if err != nil {
		return err
	}
	if topology == nil {
		// A source-only replacement also retires the prior process stamp.
		if _, err := r.q.topology(ctx); err == nil {
			if err := r.q.lockMembership(ctx); err != nil {
				return err
			}
			return r.q.fenceTopologyLease(ctx, at)
		} else if !isNoRows(err) && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		return nil
	}
	if action != "submit" || row.PlanDigest == "" || len(nodes) != 1 || nodes[0].NodeID != topology.Before.NodeID {
		return domain.ErrConflict
	}
	if err := r.q.lockMembership(ctx); err != nil {
		return err
	}
	// A newly registered peer cannot turn a prepared singleton into a cluster.
	peers, err := r.q.participants(ctx, at.Add(-30*time.Second))
	if err != nil {
		return err
	}
	latest, latestErr := r.q.topology(ctx)
	if latestErr == nil {
		prior, action, err := rolloutTopology(latest)
		if err != nil || prior == nil {
			return domain.ErrConflict
		}
		expected := prior.After
		if action == "restore" {
			expected = prior.Before
		}
		if expected != topology.Before {
			return domain.ErrConflict
		}
	} else if isNoRows(latestErr) || errors.Is(latestErr, domain.ErrNotFound) {
		for _, peer := range peers {
			if peer != topology.Before.NodeID {
				return domain.ErrConflict
			}
		}
	} else {
		return latestErr
	}
	if err := r.q.fenceTopologyLease(ctx, at); err != nil {
		return err
	}
	node := nodes[0]
	node.NodeID = topology.After.NodeID
	node.ActiveGeneration = 0
	node.ActiveRevision = 0
	node.Prepared = false
	if err := r.q.deleteNodes(ctx); err != nil {
		return err
	}
	return r.q.putNode(ctx, node)
}
