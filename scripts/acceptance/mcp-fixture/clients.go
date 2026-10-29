//go:build darwin || linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (b bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header = req.Header.Clone()
	if r.Header.Get("Mcp-Method") == "tools/call" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return b.base.RoundTrip(r)
}

func (p *probe) namedClients(ctx context.Context, dir string) error {
	client := mcp.NewClient(&mcp.Implementation{Name: "hikyo-651-official", Version: "1"}, nil)
	httpClient := *p.client
	httpClient.Transport = bearerTransport{base: p.client.Transport, token: p.fixture.Credentials["clients"].Token}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: p.target.URL, HTTPClient: &httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return errors.New("official SDK connection failed")
	}
	defer session.Close()
	if session.InitializeResult().ProtocolVersion != mcpserver.ProtocolVersion {
		return errors.New("official SDK negotiated wrong protocol")
	}
	for _, name := range mcpserver.ProductionToolNames() {
		args := map[string]any{"org_id": p.fixture.Org, "project_id": p.fixture.Project}
		if name != mcpserver.ToolListDefinitions && name != mcpserver.ToolListEnvironments {
			args["environment_id"] = p.fixture.Environment
		}
		r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || r == nil || r.IsError {
			return errors.New("official SDK authenticated read failed")
		}
		b, err := json.Marshal(r)
		if err != nil || !p.safe(b) {
			return errors.New("official SDK output disclosed protected material")
		}
		if name == mcpserver.ToolInspectConfiguration && !bytes.Contains(b, []byte("synthetic-config-value")) {
			return errors.New("official SDK did not read published synthetic configuration")
		}
	}
	p.receipt.Checks["official_go_v1.8.0_requested_negotiated_2026-07-28_five_tools"] = "PASS"
	if err := p.inspector(ctx, dir); err != nil {
		return err
	}
	return p.codex(ctx, dir)
}

func (p *probe) safe(b []byte) bool {
	for _, s := range p.fixture.Canaries {
		if s != "" && bytes.Contains(b, []byte(s)) {
			return false
		}
	}
	for _, c := range p.fixture.Credentials {
		if c.Token != "" && bytes.Contains(b, []byte(c.Token)) {
			return false
		}
	}
	return true
}

func clientEnvironment(dir, ca string) []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "NODE_EXTRA_CA_CERTS=" + ca, "SSL_CERT_FILE=" + ca, "CODEX_CA_CERTIFICATE=" + ca, "NO_COLOR=1"}
}

func (p *probe) inspector(ctx context.Context, dir string) error {
	entry := os.Getenv("MCP651_INSPECTOR_ENTRY")
	wrapper := os.Getenv("MCP651_INSPECTOR_WRAPPER")
	if entry == "" || wrapper == "" {
		return errors.New("pinned Inspector entry and protected header wrapper required")
	}
	var pkg struct {
		Version string `json:"version"`
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(entry), "..", "..", "..", "package.json"))
	if err != nil || json.Unmarshal(b, &pkg) != nil || pkg.Version != "2.11.0" {
		return errors.New("Inspector must be exactly 2.11.0")
	}
	for _, method := range []string{"initialize", "tools/call"} {
		node := os.Getenv("MCP651_NODE")
		if !filepath.IsAbs(node) {
			return errors.New("absolute actual Node executable required, not a version-manager shim")
		}
		cmd := exec.CommandContext(ctx, node, wrapper)
		cmd.Env = append(clientEnvironment(dir, p.target.CAFile), "HIKYO_INSPECTOR_ENTRY="+entry, "HIKYO_MCP_URL="+p.target.URL, "HIKYO_MCP_TOKEN="+p.fixture.Credentials["clients"].Token, "HIKYO_MCP_ORG_ID="+string(p.fixture.Org), "HIKYO_MCP_PROJECT_ID="+string(p.fixture.Project), "HIKYO_MCP_ENV_ID="+string(p.fixture.Environment), "HIKYO_INSPECTOR_METHOD="+method)
		out, err := cmd.Output()
		if err != nil || !p.safe(out) {
			return errors.New("Inspector authenticated call failed; output withheld")
		}
		if method == "initialize" {
			var init struct {
				Protocol string `json:"protocolVersion"`
			}
			if json.Unmarshal(out, &init) != nil || init.Protocol != mcpserver.ProtocolVersion {
				return errors.New("Inspector negotiated protocol not observed")
			}
		} else {
			var r struct {
				IsError    bool            `json:"isError"`
				Structured json.RawMessage `json:"structuredContent"`
			}
			if json.Unmarshal(out, &r) != nil || r.IsError || !bytes.Contains(r.Structured, []byte("synthetic-config-value")) {
				return errors.New("Inspector authenticated configuration read missing")
			}
		}
	}
	p.receipt.Checks["inspector_2.11.0_modern_requested_negotiated_2026-07-28_authenticated"] = "PASS"
	return nil
}

