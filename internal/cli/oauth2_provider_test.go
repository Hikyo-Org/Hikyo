package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

func TestOAuth2CreateRefusesOverwriteAndOversizedSecret(t *testing.T) {
	for _, tc := range []struct {
		name, secret string
		exists       bool
		want         int
	}{
		{"existing", "fixture", true, ExitUsage},
		{"oversized", strings.Repeat("x", 4097), false, ExitUsage},
		{"limit-newline", strings.Repeat("x", 4096) + "\r\n", false, ExitOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			puts := 0
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == api.PathPrefix+"/meta" {
					_ = json.NewEncoder(w).Encode(apigen.Meta{ServerVersion: "fixture-current", ApiRevision: api.Revision})
					return
				}
				if r.Method == http.MethodGet && !tc.exists {
					w.WriteHeader(404)
					_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"not found"}}`))
					return
				}
				if r.Method == http.MethodPut {
					puts++
				}
				_ = json.NewEncoder(w).Encode(apigen.Oauth2Provider{Slug: "github"})
			}))
			defer srv.Close()
			st, dir := machineState(t, srv.URL, SPKIFingerprint(srv.Certificate()))
			if err := st.PutSession(SessionArtifact{Instance: "local", Origin: srv.URL, Token: "fixture-human", SessionID: "ses_fixture", Principal: "usr_fixture", ExpiresAt: "2030-01-01T00:00:00Z"}); err != nil {
				t.Fatal(err)
			}
			secret := filepath.Join(t.TempDir(), "secret")
			if err := os.WriteFile(secret, []byte(tc.secret), 0600); err != nil {
				t.Fatal(err)
			}
			ios, _, stderr := composeIO(dir, t.TempDir(), "", nil)
			code := Run(t.Context(), ios, []string{"instance-config", "oauth2-provider", "create", "--slug", "github", "--client-id", "fixture", "--client-secret-file", secret, "--instance", "local"})
			if code != tc.want || (puts != 0) != (tc.want == ExitOK) {
				t.Fatalf("code %d puts %d: %s", code, puts, stderr.String())
			}
		})
	}
}
