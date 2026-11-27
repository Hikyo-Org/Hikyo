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

// Schedules run on the default branch only, so dev's feedback seed is refreshed by a main job that
// asks the seed workflow to run on dev. That job must hold actions: write and nothing else, run no
// repository code, and name exactly the seed workflow and the dev ref: the seed's own guard then
// decides what a dev run may save, so the dev family stays writable from dev only.

const (
	devSeedRefreshWorkflow = ".github/workflows/ci-dev-seed-refresh.yml"
	devSeedRefreshGuard    = "(github.event_name == 'workflow_dispatch' || github.event_name == 'schedule') && github.ref == 'refs/heads/main'"
	devSeedRefreshRun      = "gh workflow run ci-feedback-backend-cache.yml --ref dev"
)

func requireDevSeedRefreshWorkflow(workflow map[string]any) error {
	if !reflect.DeepEqual(workflow["on"], map[string]any{
		"workflow_dispatch": nil,
		"schedule":          []any{map[string]any{"cron": "44 3 * * *"}},
	}) {
		return fmt.Errorf("the refresh runs only on a schedule or by hand, before the Monday 04:11 lint driver rollover")
	}
	if !reflect.DeepEqual(workflow["permissions"], map[string]any{}) {
		return fmt.Errorf("the workflow grants nothing by default")
	}
	if !reflect.DeepEqual(workflow["concurrency"], map[string]any{"group": "ci-dev-seed-refresh", "cancel-in-progress": false}) {
		return fmt.Errorf("runs must queue behind each other")
	}
	jobs, _ := workflow["jobs"].(map[string]any)
	job, _ := jobs["dispatch"].(map[string]any)
	if len(jobs) != 1 || job == nil || job["if"] != devSeedRefreshGuard {
		return fmt.Errorf("one dispatch job, only from a schedule or a manual run on main")
	}
	if !reflect.DeepEqual(job["permissions"], map[string]any{"actions": "write"}) {
		return fmt.Errorf("dispatch must hold actions: write and nothing else")
	}
	for _, key := range []string{"needs", "container", "services", "strategy", "continue-on-error", "defaults"} {
		if job[key] != nil {
			return fmt.Errorf("dispatch must not set %s", key)
		}
	}
	if !reflect.DeepEqual(job["env"], map[string]any{"GH_TOKEN": "${{ github.token }}", "GH_REPO": "${{ github.repository }}"}) {
		return fmt.Errorf("dispatch takes the workflow token and repository only")
	}
	steps, _ := job["steps"].([]any)
	if len(steps) != 1 {
		return fmt.Errorf("dispatch is a single command: no checkout, no action, no repository code")
	}
	step, _ := steps[0].(map[string]any)
	if step["run"] != devSeedRefreshRun || step["uses"] != nil || step["working-directory"] != nil || step["shell"] != nil || step["env"] != nil || step["if"] != nil || step["continue-on-error"] != nil {
		return fmt.Errorf("dispatch must run exactly %q", devSeedRefreshRun)
	}
	return nil
}

func TestDevSeedRefreshStaysInsideItsTrustBoundary(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), devSeedRefreshWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	var workflow map[string]any
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if err := requireDevSeedRefreshWorkflow(workflow); err != nil {
		t.Fatal(err)
	}
	job := func(w map[string]any) map[string]any { return w["jobs"].(map[string]any)["dispatch"].(map[string]any) }
	step := func(w map[string]any) map[string]any { return job(w)["steps"].([]any)[0].(map[string]any) }
	for name, mutate := range map[string]func(map[string]any){
		"runs on pull requests": func(w map[string]any) { w["on"].(map[string]any)["pull_request"] = map[string]any{} },
		"runs on pushes":        func(w map[string]any) { w["on"].(map[string]any)["push"] = map[string]any{} },
		"runs after a workflow": func(w map[string]any) { w["on"].(map[string]any)["workflow_run"] = map[string]any{} },
		"after the rollover": func(w map[string]any) {
			w["on"].(map[string]any)["schedule"] = []any{map[string]any{"cron": "44 5 * * *"}}
		},
		"guard accepts any ref": func(w map[string]any) { job(w)["if"] = "github.event_name == 'schedule'" },
		"guard accepts dev": func(w map[string]any) {
			job(w)["if"] = "(github.event_name == 'workflow_dispatch' || github.event_name == 'schedule') && github.ref == 'refs/heads/dev'"
		},
		"widened permissions": func(w map[string]any) { job(w)["permissions"].(map[string]any)["contents"] = "write" },
		"workflow-wide token": func(w map[string]any) { w["permissions"] = map[string]any{"actions": "write"} },
		"dispatches onto main": func(w map[string]any) {
			step(w)["run"] = "gh workflow run ci-feedback-backend-cache.yml --ref main"
		},
		"dispatches another workflow": func(w map[string]any) { step(w)["run"] = "gh workflow run ci.yml --ref dev" },
		"dispatches a ref from the event": func(w map[string]any) {
			step(w)["run"] = "gh workflow run ci-feedback-backend-cache.yml --ref ${{ github.event.inputs.ref }}"
		},
		"checks out the repository": func(w map[string]any) {
			job(w)["steps"] = append([]any{map[string]any{"uses": "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"}}, job(w)["steps"].([]any)...)
		},
		"cancels a half-done run": func(w map[string]any) { w["concurrency"].(map[string]any)["cancel-in-progress"] = true },
	} {
		t.Run("structure/"+name, func(t *testing.T) {
			var changed map[string]any
			if err := yaml.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			mutate(changed)
			if requireDevSeedRefreshWorkflow(changed) == nil {
				t.Fatal("accepted a refresh workflow outside its trust boundary")
			}
		})
	}
}

// The seed workflow keeps what makes a dispatched dev run a dev run: it accepts a manual run on dev,
// writes the dev family only from the dev ref, and keys the entry by the UTC day. A refresh that
// stopped being a dev write would be a write into the wrong trust family.
func TestDevSeedRefreshTargetsADevOnlyWriter(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-feedback-backend-cache.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow map[string]any
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	on, _ := workflow["on"].(map[string]any)
	if _, ok := on["workflow_dispatch"]; !ok {
		t.Fatal("the seed workflow must accept a manual run, the way the refresh starts it on dev")
	}
	text := string(raw)
	for _, want := range []string{
		"(github.event_name == 'workflow_dispatch' || github.event_name == 'push') && github.ref == 'refs/heads/dev'",
		"github.ref == 'refs/heads/dev' && 'dev' || 'main'",
		"github.ref == 'refs/heads/dev' && 'dev' || github.sha }}-${{ steps.seed-day.outputs.value }}",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the seed workflow no longer carries %q", want)
		}
	}
}
