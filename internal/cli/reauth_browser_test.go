package cli_test

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

// noTOTPLookup fails the test if a browser-mode ceremony asks whether an
// authenticator is enrolled: a chosen browser needs no such lookup.
func noTOTPLookup(t *testing.T, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/totp") {
			t.Error("browser mode asked whether an authenticator is enrolled")
		}
		next.ServeHTTP(w, r)
	})
}

func emergencyArgs(extra ...string) []string {
	return append([]string{"access", "request", "emergency", "--capability", "read", "--reason", "incident 42",
		"--instance", "local", "--org", "org_70", "--project", "prj_70", "--env", "env_70"}, extra...)
}

// accessHandoffServer refuses the emergency request until the browser handoff
// was redeemed, after which the rotated bearer is the only one it accepts. The
// inline TOTP route fails the test: the cases below must not reach it.
func accessHandoffServer(t *testing.T, window apigen.RevealWindow, totpEnrolled bool, startBody *map[string]any, callbackURI *string) http.Handler {
	t.Helper()
	const rotated = "hik_1_cli_access-rotated"
	redeemed := false
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/reveal-window"):
			_ = jsonwire.Encode(w, window)
		case strings.HasSuffix(r.URL.Path, "/auth/totp"):
			_ = jsonwire.Encode(w, apigen.TotpStatus{Confirmed: totpEnrolled})
		case strings.HasSuffix(r.URL.Path, "/auth/reauth/totp"):
			t.Error("the browser ceremony fell back to an inline code")
			w.WriteHeader(http.StatusUnauthorized)
		case strings.HasSuffix(r.URL.Path, "/auth/cli-reauth/start"):
			_ = json.UnmarshalRead(r.Body, startBody)
			*callbackURI, _ = (*startBody)["redirect_uri"].(string)
			_, _ = w.Write([]byte(`{"state":"hik_1_hs_access","expires_at":"2030-01-01T00:00:00Z"}`))
		case strings.HasSuffix(r.URL.Path, "/auth/cli-reauth/redeem"):
			redeemed = true
			_, _ = w.Write([]byte(`{"session_id":"ses_access","session_token":"` + rotated + `","windows":[{"environment_id":"env_70","session_id":"ses_access","single_decision":true,"window_expires":"2030-01-01T00:00:00Z"}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/access-requests/emergency"):
			if !redeemed {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"not permitted"}}`))
				return
			}
			if !strings.Contains(r.Header.Get("Authorization"), rotated) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"code":"unauthenticated","message":"authentication required"}}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = jsonwire.Encode(w, apigen.AccessRequest{Id: "xreq_granted", Requester: "usr_70", State: "granted", Bypassed: true,
				Capabilities: []apigen.AccessCapability{"read"}})
		default:
			http.NotFound(w, r)
		}
	})
}

