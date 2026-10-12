package ci_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

type queueWorkflowContract struct {
	Name        string               `yaml:"name"`
	On          map[string]yaml.Node `yaml:"on"`
	Permissions map[string]string    `yaml:"permissions"`
	Env         yaml.Node            `yaml:"env"`
	Defaults    yaml.Node            `yaml:"defaults"`
	Concurrency struct {
		Group  string `yaml:"group"`
		Cancel bool   `yaml:"cancel-in-progress"`
		Queue  string `yaml:"queue"`
	} `yaml:"concurrency"`
	Jobs map[string]struct {
		Name        string            `yaml:"name"`
		Uses        string            `yaml:"uses"`
		If          string            `yaml:"if"`
		Needs       yaml.Node         `yaml:"needs"`
		Continue    yaml.Node         `yaml:"continue-on-error"`
		Env         yaml.Node         `yaml:"env"`
		Defaults    yaml.Node         `yaml:"defaults"`
		Container   yaml.Node         `yaml:"container"`
		Services    yaml.Node         `yaml:"services"`
		Strategy    yaml.Node         `yaml:"strategy"`
		RunsOn      string            `yaml:"runs-on"`
		Timeout     int               `yaml:"timeout-minutes"`
		Permissions map[string]string `yaml:"permissions"`
		Steps       []struct {
			Uses      string            `yaml:"uses"`
			Run       string            `yaml:"run"`
			If        string            `yaml:"if"`
			Continue  yaml.Node         `yaml:"continue-on-error"`
			Shell     string            `yaml:"shell"`
			Directory string            `yaml:"working-directory"`
			With      map[string]string `yaml:"with"`
			Env       map[string]string `yaml:"env"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func queuePublisherContract(raw []byte) error {
	var w queueWorkflowContract
	if err := yaml.Unmarshal(raw, &w); err != nil {
		return err
	}
	if w.Name != "queue-gate" || len(w.On) != 5 || w.Env.Kind != 0 || w.Defaults.Kind != 0 {
		return fmt.Errorf("queue publisher must have only its five trusted events and no execution overrides")
	}
	for _, name := range []string{"pull_request_target", "push", "workflow_run", "workflow_dispatch", "schedule"} {
		if _, ok := w.On[name]; !ok {
			return fmt.Errorf("queue publisher missing %s", name)
		}
	}
	var pr struct {
		Types []string `yaml:"types"`
	}
	node := w.On["pull_request_target"]
	if err := node.Decode(&pr); err != nil {
		return err
	}
	if !slices.Equal(pr.Types, []string{"opened", "synchronize", "reopened", "edited", "closed"}) {
		return fmt.Errorf("queue publisher must reconcile head, base and PR membership changes")
	}
	var push struct {
		Branches []string `yaml:"branches"`
	}
	node = w.On["push"]
	if err := node.Decode(&push); err != nil {
		return err
	}
	if !slices.Equal(push.Branches, []string{"main"}) {
		return fmt.Errorf("queue publisher push repair must be bound to main")
	}
	var callback struct {
		Types     []string `yaml:"types"`
		Workflows []string `yaml:"workflows"`
	}
	node = w.On["workflow_run"]
	if err := node.Decode(&callback); err != nil {
		return err
	}
	if !slices.Equal(callback.Types, []string{"requested", "in_progress", "completed"}) || !slices.Equal(callback.Workflows, []string{"ci-feedback", "trusted-merge-ci"}) {
		return fmt.Errorf("queue publisher must receive feedback and group start, rerun and completion callbacks")
	}
	var dispatch struct {
		Inputs map[string]struct {
			Required bool   `yaml:"required"`
			Type     string `yaml:"type"`
		} `yaml:"inputs"`
	}
	node = w.On["workflow_dispatch"]
	if err := node.Decode(&dispatch); err != nil {
		return err
	}
	if len(dispatch.Inputs) != 1 || !dispatch.Inputs["pr_number"].Required || dispatch.Inputs["pr_number"].Type != "string" {
		return fmt.Errorf("manual queue repair must supply only a required PR number")
	}
	var schedule []struct {
		Cron string `yaml:"cron"`
	}
	node = w.On["schedule"]
	if err := node.Decode(&schedule); err != nil {
		return err
	}
	if len(schedule) != 1 || schedule[0].Cron != "10,25,40,55 * * * *" {
		return fmt.Errorf("queue publisher must retain bounded periodic repair")
	}
	if !reflect.DeepEqual(w.Permissions, map[string]string{"contents": "read"}) || w.Concurrency.Group != "queue-gate-publisher" || w.Concurrency.Cancel || w.Concurrency.Queue != "max" {
		return fmt.Errorf("queue publisher must serialize check writes without cancelling retained events")
	}
	j, ok := w.Jobs["publish"]
	if !ok || len(w.Jobs) != 1 || j.Uses != "" || j.If != "" || j.Needs.Kind != 0 || j.Continue.Kind != 0 || j.Env.Kind != 0 || j.Defaults.Kind != 0 || j.Container.Kind != 0 || j.Services.Kind != 0 || j.Strategy.Kind != 0 || j.RunsOn != "ubuntu-latest" || j.Timeout <= 0 || j.Timeout > 10 {
		return fmt.Errorf("queue publication must be one bounded unconditional trusted job")
	}
	if !reflect.DeepEqual(j.Permissions, map[string]string{"actions": "read", "checks": "write", "contents": "read", "pull-requests": "read"}) || len(j.Steps) != 3 {
		return fmt.Errorf("queue publisher may only write checks and execute its three trusted steps")
	}
	for _, step := range j.Steps {
		if step.If != "" || step.Continue.Kind != 0 || step.Shell != "" || step.Directory != "" {
			return fmt.Errorf("queue publication cannot skip, mask failure or override its execution boundary")
		}
	}
	checkout, setup, publish := j.Steps[0], j.Steps[1], j.Steps[2]
	if !regexp.MustCompile(`^actions/checkout@[0-9a-f]{40}$`).MatchString(checkout.Uses) || checkout.Run != "" || len(checkout.Env) != 0 || !reflect.DeepEqual(checkout.With, map[string]string{"ref": "${{ github.event.repository.default_branch }}", "persist-credentials": "false"}) {
		return fmt.Errorf("queue publisher must check out pinned default-branch tooling without credentials")
	}
	if !regexp.MustCompile(`^actions/setup-go@[0-9a-f]{40}$`).MatchString(setup.Uses) || setup.Run != "" || len(setup.Env) != 0 || !reflect.DeepEqual(setup.With, map[string]string{"go-version-file": "go.mod", "cache": "false"}) {
		return fmt.Errorf("queue publisher must use pinned trusted Go without PR execution caches")
	}
	if publish.Uses != "" || len(publish.With) != 0 || publish.Run != "go run ./scripts/ci/pr-gate --queue" || !reflect.DeepEqual(publish.Env, map[string]string{"GH_TOKEN": "${{ github.token }}", "GH_REPO": "${{ github.repository }}", "PR_GATE_CHECK_NAME": "ci-queue-required"}) {
		return fmt.Errorf("queue controller must publish only its distinct protected context using the workflow token")
	}
	return nil
}

func queueNativeContextContract(raw []byte) error {
	var w struct {
		Jobs map[string]struct {
			Name string `yaml:"name"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &w); err != nil {
		return err
	}
	for id, job := range w.Jobs {
		for _, context := range []string{"ci-queue-required", "ci-required-next"} {
			if id == context || job.Name == context {
				return fmt.Errorf("native workflow job shadows trusted context %s", context)
			}
		}
	}
	return nil
}

func queueMarkerContract(raw []byte) error {
	var w queueWorkflowContract
	if err := yaml.Unmarshal(raw, &w); err != nil {
		return err
	}
	if w.Env.Kind != 0 || w.Defaults.Kind != 0 {
		return fmt.Errorf("group evidence cannot inherit initialization code or shell overrides")
	}
	j, ok := w.Jobs["merge-group-proof"]
	if !ok || j.If != "always() && !cancelled()" || j.Needs.Kind != yaml.ScalarNode || j.Needs.Value != "validation" || j.Continue.Kind != 0 || j.Env.Kind != 0 || j.Defaults.Kind != 0 || j.Container.Kind != 0 || j.Services.Kind != 0 || j.Strategy.Kind != 0 || len(j.Steps) != 1 {
		return fmt.Errorf("group evidence must depend on actual validation without masking failure")
	}
	s := j.Steps[0]
	if s.If != "" || s.Continue.Kind != 0 || s.Uses != "" || s.Shell != "" || s.Directory != "" || s.Run != `test "$VALIDATION_RESULT" = success` || !reflect.DeepEqual(s.Env, map[string]string{"VALIDATION_RESULT": "${{ needs.validation.result }}"}) {
		return fmt.Errorf("group evidence must reject all unsuccessful validation results")
	}
	return nil
}

func TestQueuePublisherWorkflowTrust(t *testing.T) {
	root := filepath.Join(repositoryRoot(t), ".github", "workflows")
	raw, err := os.ReadFile(filepath.Join(root, "ci-queue-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := queuePublisherContract(raw); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(root, "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := queueNativeContextContract(raw); err != nil {
			t.Fatalf("%s: %v", filepath.Base(path), err)
		}
	}
}

func TestQueuePublisherRejectsUnsafeWorkflowMutations(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "ci-queue-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(map[string]any){
		"unaudited container": func(w map[string]any) { queuePublisherJob(w)["container"] = "untrusted/image:latest" },
		"secret service execution": func(w map[string]any) {
			queuePublisherJob(w)["services"] = map[string]any{"untrusted": map[string]any{"image": "untrusted/image:latest", "env": map[string]any{"TOKEN": "${{ secrets.PR_TOKEN }}"}}}
		},
		"parallel publication": func(w map[string]any) {
			queuePublisherJob(w)["strategy"] = map[string]any{"matrix": map[string]any{"writer": []any{1, 2}}}
		},
		"PR checkout": func(w map[string]any) {
			queuePublisherStep(w, 0)["with"].(map[string]any)["ref"] = "${{ github.event.pull_request.head.sha }}"
		},
		"retained credentials": func(w map[string]any) {
			queuePublisherStep(w, 0)["with"].(map[string]any)["persist-credentials"] = true
		},
		"untrusted checkout repository": func(w map[string]any) {
			queuePublisherStep(w, 0)["with"].(map[string]any)["repository"] = "${{ github.event.pull_request.head.repo.full_name }}"
		},
		"unpinned checkout": func(w map[string]any) { queuePublisherStep(w, 0)["uses"] = "actions/checkout@main" },
		"PR secret execution": func(w map[string]any) {
			queuePublisherStep(w, 2)["run"] = "./untrusted-pr-script.sh"
			queuePublisherStep(w, 2)["env"].(map[string]any)["GH_TOKEN"] = "${{ secrets.PR_TOKEN }}"
		},
		"PR token secret": func(w map[string]any) {
			queuePublisherStep(w, 2)["env"].(map[string]any)["GH_TOKEN"] = "${{ secrets.PR_TOKEN }}"
		},
		"PR artifact import": func(w map[string]any) {
			queuePublisherStep(w, 1)["uses"] = "actions/download-artifact@0000000000000000000000000000000000000000"
		},
		"cache replay":             func(w map[string]any) { queuePublisherStep(w, 1)["with"].(map[string]any)["cache"] = true },
		"source write permissions": func(w map[string]any) { queuePublisherJob(w)["permissions"].(map[string]any)["contents"] = "write" },
		"PR write permissions": func(w map[string]any) {
			queuePublisherJob(w)["permissions"].(map[string]any)["pull-requests"] = "write"
		},
		"workflow write defaults": func(w map[string]any) { w["permissions"].(map[string]any)["contents"] = "write" },
		"legacy context reuse": func(w map[string]any) {
			queuePublisherStep(w, 2)["env"].(map[string]any)["PR_GATE_CHECK_NAME"] = "ci-required-next"
		},
		"missing queue mode":        func(w map[string]any) { queuePublisherStep(w, 2)["run"] = "go run ./scripts/ci/pr-gate" },
		"skip publication":          func(w map[string]any) { queuePublisherJob(w)["if"] = "false" },
		"skip controller":           func(w map[string]any) { queuePublisherStep(w, 2)["if"] = "false" },
		"masked controller failure": func(w map[string]any) { queuePublisherStep(w, 2)["continue-on-error"] = true },
		"masked job failure":        func(w map[string]any) { queuePublisherJob(w)["continue-on-error"] = true },
		"PR source environment": func(w map[string]any) {
			queuePublisherJob(w)["env"] = map[string]any{"GOTOOLCHAIN": "${{ github.event.pull_request.title }}"}
		},
		"untrusted working directory": func(w map[string]any) { queuePublisherStep(w, 2)["working-directory"] = "pr-source" },
		"reusable PR execution":       func(w map[string]any) { queuePublisherJob(w)["uses"] = "./.github/workflows/ci.yml" },
		"lost rerun callback": func(w map[string]any) {
			w["on"].(map[string]any)["workflow_run"].(map[string]any)["types"] = []any{"completed"}
		},
		"lost group callback": func(w map[string]any) {
			w["on"].(map[string]any)["workflow_run"].(map[string]any)["workflows"] = []any{"ci-feedback"}
		},
		"lost main reconciliation": func(w map[string]any) { w["on"].(map[string]any)["push"].(map[string]any)["branches"] = []any{"other"} },
		"lost head invalidation": func(w map[string]any) {
			w["on"].(map[string]any)["pull_request_target"].(map[string]any)["types"] = []any{"opened"}
		},
		"optional repair input": func(w map[string]any) {
			w["on"].(map[string]any)["workflow_dispatch"].(map[string]any)["inputs"].(map[string]any)["pr_number"].(map[string]any)["required"] = false
		},
		"lost serialization":  func(w map[string]any) { w["concurrency"].(map[string]any)["cancel-in-progress"] = true },
		"discarded callbacks": func(w map[string]any) { w["concurrency"].(map[string]any)["queue"] = "single" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := queueMutatedWorkflow(t, raw, mutate)
			if err := queuePublisherContract(changed); err == nil {
				t.Fatal("unsafe mutation of actual publisher YAML was accepted")
			}
		})
	}
	for _, key := range []string{"id", "name"} {
		t.Run("native context "+key, func(t *testing.T) {
			changed := queueMutatedWorkflow(t, raw, func(w map[string]any) {
				jobs := w["jobs"].(map[string]any)
				if key == "id" {
					jobs["ci-queue-required"] = jobs["publish"]
					delete(jobs, "publish")
				} else {
					queuePublisherJob(w)["name"] = "ci-queue-required"
				}
			})
			if err := queueNativeContextContract(changed); err == nil {
				t.Fatal("native shadow of trusted queue context was accepted")
			}
		})
	}
}

