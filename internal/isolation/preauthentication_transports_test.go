package isolation

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

func TestPreauthenticationDedicatedTransports(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		t.Run("scim", func(t *testing.T) {
			limiter := preauthLimiter(t)
			for range admission.MetaPerIPPerMinute - 1 {
				if err := limiter.AdmitDiscovery("192.0.2.83"); err != nil {
					t.Fatal(err)
				}
			}
			handler := server.New(&server.API{SCIMWire: &service.SCIM{DB: db}, Admission: limiter}, nil)
			token, _, err := crypto.NewArtifact(crypto.ArtifactSCIM)
			if err != nil {
				t.Fatal(err)
			}
			var queries atomic.Int64
			restore := authn.SetQueryObserver(func(string) { queries.Add(1) })
			defer restore()
			const path = "/orgs/org_0198f000-0000-7000-8000-00000000000a/scim/v2/scb_0198f000-0000-7000-8000-00000000000b/Users"
			rec := preauthRequest(handler, token, crypto.ArtifactSCIM, "192.0.2.83", http.MethodGet, path)
			if rec.Code != http.StatusUnauthorized || queries.Load() == 0 || !strings.Contains(rec.Header().Get("Content-Type"), "application/scim+json") {
				t.Fatalf("SCIM invalid credential = %d %s, %d queries", rec.Code, rec.Body.String(), queries.Load())
			}
			queries.Store(0)
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				// The POST has no resource body and takes the pre-handler error
				// path, which must rank admission before its syntax response too.
				rec := preauthRequest(handler, token, crypto.ArtifactSCIM, "192.0.2.83", method, path)
				if rec.Code != http.StatusTooManyRequests || queries.Load() != 0 || rec.Header().Get("Retry-After") == "" {
					t.Fatalf("SCIM %s overflow = %d %s, %d queries", method, rec.Code, rec.Body.String(), queries.Load())
				}
			}
		})
		t.Run("paired_connection", func(t *testing.T) {
			limiter := preauthLimiter(t)
			handler := server.New(&server.API{Workspace: &service.Workspace{DB: db}, Admission: limiter}, nil)
			token, _, err := crypto.NewArtifact(crypto.ArtifactInstanceConn)
			if err != nil {
				t.Fatal(err)
			}
			var queries atomic.Int64
			restore := authn.SetQueryObserver(func(string) { queries.Add(1) })
			defer restore()
			for i := 0; i <= admission.MetaPerIPPerMinute; i++ {
				queries.Store(0)
				req := httptest.NewRequest(http.MethodGet, api.PathPrefix+"/instance/directory", nil)
				req.RemoteAddr = "192.0.2.83:1234"
				req.Header.Set("Authorization", "DPoP "+token)
				req.Header.Set("DPoP", "grammatical-header-presence-only")
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if i < admission.MetaPerIPPerMinute {
					if rec.Code != http.StatusUnauthorized || queries.Load() == 0 {
						t.Fatalf("paired attempt %d = %d %s, %d queries", i, rec.Code, rec.Body.String(), queries.Load())
					}
				} else if rec.Code != http.StatusTooManyRequests || queries.Load() != 0 {
					t.Fatalf("paired overflow = %d %s, %d queries", rec.Code, rec.Body.String(), queries.Load())
				}
			}
		})
		t.Run("mcp", func(t *testing.T) {
			kr := probeKeyring(t, db)
			sealer, err := kr.MCPCursorSealer()
			if err != nil {
				t.Fatal(err)
			}
			registry := mcpserver.NewRegistry()
			if err := mcpserver.RegisterProductionTools(registry, mcpserver.ProductionServices{
				Admission: &service.MCPAdmission{DB: db}, Definitions: &service.Keys{DB: db},
				Environments: &service.Environments{DB: db}, Configuration: &service.Values{DB: db},
				Pending: &service.Revisions{DB: db}, Revisions: &service.Revisions{DB: db},
			}); err != nil {
				t.Fatal(err)
			}
			limiter := preauthLimiter(t)
			handler, err := mcpserver.New(mcpserver.Options{Registry: registry, ExternalOrigin: "https://hikyo.example.com", CursorSealer: sealer, Admission: limiter})
			if err != nil {
				t.Fatal(err)
			}
			token, _, err := crypto.NewArtifact(crypto.ArtifactAutomation)
			if err != nil {
				t.Fatal(err)
			}
			var queries atomic.Int64
			restore := authn.SetQueryObserver(func(string) { queries.Add(1) })
			defer restore()
			for i := 0; i <= admission.MetaPerIPPerMinute; i++ {
				queries.Store(0)
				rec := mcpCall(t, mcpProfile(handler, i%2 == 1), token, mcpserver.ToolListEnvironments, `{"org_id":"org_a","project_id":"prj_a1"}`)
				if i < admission.MetaPerIPPerMinute {
					if rec.Code != http.StatusUnauthorized || queries.Load() != 3 {
						t.Fatalf("MCP attempt %d = %d %s, %d queries", i, rec.Code, rec.Body.String(), queries.Load())
					}
				} else if rec.Code != http.StatusTooManyRequests || queries.Load() != 0 || rec.Header().Get("Retry-After") != "60" {
					t.Fatalf("MCP overflow = %d %s, %d queries", rec.Code, rec.Body.String(), queries.Load())
				}
			}
			queries.Store(0)
			rec := mcpCall(t, mcpProfile(handler, true), token, mcpserver.ToolListEnvironments, `{"org_id":"org_a","project_id":"prj_a1"}`)
			if rec.Code != http.StatusTooManyRequests || queries.Load() != 0 {
				t.Fatalf("compatibility MCP overflow = %d %s, %d queries", rec.Code, rec.Body.String(), queries.Load())
			}
		})
	})
}

