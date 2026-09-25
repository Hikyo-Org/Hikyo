package isolation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestRegistrationPolicy(t *testing.T) {
	forEngines(t, runRegistrationPolicy)
}

// seedRegistrationProvider writes an OIDC provider row directly: the policy
// surface only READS provider configuration.
func seedRegistrationProvider(t *testing.T, db *store.DB, slug, scopes string, enabled bool) {
	t.Helper()
	secret, ts := `X'00'`, `'2026-01-01T00:00:00.000000Z'`
	if db.PG() != nil {
		secret, ts = `'\x00'::bytea`, `'2026-01-01T00:00:00Z'`
	}
	on := "0"
	if enabled {
		on = "1"
	}
	execRaw(t, db, `INSERT INTO oidc_providers `+
		`(id, slug, display_name, kind, issuer, client_id, client_secret, scopes, redirect_uri, `+
		` enabled, dek_version, row_version, created_at, updated_at) VALUES `+
		`('idp_`+slug+`', '`+slug+`', 'Provider `+slug+`', 'oidc', 'https://`+slug+`.example.test', 'cid', `+secret+`, '`+scopes+`', `+
		`'https://hikyo.example.test/cb', `+on+`, 1, 1, `+ts+`, `+ts+`)`)
}

var (
	orgAReg     = service.OrgRegistrationScope(orgA)
	instanceReg = service.InstanceRegistrationScope()
)

// newRegistration builds the service the way the app does, with a
// mailer predicate the test controls.
func newRegistration(t *testing.T, c service.RegistrationConfig) *service.Registration {
	t.Helper()
	if c.MailConfigured == nil {
		c.MailConfigured = func(context.Context) (bool, error) { return false, nil }
	}
	reg, err := service.NewRegistration(c)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func oidcEntry(slug string) service.RegistrationExternalEntry {
	return service.RegistrationExternalEntry{Provider: domain.ProviderRef{Kind: domain.ProviderOIDC, Slug: slug}}
}

func orgTemplateLanding() service.RegistrationLanding {
	return service.RegistrationLanding{Kind: service.LandingOrgTemplate, Template: domain.TemplateViewer}
}

// wantRefusal asserts a 400 whose safe detail starts with the named item.
func wantRefusal(t *testing.T, err error, name string) {
	t.Helper()
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("refusal = %v, want domain.ErrInvalid naming %q", err, name)
	}
	var sd interface{ SafeDetail() string }
	if !errors.As(err, &sd) || !strings.HasPrefix(sd.SafeDetail(), name) {
		t.Fatalf("refusal detail = %v, want it to name %q", err, name)
	}
}

