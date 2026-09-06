package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Coordination is the installation-wide, proof-free coordination surface for
// multi-node HA (#146): the fenced singleton lease, the live-node registry,
// and the shared pre-authentication admission counters. It is infrastructure,
// not tenant data, so it does not pass through authz.Verify or a TxToken;
// like the migrator and the adapter outbox worker it runs at the app layer,
// and its tables carry no tenant chain (scope class instance).
type Coordination struct {
	db                                  *DB
	runtimeNodeID, runtimeTemplateStamp string
}

// Coordination returns the coordination surface bound to this datastore.
func (d *DB) Coordination() *Coordination { return &Coordination{db: d} }

// accountWindow is the fixed sentinel window used for the per-account backoff
// bucket, which is not time-windowed: one row per account holds the running
// consecutive-failure count and the current delay deadline.
var accountWindow = time.Unix(0, 0).UTC()

// AccountBucket, IPBucket, MetaBucket, and IssuerBucket name the admission
// dimensions stored in admission_counters.
const (
	AccountBucket = "account"
	IPBucket      = "ip"
	MetaBucket    = "meta"
	IssuerBucket  = "issuer"

	// MCP admission is shared across replicas. One token is restored each
	// second (60/minute), with at most 20 immediately available. In-flight
	// calls are bounded independently by principal, organization, and instance.
	MCPRateCapacity       = 20
	MCPRateRefillInterval = time.Second
	MCPPrincipalLimit     = 4
	MCPOrganizationLimit  = 8
	MCPInstanceLimit      = 64
)

// ErrMCPAdmissionLimited is the non-distinguishing shared rate/concurrency
// refusal. The transport translates it to the uniform authenticated 429.
var ErrMCPAdmissionLimited = errors.New("store: MCP admission limit reached")

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows)
}

// ErrMixedRootKey reports that another live node registered a different
// root-key fingerprint: the installation's nodes must share one root-key
// authority. RegisterNodeChecked returns it and boot refuses to serve.
var ErrMixedRootKey = errors.New("store: another live node registered a different root-key fingerprint (mixed root keys)")

// ---- Singleton lease -------------------------------------------------------

// ClaimLease acquires the named lease for owner when it is unheld or its
// current holder's lease has expired at now. fence_token increments on every
// acquisition, so a holder that resumes after losing the lease writes under a
// stale token and its guarded writes match zero rows. held is false when a
// live holder still owns the lease.
func (c *coordinationTx) ClaimLease(ctx context.Context, name, owner string, now, expires time.Time) (fence int64, held bool, err error) {
	if name == "scheduler" {
		if err := c.topologyLeaseAllowed(ctx, owner); err != nil {
			return 0, false, err
		}
	}
	switch c.db.engine {
	case EnginePostgres:
		err = c.db.pool.QueryRow(ctx,
			`INSERT INTO singleton_leases (name, owner, fence_token, acquired_at, expires_at)
			 VALUES ($1, $2, 1, $3, $4)
			 ON CONFLICT (name) DO UPDATE
			   SET owner = EXCLUDED.owner,
			       fence_token = singleton_leases.fence_token + 1,
			       acquired_at = EXCLUDED.acquired_at,
			       expires_at = EXCLUDED.expires_at
			   WHERE singleton_leases.expires_at <= $3
			 RETURNING fence_token`,
			name, owner, now, expires).Scan(&fence)
	case EngineSQLite:
		err = c.db.sqWrite.QueryRowContext(ctx,
			`INSERT INTO singleton_leases (name, owner, fence_token, acquired_at, expires_at)
			 VALUES (?, ?, 1, ?, ?)
			 ON CONFLICT (name) DO UPDATE
			   SET owner = excluded.owner,
			       fence_token = singleton_leases.fence_token + 1,
			       acquired_at = excluded.acquired_at,
			       expires_at = excluded.expires_at
			   WHERE singleton_leases.expires_at <= ?
			 RETURNING fence_token`,
			name, owner, fixedStamp(now), fixedStamp(expires), fixedStamp(now)).Scan(&fence)
	default:
		return 0, false, fmt.Errorf("store: coordination lease claim on unknown engine %q", c.db.engine)
	}
	if isNoRows(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("store: claim lease %q: %w", name, err)
	}
	return fence, true, nil
}