func (p *probe) codex(ctx context.Context, dir string) error {
	binary := os.Getenv("MCP651_CODEX")
	if binary == "" {
		return errors.New("isolated Codex 0.160.1 executable required")
	}
	version, err := exec.CommandContext(ctx, binary, "--version").Output()
	if err != nil || strings.TrimSpace(string(version)) != "codex-cli 0.160.1" {
		return errors.New("Codex must be exactly 0.160.1")
	}
	home := filepath.Join(dir, "codex")
	if err := os.Mkdir(home, 0700); err != nil {
		return err
	}
	p.codexInvocation = randomHex()
	p.receipt.Identities["codex_invocation_sha256"] = invocationHash(p.codexInvocation)
	config, err := codexConfiguration(p.target.URL+"/codex", p.codexInvocation)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), config, 0600); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "app-server")
	cmd.Dir = home
	cmd.Env = append(clientEnvironment(home, p.target.CAFile), "CODEX_HOME="+home, "HIKYO_NATIVE_TEST_TOKEN="+p.fixture.Credentials["clients"].Token, "HTTPS_PROXY=http://127.0.0.1:9", "HTTP_PROXY=http://127.0.0.1:9", codexNoProxy(p.target.URL))
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(dir, "codex.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		return errors.New("native Codex launch failed")
	}
	defer func() { input.Close(); cancel(); cmd.Wait() }()
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	sequence := 0
	rpc := func(method string, params any) (json.RawMessage, error) {
		sequence++
		if json.NewEncoder(input).Encode(map[string]any{"jsonrpc": "2.0", "id": sequence, "method": method, "params": params}) != nil {
			return nil, errors.New("native client input failed")
		}
		for scanner.Scan() {
			if !p.safe(scanner.Bytes()) {
				return nil, errors.New("native client disclosure; frame withheld")
			}
			var r struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if json.Unmarshal(scanner.Bytes(), &r) != nil || r.ID != sequence {
				continue
			}
			if len(r.Error) > 0 {
				return nil, errors.New("native client RPC refused; frame withheld")
			}
			return r.Result, nil
		}
		return nil, errors.New("native client ended without result at " + method)
	}
	if _, err := rpc("initialize", map[string]any{"clientInfo": map[string]string{"name": "hikyo-651-proof", "version": "1"}, "capabilities": map[string]bool{"experimentalApi": true}}); err != nil {
		return err
	}
	if err := json.NewEncoder(input).Encode(map[string]any{"jsonrpc": "2.0", "method": "initialized"}); err != nil {
		return errors.New("native initialization failed")
	}
	thread, err := rpc("thread/start", map[string]any{"cwd": home, "ephemeral": true, "approvalPolicy": "never", "sandbox": "read-only"})
	if err != nil {
		return err
	}
	var started struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if json.Unmarshal(thread, &started) != nil || started.Thread.ID == "" {
		return errors.New("native thread identity missing")
	}
	status, err := rpc("mcpServerStatus/list", map[string]any{"threadId": started.Thread.ID})
	if err != nil {
		return err
	}
	var catalog struct {
		Data []struct {
			Name       string  `json:"name"`
			ToolsError *string `json:"toolsError"`
			Tools      map[string]struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"data"`
	}
	if json.Unmarshal(status, &catalog) != nil || len(catalog.Data) != 1 || catalog.Data[0].Name != "hikyo" || catalog.Data[0].ToolsError != nil || len(catalog.Data[0].Tools) != 5 {
		return errors.New("native closed catalog missing")
	}
	// The pinned client's Tool.name is the original MCP name, independently
	// from its client-owned map keys. The real native rehearsal checks this.
	names := map[string]bool{}
	for _, tool := range catalog.Data[0].Tools {
		names[tool.Name] = true
	}
	for _, name := range mcpserver.ProductionToolNames() {
		if !names[name] {
			return errors.New("native catalog names differ from production read profile")
		}
	}
	called, err := rpc("mcpServer/tool/call", map[string]any{"threadId": started.Thread.ID, "server": "hikyo", "tool": mcpserver.ToolInspectConfiguration, "arguments": map[string]any{"org_id": p.fixture.Org, "project_id": p.fixture.Project, "environment_id": p.fixture.Environment}})
	if err != nil {
		return err
	}
	var result struct {
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(called, &result) != nil || result.IsError || !bytes.Contains(called, []byte("synthetic-config-value")) {
		return errors.New("native authenticated configuration read missing")
	}
	p.receipt.Checks["codex_0.160.1_authenticated_native_no_model_turn"] = "PASS; protocol wire receipt required separately"
	return nil
}

func codexConfiguration(endpoint, invocation string) ([]byte, error) {
	var out bytes.Buffer
	err := toml.NewEncoder(&out).Encode(map[string]any{
		"model_provider":  "fixture-offline",
		"model_providers": map[string]any{"fixture-offline": map[string]any{"name": "No provider calls", "base_url": "http://127.0.0.1:9", "wire_api": "responses", "requires_openai_auth": false}},
		"mcp_servers":     map[string]any{"hikyo": map[string]any{"url": endpoint, "bearer_token_env_var": "HIKYO_NATIVE_TEST_TOKEN", "http_headers": map[string]string{probeHeader: invocation}}},
	})
	return out.Bytes(), err
}

func codexNoProxy(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return "NO_PROXY=localhost,127.0.0.1"
	}
	return "NO_PROXY=localhost,127.0.0.1," + u.Hostname()
}
