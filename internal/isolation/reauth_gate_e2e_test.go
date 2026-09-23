package isolation

import (
	"errors"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/webauthntest"
)

func TestReauthGatedOperationsRefuseWithoutProof(t *testing.T) {
	forEngines(t, runReauthGatedOperationsRefuseWithoutProof)
}

// runReauthGatedOperationsRefuseWithoutProof is the runtime half of the
// x-hikyo-reauth contract (#606): EVERY operation the contract marks reaches a
// service that refuses it when the proof is absent (for a conditional gate,
// when the condition holds), and every probe here names a marked operation,
// so the extension and the gates cannot drift apart. The static half (every
// credential-carrying body is marked or exempt by name) is in api.
//
// Every probe runs on one real account prepared so the proof check is the
// first thing that can refuse: it holds a password, a confirmed TOTP factor,
// a passkey and a linked OIDC identity, and SAML and OIDC providers exist.
// A proof refusal is ErrReauthProofRequired (the selection names the missing
// class) or the uniform ErrUnauthenticated of a failed proof; the latter is
// attributed to the proof by checking the session still resolves after it.
func runReauthGatedOperationsRefuseWithoutProof(t *testing.T, db *store.DB) {
	administrator := bootstrapWebAuthnAdmin(t, db)
	auth, accountID, token, password := administrator.auth, administrator.accountID, administrator.token, administrator.password
	principal := administrator.boot.PrincipalID
	ctx := t.Context()
	base := time.Now().UTC()
	clk := base
	auth.Now = func() time.Time { return clk }

	// A linked OIDC identity (for unlink), then a passkey (for its removal),
	// then a confirmed TOTP factor (for its removal), all proven properly.
	configureProvider(t, auth, ctx, principal, "gate-idp", service.ProviderInput{
		DisplayName: "Gate IdP", ClientID: "client", ClientSecret: "secret", Scopes: "openid", Enabled: true,
	})
	start, err := auth.OIDCStart(ctx, "gate-idp", "link", "", token, password, false)
	if err != nil {
		t.Fatalf("link start: %v", err)
	}
	code, state := driveIdP(t, start.AuthURL+"&sub=gate-subject")
	linked, err := auth.OIDCCallback(ctx, "gate-idp", code, state, "", "", "", token)
	if err != nil {
		t.Fatalf("link callback: %v", err)
	}
	token = linked.Login.SessionToken
	identities, err := auth.ListIdentities(ctx, token)
	if err != nil || len(identities) != 1 {
		t.Fatalf("identities = %v, %v", identities, err)
	}
	token = enrolPasskey(t, auth, ctx, token, password, webauthntest.New(waRPID, waOrigin))
	passkey := queryString(t, db, "SELECT id FROM webauthn_credentials WHERE account_id = '"+accountID+"'")
	configureSAMLProvider(t, auth, principal)
	// enrolTotpStart is gated only while no factor stands (a second enrolment
	// is refused as already enrolled), so its proof-less probe runs before
	// the factor exists; its result is checked with the others below.
	_, totpStartErr := auth.EnrolTOTPStart(ctx, token, "")
	auth.Admission.RecordSuccess(accountID)
	if _, err := auth.Identity(ctx, token); err != nil {
		t.Fatalf("the session died on the proof-less enrolTotpStart: %v", err)
	}
	uri, err := auth.EnrolTOTPStart(ctx, token, password)
	if err != nil {
		t.Fatalf("totp enrol start: %v", err)
	}
	clk = base.Add(30 * time.Second)
	confirmed, err := auth.EnrolTOTPConfirm(ctx, token, totpCode(t, uri, clk))
	if err != nil {
		t.Fatalf("totp enrol confirm: %v", err)
	}
	token = confirmed.SessionToken
	profile, err := auth.MyProfile(ctx, token)
	if err != nil {
		t.Fatal(err)
	}

	actor := service.Bearer(token)
	reg := newRegistration(t, service.RegistrationConfig{DB: db, Auth: auth, PublicOriginExplicit: true})
	scim := &service.SCIM{DB: db, Auth: auth}
	orgIn := service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{oidcEntry("gate-idp")}, Landing: orgTemplateLanding()}
	instanceIn := service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{oidcEntry("gate-idp")}, Landing: service.RegistrationLanding{Kind: service.LandingNone}}

	probes := map[string]func() error{
		"putOrgRegistrationPolicy": func() error {
			_, err := reg.Put(ctx, actor, service.OrgRegistrationScope(orgA), orgIn, "")
			return err
		},
		"deleteOrgRegistrationPolicy": func() error { return reg.Delete(ctx, actor, service.OrgRegistrationScope(orgA), "") },
		"putInstanceRegistrationPolicy": func() error {
			_, err := reg.Put(ctx, actor, service.InstanceRegistrationScope(), instanceIn, "")
			return err
		},
		"deleteInstanceRegistrationPolicy": func() error { return reg.Delete(ctx, actor, service.InstanceRegistrationScope(), "") },
		"regenerateRecoveryCodes": func() error {
			_, _, err := auth.GenerateRecoveryCodes(ctx, token, "")
			return err
		},
		"unlinkIdentity": func() error {
			_, err := auth.UnlinkIdentity(ctx, token, identities[0].ID, "")
			return err
		},
		// linkIdentity is the OIDC alias of oidcStart {purpose: link}.
		"linkIdentity": func() error {
			_, err := auth.OIDCStart(ctx, "gate-idp", "link", "", token, "", false)
			return err
		},
		"oidcStart": func() error {
			_, err := auth.OIDCStart(ctx, "gate-idp", "link", "", token, "", false)
			return err
		},
		"samlStart": func() error {
			_, err := auth.SAMLStart(ctx, "saml-idp", "link", "", token, "")
			return err
		},
		"enrolTotpStart": func() error { return totpStartErr },
		"removeTotp": func() error {
			_, err := auth.RemoveTOTP(ctx, token, "")
			return err
		},
		"enrolPasskeyStart": func() error {
			_, err := auth.EnrolPasskeyStart(ctx, token, "", "")
			return err
		},
		"removePasskey": func() error {
			_, err := auth.RemovePasskey(ctx, token, passkey, "", "")
			return err
		},
		"mintScimCredential": func() error {
			_, err := scim.MintCredential(ctx, actor, orgA, "scb_gate", false, "")
			return err
		},
		// when-changed: the username changes, so the proof is required.
		"updateMyProfile": func() error {
			_, err := auth.UpdateMyProfile(ctx, token, service.ProfileUpdate{Username: profile.Username + "-renamed", DisplayName: profile.DisplayName}, "")
			return err
		},
	}

	operations, err := api.Operations()
	if err != nil {
		t.Fatal(err)
	}
	var marked []string
	for id, op := range operations {
		if op.Reauth.Class != "" {
			marked = append(marked, id)
		}
	}
	sort.Strings(marked)
	for id := range probes {
		if !slices.Contains(marked, id) {
			t.Errorf("%s is proof-gated in its service but not marked x-hikyo-reauth", id)
		}
	}
	for _, id := range marked {
		probe, ok := probes[id]
		if !ok {
			t.Errorf("%s is marked x-hikyo-reauth but has no proof-less probe here", id)
			continue
		}
		err := probe()
		// A failed proof feeds the per-account backoff; clear it so the next
		// probe reaches its own proof check rather than the throttle.
		auth.Admission.RecordSuccess(accountID)
		switch {
		case errors.Is(err, service.ErrReauthProofRequired):
		case errors.Is(err, domain.ErrUnauthenticated):
			if _, idErr := auth.Identity(ctx, token); idErr != nil {
				t.Errorf("%s refused as unauthenticated because the session died, not the proof: %v", id, idErr)
			}
		default:
			t.Errorf("%s without its proof = %v, want the proof refusal", id, err)
		}
	}
	// The gated state is untouched: nothing was unlinked, removed or renamed.
	if n := queryInt(t, db, "SELECT COUNT(*) FROM webauthn_credentials WHERE account_id = '"+accountID+"'"); n != 1 {
		t.Errorf("passkeys after proof-less probes = %d, want 1", n)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM external_identities WHERE account_id = '"+accountID+"'"); n != 1 {
		t.Errorf("linked identities after proof-less probes = %d, want 1", n)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM registration_policies"); n != 0 {
		t.Errorf("registration policies after proof-less probes = %d, want 0", n)
	}
}
