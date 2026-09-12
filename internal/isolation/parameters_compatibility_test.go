package isolation

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestParameterOptInPreservesExistingLiterals(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		seedDeliveryCatalogue(t, db)
		actor := service.LocalPrincipal(identAdmin)
		scope := scopeEnv(orgA, prjA1, envA1)
		const literal = "postgres://host:${PORT}/db/${unfinished"
		publishDeliveryValues(t, db, envA1, map[string]string{"DATABASE_URL": literal})
		// Simulate a pre-upgrade snapshot, then publish an unrelated change. The
		// encrypted untouched literal must retain its original interpretation.
		execRaw(t, db, `UPDATE snapshots SET parameter_contract='{}' WHERE environment_id='env_a1'`)
		publishDeliveryValues(t, db, envA1, map[string]string{"DATABASE_PASSWORD": "changed"})
		assertURL := func(want string, supplied map[string]string) {
			t.Helper()
			out, err := deliverySvc(t, db).FetchAs(t.Context(), actor, scope, "", service.FetchOptions{Parameters: supplied})
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range out.Keys {
				if key.Name == "DATABASE_URL" {
					if key.Value == nil || *key.Value != want {
						t.Fatalf("wrong literal: %+v", key)
					}
					return
				}
			}
			t.Fatal("URL absent")
		}
		assertURL(literal, nil)
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
		if err := envs.SetParameter(t.Context(), actor, scope, "NUMBER", "[0-9]+", false); err != nil {
			t.Fatal(err)
		}
		assertURL(literal, nil) // Declaration edits do not change existing snapshots.
		staged, err := valueSvc(t, db).Set(t.Context(), actor, scope, "DATABASE_PASSWORD", "next", nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = revisionSvc(t, db).PublishPlanned(t.Context(), actor, scope, service.PublishRequest{VersionIDs: []string{staged.VersionID}})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("first opt-in did not require escaping legacy references: %v", err)
		}
		assertURL(literal, nil)
		publishDeliveryValues(t, db, envA1, map[string]string{"DATABASE_URL": "postgres://host:$${PORT}/db/$${unfinished/${NUMBER}"})
		assertURL(literal+"/123", map[string]string{"NUMBER": "123"})
	})
}

func TestParameterDraftReportsDeferredValidation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		seedDeliveryCatalogue(t, db)
		actor := service.LocalPrincipal(identAdmin)
		scope := scopeEnv(orgA, prjA1, envA1)
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
		if err := envs.SetParameter(t.Context(), actor, scope, "NUMBER", "[0-9]+", false); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			value    string
			deferred bool
		}{{"https://host/${NUMBER}", true}, {"https://host/$${NUMBER}", false}} {
			staged, err := valueSvc(t, db).Set(t.Context(), actor, scope, "DATABASE_URL", tc.value, nil)
			if err != nil {
				t.Fatal(err)
			}
			drafts, err := revisionSvc(t, db).PendingDrafts(t.Context(), actor, scope)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, draft := range drafts {
				if draft.VersionID == staged.VersionID {
					found = true
					if !draft.Valid || draft.ValidationDeferred != tc.deferred {
						t.Fatalf("wrong advisory: %+v", draft)
					}
				}
			}
			if !found {
				t.Fatal("draft missing")
			}
		}
	})
}
