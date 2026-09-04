package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
)

func TestRunProvesPublicProfileWithoutPersistingCredentials(t *testing.T) {
	var rotatingCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != mcpserver.Path || r.Header.Get("Mcp-Protocol-Version") != mcpserver.ProtocolVersion {
			http.Error(w, "bad profile", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		method := r.Header.Get("Mcp-Method")
		var result any
		switch method {
		case "server/discover":
			result = map[string]any{
				"supportedVersions": []string{mcpserver.ProtocolVersion},
				"capabilities":      map[string]any{"tools": map[string]any{}},
			}
		case "tools/list":
			tools := make([]map[string]string, 0, len(mcpserver.ProductionToolNames()))
			for _, name := range mcpserver.ProductionToolNames() {
				tools = append(tools, map[string]string{"name": name})
			}
			result = map[string]any{"tools": tools}
		case "tools/call":
			if r.Header.Get("Authorization") == "Bearer live-runtime-token" {
				result = successfulDefinitionsResult()
			} else if r.Header.Get("Authorization") == "Bearer rot-token" && rotatingCalls.Add(1) == 1 {
				result = successfulDefinitionsResult()
			} else {
				result = map[string]any{
					"isError": true,
					"content": []map[string]string{{"type": "text", "text": mcpserver.SafeOperationError}},
				}
			}
		default:
			http.Error(w, "unknown method", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}))
	t.Cleanup(server.Close)
	t.Setenv("TEST_MCP_LIVE", "live-runtime-token")
	t.Setenv("TEST_MCP_ROTATING", "rot-token")

	err := run(options{
		endpoint: server.URL + mcpserver.Path,
		tokenEnv: "TEST_MCP_LIVE", rotatingTokenEnv: "TEST_MCP_ROTATING",
		orgID: "org_test", projectID: "prj_test",
		revocationTimeout: time.Second,
	}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
}

func successfulDefinitionsResult() map[string]any {
	return map[string]any{
		"content": []map[string]string{{"type": "text", "text": "Hikyo returned structured data."}},
		"structuredContent": map[string]any{
			"org_id": "org_test", "project_id": "prj_test", "schema_revision": 1, "definitions": []any{},
		},
	}
}

func populatedDefinitionsResult() string {
	return `{"content":[{"type":"text","text":"Hikyo returned structured data."}],"structuredContent":{"org_id":"org_test","project_id":"prj_test","schema_revision":1,"definitions":[{"name":"DATABASE_URL","description":"Database endpoint","classification":"secret","deprecated":false,"declaration":{"rule":{"type":"string"}},"presence":{"required_in":{"mode":"none"},"forbidden_in":{"mode":"none"}}}]}}`
}

func TestRunRefusesRedirectBeforeBearerCanReachDowngradedTarget(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("redirect target received Authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(target.Close)

	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mcp-Method") == "tools/call" {
			http.Redirect(w, r, target.URL+mcpserver.Path, http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Mcp-Method") == "server/discover" {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{
				"supportedVersions": []string{mcpserver.ProtocolVersion}, "capabilities": map[string]any{"tools": map[string]any{}},
			}})
			return
		}
		tools := make([]map[string]string, 0, len(mcpserver.ProductionToolNames()))
		for _, name := range mcpserver.ProductionToolNames() {
			tools = append(tools, map[string]string{"name": name})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"tools": tools}})
	}))
	t.Cleanup(source.Close)
	t.Setenv("TEST_MCP_LIVE", "live-runtime-token")
	t.Setenv("TEST_MCP_ROTATING", "rot-token")

	err := run(options{
		endpoint: source.URL + mcpserver.Path,
		tokenEnv: "TEST_MCP_LIVE", rotatingTokenEnv: "TEST_MCP_ROTATING",
		orgID: "org_test", projectID: "prj_test", revocationTimeout: time.Second,
	}, source.Client())
	if err == nil || !strings.Contains(err.Error(), "HTTP status 307") {
		t.Fatalf("run() error = %v, want redirect refusal", err)
	}
	if targetCalls.Load() != 0 {
		t.Fatalf("redirect target received %d requests", targetCalls.Load())
	}
}

func TestDecodeRPCResponseRejectsNonCanonicalEnvelopes(t *testing.T) {
	for _, encoded := range []string{
		`{"jsonrpc":"1.0","id":1,"result":{}}`,
		`{"jsonrpc":"2.0","id":2,"result":{}}`,
		`{"jsonrpc":"2.0","id":1,"result":{},"tenant":"fact"}`,
		`{"jsonrpc":"2.0","id":1,"result":{},"error":{"code":-1,"message":"x"}}`,
		`{"jsonrpc":"2.0","id":1,"error":{"code":-1,"message":"x","data":{"tenant":"fact"}}}`,
	} {
		if _, err := decodeRPCResponse([]byte(encoded)); err == nil {
			t.Fatalf("accepted non-canonical envelope %s", encoded)
		}
	}
}

