package conformance

import (
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"testing"
)

func init() {
	corpus = append(corpus, scenario{"ui_affordances_are_caller_scoped", scenarioUIAffordances})
}

func scenarioUIAffordances(t *testing.T, db *store.DB) {
	who, scope, values, envs, keys := valueFixture(t, db, "uiaffordances")
	actor := service.LocalPrincipal(who)
	dev := mustEnv(t, envs, actor, scope, "dev")
	key := mustKey(t, keys, actor, scope, "SECRET_PORT", string(schema.Secret), schema.DefaultPresenceRules())
	if _, err := keys.UpdateDeclaration(t.Context(), actor, scope, key.ID, service.KeyDeclarationUpdate{
		Declaration: decl(schema.Rule{Type: schema.TypeInteger}), Presence: schema.DefaultPresenceRules(),
	}, nil); err != nil {
		t.Fatal(err)
	}
	reader := newPrincipal(t, db, "usr_ui_reader_"+string(scope.Project), []grantSpec{{capability: "read", scope: scope}})
	editor := newPrincipal(t, db, "usr_ui_editor_"+string(scope.Project), []grantSpec{{capability: "read", scope: scope}, {capability: "edit", scope: scope}})
	if _, err := values.Set(t.Context(), service.LocalPrincipal(editor), dev, "SECRET_PORT", "banana", nil); err != nil {
		t.Fatal(err)
	}
	revisions := revisionSvc(t, db)
	drafts, err := revisions.PendingDrafts(t.Context(), service.LocalPrincipal(editor), dev)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].Valid || drafts[0].OwnerID != string(editor) || drafts[0].Revealed || drafts[0].Value != "" {
		t.Fatalf("owner invalid secret advisory = %+v", drafts)
	}
	others, err := revisions.PendingDrafts(t.Context(), service.LocalPrincipal(reader), dev)
	if err != nil || len(others) != 0 {
		t.Fatalf("reader learned another draft predicate: %+v, %v", others, err)
	}
	// A newly saved valid version replaces the invalid marker on the same cell.
	if _, err := values.Set(t.Context(), service.LocalPrincipal(editor), dev, "SECRET_PORT", "123", nil); err != nil {
		t.Fatal(err)
	}
	drafts, err = revisions.PendingDrafts(t.Context(), service.LocalPrincipal(editor), dev)
	if err != nil || len(drafts) != 1 || !drafts[0].Valid {
		t.Fatalf("corrected advisory = %+v, %v", drafts, err)
	}
	// Existing pending rows are re-evaluated against the current declaration.
	minimum := int64(200)
	if _, err := keys.UpdateDeclaration(t.Context(), actor, scope, key.ID, service.KeyDeclarationUpdate{
		Declaration: decl(schema.Rule{Type: schema.TypeInteger, Min: &minimum}), Presence: schema.DefaultPresenceRules(),
	}, nil); err != nil {
		t.Fatal(err)
	}
	drafts, err = revisions.PendingDrafts(t.Context(), service.LocalPrincipal(editor), dev)
	if err != nil || len(drafts) != 1 || drafts[0].Valid {
		t.Fatalf("stale declaration advisory = %+v, %v", drafts, err)
	}
	projects := &service.Projects{DB: db}
	assertPolicy := func(principal domain.PrincipalID, policy, deletion bool) {
		t.Helper()
		got, err := projects.Get(t.Context(), service.LocalPrincipal(principal), scope)
		if err != nil {
			t.Fatal(err)
		}
		if got.CanManagePolicy == nil || *got.CanManagePolicy != policy || got.CanDelete == nil || *got.CanDelete != deletion {
			t.Fatalf("project caller affordances = %+v, want policy=%v delete=%v", got, policy, deletion)
		}
	}
	assertPolicy(reader, false, false)
	grantOrg(t, db, reader, scope.Org, "uipolicy", "project-settings")
	assertPolicy(reader, true, false)
	grantOrg(t, db, reader, scope.Org, "uidelete", "manage-projects")
	assertPolicy(reader, true, true)
	discovery := &service.Discovery{DB: db}
	first, err := discovery.InstanceIdentity(t.Context())
	if err != nil || first == "" {
		t.Fatalf("instance discovery = %q, %v", first, err)
	}
	second, err := discovery.InstanceIdentity(t.Context())
	if err != nil || second != first {
		t.Fatalf("instance identity changed = %q, %v", second, err)
	}
}
