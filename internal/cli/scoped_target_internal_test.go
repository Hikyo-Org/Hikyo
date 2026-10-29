package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

func scopedCommandIO(t *testing.T, server *httptest.Server, human bool, token string, extra map[string]string) (*State, IO, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	st, stateDir := machineState(t, server.URL, SPKIFingerprint(server.Certificate()))
	if human {
		if err := st.PutSession(SessionArtifact{
			Instance: "local", Origin: server.URL, Token: "human-token", Principal: "usr_scope",
		}); err != nil {
			t.Fatal(err)
		}
	}
	ios, stdout, stderr := composeIO(stateDir, t.TempDir(), token, extra)
	return st, ios, stdout, stderr
}

// These are command contracts, including the old authenticate/echo/require
// order. Do not move tenant validation ahead of authentication to simplify it.
func TestScopedCommandsAuthenticateBeforeMissingScope(t *testing.T) {
	for _, tc := range []struct {
		command string
		depth   int
	}{
		{"org retention get", 1},
		{"project retention get", 2},
		{"env param list", 3},
		{"ssh-ca list", 3},
		{"ssh-profile list", 3},
		{"ssh-cert list", 3},
		{"adapter target list --adapter adp_one", 2},
		{"adapter target add --adapter adp_one", 3},
		{"adapter target show atg_one", 2},
		{"adapter plan --target atg_one", 2},
		{"adapter sync --target atg_one", 2},
		{"adapter test --target atg_one", 2},
		{"adapter adopt --target atg_one NAME", 2},
	} {
		dimensions := []string{"org", "project", "env"}
		for missing := 0; missing < tc.depth; missing++ {
			for _, human := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/no-%s/human=%t", tc.command, dimensions[missing], human), func(t *testing.T) {
					var calls atomic.Int32
					server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						calls.Add(1)
						http.Error(w, "unexpected request", http.StatusInternalServerError)
					}))
					t.Cleanup(server.Close)
					_, ios, stdout, stderr := scopedCommandIO(t, server, human, "", nil)
					args := append(strings.Fields(tc.command), "--instance", "local")
					for i := 0; i < missing; i++ {
						args = append(args, "--"+dimensions[i], dimensions[i]+"_one")
					}
					code := Run(t.Context(), ios, args)
					if !human {
						if code != ExitAuth || !strings.Contains(stderr.String(), "no human session is available") || strings.Contains(stderr.String(), "target:") {
							t.Fatalf("exit=%d stderr=%q, want auth failure without echo", code, stderr)
						}
					} else {
						wantEcho := "target: instance=local (flag)"
						for i := 0; i < missing; i++ {
							wantEcho += fmt.Sprintf(" %s=%s_one (flag)", dimensions[i], dimensions[i])
						}
						wantEcho += fmt.Sprintf(" [origin %s, artifact human-session usr_scope]\n", server.URL)
						wantError := "hikyo: no " + dimensions[missing] + " resolved."
						if code != ExitUsage || !strings.HasPrefix(stderr.String(), wantEcho+wantError) || strings.Count(stderr.String(), "target:") != 1 {
							t.Fatalf("exit=%d stderr=%q, want echo then %q", code, stderr, wantError)
						}
					}
					if calls.Load() != 0 || stdout.Len() != 0 {
						t.Fatalf("refused command made %d requests and wrote stdout=%q", calls.Load(), stdout)
					}
				})
			}
		}
	}
}

