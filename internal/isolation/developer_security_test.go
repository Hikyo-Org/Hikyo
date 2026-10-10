package isolation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Seed only the delegation metadata so these fixtures exercise the public
// security mutation and authentication boundaries independently of minting.
func seedSecurityDeveloper(t *testing.T, db *store.DB, ownerToken string, scope domain.Scope, suffix string) string {
	t.Helper()
	owner := authenticateCaller(t, db, ownerToken)
	if owner.Principal == "" {
		t.Fatal("owner did not authenticate")
	}
	bearer, verifier, err := crypto.NewArtifact(crypto.ArtifactDeveloper)
	if err != nil {
		t.Fatal(err)
	}
	err = tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		generation, err := az.PrincipalGeneration(ctx, owner.Principal)
		if err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		return az.CreateDeveloperCredential(ctx, authz.DeveloperCredential{
			ID: "dev_security_" + suffix, PrincipalID: domain.PrincipalID("dvp_security_" + suffix),
			AuthorityPrincipal: owner.Principal, Scope: scope, ParentSessionID: owner.SessionID,
			ProviderID: owner.ProviderID, OAuth2ProviderID: owner.OAuth2ProviderID, SAMLProviderID: owner.SAMLProviderID,
			AuthMethod: owner.Assurance.Method, AuthorityGeneration: generation, CredentialEpoch: epoch,
			CreatedAt: now, ExpiresAt: now.Add(8 * time.Hour),
		}, verifier)
	})
	if err != nil {
		t.Fatal(err)
	}
	if authenticateCaller(t, db, bearer).Principal == "" {
		t.Fatal("developer credential must authenticate before security mutation")
	}
	return bearer
}

func TestDeveloperLogoutAndExplicitSessionRevocation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		fixture := ceremonyFixture(t, db, "developer-security-session")
		owner := fixture.admin.token
		delegation := seedSecurityDeveloper(t, db, owner, envScope(envA1), "logout")
		if err := fixture.admin.auth.Logout(t.Context(), owner); err != nil {
			t.Fatal(err)
		}
		if authenticateCaller(t, db, delegation).Principal == "" {
			t.Fatal("ordinary logout ended the fixed developer delegation")
		}

		owner = sessionWithWindows(t, db, fixture.admin.boot.PrincipalID)
		expiringOwner := owner
		expiryDelegation := seedSecurityDeveloper(t, db, owner, envScope(envA1), "parent-expiry")
		if err := tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			future := time.Now().UTC().Add(3 * time.Hour)
			if _, err := az.AuthenticateCaller(ctx, expiringOwner, future); !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatalf("parent must be expired before testing independence: %v", err)
			}
			if _, err := az.AuthenticateCaller(ctx, expiryDelegation, future); err != nil {
				t.Fatalf("ordinary parent session expiry ended the bounded delegation: %v", err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		delegation = seedSecurityDeveloper(t, db, owner, envScope(envA1), "explicit")
		session := authenticateCaller(t, db, owner)
		workspace := &service.Workspace{DB: db}
		if err := workspace.RevokeSession(t.Context(), service.Bearer(owner), session.SessionID); err != nil {
			t.Fatal(err)
		}
		if authenticateCaller(t, db, delegation).Principal != "" {
			t.Fatal("explicit security revocation left the issuing session's developer bearer live")
		}
	})
}

