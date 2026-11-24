package ci_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The wait job is the one place a long timeout is designed in. Pin every bound
// that keeps it from becoming a loop, a privilege or a bigger queue timeout.
func TestQueueRetryWaitJobIsABoundedDesignedWait(t *testing.T) {
	w := readQueueRetryWorkflow(t)
	job, ok := w.Jobs["rerun-after-reset"]
	if !ok {
		t.Fatal("rerun-after-reset job missing")
	}
	// The core window is at most 60 minutes: wait <= 61 minutes, 70 minutes for the
	// sweep, and well under the 120-minute merge-queue timeout it protects.
	if job.Timeout != 70 {
		t.Fatalf("the wait job's timeout must be sized to one rate-limit window: %d", job.Timeout)
	}
	if job.Needs != "rerun" || !reflect.DeepEqual(job.Permissions, map[string]string{"actions": "write"}) {
		t.Fatalf("the wait follows only the trusted rerun job and holds only actions: write: %+v", job)
	}
	if job.Concurrency.Group != "queue-retry-after-reset" || job.Concurrency.Cancel {
		t.Fatalf("waiters must coalesce repo-wide without cancelling the running sleeper: %+v", job.Concurrency)
	}
	wantIf := "${{ always() && (needs.rerun.outputs.exhausted == 'true' || needs.rerun.result == 'failure') }}"
	if job.If != wantIf {
		t.Fatalf("the wait may start only after the trusted rerun job ran:\n got %s\nwant %s", job.If, wantIf)
	}
	if len(job.Steps) != 1 || job.Steps[0].Uses != "" {
		t.Fatal("the wait job must be one shell step that runs no repository code and checks nothing out")
	}
	for _, name := range []string{"ci-queue-gate.yml", "ci-queue-worker.yml", "ci-dev-gate.yml"} {
		if !strings.Contains(job.Steps[0].Run, name) {
			t.Fatalf("the sweep must cover %s", name)
		}
	}
	if !strings.Contains(job.Steps[0].Run, `[ "$wait" -gt 3660 ]`) || strings.Contains(job.Steps[0].Run, "while true") {
		t.Fatal("the wait must refuse a reset beyond one window and never loop")
	}
}

type waitRun struct {
	id      int
	attempt int
	event   string
	branch  string
	repo    string
	age     time.Duration
	jobs    []string // job JSON for this run
	wf      string
}

// rerunAfterResetFixture runs the wait job's script against a stub gh whose
// rate limit recovers only once the stub sleep has been asked to wait.
type rerunAfterResetFixture struct {
	t        *testing.T
	dir      string
	now      int64
	reset    int64
	recovers bool // false: the window is still spent after the sleep
	runs     []waitRun
	refuse   string // job id whose rerun is refused
}

const waitHead = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func failedGroupJob(id int, name, step string) string {
	return `{"id":` + strconv.Itoa(id) + `,"name":"` + name + `","conclusion":"failure","steps":[{"name":"` + step + `","conclusion":"failure"}]}`
}

