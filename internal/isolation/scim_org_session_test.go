package isolation

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The operator-approved 2026-10-01 amendment binds SCIM to its own org's
// authority. Exercise real authorization with one live bearer throughout,
// not just a session-row count or a fabricated credential.
func TestSCIMOrgChangesPreserveUnrelatedOrgSession(t *testing.T) {
	forEngines(t, runSCIMOrgChangesPreserveUnrelatedOrgSession)
}

func runSCIMOrgChangesPreserveUnrelatedOrgSession(t *testing.T, db *store.DB) {
	ctx := t.Context()
	s := scimSvc(db)
	binding, credential := newSCIMBinding(t, db, "org-session")
	wire := service.SCIMCredentialActor(credential, binding)
	admin := service.LocalPrincipal(orgAdmin)
	desired := service.DesiredUser{
		Active: true, UserName: "shared@example.test",
		ExternalID: "shared-subject", SubjectRaw: "shared-subject",
	}
	user, err := s.CreateUser(ctx, wire, orgA, binding, desired)
	if err != nil {
		t.Fatal(err)
	}
	principal := principalOf(t, db, accountOf(t, db, user.ID))
	group, err := s.CreateGroup(ctx, wire, orgA, binding, service.DesiredGroup{
		DisplayName: "Readers", Members: []string{user.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	mapping := service.SCIMMappingSpec{
		GroupID: group.ID, Template: domain.TemplateEditor, ProjectID: string(prjA1),
	}
	if _, err := s.CreateMapping(ctx, admin, orgA, binding, mapping); err != nil {
		t.Fatal(err)
	}
	// Prior membership in an unrelated org is setup, not an org-A write.
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES `+
		`('g_shared_b', '`+string(principal)+`', 'read', 'org_b', 'prj_b1', NULL, `+ts+`)`)
	seedOrigins(t, db)
	token := seedSessionFactors(t, db, principal, `[]`)
	_, projects, _ := services(t, db)
	generation := queryInt(t, db, `SELECT session_generation FROM principals WHERE id = '`+string(principal)+`'`)
	assertAccess := func(phase string, orgAReadable bool) {
		t.Helper()
		_, err := projects.Get(ctx, service.Bearer(token), scopeProject(orgA, prjA1))
		if orgAReadable && err != nil {
			t.Fatalf("%s: org A must remain readable: %v", phase, err)
		}
		if !orgAReadable && !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("%s: org A must immediately refuse removed authority, got %v", phase, err)
		}
		if _, err := projects.Get(ctx, service.Bearer(token), scopeProject(orgB, prjB1)); err != nil {
			t.Fatalf("%s: same bearer must still reach unrelated org B: %v", phase, err)
		}
		if got := queryInt(t, db, `SELECT session_generation FROM principals WHERE id = '`+string(principal)+`'`); got != generation {
			t.Fatalf("%s: org-scoped SCIM changed global generation: %d -> %d", phase, generation, got)
		}
	}
	assertAccess("setup", true)
	desired.Active = false
	if _, err := s.ReplaceUser(ctx, wire, orgA, binding, user.ID, desired); err != nil {
		t.Fatal(err)
	}
	assertAccess("deactivate", false)
	desired.Active = true
	if _, err := s.ReplaceUser(ctx, wire, orgA, binding, user.ID, desired); err != nil {
		t.Fatal(err)
	}
	assertAccess("reactivate", true)
	mapping.Template = domain.TemplateViewer
	if _, err := s.UpdateMapping(ctx, admin, orgA, binding, mapping); err != nil {
		t.Fatal(err)
	}
	if held(t, db, principal, domain.CapEdit, scopeProject(orgA, prjA1)) {
		t.Fatal("mapping narrowing kept removed edit authority")
	}
	assertAccess("mapping narrowing", true)
	if _, err := s.DeleteMapping(ctx, admin, orgA, binding, mapping); err != nil {
		t.Fatal(err)
	}
	assertAccess("mapping deletion", false)
	if _, err := s.CreateMapping(ctx, admin, orgA, binding, mapping); err != nil {
		t.Fatal(err)
	}
	assertAccess("mapping creation", true)
	if err := s.DeleteUser(ctx, wire, orgA, binding, user.ID); err != nil {
		t.Fatal(err)
	}
	assertAccess("user deletion", false)
	user, err = s.CreateUser(ctx, wire, orgA, binding, desired)
	if err != nil {
		t.Fatal(err)
	}
	if got := principalOf(t, db, accountOf(t, db, user.ID)); got != principal {
		t.Fatalf("recreated SCIM resource must attach the existing account: %s != %s", got, principal)
	}
	if _, err := s.ReplaceGroup(ctx, wire, orgA, binding, group.ID, service.DesiredGroup{
		DisplayName: "Readers", Members: []string{user.ID},
	}); err != nil {
		t.Fatal(err)
	}
	assertAccess("user reattachment and group re-add", true)
	if err := s.DeleteBinding(ctx, admin, orgA, binding); err != nil {
		t.Fatal(err)
	}
	assertAccess("binding deletion", false)
}
