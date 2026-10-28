package isolation

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// This is a manual sequence through existing owners, not an account quarantine
// flag. Its external prerequisite is a fence against untrusted policy writers.
// Deliberately allow attacker logins between steps: reset alone is insufficient.
func TestManualIncidentContainment(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		f := ceremonyFixture(t, db, "incident-human")
		target := f.admin.boot.PrincipalID
		grants := &service.Grants{DB: db}
		rules := &service.Rules{DB: db}
		operator := service.Bearer(sessionWithWindows(t, db, root))
		mode, envs := onlyEnvs(envA1)
		var ruleIDs []string
		for _, capability := range []domain.Capability{domain.CapRead, domain.CapReveal} {
			rule, err := rules.Create(ctx, operator, service.RuleSpec{Target: target, Capability: capability, Org: orgA, Where: whereIn(mode, envs, domain.AxisAll)})
			if err != nil {
				t.Fatal(err)
			}
			ruleIDs = append(ruleIDs, rule.ID)
		}
		// Policy changes ended the original login. The compromised passkey can
		// still create a fully assured session, including a new delegation.
		login, err := discoverableLogin(t, f.admin.auth, ctx, f.device)
		if err != nil {
			t.Fatal(err)
		}
		f.admin.token = login.SessionToken
		delegation, session := mintDeveloperWithPasskey(t, db, &f, time.Hour)
		delivery := deliverySvc(t, db)
		if _, err := delivery.Fetch(ctx, delegation.Value, envScope(envA1), "", service.FetchOptions{}); err != nil {
			t.Fatalf("pre-containment disclosure control: %v", err)
		}
		reset, err := f.admin.auth.BreakGlassResetCredential(ctx, string(target), "terminal")
		if err != nil {
			t.Fatal(err)
		}
		for _, bearer := range []string{session, delegation.Value} {
			if authenticateCaller(t, db, bearer).Principal != "" {
				t.Fatal("reset retained a human session or developer delegation")
			}
		}
		// Issuance is not password replacement or factor/IdP revocation.
		passwordLogin, err := f.admin.auth.LocalLogin(ctx, "incident-human", ceremonyPassword, service.ArtifactBrowser)
		if err != nil || passwordLogin.Challenge == nil {
			t.Fatalf("old password must still reach its enrolled-factor challenge: %+v, %v", passwordLogin, err)
		}
		login, err = discoverableLogin(t, f.admin.auth, ctx, f.device)
		if err != nil {
			t.Fatal(err)
		}
		f.admin.token = login.SessionToken
		interleaved, _ := mintDeveloperWithPasskey(t, db, &f, time.Hour)
		if _, err := delivery.Fetch(ctx, interleaved.Value, envScope(envA1), "", service.FetchOptions{}); err != nil {
			t.Fatalf("reset alone must not be mistaken for containment: %v", err)
		}
		// Sweep instance AND org membership. A project list omits ancestors.
		for _, scope := range []domain.Scope{{}, {Org: orgA}, {Org: orgB}} {
			lines, err := grants.List(ctx, operator, scope)
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range lines {
				if line.Principal == target {
					if err := grants.Revoke(ctx, operator, service.GrantSpec{Target: target, Capability: line.Capability, Scope: line.Scope}); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		// Grants-only containment is false: independent whole-environment
		// rules still authorize a new login and a real passkey mint ceremony.
		login, err = discoverableLogin(t, f.admin.auth, ctx, f.device)
		if err != nil {
			t.Fatal(err)
		}
		f.admin.token = login.SessionToken
		ruleDelegation, _ := mintDeveloperWithPasskey(t, db, &f, time.Hour)
		if _, err := delivery.Fetch(ctx, ruleDelegation.Value, envScope(envA1), "", service.FetchOptions{}); err != nil {
			t.Fatalf("residual rule positive control: %v", err)
		}
		for _, id := range ruleIDs {
			if err := rules.Revoke(ctx, operator, orgA, id); err != nil {
				t.Fatal(err)
			}
		}
		// Final reset retires all interleaved sessions/delegations and sweeps
		// the earlier outstanding reset authority. This is the completion edge.
		finalReset, err := f.admin.auth.BreakGlassResetCredential(ctx, string(target), "terminal")
		if err != nil {
			t.Fatal(err)
		}
		for _, bearer := range []string{session, f.admin.token, delegation.Value, interleaved.Value, ruleDelegation.Value} {
			if _, err := delivery.FetchAs(ctx, service.Bearer(bearer), envScope(envA1), "", service.FetchOptions{}); !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatalf("retired artifact retained delivery after completion: %v", err)
			}
		}
		if err := f.admin.auth.EstablishCredential(ctx, reset.Authority, "superseded incident password"); err == nil {
			t.Fatal("restarting the reset sequence retained an old establishment authority")
		}
		// Even the still-compromised allowed factor can log in, but cannot
		// restore the removed authority or mint a new developer credential.
		login, err = discoverableLogin(t, f.admin.auth, ctx, f.device)
		if err != nil {
			t.Fatal(err)
		}
		assertIncidentAuthorityAbsent(t, db, f.admin.auth, login.SessionToken)
		const replacement = "independently delivered replacement password"
		if err := f.admin.auth.EstablishCredential(ctx, finalReset.Authority, replacement); err != nil {
			t.Fatal(err)
		}
		if _, err := f.admin.auth.LocalLogin(ctx, "incident-human", ceremonyPassword, service.ArtifactCLI); err == nil {
			t.Fatal("old password still accepted after credential replacement")
		}
		// Deliberately regrant one capability as a terminal-bearer negative
		// control. This is not a safe reopen while the test factor is stolen.
		if _, err := grants.Create(ctx, operator, service.GrantSpec{Target: target, Capability: domain.CapRead, Scope: domain.Scope{Org: orgA}}); err != nil {
			t.Fatal(err)
		}
		if authenticateCaller(t, db, ruleDelegation.Value).Principal != "" {
			t.Fatal("reopening policy revived a pre-containment delegation")
		}
		if _, err := f.values.Get(ctx, service.LocalPrincipal(custodian), envScope(envA1), ceremonySecretA, false); err != nil {
			t.Fatalf("unaffected principal control: %v", err)
		}
	})
}

