package isolation

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestParameterDeclarationWirePreservesAuthority(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := newAccessWireEnv(t, db)
		execRaw(t, db, `INSERT INTO project_schema_revisions (org_id,project_id,revision) VALUES ('`+e.org+`','`+e.project+`',0)`)
		path := api.PathPrefix + "/orgs/" + e.org + "/projects/" + e.project + "/environments/" + e.env + "/parameters"
		body := map[string]string{"name": "PR_NUMBER", "pattern": "^[0-9]{1,6}$", "action": "add"}
		if status, payload := e.call(t, http.MethodPost, path, body); status != http.StatusNoContent {
			t.Fatalf("authorized declaration: %d %s", status, payload)
		}
		if status, payload := e.call(t, http.MethodGet, path, nil); status != http.StatusOK || !bytes.Contains(payload, []byte("PR_NUMBER")) {
			t.Fatalf("declaration list: %d %s", status, payload)
		}
		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_param_identity_admin','`+string(e.admin)+`','manage-identities','`+e.org+`','`+e.project+`',NULL,`+ts+`)`)
		execRaw(t, db, `INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('gor_param_identity_admin','g_param_identity_admin','manual','`+string(e.admin)+`',`+ts+`)`)
		identities := &service.Identities{DB: db, Auth: e.auth}
		scope := domain.Scope{Org: domain.OrgID(e.org), Project: domain.ProjectID(e.project)}
		account, err := identities.CreateServiceAccount(t.Context(), service.Bearer(e.token), scope, "read-only-preview", domain.ClassWorkload)
		if err != nil {
			t.Fatal(err)
		}
		credential, err := identities.MintCredential(t.Context(), service.Bearer(e.token), scope, account.ID, service.MintRequest{})
		if err != nil {
			t.Fatal(err)
		}
		body["name"] = "OTHER"
		status, payload := e.callAs(t, credential.Value, http.MethodPost, path, body)
		missing := api.PathPrefix + "/orgs/" + e.org + "/projects/" + e.project + "/environments/env_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fab/parameters"
		missingStatus, missingPayload := e.callAs(t, credential.Value, http.MethodPost, missing, body)
		if status != http.StatusNotFound || status != missingStatus || !bytes.Equal(payload, missingPayload) {
			t.Fatalf("unauthorized declaration differs from missing: %d %s versus %d %s", status, payload, missingStatus, missingPayload)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM environments WHERE parameters_json LIKE '%OTHER%'`); n != 0 {
			t.Fatal("unauthorized declaration persisted")
		}
	})
}
