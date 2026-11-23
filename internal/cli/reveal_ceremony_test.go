package cli_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

// The inline TOTP disclosure ceremony (reveal_window.go): a dead window with
// TOTP offered prompts for a code, opens the window, rotates and PERSISTS the
// session token, and then runs the disclosure. A 0-window environment is
// refused naming the browser path and the project-settings knob.
func ceremonyServer(t *testing.T, window apigen.RevealWindow) (http.Handler, *[]string) {
	t.Helper()
	var seen []string
	live := window.Live
	rotated := "hik_1_cli_rotated-token"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/reveal-window"):
			window.Live = live
			_ = jsonwire.Encode(w, window)
		case strings.HasSuffix(r.URL.Path, "/auth/totp"):
			// The inline path dispatches on enrolment: an authenticator stands.
			_ = jsonwire.Encode(w, apigen.TotpStatus{Confirmed: true})
		case strings.HasSuffix(r.URL.Path, "/auth/reauth/totp"):
			var body apigen.TotpReauthRequest
			_ = json.UnmarshalRead(r.Body, &body)
			environment, err := body.AsTotpEnvironmentReauthRequest()
			if err != nil || environment.Code != "123456" || string(environment.EnvironmentId) != "env_70" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"code":"unauthenticated","message":"authentication required"}}`))
				return
			}
			live = true
			_ = jsonwire.Encode(w, apigen.ReauthResult{
				EnvironmentId: "env_70", SessionId: "ses_rotated", SessionToken: &rotated,
				WindowExpires: apigen.Timestamp(time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)),
			})
		case strings.HasSuffix(r.URL.Path, "/values/export"):
			if !live || !strings.Contains(r.Header.Get("Authorization"), rotated) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"not permitted"}}`))
				return
			}
			v := "s3cret"
			_ = jsonwire.Encode(w, apigen.ExportedValues{Items: []apigen.ExportedValue{{Name: "DATABASE_PASSWORD", Classification: apigen.KeyClassificationSecret, Value: &v}}})
		case strings.HasSuffix(r.URL.Path, "/values/DATABASE_PASSWORD/reveal"):
			if !live || !strings.Contains(r.Header.Get("Authorization"), rotated) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"not permitted"}}`))
				return
			}
			v := "s3cret"
			_ = jsonwire.Encode(w, apigen.ValueCell{Name: "DATABASE_PASSWORD", Classification: "secret", KeyId: "key_1", Set: true, Revealed: true, Value: &v})
		default:
			http.NotFound(w, r)
		}
	}), &seen
}

func revealArgs() []string {
	return []string{"values", "get", "DATABASE_PASSWORD", "--reveal", "--dangerously-print",
		"--instance", "local", "--org", "org_70", "--project", "prj_70", "--env", "env_70"}
}

func TestRevealCeremonyOpensWindowByTOTPAndPersistsRotatedToken(t *testing.T) {
	handler, seen := ceremonyServer(t, apigen.RevealWindow{CanReveal: true, EffectiveWindowSeconds: 300, TotpOffered: true})
	ios, stdout, stderr := definitionsTestIO(t, handler)
	prompts := 0
	ios.ReadPassword = func(prompt string) (string, error) {
		prompts++
		if !strings.Contains(prompt, "env_70") || !strings.Contains(prompt, "300s") {
			t.Errorf("prompt does not name the environment and window: %q", prompt)
		}
		return "123456", nil
	}
	if code := cli.Run(t.Context(), ios, revealArgs()); code != cli.ExitOK {
		t.Fatalf("exit %d, want ok; stderr=%s", code, stderr.String())
	}
	if prompts != 1 {
		t.Fatalf("prompted %d times, want exactly once", prompts)
	}
	if !strings.Contains(stdout.String(), "s3cret") {
		t.Fatalf("the revealed value did not reach the chosen destination: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "reauthentication window open over env_70") {
		t.Errorf("no window-open notice: %s", stderr.String())
	}
	// Order: refused disclosure, window read, TOTP reauth, retried disclosure
	// under the ROTATED bearer.
	var order []string
	for _, s := range *seen {
		switch {
		case strings.Contains(s, "/reveal-window"):
			order = append(order, "window")
		case strings.Contains(s, "/reauth/totp"):
			order = append(order, "totp")
		case strings.Contains(s, "/reveal"):
			order = append(order, "reveal")
		}
	}
	if got := strings.Join(order, ","); got != "reveal,window,totp,reveal" {
		t.Fatalf("call order %s, want reveal,window,totp,reveal", got)
	}
	state, err := cli.NewState(ios.Env)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := state.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	session, ok := sessions["local"]
	if !ok || session.Token != "hik_1_cli_rotated-token" || session.SessionID != "ses_rotated" {
		t.Fatalf("the rotated session was not persisted: %+v", session)
	}
}

func TestRevealCeremonyRefusesZeroWindowNamingTheBrowserPath(t *testing.T) {
	// Raising the window is offered only where it can help: the server caps a
	// protected environment's window at 0 (ErrProtectedWindowCap).
	for _, tc := range []struct {
		name      string
		protected bool
		want      []string
		lacks     []string
	}{
		{"protected", true, []string{"protected environment", "ceremony is the browser's"}, []string{"reauth-window-seconds"}},
		{"zero_window", false, []string{"reveal window is 0", "ceremony is the browser's", "project-settings set --env env_70 --reauth-window-seconds"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, _ := ceremonyServer(t, apigen.RevealWindow{CanReveal: true, EffectiveWindowSeconds: 0, Protected: tc.protected})
			ios, _, stderr := definitionsTestIO(t, handler)
			ios.ReadPassword = func(string) (string, error) {
				t.Fatal("a 0-window environment must not prompt for a code")
				return "", nil
			}
			if code := cli.Run(t.Context(), ios, revealArgs()); code != cli.ExitAuth {
				t.Fatalf("exit %d, want ExitAuth; stderr=%s", code, stderr.String())
			}
			for _, want := range tc.want {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("refusal lacks %q: %s", want, stderr.String())
				}
			}
			for _, lack := range tc.lacks {
				if strings.Contains(stderr.String(), lack) {
					t.Errorf("refusal offers %q, which cannot help here: %s", lack, stderr.String())
				}
			}
		})
	}
}

func TestRevealCeremonyHandsBackTheRefusalWithoutReveal(t *testing.T) {
	handler, _ := ceremonyServer(t, apigen.RevealWindow{CanReveal: false, EffectiveWindowSeconds: 300, TotpOffered: true})
	ios, _, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) {
		t.Fatal("a principal without reveal must not be offered a ceremony")
		return "", nil
	}
	if code := cli.Run(t.Context(), ios, revealArgs()); code != cli.ExitRefused {
		t.Fatalf("exit %d, want refused; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "not permitted") {
		t.Errorf("the server's own refusal was replaced: %s", stderr.String())
	}
}

func TestRevealCeremonyCoversExport(t *testing.T) {
	handler, _ := ceremonyServer(t, apigen.RevealWindow{CanReveal: true, EffectiveWindowSeconds: 300, TotpOffered: true})
	ios, stdout, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) { return "123456", nil }
	args := []string{"values", "export", "--reveal", "--format", "dotenv", "--dangerously-print",
		"--instance", "local", "--org", "org_70", "--project", "prj_70", "--env", "env_70"}
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
		t.Fatalf("exit %d, want ok; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "DATABASE_PASSWORD=s3cret") {
		t.Fatalf("export did not carry the revealed value after the ceremony: %q", stdout.String())
	}
}

// A 0-window environment hands off to the browser's purpose-bound,
// enumerated-key-set ceremony: the CLI resolves the unit (the secret keys the
// act opens), starts a disclosure-purpose handoff carrying it, opens the
// browser, redeems the code for a rotated bearer, and runs the disclosure
// under it. The bearer never reaches stdout/stderr.
func TestRevealCeremonyHandsOffToTheBrowserAtZeroWindow(t *testing.T) {
	const rotated = "hik_1_cli_handoff-rotated"
	live := false
	var callbackURI string
	var startBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/reveal-window"):
			_ = jsonwire.Encode(w, apigen.RevealWindow{CanReveal: true, Protected: true, EffectiveWindowSeconds: 0, Live: live})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/environments/env_70/values"):
			_ = jsonwire.Encode(w, apigen.ValueList{Items: []apigen.ValueCell{
				{Name: "DATABASE_PASSWORD", Classification: "secret", KeyId: "key_pw", Set: true},
				{Name: "LOG_LEVEL", Classification: "config", KeyId: "key_ll", Set: true},
			}})
		case strings.HasSuffix(r.URL.Path, "/auth/cli-reauth/start"):
			_ = json.UnmarshalRead(r.Body, &startBody)
			callbackURI, _ = startBody["redirect_uri"].(string)
			_, _ = w.Write([]byte(`{"state":"hik_1_hs_reveal","expires_at":"2030-01-01T00:00:00Z"}`))
		case strings.HasSuffix(r.URL.Path, "/auth/cli-reauth/redeem"):
			live = true
			_, _ = w.Write([]byte(`{"session_id":"ses_handoff","session_token":"` + rotated + `","windows":[{"environment_id":"env_70","session_id":"ses_handoff","single_decision":true,"window_expires":"2030-01-01T00:00:00Z"}]}`))
		case strings.HasSuffix(r.URL.Path, "/values/DATABASE_PASSWORD/reveal"):
			if !live || !strings.Contains(r.Header.Get("Authorization"), rotated) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"not permitted"}}`))
				return
			}
			v := "s3cret"
			_ = jsonwire.Encode(w, apigen.ValueCell{Name: "DATABASE_PASSWORD", Classification: "secret", KeyId: "key_pw", Set: true, Revealed: true, Value: &v})
		default:
			http.NotFound(w, r)
		}
	})
	ios, stdout, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) { t.Fatal("a 0-window handoff must not prompt for a code"); return "", nil }
	ios.OpenURL = func(browserURL string) error {
		if !strings.Contains(browserURL, "/reauth/cli?transaction=hik_1_hs_reveal") {
			t.Errorf("browser opened at %q", browserURL)
		}
		go func() {
			resp, _ := http.Get(callbackURI + "?code=hik_1_hc_reveal&state=hik_1_hs_reveal")
			if resp != nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}
	if code := cli.Run(t.Context(), ios, revealArgs()); code != cli.ExitOK {
		t.Fatalf("exit %d, want ok; stderr=%s", code, stderr.String())
	}
	if startBody["purpose"] != "reveal" || startBody["operation"] != "value.reveal" {
		t.Fatalf("handoff started with purpose=%v operation=%v", startBody["purpose"], startBody["operation"])
	}
	keys, _ := startBody["key_ids"].([]any)
	if len(keys) != 1 || keys[0] != "key_pw" {
		t.Fatalf("handoff bound key set %v, want exactly the one secret key the act opens", keys)
	}
	if !strings.Contains(stdout.String(), "s3cret") {
		t.Fatalf("the disclosure did not run under the redeemed bearer: %q", stdout.String())
	}
	if strings.Contains(stdout.String(), rotated) || strings.Contains(stderr.String(), rotated) {
		t.Fatal("the rotated bearer reached command output")
	}
}

