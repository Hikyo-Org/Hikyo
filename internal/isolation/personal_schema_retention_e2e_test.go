package isolation

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Personal values: schema rules, retention and notices (#1235,
// personal-values ADR). The owner is the ceremony fixture's human.

// personalApply exports prj_a1's bundle, lets edit change it, then plans and
// applies it as the custodian, who holds publish on every environment.
func personalApply(t *testing.T, db *store.DB, e personalEnv, edit func(*definitions.Bundle)) {
	t.Helper()
	svc := &service.Definitions{DB: db, Keyring: e.values.Keyring, Advisory: service.NewAdvisory()}
	actor := service.LocalPrincipal(custodian)
	raw, err := svc.Export(t.Context(), actor, scopeProject(orgA, prjA1), false)
	if err != nil {
		t.Fatal(err)
	}
	bundle := parseDefinitions(t, raw)
	edit(&bundle)
	plan, err := svc.Plan(t.Context(), actor, scopeProject(orgA, prjA1), encodeDefinitions(t, bundle), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(t.Context(), actor, scopeProject(orgA, prjA1), plan.ID, service.ApplyOptions{AllowDelete: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
}

func TestPersonalValueProjectCap(t *testing.T) {
	saved := service.MaxPersonalValuesPerProject
	service.MaxPersonalValuesPerProject = 1
	t.Cleanup(func() { service.MaxPersonalValuesPerProject = saved })
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-cap")
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		// Replacing an existing value never counts against the cap.
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain+"-again")
		e.adminTok = personalReauth(t, e.admin.auth, e.adminTok, e.adminDev, e.keyA)
		if _, err := e.pv.Set(t.Context(), service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA, "second-owner"); !errors.Is(err, domain.ErrLimitExceeded) {
			t.Fatalf("set past the project cap = %v", err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 1 {
			t.Fatal("a refused set stored a value")
		}
	})
}

func TestPersonalValueApplyKeyDeleteAndRenameCascade(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-apply-keys")
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		e.setOwn(t, ceremonySecretB, "key_"+ceremonySecretB, personalPlain)
		// Deleting a key through apply needs it free of shared values.
		execRaw(t, db, `DELETE FROM value_entries WHERE key_id='key_`+ceremonySecretB+`'`)
		personalApply(t, db, e, func(b *definitions.Bundle) {
			kept := b.Keys[:0]
			for _, k := range b.Keys {
				switch k.Name {
				case ceremonySecretA:
					k.Name = "CEREMONY_SECRET_APPLIED"
				case ceremonySecretB:
					continue
				}
				kept = append(kept, k)
			}
			b.Keys = kept
		})
		if n := queryInt(t, db, `SELECT COUNT(*) FROM personal_values`); n != 0 {
			t.Fatalf("apply cascade left %d personal values", n)
		}
		for name, cause := range map[string]string{ceremonySecretA: service.PersonalCauseKeyRenamed, ceremonySecretB: service.PersonalCauseKeyDeleted} {
			if queryInt(t, db, `SELECT COUNT(*) FROM personal_value_notices WHERE key_name='`+name+`' AND cause='`+cause+`'`) != 1 {
				t.Fatalf("no %s notice for %s", cause, name)
			}
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='personal_value.deleted'`); n != 2 {
			t.Fatalf("apply cascade audited %d deletions, want 2", n)
		}
	})
}

func TestPersonalValueApplyEnvironmentDeleteAuditsPerOwner(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-apply-env")
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		e.setOwn(t, ceremonySecretB, "key_"+ceremonySecretB, personalPlain)
		e.adminTok = personalReauth(t, e.admin.auth, e.adminTok, e.adminDev, e.keyA)
		if _, err := e.pv.Set(t.Context(), service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA, "second-owner"); err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, `DELETE FROM value_entries WHERE environment_id='env_a1'`)
		personalApply(t, db, e, func(b *definitions.Bundle) {
			kept := b.Environments[:0]
			for _, env := range b.Environments {
				if env.ID != string(envA1) {
					kept = append(kept, env)
				}
			}
			b.Environments = kept
		})
		if n := queryInt(t, db, `SELECT COUNT(*) FROM personal_values`); n != 0 {
			t.Fatalf("environment apply left %d personal values", n)
		}
		// One event per owner, each carrying that owner's count.
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='personal_value.deleted'`); n != 2 {
			t.Fatalf("environment apply audited %d owners, want 2", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='personal_value.deleted' AND payload LIKE '%"count":2%'`); n != 1 {
			t.Fatalf("owner with two values audited %d times with count 2", n)
		}
	})
}

func personalRequiredHere() schema.Presence {
	return schema.Presence{Mode: schema.PresenceExplicit, Environments: []string{string(envA1)}}
}

func personalRequiredRows(t *testing.T, db *store.DB) int64 {
	t.Helper()
	return queryInt(t, db, `SELECT COUNT(*) FROM key_personal_required_environments`)
}

func TestPersonalRequiredInRulesAndAuthority(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		keys := &service.Keys{DB: db, Keyring: probeKeyring(t, db)}
		project := scopeProject(orgA, prjA1)
		spec := func(name string, personal schema.Presence, presence schema.PresenceRules) service.KeySpec {
			return service.KeySpec{Name: name, Classification: string(schema.Secret),
				Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}},
				Presence:    presence, PersonalRequired: personal}
		}
		// alice holds definitions-edit and no publish.
		k, err := keys.Create(ctx, service.LocalPrincipal(alice), project, spec("PERSONAL_REQUIRED", personalRequiredHere(), schema.DefaultPresenceRules()), nil)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := keys.Get(ctx, service.LocalPrincipal(alice), project, k.ID); err != nil || got.PersonalRequired.Mode != schema.PresenceExplicit || len(got.PersonalRequired.Environments) != 1 {
			t.Fatalf("get after create: err=%v personal=%v", err, got.PersonalRequired)
		}
		overlap := schema.DefaultPresenceRules()
		overlap.Required = personalRequiredHere()
		forbidden := schema.DefaultPresenceRules()
		forbidden.Forbidden = personalRequiredHere()
		for name, s := range map[string]service.KeySpec{
			"all":       spec("PERSONAL_ALL", schema.Presence{Mode: schema.PresenceAll}, schema.DefaultPresenceRules()),
			"required":  spec("PERSONAL_OVERLAP_REQUIRED", personalRequiredHere(), overlap),
			"forbidden": spec("PERSONAL_OVERLAP_FORBIDDEN", personalRequiredHere(), forbidden),
		} {
			if _, err := keys.Create(ctx, service.LocalPrincipal(alice), project, s, nil); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("%s personal_required_in = %v, want invalid", name, err)
			}
		}

		// A change to personal_required_in alone needs no publish.
		update := service.KeyDeclarationUpdate{Declaration: k.Declaration, Presence: k.Presence}
		if _, err := keys.UpdateDeclaration(ctx, service.LocalPrincipal(alice), project, k.ID, update, nil); err != nil {
			t.Fatalf("clear personal_required_in without publish: %v", err)
		}
		if personalRequiredRows(t, db) != 0 {
			t.Fatal("none left rows behind")
		}
		update.PersonalRequired = personalRequiredHere()
		if _, err := keys.UpdateDeclaration(ctx, service.LocalPrincipal(alice), project, k.ID, update, nil); err != nil {
			t.Fatalf("set personal_required_in without publish: %v", err)
		}
		update.PersonalRequired = schema.Presence{Mode: schema.PresenceAll}
		if _, err := keys.UpdateDeclaration(ctx, service.LocalPrincipal(alice), project, k.ID, update, nil); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("update to all = %v", err)
		}
		update.PersonalRequired = schema.Presence{Mode: schema.PresenceNone}
		if _, err := keys.UpdateDeclaration(ctx, service.LocalPrincipal(reader), project, k.ID, update, nil); err == nil {
			t.Fatal("a principal without definitions-edit changed personal_required_in")
		}
		if personalRequiredRows(t, db) != 1 {
			t.Fatal("refused updates changed the rows")
		}

		// A key with personal_required_in cannot join a group.
		group, err := (&service.KeyGroups{DB: db, Keyring: keys.Keyring}).Create(ctx, service.LocalPrincipal(custodian), project, "personal-group", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := keys.SetGroup(ctx, service.LocalPrincipal(custodian), project, k.ID, group.ID); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("grouping a personal-required key = %v", err)
		}

		if err := keys.Delete(ctx, service.LocalPrincipal(alice), project, k.ID); err != nil {
			t.Fatal(err)
		}
		if personalRequiredRows(t, db) != 0 {
			t.Fatal("key delete left personal_required_in rows")
		}
	})
}

func TestPersonalRequiredInDefinitionsRoundTripAndEnvironmentDelete(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-required-apply")
		var envName string
		personalApply(t, db, e, func(b *definitions.Bundle) {
			for _, env := range b.Environments {
				if env.ID == string(envA1) {
					envName = env.Name
				}
			}
			for i := range b.Keys {
				if b.Keys[i].Name == ceremonySecretA {
					b.Keys[i].PersonalRequiredIn = &definitions.Presence{Mode: string(schema.PresenceExplicit), Environments: []string{envName}}
				}
			}
		})
		if personalRequiredRows(t, db) != 1 {
			t.Fatal("apply did not store personal_required_in")
		}
		svc := &service.Definitions{DB: db, Keyring: e.values.Keyring, Advisory: service.NewAdvisory()}
		raw, err := svc.Export(t.Context(), service.LocalPrincipal(custodian), scopeProject(orgA, prjA1), false)
		if err != nil {
			t.Fatal(err)
		}
		exported := 0
		for _, k := range parseDefinitions(t, raw).Keys {
			if k.PersonalRequiredIn != nil {
				if k.Name != ceremonySecretA || k.PersonalRequiredIn.Mode != string(schema.PresenceExplicit) || len(k.PersonalRequiredIn.Environments) != 1 || k.PersonalRequiredIn.Environments[0] != envName {
					t.Fatalf("exported personal_required_in on %s = %+v", k.Name, *k.PersonalRequiredIn)
				}
				exported++
			}
		}
		if exported != 1 {
			t.Fatalf("export carried personal_required_in on %d keys", exported)
		}
		// `all` in a bundle is refused at plan time.
		bundle := parseDefinitions(t, raw)
		for i := range bundle.Keys {
			if bundle.Keys[i].Name == ceremonySecretA {
				bundle.Keys[i].PersonalRequiredIn = &definitions.Presence{Mode: string(schema.PresenceAll), Environments: []string{}}
			}
		}
		allRaw, err := json.Marshal(bundle)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Plan(t.Context(), service.LocalPrincipal(custodian), scopeProject(orgA, prjA1), allRaw, nil); !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "`personal_required_in` mode `all` is not allowed") {
			t.Fatalf("plan with personal_required_in all = %v", err)
		}

		execRaw(t, db, `DELETE FROM value_entries WHERE environment_id='env_a1'`)
		personalApply(t, db, e, func(b *definitions.Bundle) {
			kept := b.Environments[:0]
			for _, env := range b.Environments {
				if env.ID != string(envA1) {
					kept = append(kept, env)
				}
			}
			b.Environments = kept
			for i := range b.Keys {
				b.Keys[i].PersonalRequiredIn = nil
			}
		})
		if personalRequiredRows(t, db) != 0 {
			t.Fatal("environment delete left personal_required_in rows")
		}
	})
}

func TestPersonalRequiredInDeliversMissing(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-missing")
		ctx := t.Context()
		keys := &service.Keys{DB: db, Keyring: e.values.Keyring}
		project := scopeProject(orgA, prjA1)
		keyB := "key_" + ceremonySecretB
		k, err := keys.Get(ctx, service.LocalPrincipal(alice), project, keyB)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := keys.UpdateDeclaration(ctx, service.LocalPrincipal(alice), project, keyB, service.KeyDeclarationUpdate{
			Declaration: k.Declaration, Presence: k.Presence, PersonalRequired: personalRequiredHere(),
		}, nil); err != nil {
			t.Fatal(err)
		}
		// A shared publish never consults personal_required_in.
		publishValue(t, e.values, service.LocalPrincipal(custodian), envScope(envA1), ceremonySecretB, "plaintext-republished")

		own, err := e.pv.List(ctx, service.Bearer(e.admin.token), envScope(envA1))
		if err != nil || len(own) != 1 || own[0].Name != ceremonySecretB || !own[0].Required || own[0].Set {
			t.Fatalf("checklist row: err=%v entries=%+v", err, own)
		}

		del := deliverySvc(t, db)
		consented, _ := mintDeveloper(t, db, &e.ceremonyEnv, time.Hour, true)
		r, err := del.Fetch(ctx, consented.Value, envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if b := deliveredKey(t, r, ceremonySecretB); b.Value != nil || b.Personal != service.PersonalMissing || b.PersonalReason != "" {
			t.Fatalf("required key without a value: personal=%q reason=%q value=%t", b.Personal, b.PersonalReason, b.Value != nil)
		}
		if a := deliveredKey(t, r, ceremonySecretA); !valueIs(a, "plaintext-"+ceremonySecretA) || a.Personal != "" {
			t.Fatal("a key that is not personal-required changed")
		}
		// Without consent, and config-only, the shared value is delivered.
		unconsented, _ := mintDeveloper(t, db, &e.ceremonyEnv, time.Hour, false)
		r, err = del.Fetch(ctx, unconsented.Value, envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if b := deliveredKey(t, r, ceremonySecretB); b.Personal == service.PersonalMissing {
			t.Fatal("an unconsented fetch reported missing")
		}

		// A shared consumer still receives the shared value.
		r, err = del.FetchAs(ctx, service.LocalPrincipal(custodian), envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if b := deliveredKey(t, r, ceremonySecretB); !valueIs(b, "plaintext-republished") || b.Personal != "" {
			t.Fatalf("shared delivery of a personal-required key: personal=%q", b.Personal)
		}

		e.setOwn(t, ceremonySecretB, keyB, personalPlain)
		r, err = del.Fetch(ctx, consented.Value, envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if b := deliveredKey(t, r, ceremonySecretB); !valueIs(b, personalPlain) || b.Personal != service.PersonalDelivered {
			t.Fatalf("held required value: personal=%q", b.Personal)
		}
	})
}

func TestPersonalValueWarningsNeverRefuseAndPublishIgnoresThem(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-warnings")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)

		// A tightening the shared value satisfies and the personal value does
		// not: the publish succeeds and the personal value stays.
		keys := &service.Keys{DB: db, Keyring: e.values.Keyring}
		project := scopeProject(orgA, prjA1)
		k, err := keys.Get(ctx, service.LocalPrincipal(custodian), project, e.keyA)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := keys.UpdateDeclaration(ctx, service.LocalPrincipal(custodian), project, e.keyA, service.KeyDeclarationUpdate{
			Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString, Pattern: "plaintext-.*"}}, Presence: k.Presence,
		}, nil); err != nil {
			t.Fatalf("schema publish refused over a personal value: %v", err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 1 {
			t.Fatal("schema publish removed the personal value")
		}

		e.admin.token = personalReauth(t, e.admin.auth, e.admin.token, e.device, e.keyA)
		revealed, err := e.pv.Reveal(ctx, service.Bearer(e.admin.token), envScope(envA1), ceremonySecretA)
		if err != nil || len(revealed.Warnings) == 0 {
			t.Fatalf("reveal of a value breaking the rule: err=%v warnings=%v", err, revealed.Warnings)
		}
		e.admin.token = personalReauth(t, e.admin.auth, e.admin.token, e.device, e.keyA)
		broken, err := e.pv.Set(ctx, service.Bearer(e.admin.token), envScope(envA1), ceremonySecretA, personalPlain+"-still-broken")
		if err != nil || len(broken.Warnings) == 0 {
			t.Fatalf("set breaking the rule: err=%v warnings=%v", err, broken.Warnings)
		}
		e.admin.token = personalReauth(t, e.admin.auth, e.admin.token, e.device, e.keyA)
		ok, err := e.pv.Set(ctx, service.Bearer(e.admin.token), envScope(envA1), ceremonySecretA, "plaintext-conforming")
		if err != nil || len(ok.Warnings) != 0 {
			t.Fatalf("set satisfying the rule: err=%v warnings=%v", err, ok.Warnings)
		}
		// The two set events have one shape: nothing about the rule.
		shapes := queryInt(t, db, `SELECT COUNT(DISTINCT LENGTH(payload)) FROM audit_tenant_events WHERE type='personal_value.set' AND id IN (SELECT id FROM audit_tenant_events WHERE type='personal_value.set' ORDER BY recorded_at DESC LIMIT 2)`)
		if shapes != 1 {
			t.Fatal("a set breaking a rule audited differently")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='personal_value.set' AND (payload LIKE '%warn%' OR payload LIKE '%pattern%')`); n != 0 {
			t.Fatal("a personal set event names a schema rule")
		}
	})
}

func TestPersonalValueForbiddenCascade(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-forbidden")
		ctx := t.Context()
		keyB := "key_" + ceremonySecretB
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		e.setOwn(t, ceremonySecretB, keyB, personalPlain)
		execRaw(t, db, `DELETE FROM value_entries WHERE environment_id='env_a1'`)

		// Through the key declaration update.
		keys := &service.Keys{DB: db, Keyring: e.values.Keyring}
		project := scopeProject(orgA, prjA1)
		k, err := keys.Get(ctx, service.LocalPrincipal(custodian), project, e.keyA)
		if err != nil {
			t.Fatal(err)
		}
		forbid := k.Presence
		forbid.Forbidden = personalRequiredHere()
		if _, err := keys.UpdateDeclaration(ctx, service.LocalPrincipal(custodian), project, e.keyA, service.KeyDeclarationUpdate{Declaration: k.Declaration, Presence: forbid}, nil); err != nil {
			t.Fatal(err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values WHERE key_id='`+e.keyA+`'`) != 0 {
			t.Fatal("forbidding the key kept its personal value")
		}
		e.admin.token = personalReauth(t, e.admin.auth, e.admin.token, e.device, e.keyA)
		if _, err := e.pv.Set(ctx, service.Bearer(e.admin.token), envScope(envA1), ceremonySecretA, personalPlain); err == nil {
			t.Fatal("set on a forbidden key succeeded")
		}

		// Through definitions apply.
		personalApply(t, db, e, func(b *definitions.Bundle) {
			var envName string
			for _, env := range b.Environments {
				if env.ID == string(envA1) {
					envName = env.Name
				}
			}
			for i := range b.Keys {
				if b.Keys[i].Name == ceremonySecretB {
					b.Keys[i].ForbiddenIn = definitions.Presence{Mode: string(schema.PresenceExplicit), Environments: []string{envName}}
				}
			}
		})
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 0 {
			t.Fatal("forbidding through apply kept the personal value")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM personal_value_notices WHERE cause='`+service.PersonalCauseForbidden+`'`); n != 2 {
			t.Fatalf("forbidden cascade left %d notices, want 2", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='personal_value.deleted' AND payload LIKE '%"cause":"forbidden"%'`); n != 2 {
			t.Fatalf("forbidden cascade audited %d deletions, want 2", n)
		}
	})
}

func TestPersonalValueRetentionSweep(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-retention")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
		sweep := &service.PersonalValues{DB: db, Keyring: e.values.Keyring, Auth: e.admin.auth, Now: func() time.Time { return clock }}
		run := func() {
			t.Helper()
			if err := sweep.SweepRetention(ctx); err != nil {
				t.Fatal(err)
			}
		}
		suspended := func() bool {
			t.Helper()
			own, err := e.pv.List(ctx, service.Bearer(e.admin.token), envScope(envA1))
			if err != nil || len(own) != 1 {
				t.Fatalf("own list: err=%v entries=%d", err, len(own))
			}
			return own[0].Suspended
		}
		grant := "g_cer_personal-retention_reveal"
		revoke := func() {
			execRaw(t, db, `DELETE FROM grant_origins WHERE grant_id='`+grant+`'`)
			execRaw(t, db, `DELETE FROM grants WHERE id='`+grant+`'`)
		}
		regrant := func() {
			execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('`+grant+`', '`+string(e.admin.boot.PrincipalID)+`', 'reveal', 'org_a', NULL, NULL, `+ts+`)`)
			execRaw(t, db, `INSERT INTO grant_origins (id, grant_id, kind, subject, created_at) VALUES ('gor_`+grant+`', '`+grant+`', 'manual', '`+string(e.admin.boot.PrincipalID)+`', `+ts+`)`)
		}

		run()
		if suspended() {
			t.Fatal("an owner holding the predicate was suspended")
		}
		consented, _ := mintDeveloper(t, db, &e.ceremonyEnv, time.Hour, true)
		del := deliverySvc(t, db)

		revoke()
		// Delivery stops at once, before any sweep.
		if r, err := del.Fetch(ctx, consented.Value, envScope(envA1), "", service.FetchOptions{}); err == nil && valueIs(deliveredKey(t, r, ceremonySecretA), personalPlain) {
			t.Fatal("delivery continued after reveal was revoked")
		}
		run()
		if !suspended() {
			t.Fatal("the sweep did not record the suspension")
		}
		// Access returns: the suspension clears and delivery resumes.
		regrant()
		clock = clock.Add(personalHalfWindow)
		run()
		if suspended() {
			t.Fatal("the sweep did not clear the suspension")
		}
		if r, err := del.Fetch(ctx, consented.Value, envScope(envA1), "", service.FetchOptions{}); err != nil || !valueIs(deliveredKey(t, r, ceremonySecretA), personalPlain) {
			t.Fatalf("delivery did not resume: err=%v", err)
		}

		// Lost again: the window restarts from the new suspension.
		revoke()
		run()
		clock = clock.Add(service.PersonalRetentionWindow - time.Second)
		run()
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 1 {
			t.Fatal("deleted before the retention window elapsed")
		}
		clock = clock.Add(time.Second)
		run()
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 0 {
			t.Fatal("not deleted once the retention window elapsed")
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_value_notices WHERE cause='`+service.PersonalCauseRetention+`' AND key_name='`+ceremonySecretA+`'`) != 1 {
			t.Fatal("retention left no notice")
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='personal_value.deleted' AND payload LIKE '%"cause":"retention"%'`) != 1 {
			t.Fatal("retention deletion not audited")
		}
	})
}

// personalHalfWindow is any time short of the retention window.
const personalHalfWindow = service.PersonalRetentionWindow / 2

func TestPersonalValueRestorePurges(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-restore")
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		db = restoreIsolationFixture(t, db)
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 0 {
			t.Fatal("a personal value survived the restore")
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_value_notices WHERE cause='`+service.PersonalCauseRestore+`' AND key_name='`+ceremonySecretA+`'`) != 1 {
			t.Fatal("restore left the owner no notice")
		}
	})
}
