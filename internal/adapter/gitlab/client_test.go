package gitlab

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

func apiMethods() []string {
	typeOf := reflect.TypeOf((*API)(nil)).Elem()
	out := make([]string, 0, typeOf.NumMethod())
	for i := range typeOf.NumMethod() {
		out = append(out, typeOf.Method(i).Name)
	}
	return out
}

type recorded struct {
	method, path, query, token string
	body                       map[string]any
}

func tlsServer(t *testing.T, handler func(http.ResponseWriter, *http.Request)) (*httptest.Server, *[]recorded) {
	t.Helper()
	var calls []recorded
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := recorded{method: r.Method, path: r.URL.EscapedPath(), query: r.URL.RawQuery, token: r.Header.Get("PRIVATE-TOKEN")}
		if raw, _ := io.ReadAll(r.Body); len(raw) != 0 {
			_ = json.Unmarshal(raw, &rec.body)
		}
		calls = append(calls, rec)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func serverCA(server *httptest.Server) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
}

func serverPin(server *httptest.Server) string {
	sum := sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)
	return base64.StdEncoding.EncodeToString(sum[:])
}

func testClient(t *testing.T, server *httptest.Server, pin string) *Client {
	t.Helper()
	client, err := NewClient(ClientConfig{
		Origin: server.URL, Credential: "glpat-test", Deadline: 5 * time.Second,
		AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
		CABundlePEM:  serverCA(server), SPKIPin: pin,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Forget)
	return client
}

func project() adapter.Destination {
	return adapter.Destination{Kind: adapter.Repository, Owner: "platform", Name: "api", NumericID: 42, Scope: "production"}
}

func TestCanonicalOrigin(t *testing.T) {
	for raw, want := range map[string]string{
		"":                                "https://gitlab.com",
		"https://GitLab.Example/":         "https://gitlab.example",
		"https://gitlab.example/api/v4":   "https://gitlab.example",
		"https://host.example/gitlab/":    "https://host.example/gitlab",
		"https://gitlab.example:8443/sub": "https://gitlab.example:8443/sub",
	} {
		if got, err := CanonicalOrigin(raw); err != nil || got != want {
			t.Errorf("CanonicalOrigin(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"http://gitlab.example", "https://u:p@gitlab.example", "https://gitlab.example/?x=1", "https://gitlab.example/#f", "https://gitlab.example/../x"} {
		if _, err := CanonicalOrigin(raw); err == nil {
			t.Errorf("CanonicalOrigin(%q) accepted", raw)
		}
	}
}

func TestClientWritesWithScopeFlagsAndNoRead(t *testing.T) {
	server, calls := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
		_, _ = w.Write([]byte(`{"key":"TOKEN","value":"must-not-matter"}`))
	})
	client := testClient(t, server, serverPin(server))
	v := Variable{Key: "TOKEN", Value: "abcdefgh1", Scope: "production", Protected: true, Masked: true, Hidden: true, Raw: true}
	if result, err := client.CreateVariable(t.Context(), project(), v); err != nil || result.Status != http.StatusCreated {
		t.Fatalf("create = %+v, %v", result, err)
	}
	if result, err := client.UpdateVariable(t.Context(), project(), v); err != nil || result.Status != http.StatusOK {
		t.Fatalf("update = %+v, %v", result, err)
	}
	if err := client.DeleteVariable(t.Context(), project(), "TOKEN", "production"); err != nil {
		t.Fatal(err)
	}
	got := *calls
	if len(got) != 3 {
		t.Fatalf("calls = %+v", got)
	}
	create, update, remove := got[0], got[1], got[2]
	if create.path != "/api/v4/projects/42/variables" || create.token != "glpat-test" {
		t.Fatalf("create = %+v", create)
	}
	for key, want := range map[string]any{"key": "TOKEN", "value": "abcdefgh1", "environment_scope": "production", "protected": true, "masked": true, "masked_and_hidden": true, "raw": true, "variable_type": "env_var"} {
		if create.body[key] != want {
			t.Errorf("create body %s = %v, want %v", key, create.body[key], want)
		}
	}
	if update.path != "/api/v4/projects/42/variables/TOKEN" || update.query != "filter%5Benvironment_scope%5D=production" {
		t.Fatalf("update = %+v", update)
	}
	if _, ok := update.body["masked_and_hidden"]; ok {
		t.Fatal("masked_and_hidden may only be sent on create")
	}
	if remove.method != http.MethodDelete || remove.query != "filter%5Benvironment_scope%5D=production" {
		t.Fatalf("delete = %+v", remove)
	}
	for _, call := range got {
		if call.method == http.MethodGet {
			t.Fatalf("a write path issued a GET: %+v", call)
		}
	}
}

func TestClientGroupPathAndWildcardScope(t *testing.T) {
	server, calls := tlsServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	client := testClient(t, server, "")
	group := adapter.Destination{Kind: adapter.Organization, Owner: "platform", NumericID: 7}
	if _, err := client.UpdateVariable(t.Context(), group, Variable{Key: "MODE", Value: "x"}); err != nil {
		t.Fatal(err)
	}
	if call := (*calls)[0]; call.path != "/api/v4/groups/7/variables/MODE" || call.query != "filter%5Benvironment_scope%5D=%2A" || call.body["environment_scope"] != "*" {
		t.Fatalf("call = %+v", call)
	}
}

func TestClientSPKIPinMismatchRefusesBeforeAnyRequest(t *testing.T) {
	server, calls := tlsServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })
	wrong := base64.StdEncoding.EncodeToString(make([]byte, sha256.Size))
	client := testClient(t, server, wrong)
	_, err := client.CreateVariable(t.Context(), project(), Variable{Key: "MODE", Value: "x"})
	if err == nil || !strings.Contains(err.Error(), "SPKI pin") || len(*calls) != 0 {
		t.Fatalf("err=%v calls=%d; want pin refusal before request", err, len(*calls))
	}
}

