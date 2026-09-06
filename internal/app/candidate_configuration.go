package app

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/scanning"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

// checkCandidateConfiguration validates local constructor inputs before the
// gate can mark a migrated candidate healthy. No runtime DB, listener, worker,
// secret-provider call or new key hierarchy is available here.
func checkCandidateConfiguration(ctx context.Context, cfg *config.Config, projection *upgrade.CandidateConfiguration, values map[string]string) error {
	return checkCandidateConfigurationFromSources(ctx, cfg, projection, values, deploymentSourcesDirectory)
}

func checkCandidateConfigurationFromSources(ctx context.Context, cfg *config.Config, projection *upgrade.CandidateConfiguration, values map[string]string, sourcesDirectory string) error {
	var bundle *runtimeconfig.Bundle
	var err error
	// Both initial seed and missing-node recovery use the same lazy, captured
	// node projection. An existing configured node must never inspect stale
	// startup candidate files, even while another node is being added.
	var nodeOnce sync.Once
	var seedNode map[string]string
	var seedErr error
	readSeedNode := func() (map[string]string, error) {
		nodeOnce.Do(func() {
			var sources nextRootSources
			if cfg.NewRootKeyFile != "" {
				sources, seedErr = loadEnrolledRootSources(cfg, sourcesDirectory)
				if seedErr != nil {
					return
				}
			}
			seedNode, seedErr = seedNodeWithNextRoot(cfg, sources)
		})
		return seedNode, seedErr
	}
	readSeed := func() (map[string]string, error) {
		node, err := readSeedNode()
		if err != nil {
			return nil, err
		}
		return cfg.ManagedSeedForNode(node)
	}
	if projection != nil {
		if projection.SchemaVersion != runtimeconfig.SchemaVersion {
			return errors.New("candidate configuration schema is incompatible")
		}
		expected := runtimeconfig.Catalogue()
		if len(projection.Catalogue) != len(expected) {
			return errors.New("candidate configuration catalogue has changed")
		}
		byName := make(map[string]upgrade.CandidateConfigurationKey, len(expected))
		for _, key := range projection.Catalogue {
			byName[key.Name] = key
		}
		for _, key := range expected {
			stored, ok := byName[key.Name]
			if !ok || stored.Classification != string(key.Classification) || stored.RequiredMode != string(schema.PresenceNone) || stored.ForbiddenMode != string(schema.PresenceNone) || stored.GroupID != "" || stored.FolderPath != "" {
				return errors.New("candidate configuration catalogue has changed")
			}
			compiled, err := schema.CompileClassified(key.Classification, key.Declaration)
			if err != nil {
				return err
			}
			canonical, err := compiled.Canonical()
			if err != nil {
				return err
			}
			if string(canonical) != stored.Declaration {
				return errors.New("candidate configuration declaration has changed")
			}
		}
		bundle, err = runtimeconfig.Prepare(values)
	} else {
		node, seedErr := readSeedNode()
		if seedErr != nil {
			return seedErr
		}
		seed, seedErr := readSeed()
		if seedErr != nil {
			return seedErr
		}
		nodeID := cfg.NodeID
		if nodeID == "" {
			nodeID = "local"
		}
		seed[config.ManagedNodeOverridesKey], seedErr = runtimeconfig.EncodeNodeOverrides(map[string]map[string]string{nodeID: node})
		if seedErr != nil {
			return seedErr
		}
		bundle, err = runtimeconfig.Prepare(seed)
	}
	if err != nil {
		return err
	}
	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = "local"
	}
	coordinator := &service.SelfConfig{NodeID: nodeID, SeedNode: readSeedNode, Seed: readSeed}
	cfg, _, _, _, err = bootNodeConfiguration(cfg, coordinator, bundle)
	if err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if cfg.NewRootSource != "" {
		sources, err := loadEnrolledRootSources(cfg, sourcesDirectory)
		if err != nil {
			return err
		}
		root, err := sources.rootSource(cfg.NewRootSource)
		crypto.Zero(root)
		if err != nil {
			return errors.New("HIKYO_NEW_ROOT_SOURCE: candidate root source is unavailable or invalid")
		}
	}
	if _, _, err := AuthComponents(cfg); err != nil {
		return fmt.Errorf("authentication configuration: %w", err)
	}
	if _, err := scanning.Load(); err != nil {
		return fmt.Errorf("secret-scanning ruleset: %w", err)
	}
	if _, err := parseCIDRs(cfg.TrustedProxyCIDRs); err != nil {
		return err
	}
	auth := service.Auth{ExternalOrigin: cfg.ExternalOrigin}
	if err := auth.ConfigureWebAuthnRP(); err != nil {
		return fmt.Errorf("WebAuthn relying-party configuration: %w", err)
	}
	// ApplyManagedOwnerAndNodeValues above validates the effective TLS content.
	// An existing node overlay does not consult retired bootstrap sources.
	return ctx.Err()
}
