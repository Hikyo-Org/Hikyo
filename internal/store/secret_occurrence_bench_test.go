package store

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// Compare the old point SQL and the generated batch SQL plus membership-map
// construction on an admitted, disposable database. This measures local query
// cost, excluding proof verification, network deployment and service work.
func BenchmarkSecretOccurrenceMembership(b *testing.B) {
	for _, engine := range []Engine{EngineSQLite, EnginePostgres} {
		b.Run(string(engine), func(b *testing.B) {
			cfg := Config{Engine: engine}
			if engine == EngineSQLite {
				cfg.Path = filepath.Join(b.TempDir(), "occurrences.db")
			}
			if engine == EnginePostgres {
				raw := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
				if raw == "" {
					if os.Getenv("CI") != "" {
						b.Fatal("CI requires HIKYO_TEST_POSTGRES_DSN")
					}
					b.Skip("HIKYO_TEST_POSTGRES_DSN not set")
				}
				admin, err := pgx.Connect(b.Context(), raw)
				if err != nil {
					b.Fatal(err)
				}
				name := fmt.Sprintf("hikyo_occurrence_bench_%d", time.Now().UnixNano())
				if _, err = admin.Exec(b.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() {
					_, _ = admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
					_ = admin.Close(context.Background())
				})
				parsed, err := url.Parse(raw)
				if err != nil {
					b.Fatal(err)
				}
				parsed.Path = "/" + name
				cfg.DSN = parsed.String()
			}
			db, err := admittedStoreFixture(b, cfg)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = db.Close() })
			exec := func(sql string) {
				b.Helper()
				if engine == EngineSQLite {
					_, err = db.SQLiteWrite().ExecContext(b.Context(), sql)
				} else {
					_, err = db.PG().Exec(b.Context(), sql)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
			exec(`INSERT INTO orgs(id,name,active,metadata,created_at) VALUES('org_bench','bench',TRUE,'{}','2026-10-18T00:00:00Z')`)
			exec(`INSERT INTO projects(id,org_id,name,created_at) VALUES('prj_bench','org_bench','bench','2026-10-18T00:00:00Z')`)
			exec(`INSERT INTO environments(id,org_id,project_id,name,note,created_at,display_order) VALUES('env_bench','org_bench','prj_bench','bench','','2026-10-18T00:00:00Z',0)`)
			exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<9999) INSERT INTO secret_value_occurrences(value_entry_id,org_id,project_id,environment_id) SELECT 'occ_' || CAST(i AS TEXT),'org_bench','prj_bench','env_bench' FROM n`)
			// The store helper executes inside one transaction. Hold the same
			// connection for both algorithms so pool checkout is not counted N
			// times for the baseline and once per batch for the replacement.
			var sqliteTx *SQLiteTransaction
			var postgresTx *PostgresTransaction
			if engine == EngineSQLite {
				sqliteTx, err = db.BeginSQLite(b.Context(), true)
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { _ = sqliteTx.Rollback() })
			} else {
				postgresTx, err = db.BeginPostgres(b.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { _ = postgresTx.Rollback(context.Background()) })
			}
			sq := sqlitegen.New(sqliteTx)
			pg := pggen.New(postgresTx)
			for _, n := range []int{1, 100, 500, 1000, 10000} {
				ids := make([]string, n)
				for i := range ids {
					ids[i] = fmt.Sprintf("occ_%d", i)
				}
				for _, mode := range []string{"point", "batch"} {
					b.Run(fmt.Sprintf("%d/%s", n, mode), func(b *testing.B) {
						b.ReportAllocs()
						for b.Loop() {
							out := make(map[string]bool, n)
							if mode == "point" {
								for _, id := range ids {
									var count int64
									if engine == EngineSQLite {
										err = sqliteTx.QueryRowContext(b.Context(), `SELECT COUNT(*) FROM secret_value_occurrences WHERE org_id=? AND project_id=? AND environment_id=? AND value_entry_id=?`, "org_bench", "prj_bench", "env_bench", id).Scan(&count)
									} else {
										err = postgresTx.QueryRow(b.Context(), `SELECT COUNT(*) FROM secret_value_occurrences WHERE org_id=$1 AND project_id=$2 AND environment_id=$3 AND value_entry_id=$4`, "org_bench", "prj_bench", "env_bench", id).Scan(&count)
									}
									if err != nil {
										b.Fatal(err)
									}
									if count > 0 {
										out[id] = true
									}
								}
							} else {
								for batch := range slices.Chunk(ids, secretOccurrenceBatchSize) {
									var found []string
									if engine == EngineSQLite {
										found, err = sq.ListSecretValueOccurrenceIDsIn(b.Context(), sqlitegen.ListSecretValueOccurrenceIDsInParams{ChainOrgID: "org_bench", ChainProjectID: "prj_bench", ChainEnvID: "env_bench", ValueEntryIds: batch})
									} else {
										found, err = pg.ListSecretValueOccurrenceIDsIn(b.Context(), pggen.ListSecretValueOccurrenceIDsInParams{ChainOrgID: "org_bench", ChainProjectID: "prj_bench", ChainEnvID: "env_bench", ValueEntryIds: batch})
									}
									if err != nil {
										b.Fatal(err)
									}
									for _, id := range found {
										out[id] = true
									}
								}
							}
							if len(out) != n {
								b.Fatalf("membership count=%d want=%d", len(out), n)
							}
						}
						queries := n
						if mode == "batch" {
							queries = (n + secretOccurrenceBatchSize - 1) / secretOccurrenceBatchSize
						}
						b.ReportMetric(float64(queries), "queries/op")
					})
				}
			}
		})
	}
}
