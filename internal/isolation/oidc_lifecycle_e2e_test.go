package isolation

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Directory lifecycle events (human-auth ADR, 2026-11-17 amendment), dual
// engine, driven through the service entry point with bodies signed by the
// Standard Webhooks library itself.

const lifecycleTestSecret = "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"

// lifecycleFixture is an admin linked to an OIDC provider that takes clerk
// lifecycle events, plus the two session kinds the sweep scope separates.
type lifecycleFixture struct {
	admin    admin
	db       *store.DB
	slug     string
	subject  string
	password string // the admin's password session token
	provider *service.Providers
	idpIssue string
}

func newLifecycleFixture(t *testing.T, db *store.DB) lifecycleFixture {
	t.Helper()
	ctx := t.Context()
	a := oidcAdmin(t, db)
	providers, idp := configureProvider(t, a.auth, ctx, a.boot.PrincipalID, "clerk-idp", service.ProviderInput{
		DisplayName: "Clerk", ClientID: "c", ClientSecret: "s", Scopes: "openid", Enabled: true,
		LifecycleProfile: strptr(service.LifecycleProfileClerk), LifecycleSecret: lifecycleTestSecret,
	})
	linkOn(t, a.auth, ctx, "clerk-idp", "user_1", a.password)
	pw, err := a.auth.LocalLogin(ctx, "oidc-admin", a.password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	return lifecycleFixture{admin: a, db: db, slug: "clerk-idp", subject: "user_1", password: pw.SessionToken, provider: providers, idpIssue: idp.Issuer()}
}

// send signs body with secret at ts and delivers it under the Svix header
// names, as Clerk does.
func (f lifecycleFixture) send(t *testing.T, slug, secret string, ts time.Time, body string) error {
	t.Helper()
	wh, err := standardwebhooks.NewWebhook(secret)
	if err != nil {
		t.Fatal(err)
	}
	id := "msg_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	sig, err := wh.Sign(id, ts, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	h := http.Header{}
	h.Set("Svix-Id", id)
	h.Set("Svix-Timestamp", strconv.FormatInt(ts.Unix(), 10))
	h.Set("Svix-Signature", sig)
	return f.admin.auth.OIDCLifecycle(t.Context(), slug, h, []byte(body))
}

func (f lifecycleFixture) ok(t *testing.T, body string) {
	t.Helper()
	if err := f.send(t, f.slug, lifecycleTestSecret, time.Now(), body); err != nil {
		t.Fatalf("verified event %s = %v, want success", body, err)
	}
}

func (f lifecycleFixture) live(t *testing.T, token string) bool {
	t.Helper()
	_, err := f.admin.auth.Identity(t.Context(), token)
	if err != nil && !isUnauth(err) {
		t.Fatalf("session probe: %v", err)
	}
	return err == nil
}

func userEvent(typ, id string, extra string) string {
	return fmt.Sprintf(`{"type":%q,"data":{"id":%q%s}}`, typ, id, extra)
}

func countAudit(t *testing.T, db *store.DB, typ string) int64 {
	t.Helper()
	return queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = '"+typ+"'")
}

func TestOIDCLifecycleSweepScope(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newLifecycleFixture(t, db)
		ctx := t.Context()
		oidcSession := oidcLogin(t, f.admin.auth, ctx, f.slug, f.subject).SessionToken
		if !f.live(t, oidcSession) || !f.live(t, f.password) {
			t.Fatal("both sessions must be live before the events")
		}

		// A ban ends only the OIDC session and keeps the link.
		f.ok(t, userEvent("user.updated", f.subject, `,"banned":true`))
		if f.live(t, oidcSession) {
			t.Fatal("a ban left the OIDC session live")
		}
		if !f.live(t, f.password) {
			t.Fatal("a ban ended the password session")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM external_identities"); n != 1 {
			t.Fatalf("a ban removed the link: %d identities", n)
		}
		if countAudit(t, db, "auth.sessions_swept_by_directory") != 1 {
			t.Fatal("ban sweep not audited once")
		}

		// A delete unlinks and ends only the OIDC session.
		oidcSession = oidcLogin(t, f.admin.auth, ctx, f.slug, f.subject).SessionToken
		f.ok(t, userEvent("user.deleted", f.subject, ""))
		if f.live(t, oidcSession) {
			t.Fatal("a delete left the OIDC session live")
		}
		if !f.live(t, f.password) {
			t.Fatal("a delete ended the password session")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM external_identities"); n != 0 {
			t.Fatalf("a delete left %d identities", n)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM password_credentials"); n != 1 {
			t.Fatalf("local credential not retained: %d", n)
		}
		if countAudit(t, db, "auth.identity_unlinked_by_directory") != 1 {
			t.Fatal("unlink not audited once")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.identity_unlinked_by_directory' AND actor_class = 'system'"); n != 1 {
			t.Fatal("unlink not attributed to the system actor")
		}
		// The password account still has a credential: the zero-credential event stays quiet.
		if countAudit(t, db, "auth.account_no_login_credential") != 0 {
			t.Fatal("zero-credential event fired for an account that kept its password")
		}
	})
}

