package ci_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// GitHub sends requested, in_progress and completed for a first attempt of a workflow
// run, but a re-run sends no requested ("The requested activity type does not occur
// when a workflow is re-run", events-that-trigger-workflows, workflow_run). The gates
// must reconcile at the start of every attempt, once: a first attempt on requested,
// a rerun on in_progress. Otherwise a rerun's earlier success keeps admitting a merge
// until the rerun completes, or a first attempt pays for two reconciles (#1553).
const attemptFilter = "(github.event.action != 'in_progress' || github.event.workflow_run.run_attempt > 1)"

type callbackGate struct {
	file string
	jobs []string // jobs that react to the feedback or group callback
}

func TestGateCallbacksFollowTheRealEventSequencePerAttempt(t *testing.T) {
	for _, gate := range []callbackGate{
		{"ci-queue-gate.yml", []string{"publish", "group"}},
		{"ci-dev-gate.yml", []string{"discover"}},
	} {
		t.Run(gate.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", gate.file))
			if err != nil {
				t.Fatal(err)
			}
			var w struct {
				On struct {
					WorkflowRun struct {
						Types []string `yaml:"types"`
					} `yaml:"workflow_run"`
				} `yaml:"on"`
				Jobs map[string]struct {
					If string `yaml:"if"`
				} `yaml:"jobs"`
			}
			if err := yaml.Unmarshal(raw, &w); err != nil {
				t.Fatal(err)
			}
			subscribed := func(action string) bool {
				for _, ty := range w.On.WorkflowRun.Types {
					if ty == action {
						return true
					}
				}
				return false
			}
			for _, name := range gate.jobs {
				// The model below is only the job's real filter, so pin that it is there verbatim.
				if !strings.Contains(strings.Join(strings.Fields(w.Jobs[name].If), " "), attemptFilter) {
					t.Fatalf("%s job %s lost the in_progress attempt filter:\n%s", gate.file, name, w.Jobs[name].If)
				}
			}
			handled := func(actions []string, attempt int) []string {
				var out []string
				for _, action := range actions {
					if subscribed(action) && (action != "in_progress" || attempt > 1) {
						out = append(out, action)
					}
				}
				return out
			}
			if got := handled([]string{"requested", "in_progress", "completed"}, 1); !reflect.DeepEqual(got, []string{"requested", "completed"}) {
				t.Fatalf("a first attempt must reconcile on requested and completed only, got %v", got)
			}
			if got := handled([]string{"in_progress", "completed"}, 2); !reflect.DeepEqual(got, []string{"in_progress", "completed"}) {
				t.Fatalf("a rerun sends no requested, so it must reconcile on in_progress and completed, got %v", got)
			}
		})
	}
}