func assertIncidentAuthorityAbsent(t *testing.T, db *store.DB, auth *service.Auth, token string) {
	t.Helper()
	ctx := t.Context()
	if authenticateCaller(t, db, token).Principal == "" {
		t.Fatal("negative control needs a successful fresh login")
	}
	if _, err := deliverySvc(t, db).FetchAs(ctx, service.Bearer(token), envScope(envA1), "", service.FetchOptions{}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("fresh login regained delivery: %v", err)
	}
	_, err := (&service.DeveloperCredentials{DB: db, Auth: auth}).Mint(ctx, service.Bearer(token), envScope(envA1), service.MintDeveloperCredentialRequest{Lifetime: time.Hour, ConsentCurrentAndFuture: true})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("fresh login passed developer mint authorization: %v", err)
	}
	if _, err := (&service.Grants{DB: db}).Create(ctx, service.Bearer(token), service.GrantSpec{Target: authenticateCaller(t, db, token).Principal, Capability: domain.CapRead, Scope: domain.Scope{Org: orgA}}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("fresh login could regrant its own authority: %v", err)
	}
}

func TestCredentialResetTerminallyRevokesDeveloperCustody(t *testing.T) {
	for _, network := range []bool{false, true} {
		name := "host"
		if network {
			name = "network"
		}
		t.Run(name, func(t *testing.T) { testIncidentResetCustody(t, network) })
	}
}