func TestDeveloperProtectionIsTerminalAndEnvironmentScoped(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		fixture := ceremonyFixture(t, db, "developer-security-protection")
		first := seedSecurityDeveloper(t, db, fixture.admin.token, envScope(envA1), "protection")
		otherScope := scopeEnv(orgA, prjA2, envA2)
		other := seedSecurityDeveloper(t, db, fixture.admin.token, otherScope, "other-environment")
		settings := &service.ProjectSettings{DB: db, Auth: fixture.admin.auth}
		actor := service.LocalPrincipal(orgAdmin)
		if _, err := settings.SetEnvironment(t.Context(), actor, envScope(envA1), service.EnvironmentSettings{Protected: true}); err != nil {
			t.Fatal(err)
		}
		if authenticateCaller(t, db, first).Principal != "" {
			t.Fatal("protected environment's existing developer bearer still authenticates")
		}
		if authenticateCaller(t, db, other).Principal == "" {
			t.Fatal("protecting one environment revoked another environment's delegation")
		}
		if _, err := settings.SetEnvironment(t.Context(), actor, envScope(envA1), service.EnvironmentSettings{}); err != nil {
			t.Fatal(err)
		}
		if authenticateCaller(t, db, first).Principal != "" {
			t.Fatal("unprotecting revived a terminally revoked developer bearer")
		}
		trail, err := (&service.Audits{DB: db}).InstanceQuery(t.Context(), root,
			service.AuditFilter{Type: string(audit.EventDeveloperCredentialRevoked), Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(trail.Events) != 1 {
			t.Fatalf("protection must record exactly one affected credential revocation: %d", len(trail.Events))
		}
		event := trail.Events[0]
		if event.Actor.ID != string(orgAdmin) || event.AuthorityID != string(fixture.admin.boot.PrincipalID) {
			t.Fatalf("security actor and delegating human must remain distinct: %+v", event.Event)
		}
		if strings.Contains(event.RawPayload, first) || strings.Contains(event.RawPayload, "plaintext-"+ceremonySecretA) {
			t.Fatal("credential revocation audit contains bearer or secret material")
		}
	})
}

func TestDeveloperAccountRestrictionCannotBeReversedByRelease(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		fixture := ceremonyFixture(t, db, "developer-security-restriction")
		delegation := seedSecurityDeveloper(t, db, fixture.admin.token, envScope(envA1), "restriction")
		principal := string(fixture.admin.boot.PrincipalID)
		if _, err := fixture.admin.auth.ApplyPrivacySubject(t.Context(), principal, "restrict", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.admin.auth.ApplyPrivacySubject(t.Context(), principal, "release", ""); err != nil {
			t.Fatal(err)
		}
		if authenticateCaller(t, db, delegation).Principal != "" {
			t.Fatal("releasing account restriction revived an old developer bearer")
		}
	})
}