func TestScopedCommandsPreserveUsagePrecedence(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    string
		code    int
	}{
		{"env param list stray", "takes no positional arguments", ExitUsage},
		{"env param add --name bad", "name", ExitUsage},
		{"org retention get stray", "takes no positional arguments", ExitUsage},
		{"project retention set --inherit --max-age 1h", "--inherit cannot be combined", ExitUsage},
		{"adapter target add", "requires --adapter", ExitUsage},
		{"adapter target show", "takes one positional target", ExitUsage},
		{"adapter plan", "requires --target", ExitUsage},
		{"adapter adopt --target atg_one NAME NAME", "names must be unique", ExitUsage},
		// SSH keeps its established later positional checks.
		{"ssh-ca list stray", "no human session is available", ExitAuth},
		{"ssh-profile list stray", "no human session is available", ExitAuth},
		{"ssh-cert list stray", "no human session is available", ExitAuth},
	} {
		t.Run(tc.command, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				http.Error(w, "unexpected request", http.StatusInternalServerError)
			}))
			t.Cleanup(server.Close)
			_, ios, stdout, stderr := scopedCommandIO(t, server, false, "", nil)
			args := append(strings.Fields(tc.command), "--instance", "local")
			if code := Run(t.Context(), ios, args); code != tc.code || !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("exit=%d stderr=%q, want exit=%d containing %q", code, stderr, tc.code, tc.want)
			}
			if calls.Load() != 0 || stdout.Len() != 0 || strings.Contains(stderr.String(), "target:") {
				t.Fatalf("invalid command acted or echoed: requests=%d stdout=%q stderr=%q", calls.Load(), stdout, stderr)
			}
		})
	}
}

func TestScopedCommandsPreparedPaths(t *testing.T) {
	const orgBase = "/api/v1/orgs/org%20%3F%23%25"
	const projectBase = orgBase + "/projects/project%20%3F%23%25"
	const envBase = projectBase + "/environments/env%20%3F%23%25"
	for _, tc := range []struct {
		command, method, path, response string
	}{
		{"org retention get", "GET", orgBase + "/retention", `{"mode":"unlimited"}`},
		{"org retention set --unlimited", "PUT", orgBase + "/retention", `{"mode":"unlimited"}`},
		{"project retention get", "GET", projectBase + "/retention", `{"mode":"unlimited","inherited":true}`},
		{"project retention set --inherit", "PUT", projectBase + "/retention", `{"mode":"unlimited","inherited":true}`},
		{"env param list", "GET", envBase + "/parameters", `{"NAME":".*"}`},
		{"env param add --name NAME --pattern .*", "POST", envBase + "/parameters", ``},
		{"env param delete --name NAME", "POST", envBase + "/parameters", ``},
		{"ssh-ca list", "GET", envBase + "/ssh-cas", `{"items":[]}`},
		{"ssh-profile list", "GET", envBase + "/ssh-profiles", `{"items":[]}`},
		{"ssh-cert list", "GET", envBase + "/ssh-certificates", `{"items":[]}`},
		{"adapter target list --adapter adp_one", "GET", projectBase + "/adapters/adp_one/targets", `{"items":[]}`},
		{"adapter target show atg_one", "GET", projectBase + "/adapter-targets/atg_one", `{"target":{"id":"atg_one"}}`},
		{"adapter plan --target atg_one", "POST", projectBase + "/adapter-targets/atg_one/plan", `{"changes":[],"warnings":[]}`},
		{"adapter test --target atg_one", "POST", projectBase + "/adapter-targets/atg_one/test", `{"version":"fixture"}`},
	} {
		t.Run(tc.command, func(t *testing.T) {
			requests := make(chan string, 2)
			server := newRevisionAwareFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.Method + " " + r.URL.EscapedPath()
				if r.Header.Get("Authorization") != "Bearer human-token" {
					t.Error("request lost the human session")
				}
				if r.URL.RawQuery != "" {
					t.Errorf("selector escaped into query: %s", r.URL.RawQuery)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.response))
			}))
			t.Cleanup(server.Close)
			_, ios, stdout, stderr := scopedCommandIO(t, server, true, "", nil)
			args := append(strings.Fields(tc.command), "--instance", "local", "--org", "org ?#%", "--project", "project ?#%", "--env", "env ?#%", "-o", "json")
			if code := Run(t.Context(), ios, args); code != ExitOK {
				t.Fatalf("exit=%d stderr=%q", code, stderr)
			}
			if len(requests) != 1 {
				t.Fatalf("requests=%d, want 1", len(requests))
			}
			if got := <-requests; got != tc.method+" "+tc.path {
				t.Fatalf("request=%q, want %q", got, tc.method+" "+tc.path)
			}
			wantEcho := fmt.Sprintf("target: instance=local (flag) org=org ?#%% (flag) project=project ?#%% (flag) env=env ?#%% (flag) [origin %s, artifact human-session usr_scope]\n", server.URL)
			if stderr.String() != wantEcho || strings.Contains(stdout.String(), "target:") {
				t.Fatalf("echo changed: stdout=%q stderr=%q, want stderr=%q", stdout, stderr, wantEcho)
			}
			if tc.response != "" && !json.Valid(stdout.Bytes()) {
				t.Fatalf("output is not parseable JSON: %q", stdout)
			}
		})
	}
}

