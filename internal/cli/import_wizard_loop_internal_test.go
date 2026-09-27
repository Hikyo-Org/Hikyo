package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/importer"
)

func TestWizardLiveVaultRefusesSyncLoopBeforeReturningSource(t *testing.T) {
	vault := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/secret/metadata/apps":
			fmt.Fprint(w, `{"data":{"keys":["key"]}}`)
		case "/v1/secret/metadata/apps/key":
			fmt.Fprint(w, `{"data":{"current_version":1,"versions":{"1":{"deletion_time":"","destroyed":false}}}}`)
		case "/v1/secret/data/apps/key":
			fmt.Fprint(w, `{"data":{"data":{"TOKEN":"source-secret"},"metadata":{"version":1}}}`)
		default:
			t.Errorf("unexpected Vault path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer vault.Close()
	t.Setenv("BAO_ADDR", vault.URL)
	t.Setenv("BAO_TOKEN", "fixture-token")
	t.Setenv("BAO_NAMESPACE", "")
	t.Setenv("VAULT_NAMESPACE", "")
	for _, tc := range []struct {
		name    string
		status  int
		path    string
		refused bool
	}{{"overlap", 200, "apps", true}, {"cannot-list", 403, "apps", true}, {"unrelated", 200, "other", false}} {
		t.Run(tc.name, func(t *testing.T) {
			listed := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == api.PathPrefix+"/meta" {
					_ = json.NewEncoder(w).Encode(apigen.Meta{ApiRevision: api.Revision})
					return
				}
				if r.URL.Path != "/project/adapters" {
					t.Errorf("unexpected Hikyo path %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				listed = true
				if tc.status != 200 {
					w.WriteHeader(tc.status)
					fmt.Fprint(w, `{"code":"forbidden","message":"denied"}`)
					return
				}
				_ = json.NewEncoder(w).Encode(apigen.AdapterList{Items: []apigen.Adapter{{Id: "adp_one", Provider: "vault-kv", Origin: vault.URL, State: "active", Targets: []apigen.AdapterTarget{{Id: "tgt_one", DestinationOwner: "secret", DestinationName: tc.path, State: "active"}}}}})
			}))
			defer server.Close()
			host := cliWizardHost{ctx: t.Context(), client: &Client{Entry: TrustEntry{Origin: server.URL}, HTTP: server.Client(), Bearer: "token"}, projectBase: "/project"}
			result, err := host.ReadSource("vault", importer.Selector{Live: true, Mount: "secret", Path: "apps", KVVersion: 2})
			if !listed {
				t.Fatal("wizard skipped loop guard")
			}
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), "loop") || len(result.Result.Records) != 0 {
					t.Fatalf("result=%v err=%v", result, err)
				}
			} else if err != nil || len(result.Result.Records) != 1 {
				t.Fatalf("unrelated source refused: %v", err)
			}
		})
	}
}