// RenewLease extends the lease deadline only while owner still holds a STILL
// LIVE lease at the given fence. The expires_at > now predicate is essential: a
// paused holder that wakes after its lease lapsed must not be able to revive an
// expired term under its old fence just because no one has claimed yet. A
// takeover changes owner and increments fence, so a stale holder's renewal
// matches zero rows and held is false: that is the fenced write the HA design
// turns on. now must be datastore time (the scheduler reads it via Now) so the
// comparison is not subject to per-node clock skew.
func (c *coordinationTx) RenewLease(ctx context.Context, name, owner string, fence int64, now, expires time.Time) (held bool, err error) {
	if name == "scheduler" {
		if err := c.topologyLeaseAllowed(ctx, owner); err != nil {
			return false, err
		}
	}
	var affected int64
	switch c.db.engine {
	case EnginePostgres:
		tag, e := c.db.pool.Exec(ctx,
			`UPDATE singleton_leases SET expires_at = $1
			 WHERE name = $2 AND owner = $3 AND fence_token = $4 AND expires_at > $5`,
			expires, name, owner, fence, now)
		if e != nil {
			return false, fmt.Errorf("store: renew lease %q: %w", name, e)
		}
		affected = tag.RowsAffected()
	case EngineSQLite:
		res, e := c.db.sqWrite.ExecContext(ctx,
			`UPDATE singleton_leases SET expires_at = ?
			 WHERE name = ? AND owner = ? AND fence_token = ? AND expires_at > ?`,
			fixedStamp(expires), name, owner, fence, fixedStamp(now))
		if e != nil {
			return false, fmt.Errorf("store: renew lease %q: %w", name, e)
		}
		affected, _ = res.RowsAffected()
	default:
		return false, fmt.Errorf("store: coordination lease renew on unknown engine %q", c.db.engine)
	}
	return affected == 1, nil
}

// ReleaseLease relinquishes the lease on graceful shutdown so a standby can
// take over immediately rather than waiting out the TTL. It marks the row
// expired rather than deleting it, so the monotonic fence_token is PRESERVED: a
// deleted-and-reinserted row would reset the token to 1 and let a delayed
// same-owner process match an old token. It only releases the row the caller
// still holds at its fence.
func (c *coordinationTx) ReleaseLease(ctx context.Context, name, owner string, fence int64) error {
	var err error
	switch c.db.engine {
	case EnginePostgres:
		_, err = c.db.pool.Exec(ctx,
			`UPDATE singleton_leases SET expires_at = $1 WHERE name = $2 AND owner = $3 AND fence_token = $4`,
			accountWindow, name, owner, fence)
	case EngineSQLite:
		_, err = c.db.sqWrite.ExecContext(ctx,
			`UPDATE singleton_leases SET expires_at = ? WHERE name = ? AND owner = ? AND fence_token = ?`,
			fixedStamp(accountWindow), name, owner, fence)
	default:
		return fmt.Errorf("store: coordination lease release on unknown engine %q", c.db.engine)
	}
	if err != nil {
		return fmt.Errorf("store: release lease %q: %w", name, err)
	}
	return nil
}

// Now returns the datastore's clock. HA lease decisions read time from here so
// every node compares against one clock, removing per-node skew as a
// split-brain vector. On sqlite (single node, never HA) the process clock is
// authoritative.
func (c *coordinationTx) Now(ctx context.Context) (time.Time, error) {
	switch c.db.engine {
	case EnginePostgres:
		var now time.Time
		// now()/transaction_timestamp() (frozen at BEGIN), NOT clock_timestamp():
		// every lease comparison inside one transaction reads a single stable
		// instant. AuditExportSnapshotTime deliberately uses the opposite clock.
		if err := c.db.pool.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
			return time.Time{}, fmt.Errorf("store: datastore clock: %w", err)
		}
		return now.UTC(), nil
	case EngineSQLite:
		return time.Now().UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("store: datastore clock on unknown engine %q", c.db.engine)
	}
}

