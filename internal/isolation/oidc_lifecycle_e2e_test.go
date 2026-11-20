package isolation

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
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

// signedHeaders signs body at ts. standard selects the Standard Webhooks
// header names; otherwise the Svix names Clerk sends.
func signedHeaders(t *testing.T, secret string, ts time.Time, body string, standard bool) http.Header {
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
	prefix := "Svix-"
	if standard {
		prefix = "Webhook-"
	}
	h := http.Header{}
	h.Set(prefix+"Id", id)
	h.Set(prefix+"Timestamp", strconv.FormatInt(ts.Unix(), 10))
	h.Set(prefix+"Signature", sig)
	return h
}

// TestOIDCLifecycleHTTP drives the real handler over the real service: header
// names, the status mapping, and verification before parsing.
func TestOIDCLifecycleHTTP(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newLifecycleFixture(t, db)
		h := server.New(&server.API{Auth: f.admin.auth}, nil)
		post := func(slug string, header http.Header, body string) int {
			t.Helper()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, api.PathPrefix+"/auth/oidc/"+slug+"/lifecycle", bytes.NewReader([]byte(body)))
			for k, v := range header {
				req.Header[k] = v
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			return rec.Code
		}
		wrong := "whsec_" + "MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSx"
		unrelated := userEvent("user.deleted", "user_nobody", "")

		// Malformed JSON under a bad signature is the signature refusal, not a parse error.
		if got := post(f.slug, signedHeaders(t, wrong, time.Now(), `{malformed`, false), `{malformed`); got != http.StatusUnauthorized {
			t.Fatalf("malformed body with a bad signature = %d, want 401", got)
		}
		// Malformed JSON under a good signature is a verified no-op.
		if got := post(f.slug, signedHeaders(t, lifecycleTestSecret, time.Now(), `{malformed`, false), `{malformed`); got != http.StatusNoContent {
			t.Fatalf("malformed body with a good signature = %d, want 204", got)
		}
		if got := post("no-such-slug", signedHeaders(t, lifecycleTestSecret, time.Now(), unrelated, false), unrelated); got != http.StatusUnauthorized {
			t.Fatalf("unknown slug = %d, want 401", got)
		}
		if got := post(f.slug, http.Header{}, unrelated); got != http.StatusUnauthorized {
			t.Fatalf("no signature headers = %d, want 401", got)
		}
		// A body over the bound never reaches verification.
		big := string(bytes.Repeat([]byte("a"), service.MaxLifecycleBodyBytes+1))
		if got := post(f.slug, signedHeaders(t, lifecycleTestSecret, time.Now(), big, false), big); got != http.StatusBadRequest {
			t.Fatalf("oversize body = %d, want 400", got)
		}

		// Both header families apply a delete, through HTTP.
		del := userEvent("user.deleted", f.subject, "")
		if got := post(f.slug, signedHeaders(t, lifecycleTestSecret, time.Now(), del, true), del); got != http.StatusNoContent {
			t.Fatalf("standard headers = %d, want 204", got)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM external_identities"); n != 0 {
			t.Fatalf("a standard-header delete left %d identities", n)
		}
		linkOn(t, f.admin.auth, t.Context(), f.slug, f.subject, f.admin.password)
		if got := post(f.slug, signedHeaders(t, lifecycleTestSecret, time.Now(), del, false), del); got != http.StatusNoContent {
			t.Fatalf("svix headers = %d, want 204", got)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM external_identities"); n != 0 {
			t.Fatalf("a svix-header delete left %d identities", n)
		}
	})
}

