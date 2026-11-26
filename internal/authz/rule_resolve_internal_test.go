package authz

import (
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// A question about Reveal never consults the database: holds is the ordinary
// atom over the given lines, so a zero TxAuthorizer is enough.
func revealQuestion(key *domain.RuleKey, visible ...string) accessQuestion {
	q := accessQuestion{
		chain:      domain.Scope{Org: "org_a", Project: "prj_a", Env: "env_dev"},
		capability: domain.CapReveal, at: domain.LevelEnv, key: key, visible: map[string]bool{}, envs: &questionEnvs{},
	}
	for _, id := range visible {
		q.visible[id] = true
	}
	return q
}

// A rule outside the caller's view still counts towards the holder's state,
// but is never named: the holder is can with no sources (api-cli-surface
// amendment, "only visible rules are named").
func TestRuleResolveSourcesNameOnlyVisibleRules(t *testing.T) {
	rule := domain.Rule{ID: "rul_hidden", Principal: "usr_carol", Capability: domain.CapReveal, Org: "org_a",
		Where: domain.Where{Projects: []domain.ProjectID{"prj_a"}, EnvMode: domain.AxisAll, KeyMode: domain.AxisAll}}
	if err := rule.Validate(); err != nil {
		t.Fatalf("fixture rule is invalid: %v", err)
	}
	for _, tc := range []struct {
		name    string
		visible []string
		want    []string
	}{
		{"hidden", nil, nil},
		{"visible", []string{rule.ID}, []string{rule.ID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := revealQuestion(nil, tc.visible...)
			held, err := q.holds(t.Context(), q.capability, nil, []domain.Rule{rule})
			if err != nil || !held {
				t.Fatalf("holds = %v, %v; want held through the rule", held, err)
			}
			var out AccessAnswer
			if err := q.sources(t.Context(), &out, nil, []domain.Rule{rule}); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(out.Rules, tc.want) || len(out.Grants) != 0 {
				t.Fatalf("sources = %+v, want rules %v", out, tc.want)
			}
		})
	}
}

// A rule excepting both the asked environment and the asked key names both
// excepts, so excepted's "none of them alone" error stays unreachable for it.
func TestRuleResolveExceptedNamesEnvironmentAndKeyExcepts(t *testing.T) {
	item := domain.RuleKeyItem{Folder: "db", IsFolder: true}
	rule := domain.Rule{ID: "rul_both", Principal: "usr_dave", Capability: domain.CapReveal, Org: "org_a",
		Where: domain.Where{
			Projects: []domain.ProjectID{"prj_a"},
			EnvMode:  domain.AxisAll, EnvNames: []string{"dev"},
			KeyMode: domain.AxisAll, Keys: map[domain.ProjectID][]domain.RuleKeyItem{"prj_a": {item}},
		}}
	rule.Where = rule.Where.BindEnvs([]domain.EnvRef{{Project: "prj_a", ID: "env_dev", Name: "dev"}})
	if err := rule.Validate(); err != nil {
		t.Fatalf("fixture rule is invalid: %v", err)
	}
	q := revealQuestion(&domain.RuleKey{ID: "key_pw", Folder: "db"}, rule.ID)
	except, ok, err := q.excepted(t.Context(), rule)
	if err != nil || !ok {
		t.Fatalf("excepted = %v, %v; want the rule excepted", ok, err)
	}
	if !slices.Equal(except.Envs, []domain.EnvID{"env_dev"}) || !slices.Equal(except.Keys, []domain.RuleKeyItem{item}) || except.KeyLimited {
		t.Fatalf("except = %+v, want env_dev and folder db", except)
	}
}
