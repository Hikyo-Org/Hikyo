package clientauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/clienterr"
)

// fakeTransport answers the loopback login and reauth routes in memory.
type fakeTransport struct {
	t        *testing.T
	start    apigen.CLILoginStartRequest
	redeem   apigen.CLILoginRedeemRequest
	reauth   apigen.CLIReauthStartRequest
	reauthed apigen.CLIReauthRedeemRequest
}

func (f *fakeTransport) Origin() string { return "https://hikyo.example" }

func (f *fakeTransport) Do(_ context.Context, method, path string, body, out any) error {
	if method != http.MethodPost {
		f.t.Fatalf("method %s", method)
	}
	switch path {
	case api.PathPrefix + "/auth/cli-login/start":
		f.start = body.(apigen.CLILoginStartRequest)
		*out.(*apigen.CLILoginStart) = apigen.CLILoginStart{State: f.start.State, ExpiresAt: time.Now().Add(time.Minute)}
	case api.PathPrefix + "/auth/cli-login/redeem":
		f.redeem = body.(apigen.CLILoginRedeemRequest)
		token := "session-token"
		*out.(*apigen.LoginResult) = apigen.LoginResult{SessionToken: &token}
	case api.PathPrefix + "/auth/cli-reauth/start":
		f.reauth = body.(apigen.CLIReauthStartRequest)
		*out.(*apigen.CLIReauthStart) = apigen.CLIReauthStart{State: "hik_1_hs_opaque", ExpiresAt: time.Now().Add(time.Minute)}
	case api.PathPrefix + "/auth/cli-reauth/redeem":
		f.reauthed = body.(apigen.CLIReauthRedeemRequest)
		*out.(*apigen.CLIReauthRedeemed) = apigen.CLIReauthRedeemed{SessionToken: "rotated", SessionId: "ses_new"}
	default:
		f.t.Fatalf("unexpected path %s", path)
	}
	return nil
}

func (f *fakeTransport) PostForm(context.Context, string, url.Values) (int, http.Header, []byte, error) {
	f.t.Fatal("unexpected form post")
	return 0, nil, nil, nil
}

func (f *fakeTransport) ResponseError(status int, _ http.Header, _ []byte) error {
	return clienterr.Failf(clienterr.ExitInternal, "status %d", status)
}

func (f *fakeTransport) Wait(context.Context, time.Duration) error { return nil }

func callback(t *testing.T, redirect, code, state string) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, redirect+"?"+url.Values{"code": {code}, "state": {state}}.Encode(), nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("callback status %d", response.StatusCode)
	}
}