func (f *rerunAfterResetFixture) run() (string, string, error) {
	f.t.Helper()
	calls := filepath.Join(f.dir, "calls")
	state := filepath.Join(f.dir, "slept")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(f.dir, name), []byte(body), 0o755); err != nil {
			f.t.Fatal(err)
		}
	}
	// No real sleeps and a pinned clock: sleep records its argument and marks the reset reached.
	write("sleep", "#!/usr/bin/env bash\nprintf 'sleep %s\\n' \"$*\" >> '"+calls+"'\n: > '"+state+"'\n")
	write("date", "#!/usr/bin/env bash\n[ \"$*\" = +%s ] || exec /bin/date \"$@\"\necho "+strconv.FormatInt(f.now, 10)+"\n")
	var cases strings.Builder
	for _, wf := range []string{"ci-queue-gate.yml", "ci-queue-worker.yml", "ci-dev-gate.yml"} {
		var listed []string
		for _, r := range f.runs {
			if r.wf != wf {
				continue
			}
			created := time.Unix(f.now, 0).Add(-r.age).UTC().Format(time.RFC3339)
			listed = append(listed, fmt.Sprintf(`{"id":%d,"run_attempt":%d,"conclusion":"failure","event":%q,"head_branch":%q,"head_repository":{"full_name":%q},"created_at":%q}`, r.id, r.attempt, r.event, r.branch, r.repo, created))
		}
		fmt.Fprintf(&cases, "  *workflows/%s/runs*) printf '%%s' '{\"workflow_runs\":[%s]}' ;;\n", wf, strings.Join(listed, ","))
	}
	for _, r := range f.runs {
		fmt.Fprintf(&cases, "  *runs/%d/attempts/%d/jobs*) printf '%%s' '{\"jobs\":[%s]}' | jq -r \"$filter\" ;;\n", r.id, r.attempt, strings.Join(r.jobs, ","))
	}
	remaining := fmt.Sprintf("if [ -e '%s' ] && %t; then r=4000; else r=40; fi\n", state, f.recovers)
	stub := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >> '" + calls + "'\n" +
		"filter=\nfor a in \"$@\"; do if [ \"$prev\" = --jq ]; then filter=$a; fi; prev=$a; done\n" + remaining +
		"case \"$*\" in\n" +
		"  'api rate_limit '*) printf '%s' \"{\\\"resources\\\":{\\\"core\\\":{\\\"remaining\\\":$r,\\\"limit\\\":5000,\\\"reset\\\":" + strconv.FormatInt(f.reset, 10) + "}}}\" | jq -r \"$filter\" ;;\n"
	if f.refuse != "" {
		stub += "  *jobs/" + f.refuse + "/rerun*) echo 'secret response text' >&2; exit 1 ;;\n"
	}
	stub += "  *--method\\ POST*) ;;\n" + cases.String() + "esac\n"
	write("gh", stub)
	script := readQueueRetryWorkflow(f.t).Jobs["rerun-after-reset"].Steps[0].Run
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+f.dir+string(os.PathListSeparator)+os.Getenv("PATH"), "GH_TOKEN=x", "GH_REPO=Hikyo-Org/Hikyo")
	out, err := cmd.CombinedOutput()
	recorded, _ := os.ReadFile(calls)
	return string(recorded), string(out), err
}

func (f *rerunAfterResetFixture) reruns(recorded string) []string {
	var got []string
	for _, line := range strings.Split(recorded, "\n") {
		if strings.HasPrefix(line, "api --method POST ") {
			got = append(got, strings.TrimPrefix(line, "api --method POST repos/Hikyo-Org/Hikyo/actions/jobs/"))
		}
	}
	return got
}

func TestQueueRetryWaitsForTheResetThenRerunsExactlyTheFailedJob(t *testing.T) {
	now := int64(1795475603)
	trusted := func(wf string, id int, jobs ...string) waitRun {
		return waitRun{id: id, attempt: 1, event: "schedule", branch: "main", repo: "Hikyo-Org/Hikyo", age: 20 * time.Minute, jobs: jobs, wf: wf}
	}
	const groupStep = "Reconcile complete group proof under its head lease"
	for _, tc := range []struct {
		name string
		run  waitRun
		want string
	}{
		{"rate-limited group job", trusted("ci-queue-worker.yml", 900, failedGroupJob(11, "group-"+waitHead, groupStep), `{"id":12,"name":"group-`+strings.Repeat("b", 40)+`","conclusion":"success","steps":[]}`), "11/rerun"},
		{"rate-limited queue publish job", trusted("ci-queue-gate.yml", 901, failedGroupJob(21, "publish-"+waitHead, "Reconcile one authenticated head")), "21/rerun"},
		{"rate-limited dev publish job", trusted("ci-dev-gate.yml", 902, failedGroupJob(31, "dev-publish-"+waitHead, "Reconcile exact-head dev admission")), "31/rerun"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The reset is 30 minutes away: far beyond a group job's 5-minute budget and a publisher's 1 minute.
			f := &rerunAfterResetFixture{t: t, dir: t.TempDir(), now: now, reset: now + 1800, recovers: true, runs: []waitRun{tc.run}}
			recorded, out, err := f.run()
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			wantSleep := "sleep " + strconv.Itoa(1800+5)
			sleepAt, rerunAt := strings.Index(recorded, wantSleep), strings.Index(recorded, "api --method POST")
			if sleepAt < 0 || rerunAt < 0 || sleepAt > rerunAt {
				t.Fatalf("the rerun must follow a sleep of reset+5s (%q):\n%s", wantSleep, recorded)
			}
			if got := f.reruns(recorded); !reflect.DeepEqual(got, []string{tc.want}) {
				t.Fatalf("exactly the failed job must be rerun: %v\n%s", got, recorded)
			}
			if strings.Count(recorded, "sleep ") != 1 {
				t.Fatalf("one wait per sweep:\n%s", recorded)
			}
		})
	}
}

