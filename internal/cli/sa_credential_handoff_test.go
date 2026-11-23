package cli_test

import (
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

func mintHandoffArgs() []string {
	return []string{"sa", "credential", "mint", "--sa", "sa_1", "--lifetime", "720h", "--dangerously-print",
		"--instance", "local", "--org", "org_70", "--project", "prj_70"}
}

// mintHandoffServer refuses the mint naming env_70 until the browser handoff
// was redeemed, after which the rotated bearer is the only one it accepts.
func mintHandoffServer(t *testing.T, window apigen.RevealWindow, startBody *map[string]any, callbackURI *string) http.Handler {
	t.Helper()
	const rotated = "hik_1_cli_mint-rotated"
	redeemed := false
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/reveal-window"):
			_ = jsonwire.Encode(w, window)
		case strings.HasSuffix(r.URL.Path, "/auth/cli-reauth/start"):
			_ = json.UnmarshalRead(r.Body, startBody)
			*callbackURI, _ = (*startBody)["redirect_uri"].(string)
			_, _ = w.Write([]byte(`{"state":"hik_1_hs_mint","expires_at":"2030-01-01T00:00:00Z"}`))
		case strings.HasSuffix(r.URL.Path, "/auth/cli-reauth/redeem"):
			redeemed = true
			_, _ = w.Write([]byte(`{"session_id":"ses_mint","session_token":"` + rotated + `","windows":[{"environment_id":"env_70","session_id":"ses_mint","single_decision":true,"window_expires":"2030-01-01T00:00:00Z"}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/service-accounts/sa_1/credentials"):
			if !redeemed {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":{"code":"conflict","message":"the current state of this resource refuses the request",` +
					`"detail":"reauthenticate over the environments this operation makes reachable, then retry (env_70)"}}`))
				return
			}
			if !strings.Contains(r.Header.Get("Authorization"), rotated) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"code":"unauthenticated","message":"authentication required"}}`))
				return
			}
			expires := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
			hint := "hik_1_au_ab"
			w.WriteHeader(http.StatusCreated)
			_ = jsonwire.Encode(w, apigen.MintCredentialResult{
				Value: "hik_1_au_minted-secret", ExpiresAt: &expires,
				Credential: apigen.MachineCredential{Id: "mcr_new", Kind: apigen.HikyoToken, Lifetime: apigen.Finite, ExpiresAt: &expires, PrefixHint: &hint},
			})
		default:
			http.NotFound(w, r)
		}
	})
}

// Issue #1516: a mint that cannot be satisfied by a code at the terminal
// (window 0 here) hands off to the browser's passkey ceremony, over its one
// environment and with no key ids, then retries under the rotated bearer.
func TestServiceAccountMintHandsOffToTheBrowserAtZeroWindow(t *testing.T) {
	var startBody map[string]any
	var callbackURI string
	handler := mintHandoffServer(t, apigen.RevealWindow{CanReveal: true, Protected: true, EffectiveWindowSeconds: 0}, &startBody, &callbackURI)
	ios, stdout, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) { t.Fatal("a 0-window handoff must not prompt for a code"); return "", nil }
	ios.OpenURL = func(browserURL string) error {
		if !strings.Contains(browserURL, "/reauth/cli?transaction=hik_1_hs_mint") {
			t.Errorf("browser opened at %q", browserURL)
		}
		go func() {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, callbackURI+"?code=hik_1_hc_mint&state=hik_1_hs_mint", nil)
			if err != nil {
				t.Error(err)
				return
			}
			if resp, err := http.DefaultClient.Do(req); err == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}
	if code := cli.Run(t.Context(), ios, mintHandoffArgs()); code != cli.ExitOK {
		t.Fatalf("exit %d, want ok; stderr=%s", code, stderr.String())
	}
	if startBody["purpose"] != "mint" || startBody["operation"] != "identity.credential-mint" {
		t.Fatalf("handoff started with purpose=%v operation=%v", startBody["purpose"], startBody["operation"])
	}
	if envs, _ := startBody["environment_ids"].([]any); len(envs) != 1 || envs[0] != "env_70" {
		t.Fatalf("handoff environments = %v, want exactly env_70", startBody["environment_ids"])
	}
	if keys, present := startBody["key_ids"]; present {
		if list, _ := keys.([]any); len(list) != 0 {
			t.Fatalf("a mint handoff carries no key ids, got %v", keys)
		}
	}
	if !strings.Contains(stderr.String(), "credential mint over env_70") {
		t.Errorf("the handoff was not announced: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "hik_1_au_minted-secret") {
		t.Fatalf("the mint did not run under the redeemed bearer: %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "hik_1_cli_mint-rotated") || strings.Contains(stderr.String(), "hik_1_cli_mint-rotated") {
		t.Fatal("the rotated bearer reached command output")
	}
}

// With no way to open a browser the dead end stays, and names the way out.
func TestServiceAccountMintWithoutABrowserNamesTheMachineAccessPage(t *testing.T) {
	var startBody map[string]any
	var callbackURI string
	handler := mintHandoffServer(t, apigen.RevealWindow{CanReveal: true, Protected: true, EffectiveWindowSeconds: 0}, &startBody, &callbackURI)
	ios, _, stderr := definitionsTestIO(t, handler)
	ios.OpenURL = nil
	if code := cli.Run(t.Context(), ios, mintHandoffArgs()); code != cli.ExitAuth {
		t.Fatalf("exit %d, want auth; stderr=%s", code, stderr.String())
	}
	if startBody != nil {
		t.Fatalf("a handoff started with no browser to open: %v", startBody)
	}
	if !strings.Contains(stderr.String(), "Machine access page") {
		t.Errorf("the browser path was not named: %s", stderr.String())
	}
}
