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
type Coordination struct{ db *DB }

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
)

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows)
}

// ErrMixedRootKey reports that another live node registered a different
// root-key fingerprint: the installation's nodes must share one root-key
// authority. RegisterNodeChecked returns it and boot refuses to serve.
var ErrMixedRootKey = errors.New("store: another live node registered a different root-key fingerprint (mixed root keys)")

// sqliteTime renders an instant in the fixed-width canonical form the sqlite
// schema stores, so lexicographic comparison in SQL matches time comparison.
func sqliteTime(t time.Time) string { return t.UTC().Format(adapterTimeFormat) }

// ---- Singleton lease -------------------------------------------------------

// ClaimLease acquires the named lease for owner when it is unheld or its
// current holder's lease has expired at now. fence_token increments on every
// acquisition, so a holder that resumes after losing the lease writes under a
// stale token and its guarded writes match zero rows. held is false when a
// live holder still owns the lease.
func (c *Coordination) ClaimLease(ctx context.Context, name, owner string, now, expires time.Time) (fence int64, held bool, err error) {
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
			name, owner, sqliteTime(now), sqliteTime(expires), sqliteTime(now)).Scan(&fence)
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
func (c *Coordination) RenewLease(ctx context.Context, name, owner string, fence int64, now, expires time.Time) (held bool, err error) {
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
			sqliteTime(expires), name, owner, fence, sqliteTime(now))
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
func (c *Coordination) ReleaseLease(ctx context.Context, name, owner string, fence int64) error {
	var err error
	switch c.db.engine {
	case EnginePostgres:
		_, err = c.db.pool.Exec(ctx,
			`UPDATE singleton_leases SET expires_at = $1 WHERE name = $2 AND owner = $3 AND fence_token = $4`,
			accountWindow, name, owner, fence)
	case EngineSQLite:
		_, err = c.db.sqWrite.ExecContext(ctx,
			`UPDATE singleton_leases SET expires_at = ? WHERE name = ? AND owner = ? AND fence_token = ?`,
			sqliteTime(accountWindow), name, owner, fence)
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
func (c *Coordination) Now(ctx context.Context) (time.Time, error) {
	switch c.db.engine {
	case EnginePostgres:
		var now time.Time
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
func (c *Coordination) LeaseHolder(ctx context.Context, name string, now time.Time) (owner string, acquiredAt time.Time, live bool, err error) {
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
		acquiredAt, _ = time.Parse(adapterTimeFormat, acquiredStr)
		expires, _ := time.Parse(adapterTimeFormat, expiresStr)
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
func (c *Coordination) UpsertNode(ctx context.Context, n HANode) error {
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
			n.NodeID, n.BinaryVersion, n.SchemaVersion, n.RootKeyFingerprint, sqliteTime(n.StartedAt), sqliteTime(n.HeartbeatAt))
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
func (c *Coordination) RegisterNodeChecked(ctx context.Context, n HANode, since time.Time) error {
	switch c.db.engine {
	case EnginePostgres:
		tx, err := c.db.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("store: register node: begin: %w", err)
		}
		defer tx.Rollback(ctx)
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
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("store: register node: commit: %w", err)
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
func (c *Coordination) CountLiveNodes(ctx context.Context, since time.Time) (int, error) {
	var count int
	var err error
	switch c.db.engine {
	case EnginePostgres:
		err = c.db.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM ha_nodes WHERE heartbeat_at >= $1`, since).Scan(&count)
	case EngineSQLite:
		err = c.db.sqRead.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM ha_nodes WHERE heartbeat_at >= ?`, sqliteTime(since)).Scan(&count)
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
func (c *Coordination) PruneNodes(ctx context.Context, cutoff time.Time) error {
	var err error
	switch c.db.engine {
	case EnginePostgres:
		_, err = c.db.pool.Exec(ctx, `DELETE FROM ha_nodes WHERE heartbeat_at < $1`, cutoff)
	case EngineSQLite:
		_, err = c.db.sqWrite.ExecContext(ctx, `DELETE FROM ha_nodes WHERE heartbeat_at < ?`, sqliteTime(cutoff))
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
func (c *Coordination) ForeignRootKeyFingerprints(ctx context.Context, nodeID, fingerprint string, since time.Time) ([]string, error) {
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
			 WHERE node_id <> ? AND root_key_fingerprint <> ? AND heartbeat_at >= ?`, nodeID, fingerprint, sqliteTime(since))
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
func (c *Coordination) BumpWindow(ctx context.Context, bucket, subject string, windowStart time.Time) (int64, error) {
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
			bucket, subject, sqliteTime(windowStart)).Scan(&count)
	default:
		return 0, fmt.Errorf("store: coordination window bump on unknown engine %q", c.db.engine)
	}
	if err != nil {
		return 0, fmt.Errorf("store: bump admission window %s/%s: %w", bucket, subject, err)
	}
	return count, nil
}

// AccountFailureState reports an account subject's consecutive-failure count
// and the instant of its most recent failure. The delay deadline is a pure,
// deterministic function of these two values (computed in the admission
// package), so there is no separately written deadline that could be left
// stale or overwritten by a shorter one. ok is false when no failures are
// recorded.
func (c *Coordination) AccountFailureState(ctx context.Context, subject string) (failures int64, lastFailure time.Time, ok bool, err error) {
	switch c.db.engine {
	case EnginePostgres:
		var until sql.NullTime
		e := c.db.pool.QueryRow(ctx,
			`SELECT failures, until_at FROM admission_counters WHERE bucket = $1 AND subject = $2 AND window_start = $3`,
			AccountBucket, subject, accountWindow).Scan(&failures, &until)
		if isNoRows(e) {
			return 0, time.Time{}, false, nil
		}
		if e != nil {
			return 0, time.Time{}, false, fmt.Errorf("store: account failure state %s: %w", subject, e)
		}
		if until.Valid {
			lastFailure = until.Time.UTC()
		}
		return failures, lastFailure, true, nil
	case EngineSQLite:
		var until sql.NullString
		e := c.db.sqRead.QueryRowContext(ctx,
			`SELECT failures, until_at FROM admission_counters WHERE bucket = ? AND subject = ? AND window_start = ?`,
			AccountBucket, subject, sqliteTime(accountWindow)).Scan(&failures, &until)
		if isNoRows(e) {
			return 0, time.Time{}, false, nil
		}
		if e != nil {
			return 0, time.Time{}, false, fmt.Errorf("store: account failure state %s: %w", subject, e)
		}
		if until.Valid && until.String != "" {
			lastFailure, _ = time.Parse(adapterTimeFormat, until.String)
			lastFailure = lastFailure.UTC()
		}
		return failures, lastFailure, true, nil
	default:
		return 0, time.Time{}, false, fmt.Errorf("store: coordination account failure state on unknown engine %q", c.db.engine)
	}
}

// RecordAccountFailure atomically increments the consecutive-failure count for
// an account subject and stamps the failure instant, in one statement. Storing
// the last-failure time (not a precomputed deadline) is what keeps the backoff
// monotonic and race-free: the deadline is derived from failures and this time,
// so a concurrent record can never leave the row admitting again or shorten a
// stronger deadline. now must be datastore time.
func (c *Coordination) RecordAccountFailure(ctx context.Context, subject string, now time.Time) (int64, error) {
	var failures int64
	var err error
	switch c.db.engine {
	case EnginePostgres:
		err = c.db.pool.QueryRow(ctx,
			`INSERT INTO admission_counters (bucket, subject, window_start, failures, until_at)
			 VALUES ($1, $2, $3, 1, $4)
			 ON CONFLICT (bucket, subject, window_start) DO UPDATE
			   SET failures = admission_counters.failures + 1, until_at = $4
			 RETURNING failures`,
			AccountBucket, subject, accountWindow, now).Scan(&failures)
	case EngineSQLite:
		err = c.db.sqWrite.QueryRowContext(ctx,
			`INSERT INTO admission_counters (bucket, subject, window_start, failures, until_at)
			 VALUES (?, ?, ?, 1, ?)
			 ON CONFLICT (bucket, subject, window_start) DO UPDATE
			   SET failures = admission_counters.failures + 1, until_at = ?
			 RETURNING failures`,
			AccountBucket, subject, sqliteTime(accountWindow), sqliteTime(now), sqliteTime(now)).Scan(&failures)
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
func (c *Coordination) PruneAccountBackoff(ctx context.Context, cutoff time.Time) error {
	var err error
	switch c.db.engine {
	case EnginePostgres:
		_, err = c.db.pool.Exec(ctx,
			`DELETE FROM admission_counters WHERE bucket = $1 AND (until_at IS NULL OR until_at < $2)`,
			AccountBucket, cutoff)
	case EngineSQLite:
		_, err = c.db.sqWrite.ExecContext(ctx,
			`DELETE FROM admission_counters WHERE bucket = ? AND (until_at IS NULL OR until_at < ?)`,
			AccountBucket, sqliteTime(cutoff))
	default:
		return fmt.Errorf("store: coordination account prune on unknown engine %q", c.db.engine)
	}
	if err != nil {
		return fmt.Errorf("store: prune account backoff: %w", err)
	}
	return nil
}

// ClearAccount drops an account subject's backoff row on a successful attempt.
func (c *Coordination) ClearAccount(ctx context.Context, subject string) error {
	var err error
	switch c.db.engine {
	case EnginePostgres:
		_, err = c.db.pool.Exec(ctx,
			`DELETE FROM admission_counters WHERE bucket = $1 AND subject = $2 AND window_start = $3`,
			AccountBucket, subject, accountWindow)
	case EngineSQLite:
		_, err = c.db.sqWrite.ExecContext(ctx,
			`DELETE FROM admission_counters WHERE bucket = ? AND subject = ? AND window_start = ?`,
			AccountBucket, subject, sqliteTime(accountWindow))
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
func (c *Coordination) PruneAdmissionWindows(ctx context.Context, cutoff time.Time) error {
	var err error
	switch c.db.engine {
	case EnginePostgres:
		_, err = c.db.pool.Exec(ctx,
			`DELETE FROM admission_counters WHERE bucket <> $1 AND window_start < $2`,
			AccountBucket, cutoff)
	case EngineSQLite:
		_, err = c.db.sqWrite.ExecContext(ctx,
			`DELETE FROM admission_counters WHERE bucket <> ? AND window_start < ?`,
			AccountBucket, sqliteTime(cutoff))
	default:
		return fmt.Errorf("store: coordination prune on unknown engine %q", c.db.engine)
	}
	if err != nil {
		return fmt.Errorf("store: prune admission windows: %w", err)
	}
	return nil
}
