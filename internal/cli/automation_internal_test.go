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

func TestAutomationVerbsReachServerWithMachineCredential(t *testing.T) {
	for _, command := range []string{
		"env list", "env show", "env create --name pr-730",
		"env create --name pr-730 --clone-from env_source", "env delete",
		"values set PASSWORD --stdin", "values set PASSWORD --clear",
		"values copy --from env_source --to env_target --keys PASSWORD",
		"values publish --versions version_one", "pin list", "revision show latest",
	} {
		t.Run(command, func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == api.PathPrefix+"/meta" {
					_ = json.NewEncoder(w).Encode(apigen.Meta{ServerVersion: "fixture-current", ApiRevision: api.Revision})
					return
				}
				requests++
				if r.Header.Get("Authorization") != "Bearer automation-token" {
					t.Error("machine credential missing")
				}
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"not found"}}`))
			}))
			defer srv.Close()
			_, stateDir := machineState(t, srv.URL)
			ios, _, stderr := composeIO(stateDir, t.TempDir(), "automation-token", nil)
			ios.Stdin = strings.NewReader("new-password")
			args := append(strings.Fields(command), "--instance", "local", "--org", "org_one", "--project", "prj_one", "--env", "env_one")
			code := Run(t.Context(), ios, args)
			if requests != 1 || code == ExitOK || strings.Contains(stderr.String(), "requires a human session") {
				t.Fatalf("requests=%d exit=%d stderr=%s", requests, code, stderr)
			}
		})
	}
}
