package vaultkv

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

// TestNoValueReadPathExists pins the closed provider surface. The API method
// set is exact, and the operation registry every request resolves through
// holds no read or list of a /data/ path, no version destroy, no metadata
// delete, and no sys/raw.
func TestNoValueReadPathExists(t *testing.T) {
	typeOf := reflect.TypeOf((*API)(nil)).Elem()
	got := make([]string, 0, typeOf.NumMethod())
	for i := range typeOf.NumMethod() {
		got = append(got, typeOf.Method(i).Name)
	}
	want := []string{"DeleteLatest", "Health", "LookupSelf", "MountInfo", "PatchCustomMetadata", "ReadMetadata", "WriteCAS"}
	if !slices.Equal(got, want) {
		t.Fatalf("linked Vault API operations = %v, want closed value-blind set %v", got, want)
	}
	for name, op := range operationRegistry {
		switch {
		case strings.Contains(op.Path, "/data/") && (op.Method == http.MethodGet || op.Method == "LIST"):
			t.Errorf("operation %s links a value read %s %s", name, op.Method, op.Path)
		case op.Method == "LIST":
			t.Errorf("operation %s links a LIST", name)
		case strings.Contains(op.Path, "/destroy/") || strings.Contains(op.Path, "/delete/") || strings.Contains(op.Path, "/undelete/"):
			t.Errorf("operation %s links version lifecycle %s", name, op.Path)
		case strings.Contains(op.Path, "/metadata/") && op.Method == http.MethodDelete:
			t.Errorf("operation %s links irreversible metadata deletion", name)
		case strings.Contains(op.Path, "sys/raw"):
			t.Errorf("operation %s links sys/raw", name)
		}
	}
}

// TestClientSourceBuildsRequestsOnlyThroughTheRegistry is the route scan: a
// hand-built request path would bypass the registry the test above pins.
func TestClientSourceBuildsRequestsOnlyThroughTheRegistry(t *testing.T) {
	raw, err := os.ReadFile("client.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, forbidden := range []string{"http.MethodGet, c.base", "\"GET\"", "\"LIST\"", "/destroy/", "\"/v1/sys/raw"} {
		if strings.Contains(source, forbidden) {
			t.Errorf("client.go contains %q outside the operation registry", forbidden)
		}
	}
	calls := regexp.MustCompile(`c\.do\(ctx, "([a-z-]+)"`).FindAllStringSubmatch(source, -1)
	if len(calls) == 0 {
		t.Fatal("no registry calls found")
	}
	for _, call := range calls {
		if _, ok := operationRegistry[call[1]]; !ok {
			t.Errorf("client.go calls unregistered operation %q", call[1])
		}
	}
}

func TestParseOrigin(t *testing.T) {
	for raw, want := range map[string]Origin{
		"https://vault.example:8200":          {Base: "https://vault.example:8200"},
		"https://vault.example:8200/":         {Base: "https://vault.example:8200"},
		"https://vault.example/team-a":        {Base: "https://vault.example", Namespace: "team-a"},
		"https://vault.example/team-a/child1": {Base: "https://vault.example", Namespace: "team-a/child1"},
	} {
		got, err := ParseOrigin(raw)
		if err != nil || got != want {
			t.Errorf("ParseOrigin(%q) = %+v, %v; want %+v", raw, got, err, want)
		}
	}
	for _, raw := range []string{"http://vault.example", "https://user@vault.example", "https://vault.example?x=1", "https://vault.example/team-a/", "https://vault.example/ns/../x", "https://vault.example/root", "https://vault.example/a%2Fb", "vault.example"} {
		if _, err := ParseOrigin(raw); err == nil {
			t.Errorf("ParseOrigin(%q) accepted", raw)
		}
	}
	if got, _ := CanonicalOrigin("https://vault.example:8200/team-a"); got != "https://vault.example:8200/team-a" {
		t.Fatalf("CanonicalOrigin = %q", got)
	}
}

