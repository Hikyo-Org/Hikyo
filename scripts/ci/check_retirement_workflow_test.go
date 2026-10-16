package ci_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func reporterRepairWorkflowContract(raw []byte) error {
	var w queueWorkflowContract
	if err := yaml.Unmarshal(raw, &w); err != nil {
		return err
	}
	discovery, ok := w.Jobs["discover"]
	if !ok || !reflect.DeepEqual(discovery.Permissions, map[string]string{"actions": "read", "checks": "read", "contents": "read"}) {
		return fmt.Errorf("reporter discovery must remain read-only")
	}
	j, ok := w.Jobs["reporter"]
	if !ok || j.Needs.Value != "discover" || strings.TrimSpace(j.If) != "!cancelled() && needs.discover.result == 'success' && needs.discover.outputs.reporter_targets != '[]'" || len(j.Steps) != 3 {
		return fmt.Errorf("repair requires successful nonempty discovery")
	}
	if err := queueTrustedJob(j); err != nil {
		return err
	}
	if err := queueTrustedSetup(j, 0, 1); err != nil {
		return err
	}
	if j.Concurrency.Group != "merge-queue-report-${{ matrix.target.run }}-${{ matrix.target.attempt }}" || j.Concurrency.Cancel.Value != "false" || j.Concurrency.Queue != "max" {
		return fmt.Errorf("repair must share the callback's run/attempt lease without cancellation")
	}
	if !reflect.DeepEqual(j.Permissions, map[string]string{"actions": "read", "checks": "write", "contents": "read", "pull-requests": "read"}) || j.Timeout.Value != "5" {
		return fmt.Errorf("repair permissions and duration must remain bounded")
	}
	step := j.Steps[2]
	if step.Run != `go run ./scripts/ci/queue-report --repair-work "$WORK"` || !reflect.DeepEqual(step.Env, map[string]string{"GH_TOKEN": "${{ github.token }}", "GH_REPO": "${{ github.repository }}", "WORK": "${{ toJSON(matrix.target) }}"}) {
		return fmt.Errorf("repair must pass the discovered work as data to its authenticating owner")
	}
	return nil
}

func TestObservationRetirementBatchCompletesEveryIndependentHead(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "ci-check-retirement.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var w queueWorkflowContract
	if err := yaml.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	job := w.Jobs["observation"]
	if job.Concurrency.Group != "pr-gate-publisher" || job.Concurrency.Cancel.Value != "false" || job.Timeout.Value != "5" {
		t.Fatal("batch must retain its global writer lease and five-minute job bound")
	}
	heads := make([]string, 10)
	for i := range heads {
		heads[i] = fmt.Sprintf("%040x", i+1)
	}
	for _, tc := range []struct {
		name      string
		heads     []string
		fail      string
		wantError bool
		wantCalls int
	}{
		{"all succeed", heads, "", false, 10},
		{"first head fails", heads, heads[0], true, 10},
		{"last head fails", heads, heads[9], true, 10},
		{"duplicates refused before launch", []string{heads[0], heads[0]}, "", true, 0},
		{"oversize refused before launch", append(append([]string{}, heads...), fmt.Sprintf("%040x", 11)), "", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fake := `#!/usr/bin/env bash
set -euo pipefail
test "$1" = --retire-checks
test "$2" = --expected-head
test "$3" = "$PR_GATE_RETIREMENT_HEAD"
head=$3
touch "$RUNNER_TEMP/$head.started"
# All siblings must start before any finishes, proving bounded parallelism.
ready=false
for ((attempt=0; attempt<100; attempt++)); do
  started=("$RUNNER_TEMP"/*.started)
  if [ "${#started[@]}" -eq "$EXPECTED" ]; then ready=true; break; fi
  sleep 0.01
done
test "$ready" = true
touch "$RUNNER_TEMP/$head.finished"
test "$head" != "$FAIL_HEAD"
`
			if err := os.WriteFile(filepath.Join(dir, "check-retirement"), []byte(fake), 0700); err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(tc.heads)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash", "-c", job.Steps[3].Run)
			cmd.Env = append(os.Environ(), "RUNNER_TEMP="+dir, "HEADS="+string(payload), "EXPECTED=10", "FAIL_HEAD="+tc.fail)
			output, err := cmd.CombinedOutput()
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v output=%s", err, output)
			}
			for _, suffix := range []string{"started", "finished"} {
				files, err := filepath.Glob(filepath.Join(dir, "*."+suffix))
				if err != nil || len(files) != tc.wantCalls {
					t.Fatalf("%s count=%d want=%d error=%v output=%s", suffix, len(files), tc.wantCalls, err, output)
				}
			}
			if tc.fail != "" && !strings.Contains(string(output), "Retirement failed for: "+tc.fail) {
				t.Fatalf("failed head missing from aggregate: %s", output)
			}
		})
	}
}

func TestReporterRepairWorkflowTrustBoundary(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "ci-check-retirement.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reporterRepairWorkflowContract(raw); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(map[string]any){
		"discovery can mutate": func(w map[string]any) {
			w["jobs"].(map[string]any)["discover"].(map[string]any)["permissions"].(map[string]any)["checks"] = "write"
		},
		"foreign lease": func(w map[string]any) {
			w["jobs"].(map[string]any)["reporter"].(map[string]any)["concurrency"].(map[string]any)["group"] = "independent-repair"
		},
		"cancel active publisher": func(w map[string]any) {
			w["jobs"].(map[string]any)["reporter"].(map[string]any)["concurrency"].(map[string]any)["cancel-in-progress"] = true
		},
		"PR source checkout": func(w map[string]any) {
			w["jobs"].(map[string]any)["reporter"].(map[string]any)["steps"].([]any)[0].(map[string]any)["with"].(map[string]any)["ref"] = "${{ github.event.pull_request.head.sha }}"
		},
		"missing discovery gate": func(w map[string]any) {
			w["jobs"].(map[string]any)["reporter"].(map[string]any)["if"] = "always()"
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := reporterRepairWorkflowContract(queueMutatedWorkflow(t, raw, change)); err == nil {
				t.Fatal("unsafe reporter repair workflow accepted")
			}
		})
	}
}