// runRegistrationPolicy covers #606's service criteria on one engine: 0..1
// policy per scope, the authority reassigned by every edit and re-checked at
// every read, every write-time precondition refused by name, the reauth gate
// (fresh proof, no session purge), the events on the scope's trail, the
// public door incl. `?org=`, `n / cap`, and delete clearing pending sign-ups.
func runRegistrationPolicy(t *testing.T, db *store.DB) {
	ctx := t.Context()
	seedRegistrationProvider(t, db, "corp", "openid email profile", true)
	seedRegistrationProvider(t, db, "noemail", "openid profile", true)
	seedRegistrationProvider(t, db, "off", "openid email", false)
	mailer := false
	var mailFault error
	reg := newRegistration(t, service.RegistrationConfig{DB: db, PublicOriginExplicit: true,
		MailConfigured: func(context.Context) (bool, error) { return mailer, mailFault }})
	admin := service.LocalPrincipal(orgAdmin)
	operator := service.LocalPrincipal(root)

	// Closed until a policy exists; an unknown org is the same closed door.
	if got, err := reg.Get(ctx, admin, orgAReg); err != nil || got != nil {
		t.Fatalf("org policy before any write = %+v, %v; want none", got, err)
	}
	for _, org := range []service.RegistrationScope{orgAReg, service.OrgRegistrationScope("org_nope"), instanceReg} {
		door, err := reg.SignupDoor(ctx, org)
		if err != nil || door.Open || door.Paused || len(door.Methods) != 0 {
			t.Fatalf("door %+v before any policy = %+v, %v; want closed", org, door, err)
		}
	}

	// Write-time preconditions refuse 400 by name, the provider row named.
	cases := []struct {
		name  string
		svc   *service.Registration
		scope service.RegistrationScope
		in    service.RegistrationPolicyInput
	}{
		{string(service.PreconditionNoPublicOrigin), newRegistration(t, service.RegistrationConfig{DB: db}), orgAReg,
			service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{oidcEntry("corp")}, Landing: orgTemplateLanding()}},
		{string(service.PreconditionMailerUnconfigured), reg, orgAReg,
			service.RegistrationPolicyInput{Local: &service.RegistrationLocalEntry{}, Landing: orgTemplateLanding()}},
		{string(service.PreconditionProviderDisabled) + ": oidc:off", reg, orgAReg,
			service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{oidcEntry("off")}, Landing: orgTemplateLanding()}},
		{string(service.PreconditionProviderMissingEmail) + ": oidc:noemail", reg, orgAReg,
			service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{oidcEntry("noemail")}, Landing: orgTemplateLanding()}},
		{string(service.PreconditionTemplateNotOrgApplicable), reg, orgAReg,
			service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{oidcEntry("corp")},
				Landing: service.RegistrationLanding{Kind: service.LandingOrgTemplate, Template: domain.TemplateOperator}}},
		{string(service.PreconditionCapZero), reg, instanceReg,
			service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{oidcEntry("corp")},
				Landing: service.RegistrationLanding{Kind: service.LandingFreshOrg}}},
		{"external[0].claim", reg, orgAReg,
			service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{{
				Provider: domain.ProviderRef{Kind: domain.ProviderOIDC, Slug: "corp"}, Claim: "email", Values: []string{"a@b.test"}}},
				Landing: orgTemplateLanding()}},
		{"external[0]", reg, orgAReg,
			service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{{
				Provider: domain.ProviderRef{Kind: domain.ProviderOIDC, Slug: "corp"}, Claim: "hd"}},
				Landing: orgTemplateLanding()}},
		{"external[0].provider.kind", reg, orgAReg,
			service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{{
				Provider: domain.ProviderRef{Kind: domain.ProviderSAML, Slug: "corp"}}}, Landing: orgTemplateLanding()}},
		{"external[0].provider", reg, orgAReg,
			service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{oidcEntry("missing")}, Landing: orgTemplateLanding()}},
		{"landing.kind", reg, instanceReg,
			service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{oidcEntry("corp")}, Landing: orgTemplateLanding()}},
		// oauth2 has no provider table until #609: refused by name, not as
		// an unknown row.
		{string(service.PreconditionProviderKindUnsupported) + ": oauth2:github", reg, orgAReg,
			service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{{
				Provider: domain.ProviderRef{Kind: domain.ProviderOAuth2, Slug: "github"}}}, Landing: orgTemplateLanding()}},
	}
	for _, c := range cases {
		actor := admin
		if c.scope.Instance() {
			actor = operator
		}
		_, err := c.svc.Put(ctx, actor, c.scope, c.in, "")
		wantRefusal(t, err, c.name)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM registration_policies"); n != 0 {
		t.Fatalf("a refused write left %d policy rows", n)
	}

	// An org policy: orgAdmin becomes the authority; tenant trail.
	orgPolicy, err := reg.Put(ctx, admin, orgAReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{{
			Provider: domain.ProviderRef{Kind: domain.ProviderOIDC, Slug: "corp"}, Claim: "hd", Values: []string{"acme.example"}}},
		Landing: orgTemplateLanding(),
	}, "")
	if err != nil {
		t.Fatalf("put org policy: %v", err)
	}
	if !orgPolicy.Active || orgPolicy.AuthorityPrincipalID != orgAdmin || orgPolicy.External[0].Provider.Slug != "corp" ||
		orgPolicy.External[0].Values[0] != "acme.example" {
		t.Fatalf("org policy = %+v", orgPolicy)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'registration.policy_created' AND org_id = 'org_a' "+
		"AND payload LIKE '%\"authority_principal_id\":\"usr_orgadmin\"%'"); n != 1 {
		t.Errorf("registration.policy_created on org_a's trail = %d, want 1", n)
	}
	door, err := reg.SignupDoor(ctx, orgAReg)
	if err != nil || !door.Open || door.Paused || len(door.Methods) != 1 || door.Methods[0] != (service.SignupMethod{Kind: "oidc", Slug: "corp"}) {
		t.Fatalf("org door = %+v, %v; want open with oidc:corp", door, err)
	}
	if door, err := reg.SignupDoor(ctx, instanceReg); err != nil || door.Open || door.Paused {
		t.Fatalf("the org policy opened the instance door: %+v", door)
	}

	// Any edit reassigns authority to the editor: root edits org_a's policy.
	edited, err := reg.Put(ctx, operator, orgAReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("corp")}, Landing: orgTemplateLanding(),
	}, "")
	if err != nil || edited.AuthorityPrincipalID != root || edited.ID != orgPolicy.ID {
		t.Fatalf("edit by root = %+v, %v; want the same policy with root as authority", edited, err)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'registration.policy_updated' AND org_id = 'org_a' "+
		"AND payload LIKE '%\"previous_authority_principal_id\":\"usr_orgadmin\"%' AND payload LIKE '%\"authority_principal_id\":\"usr_root\"%'"); n != 1 {
		t.Errorf("registration.policy_updated with the authority reassignment = %d, want 1", n)
	}
	// Back to orgAdmin, then revoke orgAdmin's manage-members: the next read
	// is inactive authority-lost and the door pauses (cause off the page).
	if _, err := reg.Put(ctx, admin, orgAReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("corp")}, Landing: orgTemplateLanding(),
	}, ""); err != nil {
		t.Fatal(err)
	}
	execRaw(t, db, "DELETE FROM grant_origins WHERE grant_id = 'g_oa_mm'")
	execRaw(t, db, "DELETE FROM grants WHERE id = 'g_oa_mm'")
	lost, err := reg.Get(ctx, operator, orgAReg)
	if err != nil || lost == nil || lost.Active || lost.InactiveCause != service.InactiveAuthorityLost {
		t.Fatalf("after revoking the authority's manage-members = %+v, %v; want inactive authority-lost", lost, err)
	}
	door, err = reg.SignupDoor(ctx, orgAReg)
	if err != nil || door.Open || !door.Paused || len(door.Methods) != 0 {
		t.Fatalf("door with lost authority = %+v, %v; want paused, no methods", door, err)
	}
	// Re-save as authority: the operator now holds it and the policy lives.
	resaved, err := reg.Put(ctx, operator, orgAReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("corp")}, Landing: orgTemplateLanding(),
	}, "")
	if err != nil || !resaved.Active || resaved.AuthorityPrincipalID != root {
		t.Fatalf("re-save = %+v, %v; want active under root", resaved, err)
	}
	// A provider disabled after the write is a use-time precondition.
	execRaw(t, db, "UPDATE oidc_providers SET enabled = 0 WHERE slug = 'corp'")
	pre, err := reg.Get(ctx, operator, orgAReg)
	if err != nil || pre.Active || pre.InactiveCause != service.InactivePrecondition ||
		pre.InactivePrecondition != string(service.PreconditionProviderDisabled)+": oidc:corp" {
		t.Fatalf("after disabling the provider = %+v, %v; want inactive precondition provider-disabled", pre, err)
	}
	execRaw(t, db, "UPDATE oidc_providers SET enabled = 1 WHERE slug = 'corp'")
	// An org-scope entry names only instance-enabled providers: the same
	// refusal applies at org scope as at instance scope.
	_, err = reg.Put(ctx, operator, orgAReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("off")}, Landing: orgTemplateLanding()}, "")
	wantRefusal(t, err, string(service.PreconditionProviderDisabled)+": oidc:off")

	// Instance policy, fresh-org: `n / cap` counts live orgs it minted.
	mailer = true
	inst, err := reg.Put(ctx, operator, instanceReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("corp")},
		Local:    &service.RegistrationLocalEntry{Domains: []string{"Acme.Example"}},
		Landing:  service.RegistrationLanding{Kind: service.LandingFreshOrg, Cap: 3},
	}, "")
	if err != nil || !inst.Active || inst.FreshOrgCount == nil || *inst.FreshOrgCount != 0 || inst.Local.Domains[0] != "acme.example" {
		t.Fatalf("instance fresh-org policy = %+v, %v", inst, err)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'registration.policy_created' "+
		"AND payload LIKE '%\"authority_principal_id\":\"usr_root\"%'"); n != 1 {
		t.Errorf("registration.policy_created on the instance trail = %d, want 1", n)
	}
	execRaw(t, db, "UPDATE orgs SET origin = 'registration', registration_policy_id = '"+inst.ID+"' WHERE id = 'org_b'")
	counted, err := reg.Get(ctx, operator, instanceReg)
	if err != nil || *counted.FreshOrgCount != 1 {
		t.Fatalf("n / cap after one minted org = %+v, %v", counted, err)
	}
	door, err = reg.SignupDoor(ctx, instanceReg)
	if err != nil || !door.Open || len(door.Methods) != 2 || door.Methods[1].Kind != "local" {
		t.Fatalf("instance door = %+v, %v; want oidc:corp + local", door, err)
	}
	// The mailer going away pauses a policy with a local entry.
	mailer = false
	if got, _ := reg.Get(ctx, operator, instanceReg); got.Active || got.InactivePrecondition != string(service.PreconditionMailerUnconfigured) {
		t.Fatalf("instance policy without a mailer = %+v; want inactive mailer-unconfigured", got)
	}
	mailer = true
	// A mailer predicate that FAILS is a fault on every path, never a
	// silent "unconfigured": the write, the read and the public door all
	// surface it.
	mailFault = errors.New("runtime configuration read failed")
	if _, err := reg.Put(ctx, operator, instanceReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("corp")},
		Local:    &service.RegistrationLocalEntry{},
		Landing:  service.RegistrationLanding{Kind: service.LandingFreshOrg, Cap: 3},
	}, ""); !errors.Is(err, mailFault) {
		t.Fatalf("put with a failing mailer predicate = %v, want the fault", err)
	}
	if _, err := reg.Get(ctx, operator, instanceReg); !errors.Is(err, mailFault) {
		t.Fatalf("read with a failing mailer predicate = %v, want the fault", err)
	}
	if _, err := reg.SignupDoor(ctx, instanceReg); !errors.Is(err, mailFault) {
		t.Fatalf("door with a failing mailer predicate = %v, want the fault", err)
	}
	mailFault = nil
	// An editor without org.create cannot delegate fresh orgs.
	if _, err := reg.Put(ctx, service.LocalPrincipal(orgAdmin), instanceReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("corp")},
		Landing:  service.RegistrationLanding{Kind: service.LandingFreshOrg, Cap: 3}}, ""); err == nil {
		t.Fatal("an org administrator wrote the instance policy")
	}
	// Revoking the authority's org.create input (instance-config) flips the
	// instance policy to authority-lost.
	execRaw(t, db, "DELETE FROM grant_origins WHERE grant_id = 'g_ro_ic'")
	execRaw(t, db, "DELETE FROM grants WHERE id = 'g_ro_ic'")
	if got, err := reg.Get(ctx, operator, instanceReg); err != nil || got.InactiveCause != service.InactiveAuthorityLost {
		t.Fatalf("instance policy after revoking instance-config = %+v, %v; want authority-lost", got, err)
	}
	// A manage-members-only instance editor cannot delegate fresh orgs: the
	// write needs org.create too, refused as an instance-scope grant refusal.
	if _, err := reg.Put(ctx, operator, instanceReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("corp")},
		Landing:  service.RegistrationLanding{Kind: service.LandingFreshOrg, Cap: 3}}, ""); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("fresh-org write without org.create = %v, want domain.ErrUnauthorized", err)
	}

	// 0..1 per scope is the database's: a second instance row is refused,
	// and so is a second row for the same org.
	err = tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		now := time.Now()
		return az.CreateRegistrationPolicy(ctx, authz.RegistrationPolicy{
			ID: "rpol_second", AuthorityPrincipalID: root, Landing: string(service.LandingNone), CreatedAt: now, UpdatedAt: now,
		})
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second instance policy = %v, want domain.ErrConflict", err)
	}
	err = tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		now := time.Now()
		return az.CreateRegistrationPolicy(ctx, authz.RegistrationPolicy{
			ID: "rpol_second_org", OrgID: orgA, AuthorityPrincipalID: root, Landing: string(service.LandingOrgTemplate),
			Template: string(domain.TemplateViewer), CreatedAt: now, UpdatedAt: now,
		})
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second org policy = %v, want domain.ErrConflict", err)
	}
	// The writer holds the one cross-table rule: values iff claim.
	err = tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		now := time.Now()
		return az.CreateRegistrationPolicy(ctx, authz.RegistrationPolicy{
			ID: "rpol_values", OrgID: orgB, AuthorityPrincipalID: root, Landing: string(service.LandingOrgTemplate),
			Template: string(domain.TemplateViewer), CreatedAt: now, UpdatedAt: now,
			Entries: []authz.RegistrationEntry{{ID: "rpe_v", ProviderKind: "oidc", ProviderID: "idp_corp", Values: []string{"x"}, CreatedAt: now}},
		})
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("values without a claim = %v, want domain.ErrInvalid", err)
	}

	// Deleting a policy deletes its pending sign-ups inside the same
	// transaction, each with registration.signup_expired {policy-deleted}.
	seedPendingSignup(t, db, "su_one", "one@acme.example", inst.ID, "")
	seedPendingSignup(t, db, "su_two", "two@acme.example", inst.ID, "")
	if err := reg.Delete(ctx, operator, instanceReg, ""); err != nil {
		t.Fatalf("delete the instance policy: %v", err)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM registration_signups"); n != 0 {
		t.Errorf("pending sign-ups left after the policy delete: %d", n)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'registration.signup_expired' "+
		"AND payload LIKE '%\"cause\":\"policy-deleted\"%'"); n != 2 {
		t.Errorf("registration.signup_expired on the instance trail = %d, want 2", n)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'registration.policy_deleted'"); n != 1 {
		t.Errorf("registration.policy_deleted on the instance trail = %d, want 1", n)
	}
	// The org the policy minted keeps its trail pointer (no FK, #585 d8).
	if n := queryInt(t, db, "SELECT COUNT(*) FROM orgs WHERE registration_policy_id = '"+inst.ID+"'"); n != 1 {
		t.Errorf("deleting the policy touched the org it minted")
	}
	if err := reg.Delete(ctx, operator, instanceReg, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleting an absent policy = %v, want domain.ErrNotFound", err)
	}

	// org_b: a deleted provider row reads provider-missing (nothing
	// auto-deletes the entry), and restricting the authority principal (a
	// disabled account holds no grants) flips the policy to authority-lost.
	seedRegistrationProvider(t, db, "gone", "openid email", true)
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) `+
		`SELECT 'g_gr_mm_b', 'usr_grantee', 'manage-members', 'org_b', NULL, NULL, created_at FROM grants WHERE id = 'g_oa_read'`)
	seedOrigins(t, db)
	orgBReg := service.OrgRegistrationScope(orgB)
	if _, err := reg.Put(ctx, service.LocalPrincipal(grantee), orgBReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("corp"), oidcEntry("gone")}, Landing: orgTemplateLanding(),
	}, ""); err != nil {
		t.Fatalf("put org_b policy as grantee: %v", err)
	}
	execRaw(t, db, "DELETE FROM oidc_providers WHERE slug = 'gone'")
	missing, err := reg.Get(ctx, operator, orgBReg)
	if err != nil || missing.InactiveCause != service.InactivePrecondition ||
		missing.InactivePrecondition != string(service.PreconditionProviderMissing)+": oidc:idp_gone" {
		t.Fatalf("policy naming a deleted provider = %+v, %v; want precondition provider-missing", missing, err)
	}
	execRaw(t, db, "UPDATE principals SET privacy_state = 'restricted' WHERE id = 'usr_grantee'")
	restricted, err := reg.Get(ctx, operator, orgBReg)
	if err != nil || restricted.InactiveCause != service.InactiveAuthorityLost {
		t.Fatalf("policy whose authority is restricted = %+v, %v; want authority-lost", restricted, err)
	}
	if door, err := reg.SignupDoor(ctx, orgBReg); err != nil || !door.Paused {
		t.Fatalf("door with a restricted authority = %+v, %v; want paused", door, err)
	}

	// Someone without manage-members at org_a is refused uniformly.
	if _, err := reg.Get(ctx, service.LocalPrincipal(alice), orgAReg); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("alice reading org_a's policy = %v, want the uniform domain.ErrNotFound", err)
	}
	if err := reg.Delete(ctx, service.LocalPrincipal(alice), orgAReg, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("alice deleting org_a's policy = %v, want the uniform domain.ErrNotFound", err)
	}
}

// seedPendingSignup writes one pending local sign-up row through the
// resolution writer (#608 owns the request that writes it in production).
func seedPendingSignup(t *testing.T, db *store.DB, id, email, policyID string, org domain.OrgID) {
	t.Helper()
	if err := tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		now := time.Now()
		return az.CreateRegistrationSignup(ctx, authz.NewRegistrationSignup{
			ID: id, Email: email, TokenVerifier: []byte(id), PolicyID: policyID, SignupScopeOrgID: org,
			CredentialEpoch: 1, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
		})
	}); err != nil {
		t.Fatalf("seed pending sign-up: %v", err)
	}
}

func TestRegistrationPolicyReauth(t *testing.T) {
	forEngines(t, runRegistrationPolicyReauth)
}

// runRegistrationPolicyReauth: a policy mutation requires fresh proof (a
// TOTP code where one stands), a spent code cannot authorize a second
// mutation, and the actor's sessions survive: the gate is reauth, not the
// full account-security procedure (#579 d8).
func runRegistrationPolicyReauth(t *testing.T, db *store.DB) {
	seedRegistrationProvider(t, db, "corp", "openid email", true)
	factorAdmin := bootstrapFactorAdmin(t, db)
	auth, password := factorAdmin.auth, factorAdmin.password
	base := time.Now().UTC()
	clk := base
	auth.Now = func() time.Time { return clk }
	ctx := t.Context()
	reg := newRegistration(t, service.RegistrationConfig{DB: db, Auth: auth, Now: func() time.Time { return clk }, PublicOriginExplicit: true})

	login, err := auth.LocalLogin(ctx, "factor-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := auth.EnrolTOTPStart(ctx, login.SessionToken, password)
	if err != nil {
		t.Fatal(err)
	}
	clk = base.Add(30 * time.Second)
	confirmed, err := auth.EnrolTOTPConfirm(ctx, login.SessionToken, totpCode(t, uri, clk))
	if err != nil {
		t.Fatal(err)
	}
	clk = base.Add(60 * time.Second)
	stepped, err := auth.StepUpTOTP(ctx, confirmed.SessionToken, totpCode(t, uri, clk))
	if err != nil {
		t.Fatal(err)
	}
	token := stepped.SessionToken
	actor := service.Bearer(token)
	in := service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("corp")}, Landing: service.RegistrationLanding{Kind: service.LandingNone},
	}

	if _, err := reg.Put(ctx, actor, instanceReg, in, ""); !errors.Is(err, service.ErrReauthProofRequired) {
		t.Fatalf("put without proof = %v, want ErrReauthProofRequired", err)
	}
	if _, err := reg.Put(ctx, actor, instanceReg, in, password); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("put with a password where a TOTP stands = %v, want the uniform refusal", err)
	}
	sessionsBefore := queryInt(t, db, "SELECT COUNT(*) FROM sessions")
	clk = base.Add(90 * time.Second)
	code := totpCode(t, uri, clk)
	if _, err := reg.Put(ctx, actor, instanceReg, in, code); err != nil {
		t.Fatalf("put with fresh proof: %v", err)
	}
	if _, err := reg.Put(ctx, actor, instanceReg, in, code); err == nil {
		t.Fatal("a spent TOTP step authorized a second mutation")
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM sessions"); n != sessionsBefore {
		t.Errorf("a policy mutation changed the session count %d → %d", sessionsBefore, n)
	}
	if _, err := auth.Identity(ctx, token); err != nil {
		t.Fatalf("the actor's session did not survive the mutation: %v", err)
	}
	if err := reg.Delete(ctx, actor, instanceReg, ""); !errors.Is(err, service.ErrReauthProofRequired) {
		t.Fatalf("delete without proof = %v, want ErrReauthProofRequired", err)
	}
	clk = base.Add(120 * time.Second)
	if err := reg.Delete(ctx, actor, instanceReg, totpCode(t, uri, clk)); err != nil {
		t.Fatalf("delete with fresh proof: %v", err)
	}
	if _, err := auth.Identity(ctx, token); err != nil {
		t.Fatalf("the actor's session did not survive the delete: %v", err)
	}
	// Without an Auth library a network mutation fails closed.
	bare := newRegistration(t, service.RegistrationConfig{DB: db, PublicOriginExplicit: true})
	if _, err := bare.Put(ctx, actor, instanceReg, in, "123456"); !errors.Is(err, service.ErrRegistrationProofUnavailable) {
		t.Fatalf("put with no reauth library = %v, want ErrRegistrationProofUnavailable", err)
	}
}

// runRegistrationLifecycle emits every registration.* type the registry
// declares (#606, #607) through the real service, on a datastore shared with
// the audit emitter-closure check: an org policy created, edited and deleted,
// an instance policy deleted with one pending sign-up behind it, and a
// federated sign-up admitted into a fresh org beside one refused.
func runRegistrationLifecycle(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := t.Context()
	seedRegistrationProvider(t, db, "reg-lifecycle", "openid email", true)
	reg := newRegistration(t, service.RegistrationConfig{DB: db, PublicOriginExplicit: true})
	org := service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("reg-lifecycle")}, Landing: orgTemplateLanding(),
	}
	for range 2 {
		if _, err := reg.Put(ctx, service.LocalPrincipal(root), orgAReg, org, ""); err != nil {
			t.Fatalf("registration lifecycle put: %v", err)
		}
	}
	if err := reg.Delete(ctx, service.LocalPrincipal(root), orgAReg, ""); err != nil {
		t.Fatalf("registration lifecycle delete: %v", err)
	}
	inst, err := reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("reg-lifecycle")},
		Landing:  service.RegistrationLanding{Kind: service.LandingNone},
	}, "")
	if err != nil {
		t.Fatalf("registration lifecycle instance put: %v", err)
	}
	seedPendingSignup(t, db, "su_lifecycle", "lifecycle@example.test", inst.ID, "")
	if err := reg.Delete(ctx, service.LocalPrincipal(root), instanceReg, ""); err != nil {
		t.Fatalf("registration lifecycle instance delete: %v", err)
	}

	// The federated sign-up outcomes (#607) through a real IdP round trip:
	// signup_admitted, the widened settings.org_created (a fresh org under
	// the authority), signup_completed, and a signup_refused.
	h := newSignupHarness(t, db)
	h.provider("reg-signup", service.ProviderInput{})
	if _, err := h.reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("reg-signup")},
		Landing:  service.RegistrationLanding{Kind: service.LandingFreshOrg, Cap: 5},
	}, ""); err != nil {
		t.Fatalf("registration lifecycle sign-up policy: %v", err)
	}
	if _, err := h.login("reg-signup", "lifecycle-signup", "sign-up", "", googleClaims("lifecycle@acme.example")); err != nil {
		t.Fatalf("registration lifecycle sign-up: %v", err)
	}
	if _, err := h.login("reg-signup", "lifecycle-refused", "sign-up", "", map[string]any{"email": "x@acme.example"}); err == nil {
		t.Fatal("registration lifecycle: an unverified address signed up")
	}
	if err := h.reg.Delete(ctx, service.LocalPrincipal(root), instanceReg, ""); err != nil {
		t.Fatalf("registration lifecycle sign-up policy delete: %v", err)
	}
}
