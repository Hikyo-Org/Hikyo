package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestMCPRouteIsExactAndFeatureGated(t *testing.T) {
	marker := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-MCP-Test", "reached")
		w.WriteHeader(http.StatusAccepted)
	})
	enabled := NewPublic(nil, nil, nil, PublicOptions{MCP: marker})
	rec := httptest.NewRecorder()
	enabled.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if rec.Code != http.StatusAccepted || rec.Header().Get("X-MCP-Test") != "reached" {
		t.Fatalf("enabled /mcp = %d marker %q", rec.Code, rec.Header().Get("X-MCP-Test"))
	}

	for _, target := range []string{"/mcp/", "/mcp/anything"} {
		rec = httptest.NewRecorder()
		enabled.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, target, nil))
		if rec.Code != http.StatusNotFound || rec.Header().Get("X-MCP-Test") != "" {
			t.Fatalf("%s = %d marker %q", target, rec.Code, rec.Header().Get("X-MCP-Test"))
		}
	}

	disabled := NewPublic(nil, nil, fstest.MapFS{"index.html": {Data: []byte("SPA")}}, PublicOptions{})
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Accept", "text/html")
	disabled.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || rec.Body.String() == "SPA" {
		t.Fatalf("disabled /mcp = %d %q", rec.Code, rec.Body.String())
	}
}

func TestWorkspaceCORSNeverDecoratesMCP(t *testing.T) {
	h := workspaceCORS(func(context.Context, string) bool { return true })(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Origin", "https://workspace.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("MCP CORS = %d origin %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
}
