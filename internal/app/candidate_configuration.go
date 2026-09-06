package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/config"
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
	var bundle *runtimeconfig.Bundle
	var err error
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
		var seed map[string]string
		seed, err = cfg.ManagedSeed()
		if err == nil {
			bundle, err = runtimeconfig.Prepare(seed)
		}
	}
	if err != nil {
		return err
	}
	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = "local"
	}
	coordinator := &service.SelfConfig{NodeID: nodeID, SeedNode: cfg.SeedNodeValues, Seed: cfg.ManagedSeed}
	cfg, _, _, _, err = bootNodeConfiguration(cfg, coordinator, bundle)
	if err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
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
