package cloudflare

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

// The API method set is the import boundary Sync can link. It contains no
// operation that returns a secret value, and every write is secret_text.
func TestAPIIsClosedAndValueBlind(t *testing.T) {
	typeOf := reflect.TypeOf((*API)(nil)).Elem()
	got := make([]string, 0, typeOf.NumMethod())
	for i := range typeOf.NumMethod() {
		got = append(got, typeOf.Method(i).Name)
	}
	want := []string{"DeleteSecret", "ListAccountIDs", "ListSecretNames", "PatchPagesSecret", "PutSecret", "ResolveProject", "ResolveScript", "VerifyToken"}
	if !slices.Equal(got, want) {
		t.Fatalf("linked Cloudflare API = %v, want closed set %v", got, want)
	}
	for name, op := range operationRegistry {
		if op.Method == http.MethodGet && strings.Contains(op.Path, "/secrets/") {
			t.Errorf("operation %s reads a single secret: %s", name, op.Path)
		}
		if op.Method == http.MethodGet && op.Mutation {
			t.Errorf("operation %s is a GET marked as mutation", name)
		}
	}
	// The only read that can carry a plaintext Pages value decodes into a
	// shape with no field able to hold it.
	shape := reflect.TypeOf(ProjectShape{})
	if shape.NumField() != 2 || shape.Field(1).Type != reflect.TypeOf(map[string]string{}) {
		t.Fatalf("ProjectShape grew a field: %v", shape)
	}
}

type recorded struct {
	method, path, auth string
	body               map[string]any
}

type fixture struct {
	mu       sync.Mutex
	requests []recorded
	handler  func(w http.ResponseWriter, r *http.Request)
}

func (f *fixture) client(t *testing.T) *Client {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.requests = append(f.requests, recorded{method: r.Method, path: r.URL.RequestURI(), auth: r.Header.Get("Authorization"), body: body})
		f.mu.Unlock()
		f.handler(w, r)
	}))
	t.Cleanup(server.Close)
	return &Client{origin: server.URL, token: testToken, http: server.Client(), now: func() time.Time { return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) }}
}

func ok(w http.ResponseWriter, result string) {
	_, _ = io.WriteString(w, `{"success":true,"errors":[],"messages":[],"result":`+result+`}`)
}

func TestWorkersWritesAreSecretTextWithBearer(t *testing.T) {
	f := &fixture{handler: func(w http.ResponseWriter, _ *http.Request) { ok(w, `{"name":"TOKEN","type":"secret_text"}`) }}
	c := f.client(t)
	d := workersTarget().Destination
	if err := c.PutSecret(t.Context(), d, "TOKEN", "v"); err != nil {
		t.Fatal(err)
	}
	req := f.requests[0]
	if req.method != http.MethodPut || req.path != "/client/v4/accounts/"+testAccount+"/workers/scripts/api-worker/secrets" {
		t.Fatalf("request = %s %s", req.method, req.path)
	}
	if req.auth != "Bearer "+testToken || req.body["type"] != SecretType || req.body["name"] != "TOKEN" || req.body["text"] != "v" {
		t.Fatalf("request = %+v", req)
	}
}

func TestPagesPatchIsSingleKeySecretTextOrNull(t *testing.T) {
	f := &fixture{handler: func(w http.ResponseWriter, _ *http.Request) { ok(w, `{}`) }}
	c := f.client(t)
	d := pagesTarget("preview").Destination
	value := "v"
	if err := c.PatchPagesSecret(t.Context(), d, "TOKEN", &value); err != nil {
		t.Fatal(err)
	}
	if err := c.PatchPagesSecret(t.Context(), d, "OLD", nil); err != nil {
		t.Fatal(err)
	}
	vars := func(i int) map[string]any {
		configs := f.requests[i].body["deployment_configs"].(map[string]any)
		if len(configs) != 1 || configs["preview"] == nil {
			t.Fatalf("patch touched other environments: %v", configs)
		}
		return configs["preview"].(map[string]any)["env_vars"].(map[string]any)
	}
	set := vars(0)
	if len(set) != 1 || set["TOKEN"].(map[string]any)["type"] != SecretType {
		t.Fatalf("set body = %v", set)
	}
	if del := vars(1); len(del) != 1 || del["OLD"] != nil {
		t.Fatalf("delete body = %v", del)
	} else if _, present := del["OLD"]; !present {
		t.Fatal("delete omitted the key instead of sending null")
	}
}

