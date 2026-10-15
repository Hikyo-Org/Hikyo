package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
)

const loginTestToken = "hik_1_cli_secret-login-artifact"

func loginTestResult() apigen.LoginResult {
	token := loginTestToken
	return apigen.LoginResult{SessionToken: &token, Principal: apigen.Principal{Id: "usr_login", Kind: "human"}, Session: apigen.Session{
		Id: "ses_login", Artifact: "cli", CreatedAt: time.Now(), IdleExpiresAt: time.Now().Add(time.Hour), AbsoluteExpiresAt: time.Now().Add(24 * time.Hour),
	}}
}

func loginTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := newRevisionAwareFixtureServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(pinnedTestEntry("login", server), "")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestLoginLoopbackRejectsWrongCallbacksAndClosesBeforeRedeem(t *testing.T) {
	var start apigen.CLILoginStartRequest
	redeems := 0
	client := loginTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case api.PathPrefix + "/auth/cli-login/start":
			if err := json.NewDecoder(r.Body).Decode(&start); err != nil {
				t.Error(err)
			}
			if start.Transport != "loopback" || len(start.State) != 43 || start.State == start.PkceChallenge {
				t.Error("state and PKCE proof are not independent loopback inputs")
			}
			_ = json.NewEncoder(w).Encode(apigen.CLILoginStart{State: start.State, ExpiresAt: time.Now().Add(10 * time.Minute)})
		case api.PathPrefix + "/auth/cli-login/redeem":
			redeems++
			var redeem apigen.CLILoginRedeemRequest
			if err := json.NewDecoder(r.Body).Decode(&redeem); err != nil {
				t.Error(err)
			}
			digest := sha256.Sum256([]byte(redeem.PkceVerifier))
			if redeem.Code != "hik_1_hc_approved" || redeem.RedirectUri != start.RedirectUri || base64.RawURLEncoding.EncodeToString(digest[:]) != start.PkceChallenge {
				t.Error("redeem did not preserve the initiating PKCE and exact redirect")
			}
			callback, _ := url.Parse(start.RedirectUri)
			conn, err := net.DialTimeout("tcp", callback.Host, time.Second)
			if err == nil {
				conn.Close()
				t.Error("callback listener was still reachable during redemption")
			}
			_ = json.NewEncoder(w).Encode(loginTestResult())
		default:
			http.NotFound(w, r)
		}
	}))
	result, err := loginWithLoopback(t.Context(), client, "oidc:work", func(target string) error {
		page, err := url.Parse(target)
		if err != nil || page.Path != "/login/cli" || page.Query().Get("state") != start.State || page.Query().Get("provider") != "oidc:work" {
			return errors.New("incorrect browser page")
		}
		for _, negative := range []struct{ method, path, query, host string }{
			{http.MethodPost, "/callback", "code=x&state=" + start.State, ""},
			{http.MethodGet, "/wrong", "code=x&state=" + start.State, ""},
			{http.MethodGet, "/%63allback", "code=x&state=" + start.State, ""},
			{http.MethodGet, "/callback", "code=x&state=" + start.State, "localhost:1"},
			{http.MethodGet, "/callback", "code=x&state=wrong", ""},
			{http.MethodGet, "/callback", "code=x&state=" + start.State + "&state=" + start.State, ""},
			{http.MethodGet, "/callback", "code=x&code=y&state=" + start.State, ""},
			{http.MethodGet, "/callback", "code=x&state=" + start.State + "&extra=1", ""},
			{http.MethodGet, "/callback", "code=x&state=" + start.State + "&bad;query=1", ""},
		} {
			callback, _ := url.Parse(start.RedirectUri)
			request, err := http.NewRequest(negative.method, "http://"+callback.Host+negative.path+"?"+negative.query, nil)
			if err != nil {
				return err
			}
			if negative.host != "" {
				request.Host = negative.host
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				return err
			}
			response.Body.Close()
			if response.StatusCode != http.StatusBadRequest {
				t.Errorf("callback %v was accepted with status %d", negative, response.StatusCode)
			}
		}
		response, err := http.Get(start.RedirectUri + "?code=hik_1_hc_approved&state=" + start.State)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return errors.New("valid callback refused")
		}
		return nil
	})
	if err != nil || result.Session.Id != "ses_login" || redeems != 1 {
		t.Fatalf("result=%v redeems=%d err=%v", result.Session.Id, redeems, err)
	}
}

