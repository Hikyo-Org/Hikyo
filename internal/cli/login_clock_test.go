package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

func TestInteractiveLoginClockSkewPreservesServerExpiry(t *testing.T) {
	for _, device := range []bool{false, true} {
		for _, skew := range []time.Duration{-48 * time.Hour, 48 * time.Hour} {
			t.Run(strings.Join([]string{map[bool]string{false: "loopback", true: "device"}[device], skew.String()}, "/"), func(t *testing.T) {
				var start apigen.CLILoginStartRequest
				result := loginTestResult()
				result.Session.CreatedAt = result.Session.CreatedAt.Add(skew)
				result.Session.IdleExpiresAt = result.Session.IdleExpiresAt.Add(skew)
				result.Session.AbsoluteExpiresAt = result.Session.AbsoluteExpiresAt.Add(skew)
				var client *Client
				client = loginTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case api.PathPrefix + "/auth/cli-login/start":
						_ = json.NewDecoder(r.Body).Decode(&start)
						_ = json.NewEncoder(w).Encode(apigen.CLILoginStart{State: start.State, ExpiresAt: time.Now().Add(skew + 10*time.Minute)})
					case api.PathPrefix + "/auth/cli-login/redeem":
						_ = json.NewEncoder(w).Encode(result)
					case api.PathPrefix + "/auth/cli-login/device/start":
						_ = json.NewEncoder(w).Encode(apigen.CLIDeviceStart{DeviceCode: "secret", UserCode: "ABCD1234", VerificationUri: client.Entry.Origin + "/login/cli?device=1", ExpiresIn: 900, Interval: 5, ExpiresAt: time.Now().Add(skew + 15*time.Minute)})
					case api.PathPrefix + "/auth/cli-login/device/token":
						_ = json.NewEncoder(w).Encode(apigen.CLIDeviceToken{AccessToken: loginTestToken, TokenType: "Bearer", ExpiresIn: 86400, Session: result.Session, Principal: result.Principal})
					default:
						t.Errorf("unexpected login request %s", r.URL.Path)
						w.WriteHeader(http.StatusNotFound)
					}
				}))
				client.sleep = func(context.Context, time.Duration) error { return nil }
				var stderr bytes.Buffer
				ios := IO{Stderr: &stderr, OpenURL: func(string) error {
					response, err := http.Get(start.RedirectUri + "?code=approved&state=" + start.State)
					if err != nil {
						return err
					}
					return response.Body.Close()
				}}
				state := &State{dir: t.TempDir()}
				err := runInteractiveLogin(t.Context(), ios, state, client, apigen.Meta{ProtocolCapabilities: []string{"device-code", "loopback-handoff"}}, device, "")
				if err != nil {
					t.Fatal(err)
				}
				sessions, err := state.Sessions()
				if err != nil || len(sessions) != 1 || sessions["login"].Token != loginTestToken || sessions["login"].ExpiresAt != result.Session.AbsoluteExpiresAt.Format(time.RFC3339) {
					t.Fatalf("server session expiry was not preserved: sessions=%d err=%v", len(sessions), err)
				}
			})
		}
	}
}