// LeaseHolder reports the current owner of a lease and whether the lease is
// still live at now. Used by health to expose the leader and the lease age.
func (c *coordinationTx) LeaseHolder(ctx context.Context, name string, now time.Time) (owner string, acquiredAt time.Time, live bool, err error) {
	switch c.db.engine {
	case EnginePostgres:
		var expires time.Time
		err = c.db.pool.QueryRow(ctx,
			`SELECT owner, acquired_at, expires_at FROM singleton_leases WHERE name = $1`, name).
			Scan(&owner, &acquiredAt, &expires)
		if isNoRows(err) {
			return "", time.Time{}, false, nil
		}
		if err != nil {
			return "", time.Time{}, false, fmt.Errorf("store: lease holder %q: %w", name, err)
		}
		return owner, acquiredAt.UTC(), expires.After(now), nil
	case EngineSQLite:
		var acquiredStr, expiresStr string
		err = c.db.sqRead.QueryRowContext(ctx,
			`SELECT owner, acquired_at, expires_at FROM singleton_leases WHERE name = ?`, name).
			Scan(&owner, &acquiredStr, &expiresStr)
		if isNoRows(err) {
			return "", time.Time{}, false, nil
		}
		if err != nil {
			return "", time.Time{}, false, fmt.Errorf("store: lease holder %q: %w", name, err)
		}
		acquiredAt, _ = parseStamp(acquiredStr)
		expires, _ := parseStamp(expiresStr)
		return owner, acquiredAt.UTC(), expires.After(now), nil
	default:
		return "", time.Time{}, false, fmt.Errorf("store: coordination lease holder on unknown engine %q", c.db.engine)
	}
}

// ---- Node registry ---------------------------------------------------------

// HANode is one serving node's registry row.
type HANode struct {
	NodeID             string
	BinaryVersion      string
	SchemaVersion      int64
	RootKeyFingerprint string
	StartedAt          time.Time
	HeartbeatAt        time.Time
}

// UpsertNode records or refreshes a node's registry row. StartedAt is only set
// on first insert; the heartbeat advances on every call.
func (c *coordinationTx) UpsertNode(ctx context.Context, n HANode) error {
	if err := c.topologyLeaseAllowed(ctx, n.NodeID); err != nil {
		return err
	}
	var err error
	switch c.db.engine {
	case EnginePostgres:
		_, err = c.db.pool.Exec(ctx,
			`INSERT INTO ha_nodes (node_id, binary_version, schema_version, root_key_fingerprint, started_at, heartbeat_at)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (node_id) DO UPDATE
			   SET binary_version = EXCLUDED.binary_version,
			       schema_version = EXCLUDED.schema_version,
			       root_key_fingerprint = EXCLUDED.root_key_fingerprint,
			       heartbeat_at = EXCLUDED.heartbeat_at`,
			n.NodeID, n.BinaryVersion, n.SchemaVersion, n.RootKeyFingerprint, n.StartedAt, n.HeartbeatAt)
	case EngineSQLite:
		_, err = c.db.sqWrite.ExecContext(ctx,
			`INSERT INTO ha_nodes (node_id, binary_version, schema_version, root_key_fingerprint, started_at, heartbeat_at)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT (node_id) DO UPDATE
			   SET binary_version = excluded.binary_version,
			       schema_version = excluded.schema_version,
			       root_key_fingerprint = excluded.root_key_fingerprint,
			       heartbeat_at = excluded.heartbeat_at`,
			n.NodeID, n.BinaryVersion, n.SchemaVersion, n.RootKeyFingerprint, fixedStamp(n.StartedAt), fixedStamp(n.HeartbeatAt))
	default:
		return fmt.Errorf("store: coordination node upsert on unknown engine %q", c.db.engine)
	}
	if err != nil {
		return fmt.Errorf("store: upsert node %q: %w", n.NodeID, err)
	}
	return nil
}

// coordinationAdvisoryClass is the advisory-lock namespace shared across the
// datastore; classID 87 is this ticket's (84-86 are the audit-export locks).
const (
	coordinationAdvisoryNamespace = 1464159830
	haRegisterAdvisoryClass       = 87
)

