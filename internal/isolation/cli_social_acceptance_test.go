package isolation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

type socialAcceptanceTerminal struct{ io.ReadWriter }

func (socialAcceptanceTerminal) Close() error { return nil }

// TestCLISocialProviderAcceptance joins the actual CLI dispatcher, protected
// trust/session custody, TLS router, OIDC exchange and loopback callback. The
// injected browser opener drives the browser HTTP ceremony without UI mocks;
// desktop/mobile browser flows separately prove the rendered continuation.
func TestCLISocialProviderAcceptance(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		h := newSignupHarness(t, db)
		srv := httptest.NewTLSServer(server.New(&server.API{Auth: h.auth, Registration: h.reg}, nil))
		t.Cleanup(srv.Close)
		h.auth.ExternalOrigin = srv.URL
		h.provider("acceptance", service.ProviderInput{})
		if _, err := h.reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{
			External: []service.RegistrationExternalEntry{{Provider: domain.ProviderRef{Kind: domain.ProviderOIDC, Slug: "acceptance"}}},
			Landing:  service.RegistrationLanding{Kind: service.LandingNone},
		}, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := h.login("acceptance", "known", "sign-up", "", googleClaims("cli-acceptance@example.test")); err != nil {
			t.Fatal(err)
		}
		registrations := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type LIKE 'registration.%'")
		stateDir := t.TempDir()
		env := cli.Env{Getenv: func(name string) string {
			if name == "HIKYO_STATE_DIR" {
				return stateDir
			}
			return ""
		}}
		state, err := cli.NewState(env)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.Trust().Put(cli.TrustEntry{Name: "acceptance", Origin: srv.URL, SPKIPin: cli.SPKIFingerprint(srv.Certificate())}); err != nil {
			t.Fatal(err)
		}
		terminal, err := disclose.NewTerminalSession(socialAcceptanceTerminal{ReadWriter: &bytes.Buffer{}})
		if err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		var browserToken string
		opened := false
		ios := cli.IO{Env: env, Workdir: t.TempDir(), Stdout: &stdout, Stderr: &stderr, TerminalSession: terminal,
			OpenURL: func(target string) error {
				opened = true
				page, err := url.Parse(target)
				if err != nil {
					return err
				}
				if page.Path != "/login/cli" || page.Query().Get("provider") != "oidc:acceptance" {
					return fmt.Errorf("wrong provider continuation")
				}
				browser := srv.Client()
				browser.Jar, err = cookiejar.New(nil)
				if err != nil {
					return err
				}
				browser.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
				post := func(path string, input, output any, csrf string) error {
					raw, err := json.Marshal(input)
					if err != nil {
						return err
					}
					req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+api.PathPrefix+path, bytes.NewReader(raw))
					if err != nil {
						return err
					}
					req.Header.Set("Content-Type", "application/json")
					if csrf != "" {
						req.Header.Set("X-Hikyo-CSRF", csrf)
					}
					response, err := browser.Do(req)
					if err != nil {
						return err
					}
					defer response.Body.Close()
					if response.StatusCode != http.StatusOK {
						raw, _ := io.ReadAll(response.Body)
						return fmt.Errorf("%s status %d: %s", path, response.StatusCode, raw)
					}
					return json.NewDecoder(response.Body).Decode(output)
				}
				var started apigen.OidcStartResult
				if err := post("/auth/oidc/acceptance/start", map[string]any{"purpose": "login", "intent": "sign-in", "browser": true}, &started, ""); err != nil {
					return err
				}
				code, oidcState := driveIdP(t, started.AuthorizationUrl+"&sub=known")
				response, err := browser.Get(srv.URL + api.PathPrefix + "/auth/oidc/acceptance/callback?" + url.Values{"code": {code}, "state": {oidcState}}.Encode())
				if err != nil {
					return err
				}
				response.Body.Close()
				if response.StatusCode != http.StatusSeeOther && response.StatusCode != http.StatusFound {
					return fmt.Errorf("OIDC callback status %d", response.StatusCode)
				}
				var csrf string
				for _, cookie := range browser.Jar.Cookies(page) {
					switch cookie.Name {
					case "__Host-hikyo":
						browserToken = cookie.Value
					case "__Host-hikyo-csrf":
						csrf = cookie.Value
					}
				}
				if browserToken == "" || csrf == "" {
					return fmt.Errorf("federated callback did not establish browser cookies")
				}
				var approved apigen.CLILoginApproved
				if err := post("/auth/cli-login/approve", apigen.CLILoginApproveRequest{Reference: page.Query().Get("state")}, &approved, csrf); err != nil {
					return err
				}
				callback, err := url.Parse(approved.RedirectUri)
				if err != nil {
					return err
				}
				callback.RawQuery = url.Values{"code": {approved.Code}, "state": {approved.State}}.Encode()
				completed, err := http.DefaultClient.Get(callback.String())
				if err != nil {
					return err
				}
				completed.Body.Close()
				if completed.StatusCode != http.StatusOK {
					return fmt.Errorf("loopback callback status %d", completed.StatusCode)
				}
				return nil
			},
		}
		if exit := cli.Run(ctx, ios, []string{"login", srv.URL, "--provider", "oidc:acceptance", "--name", "acceptance"}); exit != cli.ExitOK {
			t.Fatalf("CLI exit %d: %s", exit, stderr.String())
		}
		if !opened {
			t.Fatal("CLI did not open its federated continuation")
		}
		sessions, err := state.Sessions()
		if err != nil {
			t.Fatal(err)
		}
		artifact, exists := sessions["acceptance"]
		if !exists || len(sessions) != 1 || artifact.Token == "" || artifact.Token == browserToken {
			t.Fatal("CLI did not retain a separate session in custody")
		}
		identity, err := h.auth.Identity(ctx, artifact.Token)
		if err != nil || identity.Artifact != service.ArtifactCLI {
			t.Fatalf("persisted CLI identity: %+v %v", identity, err)
		}
		browserIdentity, err := h.auth.Identity(ctx, browserToken)
		if err != nil || browserIdentity.Principal != identity.Principal || browserIdentity.Artifact != service.ArtifactBrowser {
			t.Fatalf("browser identity not preserved: %+v %v", browserIdentity, err)
		}
		if sessionProviderProvenance(t, db, artifact.SessionID) != sessionProviderProvenance(t, db, browserIdentity.SessionID) {
			t.Fatal("CLI lost federated provider provenance")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type LIKE 'registration.%'"); n != registrations {
			t.Fatal("CLI sign-in entered registration")
		}
		if stdout.Len() != 0 || strings.Contains(stderr.String(), artifact.Token) || strings.Contains(stderr.String(), browserToken) {
			t.Fatal("CLI disclosed authentication artifacts")
		}
	})
}