func TestSuccessfulToolResultRequiresTypedExactShape(t *testing.T) {
	valid, err := json.Marshal(successfulDefinitionsResult())
	if err != nil {
		t.Fatal(err)
	}
	if !successfulToolResult(valid, "org_test", "prj_test") {
		t.Fatal("valid definitions result was rejected")
	}
	if !successfulToolResult(json.RawMessage(populatedDefinitionsResult()), "org_test", "prj_test") {
		t.Fatal("valid populated definitions result was rejected")
	}
	for _, result := range []string{
		`{"content":[{"type":"text","text":"Hikyo returned structured data."}],"structuredContent":null}`,
		`{"content":[{"type":"text","text":"Hikyo returned structured data."}],"structuredContent":{"arbitrary":true}}`,
		`{"content":[{"type":"text","text":"Hikyo returned structured data."}],"structuredContent":{"org_id":"other","project_id":"prj_test","schema_revision":1,"definitions":[]}}`,
		`{"content":[{"type":"text","text":"Hikyo returned structured data.","tenant":"fact"}],"structuredContent":{"org_id":"org_test","project_id":"prj_test","schema_revision":1,"definitions":[]}}`,
		`{"content":[{"type":"text","text":"Hikyo returned structured data."}],"structuredContent":{"org_id":"org_test","project_id":"prj_test","schema_revision":1,"definitions":[null]}}`,
		`{"content":[{"type":"text","text":"Hikyo returned structured data."}],"structuredContent":{"org_id":"org_test","project_id":"prj_test","schema_revision":1,"definitions":["secret"]}}`,
		`{"content":[{"type":"text","text":"Hikyo returned structured data."}],"structuredContent":{"org_id":"org_test","project_id":"prj_test","schema_revision":1,"definitions":[{"name":"DATABASE_URL","description":"Database endpoint","classification":"secret","declaration":{"rule":{"type":"string"}},"presence":{"required_in":{"mode":"none"},"forbidden_in":{"mode":"none"}}}]}}`,
		`{"content":[{"type":"text","text":"Hikyo returned structured data."}],"structuredContent":{"org_id":"org_test","project_id":"prj_test","schema_revision":1,"definitions":[{"name":"DATABASE_URL","description":"Database endpoint","classification":"secret","deprecated":false,"value":"tenant-secret","declaration":{"rule":{"type":"string"}},"presence":{"required_in":{"mode":"none"},"forbidden_in":{"mode":"none"}}}]}}`,
		`{"content":[{"type":"text","text":"Hikyo returned structured data."}],"structuredContent":{"org_id":"org_test","project_id":"prj_test","schema_revision":1,"definitions":[{"name":"DATABASE_URL","description":"Database endpoint","classification":"secret","deprecated":false,"declaration":{"rule":{"type":"string","tenant":"fact"}},"presence":{"required_in":{"mode":"none"},"forbidden_in":{"mode":"none"}}}]}}`,
	} {
		if successfulToolResult(json.RawMessage(result), "org_test", "prj_test") {
			t.Fatalf("accepted malformed tool result %s", result)
		}
	}
}

func TestValidateDiscoveryPinsProtocolAndCapabilities(t *testing.T) {
	tests := []struct {
		name   string
		result string
	}{
		{"extra protocol", `{"supportedVersions":["2026-07-28","2025-11-25"],"capabilities":{"tools":{}}}`},
		{"extra capability", `{"supportedVersions":["2026-07-28"],"capabilities":{"tools":{},"prompts":{}}}`},
		{"tool list changes", `{"supportedVersions":["2026-07-28"],"capabilities":{"tools":{"listChanged":true}}}`},
		{"extra result field", `{"supportedVersions":["2026-07-28"],"capabilities":{"tools":{}},"tenant":"fact"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if validateDiscovery(json.RawMessage(test.result)) == nil {
				t.Fatal("unapproved discovery profile accepted")
			}
		})
	}
}

func TestValidateCatalogRequiresExactProductionSet(t *testing.T) {
	if validateCatalog(json.RawMessage(`{"tools":[{"name":"hikyo_list_definitions"}]}`)) == nil {
		t.Fatal("partial catalog accepted")
	}
	all := mcpserver.ProductionToolNames()
	tools := make([]map[string]string, 0, len(all)+1)
	for _, name := range all {
		tools = append(tools, map[string]string{"name": name})
	}
	tools = append(tools, map[string]string{"name": "hikyo_unapproved"})
	result, err := json.Marshal(map[string]any{"tools": tools})
	if err != nil {
		t.Fatal(err)
	}
	if validateCatalog(result) == nil {
		t.Fatal("widened catalog accepted")
	}
	result, err = json.Marshal(map[string]any{"tools": tools[:len(tools)-1], "tenant": "fact"})
	if err != nil {
		t.Fatal(err)
	}
	if validateCatalog(result) == nil {
		t.Fatal("catalog with an extra result field accepted")
	}
}

func TestValidateOptionsRefusesPlainHTTP(t *testing.T) {
	err := validateOptions(options{
		endpoint: "http://127.0.0.1:8080/mcp",
		tokenEnv: "LIVE", rotatingTokenEnv: "ROTATING",
		orgID: "org_test", projectID: "prj_test",
		revocationTimeout: time.Second,
	})
	if err == nil {
		t.Fatal("plain HTTP public endpoint accepted")
	}
}