func TestLoginLoopbackRefusalsCloseListener(t *testing.T) {
	for _, scenario := range []string{"opener", "state", "expired", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			var start apigen.CLILoginStartRequest
			client := loginTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != api.PathPrefix+"/auth/cli-login/start" {
					t.Error("a refused handoff reached redemption")
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				_ = json.NewDecoder(r.Body).Decode(&start)
				response := apigen.CLILoginStart{State: start.State, ExpiresAt: time.Now().Add(time.Minute)}
				if scenario == "state" {
					response.State = "wrong"
				}
				if scenario == "expired" {
					response.ExpiresAt = time.Now().Add(-time.Second)
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			opens := 0
			_, err := loginWithLoopback(ctx, client, "", func(string) error {
				opens++
				if scenario == "cancelled" {
					cancel()
					return nil
				}
				return errors.New("browser missing")
			})
			if err == nil || ((scenario == "state" || scenario == "expired") && opens != 0) {
				t.Fatalf("refusal err=%v opens=%d", err, opens)
			}
			callback, _ := url.Parse(start.RedirectUri)
			conn, err := net.DialTimeout("tcp", callback.Host, time.Second)
			if err == nil {
				conn.Close()
				t.Fatal("refused handoff left listener reachable")
			}
		})
	}
}

func TestLoginProviderResolutionPrecedesBrowser(t *testing.T) {
	for _, scenario := range []struct{ hint, want string }{
		{"work", ""}, {"missing", ""}, {"saml:work", "saml:work"}, {"personal", "oauth2:personal"},
	} {
		t.Run(scenario.hint, func(t *testing.T) {
			requests := 0
			client := loginTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path != api.PathPrefix+"/auth/methods" {
					t.Error("provider resolution started an authorization")
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				_ = json.NewEncoder(w).Encode(apigen.AuthMethods{Providers: []apigen.AuthMethodProvider{{Kind: "oidc", Slug: "work"}, {Kind: "saml", Slug: "work"}, {Kind: "oauth2", Slug: "personal"}}})
			}))
			got, err := resolveLoginProvider(t.Context(), client, scenario.hint)
			if got != scenario.want || (err != nil) != (scenario.want == "") || requests != 1 {
				t.Fatalf("provider=%q err=%v requests=%d", got, err, requests)
			}
			if scenario.want == "" {
				opened := false
				err := runInteractiveLogin(t.Context(), IO{OpenURL: func(string) error { opened = true; return nil }}, &State{dir: t.TempDir()}, client, apigen.Meta{ProtocolCapabilities: []string{"loopback-handoff"}}, false, scenario.hint)
				if err == nil || opened {
					t.Fatalf("bad provider reached browser: opened=%v err=%v", opened, err)
				}
			}
		})
	}
}

