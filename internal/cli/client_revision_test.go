package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// Existing command fixtures exercise operation-specific behavior. Give them
// explicit current discovery without weakening their handlers or assertions.
func newRevisionAwareFixtureServer(handler http.Handler) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == api.PathPrefix+"/meta" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(apigen.Meta{ServerVersion: "fixture-current", ApiRevision: api.Revision})
			return
		}
		handler.ServeHTTP(w, r)
	}))
}

func TestClientRefusesSubminimumOperationBeforeDispatch(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		revision           int
	}{
		{"ordinary read", "GET", "/api/v1/orgs", 0},
		{"ordinary mutation", "POST", "/api/v1/orgs", 0},
		{"later definitions operation", "POST", "/api/v1/orgs/org_a/projects/prj_a/definitions/check", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			discovery, operations := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/meta" {
					discovery++
					_ = json.NewEncoder(w).Encode(apigen.Meta{ServerVersion: "old-fixture", ApiRevision: tc.revision})
					return
				}
				operations++
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()
			client, err := NewClient(TrustEntry{Origin: srv.URL}, "scoped-test-bearer")
			if err != nil {
				t.Fatal(err)
			}
			err = client.Do(t.Context(), tc.method, tc.path, nil, nil)
			if err == nil || !strings.Contains(err.Error(), "old-fixture") || !strings.Contains(err.Error(), "needs revision") {
				t.Fatalf("refusal=%v", err)
			}
			if discovery != 1 || operations != 0 {
				t.Fatalf("discovery=%d operations=%d", discovery, operations)
			}
		})
	}
}

func TestClientReusesDiscoveryOnlyWithinItsOriginBoundCommand(t *testing.T) {
	discovery, operations := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/meta" {
			discovery++
			_ = json.NewEncoder(w).Encode(apigen.Meta{ServerVersion: "current", ApiRevision: api.Revision})
			return
		}
		operations++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	for range 2 {
		client, err := NewClient(TrustEntry{Origin: srv.URL}, "")
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := client.Do(t.Context(), "GET", "/api/v1/orgs", nil, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	if discovery != 2 || operations != 4 {
		t.Fatalf("discovery=%d operations=%d", discovery, operations)
	}
}

func TestClientDiscoveryFailureDoesNotDispatchOperation(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.Error(w, "unavailable", 503) }))
	defer srv.Close()
	client, err := NewClient(TrustEntry{Origin: srv.URL}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Do(t.Context(), "POST", "/api/v1/orgs", nil, nil); err == nil {
		t.Fatal("missing discovery accepted")
	}
	if calls != 1 {
		t.Fatalf("requests=%d", calls)
	}
}

func TestClientChangingOriginCannotReuseAnotherServersRevision(t *testing.T) {
	current := newRevisionAwareFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer current.Close()
	discovery, operations := 0, 0
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == api.PathPrefix+"/meta" {
			discovery++
			_ = json.NewEncoder(w).Encode(apigen.Meta{ServerVersion: "old-origin", ApiRevision: 0})
			return
		}
		operations++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer old.Close()
	client, err := NewClient(TrustEntry{Origin: current.URL}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Do(t.Context(), "GET", "/api/v1/orgs", nil, nil); err != nil {
		t.Fatal(err)
	}
	client.Entry.Origin = old.URL
	err = client.Do(t.Context(), "POST", "/api/v1/orgs", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "old-origin") {
		t.Fatalf("refusal=%v", err)
	}
	if discovery != 1 || operations != 0 {
		t.Fatalf("old discovery=%d operations=%d", discovery, operations)
	}
}
