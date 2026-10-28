package isolation

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The Python helper is shared by rollout and upgrade fixtures. Exercise its
// standard-library HTTP transport against the same production router as CLI.
func TestFixturePythonEnrolledLoginAgainstCurrentServer(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python fixture ceremony requires python3")
	}
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := bootstrapAdmin(t, db, adminOpts{username: "fixture-admin", password: "synthetic fixture login password"})
		clock := time.Now().UTC()
		admin.auth.Now = func() time.Time { return clock }
		initial, err := admin.auth.LocalLogin(t.Context(), "fixture-admin", admin.password, service.ArtifactCLI)
		if err != nil {
			t.Fatal(err)
		}
		uri, err := admin.auth.EnrolTOTPStart(t.Context(), initial.SessionToken, admin.password)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := admin.auth.EnrolTOTPConfirm(t.Context(), initial.SessionToken, totpCode(t, uri, clock)); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(30 * time.Second)
		srv := httptest.NewServer(server.New(&server.API{Auth: admin.auth}, nil))
		defer srv.Close()
		admin.auth.ExternalOrigin = srv.URL
		input, err := json.Marshal(map[string]string{"origin": srv.URL, "username": "fixture-admin", "password": admin.password, "code": totpCode(t, uri, clock)})
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), python, "-c", `import json, runpy, sys
fixture = json.load(sys.stdin)
login = runpy.run_path("../../scripts/ci/fixture_cli_login.py")["login"]
result = login(fixture["origin"], fixture["username"], fixture["password"], lambda: fixture["code"])
json.dump(result, sys.stdout)
`)
		cmd.Stdin = bytes.NewReader(input)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("Python fixture ceremony failed: %v\n%s", err, stderr.String())
		}
		var result struct {
			Token string `json:"session_token"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatal("invalid Python fixture result")
		}
		identity, err := admin.auth.Identity(t.Context(), result.Token)
		if err != nil || identity.Principal != admin.boot.PrincipalID || identity.Artifact != service.ArtifactCLI || len(identity.Assurance.Factors) != 2 {
			t.Fatalf("Python fixture CLI assurance invalid: %v", err)
		}
		if queryInt(t, db, "SELECT COUNT(*) FROM sessions WHERE artifact='browser'") != 0 {
			t.Fatal("Python helper left its browser session live")
		}
		if queryInt(t, db, "SELECT COUNT(*) FROM reauth_windows") != 0 {
			t.Fatal("Python handoff created operation proof")
		}
	})
}
