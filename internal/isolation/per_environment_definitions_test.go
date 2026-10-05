package isolation

import (
	"errors"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"testing"
)

func TestSelectedEnvironmentDefinitionAndInitialDraft(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		project := scopeProject(orgA, prjA1)
		dev := scopeEnv(orgA, prjA1, envA1)
		prod := scopeEnv(orgA, prjA1, envProd)
		mode, envs := onlyEnvs(envA1)
		where := whereIn(mode, envs, domain.AxisOnly, domain.RuleKeyItem{Folder: "db", IsFolder: true})
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapDefinitionsEdit, Org: orgA, Where: where})
		actor := service.LocalPrincipal(frank)
		key, err := f.keys.Create(t.Context(), actor, project, service.KeySpec{Name: "INITIAL_DB", FolderPath: "db", Classification: string(schema.Config), Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules()}, nil)
		if err != nil {
			t.Fatalf("selected Define creates empty shared key without Publish: %v", err)
		}
		if _, err := f.values.Initialize(t.Context(), actor, dev, key.Name, "initial", nil); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("initial value needs Publish: %v", err)
		}
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapPublish, Org: orgA, Where: where})
		draft, err := f.values.Initialize(t.Context(), actor, dev, key.Name, "initial", nil)
		if err != nil {
			t.Fatalf("initial draft without Edit: %v", err)
		}
		if _, err := f.values.Initialize(t.Context(), actor, dev, key.Name, "ack replay", nil); err != nil {
			t.Fatalf("owner can revise initial draft: %v", err)
		}
		if _, err := f.values.Initialize(t.Context(), service.LocalPrincipal(custodian), dev, key.Name, "other owner", nil); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("cannot replace another owner's draft: %v", err)
		}
		if _, err := f.values.Set(t.Context(), actor, dev, key.Name, "edit", nil); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("Define is not ordinary Edit: %v", err)
		}
		if _, err := f.values.Initialize(t.Context(), actor, prod, key.Name, "excluded", nil); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("excluded destination: %v", err)
		}
		// Consume the revised version; initialization itself left delivery absent.
		draft, err = f.values.Initialize(t.Context(), actor, dev, key.Name, "published", nil)
		if err != nil {
			t.Fatal(err)
		}
		revisions := &service.Revisions{DB: db, Keyring: f.values.Keyring}
		if _, err := revisions.PublishPlanned(t.Context(), actor, dev, service.PublishRequest{VersionIDs: []string{draft.VersionID}}); err != nil {
			t.Fatalf("publish initial draft: %v", err)
		}
		if _, err := f.values.Initialize(t.Context(), actor, dev, key.Name, "overwrite", nil); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("cannot initialize existing value: %v", err)
		}

		if _, err := f.keys.Rename(t.Context(), actor, project, key.ID, "DENIED_ABSENT", nil); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("global semantic edit requires excluded env authority even when absent: %v", err)
		}
		if _, _, err := f.values.Declare(t.Context(), service.LocalPrincipal(custodian), project, []string{string(envProd)}, key.Name, "outside"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.keys.Rename(t.Context(), actor, project, key.ID, "DENIED_PRESENT", nil); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("global semantic refusal independent of excluded presence: %v", err)
		}
		prodMode, prodEnvs := onlyEnvs(envProd)
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapPublish, Org: orgA, Where: whereIn(prodMode, prodEnvs, domain.AxisOnly, domain.RuleKeyItem{Folder: "db", IsFolder: true})})
		mixedKey, err := f.keys.Create(t.Context(), actor, project, service.KeySpec{Name: "MIXED_INITIAL_DB", FolderPath: "db", Classification: string(schema.Config), Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules()}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.values.Initialize(t.Context(), actor, prod, mixedKey.Name, "mixed", nil); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("Publish cannot supply missing Define in another env: %v", err)
		}
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapDefinitionsEdit, Org: orgA, Where: whereIn(prodMode, prodEnvs, domain.AxisOnly, domain.RuleKeyItem{Folder: "db", IsFolder: true})})
		if _, err := f.keys.Rename(t.Context(), actor, project, key.ID, "INITIAL_RENAMED", nil); err != nil {
			t.Fatalf("all affected envs held: %v", err)
		}

	})
}