// TestOIDCLifecycleSweepBoundaries pins which sessions a delete ends: the
// principal's sessions through the issuer, and a password session carrying a
// credential-establishment stamp for that issuer; nothing else.
func TestOIDCLifecycleSweepBoundaries(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newLifecycleFixture(t, db)
		ctx := t.Context()
		// A second issuer linked to the same account.
		configureProvider(t, f.admin.auth, ctx, f.admin.boot.PrincipalID, "other-idp", service.ProviderInput{
			DisplayName: "Other", ClientID: "c", ClientSecret: "s", Scopes: "openid", Enabled: true,
		})
		linkOn(t, f.admin.auth, ctx, "other-idp", "user_2", f.admin.password)
		clerkSession := oidcLogin(t, f.admin.auth, ctx, f.slug, f.subject).SessionToken
		otherSession := oidcLogin(t, f.admin.auth, ctx, "other-idp", "user_2").SessionToken

		extraPassword := func() service.LoginResult {
			t.Helper()
			r, err := f.admin.auth.LocalLogin(ctx, "oidc-admin", f.admin.password, service.ArtifactCLI)
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		// Linking reissues earlier sessions, so mint the password ones afterwards.
		plain := extraPassword()
		stamped := extraPassword()
		identityID := queryString(t, db, "SELECT id FROM external_identities WHERE subject = '"+f.subject+"'")
		execRaw(t, db, "INSERT INTO credential_establish_evidence (session_id, identity_id, purpose, expires_at) VALUES ('"+
			stamped.SessionID+"', '"+identityID+"', 'account-security', '2099-01-01T00:00:00Z')")
		// A passkey session: no provider, a non-password method.
		passkey := extraPassword()
		execRaw(t, db, "UPDATE sessions SET auth_method = 'passkey' WHERE id = '"+passkey.SessionID+"'")
		// A stamp for another issuer's identity must not sweep.
		stampedOther := extraPassword()
		otherIdentity := queryString(t, db, "SELECT id FROM external_identities WHERE subject = 'user_2'")
		execRaw(t, db, "INSERT INTO credential_establish_evidence (session_id, identity_id, purpose, expires_at) VALUES ('"+
			stampedOther.SessionID+"', '"+otherIdentity+"', 'account-security', '2099-01-01T00:00:00Z')")

		f.ok(t, userEvent("user.deleted", f.subject, ""))

		for name, tc := range map[string]struct {
			token string
			live  bool
		}{
			"clerk oidc session":         {clerkSession, false},
			"establish-stamped password": {stamped.SessionToken, false},
			"plain password":             {plain.SessionToken, true},
			"passkey":                    {passkey.SessionToken, true},
			"other issuer oidc":          {otherSession, true},
			"other issuer stamp":         {stampedOther.SessionToken, true},
		} {
			if got := f.live(t, tc.token); got != tc.live {
				t.Errorf("%s: live=%v, want %v", name, got, tc.live)
			}
		}
	})
}

// TestOIDCLifecycleResolution covers identities the happy path does not: one
// from an earlier credential epoch, and one whose provider_id points at another
// row of the same issuer.
func TestOIDCLifecycleResolution(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newLifecycleFixture(t, db)
		ctx := t.Context()
		// An earlier-epoch identity: below the account's current password epoch.
		current := queryInt(t, db, "SELECT credential_epoch FROM password_credentials")
		execRaw(t, db, "UPDATE external_identities SET credential_epoch = credential_epoch - 1")
		if queryInt(t, db, "SELECT credential_epoch FROM external_identities") == current {
			t.Fatal("the identity is not from an earlier epoch")
		}
		// A disabled twin row of the same issuer; the identity points at it.
		if _, err := f.provider.Put(ctx, service.LocalPrincipal(f.admin.boot.PrincipalID), "clerk-twin", service.ProviderInput{
			DisplayName: "Twin", Issuer: f.idpIssue, ClientID: "c", ClientSecret: "s", Scopes: "openid", Enabled: false,
		}); err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, "UPDATE external_identities SET provider_id = (SELECT id FROM oidc_providers WHERE slug = 'clerk-twin')")

		f.ok(t, userEvent("user.deleted", f.subject, ""))
		if n := queryInt(t, db, "SELECT COUNT(*) FROM external_identities"); n != 0 {
			t.Fatalf("an earlier-epoch identity pointing at a twin row survived: %d", n)
		}
		if countAudit(t, db, "auth.identity_unlinked_by_directory") != 1 {
			t.Fatal("unlink not audited")
		}
	})
}