func TestLoginLoopbackRedeemsWithMatchingPKCEOnLoopback(t *testing.T) {
	tr := &fakeTransport{t: t}
	result, err := LoginLoopback(t.Context(), tr, "oidc:work", func(target string) error {
		u, err := url.Parse(target)
		if err != nil {
			return err
		}
		if u.String() != "https://hikyo.example/login/cli?provider=oidc%3Awork&state="+url.QueryEscape(tr.start.State) {
			t.Errorf("browser target %q", u)
		}
		callback(t, tr.start.RedirectUri, "one-time-code", tr.start.State)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionToken == nil || *result.SessionToken != "session-token" {
		t.Fatalf("result %+v", result)
	}
	if !strings.HasPrefix(tr.start.RedirectUri, "http://127.0.0.1:") || !strings.HasSuffix(tr.start.RedirectUri, "/callback") {
		t.Fatalf("redirect %q is not an ephemeral loopback callback", tr.start.RedirectUri)
	}
	digest := sha256.Sum256([]byte(tr.redeem.PkceVerifier))
	if tr.start.PkceChallenge != base64.RawURLEncoding.EncodeToString(digest[:]) || tr.redeem.Code != "one-time-code" || tr.redeem.RedirectUri != tr.start.RedirectUri {
		t.Fatalf("redeem %+v does not answer start %+v", tr.redeem, tr.start)
	}
}

func TestLoginLoopbackRefusesBrowserFailureAndCancellation(t *testing.T) {
	tr := &fakeTransport{t: t}
	_, err := LoginLoopback(t.Context(), tr, "", func(string) error { return errors.New("no browser") })
	var failure *clienterr.Error
	if !errors.As(err, &failure) || failure.Code != clienterr.ExitUnavailable {
		t.Fatalf("browser failure = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	_, err = LoginLoopback(ctx, tr, "", func(string) error { cancel(); return nil })
	if !errors.As(err, &failure) || failure.Code != clienterr.ExitAuth {
		t.Fatalf("cancelled login = %v", err)
	}
}

func TestReauthCarriesIntentAndRotatesAfterExactCallback(t *testing.T) {
	tr := &fakeTransport{t: t}
	rotated, err := Reauth(t.Context(), tr, ReauthRequest{
		Purpose: "adapter", Operation: "adapter.sync", EnvironmentIDs: []string{"env_one"}, KeyIDs: []string{"key_one"},
	}, func(target string) error {
		if target != "https://hikyo.example/reauth/cli?transaction=hik_1_hs_opaque" {
			t.Errorf("browser target %q", target)
		}
		callback(t, tr.reauth.RedirectUri, "reauth-code", "hik_1_hs_opaque")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if rotated != (RotatedSession{Token: "rotated", SessionID: "ses_new"}) {
		t.Fatalf("rotated %+v", rotated)
	}
	if tr.reauth.Purpose != "adapter" || len(tr.reauth.EnvironmentIds) != 1 || tr.reauth.KeyIds == nil || len(*tr.reauth.KeyIds) != 1 {
		t.Fatalf("start request %+v", tr.reauth)
	}
	digest := sha256.Sum256([]byte(tr.reauthed.PkceVerifier))
	if tr.reauth.PkceChallenge != base64.RawURLEncoding.EncodeToString(digest[:]) || tr.reauthed.Code != "reauth-code" {
		t.Fatalf("redeem %+v does not answer start", tr.reauthed)
	}
}

func TestValidDeviceUserCode(t *testing.T) {
	for code, want := range map[string]bool{"ABCD2345": true, "abcd2345": false, "ABCD234": false, "ABCDI234": false, "ABCD234O": false} {
		if got := validDeviceUserCode(code); got != want {
			t.Errorf("validDeviceUserCode(%q) = %t", code, got)
		}
	}
}

func TestValidateSessionRefusesMalformedResult(t *testing.T) {
	if err := ValidateSession(apigen.LoginResult{}); err == nil {
		t.Fatal("empty login result accepted")
	}
}

func TestReauthCallbackRefusesForeignHost(t *testing.T) {
	tr := &fakeTransport{t: t}
	_, err := Reauth(t.Context(), tr, ReauthRequest{Purpose: "adapter", Operation: "adapter.sync", EnvironmentIDs: []string{"env_one"}}, func(string) error {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, tr.reauth.RedirectUri+"?"+url.Values{"code": {"x"}, "state": {"hik_1_hs_opaque"}}.Encode(), nil)
		if err != nil {
			return err
		}
		request.Host = "evil.example"
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return err
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("foreign Host status %d", response.StatusCode)
		}
		// The refused request must not consume the single-use callback.
		callback(t, tr.reauth.RedirectUri, "reauth-code", "hik_1_hs_opaque")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type deviceTransport struct{ fakeTransport }

func (d *deviceTransport) PostForm(_ context.Context, path string, _ url.Values) (int, http.Header, []byte, error) {
	if strings.HasSuffix(path, "/device/start") {
		return 200, nil, []byte(`{"device_code":"dc","user_code":"ABCD2345","verification_uri":"https://hikyo.example/login/cli?device=1","expires_in":600,"interval":5,"expires_at":"2030-01-01T00:00:00Z"}`), nil
	}
	return 200, nil, []byte(`{"access_token":"tok","token_type":"Bearer","expires_in":60}`), nil
}

func TestLoginDeviceNotifyMatchesPrintedPrompt(t *testing.T) {
	var out strings.Builder
	var got DevicePrompt
	_, err := LoginDeviceNotify(t.Context(), &deviceTransport{fakeTransport{t: t}}, &out, "", func(p DevicePrompt) { got = p })
	if err != nil {
		t.Fatal(err)
	}
	if got.VerificationURL != "https://hikyo.example/login/cli?device=1" || got.UserCode != "ABCD2345" || got.ExpiresIn != 10*time.Minute {
		t.Fatalf("prompt %+v", got)
	}
	want := "On another device, visit https://hikyo.example/login/cli?device=1\nEnter code: ABCD2345\nApprove a CLI login for https://hikyo.example.\n"
	if out.String() != want {
		t.Fatalf("printed %q", out.String())
	}
}

// throttledTransport answers the first token poll with a 429 and the second with
// an approval; its ResponseError never returns a clienterr.RateLimitedError.
type throttledTransport struct {
	fakeTransport
	polls int
	waits []time.Duration
}

func (f *throttledTransport) PostForm(context.Context, string, url.Values) (int, http.Header, []byte, error) {
	f.polls++
	if f.polls == 1 {
		return http.StatusTooManyRequests, http.Header{"Retry-After": {"60"}}, nil, nil
	}
	return http.StatusOK, nil, []byte(`{"access_token":"tok","token_type":"Bearer","expires_in":60}`), nil
}

func (f *throttledTransport) Wait(_ context.Context, d time.Duration) error {
	f.waits = append(f.waits, d)
	return nil
}

func TestPollDeviceHonorsRetryAfterWithoutRateLimitedError(t *testing.T) {
	tr := &throttledTransport{fakeTransport: fakeTransport{t: t}}
	result, err := PollDevice(t.Context(), tr, io.Discard, "device-code", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionToken == nil || *result.SessionToken != "tok" {
		t.Fatalf("result %+v", result)
	}
	if len(tr.waits) != 2 || tr.waits[0] != 5*time.Second || tr.waits[1] != 60*time.Second {
		t.Fatalf("waits = %v, want [5s 60s]", tr.waits)
	}
}
