package isolation

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestPerKeyDefinitionAffordances(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		mode, envs := onlyEnvs(envA1)
		one := whereIn(mode, envs, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.dbKeyID})
		for _, capability := range []domain.Capability{domain.CapRead, domain.CapDefinitionsEdit} {
			f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: capability, Org: orgA, Where: one})
		}
		actor := service.LocalPrincipal(frank)
		project := scopeProject(orgA, prjA1)
		definitions := &service.Definitions{DB: db}
		settings, err := definitions.GetSettings(t.Context(), actor, project)
		if err != nil || settings.CanDeclareKeys == nil || *settings.CanDeclareKeys || settings.CanEditDefinitions == nil || !*settings.CanEditDefinitions {
			t.Fatalf("single-key editing must not offer future-key creation: %+v, %v", settings, err)
		}
		if _, err := definitions.Export(t.Context(), actor, project, false); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("metadata navigation must not allow a whole-project bundle: %v", err)
		}
		spec := service.KeySpec{Name: "AFFORDANCE_KEY", FolderPath: "db", Classification: string(schema.Config),
			Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules()}
		if _, err := f.keys.Create(t.Context(), actor, project, spec, nil); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("single-key selector must not create another key: %v", err)
		}
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapDefinitionsEdit, Org: orgA,
			Where: whereIn(mode, envs, domain.AxisOnly, folderItem("db"))})
		settings, err = definitions.GetSettings(t.Context(), actor, project)
		if err != nil || settings.CanDeclareKeys == nil || !*settings.CanDeclareKeys || settings.CanEditDefinitions == nil || !*settings.CanEditDefinitions {
			t.Fatalf("selected-environment folder rule should offer empty-key creation: %+v, %v", settings, err)
		}
		if _, err := f.keys.Create(t.Context(), actor, project, spec, nil); err != nil {
			t.Fatalf("offered empty-key creation should work without Publish: %v", err)
		}
	})
}
