package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func codexRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "https://hikyo.example.com"+CodexPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	return req
}

const codexInitialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"codex-mcp-client","version":"0.155.0"}}}`

func TestCodexLifecycleAndTools(t *testing.T) {
	registry, seen := testRegistry(t, "echo")
	h := testHandler(t, registry)
	init := serve(t, h, codexRequest(codexInitialize))
	if init.Code != 200 || !strings.Contains(init.Body.String(), `"protocolVersion":"2025-06-18"`) || init.Header().Get("Mcp-Session-Id") != "" {
		t.Fatalf("initialize = %d %s", init.Code, init.Body.String())
	}
	for _, body := range []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":99}}`,
	} {
		rec := serve(t, h, codexRequest(body))
		if rec.Code != 202 || rec.Body.Len() != 0 {
			t.Fatalf("notification = %d %s", rec.Code, rec.Body.String())
		}
	}
	list := serve(t, h, codexRequest(`{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{}}`))
	if list.Code != 200 || !strings.Contains(list.Body.String(), `"name":"echo"`) {
		t.Fatalf("list = %d %s", list.Code, list.Body.String())
	}
	req := codexRequest(`{"jsonrpc":"2.0","id":"call","method":"tools/call","params":{"name":"echo","arguments":{"value":"legacy-ok"}}}`)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := serve(t, h, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"value":"legacy-ok"`) || !strings.Contains(rec.Body.String(), `"id":"call"`) {
		t.Fatalf("call = %d %s", rec.Code, rec.Body.String())
	}
	if got := <-seen; got != "test-token" {
		t.Fatalf("bearer = %q", got)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("not JSON: %v", rec.Header())
	}
}

func TestCodexRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		change     func(*http.Request)
	}{
		{"unsupported initialize", strings.ReplaceAll(codexInitialize, "2025-06-18", "2024-11-05"), 400, nil},
		{"unsupported header", codexInitialize, 400, func(r *http.Request) { r.Header.Set("Mcp-Protocol-Version", ProtocolVersion) }},
		{"duplicate header", codexInitialize, 400, func(r *http.Request) {
			r.Header.Add("Mcp-Protocol-Version", CodexProtocolVersion)
			r.Header.Add("Mcp-Protocol-Version", CodexProtocolVersion)
		}},
		{"session", codexInitialize, 400, func(r *http.Request) { r.Header.Set("Mcp-Session-Id", "untrusted") }},
		{"host", codexInitialize, 403, func(r *http.Request) { r.Host = "evil.example" }},
		{"origin", codexInitialize, 403, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }},
		{"body bound", strings.Repeat(" ", MaxRequestBytes+1), 413, nil},
		{"batch", "[" + codexInitialize + "]", 400, nil},
		{"trailing JSON", codexInitialize + `{}`, 400, nil},
		{"missing init fields", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, 400, nil},
		{"null params", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":null}`, 400, nil},
		{"bad id", `{"jsonrpc":"2.0","id":{},"method":"tools/list"}`, 400, nil},
		{"tool notification", `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"echo","arguments":{"value":"no"}}}`, 400, nil},
		{"missing bearer", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"value":"no"}}}`, 401, nil},
		{"unknown method", `{"jsonrpc":"2.0","id":1,"method":"publish"}`, 404, nil},
		{"get", codexInitialize, 405, func(r *http.Request) { r.Method = "GET" }},
		{"delete", codexInitialize, 405, func(r *http.Request) { r.Method = "DELETE" }},
		{"content type", codexInitialize, 415, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
		{"accept", codexInitialize, 406, func(r *http.Request) { r.Header.Set("Accept", "text/plain") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry, seen := testRegistry(t, "echo")
			req := codexRequest(tc.body)
			if tc.change != nil {
				tc.change(req)
			}
			rec := serve(t, testHandler(t, registry), req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
			if len(seen) != 0 {
				t.Fatal("rejected request executed tool")
			}
		})
	}
}

func TestCodexDiscoveryAdmission(t *testing.T) {
	h, err := New(Options{ExternalOrigin: "https://hikyo.example.com", Admission: fixedAdmission(false)})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{codexInitialize, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, `{"jsonrpc":"2.0","method":"notifications/initialized"}`} {
		rec := serve(t, h, codexRequest(body))
		if rec.Code != 429 || rec.Header().Get("Retry-After") != "60" {
			t.Fatalf("admission=%d %s", rec.Code, rec.Body.String())
		}
	}
}

func TestCodexToolErrorsRemainSafe(t *testing.T) {
	registry, _ := testRegistry(t, "echo")
	for _, params := range []string{`{"name":"unknown","arguments":{}}`, `{"name":"echo","arguments":{"value":42}}`} {
		req := codexRequest(`{"jsonrpc":"2.0","id":9007199254740993,"method":"tools/call","params":` + params + `}`)
		req.Header.Set("Authorization", "Bearer test-token")
		rec := serve(t, testHandler(t, registry), req)
		if (!strings.Contains(rec.Body.String(), `"error"`) && !strings.Contains(rec.Body.String(), `"isError":true`)) || !strings.Contains(rec.Body.String(), `9007199254740993`) {
			t.Fatalf("error=%d %s", rec.Code, rec.Body.String())
		}
	}
}

func TestCodexLifecycleResponseBound(t *testing.T) {
	for _, body := range []string{
		strings.Replace(strings.ReplaceAll(codexInitialize, "2025-06-18", "unsupported"), `"id":1`, `"id":"`+strings.Repeat("x", MaxStaticResponseBytes)+`"`, 1),
		strings.Replace(codexInitialize, `"id":1`, `"id":"`+strings.Repeat("x", MaxStaticResponseBytes)+`"`, 1),
		`{"jsonrpc":"2.0","id":"` + strings.Repeat("x", MaxStaticResponseBytes) + `","method":"ping"}`,
	} {
		rec := serve(t, testHandler(t, nil), codexRequest(body))
		if rec.Code != http.StatusInternalServerError || rec.Body.Len() > MaxStaticResponseBytes {
			t.Fatalf("unbounded lifecycle response: %d, %d bytes", rec.Code, rec.Body.Len())
		}
	}
}
