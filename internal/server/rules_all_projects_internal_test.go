package server

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// An all-projects create (owner decision R2, #1446) repeats one folder set
// under every listed project; the request collapses to that set and keeps the
// list as the claim the service checks. Anything uneven is refused on shape.
func TestRuleRequestAllProjects(t *testing.T) {
	all := apigen.RuleProjectModeAll
	folder := func(p, path string) apigen.RuleKeyItem { return apigen.RuleKeyItem{Project: p, Folder: &path} }
	req := func(items ...apigen.RuleKeyItem) apigen.CreateRuleRequest {
		return apigen.CreateRuleRequest{Principal: "usr_x", Capability: apigen.RuleCapabilityEdit, Where: apigen.RuleWhere{
			ProjectMode: &all, Projects: []apigen.ID{"prj_a", "prj_b"},
			Environments: apigen.RuleEnvironmentAxis{Mode: apigen.RuleAxisModeAll, Items: []apigen.RuleEnvironmentItem{}},
			Keys:         apigen.RuleKeyAxis{Mode: apigen.RuleAxisModeAll, Items: items},
		}}
	}

	spec, err := ruleRequest("org_x", req(folder("prj_a", "db"), folder("prj_a", "app"), folder("prj_b", "app"), folder("prj_b", "db")))
	if err != nil {
		t.Fatal(err)
	}
	w := spec.Where
	if !w.AllProjects || len(w.Projects) != 0 || len(w.Keys) != 1 || len(w.Keys[domain.AnyProject]) != 2 ||
		len(spec.ClaimedProjects) != 2 || spec.ClaimedProjects[1] != "prj_b" {
		t.Fatalf("spec %+v", spec)
	}

	for name, items := range map[string][]apigen.RuleKeyItem{
		"a folder missing under one project": {folder("prj_a", "db"), folder("prj_a", "app"), folder("prj_b", "db")},
		"a different folder":                 {folder("prj_a", "db"), folder("prj_b", "app")},
		"a duplicate hiding a missing one":   {folder("prj_a", "db"), folder("prj_a", "app"), folder("prj_b", "db"), folder("prj_b", "db")},
		"a duplicate in the first project":   {folder("prj_a", "db"), folder("prj_a", "db"), folder("prj_b", "db"), folder("prj_b", "app")},
		"a project the rule does not list":   {folder("prj_a", "db"), folder("prj_b", "db"), folder("prj_c", "db")},
	} {
		if _, err := ruleRequest("org_x", req(items...)); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
	key := "key_1"
	if _, err := ruleRequest("org_x", req(apigen.RuleKeyItem{Project: "prj_a", Key: &key}, apigen.RuleKeyItem{Project: "prj_b", Key: &key})); !errors.Is(err, domain.ErrRuleAllProjectsKeys) {
		t.Errorf("single keys: %v, want ErrRuleAllProjectsKeys", err)
	}

	// Positive control: project_mode only is passed through untouched.
	only := req(folder("prj_a", "db"))
	only.Where.ProjectMode = nil
	if spec, err := ruleRequest("org_x", only); err != nil || spec.Where.AllProjects || len(spec.ClaimedProjects) != 0 {
		t.Fatalf("only-mode spec %+v, %v", spec, err)
	}
}
