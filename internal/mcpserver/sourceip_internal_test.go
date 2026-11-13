package mcpserver

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The MCP transport reads the source the same way the REST API does: separate
// X-Forwarded-For field lines are one list walked from the right.
func TestSourceIPJoinsForwardedFieldLines(t *testing.T) {
	_, proxy, err := net.ParseCIDR("192.0.2.0/24")
	if err != nil {
		t.Fatal(err)
	}
	h := &handler{trustedProxies: []*net.IPNet{proxy}}
	for _, tc := range []struct {
		name   string
		peer   string
		lines  []string
		source string
	}{
		{"untrusted peer ignores the header", "198.51.100.7:1", []string{"203.0.113.9"}, "198.51.100.7"},
		{"single line", "192.0.2.1:1", []string{"203.0.113.9, 192.0.2.2"}, "203.0.113.9"},
		{"forged first line", "192.0.2.1:1", []string{"203.0.113.50", "203.0.113.9, 192.0.2.2"}, "203.0.113.9"},
		{"proxy appends a line", "192.0.2.1:1", []string{"203.0.113.50", "203.0.113.9"}, "203.0.113.9"},
		{"malformed entry falls back to the peer", "192.0.2.1:1", []string{"203.0.113.9", "not-an-ip"}, "192.0.2.1"},
		{"no header", "192.0.2.1:1", nil, "192.0.2.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
			r.RemoteAddr = tc.peer
			r.Header["X-Forwarded-For"] = tc.lines
			if got := h.sourceIP(r); got != tc.source {
				t.Fatalf("source = %q, want %q", got, tc.source)
			}
		})
	}
}
