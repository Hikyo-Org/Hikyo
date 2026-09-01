package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
)

// admissionWindowRetention is how long a windowed admission bucket is kept
// before the per-tick sweep drops it. Two minutes comfortably covers the
// one-minute rate windows plus clock skew between nodes.
const admissionWindowRetention = 2 * time.Minute

// nodeLivenessWindow is how recently a node must have heartbeat to count as a
// live peer for the mixed-root-key check. Three heartbeats of slack tolerates
// one missed beat without treating a live peer as gone.
const nodeLivenessWindow = 3 * defaultHeartbeat

// nodeRegistryRetention is how long a silent node's registry row survives the
// per-tick sweep. Well beyond any rolling restart, so only genuinely
// decommissioned nodes are pruned.
const nodeRegistryRetention = 24 * time.Hour

// configureHA prepares this node to join a multi-node installation (#146). It
// resolves the shared root key's fingerprint, refuses to serve when another
// node has registered a different fingerprint (a mixed-root-key
// misconfiguration, named rather than surfaced only as an opaque keyring
// failure), records this node in the live registry, and returns the
// coordination surface plus the per-tick maintenance closure the scheduler
// runs on every node. Any error is a boot refusal.
func configureHA(ctx context.Context, cfg *config.Config, log *slog.Logger, db *store.DB, sc store.Config) (*store.Coordination, func(context.Context), error) {
	root, err := resolveRootKey(cfg, log)
	if err != nil {
		return nil, nil, fmt.Errorf("ha: resolve root key: %w", err)
	}
	fingerprint := crypto.RootKeyFingerprint(root)
	crypto.Zero(root)

	schemaVersion, err := migrate.MaxVersion(ctx, sc)
	if err != nil {
		return nil, nil, fmt.Errorf("ha: schema version: %w", err)
	}

	coord := db.Coordination()
	foreign, err := coord.ForeignRootKeyFingerprints(ctx, cfg.NodeID, fingerprint, time.Now().UTC().Add(-nodeLivenessWindow))
	if err != nil {
		return nil, nil, fmt.Errorf("ha: read node registry: %w", err)
	}
	if len(foreign) > 0 {
		return nil, nil, fmt.Errorf("ha: refusing to serve: another live node registered a different root-key fingerprint; this installation's nodes must share one root-key authority (mixed root keys)")
	}

	node := store.HANode{
		NodeID:             cfg.NodeID,
		BinaryVersion:      Version,
		SchemaVersion:      schemaVersion,
		RootKeyFingerprint: fingerprint,
		StartedAt:          time.Now().UTC(),
		HeartbeatAt:        time.Now().UTC(),
	}
	if err := coord.UpsertNode(ctx, node); err != nil {
		return nil, nil, fmt.Errorf("ha: register node: %w", err)
	}
	log.Info("multi-node HA enabled", "node", cfg.NodeID, "schema_version", schemaVersion, "binary_version", Version)

	onTick := func(tickCtx context.Context) {
		now := time.Now().UTC()
		node.HeartbeatAt = now
		if err := coord.UpsertNode(tickCtx, node); err != nil {
			log.Warn("ha: node heartbeat failed", "node", cfg.NodeID, "err", err)
		}
		if err := coord.PruneAdmissionWindows(tickCtx, now.Add(-admissionWindowRetention)); err != nil {
			log.Warn("ha: admission window sweep failed", "err", err)
		}
		if err := coord.PruneNodes(tickCtx, now.Add(-nodeRegistryRetention)); err != nil {
			log.Warn("ha: node registry sweep failed", "err", err)
		}
	}
	return coord, onTick, nil
}