// A sliding window on an account WITHOUT an authenticator: the inline prompt
// would be a dead end, so the CLI hands off to the browser, whose page offers
// the passkey (and no code).
func TestRevealCeremonyHandsOffWhenNoAuthenticatorIsEnrolled(t *testing.T) {
	var started bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/reveal-window"):
			_ = jsonwire.Encode(w, apigen.RevealWindow{CanReveal: true, EffectiveWindowSeconds: 300, TotpOffered: true, Live: started})
		case strings.HasSuffix(r.URL.Path, "/auth/totp"):
			_ = jsonwire.Encode(w, apigen.TotpStatus{Confirmed: false})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/environments/env_70/values"):
			_ = jsonwire.Encode(w, apigen.ValueList{Items: []apigen.ValueCell{{Name: "DATABASE_PASSWORD", Classification: "secret", KeyId: "key_pw", Set: true}}})
		case strings.HasSuffix(r.URL.Path, "/auth/cli-reauth/start"):
			started = true
			// The handoff began; the test needs no browser round trip to prove
			// the dispatch, so it ends the flow here with a refusal.
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"conflict","message":"the current state of this resource refuses the request"}}`))
		case strings.HasSuffix(r.URL.Path, "/values/DATABASE_PASSWORD/reveal"):
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"not permitted"}}`))
		default:
			http.NotFound(w, r)
		}
	})
	ios, _, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) {
		t.Fatal("no authenticator is enrolled: a code prompt is a dead end")
		return "", nil
	}
	ios.OpenURL = func(string) error { t.Fatal("the handoff was refused at start; the browser must not open"); return nil }
	_ = cli.Run(t.Context(), ios, revealArgs())
	if !started {
		t.Fatalf("no handoff was started for an account without an authenticator; stderr=%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "no authenticator is enrolled") {
		t.Errorf("the reason was not named: %s", stderr.String())
	}
}

