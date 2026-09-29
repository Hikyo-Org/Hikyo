package isolation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Member access rules (member-access-rules ADR), stage A. Every test here is
// built to discriminate fail-closed from fail-open: each denial has a
// positive control proving the same call succeeds for someone who should
// reach it, so a denial for the wrong reason cannot pass.

const (
	carol = domain.PrincipalID("usr_carol")
	dave  = domain.PrincipalID("usr_dave")
	erin  = domain.PrincipalID("usr_erin")
	frank = domain.PrincipalID("usr_frank")
	gina  = domain.PrincipalID("usr_gina")
	hank  = domain.PrincipalID("usr_hank")
	ivan  = domain.PrincipalID("usr_ivan")
)

type rulesFixture struct {
	rules    *service.Rules
	keys     *service.Keys
	values   *service.Values
	dbKeyID  string
	strKeyID string
}

func seedRulesFixture(t *testing.T, db *store.DB) rulesFixture {
	t.Helper()
	for _, p := range []domain.PrincipalID{carol, dave, erin, frank, gina, hank, ivan} {
		execRaw(t, db, "INSERT INTO principals (id, kind, created_at) VALUES ('"+string(p)+"', 'human', "+ts+")")
	}
	f := rulesFixture{rules: &service.Rules{DB: db}, keys: keySvc(t, db), values: valueSvc(t, db)}
	project := scopeProject(orgA, prjA1)
	for _, k := range []struct {
		name, folder string
		id           *string
	}{{"DB_PASSWORD", "db", &f.dbKeyID}, {"STRIPE_KEY", "stripe", &f.strKeyID}} {
		key, err := f.keys.Create(t.Context(), service.LocalPrincipal(custodian), project, service.KeySpec{
			Name: k.name, FolderPath: k.folder, Classification: string(schema.Secret),
			Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}},
			Presence:    schema.DefaultPresenceRules(),
		}, nil)
		if err != nil {
			t.Fatalf("create %s: %v", k.name, err)
		}
		*k.id = key.ID
		if _, _, err := f.values.Declare(t.Context(), service.LocalPrincipal(custodian), project,
			[]string{string(envA1), string(envProd)}, k.name, "plain-"+k.name); err != nil {
			t.Fatalf("declare %s: %v", k.name, err)
		}
	}
	return f
}

func onlyEnvs(envs ...domain.EnvID) (domain.AxisMode, map[domain.ProjectID][]domain.EnvID) {
	return domain.AxisOnly, map[domain.ProjectID][]domain.EnvID{prjA1: envs}
}

func (f rulesFixture) create(t *testing.T, by domain.PrincipalID, spec service.RuleSpec) string {
	t.Helper()
	view, err := f.rules.Create(t.Context(), service.LocalPrincipal(by), spec)
	if err != nil {
		t.Fatalf("create rule %+v: %v", spec, err)
	}
	return view.ID
}

func whereIn(envMode domain.AxisMode, envs map[domain.ProjectID][]domain.EnvID, keyMode domain.AxisMode, keys ...domain.RuleKeyItem) domain.Where {
	w := domain.Where{Projects: []domain.ProjectID{prjA1}, EnvMode: envMode, Envs: envs, KeyMode: keyMode}
	if len(keys) > 0 {
		w.Keys = map[domain.ProjectID][]domain.RuleKeyItem{prjA1: keys}
	}
	return w
}

func folderItem(path string) domain.RuleKeyItem {
	return domain.RuleKeyItem{Folder: path, IsFolder: true}
}

func TestMemberAccessRules(t *testing.T) {
	forEngines(t, runMemberAccessRules)
}

