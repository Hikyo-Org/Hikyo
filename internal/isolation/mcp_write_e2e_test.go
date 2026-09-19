package isolation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
	"github.com/Hikyo-Org/hikyo/internal/scanning"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// TestMCPWriteSurfaceEndToEnd is the mcp-write ADR acceptance run against the
// real datastore: the write tools are installed only behind the flag, a stage
// lands an inert pending draft and its audit row with origin=mcp, a validate
// reports publish-time refusals and scanner findings without staging, nothing
// publishes, and the secret canary never crosses the transport or the trail.
func TestMCPWriteSurfaceEndToEnd(t *testing.T) {
	for _, codex := range []bool{false, true} {
		name := "modern"
		if codex {
			name = "codex"
		}
		t.Run(name, func(t *testing.T) { runTestMCPWriteSurfaceEndToEnd(t, codex) })
	}
}

func runTestMCPWriteSurfaceEndToEnd(t *testing.T, codex bool) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		kr := probeKeyring(t, db)
		auth := authService(t, db)
		rs, err := scanning.Load()
		if err != nil {
			t.Fatal(err)
		}
		keys := &service.Keys{DB: db, Keyring: kr, Scan: rs}
		values := &service.Values{DB: db, Keyring: kr, Auth: auth, Scan: rs}
		revisions := &service.Revisions{DB: db, Keyring: kr, Auth: auth}
		environments := &service.Environments{DB: db, Keyring: kr}
		projectScope := scopeProject(orgA, prjA1)
		envScope := scopeEnv(orgA, prjA1, envA1)

		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES (`+
			`'g_cu_mid', 'usr_custodian', 'manage-identities', 'org_a', NULL, NULL, `+ts+`)`)
		mustCreateKey(t, keys, projectScope, "CANARY_SECRET", schema.Secret)
		mustCreateKey(t, keys, projectScope, "PUBLIC_CFG", schema.Config)
		if _, err := keys.Create(ctx, service.LocalPrincipal(custodian), projectScope, service.KeySpec{
			Name: "PORT", Classification: string(schema.Config),
			Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeInteger}},
			Presence:    schema.DefaultPresenceRules(),
		}, nil); err != nil {
			t.Fatalf("create PORT: %v", err)
		}

		// Two automation identities: one holds edit on the environment, one only
		// read on the project, through the same grant writer operators use.
		editor := mintMCPAutomation(t, db, auth, projectScope, "mcp-editor", false)
		if _, err := (&service.Grants{DB: db, Auth: auth}).Create(ctx, service.LocalPrincipal(domain.PrincipalID("usr_orgadmin")),
			service.GrantSpec{Target: editor.Principal, Capability: domain.CapEdit, Scope: envScope}); err != nil {
			t.Fatalf("grant edit: %v", err)
		}
		reader := mintMCPAutomation(t, db, auth, projectScope, "mcp-reader", true)

		sealer, err := kr.MCPCursorSealer()
		if err != nil {
			t.Fatal(err)
		}
		readServices := mcpserver.ProductionServices{
			Admission: &service.MCPAdmission{DB: db}, Definitions: keys, Environments: environments,
			Configuration: values, Pending: revisions, Revisions: revisions,
		}
		newHandler := func(write bool) http.Handler {
			registry := mcpserver.NewRegistry()
			if err := mcpserver.RegisterProductionTools(registry, readServices); err != nil {
				t.Fatal(err)
			}
			if write {
				if err := mcpserver.RegisterWriteTools(registry, mcpserver.WriteServices{
					Admission: &service.MCPAdmission{DB: db}, Staging: values, Validation: values,
				}); err != nil {
					t.Fatal(err)
				}
			}
			h, err := mcpserver.New(mcpserver.Options{Registry: registry, ExternalOrigin: "https://hikyo.example.com", Version: "mcp-write-e2e", CursorSealer: sealer})
			if err != nil {
				t.Fatal(err)
			}
			return mcpProfile(h, codex)
		}
		toolNames := func(h http.Handler) map[string]bool {
			rec := mcpRequest(t, h, "", "tools/list", "", "", mcpserver.ProtocolVersion)
			var body struct {
				Result struct {
					Tools []struct {
						Name string `json:"name"`
					} `json:"tools"`
				} `json:"result"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode catalog: %v", err)
			}
			names := map[string]bool{}
			for _, tool := range body.Result.Tools {
				names[tool.Name] = true
			}
			return names
		}

		// Registration-time exclusion: with the flag off the write tools are
		// absent from the catalog and unknown to tools/call.
		readOnly := newHandler(false)
		if names := toolNames(readOnly); names[mcpserver.ToolStageChange] || names[mcpserver.ToolValidateChange] {
			t.Fatalf("read-only registry advertises write tools: %v", names)
		}
		stageArgs := `{"org_id":"org_a","project_id":"prj_a1","environment_id":"env_a1","key_name":"CANARY_SECRET","operation":"set","value":"` + mcpCanaryPlaintext + `"}`
		if rec := mcpCall(t, readOnly, editor.token, mcpserver.ToolStageChange, stageArgs); strings.Contains(rec.Body.String(), mcpserver.SafeOperationError) || !strings.Contains(rec.Body.String(), "error") {
			t.Fatalf("uninstalled stage tool answered as registered: %s", rec.Body.String())
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM pending_changes WHERE owner_id = '`+string(editor.Principal)+`'`); n != 0 {
			t.Fatalf("uninstalled write tool staged %d drafts", n)
		}

		handler := newHandler(true)
		if names := toolNames(handler); !names[mcpserver.ToolStageChange] || !names[mcpserver.ToolValidateChange] || len(names) != len(mcpserver.AllToolNames()) {
			t.Fatalf("write registry catalog = %v", names)
		}

		// Stage the canary into the secret key over MCP: the draft lands, its
		// value.staged row carries origin=mcp, nothing publishes, and the
		// response, the trail, and the logs are canary-free.
		stage := mcpCall(t, handler, editor.token, mcpserver.ToolStageChange, stageArgs)
		if stage.Code != http.StatusOK || !strings.Contains(stage.Body.String(), `"version_id"`) {
			t.Fatalf("stage = %d %s", stage.Code, stage.Body.String())
		}
		assertMCPNoCanary(t, "stage response", stage.Body.Bytes())
		editorDrafts := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM pending_changes WHERE owner_id = '`+string(editor.Principal)+`' AND operation = 'set'`)
		}
		if n := editorDrafts(); n != 1 {
			t.Fatalf("drafts after stage = %d, want 1", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'value.staged' AND origin = 'mcp' AND actor_id = '`+string(editor.Principal)+`'`); n != 1 {
			t.Fatalf("value.staged origin=mcp events = %d, want 1", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'revision.published' AND origin = 'mcp'`); n != 0 {
			t.Fatalf("MCP stage published %d revisions", n)
		}

		// A config value that trips the scanner: the finding rides the response
		// with a keep-as-config token, and finding_warned is recorded.
		warn := mcpCall(t, handler, editor.token, mcpserver.ToolStageChange,
			`{"org_id":"org_a","project_id":"prj_a1","environment_id":"env_a1","key_name":"PUBLIC_CFG","operation":"set","value":"`+plantedCredential+`"}`)
		if warn.Code != http.StatusOK || !strings.Contains(warn.Body.String(), `"acknowledgement"`) {
			t.Fatalf("stage with finding = %d %s", warn.Code, warn.Body.String())
		}

		// Validate: an integer key with a non-integer proposal is reported as a
		// publish-time refusal, with the same safe wording publish uses, and
		// nothing is staged; the audit row carries the verdict and origin=mcp.
		validate := mcpCall(t, handler, editor.token, mcpserver.ToolValidateChange,
			`{"org_id":"org_a","project_id":"prj_a1","environment_id":"env_a1","key_name":"PORT","operation":"set","value":"eighty"}`)
		body := validate.Body.String()
		if validate.Code != http.StatusOK || !strings.Contains(body, `"valid":false`) || !strings.Contains(body, `value for \"PORT\" is invalid`) {
			t.Fatalf("validate = %d %s", validate.Code, body)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'value.change_validated' AND origin = 'mcp' AND actor_id = '`+string(editor.Principal)+`'`); n != 1 {
			t.Fatalf("value.change_validated origin=mcp events = %d, want 1", n)
		}
		if n := editorDrafts(); n != 2 {
			t.Fatalf("validate changed the draft count: %d, want 2", n)
		}
		// Validate evaluates what stage would store: a padded integer is valid
		// because stage seals the normalized value and publish validates that.
		padded := mcpCall(t, handler, editor.token, mcpserver.ToolValidateChange,
			`{"org_id":"org_a","project_id":"prj_a1","environment_id":"env_a1","key_name":"PORT","operation":"set","value":"  8080 "}`)
		if !strings.Contains(padded.Body.String(), `"valid":true`) {
			t.Fatalf("padded integer validate = %s", padded.Body.String())
		}

		// Validate an INVALID proposal on a secret key: the schema verdict text
		// rides `problems`, and for a secret key the engine puts no instance
		// data in it, so the canary still never crosses.
		if _, err := keys.Create(ctx, service.LocalPrincipal(custodian), projectScope, service.KeySpec{
			Name: "SECRET_PORT", Classification: string(schema.Secret),
			Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeInteger}},
			Presence:    schema.DefaultPresenceRules(),
		}, nil); err != nil {
			t.Fatalf("create SECRET_PORT: %v", err)
		}
		secretInvalid := mcpCall(t, handler, editor.token, mcpserver.ToolValidateChange,
			`{"org_id":"org_a","project_id":"prj_a1","environment_id":"env_a1","key_name":"SECRET_PORT","operation":"set","value":"`+mcpCanaryPlaintext+`"}`)
		if !strings.Contains(secretInvalid.Body.String(), `"valid":false`) {
			t.Fatalf("secret invalid validate = %s", secretInvalid.Body.String())
		}
		assertMCPNoCanary(t, "secret invalid validate response", secretInvalid.Body.Bytes())

		// Validate on a secret key with the canary: valid, no scanner, no echo.
		secretValidate := mcpCall(t, handler, editor.token, mcpserver.ToolValidateChange,
			`{"org_id":"org_a","project_id":"prj_a1","environment_id":"env_a1","key_name":"CANARY_SECRET","operation":"set","value":"`+mcpCanaryPlaintext+`"}`)
		if !strings.Contains(secretValidate.Body.String(), `"valid":true`) {
			t.Fatalf("secret validate = %s", secretValidate.Body.String())
		}
		assertMCPNoCanary(t, "secret validate response", secretValidate.Body.Bytes())

		// A read-only automation is refused both write tools with the one safe
		// error, emits grant.denied with origin=mcp, and stages nothing.
		for _, tool := range []string{mcpserver.ToolStageChange, mcpserver.ToolValidateChange} {
			denied := mcpCall(t, handler, reader.token, tool, stageArgs)
			if !strings.Contains(denied.Body.String(), mcpserver.SafeOperationError) {
				t.Fatalf("%s for a read-only identity = %s", tool, denied.Body.String())
			}
			assertMCPNoCanary(t, tool+" denial", denied.Body.Bytes())
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'grant.denied' AND origin = 'mcp' AND actor_id = '`+string(reader.Principal)+`'`); n == 0 {
			t.Fatal("write denial emitted no grant.denied with origin=mcp")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM pending_changes WHERE owner_id = '`+string(reader.Principal)+`'`); n != 0 {
			t.Fatalf("denied identity owns %d drafts", n)
		}

		// Cancellation (ADR § 6): a tools/call whose request context is cancelled
		// as the stage reaches the service commits nothing. The decorator cancels
		// the request context at the service boundary, so the real service and
		// transaction path run under a cancelled context and no draft or
		// value.staged row survives. Mid-transaction cancellation rolling back
		// open store work is the tx package's contract
		// (TestRetryLoopRespectsCancelledContext); this proves the transport
		// hands the cancelled context all the way down.
		cancelHandler := func() (http.Handler, *cancellingStager) {
			stager := &cancellingStager{inner: values}
			registry := mcpserver.NewRegistry()
			if err := mcpserver.RegisterProductionTools(registry, readServices); err != nil {
				t.Fatal(err)
			}
			if err := mcpserver.RegisterWriteTools(registry, mcpserver.WriteServices{
				Admission: &service.MCPAdmission{DB: db}, Staging: stager, Validation: values,
			}); err != nil {
				t.Fatal(err)
			}
			h, err := mcpserver.New(mcpserver.Options{Registry: registry, ExternalOrigin: "https://hikyo.example.com", Version: "mcp-write-e2e", CursorSealer: sealer})
			if err != nil {
				t.Fatal(err)
			}
			return mcpProfile(h, codex), stager
		}
		cancelling, stager := cancelHandler()
		cancelCtx, cancel := context.WithCancel(ctx)
		stager.cancel = cancel
		beforeStaged := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'value.staged'`)
		draftsBefore := editorDrafts()
		cancelArgs := `{"org_id":"org_a","project_id":"prj_a1","environment_id":"env_a1","key_name":"PORT","operation":"set","value":"8080"}`
		cancelRec := mcpRequestWithContext(t, cancelCtx, cancelling, editor.token, "tools/call", mcpserver.ToolStageChange, cancelArgs, mcpserver.ProtocolVersion)
		cancel()
		if !stager.reached {
			t.Fatal("cancellation probe never reached the staging service")
		}
		if strings.Contains(cancelRec.Body.String(), `"version_id"`) {
			t.Fatalf("cancelled stage reported success: %s", cancelRec.Body.String())
		}
		if n := editorDrafts(); n != draftsBefore {
			t.Fatalf("cancelled stage left a draft: %d, want %d", n, draftsBefore)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'value.staged'`); n != beforeStaged {
			t.Fatalf("cancelled stage recorded value.staged: %d, want %d", n, beforeStaged)
		}

		for _, table := range []string{"audit_tenant_events", "audit_instance_events"} {
			if leaked := queryInt(t, db, `SELECT COUNT(*) FROM `+table+` WHERE CAST(payload AS TEXT) LIKE '%`+mcpCanaryPlaintext+`%'`); leaked != 0 {
				t.Fatalf("secret canary leaked into %s: %d rows", table, leaked)
			}
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://hikyo.example.com/mcp", nil))
		assertMCPNoCanary(t, "transport", rec.Body.Bytes())
	})
}

// cancellingStager cancels the request context the moment the stage reaches
// the service, then delegates, so the real transaction runs cancelled.
type cancellingStager struct {
	inner   mcpserver.StagingService
	cancel  context.CancelFunc
	reached bool
}

func (c *cancellingStager) Set(ctx context.Context, actor service.Actor, scope domain.Scope, keyName, value string, acks []string) (service.StagedChange, error) {
	c.reached = true
	if c.cancel != nil {
		c.cancel()
	}
	return c.inner.Set(ctx, actor, scope, keyName, value, acks)
}

func (c *cancellingStager) Unset(ctx context.Context, actor service.Actor, scope domain.Scope, keyName string) (service.StagedChange, error) {
	c.reached = true
	if c.cancel != nil {
		c.cancel()
	}
	return c.inner.Unset(ctx, actor, scope, keyName)
}