func TestSelectedDefinitionGroupCreationDoesNotRevealExcludedPresence(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		project := scopeProject(orgA, prjA1)
		groups := &service.KeyGroups{DB: db}
		group, err := groups.Create(t.Context(), service.LocalPrincipal(custodian), project, "oracle-group", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.keys.SetGroup(t.Context(), service.LocalPrincipal(custodian), project, f.dbKeyID, group.ID); err != nil {
			t.Fatal(err)
		}
		mode, envs := onlyEnvs(envA1)
		where := whereIn(mode, envs, domain.AxisOnly, domain.RuleKeyItem{Folder: "db", IsFolder: true})
		for _, cap := range []domain.Capability{domain.CapDefinitionsEdit, domain.CapPublish} {
			f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: cap, Org: orgA, Where: where})
		}
		spec := service.KeySpec{Name: "GROUP_INITIAL", FolderPath: "db", GroupID: group.ID, Classification: string(schema.Config), Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules()}
		actor := service.LocalPrincipal(frank)
		if _, err = f.keys.Create(t.Context(), actor, project, spec, nil); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("group creation with excluded sibling present: %v", err)
		}
		prod := scopeEnv(orgA, prjA1, envProd)
		draft, err := f.values.Unset(t.Context(), service.LocalPrincipal(custodian), prod, "DB_PASSWORD")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = revisionSvc(t, db).PublishPlanned(t.Context(), service.LocalPrincipal(custodian), prod, service.PublishRequest{VersionIDs: []string{draft.VersionID}}); err != nil {
			t.Fatal(err)
		}
		if _, err = f.keys.Create(t.Context(), actor, project, spec, nil); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("same refusal with excluded sibling absent: %v", err)
		}
		spec.GroupID = ""
		if _, err = f.keys.Create(t.Context(), actor, project, spec, nil); err != nil {
			t.Fatalf("ungrouped new key remains legal: %v", err)
		}

		// The same group is valid once its remaining delivered cell is absent;
		// a manager with whole-environment authority can then add an empty key.
		dev := scopeEnv(orgA, prjA1, envA1)
		draft, err = f.values.Unset(t.Context(), service.LocalPrincipal(custodian), dev, "DB_PASSWORD")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = revisionSvc(t, db).PublishPlanned(t.Context(), service.LocalPrincipal(custodian), dev, service.PublishRequest{VersionIDs: []string{draft.VersionID}}); err != nil {
			t.Fatal(err)
		}
		spec.Name = "FULL_GROUP_INITIAL"
		spec.GroupID = group.ID
		if _, err = f.keys.Create(t.Context(), service.LocalPrincipal(custodian), project, spec, nil); err != nil {
			t.Fatalf("whole-environment group creation control: %v", err)
		}
	})
}