// TestOIDCLifecycleSystemActor pins the developer-credential and personal-value
// consequences of a directory delete: attributed to the system, and scoped to
// the swept sessions.
func TestOIDCLifecycleSystemActor(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newLifecycleFixture(t, db)
		ctx := t.Context()
		oidcSession := oidcLogin(t, f.admin.auth, ctx, f.slug, f.subject)
		fromOIDC := seedSecurityDeveloper(t, db, oidcSession.SessionToken, envScope(envA1), "lifecycle-oidc")
		fromPassword := seedSecurityDeveloper(t, db, f.password, envScope(envA1), "lifecycle-password")

		blob := "X'00'"
		if db.Engine() == store.EnginePostgres {
			blob = `'\x00'::bytea`
		}
		execRaw(t, db, `INSERT INTO personal_values (id, org_id, project_id, environment_id, key_id, owner_principal_id, ciphertext, created_at, updated_at) VALUES `+
			`('pv_lifecycle', 'org_a', 'prj_a1', 'env_a1', 'key_a1', '`+string(f.admin.boot.PrincipalID)+`', `+blob+`, `+ts+`, `+ts+`)`)

		f.ok(t, userEvent("user.deleted", f.subject, ""))

		if authenticateCaller(t, db, fromOIDC).Principal != "" {
			t.Fatal("a developer credential minted from the swept OIDC session still authenticates")
		}
		if authenticateCaller(t, db, fromPassword).Principal == "" {
			t.Fatal("a developer credential minted from a surviving password session was revoked")
		}
		revoked := "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'identity.developer_credential_revoked'"
		if queryInt(t, db, revoked) != 1 || queryInt(t, db, revoked+" AND actor_class = 'system'") != 1 {
			t.Fatal("the developer-credential revocation is not recorded once with the system actor")
		}
		if queryInt(t, db, "SELECT COUNT(*) FROM personal_values") != 0 {
			t.Fatal("the personal value was not purged")
		}
		purged := "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'personal_value.deleted'"
		if queryInt(t, db, purged) != 1 || queryInt(t, db, purged+" AND actor_class = 'system'") != 1 {
			t.Fatal("the purge is not recorded once with the system actor")
		}
	})
}

// TestOIDCLifecycleKeepsPasskeyAccountQuiet: an account that still has a
// passkey has a way in, so the zero-credential event stays quiet.
func TestOIDCLifecycleKeepsPasskeyAccountQuiet(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newLifecycleFixture(t, db)
		blob := "X'01'"
		if db.Engine() == store.EnginePostgres {
			blob = `'\x01'::bytea`
		}
		execRaw(t, db, "DELETE FROM password_credentials")
		execRaw(t, db, `INSERT INTO webauthn_credentials (id, account_id, credential_id, public_key, aaguid, sign_count, transports, discoverable, backup_eligible, backup_state, label, credential_epoch, row_version, created_at) VALUES `+
			`('wc_lifecycle', (SELECT id FROM accounts LIMIT 1), `+blob+`, `+blob+`, `+blob+`, 0, '[]', 1, 0, 0, 'key', 1, 1, `+ts+`)`)
		f.ok(t, userEvent("user.deleted", f.subject, ""))
		if countAudit(t, db, "auth.identity_unlinked_by_directory") != 1 {
			t.Fatal("unlink not audited")
		}
		if countAudit(t, db, "auth.account_no_login_credential") != 0 {
			t.Fatal("the zero-credential event fired for an account with a passkey")
		}
	})
}

// TestOIDCLifecycleBanDeveloperCredentials: a ban revokes the developer
// credentials of the swept OIDC session, with the system actor, and leaves one
// minted from a password session working.
func TestOIDCLifecycleBanDeveloperCredentials(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newLifecycleFixture(t, db)
		oidcSession := oidcLogin(t, f.admin.auth, t.Context(), f.slug, f.subject)
		fromOIDC := seedSecurityDeveloper(t, db, oidcSession.SessionToken, envScope(envA1), "ban-oidc")
		fromPassword := seedSecurityDeveloper(t, db, f.password, envScope(envA1), "ban-password")

		f.ok(t, userEvent("user.updated", f.subject, `,"banned":true`))

		if authenticateCaller(t, db, fromOIDC).Principal != "" {
			t.Fatal("a ban left the OIDC-born developer credential working")
		}
		if authenticateCaller(t, db, fromPassword).Principal == "" {
			t.Fatal("a ban revoked a credential minted from a password session")
		}
		revoked := "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'identity.developer_credential_revoked'"
		if queryInt(t, db, revoked) != 1 || queryInt(t, db, revoked+" AND actor_class = 'system'") != 1 {
			t.Fatal("the ban revocation is not recorded once with the system actor")
		}
	})
}