func TestParseCredentialNeverEchoesInput(t *testing.T) {
	cred, err := ParseCredential("  hvs.static-token\n")
	if err != nil || cred.Method != TokenAuth || cred.Token != "hvs.static-token" {
		t.Fatalf("bare token = %+v, %v", cred, err)
	}
	cred, err = ParseCredential(`{"method":"approle","role_id":"r","secret_id":"s"}`)
	if err != nil || cred.Method != AppRoleAuth || cred.Mount != "approle" {
		t.Fatalf("approle = %+v, %v", cred, err)
	}
	for _, raw := range []string{
		"",
		"two words",
		`{"method":"token","token":"hvs.leak-me","tokn":"typo"}`,
		`{"method":"approle","role_id":"r"}`,
		`{"method":"approle","role_id":"r","secret_id":"s","mount":"../x"}`,
		`{"method":"jwt","token":"hvs.leak-me"}`,
		`{"method":"token","token":"hvs.leak-me","spki_sha256":"short"}`,
		`{"method":"token","token":"hvs.leak-me","ca_pem":"not pem"}`,
	} {
		_, err := ParseCredential(raw)
		if err == nil {
			t.Errorf("ParseCredential(%q) accepted", raw)
			continue
		}
		if strings.Contains(err.Error(), "leak-me") {
			t.Errorf("ParseCredential error echoes the credential: %v", err)
		}
	}
}

func TestClientRefusesDeadlineThatCanOutliveProviderFence(t *testing.T) {
	_, err := NewClient(ClientConfig{Origin: "https://vault.example", Credential: "t", Deadline: adapter.LeaseTime})
	if err == nil || !strings.Contains(err.Error(), "shorter than") {
		t.Fatalf("NewClient() error = %v, want provider-write lease bound", err)
	}
}

