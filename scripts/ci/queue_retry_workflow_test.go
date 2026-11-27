package ci_test

import (
	"fmt"
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
	const runLevel = 0 // the run-level endpoint rather than a job id
	manyPublishers := func(n int) []string {
		var jobs []string
		for i := 0; i < n; i++ {
			jobs = append(jobs, job(i+1, "publish-"+fmt.Sprintf("%040x", i+1), "failure", "Reconcile one authenticated head"))
		}
		return jobs
	}
	const healthy, spent = "4000 5000 1795475603", "40 5000 1795475603"
	const reconcile, groupReconcile = "Reconcile one authenticated head", "Reconcile complete group proof under its head lease"
	// The worker names its jobs by kind and runs one step for every kind.
	const workerReconcile = "Reconcile one authenticated head without borrowing another lease"
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
		// dev-revoke is never rerun by the retry: rerunning a job reruns every job that needs it, and every dev publisher does.
		{"a failed revoke alone reruns nothing", []string{job(1, "dev-revoke", "failure", "Block every routed head until its reconciliation finishes")}, "1", healthy, nil, false, ""},
		{"a cancelled revoke reruns nothing", []string{job(1, "dev-revoke", "cancelled", "Block every routed head until its reconciliation finishes")}, "1", healthy, nil, false, ""},
		{"a failed revoke keeps the failed publishers on job-level reruns", []string{job(1, "dev-revoke", "cancelled", "Block every routed head until its reconciliation finishes"), job(2, "dev-publish-"+head, "failure", "Reconcile exact-head dev admission"), job(3, "dev-publish-"+strings.Repeat("b", 40), "failure", "Reconcile exact-head dev admission"), job(4, "dev-publish-"+strings.Repeat("c", 40), "failure", "Reconcile exact-head dev admission")}, "1", healthy, []int{2}, false, ""},
		{"a failed revoke beside one failed publisher reruns only the publisher", []string{job(1, "dev-revoke", "failure", "Block every routed head until its reconciliation finishes"), job(2, "dev-publish-"+head, "failure", "Reconcile exact-head dev admission")}, "1", healthy, []int{2}, false, ""},
		{"only replaced pending reconciles", []string{replaced(1, "publish-"+head), replaced(2, "dev-publish-"+head)}, "1", healthy, nil, false, ""},
		{"nothing is rerun while the budget is spent", []string{job(1, "publish-"+head, "failure", reconcile)}, "1", spent, nil, false, "40 of 5000"},
		// The reserve (480) is for merge-group jobs: another job is rerun only while its expected
		// cost (42 reads) still leaves the reserve, the same rule the job itself defers on.
		{"a non-group job one request short of cost plus reserve is held for the wait", []string{job(1, "publish-"+head, "failure", reconcile)}, "1", "521 1000 1795475603", nil, false, "Held for the group reserve (521 of 1000"},
		{"a non-group job at cost plus reserve is rerun", []string{job(1, "publish-"+head, "failure", reconcile)}, "1", "522 1000 1795475603", []int{1}, false, ""},
		// The jobs listing spends requests after the first read, so the decision uses a fresh balance.
		{"a group job the jobs listing leaves below a tenth is not rerun", []string{job(1, "group-"+head, "failure", groupReconcile)}, "1", "100 1000 1795475603|99 1000 1795475603", nil, false, "99 of 1000"},
		{"a failed second balance read fails the step", []string{job(1, "publish-"+head, "failure", reconcile)}, "1", "900 1000 1795475603|bad 1000 1795475603", nil, true, ""},
		{"a balance that is not a number fails the step", []string{job(1, "group-"+head, "failure", groupReconcile)}, "1", "\"x\" 1000 1795475603", nil, true, ""},
		{"a non-group job the jobs listing leaves one short is held", []string{job(1, "publish-"+head, "failure", reconcile)}, "1", "522 1000 1795475603|521 1000 1795475603", nil, false, "Held for the group reserve (521 of 1000"},
		{"a non-group job at fifteen percent is deferred, not rerun into the reserve", []string{job(1, "publish-"+head, "failure", reconcile)}, "1", "150 1000 1795475603", nil, false, "Held for the group reserve (150 of 1000"},
		{"a dev publisher is held below the reserve too", []string{job(1, "dev-publish-"+head, "failure", "Reconcile exact-head dev admission")}, "1", "521 1000 1795475603", nil, false, "Held for the group reserve"},
		{"a group job below the reserve is rerun while the publisher waits", []string{job(1, "publish-"+head, "failure", reconcile), job(2, "group-"+head, "failure", groupReconcile)}, "1", "249 1000 1795475603", []int{2}, false, "Held for the group reserve"},
		{"a group job at fifteen percent is rerun", []string{job(1, "group-"+head, "failure", groupReconcile)}, "1", "150 1000 1795475603", []int{1}, false, ""},
		// GitHub refuses a rerun of another job while the run is active again: one rerun per run.
		{"a mixed failed set reruns only the group job", []string{job(1, "publish-"+head, "failure", reconcile), job(2, "group-"+head, "failure", groupReconcile), job(3, "dev-publish-"+head, "failure", "Reconcile exact-head dev admission")}, "1", healthy, []int{2}, false, ""},
		{"several failed publishers of one kind use the run-level rerun", []string{job(1, "publish-"+head, "failure", reconcile), job(2, "publish-"+strings.Repeat("b", 40), "failure", reconcile)}, "1", healthy, []int{runLevel}, false, ""},
		// The run-level rerun starts every failed job together: their combined cost must leave the reserve (2 x 42 above 480).
		// Rerunning one would strand the other (the next attempt lists only its own jobs), so both are held for the wait.
		{"several failed publishers that fit one by one but not together are all held", []string{job(1, "publish-"+head, "failure", reconcile), job(2, "publish-"+strings.Repeat("b", 40), "failure", reconcile)}, "1", "563 1000 1795475603", nil, false, "Held for the group reserve (563 of 1000"},
		{"several failed publishers whose combined cost just fits use the run-level rerun", []string{job(1, "publish-"+head, "failure", reconcile), job(2, "publish-"+strings.Repeat("b", 40), "failure", reconcile)}, "1", "564 1000 1795475603", []int{runLevel}, false, ""},
		{"several failed publishers the jobs listing leaves short together are all held", []string{job(1, "publish-"+head, "failure", reconcile), job(2, "publish-"+strings.Repeat("b", 40), "failure", reconcile)}, "1", "564 1000 1795475603|563 1000 1795475603", nil, false, "Held for the group reserve (563 of 1000"},
		// 13 x 42 = 546 exceeds the 520 a fresh 1000-request window leaves above the reserve: the retry job never releases them
		// (known limit, routed to #1598: the operator completes such a set).
		{"a set no fresh window could afford together is held for the wait, never released here", manyPublishers(13), "1", "700 1000 1795475603", nil, false, "Held for the group reserve (700 of 1000"},
		{"an oversized set on the last attempts is still held, not released", manyPublishers(13), "2", "700 1000 1795475603", nil, false, "Held for the group reserve (700 of 1000"},
		{"twelve publishers are held while they do not fit (the sweep's listings usually keep them held after the reset)", manyPublishers(12), "1", "700 1000 1795475603", nil, false, "Held for the group reserve (700 of 1000"},
		{"several failed group jobs use the run-level rerun", []string{job(1, "group-"+head, "failure", groupReconcile), job(2, "group-"+strings.Repeat("b", 40), "failure", groupReconcile)}, "1", healthy, []int{runLevel}, false, ""},
		// A mixed run gains nothing from the hold: the wait would rerun the same single job, so it is rerun now.
		{"a mixed run whose publishers do not fit together still reruns its first job at once", []string{job(1, "publish-"+head, "failure", reconcile), job(2, "publish-"+strings.Repeat("b", 40), "failure", reconcile), job(3, "dev-revoke", "failure", "Revoke")}, "1", "563 1000 1795475603", []int{1}, false, ""},
		{"another failed job in the run keeps the run-level rerun off", []string{job(1, "publish-"+head, "failure", reconcile), job(2, "publish-"+strings.Repeat("b", 40), "failure", reconcile), job(3, "discover", "failure", "Discover")}, "1", healthy, []int{1}, false, ""},
		{"a held job in the run keeps the run-level rerun off", []string{job(1, "publish-"+head, "failure", reconcile), job(2, "group-"+head, "failure", groupReconcile)}, "1", "249 1000 1795475603", []int{2}, false, "Held for the group reserve"},
		// Worker group jobs are named group-<sha> in the worker run and may spend the reserve.
		{"a worker group job at 270 of 1000 is rerun", []string{job(1, "group-"+head, "failure", workerReconcile)}, "1", "270 1000 1795475603", []int{1}, false, ""},
		{"a worker publisher at 270 of 1000 is held while its group job runs", []string{job(1, "publish-"+head, "failure", workerReconcile), job(2, "group-"+strings.Repeat("c", 40), "failure", workerReconcile)}, "1", "270 1000 1795475603", []int{2}, false, "Held for the group reserve (270 of 1000"},
		// The core and graphql buckets are separate: the one with the lower remaining share decides.
		{"a spent graphql bucket declines the rerun", []string{job(1, "group-"+head, "failure", groupReconcile)}, "1", "4000 5000 1795475603 20 1000 1795475603", nil, false, "20 of 1000"},
		// The Go gate judges each bucket by its absolute remaining; the lower share (graphql here) must not win.
		{"the bucket with the fewest requests left decides, not the lowest share", []string{job(1, "publish-"+head, "failure", reconcile)}, "1", "260 1000 1795475603 900 5000 1795475603", nil, false, "Held for the group reserve (260 of 1000"},
		{"a healthy graphql bucket changes nothing", []string{job(1, "group-"+head, "failure", groupReconcile)}, "1", "4000 5000 1795475603 900 1000 1795475603", []int{1}, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			calls := filepath.Join(dir, "calls")
			// Each "|"-separated balance answers one rate_limit read in turn; the last repeats.
			balances := strings.Split(tc.rateLimit, "|")
			for i, balance := range append(balances, balances[len(balances)-1]) {
				rl := strings.Fields(balance)
				graphql := ""
				if len(rl) == 6 {
					graphql = ",\"graphql\":{\"remaining\":" + rl[3] + ",\"limit\":" + rl[4] + ",\"reset\":" + rl[5] + "}"
				}
				body := "{\"resources\":{\"core\":{\"remaining\":" + rl[0] + ",\"limit\":" + rl[1] + ",\"reset\":" + rl[2] + "}" + graphql + "}}"
				name := fmt.Sprintf("rl%d.json", i)
				if i == len(balances) {
					name = "rl-last.json" // the answer once the list runs out
				}
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			// The stub answers rate_limit and the jobs listing by applying the script's own --jq filter, and records reruns.
			stub := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >> '" + calls + "'\n" +
				"filter=\nfor a in \"$@\"; do if [ \"$prev\" = --jq ]; then filter=$a; fi; prev=$a; done\n" +
				"case \"$*\" in\n" +
				"  'api rate_limit '*) n=$(cat '" + dir + "/count' 2>/dev/null || echo 0); echo $((n + 1)) > '" + dir + "/count'; f='" + dir + "'/rl$n.json; [ -f \"$f\" ] || f='" + dir + "'/rl-last.json; jq -r \"$filter\" < \"$f\" ;;\n" +
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
				if id == runLevel {
					want = append(want, "api --method POST repos/Hikyo-Org/Hikyo/actions/runs/41595466545/rerun-failed-jobs")
				} else {
					want = append(want, "api --method POST repos/Hikyo-Org/Hikyo/actions/jobs/"+strconv.Itoa(id)+"/rerun")
				}
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

// GitHub refuses a rerun of another job while the run is active again (403, "the workflow
// run containing this job is already running"). At most one rerun is requested per run, so
// that refusal never arises and a second failed job is not reported as lost; a rerun that
// is refused anyway is retried, named without GitHub's text, and fails the step.
func TestQueueRetryRequestsOneRerunPerRun(t *testing.T) {
	script := readQueueRetryWorkflow(t).Jobs["rerun"].Steps[0].Run
	head := strings.Repeat("a", 40)
	failedJob := func(id int, name, step string) string {
		return `{"id":` + strconv.Itoa(id) + `,"name":"` + name + `","conclusion":"failure","steps":[{"name":"` + step + `","conclusion":"failure"}]}`
	}
	const reconcile, groupReconcile = "Reconcile one authenticated head", "Reconcile complete group proof under its head lease"
	run := func(t *testing.T, jobs, refuse string) (string, string, error) {
		dir := t.TempDir()
		calls := filepath.Join(dir, "calls")
		// Once any rerun is accepted the run is active again: every later POST is refused as GitHub does.
		stub := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >> '" + calls + "'\n" +
			"filter=\nfor a in \"$@\"; do if [ \"$prev\" = --jq ]; then filter=$a; fi; prev=$a; done\n" +
			"case \"$*\" in\n" +
			"  'api rate_limit '*) printf '%s' '{\"resources\":{\"core\":{\"remaining\":4000,\"limit\":5000,\"reset\":1795475603}}}' | jq -r \"$filter\" ;;\n" +
			"  *--method\\ POST*) if [ -e '" + dir + "/active' ] || [ -n '" + refuse + "' ] && case \"$*\" in *'" + refuse + "'*) true ;; *) false ;; esac; then echo 'secret response text' >&2; exit 1; fi; : > '" + dir + "/active' ;;\n" +
			"  api*) printf '%s' '{\"jobs\":[" + jobs + "]}' | jq -r \"$filter\" ;;\n" +
			"esac\n"
		if err := os.WriteFile(filepath.Join(dir, "sleep"), []byte("#!/usr/bin/env bash\nprintf 'sleep %s\\n' \"$*\" >> '"+calls+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), "bash", "-c", script)
		cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "GH_TOKEN=x", "GH_REPO=Hikyo-Org/Hikyo", "RUN_ID=41595466545", "RUN_ATTEMPT=1")
		out, err := cmd.CombinedOutput()
		recorded, _ := os.ReadFile(calls)
		return string(recorded), string(out), err
	}
	t.Run("mixed kinds in one run", func(t *testing.T) {
		recorded, out, err := run(t, strings.Join([]string{failedJob(1, "publish-"+head, reconcile), failedJob(2, "group-"+head, groupReconcile), failedJob(3, "dev-publish-"+head, "Reconcile exact-head dev admission")}, ","), "")
		if err != nil || strings.Count(recorded, "--method POST") != 1 || !strings.Contains(recorded, "jobs/2/rerun") {
			t.Fatalf("one rerun of the group job; the second POST would be refused and must never be sent: %v\n%s\n%s", err, recorded, out)
		}
		if strings.Contains(out, "was refused") || !strings.Contains(out, "left for that rerun's own completion") {
			t.Fatalf("the other failed jobs are left for the rerun's completion, not reported lost:\n%s", out)
		}
	})
	t.Run("a uniform failed set goes through the run-level endpoint", func(t *testing.T) {
		recorded, out, err := run(t, strings.Join([]string{failedJob(1, "publish-"+head, reconcile), failedJob(2, "publish-"+strings.Repeat("b", 40), reconcile)}, ","), "")
		if err != nil || strings.Count(recorded, "--method POST") != 1 || !strings.Contains(recorded, "runs/41595466545/rerun-failed-jobs") {
			t.Fatalf("one run-level rerun covers both failed jobs: %v\n%s\n%s", err, recorded, out)
		}
	})
	t.Run("a refused rerun is retried, named, and fails the step", func(t *testing.T) {
		recorded, out, err := run(t, failedJob(2, "publish-"+head, reconcile), "jobs/2/rerun")
		if err == nil {
			t.Fatalf("a refused rerun must fail the step, not report success:\n%s", out)
		}
		if n := strings.Count(recorded, "jobs/2/rerun"); n != 3 {
			t.Fatalf("a refused rerun request is retried twice with backoff before it is reported: %d requests\n%s", n, recorded)
		}
		if !strings.Contains(out, "The rerun of job 2 was refused") || strings.Contains(out, "secret response text") {
			t.Fatalf("the refused job must be named without GitHub's text:\n%s", out)
		}
	})
}