// openAndApprove is the browser: it opens the approval page and answers the
// loopback callback the way the server's redirect does.
func openAndApprove(t *testing.T, callbackURI *string) func(string) error {
	return func(browserURL string) error {
		if !strings.Contains(browserURL, "/reauth/cli?transaction=hik_1_hs_access") {
			t.Errorf("browser opened at %q", browserURL)
		}
		go func() {
			resp, _ := http.Get(*callbackURI + "?code=hik_1_hc_access&state=hik_1_hs_access")
			if resp != nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}
}

func assertAccessHandoff(t *testing.T, startBody map[string]any) {
	t.Helper()
	if startBody["purpose"] != "access" || startBody["operation"] != "access.bypass" {
		t.Fatalf("handoff started with purpose=%v operation=%v", startBody["purpose"], startBody["operation"])
	}
	if envs, _ := startBody["environment_ids"].([]any); len(envs) != 1 || envs[0] != "env_70" {
		t.Fatalf("handoff environments = %v, want exactly env_70", startBody["environment_ids"])
	}
	if keys, present := startBody["key_ids"]; present {
		if list, _ := keys.([]any); len(list) != 0 {
			t.Fatalf("an access handoff carries no key ids, got %v", keys)
		}
	}
}

// Issue #1531: emergency access at a 0-window environment hands off to the
// browser over its one environment and no keys, then retries.
func TestEmergencyAccessHandsOffToTheBrowserAtZeroWindow(t *testing.T) {
	var startBody map[string]any
	var callbackURI string
	handler := accessHandoffServer(t, apigen.RevealWindow{Protected: true, EffectiveWindowSeconds: 0}, false, &startBody, &callbackURI)
	ios, stdout, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) { t.Fatal("a 0-window handoff must not prompt for a code"); return "", nil }
	ios.OpenURL = openAndApprove(t, &callbackURI)
	if code := cli.Run(t.Context(), ios, emergencyArgs()); code != cli.ExitOK {
		t.Fatalf("exit %d, want ok; stderr=%s", code, stderr.String())
	}
	assertAccessHandoff(t, startBody)
	if !strings.Contains(stderr.String(), "emergency access in env_70") || !strings.Contains(stdout.String(), "xreq_granted") {
		t.Fatalf("handoff not announced or request not made: stderr=%s stdout=%s", stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "hik_1_cli_access-rotated") {
		t.Fatal("the rotated bearer reached command output")
	}
}

// With no authenticator on the account the same handoff applies.
func TestEmergencyAccessHandsOffWithoutAnAuthenticator(t *testing.T) {
	var startBody map[string]any
	var callbackURI string
	handler := accessHandoffServer(t, apigen.RevealWindow{EffectiveWindowSeconds: 300, TotpOffered: true}, false, &startBody, &callbackURI)
	ios, _, stderr := definitionsTestIO(t, handler)
	ios.OpenURL = openAndApprove(t, &callbackURI)
	if code := cli.Run(t.Context(), ios, emergencyArgs()); code != cli.ExitOK {
		t.Fatalf("exit %d, want ok; stderr=%s", code, stderr.String())
	}
	assertAccessHandoff(t, startBody)
}

// With no way to open a browser the dead end stays, starts nothing and names the way out.
func TestEmergencyAccessWithoutABrowserRefuses(t *testing.T) {
	var startBody map[string]any
	var callbackURI string
	handler := accessHandoffServer(t, apigen.RevealWindow{Protected: true}, false, &startBody, &callbackURI)
	ios, _, stderr := definitionsTestIO(t, handler)
	ios.OpenURL = nil
	if code := cli.Run(t.Context(), ios, emergencyArgs()); code != cli.ExitAuth {
		t.Fatalf("exit %d, want auth; stderr=%s", code, stderr.String())
	}
	if startBody != nil {
		t.Fatalf("a handoff started with no browser to open: %v", startBody)
	}
	if !strings.Contains(stderr.String(), "take it in the browser") {
		t.Errorf("the browser path was not named: %s", stderr.String())
	}
}

// --approve-in-browser forces the handoff where an authenticator code is
// offered and would have worked.
func TestApproveInBrowserFlagForcesTheHandoffWhereTOTPIsOffered(t *testing.T) {
	var startBody map[string]any
	var callbackURI string
	handler := accessHandoffServer(t, apigen.RevealWindow{EffectiveWindowSeconds: 300, TotpOffered: true}, true, &startBody, &callbackURI)
	handler = noTOTPLookup(t, handler)
	ios, _, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) { t.Fatal("the flag must skip the code prompt"); return "", nil }
	ios.OpenURL = openAndApprove(t, &callbackURI)
	if code := cli.Run(t.Context(), ios, emergencyArgs("--approve-in-browser")); code != cli.ExitOK {
		t.Fatalf("exit %d, want ok; stderr=%s", code, stderr.String())
	}
	assertAccessHandoff(t, startBody)
	if !strings.Contains(stderr.String(), "you chose the browser approval") {
		t.Errorf("the choice was not named: %s", stderr.String())
	}
}

