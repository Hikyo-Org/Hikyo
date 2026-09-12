package isolation

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestExportAuditSeparatesPublicParametersFromSecretDisclosure(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		seedDeliveryCatalogue(t, db)
		actor := service.LocalPrincipal(identAdmin)
		scope := scopeEnv(orgA, prjA1, envA1)
		svc := revisionSvc(t, db)
		configBefore := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='disclosure.value_revealed' AND object_id='key_fed_url'`)
		secretBefore := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='disclosure.value_revealed' AND object_id='key_fed_pw'`)
		exportBefore := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='disclosure.values_exported'`)
		if _, _, err := svc.ExportWithParameters(t.Context(), actor, scope, 0, false, nil); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='disclosure.values_exported'`); n != exportBefore {
			t.Fatal("ordinary config export emitted parameter audit")
		}

		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_export_reveal','`+string(identAdmin)+`','reveal','org_a','prj_a1',NULL,`+ts+`)`)
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
		if err := envs.SetParameter(t.Context(), actor, scope, "PR_NUMBER", "^[0-9]+$", false); err != nil {
			t.Fatal(err)
		}
		publishDeliveryValues(t, db, envA1, map[string]string{"DATABASE_URL": "https://pr-${PR_NUMBER}.example.com"})
		supplied := map[string]string{"PR_NUMBER": "123"}
		for _, reveal := range []bool{false, true} {
			if _, _, err := svc.ExportWithParameters(t.Context(), actor, scope, 0, reveal, supplied); err != nil {
				t.Fatal(err)
			}
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='disclosure.value_revealed' AND object_id='key_fed_url'`); n != configBefore {
			t.Fatalf("config export added per-key disclosure events: %d -> %d", configBefore, n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='disclosure.value_revealed' AND object_id='key_fed_pw'`); n != secretBefore+1 {
			t.Fatalf("secret export disclosure count = %d, want %d", n, secretBefore+1)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='disclosure.values_exported' AND payload LIKE '%"PR_NUMBER":"123"%' AND payload LIKE '%"revision":2%'`); n != 2 {
			t.Fatalf("parameter export audit count = %d, want 2", n)
		}
		if _, _, err := svc.ExportWithParameters(t.Context(), actor, scope, 0, false, map[string]string{"PR_NUMBER": "invalid"}); err == nil {
			t.Fatal("invalid inputs exported")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='disclosure.values_exported'`); n != exportBefore+2 {
			t.Fatal("refused export emitted success event")
		}
	})
}