func TestLoginDispatchAndSessionCustody(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(map[bool]string{false: "browser", true: "local"}[local], func(t *testing.T) {
			var start apigen.CLILoginStartRequest
			passwordRead := false
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case api.PathPrefix + "/meta":
					_ = json.NewEncoder(w).Encode(apigen.Meta{ApiRevision: api.Revision, ProtocolCapabilities: []string{"local-password", "loopback-handoff"}})
				case api.PathPrefix + "/auth/cli-login/start":
					_ = json.NewDecoder(r.Body).Decode(&start)
					_ = json.NewEncoder(w).Encode(apigen.CLILoginStart{State: start.State, ExpiresAt: time.Now().Add(time.Minute)})
				case api.PathPrefix + "/auth/local/login":
					var login apigen.LocalLoginRequest
					_ = json.NewDecoder(r.Body).Decode(&login)
					if !local || login.Username != "admin" || login.Password != "local-secret" {
						t.Error("local login changed its credential transport")
					}
					_ = json.NewEncoder(w).Encode(loginTestResult())
				case api.PathPrefix + "/auth/cli-login/redeem":
					_ = json.NewEncoder(w).Encode(loginTestResult())
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			state := &State{dir: t.TempDir()}
			if err := state.Trust().Put(pinnedTestEntry("login", server)); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			terminal, err := disclose.NewTerminalSession(nopTerminal{ReadWriter: &bytes.Buffer{}})
			if err != nil {
				t.Fatal(err)
			}
			ios := IO{Stdout: &stdout, Stderr: &stderr, TerminalSession: terminal, Env: Env{Getenv: func(name string) string {
				if name == "HIKYO_STATE_DIR" {
					return state.dir
				}
				return ""
			}}, ReadPassword: func(string) (string, error) { passwordRead = true; return "local-secret", nil }, OpenURL: func(string) error {
				response, err := http.Get(start.RedirectUri + "?code=hik_1_hc_login&state=" + start.State)
				if response != nil {
					response.Body.Close()
				}
				return err
			}}
			args := []string{"login", "login"}
			if local {
				args = append(args, "--local", "--as", "admin")
			}
			if code := Run(t.Context(), ios, args); code != ExitOK {
				t.Fatalf("exit=%d stderr=%s", code, stderr.String())
			}
			sessions, err := state.Sessions()
			if err != nil || len(sessions) != 1 || sessions["login"].Token != loginTestToken || passwordRead != local {
				t.Fatalf("session custody failed: count=%d password=%v err=%v", len(sessions), passwordRead, err)
			}
			if stdout.Len() != 0 || strings.Contains(stderr.String(), loginTestToken) || strings.Contains(stderr.String(), "local-secret") || (start.State != "" && strings.Contains(stderr.String(), start.State)) {
				t.Fatal("login disclosed an authentication secret in output")
			}
		})
	}
}

func TestLoginRefusesFlagMixesAndNonTerminalBeforeNetwork(t *testing.T) {
	for _, args := range [][]string{
		{"login", "https://unused.invalid"}, {"login", "https://unused.invalid", "--device"},
		{"login", "ref", "--local", "--device"}, {"login", "ref", "--local", "--provider", "oidc:work"},
		{"login", "ref", "--as", "admin"}, {"login", "one", "two"},
	} {
		var output bytes.Buffer
		ios := IO{Stdout: &output, Stderr: &output}
		if code := Run(t.Context(), ios, args); code != ExitUsage && code != ExitRefused {
			t.Fatalf("%v returned %d: %s", args, code, output.String())
		}
	}
}

type loginRoundTripper func(*http.Request) (*http.Response, error)

