package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/Hikyo-Org/hikyo/internal/bridgerevocation"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/diagnostics"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
)

// guardBridges makes a bundle's plans consult the revocation list when the route
// uses a recovery bridge, so an export or drill cannot mint evidence for a
// revoked bridge.
func guardBridges(ctx context.Context, cfg *config.Config, bundle upgradebundle.Bundle) upgradebundle.Bundle {
	lookup, snapshot := bridgeRevocation(cfg), bundle.Snapshot()
	return bundle.WithBridgeRevocations(func() ([]releaseidentity.Digest, error) { return lookup(ctx, snapshot) })
}

// loadGuardedBundle loads and authenticates a bundle for a command that can mint or
// consume upgrade evidence, with the revocation guard attached. These commands
// never call upgradebundle.Load directly (a test enforces it), so none can skip it.
func loadGuardedBundle(ctx context.Context, cfg *config.Config, directory string, pinned releasetrust.PinnedTrust, floor releaseidentity.SnapshotFloor) (upgradebundle.Bundle, error) {
	bundle, err := upgradebundle.Load(ctx, directory, pinned, floor)
	if err != nil {
		return upgradebundle.Bundle{}, err
	}
	return guardBridges(ctx, cfg, bundle), nil
}

// bridgeRevocation builds the gate's lazy revocation lookup from configuration.
// A bad setting is not reported until a route actually needs a recovery bridge,
// where it refuses that route; ordinary upgrades never reach it.
func bridgeRevocation(cfg *config.Config) func(context.Context, releasetrust.Snapshot) ([]releaseidentity.Digest, error) {
	return func(ctx context.Context, snapshot releasetrust.Snapshot) ([]releaseidentity.Digest, error) {
		mode, err := bridgerevocation.ParseMode(cfg.Upgrade.RevocationCheck)
		if err != nil {
			return nil, err
		}
		if cfg.Upgrade.StateDirectory == "" {
			return nil, errors.New("a recovery bridge needs HIKYO_UPGRADE_STATE_DIR to record the revocation list floor")
		}
		checker := bridgerevocation.Checker{Mode: mode, Directory: cfg.Upgrade.StateDirectory}
		if mode == bridgerevocation.Online {
			address := cfg.Upgrade.RevocationURL
			if address == "" {
				address = bridgerevocation.DefaultURL
			}
			fetcher, err := bridgerevocation.NewHTTPFetcher(address)
			if err != nil {
				return nil, err
			}
			checker.Fetcher = fetcher
		}
		result, err := checker.Check(ctx, snapshot)
		if err != nil {
			return nil, err
		}
		if result.Outcome == bridgerevocation.Checked {
			diagnostics.Printf(ctx, 1, "Recovery bridge revocation check: %s (list sequence %d)", result.Outcome, result.Sequence)
		} else {
			// Relying on not_after alone must be visible at the default level.
			slog.WarnContext(ctx, "recovery bridge revocation list was not applied; relying on the bridge's not_after", "outcome", string(result.Outcome), "sequence", result.Sequence)
		}
		return result.Revoked, nil
	}
}