func TestProjectShapeDiscardsPlaintextValues(t *testing.T) {
	f := &fixture{handler: func(w http.ResponseWriter, _ *http.Request) {
		ok(w, `{"id":"proj-1","name":"site","deployment_configs":{"preview":{"env_vars":{"PUBLIC":{"type":"plain_text","value":"visible-plaintext"},"TOKEN":{"type":"secret_text"}}},"production":{"env_vars":{"PROD_ONLY":{"type":"secret_text"}}}}}`)
	}}
	shape, err := f.client(t).ResolveProject(t.Context(), pagesTarget("preview").Destination)
	if err != nil {
		t.Fatal(err)
	}
	if shape.ID != "proj-1" || len(shape.Names) != 2 || shape.Names["PUBLIC"] != "plain_text" || shape.Names["TOKEN"] != SecretType {
		t.Fatalf("shape = %+v", shape)
	}
	for name, kind := range shape.Names {
		if strings.Contains(name+kind, "visible-plaintext") {
			t.Fatal("plaintext value decoded")
		}
	}
}

func TestRateLimitCarriesRetryAt(t *testing.T) {
	f := &fixture{handler: func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}}
	err := f.client(t).DeleteSecret(t.Context(), workersTarget().Destination, "TOKEN")
	at, found := adapter.ProviderRetryAt(err)
	if !errors.Is(err, adapter.ErrRateLimited) || !found || !at.Equal(time.Date(2026, 9, 1, 0, 0, 30, 0, time.UTC)) {
		t.Fatalf("err=%v retryAt=%v", err, at)
	}
}

func TestUnconfirmedEnvelopeIsAmbiguous(t *testing.T) {
	for name, body := range map[string]string{
		"success false": `{"success":false,"errors":[{"code":10000,"message":"echo s3cret"}]}`,
		"no envelope":   `not json s3cret`,
	} {
		t.Run(name, func(t *testing.T) {
			f := &fixture{handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }}
			err := f.client(t).PutSecret(t.Context(), workersTarget().Destination, "TOKEN", "s3cret")
			if !errors.Is(err, ErrAmbiguousResponse) || strings.Contains(err.Error(), "s3cret") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestRefusalsNeverEchoBodies(t *testing.T) {
	f := &fixture{handler: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"success":false,"errors":[{"message":"s3cret"}]}`)
	}}
	err := f.client(t).PutSecret(t.Context(), workersTarget().Destination, "TOKEN", "s3cret")
	if !errors.Is(err, adapter.ErrProviderAuth) || !IsStatus(err, http.StatusForbidden) || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyTokenFallsBackToUserEndpointAndRecordsExpiry(t *testing.T) {
	f := &fixture{handler: func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/client/v4/accounts/") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		ok(w, `{"id":"tok","status":"active","expires_on":"2027-01-01T00:00:00Z"}`)
	}}
	c := f.client(t)
	status, err := c.VerifyToken(t.Context(), testAccount)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if status.Status != "active" || !status.ExpiresAt.Equal(want) || !c.CredentialExpiresAt().Equal(want) {
		t.Fatalf("status = %+v", status)
	}
	if len(f.requests) != 2 || f.requests[1].path != "/client/v4/user/tokens/verify" {
		t.Fatalf("requests = %+v", f.requests)
	}
}

func TestResolveScriptMissingIsNotFound(t *testing.T) {
	f := &fixture{handler: func(w http.ResponseWriter, _ *http.Request) { ok(w, `[{"id":"other","tag":"t"}]`) }}
	_, err := f.client(t).ResolveScript(t.Context(), workersTarget().Destination)
	if !IsStatus(err, http.StatusNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestClientConfigRefusals(t *testing.T) {
	cases := map[string]ClientConfig{
		"other origin":  {Origin: "https://api.example.com", Credential: testToken, Deadline: time.Second},
		"http origin":   {Origin: "http://api.cloudflare.com", Credential: testToken, Deadline: time.Second},
		"global key":    {Credential: "0123456789abcdef0123456789abcdef01234", Deadline: time.Second},
		"email pair":    {Credential: "ops@example.com:0123456789abcdef0123456789abcdef01234", Deadline: time.Second},
		"long deadline": {Credential: testToken, Deadline: adapter.LeaseTime},
	}
	for name, cfg := range cases {
		if _, err := NewClient(cfg); err == nil {
			t.Errorf("%s: NewClient accepted %+v", name, cfg)
		}
	}
	c, err := NewClient(ClientConfig{Credential: testToken, Deadline: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if c.origin != DefaultOrigin {
		t.Fatalf("origin = %q", c.origin)
	}
	transport := c.http.Transport.(*http.Transport)
	if transport.Proxy != nil || transport.TLSClientConfig.MinVersion < 0x0303 || c.http.CheckRedirect == nil {
		t.Fatal("transport policy weakened")
	}
	c.Forget()
	if c.token != "" {
		t.Fatal("Forget retained the bearer")
	}
}
