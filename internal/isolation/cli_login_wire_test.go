package isolation

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// This is the real router, CSRF gate, form decoder, admission controller and
// datastore. Time is controlled; no protocol or authorization handler is mocked.
func TestCLILoginDeviceWireAdmission(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := oidcAdmin(t, db)
		auth := admin.auth
		ctx := t.Context()
		clock := time.Now().UTC()
		auth.Now = func() time.Time { return clock }
		browser, err := auth.LocalLogin(ctx, "oidc-admin", admin.password, service.ArtifactBrowser)
		if err != nil {
			t.Fatal(err)
		}
		limiter, err := admission.New(admission.Config{ArgonMemoryKiB: crypto.PasswordFloor.MemoryKiB, Now: func() time.Time { return clock }})
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(server.New(&server.API{Auth: auth, Admission: limiter}, nil))
		t.Cleanup(srv.Close)
		call := func(path, body, content string, cookie bool) (int, http.Header, []byte) {
			t.Helper()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+api.PathPrefix+path, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", content)
			if cookie {
				req.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: browser.SessionToken})
				req.AddCookie(&http.Cookie{Name: "__Host-hikyo-csrf", Value: browser.CSRFToken})
				req.Header.Set("X-Hikyo-CSRF", browser.CSRFToken)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			raw, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if err := api.ValidateResponse(req, resp.StatusCode, resp.Header, raw); err != nil {
				t.Fatalf("%s status%d invalid response: %v\n%s", path, resp.StatusCode, err, raw)
			}
			return resp.StatusCode, resp.Header, raw
		}
		for _, bad := range []struct{ path, body, content string }{
			{"/auth/cli-login/device/start", "", "application/x-www-form-urlencoded"},
			{"/auth/cli-login/device/start", "client_id=other", "application/x-www-form-urlencoded"},
			{"/auth/cli-login/device/start", "client_id=hikyo-cli&client_id=other", "application/x-www-form-urlencoded"},
			{"/auth/cli-login/device/start", `{"client_id":"hikyo-cli"}`, "application/json"},
			{"/auth/cli-login/device/token", "device_code=wrong&client_id=hikyo-cli", "application/x-www-form-urlencoded"},
			{"/auth/cli-login/device/token", "device_code=wrong&client_id=hikyo-cli&grant_type=other", "application/x-www-form-urlencoded"},
			{"/auth/cli-login/device/token", "device_code=wrong&client_id=other&grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Adevice_code", "application/x-www-form-urlencoded"},
			{"/auth/cli-login/device/token", "device_code=wrong&client_id=hikyo-cli&grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Adevice_code&grant_type=other", "application/x-www-form-urlencoded"},
		} {
			status, _, raw := call(bad.path, bad.body, bad.content, false)
			if status != 400 {
				t.Fatalf("malformed device request accepted for %s (%s): status%d body%s", bad.path, bad.body, status, raw)
			}
		}
		status, headers, raw := call("/auth/cli-login/device/start", "client_id=hikyo-cli", "application/x-www-form-urlencoded", false)
		if status != 200 || headers.Get("Cache-Control") != "no-store" || headers.Get("Pragma") != "no-cache" {
			t.Fatalf("start status%d headers%v body%s", status, headers, raw)
		}
		var start apigen.CLIDeviceStart
		if err := json.Unmarshal(raw, &start); err != nil {
			t.Fatal(err)
		}
		if start.ExpiresIn != 900 || start.Interval != 5 {
			t.Fatalf("RFC start %+v", start)
		}
		body := url.Values{"client_id": {"hikyo-cli"}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {start.DeviceCode}}.Encode()
		throttled := false
		pendingAfterMinute := false
		for range 15 {
			clock = clock.Add(5 * time.Second)
			status, headers, raw = call("/auth/cli-login/device/token", body, "application/x-www-form-urlencoded", false)
			if status == 429 {
				throttled = true
				wait, err := strconv.Atoi(headers.Get("Retry-After"))
				if err != nil || wait <= 0 {
					t.Fatalf("invalid Retry-After: %v", headers)
				}
				clock = clock.Add(time.Duration(wait) * time.Second)
				continue
			}
			var poll apigen.CLIDeviceError
			if status != 400 || json.Unmarshal(raw, &poll) != nil || poll.Error != "authorization_pending" {
				t.Fatalf("poll status%d body%s", status, raw)
			}
			if clock.After(start.ExpiresAt.Add(-service.CLIDeviceLifetime).Add(time.Minute)) {
				pendingAfterMinute = true
			}
		}
		if !throttled || !pendingAfterMinute {
			t.Fatalf("throttled=%t pendingAfterMinute=%t", throttled, pendingAfterMinute)
		}
		clock = clock.Add(time.Minute)
		approval, _ := json.Marshal(apigen.CLILoginApproveRequest{Reference: start.UserCode})
		status, _, raw = call("/auth/cli-login/approve", string(approval), "application/json", true)
		if status != 200 {
			t.Fatalf("cookie approval status%d body%s", status, raw)
		}
		clock = clock.Add(5 * time.Second)
		status, headers, raw = call("/auth/cli-login/device/token", body, "application/x-www-form-urlencoded", false)
		if status != 200 || headers.Get("Cache-Control") != "no-store" {
			t.Fatalf("token status%d headers%v body%s", status, headers, raw)
		}
		var token apigen.CLIDeviceToken
		if err := json.Unmarshal(raw, &token); err != nil {
			t.Fatal(err)
		}
		if token.TokenType != "Bearer" || token.ExpiresIn <= 0 || token.Session.Artifact != "cli" || token.AccessToken == "" {
			t.Fatalf("RFC token metadata invalid")
		}
		if _, err := auth.Identity(ctx, token.AccessToken); err != nil {
			t.Fatal(err)
		}
		if _, err := auth.Identity(ctx, browser.SessionToken); err != nil {
			t.Fatalf("browser did not retain session: %v", err)
		}
	})
}
