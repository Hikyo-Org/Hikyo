package ci_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

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
	Jobs map[string]queueWorkflowJob `yaml:"jobs"`
}

type queueWorkflowJob struct {
	Name        string    `yaml:"name"`
	Uses        string    `yaml:"uses"`
	If          string    `yaml:"if"`
	Needs       yaml.Node `yaml:"needs"`
	Continue    yaml.Node `yaml:"continue-on-error"`
	Env         yaml.Node `yaml:"env"`
	Defaults    yaml.Node `yaml:"defaults"`
	Container   yaml.Node `yaml:"container"`
	Services    yaml.Node `yaml:"services"`
	Strategy    yaml.Node `yaml:"strategy"`
	RunsOn      string    `yaml:"runs-on"`
	Timeout     yaml.Node `yaml:"timeout-minutes"`
	Concurrency struct {
		Group  string    `yaml:"group"`
		Cancel yaml.Node `yaml:"cancel-in-progress"`
		Queue  string    `yaml:"queue"`
	} `yaml:"concurrency"`
	Permissions map[string]string `yaml:"permissions"`
	Steps       []struct {
		Name      string            `yaml:"name"`
		Uses      string            `yaml:"uses"`
		Run       string            `yaml:"run"`
		If        string            `yaml:"if"`
		Continue  yaml.Node         `yaml:"continue-on-error"`
		Shell     string            `yaml:"shell"`
		Directory string            `yaml:"working-directory"`
		With      map[string]string `yaml:"with"`
		Env       map[string]string `yaml:"env"`
	} `yaml:"steps"`
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
	if !reflect.DeepEqual(w.Permissions, map[string]string{"contents": "read"}) || w.Concurrency.Group != "" || w.Concurrency.Cancel || w.Concurrency.Queue != "" {
		return fmt.Errorf("discovery must not hold a repository-global writer lock")
	}
	if len(w.Jobs) != 3 {
		return fmt.Errorf("queue publisher must retain direct admission/group and independent complete discovery")
	}
	head := "${{ github.event.pull_request.head.sha || github.event.workflow_run.head_sha }}"
	groupHead := "${{ github.event.workflow_run.head_sha }}"
	wantIf := map[string]string{
		"publish":  "vars.QUEUE_HEAD_WORKERS_ENABLED == 'true' && (github.event_name == 'pull_request_target' || (github.event_name == 'workflow_run' && github.event.workflow_run.name == 'ci-feedback'))",
		"group":    "vars.QUEUE_HEAD_WORKERS_ENABLED == 'true' && github.event_name == 'workflow_run' && github.event.workflow_run.name == 'trusted-merge-ci'",
		"dispatch": "vars.QUEUE_HEAD_WORKERS_ENABLED == 'true' && (github.event_name == 'push' || github.event_name == 'schedule' || github.event_name == 'workflow_dispatch')",
	}
	for _, id := range []string{"publish", "group", "dispatch"} {
		j, ok := w.Jobs[id]
		if !ok || j.If != wantIf[id] || j.Needs.Kind != 0 || j.Strategy.Kind != 0 {
			return fmt.Errorf("exact direct-head routing and independent bulk discovery are required")
		}
		if err := queueTrustedJob(j); err != nil {
			return err
		}
		budget := "10"
		permissions := map[string]string{"actions": "read", "checks": "write", "contents": "read", "pull-requests": "read"}
		jobHead, command := groupHead, `go run ./scripts/ci/pr-gate --queue --expected-head "$HEAD"`
		checkoutIndex, setupIndex, publishIndex := 0, 1, 2
		if id == "publish" {
			budget, jobHead = "1", head
			checkoutIndex, setupIndex, publishIndex = 1, -1, 3
			command = "test -x \"$RUNNER_TEMP/queue-publisher\"\n\"$RUNNER_TEMP/queue-publisher\" --queue --targeted --expected-head \"$HEAD\""
			if len(j.Steps) != 4 || j.Steps[3].If != "always()" {
				return fmt.Errorf("targeted setup/compile/terminal reserve is required")
			}
			if err := queueBootstrapContract(j.Steps[0].Run, true); err != nil {
				return err
			}
			if !reflect.DeepEqual(j.Steps[0].Env, map[string]string{"GH_TOKEN": "${{ github.token }}", "GH_REPO": "${{ github.repository }}", "HEAD": head}) {
				return fmt.Errorf("bootstrap must authenticate current exact head job")
			}
			if strings.TrimSpace(j.Steps[2].Run) != queueCompileCommand || j.Steps[2].Uses != "" || len(j.Steps[2].With) != 0 || len(j.Steps[2].Env) != 0 {
				return fmt.Errorf("compile must share the setup-inclusive45-second work cutoff")
			}
		} else if len(j.Steps) != 3 {
			return fmt.Errorf("group/discovery must retain only trusted setup and controller")
		}
		if id == "dispatch" {
			permissions = map[string]string{"actions": "write", "contents": "read", "pull-requests": "read"}
			command = "go run ./scripts/ci/pr-gate --queue --dispatch-workers"
			if j.Name != "" || j.Concurrency.Group != "" || j.Concurrency.Cancel.Kind != 0 || j.Concurrency.Queue != "" {
				return fmt.Errorf("read-only discovery cannot hold a head writer lock")
			}
		} else if j.Name != map[string]string{"publish": "publish-", "group": "group-"}[id]+jobHead || j.Concurrency.Group != "queue-check-${{ github.repository_id }}-"+jobHead || j.Concurrency.Cancel.Kind != yaml.ScalarNode || j.Concurrency.Cancel.Tag != "!!bool" || j.Concurrency.Cancel.Value != "false" || j.Concurrency.Queue != "max" {
			return fmt.Errorf("all check writers must share repository and exacthead FIFO lease without cancellation")
		}
		if j.Timeout.Kind != yaml.ScalarNode || j.Timeout.Tag != "!!int" || j.Timeout.Value != budget || !reflect.DeepEqual(j.Permissions, permissions) {
			return fmt.Errorf("writer/discovery budgets or minimumpermissions changed")
		}
		for i, step := range j.Steps {
			condition := ""
			if id == "publish" && i == publishIndex {
				condition = "always()"
			}
			if step.If != condition {
				return fmt.Errorf("publication/setup step was skipped or rerouted")
			}
		}
		if err := queueTrustedSetup(j, checkoutIndex, setupIndex); err != nil {
			return err
		}
		env := map[string]string{"GH_TOKEN": "${{ github.token }}", "GH_REPO": "${{ github.repository }}", "PR_GATE_CHECK_NAME": "ci-queue-required"}
		if id != "dispatch" {
			env["HEAD"] = jobHead
		}
		final := j.Steps[publishIndex]
		if final.Uses != "" || len(final.With) != 0 || strings.TrimSpace(final.Run) != command || !reflect.DeepEqual(final.Env, env) {
			return fmt.Errorf("trusted controller must receive exacthead ownership or pure dispatch mode")
		}
	}
	return nil
}