func TestDeveloperProviderRevocationSurvivesParentLogout(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		administrator := oidcAdmin(t, db)
		providers, idp := configureProvider(t, administrator.auth, ctx, administrator.boot.PrincipalID, "developer-idp", service.ProviderInput{
			DisplayName: "Developer IdP", ClientID: "c", ClientSecret: "s", Scopes: "openid", Enabled: true,
		})
		linkOn(t, administrator.auth, ctx, "developer-idp", "developer", administrator.password)
		owner, err := administrator.auth.LocalLogin(ctx, "oidc-admin", administrator.password, service.ArtifactCLI)
		if err != nil {
			t.Fatal(err)
		}
		local := seedSecurityDeveloper(t, db, owner.SessionToken, envScope(envA1), "local-provider-independent")
		federated := oidcLogin(t, administrator.auth, ctx, "developer-idp", "developer")
		delegation := seedSecurityDeveloper(t, db, federated.SessionToken, envScope(envA1), "federated-provider")
		if err := administrator.auth.Logout(ctx, federated.SessionToken); err != nil {
			t.Fatal(err)
		}
		if _, err := providers.Put(ctx, service.LocalPrincipal(administrator.boot.PrincipalID), "developer-idp", service.ProviderInput{
			DisplayName: "Developer IdP", Issuer: idp.Issuer(), ClientID: "c", ClientSecret: "s", Scopes: "openid", Enabled: false,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := providers.Put(ctx, service.LocalPrincipal(administrator.boot.PrincipalID), "developer-idp", service.ProviderInput{
			DisplayName: "Developer IdP", Issuer: idp.Issuer(), ClientID: "c", ClientSecret: "s", Scopes: "openid", Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
		if authenticateCaller(t, db, delegation).Principal != "" {
			t.Fatal("provider re-enable revived a delegation whose parent session was already logged out")
		}
		if authenticateCaller(t, db, local).Principal == "" {
			t.Fatal("changing a provider revoked another authentication provenance")
		}
	})
}

func TestDeveloperSCIMAccessLossIsTerminalAndOrgScoped(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		s := scimSvc(db)
		binding, credential := newSCIMBinding(t, db, "developer-scim")
		wire := service.SCIMCredentialActor(credential, binding)
		admin := service.LocalPrincipal(orgAdmin)
		desired := service.DesiredUser{Active: true, UserName: "developer@example.test", ExternalID: "developer", SubjectRaw: "developer"}
		user, err := s.CreateUser(ctx, wire, orgA, binding, desired)
		if err != nil {
			t.Fatal(err)
		}
		principal := principalOf(t, db, accountOf(t, db, user.ID))
		group, err := s.CreateGroup(ctx, wire, orgA, binding, service.DesiredGroup{DisplayName: "Developers", Members: []string{user.ID}})
		if err != nil {
			t.Fatal(err)
		}
		for _, template := range []domain.Template{domain.TemplateViewer, domain.TemplateRevealer} {
			if _, err := s.CreateMapping(ctx, admin, orgA, binding, service.SCIMMappingSpec{GroupID: group.ID, Template: template, ProjectID: string(prjA1)}); err != nil {
				t.Fatal(err)
			}
		}
		// This unrelated org authority belongs to the human, outside SCIM's scope.
		for _, capability := range []string{"read", "reveal"} {
			execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_dev_b_`+capability+`', '`+string(principal)+`', '`+capability+`', 'org_b', 'prj_b1', NULL, `+ts+`)`)
		}
		seedOrigins(t, db)
		owner := sessionWithWindows(t, db, principal)
		affected := seedSecurityDeveloper(t, db, owner, envScope(envA1), "scim-affected")
		unrelated := seedSecurityDeveloper(t, db, owner, scopeEnv(orgB, prjB1, envB1), "scim-independent")
		desired.Active = false
		if _, err := s.ReplaceUser(ctx, wire, orgA, binding, user.ID, desired); err != nil {
			t.Fatal(err)
		}
		if authenticateCaller(t, db, affected).Principal != "" {
			t.Fatal("SCIM access withdrawal left a developer credential live")
		}
		if authenticateCaller(t, db, unrelated).Principal == "" {
			t.Fatal("SCIM access withdrawal revoked another org's developer credential")
		}
		desired.Active = true
		if _, err := s.ReplaceUser(ctx, wire, orgA, binding, user.ID, desired); err != nil {
			t.Fatal(err)
		}
		if authenticateCaller(t, db, affected).Principal != "" {
			t.Fatal("SCIM reactivation revived a terminally revoked developer credential")
		}
	})
}

