package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
)

// dekServer answers rotate-dek and both reencrypt routes, echoing the
// addressed scope, and records every request's method, path and body.
func dekServer(t *testing.T) (http.Handler, *[]string) {
	t.Helper()
	var seen []string
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+string(body))
		w.Header().Set("Content-Type", "application/json")
		org, project := "org_70", "prj_70"
		switch {
		case r.URL.Path == "/api/v1/instance/rotate-dek":
			var req apigen.RotateDEKRequest
			if err := json.Unmarshal(body, &req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			out := apigen.DEKRotation{Scope: apigen.DEKRotationScope(req.Scope), KeyVersion: 2, Org: req.Org, Project: req.Project}
			_ = json.NewEncoder(w).Encode(out)
		case r.URL.Path == "/api/v1/instance/reencrypt":
			_ = json.NewEncoder(w).Encode(apigen.ReencryptResult{Scope: apigen.ReencryptResultScopeInstance, RowsMoved: 3})
		case r.URL.Path == "/api/v1/orgs/org_70/projects/prj_70/reencrypt":
			_ = json.NewEncoder(w).Encode(apigen.ReencryptResult{Scope: apigen.ReencryptResultScopeProject, RowsMoved: 5, Org: &org, Project: &project})
		default:
			http.NotFound(w, r)
		}
	}), &seen
}

// Both verbs dispatch at both scopes. --instance stays the shared instance
// reference; --instance-scope selects the instance DEK.
func TestDEKRotationDispatch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		request string
		body    string
		row     string
	}{
		{"rotate-dek instance", []string{"rotate-dek", "--instance-scope", "--yes"},
			"POST /api/v1/instance/rotate-dek ", `"scope":"instance"`, "instance"},
		{"rotate-dek project", []string{"rotate-dek", "--org", "org_70", "--project", "prj_70", "--yes"},
			"POST /api/v1/instance/rotate-dek ", `"org":"org_70","project":"prj_70","scope":"project"`, "org_70/prj_70"},
		{"reencrypt instance", []string{"reencrypt", "--instance-scope"},
			"POST /api/v1/instance/reencrypt", "", "3"},
		{"reencrypt project", []string{"reencrypt", "--org", "org_70", "--project", "prj_70"},
			"POST /api/v1/orgs/org_70/projects/prj_70/reencrypt", "", "org_70/prj_70"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, seen := dekServer(t)
			ios, stdout, stderr := definitionsTestIO(t, handler)
			if code := cli.Run(t.Context(), ios, append(tc.args, "--instance", "local")); code != cli.ExitOK {
				t.Fatalf("exit %d; stderr=%s", code, stderr)
			}
			if len(*seen) != 1 || !strings.HasPrefix((*seen)[0], tc.request) || !strings.Contains((*seen)[0], tc.body) {
				t.Fatalf("requests = %v", *seen)
			}
			if !strings.Contains(stdout.String(), tc.row) {
				t.Fatalf("stdout = %q", stdout)
			}
		})
	}
}

// Conflicting and incomplete scopes are usage errors that reach no server.
func TestDEKRotationScopeRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"rotate-dek instance scope with org", []string{"rotate-dek", "--instance-scope", "--org", "org_70", "--yes"}},
		{"rotate-dek instance scope with project", []string{"rotate-dek", "--instance-scope", "--project", "prj_70", "--yes"}},
		{"rotate-dek instance scope with env", []string{"rotate-dek", "--instance-scope", "--env", "env_70", "--yes"}},
		{"rotate-dek org without project", []string{"rotate-dek", "--org", "org_70", "--yes"}},
		{"rotate-dek no scope", []string{"rotate-dek", "--yes"}},
		{"reencrypt instance scope with org", []string{"reencrypt", "--instance-scope", "--org", "org_70", "--project", "prj_70"}},
		{"reencrypt project without org", []string{"reencrypt", "--project", "prj_70"}},
		{"reencrypt no scope", []string{"reencrypt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, seen := dekServer(t)
			ios, _, stderr := definitionsTestIO(t, handler)
			if code := cli.Run(t.Context(), ios, append(tc.args, "--instance", "local")); code != cli.ExitUsage {
				t.Fatalf("exit %d, want usage; stderr=%s", code, stderr)
			}
			if len(*seen) != 0 {
				t.Fatalf("a refused rotation reached the server: %v", *seen)
			}
		})
	}
}