func testIncidentResetCustody(t *testing.T, network bool) {
	t.Helper()
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := ceremonyFixture(t, db, "incident-reset")
		var operator string
		if network {
			// Network reset excludes instance-capability holders. Preserve that
			// boundary while exercising the other tier of the shared owner.
			grants := &service.Grants{DB: db}
			rows, err := grants.List(t.Context(), service.LocalPrincipal(root), domain.Scope{})
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if row.Principal == f.admin.boot.PrincipalID {
					if err := grants.Revoke(t.Context(), service.LocalPrincipal(root), service.GrantSpec{Target: row.Principal, Capability: row.Capability}); err != nil {
						t.Fatal(err)
					}
				}
			}
			grantInstanceCapability(t, db, root, domain.CapCredentialReset)
			operator = sessionWithWindows(t, db, root)
			login, err := discoverableLogin(t, f.admin.auth, t.Context(), f.device)
			if err != nil {
				t.Fatal(err)
			}
			f.admin.token = login.SessionToken
		}
		minted, _ := mintDeveloperWithPasskey(t, db, &f, time.Hour)
		reset := func() error {
			if network {
				_, err := f.admin.auth.ResetCredential(t.Context(), service.Bearer(operator), string(f.admin.boot.PrincipalID), "response")
				return err
			}
			_, err := f.admin.auth.BreakGlassResetCredential(t.Context(), string(f.admin.boot.PrincipalID), "terminal")
			return err
		}
		if err := reset(); err != nil {
			t.Fatal(err)
		}
		if err := reset(); err != nil {
			t.Fatalf("resuming reset: %v", err)
		}
		rows, err := (&service.DeveloperCredentials{DB: db}).List(t.Context(), service.Bearer(sessionWithWindows(t, db, f.admin.boot.PrincipalID)), domain.Scope{})
		if err != nil || len(rows) != 1 || rows[0].RevokedAt.IsZero() {
			t.Fatalf("reset must durably revoke developer custody, not only mismatch generation: %+v, %v", rows, err)
		}
		trail, err := (&service.Audits{DB: db}).InstanceQuery(t.Context(), root, service.AuditFilter{Type: string(audit.EventDeveloperCredentialRevoked), Limit: 10})
		if err != nil || len(trail.Events) != 1 {
			t.Fatalf("reset must audit terminal developer revocation: %+v, %v", trail, err)
		}
		expectedClass := audit.ActorBreakGlass
		if network {
			expectedClass = audit.ActorHuman
			if trail.Events[0].Actor.ID != string(root) {
				t.Fatalf("network reset lost operator attribution: %+v", trail.Events[0].Event)
			}
		}
		if trail.Events[0].Actor.Class != expectedClass || trail.Events[0].AuthorityID != string(minted.Credential.AuthorityPrincipal) {
			t.Fatalf("reset attribution lost host authority or delegator: %+v", trail.Events[0].Event)
		}
		issuer := "break-glass"
		if network {
			issuer = "credential-reset"
		}
		for _, typ := range []audit.EventType{audit.EventAuthAuthorityMinted, audit.EventAuthCredentialResetIssued} {
			issued, err := (&service.Audits{DB: db}).InstanceQuery(t.Context(), root, service.AuditFilter{Type: string(typ), Limit: 20})
			if err != nil {
				t.Fatal(err)
			}
			found := 0
			for _, event := range issued.Events {
				// Bootstrap also mints an authority. Match only this target's
				// administrator-issued resets, not that initial ceremony.
				var payload struct {
					IssuedBy string `json:"issued_by"`
				}
				if err := json.Unmarshal([]byte(event.RawPayload), &payload); err != nil {
					t.Fatal(err)
				}
				if payload.IssuedBy != issuer {
					continue
				}
				found++
				if event.Actor.Class != expectedClass || (network && event.Actor.ID != string(root)) || (!network && event.Actor.ID != "") {
					t.Fatalf("%s attributed the operator's reset to its target: %+v", typ, event.Event)
				}
			}
			if found != 2 {
				t.Fatalf("%s reset attribution records: got %d, want 2", typ, found)
			}
		}
	})
}