// Drive the CLI's browser-consent branch with real snapshot, export and audit
// services. The small HTTP bridge retains the harness's short fixture IDs;
// the separate transport conformance suite verifies the UUID wire contract.
func TestParameterizedExportConsentDoesNotDuplicateConfigDisclosure(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		fixture := ceremonyFixture(t, db, "parameter-export-ceremony")
		auth := fixture.admin.auth
		auth.ReauthWindow = 0
		scope := scopeEnv(orgA, prjA1, envA1)
		envs := &service.Environments{DB: db, Keyring: fixture.values.Keyring}
		if err := envs.SetParameter(t.Context(), service.LocalPrincipal(custodian), scope, "PR_NUMBER", "^[0-9]+$", false); err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, `INSERT INTO keys (id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,group_id,created_at) VALUES ('key_export_config','org_a','prj_a1','EXPORT_CONFIG','','config','',FALSE,'','{"rule":{"type":"string"}}','none','none',NULL,`+ts+`)`)
		publishValue(t, fixture.values, service.LocalPrincipal(custodian), scope, "EXPORT_CONFIG", "preview-${PR_NUMBER}")
		revisions := &service.Revisions{DB: db, Keyring: fixture.values.Keyring, Auth: auth}
		before := disclosureRows(t, db)
		metadataReads := 0
		var callbackURI string
		var consent apigen.CLIReauthStartRequest
		var opened service.ReauthResult
		httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			actor := service.Bearer(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			encode := func(value any) {
				if err := json.NewEncoder(w).Encode(value); err != nil {
					t.Error(err)
				}
			}
			switch {
			case r.URL.Path == api.PathPrefix+"/meta":
				encode(apigen.Meta{ServerVersion: "fixture-current", ApiRevision: api.Revision})
			case strings.HasSuffix(r.URL.Path, "/values/export"):
				var body apigen.ExportValuesRequest
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				supplied := map[string]string{}
				if body.Parameters != nil {
					supplied = *body.Parameters
				}
				reveal := body.Reveal != nil && *body.Reveal
				values, revision, err := revisions.ExportWithParameters(r.Context(), actor, scope, 0, reveal, supplied)
				if err != nil {
					w.WriteHeader(http.StatusForbidden)
					encode(map[string]any{"error": map[string]string{"code": "forbidden", "message": "not permitted"}})
					return
				}
				out := apigen.ExportedValues{Revision: revision, Items: []apigen.ExportedValue{}}
				for _, value := range values {
					item := apigen.ExportedValue{Name: value.Name, Classification: apigen.KeyClassification(value.Classification), Revealed: value.Revealed}
					if value.Revealed {
						item.Value = &value.Value
					}
					out.Items = append(out.Items, item)
				}
				out.Count = len(out.Items)
				encode(out)
			case strings.HasSuffix(r.URL.Path, "/reveal-window"):
				encode(apigen.RevealWindow{CanReveal: true, EffectiveWindowSeconds: 0})
			case strings.HasSuffix(r.URL.Path, "/revisions/latest"):
				metadataReads++
				detail, err := revisions.Show(r.Context(), actor, scope, 0)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusForbidden)
					return
				}
				out := apigen.RevisionDetail{Revision: detail.Revision, Keys: []apigen.SnapshotKey{}}
				for _, key := range detail.Keys {
					out.Keys = append(out.Keys, apigen.SnapshotKey{KeyId: key.KeyID, Name: key.Name, Classification: apigen.KeyClassification(key.Classification)})
				}
				encode(out)
			case strings.HasSuffix(r.URL.Path, "/auth/cli-reauth/start"):
				if err := json.NewDecoder(r.Body).Decode(&consent); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				callbackURI = consent.RedirectUri
				encode(map[string]string{"state": "hik_1_hs_export", "expires_at": "2030-01-01T00:00:00Z"})
			case strings.HasSuffix(r.URL.Path, "/auth/cli-reauth/redeem"):
				encode(map[string]any{"session_id": opened.SessionID, "session_token": opened.SessionToken, "windows": []any{map[string]any{"environment_id": envA1, "session_id": opened.SessionID, "single_decision": true, "window_expires": opened.WindowExpires}}})
			default:
				t.Errorf("unexpected consent/export request: %s %s", r.Method, r.URL.Path)
				http.NotFound(w, r)
			}
		}))
		defer httpServer.Close()
		stdout, stderr := &strings.Builder{}, &strings.Builder{}
		stateDir := t.TempDir()
		ios := cli.IO{Stdout: stdout, Stderr: stderr, Workdir: t.TempDir(), Env: cli.Env{Getenv: func(key string) string {
			if key == "HIKYO_STATE_DIR" {
				return stateDir
			}
			return ""
		}}}
		state, err := cli.NewState(ios.Env)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.Trust().Put(cli.TrustEntry{Name: "local", Origin: httpServer.URL}); err != nil {
			t.Fatal(err)
		}
		if err := state.PutSession(cli.SessionArtifact{Instance: "local", Origin: httpServer.URL, Token: fixture.admin.token, SessionID: "ses_fixture", Principal: string(fixture.admin.boot.PrincipalID), ExpiresAt: "2030-01-01T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
		ios.OpenURL = func(string) error {
			if metadataReads != 1 {
				return fmt.Errorf("consent metadata reads = %d, want 1", metadataReads)
			}
			if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='disclosure.values_exported'`); n != 0 {
				return fmt.Errorf("pre-consent export events = %d, want 0", n)
			}
			if consent.KeyIds == nil || len(*consent.KeyIds) != 2 {
				return fmt.Errorf("consent keys = %v, want two snapshot secrets", consent.KeyIds)
			}
			var keys []string
			for _, key := range *consent.KeyIds {
				keys = append(keys, string(key))
			}
			opened = passkeyCeremony(t, auth, t.Context(), fixture.admin.token, service.PurposeReveal, string(envA1), keys, fixture.device)
			go func() {
				response, err := http.Get(callbackURI + "?" + url.Values{"state": {"hik_1_hs_export"}, "code": {"hik_1_hc_export"}}.Encode())
				if err != nil {
					t.Error(err)
					return
				}
				_ = response.Body.Close()
			}()
			return nil
		}
		args := []string{"values", "export", "--reveal", "--param", "PR_NUMBER=321", "--format", "dotenv", "--dangerously-print", "--instance", "local", "--org", string(orgA), "--project", string(prjA1), "--env", string(envA1)}
		if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
			t.Fatalf("exit %d, stderr=%s", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "EXPORT_CONFIG=preview-321") || !strings.Contains(stdout.String(), "plaintext-"+ceremonySecretA) {
			t.Fatalf("export output: %s", stdout.String())
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='disclosure.value_revealed' AND object_id='key_export_config'`); n != 0 {
			t.Fatalf("config disclosure events = %d, want 0", n)
		}
		if got := disclosureRows(t, db); got != before+2 {
			t.Fatalf("secret disclosure events added = %d, want 2", got-before)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='disclosure.values_exported' AND payload LIKE '%"PR_NUMBER":"321"%'`); n != 1 {
			t.Fatalf("successful export events = %d, want 1", n)
		}
	})
}
