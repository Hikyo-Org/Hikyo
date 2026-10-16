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
