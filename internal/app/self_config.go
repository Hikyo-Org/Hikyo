package app

import (
	"context"
	"encoding/json/v2"
	"errors"
	"maps"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func newSelfConfig(cfg *config.Config, db *store.DB, kr *crypto.Keyring, auth *service.Auth) *service.SelfConfig {
	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = "local"
	}
	coordinator := &service.SelfConfig{DB: db, Keyring: kr, Auth: auth, NodeID: nodeID, HAMode: cfg.HA}
	var once sync.Once
	var node map[string]string
	var seedErr error
	readNode := func() (map[string]string, error) {
		once.Do(func() {
			sources, _ := coordinator.Deployment.(nextRootSources)
			node, seedErr = seedNodeWithNextRoot(cfg, sources)
		})
		return maps.Clone(node), seedErr
	}
	coordinator.SeedNode = readNode
	coordinator.Seed = func() (map[string]string, error) {
		values, err := readNode()
		if err != nil {
			return nil, err
		}
		seed, err := cfg.ManagedSeedForNode(values)
		if err != nil {
			return nil, err
		}
		if coordinator.Deployment != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			sources, err := coordinator.Deployment.SeedSources(ctx)
			if err != nil {
				return nil, err
			}
			if sources.Version != 0 {
				raw, err := json.Marshal(sources, jsonwire.Stable)
				if err != nil {
					return nil, err
				}
				seed[config.ManagedBootstrapSourcesKey] = string(raw)
			}
		}
		return seed, nil
	}
	return coordinator
}

func bootNodeConfiguration(cfg *config.Config, coordinator *service.SelfConfig, bundle *runtimeconfig.Bundle) (*config.Config, map[string]string, map[string]string, bool, error) {
	owner := bundle.OwnerValues()
	var node map[string]string
	var err error
	missing := false
	if bundle.HasNodeValues() {
		node, err = bundle.NodeValues(coordinator.NodeID)
		if err != nil && !errors.Is(err, runtimeconfig.ErrNodeNotConfigured) {
			return nil, nil, nil, false, err
		}
		missing = errors.Is(err, runtimeconfig.ErrNodeNotConfigured)
	}
	if !bundle.HasNodeValues() || missing {
		node, err = coordinator.SeedNode()
		if err != nil {
			return nil, nil, nil, missing, err
		}
	}
	effective, err := config.ApplyManagedOwnerAndNodeValues(cfg, owner, node)
	if err != nil && missing {
		// An unconfigured joining node may need its own valid bootstrap auth
		// graph to enroll. Capture remains unavailable until an administrator
		// publishes a node projection compatible with the managed owner policy.
		seed, seedErr := coordinator.Seed()
		if seedErr != nil {
			return nil, nil, nil, true, errors.New("joining node requires valid bootstrap configuration for administrative recovery")
		}
		baseline, seedErr := runtimeconfig.Prepare(seed)
		if seedErr != nil {
			return nil, nil, nil, true, errors.New("joining node requires valid bootstrap configuration for administrative recovery")
		}
		// The seed is only a recovery baseline for everything else. The adopted
		// transport profile is a security property that must not be weakened on
		// this path: carry it so a node that cannot serve strict refuses to boot
		// instead of serving the seed's compat.
		adopted := owner[config.TLSProfileKey]
		owner = baseline.OwnerValues()
		if adopted != "" {
			owner[config.TLSProfileKey] = adopted
		}
		effective, err = config.ApplyManagedOwnerAndNodeValues(cfg, owner, node)
	}
	return effective, owner, node, missing, err
}
