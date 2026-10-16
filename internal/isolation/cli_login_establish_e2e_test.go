package isolation

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestCLILoginDoesNotDelegateCredentialEstablishment(t *testing.T) {
	for _, kind := range []string{"oidc", "oauth2"} {
		for _, transport := range []string{"loopback", "device"} {
			t.Run(kind+"/"+transport, func(t *testing.T) {
				forEngines(t, func(t *testing.T, db *store.DB) {
					ctx := t.Context()
					h, browser, establish := socialCredentialFixture(t, db, kind)
					now := time.Now().UTC()
					h.auth.Now = func() time.Time { return now }
					h.auth.ReauthWindow = 5 * time.Minute
					browser, err := establish(browser.SessionToken)
					if err != nil {
						t.Fatal(err)
					}
					if browser.Artifact != service.ArtifactBrowser {
						t.Fatalf("fixture artifact = %q, want browser", browser.Artifact)
					}
					startApproved := func(state string) (service.CLILoginStart, string, service.CLIReauthApproval) {
						t.Helper()
						if transport == "loopback" {
							start, verifier := loginStart(t, h.auth, state)
							return start, verifier, loginApprove(t, h.auth, browser.SessionToken, start.State)
						}
						start, err := h.auth.StartCLILogin(ctx, "device", "", "", "")
						if err != nil {
							t.Fatal(err)
						}
						return start, "", loginApprove(t, h.auth, browser.SessionToken, start.UserCode)
					}
					redeem := func(start service.CLILoginStart, verifier string, approval service.CLIReauthApproval) (service.LoginResult, string, error) {
						if transport == "loopback" {
							login, err := h.auth.RedeemCLILogin(ctx, approval.Code, verifier, loginRedirect)
							return login, "", err
						}
						now = now.Add(time.Duration(start.Interval) * time.Second)
						attempt, err := h.auth.PollCLIDevice(ctx, start.DeviceCode)
						return attempt.Login, attempt.Status, err
					}
					initialState, _ := pkcePair("cli-establish-initial")
					start, verifier, approval := startApproved(initialState)
					cli, status, err := redeem(start, verifier, approval)
					if err != nil || status != "" || cli.SessionToken == "" {
						t.Fatalf("CLI redemption status=%q: %v", status, err)
					}
					assertCLISnapshot(t, h.auth, db, browser, cli)
					password := "a browser establishes this first local password"
					if result, err := h.auth.SetPassword(ctx, cli.SessionToken, password, ""); !errors.Is(err, service.ErrNoProofCredential) || result.SessionToken != "" {
						t.Fatalf("CLI spent browser establishment: %v", err)
					}
					grantRead(t, db, cli.Principal)
					intent, err := service.NewDisclosureReauthIntent(service.PurposeReveal, []string{"env_a1"}, []string{"key_611"})
					if err != nil {
						t.Fatal(err)
					}
					_, stepupChallenge := pkcePair("cli-establish-stepup")
					if _, err := h.auth.StartCLIReauth(ctx, cli.SessionToken, intent, stepupChallenge, loginRedirect); !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "cannot satisfy step-up") {
						t.Fatalf("CLI used inherited establishment for step-up: %v", err)
					}
					if queryInt(t, db, "SELECT COUNT(*) FROM cli_reauth_handoffs") != 0 {
						t.Fatal("impossible step-up wrote a handoff")
					}
					inventory, err := h.auth.TOTPStatus(ctx, browser.SessionToken)
					if err != nil || inventory.Establish == nil || inventory.Establish.Kind != kind || inventory.HasPassword {
						t.Fatalf("browser lost unspent establishment: %+v, %v", inventory, err)
					}
					pendingState, _ := pkcePair("cli-establish-pending")
					pending, pendingVerifier, pendingApproval := startApproved(pendingState)
					reissued, err := h.auth.SetPassword(ctx, browser.SessionToken, password, "")
					if err != nil {
						t.Fatalf("browser could not spend its stamp: %v", err)
					}
					assertSocialReissue(t, h, reissued, kind)
					if _, err := h.auth.Identity(ctx, cli.SessionToken); !errors.Is(err, domain.ErrUnauthenticated) {
						t.Fatalf("password establishment retained CLI session: %v", err)
					}
					if _, err := h.auth.Identity(ctx, browser.SessionToken); !errors.Is(err, domain.ErrUnauthenticated) {
						t.Fatalf("password establishment retained browser session: %v", err)
					}
					if queryInt(t, db, "SELECT COUNT(*) FROM cli_login_handoffs WHERE browser_session_id='"+browser.SessionID+"'") != 0 {
						t.Fatal("password establishment retained approved login handoffs")
					}
					result, status, err := redeem(pending, pendingVerifier, pendingApproval)
					if result.SessionToken != "" {
						t.Fatal("retired approval minted a CLI session")
					}
					if transport == "loopback" {
						if !errors.Is(err, service.ErrCLILoginInvalid) {
							t.Fatalf("retired loopback approval: %v", err)
						}
					} else if err != nil || status != "expired_token" {
						t.Fatalf("retired device approval status=%q: %v", status, err)
					}
				})
			})
		}
	}
}
