package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
)

// haStatus holds the cached values the label-free HA gauges read at scrape
// time. nodesSeen and the lease age are refreshed on every scheduler tick so a
// scrape never issues a datastore query; leadership is read live from the
// scheduler.
type haStatus struct {
	nodesSeen      atomic.Int64
	leaseAgeMillis atomic.Int64
	leader         func() bool
}

func (h *haStatus) HASnapshot() server.HAStats {
	leader := true
	if h.leader != nil {
		leader = h.leader()
	}
	return server.HAStats{
		Enabled:         true,
		IsLeader:        leader,
		NodesSeen:       int(h.nodesSeen.Load()),
		LeaseAgeSeconds: float64(h.leaseAgeMillis.Load()) / 1000,
	}
}

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

// accountBackoffRetention is how long an idle account-backoff row survives.
// Well past the maximum backoff, so a live backoff is never swept and unknown
// usernames cannot accumulate permanent rows.
const accountBackoffRetention = time.Hour

// readyChecker is the operational readiness probe: the base datastore-and-schema
// check, plus an optional HA lease-datastore probe attached at boot when HA is
// enabled. The probe pointer is atomic so it can be set after construction
// without racing a scrape of /readyz.
type readyChecker struct {
	base    server.ReadyChecker
	haProbe atomic.Pointer[func(ctx context.Context) error]
}

func (r *readyChecker) setHAProbe(probe func(context.Context) error) {
	r.haProbe.Store(&probe)
}

func (r *readyChecker) Ready(ctx context.Context) error {
	if err := r.base.Ready(ctx); err != nil {
		return err
	}
	if p := r.haProbe.Load(); p != nil {
		if err := (*p)(ctx); err != nil {
			return err
		}
	}
	return nil
}

// haReadinessProbe fails /readyz when the lease datastore is unreachable, so a
// node that has lost its coordination datastore stops receiving traffic (fail
// closed) rather than serving in ignorance of the rest of the cluster.
func haReadinessProbe(coord *store.Coordination) func(context.Context) error {
	return func(ctx context.Context) error {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if _, _, _, err := coord.LeaseHolder(probeCtx, schedulerLeaseName, time.Now().UTC()); err != nil {
			return fmt.Errorf("ha: lease datastore unreachable: %w", err)
		}
		return nil
	}
}

// configureHA prepares this node to join a multi-node installation (#146). It
// resolves the shared root key's fingerprint, refuses to serve when another
// node has registered a different fingerprint (a mixed-root-key
// misconfiguration, named rather than surfaced only as an opaque keyring
// failure), records this node in the live registry, and returns the
// coordination surface plus the per-tick maintenance closure the scheduler
// runs on every node. Any error is a boot refusal.
func configureHA(ctx context.Context, cfg *config.Config, log *slog.Logger, db *store.DB, sc store.Config, kr *crypto.Keyring) (*store.Coordination, func(context.Context), *haStatus, error) {
	// Defence in depth beyond config.Load: HA is Postgres-only. sqlite is
	// single-writer and cannot back multi-node coordination, so refuse rather
	// than degrade even when a caller constructs the config directly.
	if db.Engine() != store.EnginePostgres {
		return nil, nil, nil, fmt.Errorf("ha: refusing to serve: multi-node HA requires a PostgreSQL datastore, not %s", db.Engine())
	}
	if cfg.NodeID == "" {
		return nil, nil, nil, fmt.Errorf("ha: refusing to serve: multi-node HA requires a stable node id")
	}
	root, err := resolveRootKey(cfg, log)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ha: resolve root key: %w", err)
	}
	fingerprint := crypto.RootKeyFingerprint(root)
	crypto.Zero(root)

	schemaVersion, err := migrate.MaxVersion(ctx, sc)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ha: schema version: %w", err)
	}

	coord := db.Coordination()
	now, err := coord.Now(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ha: datastore clock: %w", err)
	}
	node := store.HANode{
		NodeID:             cfg.NodeID,
		BinaryVersion:      Version,
		SchemaVersion:      schemaVersion,
		RootKeyFingerprint: fingerprint,
		StartedAt:          now,
		HeartbeatAt:        now,
	}
	// Atomic check-and-register under a datastore lock: a mixed-root-key
	// installation is refused before this node serves, and two nodes starting
	// at once cannot both slip past the check.
	if err := coord.RegisterNodeChecked(ctx, node, now.Add(-nodeLivenessWindow)); err != nil {
		return nil, nil, nil, fmt.Errorf("ha: refusing to serve: %w", err)
	}
	log.Info("multi-node HA enabled", "node", cfg.NodeID, "schema_version", schemaVersion, "binary_version", Version)

	status := &haStatus{}
	status.nodesSeen.Store(1)

	onTick := func(tickCtx context.Context) {
		// Use datastore time so every node's heartbeat and liveness windows
		// share one clock (no per-node skew in nodes_seen or the sweeps).
		now, err := coord.Now(tickCtx)
		if err != nil {
			log.Warn("ha: datastore clock unreachable on tick", "err", err)
			return
		}
		// Build a local copy: the boot goroutine already read `node` for the
		// initial register, and this closure runs on the scheduler goroutine.
		hb := node
		hb.HeartbeatAt = now
		if err := coord.UpsertNode(tickCtx, hb); err != nil {
			log.Warn("ha: node heartbeat failed", "node", cfg.NodeID, "err", err)
		}
		if err := coord.PruneAdmissionWindows(tickCtx, now.Add(-admissionWindowRetention)); err != nil {
			log.Warn("ha: admission window sweep failed", "err", err)
		}
		if err := coord.PruneAccountBackoff(tickCtx, now.Add(-accountBackoffRetention)); err != nil {
			log.Warn("ha: account backoff sweep failed", "err", err)
		}
		if err := coord.PruneNodes(tickCtx, now.Add(-nodeRegistryRetention)); err != nil {
			log.Warn("ha: node registry sweep failed", "err", err)
		}
		// The instance DEK is a single held set, not an LRU, so it cannot be
		// revalidated per fetch like a project DEK (ForInstance takes no
		// context). Refresh it each tick instead, so a rotate-dek --instance on
		// another node is picked up within one heartbeat rather than fencing
		// this node's instance-credential writes until restart.
		if err := kr.ReloadInstanceDEK(tickCtx); err != nil {
			log.Warn("ha: instance DEK refresh failed", "err", err)
		}
		// Refresh the cached gauge values so a /metrics scrape never queries.
		if n, err := coord.CountLiveNodes(tickCtx, now.Add(-nodeLivenessWindow)); err == nil {
			status.nodesSeen.Store(int64(n))
		}
		if _, acquiredAt, live, err := coord.LeaseHolder(tickCtx, schedulerLeaseName, now); err == nil && live {
			status.leaseAgeMillis.Store(now.Sub(acquiredAt).Milliseconds())
		} else if err == nil {
			status.leaseAgeMillis.Store(0)
		}
	}
	return coord, onTick, status, nil
}
