package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

type disclosedValues struct {
	stubValues
	err error
}

func (s disclosedValues) Get(_ context.Context, _ service.Actor, _ domain.Scope, key string, reveal bool) (service.ValueCell, error) {
	return service.ValueCell{KeyID: testKeyID, Name: key, Classification: "secret", Set: true, Revealed: reveal, Value: "disclosed-secret", UpdatedBy: testPrincipalID, UpdatedAt: time.Unix(1_800_000_000, 0).UTC()}, s.err
}

func assertNoStore(t *testing.T, h http.Header) {
	t.Helper()
	if got := h.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := h.Get("Pragma"); got != "no-cache" {
		t.Errorf("Pragma = %q, want no-cache", got)
	}
	vary := strings.Join(h.Values("Vary"), ",")
	for _, field := range []string{"Cookie", "Authorization", "Origin"} {
		found := false
		for part := range strings.SplitSeq(vary, ",") {
			if strings.EqualFold(strings.TrimSpace(part), field) {
				found = true
			}
		}
		if !found {
			t.Errorf("Vary = %q, missing %s", vary, field)
		}
	}
}

// Exercise the public router and rendered responses over a socket, including
// successful disclosure/mint bodies and the service and middleware refusals.
func TestSensitiveResponsesNoStore(t *testing.T) {
	for _, cookie := range []bool{false, true} {
		transport := "header"
		if cookie {
			transport = "cookie"
		}
		for _, outcome := range []struct {
			name   string
			err    error
			status int
		}{
			{"success", nil, http.StatusOK},
			{"unauthenticated", domain.ErrUnauthenticated, http.StatusUnauthorized},
			{"not found", domain.ErrNotFound, http.StatusNotFound},
		} {
			refused, refusal := outcome.err != nil, outcome.err
			t.Run(transport+"/"+outcome.name, func(t *testing.T) {
				auth := browserLoginAuth()
				auth.reissue = func() (service.LoginResult, error) {
					artifact := service.ArtifactCLI
					if cookie {
						artifact = service.ArtifactBrowser
					}
					return service.LoginResult{Principal: liveIdentity.Principal, DisplayName: "Admin", SessionID: liveIdentity.SessionID, SessionToken: "hik_1_cli_rotated", CSRFToken: "hik_1_cs_rotated", Artifact: artifact, Assurance: liveIdentity.Assurance, CreatedAt: liveIdentity.CreatedAt, IdleExpires: liveIdentity.IdleExpiresAt, AbsExpires: liveIdentity.AbsoluteExpiresAt}, refusal
				}
				srv := httptest.NewServer(server.New(&server.API{Auth: auth, Delivery: stubDelivery{cursor: "v1:cursor", err: refusal}, Values: disclosedValues{err: refusal}}, nil))
				defer srv.Close()
				for _, operation := range []struct {
					name, method, path, contains string
					body                         any
				}{
					{"delivery", http.MethodGet, deliveryPath, "postgres://render-test", nil},
					{"reveal", http.MethodPost, api.PathPrefix + "/orgs/" + testOrgID + "/projects/" + testProjectID + "/environments/" + testEnvID + "/values/API_SECRET/reveal", "disclosed-secret", nil},
					{"recovery mint", http.MethodPost, api.PathPrefix + "/auth/recovery-codes/regenerate", "hik_1_rc_one", map[string]any{"proof": "password"}},
				} {
					t.Run(operation.name, func(t *testing.T) {
						response, payload := raw(t, srv, operation.method, operation.path, operation.body, func(r *http.Request) {
							if cookie {
								r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "hik_1_br_stub"})
								r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "hik_1_cs_stub"})
								r.Header.Set(csrfHeaderName, "hik_1_cs_stub")
							} else {
								r.Header.Set("Authorization", "Bearer hik_1_cli_stub")
							}
						})
						want := outcome.status
						if response.StatusCode != want {
							t.Fatalf("status = %d, want %d: %s", response.StatusCode, want, payload)
						}
						assertNoStore(t, response.Header)
						if strings.Contains(string(payload), operation.contains) == refused {
							t.Fatalf("unexpected disclosure body: %s", payload)
						}
						request := httptest.NewRequest(operation.method, operation.path, nil)
						if err := api.ValidateResponse(request, response.StatusCode, response.Header, payload); err != nil {
							t.Fatal(err)
						}
					})
				}
			})
		}
	}
}

func TestAPIMiddlewareRefusalsNoStore(t *testing.T) {
	srv := newTestServer(t, browserLoginAuth(), stubOrgs{})
	for _, tt := range []struct {
		name, method, path string
		body               any
		mutate             func(*http.Request)
		status             int
	}{
		{"both credentials", http.MethodGet, api.PathPrefix + "/auth/whoami", nil, func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer hik_1_cli_stub")
			r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "hik_1_br_stub"})
		}, 401},
		{"missing CSRF", http.MethodPost, api.PathPrefix + "/auth/recovery-codes/regenerate", map[string]any{"proof": "password"}, func(r *http.Request) { r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "hik_1_br_stub"}) }, 401},
		{"invalid body", http.MethodPost, api.PathPrefix + "/auth/local/login", map[string]any{"unexpected": true}, nil, 400},
		{"unmatched", http.MethodGet, api.PathPrefix + "/missing", nil, nil, 404},
		{"API root", http.MethodGet, api.PathPrefix, nil, nil, 404},
		{"unsupported method", http.MethodTrace, api.PathPrefix + "/auth/local/login", nil, nil, 404},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response, payload := raw(t, srv, tt.method, tt.path, tt.body, tt.mutate)
			if response.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d: %s", response.StatusCode, tt.status, payload)
			}
			assertNoStore(t, response.Header)
		})
	}
}
