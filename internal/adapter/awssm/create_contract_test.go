package awssm

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

// A local request capture verifies the raw HTTP contract independently of the
// emulator. It does not establish real AWS acceptance of metadata-only creation.
func TestCreateSecretWireIncludesTokenWithoutValue(t *testing.T) {
	token := strings.Repeat("a", 64)
	requests := make(chan map[string]json.RawMessage, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Amz-Target") != "secretsmanager.CreateSecret" {
			t.Errorf("unexpected operation: %s", r.Header.Get("X-Amz-Target"))
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- body
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	client, err := NewClient(ClientConfig{
		Origin: server.URL, Credential: staticDescriptor(testRegion), Deadline: 5 * time.Second,
		AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, RootCAs: roots,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Forget)
	for range 2 {
		if err := client.CreateSecret(t.Context(), CreateSecretInput{Name: "metadata-only", ClientRequestToken: token}); err != nil {
			t.Fatal(err)
		}
		body := <-requests
		var got string
		if err := json.Unmarshal(body["ClientRequestToken"], &got); err != nil || got != token {
			t.Fatalf("create token = %q, decode error = %v", got, err)
		}
		for _, field := range []string{"SecretString", "SecretBinary"} {
			if _, present := body[field]; present {
				t.Fatalf("metadata-only create sent %s", field)
			}
		}
	}
}

type captureCreateAPI struct {
	*fakeAPI
	inputs []CreateSecretInput
}

func (api *captureCreateAPI) CreateSecret(ctx context.Context, input CreateSecretInput) error {
	api.inputs = append(api.inputs, input)
	return api.fakeAPI.CreateSecret(ctx, input)
}

func TestCreateSecretTokenSurvivesJobRetry(t *testing.T) {
	api := &captureCreateAPI{fakeAPI: newFakeAPI()}
	// Synthetic transport failure, rather than a claim about an AWS response.
	api.failOnce["create:prod/app"] = &ResponseError{Status: 500, Code: "InternalServiceError"}
	journal := newFakeJournal()
	req := adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), JobID: "create-retry"}
	module := &Module{API: api}
	if result, err := module.Sync(t.Context(), req, journal); err == nil && len(result.Failed) == 0 {
		t.Fatal("synthetic create failure was not reported")
	}
	req.Ledger = journal.ledger()
	if result, err := module.Sync(t.Context(), req, journal); err != nil || len(result.Failed) != 0 {
		t.Fatalf("create retry: %+v, %v", result, err)
	}
	if len(api.inputs) != 2 {
		t.Fatalf("create attempts = %d, want 2", len(api.inputs))
	}
	want := idempotencyToken(req.JobID, req.Target.ID, req.Target.Generation, "prod/app")
	for _, input := range api.inputs {
		if input.ClientRequestToken != want {
			t.Fatalf("create retry changed token: %q, want %q", input.ClientRequestToken, want)
		}
	}
	if api.current("prod/app") == "" || len(api.secrets["prod/app"].values) != 1 {
		t.Fatal("retry did not deliver exactly one version")
	}
}