// TestOIDCLifecycleProviderMatchRevokes: a credential minted through the
// issuer's provider whose parent session ended before the event is still
// revoked, through the provider-id match alone.
func TestOIDCLifecycleProviderMatchRevokes(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newLifecycleFixture(t, db)
		oidcSession := oidcLogin(t, f.admin.auth, t.Context(), f.slug, f.subject)
		seedSecurityDeveloper(t, db, oidcSession.SessionToken, envScope(envA1), "ended-session")
		execRaw(t, db, "UPDATE developer_credentials SET parent_session_id = 'ses_ended' WHERE id = 'dev_security_ended-session'")
		pending := "SELECT COUNT(*) FROM developer_credentials WHERE id = 'dev_security_ended-session' AND revoked_at IS NULL"
		if queryInt(t, db, pending) != 1 {
			t.Fatal("the credential is already revoked before the event")
		}

		f.ok(t, userEvent("user.updated", f.subject, `,"banned":true`))

		if queryInt(t, db, pending) != 0 {
			t.Fatal("a credential minted through the issuer's provider survived the event")
		}
	})
}

// runOIDCDirectoryLifecycle drives every directory lifecycle audit event once
// into the shared audit_e2e datastore: a ban that sweeps an OIDC session, then
// a delete that leaves the account with no login credential.
func runOIDCDirectoryLifecycle(t *testing.T, db *store.DB, auth *service.Auth, admin domain.PrincipalID) {
	t.Helper()
	ctx := t.Context()
	const password = "correct horse battery staple directory"
	invite, err := (&service.Grants{DB: db, Auth: auth}).InviteMember(ctx, service.LocalPrincipal(admin),
		service.InviteSpec{Scope: domain.Scope{Org: orgA}, Username: "directory-user", Template: domain.TemplateViewer, Delivery: "response"})
	if err != nil {
		t.Fatalf("invite directory user: %v", err)
	}
	if err := auth.EstablishCredential(ctx, invite.Authority, password); err != nil {
		t.Fatalf("establish directory user: %v", err)
	}
	configureProvider(t, auth, ctx, admin, "directory-idp", service.ProviderInput{
		DisplayName: "Directory", ClientID: "c", ClientSecret: "s", Scopes: "openid", Enabled: true,
		LifecycleProfile: strptr(service.LifecycleProfileClerk), LifecycleSecret: lifecycleTestSecret,
	})
	login, err := auth.LocalLogin(ctx, "directory-user", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	start, err := auth.OIDCStart(ctx, "directory-idp", "link", "", "", "", login.SessionToken, password, false)
	if err != nil {
		t.Fatalf("directory link start: %v", err)
	}
	code, state := driveIdP(t, start.AuthURL+"&sub=directory-subject")
	if _, err := auth.OIDCCallback(ctx, "directory-idp", code, state, "", "", "", login.SessionToken); err != nil {
		t.Fatalf("directory link callback: %v", err)
	}
	oidcLogin(t, auth, ctx, "directory-idp", "directory-subject")
	deliver := func(body string) {
		t.Helper()
		if err := auth.OIDCLifecycle(ctx, "directory-idp", signedHeaders(t, lifecycleTestSecret, time.Now(), body, false), []byte(body)); err != nil {
			t.Fatalf("directory event %s: %v", body, err)
		}
	}
	deliver(userEvent("user.updated", "directory-subject", `,"banned":true`))
	execRaw(t, db, "DELETE FROM password_credentials WHERE account_id = (SELECT account_id FROM external_identities WHERE subject = 'directory-subject')")
	deliver(userEvent("user.deleted", "directory-subject", ""))
}