// Without the flag the same environment takes the inline code, so the test
// above proves the flag and not the fixture.
func TestWithoutTheFlagTheCodeIsPromptedWhereTOTPIsOffered(t *testing.T) {
	var startBody map[string]any
	var callbackURI string
	handler := accessHandoffServer(t, apigen.RevealWindow{EffectiveWindowSeconds: 300, TotpOffered: true}, true, &startBody, &callbackURI)
	ios, _, _ := definitionsTestIO(t, handler)
	prompted := false
	ios.ReadPassword = func(string) (string, error) { prompted = true; return "", errors.New("stop at the prompt") }
	ios.OpenURL = func(string) error { t.Error("the browser opened where a code works"); return nil }
	cli.Run(t.Context(), ios, emergencyArgs())
	if !prompted || startBody != nil {
		t.Fatalf("prompted=%v handoff=%v", prompted, startBody)
	}
}

// A chosen browser ceremony never falls back to a typed code.
func TestApproveInBrowserWithoutABrowserRefusesInsteadOfPrompting(t *testing.T) {
	var startBody map[string]any
	var callbackURI string
	handler := accessHandoffServer(t, apigen.RevealWindow{EffectiveWindowSeconds: 300, TotpOffered: true}, true, &startBody, &callbackURI)
	handler = noTOTPLookup(t, handler)
	ios, _, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) { t.Fatal("a chosen browser ceremony prompted for a code"); return "", nil }
	ios.OpenURL = nil
	if code := cli.Run(t.Context(), ios, emergencyArgs("--approve-in-browser")); code != cli.ExitAuth {
		t.Fatalf("exit %d, want auth; stderr=%s", code, stderr.String())
	}
	// Text only the cannot-open branch carries, not the headless one.
	if !strings.Contains(stderr.String(), "which this invocation cannot open; take it in the browser") ||
		strings.Contains(stderr.String(), "no graphical session") ||
		!strings.Contains(stderr.String(), "you chose the browser approval") || strings.Contains(stderr.String(), "raise the window") {
		t.Errorf("the refusal does not name the choice: %s", stderr.String())
	}
}

// A purpose with no handoff (a machine reveal grant) is refused, not typed.
func TestApproveInBrowserRefusesAPurposeWithoutAHandoff(t *testing.T) {
	handler := ceremonyHandlerForGrant(t)
	ios, _, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) { t.Fatal("prompted for a code"); return "", nil }
	ios.OpenURL = func(string) error { t.Error("opened a browser for a purpose with no handoff"); return nil }
	code := cli.Run(t.Context(), ios, []string{"access", "grant", "add", "--principal", "sa_1", "--capability", "reveal", "--approve-in-browser",
		"--instance", "local", "--org", "org_70", "--project", "prj_70", "--env", "env_70"})
	if code != cli.ExitAuth || !strings.Contains(stderr.String(), "no CLI browser handoff") {
		t.Fatalf("exit %d; stderr=%s", code, stderr.String())
	}
}

