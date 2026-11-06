package isolation

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/domain"
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
