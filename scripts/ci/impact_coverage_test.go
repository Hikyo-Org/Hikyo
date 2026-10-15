package ci_test

import (
	"fmt"
	"reflect"
)

// Preserve the original full-suite execution inventory. Permit only the exact
// additional tests and read-only metadata permission needed by staged impact CI.
func normalizeImpactAdditions(jobs map[string]map[string]any) error {
	permission := map[string]any{"actions": "read", "contents": "read", "pull-requests": "read"}
	if !reflect.DeepEqual(jobs["ci-required"]["permissions"], permission) {
		return fmt.Errorf("queue aggregate requires exact read-only metadata permissions")
	}
	delete(jobs["ci-required"], "permissions")
	expected := map[string]any{
		"name": "Staged impact CI fixtures",
		"run": "go test -count=1 ./scripts/ci/queue-report ./scripts/ci/impact-shadow\n" +
			"go test -count=1 ./scripts/ci -run '^(TestDocsQueue|TestBackendImpactShadow)'\n",
	}
	steps, ok := jobs["supply-chain-checks"]["steps"].([]any)
	if !ok {
		return fmt.Errorf("missing supply-chain steps")
	}
	kept := make([]any, 0, len(steps))
	found := 0
	for _, value := range steps {
		step, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("malformed supply-chain step")
		}
		if step["name"] == expected["name"] {
			if !reflect.DeepEqual(step, expected) {
				return fmt.Errorf("impact fixtures must remain unconditional and complete")
			}
			found++
		} else {
			kept = append(kept, step)
		}
	}
	if found != 1 {
		return fmt.Errorf("require exactly one complete impact fixture step")
	}
	jobs["supply-chain-checks"]["steps"] = kept
	return nil
}