// RegisterNodeChecked atomically refuses a mixed-root-key installation and
// registers this node. The foreign-fingerprint check and the node upsert run
// under one Postgres advisory lock so two nodes starting simultaneously with
// different roots (as can happen mid root-key rotation, when a dual-wrapped
// master unwraps under either root) cannot both observe no foreign row and both
// proceed. On sqlite there is a single node and no race, so it is a plain
// upsert. since bounds which peers count as live.
func (c *coordinationTx) RegisterNodeChecked(ctx context.Context, n HANode, since time.Time) error {
	if err := c.topologyLeaseAllowed(ctx, n.NodeID); err != nil {
		return err
	}
	switch c.db.engine {
	case EnginePostgres:
		tx := c.db.pool
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, coordinationAdvisoryNamespace, haRegisterAdvisoryClass); err != nil {
			return fmt.Errorf("store: register node: lock: %w", err)
		}
		var foreign int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM ha_nodes
			 WHERE node_id <> $1 AND root_key_fingerprint <> $2 AND heartbeat_at >= $3`,
			n.NodeID, n.RootKeyFingerprint, since).Scan(&foreign); err != nil {
			return fmt.Errorf("store: register node: check: %w", err)
		}
		if foreign > 0 {
			return ErrMixedRootKey
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO ha_nodes (node_id, binary_version, schema_version, root_key_fingerprint, started_at, heartbeat_at)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (node_id) DO UPDATE
			   SET binary_version = EXCLUDED.binary_version,
			       schema_version = EXCLUDED.schema_version,
			       root_key_fingerprint = EXCLUDED.root_key_fingerprint,
			       heartbeat_at = EXCLUDED.heartbeat_at`,
			n.NodeID, n.BinaryVersion, n.SchemaVersion, n.RootKeyFingerprint, n.StartedAt, n.HeartbeatAt); err != nil {
			return fmt.Errorf("store: register node: upsert: %w", err)
		}
		return nil
	case EngineSQLite:
		foreign, err := c.ForeignRootKeyFingerprints(ctx, n.NodeID, n.RootKeyFingerprint, since)
		if err != nil {
			return err
		}
		if len(foreign) > 0 {
			return ErrMixedRootKey
		}
		return c.UpsertNode(ctx, n)
	default:
		return fmt.Errorf("store: register node on unknown engine %q", c.db.engine)
	}
}

// CountLiveNodes counts nodes whose heartbeat is at or after since.
func (c *coordinationTx) CountLiveNodes(ctx context.Context, since time.Time) (int, error) {
	var count int
	var err error
	switch c.db.engine {
	case EnginePostgres:
		err = c.db.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM ha_nodes WHERE heartbeat_at >= $1`, since).Scan(&count)
	case EngineSQLite:
		err = c.db.sqRead.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM ha_nodes WHERE heartbeat_at >= ?`, fixedStamp(since)).Scan(&count)
	default:
		return 0, fmt.Errorf("store: coordination live-node count on unknown engine %q", c.db.engine)
	}
	if err != nil {
		return 0, fmt.Errorf("store: count live nodes: %w", err)
	}
	return count, nil
}

// PruneNodes drops registry rows whose heartbeat fell before cutoff, so a
// decommissioned node does not linger in nodes_seen or the fingerprint check.
func (c *coordinationTx) PruneNodes(ctx context.Context, cutoff time.Time) error {
	var err error
	switch c.db.engine {
	case EnginePostgres:
		_, err = c.db.pool.Exec(ctx, `DELETE FROM ha_nodes WHERE heartbeat_at < $1`, cutoff)
	case EngineSQLite:
		_, err = c.db.sqWrite.ExecContext(ctx, `DELETE FROM ha_nodes WHERE heartbeat_at < ?`, fixedStamp(cutoff))
	default:
		return fmt.Errorf("store: coordination node prune on unknown engine %q", c.db.engine)
	}
	if err != nil {
		return fmt.Errorf("store: prune nodes: %w", err)
	}
	return nil
}

