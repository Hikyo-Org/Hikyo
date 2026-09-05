package store_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestCoordinationSQLite(t *testing.T) {
	cfg := store.Config{Engine: store.EngineSQLite, Path: t.TempDir() + "/coord.db"}
	runCoordinationInvariants(t, openKeyTestDB(t, cfg))
}

func TestCoordinationPostgres(t *testing.T) {
	runCoordinationInvariants(t, postgresTestDB(t))
}

func runCoordinationInvariants(t *testing.T, db *store.DB) {
	t.Helper()
	c := db.Coordination()
	ctx := t.Context()
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	t.Run("lease_single_holder_and_fenced_takeover", func(t *testing.T) {
		// A acquires the fresh lease.
		fenceA, held, err := c.ClaimLease(ctx, "scheduler", "node-a", base, base.Add(30*time.Second))
		if err != nil || !held {
			t.Fatalf("A claim: held=%v err=%v", held, err)
		}
		if fenceA != 1 {
			t.Fatalf("first fence = %d, want 1", fenceA)
		}
		// B cannot claim while A's lease is live.
		if _, held, err := c.ClaimLease(ctx, "scheduler", "node-b", base.Add(5*time.Second), base.Add(35*time.Second)); err != nil || held {
			t.Fatalf("B claim while live: held=%v err=%v (want held=false)", held, err)
		}
		// A renews within its term (now < expiry, still live).
		if ok, err := c.RenewLease(ctx, "scheduler", "node-a", fenceA, base.Add(10*time.Second), base.Add(40*time.Second)); err != nil || !ok {
			t.Fatalf("A renew: ok=%v err=%v", ok, err)
		}
		// A renewal past the lease deadline cannot revive an expired term.
		if ok, err := c.RenewLease(ctx, "scheduler", "node-a", fenceA, base.Add(45*time.Second), base.Add(75*time.Second)); err != nil || ok {
			t.Fatalf("expired-term renew: ok=%v err=%v (want ok=false)", ok, err)
		}
		// After the (renewed) lease expires, B takes over with a higher fence.
		fenceB, held, err := c.ClaimLease(ctx, "scheduler", "node-b", base.Add(41*time.Second), base.Add(71*time.Second))
		if err != nil || !held {
			t.Fatalf("B takeover: held=%v err=%v", held, err)
		}
		if fenceB <= fenceA {
			t.Fatalf("takeover fence = %d, want > %d", fenceB, fenceA)
		}
		// The stale leader's fenced write now affects zero rows: A no longer holds.
		if ok, err := c.RenewLease(ctx, "scheduler", "node-a", fenceA, base.Add(50*time.Second), base.Add(90*time.Second)); err != nil || ok {
			t.Fatalf("stale A renew: ok=%v err=%v (want ok=false)", ok, err)
		}
		// LeaseHolder reports B live.
		owner, _, live, err := c.LeaseHolder(ctx, "scheduler", base.Add(50*time.Second))
		if err != nil || owner != "node-b" || !live {
			t.Fatalf("holder = %q live=%v err=%v, want node-b live", owner, live, err)
		}
		// Release preserves the monotonic fence: a fresh claim increments past
		// the released fence rather than resetting to 1.
		if err := c.ReleaseLease(ctx, "scheduler", "node-b", fenceB); err != nil {
			t.Fatalf("release: %v", err)
		}
		fenceC, held, err := c.ClaimLease(ctx, "scheduler", "node-c", base.Add(51*time.Second), base.Add(81*time.Second))
		if err != nil || !held {
			t.Fatalf("claim after release: held=%v err=%v", held, err)
		}
		if fenceC <= fenceB {
			t.Fatalf("post-release fence = %d, want > %d (monotonic across release)", fenceC, fenceB)
		}
	})

	t.Run("admission_windows_shared", func(t *testing.T) {
		win := base.Truncate(time.Minute)
		n1, err := c.BumpWindow(ctx, store.IPBucket, "1.2.3.4", win)
		if err != nil || n1 != 1 {
			t.Fatalf("bump 1 = %d err=%v", n1, err)
		}
		n2, err := c.BumpWindow(ctx, store.IPBucket, "1.2.3.4", win)
		if err != nil || n2 != 2 {
			t.Fatalf("bump 2 = %d err=%v", n2, err)
		}
		// A different window starts fresh.
		if n, err := c.BumpWindow(ctx, store.IPBucket, "1.2.3.4", win.Add(time.Minute)); err != nil || n != 1 {
			t.Fatalf("new window bump = %d err=%v", n, err)
		}
	})

	t.Run("account_backoff", func(t *testing.T) {
		// Timestamps are datastore-clock (Postgres now()), so assert on
		// behaviour and monotonicity rather than exact injected instants.
		if _, _, _, ok, err := c.AccountFailureState(ctx, "acct"); err != nil || ok {
			t.Fatalf("fresh state: ok=%v err=%v (want none)", ok, err)
		}
		// Stamp with the datastore clock so the sqlite leg (which uses the passed
		// time) agrees with the dbNow the state read reports.
		recNow, err := c.Now(ctx)
		if err != nil {
			t.Fatalf("now: %v", err)
		}
		f, err := c.RecordAccountFailure(ctx, "acct", recNow)
		if err != nil || f != 1 {
			t.Fatalf("failure 1 = %d err=%v", f, err)
		}
		_, last1, _, _, _ := c.AccountFailureState(ctx, "acct")
		if f, err := c.RecordAccountFailure(ctx, "acct", recNow); err != nil || f != 2 {
			t.Fatalf("failure 2 = %d err=%v", f, err)
		}
		failures, last2, dbNow, ok, err := c.AccountFailureState(ctx, "acct")
		if err != nil || !ok || failures != 2 {
			t.Fatalf("state = (%d) ok=%v err=%v, want failures 2", failures, ok, err)
		}
		if last2.Before(last1) {
			t.Fatalf("last-failure moved backwards: %v -> %v", last1, last2)
		}
		if last2.IsZero() || dbNow.IsZero() {
			t.Fatal("last-failure or dbNow unset")
		}
		// A cutoff before the failure never sweeps a fresh row.
		if err := c.PruneAccountBackoff(ctx, dbNow.Add(-time.Hour)); err != nil {
			t.Fatalf("prune fresh: %v", err)
		}
		if _, _, _, ok, _ := c.AccountFailureState(ctx, "acct"); !ok {
			t.Fatal("fresh account row was pruned")
		}
		// A cutoff after the failure sweeps it.
		if err := c.PruneAccountBackoff(ctx, dbNow.Add(time.Hour)); err != nil {
			t.Fatalf("prune stale: %v", err)
		}
		if _, _, _, ok, _ := c.AccountFailureState(ctx, "acct"); ok {
			t.Fatal("old account row survived prune")
		}
		// Clear removes the row on success.
		if _, err := c.RecordAccountFailure(ctx, "acct2", recNow); err != nil {
			t.Fatalf("record acct2: %v", err)
		}
		if err := c.ClearAccount(ctx, "acct2"); err != nil {
			t.Fatalf("clear: %v", err)
		}
		if _, _, _, ok, err := c.AccountFailureState(ctx, "acct2"); err != nil || ok {
			t.Fatalf("after clear: ok=%v err=%v (want none)", ok, err)
		}
	})

	t.Run("datastore_clock", func(t *testing.T) {
		if _, err := c.Now(ctx); err != nil {
			t.Fatalf("datastore now: %v", err)
		}
	})

	t.Run("node_registry_and_foreign_fingerprint", func(t *testing.T) {
		now := base
		mustUpsert := func(id, fp string) {
			t.Helper()
			if err := c.UpsertNode(ctx, store.HANode{NodeID: id, BinaryVersion: "test", SchemaVersion: 36, RootKeyFingerprint: fp, StartedAt: now, HeartbeatAt: now}); err != nil {
				t.Fatalf("upsert %s: %v", id, err)
			}
		}
		mustUpsert("node-a", "fp-shared")
		mustUpsert("node-b", "fp-shared")
		live := now.Add(-time.Second)
		// A node sharing the fingerprint sees no foreign fingerprints.
		if fps, err := c.ForeignRootKeyFingerprints(ctx, "node-b", "fp-shared", live); err != nil || len(fps) != 0 {
			t.Fatalf("foreign for matching = %v err=%v, want none", fps, err)
		}
		// A node with a different fingerprint sees the installation's fingerprint.
		if fps, err := c.ForeignRootKeyFingerprints(ctx, "node-c", "fp-different", live); err != nil || len(fps) == 0 {
			t.Fatalf("foreign for mismatch = %v err=%v, want non-empty", fps, err)
		}
		// A stale peer (heartbeat before the liveness cutoff) no longer vetoes.
		if fps, err := c.ForeignRootKeyFingerprints(ctx, "node-c", "fp-different", now.Add(time.Minute)); err != nil || len(fps) != 0 {
			t.Fatalf("foreign with stale peers = %v err=%v, want none", fps, err)
		}
		if n, err := c.CountLiveNodes(ctx, live); err != nil || n != 2 {
			t.Fatalf("live nodes = %d err=%v, want 2", n, err)
		}
		if n, err := c.CountLiveNodes(ctx, now.Add(time.Minute)); err != nil || n != 0 {
			t.Fatalf("live nodes after cutoff = %d err=%v, want 0", n, err)
		}
		// PruneNodes drops rows heartbeat before the cutoff.
		if err := c.PruneNodes(ctx, now.Add(time.Minute)); err != nil {
			t.Fatalf("prune nodes: %v", err)
		}
		if n, err := c.CountLiveNodes(ctx, now.Add(-time.Hour)); err != nil || n != 0 {
			t.Fatalf("live nodes after prune = %d err=%v, want 0", n, err)
		}
	})

	t.Run("register_node_checked_refuses_mixed_root", func(t *testing.T) {
		now := base.Add(time.Hour) // fresh window; earlier subtest pruned ha_nodes
		live := now.Add(-time.Second)
		seed := store.HANode{NodeID: "reg-a", BinaryVersion: "test", SchemaVersion: 36, RootKeyFingerprint: "fp-a", StartedAt: now, HeartbeatAt: now}
		if err := c.RegisterNodeChecked(ctx, seed, live); err != nil {
			t.Fatalf("first register: %v", err)
		}
		// A second node with a different fingerprint is refused.
		mixed := store.HANode{NodeID: "reg-b", BinaryVersion: "test", SchemaVersion: 36, RootKeyFingerprint: "fp-b", StartedAt: now, HeartbeatAt: now}
		if err := c.RegisterNodeChecked(ctx, mixed, live); !errors.Is(err, store.ErrMixedRootKey) {
			t.Fatalf("mixed-root register err = %v, want ErrMixedRootKey", err)
		}
		// A node sharing the fingerprint registers.
		matching := store.HANode{NodeID: "reg-c", BinaryVersion: "test", SchemaVersion: 36, RootKeyFingerprint: "fp-a", StartedAt: now, HeartbeatAt: now}
		if err := c.RegisterNodeChecked(ctx, matching, live); err != nil {
			t.Fatalf("matching register: %v", err)
		}
		if err := c.PruneNodes(ctx, now.Add(time.Hour)); err != nil {
			t.Fatalf("cleanup prune: %v", err)
		}
	})

	t.Run("mcp_admission_is_shared_and_bounded", func(t *testing.T) {
		const ttl = time.Minute
		seedMCPSubject(t, db, "mcp-rate-principal", "mcp-rate-org")
		leases := make([]string, 0, store.MCPPrincipalLimit)
		for i := range store.MCPPrincipalLimit {
			id := fmt.Sprintf("mcp-principal-call-%d", i)
			if err := c.AcquireMCP(ctx, id, "mcp-rate-principal", "mcp-rate-org", ttl); err != nil {
				t.Fatalf("principal claim %d: %v", i+1, err)
			}
			leases = append(leases, id)
		}
		beforeRefusal := mcpBucketDeadline(t, db, "mcp-rate-principal")
		if err := db.Coordination().AcquireMCP(ctx, "mcp-principal-overflow", "mcp-rate-principal", "mcp-rate-org", ttl); !errors.Is(err, store.ErrMCPAdmissionLimited) {
			t.Fatalf("principal overflow = %v, want ErrMCPAdmissionLimited", err)
		}
		if after := mcpBucketDeadline(t, db, "mcp-rate-principal"); !after.Equal(beforeRefusal) {
			t.Fatalf("rejected concurrency claim changed token bucket: %v -> %v", beforeRefusal, after)
		}
		for _, id := range leases {
			if err := c.ReleaseMCP(ctx, id); err != nil {
				t.Fatalf("release %s: %v", id, err)
			}
		}

		// The rejected concurrency claim did not consume a token: 16 more
		// admitted calls fill the capacity after the 4 successful claims.
		for i := store.MCPPrincipalLimit; i < store.MCPRateCapacity; i++ {
			id := fmt.Sprintf("mcp-rate-call-%d", i)
			if err := c.AcquireMCP(ctx, id, "mcp-rate-principal", "mcp-rate-org", ttl); err != nil {
				t.Fatalf("rate fill %d: %v", i+1, err)
			}
			if err := c.ReleaseMCP(ctx, id); err != nil {
				t.Fatal(err)
			}
		}
		// The real clock may refill while a loaded CI machine runs the
		// preceding calls. Seed an explicitly exhausted bucket for the
		// overflow assertion; do not make correctness depend on 20 database
		// transactions completing within one refill interval.
		exhausted := time.Now().UTC().Add(time.Duration(store.MCPRateCapacity+1) * store.MCPRateRefillInterval)
		if db.Engine() == store.EnginePostgres {
			if _, err := db.PG().Exec(ctx, "UPDATE mcp_rate_buckets SET next_at=$1 WHERE principal_id=$2", exhausted, "mcp-rate-principal"); err != nil {
				t.Fatal(err)
			}
		} else if _, err := db.SQLiteWrite().ExecContext(ctx, "UPDATE mcp_rate_buckets SET next_at=? WHERE principal_id=?", exhausted.Format(time.RFC3339Nano), "mcp-rate-principal"); err != nil {
			t.Fatal(err)
		}
		if err := db.Coordination().AcquireMCP(ctx, "mcp-rate-overflow", "mcp-rate-principal", "mcp-rate-org", ttl); !errors.Is(err, store.ErrMCPAdmissionLimited) {
			t.Fatalf("rate overflow = %v, want ErrMCPAdmissionLimited", err)
		}

		orgLeases := make([]string, 0, store.MCPOrganizationLimit)
		for i := range store.MCPOrganizationLimit + 1 {
			principal := fmt.Sprintf("mcp-org-principal-%d", i)
			seedMCPSubject(t, db, principal, "mcp-shared-org")
			id := fmt.Sprintf("mcp-org-call-%d", i)
			err := db.Coordination().AcquireMCP(ctx, id, principal, "mcp-shared-org", ttl)
			if i == store.MCPOrganizationLimit {
				if !errors.Is(err, store.ErrMCPAdmissionLimited) {
					t.Fatalf("org overflow = %v, want ErrMCPAdmissionLimited", err)
				}
				break
			}
			if err != nil {
				t.Fatalf("org claim %d: %v", i+1, err)
			}
			orgLeases = append(orgLeases, id)
		}
		for _, id := range orgLeases {
			if err := c.ReleaseMCP(ctx, id); err != nil {
				t.Fatal(err)
			}
		}

		instanceLeases := make([]string, 0, store.MCPInstanceLimit)
		for i := range store.MCPInstanceLimit + 1 {
			principal := fmt.Sprintf("mcp-instance-principal-%d", i)
			org := fmt.Sprintf("mcp-instance-org-%d", i/store.MCPOrganizationLimit)
			seedMCPSubject(t, db, principal, org)
			id := fmt.Sprintf("mcp-instance-call-%d", i)
			err := db.Coordination().AcquireMCP(ctx, id, principal, org, ttl)
			if i == store.MCPInstanceLimit {
				if !errors.Is(err, store.ErrMCPAdmissionLimited) {
					t.Fatalf("instance overflow = %v, want ErrMCPAdmissionLimited", err)
				}
				break
			}
			if err != nil {
				t.Fatalf("instance claim %d: %v", i+1, err)
			}
			instanceLeases = append(instanceLeases, id)
		}
		for _, id := range instanceLeases {
			if err := c.ReleaseMCP(ctx, id); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func seedMCPSubject(t *testing.T, db *store.DB, principalID, orgID string) {
	t.Helper()
	now := time.Now().UTC()
	var err error
	switch db.Engine() {
	case store.EngineSQLite:
		_, err = db.SQLiteWrite().ExecContext(t.Context(), `INSERT OR IGNORE INTO orgs (id, name, active, metadata, created_at) VALUES (?, ?, 1, '{}', ?)`, orgID, orgID, now)
		if err == nil {
			_, err = db.SQLiteWrite().ExecContext(t.Context(), `INSERT OR IGNORE INTO principals (id, kind, created_at) VALUES (?, 'machine', ?)`, principalID, now)
		}
	case store.EnginePostgres:
		_, err = db.PG().Exec(t.Context(), `INSERT INTO orgs (id, name, active, metadata, created_at) VALUES ($1, $2, TRUE, '{}', $3) ON CONFLICT (id) DO NOTHING`, orgID, orgID, now)
		if err == nil {
			_, err = db.PG().Exec(t.Context(), `INSERT INTO principals (id, kind, created_at) VALUES ($1, 'machine', $2) ON CONFLICT (id) DO NOTHING`, principalID, now)
		}
	default:
		err = fmt.Errorf("unknown engine %q", db.Engine())
	}
	if err != nil {
		t.Fatalf("seed MCP subject %s/%s: %v", principalID, orgID, err)
	}
}

func mcpBucketDeadline(t *testing.T, db *store.DB, principal string) time.Time {
	t.Helper()
	if db.Engine() == store.EnginePostgres {
		var deadline time.Time
		if err := db.PG().QueryRow(t.Context(), "SELECT next_at FROM mcp_rate_buckets WHERE principal_id=$1", principal).Scan(&deadline); err != nil {
			t.Fatal(err)
		}
		return deadline
	}
	var raw string
	if err := db.SQLiteRead().QueryRowContext(t.Context(), "SELECT next_at FROM mcp_rate_buckets WHERE principal_id=?", principal).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	deadline, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatal(err)
	}
	return deadline
}
