package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/webauthntest"
)

// Exercise the actual router and cookie delivery, not just the service fork.
func TestCLILocalPasswordRequiresEnrolledFactor(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := oidcAdmin(t, db)
		auth := admin.auth
		clock := time.Now().UTC()
		auth.Now = func() time.Time { return clock }
		srv := httptest.NewServer(server.New(&server.API{Auth: auth}, nil))
		defer srv.Close()
		post := cliMFAHTTP(t, srv)
		loginBody := `{"username":"oidc-admin","password":"` + admin.password + `"}`
		// The no-factor optional and required semantics remain available to CLI.
		for _, required := range []bool{false, true} {
			auth.SecondFactorRequired = required
			status, headers, raw := post("/auth/local/login", loginBody, service.LoginResult{})
			if status != 200 || headers.Get("Set-Cookie") != "" {
				t.Fatalf("unenrolled CLI status%d headers%v body%s", status, headers, raw)
			}
			var login apigen.LoginResult
			if err := json.Unmarshal(raw, &login); err != nil || login.SessionToken == nil {
				t.Fatalf("missing CLI token: %s %v", raw, err)
			}
			identity, err := auth.Identity(t.Context(), *login.SessionToken)
			if err != nil || identity.EnrolmentRequired != required {
				t.Fatalf("enrolment gate = %v, want %v: %v", identity.EnrolmentRequired, required, err)
			}
		}
		uri, err := auth.EnrolTOTPStart(t.Context(), admin.token, admin.password)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := auth.EnrolTOTPConfirm(t.Context(), admin.token, totpCode(t, uri, clock)); err != nil {
			t.Fatal(err)
		}
		for _, required := range []bool{false, true} {
			auth.SecondFactorRequired = required
			before := queryInt(t, db, "SELECT COUNT(*) FROM sessions")
			audits := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.login' AND outcome='failure'")
			status, headers, raw := post("/auth/local/login", loginBody, service.LoginResult{})
			if status != 400 || headers.Get("Set-Cookie") != "" || !strings.Contains(string(raw), "hikyo login") {
				t.Fatalf("enrolled CLI status%d, want refusal and browser guidance", status)
			}
			if queryInt(t, db, "SELECT COUNT(*) FROM sessions") != before {
				t.Fatal("refused login minted session")
			}
			if queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.login' AND outcome='failure'") != audits+1 {
				t.Fatal("MFA refusal audit was rolled back")
			}
		}
		status, headers, _ := post("/auth/local/login", `{"username":"oidc-admin","password":"incorrect password"}`, service.LoginResult{})
		if status != 401 || headers.Get("Set-Cookie") != "" {
			t.Fatalf("bad password disclosed enrollment: status%d", status)
		}
		// The existing challenge still produces cookies only, even if a CLI
		// client attempts to select another artifact at completion.
		status, _, raw := post("/auth/local/login", strings.TrimSuffix(loginBody, "}")+`,"artifact":"browser"}`, service.LoginResult{})
		var challenge apigen.LoginChallenge
		if status != 202 || json.Unmarshal(raw, &challenge) != nil {
			t.Fatalf("browser challenge: %d %s", status, raw)
		}
		clock = clock.Add(30 * time.Second)
		status, _, _ = post("/auth/login/challenge/"+string(challenge.ChallengeId)+"/totp", `{"code":"`+totpCode(t, uri, clock)+`","artifact":"cli"}`, service.LoginResult{})
		if status != 400 {
			t.Fatalf("finish accepted artifact override: %d", status)
		}
		status, headers, raw = post("/auth/login/challenge/"+string(challenge.ChallengeId)+"/totp", `{"code":"`+totpCode(t, uri, clock)+`"}`, service.LoginResult{})
		var browser apigen.LoginResult
		if status != 200 || headers.Get("Set-Cookie") == "" || json.Unmarshal(raw, &browser) != nil || browser.SessionToken != nil || browser.Session.Artifact != "browser" {
			t.Fatalf("browser finish artifact leaked: %d %v %s", status, headers, raw)
		}
	})
}