func TestClientUntrustedCertificateRefused(t *testing.T) {
	server, calls := tlsServer(t, func(w http.ResponseWriter, _ *http.Request) {})
	client, err := NewClient(ClientConfig{Origin: server.URL, Credential: "glpat-test", Deadline: 5 * time.Second, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, SPKIPin: serverPin(server)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Version(t.Context()); err == nil || len(*calls) != 0 {
		t.Fatalf("pin alone must not bypass chain verification: err=%v", err)
	}
}

func TestClientErrorClassification(t *testing.T) {
	reset := time.Date(2026, 9, 26, 13, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name   string
		status int
		header map[string]string
		body   string
		check  func(error) bool
	}{
		{name: "taken", status: 400, body: `{"message":{"key":["(TOKEN) has already been taken"]}}`, check: IsTaken},
		{name: "invalid value is not taken", status: 400, body: `{"message":{"value":["is invalid"]}}`, check: func(err error) bool { return !IsTaken(err) && IsStatus(err, 400) }},
		{name: "unauthorized", status: 401, check: func(err error) bool { return errors.Is(err, adapter.ErrProviderAuth) }},
		{name: "retry after", status: 429, header: map[string]string{"Retry-After": "30"}, check: func(err error) bool {
			at, ok := adapter.ProviderRetryAt(err)
			return ok && errors.Is(err, adapter.ErrRateLimited) && at.After(time.Now().Add(20*time.Second))
		}},
		{name: "ratelimit reset", status: 429, header: map[string]string{"RateLimit-Reset": "1790427600"}, check: func(err error) bool {
			at, ok := adapter.ProviderRetryAt(err)
			return ok && at.Equal(reset)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server, _ := tlsServer(t, func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tt.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			_, err := testClient(t, server, "").CreateVariable(t.Context(), project(), Variable{Key: "TOKEN", Value: "abcdefgh1"})
			if err == nil || !tt.check(err) {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), "already been taken") && !IsTaken(err) {
				t.Fatal("body text leaked into error")
			}
		})
	}
}

func TestClientRefusesRedirects(t *testing.T) {
	server, _ := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/", http.StatusFound)
	})
	if _, err := testClient(t, server, "").Version(t.Context()); err == nil || !strings.Contains(err.Error(), "redirects are refused") {
		t.Fatalf("err = %v", err)
	}
}

func TestClientResolvesTokenAndDestination(t *testing.T) {
	server, calls := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/v4/personal_access_tokens/self":
			_, _ = w.Write([]byte(`{"scopes":["api"],"active":true,"revoked":false,"expires_at":"2026-12-01"}`))
		case "/api/v4/user":
			_, _ = w.Write([]byte(`{"id":9,"bot":true}`))
		case "/api/v4/projects/platform%2Fapi":
			_, _ = w.Write([]byte(`{"id":42,"path_with_namespace":"platform/api"}`))
		case "/api/v4/version":
			_, _ = w.Write([]byte(`{"version":"17.5.1"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	client := testClient(t, server, "")
	token, err := client.Token(t.Context())
	if err != nil || !token.Bot || !token.ExpiresAt.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("Token() = %+v, %v", token, err)
	}
	destination := project()
	destination.NumericID = 0
	identity, err := client.ResolveDestination(t.Context(), destination)
	if err != nil || identity.ID != 42 || identity.Path != "platform/api" {
		t.Fatalf("ResolveDestination() = %+v, %v", identity, err)
	}
	if version, err := client.Version(t.Context()); err != nil || version != "17.5.1" {
		t.Fatalf("Version() = %q, %v", version, err)
	}
	for _, call := range *calls {
		if strings.Contains(call.path, "variables") {
			t.Fatalf("metadata path touched variables: %+v", call)
		}
	}
}

func TestClientConfigValidation(t *testing.T) {
	base := ClientConfig{Origin: "https://gitlab.example", Credential: "glpat-x", Deadline: 15 * time.Second}
	if _, err := NewClient(base); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ClientConfig){
		"deadline":   func(c *ClientConfig) { c.Deadline = adapter.LeaseTime },
		"credential": func(c *ClientConfig) { c.Credential = "" },
		"pin":        func(c *ClientConfig) { c.SPKIPin = "not-a-pin" },
		"bundle":     func(c *ClientConfig) { c.CABundlePEM = "garbage" },
		"http":       func(c *ClientConfig) { c.Origin = "http://gitlab.example" },
	} {
		cfg := base
		mutate(&cfg)
		if _, err := NewClient(cfg); err == nil {
			t.Errorf("%s: NewClient accepted %+v", name, cfg)
		}
	}
}

func TestRateDeadlineClampsBeforeDurationConversion(t *testing.T) {
	now := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	for _, raw := range []string{"18446744074", "9223372036854775807"} {
		client := &Client{}
		at, ok := client.rateDeadline(http.StatusTooManyRequests, http.Header{"Retry-After": []string{raw}}, now)
		if !ok || !at.Equal(now.Add(adapter.RetryCap)) {
			t.Fatalf("Retry-After %s: got %v, %v; want capped retry", raw, at, ok)
		}
	}
}