// ForeignRootKeyFingerprints returns the distinct root-key fingerprints
// recorded by any other LIVE node (heartbeat at or after since) that differ
// from the caller's. A non-empty result is a mixed-root-key misconfiguration:
// the caller refuses to serve rather than join an installation whose key
// authority it does not share. The liveness filter is essential: a
// decommissioned node's stale row must not veto forever, and a root-key
// rotation (stop-all, rotate, start-all) must not be refused by the outgoing
// nodes' rows.
func (c *coordinationTx) ForeignRootKeyFingerprints(ctx context.Context, nodeID, fingerprint string, since time.Time) ([]string, error) {
	var others []string
	switch c.db.engine {
	case EnginePostgres:
		r, err := c.db.pool.Query(ctx,
			`SELECT DISTINCT root_key_fingerprint FROM ha_nodes
			 WHERE node_id <> $1 AND root_key_fingerprint <> $2 AND heartbeat_at >= $3`, nodeID, fingerprint, since)
		if err != nil {
			return nil, fmt.Errorf("store: foreign root fingerprints: %w", err)
		}
		defer r.Close()
		for r.Next() {
			var fp string
			if err := r.Scan(&fp); err != nil {
				return nil, fmt.Errorf("store: foreign root fingerprints scan: %w", err)
			}
			others = append(others, fp)
		}
		return others, r.Err()
	case EngineSQLite:
		r, err := c.db.sqRead.QueryContext(ctx,
			`SELECT DISTINCT root_key_fingerprint FROM ha_nodes
			 WHERE node_id <> ? AND root_key_fingerprint <> ? AND heartbeat_at >= ?`, nodeID, fingerprint, fixedStamp(since))
		if err != nil {
			return nil, fmt.Errorf("store: foreign root fingerprints: %w", err)
		}
		defer r.Close()
		for r.Next() {
			var fp string
			if err := r.Scan(&fp); err != nil {
				return nil, fmt.Errorf("store: foreign root fingerprints scan: %w", err)
			}
			others = append(others, fp)
		}
		return others, r.Err()
	default:
		return nil, fmt.Errorf("store: coordination foreign fingerprints on unknown engine %q", c.db.engine)
	}
}

// ---- Admission counters ----------------------------------------------------

// BumpWindow increments the hit count for a windowed admission bucket
// (ip/meta/issuer) and returns the new count within that fixed window. Callers
// compare it against the dimension's per-minute allowance.
func (c *coordinationTx) BumpWindow(ctx context.Context, bucket, subject string, windowStart time.Time) (int64, error) {
	var count int64
	var err error
	switch c.db.engine {
	case EnginePostgres:
		err = c.db.pool.QueryRow(ctx,
			`INSERT INTO admission_counters (bucket, subject, window_start, hits)
			 VALUES ($1, $2, $3, 1)
			 ON CONFLICT (bucket, subject, window_start) DO UPDATE
			   SET hits = admission_counters.hits + 1
			 RETURNING hits`,
			bucket, subject, windowStart).Scan(&count)
	case EngineSQLite:
		err = c.db.sqWrite.QueryRowContext(ctx,
			`INSERT INTO admission_counters (bucket, subject, window_start, hits)
			 VALUES (?, ?, ?, 1)
			 ON CONFLICT (bucket, subject, window_start) DO UPDATE
			   SET hits = admission_counters.hits + 1
			 RETURNING hits`,
			bucket, subject, fixedStamp(windowStart)).Scan(&count)
	default:
		return 0, fmt.Errorf("store: coordination window bump on unknown engine %q", c.db.engine)
	}
	if err != nil {
		return 0, fmt.Errorf("store: bump admission window %s/%s: %w", bucket, subject, err)
	}
	return count, nil
}

// The MCP claim lock serializes only the short datastore admission decision.
// Calls themselves run concurrently after commit and are represented by
// expiring rows. A crashed replica therefore releases capacity automatically.
const mcpAdmissionAdvisoryClass = 88

// AcquireMCP atomically charges the principal token bucket and claims all
// three concurrency dimensions. No rate token is consumed unless every
// concurrency dimension has room. The PostgreSQL clock is authoritative;
// SQLite is single-node and uses the process clock like Coordination.Now.
func (c *coordinationTx) AcquireMCP(ctx context.Context, callID, principalID, orgID string, ttl time.Duration) error {
	if callID == "" || principalID == "" || orgID == "" || ttl <= 0 {
		return errors.New("store: invalid MCP admission claim")
	}
	switch c.db.engine {
	case EnginePostgres:
		return c.acquireMCPPostgres(ctx, callID, principalID, orgID, ttl)
	case EngineSQLite:
		return c.acquireMCPSQLite(ctx, callID, principalID, orgID, ttl)
	default:
		return fmt.Errorf("store: MCP admission claim on unknown engine %q", c.db.engine)
	}
}

