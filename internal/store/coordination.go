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

// RenewLease extends the lease deadline only while owner still holds it at the
// given fence. A takeover changes owner and increments fence, so a stale
// holder's renewal matches zero rows and held is false: that is the fenced
// write the HA design turns on.
func (c *Coordination) RenewLease(ctx context.Context, name, owner string, fence int64, expires time.Time) (held bool, err error) {
	var affected int64
	switch c.db.engine {
	case EnginePostgres:
		tag, e := c.db.pool.Exec(ctx,
			`UPDATE singleton_leases SET expires_at = $1
			 WHERE name = $2 AND owner = $3 AND fence_token = $4`,
			expires, name, owner, fence)
		if e != nil {
			return false, fmt.Errorf("store: renew lease %q: %w", name, e)
		}
		affected = tag.RowsAffected()
	case EngineSQLite:
		res, e := c.db.sqWrite.ExecContext(ctx,
			`UPDATE singleton_leases SET expires_at = ?
			 WHERE name = ? AND owner = ? AND fence_token = ?`,
			sqliteTime(expires), name, owner, fence)
		if e != nil {
			return false, fmt.Errorf("store: renew lease %q: %w", name, e)
		}
		affected, _ = res.RowsAffected()
	default:
		return false, fmt.Errorf("store: coordination lease renew on unknown engine %q", c.db.engine)
	}
	return affected == 1, nil
}

// ReleaseLease drops the lease on graceful shutdown so a standby can take over
// immediately rather than waiting out the TTL. It only releases the row the
// caller still holds at its fence.
func (c *Coordination) ReleaseLease(ctx context.Context, name, owner string, fence int64) error {
	var err error
	switch c.db.engine {
	case EnginePostgres:
		_, err = c.db.pool.Exec(ctx,
			`DELETE FROM singleton_leases WHERE name = $1 AND owner = $2 AND fence_token = $3`,
			name, owner, fence)
	case EngineSQLite:
		_, err = c.db.sqWrite.ExecContext(ctx,
			`DELETE FROM singleton_leases WHERE name = ? AND owner = ? AND fence_token = ?`,
			name, owner, fence)
	default:
		return fmt.Errorf("store: coordination lease release on unknown engine %q", c.db.engine)
	}
	if err != nil {
		return fmt.Errorf("store: release lease %q: %w", name, err)
	}
	return nil
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

// ForeignRootKeyFingerprints returns the distinct root-key fingerprints
// recorded by any OTHER node that differ from the caller's. A non-empty result
// is a mixed-root-key misconfiguration: the caller refuses to serve rather
// than join an installation whose key authority it does not share.
func (c *Coordination) ForeignRootKeyFingerprints(ctx context.Context, nodeID, fingerprint string) ([]string, error) {
	var others []string
	switch c.db.engine {
	case EnginePostgres:
		r, err := c.db.pool.Query(ctx,
			`SELECT DISTINCT root_key_fingerprint FROM ha_nodes
			 WHERE node_id <> $1 AND root_key_fingerprint <> $2`, nodeID, fingerprint)
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
			 WHERE node_id <> ? AND root_key_fingerprint <> ?`, nodeID, fingerprint)
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

// AccountBackoff reports the current delay deadline for an account subject and
// whether one is recorded. A live deadline in the future blocks the attempt.
func (c *Coordination) AccountBackoff(ctx context.Context, subject string) (deadline time.Time, ok bool, err error) {
	switch c.db.engine {
	case EnginePostgres:
		var until sql.NullTime
		e := c.db.pool.QueryRow(ctx,
			`SELECT until_at FROM admission_counters WHERE bucket = $1 AND subject = $2 AND window_start = $3`,
			AccountBucket, subject, accountWindow).Scan(&until)
		if isNoRows(e) {
			return time.Time{}, false, nil
		}
		if e != nil {
			return time.Time{}, false, fmt.Errorf("store: account backoff %s: %w", subject, e)
		}
		if !until.Valid {
			return time.Time{}, false, nil
		}
		return until.Time.UTC(), true, nil
	case EngineSQLite:
		var until sql.NullString
		e := c.db.sqRead.QueryRowContext(ctx,
			`SELECT until_at FROM admission_counters WHERE bucket = ? AND subject = ? AND window_start = ?`,
			AccountBucket, subject, sqliteTime(accountWindow)).Scan(&until)
		if isNoRows(e) {
			return time.Time{}, false, nil
		}
		if e != nil {
			return time.Time{}, false, fmt.Errorf("store: account backoff %s: %w", subject, e)
		}
		if !until.Valid || until.String == "" {
			return time.Time{}, false, nil
		}
		t, _ := time.Parse(adapterTimeFormat, until.String)
		return t.UTC(), true, nil
	default:
		return time.Time{}, false, fmt.Errorf("store: coordination account backoff on unknown engine %q", c.db.engine)
	}
}

// RecordAccountFailure increments the consecutive-failure count for an account
// subject and returns the new count. The caller computes the delay deadline
// from the count (the backoff curve lives in the admission package) and writes
// it with SetAccountDeadline.
func (c *Coordination) RecordAccountFailure(ctx context.Context, subject string) (int64, error) {
	var failures int64
	var err error
	switch c.db.engine {
	case EnginePostgres:
		err = c.db.pool.QueryRow(ctx,
			`INSERT INTO admission_counters (bucket, subject, window_start, failures)
			 VALUES ($1, $2, $3, 1)
			 ON CONFLICT (bucket, subject, window_start) DO UPDATE
			   SET failures = admission_counters.failures + 1
			 RETURNING failures`,
			AccountBucket, subject, accountWindow).Scan(&failures)
	case EngineSQLite:
		err = c.db.sqWrite.QueryRowContext(ctx,
			`INSERT INTO admission_counters (bucket, subject, window_start, failures)
			 VALUES (?, ?, ?, 1)
			 ON CONFLICT (bucket, subject, window_start) DO UPDATE
			   SET failures = admission_counters.failures + 1
			 RETURNING failures`,
			AccountBucket, subject, sqliteTime(accountWindow)).Scan(&failures)
	default:
		return 0, fmt.Errorf("store: coordination record failure on unknown engine %q", c.db.engine)
	}
	if err != nil {
		return 0, fmt.Errorf("store: record account failure %s: %w", subject, err)
	}
	return failures, nil
}

// SetAccountDeadline writes the current delay deadline for an account subject.
func (c *Coordination) SetAccountDeadline(ctx context.Context, subject string, deadline time.Time) error {
	var err error
	switch c.db.engine {
	case EnginePostgres:
		_, err = c.db.pool.Exec(ctx,
			`UPDATE admission_counters SET until_at = $1 WHERE bucket = $2 AND subject = $3 AND window_start = $4`,
			deadline, AccountBucket, subject, accountWindow)
	case EngineSQLite:
		_, err = c.db.sqWrite.ExecContext(ctx,
			`UPDATE admission_counters SET until_at = ? WHERE bucket = ? AND subject = ? AND window_start = ?`,
			sqliteTime(deadline), AccountBucket, subject, sqliteTime(accountWindow))
	default:
		return fmt.Errorf("store: coordination set deadline on unknown engine %q", c.db.engine)
	}
	if err != nil {
		return fmt.Errorf("store: set account deadline %s: %w", subject, err)
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
