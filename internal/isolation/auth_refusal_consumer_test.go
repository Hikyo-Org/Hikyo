package isolation

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// This consumer belongs to the producer. #1035 owns the reader implementation.
// Run it on the combined source before claiming supported-interface acceptance.
func TestRequestRefusalSupportedInstanceConsumers(t *testing.T) {
	match, err := api.MatchRequest(httptest.NewRequest(http.MethodGet, api.PathPrefix+"/instance/audit", nil))
	if err != nil || match.Operation().ID != "queryInstanceAudit" {
		t.Skip("NOTRUN: #1035 instance reader is absent; run on the pinned combined source")
	}
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		auth := authService(t, db)
		execRaw(t, db, "INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_refusal_reader','usr_ident','audit-read',NULL,NULL,NULL,"+ts+")")
		seedOrigins(t, db)
		operator := refusalSession(t, db, crypto.ArtifactCLISession, "live")
		browser := refusalSession(t, db, crypto.ArtifactBrowserSession, "live")
		revoked := refusalSession(t, db, crypto.ArtifactCLISession, "revoked")
		signals := service.NewRefusalSignals(db)
		srv := httptest.NewTLSServer(server.New(&server.API{Auth: auth, Audits: &service.Audits{DB: db}, Refusals: signals}, nil))
		t.Cleanup(srv.Close)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+api.PathPrefix+"/auth/whoami", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+revoked)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		requestID := resp.Header.Get(server.RefusalRequestIDHeader)
		if resp.StatusCode != http.StatusUnauthorized || requestID == "" {
			t.Fatal("revoked replay did not produce the uniform correlated refusal")
		}
		expireRefusalWindow(t, db)
		if err := signals.Seal(t.Context()); err != nil {
			t.Fatal(err)
		}
		// Exercise the same cookie-authenticated endpoint the browser reads.
		req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+api.PathPrefix+"/instance/audit?operation=auth.request_refused", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: browser})
		resp, err = srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(requestID)) || !bytes.Contains(body, []byte("127.0.0.1")) {
			t.Fatalf("browser reader lost retained correlation: status=%d err=%v", resp.StatusCode, err)
		}
		stateDir := t.TempDir()
		var stdout, stderr bytes.Buffer
		ios := cli.IO{Stdout: &stdout, Stderr: &stderr, Workdir: t.TempDir(), Env: cli.Env{Getenv: func(key string) string {
			if key == "HIKYO_STATE_DIR" {
				return stateDir
			}
			return ""
		}}}
		state, err := cli.NewState(ios.Env)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.Trust().Put(cli.TrustEntry{Name: "refusal-fixture", Origin: srv.URL, SPKIPin: cli.SPKIFingerprint(srv.Certificate())}); err != nil {
			t.Fatal(err)
		}
		if err := state.PutSession(cli.SessionArtifact{Instance: "refusal-fixture", Origin: srv.URL, Token: operator, SessionID: "fixture-cli", Principal: "usr_ident", ExpiresAt: "2030-01-01T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
		for _, verb := range []string{"query", "export"} {
			stdout.Reset()
			stderr.Reset()
			args := []string{"audit", verb, "--instance", "refusal-fixture", "--instance-scope", "--operation", "auth.request_refused"}
			if verb == "query" {
				args = append(args, "-o", "json")
			}
			if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK || !strings.Contains(stdout.String(), requestID) || strings.Contains(stdout.String()+stderr.String(), revoked) {
				t.Fatalf("CLI %s failed retained-correlation proof: code=%d stderr=%s", verb, code, stderr.String())
			}
		}
	})
}
