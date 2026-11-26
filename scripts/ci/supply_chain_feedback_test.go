package ci_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The names are the gate's contract: scripts/ci/pr-gate requires exactly one
// job of each name, and refuses a skipped fixtures job unless the selection job
// succeeded.
const (
	supplyChainSelectKey      = "select-supply-chain"
	supplyChainSelectName     = "select supply-chain fixtures"
	supplyChainFixturesKey    = "supply-chain-fixtures"
	supplyChainFixturesName   = "supply-chain fixtures"
	supplyChainMetadataGuard  = "github.event.action != 'edited' || github.event.changes.base.ref.from != ''"
	supplyChainFixturesGuard  = "(github.event.action != 'edited' || github.event.changes.base.ref.from != '') && needs.select-supply-chain.outputs.run == 'true'"
	supplyChainReplayCommand  = "go run ./scripts/ci/run-job-steps .github/workflows/ci.yml supply-chain-checks"
	supplyChainFixturesBudget = 20
)

func requireSupplyChainFeedbackJobs(raw []byte) error {
	var got struct {
		Jobs map[string]struct {
			Name     string            `yaml:"name"`
			If       string            `yaml:"if"`
			Needs    any               `yaml:"needs"`
			Timeout  int               `yaml:"timeout-minutes"`
			Continue bool              `yaml:"continue-on-error"`
			Strategy map[string]any    `yaml:"strategy"`
			Outputs  map[string]string `yaml:"outputs"`
			Env      map[string]string `yaml:"env"`
			Steps    []struct {
				Name     string            `yaml:"name"`
				ID       string            `yaml:"id"`
				Run      string            `yaml:"run"`
				Uses     string            `yaml:"uses"`
				If       string            `yaml:"if"`
				With     map[string]string `yaml:"with"`
				Continue bool              `yaml:"continue-on-error"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &got); err != nil {
		return err
	}
	selection, ok := got.Jobs[supplyChainSelectKey]
	if !ok || selection.Name != supplyChainSelectName {
		return fmt.Errorf("the gate requires the selection job named %q", supplyChainSelectName)
	}
	fixtures, ok := got.Jobs[supplyChainFixturesKey]
	if !ok || fixtures.Name != supplyChainFixturesName {
		return fmt.Errorf("the gate requires the fixtures job named %q", supplyChainFixturesName)
	}
	if selection.If != supplyChainMetadataGuard || !strings.HasPrefix(fixtures.If, "("+supplyChainMetadataGuard+")") {
		return fmt.Errorf("metadata-only edits must skip the supply-chain jobs like the feedback job")
	}
	if fixtures.If != supplyChainFixturesGuard || !reflect.DeepEqual(fixtures.Needs, supplyChainSelectKey) {
		return fmt.Errorf("the fixtures run only when the selection job selects them")
	}
	if selection.Continue || fixtures.Continue || selection.Strategy != nil || fixtures.Strategy != nil {
		return fmt.Errorf("the supply-chain jobs must be plain and fail closed, not a matrix lane")
	}
	if selection.Timeout != 3 || fixtures.Timeout != supplyChainFixturesBudget {
		return fmt.Errorf("the supply-chain jobs keep their own bounded timeouts")
	}
	if selection.Outputs["run"] != "${{ steps.select.outputs.run }}" {
		return fmt.Errorf("selection must publish run for the fixtures job")
	}
	if len(selection.Steps) != 2 || selection.Steps[0].With["ref"] != "${{ github.sha }}" || selection.Steps[0].With["fetch-depth"] != "2" || selection.Steps[0].With["persist-credentials"] != "false" {
		return fmt.Errorf("selection must read the merge commit and both parents without credentials")
	}
	pick := selection.Steps[1]
	if pick.ID != "select" || pick.If != "" || pick.Continue || !strings.Contains(pick.Run, `test "$(git rev-parse HEAD^2)" = "$PR_HEAD"`) || !strings.Contains(pick.Run, "./scripts/ci/select-supply-chain-fixtures.sh") || selection.Env["PR_HEAD"] != "${{ github.event.pull_request.head.sha }}" {
		return fmt.Errorf("selection must bind the merge commit to the PR head and run the selector")
	}
	if len(fixtures.Steps) < 3 || fixtures.Steps[0].With["ref"] != "${{ github.event.pull_request.head.sha }}" || fixtures.Steps[0].With["persist-credentials"] != "false" {
		return fmt.Errorf("the fixtures must test the exact PR head without credentials")
	}
	last := fixtures.Steps[len(fixtures.Steps)-1]
	if last.If != "" || last.Continue || last.Run != supplyChainReplayCommand {
		return fmt.Errorf("the fixtures job must replay the ci.yml job's steps")
	}
	for _, step := range fixtures.Steps {
		if step.Continue || strings.Contains(step.Uses, "cache/save") {
			return fmt.Errorf("the fixtures job must fail closed and write no cache")
		}
	}
	return nil
}

func TestFeedbackSupplyChainJobsAndMutations(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-feedback.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := requireSupplyChainFeedbackJobs(raw); err != nil {
		t.Fatal(err)
	}
	job := func(w map[string]any, key string) map[string]any {
		return w["jobs"].(map[string]any)[key].(map[string]any)
	}
	for name, mutation := range map[string]func(map[string]any){
		"selection loses the metadata guard": func(w map[string]any) { delete(job(w, supplyChainSelectKey), "if") },
		"fixtures lose the metadata guard": func(w map[string]any) {
			job(w, supplyChainFixturesKey)["if"] = "needs.select-supply-chain.outputs.run == 'true'"
		},
		"fixtures ignore the selection":         func(w map[string]any) { job(w, supplyChainFixturesKey)["if"] = "(" + supplyChainMetadataGuard + ")" },
		"fixtures lose the guard entirely":      func(w map[string]any) { delete(job(w, supplyChainFixturesKey), "if") },
		"selection renamed":                     func(w map[string]any) { job(w, supplyChainSelectKey)["name"] = "select fixtures" },
		"fixtures renamed":                      func(w map[string]any) { job(w, supplyChainFixturesKey)["name"] = "fixtures" },
		"selection removed":                     func(w map[string]any) { delete(w["jobs"].(map[string]any), supplyChainSelectKey) },
		"fixtures removed":                      func(w map[string]any) { delete(w["jobs"].(map[string]any), supplyChainFixturesKey) },
		"fixtures no longer need the selection": func(w map[string]any) { delete(job(w, supplyChainFixturesKey), "needs") },
		"fixtures timeout raised": func(w map[string]any) {
			job(w, supplyChainFixturesKey)["timeout-minutes"] = supplyChainFixturesBudget + 10
		},
		"fixtures fail open": func(w map[string]any) { job(w, supplyChainFixturesKey)["continue-on-error"] = true },
		"fixtures turned into a matrix lane": func(w map[string]any) {
			job(w, supplyChainFixturesKey)["strategy"] = map[string]any{"matrix": map[string]any{"lane": []any{"a"}}}
		},
		"selection not bound to the PR head": func(w map[string]any) { job(w, supplyChainSelectKey)["env"] = map[string]any{"PR_HEAD": "x"} },
		"replay replaced": func(w map[string]any) {
			steps := job(w, supplyChainFixturesKey)["steps"].([]any)
			steps[len(steps)-1].(map[string]any)["run"] = "true"
		},
	} {
		t.Run(name, func(t *testing.T) {
			var workflow map[string]any
			if err := yaml.Unmarshal(raw, &workflow); err != nil {
				t.Fatal(err)
			}
			mutation(workflow)
			changed, err := yaml.Marshal(workflow)
			if err != nil {
				t.Fatal(err)
			}
			if requireSupplyChainFeedbackJobs(changed) == nil {
				t.Fatal("unsafe supply-chain job configuration escaped the contract")
			}
			if requireFeedbackWorkflow(changed) == nil {
				t.Fatal("the feedback workflow contract accepted it")
			}
		})
	}
}