func TestCredentialResetConcurrentDeveloperMint(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		f := ceremonyFixture(t, db, "incident-concurrent")
		ids := []string{}
		// Match the complete materialized key set, including the fixture key.
		preview, err := deliverySvc(t, db).FetchAs(ctx, service.LocalPrincipal(f.admin.boot.PrincipalID), envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range preview.Keys {
			ids = append(ids, key.KeyID)
		}
		intent, err := service.NewDeveloperCredentialReauthIntent(string(envA1), ids, time.Hour, true)
		if err != nil {
			t.Fatal(err)
		}
		opts, err := f.admin.auth.ReauthPasskeyStart(ctx, f.admin.token, intent)
		if err != nil {
			t.Fatal(err)
		}
		response, err := f.device.Assert(opts)
		if err != nil {
			t.Fatal(err)
		}
		proof, err := f.admin.auth.ReauthPasskeyFinish(ctx, f.admin.token, response)
		if err != nil {
			t.Fatal(err)
		}
		type result struct {
			mint service.MintedDeveloperCredential
			err  error
		}
		start := make(chan struct{})
		minted := make(chan result, 1)
		reset := make(chan error, 1)
		go func() {
			<-start
			svc := &service.DeveloperCredentials{DB: db, Auth: f.admin.auth}
			out, err := svc.Mint(ctx, service.Bearer(proof.SessionToken), envScope(envA1), service.MintDeveloperCredentialRequest{Lifetime: time.Hour, ConsentCurrentAndFuture: true, KeyIDs: ids})
			minted <- result{out, err}
		}()
		go func() {
			<-start
			_, err := f.admin.auth.BreakGlassResetCredential(ctx, string(f.admin.boot.PrincipalID), "terminal")
			reset <- err
		}()
		close(start)
		mintResult, resetErr := <-minted, <-reset
		if resetErr != nil {
			t.Fatal(resetErr)
		}
		if mintResult.err != nil && !errors.Is(mintResult.err, domain.ErrUnauthenticated) {
			t.Fatalf("concurrent mint must precede reset or refuse retired session: %v", mintResult.err)
		}
		for _, bearer := range []string{proof.SessionToken, mintResult.mint.Value} {
			if bearer != "" && authenticateCaller(t, db, bearer).Principal != "" {
				t.Fatal("concurrent mint retained authority past reset completion")
			}
		}
	})
}

