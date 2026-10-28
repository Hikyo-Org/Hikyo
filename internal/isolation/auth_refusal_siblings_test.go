package isolation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestFailedLoginWithDeadCookieKeepsDedicatedOwnershipAndNoProse(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := bootstrapFactorAdmin(t, db)
		signals := service.NewRefusalSignals(db)
		h := server.New(&server.API{Auth: admin.auth, Refusals: signals}, nil)
		cookie, _, err := crypto.NewArtifact(crypto.ArtifactBrowserSession)
		if err != nil {
			t.Fatal(err)
		}
		bad := httptest.NewRequest(http.MethodPost, api.PathPrefix+"/auth/local/login", bytes.NewReader([]byte("CANARY-BAD-BODY")))
		bad.RemoteAddr = "192.0.2.103:1234"
		bad.Header.Set("Content-Type", "application/json")
		bad.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: cookie})
		badResponse := httptest.NewRecorder()
		h.ServeHTTP(badResponse, bad)
		if badResponse.Code != http.StatusBadRequest || signals.RefusalHealth().PendingSince != 0 {
			t.Fatal("incidental cookie probe invented an authentication refusal")
		}
		body := []byte(`{"username":"canary-attempted-username","password":"CANARY-PASSWORD-REQUEST"}`)
		r := httptest.NewRequest(http.MethodPost, api.PathPrefix+"/auth/local/login", bytes.NewReader(body))
		r.RemoteAddr = "192.0.2.103:1234"
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: cookie})
		before := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.login' AND outcome='failure'")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized || queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.login' AND outcome='failure'") != before+1 || signals.RefusalHealth().PendingSince != 0 {
			t.Fatalf("dead cookie duplicated or blocked login failure: %d", w.Code)
		}
		var capture bytes.Buffer
		capture.Write(w.Body.Bytes())
		if err := (&service.Audits{DB: db}).InstanceExport(t.Context(), root, store.AuditFilter{}, 100, &capture); err != nil {
			t.Fatal(err)
		}
		for _, canary := range []string{"canary-attempted-username", "CANARY-PASSWORD-REQUEST", cookie} {
			if bytes.Contains(capture.Bytes(), []byte(canary)) {
				t.Fatal("credential prose leaked")
			}
		}
		payload := queryString(t, db, "SELECT payload FROM audit_instance_events WHERE type='auth.login' AND outcome='failure' ORDER BY seq DESC LIMIT 1")
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(payload), &fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := fields["username"]; ok {
			t.Fatal("username prose field present")
		}
	})
}

func TestFederationDedicatedRefusalIsNotDuplicated(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		rig := newFedRig(t, db)
		signals := service.NewRefusalSignals(db)
		h := server.New(&server.API{Refusals: signals, Delivery: rig.del}, nil)
		path := api.PathPrefix + "/orgs/org_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fee/projects/prj_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fdd/environments/env_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fcc/delivery"
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "192.0.2.103:1234"
		r.Header.Set("Authorization", "Bearer e30.e30.CANARY-ASSERTION")
		before := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='identity.federation_refused'")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized || queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='identity.federation_refused'") != before+1 || signals.RefusalHealth().PendingSince != 0 {
			t.Fatalf("dedicated federation refusal lost or duplicated: %d %+v", w.Code, signals.RefusalHealth())
		}
		if got := queryString(t, db, "SELECT correlation_id FROM audit_instance_events WHERE type='identity.federation_refused' ORDER BY seq DESC LIMIT 1"); got != w.Header().Get(server.RefusalRequestIDHeader) {
			t.Fatal("dedicated federation event lost correlation")
		}
	})
}