func TestClientTransportPolicy(t *testing.T) {
	client, err := NewClient(ClientConfig{Origin: "https://vault.example", Credential: "t", Deadline: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	transport := client.http.Transport.(*http.Transport)
	if transport.Proxy != nil || transport.DialContext == nil || transport.TLSClientConfig.MinVersion < 0x0303 {
		t.Fatalf("transport policy = proxy:%v dial:%v tls:%#v", transport.Proxy != nil, transport.DialContext != nil, transport.TLSClientConfig)
	}
	if err := client.http.CheckRedirect(&http.Request{}, nil); err == nil {
		t.Fatal("redirects are followed")
	}
}

type recordedRequest struct {
	Method, Path, Token, Namespace, ContentType string
	Body                                        map[string]any
}

type vaultStub struct {
	t        *testing.T
	mu       sync.Mutex
	requests []recordedRequest
	handle   func(w http.ResponseWriter, r recordedRequest)
}

func (s *vaultStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	rec := recordedRequest{Method: r.Method, Path: r.URL.RequestURI(), Token: r.Header.Get("X-Vault-Token"), Namespace: r.Header.Get("X-Vault-Namespace"), ContentType: r.Header.Get("Content-Type")}
	if len(raw) != 0 {
		_ = json.Unmarshal(raw, &rec.Body)
	}
	s.mu.Lock()
	s.requests = append(s.requests, rec)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	s.handle(w, rec)
}

func pinnedClient(t *testing.T, server *httptest.Server, namespace, credential string) *Client {
	t.Helper()
	leaf := server.Certificate()
	sum := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	var parsed map[string]any
	if strings.HasPrefix(credential, "{") {
		if err := json.Unmarshal([]byte(credential), &parsed); err != nil {
			t.Fatal(err)
		}
	} else {
		parsed = map[string]any{"method": "token", "token": credential}
	}
	parsed["spki_sha256"] = base64.StdEncoding.EncodeToString(sum[:])
	// The test certificate is self-signed for 127.0.0.1; use it as the CA
	// bundle so chain and hostname verification still run beside the pin.
	parsed["ca_pem"] = pemCertificate(leaf.Raw)
	encoded, _ := json.Marshal(parsed)
	origin := server.URL
	if namespace != "" {
		origin += "/" + namespace
	}
	client, err := NewClient(ClientConfig{Origin: origin, Credential: string(encoded), AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, Deadline: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func pemCertificate(der []byte) string {
	encoded := base64.StdEncoding.EncodeToString(der)
	var out strings.Builder
	out.WriteString("-----BEGIN CERTIFICATE-----\n")
	for len(encoded) > 64 {
		out.WriteString(encoded[:64] + "\n")
		encoded = encoded[64:]
	}
	out.WriteString(encoded + "\n-----END CERTIFICATE-----\n")
	return out.String()
}

func TestClientSendsNamespaceTokenAndCASWithoutReadingData(t *testing.T) {
	stub := &vaultStub{t: t, handle: func(w http.ResponseWriter, r recordedRequest) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.Path, "/v1/secret/metadata/"):
			_, _ = io.WriteString(w, `{"data":{"current_version":3,"custom_metadata":{"managed_by_hikyo":"tgt"},"versions":{"3":{"deletion_time":"2026-07-01T00:00:00.5Z","destroyed":false},"2":{"deletion_time":"2026-01-01T00:00:00Z","destroyed":false},"1":{"deletion_time":"2026-06-01T00:00:00Z","destroyed":false}}}}`)
		case r.Method == http.MethodPost && strings.HasPrefix(r.Path, "/v1/secret/data/"):
			_, _ = io.WriteString(w, `{"data":{"version":4}}`)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}}
	server := httptest.NewTLSServer(stub)
	defer server.Close()
	client := pinnedClient(t, server, "team-a", "hvs.static")
	// delete_version_after stamps a future deletion_time on a live version;
	// only a time at or before now is a soft delete.
	client.now = func() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) }
	meta, err := client.ReadMetadata(t.Context(), "secret", "apps/pay svc/TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if meta.CurrentVersion != 3 || meta.CustomMetadata[MarkerKey] != "tgt" || !meta.Versions[1].Deleted || !meta.Versions[2].Deleted || meta.Versions[3].Deleted {
		t.Fatalf("metadata = %+v", meta)
	}
	version, err := client.WriteCAS(t.Context(), "secret", "apps/pay svc/TOKEN", "plaintext", 3)
	if err != nil || version != 4 {
		t.Fatalf("WriteCAS = %d, %v", version, err)
	}
	value := "tgt"
	if err := client.PatchCustomMetadata(t.Context(), "secret", "apps/pay svc/TOKEN", map[string]*string{MarkerKey: &value, PendingKey: nil}); err != nil {
		t.Fatal(err)
	}
	if len(stub.requests) != 3 {
		t.Fatalf("requests = %+v", stub.requests)
	}
	for _, r := range stub.requests {
		if r.Token != "hvs.static" || r.Namespace != "team-a" {
			t.Errorf("%s %s token=%q namespace=%q", r.Method, r.Path, r.Token, r.Namespace)
		}
		if r.Method == http.MethodGet && strings.Contains(r.Path, "/data/") {
			t.Errorf("value read issued: %s", r.Path)
		}
	}
	write := stub.requests[1]
	if write.Path != "/v1/secret/data/apps/pay%20svc/TOKEN" {
		t.Fatalf("write path = %s", write.Path)
	}
	if options, _ := write.Body["options"].(map[string]any); options["cas"] != float64(3) {
		t.Fatalf("write options = %v, want cas 3", write.Body["options"])
	}
	if data, _ := write.Body["data"].(map[string]any); data["value"] != "plaintext" || len(data) != 1 {
		t.Fatalf("write data = %v", write.Body["data"])
	}
	patch := stub.requests[2]
	if patch.Method != http.MethodPatch || patch.ContentType != "application/merge-patch+json" {
		t.Fatalf("patch = %+v", patch)
	}
	if custom, _ := patch.Body["custom_metadata"].(map[string]any); custom[MarkerKey] != "tgt" || custom[PendingKey] != nil {
		t.Fatalf("patch body = %v", patch.Body)
	} else if _, present := custom[PendingKey]; !present {
		t.Fatal("merge patch dropped the null that removes the pending marker")
	}
}

func TestClientClassifiesProviderAnswersWithoutSurfacingBodies(t *testing.T) {
	status := 0
	body := ""
	header := http.Header{}
	stub := &vaultStub{t: t, handle: func(w http.ResponseWriter, _ recordedRequest) {
		for k, v := range header {
			w.Header()[k] = v
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}}
	server := httptest.NewTLSServer(stub)
	defer server.Close()
	client := pinnedClient(t, server, "", "hvs.static")

	status, body = 400, `{"errors":["check-and-set parameter did not match the current version: plaintext-echo"]}`
	_, err := client.WriteCAS(t.Context(), "secret", "p", "v", 1)
	if !IsCASMismatch(err) || strings.Contains(err.Error(), "plaintext-echo") {
		t.Fatalf("CAS mismatch = %v", err)
	}
	status, body = 400, `{"errors":["something else"]}`
	if _, err := client.WriteCAS(t.Context(), "secret", "p", "v", 1); IsCASMismatch(err) || !definitive(err) {
		t.Fatalf("plain 400 = %v", err)
	}
	status, body = 412, `{"errors":["standby not caught up"]}`
	if _, err := client.WriteCAS(t.Context(), "secret", "p", "v", 1); err == nil || definitive(err) {
		t.Fatalf("412 = %v, want retryable", err)
	}
	status, body = 403, `{"errors":["permission denied"]}`
	if _, err := client.ReadMetadata(t.Context(), "secret", "p"); !errors.Is(err, adapter.ErrProviderAuth) {
		t.Fatalf("403 = %v, want provider auth", err)
	}
	status, body = 503, `{"errors":["Vault is sealed"]}`
	var response *ResponseError
	if _, err := client.ReadMetadata(t.Context(), "secret", "p"); !errors.As(err, &response) || !response.Sealed || definitive(err) {
		t.Fatalf("503 = %v, want sealed, not definitive", err)
	}
	status, body = 429, `{"errors":["rate limit quota exceeded"]}`
	header.Set("Retry-After", "7")
	before := time.Now()
	_, err = client.ReadMetadata(t.Context(), "secret", "p")
	at, ok := adapter.ProviderRetryAt(err)
	if !errors.Is(err, adapter.ErrRateLimited) || !ok || at.Before(before.Add(6*time.Second)) {
		t.Fatalf("429 = %v retry %v %v", err, at, ok)
	}
	header = http.Header{}
	status, body = 404, `{"errors":[]}`
	if _, err := client.ReadMetadata(t.Context(), "secret", "p"); !IsNotFound(err) {
		t.Fatalf("404 = %v", err)
	}
	status, body = 200, `{"data":{"version":9}}`
	if _, err := client.WriteCAS(t.Context(), "secret", "p", "v", 1); !errors.Is(err, adapter.ErrIndeterminate) {
		t.Fatalf("version skew = %v, want indeterminate", err)
	}
	status, body = 200, strings.Repeat("x", responseCap+1)
	if _, err := client.ReadMetadata(t.Context(), "secret", "p"); err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("oversized response = %v", err)
	}
}

func TestClientRefusesUnpinnedCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	// httptest servers share one built-in key, so pin a different SPKI.
	sum := sha256.Sum256([]byte("some other public key"))
	credential, _ := json.Marshal(map[string]string{
		"method": "token", "token": "hvs.static",
		"ca_pem":      pemCertificate(server.Certificate().Raw),
		"spki_sha256": base64.StdEncoding.EncodeToString(sum[:]),
	})
	client, err := NewClient(ClientConfig{Origin: server.URL, Credential: string(credential), AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, Deadline: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Health(t.Context()); err == nil || !strings.Contains(err.Error(), "pinned SPKI") {
		t.Fatalf("Health() with mismatched pin = %v", err)
	}
}

func TestAppRoleLoginIsBoundedAndRevokedOnForget(t *testing.T) {
	stub := &vaultStub{t: t, handle: func(w http.ResponseWriter, r recordedRequest) {
		switch r.Path {
		case "/v1/auth/approle-ci/login":
			_, _ = io.WriteString(w, `{"auth":{"client_token":"hvs.minted","lease_duration":3600,"renewable":true}}`)
		case "/v1/auth/token/revoke-self":
			w.WriteHeader(http.StatusNoContent)
		default:
			_, _ = io.WriteString(w, `{"data":{"current_version":0,"custom_metadata":null,"versions":{}}}`)
		}
	}}
	server := httptest.NewTLSServer(stub)
	defer server.Close()
	client := pinnedClient(t, server, "team-a", `{"method":"approle","role_id":"role","secret_id":"secret-id","mount":"approle-ci"}`)
	for range 2 {
		if _, err := client.ReadMetadata(t.Context(), "secret", "p"); err != nil {
			t.Fatal(err)
		}
	}
	client.Forget()
	var paths []string
	for _, r := range stub.requests {
		paths = append(paths, r.Path)
		if r.Namespace != "team-a" {
			t.Errorf("%s namespace = %q", r.Path, r.Namespace)
		}
	}
	want := []string{"/v1/auth/approle-ci/login", "/v1/secret/metadata/p", "/v1/secret/metadata/p", "/v1/auth/token/revoke-self"}
	if !slices.Equal(paths, want) {
		t.Fatalf("requests = %v, want one login, reads, one revoke %v", paths, want)
	}
	if login := stub.requests[0]; login.Token != "" || login.Body["role_id"] != "role" || login.Body["secret_id"] != "secret-id" {
		t.Fatalf("login = %+v", login)
	}
	if stub.requests[1].Token != "hvs.minted" || stub.requests[3].Token != "hvs.minted" {
		t.Fatal("minted token was not used and revoked")
	}
	if _, err := client.ReadMetadata(t.Context(), "secret", "p"); !errors.Is(err, adapter.ErrProviderAuth) {
		t.Fatalf("use after Forget = %v, want refused", err)
	}
}

func TestAppRoleRenewalBudgetAndFailureAreAuthFailures(t *testing.T) {
	renewals := 0
	stub := &vaultStub{t: t, handle: func(w http.ResponseWriter, r recordedRequest) {
		switch r.Path {
		case "/v1/auth/approle/login":
			_, _ = io.WriteString(w, `{"auth":{"client_token":"hvs.minted","lease_duration":10,"renewable":true}}`)
		case "/v1/auth/token/renew-self":
			renewals++
			if renewals > 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = io.WriteString(w, `{"auth":{"lease_duration":10}}`)
		default:
			_, _ = io.WriteString(w, `{"data":{"current_version":0,"versions":{}}}`)
		}
	}}
	server := httptest.NewTLSServer(stub)
	defer server.Close()
	client := pinnedClient(t, server, "", `{"method":"approle","role_id":"r","secret_id":"s"}`)
	if _, err := client.ReadMetadata(t.Context(), "secret", "p"); err != nil {
		t.Fatal(err)
	}
	// The 10s lease is inside the renewal margin: the next request renews.
	if _, err := client.ReadMetadata(t.Context(), "secret", "p"); err != nil {
		t.Fatalf("first renewal = %v", err)
	}
	if _, err := client.ReadMetadata(t.Context(), "secret", "p"); !errors.Is(err, adapter.ErrProviderAuth) {
		t.Fatalf("refused renewal = %v, want provider auth", err)
	}
	client.renewals = maxRenewals
	if _, err := client.ReadMetadata(t.Context(), "secret", "p"); !errors.Is(err, adapter.ErrProviderAuth) || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("exhausted budget = %v", err)
	}
}

func TestStaticTokenIsNeverRevoked(t *testing.T) {
	stub := &vaultStub{t: t, handle: func(w http.ResponseWriter, _ recordedRequest) {
		_, _ = io.WriteString(w, `{"data":{"expire_time":"2030-01-02T03:04:05Z"}}`)
	}}
	server := httptest.NewTLSServer(stub)
	defer server.Close()
	client := pinnedClient(t, server, "", "hvs.operator")
	info, err := client.LookupSelf(t.Context())
	if err != nil || !info.ExpireTime.Equal(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("LookupSelf = %+v, %v", info, err)
	}
	client.Forget()
	for _, r := range stub.requests {
		if strings.Contains(r.Path, "revoke") {
			t.Fatal("operator static token was revoked")
		}
	}
}

func TestReadMetadataRefusesMalformedDeletionTime(t *testing.T) {
	stub := &vaultStub{t: t, handle: func(w http.ResponseWriter, _ recordedRequest) {
		_, _ = io.WriteString(w, `{"data":{"current_version":1,"custom_metadata":{},"versions":{"1":{"deletion_time":"yesterday","destroyed":false}}}}`)
	}}
	server := httptest.NewTLSServer(stub)
	defer server.Close()
	if _, err := pinnedClient(t, server, "", "hvs.static").ReadMetadata(t.Context(), "secret", "apps/pay/TOKEN"); err == nil || !strings.Contains(err.Error(), "malformed deletion time") {
		t.Fatalf("ReadMetadata() = %v, want malformed deletion time refusal", err)
	}
}
