package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountPasswordAndFactorSocialOnlyRefuseBeforePrompt(t *testing.T) {
	for _, command := range [][]string{
		{"account", "password", "set"},
		{"account", "password", "change"},
		{"account", "factor", "enrol-totp", "--dangerously-print"},
	} {
		t.Run(strings.Join(command, " "), func(t *testing.T) {
			mutations, prompts := 0, 0
			server := newRevisionAwareFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/totp" {
					_, _ = w.Write([]byte(`{"confirmed":false,"pending":false,"has_password":false,"has_passkey":false,"can_reauth_oidc":false}`))
					return
				}
				mutations++
				http.Error(w, "unexpected mutation", http.StatusInternalServerError)
			}))
			defer server.Close()
			_, ios, stdout, stderr := passwordTestIO(t, server.URL, pinnedTestEntry("local", server))
			ios.ReadPassword = func(string) (string, error) { prompts++; return "", errors.New("unexpected prompt") }
			code := Run(t.Context(), ios, append(command, "--instance", "local"))
			if code != ExitRefused || prompts != 0 || mutations != 0 {
				t.Fatalf("exit=%d prompts=%d mutations=%d stderr=%s", code, prompts, mutations, stderr)
			}
			if !strings.Contains(stderr.String(), "social-only account") || !strings.Contains(stderr.String(), server.URL+"/settings?tab=factors") || stdout.Len() != 0 {
				t.Fatalf("stdout=%s stderr=%s", stdout, stderr)
			}
		})
	}
}