func TestScopedCommandsArtifactSelection(t *testing.T) {
	for _, tc := range []struct {
		name, command, choice, token, sessionOrigin, bearer, want string
		human, missingScope                                       bool
		code                                                      int
	}{
		{name: "parameters ambiguity", command: "env param list", human: true, token: "machine-token", code: ExitRefused, want: "both a stored human session and a machine credential"},
		{name: "parameters ambiguity before scope", command: "env param list", human: true, token: "machine-token", missingScope: true, code: ExitRefused, want: "both a stored human session and a machine credential"},
		{name: "SSH ambiguity", command: "ssh-ca list", human: true, token: "machine-token", code: ExitRefused, want: "both a stored human session and a machine credential"},
		{name: "human choice ignores unreadable token file", command: "env param list --token-file missing-token", choice: "human", human: true, bearer: "human-token"},
		{name: "human-only retention ignores ambient machine", command: "org retention get --token-file missing-token", human: true, token: "machine-token", bearer: "human-token"},
		{name: "human-only adapters ignore ambient machine", command: "adapter target list --adapter adp_one", human: true, token: "machine-token", bearer: "human-token"},
		{name: "machine parameters", command: "env param list", token: "machine-token", bearer: "machine-token"},
		{name: "machine SSH", command: "ssh-cert list", token: "machine-token", bearer: "machine-token"},
		{name: "machine choice ignores stale human origin", command: "env param list", choice: "machine", human: true, sessionOrigin: "https://previous.example", token: "machine-token", bearer: "machine-token"},
		{name: "explicit machine cannot administer", command: "project retention get", choice: "machine", human: true, token: "machine-token", code: ExitRefused, want: "requires a human session"},
		{name: "machine cannot add adapter targets", command: "adapter target add --adapter adp_one", token: "machine-token", code: ExitRefused, want: "requires a human session"},
		{name: "origin mismatch before scope", command: "env param list", human: true, sessionOrigin: "https://previous.example", missingScope: true, code: ExitRefused, want: "the stored session for \"local\" was established against https://previous.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer "+tc.bearer {
					t.Errorf("authorization=%q, want selected bearer", r.Header.Get("Authorization"))
				}
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"selected artifact reached server"}}`))
			}))
			t.Cleanup(server.Close)
			st, ios, stdout, stderr := scopedCommandIO(t, server, tc.human, tc.token, nil)
			if tc.sessionOrigin != "" {
				if err := st.PutSession(SessionArtifact{Instance: "local", Origin: tc.sessionOrigin, Token: "human-token", Principal: "usr_scope"}); err != nil {
					t.Fatal(err)
				}
			}
			args := append(strings.Fields(tc.command), "--instance", "local")
			if !tc.missingScope {
				args = append(args, "--org", "org_one", "--project", "prj_one", "--env", "env_one")
			}
			if tc.choice != "" {
				args = append(args, "--auth", tc.choice)
			}
			code := Run(t.Context(), ios, args)
			if tc.bearer == "" {
				if code != tc.code || calls.Load() != 0 || !strings.Contains(stderr.String(), tc.want) || strings.Contains(stderr.String(), "target:") {
					t.Fatalf("exit=%d requests=%d stderr=%q, want exit=%d containing %q before network or echo", code, calls.Load(), stderr, tc.code, tc.want)
				}
			} else {
				if code != ExitNotFound || calls.Load() != 1 || !strings.Contains(stderr.String(), "selected artifact reached server") {
					t.Fatalf("exit=%d requests=%d stderr=%q, want selected artifact to reach discovery", code, calls.Load(), stderr)
				}
				artifact := "human-session usr_scope"
				if tc.bearer == "machine-token" {
					artifact = "machine-credential"
				}
				wantEcho := fmt.Sprintf("target: instance=local (flag) org=org_one (flag) project=prj_one (flag) env=env_one (flag) [origin %s, artifact %s]\n", server.URL, artifact)
				if !strings.HasPrefix(stderr.String(), wantEcho) || strings.Count(stderr.String(), "target:") != 1 {
					t.Fatalf("selection echo changed: %q", stderr)
				}
			}
			if stdout.Len() != 0 || strings.Contains(stderr.String(), "human-token") || strings.Contains(stderr.String(), "machine-token") {
				t.Fatalf("artifact plaintext or non-result output leaked: stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
}

func TestScopedAdapterAddKeepsProjectPathAndEnvironmentBody(t *testing.T) {
	requests := make(chan string, 3)
	server := newRevisionAwareFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.URL.EscapedPath()
		if r.Header.Get("Authorization") != "Bearer human-token" {
			t.Error("adapter target lost its human session")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"provider":"forgejo"}`))
			return
		}
		var body struct {
			Environment string `json:"environment_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("invalid adapter target body: %v", err)
		}
		if body.Environment != "env ?#%" {
			t.Errorf("environment=%q, want the unescaped resolved selector", body.Environment)
		}
		_, _ = w.Write([]byte(`{"id":"atg_one","environment_id":"env ?#%"}`))
	}))
	t.Cleanup(server.Close)
	_, ios, stdout, stderr := scopedCommandIO(t, server, true, "", nil)
	// This uses the existing no-browser ceremony seam, without changing the
	// human-only artifact requirement or adding authentication test machinery.
	args := []string{"adapter", "target", "add", "--adapter", "adp_one", "--kind", "repository", "--owner", "team", "--repo", "app", "--keys", "key_one",
		"--instance", "local", "--org", "org_one", "--project", "prj_one", "--env", "env ?#%", "-o", "json"}
	if code := Run(t.Context(), ios, args); code != ExitOK || len(requests) != 2 {
		t.Fatalf("exit=%d requests=%d stdout=%q stderr=%q", code, len(requests), stdout, stderr)
	}
	for _, want := range []string{
		"GET /api/v1/orgs/org_one/projects/prj_one/adapters/adp_one",
		"POST /api/v1/orgs/org_one/projects/prj_one/adapters/adp_one/targets",
	} {
		if got := <-requests; got != want {
			t.Fatalf("request=%q, want %q", got, want)
		}
	}
	if !strings.Contains(stdout.String(), `"atg_one"`) || strings.Count(stderr.String(), "target:") != 1 {
		t.Fatalf("target response or echo changed: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestScopedCommandsKeepTrustFallback(t *testing.T) {
	for _, tc := range []struct {
		name        string
		human, many bool
		code        int
		want        string
	}{
		{"one trusted instance", true, false, ExitOK, "artifact human-session usr_scope"},
		{"logged out", false, false, ExitAuth, "no human session is available"},
		{"ambiguous instances", true, true, ExitUsage, "no instance resolved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := newRevisionAwareFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(server.Close)
			st, ios, _, stderr := scopedCommandIO(t, server, tc.human, "", nil)
			if tc.many {
				entry := pinnedTestEntry("other", server)
				if err := st.Trust().Put(entry); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"env", "param", "list", "--org", "org_one", "--project", "prj_one", "--env", "env_one"}
			if code := Run(t.Context(), ios, args); code != tc.code || !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("exit=%d stderr=%q, want exit=%d containing %q", code, stderr, tc.code, tc.want)
			}
			if tc.code == ExitOK {
				wantEcho := fmt.Sprintf("target: org=org_one (flag) project=prj_one (flag) env=env_one (flag) [origin %s, artifact human-session usr_scope]\n", server.URL)
				if calls.Load() != 1 || stderr.String() != wantEcho {
					t.Fatalf("fallback changed target echo: requests=%d stderr=%q", calls.Load(), stderr)
				}
			} else if calls.Load() != 0 || strings.Contains(stderr.String(), "target:") {
				t.Fatalf("unprepared fallback acted: requests=%d stderr=%q", calls.Load(), stderr)
			}
		})
	}
}

func TestScopedSSHPositionalsFollowPreparation(t *testing.T) {
	for _, command := range []string{"ssh-ca list", "ssh-profile list", "ssh-cert list"} {
		t.Run(command, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				http.Error(w, "unexpected request", http.StatusInternalServerError)
			}))
			t.Cleanup(server.Close)
			_, ios, _, stderr := scopedCommandIO(t, server, true, "", nil)
			args := append(strings.Fields(command), "stray", "--instance", "local", "--org", "org_one", "--project", "prj_one")
			if code := Run(t.Context(), ios, args); code != ExitUsage || !strings.Contains(stderr.String(), "no env resolved") || strings.Contains(stderr.String(), "takes no positional") {
				t.Fatalf("missing scope must precede SSH positional validation: exit=%d stderr=%q", code, stderr)
			}
			stderr.Reset()
			args = append(args, "--env", "env_one")
			if code := Run(t.Context(), ios, args); code != ExitUsage || !strings.Contains(stderr.String(), "takes no positional arguments") || !strings.HasPrefix(stderr.String(), "target: ") {
				t.Fatalf("fully prepared SSH command must check positionals after echo: exit=%d stderr=%q", code, stderr)
			}
			if calls.Load() != 0 {
				t.Fatalf("invalid SSH command made %d requests", calls.Load())
			}
		})
	}
}

func TestScopedCommandsDoNotSelectStoredDeveloperCredentials(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	st, ios, _, stderr := scopedCommandIO(t, server, false, "", nil)
	token, _, err := crypto.NewArtifact(crypto.ArtifactDeveloperCredential)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutDeveloperCredential(DeveloperCredentialArtifact{
		ID: "dev_one", Origin: server.URL, Org: "org_one", Project: "prj_one", Environment: "env_one",
		Token: token, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	args := []string{"env", "param", "list", "--org", "org_one", "--project", "prj_one", "--env", "env_one"}
	if code := Run(t.Context(), ios, args); code != ExitAuth || calls.Load() != 0 || !strings.Contains(stderr.String(), "no human session is available") || strings.Contains(stderr.String(), token) {
		t.Fatalf("stored developer credential changed parameter selection: exit=%d requests=%d stderr=%q", code, calls.Load(), stderr)
	}
}

func TestScopedCommandsResolutionAndTupleRefusal(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		t.Run(fmt.Sprintf("refuse=%t", refuse), func(t *testing.T) {
			requests := make(chan string, 2)
			server := newRevisionAwareFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.Method + " " + r.URL.EscapedPath()
				w.Header().Set("Content-Type", "application/json")
				if refuse {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":{"code":"bad_request","message":"environment does not belong to this project"}}`))
					return
				}
				_, _ = w.Write([]byte(`{"NAME":".*"}`))
			}))
			t.Cleanup(server.Close)
			st, ios, stdout, stderr := scopedCommandIO(t, server, true, "", map[string]string{"HIKYO_ORG": "org_environment"})
			if err := st.PutContext(Context{Name: "dev", Instance: "local", Org: "org_context", Project: "prj_context", Env: "env_context"}); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ios.Workdir, PinFileName), []byte(`{"org":"org_pin","project":"prj_pin","env":"env_pin"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"env", "param", "list", "--context", "dev", "--env", "env_flag", "-o", "json"}
			wantCode := ExitOK
			if refuse {
				wantCode = ExitRefused
			}
			if code := Run(t.Context(), ios, args); code != wantCode || len(requests) != 1 {
				t.Fatalf("exit=%d requests=%d stderr=%q, want exit=%d and one request", code, len(requests), stderr, wantCode)
			}
			if got := <-requests; got != "GET /api/v1/orgs/org_environment/projects/prj_pin/environments/env_flag/parameters" {
				t.Fatalf("resolved tuple changed: %q", got)
			}
			wantEcho := fmt.Sprintf("target: instance=local (context) org=org_environment (environment) project=prj_pin (pin file) env=env_flag (flag) [origin %s, artifact human-session usr_scope]\n", server.URL)
			if !strings.HasPrefix(stderr.String(), wantEcho) || strings.Count(stderr.String(), "target:") != 1 {
				t.Fatalf("resolved provenance changed: %q", stderr)
			}
			if refuse && (stdout.Len() != 0 || !strings.Contains(stderr.String(), "environment does not belong to this project")) {
				t.Fatalf("tuple refusal was hidden or rendered as success: stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
}