func ceremonyHandlerForGrant(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/reveal-window"):
			_ = jsonwire.Encode(w, apigen.RevealWindow{CanReveal: true, EffectiveWindowSeconds: 300, TotpOffered: true})
		case strings.HasSuffix(r.URL.Path, "/auth/totp"):
			_ = jsonwire.Encode(w, apigen.TotpStatus{Confirmed: true})
		default:
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"conflict","message":"the current state of this resource refuses the request","detail":"reauthenticate over the environments this operation makes reachable, then retry (env_70)"}}`))
		}
	})
}

// `reauth.method = browser` from the instance's stored settings does the same
// as the flag, and `auto` (the default) does not.
func TestReauthMethodBrowserFromConfigForcesTheHandoff(t *testing.T) {
	var startBody map[string]any
	var callbackURI string
	handler := accessHandoffServer(t, apigen.RevealWindow{EffectiveWindowSeconds: 300, TotpOffered: true}, true, &startBody, &callbackURI)
	handler = noTOTPLookup(t, handler)
	ios, stdout, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) { t.Fatal("the setting must skip the code prompt"); return "", nil }
	ios.OpenURL = openAndApprove(t, &callbackURI)
	if code := cli.Run(t.Context(), ios, []string{"config", "set", "reauth.method", "browser", "--instance", "local"}); code != cli.ExitOK {
		t.Fatalf("config set: exit %d; stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := cli.Run(t.Context(), ios, []string{"config", "get", "reauth.method", "--instance", "local"}); code != cli.ExitOK || strings.TrimSpace(stdout.String()) != "browser" {
		t.Fatalf("config get: exit %d, %q", code, stdout.String())
	}
	if code := cli.Run(t.Context(), ios, emergencyArgs()); code != cli.ExitOK {
		t.Fatalf("exit %d, want ok; stderr=%s", code, stderr.String())
	}
	assertAccessHandoff(t, startBody)
	// Setting auto again removes the choice.
	if code := cli.Run(t.Context(), ios, []string{"config", "set", "reauth.method", "auto", "--instance", "local"}); code != cli.ExitOK {
		t.Fatalf("config set auto: exit %d", code)
	}
	stdout.Reset()
	cli.Run(t.Context(), ios, []string{"config", "get", "reauth.method", "--instance", "local"})
	if strings.TrimSpace(stdout.String()) != "auto" {
		t.Fatalf("config get after auto = %q", stdout.String())
	}
}

// An unknown value fails loudly, in the verb and in a hand-edited file.
func TestReauthMethodRejectsUnknownValues(t *testing.T) {
	var startBody map[string]any
	var callbackURI string
	handler := accessHandoffServer(t, apigen.RevealWindow{EffectiveWindowSeconds: 300, TotpOffered: true}, true, &startBody, &callbackURI)
	ios, _, stderr := definitionsTestIO(t, handler)
	if code := cli.Run(t.Context(), ios, []string{"config", "set", "reauth.method", "passkey", "--instance", "local"}); code != cli.ExitUsage || !strings.Contains(stderr.String(), "auto or browser") {
		t.Fatalf("set: exit %d; stderr=%s", code, stderr.String())
	}
	stderr.Reset()
	if code := cli.Run(t.Context(), ios, []string{"config", "set", "reauth.mode", "browser", "--instance", "local"}); code != cli.ExitUsage {
		t.Fatalf("an unknown key: exit %d", code)
	}
	for name, content := range map[string]string{
		"bad value":   `{"local":{"reauth.method":"always"}}`,
		"empty value": `{"local":{"reauth.method":""}}`,
		"unknown key": `{"local":{"reauth.mehtod":"browser"}}`,
		"null value":  `{"local":{"reauth.method":null}}`,
		"not json":    `{"local":`,
	} {
		stderr.Reset()
		if err := os.WriteFile(filepath.Join(ios.Env.Getenv("HIKYO_STATE_DIR"), "settings.json"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		ios.ReadPassword = func(string) (string, error) {
			t.Fatalf("%s: a ceremony ran under invalid settings", name)
			return "", nil
		}
		if code := cli.Run(t.Context(), ios, emergencyArgs()); code == cli.ExitOK {
			t.Errorf("%s: the ceremony ran under invalid settings", name)
		}
		// A damaged file is a state fault like the contexts file, not a usage error.
		if code := cli.Run(t.Context(), ios, []string{"config", "get", "reauth.method", "--instance", "local"}); code == cli.ExitOK || code == cli.ExitUsage || !strings.Contains(stderr.String(), "settings.json") {
			t.Errorf("%s: config get exit %d; stderr=%s", name, code, stderr.String())
		}
	}
	if startBody != nil {
		t.Fatalf("a handoff started under invalid settings: %v", startBody)
	}
}

// When the browser was chosen and cannot be opened (SSH, no display), the
// failure names the choice and the way back to the code prompt, and the same
// is said before the handoff waits.
func TestChosenBrowserThatCannotOpenNamesTheChoiceAndTheRemedy(t *testing.T) {
	var startBody map[string]any
	var callbackURI string
	handler := accessHandoffServer(t, apigen.RevealWindow{EffectiveWindowSeconds: 300, TotpOffered: true}, true, &startBody, &callbackURI)
	handler = noTOTPLookup(t, handler)
	ios, _, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) { t.Fatal("a chosen browser ceremony prompted for a code"); return "", nil }
	ios.OpenURL = func(string) error { return errors.New("no display") }
	if code := cli.Run(t.Context(), ios, emergencyArgs("--approve-in-browser")); code != cli.ExitUnavailable {
		t.Fatalf("exit %d, want unavailable; stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"no display", "you chose the browser approval", "drop --approve-in-browser to enter an authenticator code here, if one is enrolled"} {
		if strings.Count(stderr.String(), want) < 1 {
			t.Errorf("stderr lacks %q: %s", want, stderr.String())
		}
	}
	if !strings.Contains(stderr.String(), "waiting for it") {
		t.Errorf("no hint before waiting: %s", stderr.String())
	}
}

// The setting is not blamed where it was not the cause: with no code possible
// the browser was the only way anyway.
func TestUnchosenBrowserThatCannotOpenDoesNotBlameTheSetting(t *testing.T) {
	var startBody map[string]any
	var callbackURI string
	handler := accessHandoffServer(t, apigen.RevealWindow{Protected: true}, false, &startBody, &callbackURI)
	ios, _, stderr := definitionsTestIO(t, handler)
	ios.OpenURL = func(string) error { return errors.New("no display") }
	if code := cli.Run(t.Context(), ios, emergencyArgs("--approve-in-browser")); code != cli.ExitUnavailable {
		t.Fatalf("exit %d, want unavailable; stderr=%s", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "reauth.method") || strings.Contains(stderr.String(), "you chose") {
		t.Errorf("the setting was blamed where a code was never possible: %s", stderr.String())
	}
}

// A live window needs no ceremony, so a chosen browser starts no handoff.
func TestChosenBrowserWithALiveWindowStartsNoHandoff(t *testing.T) {
	var startBody map[string]any
	var callbackURI string
	handler := accessHandoffServer(t, apigen.RevealWindow{Live: true, EffectiveWindowSeconds: 300, TotpOffered: true}, true, &startBody, &callbackURI)
	handler = noTOTPLookup(t, handler)
	ios, _, _ := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) { t.Fatal("prompted"); return "", nil }
	ios.OpenURL = func(string) error { t.Error("opened a browser with a live window"); return nil }
	cli.Run(t.Context(), ios, emergencyArgs("--approve-in-browser"))
	if startBody != nil {
		t.Fatalf("a handoff started under a live window: %v", startBody)
	}
}

// `values get --reveal` takes the disclosure handoff under reauth.method browser.
func TestReauthMethodBrowserCoversValuesGetReveal(t *testing.T) {
	ceremony, _ := ceremonyServer(t, apigen.RevealWindow{CanReveal: true, EffectiveWindowSeconds: 300, TotpOffered: true})
	inner := noTOTPLookup(t, ceremony)
	var startBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/cli-reauth/start") {
			_ = json.UnmarshalRead(r.Body, &startBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"bad_request","message":"stop at the handoff"}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/values") {
			w.Header().Set("Content-Type", "application/json")
			_ = jsonwire.Encode(w, apigen.ValueList{Items: []apigen.ValueCell{{Name: "DATABASE_PASSWORD", Classification: "secret", KeyId: "key_1", Set: true}}})
			return
		}
		inner.ServeHTTP(w, r)
	})
	ios, _, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) { t.Fatal("the setting must skip the code prompt"); return "", nil }
	ios.OpenURL = func(string) error { t.Error("opened a browser before the handoff started"); return nil }
	if code := cli.Run(t.Context(), ios, []string{"config", "set", "reauth.method", "browser", "--instance", "local"}); code != cli.ExitOK {
		t.Fatalf("config set: exit %d; stderr=%s", code, stderr.String())
	}
	cli.Run(t.Context(), ios, revealArgs())
	if startBody["purpose"] != "reveal" || startBody["operation"] != "value.reveal" {
		t.Fatalf("handoff body = %v; stderr=%s", startBody, stderr.String())
	}
}

// `hikyo dev session --approve-in-browser` takes the developer-credential
// handoff although a code is possible; removing `&& !browser` from
// freshDeveloperReauth makes it prompt and fail this test.
func TestApproveInBrowserForcesTheDeveloperCredentialHandoff(t *testing.T) {
	ceilingSeconds := int64(8 * 60 * 60)
	var startBody map[string]any
	ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/revisions/latest"):
			_ = jsonwire.Encode(w, apigen.RevisionDetail{Keys: []apigen.SnapshotKey{{KeyId: "key_01989abc-def0-7123-8123-000000000006", Name: "DEV_807_SECRET"}}})
		case strings.HasSuffix(r.URL.Path, "/reveal-window"):
			_ = jsonwire.Encode(w, apigen.RevealWindow{CanReveal: true, EffectiveWindowSeconds: 300, TotpOffered: true, DeveloperCredentialMaxLifetimeSeconds: &ceilingSeconds})
		case r.URL.Path == "/api/v1/auth/totp":
			t.Error("browser mode asked whether an authenticator is enrolled")
			_ = jsonwire.Encode(w, apigen.TotpStatus{Confirmed: true})
		case r.URL.Path == "/api/v1/auth/cli-reauth/start":
			_ = json.UnmarshalRead(r.Body, &startBody)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"bad_request","message":"stop at the handoff"}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	ios.TerminalSession, _ = terminalSession(t, "y\n")
	ios.ReadPassword = func(string) (string, error) { t.Fatal("the flag must skip the code prompt"); return "", nil }
	ios.OpenURL = func(string) error { t.Error("opened a browser before the handoff started"); return nil }
	cli.Run(t.Context(), ios, []string{"dev", "session", "--approve-in-browser", "--instance", "local", "--org", developerOrg, "--project", developerProject, "--env", developerEnv, "--ttl", "1h"})
	if startBody["purpose"] != "developer-credential" || startBody["operation"] != "developer-credential.mint" {
		t.Fatalf("handoff body = %v; stderr=%s", startBody, stderr.String())
	}
}

// Forgetting an instance's trust entry also forgets its stored settings.
func TestContextDeleteInstanceForgetsItsSettings(t *testing.T) {
	var startBody map[string]any
	var callbackURI string
	ios, _, stderr := definitionsTestIO(t, accessHandoffServer(t, apigen.RevealWindow{}, false, &startBody, &callbackURI))
	if code := cli.Run(t.Context(), ios, []string{"config", "set", "reauth.method", "browser", "--instance", "local"}); code != cli.ExitOK {
		t.Fatalf("config set: exit %d; stderr=%s", code, stderr.String())
	}
	if code := cli.Run(t.Context(), ios, []string{"context", "delete", "--instance", "local"}); code != cli.ExitOK {
		t.Fatalf("context delete: exit %d; stderr=%s", code, stderr.String())
	}
	st, err := cli.NewState(ios.Env)
	if err != nil {
		t.Fatal(err)
	}
	all, err := st.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := all["local"]; ok {
		t.Fatalf("settings outlived the trust entry: %v", all)
	}
}

// With a corrupt settings.json the forget refuses before the trust entry goes.
func TestContextDeleteInstanceWithCorruptSettingsChangesNothing(t *testing.T) {
	ios, _, stderr := definitionsTestIO(t, http.NotFoundHandler())
	if err := os.WriteFile(filepath.Join(ios.Env.Getenv("HIKYO_STATE_DIR"), "settings.json"), []byte(`{"local":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := cli.Run(t.Context(), ios, []string{"context", "delete", "--instance", "local"}); code == cli.ExitOK {
		t.Fatalf("context delete succeeded over corrupt settings; stderr=%s", stderr.String())
	}
	st, err := cli.NewState(ios.Env)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Trust().Lookup("local"); err != nil {
		t.Fatalf("the trust entry was deleted although the settings refused: %v", err)
	}
}

// A host with no graphical session refuses a chosen browser before the handoff
// starts, naming the cause and the conditional remedy.
func TestChosenBrowserOnAHostWithoutADisplayRefusesBeforeTheHandoff(t *testing.T) {
	var startBody map[string]any
	var callbackURI string
	handler := noTOTPLookup(t, accessHandoffServer(t, apigen.RevealWindow{EffectiveWindowSeconds: 300, TotpOffered: true}, true, &startBody, &callbackURI))
	ios, _, stderr := definitionsTestIO(t, handler)
	withoutDisplay(&ios)
	ios.OpenURL = func(string) error { t.Error("opened a browser on a host with no display"); return nil }
	if code := cli.Run(t.Context(), ios, emergencyArgs("--approve-in-browser")); code != cli.ExitAuth {
		t.Fatalf("exit %d, want auth; stderr=%s", code, stderr.String())
	}
	if startBody != nil {
		t.Fatalf("a handoff started on a headless host: %v", startBody)
	}
	for _, want := range []string{"no graphical session", "--approve-in-browser", "if one is enrolled"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("refusal lacks %q: %s", want, stderr.String())
		}
	}
}

