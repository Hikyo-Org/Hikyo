package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type queueRetryWorkflow struct {
	Name        string               `yaml:"name"`
	On          map[string]yaml.Node `yaml:"on"`
	Permissions map[string]string    `yaml:"permissions"`
	Concurrency struct {
		Group  string `yaml:"group"`
		Cancel bool   `yaml:"cancel-in-progress"`
	} `yaml:"concurrency"`
	Jobs map[string]struct {
		If          string            `yaml:"if"`
		Permissions map[string]string `yaml:"permissions"`
		Steps       []struct {
			Uses string `yaml:"uses"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func readQueueRetryWorkflow(t *testing.T) queueRetryWorkflow {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "ci-queue-retry.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var w queueRetryWorkflow
	if err := yaml.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	return w
}

func workflowName(t *testing.T, file string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", file))
	if err != nil {
		t.Fatal(err)
	}
	var w struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	return w.Name
}

// The rerun is the only repair that does not wait for the throttled cron, so it
// must stay a bounded, single-purpose, unprivileged-beyond-rerun workflow.
func TestQueueRetryWorkflowIsBoundedAndMinimal(t *testing.T) {
	w := readQueueRetryWorkflow(t)
	if w.Name != "gate-queue-retry" || len(w.On) != 1 || !reflect.DeepEqual(w.Permissions, map[string]string{"contents": "read"}) || w.Concurrency.Cancel {
		t.Fatalf("unexpected workflow shell: %+v", w)
	}
	var trigger struct {
		Workflows []string `yaml:"workflows"`
		Types     []string `yaml:"types"`
	}
	node := w.On["workflow_run"]
	if err := node.Decode(&trigger); err != nil {
		t.Fatal(err)
	}
	want := []string{workflowName(t, "ci-queue-gate.yml"), workflowName(t, "ci-queue-worker.yml"), workflowName(t, "ci-dev-gate.yml")}
	if !reflect.DeepEqual(trigger.Workflows, want) || !reflect.DeepEqual(trigger.Types, []string{"completed"}) {
		t.Fatalf("retry must follow exactly the three publishers and never itself: %+v want %v", trigger, want)
	}
	if len(w.Jobs) != 1 {
		t.Fatalf("one rerun job expected: %d", len(w.Jobs))
	}
	job := w.Jobs["rerun"]
	if !reflect.DeepEqual(job.Permissions, map[string]string{"actions": "write"}) {
		t.Fatalf("the rerun job may hold only actions: write: %+v", job.Permissions)
	}
	if len(job.Steps) != 1 || job.Steps[0].Uses != "" {
		t.Fatal("the rerun job must be one shell step that runs no repository code and checks nothing out")
	}
	// Pinned whole: substrings would pass with a group's parentheses gone. A branch can edit
	// these workflows, so the trigger is an allowlist on protected branches.
	const run = "github.event.workflow_run."
	wantIf := "(" + run + "conclusion == 'failure' || " + run + "conclusion == 'cancelled') && " +
		run + "run_attempt < 3 && " +
		"(" + run + "path == '.github/workflows/ci-queue-gate.yml' || " +
		run + "path == '.github/workflows/ci-queue-worker.yml' || " +
		run + "path == '.github/workflows/ci-dev-gate.yml') && " +
		"(" + run + "event == 'pull_request_target' || " +
		"((" + run + "event == 'push' || " + run + "event == 'workflow_run' || " +
		run + "event == 'workflow_dispatch' || " + run + "event == 'schedule') && " +
		run + "head_repository.full_name == github.repository && " +
		"(" + run + "head_branch == 'main' || " + run + "head_branch == 'dev')))"
	if got := strings.Join(strings.Fields(job.If), " "); got != wantIf {
		t.Fatalf("retry trigger changed:\n got %s\nwant %s", got, wantIf)
	}
	// Both loop bounds must equal the publisher's attempt cap.
	source, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "ci", "pr-gate", "queue_deadline.go"))
	if err != nil {
		t.Fatal(err)
	}
	cap := regexp.MustCompile(`const admissionMaxAttempts = (\d+)`).FindSubmatch(source)
	if cap == nil {
		t.Fatal("admissionMaxAttempts not found")
	}
	n := string(cap[1])
	if !strings.Contains(job.If, "github.event.workflow_run.conclusion == 'failure' || github.event.workflow_run.conclusion == 'cancelled'") || !strings.Contains(job.If, "github.event.workflow_run.run_attempt < "+n) || !strings.Contains(job.Steps[0].Run, `test "$RUN_ATTEMPT" -lt `+n) {
		t.Fatalf("attempt bound differs from admissionMaxAttempts=%s:\n%s\n%s", n, job.If, job.Steps[0].Run)
	}
}

func TestQueueRetryOnlyRerunsFailedHeadPublishers(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatal("the rerun step filters jobs with jq, which the runner image and this test need")
	}
	script := readQueueRetryWorkflow(t).Jobs["rerun"].Steps[0].Run
	job := func(name, conclusion, failedStep string) string {
		step := conclusion
		if conclusion == "cancelled" { // On a timeout the step failed and the job was then cancelled.
			step = "failure"
		}
		return `{"name":"` + name + `","conclusion":"` + conclusion + `","steps":[{"name":"Compile trusted publisher","conclusion":"success"},{"name":"` + failedStep + `","conclusion":"` + step + `"}]}`
	}
	head := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name    string
		jobs    []string
		attempt string
		rerun   bool
		fails   bool
	}{
		{"queue publisher failed", []string{job("publish-"+head, "failure", "Reconcile one authenticated head")}, "1", true, false},
		{"dev publisher failed", []string{job("dev-publish-"+head, "failure", "Reconcile exact-head dev admission")}, "2", true, false},
		{"one failure among passes", []string{job("publish-"+head, "success", "Reconcile one authenticated head"), job("publish-"+strings.Repeat("b", 40), "failure", "Reconcile one authenticated head")}, "1", true, false},
		{"last attempt is never rerun", []string{job("publish-"+head, "failure", "Reconcile one authenticated head")}, "3", false, true},
		{"publisher passed", []string{job("publish-"+head, "success", "Reconcile one authenticated head")}, "1", false, false},
		{"no failed reconcile step", []string{job("publish-"+head, "failure", "Capture the authenticated setup-inclusive admission budget")}, "1", false, false},
		{"publisher cancelled by its timeout", []string{job("publish-"+head, "cancelled", "Reconcile one authenticated head")}, "1", true, false},
		{"cancelled without a failed reconcile", []string{job("publish-"+head, "cancelled", "Capture the authenticated setup-inclusive admission budget")}, "1", false, false},
		{"cancelled after reconcile passed", []string{`{"name":"publish-` + head + `","conclusion":"cancelled","steps":[{"name":"Reconcile one authenticated head","conclusion":"success"}]}`}, "1", false, false},
		{"worker group job shares the publisher name", []string{job("publish-"+head, "failure", "Reconcile complete group proof under its head lease")}, "1", true, false},
		{"group job is not a head publisher", []string{job("group-"+head, "failure", "Reconcile complete group proof under its head lease")}, "1", false, false},
		{"job name that is not a head is ignored", []string{job("publish-not-a-sha", "failure", "Reconcile")}, "1", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			calls := filepath.Join(dir, "calls")
			// The stub answers the jobs listing and applies the script's own --jq filter to it.
			stub := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >> '" + calls + "'\n" +
				"if [ \"$1\" = api ]; then\n  while [ $# -gt 0 ]; do\n    if [ \"$1\" = --jq ]; then filter=$2; break; fi\n    shift\n  done\n" +
				"  printf '%s' '{\"jobs\":[" + strings.Join(tc.jobs, ",") + "]}' | jq \"$filter\"\nfi\n"
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(stub), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "GH_TOKEN=x", "GH_REPO=Hikyo-Org/Hikyo", "RUN_ID=41595466545", "RUN_ATTEMPT="+tc.attempt)
			out, err := cmd.CombinedOutput()
			if tc.fails != (err != nil) {
				t.Fatalf("exit mismatch (want failure=%t): %v\n%s", tc.fails, err, out)
			}
			recorded, _ := os.ReadFile(calls)
			if reran := strings.Contains(string(recorded), "run rerun 41595466545 --failed"); reran != tc.rerun {
				t.Fatalf("rerun=%t want %t\ncalls:\n%s\noutput:\n%s", reran, tc.rerun, recorded, out)
			}
		})
	}
}
