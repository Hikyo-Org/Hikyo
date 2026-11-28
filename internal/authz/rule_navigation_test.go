package authz

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestNavigationProjectionKeepsMixedReadSelectorValid(t *testing.T) {
	rule := domain.Rule{Org: "org", Capability: domain.CapRead, Where: domain.Where{
		Projects: []domain.ProjectID{"selected", "unselected"}, EnvMode: domain.AxisOnly, EnvNames: []string{"env"},
		KeyMode: domain.AxisOnly, Keys: map[domain.ProjectID][]domain.RuleKeyItem{"unselected": {{IsFolder: true, Folder: "private"}}},
	}}
	// Only the selected project has an environment of that name.
	rule.Where = rule.Where.BindEnvs([]domain.EnvRef{{Project: "selected", ID: "env", Name: "env"}})
	if err := rule.Validate(); err != nil {
		t.Fatal(err)
	}
	projected := ruleNavigationReach(OpProjectList, domain.Scope{Org: "org"}, []domain.Rule{rule}, nil)
	if len(projected) != 1 || len(projected[0].Where.Projects) != 1 || projected[0].Where.Projects[0] != "selected" {
		t.Fatalf("projection: %+v", projected)
	}
	if err := projected[0].Validate(); err != nil {
		t.Fatalf("projection invalid: %v", err)
	}
	if !projected[0].Reaches(domain.CapRead, domain.LevelEnv, domain.Scope{Org: "org", Project: "selected", Env: "env"}, nil) {
		t.Fatal("selected environment lost through mixed-key Read selector")
	}
	if len(rule.Where.Projects) != 2 || len(rule.Where.Keys) != 1 || len(rule.Where.Envs) != 1 || len(rule.Where.EnvNames) != 1 {
		t.Fatal("projection mutated original rule")
	}
}