func TestSingleKeyGroupAssignmentDoesNotRevealOtherMembersPresence(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		project := scopeProject(orgA, prjA1)
		groups := &service.KeyGroups{DB: db}
		group, err := groups.Create(t.Context(), service.LocalPrincipal(custodian), project, "assign-oracle", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.keys.SetGroup(t.Context(), service.LocalPrincipal(custodian), project, f.strKeyID, group.ID); err != nil {
			t.Fatal(err)
		}
		where := whereIn(domain.AxisAll, nil, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.dbKeyID})
		for _, cap := range []domain.Capability{domain.CapDefinitionsEdit, domain.CapPublish} {
			f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: cap, Org: orgA, Where: where})
		}
		actor := service.LocalPrincipal(frank)
		if _, err = f.keys.SetGroup(t.Context(), actor, project, f.dbKeyID, group.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("group sibling present: %v", err)
		}
		// Target remains present, while the unknown sibling becomes absent: the
		// refusal must remain the same even though all-or-none validation would fail.
		for _, env := range []domain.EnvID{envA1, envProd} {
			scope := scopeEnv(orgA, prjA1, env)
			draft, err := f.values.Unset(t.Context(), service.LocalPrincipal(custodian), scope, "STRIPE_KEY")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = revisionSvc(t, db).PublishPlanned(t.Context(), service.LocalPrincipal(custodian), scope, service.PublishRequest{VersionIDs: []string{draft.VersionID}}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = f.keys.SetGroup(t.Context(), actor, project, f.dbKeyID, group.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("group sibling absent: %v", err)
		}
		// A broad manager can inspect the group and receives its honest validation
		// failure, proving the hidden state would otherwise change the outcome.
		if _, err = f.keys.SetGroup(t.Context(), service.LocalPrincipal(custodian), project, f.dbKeyID, group.ID); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("whole-environment validation control: %v", err)
		}
	})
}

func TestOnlyEnvironmentSeeDoesNotNavigateUnselectedProject(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		where := domain.Where{Projects: []domain.ProjectID{prjA1, prjA2}, EnvMode: domain.AxisOnly, Envs: map[domain.ProjectID][]domain.EnvID{prjA1: {envA1}}, KeyMode: domain.AxisAll}
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapRead, Org: orgA, Where: where})
		project := scopeProject(orgA, prjA1)
		groups := &service.KeyGroups{DB: db}
		group, err := groups.Create(t.Context(), service.LocalPrincipal(custodian), project, "nav-catalogue", nil)
		if err != nil {
			t.Fatal(err)
		}
		folders := &service.Folders{DB: db}
		folder, err := folders.Create(t.Context(), service.LocalPrincipal(custodian), project, "navigation-folder", nil)
		if err != nil {
			t.Fatal(err)
		}
		actor := service.LocalPrincipal(frank)
		projects := &service.Projects{DB: db}
		listed, err := projects.List(t.Context(), actor, orgA)
		if err != nil || len(listed) != 1 || listed[0].ID != string(prjA1) {
			t.Fatalf("navigation project projection: %+v,%v", listed, err)
		}
		settings := &service.Definitions{DB: db}
		if _, err = projects.Get(t.Context(), actor, scopeProject(orgA, prjA2)); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("project with no selected environment: %v", err)
		}
		if _, err = settings.GetSettings(t.Context(), actor, scopeProject(orgA, prjA2)); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("unselected project provenance: %v", err)
		}
		if _, err = settings.GetSettings(t.Context(), actor, scopeProject(orgA, prjA1)); err != nil {
			t.Fatalf("selected project settings: %v", err)
		}
		if _, err = f.keys.Get(t.Context(), actor, project, f.strKeyID); err != nil {
			t.Fatalf("selected See key detail metadata: %v", err)
		}
		if _, err = folders.Get(t.Context(), actor, project, folder.ID); err != nil {
			t.Fatalf("selected See folder detail metadata: %v", err)
		}
		if _, err = groups.Get(t.Context(), actor, project, group.ID); err != nil {
			t.Fatalf("selected See group detail metadata: %v", err)
		}
		if listed, err := groups.List(t.Context(), actor, project); err != nil || len(listed) != 1 {
			t.Fatalf("Matrix group catalogue: %+v,%v", listed, err)
		}
		unselected := scopeProject(orgA, prjA2)
		if _, err = groups.List(t.Context(), actor, unselected); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("unselected group catalogue: %v", err)
		}
		if _, err = groups.Get(t.Context(), actor, unselected, group.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("unselected group metadata: %v", err)
		}
		if _, err = f.keys.Get(t.Context(), actor, unselected, f.strKeyID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("unselected key metadata: %v", err)
		}
		if _, err = folders.Get(t.Context(), actor, unselected, folder.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("unselected folder metadata: %v", err)
		}

	})
}