func TestRevealClipboardPreservesCeremonyAndNeverPrintsPlaintext(t *testing.T) {
	handler, seen := ceremonyServer(t, apigen.RevealWindow{CanReveal: true, EffectiveWindowSeconds: 300, TotpOffered: true})
	ios, stdout, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) { return "123456", nil }
	var copied string
	ios.PrepareClipboard = func() (func(context.Context, string) error, error) {
		if len(*seen) != 0 {
			t.Fatal("clipboard destination prepared after server request")
		}
		return func(_ context.Context, value string) error { copied = value; return nil }, nil
	}
	args := revealArgs()
	args[4] = "--clipboard"
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
		t.Fatalf("exit %d; stderr=%s", code, stderr.String())
	}
	if copied != "s3cret" || stdout.Len() != 0 || strings.Contains(stderr.String(), "s3cret") {
		t.Fatalf("copy=%q stdout=%q stderr=%q", copied, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "history-exclusion markers") {
		t.Fatal("missing protected-copy notice")
	}
}

func TestRevealClipboardDestinationUnavailableBeforeDisclosure(t *testing.T) {
	handler, seen := ceremonyServer(t, apigen.RevealWindow{CanReveal: true, Live: true})
	ios, stdout, stderr := definitionsTestIO(t, handler)
	ios.PrepareClipboard = func() (func(context.Context, string) error, error) { return nil, errors.New("unavailable") }
	args := revealArgs()
	args[4] = "--clipboard"
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitRefused {
		t.Fatalf("exit %d; %s", code, stderr.String())
	}
	if len(*seen) != 0 || stdout.Len() != 0 {
		t.Fatal("unavailable destination contacted server or printed")
	}
}

