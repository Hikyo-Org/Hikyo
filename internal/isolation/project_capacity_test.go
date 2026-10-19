package isolation

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func grantProjectCapacitySettings(t *testing.T, db *store.DB, f definitionsFixture) {
	t.Helper()
	execRaw(t, db, fmt.Sprintf("INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_capacity_%s', 'usr_alice', 'project-settings', 'org_a', '%s', NULL, %s)", f.project, f.project, ts))
}

func updateProjectCapacity(t *testing.T, s *service.ProjectSettings, f definitionsFixture, before service.ProjectCapacity, override *int) service.ProjectCapacity {
	t.Helper()
	out, err := s.SetCapacity(t.Context(), service.LocalPrincipal(alice), f.scope(), service.CapacityUpdate{
		KeyLimitOverride: override, ExpectedRevision: before.Revision,
		ExpectedInstanceDefault: before.InstanceDefault, ExpectedInstanceMaximum: before.InstanceMaximum,
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestProjectCapacityPolicy(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedDefinitionsProject(t, db, "capacity_policy", false)
		s := &service.ProjectSettings{DB: db, DefaultKeysPerProject: 2, MaxKeysPerProject: 6}
		actor := service.LocalPrincipal(alice)
		if _, err := s.GetCapacity(t.Context(), actor, f.scope()); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("reader without project-settings read = %v", err)
		}
		if _, err := s.SetCapacity(t.Context(), actor, f.scope(), service.CapacityUpdate{}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("reader without project-settings write = %v", err)
		}
		grantProjectCapacitySettings(t, db, f)
		beforeAudit := auditCount(t, db, "project.capacity_changed")
		out, err := s.GetCapacity(t.Context(), actor, f.scope())
		if err != nil || !out.Inherited || out.Capped || out.KeyLimitOverride != nil || out.EffectiveLimit != 2 || out.InstanceDefault != 2 || out.InstanceMaximum != 6 || out.Revision != 0 || out.KeyCount != 0 || out.EnvironmentCount != 0 {
			t.Fatalf("inherited capacity = %+v, %v", out, err)
		}
		out = updateProjectCapacity(t, s, f, out, nil)
		if out.Revision != 0 || auditCount(t, db, "project.capacity_changed") != beforeAudit {
			t.Fatal("inherited no-op wrote policy or audit")
		}
		custom := 4
		out = updateProjectCapacity(t, s, f, out, &custom)
		if out.Inherited || out.Capped || out.KeyLimitOverride == nil || *out.KeyLimitOverride != 4 || out.EffectiveLimit != 4 || out.Revision != 1 {
			t.Fatalf("custom capacity = %+v", out)
		}
		out = updateProjectCapacity(t, s, f, out, &custom)
		if out.Revision != 1 || auditCount(t, db, "project.capacity_changed") != beforeAudit+1 {
			t.Fatal("custom no-op changed revision or audit")
		}
		for _, want := range []service.CapacityUpdate{
			{ExpectedRevision: 0, ExpectedInstanceDefault: 2, ExpectedInstanceMaximum: 6},
			{ExpectedRevision: 1, ExpectedInstanceDefault: 1, ExpectedInstanceMaximum: 6},
			{ExpectedRevision: 1, ExpectedInstanceDefault: 2, ExpectedInstanceMaximum: 5},
		} {
			if _, err := s.SetCapacity(t.Context(), actor, f.scope(), want); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("stale edit = %v, want conflict", err)
			}
		}
		for _, invalid := range []int{0, -1, 7} {
			if _, err := s.SetCapacity(t.Context(), actor, f.scope(), service.CapacityUpdate{KeyLimitOverride: &invalid, ExpectedRevision: 1, ExpectedInstanceDefault: 2, ExpectedInstanceMaximum: 6}); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("invalid custom limit %d = %v", invalid, err)
			}
		}
		if _, err := s.GetCapacity(t.Context(), service.LocalPrincipal(bob), f.scope()); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("cross-org read = %v", err)
		}
		if _, err := s.SetCapacity(t.Context(), service.LocalPrincipal(bob), f.scope(), service.CapacityUpdate{}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("cross-org write = %v", err)
		}
		// The stored override remains four after lowering the operator ceiling.
		s.MaxKeysPerProject = 3
		out, err = s.GetCapacity(t.Context(), actor, f.scope())
		if err != nil || !out.Capped || out.KeyLimitOverride == nil || *out.KeyLimitOverride != 4 || out.EffectiveLimit != 3 || out.Revision != 1 {
			t.Fatalf("lower ceiling = %+v, %v", out, err)
		}
		if _, err := s.SetCapacity(t.Context(), actor, f.scope(), service.CapacityUpdate{KeyLimitOverride: &custom, ExpectedRevision: 1, ExpectedInstanceDefault: 2, ExpectedInstanceMaximum: 3}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("PUT retained override above current maximum = %v", err)
		}
		invalid := 5
		if _, err := s.SetCapacity(t.Context(), actor, f.scope(), service.CapacityUpdate{KeyLimitOverride: &invalid, ExpectedRevision: 1, ExpectedInstanceDefault: 2, ExpectedInstanceMaximum: 3}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("new override above lowered maximum = %v", err)
		}
		out = updateProjectCapacity(t, s, f, out, nil)
		if !out.Inherited || out.Capped || out.KeyLimitOverride != nil || out.EffectiveLimit != 2 || out.Revision != 2 {
			t.Fatalf("reset capacity = %+v", out)
		}
		if got := auditCount(t, db, "project.capacity_changed"); got != beforeAudit+2 {
			t.Fatalf("capacity audit count = %d, want %d", got, beforeAudit+2)
		}
	})
}

