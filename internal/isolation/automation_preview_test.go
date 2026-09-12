package isolation

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Exercise network admission AND the live grants, rather than just asserting
// that the CLI's machine-eligible table contains the new spellings.
func TestAutomationPreviewLifecycle(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		wire := newAccessWireEnv(t, db)
		keyring := wire.auth.Keyring
		scope := domain.Scope{Org: domain.OrgID(wire.org), Project: domain.ProjectID(wire.project)}
		sourceScope := domain.Scope{Org: scope.Org, Project: scope.Project, Env: domain.EnvID(wire.env)}
		execRaw(t, db, fmt.Sprintf(`INSERT INTO project_schema_revisions (org_id, project_id, revision) VALUES ('%s', '%s', 0)`, wire.org, wire.project))
		admin := service.LocalPrincipal(wire.admin)
		for i, capability := range []string{"read", "edit", "publish", "definitions-edit", "manage-identities"} {
			execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_preview_admin_%d', '%s', '%s', '%s', '%s', NULL, %s)`, i, wire.admin, capability, wire.org, wire.project, ts))
		}
		seedOrigins(t, db)
		values := &service.Values{DB: db, Keyring: keyring}
		revisions := &service.Revisions{DB: db, Keyring: keyring}
		for i, key := range []struct{ name, classification, value string }{{"DATABASE_URL", "config", "postgres://preview"}, {"DATABASE_PASSWORD", "secret", "source-password"}} {
			execRaw(t, db, fmt.Sprintf(`INSERT INTO keys (id, org_id, project_id, name, folder_path, classification, description, deprecated, deprecation_note, declaration, required_mode, forbidden_mode, group_id, created_at) VALUES ('key_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f0%d', '%s', '%s', '%s', '', '%s', '', FALSE, '', '{"rule":{"type":"string"}}', 'none', 'none', NULL, %s)`, i, wire.org, wire.project, key.name, key.classification, ts))
			staged, err := values.Set(t.Context(), admin, sourceScope, key.name, key.value, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := revisions.PublishPlanned(t.Context(), admin, sourceScope, service.PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
				t.Fatal(err)
			}
		}
		srv := httptest.NewServer(server.New(&service.System{DB: db}, &server.API{
			Auth:         wire.auth,
			Environments: &service.Environments{DB: db, Keyring: keyring},
			Values:       values, Revisions: revisions,
			Pins:     &service.Pins{DB: db},
			Delivery: &service.Delivery{DB: db, Keyring: keyring},
		}, nil))
		t.Cleanup(srv.Close)
		wire.srv = srv
		identities := &service.Identities{DB: db, Auth: wire.auth}
		sa, err := identities.CreateServiceAccount(t.Context(), admin, scope, "preview-ci", domain.ClassAutomation)
		if err != nil {
			t.Fatal(err)
		}
		minted, err := identities.MintCredential(t.Context(), admin, scope, sa.ID, service.MintRequest{})
		if err != nil {
			t.Fatal(err)
		}
		for i, cap := range []string{"read", "edit", "publish", "definitions-edit"} {
			execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_preview_%d', '%s', '%s', '%s', '%s', NULL, %s)`, i, sa.Principal, cap, wire.org, wire.project, ts))
		}
		seedOrigins(t, db)
		base := api.PathPrefix + "/orgs/" + wire.org + "/projects/" + wire.project
		call := func(method, path string, body any, want int) []byte {
			t.Helper()
			code, raw := wire.callAs(t, minted.Value, method, path, body)
			if code != want {
				t.Fatalf("%s %s = %d %s, want %d", method, path, code, raw, want)
			}
			return raw
		}
		// Without disclosure authority, a clone takes config only and reports
		// the secret it omitted. It never silently launders source plaintext.
		raw := call(http.MethodPost, base+"/environments/clone", apigen.CloneEnvironmentRequest{Name: "pr-730", SourceEnvironmentId: wire.env}, http.StatusCreated)
		var cloned apigen.ClonedEnvironment
		if err := json.Unmarshal(raw, &cloned); err != nil {
			t.Fatal(err)
		}
		if len(cloned.UncopiedSecrets) != 1 || cloned.UncopiedSecrets[0] != "DATABASE_PASSWORD" {
			t.Fatalf("clone omission = %s", raw)
		}
		envPath := base + "/environments/" + cloned.Environment.Id
		call(http.MethodGet, envPath, nil, http.StatusOK)
		call(http.MethodGet, base+"/environments", nil, http.StatusOK)
		copyBody := apigen.CopyValuesRequest{SourceEnvironmentId: wire.env, DestinationEnvironmentIds: []string{cloned.Environment.Id}, Keys: []string{"DATABASE_PASSWORD"}}
		call(http.MethodPost, base+"/values/copy", copyBody, http.StatusNotFound)
		// New material is a supported blind write. Staging and publication
		// return identifiers/metadata, never either password.
		raw = call(http.MethodPut, envPath+"/values/DATABASE_PASSWORD", map[string]string{"value": "new-ci-password"}, http.StatusOK)
		var staged apigen.PendingChange
		if err := json.Unmarshal(raw, &staged); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "new-ci-password") {
			t.Fatal("staging echoed secret")
		}
		versions := []string{staged.VersionId}
		call(http.MethodPost, envPath+"/publish", apigen.PublishRequest{VersionIds: &versions}, http.StatusOK)
		raw = call(http.MethodGet, envPath+"/revisions/latest", nil, http.StatusOK)
		if strings.Contains(string(raw), "new-ci-password") {
			t.Fatal("revision metadata disclosed secret")
		}
		call(http.MethodGet, envPath+"/pins", nil, http.StatusOK)
		raw = call(http.MethodGet, envPath+"/delivery?projection=config-only", nil, http.StatusOK)
		var fetched apigen.DeliveryResponse
		if err := json.Unmarshal(raw, &fetched); err != nil {
			t.Fatal(err)
		}
		if fetched.Revision < 1 || strings.Contains(string(raw), "new-ci-password") {
			t.Fatalf("config export leaked a secret or lost snapshot identity: %s", raw)
		}
		call(http.MethodPost, envPath+"/values/export", apigen.ExportValuesRequest{}, http.StatusNotFound)
		// Secret copying needs explicit project opt-in AND scoped reveal.
		execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_preview_reveal', '%s', 'reveal', '%s', '%s', '%s', %s)`, sa.Principal, wire.org, wire.project, wire.env, ts))
		seedOrigins(t, db)
		call(http.MethodPost, base+"/values/copy", copyBody, http.StatusNotFound)
		execRaw(t, db, fmt.Sprintf(`UPDATE projects SET machine_reveal = TRUE WHERE id = '%s'`, wire.project))
		call(http.MethodPost, base+"/values/copy", copyBody, http.StatusNotFound)
		execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_preview_reveal_dest', '%s', 'reveal', '%s', '%s', '%s', %s)`, sa.Principal, wire.org, wire.project, cloned.Environment.Id, ts))
		seedOrigins(t, db)
		raw = call(http.MethodGet, envPath+"/delivery", nil, http.StatusOK)
		if !strings.Contains(string(raw), "new-ci-password") {
			t.Fatal("authorized machine delivery did not export the published secret")
		}
		call(http.MethodPost, base+"/values/copy", copyBody, http.StatusOK)
		// Protection is checked live at the shared authorizer, even though the
		// machine still holds project-wide definitions-edit.
		execRaw(t, db, fmt.Sprintf(`UPDATE environments SET protected = TRUE WHERE id = '%s'`, cloned.Environment.Id))
		call(http.MethodDelete, envPath, nil, http.StatusNotFound)
		execRaw(t, db, fmt.Sprintf(`UPDATE environments SET protected = FALSE WHERE id = '%s'`, cloned.Environment.Id))
		call(http.MethodDelete, envPath, nil, http.StatusNoContent)
		if n := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM snapshots WHERE environment_id = '%s'`, cloned.Environment.Id)); n != 0 {
			t.Fatalf("cleanup retained %d snapshots", n)
		}
		const sibling = "prj_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f99"
		execRaw(t, db, fmt.Sprintf(`INSERT INTO projects (id, org_id, name, created_at) VALUES ('%s', '%s', 'foreign-project', %s)`, sibling, wire.org, ts))
		call(http.MethodPost, api.PathPrefix+"/orgs/"+wire.org+"/projects/"+sibling+"/environments", apigen.CreateEnvironmentRequest{Name: "foreign"}, http.StatusNotFound)
		if err := identities.RevokeCredential(t.Context(), admin, scope, sa.ID, minted.Credential.ID); err != nil {
			t.Fatal(err)
		}
		call(http.MethodPost, base+"/environments", apigen.CreateEnvironmentRequest{Name: "revoked"}, http.StatusUnauthorized)
	})
}