func TestCLILocalPasswordWithPasskeyUsesBrowser(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := bootstrapWebAuthnAdmin(t, db)
		auth := admin.auth
		dev := webauthntest.New(waRPID, waOrigin)
		enrolPasskey(t, auth, t.Context(), admin.token, waPassword, dev)
		srv := httptest.NewServer(server.New(&server.API{Auth: auth}, nil))
		defer srv.Close()
		status, headers, raw := cliMFAHTTP(t, srv)("/auth/local/login", `{"username":"`+waAdmin+`","password":"`+waPassword+`"}`, service.LoginResult{})
		if status != 400 || headers.Get("Set-Cookie") != "" || !strings.Contains(string(raw), "--device") {
			t.Fatalf("passkey-enrolled CLI status%d, want browser guidance", status)
		}
	})
}

func TestCLIHandoffPreservesProviderAssuranceWithLocalFactor(t *testing.T) {
	for _, kind := range []string{"oidc", "oauth2"} {
		t.Run(kind, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h, browser, establish := socialCredentialFixture(t, db, kind)
				browser, err := establish(browser.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				now := time.Now().UTC()
				h.auth.Now = func() time.Time { return now }
				uri, err := h.auth.EnrolTOTPStart(t.Context(), browser.SessionToken, "")
				if err != nil {
					t.Fatal(err)
				}
				browser, err = h.auth.EnrolTOTPConfirm(t.Context(), browser.SessionToken, totpCode(t, uri, now))
				if err != nil {
					t.Fatal(err)
				}
				if browser.Assurance.Method == service.MethodLocalPassword || len(browser.Assurance.Factors) != 1 {
					t.Fatal("fixture did not retain single-factor provider assurance")
				}
				srv := httptest.NewServer(server.New(&server.API{Auth: h.auth}, nil))
				defer srv.Close()
				post := cliMFAHTTP(t, srv)
				start, verifier := loginStart(t, h.auth, cliLoginState())
				status, _, raw := post("/auth/cli-login/approve", `{"reference":"`+start.State+`"}`, browser)
				var approved apigen.CLILoginApproved
				if status != 200 || json.Unmarshal(raw, &approved) != nil {
					t.Fatalf("provider approval status%d", status)
				}
				body, err := json.Marshal(apigen.CLILoginRedeemRequest{Code: approved.Code, PkceVerifier: verifier, RedirectUri: loginRedirect})
				if err != nil {
					t.Fatal(err)
				}
				status, _, raw = post("/auth/cli-login/redeem", string(body), service.LoginResult{})
				var cli apigen.LoginResult
				if status != 200 || json.Unmarshal(raw, &cli) != nil || cli.SessionToken == nil {
					t.Fatalf("provider redemption status%d", status)
				}
				id, err := h.auth.Identity(t.Context(), *cli.SessionToken)
				if err != nil || id.Assurance.Method != browser.Assurance.Method || authz.AdequateAssurance(authz.Assurance{Method: id.Assurance.Method, Factors: id.Assurance.Factors}) {
					t.Fatalf("provider assurance changed: %v", err)
				}
				if sessionProviderProvenance(t, db, cli.Session.Id) != sessionProviderProvenance(t, db, browser.SessionID) {
					t.Fatal("provider provenance changed")
				}
			})
		})
	}
}

func cliMFAHTTP(t *testing.T, srv *httptest.Server) func(string, string, service.LoginResult) (int, http.Header, []byte) {
	t.Helper()
	return func(path, body string, browser service.LoginResult) (int, http.Header, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+api.PathPrefix+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if browser.SessionToken != "" {
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
			t.Fatalf("%s invalid response: %v", path, err)
		}
		return resp.StatusCode, resp.Header, raw
	}
}