func TestProjectCapacityAdmission(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedDefinitionsProject(t, db, "capacity_admission", false)
		grantProjectCapacitySettings(t, db, f)
		settings := &service.ProjectSettings{DB: db, DefaultKeysPerProject: 1, MaxKeysPerProject: 4}
		keys := keySvc(t, db)
		keys.DefaultKeysPerProject, keys.MaxKeysPerProject = 1, 4
		actor := service.LocalPrincipal(alice)
		out, err := settings.GetCapacity(t.Context(), actor, f.scope())
		if err != nil {
			t.Fatal(err)
		}
		custom := 3
		out = updateProjectCapacity(t, settings, f, out, &custom)
		var first string
		for _, name := range []string{"ONE", "TWO"} {
			created, err := keys.Create(t.Context(), actor, f.scope(), keySpec(name, "config"), nil)
			if err != nil {
				t.Fatalf("custom capacity refused %s: %v", name, err)
			}
			first = created.ID
		}
		keys.MaxKeysPerProject = 2
		if _, err := keys.Create(t.Context(), actor, f.scope(), keySpec("THREE", "config"), nil); !errors.Is(err, domain.ErrLimitExceeded) {
			t.Fatalf("lowered instance ceiling did not clamp stored override: %v", err)
		}
		keys.MaxKeysPerProject = 4
		if _, err := keys.Create(t.Context(), actor, f.scope(), keySpec("THREE", "config"), nil); err != nil {
			t.Fatalf("restored ceiling did not restore saved override: %v", err)
		}
		if _, err := keys.Create(t.Context(), actor, f.scope(), keySpec("FOUR", "config"), nil); !errors.Is(err, domain.ErrLimitExceeded) {
			t.Fatalf("custom capacity growth = %v", err)
		}
		custom = 1
		out = updateProjectCapacity(t, settings, f, out, &custom)
		if out.KeyCount != 3 {
			t.Fatalf("lowering policy changed key count: %+v", out)
		}
		if _, err := keys.Rename(t.Context(), actor, f.scope(), first, "RENAMED", nil); err != nil {
			t.Fatalf("lowered override blocked edit: %v", err)
		}
		if _, err := keys.Create(t.Context(), actor, f.scope(), keySpec("FOUR", "config"), nil); !errors.Is(err, domain.ErrLimitExceeded) {
			t.Fatalf("lowered override growth = %v", err)
		}
		// A sibling continues to inherit the owner default.
		sibling := seedDefinitionsProject(t, db, "capacity_sibling", false)
		if _, err := keys.Create(t.Context(), actor, sibling.scope(), keySpec("FIRST", "config"), nil); err != nil {
			t.Fatal(err)
		}
		if _, err := keys.Create(t.Context(), actor, sibling.scope(), keySpec("SECOND", "config"), nil); !errors.Is(err, domain.ErrLimitExceeded) {
			t.Fatalf("project override leaked to sibling: %v", err)
		}
	})
}