func TestRevealClipboardRejectsConflictingDestinations(t *testing.T) {
	for _, extra := range [][]string{{"--dangerously-print"}, {"--output-file", "unused"}, {"-o", "json"}} {
		t.Run(strings.Join(extra, " "), func(t *testing.T) {
			handler, seen := ceremonyServer(t, apigen.RevealWindow{CanReveal: true, Live: true})
			ios, _, stderr := definitionsTestIO(t, handler)
			args := revealArgs()
			args[4] = "--clipboard"
			args = append(args, extra...)
			if code := cli.Run(t.Context(), ios, args); code != cli.ExitUsage {
				t.Fatalf("exit %d; %s", code, stderr.String())
			}
			if len(*seen) != 0 {
				t.Fatal("conflicting destinations contacted server")
			}
		})
	}
}

func TestClipboardRequiresExplicitReveal(t *testing.T) {
	handler, seen := ceremonyServer(t, apigen.RevealWindow{})
	ios, _, stderr := definitionsTestIO(t, handler)
	args := []string{"values", "get", "DATABASE_PASSWORD", "--clipboard"}
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitUsage {
		t.Fatalf("exit %d; %s", code, stderr.String())
	}
	if len(*seen) != 0 {
		t.Fatal("implicit reveal contacted server")
	}
}