func withoutDisplay(ios *cli.IO) {
	base := ios.Env.Getenv
	ios.Env = cli.Env{Getenv: func(k string) string {
		if k == "DISPLAY" || k == "WAYLAND_DISPLAY" || k == "BROWSER" {
			return ""
		}
		return base(k)
	}}
	ios.HostOS = "linux"
}

// A purpose with no handoff is refused for that reason on a headless host too:
// a display would not help, so the refusal must not blame its absence.
func TestApproveInBrowserRefusesAPurposeWithoutAHandoffOnAHeadlessHost(t *testing.T) {
	ios, _, stderr := definitionsTestIO(t, noTOTPLookup(t, ceremonyHandlerForGrant(t)))
	withoutDisplay(&ios)
	ios.ReadPassword = func(string) (string, error) { t.Fatal("prompted for a code"); return "", nil }
	code := cli.Run(t.Context(), ios, []string{"access", "grant", "add", "--principal", "sa_1", "--capability", "reveal", "--approve-in-browser",
		"--instance", "local", "--org", "org_70", "--project", "prj_70", "--env", "env_70"})
	if code != cli.ExitAuth || !strings.Contains(stderr.String(), "no CLI browser handoff") || strings.Contains(stderr.String(), "no graphical session") {
		t.Fatalf("exit %d; stderr=%s", code, stderr.String())
	}
}

