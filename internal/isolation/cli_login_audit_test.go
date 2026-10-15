package isolation

import (
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The reason-pinned preparation routes must lead to a real audited completion,
// not a declaration attached to routes that never emit the claimed events.
func TestCLILoginSuccessAudit(t *testing.T) {
	for _, transport := range []string{"loopback", "device"} {
		t.Run(transport, func(t *testing.T) {
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
				logins := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.login' AND outcome='success'")
				sessions := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.session_created' AND outcome='success'")
				assertCounts := func(delta int64) {
					t.Helper()
					if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.login' AND outcome='success'"); got != logins+delta {
						t.Fatalf("successful login events: got %d want %d", got, logins+delta)
					}
					if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.session_created' AND outcome='success'"); got != sessions+delta {
						t.Fatalf("session creation events: got %d want %d", got, sessions+delta)
					}
				}
				var start service.CLILoginStart
				var verifier, reference string
				if transport == "loopback" {
					start, verifier = loginStart(t, auth, cliLoginState())
					reference = start.State
				} else {
					start, err = auth.StartCLILogin(ctx, "device", "", "", "")
					if err != nil {
						t.Fatal(err)
					}
					reference = start.UserCode
				}
				assertCounts(0)
				if _, err := auth.CLILoginTransaction(ctx, service.Bearer(browser.SessionToken), reference); err != nil {
					t.Fatal(err)
				}
				assertCounts(0)
				approval := loginApprove(t, auth, browser.SessionToken, reference)
				assertCounts(0)
				var cli service.LoginResult
				if transport == "loopback" {
					cli, err = auth.RedeemCLILogin(ctx, approval.Code, verifier, loginRedirect)
				} else {
					clock = clock.Add(5 * time.Second)
					var attempt service.CLIDeviceAttempt
					attempt, err = auth.PollCLIDevice(ctx, start.DeviceCode)
					if attempt.Status != "" {
						t.Fatalf("device completion status: %s", attempt.Status)
					}
					cli = attempt.Login
				}
				if err != nil {
					t.Fatal(err)
				}
				assertCounts(1)
				if cli.SessionID == "" {
					t.Fatal("audited completion returned no CLI session")
				}
				if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type IN ('auth.login','auth.session_created') AND outcome='success' AND object_id='"+cli.SessionID+"'"); got != 2 {
					t.Fatalf("completion events do not name the issued CLI session: %d", got)
				}
			})
		})
	}
}
