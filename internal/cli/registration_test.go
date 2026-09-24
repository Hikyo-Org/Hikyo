package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
)

// registrationServer answers the registration-policy routes and records every
// request's method, path and body.
func registrationServer(t *testing.T) (http.Handler, *[]string) {
	t.Helper()
	var seen []string
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+string(body))
		if !strings.HasSuffix(r.URL.Path, "/registration-policy") {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		limit, count := 100, 12
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apigen.RegistrationPolicy{
			Id: "rpol_1", AuthorityPrincipalId: "usr_70", State: apigen.RegistrationPolicyStateActive,
			External: []apigen.RegistrationExternalEntry{{Provider: apigen.ProviderRef{Kind: "oidc", Slug: "corp"}}},
			Landing:  apigen.RegistrationLanding{Kind: apigen.RegistrationLandingKindFreshOrg, Cap: &limit}, FreshOrgCount: &count,
			RowVersion: 1, CreatedAt: time.Unix(1_800_000_000, 0).UTC(), UpdatedAt: time.Unix(1_800_000_000, 0).UTC(),
		})
	}), &seen
}

func writePolicyFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// `set` reads the policy file, prompts for the proof (never a flag, never
// the file) and PUTs the full replacement at the addressed scope.
func TestAccessRegistrationSetPromptsForProof(t *testing.T) {
	handler, seen := registrationServer(t)
	ios, stdout, stderr := definitionsTestIO(t, handler)
	prompts := 0
	ios.ReadPassword = func(string) (string, error) { prompts++; return "123456", nil }
	file := writePolicyFile(t, `{"external":[{"provider":{"kind":"oidc","slug":"corp"}}],"landing":{"kind":"fresh-org","cap":100}}`)
	code := cli.Run(t.Context(), ios, []string{"access", "registration", "set", "--instance-scope", "--file", file, "--instance", "local"})
	if code != cli.ExitOK {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	if prompts != 1 {
		t.Fatalf("prompted %d times, want once", prompts)
	}
	if len(*seen) != 1 || !strings.HasPrefix((*seen)[0], "PUT /api/v1/instance/registration-policy ") ||
		!strings.Contains((*seen)[0], `"proof":"123456"`) {
		t.Fatalf("requests = %v", *seen)
	}
	if !strings.Contains(stdout.String(), "12 / 100") || !strings.Contains(stderr.String(), "you are its authority") {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
}

// A proof in the file, an unknown member, and a project address are usage
// errors that reach no server.
func TestAccessRegistrationSetRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		args []string
	}{
		{"proof in file", `{"external":[],"landing":{"kind":"none"},"proof":"hunter2"}`, []string{"--instance-scope"}},
		{"proof in file, capitalised", `{"external":[],"landing":{"kind":"none"},"Proof":"hunter2"}`, []string{"--instance-scope"}},
		{"proof in file, upper case", `{"external":[],"landing":{"kind":"none"},"PROOF":"hunter2"}`, []string{"--instance-scope"}},
		{"unknown member", `{"external":[],"landing":{"kind":"none"},"jit":true}`, []string{"--instance-scope"}},
		{"project address", `{"external":[],"landing":{"kind":"none"}}`, []string{"--org", "org_70", "--project", "prj_70"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, seen := registrationServer(t)
			ios, _, stderr := definitionsTestIO(t, handler)
			ios.ReadPassword = func(string) (string, error) { return "123456", nil }
			args := append([]string{"access", "registration", "set", "--file", writePolicyFile(t, tc.body), "--instance", "local"}, tc.args...)
			if code := cli.Run(t.Context(), ios, args); code != cli.ExitUsage {
				t.Fatalf("exit %d, want usage; stderr=%s", code, stderr)
			}
			if len(*seen) != 0 {
				t.Fatalf("a refused set reached the server: %v", *seen)
			}
		})
	}
}

func TestAccessRegistrationShowAndDelete(t *testing.T) {
	handler, seen := registrationServer(t)
	ios, stdout, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) { return "correct horse", nil }
	if code := cli.Run(t.Context(), ios, []string{"access", "registration", "show", "--org", "org_70", "-o", "json", "--instance", "local"}); code != cli.ExitOK {
		t.Fatalf("show exit %d; stderr=%s", code, stderr)
	}
	var shown apigen.RegistrationPolicy
	if err := json.Unmarshal(stdout.Bytes(), &shown); err != nil || shown.Id != "rpol_1" {
		t.Fatalf("show -o json = %q (%v)", stdout, err)
	}
	if code := cli.Run(t.Context(), ios, []string{"access", "registration", "delete", "--org", "org_70", "--instance", "local"}); code != cli.ExitOK {
		t.Fatalf("delete exit %d; stderr=%s", code, stderr)
	}
	if len(*seen) != 2 || !strings.HasPrefix((*seen)[0], "GET /api/v1/orgs/org_70/registration-policy") ||
		!strings.HasPrefix((*seen)[1], "DELETE /api/v1/orgs/org_70/registration-policy ") ||
		!strings.Contains((*seen)[1], `"proof":"correct horse"`) {
		t.Fatalf("requests = %v", *seen)
	}
}
