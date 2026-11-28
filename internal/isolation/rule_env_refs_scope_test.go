package isolation

import (
	"errors"
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// A rule create binds its names before the caller is proved (#1447). A rule
// over one named project reads only that project's environments, the same
// statements whether the project exists or not, so a refused caller's cost
// does not grow with the organization; a rule over several projects reads
// the organization's.
func TestRuleCreateReadsOneProjectsEnvironmentsBeforeAuthorization(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		const caller, target = domain.PrincipalID("usr_refs_caller"), domain.PrincipalID("usr_refs_target")
		for _, p := range []domain.PrincipalID{caller, target} {
			execRaw(t, db, "INSERT INTO principals (id, kind, created_at) VALUES ('"+string(p)+"', 'human', "+ts+")")
		}
		rules := &service.Rules{DB: db}
		create := func(t *testing.T, projects ...domain.ProjectID) ([]string, error) {
			t.Helper()
			var seen []string
			// The refusal's audit row lands in the tenant or the instance log by
			// whether the project resolves; the caller sees not found either way.
			restore := authn.SetQueryObserver(func(sql string) {
				if q := queryIdentity(sql); q != "InsertTenantAuditEvent" && q != "InsertInstanceAuditEvent" {
					seen = append(seen, q)
				}
			})
			defer restore()
			_, err := rules.Create(t.Context(), service.LocalPrincipal(caller), service.RuleSpec{
				Target: target, Capability: domain.CapRead, Org: orgA,
				Where: domain.Where{Projects: projects, EnvMode: domain.AxisOnly, EnvNames: []string{"dev"}, KeyMode: domain.AxisAll},
			})
			return seen, err
		}
		one, err := create(t, prjA1)
		if !errors.Is(err, domain.ErrNotFound) || slices.Contains(one, "ListRuleEnvRefsInOrg") || !slices.Contains(one, "ListRuleEnvRefsInProject") {
			t.Fatalf("refused single-project create: statements %v, err %v; want one project's environments, not found", one, err)
		}
		if missing, err := create(t, "prj_refs_missing"); !errors.Is(err, domain.ErrNotFound) || !slices.Equal(missing, one) {
			t.Fatalf("refused create over a missing project: statements %v, err %v; want %v, not found", missing, err, one)
		}
		// Control: the observer sees the organization-wide read where it is kept.
		if both, err := create(t, prjA1, prjA2); !errors.Is(err, domain.ErrNotFound) || !slices.Contains(both, "ListRuleEnvRefsInOrg") {
			t.Fatalf("refused two-project create: statements %v, err %v; want the organization's environments, not found", both, err)
		}
	})
}