func TestLoginLoopbackClockSkew(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		serverClock time.Duration
		serverError bool
		callerLimit bool
	}{
		{name: "client ahead", serverClock: -time.Hour},
		{name: "client slightly ahead", serverClock: -9 * time.Minute},
		{name: "client behind", serverClock: time.Hour},
		{name: "server expired", serverClock: time.Hour, serverError: true},
		{name: "caller deadline", serverClock: time.Hour, callerLimit: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var start apigen.CLILoginStartRequest
			redeems := 0
			began := time.Now()
			client := &Client{Entry: TrustEntry{Origin: "https://hikyo.example"}, meta: &apigen.Meta{ApiRevision: api.Revision}, metaOrigin: "https://hikyo.example"}
			ctx := t.Context()
			budget := 10 * time.Minute
			if scenario.callerLimit {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Minute)
				defer cancel()
				budget = time.Minute
			}
			client.HTTP = &http.Client{Transport: loginRoundTripper(func(r *http.Request) (*http.Response, error) {
				status := http.StatusOK
				var body any
				switch r.URL.Path {
				case api.PathPrefix + "/auth/cli-login/start":
					if err := json.NewDecoder(r.Body).Decode(&start); err != nil {
						t.Fatal(err)
					}
					body = apigen.CLILoginStart{State: start.State, ExpiresAt: time.Now().Add(scenario.serverClock + 10*time.Minute)}
				case api.PathPrefix + "/auth/cli-login/redeem":
					redeems++
					deadline, ok := r.Context().Deadline()
					if !ok || deadline.Before(began.Add(budget-time.Second)) || deadline.After(time.Now().Add(budget)) || deadline == deadline.Round(0) {
						t.Errorf("redemption deadline %v does not preserve local budget %v", deadline, budget)
					}
					if scenario.serverError {
						status = http.StatusUnauthorized
						body = map[string]any{"error": map[string]string{"code": "unauthenticated", "message": "transaction expired"}}
					} else {
						body = loginTestResult()
					}
				default:
					t.Fatalf("unexpected login request %s", r.URL.Path)
				}
				raw, _ := json.Marshal(body)
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
			})}
			result, err := loginWithLoopback(ctx, client, "", func(string) error {
				response, err := http.Get(start.RedirectUri + "?code=approved&state=" + start.State)
				if err != nil {
					return err
				}
				return response.Body.Close()
			})
			if (err != nil) != scenario.serverError || redeems != 1 || (!scenario.serverError && result.Session.Id != "ses_login") {
				t.Fatalf("skewed login result=%s redeems=%d err=%v", result.Session.Id, redeems, err)
			}
		})
	}
}

func TestDeviceLoginClockSkew(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		serverClock time.Duration
		serverError bool
		callerLimit bool
	}{
		{name: "client ahead", serverClock: -time.Hour},
		{name: "client slightly ahead", serverClock: -14 * time.Minute},
		{name: "client behind", serverClock: time.Hour},
		{name: "server expired", serverClock: time.Hour, serverError: true},
		{name: "caller deadline", serverClock: time.Hour, callerLimit: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var client *Client
			polls := 0
			client = loginTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case api.PathPrefix + "/auth/cli-login/device/start":
					_ = json.NewEncoder(w).Encode(apigen.CLIDeviceStart{DeviceCode: "secret", UserCode: "ABCD1234", VerificationUri: client.Entry.Origin + "/login/cli?device=1", ExpiresIn: 900, Interval: 5, ExpiresAt: time.Now().Add(scenario.serverClock + 15*time.Minute)})
				case api.PathPrefix + "/auth/cli-login/device/token":
					polls++
					if scenario.serverError {
						w.WriteHeader(http.StatusBadRequest)
						_ = json.NewEncoder(w).Encode(apigen.CLIDeviceError{Error: "expired_token"})
						return
					}
					result := loginTestResult()
					_ = json.NewEncoder(w).Encode(apigen.CLIDeviceToken{AccessToken: loginTestToken, TokenType: "Bearer", ExpiresIn: 3600, Session: result.Session, Principal: result.Principal})
				default:
					t.Errorf("unexpected login request %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			began := time.Now()
			ctx := t.Context()
			budget := 15 * time.Minute
			if scenario.callerLimit {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Minute)
				defer cancel()
				budget = time.Minute
			}
			client.sleep = func(ctx context.Context, wait time.Duration) error {
				deadline, ok := ctx.Deadline()
				if !ok || deadline.Before(began.Add(budget-time.Second)) || deadline.After(time.Now().Add(budget)) || deadline == deadline.Round(0) {
					t.Errorf("polling deadline %v does not preserve monotonic local budget %v", deadline, budget)
				}
				if wait != 5*time.Second {
					t.Errorf("initial interval=%v, want 5s", wait)
				}
				return nil
			}
			result, err := loginWithDevice(ctx, IO{Stderr: io.Discard}, client, "")
			if (err != nil) != scenario.serverError || polls != 1 || (!scenario.serverError && result.Session.Id != "ses_login") {
				t.Fatalf("skewed login result=%s polls=%d err=%v", result.Session.Id, polls, err)
			}
		})
	}
}

