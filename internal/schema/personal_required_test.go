package schema_test

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/schema"
)

func TestCheckPersonalRequired(t *testing.T) {
	none := schema.Presence{Mode: schema.PresenceNone}
	dev := schema.Presence{Mode: schema.PresenceExplicit, Environments: []string{"env_dev"}}
	prod := schema.Presence{Mode: schema.PresenceExplicit, Environments: []string{"env_prod"}}
	all := schema.Presence{Mode: schema.PresenceAll}
	rules := func(required, forbidden schema.Presence) schema.PresenceRules {
		return schema.PresenceRules{Required: required, Forbidden: forbidden}
	}
	for name, tc := range map[string]struct {
		personal schema.Presence
		rules    schema.PresenceRules
		grouped  bool
		ok       bool
	}{
		"none":                      {none, rules(all, none), true, true},
		"explicit":                  {dev, rules(prod, prod), false, true},
		"all refused":               {all, rules(none, none), false, false},
		"explicit without ids":      {schema.Presence{Mode: schema.PresenceExplicit}, rules(none, none), false, false},
		"none with ids":             {schema.Presence{Mode: schema.PresenceNone, Environments: []string{"env_dev"}}, rules(none, none), false, false},
		"grouped key":               {dev, rules(none, none), true, false},
		"overlaps required_in":      {dev, rules(dev, none), false, false},
		"overlaps required_in all":  {dev, rules(all, none), false, false},
		"overlaps forbidden_in":     {dev, rules(none, dev), false, false},
		"overlaps forbidden_in all": {dev, rules(none, all), false, false},
		"unknown mode":              {schema.Presence{Mode: "sometimes"}, rules(none, none), false, false},
	} {
		if err := schema.CheckPersonalRequired(tc.personal, tc.rules, tc.grouped); (err == nil) != tc.ok {
			t.Errorf("%s: err=%v, want ok=%t", name, err, tc.ok)
		}
	}
}
