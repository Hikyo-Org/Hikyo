package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"modernc.org/sqlite"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Instrument the real SQLite driver, rather than inferring transaction counts
// from delivered events. No runtime instrumentation or authority cache is added.
var advisoryBenchmarkDriver = &advisoryCountingDriver{Driver: &sqlite.Driver{}}

func init() { sql.Register("hikyo-advisory-benchmark", advisoryBenchmarkDriver) }

type advisoryCountingDriver struct {
	driver.Driver
	transactions atomic.Int64
}

func (d *advisoryCountingDriver) Open(name string) (driver.Conn, error) {
	c, err := d.Driver.Open(name)
	if err != nil {
		return nil, err
	}
	return &advisoryCountingConn{Conn: c, transactions: &d.transactions}, nil
}

type advisoryCountingConn struct {
	driver.Conn
	transactions *atomic.Int64
}

func (c *advisoryCountingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	transaction, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if err == nil {
		c.transactions.Add(1)
	}
	return transaction, err
}
func (c *advisoryCountingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
}
func (c *advisoryCountingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
}

// BenchmarkAdvisoryFanout measures the post-commit published() fan-out plus
// real per-event session resolution and authorization, not publish encryption
// or commit latency. Fixture creation and subscription admission are untimed.
// Consumers actively drain; delivery is explicitly counted, never assumed to
// equal keys*streams when a burst overflows a bounded raw queue.
func BenchmarkAdvisoryFanout(b *testing.B) {
	for _, streams := range []int{1, 8, 32} {
		for _, keys := range []int{8, 64, 300} {
			b.Run(fmt.Sprintf("streams=%d/keys=%d", streams, keys), func(b *testing.B) {
				f := newWorkflowBenchmarkDrafts(b, keys, false)
				projectScope := f.scope
				projectScope.Env = ""
				if err := f.values.DB.Close(); err != nil {
					b.Fatal(err)
				}
				db, err := store.Open(b.Context(), store.Config{Engine: store.EngineSQLite, Path: f.path, SQLiteDriver: "hikyo-advisory-benchmark"}, f.admission)
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { _ = db.Close() })
				advisory := NewAdvisory()
				revisions := &Revisions{DB: db, Advisory: advisory}
				// Eight principals x four streams reaches the org cap without violating
				// the principal cap. Noise grants are unique principal/org pairs,
				// capped at 1,000 per org, and make the #974 index relevant.
				exec := func(query string, args ...any) {
					b.Helper()
					if _, err := db.SQLiteWrite().ExecContext(b.Context(), query, args...); err != nil {
						b.Fatal(err)
					}
				}
				exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<999)
 INSERT INTO principals(id,kind,created_at) SELECT 'usr_advisory_noise_' || i,'human','2026-01-01T00:00:00Z' FROM n`)
				exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<9)
 INSERT INTO orgs(id,name,active,metadata,created_at) SELECT 'org_advisory_noise_' || i,'advisory-noise-' || i,TRUE,'{}','2026-01-01T00:00:00Z' FROM n`)
				exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<9999)
     INSERT INTO grants(id,principal_id,capability,org_id,created_at) SELECT 'grt_noise_' || i,'usr_advisory_noise_' || (i % 1000),'read','org_advisory_noise_' || (i / 1000),'2026-01-01T00:00:00Z' FROM n`)
				// Keep noise inside the same grant bounds and identity
				// uniqueness enforced by the service's minting path.
				var uniqueNoise, maxNoisePerOrg int
				err = db.SQLiteWrite().QueryRowContext(b.Context(), `SELECT COUNT(*) FROM
 (SELECT principal_id,capability,org_id,project_id,env_id FROM grants WHERE id LIKE 'grt_noise_%'
 GROUP BY principal_id,capability,org_id,project_id,env_id)`).Scan(&uniqueNoise)
				if err != nil || uniqueNoise != 10000 {
					b.Fatalf("unique noise grants=%d err=%v", uniqueNoise, err)
				}
				err = db.SQLiteWrite().QueryRowContext(b.Context(), `SELECT MAX(n) FROM
 (SELECT COUNT(*) AS n FROM grants WHERE id LIKE 'grt_noise_%' GROUP BY org_id)`).Scan(&maxNoisePerOrg)
				if err != nil || maxNoisePerOrg != 1000 {
					b.Fatalf("noise grants per org=%d err=%v", maxNoisePerOrg, err)
				}
				actors := make([]Actor, 8)
				for i := range actors {
					principal := domain.PrincipalID(fmt.Sprintf("usr_advisory_benchmark_%d", i))
					exec(`INSERT INTO principals(id,kind,created_at) VALUES (?,'human','2026-01-01T00:00:00Z')`, string(principal))
					exec(`INSERT INTO grants(id,principal_id,capability,org_id,project_id,created_at) VALUES (?,?,'read',?,?,'2026-01-01T00:00:00Z')`, fmt.Sprintf("grt_advisory_%d", i), string(principal), string(f.scope.Org), string(f.scope.Project))
					artifact, verifier, err := crypto.NewArtifact(crypto.ArtifactCLISession)
					if err != nil {
						b.Fatal(err)
					}
					now := time.Now().UTC()
					err = tx.Write(b.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
						generation, err := az.PrincipalGeneration(ctx, principal)
						if err != nil {
							return err
						}
						epoch, err := az.CredentialEpoch(ctx)
						if err != nil {
							return err
						}
						return az.MintSession(ctx, authz.NewSession{ID: fmt.Sprintf("ses_advisory_%d", i), PrincipalID: principal, Verifier: verifier, Artifact: "cli", SessionGeneration: generation, CredentialEpoch: epoch, AuthMethod: "local-password", Factors: `["password"]`, AuthenticatedAt: now, CreatedAt: now, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(24 * time.Hour)})
					})
					if err != nil {
						b.Fatal(err)
					}
					actors[i] = Bearer(artifact)
				}
				changes := make([]ChangedKey, keys)
				for i := range changes {
					name := fmt.Sprintf("SETTING_%04d", i)
					changes[i] = ChangedKey{KeyID: f.keyIDs[name], Name: name}
				}
				envs := []PublishedEnvironment{{EnvironmentID: string(f.scope.Env), Revision: 1, ChangedKeys: changes}}
				var processed, dropped, transactions int64
				b.ResetTimer()
				for range b.N {
					b.StopTimer()
					ctx, cancel := context.WithTimeout(b.Context(), 15*time.Second)
					channels := make([]<-chan AdvisoryEvent, streams)
					for i := range streams {
						channels[i], err = revisions.Watch(ctx, actors[i/4], projectScope)
						if err != nil {
							cancel()
							b.Fatal(err)
						}
					}
					if streams == advisoryOrgLimit {
						if _, err := revisions.Watch(ctx, f.actor, projectScope); err != ErrAdvisoryOrgLimit {
							cancel()
							b.Fatalf("org cap: %v", err)
						}
					}
					counts := make(chan int, streams)
					for _, ch := range channels {
						go func() {
							n := 0
							for n < keys+1 {
								select {
								case _, ok := <-ch:
									if !ok {
										counts <- n
										return
									}
									n++
								case <-ctx.Done():
									counts <- n
									return
								}
							}
							counts <- n
						}()
					}
					before := advisoryBenchmarkDriver.transactions.Load()
					beforeProcessed := processed
					b.StartTimer()
					advisory.published(f.scope, envs)
					for range streams {
						n := <-counts
						processed += int64(n)
						if n < keys+1 {
							dropped++
						}
					}
					b.StopTimer()
					counted := advisoryBenchmarkDriver.transactions.Load() - before
					transactions += counted
					if counted != processed-beforeProcessed {
						cancel()
						b.Fatalf("read transactions=%d, processed=%d", counted, processed-beforeProcessed)
					}
					if ctx.Err() != nil {
						cancel()
						b.Fatal("fan-out failed to drain before deadline")
					}
					cancel()
					// Wait for each Watch goroutine to release its subscription before the
					// next iteration, including survivors of a short burst.
					for _, ch := range channels {
						for range ch {
						}
					}
				}
				b.ReportMetric(float64(processed)/float64(b.N), "events/op")
				b.ReportMetric(float64(dropped)/float64(b.N), "dropped-streams/op")
				b.ReportMetric(float64(transactions)/float64(b.N), "read-tx/op")
				b.ReportMetric(float64(streams*(keys+1)), "offered-events/op")
			})
		}
	}
}
