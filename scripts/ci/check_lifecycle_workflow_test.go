package ci_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

// Pin the only privileged canary route. It cannot accept a PR source, choose
// another check name, or silently skip the receipt needed for activation.
func checkLifecycleWorkflowContract(raw []byte) error {
	var w queueWorkflowContract
	if err := yaml.Unmarshal(raw, &w); err != nil {
		return err
	}
	var document map[string]yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return err
	}
	if len(document) != 5 || w.Name != "check-lifecycle-probe" || len(w.On) != 1 || !reflect.DeepEqual(w.Permissions, map[string]string{"contents": "read"}) {
		return fmt.Errorf("probe must expose only the fixed manual diagnostic with read-only defaults")
	}
	dispatch, ok := w.On["workflow_dispatch"]
	if !ok || dispatch.Tag != "!!null" {
		return fmt.Errorf("probe cannot accept arbitrary inputs or automatic PR events")
	}
	var lock map[string]yaml.Node
	node := document["concurrency"]
	if err := node.Decode(&lock); err != nil {
		return err
	}
	if len(lock) != 3 || w.Concurrency.Group != "check-lifecycle-probe-${{ github.repository_id }}" || w.Concurrency.Queue != "max" || lock["cancel-in-progress"].Tag != "!!bool" || lock["cancel-in-progress"].Value != "false" {
		return fmt.Errorf("probe requires an explicit isolated non-cancelling lock")
	}
	j, ok := w.Jobs["lifecycle-probe"]
	if !ok || len(w.Jobs) != 1 || j.Name != "lifecycle-probe" || j.If != "github.event_name == 'workflow_dispatch' && github.ref == format('refs/heads/{0}', github.event.repository.default_branch)" || j.RunsOn != "ubuntu-latest" || j.Timeout.Tag != "!!int" || j.Timeout.Value != "3" {
		return fmt.Errorf("probe must be a bounded job executable only on trusted main")
	}
	if j.Uses != "" || j.Needs.Kind != 0 || j.Continue.Kind != 0 || j.Env.Kind != 0 || j.Defaults.Kind != 0 || j.Container.Kind != 0 || j.Services.Kind != 0 || j.Strategy.Kind != 0 || j.Concurrency.Group != "" {
		return fmt.Errorf("probe cannot inherit execution overrides or another job")
	}
	if !reflect.DeepEqual(j.Permissions, map[string]string{"actions": "read", "checks": "write", "contents": "read"}) || len(j.Steps) != 4 {
		return fmt.Errorf("probe must retain minimal run authentication and check permissions")
	}
	checkout, setup, probe, receipt := j.Steps[0], j.Steps[1], j.Steps[2], j.Steps[3]
	if checkout.Uses != "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1" || !reflect.DeepEqual(checkout.With, map[string]string{"ref": "${{ github.sha }}", "persist-credentials": "false"}) || checkout.Run != "" || len(checkout.Env) != 0 {
		return fmt.Errorf("probe must use the exact trusted dispatch source without credentials")
	}
	if setup.Uses != "actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e" || !reflect.DeepEqual(setup.With, map[string]string{"go-version-file": "go.mod", "cache": "false"}) || setup.Run != "" || len(setup.Env) != 0 {
		return fmt.Errorf("probe must use pinned Go without importing execution caches")
	}
	if probe.Run != "go run ./scripts/ci/pr-gate --check-lifecycle-probe" || probe.Uses != "" || len(probe.With) != 0 || !reflect.DeepEqual(probe.Env, map[string]string{"GH_TOKEN": "${{ github.token }}", "GH_REPO": "${{ github.repository }}", "PR_GATE_PROBE_RECEIPT": "${{ runner.temp }}/check-lifecycle-probe.json"}) {
		return fmt.Errorf("probe must invoke only its fixed trusted mode with an owned receipt path")
	}
	if receipt.Uses != "actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9" || receipt.Run != "" || len(receipt.Env) != 0 || receipt.If != "always()" || !reflect.DeepEqual(receipt.With, map[string]string{"name": "check-lifecycle-probe-${{ github.run_id }}-${{ github.run_attempt }}", "path": "${{ runner.temp }}/check-lifecycle-probe.json", "if-no-files-found": "error", "retention-days": "7"}) {
		return fmt.Errorf("probe must preserve its exact run-attempt receipt even on failure")
	}
	for i, step := range j.Steps {
		if step.Continue.Kind != 0 || step.Shell != "" || step.Directory != "" || (i != 3 && step.If != "") {
			return fmt.Errorf("probe steps cannot skip, mask failures or override execution")
		}
	}
	return nil
}