// The flag does not bypass settings validation.
func TestApproveInBrowserStillRefusesCorruptSettings(t *testing.T) {
	var startBody map[string]any
	var callbackURI string
	ios, _, stderr := definitionsTestIO(t, accessHandoffServer(t, apigen.RevealWindow{EffectiveWindowSeconds: 300, TotpOffered: true}, true, &startBody, &callbackURI))
	if err := os.WriteFile(filepath.Join(ios.Env.Getenv("HIKYO_STATE_DIR"), "settings.json"), []byte(`{"local":{"reauth.method":"always"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ios.OpenURL = func(string) error { t.Error("opened a browser under invalid settings"); return nil }
	if code := cli.Run(t.Context(), ios, emergencyArgs("--approve-in-browser")); code == cli.ExitOK || !strings.Contains(stderr.String(), "settings.json") || startBody != nil {
		t.Fatalf("exit %d, handoff=%v; stderr=%s", code, startBody, stderr.String())
	}
}

// A bad key or argument count is a usage error before any trust lookup.
func TestConfigValidatesKeyAndArityBeforeTheInstanceLookup(t *testing.T) {
	ios, _, stderr := definitionsTestIO(t, http.NotFoundHandler())
	for _, args := range [][]string{
		{"config", "get", "reauth.mode", "--instance", "nope"},
		{"config", "get", "reauth.method", "extra", "--instance", "nope"},
		{"config", "set", "reauth.method", "--instance", "nope"},
		{"config", "set", "reauth.method", "passkey", "--instance", "nope"},
	} {
		stderr.Reset()
		if code := cli.Run(t.Context(), ios, args); code != cli.ExitUsage {
			t.Errorf("%v: exit %d, want usage; stderr=%s", args, code, stderr.String())
		}
	}
}

// An account-security proof has no browser handoff; the flag says so instead of
// being ignored silently.
func TestApproveInBrowserNotesItDoesNotApplyToAProofPrompt(t *testing.T) {
	ios, _, stderr := definitionsTestIO(t, http.NotFoundHandler())
	ios.ReadPassword = func(string) (string, error) { return "", errors.New("stop at the prompt") }
	cli.Run(t.Context(), ios, []string{"account", "recovery-codes", "regenerate", "--dangerously-print", "--approve-in-browser", "--instance", "local"})
	if strings.Count(stderr.String(), "--approve-in-browser does not apply to this account-security proof") == 0 {
		t.Fatalf("no note on a proof prompt: %s", stderr.String())
	}
}
