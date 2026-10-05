package isolation

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestPerKeyDefinitionMutations(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		project := scopeProject(orgA, prjA1)
		where := whereIn(domain.AxisAll, nil, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.dbKeyID})
		for _, capability := range []domain.Capability{domain.CapDefinitionsEdit, domain.CapPublish, domain.CapReveal, domain.CapRead} {
			f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: capability, Org: orgA, Where: where})
		}
		actor := service.LocalPrincipal(frank)
		minLength := 1
		update := service.KeyDeclarationUpdate{Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString, MinLength: &minLength}}, Presence: schema.DefaultPresenceRules()}
		if _, err := f.keys.UpdateDeclaration(t.Context(), actor, project, f.dbKeyID, update, nil); err != nil {
			t.Fatalf("change held secret key rule: %v", err)
		}
		if _, err := f.keys.UpdateDeclaration(t.Context(), actor, project, f.strKeyID, update, nil); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("change sibling secret key rule: %v", err)
		}
		if _, _, err := f.keys.Reclassify(t.Context(), actor, project, f.dbKeyID, string(schema.Config)); err != nil {
			t.Fatalf("declassify held key: %v", err)
		}
		if _, _, err := f.keys.Reclassify(t.Context(), actor, project, f.strKeyID, string(schema.Config)); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("declassify sibling key: %v", err)
		}
		if _, err := f.keys.Rename(t.Context(), actor, project, f.dbKeyID, "RENAMED_DB_PASSWORD", nil); err != nil {
			t.Fatalf("rename held key with narrowed publish: %v", err)
		}
		groups := &service.KeyGroups{DB: db}
		group, err := groups.Create(t.Context(), service.LocalPrincipal(custodian), project, "delegated-group", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.keys.SetGroup(t.Context(), actor, project, f.dbKeyID, group.ID); err != nil {
			t.Fatalf("group held key: %v", err)
		}
		if _, err := f.keys.SetGroup(t.Context(), actor, project, f.strKeyID, group.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("group sibling key: %v", err)
		}

		// Delete prunes the caller's own single-key rules. Publishing must retain
		// proofs obtained before that cascade, without resolving the deleted key.
		empty, err := f.keys.Create(t.Context(), service.LocalPrincipal(custodian), project, service.KeySpec{Name: "EMPTY_DELEGATED_KEY", Classification: string(schema.Config), Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules()}, nil)
		if err != nil {
			t.Fatal(err)
		}
		emptyWhere := whereIn(domain.AxisAll, nil, domain.AxisOnly, domain.RuleKeyItem{KeyID: empty.ID})
		for _, capability := range []domain.Capability{domain.CapDefinitionsEdit, domain.CapPublish} {
			f.create(t, orgAdmin, service.RuleSpec{Target: hank, Capability: capability, Org: orgA, Where: emptyWhere})
		}
		if err := f.keys.Delete(t.Context(), service.LocalPrincipal(hank), project, empty.ID); err != nil {
			t.Fatalf("delete held key and publish after rule pruning: %v", err)
		}
	})
}