func TestOIDCLifecycleUniform(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newLifecycleFixture(t, db)
		ctx := t.Context()
		oidcSession := oidcLogin(t, f.admin.auth, ctx, f.slug, f.subject).SessionToken
		audits := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events")
		identities := queryInt(t, db, "SELECT COUNT(*) FROM external_identities")

		// Verified events that change nothing all succeed alike.
		for name, body := range map[string]string{
			"unknown subject": userEvent("user.deleted", "user_nobody", ""),
			"ignored type":    userEvent("session.created", f.subject, ""),
			"locked":          userEvent("user.updated", f.subject, `,"locked":true`),
			"not banned":      userEvent("user.updated", f.subject, `,"banned":false`),
			"not json":        `not json`,
			"no subject":      `{"type":"user.deleted","data":{}}`,
		} {
			if err := f.send(t, f.slug, lifecycleTestSecret, time.Now(), body); err != nil {
				t.Fatalf("%s = %v, want success", name, err)
			}
		}
		if !f.live(t, oidcSession) {
			t.Fatal("an ignored event ended a session")
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events"); got != audits {
			t.Fatalf("ignored events wrote %d audit rows", got-audits)
		}

		// A replayed delete: the second one is a no-op success.
		del := userEvent("user.deleted", f.subject, "")
		f.ok(t, del)
		audits = queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events")
		f.ok(t, del)
		if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events"); got != audits {
			t.Fatal("a replayed delete wrote audit rows")
		}
		if queryInt(t, db, "SELECT COUNT(*) FROM external_identities") != identities-1 {
			t.Fatal("delete did not remove exactly the one identity")
		}

		// Refusals: one uniform error, and verification precedes parsing.
		wrong := "whsec_" + "MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSx"
		if err := f.send(t, f.slug, wrong, time.Now(), `{malformed`); !isUnauth(err) {
			t.Fatalf("malformed body with a bad signature = %v, want the signature refusal", err)
		}
		if err := f.send(t, f.slug, lifecycleTestSecret, time.Now().Add(-10*time.Minute), del); !isUnauth(err) {
			t.Fatalf("stale timestamp = %v", err)
		}
		if err := f.send(t, "no-such-slug", lifecycleTestSecret, time.Now(), del); !isUnauth(err) {
			t.Fatalf("unknown slug = %v", err)
		}
		// A provider with no profile.
		_, idp2 := configureProvider(t, f.admin.auth, ctx, f.admin.boot.PrincipalID, "plain-idp", service.ProviderInput{
			DisplayName: "Plain", ClientID: "c", ClientSecret: "s", Scopes: "openid", Enabled: true,
		})
		_ = idp2
		if err := f.send(t, "plain-idp", lifecycleTestSecret, time.Now(), del); !isUnauth(err) {
			t.Fatalf("profile-less provider = %v", err)
		}
		// A disabled provider, even with a correct signature.
		if _, err := f.provider.Put(ctx, service.LocalPrincipal(f.admin.boot.PrincipalID), f.slug, service.ProviderInput{
			DisplayName: "Clerk", Issuer: f.idpIssue, ClientID: "c", ClientSecret: "s", Scopes: "openid", Enabled: false,
			LifecycleProfile: strptr(service.LifecycleProfileClerk),
		}); err != nil {
			t.Fatal(err)
		}
		if err := f.send(t, f.slug, lifecycleTestSecret, time.Now(), del); !isUnauth(err) {
			t.Fatalf("disabled provider = %v", err)
		}
	})
}