func TestCheckLifecycleProbeWorkflowTrust(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-check-lifecycle-probe.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkLifecycleWorkflowContract(raw); err != nil {
		t.Fatal(err)
	}
}

func TestCheckLifecycleProbeWorkflowRejectsUnsafeMutations(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-check-lifecycle-probe.yml"))
	if err != nil {
		t.Fatal(err)
	}
	job := func(w map[string]any) map[string]any {
		return w["jobs"].(map[string]any)["lifecycle-probe"].(map[string]any)
	}
	step := func(w map[string]any, index int) map[string]any {
		return job(w)["steps"].([]any)[index].(map[string]any)
	}
	mutations := map[string]func(map[string]any){
		"PR event": func(w map[string]any) { w["on"].(map[string]any)["pull_request_target"] = nil },
		"arbitrary check input": func(w map[string]any) {
			w["on"].(map[string]any)["workflow_dispatch"] = map[string]any{"inputs": map[string]any{"name": map[string]any{"type": "string"}}}
		},
		"missing main guard": func(w map[string]any) { job(w)["if"] = "true" },
		"PR checkout": func(w map[string]any) {
			step(w, 0)["with"].(map[string]any)["ref"] = "${{ github.event.pull_request.head.sha }}"
		},
		"moving checkout":       func(w map[string]any) { step(w, 0)["with"].(map[string]any)["ref"] = "main" },
		"retained credentials":  func(w map[string]any) { step(w, 0)["with"].(map[string]any)["persist-credentials"] = true },
		"unpinned action":       func(w map[string]any) { step(w, 0)["uses"] = "actions/checkout@main" },
		"App token replacement": func(w map[string]any) { step(w, 2)["env"].(map[string]any)["GH_TOKEN"] = "${{ secrets.PR_TOKEN }}" },
		"required context mode": func(w map[string]any) { step(w, 2)["run"] = "go run ./scripts/ci/pr-gate --queue" },
		"arbitrary mode argument": func(w map[string]any) {
			step(w, 2)["run"] = "go run ./scripts/ci/pr-gate --check-lifecycle-probe --name ci-queue-required"
		},
		"skip probe":              func(w map[string]any) { step(w, 2)["if"] = "false" },
		"mask probe failure":      func(w map[string]any) { step(w, 2)["continue-on-error"] = true },
		"mask job failure":        func(w map[string]any) { job(w)["continue-on-error"] = true },
		"source write permission": func(w map[string]any) { job(w)["permissions"].(map[string]any)["contents"] = "write" },
		"dispatch permission":     func(w map[string]any) { job(w)["permissions"].(map[string]any)["actions"] = "write" },
		"execution environment":   func(w map[string]any) { job(w)["env"] = map[string]any{"BASH_ENV": "untrusted.sh"} },
		"step shell override":     func(w map[string]any) { step(w, 2)["shell"] = "bash --rcfile untrusted.sh {0}" },
		"foreign artifact import": func(w map[string]any) {
			step(w, 1)["uses"] = "actions/download-artifact@0000000000000000000000000000000000000000"
		},
		"cancelling cleanup":           func(w map[string]any) { w["concurrency"].(map[string]any)["cancel-in-progress"] = true },
		"missing cancellation setting": func(w map[string]any) { delete(w["concurrency"].(map[string]any), "cancel-in-progress") },
		"success-only receipt":         func(w map[string]any) { step(w, 3)["if"] = "success()" },
		"missing receipt accepted":     func(w map[string]any) { step(w, 3)["with"].(map[string]any)["if-no-files-found"] = "ignore" },
		"wrong attempt receipt": func(w map[string]any) {
			step(w, 3)["with"].(map[string]any)["name"] = "check-lifecycle-probe-${{ github.run_id }}"
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var w map[string]any
			if err := yaml.Unmarshal(raw, &w); err != nil {
				t.Fatal(err)
			}
			mutate(w)
			changed, err := yaml.Marshal(w)
			if err != nil {
				t.Fatal(err)
			}
			if err := checkLifecycleWorkflowContract(changed); err == nil {
				t.Fatal("unsafe lifecycle workflow accepted")
			}
		})
	}
}
