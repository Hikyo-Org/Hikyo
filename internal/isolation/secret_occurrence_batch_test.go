package isolation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
	sqlite "modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// Exercise the public proof-consuming boundary on real engines with a counting
// transaction. Authorization reads are excluded from the membership budget.
func TestSecretOccurrenceBatches(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		for i, scope := range []domain.Scope{scopeEnv(orgA, prjA1, envA1), scopeEnv(orgA, prjA1, envProd), scopeEnv(orgA, prjA2, envA2), scopeEnv(orgB, prjB1, envB1)} {
			execRaw(t, db, fmt.Sprintf(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<250) INSERT INTO secret_value_occurrences(value_entry_id,org_id,project_id,environment_id) SELECT 'occ_' || CAST(i*4+%d AS TEXT),'%s','%s','%s' FROM n`, i, scope.Org, scope.Project, scope.Env))
		}
		count := 0
		failAt := 0
		failure := errors.New("injected occurrence read failure")
		tok := authz.NewTxToken()
		defer tok.Invalidate()
		var repos store.ReadRepos
		var resolver *authn.Resolver
		if db.Engine() == store.EnginePostgres {
			transaction, err := db.PG().BeginTx(t.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = transaction.Rollback(t.Context()) }()
			counted := failingOccurrencePG{countingPGTx: countingPGTx{tx: transaction, n: &count}, failAt: &failAt, failure: failure}
			repos = store.PGTxReadRepos(counted, tok)
			resolver = authn.NewPG(counted)
		} else {
			connection, err := db.SQLiteRead().Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = connection.Close() }()
			if _, err := sqlite.Limit(connection, sqlitelib.SQLITE_LIMIT_VARIABLE_NUMBER, 999); err != nil {
				t.Fatal(err)
			}
			transaction, err := connection.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = transaction.Rollback() }()
			counted := failingOccurrenceSQLite{countingSqliteTx: countingSqliteTx{tx: transaction, n: &count}, failAt: &failAt, failure: failure}
			repos = store.SQLiteTxReadRepos(counted, tok)
			resolver = authn.NewSQLite(counted)
		}
		az := authz.NewTxAuthorizer(resolver, tok)
		proof, err := az.Authorize(t.Context(), authz.Identity{Principal: custodian}, authz.OpValueExport, scopeEnv(orgA, prjA1, envA1))
		if err != nil {
			t.Fatal(err)
		}
		reader := repos.Snapshots()
		for _, n := range []int{0, 1, 4, 499, 500, 501, 1000, 1001, 10000} {
			t.Run(fmt.Sprint(n), func(t *testing.T) {
				ids := make([]string, n)
				want := map[string]bool{}
				for i := range ids {
					ids[i] = fmt.Sprintf("occ_%d", i)
				}
				if n > 500 {
					ids[500] = "occ_0"
				} // Duplicate across a batch boundary.
				for i, id := range ids {
					if i < 1004 && i%4 == 0 {
						want[id] = true
					}
				}

				before := count
				got, err := reader.SecretValueOccurrenceIDsIn(t.Context(), proof, ids)
				if err != nil || got == nil || !maps.Equal(got, want) {
					t.Fatalf("membership=%v err=%v want=%v", got, err, want)
				}
				if queries := count - before; queries != (n+499)/500 {
					t.Fatalf("queries=%d want=%d", queries, (n+499)/500)
				}
				// Compare representative hits and misses at every boundary against the
				// old point SQL, independently of the expected fixture map.
				for _, id := range []string{"occ_0", "occ_1", "occ_2", "occ_3", "occ_499", "occ_500", "occ_501", "occ_999", "occ_1000", "occ_1001", "occ_9999"} {
					if !slices.Contains(ids, id) {
						continue
					}
					expected := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM secret_value_occurrences WHERE org_id='org_a' AND project_id='prj_a1' AND environment_id='env_a1' AND value_entry_id='%s'`, id)) > 0
					if got[id] != expected {
						t.Fatalf("point membership differs for %s", id)
					}
				}
			})
		}
		for _, ids := range [][]string{{"", "missing", "occ_1", "occ_2", "occ_3", "' OR TRUE --"}, {"occ_0", "occ_0", "occ_0"}} {
			before := count
			got, err := reader.SecretValueOccurrenceIDsIn(t.Context(), proof, ids)
			want := map[string]bool{}
			if slices.Contains(ids, "occ_0") {
				want["occ_0"] = true
			}
			if err != nil || got == nil || !maps.Equal(got, want) || count-before != 1 {
				t.Fatalf("edge membership=%v error=%v queries=%d", got, err, count-before)
			}
		}
		// Refusal precedes the empty-input fast path, and issues no membership SQL.
		wrong, err := az.Authorize(t.Context(), authz.Identity{Principal: custodian}, authz.OpEnvRead, scopeEnv(orgA, prjA1, envA1))
		if err != nil {
			t.Fatal(err)
		}
		other := authz.NewTxToken()
		defer other.Invalidate()
		foreign, err := authz.NewTxAuthorizer(resolver, other).Authorize(t.Context(), authz.Identity{Principal: custodian}, authz.OpValueExport, scopeEnv(orgA, prjA1, envA1))
		if err != nil {
			t.Fatal(err)
		}
		for _, denied := range []authz.Proof{nil, wrong, foreign} {
			for _, ids := range [][]string{nil, {"occ_0"}} {
				before := count
				got, err := reader.SecretValueOccurrenceIDsIn(t.Context(), denied, ids)
				if err == nil || got != nil || count != before {
					t.Fatalf("refusal: map=%v err=%v queries=%d", got, err, count-before)
				}
			}
		}
		failAt = count + 2
		if got, err := reader.SecretValueOccurrenceIDsIn(t.Context(), proof, slices.Repeat([]string{"occ_0"}, 501)); err != failure || got != nil {
			t.Fatalf("later batch failure: map=%v error=%v", got, err)
		}
		failAt = 0
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if got, err := reader.SecretValueOccurrenceIDsIn(ctx, proof, []string{"occ_0"}); !errors.Is(err, context.Canceled) || got != nil {
			t.Fatalf("canceled membership=%v error=%v", got, err)
		}
		tok.Invalidate()
		if got, err := reader.SecretValueOccurrenceIDsIn(t.Context(), proof, slices.Repeat([]string{"occ_0"}, 501)); err == nil || got != nil {
			t.Fatalf("expired proof: %v %v", got, err)
		}
	})
}

// Inject an execution failure after one successful batch without changing the
// real generated query or proof path.
type failingOccurrenceSQLite struct {
	countingSqliteTx
	failAt  *int
	failure error
}

func (c failingOccurrenceSQLite) QueryContext(ctx context.Context, q string, args ...interface{}) (*sql.Rows, error) {
	if *c.n+1 == *c.failAt {
		*c.n++
		return nil, c.failure
	}
	return c.countingSqliteTx.QueryContext(ctx, q, args...)
}

type failingOccurrencePG struct {
	countingPGTx
	failAt  *int
	failure error
}

func (c failingOccurrencePG) Query(ctx context.Context, q string, args ...interface{}) (pgx.Rows, error) {
	if *c.n+1 == *c.failAt {
		*c.n++
		return nil, c.failure
	}
	return c.countingPGTx.Query(ctx, q, args...)
}
