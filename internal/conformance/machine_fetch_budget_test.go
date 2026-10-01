package conformance

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func init() {
	corpus = append(corpus, scenario{"machine_fetch_principal_budget_shared_across_credentials", scenarioMachineFetchPrincipalBudget})
}

func scenarioMachineFetchPrincipalBudget(t *testing.T, db *store.DB) {
	who, scope, values, envs, keys := valueFixture(t, db, "fetchprincipalbudget")
	actor := service.LocalPrincipal(who)
	grantOrg(t, db, who, scope.Org, "fetchbudgetmanager", "manage-identities")
	env := mustEnv(t, envs, actor, scope, "prod")
	mustKey(t, keys, actor, scope, "MODE", string(schema.Config), schema.DefaultPresenceRules())
	publishValue(t, db, values, actor, env, "MODE", "production")
	identities := &service.Identities{DB: db, Auth: &service.Auth{DB: db}}
	create := func(name string, credentials int) []service.Actor {
		t.Helper()
		account, err := identities.CreateServiceAccount(t.Context(), actor, scope, name, domain.ClassWorkload)
		if err != nil {
			t.Fatal(err)
		}
		grantOrg(t, db, account.Principal, scope.Org, name, "read")
		var out []service.Actor
		for range credentials {
			minted, err := identities.MintCredential(t.Context(), actor, scope, account.ID, service.MintRequest{})
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, service.Bearer(minted.Value))
		}
		return out
	}
	shared, independent := create("shared-fetch-budget", 2), create("independent-fetch-budget", 1)
	fetch := &service.Delivery{DB: db, Keyring: sharedKeyring(t, db), Budget: service.NewBudget()}
	// A syntactically valid but unminted artifact authenticates nobody.
	// Such attempts cannot spend either the tenant or principal allowance.
	unminted, _, err := crypto.NewArtifact(crypto.ArtifactWorkload)
	if err != nil {
		t.Fatal(err)
	}
	for range service.BudgetMachineFetchOrgPerMin + 1 {
		if _, err := fetch.FetchAs(t.Context(), service.Bearer(unminted), env, "", service.FetchOptions{}); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("unauthenticated fetch = %v", err)
		}
	}
	// Alternate real credentials. Their aggregate burst belongs to the same
	// principal, rather than each credential receiving an independent bucket.
	for index := range service.BudgetMachineFetchPrincipalBurst {
		result, err := fetch.FetchAs(t.Context(), shared[index%len(shared)], env, "", service.FetchOptions{})
		if err != nil || len(result.Keys) != 1 {
			t.Fatalf("authorized burst fetch %d = keys:%d err:%v", index+1, len(result.Keys), err)
		}
	}
	// Allow a bounded amount of real-clock refill on slow race-test machines.
	// A credential-keyed implementation would allow at least two full bursts.
	limited := false
	for index := range service.BudgetMachineFetchPrincipalBurst / 2 {
		_, err := fetch.FetchAs(t.Context(), shared[index%len(shared)], env, "", service.FetchOptions{})
		if errors.Is(err, admission.ErrOverloaded) {
			limited = true
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !limited {
		t.Fatal("two credentials bypassed the shared principal fetch bucket")
	}
	for _, credential := range shared {
		if _, err := fetch.FetchAs(t.Context(), credential, env, "", service.FetchOptions{}); !errors.Is(err, admission.ErrOverloaded) {
			t.Fatalf("sibling credential escaped exhausted principal bucket: %v", err)
		}
	}
	if result, err := fetch.FetchAs(t.Context(), independent[0], env, "", service.FetchOptions{}); err != nil || len(result.Keys) != 1 {
		t.Fatalf("independent principal inherited another principal's debt: keys:%d err:%v", len(result.Keys), err)
	}
}