func TestProjectCapacityDefinitionsRecheck(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedDefinitionsProject(t, db, "capacity_definitions", false)
		grantProjectCapacitySettings(t, db, f)
		settings := &service.ProjectSettings{DB: db, DefaultKeysPerProject: 1, MaxKeysPerProject: 4}
		svc := definitionsService(t, db)
		svc.DefaultKeysPerProject, svc.MaxKeysPerProject = 1, 4
		actor := service.LocalPrincipal(alice)
		out, err := settings.GetCapacity(t.Context(), actor, f.scope())
		if err != nil {
			t.Fatal(err)
		}
		bundle := parseDefinitions(t, exportPortableDefinitions(t, svc, f))
		bundle.Keys = append(bundle.Keys, definitionKey("FIRST", "config"), definitionKey("SECOND", "config"))
		raw := encodeDefinitions(t, bundle)
		if _, err := svc.Plan(t.Context(), actor, f.scope(), raw, nil); !errors.Is(err, domain.ErrLimitExceeded) {
			t.Fatalf("plan ignored inherited capacity: %v", err)
		}
		custom := 2
		out = updateProjectCapacity(t, settings, f, out, &custom)
		plan := planDefinitions(t, svc, f, raw)
		custom = 1
		out = updateProjectCapacity(t, settings, f, out, &custom)
		if _, err := svc.Apply(t.Context(), actor, f.scope(), plan.ID, service.ApplyOptions{}); !errors.Is(err, domain.ErrLimitExceeded) {
			t.Fatalf("apply ignored current override: %v", err)
		}
		custom = 2
		out = updateProjectCapacity(t, settings, f, out, &custom)
		if _, err := svc.Apply(t.Context(), actor, f.scope(), plan.ID, service.ApplyOptions{}); err != nil {
			t.Fatalf("apply refused raised override: %v", err)
		}
		custom = 1
		updateProjectCapacity(t, settings, f, out, &custom)
		plan = planDefinitions(t, svc, f, exportDefinitions(t, svc, f))
		if _, err := svc.Apply(t.Context(), actor, f.scope(), plan.ID, service.ApplyOptions{}); err != nil {
			t.Fatalf("lowered override blocked unchanged definitions: %v", err)
		}
	})
}

func TestProjectCapacityConcurrentUpdates(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedDefinitionsProject(t, db, "capacity_concurrent", false)
		grantProjectCapacitySettings(t, db, f)
		settings := &service.ProjectSettings{DB: db, DefaultKeysPerProject: 1, MaxKeysPerProject: 4}
		actor := service.LocalPrincipal(alice)
		beforeAudit := auditCount(t, db, "project.capacity_changed")
		results := make(chan error, 2)
		start := make(chan struct{})
		for _, limit := range []int{2, 3} {
			go func(limit int) {
				<-start
				_, err := settings.SetCapacity(t.Context(), actor, f.scope(), service.CapacityUpdate{KeyLimitOverride: &limit, ExpectedRevision: 0, ExpectedInstanceDefault: 1, ExpectedInstanceMaximum: 4})
				results <- err
			}(limit)
		}
		close(start)
		successes, conflicts := 0, 0
		for range 2 {
			err := <-results
			switch {
			case err == nil:
				successes++
			case errors.Is(err, domain.ErrConflict):
				conflicts++
			default:
				t.Errorf("concurrent policy edit: %v", err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("concurrent edits: successes=%d, conflicts=%d", successes, conflicts)
		}
		out, err := settings.GetCapacity(t.Context(), actor, f.scope())
		if err != nil || out.Revision != 1 || out.KeyLimitOverride == nil || *out.KeyLimitOverride < 2 || *out.KeyLimitOverride > 3 {
			t.Fatalf("concurrent final policy = %+v, %v", out, err)
		}
		if got := auditCount(t, db, "project.capacity_changed"); got != beforeAudit+1 {
			t.Fatalf("concurrent edits emitted %d events, want %d", got, beforeAudit+1)
		}
	})
}