func TestAccountPasswordPersistsReplacementWithoutDisclosingSecrets(t *testing.T) {
	for _, verb := range []string{"set", "change"} {
		t.Run(verb, func(t *testing.T) {
			var received struct {
				Password string `json:"password"`
				Proof    string `json:"proof"`
			}
			mutations := 0
			server := newRevisionAwareFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/auth/totp":
					_, _ = w.Write([]byte(`{"confirmed":true,"pending":false,"has_password":true,"has_passkey":false,"can_reauth_oidc":false}`))
				case "/api/v1/auth/password":
					mutations++
					if r.Method != http.MethodPut || r.Header.Get("Authorization") != "Bearer old-session-secret" {
						t.Errorf("request method=%s bearer=%q", r.Method, r.Header.Get("Authorization"))
					}
					if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
						t.Error(err)
					}
					_, _ = w.Write([]byte(`{"session_token":"replacement-session-secret","session":{"id":"ses_new","absolute_expires_at":"2030-01-01T00:00:00Z"}}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			state, ios, stdout, stderr := passwordTestIO(t, server.URL, pinnedTestEntry("local", server))
			inputs := []string{"old-password-proof", "new-password-secret", "new-password-secret"}
			prompts := 0
			ios.ReadPassword = func(prompt string) (string, error) {
				if prompts >= len(inputs) {
					t.Fatalf("unexpected prompt %q", prompt)
				}
				value := inputs[prompts]
				prompts++
				return value, nil
			}
			if code := Run(t.Context(), ios, []string{"account", "password", verb, "--instance", "local"}); code != ExitOK {
				t.Fatalf("exit=%d stderr=%s", code, stderr)
			}
			if mutations != 1 || prompts != 3 || received.Password != inputs[1] || received.Proof != inputs[0] {
				t.Fatalf("mutations=%d prompts=%d password/proof mismatch", mutations, prompts)
			}
			artifacts, err := state.Sessions()
			if err != nil {
				t.Fatal(err)
			}
			if got := artifacts["local"]; got.Token != "replacement-session-secret" || got.SessionID != "ses_new" || got.ExpiresAt != "2030-01-01T00:00:00Z" {
				t.Fatalf("replacement session metadata not persisted")
			}
			for _, secret := range append(inputs, "old-session-secret", "replacement-session-secret") {
				if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
					t.Fatal("secret reached command output")
				}
			}
		})
	}
}

func TestAccountPasswordMismatchDoesNotDispatchMutation(t *testing.T) {
	mutations := 0
	server := newRevisionAwareFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/totp" {
			_, _ = w.Write([]byte(`{"confirmed":false,"pending":false,"has_password":true,"has_passkey":false,"can_reauth_oidc":false}`))
			return
		}
		mutations++
		http.NotFound(w, r)
	}))
	defer server.Close()
	_, ios, _, stderr := passwordTestIO(t, server.URL, pinnedTestEntry("local", server))
	inputs := []string{"proof", "new-password-secret", "different-password"}
	n := 0
	ios.ReadPassword = func(string) (string, error) { v := inputs[n]; n++; return v, nil }
	if code := Run(t.Context(), ios, []string{"account", "password", "change", "--instance", "local"}); code != ExitRefused || mutations != 0 || !strings.Contains(stderr.String(), "do not match") {
		t.Fatalf("exit=%d mutations=%d stderr=%s", code, mutations, stderr)
	}
}

func passwordTestIO(t *testing.T, origin string, entry TrustEntry) (*State, IO, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	st := &State{dir: t.TempDir()}
	if err := st.Trust().Put(entry); err != nil {
		t.Fatal(err)
	}
	if err := st.PutSession(SessionArtifact{Instance: "local", Origin: origin, Token: "old-session-secret", SessionID: "ses_old", Principal: "usr_one", ExpiresAt: "2030-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	ios := IO{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr, Env: Env{Getenv: func(name string) string {
		if name == "HIKYO_STATE_DIR" {
			return st.dir
		}
		return ""
	}}}
	return st, ios, stdout, stderr
}

func TestCLIReauthUnavailableRefusesBeforeBrowser(t *testing.T) {
	starts, opens := 0, 0
	const detail = "this sign-in cannot satisfy step-up: sign in through a provider that asserts a second factor, or add an authenticator or passkey under Settings > Security"
	server := newRevisionAwareFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/cli-reauth/start" {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		starts++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]map[string]string{"error": {"code": "bad_request", "message": "the request does not satisfy the API contract", "detail": detail}})
	}))
	defer server.Close()
	st, _, _, _ := passwordTestIO(t, server.URL, pinnedTestEntry("local", server))
	client, err := NewClient(pinnedTestEntry("local", server), "old-session-secret")
	if err != nil {
		t.Fatal(err)
	}
	err = runCLIAdapterReauth(t.Context(), client, st, SessionArtifact{Instance: "local", Token: "old-session-secret"}, "adapter.sync", []string{"env_one"}, func(string) error { opens++; return nil })
	if err == nil || !strings.Contains(err.Error(), detail) || starts != 1 || opens != 0 {
		t.Fatalf("err=%v starts=%d opens=%d", err, starts, opens)
	}
	sessions, err := st.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if sessions["local"].Token != "old-session-secret" {
		t.Fatal("refused handoff changed the stored session")
	}
}

func TestAccountPasswordOldServerRefusesBeforeInventoryOrPrompt(t *testing.T) {
	for _, command := range [][]string{{"account", "password", "set"}, {"account", "password", "change"}, {"account", "factor", "enrol-totp", "--dangerously-print"}} {
		t.Run(strings.Join(command, " "), func(t *testing.T) {
			reads, prompts := 0, 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/meta" {
					_, _ = w.Write([]byte(`{"server_version":"old-feature-fixture","api_revision":10}`))
					return
				}
				reads++
				http.NotFound(w, r)
			}))
			defer server.Close()
			_, ios, _, stderr := passwordTestIO(t, server.URL, pinnedTestEntry("local", server))
			ios.ReadPassword = func(string) (string, error) { prompts++; return "", errors.New("unexpected prompt") }
			code := Run(t.Context(), ios, append(command, "--instance", "local"))
			if code != ExitRefused || prompts != 0 || reads != 0 || !strings.Contains(stderr.String(), "old-feature-fixture") || !strings.Contains(stderr.String(), "needs revision 11") || strings.Contains(stderr.String(), "social-only account") {
				t.Fatalf("exit=%d prompts=%d reads=%d stderr=%s", code, prompts, reads, stderr)
			}
		})
	}
}

func TestAccountPasswordPasskeyOnlyRequiresSupportedProofBeforePrompt(t *testing.T) {
	prompts, mutations := 0, 0
	server := newRevisionAwareFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/totp" {
			_, _ = w.Write([]byte(`{"confirmed":false,"pending":false,"has_password":false,"has_passkey":true,"can_reauth_oidc":false}`))
			return
		}
		mutations++
		http.NotFound(w, r)
	}))
	defer server.Close()
	_, ios, _, stderr := passwordTestIO(t, server.URL, pinnedTestEntry("local", server))
	ios.ReadPassword = func(string) (string, error) { prompts++; return "", errors.New("unexpected prompt") }
	code := Run(t.Context(), ios, []string{"account", "password", "set", "--instance", "local"})
	if code != ExitRefused || prompts != 0 || mutations != 0 || !strings.Contains(stderr.String(), "password or TOTP proof") || !strings.Contains(stderr.String(), "administrator for credential recovery") || strings.Contains(stderr.String(), "verify with your passkey") {
		t.Fatalf("exit=%d prompts=%d mutations=%d stderr=%s", code, prompts, mutations, stderr)
	}
}