func TestDeveloperWholeEnvironmentRulesAndTerminalAuthorityLoss(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		fixture := ceremonyFixture(t, db, "developer-security-rule-authority")
		principal := fixture.admin.boot.PrincipalID
		// Remove the fixture's broad read/reveal setup before exercising the
		// actual rule-management and mint/delivery seams.
		execRaw(t, db, `DELETE FROM grant_origins WHERE grant_id IN (SELECT id FROM grants WHERE principal_id = '`+string(principal)+`' AND capability IN ('read','reveal'))`)
		execRaw(t, db, `DELETE FROM grants WHERE principal_id = '`+string(principal)+`' AND capability IN ('read','reveal')`)
		rules := &service.Rules{DB: db}
		where := domain.Where{Projects: []domain.ProjectID{prjA1}, EnvMode: domain.AxisOnly,
			Envs: map[domain.ProjectID][]domain.EnvID{prjA1: {envA1}}, KeyMode: domain.AxisAll}
		admin := service.LocalPrincipal(orgAdmin)
		if _, err := rules.Create(t.Context(), admin, service.RuleSpec{Target: principal, Capability: domain.CapRead, Org: orgA, Where: where}); err != nil {
			t.Fatal(err)
		}
		reveal, err := rules.Create(t.Context(), admin, service.RuleSpec{Target: principal, Capability: domain.CapReveal, Org: orgA, Where: where})
		if err != nil {
			t.Fatal(err)
		}
		fixture.admin.token = sessionWithWindows(t, db, principal)
		minted, _ := mintDeveloperWithPasskey(t, db, &fixture, time.Hour)
		delivery := deliverySvc(t, db)
		result, err := delivery.Fetch(t.Context(), minted.Value, envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatalf("whole-environment rule delegation must deliver: %v", err)
		}
		found := false
		for _, key := range result.Keys {
			if key.Name == ceremonySecretB && key.Value != nil && *key.Value == "plaintext-"+ceremonySecretB {
				found = true
			}
		}
		if !found {
			t.Fatal("rule-backed whole-environment delegation lost secret authority during delivery")
		}
		if err := rules.Revoke(t.Context(), admin, orgA, reveal.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := rules.Create(t.Context(), admin, service.RuleSpec{Target: principal, Capability: domain.CapReveal, Org: orgA, Where: where}); err != nil {
			t.Fatal(err)
		}
		if _, err := delivery.Fetch(t.Context(), minted.Value, envScope(envA1), "", service.FetchOptions{}); err == nil {
			t.Fatal("restoring whole-environment rule authority revived an old developer bearer")
		}
	})
}

func TestDeveloperNarrowKeyRuleCannotConsentToFutureKeys(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		fixture := ceremonyFixture(t, db, "developer-security-narrow-rule")
		principal := fixture.admin.boot.PrincipalID
		execRaw(t, db, `DELETE FROM grant_origins WHERE grant_id IN (SELECT id FROM grants WHERE principal_id = '`+string(principal)+`' AND capability = 'reveal')`)
		execRaw(t, db, `DELETE FROM grants WHERE principal_id = '`+string(principal)+`' AND capability = 'reveal'`)
		where := domain.Where{Projects: []domain.ProjectID{prjA1}, EnvMode: domain.AxisOnly,
			Envs: map[domain.ProjectID][]domain.EnvID{prjA1: {envA1}}, KeyMode: domain.AxisOnly,
			Keys: map[domain.ProjectID][]domain.RuleKeyItem{prjA1: {{KeyID: "key_" + ceremonySecretA}}}}
		if _, err := (&service.Rules{DB: db}).Create(t.Context(), service.LocalPrincipal(orgAdmin),
			service.RuleSpec{Target: principal, Capability: domain.CapReveal, Org: orgA, Where: where}); err != nil {
			t.Fatal(err)
		}
		fixture.admin.token = sessionWithWindows(t, db, principal)
		_, err := (&service.DeveloperCredentials{DB: db, Auth: fixture.admin.auth}).Mint(t.Context(),
			service.Bearer(fixture.admin.token), envScope(envA1), service.MintDeveloperCredentialRequest{
				Lifetime: time.Hour, ConsentCurrentAndFuture: true, KeyIDs: []string{"key_" + ceremonySecretA, "key_" + ceremonySecretB},
			})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("key-limited reveal must fail whole-environment authority before mint ceremony: %v", err)
		}
	})
}

