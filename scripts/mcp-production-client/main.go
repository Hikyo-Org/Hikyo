// mcp-production-client probes actual production tools using the pinned official
// Go SDK. Credentials remain in the process environment; output is redacted.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header = r.Header.Clone()
	if r.Header.Get("Mcp-Method") == "tools/call" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "official Go MCP production probe failed:", err)
		os.Exit(1)
	}
}

func run() error {
	endpoint, token := os.Getenv("HIKYO_MCP_URL"), os.Getenv("HIKYO_MCP_TOKEN")
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		u.Path != mcpserver.Path || u.RawQuery != "" || u.Fragment != "" || token == "" {
		return fmt.Errorf("exact HTTPS /mcp URL and runtime token required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "hikyo-production-proof", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint,
		HTTPClient: &http.Client{Transport: bearerTransport{token}, Timeout: 20 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		DisableStandaloneSSE: true}, nil)
	if err != nil {
		return err
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || tools == nil || len(tools.Tools) != 5 {
		return fmt.Errorf("five-tool catalog required")
	}
	expected := make(map[string]bool)
	for _, name := range mcpserver.ProductionToolNames() {
		expected[name] = true
	}
	for _, tool := range tools.Tools {
		if !expected[tool.Name] {
			return fmt.Errorf("unexpected or duplicate production tool")
		}
		delete(expected, tool.Name)
		args := map[string]any{"org_id": "org_a", "project_id": "prj_a1", "page_size": 20}
		if tool.Name != "hikyo_list_definitions" && tool.Name != "hikyo_list_environments" {
			args["environment_id"] = "env_a1"
		}
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool.Name, Arguments: args})
		if err != nil || result == nil || result.IsError {
			return fmt.Errorf("production tool %s refused", tool.Name)
		}
		b, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "CANARY-PLAINTEXT-9Z-do-not-disclose") {
			return fmt.Errorf("secret canary disclosed")
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"client": "official-go-sdk", "version": "v1.7.0", "protocol": session.InitializeResult().ProtocolVersion, "production_tools_called": 5, "secret_canary_absent": true, "passed": true})
}
