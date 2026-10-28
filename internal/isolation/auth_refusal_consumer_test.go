package isolation

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/audit"
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

func TestRequestRefusalCanonicalAdmissionSignal(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		// The existing developer seam is present independently of #1040.
		limiter, err := admission.New(admission.Config{ArgonMemoryKiB: 64 * 1024, Now: func() time.Time { return time.Date(2026, 10, 28, 12, 0, 30, 0, time.UTC) }})
		if err != nil {
			t.Fatal(err)
		}
		for range admission.MetaPerIPPerMinute - 1 {
			if err := limiter.AdmitDiscovery("192.0.2.103"); err != nil {
				t.Fatal(err)
			}
		}
		auth := authService(t, db)
		signals := service.NewRefusalSignals(db)
		h := server.New(&server.API{Auth: auth, Settings: &service.ProjectSettings{DB: db, Auth: auth}, Admission: limiter, Refusals: signals}, nil)
		token, _, err := crypto.NewArtifact(crypto.ArtifactDeveloper)
		if err != nil {
			t.Fatal(err)
		}
		path := api.PathPrefix + "/orgs/org_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fee/projects/prj_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fdd/environments/env_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fcc/settings"
		for i := 0; i < 2; i++ {
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.RemoteAddr = "192.0.2.103:1234"
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			want := http.StatusUnauthorized
			if i == 1 {
				want = http.StatusTooManyRequests
			}
			if w.Code != want || i == 1 && w.Header().Get("Retry-After") == "" {
				t.Fatalf("canonical source allowance was charged again or overload changed: attempt=%d status=%d", i, w.Code)
			}
		}
		var state audit.RefusalWindow
		if err := json.Unmarshal([]byte(queryString(t, db, "SELECT state FROM auth_refusal_window WHERE id=1")), &state); err != nil {
			t.Fatal(err)
		}
		if state.Counts[4] != 1 || len(state.Samples) != 1 || state.Samples[0].Stage != audit.RefusalAdmission || queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.artifact_class_refused'") != 1 {
			t.Fatal("admission refusal resolved credentials or duplicated its signal")
		}
	})
}
