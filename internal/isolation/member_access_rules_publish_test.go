package isolation

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestMemberAccessRulesPerKeyPublishAndHistory(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		dev := scopeEnv(orgA, prjA1, envA1)
		mode, envs := onlyEnvs(envA1)
		one := whereIn(mode, envs, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.dbKeyID})
		for _, cap := range []domain.Capability{domain.CapRead, domain.CapPublish, domain.CapReveal, domain.CapRevealHistory} {
			f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: cap, Org: orgA, Where: one})
		}
		orgs, err := (&service.Orgs{DB: db}).ListMine(t.Context(), service.LocalPrincipal(carol))
		if err != nil || len(orgs) != 1 || orgs[0].ID != string(orgA) {
			t.Fatalf("rule-only org navigation: %+v, %v", orgs, err)
		}
		projects, err := (&service.Projects{DB: db}).List(t.Context(), service.LocalPrincipal(carol), orgA)
		if err != nil || len(projects) != 1 || projects[0].ID != string(prjA1) {
			t.Fatalf("rule-only project navigation exposed sibling: %+v, %v", projects, err)
		}
		environments, err := (&service.Environments{DB: db}).List(t.Context(), service.LocalPrincipal(carol), scopeProject(orgA, prjA1))
		if err != nil || len(environments) != 1 || environments[0].ID != string(envA1) {
			t.Fatalf("rule-only environment navigation: %+v, %v", environments, err)
		}
		// Edit is independent from Publish. Stage both keys, then prove that
		// Publish's narrower selector controls what actually becomes live.
		f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: domain.CapEdit, Org: orgA, Where: whereIn(mode, envs, domain.AxisAll)})
		actor := service.LocalPrincipal(carol)
		stage := func(name, value string) service.StagedChange {
			t.Helper()
			change, err := f.values.Set(t.Context(), actor, dev, name, value, nil)
			if err != nil {
				t.Fatal(err)
			}
			return change
		}
		draft := stage("DB_PASSWORD", "db-new")
		stripe := stage("STRIPE_KEY", "stripe-new")
		revisions := revisionSvc(t, db)
		published, err := revisions.PublishPlanned(t.Context(), actor, dev, service.PublishRequest{VersionIDs: []string{draft.VersionID}})
		if err != nil {
			t.Fatalf("selected-key publish: %v", err)
		}
		current := published.Environments[0].Revision
		cells, err := f.values.List(t.Context(), actor, dev, false)
		if err != nil || len(cells) < 2 {
			t.Fatalf("See must retain environment catalogue: %v, %+v", err, cells)
		}
		if _, err := revisions.Diff(t.Context(), actor, dev, current-1, current, f.dbKeyID); err != nil {
			t.Fatalf("selected-key history: %v", err)
		}
		if _, err := revisions.Diff(t.Context(), actor, dev, current-1, current, f.strKeyID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("sibling history = %v", err)
		}
		second := stage("DB_PASSWORD", "db-must-not-publish")
		mixed := service.PublishRequest{VersionIDs: []string{second.VersionID, stripe.VersionID}}
		if _, err := revisions.PublishPlanned(t.Context(), actor, dev, mixed); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("mixed allowed/denied publish = %v", err)
		}
		cell, err := f.values.Get(t.Context(), actor, dev, "DB_PASSWORD", true)
		if err != nil || cell.Value != "db-new" {
			t.Fatalf("refused selection changed published value: %+v, %v", cell, err)
		}
		// Same unchanged request succeeds after adding only the missing key's
		// authority. This also proves refusal retained both drafts atomically.
		f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: domain.CapPublish, Org: orgA, Where: whereIn(mode, envs, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.strKeyID})})
		if _, err := revisions.PublishPlanned(t.Context(), actor, dev, mixed); err != nil {
			t.Fatalf("union of single-key Publish rules: %v", err)
		}
		restored, err := revisions.Restore(t.Context(), actor, dev, current-1, "DB_PASSWORD")
		if err != nil {
			t.Fatalf("single-key rollback staging: %v", err)
		}
		ids := make([]string, 0, len(restored.Changes))
		for _, change := range restored.Changes {
			ids = append(ids, change.VersionID)
		}
		if _, err := revisions.PublishPlanned(t.Context(), actor, dev, service.PublishRequest{VersionIDs: ids, PreviewToken: restored.Preview.Token}); err != nil {
			t.Fatalf("single-key rollback publish: %v", err)
		}
	})
}