func TestManualIncidentContainmentAllowedIdPCannotRemintAuthority(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		f := oidcAdmin(t, db)
		_, idp := configureProvider(t, f.auth, ctx, f.boot.PrincipalID, "incident-idp", service.ProviderInput{DisplayName: "Incident IdP", ClientID: "c", ClientSecret: "s", Scopes: "openid", AssurancePolicy: strptr(`{"amr_sets":[["mfa"]]}`), Enabled: true})
		linkOn(t, f.auth, ctx, "incident-idp", "compromised-subject", f.password)
		idp.AMR = []string{"mfa", "otp"}
		idp.AuthTime = time.Now()
		grants := &service.Grants{DB: db}
		operator := service.Bearer(sessionWithWindows(t, db, root))
		for _, capability := range []domain.Capability{domain.CapRead, domain.CapReveal} {
			if _, err := grants.Create(ctx, operator, service.GrantSpec{Target: f.boot.PrincipalID, Capability: capability, Scope: domain.Scope{Org: orgA}}); err != nil {
				t.Fatal(err)
			}
		}
		initial := oidcLogin(t, f.auth, ctx, "incident-idp", "compromised-subject")
		values := &service.Values{DB: db, Keyring: f.auth.Keyring, Auth: f.auth}
		if _, err := values.Get(ctx, service.Bearer(initial.SessionToken), envScope(envA1), "SHARED_KEY", false); err != nil {
			t.Fatalf("allowed IdP initial read control: %v", err)
		}
		// Strong IdP assurance and read/reveal grants must pass authorization
		// before the sweep. Only the separate mint ceremony is missing here.
		_, err := (&service.DeveloperCredentials{DB: db, Auth: f.auth}).Mint(ctx, service.Bearer(initial.SessionToken), envScope(envA1), service.MintDeveloperCredentialRequest{Lifetime: time.Hour, ConsentCurrentAndFuture: true})
		if !errors.Is(err, service.ErrNoReauthWindow) {
			t.Fatalf("IdP mint authorization control did not reach fresh-proof gate: %v", err)
		}
		for _, scope := range []domain.Scope{{}, {Org: orgA}, {Org: orgB}} {
			rows, err := grants.List(ctx, operator, scope)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if row.Principal == f.boot.PrincipalID {
					if err := grants.Revoke(ctx, operator, service.GrantSpec{Target: row.Principal, Capability: row.Capability, Scope: row.Scope}); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		if _, err := f.auth.BreakGlassResetCredential(ctx, string(f.boot.PrincipalID), "terminal"); err != nil {
			t.Fatal(err)
		}
		if authenticateCaller(t, db, initial.SessionToken).Principal != "" {
			t.Fatal("old federated human session survived containment")
		}
		// Do not disable this synthetic provider: this is the adversarial
		// control proving an allowed compromised IdP cannot restore policy.
		fresh := oidcLogin(t, f.auth, ctx, "incident-idp", "compromised-subject")
		assertIncidentAuthorityAbsent(t, db, f.auth, fresh.SessionToken)
		if _, err := values.Get(ctx, service.Bearer(fresh.SessionToken), envScope(envA1), "SHARED_KEY", false); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("allowed IdP recreated read authority: %v", err)
		}
	})
}

func TestManualIncidentDynamicCleanupResumes(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		f, provider := ttlFixture(t, db)
		operator := service.LocalPrincipal(alice)
		project := domain.Scope{Org: orgA, Project: prjA1}
		if _, err := f.svc.RevokeLease(ctx, operator, f.scope, f.lease.Lease.ID); err != nil {
			t.Fatal(err)
		}
		if err := f.svc.RevokeCredential(ctx, operator, project, f.lease.Lease.ProviderID); err != nil {
			t.Fatal(err)
		}
		opens := provider.opens
		f.sweep(t)
		if got := f.current(t); got.State != "revoking" || provider.opens != opens || len(provider.drops) != 0 {
			t.Fatalf("missing cleanup authority must remain pending without a provider call: %+v", got)
		}
		if err := f.svc.ReplaceCredential(ctx, operator, project, f.lease.Lease.ProviderID, []byte("replacement-cleanup-only")); err != nil {
			t.Fatal(err)
		}
		// Restart the worker facade over the same durable ledger and prove it
		// loads the replacement custody, rather than reviving the old value.
		resumed := *f.svc
		resumed.Runtime = store.NewDynamicRuntime(db)
		resumed.ProviderFactory = func(_ dynamic.Kind, _, _, credential string) (dynamic.Provider, error) {
			if credential != "replacement-cleanup-only" {
				t.Error("cleanup reopened compromised provider custody")
			}
			return provider, nil
		}
		f.svc = &resumed
		f.now = f.now.Add(time.Minute)
		provider.ctl.ambiguousDrop = true
		f.sweep(t)
		if got := f.current(t); got.State != "unknown" {
			t.Fatalf("ambiguous upstream cleanup must stay unknown: %+v", got)
		}
		provider.ctl.ambiguousDrop = false
		if _, err := f.svc.SettleLease(ctx, operator, f.scope, f.lease.Lease.ID); err != nil {
			t.Fatal(err)
		}
		f.sweep(t)
		if got := f.current(t); got.State != "revoked" || len(provider.drops) != 2 {
			t.Fatalf("reconciliation did not complete the durable revoke: %+v", got)
		}
	})
}
