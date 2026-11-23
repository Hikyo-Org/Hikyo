package ci_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDevGateExecutesOnlyTrustedMainCodeUnderDistinctHeadLease(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-dev-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Name        string               `yaml:"name"`
		On          map[string]yaml.Node `yaml:"on"`
		Permissions map[string]string    `yaml:"permissions"`
		Jobs        map[string]struct {
			If          string            `yaml:"if"`
			Timeout     int               `yaml:"timeout-minutes"`
			Permissions map[string]string `yaml:"permissions"`
			Concurrency struct {
				Group  string `yaml:"group"`
				Cancel bool   `yaml:"cancel-in-progress"`
				Queue  string `yaml:"queue"`
			} `yaml:"concurrency"`
			Steps []struct {
				Name string            `yaml:"name"`
				Uses string            `yaml:"uses"`
				Run  string            `yaml:"run"`
				If   string            `yaml:"if"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if workflow.Name != "gate-dev" || !reflect.DeepEqual(workflow.Permissions, map[string]string{"contents": "read"}) || len(workflow.Jobs) != 2 {
		t.Fatal("dev publisher lost its read-only discovery/write-worker separation")
	}
	var target struct {
		Branches []string `yaml:"branches"`
	}
	pr := workflow.On["pull_request_target"]
	if err := pr.Decode(&target); err != nil || !reflect.DeepEqual(target.Branches, []string{"dev"}) {
		t.Fatal("dev target publisher must listen only to dev PRs")
	}
	discover, publish := workflow.Jobs["discover"], workflow.Jobs["publish"]
	if discover.If != "github.event_name != 'pull_request_target' || github.event.pull_request.base.ref == 'dev'" {
		t.Fatal("a stacked PR event must skip dev discovery instead of failing it, and every other event must still reach it")
	}
	if publish.If != "needs.discover.outputs.work != '[]'" {
		t.Fatal("dev workers must stay skipped when discovery is skipped or fails")
	}
	if !reflect.DeepEqual(discover.Permissions, map[string]string{"contents": "read", "pull-requests": "read"}) {
		t.Fatal("routing discovery must not write checks")
	}
	if !reflect.DeepEqual(publish.Permissions, map[string]string{"actions": "read", "contents": "read", "pull-requests": "read", "checks": "write"}) || publish.Timeout != 1 {
		t.Fatal("dev admission must retain narrow permissions and setup-inclusive budget")
	}
	if publish.Concurrency.Group != "dev-check-${{ github.repository_id }}-${{ matrix.work.head }}" || publish.Concurrency.Cancel || publish.Concurrency.Queue != "max" {
		t.Fatal("dev writers must serialize under their distinct exact-head lease")
	}
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.HasPrefix(step.Uses, "actions/checkout@") && (step.With["ref"] != "${{ github.event.repository.default_branch }}" || step.With["persist-credentials"] != "false") {
				t.Fatal("privileged dev publisher checked out candidate-controlled code")
			}
			if strings.HasPrefix(step.Uses, "actions/cache") {
				t.Fatal("fast dev admission added persistent cache storage")
			}
		}
	}
	if len(publish.Steps) != 4 || publish.Steps[1].With["sparse-checkout"] != "scripts/ci" || !strings.Contains(publish.Steps[0].Run, "PR_GATE_DEADLINE=$((started + 60))") ||
		!strings.Contains(publish.Steps[2].Run, "PR_GATE_DEADLINE - 15") || publish.Steps[3].If != "always()" ||
		!strings.Contains(publish.Steps[3].Run, "--dev --dev-pr") || !strings.Contains(publish.Steps[3].Run, `--expected-head "$HEAD"`) {
		t.Fatal("dev worker lost authenticated setup deadline, reserved settlement or typed producer mode")
	}
	if strings.Contains(string(raw), "uses: ./.github/workflows/ci.yml") || strings.Contains(string(raw), "secrets:") {
		t.Fatal("dev admission must not run the full graph or receive publication custody")
	}
}

// Observed on #1167 and #1173: GitHub filed each ci-dev-required record in the
// head's oldest Actions check suite and judged the required check only by the
// newest suite of each workflow. When gate-dev hosted the record, a gate-dev
// run for a title or body edit on the same head hid it, and the merge waited
// for an "expected" check until a new head arrived. Base retargets reach the
// publisher through ci-feedback-retarget, whose runs are named ci-feedback.
func TestDevGateDoesNotRerunOnPullRequestEdits(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-dev-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On map[string]yaml.Node `yaml:"on"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	var target struct {
		Types []string `yaml:"types"`
	}
	pr := workflow.On["pull_request_target"]
	if err := pr.Decode(&target); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(target.Types, []string{"opened", "synchronize", "reopened", "closed"}) {
		t.Fatalf("pull_request_target types %v: a PR edit would supersede the suite hosting ci-dev-required", target.Types)
	}
	var feedback struct {
		Workflows []string `yaml:"workflows"`
		Types     []string `yaml:"types"`
	}
	callback := workflow.On["workflow_run"]
	if err := callback.Decode(&feedback); err != nil || !reflect.DeepEqual(feedback.Workflows, []string{"ci-feedback"}) || !reflect.DeepEqual(feedback.Types, []string{"requested", "in_progress", "completed"}) {
		t.Fatal("a base retarget reaches dev admission only through the ci-feedback callback")
	}
}

// Observed on #1374: ci-dev-required sat in ci-feedback's opened suite, and a
// body edit started a skipped ci-feedback run whose newer suite hid it. Edits
// now start ci-feedback-retarget, which never runs when a head first gets
// suites, so it never hosts a record, and it calls ci-feedback only for a
// genuine base retarget.
func TestFeedbackRetargetRunsInItsOwnWorkflow(t *testing.T) {
	read := func(name string, out any) {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal(raw, out); err != nil {
			t.Fatal(err)
		}
	}
	var feedback struct {
		Name string               `yaml:"name"`
		On   map[string]yaml.Node `yaml:"on"`
	}
	read("ci-feedback.yml", &feedback)
	var opened struct {
		Types []string `yaml:"types"`
	}
	node := feedback.On["pull_request"]
	if err := node.Decode(&opened); err != nil || feedback.Name != "ci-feedback" || len(feedback.On) != 2 || !reflect.DeepEqual(opened.Types, []string{"opened", "synchronize", "reopened"}) {
		t.Fatalf("ci-feedback triggers %v: an edit would start a newer suite that hides a record it hosts", opened.Types)
	}
	if _, ok := feedback.On["workflow_call"]; !ok {
		t.Fatal("ci-feedback-retarget must be able to call ci-feedback")
	}
	var retarget struct {
		Name        string               `yaml:"name"`
		On          map[string]yaml.Node `yaml:"on"`
		Permissions map[string]string    `yaml:"permissions"`
		Jobs        map[string]struct {
			Name    string         `yaml:"name"`
			If      string         `yaml:"if"`
			Uses    string         `yaml:"uses"`
			Secrets any            `yaml:"secrets"`
			With    map[string]any `yaml:"with"`
		} `yaml:"jobs"`
	}
	read("ci-feedback-retarget.yml", &retarget)
	var edited struct {
		Types []string `yaml:"types"`
	}
	node = retarget.On["pull_request"]
	if err := node.Decode(&edited); err != nil || retarget.Name != "ci-feedback" || len(retarget.On) != 1 || !reflect.DeepEqual(edited.Types, []string{"edited"}) {
		t.Fatal("ci-feedback-retarget must run only on PR edits, under the name gate-dev and gate-queue call back on")
	}
	job, ok := retarget.Jobs["retarget"]
	// The gate reads lanes as "retarget / feedback (...)" and a skipped edit as one
	// "retarget" job, so the job must keep its key as its name.
	if len(retarget.Jobs) != 1 || !ok || job.Name != "" || job.If != "github.event.changes.base.ref.from != ''" || job.Uses != "./.github/workflows/ci-feedback.yml" || job.Secrets != nil || job.With != nil {
		t.Fatal("only a genuine base retarget may run ci-feedback, under the job name the gate reads")
	}
	if !reflect.DeepEqual(retarget.Permissions, map[string]string{"contents": "read"}) {
		t.Fatal("retarget feedback must stay read-only")
	}
}

// A workflow that runs when a head first gets suites can host a gate record, so
// it must not start again on that head while the PR is open. Each exception is
// a known residual: draft PRs skip automatic benchmarks until ready_for_review,
// and gate-queue still reassesses main PRs on edits.
func TestPullRequestWorkflowsDoNotRerunOnAnUnchangedHead(t *testing.T) {
	exceptions := map[string][]string{
		"pr-benchmark.yml":  {"ready_for_review"},
		"ci-queue-gate.yml": {"edited"},
	}
	paths, err := filepath.Glob(filepath.Join(repositoryRoot(t), ".github/workflows/*.yml"))
	yamlPaths, yamlErr := filepath.Glob(filepath.Join(repositoryRoot(t), ".github/workflows/*.yaml"))
	paths = append(paths, yamlPaths...)
	if err != nil || yamlErr != nil || len(paths) == 0 {
		t.Fatal("no workflows found")
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var workflow struct {
			On yaml.Node `yaml:"on"`
		}
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			t.Fatal(err)
		}
		var on map[string]yaml.Node
		if workflow.On.Kind != yaml.MappingNode || workflow.On.Decode(&on) != nil {
			continue
		}
		// Both events start suites of the same workflow, so judge their union.
		var kinds []string
		for _, event := range []string{"pull_request", "pull_request_target"} {
			node, ok := on[event]
			if !ok {
				continue
			}
			var trigger struct {
				Types []string `yaml:"types"`
			}
			if node.Kind == yaml.MappingNode {
				if err := node.Decode(&trigger); err != nil {
					t.Fatal(err)
				}
			}
			if len(trigger.Types) == 0 {
				trigger.Types = []string{"opened", "synchronize", "reopened"} // GitHub's defaults.
			}
			kinds = append(kinds, trigger.Types...)
		}
		if !slices.ContainsFunc(kinds, func(t string) bool { return t == "opened" || t == "synchronize" || t == "reopened" }) {
			continue
		}
		name := filepath.Base(path)
		for _, kind := range kinds {
			if kind != "opened" && kind != "synchronize" && kind != "reopened" && kind != "closed" && !slices.Contains(exceptions[name], kind) {
				t.Errorf("%s runs on %s: a newer suite on an unchanged head hides a gate record filed in its earlier suite", name, kind)
			}
		}
	}
}
