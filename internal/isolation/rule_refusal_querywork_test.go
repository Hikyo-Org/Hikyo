package isolation

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/jackc/pgx/v5"
)

func TestRuleManagementRefusalQueryWork(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		for _, op := range []authz.Operation{authz.OpRuleCreate, authz.OpRuleRevoke} {
			for _, scope := range []domain.Scope{{Org: orgA, Project: prjA1}, {Org: orgA, Project: "missing-project"}, {Org: "missing-org", Project: prjA1}} {
				count := 0
				token := authz.NewTxToken()
				var resolver *authn.Resolver
				var rollback func()
				if db.Engine() == store.EnginePostgres {
					txn, err := db.PG().BeginTx(t.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
					if err != nil {
						t.Fatal(err)
					}
					rollback = func() { _ = txn.Rollback(t.Context()) }
					resolver = authn.NewPG(countingPGTx{tx: txn, n: &count})
				} else {
					txn, err := db.SQLiteRead().BeginTx(t.Context(), nil)
					if err != nil {
						t.Fatal(err)
					}
					rollback = func() { _ = txn.Rollback() }
					resolver = authn.NewSQLite(countingSqliteTx{tx: txn, n: &count})
				}
				rule := domain.Rule{Org: scope.Org, Capability: domain.CapEdit, Where: domain.Where{Projects: []domain.ProjectID{scope.Project}, EnvMode: domain.AxisAll, KeyMode: domain.AxisOnly, Keys: map[domain.ProjectID][]domain.RuleKeyItem{scope.Project: {{IsFolder: true, Folder: "db"}}}}}
				_, err := authz.NewTxAuthorizer(resolver, token).AuthorizeRule(t.Context(), authz.Identity{Principal: bob}, op, scope, rule)
				token.Invalidate()
				rollback()
				if !errors.Is(err, domain.ErrNotFound) || count != 3 {
					t.Fatalf("%s scope %+v: count=%d err=%v; want chain+grants+rules and uniformnotfound", op, scope, count, err)
				}
			}
		}
	})
}

func TestRulesManageableSharesCallerAndKeyMetadata(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		where := whereIn(domain.AxisAll, nil, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.dbKeyID})
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapManageMembers, Org: orgA, Where: where})
		count := 0
		token := authz.NewTxToken()
		defer token.Invalidate()
		var resolver *authn.Resolver
		if db.Engine() == store.EnginePostgres {
			txn, err := db.PG().BeginTx(t.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			defer txn.Rollback(t.Context())
			resolver = authn.NewPG(countingPGTx{tx: txn, n: &count})
		} else {
			txn, err := db.SQLiteRead().BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer txn.Rollback()
			resolver = authn.NewSQLite(countingSqliteTx{tx: txn, n: &count})
		}
		targets := make([]domain.Rule, 101)
		for i := range targets {
			targets[i] = domain.Rule{Org: orgA, Capability: domain.CapEdit, Where: where}
		}
		targets[100].Where = whereIn(domain.AxisAll, nil, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.strKeyID})
		held, err := authz.NewTxAuthorizer(resolver, token).RulesManageable(t.Context(), authz.Identity{Principal: frank, Class: domain.ClassHuman}, targets, prjA1)
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range held {
			if want != (i < 100) {
				t.Fatalf("target %d held=%v", i, want)
			}
		}
		if count != 4 {
			t.Fatalf("%d queries for 101 rows, want grants+rules+2 unique key metadata lookups", count)
		}
	})
}