func prepareSecurityDeveloperMint(t *testing.T, db *store.DB, fixture *ceremonyEnv, username string) (service.Actor, service.MintDeveloperCredentialRequest) {
	t.Helper()
	preview, err := deliverySvc(t, db).FetchAs(t.Context(), service.LocalPrincipal(fixture.admin.boot.PrincipalID), envScope(envA1), "", service.FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(preview.Keys))
	for _, key := range preview.Keys {
		ids = append(ids, key.KeyID)
	}
	login, err := fixture.admin.auth.LocalLogin(t.Context(), username, ceremonyPassword, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	stepUpOptions, err := fixture.admin.auth.StepUpPasskeyStart(t.Context(), login.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	stepUpResponse, err := fixture.device.Assert(stepUpOptions)
	if err != nil {
		t.Fatal(err)
	}
	stepped, err := fixture.admin.auth.StepUpPasskeyFinish(t.Context(), login.SessionToken, stepUpResponse)
	if err != nil {
		t.Fatal(err)
	}
	login.SessionToken = stepped.SessionToken
	intent, err := service.NewDeveloperCredentialReauthIntent(string(envA1), ids, time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	options, err := fixture.admin.auth.ReauthPasskeyStart(t.Context(), login.SessionToken, intent)
	if err != nil {
		t.Fatal(err)
	}
	response, err := fixture.device.Assert(options)
	if err != nil {
		t.Fatal(err)
	}
	result, err := fixture.admin.auth.ReauthPasskeyFinish(t.Context(), login.SessionToken, response)
	if err != nil {
		t.Fatal(err)
	}
	return service.Bearer(result.SessionToken), service.MintDeveloperCredentialRequest{Lifetime: time.Hour, ConsentCurrentAndFuture: true, KeyIDs: ids}
}

func TestDeveloperConcurrentMintCannotExceedInstanceCap(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		const username = "developer-security-concurrent-cap"
		fixture := ceremonyFixture(t, db, username)
		for _, suffix := range []string{"concurrent-one", "concurrent-two", "concurrent-three"} {
			seedSecurityDeveloper(t, db, fixture.admin.token, envScope(envA1), suffix)
		}
		firstActor, request := prepareSecurityDeveloperMint(t, db, &fixture, username)
		secondActor, secondRequest := prepareSecurityDeveloperMint(t, db, &fixture, username)
		peer := approvalPeer(t, db)
		services := []*service.DeveloperCredentials{{DB: db, Auth: fixture.admin.auth}, {DB: peer, Auth: fixture.admin.auth}}
		actors := []service.Actor{firstActor, secondActor}
		requests := []service.MintDeveloperCredentialRequest{request, secondRequest}
		start, done := barrier(2)
		errorsByAttempt := make([]error, 2)
		for index := range 2 {
			go func() {
				defer done.Done()
				<-start
				_, errorsByAttempt[index] = services[index].Mint(t.Context(), actors[index], envScope(envA1), requests[index])
			}()
		}
		close(start)
		done.Wait()
		successes := 0
		for _, err := range errorsByAttempt {
			if err == nil {
				successes++
			} else if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("concurrent mint must succeed or refuse the cap: %v", err)
			}
		}
		if successes != 1 {
			t.Fatalf("one remaining slot must admit exactly one concurrent mint, got %d", successes)
		}
		rows, err := services[0].List(t.Context(), actors[0], domain.Scope{})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 4 {
			t.Fatalf("shared durable state exceeded four live credentials: %d", len(rows))
		}
	})
}

type developerDisclosureBarrier struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (barrier *developerDisclosureBarrier) AfterAttemptReset(_ *service.FetchResult) error {
	barrier.once.Do(func() {
		close(barrier.entered)
		<-barrier.release
	})
	return nil
}

func TestDeveloperConcurrentDisclosureAndRevocation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		fixture := ceremonyFixture(t, db, "developer-security-disclosure-order")
		minted, owner := mintDeveloperWithPasskey(t, db, &fixture, time.Hour)
		peer := approvalPeer(t, db)
		delivery := deliverySvc(t, db)
		probe := &developerDisclosureBarrier{entered: make(chan struct{}), release: make(chan struct{})}
		delivery.FetchProbe = probe
		fetchDone := make(chan error, 1)
		go func() {
			_, err := delivery.Fetch(t.Context(), minted.Value, envScope(envA1), "", service.FetchOptions{})
			fetchDone <- err
		}()
		<-probe.entered
		revocationStarted := make(chan struct{})
		revocationDone := make(chan error, 1)
		go func() {
			close(revocationStarted)
			revocationDone <- (&service.DeveloperCredentials{DB: peer, Auth: fixture.admin.auth}).Revoke(t.Context(), service.Bearer(owner), domain.Scope{}, minted.Credential.ID, false)
		}()
		<-revocationStarted
		close(probe.release)
		if err := <-fetchDone; err != nil && !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("overlapping disclosure must serialize before revocation or refuse: %v", err)
		}
		if err := <-revocationDone; err != nil {
			t.Fatal(err)
		}
		if _, err := delivery.Fetch(t.Context(), minted.Value, envScope(envA1), "", service.FetchOptions{}); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("completed revocation must prevent the next disclosure: %v", err)
		}
	})
}

