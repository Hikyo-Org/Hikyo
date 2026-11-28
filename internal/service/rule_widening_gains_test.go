package service

import (
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// A widening gain names the key selector it reaches (#1447): a gain of one
// folder does not read as the whole environment's, a gain over every key
// absorbs a narrowed one for the same environment, and different selectors
// stay separate entries in a deterministic order.
func TestGainsOfKeepsKeySelectors(t *testing.T) {
	const prj, alice, bob = domain.ProjectID("prj_g"), domain.PrincipalID("usr_alice"), domain.PrincipalID("usr_bob")
	folder := func(f string) domain.RuleKeyItem { return domain.RuleKeyItem{Folder: f, IsFolder: true} }
	onlyDB := keySelector(domain.AxisOnly, []domain.RuleKeyItem{folder("db")})
	exceptApp := keySelector(domain.AxisAll, []domain.RuleKeyItem{{KeyID: "key_1"}, folder("app"), folder("")})
	atoms := map[gainAtom]bool{
		{alice, domain.CapEdit, "env_dev", onlyDB}:     true,
		{alice, domain.CapEdit, "env_qa", onlyDB}:      true,
		{alice, domain.CapEdit, "env_qa", exceptApp}:   true,
		{alice, domain.CapEdit, "", onlyDB}:            true,
		{bob, domain.CapRead, "env_dev", ""}:           true,
		{bob, domain.CapReveal, "env_dev", ""}:         true,
		{bob, domain.CapReveal, "env_dev", onlyDB}:     true, // absorbed by every key
		{bob, domain.CapReveal, "env_qa", exceptApp}:   true,
		{bob, domain.CapReveal, "", exceptApp}:         true, // absorbed by the project over every key
		{bob, domain.CapReveal, "", ""}:                true,
		{bob, domain.CapPublish, "env_prod", onlyDB}:   true,
		{bob, domain.CapPublish, "env_prod", ""}:       true, // absorbs the db/ gain
		{alice, domain.CapReveal, "env_prod", onlyDB}:  true,
		{alice, domain.CapReveal, "env_stage", onlyDB}: true,
	}
	db := &WideningKeys{Project: prj, Mode: domain.AxisOnly, Items: []domain.RuleKeyItem{folder("db")}}
	app := &WideningKeys{Project: prj, Mode: domain.AxisAll, Items: []domain.RuleKeyItem{folder(""), folder("app"), {KeyID: "key_1"}}}
	want := []WideningGain{
		{Principal: alice, Capability: domain.CapEdit, Keys: db},
		{Principal: alice, Capability: domain.CapEdit, Envs: []domain.EnvID{"env_qa"}, Keys: app},
		{Principal: alice, Capability: domain.CapEdit, Envs: []domain.EnvID{"env_dev", "env_qa"}, Keys: db},
		{Principal: alice, Capability: domain.CapReveal, Envs: []domain.EnvID{"env_prod", "env_stage"}, Keys: db},
		{Principal: bob, Capability: domain.CapPublish, Envs: []domain.EnvID{"env_prod"}},
		{Principal: bob, Capability: domain.CapRead, Envs: []domain.EnvID{"env_dev"}},
		{Principal: bob, Capability: domain.CapReveal},
		{Principal: bob, Capability: domain.CapReveal, Envs: []domain.EnvID{"env_dev"}},
		{Principal: bob, Capability: domain.CapReveal, Envs: []domain.EnvID{"env_qa"}, Keys: app},
	}
	for range 20 { // map order varies; the result must not
		if got := gainsOf(prj, atoms); !sameGains(got, want) {
			t.Fatalf("gainsOf =\n%+v\nwant\n%+v", got, want)
		}
	}

	// Who must confirm does not depend on the selectors.
	whole := map[gainAtom]bool{}
	for a := range atoms {
		a.keys = ""
		whole[a] = true
	}
	if got, all := gainedPrincipals(gainsOf(prj, atoms)), gainedPrincipals(gainsOf(prj, whole)); !slices.Equal(got, all) ||
		!slices.Equal(got, []domain.PrincipalID{alice, bob}) {
		t.Fatalf("gained principals with selectors %v, without %v", got, all)
	}

	// A move's gain (every key) and a narrowed environment gain stay apart.
	move := []WideningGain{{Principal: alice, Capability: domain.CapEdit, Envs: []domain.EnvID{"env_dev"}}}
	merged := mergeGains(move, []WideningGain{{Principal: alice, Capability: domain.CapEdit, Envs: []domain.EnvID{"env_dev"}, Keys: db}})
	if len(merged) != 2 || merged[0].Keys != nil || !sameKeys(merged[1].Keys, db) {
		t.Fatalf("move and narrowed gains merged into %+v", merged)
	}
}

// sameGains compares gains by value, Keys included.
func sameGains(a, b []WideningGain) bool {
	return slices.EqualFunc(a, b, func(x, y WideningGain) bool {
		return x.Principal == y.Principal && x.Name == y.Name && x.Capability == y.Capability && slices.Equal(x.Envs, y.Envs) && sameKeys(x.Keys, y.Keys)
	})
}

func sameKeys(x, y *WideningKeys) bool {
	if x == nil || y == nil {
		return x == y
	}
	return x.Project == y.Project && x.Mode == y.Mode && slices.Equal(x.Items, y.Items)
}
