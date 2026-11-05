package isolation

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Tenant isolation (#23): an actor holding manage-members only in org A must
// not be able to tell a principal whose footprint lies elsewhere from a
// nonexistent one, and must not be able to write for it either. Every
// refusal below is paired with positive controls proving the same writer
// succeeds when the target is inside the actor's reach.

func TestGrantTargetFootprint(t *testing.T) {
	forEngines(t, runGrantTargetFootprint)
}

func runGrantTargetFootprint(t *testing.T, db *store.DB) {
	grants := &service.Grants{DB: db}
	rules := &service.Rules{DB: db}
	admin := service.LocalPrincipal(orgAdmin)

	// Foreign footprints that are NOT plain grants: a rule only, a timed
	// access grant only, and a machine owned by a service account.
	const (
		ruleOnly   = domain.PrincipalID("usr_fp_rule")
		accessOnly = domain.PrincipalID("usr_fp_access")
		machine    = domain.PrincipalID("mch_fp_b")
		ghost      = domain.PrincipalID("usr_fp_ghost")
	)
	for _, p := range []string{string(ruleOnly), string(accessOnly)} {
		execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('`+p+`', 'human', `+ts+`)`)
	}
	execRaw(t, db, `INSERT INTO principals (id, kind, class, created_at) VALUES ('mch_fp_b', 'machine', 'workload', `+ts+`)`)
	execRaw(t, db, `INSERT INTO rules (id, principal_id, capability, org_id, env_mode, key_mode, created_by, created_at) `+
		`VALUES ('rul_fp_b', 'usr_fp_rule', 'read', 'org_b', 'all', 'all', 'usr_bob', `+ts+`)`)
	execRaw(t, db, `INSERT INTO rule_items (id, rule_id, org_id, axis, project_id) VALUES ('rli_fp_b', 'rul_fp_b', 'org_b', 'project', 'prj_b1')`)
	execRaw(t, db, `INSERT INTO access_requests(id,org_id,project_id,environment_id,policy_id,policy_version,requester_principal_id,capabilities,duration_seconds,reason,state,created_at,review_expires_at,granted_at,expires_at) `+
		`VALUES ('ar_fp_b','org_b','prj_b1','env_b1','policy_fixture',1,'usr_fp_access','["read"]',1,'footprint fixture','granted',`+ts+`,`+ts+`,`+ts+`,`+ts+`)`)
	execRaw(t, db, `INSERT INTO access_grants(id,principal_id,capability,org_id,project_id,env_id,request_id,created_at,expires_at) `+
		`VALUES ('ag_fp_b','usr_fp_access','read','org_b','prj_b1','env_b1','ar_fp_b',`+ts+`,`+ts+`)`)
	execRaw(t, db, `INSERT INTO service_accounts (id, principal_id, org_id, project_id, name, kind, created_at, created_by) `+
		`VALUES ('sa_fp_b', 'mch_fp_b', 'org_b', 'prj_b1', 'fp-b', 'workload', `+ts+`, 'usr_bob')`)
	execRaw(t, db, `INSERT INTO sessions (id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent) `+
		`VALUES ('ses_fp_bob', 'usr_bob', `+blobLit(db, []byte("verifier-fp-bob"))+`, 'cli', 1, 0, 'local', '[]', `+ts+`, `+ts+`, `+ts+`, '2099-01-01T00:00:00.000000Z', '2099-01-01T00:00:00.000000Z', '', '')`)

	// More footprints: a developer credential only, a SCIM-provisioned account
	// only, a member of both org A and org B, and the instance-only root.
	const (
		devOnly    = domain.PrincipalID("devp_fp_b")
		scimOnly   = domain.PrincipalID("usr_fp_scim")
		shared     = domain.PrincipalID("usr_fp_shared")
		creatorInB = domain.PrincipalID("usr_fp_creator")
		bOnly      = domain.PrincipalID("usr_fp_bonly")
		bothAdmin  = domain.PrincipalID("usr_fp_bothadmin")
	)
	for _, p := range []domain.PrincipalID{scimOnly, shared, creatorInB, bOnly, bothAdmin} {
		execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('`+string(p)+`', 'human', `+ts+`)`)
	}
	// A developer principal is its own machine-kind principal, minted by the
	// human authority (usr_bob) it delegates from; it never holds a session.
	execRaw(t, db, `INSERT INTO principals (id, kind, class, created_at) VALUES ('devp_fp_b', 'machine', 'developer', `+ts+`)`)
	execRaw(t, db, `INSERT INTO developer_credentials (id, principal_id, authority_principal_id, org_id, project_id, env_id, verifier, prefix_hint, parent_session_id, provider_id, oauth2_provider_id, saml_provider_id, auth_method, authority_generation, credential_epoch, created_at, expires_at) `+
		`VALUES ('devc_fp_b', 'devp_fp_b', 'usr_bob', 'org_b', 'prj_b1', 'env_b1', `+blobLit(db, []byte("verifier-fp-dev"))+`, 'dev', 'ses_fp_dev', '', '', '', 'password', 1, 0, `+ts+`, '2099-01-01T00:00:00.000000Z')`)
	seedSCIMProvider(t, db, "fp-scim", "https://fp-scim.example.test", true)
	scimBinding, err := (&service.SCIM{DB: db}).CreateBinding(t.Context(), service.LocalPrincipal(root), orgB, service.SCIMBindingInput{
		ProviderKind: domain.ProviderOIDC, ProviderSlug: "fp-scim", SubjectSource: domain.SubjectSourceExternalID,
	})
	if err != nil {
		t.Fatalf("scim binding in org B: %v", err)
	}
	execRaw(t, db, `INSERT INTO accounts (id, principal_id, username, display_name, created_at) VALUES ('acc_fp_scim', 'usr_fp_scim', 'fp-scim', 'Fp Scim', `+ts+`)`)
	execRaw(t, db, `INSERT INTO scim_users (id,org_id,binding_id,account_id,user_name,user_name_lower,subject,created_at,updated_at) `+
		`VALUES ('scu_fp','org_b','`+scimBinding.ID+`','acc_fp_scim','fp-scim','fp-scim','fp-subject',`+ts+`,`+ts+`)`)
	for _, g := range []string{
		`('g_fp_sh_a', 'usr_fp_shared', 'read', 'org_a')`,
		`('g_fp_sh_b', 'usr_fp_shared', 'read', 'org_b')`,
		`('g_fp_cr_b', 'usr_fp_creator', 'read', 'org_b')`,
		`('g_fp_bo_b', 'usr_fp_bonly', 'read', 'org_b')`,
		`('g_fp_ba_a', 'usr_fp_bothadmin', 'manage-members', 'org_a')`,
		`('g_fp_ba_b', 'usr_fp_bothadmin', 'manage-members', 'org_b')`,
	} {
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES `+g[:len(g)-1]+`, `+ts+`)`)
	}
	for _, c := range []string{"manage-members", "instance-config"} {
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_fp_cr_`+c+`', 'usr_fp_creator', '`+c+`', NULL, `+ts+`)`)
	}
	seedOrigins(t, db)
	for _, p := range []string{"usr_root", "usr_fp_rule", "usr_fp_access", "mch_fp_b", "usr_fp_scim"} {
		execRaw(t, db, `INSERT INTO sessions (id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent) `+
			`VALUES ('ses_`+p+`', '`+p+`', `+blobLit(db, []byte("verifier-"+p))+`, 'cli', 1, 0, 'local', '[]', `+ts+`, `+ts+`, `+ts+`, '2099-01-01T00:00:00.000000Z', '2099-01-01T00:00:00.000000Z', '', '')`)
	}

	spec := func(target domain.PrincipalID) service.GrantSpec {
		return service.GrantSpec{Target: target, Capability: domain.CapRead, Scope: orgAScope}
	}
	grantRows := func() int64 {
		return queryInt(t, db, `SELECT COUNT(*) FROM grants WHERE org_id = 'org_a' AND capability = 'read'`)
	}
	generation := func(p domain.PrincipalID) int64 {
		return queryInt(t, db, `SELECT session_generation FROM principals WHERE id = '`+string(p)+`'`)
	}
	sessions := func(p domain.PrincipalID) int64 {
		return queryInt(t, db, `SELECT COUNT(*) FROM sessions WHERE principal_id = '`+string(p)+`'`)
	}

	_, missing := grants.Create(t.Context(), admin, spec(ghost))
	if !errors.Is(missing, service.ErrUnknownPrincipal) {
		t.Fatalf("nonexistent target = %v, want ErrUnknownPrincipal", missing)
	}

	t.Run("foreign_targets_look_nonexistent", func(t *testing.T) {
		watched := []domain.PrincipalID{bob, root, ruleOnly, accessOnly, machine, devOnly, scimOnly}
		gens, counts := map[domain.PrincipalID]int64{}, map[domain.PrincipalID]int64{}
		for _, p := range watched {
			gens[p], counts[p] = generation(p), sessions(p)
		}
		rowsBefore := grantRows()
		for name, target := range map[string]domain.PrincipalID{
			"grant_footprint":     bob,
			"rule_footprint":      ruleOnly,
			"access_footprint":    accessOnly,
			"machine_owned_b":     machine,
			"developer_footprint": devOnly,
			"scim_footprint":      scimOnly,
			"instance_only":       root,
		} {
			_, err := grants.Create(t.Context(), admin, spec(target))
			if !errors.Is(err, service.ErrUnknownPrincipal) {
				t.Fatalf("%s: err = %v, want ErrUnknownPrincipal", name, err)
			}
			if err.Error() != missing.Error() {
				t.Fatalf("%s: response differs from a nonexistent principal:\n  foreign: %q\n  missing: %q", name, err, missing)
			}
		}
		if got := grantRows(); got != rowsBefore {
			t.Fatalf("org A read grants %d -> %d: a refused call wrote", rowsBefore, got)
		}
		for _, p := range watched {
			if generation(p) != gens[p] || sessions(p) != counts[p] {
				t.Fatalf("%s: session generation %d -> %d, sessions %d -> %d: a refused call touched sessions", p, gens[p], generation(p), counts[p], sessions(p))
			}
		}
		if sessions(bob) != 1 {
			t.Fatalf("bob's session count = %d, want 1", sessions(bob))
		}
	})

	t.Run("project_manager_cannot_name_foreign_targets", func(t *testing.T) {
		projectScope := service.GrantSpec{Capability: domain.CapRead, Scope: scopeProject(orgA, prjA1)}
		projectScope.Target = ghost
		_, missingProject := grants.Create(t.Context(), service.LocalPrincipal(prjAdmin), projectScope)
		if !errors.Is(missingProject, service.ErrUnknownPrincipal) {
			t.Fatalf("project manager, nonexistent target = %v", missingProject)
		}
		projectScope.Target = bob
		if _, err := grants.Create(t.Context(), service.LocalPrincipal(prjAdmin), projectScope); err == nil || err.Error() != missingProject.Error() || !errors.Is(err, service.ErrUnknownPrincipal) {
			t.Fatalf("project manager, foreign target = %v, want exactly %q", err, missingProject)
		}
	})

	t.Run("template_and_rule_replace_are_uniform", func(t *testing.T) {
		_, missingTpl := grants.ApplyTemplate(t.Context(), admin, domain.TemplateViewer, ghost, orgAScope)
		if !errors.Is(missingTpl, service.ErrUnknownPrincipal) {
			t.Fatalf("template for nonexistent target = %v", missingTpl)
		}
		for _, target := range []domain.PrincipalID{bob, root, scimOnly, devOnly} {
			if _, err := grants.ApplyTemplate(t.Context(), admin, domain.TemplateViewer, target, orgAScope); err == nil || err.Error() != missingTpl.Error() || !errors.Is(err, service.ErrUnknownPrincipal) {
				t.Fatalf("template for %s = %v, want exactly %q", target, err, missingTpl)
			}
		}
		replace := func(target domain.PrincipalID) error {
			_, err := rules.Replace(t.Context(), admin, service.ReplaceRulesSpec{Org: orgA, Target: target, Create: []service.RuleSpec{{
				Target: target, Capability: domain.CapRead, Org: orgA, Where: whereIn(domain.AxisAll, nil, domain.AxisAll)}}})
			return err
		}
		missingReplace := replace(ghost)
		if !errors.Is(missingReplace, service.ErrUnknownPrincipal) {
			t.Fatalf("replace for nonexistent target = %v", missingReplace)
		}
		if err := replace(bob); err == nil || err.Error() != missingReplace.Error() || !errors.Is(err, service.ErrUnknownPrincipal) {
			t.Fatalf("replace for foreign target = %v, want exactly %q", err, missingReplace)
		}
	})

	t.Run("rules_create_is_uniform", func(t *testing.T) {
		ruleSpec := func(target domain.PrincipalID) service.RuleSpec {
			return service.RuleSpec{Target: target, Capability: domain.CapRead, Org: orgA,
				Where: whereIn(domain.AxisAll, nil, domain.AxisAll)}
		}
		_, missingRule := rules.Create(t.Context(), admin, ruleSpec(ghost))
		if !errors.Is(missingRule, service.ErrUnknownPrincipal) {
			t.Fatalf("rule for nonexistent target = %v, want ErrUnknownPrincipal", missingRule)
		}
		before := queryInt(t, db, `SELECT COUNT(*) FROM rules WHERE org_id = 'org_a'`)
		_, err := rules.Create(t.Context(), admin, ruleSpec(bob))
		if !errors.Is(err, service.ErrUnknownPrincipal) || err.Error() != missingRule.Error() {
			t.Fatalf("rule for foreign target = %v, want exactly %q", err, missingRule)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM rules WHERE org_id = 'org_a'`); got != before {
			t.Fatalf("org A rules %d -> %d: a refused call wrote", before, got)
		}
		for name, target := range map[string]domain.PrincipalID{"machine": machine, "rule_only": ruleOnly, "access_only": accessOnly} {
			if _, err := rules.Create(t.Context(), admin, ruleSpec(target)); !errors.Is(err, service.ErrUnknownPrincipal) || err.Error() != missingRule.Error() {
				t.Fatalf("rule for foreign %s target = %v, want exactly %q", name, err, missingRule)
			}
		}
		// Control: the same call for an in-reach, footprint-less principal works.
		if _, err := rules.Create(t.Context(), admin, ruleSpec(nobody)); err != nil {
			t.Fatalf("control rule for a footprint-less target: %v", err)
		}
	})

	t.Run("controls", func(t *testing.T) {
		if _, err := grants.Create(t.Context(), admin, spec(nobody)); err != nil {
			t.Fatalf("footprint-less target: %v", err)
		}
		if _, err := grants.Create(t.Context(), admin, service.GrantSpec{Target: alice, Capability: domain.CapPublish, Scope: orgAScope}); err != nil {
			t.Fatalf("target already inside org A: %v", err)
		}
		inv, err := grants.InviteMember(t.Context(), admin, service.InviteSpec{
			Username: "fp-invitee", Scope: orgAScope, Template: domain.TemplateViewer, Delivery: "terminal",
		})
		if err != nil || inv.GrantsCreated == 0 {
			t.Fatalf("invite with template = %+v, %v", inv, err)
		}
		// A member of org A and org B is org A's member: still grantable.
		if _, err := grants.Create(t.Context(), admin, spec(shared)); err != nil {
			t.Fatalf("target already in org A and org B: %v", err)
		}
		if _, err := rules.Create(t.Context(), admin, service.RuleSpec{Target: shared, Capability: domain.CapRead, Org: orgA,
			Where: whereIn(domain.AxisAll, nil, domain.AxisAll)}); err != nil {
			t.Fatalf("rule for a target already in org A and org B: %v", err)
		}
		// Project-scope manage-members still grants what the actor holds to a
		// member already in the org.
		if _, err := grants.Create(t.Context(), service.LocalPrincipal(prjAdmin), service.GrantSpec{
			Target: alice, Capability: domain.CapRead, Scope: scopeProject(orgA, prjA1)}); err != nil {
			t.Fatalf("project-scope manager granting an org A member: %v", err)
		}
		// An administrator of both orgs may name a member of org B only.
		if _, err := grants.Create(t.Context(), service.LocalPrincipal(bothAdmin), spec(bOnly)); err != nil {
			t.Fatalf("administrator of both orgs granting an org B member: %v", err)
		}
		// Creating an org grants its creator the first membership even when the
		// creator already belongs to another org.
		if _, err := (&service.Orgs{DB: db}).Create(t.Context(), service.LocalPrincipal(creatorInB), "fp-created", true, []byte(`{}`)); err != nil {
			t.Fatalf("org creation by a member of another org: %v", err)
		}
		// Instance-scope manage-members reaches every org.
		if _, err := grants.Create(t.Context(), service.LocalPrincipal(root), spec(bob)); err != nil {
			t.Fatalf("instance manager granting a foreign-footprint target: %v", err)
		}
	})
}

// Credential-reset reach follows the footprint, not the plain grants alone:
// rules and access grants are grants in the widened model, so a member whose
// only org-B access is a rule is org-B-bounded, and a member of two orgs stays
// instance-only.

func TestCredentialResetFootprintOrgA(t *testing.T) { forEngines(t, runResetFootprint(orgA)) }
func TestCredentialResetFootprintOrgB(t *testing.T) { forEngines(t, runResetFootprint(orgB)) }

func runResetFootprint(resetOrg domain.OrgID) func(t *testing.T, db *store.DB) {
	return func(t *testing.T, db *store.DB) {
		fa := bootstrapFactorAdmin(t, db)
		auth, ctx := fa.auth, t.Context()
		// The operator holds credential-reset in exactly one org.
		if _, err := (&service.Grants{DB: db}).Create(ctx, service.LocalPrincipal(root), service.GrantSpec{
			Target: fa.boot.PrincipalID, Capability: domain.CapCredentialReset, Scope: domain.Scope{Org: resetOrg},
		}); err != nil {
			t.Fatal(err)
		}
		base := time.Now().UTC()
		clk := base
		auth.Now = func() time.Time { return clk }
		token := enrolTOTPAndStepUp(t, auth, ctx, base, &clk, fa.password)

		human := func(id string) domain.PrincipalID {
			execRaw(t, db, fmt.Sprintf(`INSERT INTO principals (id, kind, created_at) VALUES ('%s', 'human', %s)`, id, ts))
			execRaw(t, db, fmt.Sprintf(`INSERT INTO accounts (id, principal_id, username, display_name, created_at) VALUES ('acc_%s', '%s', '%s', '%s', %s)`, id, id, id, id, tsMicro))
			return domain.PrincipalID(id)
		}
		viaGrant := func(id, org string) domain.PrincipalID {
			p := human(id)
			execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_%s_%s', '%s', 'read', '%s', %s)`, id, org, id, org, ts))
			return p
		}
		viaRule := func(id, rule string) {
			execRaw(t, db, fmt.Sprintf(`INSERT INTO rules (id, principal_id, capability, org_id, env_mode, key_mode, created_by, created_at) VALUES ('%s', '%s', 'read', 'org_b', 'all', 'all', 'usr_root', %s)`, rule, id, ts))
		}
		ruleB := human("usr_rst_rule_b")
		viaRule("usr_rst_rule_b", "rul_rst_b")
		accessB := human("usr_rst_access_b")
		execRaw(t, db, `INSERT INTO access_requests(id,org_id,project_id,environment_id,policy_id,policy_version,requester_principal_id,capabilities,duration_seconds,reason,state,created_at,review_expires_at,granted_at,expires_at) `+
			`VALUES ('ar_rst_b','org_b','prj_b1','env_b1','policy_fixture',1,'usr_rst_access_b','["read"]',1,'reset fixture','granted',`+ts+`,`+ts+`,`+ts+`,`+ts+`)`)
		execRaw(t, db, `INSERT INTO access_grants(id,principal_id,capability,org_id,project_id,env_id,request_id,created_at,expires_at) `+
			`VALUES ('ag_rst_b','usr_rst_access_b','read','org_b','prj_b1','env_b1','ar_rst_b',`+ts+`,`+ts+`)`)
		scimB := human("usr_rst_scim_b")
		seedSCIMProvider(t, db, "rst-scim", "https://rst-scim.example.test", true)
		binding, err := (&service.SCIM{DB: db}).CreateBinding(ctx, service.LocalPrincipal(root), orgB, service.SCIMBindingInput{
			ProviderKind: domain.ProviderOIDC, ProviderSlug: "rst-scim", SubjectSource: domain.SubjectSourceExternalID,
		})
		if err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, `INSERT INTO scim_users (id,org_id,binding_id,account_id,user_name,user_name_lower,subject,created_at,updated_at) `+
			`VALUES ('scu_rst','org_b','`+binding.ID+`','acc_usr_rst_scim_b','rst','rst','rst-subject',`+ts+`,`+ts+`)`)
		inA := viaGrant("usr_rst_a", "org_a")
		both := viaGrant("usr_rst_both", "org_a")
		viaRule("usr_rst_both", "rul_both_b")
		instanceOnly := human("usr_rst_inst")
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_rst_inst', 'usr_rst_inst', 'instance-config', NULL, `+ts+`)`)
		seedOrigins(t, db)

		reset := func(p domain.PrincipalID) error {
			_, err := auth.ResetCredential(ctx, service.Bearer(token), string(p), "response")
			return err
		}
		// The HTTP route collapses every refusal to one uniform 401; at the
		// service seam a refusal is the org-bounded not-found or the instance
		// unauthorized, and never a minted authority.
		refused := func(what string, p domain.PrincipalID, want error) {
			t.Helper()
			mints := queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.authority_minted'`)
			authorities := queryInt(t, db, `SELECT COUNT(*) FROM credential_authorities`)
			if err := reset(p); !errors.Is(err, want) {
				t.Fatalf("%s: %v, want %v", what, err, want)
			}
			if got := queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.authority_minted'`); got != mints {
				t.Fatalf("%s: a refused reset minted an authority", what)
			}
			if got := queryInt(t, db, `SELECT COUNT(*) FROM credential_authorities`); got != authorities {
				t.Fatalf("%s: a refused reset persisted a credential authority", what)
			}
		}
		// Multi-org and instance-capability targets are out of reach of an
		// org-scoped holder, whichever org the operator holds.
		refused("a member of two orgs", both, domain.ErrUnauthorized)
		refused("an instance-capability target", instanceOnly, domain.ErrUnauthorized)
		bTargets := map[string]domain.PrincipalID{"rules-only": ruleB, "access-grant-only": accessB, "scim-only": scimB}
		for what, p := range bTargets {
			if resetOrg == orgB {
				if err := reset(p); err != nil {
					t.Fatalf("org B operator resetting a %s org B member: %v", what, err)
				}
			} else {
				refused("org A operator reaching a "+what+" org B member", p, domain.ErrNotFound)
			}
		}
		if resetOrg == orgB {
			refused("org B operator reaching an org A member", inA, domain.ErrNotFound)
		} else if err := reset(inA); err != nil {
			t.Fatalf("org A operator resetting an org A member: %v", err)
		}
	}
}