func TestSCIMRefusalOwnershipAcrossRealWire(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		auth := authService(t, db)
		svc := &service.SCIM{DB: db}
		signals := service.NewRefusalSignals(db)
		execRaw(t, db, fmt.Sprintf("INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('%s','refusal-scim',TRUE,'{}',%s)", wireOrg, ts))
		execRaw(t, db, fmt.Sprintf("INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_refusal_scim','usr_orgadmin','manage-members','%s',NULL,NULL,%s)", wireOrg, ts))
		execRaw(t, db, "INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('gor_refusal_scim','g_refusal_scim','manual','usr_orgadmin',"+ts+")")
		seedSCIMProvider(t, db, "refusal-scim", "https://refusal-scim.example.test", true)
		binding, err := svc.CreateBinding(t.Context(), service.LocalPrincipal(orgAdmin), wireOrg, service.SCIMBindingInput{ProviderKind: domain.ProviderOIDC, ProviderSlug: "refusal-scim", SubjectSource: domain.SubjectSourceExternalID})
		if err != nil {
			t.Fatal(err)
		}
		mint, err := svc.MintCredential(t.Context(), service.LocalPrincipal(orgAdmin), wireOrg, binding.ID, false, "")
		if err != nil {
			t.Fatal(err)
		}
		h := server.New(&server.API{Auth: auth, SCIMWire: svc, Refusals: signals}, nil)
		call := func(bindingID, token string) *httptest.ResponseRecorder {
			r := httptest.NewRequest(http.MethodGet, api.PathPrefix+"/orgs/"+string(wireOrg)+"/scim/v2/"+bindingID+"/Schemas", nil)
			r.RemoteAddr = "192.0.2.103:1234"
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			return w
		}
		before := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='scim.credential_refused'")
		mismatch := call("scb_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fcc", mint.Token)
		if mismatch.Code != http.StatusUnauthorized || queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='scim.credential_refused'") != before+1 || signals.RefusalHealth().PendingSince != 0 {
			t.Fatalf("SCIM mismatch lost or duplicated: %d %s", mismatch.Code, mismatch.Body)
		}
		unknown, _, err := crypto.NewArtifact(crypto.ArtifactSCIM)
		if err != nil {
			t.Fatal(err)
		}
		miss := call(binding.ID, unknown)
		if err := svc.RevokeCredential(t.Context(), service.LocalPrincipal(orgAdmin), wireOrg, binding.ID, mint.Credential.ID); err != nil {
			t.Fatal(err)
		}
		revoked := call(binding.ID, mint.Token)
		if miss.Code != http.StatusUnauthorized || revoked.Code != miss.Code || revoked.Body.String() != miss.Body.String() {
			t.Fatal("SCIM artifact state enumerated")
		}
		for _, state := range []string{"missing", "malformed", "expired", "epoch"} {
			token := ""
			if state == "malformed" {
				token = "CANARY-MALFORMED-SCIM"
			}
			if state == "expired" || state == "epoch" {
				m, err := svc.MintCredential(t.Context(), service.LocalPrincipal(orgAdmin), wireOrg, binding.ID, false, "")
				if err != nil {
					t.Fatal(err)
				}
				token = m.Token
				if state == "expired" {
					execRaw(t, db, "UPDATE scim_credentials SET expires_at="+ts+" WHERE id='"+m.Credential.ID+"'")
				} else {
					execRaw(t, db, "UPDATE auth_instance_state SET credential_epoch=credential_epoch+1 WHERE id=1")
				}
			}
			w := call(binding.ID, token)
			if w.Code != miss.Code || w.Body.String() != miss.Body.String() {
				t.Fatalf("SCIM %s enumerated", state)
			}
		}
		if signals.RefusalHealth().PendingSince == 0 {
			t.Fatal("silent SCIM failure was not staged")
		}
		expireRefusalWindow(t, db)
		if err := signals.Seal(t.Context()); err != nil {
			t.Fatal(err)
		}
		assertRetainedRefusalRequest(t, db, revoked.Header().Get(server.RefusalRequestIDHeader), "192.0.2.103")
	})
}

func TestRequestRefusalConnectionAndWorkspaceWire(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		auth := authService(t, db)
		signals := service.NewRefusalSignals(db)
		h := server.New(&server.API{Auth: auth, Workspace: &service.Workspace{DB: db}, Refusals: signals}, nil)
		var proofID string
		for _, kind := range []string{"instance", "paired", "workspace"} {
			artifact := crypto.ArtifactInstanceConn
			path := api.PathPrefix + "/instance/directory"
			if kind == "workspace" {
				artifact = crypto.ArtifactWorkspaceSession
				path = api.PathPrefix + "/auth/whoami"
			}
			token, _, err := crypto.NewArtifact(artifact)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.RemoteAddr = "192.0.2.103:1234"
			r.Header.Set("Authorization", "Bearer "+token)
			if kind == "paired" {
				r.Header.Set("Authorization", "DPoP "+token)
				r.Header.Set("DPoP", "CANARY-RAW-PROOF")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("%s: %d %s", kind, w.Code, w.Body)
			}
			if kind == "paired" {
				proofID = w.Header().Get(server.RefusalRequestIDHeader)
			}
		}
		expireRefusalWindow(t, db)
		if err := signals.Seal(t.Context()); err != nil {
			t.Fatal(err)
		}
		assertRetainedRefusalRequest(t, db, proofID, "192.0.2.103")
	})
}

func TestRequestRefusalDeveloperRevokedKeepsDedicatedOwner(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		fixture := ceremonyFixture(t, db, "refusal-developer")
		minted, owner := mintDeveloperWithPasskey(t, db, &fixture, time.Hour)
		svc := &service.DeveloperCredentials{DB: db, Auth: fixture.admin.auth}
		if err := svc.Revoke(t.Context(), service.Bearer(owner), domain.Scope{}, minted.Credential.ID, false); err != nil {
			t.Fatal(err)
		}
		signals := service.NewRefusalSignals(db)
		h := server.New(&server.API{Auth: fixture.admin.auth, Settings: &service.ProjectSettings{DB: db, Auth: fixture.admin.auth}, Refusals: signals}, nil)
		path := api.PathPrefix + "/orgs/org_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fee/projects/prj_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fdd/environments/env_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fcc/settings"
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "192.0.2.103:1234"
		r.Header.Set("Authorization", "Bearer "+minted.Value)
		before := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.artifact_class_refused'")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized || queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.artifact_class_refused'") != before+1 || signals.RefusalHealth().PendingSince != 0 {
			t.Fatalf("revoked developer refusal lost or duplicated: %d", w.Code)
		}
	})
}
