package isolation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

const mcpCanaryPlaintext = "CANARY-PLAINTEXT-9Z-do-not-disclose"

func setupMCPHandler(t *testing.T, sealer mcpserver.CursorSealer, services mcpserver.ProductionServices) http.Handler {
	t.Helper()
	registry := mcpserver.NewRegistry()
	if err := mcpserver.RegisterProductionTools(registry, services); err != nil {
		t.Fatalf("register tools: %v", err)
	}
	handler, err := mcpserver.New(mcpserver.Options{
		Registry:       registry,
		ExternalOrigin: "https://hikyo.example.com",
		Version:        "mcp-e2e",
		CursorSealer:   sealer,
	})
	if err != nil {
		t.Fatalf("new mcp handler: %v", err)
	}
	return handler
}

// mcpCall drives one tools/call over the stateless JSON transport.
func mcpCall(t *testing.T, handler http.Handler, token, tool, arguments string) *httptest.ResponseRecorder {
	t.Helper()
	return mcpRequest(t, handler, token, "tools/call", tool, arguments, mcpserver.ProtocolVersion)
}

func mcpRequest(t *testing.T, handler http.Handler, token, method, tool, arguments, version string) *httptest.ResponseRecorder {
	t.Helper()
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"` + mcpserver.ProtocolVersion + `","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"e2e","version":"1"}}`
	if version != mcpserver.ProtocolVersion {
		meta = `"_meta":{"io.modelcontextprotocol/protocolVersion":"` + version + `","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"e2e","version":"1"}}`
	}
	params := meta
	if method == "tools/call" {
		params = `"name":"` + tool + `","arguments":` + arguments + `,` + meta
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":{` + params + `}}`
	req := httptest.NewRequest(http.MethodPost, "https://hikyo.example.com"+mcpserver.Path, bytes.NewReader([]byte(body)))
	req.Host = "hikyo.example.com"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", version)
	req.Header.Set("Mcp-Method", method)
	if method == "tools/call" {
		req.Header.Set("Mcp-Name", tool)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func assertMCPNoCanary(t *testing.T, surface string, body []byte) {
	t.Helper()
	if bytes.Contains(body, []byte(mcpCanaryPlaintext)) {
		t.Fatalf("secret canary leaked through %s: %q", surface, body)
	}
}

type mcpOperationalHealth struct{}

func (mcpOperationalHealth) OperationalHealth(context.Context) (service.PruneHealth, error) {
	return service.PruneHealth{}, nil
}

func mustCreateKey(t *testing.T, keys *service.Keys, scope domain.Scope, name string, classification schema.Classification) {
	t.Helper()
	spec := service.KeySpec{
		Name: name, Classification: string(classification),
		Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}},
		Presence:    schema.DefaultPresenceRules(),
	}
	if _, err := keys.Create(t.Context(), service.LocalPrincipal(custodian), scope, spec, nil); err != nil {
		t.Fatalf("create key %s: %v", name, err)
	}
}

type workloadCredential struct {
	Principal    domain.PrincipalID
	AccountID    string
	CredentialID string
	token        string
}

// mintWorkload creates a workload service account and mints one credential for
// it. custodian is granted machine-identity administration so it can act.
func mintWorkload(t *testing.T, db *store.DB, auth *service.Auth, scope domain.Scope, name string) workloadCredential {
	t.Helper()
	identities := &service.Identities{DB: db, Auth: auth}
	sa, err := identities.CreateServiceAccount(t.Context(), service.LocalPrincipal(custodian), scope, name, domain.ClassWorkload)
	if err != nil {
		t.Fatalf("create service account %s: %v", name, err)
	}
	minted, err := identities.MintCredential(t.Context(), service.LocalPrincipal(custodian), scope, sa.ID, service.MintRequest{})
	if err != nil {
		t.Fatalf("mint credential %s: %v", name, err)
	}
	return workloadCredential{
		Principal: sa.Principal, AccountID: sa.ID,
		CredentialID: minted.Credential.ID, token: minted.Value,
	}
}

func extractNextCursor(t *testing.T, body []byte) string {
	t.Helper()
	var response struct {
		Result struct {
			StructuredContent struct {
				NextCursor string `json:"next_cursor"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode tool response: %v: %s", err, body)
	}
	return response.Result.StructuredContent.NextCursor
}

func TestMCPToolsEndToEndCanaryAndDenial(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		kr := probeKeyring(t, db)
		auth := authService(t, db)
		keys := &service.Keys{DB: db, Keyring: kr}
		values := &service.Values{DB: db, Keyring: kr, Auth: auth}
		revisions := &service.Revisions{DB: db, Keyring: kr, Auth: auth}
		environments := &service.Environments{DB: db, Keyring: kr}

		projectScope := scopeProject(orgA, prjA1)
		envScope := scopeEnv(orgA, prjA1, envA1)

		// custodian gains machine-identity administration so it can mint the
		// service accounts the MCP transport authenticates.
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES (`+
			`'g_cu_mid', 'usr_custodian', 'manage-identities', 'org_a', NULL, NULL, `+ts+`)`)

		// Seed a secret canary and a config value, then publish both.
		mustCreateKey(t, keys, projectScope, "CANARY_SECRET", schema.Secret)
		mustCreateKey(t, keys, projectScope, "PUBLIC_CFG", schema.Config)
		secretStaged, err := values.Set(ctx, service.LocalPrincipal(custodian), envScope, "CANARY_SECRET", mcpCanaryPlaintext, nil)
		if err != nil {
			t.Fatalf("stage secret: %v", err)
		}
		cfgStaged, err := values.Set(ctx, service.LocalPrincipal(custodian), envScope, "PUBLIC_CFG", "public-config-value", nil)
		if err != nil {
			t.Fatalf("stage config: %v", err)
		}
		if _, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(custodian), envScope,
			service.PublishRequest{VersionIDs: []string{secretStaged.VersionID, cfgStaged.VersionID}}); err != nil {
			t.Fatalf("publish: %v", err)
		}

		// A workload service account granted read across the project, and a
		// second with no grants at all.
		granted := mintWorkload(t, db, auth, projectScope, "mcp-granted")
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES (`+
			`'g_mcp_sa_read', '`+string(granted.Principal)+`', 'read', 'org_a', 'prj_a1', NULL, `+ts+`)`)
		execRaw(t, db, `INSERT INTO grant_origins (id, grant_id, kind, subject, created_at) VALUES (`+
			`'gor_mcp_sa_read', 'g_mcp_sa_read', 'manual', '`+string(granted.Principal)+`', `+ts+`)`)
		ungranted := mintWorkload(t, db, auth, projectScope, "mcp-ungranted")

		sealer, err := kr.MCPCursorSealer()
		if err != nil {
			t.Fatal(err)
		}
		services := mcpserver.ProductionServices{
			Admission:   &service.MCPAdmission{DB: db},
			Definitions: keys, Environments: environments, Configuration: values,
			Pending: revisions, Revisions: revisions,
		}
		mcpHandler := setupMCPHandler(t, sealer, services)
		var logs bytes.Buffer
		metrics := server.NewMetrics(nil)
		handler := server.NewPublic(nil, &server.API{
			Metrics: metrics,
			Log:     slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		}, nil, server.PublicOptions{ExternalOrigin: "https://hikyo.example.com", MCP: mcpHandler})

		// Static discovery/catalog, input schemas, and protocol errors are part of
		// the model-context boundary too. The schemas expose ids, page_size, and
		// cursor only; no value, reveal, secret, or token argument exists.
		catalog := mcpRequest(t, handler, "", "tools/list", "", "", mcpserver.ProtocolVersion)
		assertMCPNoCanary(t, "static tool catalog", catalog.Body.Bytes())
		var catalogBody struct {
			Result struct {
				Tools []struct {
					InputSchema struct {
						Properties map[string]json.RawMessage `json:"properties"`
					} `json:"inputSchema"`
				} `json:"tools"`
			} `json:"result"`
		}
		if err := json.Unmarshal(catalog.Body.Bytes(), &catalogBody); err != nil {
			t.Fatalf("decode catalog: %v", err)
		}
		for _, tool := range catalogBody.Result.Tools {
			for _, forbidden := range []string{"value", "reveal", "secret", "token"} {
				if _, present := tool.InputSchema.Properties[forbidden]; present {
					t.Fatalf("static tool catalog exposes forbidden argument %q", forbidden)
				}
			}
		}
		protocolError := mcpRequest(t, handler, "", "tools/list", "", "", "2025-03-26")
		assertMCPNoCanary(t, "protocol error", protocolError.Body.Bytes())

		// Inspect discloses the config plaintext but never the secret plaintext.
		inspect := mcpCall(t, handler, granted.token, mcpserver.ToolInspectConfiguration,
			`{"org_id":"org_a","project_id":"prj_a1","environment_id":"env_a1","page_size":100}`)
		if inspect.Code != http.StatusOK {
			t.Fatalf("inspect = %d %q", inspect.Code, inspect.Body.String())
		}
		inspectBody := inspect.Body.String()
		assertMCPNoCanary(t, "inspect", inspect.Body.Bytes())
		if !strings.Contains(inspectBody, "public-config-value") {
			t.Fatalf("config plaintext missing from inspect: %q", inspectBody)
		}

		// Every other authorized tool is canary-free too.
		for _, tc := range []struct{ tool, args string }{
			{mcpserver.ToolListDefinitions, `{"org_id":"org_a","project_id":"prj_a1","page_size":100}`},
			{mcpserver.ToolListEnvironments, `{"org_id":"org_a","project_id":"prj_a1","page_size":100}`},
			{mcpserver.ToolListPendingChanges, `{"org_id":"org_a","project_id":"prj_a1","environment_id":"env_a1","page_size":100}`},
			{mcpserver.ToolListRevisions, `{"org_id":"org_a","project_id":"prj_a1","environment_id":"env_a1","page_size":100}`},
		} {
			rec := mcpCall(t, handler, granted.token, tc.tool, tc.args)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s = %d %q", tc.tool, rec.Code, rec.Body.String())
			}
			assertMCPNoCanary(t, tc.tool, rec.Body.Bytes())
		}

		// An ungranted service account is refused with the one safe error, which
		// discloses no tenant fact and no canary.
		denied := mcpCall(t, handler, ungranted.token, mcpserver.ToolInspectConfiguration,
			`{"org_id":"org_a","project_id":"prj_a1","environment_id":"env_a1"}`)
		if denied.Code != http.StatusOK {
			t.Fatalf("denied inspect http = %d %q", denied.Code, denied.Body.String())
		}
		deniedBody := denied.Body.String()
		if !strings.Contains(deniedBody, mcpserver.SafeOperationError) {
			t.Fatalf("denial not the safe error: %q", deniedBody)
		}
		if strings.Contains(deniedBody, mcpCanaryPlaintext) || strings.Contains(deniedBody, "public-config-value") {
			t.Fatalf("denial disclosed a tenant fact: %q", deniedBody)
		}

		// Audit parity (acceptance criterion 3): a capability denial through MCP
		// emits the existing grant.denied event, now carrying origin=mcp and the
		// refused principal, exactly as a REST read denial would.
		deniedEvents := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'grant.denied' AND origin = 'mcp' AND actor_id = '`+string(ungranted.Principal)+`'`)
		if deniedEvents == 0 {
			t.Fatal("MCP capability denial emitted no grant.denied event with origin=mcp")
		}
		if claims := queryInt(t, db, `SELECT COUNT(*) FROM mcp_rate_buckets WHERE principal_id = '`+string(ungranted.Principal)+`'`); claims != 0 {
			t.Fatalf("unauthorized principal consumed %d MCP rate buckets, want 0", claims)
		}

		// A valid bearer of a disallowed class (a human CLI session) is refused as
		// an artifact-class refusal with origin=mcp, not a capability denial.
		human := bootstrapAdmin(t, db, adminOpts{
			username: "mcp-human", displayName: "MCP Human",
			password: "an ordinary human session passphrase", auth: auth, login: true,
		})
		beforeRefused := queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.artifact_class_refused' AND origin = 'mcp'`)
		humanCall := mcpCall(t, handler, human.token, mcpserver.ToolInspectConfiguration,
			`{"org_id":"org_a","project_id":"prj_a1","environment_id":"env_a1"}`)
		if !strings.Contains(humanCall.Body.String(), mcpserver.SafeOperationError) {
			t.Fatalf("human-session call not the safe error: %q", humanCall.Body.String())
		}
		afterRefused := queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.artifact_class_refused' AND origin = 'mcp'`)
		if afterRefused != beforeRefused+1 {
			t.Fatalf("artifact-class refusal events = %d, want %d", afterRefused, beforeRefused+1)
		}

		// An invalid bearer is the silent, non-enumerating authentication failure:
		// no audit row of any kind.
		beforeInvalid := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events`) + queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events`)
		invalid := mcpCall(t, handler, "not-a-real-token", mcpserver.ToolInspectConfiguration,
			`{"org_id":"org_a","project_id":"prj_a1","environment_id":"env_a1"}`)
		if !strings.Contains(invalid.Body.String(), mcpserver.SafeOperationError) {
			t.Fatalf("invalid bearer call not the safe error: %q", invalid.Body.String())
		}
		afterInvalid := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events`) + queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events`)
		if afterInvalid != beforeInvalid {
			t.Fatalf("invalid bearer wrote %d audit rows, want 0", afterInvalid-beforeInvalid)
		}
		for _, table := range []string{"audit_tenant_events", "audit_instance_events"} {
			if leaked := queryInt(t, db, `SELECT COUNT(*) FROM `+table+` WHERE CAST(payload AS TEXT) LIKE '%`+mcpCanaryPlaintext+`%'`); leaked != 0 {
				t.Fatalf("secret canary leaked into %s: %d rows", table, leaked)
			}
		}
		assertMCPNoCanary(t, "debug logs", logs.Bytes())
		metricsRec := httptest.NewRecorder()
		server.NewOperational(nil, mcpOperationalHealth{}, metrics).ServeHTTP(metricsRec,
			httptest.NewRequest(http.MethodGet, "http://127.0.0.1/metrics", nil))
		assertMCPNoCanary(t, "metrics", metricsRec.Body.Bytes())

		// Alternating replicas: boot a distinct keyring, auth/admission services,
		// domain services, registry, and handler from the shared datastore and root
		// authority. A cursor minted by replica one must work on replica two.
		replicaKeyring := reloadProbeKeyring(t, db)
		replicaAuth := authServiceForKeyring(t, db, replicaKeyring)
		replicaServices := mcpserver.ProductionServices{
			Admission:     &service.MCPAdmission{DB: db},
			Definitions:   &service.Keys{DB: db, Keyring: replicaKeyring},
			Environments:  &service.Environments{DB: db, Keyring: replicaKeyring},
			Configuration: &service.Values{DB: db, Keyring: replicaKeyring, Auth: replicaAuth},
			Pending:       &service.Revisions{DB: db, Keyring: replicaKeyring, Auth: replicaAuth},
			Revisions:     &service.Revisions{DB: db, Keyring: replicaKeyring, Auth: replicaAuth},
		}
		replicaSealer, err := replicaKeyring.MCPCursorSealer()
		if err != nil {
			t.Fatal(err)
		}
		other := setupMCPHandler(t, replicaSealer, replicaServices)
		first := mcpCall(t, handler, granted.token, mcpserver.ToolListDefinitions,
			`{"org_id":"org_a","project_id":"prj_a1","page_size":1}`)
		cursor := extractNextCursor(t, first.Body.Bytes())
		if cursor == "" {
			t.Fatal("expected a continuation cursor for a one-item page")
		}
		second := mcpCall(t, other, granted.token, mcpserver.ToolListDefinitions,
			fmt.Sprintf(`{"org_id":"org_a","project_id":"prj_a1","page_size":1,"cursor":%q}`, cursor))
		if second.Code != http.StatusOK || strings.Contains(second.Body.String(), mcpserver.SafeOperationError) ||
			strings.Contains(second.Body.String(), mcpserver.ErrInvalidCursor.Error()) {
			t.Fatalf("alternating-replica continuation = %d %q", second.Code, second.Body.String())
		}
		if inflight := queryInt(t, db, `SELECT COUNT(*) FROM mcp_inflight`); inflight != 0 {
			t.Fatalf("completed MCP calls left %d shared concurrency claims", inflight)
		}

		// Revocation is uncached. The credential that just succeeded through both
		// replicas is denied on the very next request, with the exact same public
		// result as a token that never existed.
		identities := &service.Identities{DB: db, Auth: auth}
		if err := identities.RevokeCredential(ctx, service.LocalPrincipal(custodian), projectScope,
			granted.AccountID, granted.CredentialID); err != nil {
			t.Fatalf("revoke MCP credential: %v", err)
		}
		revoked := mcpCall(t, other, granted.token, mcpserver.ToolListDefinitions,
			`{"org_id":"org_a","project_id":"prj_a1"}`)
		invalidAfterRevoke := mcpCall(t, other, "not-a-real-token", mcpserver.ToolListDefinitions,
			`{"org_id":"org_a","project_id":"prj_a1"}`)
		if revoked.Code != http.StatusOK || revoked.Body.String() != invalidAfterRevoke.Body.String() ||
			!strings.Contains(revoked.Body.String(), mcpserver.SafeOperationError) {
			t.Fatalf("revoked result differs from invalid-token result: revoked=%d %q invalid=%d %q",
				revoked.Code, revoked.Body.String(), invalidAfterRevoke.Code, invalidAfterRevoke.Body.String())
		}
	})
}