func (f loginRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type loginTimeout struct{}

func (loginTimeout) Error() string   { return "connection timed out" }
func (loginTimeout) Timeout() bool   { return true }
func (loginTimeout) Temporary() bool { return true }

func TestDevicePollingSlowDownTimeoutAndThrottleAreDistinct(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		statuses []string
		waits    []time.Duration
	}{
		{"slow_down beyond thirty seconds", []string{"slow_down", "slow_down", "slow_down", "slow_down", "slow_down", "slow_down", "slow_down", "success"}, []time.Duration{5, 10, 15, 20, 25, 30, 35, 40}},
		{"connection timeout only doubles", []string{"timeout", "authorization_pending", "slow_down", "success"}, []time.Duration{5, 10, 10, 15}},
		{"long pending crosses minute throttle", []string{"authorization_pending", "authorization_pending", "authorization_pending", "authorization_pending", "authorization_pending", "authorization_pending", "authorization_pending", "authorization_pending", "authorization_pending", "throttle", "authorization_pending", "success"}, []time.Duration{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 15, 5}},
		{"throttle preserves slow_down", []string{"slow_down", "throttle-short", "success"}, []time.Duration{5, 10, 10}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			attempt := 0
			var waited []time.Duration
			client := &Client{Entry: TrustEntry{Origin: "https://hikyo.example"}, UserAgent: "hikyo-cli", meta: &apigen.Meta{ApiRevision: api.Revision}, metaOrigin: "https://hikyo.example"}
			client.sleep = func(_ context.Context, wait time.Duration) error { waited = append(waited, wait); return nil }
			client.HTTP = &http.Client{Transport: loginRoundTripper(func(r *http.Request) (*http.Response, error) {
				if attempt >= len(scenario.statuses) {
					t.Fatal("polling continued past success")
				}
				if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.Header.Get("Authorization") != "" {
					t.Error("device polling did not use unauthenticated form transport")
				}
				if err := r.ParseForm(); err != nil || r.PostForm.Get("client_id") != "hikyo-cli" || r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" || r.PostForm.Get("device_code") != "never-display-device-code" {
					t.Error("invalid RFC device token request")
				}
				step := scenario.statuses[attempt]
				attempt++
				if step == "timeout" {
					return nil, loginTimeout{}
				}
				status := http.StatusBadRequest
				header := make(http.Header)
				body := `{"error":"` + step + `","interval":5}`
				if strings.HasPrefix(step, "throttle") {
					status = http.StatusTooManyRequests
					header.Set("Retry-After", "15")
					if step == "throttle-short" {
						header.Set("Retry-After", "1")
					}
					body = `{"error":{"code":"rate_limited","message":"throttled"}}`
				}
				if step == "success" {
					status = http.StatusOK
					result := loginTestResult()
					raw, _ := json.Marshal(apigen.CLIDeviceToken{AccessToken: loginTestToken, TokenType: "Bearer", ExpiresIn: 3600, Session: result.Session, Principal: result.Principal})
					body = string(raw)
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			var stderr bytes.Buffer
			result, err := pollCLIDevice(t.Context(), &stderr, client, "never-display-device-code", 5*time.Second)
			if err != nil || result.SessionToken == nil || *result.SessionToken != loginTestToken {
				t.Fatalf("poll result=%v err=%v", result.Session.Id, err)
			}
			for i := range scenario.waits {
				scenario.waits[i] *= time.Second
			}
			if !slices.Equal(waited, scenario.waits) {
				t.Fatalf("waits=%v want=%v", waited, scenario.waits)
			}
			if strings.Contains(stderr.String(), "never-display-device-code") || strings.Contains(stderr.String(), loginTestToken) {
				t.Fatal("device login disclosed a secret")
			}
			if strings.Contains(scenario.name, "throttle") && !strings.Contains(stderr.String(), "Retry-After") {
				t.Fatal("throttling had no ordinary terminal notice")
			}
		})
	}
}

func TestDevicePollingStopsOnTerminalErrors(t *testing.T) {
	for _, protocol := range []string{"access_denied", "expired_token", "invalid_client", "invalid_grant", "bad-json", "429-without-retry", "429-overflow", "429-zero", "network"} {
		t.Run(protocol, func(t *testing.T) {
			attempts := 0
			client := loginTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts++
				status := http.StatusBadRequest
				if strings.HasPrefix(protocol, "429-") {
					status = http.StatusTooManyRequests
					if protocol == "429-overflow" {
						w.Header().Set("Retry-After", "9223372036854775807")
					}
					if protocol == "429-zero" {
						w.Header().Set("Retry-After", "0")
					}
				}
				w.WriteHeader(status)
				if protocol == "bad-json" {
					_, _ = io.WriteString(w, "malformed")
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"error": protocol})
			}))
			client.sleep = func(context.Context, time.Duration) error { return nil }
			if protocol == "network" {
				client.meta = &apigen.Meta{ApiRevision: api.Revision}
				client.metaOrigin = client.Entry.Origin
				client.HTTP.Transport = loginRoundTripper(func(*http.Request) (*http.Response, error) { attempts++; return nil, errors.New("connection refused") })
			}
			_, err := pollCLIDevice(t.Context(), io.Discard, client, "secret", 5*time.Second)
			if err == nil || attempts != 1 {
				t.Fatalf("terminal response continued polling: attempts=%d err=%v", attempts, err)
			}
		})
	}
}

func TestDevicePollingWaitCannotOutliveTransaction(t *testing.T) {
	attempts := 0
	client := loginTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { attempts++; w.WriteHeader(http.StatusBadRequest) }))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	_, err := pollCLIDevice(ctx, io.Discard, client, "secret", 5*time.Second)
	if err == nil || attempts != 0 {
		t.Fatalf("expired transaction polled: attempts=%d err=%v", attempts, err)
	}
}