// declareCeremonyServer answers `values declare` with the server's uniform 403
// until the TOTP ceremony has opened the window, then 200.
func declareCeremonyServer(t *testing.T, window apigen.RevealWindow) (http.Handler, *[]string) {
	t.Helper()
	inner, seen := ceremonyServer(t, window)
	opened := false
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/values/declare") {
			inner.ServeHTTP(w, r)
			if strings.HasSuffix(r.URL.Path, "/auth/reauth/totp") {
				opened = true
			}
			return
		}
		*seen = append(*seen, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if !opened {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"not permitted"}}`))
			return
		}
		encodeOrFail(t, w, apigen.ValueList{})
	}), seen
}

func declareArgs(t *testing.T) []string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "value")
	if err := os.WriteFile(file, []byte("declared"), 0o600); err != nil {
		t.Fatal(err)
	}
	return []string{"values", "declare", "DATABASE_PASSWORD", "--envs", "env_70", "--value-file", file,
		"--instance", "local", "--org", "org_70", "--project", "prj_70"}
}

// A publish ceremony authorizes publish, not reveal: the built-in publisher
// and maintainer roles have no reveal, and a protected declare must still
// reach them (the server starts the ceremony on read alone).
func TestPublishCeremonyProceedsWithoutRevealPermission(t *testing.T) {
	handler, seen := declareCeremonyServer(t, apigen.RevealWindow{
		CanReveal: false, Protected: true, EffectiveWindowSeconds: 300, TotpOffered: true})
	ios, _, stderr := definitionsTestIO(t, handler)
	prompts := 0
	ios.ReadPassword = func(string) (string, error) { prompts++; return "123456", nil }
	if code := cli.Run(t.Context(), ios, declareArgs(t)); code != cli.ExitOK {
		t.Fatalf("exit %d, want ok; stderr=%s", code, stderr.String())
	}
	if prompts != 1 {
		t.Fatalf("prompted %d times, want once; calls=%v", prompts, *seen)
	}
}

// Declare owes the ceremony only on a protected environment: an unprotected one
// hands the server's refusal back without prompting.
func TestDeclareSkipsCeremonyOnUnprotectedEnvironment(t *testing.T) {
	handler, _ := declareCeremonyServer(t, apigen.RevealWindow{
		CanReveal: true, Protected: false, EffectiveWindowSeconds: 300, TotpOffered: true})
	ios, _, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) {
		t.Fatal("an unprotected declare must not prompt for a ceremony")
		return "", nil
	}
	if code := cli.Run(t.Context(), ios, declareArgs(t)); code != cli.ExitRefused {
		t.Fatalf("exit %d, want refused; stderr=%s", code, stderr.String())
	}
}

// A protected declare that needs the browser binds the handoff to the declared
// key's immutable id. The CLI names the key, and the key-read route takes only
// an id, so the unit comes from the environment's non-revealing value list.
func TestDeclareHandsOffBoundToTheDeclaredKeyID(t *testing.T) {
	const rotated = "hik_1_cli_declare-rotated"
	live := false
	var callbackURI string
	var startBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/reveal-window"):
			encodeOrFail(t, w, apigen.RevealWindow{CanReveal: false, Protected: true, EffectiveWindowSeconds: 0, Live: live})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/environments/env_70/values"):
			encodeOrFail(t, w, apigen.ValueList{Items: []apigen.ValueCell{
				{Name: "DATABASE_PASSWORD", Classification: "secret", KeyId: "key_pw", Set: true},
				{Name: "LOG_LEVEL", Classification: "config", KeyId: "key_ll", Set: true},
			}})
		case strings.HasSuffix(r.URL.Path, "/auth/cli-reauth/start"):
			if err := json.UnmarshalRead(r.Body, &startBody); err != nil {
				t.Errorf("decode handoff start: %v", err)
			}
			callbackURI, _ = startBody["redirect_uri"].(string)
			_, _ = w.Write([]byte(`{"state":"hik_1_hs_declare","expires_at":"2030-01-01T00:00:00Z"}`))
		case strings.HasSuffix(r.URL.Path, "/auth/cli-reauth/redeem"):
			live = true
			_, _ = w.Write([]byte(`{"session_id":"ses_declare","session_token":"` + rotated + `","windows":[{"environment_id":"env_70","session_id":"ses_declare","single_decision":true,"window_expires":"2030-01-01T00:00:00Z"}]}`))
		case strings.HasSuffix(r.URL.Path, "/values/declare"):
			if !live || !strings.Contains(r.Header.Get("Authorization"), rotated) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"not permitted"}}`))
				return
			}
			encodeOrFail(t, w, apigen.ValueList{})
		default:
			// Includes GET keys/DATABASE_PASSWORD: the route matches ids only.
			http.NotFound(w, r)
		}
	})
	ios, _, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) { t.Fatal("a 0-window handoff must not prompt for a code"); return "", nil }
	ios.OpenURL = func(string) error {
		go func() {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, callbackURI+"?code=hik_1_hc_declare&state=hik_1_hs_declare", nil)
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
	if code := cli.Run(t.Context(), ios, declareArgs(t)); code != cli.ExitOK {
		t.Fatalf("exit %d, want ok; stderr=%s", code, stderr.String())
	}
	if startBody["purpose"] != "publish" || startBody["operation"] != "value.copy-destination" {
		t.Fatalf("handoff started with purpose=%v operation=%v", startBody["purpose"], startBody["operation"])
	}
	keys, _ := startBody["key_ids"].([]any)
	if len(keys) != 1 || keys[0] != "key_pw" {
		t.Fatalf("handoff bound key set %v, want the declared key's id", keys)
	}
}

// encodeOrFail writes v as the stub's JSON reply and fails the test if it
// cannot be encoded.
func encodeOrFail(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	if err := jsonwire.Encode(w, v); err != nil {
		t.Errorf("encode stub reply: %v", err)
	}
}
