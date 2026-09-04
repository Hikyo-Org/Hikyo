package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestOfficialGoClientInteroperability pins the exact production SDK client
// against Hikyo's controlled-bearer, stateless 2026-07-28 profile. The token is
// injected by the HTTP transport, never embedded in client configuration.
func TestOfficialGoClientInteroperability(t *testing.T) {
	registry, seen := testRegistry(t, "echo")
	server := httptest.NewUnstartedServer(nil)
	externalOrigin := "http://" + server.Listener.Addr().String()
	handler, err := New(Options{
		Registry: registry, ExternalOrigin: externalOrigin,
		Version: "go-client-interop", CursorSealer: testCursorSealer,
	})
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = handler
	server.Start()
	t.Cleanup(server.Close)

	httpClient := server.Client()
	httpClient.Transport = bearerRoundTripper{
		base: httpClient.Transport, token: "runtime-only-token",
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name: "hikyo-go-client-smoke", Version: "test",
	}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: externalOrigin + Path, HTTPClient: httpClient,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect official Go client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	if got := session.InitializeResult().ProtocolVersion; got != ProtocolVersion {
		t.Fatalf("negotiated protocol = %q, want %q", got, ProtocolVersion)
	}
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Name != "echo" {
		t.Fatalf("tools = %v, want [echo]", listed.Tools)
	}
	called, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "echo", Arguments: map[string]any{"value": "client-ok"},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if called.IsError {
		t.Fatalf("call returned tool error: %v", called.Content)
	}
	structured, ok := called.StructuredContent.(map[string]any)
	if !ok || structured["value"] != "client-ok" {
		t.Fatalf("structured content = %#v", called.StructuredContent)
	}
	if got := <-seen; got != "runtime-only-token" {
		t.Fatalf("operation bearer = %q", got)
	}
}

type bearerRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (t bearerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header = request.Header.Clone()
	request.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(request)
}
