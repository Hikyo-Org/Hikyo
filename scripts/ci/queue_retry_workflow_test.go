package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
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
		If          string `yaml:"if"`
		Needs       string `yaml:"needs"`
		Timeout     int    `yaml:"timeout-minutes"`
		Concurrency struct {
			Group  string `yaml:"group"`
			Cancel bool   `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
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
	if len(w.Jobs) != 2 {
		t.Fatalf("the rerun job and its post-reset wait expected: %d", len(w.Jobs))
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
	job := func(id int, name, conclusion, failedStep string) string {
		step := conclusion
		if conclusion == "cancelled" { // On a timeout the step failed and the job was then cancelled.
			step = "failure"
		}
		return `{"id":` + strconv.Itoa(id) + `,"name":"` + name + `","conclusion":"` + conclusion + `","steps":[{"name":"Compile trusted publisher","conclusion":"success"},{"name":"` + failedStep + `","conclusion":"` + step + `"}]}`
	}
	// A pending job that a newer one replaced was cancelled before it started: no step ran.
	replaced := func(id int, name string) string {
		return `{"id":` + strconv.Itoa(id) + `,"name":"` + name + `","conclusion":"cancelled","steps":[]}`
	}
	head := strings.Repeat("a", 40)
	const healthy, spent = "4000 5000 1795475603", "40 5000 1795475603"
	const reconcile, groupReconcile = "Reconcile one authenticated head", "Reconcile complete group proof under its head lease"
	for _, tc := range []struct {
		name      string
		jobs      []string
		attempt   string
		rateLimit string
		rerun     []int
		fails     bool
		says      string
	}{
		{"queue publisher failed", []string{job(1, "publish-"+head, "failure", reconcile)}, "1", healthy, []int{1}, false, ""},
		{"dev publisher failed", []string{job(1, "dev-publish-"+head, "failure", "Reconcile exact-head dev admission")}, "2", healthy, []int{1}, false, ""},
		{"one failure among passes", []string{job(1, "publish-"+head, "success", reconcile), job(2, "publish-"+strings.Repeat("b", 40), "failure", reconcile)}, "1", healthy, []int{2}, false, ""},
		{"last attempt is never rerun", []string{job(1, "publish-"+head, "failure", reconcile)}, "3", healthy, nil, true, ""},
		{"publisher passed", []string{job(1, "publish-"+head, "success", reconcile)}, "1", healthy, nil, false, ""},
		{"no failed reconcile step", []string{job(1, "publish-"+head, "failure", "Capture the authenticated setup-inclusive admission budget")}, "1", healthy, nil, false, ""},
		{"publisher cancelled by its timeout", []string{job(1, "publish-"+head, "cancelled", reconcile)}, "1", healthy, []int{1}, false, ""},
		{"cancelled without a failed reconcile", []string{job(1, "publish-"+head, "cancelled", "Capture the authenticated setup-inclusive admission budget")}, "1", healthy, nil, false, ""},
		{"cancelled after reconcile passed", []string{`{"id":1,"name":"publish-` + head + `","conclusion":"cancelled","steps":[{"name":"` + reconcile + `","conclusion":"success"}]}`}, "1", healthy, nil, false, ""},
		{"worker group job shares the publisher name", []string{job(1, "publish-"+head, "failure", groupReconcile)}, "1", healthy, []int{1}, false, ""},
		{"failed group job is rerun", []string{job(1, "group-"+head, "failure", groupReconcile)}, "1", healthy, []int{1}, false, ""},
		{"passed group job is not rerun", []string{job(1, "group-"+head, "success", groupReconcile)}, "1", healthy, nil, false, ""},
		{"job name that is not a head is ignored", []string{job(1, "publish-not-a-sha", "failure", "Reconcile")}, "1", healthy, nil, false, ""},
		// gh run rerun --failed reruns cancelled jobs too, so only the failed one may be rerun, by id.
		{"replaced pending reconcile is not rerun", []string{replaced(1, "publish-"+head), job(2, "publish-"+head, "failure", reconcile), replaced(3, "group-"+head)}, "1", healthy, []int{2}, false, ""},
		{"only replaced pending reconciles", []string{replaced(1, "publish-"+head), replaced(2, "dev-publish-"+head)}, "1", healthy, nil, false, ""},
		{"nothing is rerun while the budget is spent", []string{job(1, "publish-"+head, "failure", reconcile)}, "1", spent, nil, false, "40 of 5000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			calls := filepath.Join(dir, "calls")
			rl := strings.Fields(tc.rateLimit)
			// The stub answers rate_limit and the jobs listing by applying the script's own --jq filter, and records reruns.
			stub := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >> '" + calls + "'\n" +
				"filter=\nfor a in \"$@\"; do if [ \"$prev\" = --jq ]; then filter=$a; fi; prev=$a; done\n" +
				"case \"$*\" in\n" +
				"  'api rate_limit '*) printf '%s' '{\"resources\":{\"core\":{\"remaining\":" + rl[0] + ",\"limit\":" + rl[1] + ",\"reset\":" + rl[2] + "}}}' | jq -r \"$filter\" ;;\n" +
				"  *--method\\ POST*) ;;\n" +
				"  api*) printf '%s' '{\"jobs\":[" + strings.Join(tc.jobs, ",") + "]}' | jq -r \"$filter\" ;;\n" +
				"esac\n"
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(stub), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "GH_TOKEN=x", "GH_REPO=Hikyo-Org/Hikyo", "RUN_ID=41595466545", "RUN_ATTEMPT="+tc.attempt, "GITHUB_OUTPUT="+filepath.Join(dir, "output"))
			out, err := cmd.CombinedOutput()
			if tc.fails != (err != nil) {
				t.Fatalf("exit mismatch (want failure=%t): %v\n%s", tc.fails, err, out)
			}
			recorded, _ := os.ReadFile(calls)
			var got []string
			for _, line := range strings.Split(string(recorded), "\n") {
				if strings.HasPrefix(line, "api --method POST ") {
					got = append(got, line)
				}
				if strings.Contains(line, "run rerun") {
					t.Fatalf("gh run rerun also reruns cancelled jobs; rerun by job id instead: %s", line)
				}
			}
			var want []string
			for _, id := range tc.rerun {
				want = append(want, "api --method POST repos/Hikyo-Org/Hikyo/actions/jobs/"+strconv.Itoa(id)+"/rerun")
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("reruns %v want %v\ncalls:\n%s\noutput:\n%s", got, want, recorded, out)
			}
			// Declining on a spent budget is what hands the run to the post-reset wait job.
			handed, _ := os.ReadFile(filepath.Join(dir, "output"))
			if (tc.says != "") != strings.Contains(string(handed), "exhausted=true") {
				t.Fatalf("exhausted=true must be set exactly when the rerun declines on a spent budget: %q", handed)
			}
			if tc.says != "" && !strings.Contains(string(out), tc.says) {
				t.Fatalf("output must say why nothing reran (%q):\n%s", tc.says, out)
			}
		})
	}
}

// GitHub may refuse a later per-job rerun after the first restarts the run. The
// refusal must not drop the remaining heads or hide itself.
func TestQueueRetryKeepsGoingWhenARerunIsRefused(t *testing.T) {
	script := readQueueRetryWorkflow(t).Jobs["rerun"].Steps[0].Run
	head := strings.Repeat("a", 40)
	failedJob := func(id int, name string) string {
		return `{"id":` + strconv.Itoa(id) + `,"name":"` + name + `","conclusion":"failure","steps":[{"name":"Reconcile one authenticated head","conclusion":"failure"}]}`
	}
	jobs := strings.Join([]string{failedJob(1, "publish-"+head), failedJob(2, "publish-"+strings.Repeat("b", 40)), failedJob(3, "group-"+strings.Repeat("c", 40))}, ",")
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	stub := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >> '" + calls + "'\n" +
		"filter=\nfor a in \"$@\"; do if [ \"$prev\" = --jq ]; then filter=$a; fi; prev=$a; done\n" +
		"case \"$*\" in\n" +
		"  'api rate_limit '*) printf '%s' '{\"resources\":{\"core\":{\"remaining\":4000,\"limit\":5000,\"reset\":1795475603}}}' | jq -r \"$filter\" ;;\n" +
		"  *jobs/2/rerun*) echo 'secret response text' >&2; exit 1 ;;\n" +
		"  *--method\\ POST*) ;;\n" +
		"  api*) printf '%s' '{\"jobs\":[" + jobs + "]}' | jq -r \"$filter\" ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "sleep"), []byte("#!/usr/bin/env bash\nprintf 'sleep %s\\n' \"$*\" >> '"+calls+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "GH_TOKEN=x", "GH_REPO=Hikyo-Org/Hikyo", "RUN_ID=41595466545", "RUN_ATTEMPT=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("a refused rerun must fail the step, not report success:\n%s", out)
	}
	recorded, _ := os.ReadFile(calls)
	if n := strings.Count(string(recorded), "jobs/2/rerun"); n != 3 {
		t.Fatalf("a refused rerun request is retried twice with backoff before it is reported: %d requests\n%s", n, recorded)
	}
	for _, id := range []string{"1", "3"} {
		if !strings.Contains(string(recorded), "api --method POST repos/Hikyo-Org/Hikyo/actions/jobs/"+id+"/rerun") {
			t.Fatalf("job %s was not rerun around the refusal:\n%s", id, recorded)
		}
	}
	if !strings.Contains(string(out), "Job 2 was not rerun") || strings.Contains(string(out), "secret response text") {
		t.Fatalf("the refused job must be named without GitHub's text:\n%s", out)
	}
}
