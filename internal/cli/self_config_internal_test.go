package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSelfConfigApplyExposesGeneratedRetryKeyBeforePreparation(t *testing.T) {
	for _, failure := range []string{"prepare-error", "prepare-incomplete", "browser-error"} {
		t.Run(failure, func(t *testing.T) {
			var stderr bytes.Buffer
			var dispatchedKey, announcedBeforeDispatch string
			server := newRevisionAwareFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/instance/config":
					_, _ = io.WriteString(w, `{"owner_instance_id":"inst_one","managed":true,"generation":1,"binding":{"schema_version":1}}`)
				case "/api/v1/instance/config/apply":
					var request struct {
						IdempotencyKey string `json:"idempotency_key"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					dispatchedKey = request.IdempotencyKey
					announcedBeforeDispatch = stderr.String()
					if failure == "prepare-error" {
						w.WriteHeader(http.StatusConflict)
						_, _ = io.WriteString(w, `{"code":"conflict"}`)
					} else if failure == "prepare-incomplete" {
						_, _ = io.WriteString(w, `{"job":{"prepared":false}}`)
					} else {
						_, _ = io.WriteString(w, `{"job":{"prepared":true}}`)
					}
				case "/api/v1/auth/cli-reauth/start":
					_, _ = io.WriteString(w, `{"state":"hik_1_hs_retry","expires_at":"2030-01-01T00:00:00Z"}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			stateDir := t.TempDir()
			state := &State{dir: stateDir}
			if err := state.Trust().Put(pinnedTestEntry("local", server)); err != nil {
				t.Fatal(err)
			}
			if err := state.PutSession(SessionArtifact{Instance: "local", Origin: server.URL, Token: "session-secret", SessionID: "ses_one", Principal: "usr_one", ExpiresAt: "2030-01-01T00:00:00Z"}); err != nil {
				t.Fatal(err)
			}
			ios := IO{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: &stderr, Env: Env{Getenv: func(name string) string {
				if name == "HIKYO_STATE_DIR" {
					return stateDir
				}
				return ""
			}}, OpenURL: func(string) error { return errors.New("browser unavailable") }}
			err := runSelfConfig(t.Context(), ios, "apply", []string{"--instance", "local", "--revision", "2", "--expected-generation", "1"})
			if err == nil || dispatchedKey == "" || !strings.Contains(announcedBeforeDispatch, "--idempotency-key "+dispatchedKey) {
				t.Fatalf("retry key unavailable before durable preparation: key=%q err=%v stderr=%s", dispatchedKey, err, stderr.String())
			}
		})
	}
}