func TestCLIHandoffRechecksEnrolledFactor(t *testing.T) {
	for _, transport := range []string{"loopback", "device"} {
		for _, enrolAfterApproval := range []bool{false, true} {
			name := transport + "/enrol-before-approval"
			if enrolAfterApproval {
				name = transport + "/enrol-after-approval"
			}
			t.Run(name, func(t *testing.T) {
				forEngines(t, func(t *testing.T, db *store.DB) {
					admin := oidcAdmin(t, db)
					auth := admin.auth
					clock := time.Now().UTC()
					auth.Now = func() time.Time { return clock }
					browser, err := auth.LocalLogin(t.Context(), "oidc-admin", admin.password, service.ArtifactBrowser)
					if err != nil {
						t.Fatal(err)
					}
					srv := httptest.NewServer(server.New(&server.API{Auth: auth}, nil))
					defer srv.Close()
					post := cliMFAHTTP(t, srv)
					expired, _ := loginStart(t, auth, strings.Repeat("E", 43))
					clock = clock.Add(service.CLILoginLifetime + time.Second)
					uri, err := auth.EnrolTOTPStart(t.Context(), browser.SessionToken, admin.password)
					if err != nil {
						t.Fatal(err)
					}
					start, verifier := loginStart(t, auth, cliLoginState())
					reference := start.State
					if transport == "device" {
						start, err = auth.StartCLILogin(t.Context(), "device", "", "", "")
						if err != nil {
							t.Fatal(err)
						}
						reference = start.UserCode
					}
					approve := func() (int, []byte) {
						status, _, raw := post("/auth/cli-login/approve", `{"reference":"`+reference+`"}`, browser)
						return status, raw
					}
					var approved apigen.CLILoginApproved
					if enrolAfterApproval {
						status, raw := approve()
						if status != 200 || json.Unmarshal(raw, &approved) != nil {
							t.Fatalf("approve before enrol: %d %s", status, raw)
						}
					}
					if enrolAfterApproval {
						// Ordinary enrollment revokes the browser and cascades the
						// handoff. Promote the real pending fixture through its store
						// seam without revocation to prove the live inventory guard
						// independently of that existing invalidation defense.
						err = tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
							account, err := az.AccountByPrincipal(ctx, admin.boot.PrincipalID)
							if err != nil {
								return err
							}
							pending, err := az.PendingTOTP(ctx, account.ID)
							if err != nil {
								return err
							}
							promoted, err := az.ConfirmTOTP(ctx, pending.ID, pending.RowVersion, clock.Unix()/30, clock)
							if err != nil {
								return err
							}
							if !promoted {
								return errors.New("fixture factor promotion lost")
							}
							return nil
						})
						if err != nil {
							t.Fatal(err)
						}
					} else {
						// Enrollment reissues a live password-only acting browser.
						browser, err = auth.EnrolTOTPConfirm(t.Context(), browser.SessionToken, totpCode(t, uri, clock))
						if err != nil {
							t.Fatal(err)
						}
					}
					before := queryInt(t, db, "SELECT COUNT(*) FROM sessions")
					if !enrolAfterApproval {
						status, raw := approve()
						var refusal apigen.Error
						if err := json.Unmarshal(raw, &refusal); err != nil {
							t.Fatal(err)
						}
						if status != 400 || refusal.Error.Code != "cli_login_factor_required" || !strings.Contains(string(raw), "Complete your enrolled authenticator or passkey") {
							t.Fatalf("weak browser approved after enrolment: %d %s", status, raw)
						}
						for _, invalid := range []string{strings.Repeat("Z", 43), expired.State} {
							status, _, _ := post("/auth/cli-login/approve", `{"reference":"`+invalid+`"}`, browser)
							if status != 403 {
								t.Fatalf("invalid or expired reference disclosed assurance requirement: %d", status)
							}
						}
						if queryInt(t, db, "SELECT COUNT(*) FROM sessions") != before {
							t.Fatal("weak approval minted a session")
						}
						// Recover through the browser's actual logout and password +
						// TOTP HTTP ceremonies, preserving the pending CLI reference.
						status, _, _ = post("/auth/logout", "", browser)
						if status != 204 {
							t.Fatalf("logout: %d", status)
						}
						status, _, raw = post("/auth/local/login", `{"username":"oidc-admin","password":"`+admin.password+`","artifact":"browser"}`, service.LoginResult{})
						var challenge apigen.LoginChallenge
						if status != 202 || json.Unmarshal(raw, &challenge) != nil {
							t.Fatalf("recovery sign-in: %d", status)
						}
						clock = clock.Add(30 * time.Second)
						status, headers, raw := post("/auth/login/challenge/"+string(challenge.ChallengeId)+"/totp", `{"code":"`+totpCode(t, uri, clock)+`"}`, service.LoginResult{})
						var completed apigen.LoginResult
						if status != 200 || json.Unmarshal(raw, &completed) != nil || completed.SessionToken != nil {
							t.Fatalf("recovery factor: %d", status)
						}
						browser = service.LoginResult{}
						for _, cookie := range (&http.Response{Header: headers}).Cookies() {
							if cookie.Name == "__Host-hikyo" {
								browser.SessionToken = cookie.Value
							}
							if cookie.Name == "__Host-hikyo-csrf" {
								browser.CSRFToken = cookie.Value
							}
						}
						if browser.SessionToken == "" || browser.CSRFToken == "" {
							t.Fatal("browser recovery omitted cookies")
						}
						status, raw = approve()
						if status != 200 || json.Unmarshal(raw, &approved) != nil {
							t.Fatalf("approval after MFA: %d", status)
						}
						var cli service.LoginResult
						if transport == "loopback" {
							cli, err = auth.RedeemCLILogin(t.Context(), approved.Code, verifier, loginRedirect)
						} else {
							var attempt service.CLIDeviceAttempt
							attempt, err = auth.PollCLIDevice(t.Context(), start.DeviceCode)
							if attempt.Status != "" {
								t.Fatalf("device recovery status: %s", attempt.Status)
							}
							cli = attempt.Login
						}
						if err != nil || cli.Artifact != service.ArtifactCLI || cli.SessionToken == "" || cli.Assurance.Method != service.MethodLocalPassword || len(cli.Assurance.Factors) != 2 {
							t.Fatalf("recovered CLI assurance: %v", err)
						}
						if queryInt(t, db, "SELECT COUNT(*) FROM reauth_windows WHERE session_id='"+cli.SessionID+"'") != 0 {
							t.Fatal("login recovery granted operation proof")
						}
						return
					} else if transport == "loopback" {
						body, err := json.Marshal(apigen.CLILoginRedeemRequest{Code: approved.Code, PkceVerifier: verifier, RedirectUri: loginRedirect})
						if err != nil {
							t.Fatal(err)
						}
						status, headers, raw := post("/auth/cli-login/redeem", string(body), service.LoginResult{})
						if status != 403 || headers.Get("Set-Cookie") != "" {
							t.Fatalf("weak snapshot redeemed: %d %s", status, raw)
						}
					} else {
						clock = clock.Add(6 * time.Second)
						req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+api.PathPrefix+"/auth/cli-login/device/token", strings.NewReader("client_id=hikyo-cli&grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Adevice_code&device_code="+start.DeviceCode))
						if err != nil {
							t.Fatal(err)
						}
						req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
						resp, err := srv.Client().Do(req)
						if err != nil {
							t.Fatal(err)
						}
						raw, err := io.ReadAll(resp.Body)
						resp.Body.Close()
						if err != nil {
							t.Fatal(err)
						}
						if err := api.ValidateResponse(req, resp.StatusCode, resp.Header, raw); err != nil {
							t.Fatal(err)
						}
						if resp.StatusCode != 400 || !strings.Contains(string(raw), "access_denied") {
							t.Fatalf("weak device snapshot redeemed: %d %s", resp.StatusCode, raw)
						}
					}
					if queryInt(t, db, "SELECT COUNT(*) FROM sessions") != before {
						t.Fatal("weak handoff minted a session")
					}
				})
			})
		}
	}
}