func TestDeviceLoginRelativeLifetimeStopsPolling(t *testing.T) {
	for _, callerLimit := range []bool{false, true} {
		t.Run(map[bool]string{false: "transaction lifetime", true: "earlier caller deadline"}[callerLimit], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				polls := 0
				client := &Client{Entry: TrustEntry{Origin: "https://hikyo.example"}, meta: &apigen.Meta{ApiRevision: api.Revision}, metaOrigin: "https://hikyo.example"}
				client.HTTP = &http.Client{Transport: loginRoundTripper(func(r *http.Request) (*http.Response, error) {
					status := http.StatusOK
					var body any
					if r.URL.Path == api.PathPrefix+"/auth/cli-login/device/start" {
						body = apigen.CLIDeviceStart{DeviceCode: "secret", UserCode: "ABCD1234", VerificationUri: client.Entry.Origin + "/login/cli?device=1", ExpiresIn: 12, Interval: 5, ExpiresAt: time.Now().Add(time.Hour)}
					} else {
						polls++
						status = http.StatusBadRequest
						body = apigen.CLIDeviceError{Error: "authorization_pending"}
					}
					raw, _ := json.Marshal(body)
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
				})}
				ctx := t.Context()
				budget, wantPolls := 12*time.Second, 2
				if callerLimit {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, 3*time.Second)
					defer cancel()
					budget, wantPolls = 3*time.Second, 0
				}
				began := time.Now()
				_, err := loginWithDevice(ctx, IO{Stderr: io.Discard}, client, "")
				if err == nil || time.Since(began) != budget || polls != wantPolls {
					t.Fatalf("relative expiry: elapsed=%v polls=%d err=%v", time.Since(began), polls, err)
				}
			})
		})
	}
}

func TestInteractiveLoginRejectsMalformedServerSession(t *testing.T) {
	for _, scenario := range []string{"missing-creation", "missing-expiry", "expiry-before-creation", "idle-before-creation"} {
		t.Run(scenario, func(t *testing.T) {
			result := loginTestResult()
			switch scenario {
			case "missing-creation":
				result.Session.CreatedAt = time.Time{}
			case "missing-expiry":
				result.Session.AbsoluteExpiresAt = time.Time{}
			case "expiry-before-creation":
				result.Session.AbsoluteExpiresAt = result.Session.CreatedAt.Add(-time.Second)
			case "idle-before-creation":
				result.Session.IdleExpiresAt = result.Session.CreatedAt.Add(-time.Second)
			}
			var client *Client
			client = loginTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == api.PathPrefix+"/auth/cli-login/device/start" {
					_ = json.NewEncoder(w).Encode(apigen.CLIDeviceStart{DeviceCode: "secret", UserCode: "ABCD1234", VerificationUri: client.Entry.Origin + "/login/cli?device=1", ExpiresIn: 900, Interval: 5, ExpiresAt: time.Now().Add(15 * time.Minute)})
					return
				}
				_ = json.NewEncoder(w).Encode(apigen.CLIDeviceToken{AccessToken: loginTestToken, TokenType: "Bearer", ExpiresIn: 86400, Session: result.Session, Principal: result.Principal})
			}))
			client.sleep = func(context.Context, time.Duration) error { return nil }
			state := &State{dir: t.TempDir()}
			err := runInteractiveLogin(t.Context(), IO{Stderr: io.Discard}, state, client, apigen.Meta{ProtocolCapabilities: []string{"device-code"}}, true, "")
			sessions, readErr := state.Sessions()
			if err == nil || readErr != nil || len(sessions) != 0 {
				t.Fatalf("invalid server session reached custody: sessions=%d readErr=%v err=%v", len(sessions), readErr, err)
			}
		})
	}
}
