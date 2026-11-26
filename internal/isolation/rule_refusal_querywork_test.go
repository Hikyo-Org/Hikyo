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
				// Rules bind their environment names in the same
				// transaction (#1447): one more read.
				if !errors.Is(err, domain.ErrNotFound) || count != 4 {
					t.Fatalf("%s scope %+v: count=%d err=%v; want chain+grants+rules+environment names and uniform not found", op, scope, count, err)
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
		// Environment names (#1447) add one read binding the caller's rules
		// and one shared by every target, not one per row.
		if count != 6 {
			t.Fatalf("%d queries for 101 rows, want grants+rules+rule environment names+target environment names+2 unique key metadata lookups", count)
		}
	})
}

// A caller refused rule management learns nothing from the environment ids it
// sends (#1447): an id no environment has is refused exactly as one that
// exists, and only a proved caller hears that it names nothing.
func TestRuleCreateRefusalDoesNotProbeEnvironmentIDs(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		spec := func(env domain.EnvID) service.RuleSpec {
			return service.RuleSpec{Target: frank, Capability: domain.CapRead, Org: orgA, Where: domain.Where{
				Projects: []domain.ProjectID{prjA1}, EnvMode: domain.AxisOnly,
				Envs: map[domain.ProjectID][]domain.EnvID{prjA1: {env}}, KeyMode: domain.AxisAll}}
		}
		for _, env := range []domain.EnvID{envA1, "env_no_such"} {
			if _, err := f.rules.Create(t.Context(), service.LocalPrincipal(bob), spec(env)); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("refused caller sending %s: %v, want the uniform not found", env, err)
			}
		}
		if _, err := f.rules.Create(t.Context(), service.LocalPrincipal(orgAdmin), spec("env_no_such")); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("proved caller sending an unknown environment id: %v, want invalid", err)
		}
		// A manager narrowed to dev answers an unknown id as it answers prod,
		// which it does not manage: neither says whether the id exists.
		dev := whereIn(domain.AxisOnly, map[domain.ProjectID][]domain.EnvID{prjA1: {envA1}}, domain.AxisAll)
		dev.EnvNames = []string{"dev"}
		for _, c := range []domain.Capability{domain.CapManageMembers, domain.CapRead} {
			f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: c, Org: orgA, Where: dev})
		}
		narrowed := func(envs ...domain.EnvID) error {
			s := spec(envs[0])
			s.Where.Envs[prjA1] = envs
			s.Target = gina
			_, err := f.rules.Create(t.Context(), service.LocalPrincipal(frank), s)
			return err
		}
		if err := narrowed(envA1); err != nil {
			t.Fatalf("narrowed manager delegating dev: %v", err)
		}
		// Alone or beside dev, which it does manage.
		for _, env := range []domain.EnvID{envProd, "env_no_such", envB1} {
			if err := narrowed(env); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("narrowed manager sending %s: %v, want the uniform not found", env, err)
			}
			if err := narrowed(envA1, env); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("narrowed manager sending dev and %s: %v, want the uniform not found", env, err)
			}
		}
	})
}