func (c *coordinationTx) acquireMCPPostgres(ctx context.Context, callID, principalID, orgID string, ttl time.Duration) error {
	tx := c.db.pool
	var err error
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, coordinationAdvisoryNamespace, mcpAdmissionAdvisoryClass); err != nil {
		return fmt.Errorf("store: MCP admission lock: %w", err)
	}
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
		return fmt.Errorf("store: MCP admission clock: %w", err)
	}
	now = now.UTC()
	if _, err := tx.Exec(ctx, `DELETE FROM mcp_inflight WHERE expires_at <= $1`, now); err != nil {
		return fmt.Errorf("store: prune MCP claims: %w", err)
	}
	var principalCount, orgCount, instanceCount int64
	if err := tx.QueryRow(ctx, `SELECT
		COUNT(*) FILTER (WHERE principal_id = $1),
		COUNT(*) FILTER (WHERE org_id = $2),
		COUNT(*)
		FROM mcp_inflight`, principalID, orgID).Scan(&principalCount, &orgCount, &instanceCount); err != nil {
		return fmt.Errorf("store: count MCP claims: %w", err)
	}
	if principalCount >= MCPPrincipalLimit || orgCount >= MCPOrganizationLimit || instanceCount >= MCPInstanceLimit {
		return ErrMCPAdmissionLimited
	}

	nextAt := now
	err = tx.QueryRow(ctx, `SELECT next_at FROM mcp_rate_buckets WHERE principal_id = $1 FOR UPDATE`, principalID).Scan(&nextAt)
	if err != nil && !isNoRows(err) {
		return fmt.Errorf("store: read MCP rate bucket: %w", err)
	}
	if nextAt.After(now.Add(time.Duration(MCPRateCapacity-1) * MCPRateRefillInterval)) {
		return ErrMCPAdmissionLimited
	}
	if nextAt.Before(now) {
		nextAt = now
	}
	nextAt = nextAt.Add(MCPRateRefillInterval)
	if _, err := tx.Exec(ctx, `INSERT INTO mcp_rate_buckets (principal_id, next_at) VALUES ($1, $2)
		ON CONFLICT (principal_id) DO UPDATE SET next_at = EXCLUDED.next_at`, principalID, nextAt); err != nil {
		return fmt.Errorf("store: update MCP rate bucket: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO mcp_inflight (call_id, principal_id, org_id, expires_at) VALUES ($1, $2, $3, $4)`,
		callID, principalID, orgID, now.Add(ttl)); err != nil {
		return fmt.Errorf("store: insert MCP claim: %w", err)
	}
	return nil
}

func (c *coordinationTx) acquireMCPSQLite(ctx context.Context, callID, principalID, orgID string, ttl time.Duration) error {
	tx := c.db.sqWrite
	var err error
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_inflight WHERE expires_at <= ?`, fixedStamp(now)); err != nil {
		return fmt.Errorf("store: prune MCP claims: %w", err)
	}
	var principalCount, orgCount, instanceCount int64
	if err := tx.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN principal_id = ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN org_id = ? THEN 1 ELSE 0 END), 0),
		COUNT(*)
		FROM mcp_inflight`, principalID, orgID).Scan(&principalCount, &orgCount, &instanceCount); err != nil {
		return fmt.Errorf("store: count MCP claims: %w", err)
	}
	if principalCount >= MCPPrincipalLimit || orgCount >= MCPOrganizationLimit || instanceCount >= MCPInstanceLimit {
		return ErrMCPAdmissionLimited
	}

	nextAt := now
	var nextRaw string
	err = tx.QueryRowContext(ctx, `SELECT next_at FROM mcp_rate_buckets WHERE principal_id = ?`, principalID).Scan(&nextRaw)
	switch {
	case isNoRows(err):
	case err != nil:
		return fmt.Errorf("store: read MCP rate bucket: %w", err)
	default:
		nextAt, err = parseStamp(nextRaw)
		if err != nil {
			return fmt.Errorf("store: parse MCP rate bucket: %w", err)
		}
	}
	if nextAt.After(now.Add(time.Duration(MCPRateCapacity-1) * MCPRateRefillInterval)) {
		return ErrMCPAdmissionLimited
	}
	if nextAt.Before(now) {
		nextAt = now
	}
	nextAt = nextAt.Add(MCPRateRefillInterval)
	if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_rate_buckets (principal_id, next_at) VALUES (?, ?)
		ON CONFLICT (principal_id) DO UPDATE SET next_at = excluded.next_at`, principalID, fixedStamp(nextAt)); err != nil {
		return fmt.Errorf("store: update MCP rate bucket: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_inflight (call_id, principal_id, org_id, expires_at) VALUES (?, ?, ?, ?)`,
		callID, principalID, orgID, fixedStamp(now.Add(ttl))); err != nil {
		return fmt.Errorf("store: insert MCP claim: %w", err)
	}
	return nil
}