func TestQueueRetryWaitRerunsOnlyTrustedRecentFailedRuns(t *testing.T) {
	now := int64(1795475603)
	step := "Reconcile one authenticated head"
	job := func(id int) []string { return []string{failedGroupJob(id, "publish-"+waitHead, step)} }
	base := waitRun{attempt: 1, event: "schedule", branch: "main", repo: "Hikyo-Org/Hikyo", age: time.Hour, wf: "ci-queue-gate.yml"}
	with := func(id int, edit func(*waitRun)) waitRun {
		r := base
		r.id, r.jobs = id, job(id*10)
		edit(&r)
		return r
	}
	f := &rerunAfterResetFixture{t: t, dir: t.TempDir(), now: now, reset: now + 600, recovers: true, runs: []waitRun{
		with(1, func(r *waitRun) {}), // rerun
		with(2, func(r *waitRun) { r.event, r.branch = "pull_request_target", "feature/x" }),            // base-file run: trusted
		with(3, func(r *waitRun) { r.event, r.branch = "push", "feature/x" }),                           // a branch's own run: refused
		with(4, func(r *waitRun) { r.repo = "fork/Hikyo" }),                                             // fork head: refused
		with(5, func(r *waitRun) { r.attempt = 3 }),                                                     // attempt bound
		with(6, func(r *waitRun) { r.age = 4 * time.Hour }),                                             // too old to matter
		with(7, func(r *waitRun) { r.jobs = []string{failedGroupJob(70, "publish-not-a-head", step)} }), // not a head job
	}}
	recorded, out, err := f.run()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := f.reruns(recorded); !reflect.DeepEqual(got, []string{"10/rerun", "20/rerun"}) {
		t.Fatalf("only trusted, recent, in-bound failed head jobs may be rerun: %v\n%s", got, recorded)
	}
}

func TestQueueRetryWaitFailsVisiblyWhenItCannotHelp(t *testing.T) {
	now := int64(1795475603)
	run := waitRun{id: 5, attempt: 1, event: "schedule", branch: "main", repo: "Hikyo-Org/Hikyo", age: time.Minute, wf: "ci-queue-gate.yml",
		jobs: []string{failedGroupJob(51, "publish-"+waitHead, "Reconcile one authenticated head"), failedGroupJob(52, "publish-"+strings.Repeat("b", 40), "Reconcile one authenticated head")}}
	t.Run("budget is healthy", func(t *testing.T) {
		f := &rerunAfterResetFixture{t: t, dir: t.TempDir(), now: now, reset: now + 600, recovers: true, runs: []waitRun{run}}
		if err := os.WriteFile(filepath.Join(f.dir, "slept"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		recorded, out, err := f.run()
		if err != nil || strings.Contains(recorded, "sleep ") || len(f.reruns(recorded)) != 0 {
			t.Fatalf("a healthy budget needs no wait and no rerun: %v\n%s\n%s", err, recorded, out)
		}
	})
	t.Run("reset beyond one window", func(t *testing.T) {
		f := &rerunAfterResetFixture{t: t, dir: t.TempDir(), now: now, reset: now + 7200, recovers: true, runs: []waitRun{run}}
		recorded, out, err := f.run()
		if err == nil || strings.Contains(recorded, "sleep ") || len(f.reruns(recorded)) != 0 || !strings.Contains(out, "beyond one window") {
			t.Fatalf("an absurd reset must fail without sleeping: %v\n%s\n%s", err, recorded, out)
		}
	})
	t.Run("still spent after the reset", func(t *testing.T) {
		f := &rerunAfterResetFixture{t: t, dir: t.TempDir(), now: now, reset: now + 600, recovers: false, runs: []waitRun{run}}
		recorded, out, err := f.run()
		if err == nil || len(f.reruns(recorded)) != 0 || !strings.Contains(out, "still has only") {
			t.Fatalf("a still-spent window must fail instead of rerunning into it: %v\n%s\n%s", err, recorded, out)
		}
	})
	t.Run("a refused rerun does not hide the next", func(t *testing.T) {
		f := &rerunAfterResetFixture{t: t, dir: t.TempDir(), now: now, reset: now + 600, recovers: true, runs: []waitRun{run}, refuse: "51"}
		recorded, out, err := f.run()
		if err == nil || !strings.Contains(strings.Join(f.reruns(recorded), " "), "52/rerun") || !strings.Contains(out, "Job 51 was not rerun") || strings.Contains(out, "secret response text") {
			t.Fatalf("a refusal must be named without GitHub's text and the other job still rerun: %v\n%s\n%s", err, recorded, out)
		}
	})
}