func TestDeveloperZeroWindowRequiresPasskeyAtOpeningAndMint(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		fixture := ceremonyFixture(t, db, "developer-security-zero-window")
		auth := fixture.admin.auth
		clock := time.Now().UTC()
		auth.Now = func() time.Time { return clock }
		uri, err := auth.EnrolTOTPStart(t.Context(), fixture.admin.token, ceremonyPassword)
		if err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(30 * time.Second)
		confirmed, err := auth.EnrolTOTPConfirm(t.Context(), fixture.admin.token, totpCode(t, uri, clock))
		if err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(30 * time.Second)
		stepped, err := auth.StepUpTOTP(t.Context(), confirmed.SessionToken, totpCode(t, uri, clock))
		if err != nil {
			t.Fatal(err)
		}
		fixture.admin.token = stepped.SessionToken
		preview, err := deliverySvc(t, db).FetchAs(t.Context(), service.LocalPrincipal(fixture.admin.boot.PrincipalID), envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		keys := make([]string, 0, len(preview.Keys))
		for _, key := range preview.Keys {
			keys = append(keys, key.KeyID)
		}
		intent, err := service.NewDeveloperCredentialReauthIntent(string(envA1), keys, time.Hour, true)
		if err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, `UPDATE environments SET reauth_window_seconds=0 WHERE id='env_a1'`)
		clock = clock.Add(30 * time.Second)
		if _, err := auth.ReauthTOTP(t.Context(), fixture.admin.token, intent, totpCode(t, uri, clock)); !errors.Is(err, service.ErrReauthWindowClosed) {
			t.Fatalf("zero-window developer TOTP opened: %v", err)
		}
		execRaw(t, db, `UPDATE environments SET reauth_window_seconds=300 WHERE id='env_a1'`)
		opened, err := auth.ReauthTOTP(t.Context(), fixture.admin.token, intent, totpCode(t, uri, clock))
		if err != nil {
			t.Fatal(err)
		}
		fixture.admin.token = opened.SessionToken
		execRaw(t, db, `UPDATE environments SET reauth_window_seconds=0 WHERE id='env_a1'`)
		credentials := &service.DeveloperCredentials{DB: db, Auth: auth, Now: func() time.Time { return clock }}
		if _, err := credentials.Mint(t.Context(), service.Bearer(fixture.admin.token), envScope(envA1), service.MintDeveloperCredentialRequest{Lifetime: time.Hour, ConsentCurrentAndFuture: true, KeyIDs: keys}); !errors.Is(err, service.ErrReauthWindowClosed) {
			t.Fatalf("TOTP proof survived a new zero-window policy: %v", err)
		}
		options, err := auth.ReauthPasskeyStart(t.Context(), fixture.admin.token, intent)
		if err != nil {
			t.Fatal(err)
		}
		assertion, err := fixture.device.Assert(options)
		if err != nil {
			t.Fatal(err)
		}
		passkey, err := auth.ReauthPasskeyFinish(t.Context(), fixture.admin.token, assertion)
		if err != nil {
			t.Fatal(err)
		}
		minted, err := credentials.Mint(t.Context(), service.Bearer(passkey.SessionToken), envScope(envA1), service.MintDeveloperCredentialRequest{Lifetime: time.Hour, ConsentCurrentAndFuture: true, KeyIDs: keys})
		if err != nil || minted.Value == "" {
			t.Fatalf("zero-window passkey ceremony failed to mint: %v", err)
		}
	})
}