// ReleaseMCP releases one successful claim. A missing or already-expired id is
// harmless. Callers still fail closed on datastore errors.
func (c *coordinationTx) ReleaseMCP(ctx context.Context, callID string) error {
	if callID == "" {
		return errors.New("store: empty MCP admission call id")
	}
	var err error
	switch c.db.engine {
	case EnginePostgres:
		_, err = c.db.pool.Exec(ctx, `DELETE FROM mcp_inflight WHERE call_id = $1`, callID)
	case EngineSQLite:
		_, err = c.db.sqWrite.ExecContext(ctx, `DELETE FROM mcp_inflight WHERE call_id = ?`, callID)
	default:
		return fmt.Errorf("store: MCP admission release on unknown engine %q", c.db.engine)
	}
	if err != nil {
		return fmt.Errorf("store: release MCP claim: %w", err)
	}
	return nil
}

// AccountFailureState reports an account subject's consecutive-failure count,
// the instant of its most recent failure, and the datastore's current time.
// The delay deadline is a pure function of failures and lastFailure (computed
// in the admission package) and is compared against dbNow, so both the stamp
// and the comparison use one clock: no separately stored deadline to go stale,
// and no per-node clock skew in the remaining-delay calculation. ok is false
// when no failures are recorded.
func (c *coordinationTx) AccountFailureState(ctx context.Context, subject string) (failures int64, lastFailure, dbNow time.Time, ok bool, err error) {
	switch c.db.engine {
	case EnginePostgres:
		var until sql.NullTime
		e := c.db.pool.QueryRow(ctx,
			`SELECT failures, until_at, now() FROM admission_counters WHERE bucket = $1 AND subject = $2 AND window_start = $3`,
			AccountBucket, subject, accountWindow).Scan(&failures, &until, &dbNow)
		if isNoRows(e) {
			return 0, time.Time{}, time.Time{}, false, nil
		}
		if e != nil {
			return 0, time.Time{}, time.Time{}, false, fmt.Errorf("store: account failure state %s: %w", subject, e)
		}
		if until.Valid {
			lastFailure = until.Time.UTC()
		}
		return failures, lastFailure, dbNow.UTC(), true, nil
	case EngineSQLite:
		var until sql.NullString
		e := c.db.sqRead.QueryRowContext(ctx,
			`SELECT failures, until_at FROM admission_counters WHERE bucket = ? AND subject = ? AND window_start = ?`,
			AccountBucket, subject, fixedStamp(accountWindow)).Scan(&failures, &until)
		if isNoRows(e) {
			return 0, time.Time{}, time.Time{}, false, nil
		}
		if e != nil {
			return 0, time.Time{}, time.Time{}, false, fmt.Errorf("store: account failure state %s: %w", subject, e)
		}
		if until.Valid && until.String != "" {
			lastFailure, _ = parseStamp(until.String)
			lastFailure = lastFailure.UTC()
		}
		// sqlite is single-node, so the process clock is the datastore clock.
		return failures, lastFailure, time.Now().UTC(), true, nil
	default:
		return 0, time.Time{}, time.Time{}, false, fmt.Errorf("store: coordination account failure state on unknown engine %q", c.db.engine)
	}
}

