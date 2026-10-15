package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

func TestDeviceLoginUsesRFCFormsAndStoresOnlyCLISession(t *testing.T) {
	var client *Client
	requests := []string{}
	client = loginTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		if r.URL.Path == api.PathPrefix+"/auth/methods" {
			_ = json.NewEncoder(w).Encode(apigen.AuthMethods{Providers: []apigen.AuthMethodProvider{{Kind: "oidc", Slug: "work"}}})
			return
		}
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.Header.Get("Authorization") != "" {
			t.Error("device flow changed its RFC unauthenticated form transport")
		}
		if err := r.ParseForm(); err != nil || r.PostForm.Get("client_id") != "hikyo-cli" {
			t.Error("device request has no public CLI client id")
		}
		switch r.URL.Path {
		case api.PathPrefix + "/auth/cli-login/device/start":
			if len(r.PostForm) != 1 {
				t.Error("device authorization includes terminal provider state")
			}
			_ = json.NewEncoder(w).Encode(apigen.CLIDeviceStart{DeviceCode: "device-secret-never-display", UserCode: "ABCD1234", VerificationUri: client.Entry.Origin + "/login/cli?device=1", ExpiresAt: time.Now().Add(15 * time.Minute), ExpiresIn: 900, Interval: 5})
		case api.PathPrefix + "/auth/cli-login/device/token":
			if r.PostForm.Get("device_code") != "device-secret-never-display" || r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
				t.Error("device grant type or code changed")
			}
			result := loginTestResult()
			_ = json.NewEncoder(w).Encode(apigen.CLIDeviceToken{AccessToken: loginTestToken, TokenType: "Bearer", ExpiresIn: 86400, Session: result.Session, Principal: result.Principal})
		default:
			http.NotFound(w, r)
		}
	}))
	client.sleep = func(context.Context, time.Duration) error { return nil }
	state := &State{dir: t.TempDir()}
	var stdout, stderr bytes.Buffer
	opened := false
	ios := IO{Stdout: &stdout, Stderr: &stderr, OpenURL: func(string) error { opened = true; return nil }}
	err := runInteractiveLogin(t.Context(), ios, state, client, apigen.Meta{ProtocolCapabilities: []string{"device-code"}}, true, "work")
	if err != nil || opened {
		t.Fatalf("device login failed: opened=%v err=%v", opened, err)
	}
	sessions, err := state.Sessions()
	if err != nil || len(sessions) != 1 || sessions["login"].Token != loginTestToken || sessions["login"].SessionID != "ses_login" {
		t.Fatalf("device session custody count=%d err=%v", len(sessions), err)
	}
	if len(requests) != 3 || requests[0] != api.PathPrefix+"/auth/methods" || requests[1] != api.PathPrefix+"/auth/cli-login/device/start" {
		t.Fatalf("device provider discovery order=%v", requests)
	}
	if stdout.Len() != 0 || strings.Contains(stderr.String(), "device-secret-never-display") || strings.Contains(stderr.String(), loginTestToken) || !strings.Contains(stderr.String(), "Enter code: ABCD1234") || !strings.Contains(stderr.String(), "device=1&provider=oidc%3Awork") {
		t.Fatalf("device receipt omitted required instructions or disclosed a secret: %q", stderr.String())
	}
}

func TestDeviceLoginRefusesInvalidAuthorizationBeforePrintingOrPolling(t *testing.T) {
	for _, scenario := range []string{"foreign-url", "secret-in-url", "invalid-code", "missing-expiry", "expired", "interval", "lifetime", "missing-device-code"} {
		t.Run(scenario, func(t *testing.T) {
			var client *Client
			polled := false
			client = loginTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != api.PathPrefix+"/auth/cli-login/device/start" {
					polled = true
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				start := apigen.CLIDeviceStart{DeviceCode: "device-secret", UserCode: "ABCD1234", VerificationUri: client.Entry.Origin + "/login/cli?device=1", ExpiresAt: time.Now().Add(time.Minute), ExpiresIn: 60, Interval: 5}
				switch scenario {
				case "foreign-url":
					start.VerificationUri = "https://other.invalid/login/cli?device=1"
				case "secret-in-url":
					start.VerificationUri += "&device_code=device-secret"
				case "invalid-code":
					start.UserCode = "ABCD\n123"
				case "expired":
					start.ExpiresIn = 0
				case "missing-expiry":
					start.ExpiresAt = time.Time{}
				case "interval":
					start.Interval = 0
				case "lifetime":
					start.ExpiresIn = 901
				case "missing-device-code":
					start.DeviceCode = ""
				}
				_ = json.NewEncoder(w).Encode(start)
			}))
			var stderr bytes.Buffer
			_, err := loginWithDevice(t.Context(), IO{Stderr: &stderr}, client, "")
			if err == nil || polled || stderr.Len() != 0 {
				t.Fatalf("invalid authorization escaped: polled=%v printed=%q err=%v", polled, stderr.String(), err)
			}
		})
	}
}

func TestDeviceThrottleWaitEndsAtTransactionExpiry(t *testing.T) {
	attempts, waits := 0, 0
	client := loginTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"code":"rate_limited","message":"throttled"}}`)
	}))
	client.sleep = func(ctx context.Context, wait time.Duration) error {
		waits++
		if waits == 1 {
			return nil
		}
		if wait != time.Minute {
			t.Errorf("Retry-After wait=%v, want 60s", wait)
		}
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := pollCLIDevice(ctx, io.Discard, client, "device-secret", 5*time.Second)
	if err == nil || attempts != 1 || waits != 2 {
		t.Fatalf("throttle outlived expiry: attempts=%d waits=%d err=%v", attempts, waits, err)
	}
}