func runMemberAccessRules(t *testing.T, db *store.DB) {
	f := seedRulesFixture(t, db)
	dev := scopeEnv(orgA, prjA1, envA1)
	prod := scopeEnv(orgA, prjA1, envProd)

	// carol: See on dev only, Reveal on dev only for the db/ folder only.
	envMode, envs := onlyEnvs(envA1)
	f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: domain.CapRead, Org: orgA,
		Where: whereIn(envMode, envs, domain.AxisAll)})
	revealRule := f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: domain.CapReveal, Org: orgA,
		Where: whereIn(envMode, envs, domain.AxisOnly, folderItem("db"))})

	t.Run("key_narrowed_reveal", func(t *testing.T) {
		cell, err := f.values.Get(t.Context(), service.LocalPrincipal(carol), dev, "DB_PASSWORD", true)
		if err != nil {
			t.Fatalf("covered single-cell reveal: %v", err)
		}
		if !cell.Revealed || cell.Value != "plain-DB_PASSWORD" {
			t.Fatalf("covered reveal returned %+v", cell)
		}
		_, other := f.values.Get(t.Context(), service.LocalPrincipal(carol), dev, "STRIPE_KEY", true)
		_, missing := f.values.Get(t.Context(), service.LocalPrincipal(carol), dev, "NO_SUCH_KEY", true)
		assertUniformNotFound(t, other, missing)
		if _, err := f.values.Get(t.Context(), service.LocalPrincipal(carol), prod, "DB_PASSWORD", true); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("reveal outside the rule's environments = %v, want not found", err)
		}
	})

	t.Run("operations_naming_no_key_are_out_of_reach", func(t *testing.T) {
		// Bulk reveal: the custodian control proves the call itself works.
		if _, err := f.values.List(t.Context(), service.LocalPrincipal(custodian), dev, true); err != nil {
			t.Fatalf("control bulk reveal: %v", err)
		}
		if _, err := f.values.List(t.Context(), service.LocalPrincipal(carol), dev, true); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("bulk reveal via a key-narrowed rule = %v, want not found", err)
		}
		// Values export with reveal.
		revisions := &service.Revisions{DB: db, Keyring: probeKeyring(t, db)}
		if _, _, err := revisions.ExportWithParameters(t.Context(), service.LocalPrincipal(custodian), dev, 0, true, nil); err != nil {
			t.Fatalf("control export: %v", err)
		}
		if _, _, err := revisions.ExportWithParameters(t.Context(), service.LocalPrincipal(carol), dev, 0, true, nil); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("revealing export via a key-narrowed rule = %v, want not found", err)
		}
		// Delivery projects reveal from grants only: carol's rule-held See
		// admits the fetch, but no secret plaintext is delivered.
		del := &service.Delivery{DB: db, Keyring: probeKeyring(t, db)}
		res, err := del.FetchAs(t.Context(), service.LocalPrincipal(carol), dev, "", service.FetchOptions{})
		if err == nil {
			for _, k := range res.Keys {
				if k.Name == "DB_PASSWORD" && k.Value != nil {
					t.Fatal("delivery disclosed a secret through a key-narrowed rule")
				}
			}
		} else if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("delivery = %v", err)
		}
		// Definitions: a key-narrowed rule never satisfies a whole-project op.
		defs := definitionsService(t, db)
		if _, err := defs.Export(t.Context(), service.LocalPrincipal(custodian), scopeProject(orgA, prjA1), false); err != nil {
			t.Fatalf("control definitions export: %v", err)
		}
		f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: domain.CapDefinitionsEdit, Org: orgA,
			Where: whereIn(domain.AxisAll, nil, domain.AxisOnly, folderItem("db"))})
		if _, err := defs.Apply(t.Context(), service.LocalPrincipal(carol), scopeProject(orgA, prjA1), "dpl_none", service.ApplyOptions{}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("definitions apply via a folder-narrowed rule = %v, want not found", err)
		}
	})

	t.Run("excepts_are_rule_local", func(t *testing.T) {
		// dave: rule A (edit and See on every environment except prod) and rule
		// B (See on prod only). B admits See on prod; A's except does not
		// block it, and nothing admits edit on prod.
		all := domain.AxisAll
		except := map[domain.ProjectID][]domain.EnvID{prjA1: {envProd}}
		f.create(t, orgAdmin, service.RuleSpec{Target: dave, Capability: domain.CapEdit, Org: orgA, Where: whereIn(all, except, all)})
		f.create(t, orgAdmin, service.RuleSpec{Target: dave, Capability: domain.CapRead, Org: orgA, Where: whereIn(all, except, all)})
		mode, only := onlyEnvs(envProd)
		f.create(t, orgAdmin, service.RuleSpec{Target: dave, Capability: domain.CapRead, Org: orgA, Where: whereIn(mode, only, all)})

		if _, err := f.values.Get(t.Context(), service.LocalPrincipal(dave), prod, "SHARED_KEY", false); err != nil {
			t.Fatalf("See on prod through rule B: %v", err)
		}
		if _, err := f.values.Set(t.Context(), service.LocalPrincipal(dave), dev, "SHARED_KEY", "draft", nil); err != nil {
			t.Fatalf("edit on dev through rule A (control): %v", err)
		}
		if _, err := f.values.Set(t.Context(), service.LocalPrincipal(dave), prod, "SHARED_KEY", "draft", nil); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("edit on prod = %v, want not found", err)
		}
	})

	t.Run("machine_rule_refused", func(t *testing.T) {
		_, err := f.rules.Create(t.Context(), service.LocalPrincipal(orgAdmin), service.RuleSpec{
			Target: mchA1, Capability: domain.CapRead, Org: orgA, Where: whereIn(domain.AxisAll, nil, domain.AxisAll)})
		if !errors.Is(err, service.ErrRuleMachine) {
			t.Fatalf("machine rule = %v, want ErrRuleMachine", err)
		}
	})

	t.Run("shape_refusals", func(t *testing.T) {
		for name, spec := range map[string]service.RuleSpec{
			"read narrowed by keys": {Target: gina, Capability: domain.CapRead, Org: orgA, Where: whereIn(domain.AxisAll, nil, domain.AxisOnly, folderItem("db"))},
			"pin narrowed by keys":  {Target: gina, Capability: domain.CapPin, Org: orgA, Where: whereIn(domain.AxisAll, nil, domain.AxisOnly, folderItem("db"))},
			"settings on some envs": {Target: gina, Capability: domain.CapProjectSettings, Org: orgA, Where: whereIn(domain.AxisOnly, map[domain.ProjectID][]domain.EnvID{prjA1: {envA1}}, domain.AxisAll)},
			"manage-projects":       {Target: gina, Capability: domain.CapManageProjects, Org: orgA, Where: whereIn(domain.AxisAll, nil, domain.AxisAll)},
			"empty only list":       {Target: gina, Capability: domain.CapEdit, Org: orgA, Where: whereIn(domain.AxisOnly, nil, domain.AxisAll)},
			"foreign env":           {Target: gina, Capability: domain.CapEdit, Org: orgA, Where: whereIn(domain.AxisOnly, map[domain.ProjectID][]domain.EnvID{prjA1: {envB1}}, domain.AxisAll)},
		} {
			if _, err := f.rules.Create(t.Context(), service.LocalPrincipal(orgAdmin), spec); !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("%s: %v, want invalid", name, err)
			}
		}
	})

	t.Run("rule_based_manage_members_grants_nothing", func(t *testing.T) {
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapManageMembers, Org: orgA,
			Where: whereIn(domain.AxisAll, nil, domain.AxisAll)})
		f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: domain.CapRead, Org: orgA,
			Where: whereIn(domain.AxisAll, nil, domain.AxisAll)})
		// Not even inside its own where, not even an atom frank holds.
		_, err := f.rules.Create(t.Context(), service.LocalPrincipal(frank), service.RuleSpec{
			Target: gina, Capability: domain.CapRead, Org: orgA, Where: whereIn(domain.AxisAll, nil, domain.AxisAll)})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("rule create by a rule-based member manager = %v, want not found", err)
		}
		grants := &service.Grants{DB: db}
		if _, err := grants.Create(t.Context(), service.LocalPrincipal(frank), service.GrantSpec{
			Target: gina, Capability: domain.CapRead, Scope: scopeProject(orgA, prjA1)}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("grant create by a rule-based member manager = %v, want not found", err)
		}
		// A project-scope member manager (legacy) may not grant atoms it lacks.
		if _, err := f.rules.Create(t.Context(), service.LocalPrincipal(prjAdmin), service.RuleSpec{
			Target: gina, Capability: domain.CapReveal, Org: orgA, Where: whereIn(domain.AxisAll, nil, domain.AxisAll)}); !errors.Is(err, service.ErrGrantorLacksCapability) {
			t.Fatalf("unheld rule by a project manager = %v, want ErrGrantorLacksCapability", err)
		}
		// Nor outside its project.
		if _, err := f.rules.Create(t.Context(), service.LocalPrincipal(prjAdmin), service.RuleSpec{
			Target: gina, Capability: domain.CapRead, Org: orgA, Where: domain.Where{Projects: []domain.ProjectID{prjA2}, EnvMode: domain.AxisAll, KeyMode: domain.AxisAll}}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("rule outside the manager's project = %v, want not found", err)
		}
		// Lockout census: rules are invisible to it.
		err = tx.Read(t.Context(), db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			holders, err := az.ManageMembersHolders(ctx, string(orgA))
			for _, h := range holders {
				if h == frank {
					t.Fatal("a rule-based member manager counted in the lockout census")
				}
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("folder_move_needs_confirmation", func(t *testing.T) {
		// erin: edit everywhere except the stripe/ folder. Moving STRIPE_KEY
		// from stripe/ to db/ admits erin (her except no longer covers it)
		// and carol (her only-db/ reveal now does).
		f.create(t, orgAdmin, service.RuleSpec{Target: erin, Capability: domain.CapEdit, Org: orgA,
			Where: whereIn(domain.AxisAll, nil, domain.AxisAll, folderItem("stripe"))})
		to := "db"
		move := func(confirm ...domain.PrincipalID) error {
			_, err := f.keys.UpdateMetadata(t.Context(), service.LocalPrincipal(custodian), scopeProject(orgA, prjA1), f.strKeyID,
				service.KeyMetadataUpdate{FolderPath: &to, ConfirmWidening: confirm}, nil)
			return err
		}
		var widening *service.MoveWideningError
		// The custodian administers keys, not members: it learns how many
		// people gain, never who.
		if err := move(); !errors.As(err, &widening) || !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("unconfirmed widening move = %v, want MoveWideningError", err)
		}
		if widening.Count != 2 || widening.Principals != nil || strings.Contains(widening.SafeDetail(), string(carol)) {
			t.Fatalf("a non-member-manager was told %+v (%q), want only the count 2", widening, widening.SafeDetail())
		}
		// Resending the exact set learned elsewhere does not let a
		// non-member-manager confirm: only the count comes back.
		if err := move(carol, erin); !errors.As(err, &widening) || widening.Principals != nil {
			t.Fatalf("a non-member-manager confirmed a widening move: %v", err)
		}
		if _, err := f.values.Get(t.Context(), service.LocalPrincipal(carol), dev, "STRIPE_KEY", true); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("a non-member-manager's confirmation still widened: %v", err)
		}
		// With manage-members on the project the same refusal names them.
		grantCustodianMemberManagement(t, db)
		if err := move(); !errors.As(err, &widening) || !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("unconfirmed widening move = %v, want MoveWideningError", err)
		}
		if widening.Count != 2 || len(widening.Principals) != 2 || widening.Principals[0] != carol || widening.Principals[1] != erin {
			t.Fatalf("widening names %v, want [carol erin]", widening.Principals)
		}
		if err := move(carol); !errors.As(err, &widening) {
			t.Fatalf("partial confirmation = %v, want refusal", err)
		}
		if _, err := f.values.Get(t.Context(), service.LocalPrincipal(carol), dev, "STRIPE_KEY", true); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("a refused move still widened: %v", err)
		}
		if err := move(erin, carol); err != nil {
			t.Fatalf("confirmed move: %v", err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.move_widening_confirmed'"); n != 1 {
			t.Fatalf("confirmation audited %d times, want 1", n)
		}
		if _, err := f.values.Get(t.Context(), service.LocalPrincipal(carol), dev, "STRIPE_KEY", true); err != nil {
			t.Fatalf("reveal after the confirmed move: %v", err)
		}
		// Moving it back only narrows: no confirmation needed.
		back := "stripe"
		if _, err := f.keys.UpdateMetadata(t.Context(), service.LocalPrincipal(custodian), scopeProject(orgA, prjA1), f.strKeyID,
			service.KeyMetadataUpdate{FolderPath: &back}, nil); err != nil {
			t.Fatalf("narrowing move: %v", err)
		}
		revokeCustodianMemberManagement(t, db)
	})

	t.Run("census_names_subfolder_excepts_and_restricted_people", func(t *testing.T) {
		// hank: edit everywhere except the vault/ folder, which also excepts
		// vault/sub. Moving a key out of vault/sub admits him, and the census
		// names him even while his account is restricted: the restriction can
		// be lifted later, and nobody would have confirmed that access.
		project := scopeProject(orgA, prjA1)
		sub, err := f.keys.Create(t.Context(), service.LocalPrincipal(custodian), project, service.KeySpec{
			Name: "SUB_KEY", FolderPath: "vault/sub", Classification: string(schema.Secret),
			Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules(),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		rule := f.create(t, orgAdmin, service.RuleSpec{Target: hank, Capability: domain.CapEdit, Org: orgA,
			Where: whereIn(domain.AxisAll, nil, domain.AxisAll, folderItem("vault"))})
		execRaw(t, db, "UPDATE principals SET privacy_state = 'restricted' WHERE id = '"+string(hank)+"'")
		to := "misc"
		var widening *service.MoveWideningError
		grantCustodianMemberManagement(t, db)
		defer revokeCustodianMemberManagement(t, db)
		_, err = f.keys.UpdateMetadata(t.Context(), service.LocalPrincipal(custodian), project, sub.ID,
			service.KeyMetadataUpdate{FolderPath: &to}, nil)
		if !errors.As(err, &widening) {
			t.Fatalf("moving out of an excepted subfolder = %v, want MoveWideningError", err)
		}
		if len(widening.Principals) != 1 || widening.Principals[0] != hank {
			t.Fatalf("widening names %v, want [hank]", widening.Principals)
		}
		execRaw(t, db, "UPDATE principals SET privacy_state = 'active' WHERE id = '"+string(hank)+"'")
		if err := f.rules.Revoke(t.Context(), service.LocalPrincipal(orgAdmin), orgA, rule); err != nil {
			t.Fatal(err)
		}
		if err := f.keys.Delete(t.Context(), service.LocalPrincipal(custodian), project, sub.ID); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("rule_decided_key_writes_conceal_outside_objects", func(t *testing.T) {
		// ivan defines keys in the db/ folder only. A name clash with a key
		// in stripe/ must not tell him that key exists; the custodian, who
		// reads the whole catalogue, gets the precise conflict.
		project := scopeProject(orgA, prjA1)
		rule := f.create(t, orgAdmin, service.RuleSpec{Target: ivan, Capability: domain.CapDefinitionsEdit, Org: orgA,
			Where: whereIn(domain.AxisAll, nil, domain.AxisOnly, folderItem("db"))})
		spec := func(name string) service.KeySpec {
			return service.KeySpec{Name: name, FolderPath: "db", Classification: string(schema.Secret),
				Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules()}
		}
		if _, err := f.keys.Create(t.Context(), service.LocalPrincipal(custodian), project, spec("STRIPE_KEY"), nil); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("custodian clash = %v, want conflict (positive control)", err)
		}
		_, clash := f.keys.Create(t.Context(), service.LocalPrincipal(ivan), project, spec("STRIPE_KEY"), nil)
		_, outside := f.keys.Create(t.Context(), service.LocalPrincipal(ivan), project, service.KeySpec{Name: "NEW_KEY", FolderPath: "stripe",
			Classification: string(schema.Secret), Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules()}, nil)
		assertUniformNotFound(t, clash, outside)
		_, missingGroup := f.keys.Create(t.Context(), service.LocalPrincipal(ivan), project, func() service.KeySpec {
			k := spec("GROUPED_KEY")
			k.GroupID = "kgr_elsewhere"
			return k
		}(), nil)
		assertUniformNotFound(t, missingGroup, outside)
		if _, err := f.keys.Rename(t.Context(), service.LocalPrincipal(custodian), project, f.dbKeyID, "STRIPE_KEY", nil); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("custodian rename clash = %v, want conflict (positive control)", err)
		}
		_, renameClash := f.keys.Rename(t.Context(), service.LocalPrincipal(ivan), project, f.dbKeyID, "STRIPE_KEY", nil)
		_, renameOutside := f.keys.Rename(t.Context(), service.LocalPrincipal(ivan), project, f.strKeyID, "RENAMED", nil)
		assertUniformNotFound(t, renameClash, renameOutside)
		if err := f.rules.Revoke(t.Context(), service.LocalPrincipal(orgAdmin), orgA, rule); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("definitions_apply_move_needs_confirmation", func(t *testing.T) {
		df := seedDefinitionsProject(t, db, "rulemove", true)
		svc := definitionsService(t, db)
		f.create(t, orgAdmin, service.RuleSpec{Target: grantee, Capability: domain.CapEdit, Org: orgA,
			Where: domain.Where{Projects: []domain.ProjectID{df.project}, EnvMode: domain.AxisAll, KeyMode: domain.AxisOnly,
				Keys: map[domain.ProjectID][]domain.RuleKeyItem{df.project: {folderItem("moved")}}}})
		bundle := parseDefinitions(t, exportDefinitions(t, svc, df))
		for i := range bundle.Keys {
			if bundle.Keys[i].Name == "BASE_KEY" {
				bundle.Keys[i].FolderPath = "moved"
			}
		}
		plan := planDefinitions(t, svc, df, encodeDefinitions(t, bundle))
		var widening *service.MoveWideningError
		if _, err := svc.Apply(t.Context(), service.LocalPrincipal(alice), df.scope(), plan.ID, service.ApplyOptions{}); !errors.As(err, &widening) {
			t.Fatalf("unconfirmed widening apply = %v, want MoveWideningError", err)
		}
		if widening.Count != 1 || widening.Principals != nil {
			t.Fatalf("apply refusal told a non-member-manager %+v, want only the count 1", widening)
		}
		before := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.move_widening_confirmed'")
		// alice was told only the count: the exact set, learned elsewhere,
		// does not let her confirm.
		if _, err := svc.Apply(t.Context(), service.LocalPrincipal(alice), df.scope(), plan.ID,
			service.ApplyOptions{ConfirmWidening: []domain.PrincipalID{grantee}}); !errors.As(err, &widening) || widening.Principals != nil {
			t.Fatalf("a non-member-manager confirmed a widening apply: %v", err)
		}
		// With legacy manage-members on the project she may confirm.
		execRaw(t, db, fmt.Sprintf("INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_alice_mm_move', '%s', 'manage-members', 'org_a', '%s', NULL, %s)", alice, df.project, ts))
		seedOrigins(t, db)
		if _, err := svc.Apply(t.Context(), service.LocalPrincipal(alice), df.scope(), plan.ID,
			service.ApplyOptions{ConfirmWidening: []domain.PrincipalID{grantee}}); err != nil {
			t.Fatalf("confirmed apply by a member manager: %v", err)
		}
		if after := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.move_widening_confirmed'"); after != before+1 {
			t.Fatalf("apply confirmation audited %d times, want 1", after-before)
		}
	})

	t.Run("revoke_takes_effect", func(t *testing.T) {
		if err := f.rules.Revoke(t.Context(), service.LocalPrincipal(orgAdmin), orgA, revealRule); err != nil {
			t.Fatal(err)
		}
		if _, err := f.values.Get(t.Context(), service.LocalPrincipal(carol), dev, "DB_PASSWORD", true); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("reveal after revoke = %v, want not found", err)
		}
		if err := f.rules.Revoke(t.Context(), service.LocalPrincipal(orgAdmin), orgA, revealRule); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("second revoke = %v, want not found", err)
		}
	})

	t.Run("revoke_missing_and_unreachable_are_identical", func(t *testing.T) {
		// A rule the caller cannot reach and a rule that does not exist (or
		// lives in another org) answer with the same error and the same
		// captured denial.
		live := f.create(t, orgAdmin, service.RuleSpec{Target: dave, Capability: domain.CapEdit, Org: orgA,
			Where: whereIn(domain.AxisAll, nil, domain.AxisAll)})
		denials := func() int64 {
			return queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'grant.denied'") +
				queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'grant.denied'")
		}
		before := denials()
		unreachable := f.rules.Revoke(t.Context(), service.LocalPrincipal(reader), orgA, live)
		afterUnreachable := denials()
		missing := f.rules.Revoke(t.Context(), service.LocalPrincipal(reader), orgA, "rul_nonexistent")
		afterMissing := denials()
		otherOrg := f.rules.Revoke(t.Context(), service.LocalPrincipal(orgAdmin), orgB, live)
		afterOtherOrg := denials()
		assertUniformNotFound(t, unreachable, missing)
		assertUniformNotFound(t, unreachable, otherOrg)
		if afterUnreachable-before != 1 || afterMissing-afterUnreachable != 1 || afterOtherOrg-afterMissing != 1 {
			t.Fatalf("denials captured: unreachable %d, missing %d, other org %d; want one each",
				afterUnreachable-before, afterMissing-afterUnreachable, afterOtherOrg-afterMissing)
		}
		if err := f.rules.Revoke(t.Context(), service.LocalPrincipal(orgAdmin), orgA, live); err != nil {
			t.Fatalf("positive control: %v", err)
		}
	})

	t.Run("deletion_leaves_nothing_to_match", func(t *testing.T) {
		project := scopeProject(orgA, prjA1)
		// A throwaway key named by id in an only-list.
		tmp, err := f.keys.Create(t.Context(), service.LocalPrincipal(custodian), project, service.KeySpec{
			Name: "TMP_KEY", FolderPath: "tmp", Classification: string(schema.Secret),
			Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules(),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		onlyKey := f.create(t, orgAdmin, service.RuleSpec{Target: gina, Capability: domain.CapEdit, Org: orgA,
			Where: whereIn(domain.AxisAll, nil, domain.AxisOnly, domain.RuleKeyItem{KeyID: tmp.ID})})
		exceptKey := f.create(t, orgAdmin, service.RuleSpec{Target: gina, Capability: domain.CapRevealHistory, Org: orgA,
			Where: whereIn(domain.AxisAll, nil, domain.AxisAll, domain.RuleKeyItem{KeyID: tmp.ID}, folderItem("db"))})
		if err := f.keys.Delete(t.Context(), service.LocalPrincipal(custodian), project, tmp.ID); err != nil {
			t.Fatalf("delete key: %v", err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM rules WHERE id = '"+onlyKey+"'"); n != 0 {
			t.Fatal("a rule whose only key was deleted survived")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM rules WHERE id = '"+exceptKey+"'"); n != 1 {
			t.Fatal("a rule that merely excepted the deleted key was removed")
		}
		// A throwaway environment in an only-list and an except-list.
		_, _, envs := services(t, db)
		env, err := envs.Create(t.Context(), service.LocalPrincipal(alice), project, "tmp", nil)
		if err != nil {
			t.Fatal(err)
		}
		envID := domain.EnvID(env.ID)
		mode, only := onlyEnvs(envID)
		onlyEnv := f.create(t, orgAdmin, service.RuleSpec{Target: gina, Capability: domain.CapRead, Org: orgA, Where: whereIn(mode, only, domain.AxisAll)})
		exceptEnv := f.create(t, orgAdmin, service.RuleSpec{Target: gina, Capability: domain.CapReveal, Org: orgA,
			Where: whereIn(domain.AxisAll, map[domain.ProjectID][]domain.EnvID{prjA1: {envID}}, domain.AxisAll)})
		if err := envs.Delete(t.Context(), service.LocalPrincipal(alice), scopeEnv(orgA, prjA1, envID)); err != nil {
			t.Fatalf("delete env: %v", err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM rules WHERE id = '"+onlyEnv+"'"); n != 0 {
			t.Fatal("a rule whose only environment was deleted survived")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM rules WHERE id = '"+exceptEnv+"'"); n != 1 {
			t.Fatal("a rule that merely excepted the deleted environment was removed")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.revoked'"); n < 3 {
			t.Fatalf("rule.revoked recorded %d times, want the revoke and both emptied rules", n)
		}
		// No item references anything that no longer exists.
		if n := queryInt(t, db, `SELECT COUNT(*) FROM rule_items i WHERE
			(i.env_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM environments e WHERE e.id = i.env_id)) OR
			(i.key_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM keys k WHERE k.id = i.key_id))`); n != 0 {
			t.Fatalf("%d rule items reference deleted objects", n)
		}
		// Leave gina rule-less for the query-count checks below.
		for _, id := range []string{exceptKey, exceptEnv} {
			if err := f.rules.Revoke(t.Context(), service.LocalPrincipal(orgAdmin), orgA, id); err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("query_counts", func(t *testing.T) {
		// A grant-less principal authorized by a rule costs one read more than
		// a grant-authorized one: chain + grants + rules.
		if n, err := countedAuthorize(t, db, carol, dev); err != nil || n != 3 {
			t.Fatalf("rule-authorized env read: %d queries, err %v; want 3", n, err)
		}
		// A key-aware denial costs chain + grants + rules + key, whether the
		// key exists or not.
		for _, key := range []string{"DB_PASSWORD", "NO_SUCH_KEY"} {
			n, err := countedAuthorizeKey(t, db, gina, dev, authz.KeyByName(key))
			if !errors.Is(err, domain.ErrNotFound) || n != 4 {
				t.Fatalf("key-aware denial on %s: %d queries, err %v; want 4 and not found", key, n, err)
			}
		}
	})
}

// countedAuthorizeKey is countedAuthorize for the key-aware entry point, on
// value.reveal.
func countedAuthorizeKey(t *testing.T, db *store.DB, principal domain.PrincipalID, scope domain.Scope, key authz.KeyTarget) (int, error) {
	t.Helper()
	ctx := t.Context()
	count := 0
	tok := authz.NewTxToken()
	defer tok.Invalidate()
	var r *authn.Resolver
	if db.Engine() == store.EnginePostgres {
		pgtx, err := db.PG().BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = pgtx.Rollback(ctx) }()
		var dbtx pggen.DBTX = countingPGTx{tx: pgtx, n: &count}
		r = authn.NewPG(dbtx)
	} else {
		sqtx, err := db.SQLiteRead().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sqtx.Rollback() }()
		var dbtx sqlitegen.DBTX = countingSqliteTx{tx: sqtx, n: &count}
		r = authn.NewSQLite(dbtx)
	}
	_, err := authz.NewTxAuthorizer(r, tok).AuthorizeKey(ctx, authz.Identity{Principal: principal}, authz.OpValueReveal, scope, key)
	return count, err
}

// runRuleAuditLifecycle drives every rule audit event once for the
// every-registered-type-is-emitted invariant.
func runRuleAuditLifecycle(t *testing.T, db *store.DB) {
	t.Helper()
	rules := &service.Rules{DB: db}
	keys := keySvc(t, db)
	project := scopeProject(orgA, prjA2)
	key, err := keys.Create(t.Context(), service.LocalPrincipal(alice), project, service.KeySpec{
		Name: "RULE_AUDIT_KEY", FolderPath: "fenced", Classification: string(schema.Config),
		Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	created, err := rules.Create(t.Context(), service.LocalPrincipal(orgAdmin), service.RuleSpec{
		Target: grantee, Capability: domain.CapEdit, Org: orgA,
		Where: domain.Where{Projects: []domain.ProjectID{prjA2}, EnvMode: domain.AxisAll, KeyMode: domain.AxisAll,
			Keys: map[domain.ProjectID][]domain.RuleKeyItem{prjA2: {folderItem("fenced")}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	open := "open"
	if _, err := keys.UpdateMetadata(t.Context(), service.LocalPrincipal(alice), project, key.ID,
		service.KeyMetadataUpdate{FolderPath: &open, ConfirmWidening: []domain.PrincipalID{grantee}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := rules.Revoke(t.Context(), service.LocalPrincipal(orgAdmin), orgA, created.ID); err != nil {
		t.Fatal(err)
	}
}

// grantCustodianMemberManagement gives the custodian legacy manage-members on
// prj_a1 so a widening refusal may name the people who gain.
func grantCustodianMemberManagement(t *testing.T, db *store.DB) {
	t.Helper()
	execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_cu_mm_tmp', 'usr_custodian', 'manage-members', 'org_a', 'prj_a1', NULL, "+ts+")")
	seedOrigins(t, db)
}

func revokeCustodianMemberManagement(t *testing.T, db *store.DB) {
	t.Helper()
	execRaw(t, db, "DELETE FROM grant_origins WHERE grant_id = 'g_cu_mm_tmp'")
	execRaw(t, db, "DELETE FROM grants WHERE id = 'g_cu_mm_tmp'")
}