// RecordAccountFailure atomically increments the consecutive-failure count for
// an account subject and advances the failure instant, in one statement. The
// timestamp is datastore time and moves MONOTONICALLY (GREATEST/max), so a
// slow request that commits after a newer one cannot move it backwards, and no
// concurrent record can leave the row admitting again. On sqlite (single node)
// now is the process clock; on Postgres the SQL uses now() and the argument is
// ignored, so multi-node timestamps come from one clock.
func (c *coordinationTx) RecordAccountFailure(ctx context.Context, subject string, now time.Time) (int64, error) {
	var failures int64
	var err error
	switch c.db.engine {
	case EnginePostgres:
		err = c.db.pool.QueryRow(ctx,
			`INSERT INTO admission_counters (bucket, subject, window_start, failures, until_at)
			 VALUES ($1, $2, $3, 1, now())
			 ON CONFLICT (bucket, subject, window_start) DO UPDATE
			   SET failures = admission_counters.failures + 1,
			       until_at = GREATEST(admission_counters.until_at, now())
			 RETURNING failures`,
			AccountBucket, subject, accountWindow).Scan(&failures)
	case EngineSQLite:
		err = c.db.sqWrite.QueryRowContext(ctx,
			`INSERT INTO admission_counters (bucket, subject, window_start, failures, until_at)
			 VALUES (?, ?, ?, 1, ?)
			 ON CONFLICT (bucket, subject, window_start) DO UPDATE
			   SET failures = admission_counters.failures + 1,
			       until_at = max(admission_counters.until_at, ?)
			 RETURNING failures`,
			AccountBucket, subject, fixedStamp(accountWindow), fixedStamp(now), fixedStamp(now)).Scan(&failures)
	default:
		return 0, fmt.Errorf("store: coordination record failure on unknown engine %q", c.db.engine)
	}
	if err != nil {
		return 0, fmt.Errorf("store: record account failure %s: %w", subject, err)
	}
	return failures, nil
}

// PruneAccountBackoff drops account rows whose last failure fell before cutoff,
// so unique unknown usernames cannot accumulate permanent rows. The cutoff is
// well past the maximum backoff, so a live backoff is never swept.
func (c *coordinationTx) PruneAccountBackoff(ctx context.Context, cutoff time.Time) error {
	var err error
	switch c.db.engine {
	case EnginePostgres:
		_, err = c.db.pool.Exec(ctx,
			`DELETE FROM admission_counters WHERE bucket = $1 AND (until_at IS NULL OR until_at < $2)`,
			AccountBucket, cutoff)
	case EngineSQLite:
		_, err = c.db.sqWrite.ExecContext(ctx,
			`DELETE FROM admission_counters WHERE bucket = ? AND (until_at IS NULL OR until_at < ?)`,
			AccountBucket, fixedStamp(cutoff))
	default:
		return fmt.Errorf("store: coordination account prune on unknown engine %q", c.db.engine)
	}
	if err != nil {
		return fmt.Errorf("store: prune account backoff: %w", err)
	}
	return nil
}

// ClearAccount drops an account subject's backoff row on a successful attempt.
func (c *coordinationTx) ClearAccount(ctx context.Context, subject string) error {
	var err error
	switch c.db.engine {
	case EnginePostgres:
		_, err = c.db.pool.Exec(ctx,
			`DELETE FROM admission_counters WHERE bucket = $1 AND subject = $2 AND window_start = $3`,
			AccountBucket, subject, accountWindow)
	case EngineSQLite:
		_, err = c.db.sqWrite.ExecContext(ctx,
			`DELETE FROM admission_counters WHERE bucket = ? AND subject = ? AND window_start = ?`,
			AccountBucket, subject, fixedStamp(accountWindow))
	default:
		return fmt.Errorf("store: coordination clear account on unknown engine %q", c.db.engine)
	}
	if err != nil {
		return fmt.Errorf("store: clear account %s: %w", subject, err)
	}
	return nil
}

// PruneAdmissionWindows deletes windowed rate buckets whose window closed
// before cutoff. Account backoff rows use the sentinel window and are never
// swept here; a success clears them and a live deadline keeps them meaningful.
func (c *coordinationTx) PruneAdmissionWindows(ctx context.Context, cutoff time.Time) error {
	var err error
	switch c.db.engine {
	case EnginePostgres:
		_, err = c.db.pool.Exec(ctx,
			`DELETE FROM admission_counters WHERE bucket <> $1 AND window_start < $2`,
			AccountBucket, cutoff)
	case EngineSQLite:
		_, err = c.db.sqWrite.ExecContext(ctx,
			`DELETE FROM admission_counters WHERE bucket <> ? AND window_start < ?`,
			AccountBucket, fixedStamp(cutoff))
	default:
		return fmt.Errorf("store: coordination prune on unknown engine %q", c.db.engine)
	}
	if err != nil {
		return fmt.Errorf("store: prune admission windows: %w", err)
	}
	return nil
}

// ForSingletonProcess binds a coordinator to the immutable startup identity.
// This restricts existing host coordination; it does not grant tenant access.
func (c *Coordination) ForSingletonProcess(nodeID, templateStamp string) *Coordination {
	return &Coordination{db: c.db, runtimeNodeID: nodeID, runtimeTemplateStamp: templateStamp}
}
