package isolation

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestRuleDelegationMissingKeyCapturesDenial(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		managed := whereIn(domain.AxisAll, nil, domain.AxisOnly, folderItem("db"))
		for _, capability := range []domain.Capability{domain.CapManageMembers, domain.CapEdit} {
			f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: capability, Org: orgA, Where: managed})
		}
		spec := service.RuleSpec{Target: dave, Capability: domain.CapEdit, Org: orgA,
			Where: whereIn(domain.AxisAll, nil, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.dbKeyID})}
		f.create(t, frank, spec) // Prove this caller can delegate a reachable key.
		denials := func() int64 {
			return queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'grant.denied'") +
				queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'grant.denied'")
		}
		before := denials()
		spec.Where = whereIn(domain.AxisAll, nil, domain.AxisOnly, domain.RuleKeyItem{KeyID: "missing-key"})
		_, missing := f.rules.Create(t.Context(), service.LocalPrincipal(frank), spec)
		afterMissing := denials()
		spec.Where = whereIn(domain.AxisAll, nil, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.strKeyID})
		_, inaccessible := f.rules.Create(t.Context(), service.LocalPrincipal(frank), spec)
		assertUniformNotFound(t, missing, inaccessible)
		if afterMissing-before != 1 || denials()-afterMissing != 1 {
			t.Fatalf("denial counts: missing=%d inaccessible=%d; want one each", afterMissing-before, denials()-afterMissing)
		}
	})
}