const queueCompileCommand = `set -euo pipefail
remaining=$((PR_GATE_DEADLINE - 15 - $(date +%s)))
test "$remaining" -gt 0
timeout --kill-after=1s "${remaining}s" bash scripts/ci/build-queue-publisher.sh`

func queueTrustedJob(j queueWorkflowJob) error {
	if j.Uses != "" || j.Continue.Kind != 0 || j.Env.Kind != 0 || j.Defaults.Kind != 0 || j.Container.Kind != 0 || j.Services.Kind != 0 || j.RunsOn != "ubuntu-latest" {
		return fmt.Errorf("publisher execution boundary was overridden")
	}
	for _, s := range j.Steps {
		if s.Continue.Kind != 0 || s.Shell != "" || s.Directory != "" {
			return fmt.Errorf("publication cannot maskfailure or inherit untrusted execution")
		}
	}
	return nil
}
func queueTrustedSetup(j queueWorkflowJob, checkoutIndex, setupIndex int) error {
	checkout := j.Steps[checkoutIndex]
	if checkout.Uses != "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1" || checkout.Run != "" || len(checkout.Env) != 0 || !reflect.DeepEqual(checkout.With, map[string]string{"ref": "${{ github.event.repository.default_branch }}", "persist-credentials": "false"}) {
		return fmt.Errorf("publisher must checkout pinned trusted main withoutcredentials")
	}
	if setupIndex < 0 {
		return nil
	}
	setup := j.Steps[setupIndex]
	if setup.Uses != "actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e" || setup.Run != "" || len(setup.Env) != 0 || !reflect.DeepEqual(setup.With, map[string]string{"go-version-file": "go.mod", "cache": "false"}) {
		return fmt.Errorf("publisher must retain pinned trusted Go withoutPRcache")
	}
	return nil
}
func queueBootstrapContract(run string, targeted bool) error {
	want := "8035f5279452ed5df0f126e798a1e2be88f56c92f8872629450ad62d4d343dc7"
	if targeted {
		want = "4c6102d39aab61e3ca574bc277f3453214989a2cb505b699bb3aa26a128aa8a6"
	}
	if fmt.Sprintf("%x", sha256.Sum256([]byte(run))) != want {
		return fmt.Errorf("current-attempt exacthead clock bootstrap changed")
	}
	return nil
}
func queueWorkerContract(raw []byte) error {
	var w queueWorkflowContract
	if err := yaml.Unmarshal(raw, &w); err != nil {
		return err
	}
	if w.Name != "queue-check-worker" || len(w.On) != 1 || w.Env.Kind != 0 || w.Defaults.Kind != 0 || w.Concurrency.Group != "" || w.Concurrency.Cancel || w.Concurrency.Queue != "" || !reflect.DeepEqual(w.Permissions, map[string]string{"contents": "read"}) {
		return fmt.Errorf("headworker must use only trusted manual batch trigger withoutglobal lease")
	}
	var trigger struct {
		Inputs map[string]struct {
			Required bool   `yaml:"required"`
			Type     string `yaml:"type"`
		} `yaml:"inputs"`
	}
	node, ok := w.On["workflow_dispatch"]
	if !ok {
		return fmt.Errorf("worker dispatch missing")
	}
	if err := node.Decode(&trigger); err != nil {
		return err
	}
	if len(trigger.Inputs) != 1 || !trigger.Inputs["work"].Required || trigger.Inputs["work"].Type != "string" {
		return fmt.Errorf("worker accepts one typed batch routing input")
	}
	j, ok := w.Jobs["publish"]
	if !ok || len(w.Jobs) != 1 || j.If != "vars.QUEUE_HEAD_WORKERS_ENABLED == 'true'" || j.Needs.Kind != 0 || len(j.Steps) != 5 {
		return fmt.Errorf("all worker jobs must obey cutoverhold")
	}
	if err := queueTrustedJob(j); err != nil {
		return err
	}
	head := "${{ matrix.work.head }}"
	if j.Name != "publish-"+head || j.Concurrency.Group != "queue-check-${{ github.repository_id }}-"+head || j.Concurrency.Cancel.Kind != yaml.ScalarNode || j.Concurrency.Cancel.Tag != "!!bool" || j.Concurrency.Cancel.Value != "false" || j.Concurrency.Queue != "max" || j.Timeout.Value != "${{ matrix.work.kind == 'group' && 10 || 1 }}" || !reflect.DeepEqual(j.Permissions, map[string]string{"actions": "read", "checks": "write", "contents": "read", "pull-requests": "read"}) {
		return fmt.Errorf("headworker must share exactSHA lease/minimalpermissions and independentgroupbudget")
	}
	var strategy map[string]any
	if err := j.Strategy.Decode(&strategy); err != nil {
		return err
	}
	if !reflect.DeepEqual(strategy, map[string]any{"fail-fast": false, "matrix": map[string]any{"work": "${{ fromJSON(inputs.work) }}"}}) {
		return fmt.Errorf("complete worker batch must expand exactlyonce withfailfastfalse")
	}
	if err := queueBootstrapContract(j.Steps[0].Run, false); err != nil {
		return err
	}
	if j.Steps[0].If != "matrix.work.kind != 'group'" || !reflect.DeepEqual(j.Steps[0].Env, map[string]string{"GH_TOKEN": "${{ github.token }}", "GH_REPO": "${{ github.repository }}", "HEAD": head}) {
		return fmt.Errorf("worker admission clock cannot cutgroup budget")
	}
	if err := queueTrustedSetup(j, 1, 2); err != nil {
		return err
	}
	for i, step := range j.Steps {
		if i > 0 && i < 4 && step.If != map[int]string{2: "matrix.work.kind == 'group'"}[i] {
			return fmt.Errorf("worker setup/compile cannot be skipped")
		}
	}
	wantCompile := `set -euo pipefail
if [ "$KIND" = group ]; then
  go build -o "$RUNNER_TEMP/queue-publisher" ./scripts/ci/pr-gate
else
  remaining=$((PR_GATE_DEADLINE - 15 - $(date +%s)))
  test "$remaining" -gt 0
  timeout --kill-after=1s "${remaining}s" bash scripts/ci/build-queue-publisher.sh
fi`
	if strings.TrimSpace(j.Steps[3].Run) != wantCompile || j.Steps[3].Uses != "" || len(j.Steps[3].With) != 0 || !reflect.DeepEqual(j.Steps[3].Env, map[string]string{"KIND": "${{ matrix.work.kind }}"}) {
		return fmt.Errorf("worker compile must remain source-bound to sharedbudget")
	}
	final := j.Steps[4]
	if final.If != "always()" || final.Uses != "" || len(final.With) != 0 || strings.TrimSpace(final.Run) != "test -x \"$RUNNER_TEMP/queue-publisher\"\n\"$RUNNER_TEMP/queue-publisher\" --queue --expected-head \"$HEAD\" --head-worker \"$KIND\"" || !reflect.DeepEqual(final.Env, map[string]string{"GH_TOKEN": "${{ github.token }}", "GH_REPO": "${{ github.repository }}", "PR_GATE_CHECK_NAME": "ci-queue-required", "HEAD": head, "KIND": "${{ matrix.work.kind }}"}) {
		return fmt.Errorf("worker controller must own exactlyitsinputhead withoutPRshellcode")
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
	worker, err := os.ReadFile(filepath.Join(root, "ci-queue-worker.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := queueWorkerContract(worker); err != nil {
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
		"extended target budget": func(w map[string]any) { queuePublisherJob(w)["timeout-minutes"] = 10 },
		"group budget truncated": func(w map[string]any) {
			w["jobs"].(map[string]any)["group"].(map[string]any)["timeout-minutes"] = 1
		},
		"repair skipped after target failure": func(w map[string]any) { w["jobs"].(map[string]any)["group"].(map[string]any)["if"] = "success()" },
		"group depends on unrelated target":   func(w map[string]any) { w["jobs"].(map[string]any)["group"].(map[string]any)["needs"] = "publish" },
		"setup timer reset": func(w map[string]any) {
			queuePublisherJob(w)["steps"].([]any)[0].(map[string]any)["run"] = "echo PR_GATE_DEADLINE=$(($(date +%s) + 60)) >> $GITHUB_ENV"
		},
		"unbounded compiler": func(w map[string]any) {
			queuePublisherJob(w)["steps"].([]any)[2].(map[string]any)["run"] = "go build -o $RUNNER_TEMP/queue-publisher ./scripts/ci/pr-gate"
		},
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
		"cache replay":             func(w map[string]any) { queuePublisherStep(w, 1)["with"] = map[string]any{"cache": true} },
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
		"lost serialization": func(w map[string]any) {
			queuePublisherJob(w)["concurrency"].(map[string]any)["cancel-in-progress"] = true
		},
		"discarded callbacks": func(w map[string]any) { queuePublisherJob(w)["concurrency"].(map[string]any)["queue"] = "single" },
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
	return queuePublisherJob(w)["steps"].([]any)[[]int{1, 2, 3}[index]].(map[string]any)
}

func TestQueuePublisherBootstrapAuthenticatesSetupClock(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "ci-queue-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow queueWorkflowContract
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	script := workflow.Jobs["publish"].Steps[0].Run
	head := "8d0fe8559114c4a0d8eb95c17443ef2be1c78071"
	source := "f1022b9c6a6b95c98a59ac98cb2865b36d4438bb"
	groupDisplay := strings.ReplaceAll(workflow.Jobs["group"].Name, "${{ github.event.workflow_run.head_sha }}", head)
	base := map[string]any{"id": 123, "run_id": 456, "run_attempt": 2, "head_sha": head, "name": "publish-" + head, "status": "in_progress", "started_at": time.Now().Add(-5 * time.Second).UTC().Format(time.RFC3339)}
	cases := map[string]func(map[string]any) any{
		"valid": func(j map[string]any) any { return nil },
		"valid with skipped group": func(j map[string]any) any {
			return map[string]any{"total_count": 2, "jobs": []any{j, map[string]any{"id": 124, "run_id": 456, "run_attempt": 2, "head_sha": head, "name": groupDisplay, "status": "completed", "conclusion": "skipped", "started_at": nil}}}
		},
		"wrong run":               func(j map[string]any) any { j["run_id"] = 999; return nil },
		"wrong attempt":           func(j map[string]any) any { j["run_attempt"] = 1; return nil },
		"wrong source":            func(j map[string]any) any { j["head_sha"] = strings.Repeat("b", 40); return nil },
		"wrong job":               func(j map[string]any) any { j["name"] = "maintenance"; return nil },
		"invalid id type":         func(j map[string]any) any { j["id"] = "not an integer"; return nil },
		"fractional id":           func(j map[string]any) any { j["id"] = 123.5; return nil },
		"valid queued status lag": func(j map[string]any) any { j["status"] = "queued"; return nil },
		"completed status":        func(j map[string]any) any { j["status"] = "completed"; return nil },
		"missing start":           func(j map[string]any) any { delete(j, "started_at"); return nil },
		"malformed start":         func(j map[string]any) any { j["started_at"] = "invalid"; return nil },
		"future start": func(j map[string]any) any {
			j["started_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
			return nil
		},
		"expired start": func(j map[string]any) any {
			j["started_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			return nil
		},
		"duplicate matching job": func(j map[string]any) any { return map[string]any{"total_count": 2, "jobs": []any{j, j}} },
		"incomplete page":        func(j map[string]any) any { return map[string]any{"total_count": 2, "jobs": []any{j}} },
		"invalid jobs shape":     func(j map[string]any) any { return map[string]any{"total_count": 1, "jobs": j} },
		"empty response":         func(j map[string]any) any { return "" },
		"JSON stream":            func(j map[string]any) any { return "{}\n{}" },
		"missing lease":          func(j map[string]any) any { return nil },
		"malformed lease":        func(j map[string]any) any { return nil },
		"missing source":         func(j map[string]any) any { return nil },
		"malformed source":       func(j map[string]any) any { return nil },
		"unsupported event":      func(j map[string]any) any { return nil },
	}
	// These are the distinct source/job identities observed on PR 899. The
	// target job belongs to the PR head; a feedback callback belongs to main,
	// while both use the PR head for the same publication lease.
	for _, route := range []struct{ event, jobHead string }{
		{"pull_request_target", head},
		{"workflow_run", source},
	} {
		for name, mutate := range cases {
			t.Run(route.event+"/"+name, func(t *testing.T) {
				j := make(map[string]any, len(base))
				for k, v := range base {
					j[k] = v
				}
				j["head_sha"] = route.jobHead
				response := mutate(j)
				if response == nil {
					response = map[string]any{"total_count": 1, "jobs": []any{j}}
				}
				body, err := json.Marshal(response)
				if err != nil {
					t.Fatal(err)
				}
				if text, ok := response.(string); ok {
					body = []byte(text)
				}
				dir := t.TempDir()
				responsePath := filepath.Join(dir, "response.json")
				envPath := filepath.Join(dir, "environment")
				if err := os.WriteFile(responsePath, body, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(envPath, nil, 0600); err != nil {
					t.Fatal(err)
				}
				shim := "#!/bin/sh\nset -eu\ntest \"$#\" = 2\ntest \"$1\" = api\ntest \"$2\" = 'repos/Hikyo-Org/Hikyo/actions/runs/456/attempts/2/jobs?per_page=100'\ncat \"$RESPONSE_FILE\"\n"
				if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(shim), 0700); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "bash", "-c", script)
				cmd.WaitDelay = time.Second
				lease, sourceSHA, event := head, source, route.event
				switch name {
				case "missing lease":
					lease = ""
				case "malformed lease":
					lease = "not-a-sha"
				case "missing source":
					sourceSHA = ""
				case "malformed source":
					sourceSHA = "not-a-sha"
				case "unsupported event":
					event = "workflow_dispatch"
				}
				cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "GH_REPO=Hikyo-Org/Hikyo", "GITHUB_RUN_ID=456", "GITHUB_RUN_ATTEMPT=2", "GITHUB_EVENT_NAME="+event, "GITHUB_SHA="+sourceSHA, "HEAD="+lease, "GITHUB_ENV="+envPath, "RESPONSE_FILE="+responsePath)
				output, runErr := cmd.CombinedOutput()
				written, err := os.ReadFile(envPath)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.HasPrefix(name, "valid") && !(route.event == "pull_request_target" && (name == "missing source" || name == "malformed source")) {
					if runErr == nil || len(written) != 0 {
						t.Fatalf("invalid server clock reached setup: %v %s %s", runErr, output, written)
					}
					return
				}
				if runErr != nil {
					t.Fatalf("valid authentic setup clock refused: %v %s", runErr, output)
				}
				started, err := time.Parse(time.RFC3339, j["started_at"].(string))
				if err != nil {
					t.Fatal(err)
				}
				want := "PR_GATE_STARTED_AT=" + strconv.FormatInt(started.Unix(), 10) + "\nPR_GATE_DEADLINE=" + strconv.FormatInt(started.Unix()+60, 10) + "\n"
				if string(written) != want {
					t.Fatalf("setup clock was reset: %s", written)
				}
			})
		}
	}
}

func TestQueueHeadWorkerRejectsUnsafeWorkflowMutations(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "ci-queue-worker.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := queueWorkerContract(raw); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"lostheadlease": func(w map[string]any) { delete(queuePublisherJob(w), "concurrency") },
		"modeisolatedlease": func(w map[string]any) {
			queuePublisherJob(w)["concurrency"].(map[string]any)["group"] = "queue-${{ matrix.work.kind }}-${{ matrix.work.head }}"
		},
		"quotedcancel": func(w map[string]any) {
			queuePublisherJob(w)["concurrency"].(map[string]any)["cancel-in-progress"] = "false"
		},
		"cancelrunning": func(w map[string]any) {
			queuePublisherJob(w)["concurrency"].(map[string]any)["cancel-in-progress"] = true
		},
		"coalescingdropspending": func(w map[string]any) { queuePublisherJob(w)["concurrency"].(map[string]any)["queue"] = "single" },
		"failfastdefault":        func(w map[string]any) { delete(queuePublisherJob(w)["strategy"].(map[string]any), "fail-fast") },
		"excluderevocations": func(w map[string]any) {
			queuePublisherJob(w)["strategy"].(map[string]any)["matrix"].(map[string]any)["exclude"] = []any{map[string]any{"kind": "admission"}}
		},
		"duplicateexpansion": func(w map[string]any) {
			queuePublisherJob(w)["strategy"].(map[string]any)["matrix"].(map[string]any)["role"] = []any{"PR", "group"}
		},
		"bypasscutover": func(w map[string]any) { queuePublisherJob(w)["if"] = "true" },
		"workerdispatchrecursion": func(w map[string]any) {
			w["on"].(map[string]any)["workflow_run"] = map[string]any{"workflows": []any{"queue-check-worker"}}
		},
		"writerepository": func(w map[string]any) { queuePublisherJob(w)["permissions"].(map[string]any)["contents"] = "write" },
		"writeworkflows":  func(w map[string]any) { queuePublisherJob(w)["permissions"].(map[string]any)["actions"] = "write" },
		"skipworker":      func(w map[string]any) { queuePublisherStep(w, 2)["if"] = "false" },
		"failopenworker":  func(w map[string]any) { queuePublisherStep(w, 2)["continue-on-error"] = true },
		"PRcheckout": func(w map[string]any) {
			queuePublisherStep(w, 0)["with"].(map[string]any)["ref"] = "${{ github.event.inputs.head }}"
		},
		"missingheadowner":        func(w map[string]any) { queuePublisherStep(w, 2)["run"] = "go run ./scripts/ci/pr-gate --queue" },
		"truncatefullgroupbudget": func(w map[string]any) { queuePublisherJob(w)["timeout-minutes"] = 1 },
		"resetsetupclock": func(w map[string]any) {
			queuePublisherJob(w)["steps"].([]any)[0].(map[string]any)["run"] = "date +%s >> $GITHUB_ENV"
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := queueWorkerContract(queueMutatedWorkflow(t, raw, mutate)); err == nil {
				t.Fatal("unsafe actual headworker YAML accepted")
			}
		})
	}
}