func TestQueueMarkerRejectsFailOpenWorkflowMutations(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "ci-merge-group.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := queueMarkerContract(raw); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"failure-masking shell": func(j map[string]any) { j["steps"].([]any)[0].(map[string]any)["shell"] = "bash {0} || true" },
		"untrusted Bash init":   func(j map[string]any) { j["env"] = map[string]any{"BASH_ENV": "./untrusted.sh"} },
		"failure-masking job defaults": func(j map[string]any) {
			j["defaults"] = map[string]any{"run": map[string]any{"shell": "bash {0} || true"}}
		},
		"untrusted marker directory":    func(j map[string]any) { j["steps"].([]any)[0].(map[string]any)["working-directory"] = "pr-source" },
		"masked marker":                 func(j map[string]any) { j["continue-on-error"] = true },
		"skipped marker":                func(j map[string]any) { j["if"] = "false" },
		"missing validation dependency": func(j map[string]any) { delete(j, "needs") },
		"skipped result assertion":      func(j map[string]any) { j["steps"].([]any)[0].(map[string]any)["if"] = "false" },
		"masked result assertion":       func(j map[string]any) { j["steps"].([]any)[0].(map[string]any)["continue-on-error"] = true },
		"forged validation success": func(j map[string]any) {
			j["steps"].([]any)[0].(map[string]any)["env"].(map[string]any)["VALIDATION_RESULT"] = "success"
		},
		"removed result assertion": func(j map[string]any) { j["steps"].([]any)[0].(map[string]any)["run"] = "true" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := queueMutatedWorkflow(t, raw, func(w map[string]any) { mutate(w["jobs"].(map[string]any)["merge-group-proof"].(map[string]any)) })
			if err := queueMarkerContract(changed); err == nil {
				t.Fatal("fail-open mutation of actual marker YAML was accepted")
			}
		})
	}
	for name, mutate := range map[string]func(map[string]any){
		"workflow Bash init": func(w map[string]any) { w["env"] = map[string]any{"BASH_ENV": "./untrusted.sh"} },
		"workflow shell defaults": func(w map[string]any) {
			w["defaults"] = map[string]any{"run": map[string]any{"shell": "bash {0} || true"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := queueMarkerContract(queueMutatedWorkflow(t, raw, mutate)); err == nil {
				t.Fatal("unsafe inherited marker execution override was accepted")
			}
		})
	}
}

func queueMutatedWorkflow(t *testing.T, raw []byte, mutate func(map[string]any)) []byte {
	t.Helper()
	var w map[string]any
	if err := yaml.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	mutate(w)
	changed, err := yaml.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	return changed
}

func queuePublisherJob(w map[string]any) map[string]any {
	return w["jobs"].(map[string]any)["publish"].(map[string]any)
}

func queuePublisherStep(w map[string]any, index int) map[string]any {
	return queuePublisherJob(w)["steps"].([]any)[index].(map[string]any)
}
