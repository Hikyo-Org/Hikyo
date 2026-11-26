package authz

import (
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestRuleGrantsProjectsReachPerEnvironment(t *testing.T) {
	const org, p1, p2 = domain.OrgID("org"), domain.ProjectID("p1"), domain.ProjectID("p2")
	project := domain.Scope{Org: org, Project: p1}
	envs := []domain.EnvID{"dev", "prod"}
	// Each environment's name is its id; rules arrive bound, as the store reads them.
	refs := []domain.EnvRef{{Project: p1, ID: "dev", Name: "dev"}, {Project: p1, ID: "prod", Name: "prod"}}
	rule := func(c domain.Capability, envMode domain.AxisMode, envNames ...string) domain.Rule {
		w := domain.Where{Projects: []domain.ProjectID{p1}, EnvMode: envMode, EnvNames: envNames, KeyMode: domain.AxisAll}
		return domain.Rule{Capability: c, Org: org, Where: w.BindEnvs(refs)}
	}
	env := func(e domain.EnvID) domain.Scope { return domain.Scope{Org: org, Project: p1, Env: e} }
	other := rule(domain.CapRead, domain.AxisAll)
	other.Where.Projects = []domain.ProjectID{p2}
	keyed := rule(domain.CapEdit, domain.AxisAll)
	keyed.Where.KeyMode = domain.AxisOnly
	keyed.Where.Keys = map[domain.ProjectID][]domain.RuleKeyItem{p1: {{Folder: "db", IsFolder: true}}}
	got := RuleGrants([]domain.Rule{
		rule(domain.CapRead, domain.AxisAll),
		rule(domain.CapEdit, domain.AxisOnly, "dev"),
		rule(domain.CapPublish, domain.AxisAll, "dev"),
		other,
		keyed,
		{Capability: domain.CapRead, Org: org},
	}, project, envs)
	want := []domain.Grant{
		{Capability: domain.CapRead, Scope: project},
		{Capability: domain.CapEdit, Scope: env("dev")},
		{Capability: domain.CapPublish, Scope: env("prod")},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("RuleGrants = %+v, want %+v", got, want)
	}
	// A rule-held read makes a grant-held reveal reach plaintext.
	reach := ReachableFrom(project, envs, append([]domain.Grant{{Capability: domain.CapReveal, Scope: env("prod")}}, got...))
	if !reach.Current["prod"] || reach.Current["dev"] {
		t.Fatalf("reach = %+v, want current plaintext in prod only", reach)
	}
}