func TestPreauthenticationTrustedProxyAndDocument(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		_, proxy, err := net.ParseCIDR("203.0.113.0/24")
		if err != nil {
			t.Fatal(err)
		}
		limiter := preauthLimiter(t)
		handler := server.New(&server.API{Auth: &service.Auth{DB: db}, Admission: limiter, TrustedProxies: []*net.IPNet{proxy}}, nil)
		token, _, err := crypto.NewArtifact(crypto.ArtifactCLISession)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i <= admission.MetaPerIPPerMinute; i++ {
			req := httptest.NewRequest(http.MethodGet, api.PathPrefix+"/auth/whoami", nil)
			req.RemoteAddr = "203.0.113.7:1234"
			req.Header.Set("Authorization", "Bearer "+token)
			// The attacker-controlled leftmost hop changes. The rightmost
			// untrusted hop remains the only attributable client.
			req.Header.Set("X-Forwarded-For", "198.51.100.1, 192.0.2.83, 203.0.113.8")
			if i%2 == 1 {
				req.Header.Set("X-Forwarded-For", "198.51.100.2, 192.0.2.83, 203.0.113.8")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			want := http.StatusUnauthorized
			if i == admission.MetaPerIPPerMinute {
				want = http.StatusTooManyRequests
			}
			if rec.Code != want {
				t.Fatalf("trusted proxy attempt %d = %d %s", i, rec.Code, rec.Body.String())
			}
		}
		// The SPA's per-document CSP lookup is outside API middleware. It
		// shares admission and falls back to the baseline policy on refusal.
		browserToken, _, err := crypto.NewArtifact(crypto.ArtifactBrowserSession)
		if err != nil {
			t.Fatal(err)
		}
		doc := server.New(&server.API{Remotes: &service.Remotes{DB: db}, Admission: limiter}, fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>fixture</title>")}})
		var queries atomic.Int64
		restore := authn.SetQueryObserver(func(string) { queries.Add(1) })
		defer restore()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept", "text/html")
		req.RemoteAddr = "192.0.2.83:1234"
		req.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: browserToken})
		rec := httptest.NewRecorder()
		doc.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || queries.Load() != 0 {
			t.Fatalf("exhausted document policy read = %d %s, %d queries", rec.Code, rec.Body.String(), queries.Load())
		}
	})
}
