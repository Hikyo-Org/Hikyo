package isolation

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// A census rule is bound to its project's environments alone, so it must
// reach nothing in another project (#1447): bound against prj_a1, an `all
// except prod` rule knows no environment of prj_a2, and unconfined it would
// reach prj_a2's prod, which it excepts.
func TestRuleCensusIsConfinedToItsProject(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		const otherProd = domain.EnvID("env_a2prod")
		execRaw(t, db, "INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) VALUES ('"+string(otherProd)+"', 'org_a', 'prj_a2', 'prod', '', "+ts+", 1)")
		w := domain.Where{Projects: []domain.ProjectID{prjA1, prjA2}, EnvMode: domain.AxisAll, EnvNames: []string{"prod"}, KeyMode: domain.AxisAll}
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapRead, Org: orgA, Where: w})

		r := authn.NewSQLite(db.SQLiteRead())
		if db.Engine() == store.EnginePostgres {
			r = authn.NewPG(db.PG())
		}
		census, err := r.RulesForProject(t.Context(), string(orgA), string(prjA1))
		if err != nil {
			t.Fatal(err)
		}
		var rule *domain.Rule
		for i := range census {
			if census[i].Rule.Principal == frank {
				rule = &census[i].Rule
			}
		}
		if rule == nil {
			t.Fatalf("frank's rule is missing from the census %+v", census)
		}
		reaches := func(project domain.ProjectID, env domain.EnvID) bool {
			return rule.Reaches(domain.CapRead, domain.LevelEnv, domain.Scope{Org: orgA, Project: project, Env: env}, nil)
		}
		// Control: within its project the rule reaches dev and excepts prod.
		if !reaches(prjA1, envA1) || reaches(prjA1, envProd) {
			t.Fatalf("in prj_a1: dev %v, prod %v; want true, false", reaches(prjA1, envA1), reaches(prjA1, envProd))
		}
		for _, env := range []domain.EnvID{envA2, otherProd} {
			if reaches(prjA2, env) {
				t.Fatalf("a census rule bound in prj_a1 reaches %s in prj_a2", env)
			}
		}
		if rule.Reaches(domain.CapRead, domain.LevelProject, domain.Scope{Org: orgA, Project: prjA2}, nil) {
			t.Fatal("a census rule bound in prj_a1 reaches prj_a2 itself")
		}
	})
}