func TestMemberAccessRulesNavigationPreservesLegacyReadUnion(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		mode, envs := onlyEnvs(envA1)
		f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: domain.CapRead, Org: orgA,
			Where: whereIn(mode, envs, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.dbKeyID})})
		if _, err := (&service.Grants{DB: db}).Create(t.Context(), service.LocalPrincipal(orgAdmin), service.GrantSpec{
			Target: carol, Capability: domain.CapRead, Scope: scopeProject(orgA, prjA2),
		}); err != nil {
			t.Fatal(err)
		}
		projects, err := (&service.Projects{DB: db}).List(t.Context(), service.LocalPrincipal(carol), orgA)
		if err != nil || len(projects) != 2 {
			t.Fatalf("navigation must union grants and rules: %+v, %v", projects, err)
		}
		seen := map[string]bool{}
		for _, project := range projects {
			seen[project.ID] = true
		}
		if !seen[string(prjA1)] || !seen[string(prjA2)] {
			t.Fatalf("navigation lost a reachable project: %+v", projects)
		}
	})
}

func TestMemberAccessRulesPublishChecksGroupClosure(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		project := scopeProject(orgA, prjA1)
		groups := &service.KeyGroups{DB: db}
		group, err := groups.Create(t.Context(), service.LocalPrincipal(custodian), project, "linked", nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{f.dbKeyID, f.strKeyID} {
			if _, err := f.keys.SetGroup(t.Context(), service.LocalPrincipal(custodian), project, id, group.ID); err != nil {
				t.Fatal(err)
			}
		}
		mode, envs := onlyEnvs(envA1)
		for _, cap := range []domain.Capability{domain.CapRead, domain.CapEdit} {
			f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: cap, Org: orgA, Where: whereIn(mode, envs, domain.AxisAll)})
		}
		f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: domain.CapPublish, Org: orgA, Where: whereIn(mode, envs, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.dbKeyID})})
		actor := service.LocalPrincipal(carol)
		dev := scopeEnv(orgA, prjA1, envA1)
		first, err := f.values.Set(t.Context(), actor, dev, "DB_PASSWORD", "group-db", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.values.Set(t.Context(), actor, dev, "STRIPE_KEY", "group-stripe", nil); err != nil {
			t.Fatal(err)
		}
		revisions := revisionSvc(t, db)
		req := service.PublishRequest{VersionIDs: []string{first.VersionID}}
		if _, err := revisions.PublishPlanned(t.Context(), actor, dev, req); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("closure added unauthorized key: %v", err)
		}
		f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: domain.CapPublish, Org: orgA, Where: whereIn(mode, envs, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.strKeyID})})
		result, err := revisions.PublishPlanned(t.Context(), actor, dev, req)
		if err != nil || len(result.Published) != 2 {
			t.Fatalf("authorized closure control: %+v, %v", result, err)
		}
	})
}

func TestMemberAccessRulesPublishHidesUnmanagedGroupDrafts(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		project := scopeProject(orgA, prjA1)
		group, err := (&service.KeyGroups{DB: db}).Create(t.Context(), service.LocalPrincipal(custodian), project, "coupled", nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{f.dbKeyID, f.strKeyID} {
			if _, err := f.keys.SetGroup(t.Context(), service.LocalPrincipal(custodian), project, id, group.ID); err != nil {
				t.Fatal(err)
			}
		}
		mode, envs := onlyEnvs(envA1)
		for _, person := range []domain.PrincipalID{carol, dave} {
			f.create(t, orgAdmin, service.RuleSpec{Target: person, Capability: domain.CapEdit, Org: orgA, Where: whereIn(mode, envs, domain.AxisAll)})
		}
		f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: domain.CapPublish, Org: orgA, Where: whereIn(mode, envs, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.dbKeyID})})
		dev := scopeEnv(orgA, prjA1, envA1)
		first, err := f.values.Set(t.Context(), service.LocalPrincipal(carol), dev, "DB_PASSWORD", "mine", nil)
		if err != nil {
			t.Fatal(err)
		}
		revisions := revisionSvc(t, db)
		req := service.PublishRequest{VersionIDs: []string{first.VersionID}}
		_, absent := revisions.PublishPlanned(t.Context(), service.LocalPrincipal(carol), dev, req)
		if _, err := f.values.Set(t.Context(), service.LocalPrincipal(dave), dev, "STRIPE_KEY", "someone-else", nil); err != nil {
			t.Fatal(err)
		}
		_, present := revisions.PublishPlanned(t.Context(), service.LocalPrincipal(carol), dev, req)
		assertUniformNotFound(t, absent, present)
		// Only a publisher of the complete linked group may learn the named
		// cross-owner conflict. Granting B leaves the request itself unchanged.
		f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: domain.CapPublish, Org: orgA, Where: whereIn(mode, envs, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.strKeyID})})
		if _, err := revisions.PublishPlanned(t.Context(), service.LocalPrincipal(carol), dev, req); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("authorized cross-owner control: %v", err)
		}
	})
}