func TestOIDCLifecycleSecretSealing(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newLifecycleFixture(t, db)
		oidcLogin(t, f.admin.auth, t.Context(), f.slug, f.subject)
		// Swapping the client secret ciphertext into the lifecycle column must not
		// open: the two fields are sealed under distinct AAD.
		execRaw(t, db, "UPDATE oidc_providers SET lifecycle_secret = client_secret WHERE slug = 'clerk-idp'")
		err := f.send(t, f.slug, lifecycleTestSecret, time.Now(), userEvent("user.deleted", f.subject, ""))
		if !isUnauth(err) {
			t.Fatalf("a swapped ciphertext verified: %v", err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM external_identities"); n != 1 {
			t.Fatal("a swapped ciphertext changed state")
		}
	})
}

func TestOIDCLifecycleSecretSurvivesDEKRotation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newLifecycleFixture(t, db)
		ctx := t.Context()
		kr := probeKeyring(t, db)
		actor := service.LocalPrincipal(f.admin.boot.PrincipalID)
		before := queryString(t, db, "SELECT "+hexOf(db, "lifecycle_secret")+" FROM oidc_providers WHERE slug = 'clerk-idp'")
		rotation := &service.Rotation{DB: db, Keyring: kr, RootKey: probeRootSource{db: db}}
		if _, err := rotation.RotateDEK(ctx, actor, service.DEKScope{Instance: true}); err != nil {
			t.Fatalf("rotate-dek --instance: %v", err)
		}
		re := &service.Reencrypt{DB: db, Keyring: kr, ChunkPause: -1}
		if _, err := re.ReencryptInstance(ctx, actor); err != nil {
			t.Fatalf("reencrypt --instance: %v", err)
		}
		after := queryString(t, db, "SELECT "+hexOf(db, "lifecycle_secret")+" FROM oidc_providers WHERE slug = 'clerk-idp'")
		if before == after {
			t.Fatal("the lifecycle secret was not resealed")
		}
		f.ok(t, userEvent("user.deleted", f.subject, ""))
		if n := queryInt(t, db, "SELECT COUNT(*) FROM external_identities"); n != 0 {
			t.Fatal("the resealed secret no longer verifies events")
		}
	})
}

func TestOIDCLifecycleLastCredential(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newLifecycleFixture(t, db)
		// Remove the only other credential: the delete leaves the account with none.
		execRaw(t, db, "DELETE FROM password_credentials")
		f.ok(t, userEvent("user.deleted", f.subject, ""))
		if countAudit(t, db, "auth.identity_unlinked_by_directory") != 1 {
			t.Fatal("deleting the last credential was refused or not audited")
		}
		if countAudit(t, db, "auth.account_no_login_credential") != 1 {
			t.Fatal("the zero-credential event did not fire")
		}
	})
}

func TestOIDCLifecycleRotationDoesNotSweep(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newLifecycleFixture(t, db)
		ctx := t.Context()
		oidcSession := oidcLogin(t, f.admin.auth, ctx, f.slug, f.subject).SessionToken
		put := func(in service.ProviderInput) {
			t.Helper()
			in.DisplayName, in.Issuer, in.ClientID, in.ClientSecret, in.Scopes, in.Enabled = "Clerk", f.idpIssue, "c", "s", "openid", true
			if _, err := f.provider.Put(ctx, service.LocalPrincipal(f.admin.boot.PrincipalID), f.slug, in); err != nil {
				t.Fatal(err)
			}
		}
		// Rotate the lifecycle secret.
		put(service.ProviderInput{LifecycleProfile: strptr(service.LifecycleProfileClerk), LifecycleSecret: "whsec_" + "MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSx"})
		if !f.live(t, oidcSession) {
			t.Fatal("rotating the lifecycle secret swept sessions")
		}
		// Clearing the profile.
		put(service.ProviderInput{})
		if !f.live(t, oidcSession) {
			t.Fatal("clearing the lifecycle profile swept sessions")
		}
		// A login-path change still sweeps.
		put(service.ProviderInput{AssurancePolicy: strptr(`{"amr_sets":[["mfa"]]}`)})
		if f.live(t, oidcSession) {
			t.Fatal("a policy change must still sweep")
		}
	})
}

func hexOf(db *store.DB, col string) string {
	if db.Engine() == store.EnginePostgres {
		return "encode(" + col + ", 'hex')"
	}
	return "hex(" + col + ")"
}
