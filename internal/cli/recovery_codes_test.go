package cli_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
)

type recoveryCodeOutput struct {
	write func([]byte) (int, error)
}

func (w recoveryCodeOutput) Write(p []byte) (int, error) { return w.write(p) }

func TestRecoveryCodesPreserveRotatedSessionBeforeOutput(t *testing.T) {
	for _, failOutput := range []bool{false, true} {
		name := "successful-output"
		if failOutput {
			name = "broken-output"
		}
		t.Run(name, func(t *testing.T) {
			token := "replacement-session-bearer"
			calls := 0
			ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/recovery-codes/regenerate") || r.Method != http.MethodPost {
					http.NotFound(w, r)
					return
				}
				calls++
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("regeneration authority=%q", r.Header.Get("Authorization"))
				}
				_ = json.NewEncoder(w).Encode(apigen.RecoveryCodesResult{
					RecoveryCodes: []string{"new-single-use-code"},
					Login: apigen.LoginResult{SessionToken: &token, Session: apigen.Session{
						Id: "ses_replacement", AbsoluteExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
					}},
				})
			}))
			state, err := cli.NewState(ios.Env)
			if err != nil {
				t.Fatal(err)
			}
			ios.ReadPassword = func(string) (string, error) { return "123456", nil }
			writes := 0
			ios.Stdout = recoveryCodeOutput{write: func(p []byte) (int, error) {
				writes++
				sessions, err := state.Sessions()
				if err != nil || sessions["local"].Token != token || sessions["local"].SessionID != "ses_replacement" {
					t.Errorf("replacement was not durable before output: sessions=%v err=%v", sessions, err)
				}
				if failOutput {
					return 0, errors.New("output pipe closed")
				}
				return len(p), nil
			}}
			code := cli.Run(t.Context(), ios, []string{"account", "recovery-codes", "regenerate", "--instance", "local", "--dangerously-print"})
			want := cli.ExitOK
			if failOutput {
				want = cli.ExitRefused
			}
			if code != want || calls != 1 || writes == 0 {
				t.Fatalf("exit=%d want=%d requests=%d writes=%d stderr=%s", code, want, calls, writes, stderr)
			}
			sessions, err := state.Sessions()
			if err != nil || sessions["local"].Token != token || sessions["local"].Origin == "" || sessions["local"].Principal != "usr_70" {
				t.Fatalf("session lost after output: sessions=%v err=%v", sessions, err)
			}
			if failOutput && (!strings.Contains(stderr.String(), "disclosing the recovery codes") || strings.Contains(stderr.String(), "replacement-session-bearer") || strings.Contains(stderr.String(), "new-single-use-code")) {
				t.Fatalf("unsafe or missing output failure diagnostic: %s", stderr)
			}
		})
	}
}
