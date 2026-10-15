package isolation

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Exercise the last usable authentication graph through the real runtime
// fence. A missing active configuration produces the same Capture refusal as
// a failed activation, without mocking the metadata read or HTTP middleware.
func TestCLILoginRuntimeRecovery(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := oidcAdmin(t, db)
		auth := admin.auth
		clock := time.Now().UTC()
		auth.Now = func() time.Time { return clock }
		browser, err := auth.LocalLogin(t.Context(), "oidc-admin", admin.password, service.ArtifactBrowser)
		if err != nil {
			t.Fatal(err)
		}
		config := &service.SelfConfig{DB: db}
		if _, err := config.Capture(t.Context()); !errors.Is(err, service.ErrSelfConfigUnavailable) {
			t.Fatalf("fixture must refuse runtime capture: %v", err)
		}
		srv := httptest.NewServer(server.New(&server.API{Auth: auth, SelfConfig: config}, nil))
		t.Cleanup(srv.Close)
		call := func(method, path, body, content, artifact string, csrf bool, want int) []byte {
			t.Helper()
			req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+api.PathPrefix+path, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if content != "" {
				req.Header.Set("Content-Type", content)
			}
			switch artifact {
			case "browser":
				req.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: browser.SessionToken})
				if csrf {
					req.AddCookie(&http.Cookie{Name: "__Host-hikyo-csrf", Value: browser.CSRFToken})
					req.Header.Set("X-Hikyo-CSRF", browser.CSRFToken)
				}
			case "cli":
				req.Header.Set("Authorization", "Bearer "+admin.token)
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
			if resp.StatusCode != want {
				t.Fatalf("%s %s: got %d, want %d: %s", method, path, resp.StatusCode, want, raw)
			}
			if err := api.ValidateResponse(req, resp.StatusCode, resp.Header, raw); err != nil {
				t.Fatalf("%s response violates contract: %v", path, err)
			}
			return raw
		}
		postJSON := func(path string, body any, artifact string, csrf bool, want int) []byte {
			t.Helper()
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			return call(http.MethodPost, path, string(raw), "application/json", artifact, csrf, want)
		}
		verifier, challenge := pkcePair("runtime-recovery")
		raw := postJSON("/auth/cli-login/start", apigen.CLILoginStartRequest{
			Transport: "loopback", State: cliLoginState(), PkceChallenge: challenge, RedirectUri: loginRedirect,
		}, "", false, http.StatusCreated)
		var started apigen.CLILoginStart
		if err := json.Unmarshal(raw, &started); err != nil {
			t.Fatal(err)
		}
		transaction := "/auth/cli-login/transactions/" + started.State
		call(http.MethodGet, transaction, "", "", "", false, http.StatusUnauthorized)
		call(http.MethodGet, transaction, "", "", "cli", false, http.StatusUnauthorized)
		call(http.MethodGet, transaction, "", "", "browser", false, http.StatusOK)
		approval := apigen.CLILoginApproveRequest{Reference: started.State}
		postJSON("/auth/cli-login/approve", approval, "", false, http.StatusUnauthorized)
		postJSON("/auth/cli-login/approve", approval, "cli", false, http.StatusUnauthorized)
		postJSON("/auth/cli-login/approve", approval, "browser", false, http.StatusUnauthorized)
		raw = postJSON("/auth/cli-login/approve", approval, "browser", true, http.StatusOK)
		var approved apigen.CLILoginApproved
		if err := json.Unmarshal(raw, &approved); err != nil {
			t.Fatal(err)
		}
		redeem := apigen.CLILoginRedeemRequest{Code: approved.Code, PkceVerifier: strings.Repeat("Z", 43), RedirectUri: loginRedirect}
		postJSON("/auth/cli-login/redeem", redeem, "", false, http.StatusForbidden)
		redeem.PkceVerifier = verifier
		raw = postJSON("/auth/cli-login/redeem", redeem, "", false, http.StatusOK)
		var login apigen.LoginResult
		if err := json.Unmarshal(raw, &login); err != nil {
			t.Fatal(err)
		}
		if login.Session.Artifact != "cli" || login.SessionToken == nil || *login.SessionToken == "" {
			t.Fatal("loopback recovery did not mint a CLI session")
		}
		if _, err := auth.Identity(t.Context(), *login.SessionToken); err != nil {
			t.Fatalf("loopback recovery session is unusable: %v", err)
		}
		postJSON("/auth/cli-login/redeem", redeem, "", false, http.StatusForbidden)

		raw = call(http.MethodPost, "/auth/cli-login/device/start", "client_id=hikyo-cli", "application/x-www-form-urlencoded", "", false, http.StatusOK)
		var device apigen.CLIDeviceStart
		if err := json.Unmarshal(raw, &device); err != nil {
			t.Fatal(err)
		}
		poll := url.Values{"client_id": {"hikyo-cli"}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {device.DeviceCode}}.Encode()
		clock = clock.Add(5 * time.Second)
		raw = call(http.MethodPost, "/auth/cli-login/device/token", poll, "application/x-www-form-urlencoded", "", false, http.StatusBadRequest)
		var pending apigen.CLIDeviceError
		if err := json.Unmarshal(raw, &pending); err != nil || pending.Error != "authorization_pending" {
			t.Fatalf("device poll must reach pending state: %s, %v", raw, err)
		}
		call(http.MethodGet, "/auth/cli-login/transactions/"+device.UserCode, "", "", "browser", false, http.StatusOK)
		postJSON("/auth/cli-login/approve", apigen.CLILoginApproveRequest{Reference: device.UserCode}, "browser", true, http.StatusOK)
		clock = clock.Add(5 * time.Second)
		raw = call(http.MethodPost, "/auth/cli-login/device/token", poll, "application/x-www-form-urlencoded", "", false, http.StatusOK)
		var token apigen.CLIDeviceToken
		if err := json.Unmarshal(raw, &token); err != nil {
			t.Fatal(err)
		}
		if token.Session.Artifact != "cli" || token.AccessToken == "" {
			t.Fatal("device recovery did not mint a CLI session")
		}
		if _, err := auth.Identity(t.Context(), token.AccessToken); err != nil {
			t.Fatalf("device recovery session is unusable: %v", err)
		}
		// Authentication availability never opens the business-operation fence.
		postJSON("/orgs", apigen.CreateOrgRequest{Name: "still-fenced"}, "cli", false, http.StatusServiceUnavailable)
	})
}
