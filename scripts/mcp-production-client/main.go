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
	if err != nil || tools == nil {
		return fmt.Errorf("tool catalog unavailable")
	}
	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if err := checkCatalog(names); err != nil {
		return err
	}
	// The write surface is advertised only when the operator enabled it; this
	// proof exercises every read tool and never stages into production, so the
	// reported count is the number of read tools that actually answered.
	called := 0
	for _, name := range mcpserver.ProductionToolNames() {
		args := map[string]any{"org_id": "org_a", "project_id": "prj_a1", "page_size": 20}
		if name != "hikyo_list_definitions" && name != "hikyo_list_environments" {
			args["environment_id"] = "env_a1"
		}
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || result == nil || result.IsError {
			return fmt.Errorf("production tool %s refused", name)
		}
		b, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "CANARY-PLAINTEXT-9Z-do-not-disclose") {
			return fmt.Errorf("secret canary disclosed")
		}
		called++
	}
	if called != len(mcpserver.ProductionToolNames()) {
		return fmt.Errorf("called %d read tools, want %d", called, len(mcpserver.ProductionToolNames()))
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"client": "official-go-sdk", "version": "v1.7.0", "protocol": session.InitializeResult().ProtocolVersion, "production_tools_called": called, "secret_canary_absent": true, "passed": true})
}

// checkCatalog requires the advertised catalog to be EXACTLY the closed read
// set, or exactly the closed read-plus-write set: no missing read tool, no
// duplicate, no unknown name. A catalog of the right size is not enough; a
// five-entry mix of reads and writes would otherwise pass on fewer calls.
func checkCatalog(names []string) error {
	if !sameSet(names, mcpserver.ProductionToolNames()) && !sameSet(names, mcpserver.AllToolNames()) {
		return fmt.Errorf("closed read or read-plus-write tool catalog required")
	}
	return nil
}

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	pending := make(map[string]bool, len(want))
	for _, name := range want {
		pending[name] = true
	}
	for _, name := range got {
		if !pending[name] {
			return false
		}
		delete(pending, name)
	}
	return len(pending) == 0
}
