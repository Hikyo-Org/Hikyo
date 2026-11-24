package app

import (
	"context"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/bridgerevocation"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/diagnostics"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

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
		diagnostics.Printf(ctx, 1, "Recovery bridge revocation check: %s (list sequence %d)", result.Outcome, result.Sequence)
		return result.Revoked, nil
	}
}
