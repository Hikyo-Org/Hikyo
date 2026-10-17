package isolation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
	"github.com/Hikyo-Org/hikyo/internal/scanning"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// TestMCPCodexNative drives the actual Codex app-server, including its own MCP
// initialization and HTTP client. No model turn, provider call or real project
// credential is involved. Opt in with HIKYO_TEST_CODEX_NATIVE=1 and codex on PATH.
func TestMCPCodexNative(t *testing.T) {
	runMCPCodexNative(t, true)
}

// TestMCPCodexNativeReadOnly isolates the five-tool operator-managed read
// workflow. It grants no edit authority and sends no stage or validate call.
func TestMCPCodexNativeReadOnly(t *testing.T) {
	runMCPCodexNative(t, false)
}

func runMCPCodexNative(t *testing.T, writeEnabled bool) {
	t.Helper()
	if os.Getenv("HIKYO_TEST_CODEX_NATIVE") != "1" {
		t.Skip("set HIKYO_TEST_CODEX_NATIVE=1 for native Codex interoperability")
	}
	version, err := exec.Command("codex", "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(strings.TrimSpace(string(version)))
	forEngines(t, func(t *testing.T, db *store.DB) {
		kr := probeKeyring(t, db)
		auth := authService(t, db)
		scan, err := scanning.Load()
		if err != nil {
			t.Fatal(err)
		}
		keys := &service.Keys{DB: db, Keyring: kr, Scan: scan}
		values := &service.Values{DB: db, Keyring: kr, Auth: auth, Scan: scan}
		revisions := &service.Revisions{DB: db, Keyring: kr, Auth: auth}
		project := scopeProject(orgA, prjA1)
		environment := scopeEnv(orgA, prjA1, envA1)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_native_mid', 'usr_custodian', 'manage-identities', 'org_a', NULL, NULL, `+ts+`)`)
		// Publish both classifications through the real services. An empty read or
		// declaration-only result must not stand in for a configuration-value read.
		var published []string
		for _, entry := range []struct {
			name, value    string
			classification schema.Classification
		}{
			{"NATIVE_CFG", "native-published-config", schema.Config},
			{"NATIVE_SECRET", mcpCanaryPlaintext, schema.Secret},
		} {
			mustCreateKey(t, keys, project, entry.name, entry.classification)
			staged, err := values.Set(t.Context(), service.LocalPrincipal(custodian), environment, entry.name, entry.value, nil)
			if err != nil {
				t.Fatal("could not stage native fixture value")
			}
			published = append(published, staged.VersionID)
		}
		if _, err := revisions.PublishPlanned(t.Context(), service.LocalPrincipal(custodian), environment, service.PublishRequest{VersionIDs: published}); err != nil {
			t.Fatal("could not publish native fixture values")
		}
		identity := mintMCPAutomation(t, db, auth, project, "native-codex", true)
		if writeEnabled {
			if _, err := (&service.Grants{DB: db, Auth: auth}).Create(t.Context(), service.LocalPrincipal(domain.PrincipalID("usr_orgadmin")), service.GrantSpec{Target: identity.Principal, Capability: domain.CapEdit, Scope: environment}); err != nil {
				t.Fatal(err)
			}
		}
		registry := mcpserver.NewRegistry()
		if err := mcpserver.RegisterProductionTools(registry, mcpserver.ProductionServices{Admission: &service.MCPAdmission{DB: db}, Definitions: keys, Environments: &service.Environments{DB: db, Keyring: kr}, Configuration: values, Pending: revisions, Revisions: revisions}); err != nil {
			t.Fatal(err)
		}
		if writeEnabled {
			if err := mcpserver.RegisterWriteTools(registry, mcpserver.WriteServices{Admission: &service.MCPAdmission{DB: db}, Staging: values, Validation: values}); err != nil {
				t.Fatal(err)
			}
		}
		sealer, err := kr.MCPCursorSealer()
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewUnstartedServer(nil)
		origin := "http://" + server.Listener.Addr().String()
		handler, err := mcpserver.New(mcpserver.Options{Registry: registry, ExternalOrigin: origin, Version: "native-test", CursorSealer: sealer})
		if err != nil {
			t.Fatal(err)
		}
		server.Config.Handler = observeNativeCodexWire(t, handler)
		server.Start()
		defer server.Close()

		home := t.TempDir()
		config := `[mcp_servers.hikyo]
url = "` + origin + mcpserver.CodexPath + `"
bearer_token_env_var = "HIKYO_NATIVE_TEST_TOKEN"
`
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "codex", "app-server")
		// Isolate credentials and MCP configuration from the operator's Codex home.
		for _, env := range os.Environ() {
			if !strings.HasPrefix(env, "CODEX_HOME=") && !strings.HasPrefix(env, "HIKYO_NATIVE_TEST_TOKEN=") {
				cmd.Env = append(cmd.Env, env)
			}
		}
		cmd.Env = append(cmd.Env, "CODEX_HOME="+home, "HIKYO_NATIVE_TEST_TOKEN="+identity.token)
		cmd.Dir = home
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stderr = io.Discard
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = input.Close(); cancel(); _ = cmd.Wait() }()
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		sequence := 0
		rpc := func(method string, params any) json.RawMessage {
			t.Helper()
			sequence++
			if err := json.NewEncoder(input).Encode(map[string]any{"jsonrpc": "2.0", "id": sequence, "method": method, "params": params}); err != nil {
				t.Fatal(err)
			}
			for scanner.Scan() {
				// Check all client frames, including notifications and error results,
				// before parsing or reporting anything from the client. Never dump a
				// provider/tool body or credential when an assertion fails.
				if bytes.Contains(scanner.Bytes(), []byte(mcpCanaryPlaintext)) || bytes.Contains(scanner.Bytes(), []byte(identity.token)) {
					t.Fatal("native client output disclosed protected fixture material")
				}
				var response struct {
					ID     int             `json:"id"`
					Result json.RawMessage `json:"result"`
					Error  json.RawMessage `json:"error"`
				}
				if json.Unmarshal(scanner.Bytes(), &response) != nil || response.ID != sequence {
					continue
				}
				if len(response.Error) > 0 {
					t.Fatalf("%s returned an RPC error; response body withheld", method)
				}
				return response.Result
			}
			t.Fatalf("%s ended without response: %v", method, scanner.Err())
			return nil
		}
		rpc("initialize", map[string]any{"clientInfo": map[string]string{"name": "hikyo-native-proof", "version": "1"}, "capabilities": map[string]bool{"experimentalApi": true}})
		if err := json.NewEncoder(input).Encode(map[string]any{"jsonrpc": "2.0", "method": "initialized"}); err != nil {
			t.Fatal(err)
		}
		thread := rpc("thread/start", map[string]any{"cwd": home, "ephemeral": true, "approvalPolicy": "never", "sandbox": "read-only"})
		var started struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if err := json.Unmarshal(thread, &started); err != nil || started.Thread.ID == "" {
			t.Fatal("missing thread id")
		}
		status := rpc("mcpServerStatus/list", map[string]any{"threadId": started.Thread.ID})
		var catalog struct {
			Data []struct {
				Name       string  `json:"name"`
				ToolsError *string `json:"toolsError"`
				Tools      map[string]struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if json.Unmarshal(status, &catalog) != nil || len(catalog.Data) != 1 || catalog.Data[0].Name != "hikyo" || catalog.NextCursor != nil || catalog.Data[0].ToolsError != nil {
			t.Fatal("native catalog did not contain one complete Hikyo server result")
		}
		expectedTools := make(map[string]bool)
		toolNames := mcpserver.ProductionToolNames()
		if writeEnabled {
			toolNames = mcpserver.AllToolNames()
		}
		for _, name := range toolNames {
			expectedTools[name] = true
		}
		if len(catalog.Data[0].Tools) != len(expectedTools) {
			t.Fatal("native catalog did not contain the exact closed tool count")
		}
		for _, tool := range catalog.Data[0].Tools {
			if !expectedTools[tool.Name] {
				t.Fatal("native catalog contained an unknown or duplicated tool")
			}
			delete(expectedTools, tool.Name)
		}
		call := func(tool string, args map[string]any) string {
			t.Helper()
			result := rpc("mcpServer/tool/call", map[string]any{"threadId": started.Thread.ID, "server": "hikyo", "tool": tool, "arguments": args})
			var called struct {
				IsError bool `json:"isError"`
			}
			if json.Unmarshal(result, &called) != nil || called.IsError {
				t.Fatalf("%s returned a tool error; response body withheld", tool)
			}
			return string(result)
		}
		if result := call(mcpserver.ToolListDefinitions, map[string]any{"org_id": "org_a", "project_id": "prj_a1"}); !strings.Contains(result, "NATIVE_CFG") {
			t.Fatal("native read missing fixture definition")
		}
		configuration := call(mcpserver.ToolInspectConfiguration, map[string]any{"org_id": "org_a", "project_id": "prj_a1", "environment_id": "env_a1"})
		var inspected struct {
			StructuredContent struct {
				Configuration []struct {
					Name           string  `json:"name"`
					Classification string  `json:"classification"`
					Set            bool    `json:"set"`
					Value          *string `json:"value"`
				} `json:"configuration"`
			} `json:"structuredContent"`
		}
		if err := json.Unmarshal([]byte(configuration), &inspected); err != nil {
			t.Fatal("native configuration result is not a JSON object")
		}
		configCells, secretCells := 0, 0
		for _, cell := range inspected.StructuredContent.Configuration {
			switch cell.Name {
			case "NATIVE_CFG":
				configCells++
				if cell.Classification != string(schema.Config) || !cell.Set || cell.Value == nil || *cell.Value != "native-published-config" {
					t.Error("native configuration read did not return the published config value")
				}
			case "NATIVE_SECRET":
				secretCells++
				if cell.Classification != string(schema.Secret) || !cell.Set || cell.Value != nil {
					t.Error("native configuration read did not confine the secret to classification and presence")
				}
			}
		}
		if configCells != 1 || secretCells != 1 {
			t.Fatal("native configuration read missing unique config and secret cells")
		}
		if !writeEnabled {
			if n := queryInt(t, db, `SELECT COUNT(*) FROM pending_changes WHERE owner_id = '`+string(identity.Principal)+`'`); n != 0 {
				t.Fatal("native read-only client left a pending draft")
			}
			if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE origin = 'mcp' AND actor_id = '`+string(identity.Principal)+`'`); n != 0 {
				t.Fatal("native successful reads changed the locked unaudited disposition")
			}
			t.Log("native Codex read-only profile discovered exactly five tools, read published config and secret metadata without canary disclosure, and created no draft or audit event")
			return
		}
		args := map[string]any{"org_id": "org_a", "project_id": "prj_a1", "environment_id": "env_a1", "key_name": "NATIVE_CFG", "operation": "set", "value": "native-ok"}
		if result := call(mcpserver.ToolValidateChange, args); !strings.Contains(result, `"valid":true`) {
			t.Fatal("native validation did not report valid; response body withheld")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM pending_changes WHERE owner_id = '`+string(identity.Principal)+`'`); n != 0 {
			t.Fatal("validation staged a draft")
		}
		if result := call(mcpserver.ToolStageChange, args); !strings.Contains(result, `"version_id"`) {
			t.Fatal("native stage did not return a version; response body withheld")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM pending_changes WHERE owner_id = '`+string(identity.Principal)+`'`); n != 1 {
			t.Fatalf("native drafts=%d", n)
		}
		for _, event := range []string{"value.staged", "value.change_validated"} {
			if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = '`+event+`' AND origin = 'mcp' AND actor_id = '`+string(identity.Principal)+`'`); n != 1 {
				t.Fatalf("native audit %s=%d", event, n)
			}
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'revision.published' AND origin = 'mcp'`); n != 0 {
			t.Fatal("native stage published")
		}
		t.Log("native Codex initialized, discovered seven tools, read published config and secret metadata without canary disclosure, validated and staged one inert draft; MCP audit verified")
	})
}

// observeNativeCodexWire records only protocol facts from the actual native
// client's HTTP requests and the handler's initialization response. This is
// local HTTP evidence, not public HTTPS, proxy, or deployed-replica proof.
func observeNativeCodexWire(t *testing.T, next http.Handler) http.Handler {
	t.Helper()
	type observation struct {
		Method             string `json:"method"`
		RequestedProtocol  string `json:"requested_protocol,omitempty"`
		HeaderProtocol     string `json:"header_protocol,omitempty"`
		NegotiatedProtocol string `json:"negotiated_protocol,omitempty"`
		Status             int    `json:"status"`
		RequestSession     bool   `json:"request_session"`
		ResponseSession    bool   `json:"response_session"`
		BearerPresent      bool   `json:"bearer_present"`
	}
	var mu sync.Mutex
	var observations []observation
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		initialized, called := false, false
		for _, observed := range observations {
			encoded, err := json.Marshal(observed)
			if err != nil {
				t.Fatal("could not encode native protocol observation")
			}
			t.Logf("native wire: %s", encoded)
			if observed.RequestSession || observed.ResponseSession {
				t.Error("native protocol allocated or sent a session ID")
			}
			if observed.Method == "initialize" {
				initialized = true
				if observed.Status != http.StatusOK || observed.RequestedProtocol != mcpserver.CodexProtocolVersion || observed.NegotiatedProtocol != mcpserver.CodexProtocolVersion {
					t.Error("native initialization did not negotiate the pinned Codex profile")
				}
			}
			if observed.Method == "tools/call" {
				called = true
				if observed.Status != http.StatusOK || observed.HeaderProtocol != mcpserver.CodexProtocolVersion || !observed.BearerPresent {
					t.Error("native tool call did not pass with its bearer and negotiated protocol header")
				}
			}
		}
		if !initialized || !called {
			t.Error("native wire evidence missing initialization or an actual tool call")
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != mcpserver.CodexPath {
			next.ServeHTTP(w, r)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, mcpserver.MaxRequestBytes+1))
		if err != nil {
			t.Error("could not observe native request body")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var request struct {
			Method string `json:"method"`
			Params struct {
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"params"`
		}
		if err := json.Unmarshal(raw, &request); err != nil {
			t.Error("native request is not a JSON-RPC object")
		}
		recorder := httptest.NewRecorder()
		next.ServeHTTP(recorder, r)
		observed := observation{
			Method: request.Method, RequestedProtocol: request.Params.ProtocolVersion,
			HeaderProtocol: r.Header.Get("Mcp-Protocol-Version"), Status: recorder.Code,
			RequestSession:  len(r.Header.Values("Mcp-Session-Id")) != 0,
			ResponseSession: len(recorder.Header().Values("Mcp-Session-Id")) != 0,
			BearerPresent:   len(r.Header.Values("Authorization")) != 0,
		}
		if request.Method == "initialize" {
			var response struct {
				Result struct {
					ProtocolVersion string `json:"protocolVersion"`
				} `json:"result"`
			}
			if json.Unmarshal(recorder.Body.Bytes(), &response) == nil {
				observed.NegotiatedProtocol = response.Result.ProtocolVersion
			}
		}
		mu.Lock()
		observations = append(observations, observed)
		mu.Unlock()
		for name, values := range recorder.Header() {
			w.Header()[name] = values
		}
		w.WriteHeader(recorder.Code)
		if _, err := w.Write(recorder.Body.Bytes()); err != nil {
			t.Error("could not forward native response")
		}
	})
}
