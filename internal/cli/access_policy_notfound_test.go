package cli

import (
	"encoding/json"
	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAccessPolicyUpdateMissingUsesNotFoundExit(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == api.PathPrefix+"/meta" {
			_ = json.NewEncoder(w).Encode(apigen.Meta{ServerVersion: "fixture-current", ApiRevision: api.Revision})
			return
		}
		requests++
		if r.Method != http.MethodGet {
			t.Errorf("unexpected write: %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(apigen.AccessPolicyList{Items: []apigen.AccessPolicy{}})
	}))
	defer srv.Close()
	st, stateDir := machineState(t, srv.URL)
	if err := st.PutSession(SessionArtifact{Instance: "local", Origin: srv.URL, Token: "fixture-human", SessionID: "ses_fixture", Principal: "usr_fixture", ExpiresAt: "2030-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	ios, _, stderr := composeIO(stateDir, t.TempDir(), "", nil)
	code := Run(t.Context(), ios, []string{"access", "policy", "update", "xpol_missing", "--disabled", "--instance", "local", "--org", "org_one", "--project", "prj_one"})
	if code != ExitNotFound || requests != 1 {
		t.Fatalf("exit=%d requests=%d stderr=%s", code, requests, stderr)
	}
}
