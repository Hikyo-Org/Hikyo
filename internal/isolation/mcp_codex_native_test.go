package isolation

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_native_mid', 'usr_custodian', 'manage-identities', 'org_a', NULL, NULL, `+ts+`)`)
		mustCreateKey(t, keys, project, "NATIVE_CFG", schema.Config)
		identity := mintMCPAutomation(t, db, auth, project, "native-codex", true)
		if _, err := (&service.Grants{DB: db, Auth: auth}).Create(t.Context(), service.LocalPrincipal(domain.PrincipalID("usr_orgadmin")), service.GrantSpec{Target: identity.Principal, Capability: domain.CapEdit, Scope: scopeEnv(orgA, prjA1, envA1)}); err != nil {
			t.Fatal(err)
		}
		registry := mcpserver.NewRegistry()
		if err := mcpserver.RegisterProductionTools(registry, mcpserver.ProductionServices{Admission: &service.MCPAdmission{DB: db}, Definitions: keys, Environments: &service.Environments{DB: db, Keyring: kr}, Configuration: values, Pending: revisions, Revisions: revisions}); err != nil {
			t.Fatal(err)
		}
		if err := mcpserver.RegisterWriteTools(registry, mcpserver.WriteServices{Admission: &service.MCPAdmission{DB: db}, Staging: values, Validation: values}); err != nil {
			t.Fatal(err)
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
		server.Config.Handler = handler
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
				var response struct {
					ID     int             `json:"id"`
					Result json.RawMessage `json:"result"`
					Error  json.RawMessage `json:"error"`
				}
				if json.Unmarshal(scanner.Bytes(), &response) != nil || response.ID != sequence {
					continue
				}
				if len(response.Error) > 0 {
					t.Fatalf("%s failed: %s", method, response.Error)
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
		for _, name := range mcpserver.AllToolNames() {
			if !strings.Contains(string(status), name) {
				t.Fatalf("native catalog missing %s: %s", name, status)
			}
		}
		call := func(tool string, args map[string]any) string {
			t.Helper()
			result := rpc("mcpServer/tool/call", map[string]any{"threadId": started.Thread.ID, "server": "hikyo", "tool": tool, "arguments": args})
			if strings.Contains(string(result), `"isError":true`) {
				t.Fatalf("%s returned error: %s", tool, result)
			}
			return string(result)
		}
		if result := call(mcpserver.ToolListDefinitions, map[string]any{"org_id": "org_a", "project_id": "prj_a1"}); !strings.Contains(result, "NATIVE_CFG") {
			t.Fatal("native read missing fixture definition")
		}
		args := map[string]any{"org_id": "org_a", "project_id": "prj_a1", "environment_id": "env_a1", "key_name": "NATIVE_CFG", "operation": "set", "value": "native-ok"}
		if result := call(mcpserver.ToolValidateChange, args); !strings.Contains(result, `"valid":true`) {
			t.Fatalf("native validation: %s", result)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM pending_changes WHERE owner_id = '`+string(identity.Principal)+`'`); n != 0 {
			t.Fatal("validation staged a draft")
		}
		if result := call(mcpserver.ToolStageChange, args); !strings.Contains(result, `"version_id"`) {
			t.Fatalf("native stage: %s", result)
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
		t.Log("native Codex initialized, discovered seven tools, read, validated and staged one inert draft; MCP audit verified")
	})
}
