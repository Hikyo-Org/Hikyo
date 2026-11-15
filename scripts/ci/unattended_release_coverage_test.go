package ci_test

import (
	"fmt"
	"reflect"
	"testing"
)

// The unattended stable release adds four mandatory fixtures to the release
// owner job. Require each exact unconditional step once, so the frozen baseline
// digest still proves every historical check without hiding a skipped addition.
var unattendedReleaseFixtureSteps = []map[string]any{
	{"name": "Unattended stable tag authority fixture", "run": "./scripts/release/require-signed-tag_test.sh"},
	{"name": "Unattended stable compatibility fixture", "run": "./scripts/release/check-unattended-compatibility_test.sh"},
	{"name": "Unattended stable reconciler fixture", "run": "./scripts/release/stable-reconcile_test.sh"},
	{"name": "Unattended stable repository policy rollback fixture", "run": "./scripts/release/configure-stable-rollback_test.sh"},
}

func normalizeUnattendedReleaseFixtureAddition(job map[string]any) error {
	steps, ok := job["steps"].([]any)
	if !ok {
		return fmt.Errorf("unattended release fixtures lost their owner job steps")
	}
	kept := make([]any, 0, len(steps))
	found := map[string]int{}
	for _, value := range steps {
		step, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("unattended release fixture owner has malformed steps")
		}
		matched := false
		for _, expected := range unattendedReleaseFixtureSteps {
			if step["name"] == expected["name"] {
				if !reflect.DeepEqual(step, expected) {
					return fmt.Errorf("unattended release fixture %q lost its exact fail-closed configuration", step["name"])
				}
				found[step["name"].(string)]++
				matched = true
			}
		}
		if !matched {
			kept = append(kept, step)
		}
	}
	for _, expected := range unattendedReleaseFixtureSteps {
		if found[expected["name"].(string)] != 1 {
			return fmt.Errorf("unattended release fixture %q must appear exactly once", expected["name"])
		}
	}
	job["steps"] = kept
	return nil
}

func TestUnattendedReleaseFixtureAdditionIsExact(t *testing.T) {
	step := func(name, run string) any { return map[string]any{"name": name, "run": run} }
	good := func() map[string]any {
		return map[string]any{"steps": []any{
			step("old", "true"),
			step("Unattended stable tag authority fixture", "./scripts/release/require-signed-tag_test.sh"),
			step("Unattended stable compatibility fixture", "./scripts/release/check-unattended-compatibility_test.sh"),
			step("Unattended stable reconciler fixture", "./scripts/release/stable-reconcile_test.sh"),
			step("Unattended stable repository policy rollback fixture", "./scripts/release/configure-stable-rollback_test.sh"),
		}}
	}
	job := good()
	if err := normalizeUnattendedReleaseFixtureAddition(job); err != nil || len(job["steps"].([]any)) != 1 {
		t.Fatalf("exact addition must normalize away: %v", err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"removed": func(j map[string]any) { j["steps"] = j["steps"].([]any)[:4] },
		"duplicated": func(j map[string]any) {
			j["steps"] = append(j["steps"].([]any), j["steps"].([]any)[1])
		},
		"conditional": func(j map[string]any) { j["steps"].([]any)[1].(map[string]any)["if"] = "false" },
		"fail open": func(j map[string]any) {
			j["steps"].([]any)[3].(map[string]any)["run"] = "./scripts/release/stable-reconcile_test.sh || true"
		},
	} {
		job := good()
		mutate(job)
		if err := normalizeUnattendedReleaseFixtureAddition(job); err == nil {
			t.Fatalf("%s: weakened addition must be refused", name)
		}
	}
}
