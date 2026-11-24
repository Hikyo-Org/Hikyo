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
	run := job.Steps[0].Run
	for _, name := range []string{"ci-queue-gate.yml", "ci-queue-worker.yml", "ci-dev-gate.yml"} {
		if !strings.Contains(run, name) {
			t.Fatalf("the sweep must cover %s", name)
		}
	}
	for _, want := range []string{`[ "$wait" -gt 3660 ]`, "--paginate --method GET", "-f created=", "for status in failure cancelled"} {
		if !strings.Contains(run, want) {
			t.Fatalf("the wait script lost %q", want)
		}
	}
	if strings.Contains(run, "while true") || strings.Contains(run, "per_page=30") {
		t.Fatal("the wait must never loop and must not cap the run listing")
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
	status  string // conclusion: failure or cancelled
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
	pageSize int            // runs per listing page; 0 means one page
	spend    int            // core requests the stub takes off the window per accepted rerun
	failures map[string]int // job id -> rerun requests GitHub refuses before accepting one
}

const waitHead = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func failedGroupJob(id int, name, step string) string {
	return `{"id":` + strconv.Itoa(id) + `,"name":"` + name + `","conclusion":"failure","steps":[{"name":"` + step + `","conclusion":"failure"}]}`
}

func (f *rerunAfterResetFixture) run() (string, string, error) {
	f.t.Helper()
	calls := filepath.Join(f.dir, "calls")
	state := filepath.Join(f.dir, "slept")
	accepted := filepath.Join(f.dir, "accepted")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(f.dir, name), []byte(body), 0o755); err != nil {
			f.t.Fatal(err)
		}
	}
	// No real sleeps and a pinned clock: sleep records its argument and marks the reset reached.
	write("sleep", "#!/usr/bin/env bash\nprintf 'sleep %s\\n' \"$*\" >> '"+calls+"'\n: > '"+state+"'\n")
	write("date", "#!/usr/bin/env bash\n[ \"$*\" = +%s ] || exec /bin/date \"$@\"\necho "+strconv.FormatInt(f.now, 10)+"\n")
	write("accepted", "")
	var cases strings.Builder
	for _, wf := range []string{"ci-queue-gate.yml", "ci-queue-worker.yml", "ci-dev-gate.yml"} {
		for _, status := range []string{"failure", "cancelled"} {
			var listed []string
			for _, r := range f.runs {
				if r.wf != wf || r.status != status {
					continue
				}
				created := time.Unix(f.now, 0).Add(-r.age).UTC().Format(time.RFC3339)
				listed = append(listed, fmt.Sprintf(`{"id":%d,"run_attempt":%d,"conclusion":%q,"event":%q,"head_branch":%q,"head_repository":{"full_name":%q},"created_at":%q}`, r.id, r.attempt, r.status, r.event, r.branch, r.repo, created))
			}
			size := f.pageSize
			if size <= 0 {
				size = len(listed) + 1
			}
			pages := 0
			for i := 0; i < len(listed); i += size {
				end := min(i+size, len(listed))
				page := `{"workflow_runs":[` + strings.Join(listed[i:end], ",") + `]}`
				write(fmt.Sprintf("page-%s-%s-%d", wf, status, pages), page)
				pages++
			}
			// gh api --paginate applies --jq to every page; without --paginate only the first page is seen.
			fmt.Fprintf(&cases, "  *workflows/%s/runs*status=%s*) pages=%d; paginate=0; case \"$*\" in *--paginate*) paginate=1 ;; esac\n    for ((i = 0; i < pages; i++)); do cat '%s/page-%s-%s-'$i | jq -r \"$filter\"; [ $paginate = 1 ] || break; done ;;\n", wf, status, pages, f.dir, wf, status)
		}
	}
	for _, r := range f.runs {
		fmt.Fprintf(&cases, "  *runs/%d/attempts/%d/jobs*) printf '%%s' '{\"jobs\":[%s]}' | jq -r \"$filter\" ;;\n", r.id, r.attempt, strings.Join(r.jobs, ","))
	}
	for id, n := range f.failures {
		if err := os.WriteFile(filepath.Join(f.dir, "refuse-"+id), []byte(strconv.Itoa(n)), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
	stub := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >> '" + calls + "'\n" +
		"filter=\nfor a in \"$@\"; do if [ \"$prev\" = --jq ]; then filter=$a; fi; if [ \"$prev\" = -f ]; then case \"$a\" in status=*) status=${a#status=} ;; esac; fi; prev=$a; done\n" +
		"if [ -e '" + state + "' ] && " + strconv.FormatBool(f.recovers) + "; then n=$(wc -l < '" + accepted + "' 2>/dev/null || echo 0); r=$((4000 - " + strconv.Itoa(f.spend) + " * n)); else r=40; fi\n" +
		"case \"$*\" in\n" +
		"  'api rate_limit '*) printf '%s' \"{\\\"resources\\\":{\\\"core\\\":{\\\"remaining\\\":$r,\\\"limit\\\":5000,\\\"reset\\\":" + strconv.FormatInt(f.reset, 10) + "}}}\" | jq -r \"$filter\" ;;\n" +
		"  *--method\\ POST*) last=${@: -1}; id=${last##*jobs/}; id=${id%%/*}\n" +
		"    if [ -e '" + f.dir + "/refuse-'$id ] && [ \"$(cat '" + f.dir + "/refuse-'$id)\" -gt 0 ]; then echo $(($(cat '" + f.dir + "/refuse-'$id) - 1)) > '" + f.dir + "/refuse-'$id; echo 'secret response text' >&2; exit 1; fi\n" +
		"    echo \"$id\" >> '" + accepted + "' ;;\n" +
		cases.String() + "esac\n"
	write("gh", stub)
	script := readQueueRetryWorkflow(f.t).Jobs["rerun-after-reset"].Steps[0].Run
	cmd := exec.CommandContext(f.t.Context(), "bash", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+f.dir+string(os.PathListSeparator)+os.Getenv("PATH"), "GH_TOKEN=x", "GH_REPO=Hikyo-Org/Hikyo")
	out, err := cmd.CombinedOutput()
	recorded, _ := os.ReadFile(calls)
	return string(recorded), string(out), err
}

// accepted lists the job ids GitHub accepted a rerun for, in order.
func (f *rerunAfterResetFixture) accepted() []string {
	raw, _ := os.ReadFile(filepath.Join(f.dir, "accepted"))
	return strings.Fields(string(raw))
}

func (f *rerunAfterResetFixture) posts(recorded string) int {
	return strings.Count(recorded, "api --method POST ")
}

func trustedRun(wf string, id int, jobs ...string) waitRun {
	return waitRun{id: id, attempt: 1, event: "schedule", branch: "main", repo: "Hikyo-Org/Hikyo", age: 20 * time.Minute, jobs: jobs, wf: wf, status: "failure"}
}

func TestQueueRetryWaitsForTheResetThenRerunsExactlyTheFailedJob(t *testing.T) {
	now := int64(1795475603)
	const groupStep = "Reconcile complete group proof under its head lease"
	for _, tc := range []struct {
		name string
		run  waitRun
		want string
	}{
		{"rate-limited group job", trustedRun("ci-queue-worker.yml", 900, failedGroupJob(11, "group-"+waitHead, groupStep), `{"id":12,"name":"group-`+strings.Repeat("b", 40)+`","conclusion":"success","steps":[]}`), "11"},
		{"rate-limited queue publish job", trustedRun("ci-queue-gate.yml", 901, failedGroupJob(21, "publish-"+waitHead, "Reconcile one authenticated head")), "21"},
		{"rate-limited dev publish job", trustedRun("ci-dev-gate.yml", 902, failedGroupJob(31, "dev-publish-"+waitHead, "Reconcile exact-head dev admission")), "31"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The reset is 30 minutes away: far beyond a group job's 5-minute budget and a publisher's 1-minute one.
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
			if got := f.accepted(); !reflect.DeepEqual(got, []string{tc.want}) {
				t.Fatalf("exactly the failed job must be rerun: %v\n%s", got, recorded)
			}
		})
	}
}

// #1514 end to end through the two retry jobs: a gate-queue run started by the
// workflow_run callback of trusted-merge-ci, whose group-<sha> job was rate-limited
// in initRepository. No other event of any kind follows. The rerun job declines on the
// spent window and hands over; the wait job then reruns that exact job after the reset.
// scripts/ci/pr-gate/queue_rate_limit_rerun_test.go proves the rerun completes the check.
func TestQueueRetryHandsARateLimitedGroupJobToTheWaitAndReruns(t *testing.T) {
	now := int64(1795475603)
	groupJob := failedGroupJob(4242, "group-"+waitHead, "Reconcile complete group proof under its head lease")
	run := waitRun{id: 7001, attempt: 1, event: "workflow_run", branch: "main", repo: "Hikyo-Org/Hikyo", age: 3 * time.Minute, jobs: []string{groupJob}, wf: "ci-queue-gate.yml", status: "failure"}
	f := &rerunAfterResetFixture{t: t, dir: t.TempDir(), now: now, reset: now + 2400, recovers: true, runs: []waitRun{run}}
	// Step 1: the workflow_run-triggered retry job runs while the window is spent.
	output := filepath.Join(f.dir, "output")
	script := readQueueRetryWorkflow(t).Jobs["rerun"].Steps[0].Run
	stub := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >> '" + filepath.Join(f.dir, "first-calls") + "'\n" +
		"filter=\nfor a in \"$@\"; do if [ \"$prev\" = --jq ]; then filter=$a; fi; prev=$a; done\n" +
		"case \"$*\" in 'api rate_limit '*) printf '%s' '{\"resources\":{\"core\":{\"remaining\":3,\"limit\":1000,\"reset\":" + strconv.FormatInt(now+2400, 10) + "}}}' | jq -r \"$filter\" ;; esac\n"
	if err := os.WriteFile(filepath.Join(f.dir, "gh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "bash", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+f.dir+string(os.PathListSeparator)+os.Getenv("PATH"), "GH_TOKEN=x", "GH_REPO=Hikyo-Org/Hikyo", "RUN_ID=7001", "RUN_ATTEMPT=1", "GITHUB_OUTPUT="+output)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the retry job must decline cleanly on a spent window: %v\n%s", err, out)
	}
	if first, _ := os.ReadFile(filepath.Join(f.dir, "first-calls")); strings.Contains(string(first), "POST") {
		t.Fatalf("nothing may be rerun into a spent window:\n%s", first)
	}
	handed, _ := os.ReadFile(output)
	if !strings.Contains(string(handed), "exhausted=true") {
		t.Fatalf("the spent window must be handed to rerun-after-reset: %q", handed)
	}
	// Step 2: the wait job's condition holds for that output; it sleeps and reruns the exact job.
	if got := readQueueRetryWorkflow(t).Jobs["rerun-after-reset"].If; !strings.Contains(got, "needs.rerun.outputs.exhausted == 'true'") {
		t.Fatalf("the wait must start on the handoff: %s", got)
	}
	recorded, out, err := f.run()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := f.accepted(); !reflect.DeepEqual(got, []string{"4242"}) {
		t.Fatalf("the group job must be rerun by its id after the reset: %v\n%s", got, recorded)
	}
	if !strings.Contains(recorded, "sleep 2405") {
		t.Fatalf("the rerun must wait for the reset:\n%s", recorded)
	}
}

func TestQueueRetryWaitRerunsOnlyTrustedRecentFailedRuns(t *testing.T) {
	now := int64(1795475603)
	step := "Reconcile one authenticated head"
	job := func(id int) []string { return []string{failedGroupJob(id, "publish-"+waitHead, step)} }
	base := trustedRun("ci-queue-gate.yml", 0)
	base.age = time.Hour
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
		with(7, func(r *waitRun) { r.jobs = []string{failedGroupJob(70, "publish-not-a-head", step)} }), // not a head job
		with(8, func(r *waitRun) { r.status = "cancelled" }),                                            // a cancelled run is listed under its own status
	}}
	recorded, out, err := f.run()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := f.accepted(); !reflect.DeepEqual(got, []string{"10", "20", "80"}) {
		t.Fatalf("only trusted, in-bound failed head jobs may be rerun: %v\n%s", got, recorded)
	}
	if !strings.Contains(recorded, "-f created=>=") {
		t.Fatalf("the listing must ask GitHub for the last three hours only:\n%s", recorded)
	}
}

// More failed runs than one page holds: every page of every workflow and status is read.
func TestQueueRetryWaitPagesThroughEveryFailedRun(t *testing.T) {
	now := int64(1795475603)
	var runs []waitRun
	var want []string
	for i := 0; i < 7; i++ {
		id := 100 + i
		wf := []string{"ci-queue-gate.yml", "ci-queue-worker.yml", "ci-dev-gate.yml"}[i%3]
		r := trustedRun(wf, id, failedGroupJob(id*10, "publish-"+waitHead, "Reconcile one authenticated head"))
		if i == 6 {
			r.status = "cancelled"
		}
		runs = append(runs, r)
		want = append(want, strconv.Itoa(id*10))
	}
	// One run per page: every workflow with two or more failed runs needs a second page.
	f := &rerunAfterResetFixture{t: t, dir: t.TempDir(), now: now, reset: now + 600, recovers: true, pageSize: 1, runs: runs}
	recorded, out, err := f.run()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got := f.accepted()
	slicesSort(got)
	slicesSort(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a failed run beyond the first page was missed: got %v want %v\n%s", got, want, recorded)
	}
	if !strings.Contains(recorded, "api --paginate --method GET") {
		t.Fatalf("listing must paginate:\n%s", recorded)
	}
}

func slicesSort(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

// Reruns released after a reset must not re-exhaust the window they waited for: each
// is preceded by a fresh budget check, group jobs go first, and the job stops while a
// fifth of the window is left, naming what it held instead of dropping it.
func TestQueueRetryWaitThrottlesTheBurstAfterTheReset(t *testing.T) {
	now := int64(1795475603)
	step := "Reconcile one authenticated head"
	runs := []waitRun{
		trustedRun("ci-queue-gate.yml", 1, failedGroupJob(11, "publish-"+waitHead, step), failedGroupJob(12, "publish-"+strings.Repeat("b", 40), step)),
		trustedRun("ci-dev-gate.yml", 2, failedGroupJob(21, "dev-publish-"+waitHead, "Reconcile exact-head dev admission"), failedGroupJob(22, "dev-publish-"+strings.Repeat("c", 40), "Reconcile exact-head dev admission")),
		trustedRun("ci-queue-worker.yml", 3, failedGroupJob(31, "group-"+waitHead, "Reconcile complete group proof under its head lease")),
	}
	// 4000 left after the reset, 1200 spent per accepted rerun, stop below a fifth (1000) of 5000.
	f := &rerunAfterResetFixture{t: t, dir: t.TempDir(), now: now, reset: now + 600, recovers: true, spend: 1200, runs: runs}
	recorded, out, err := f.run()
	if err == nil || !strings.Contains(out, "Held to protect the window") {
		t.Fatalf("a held burst must fail visibly, naming what it held: %v\n%s", err, out)
	}
	got := f.accepted()
	if len(got) != 3 || got[0] != "31" {
		t.Fatalf("group jobs first, and only while the window has a safe margin: %v\n%s", got, recorded)
	}
	if strings.Count(recorded, "sleep 15") != 3 {
		t.Fatalf("reruns must be spaced:\n%s", recorded)
	}
	if !strings.Contains(out, "Not rerun:") {
		t.Fatalf("the held jobs must be named:\n%s", out)
	}
}

func TestQueueRetryWaitRetriesARefusedRerunWithBackoff(t *testing.T) {
	now := int64(1795475603)
	run := trustedRun("ci-queue-gate.yml", 5, failedGroupJob(51, "publish-"+waitHead, "Reconcile one authenticated head"))
	t.Run("accepted on a later try", func(t *testing.T) {
		f := &rerunAfterResetFixture{t: t, dir: t.TempDir(), now: now, reset: now + 600, recovers: true, runs: []waitRun{run}, failures: map[string]int{"51": 2}}
		recorded, out, err := f.run()
		if err != nil || !reflect.DeepEqual(f.accepted(), []string{"51"}) || f.posts(recorded) != 3 {
			t.Fatalf("a twice-refused rerun must be accepted on the third request: %v %v\n%s\n%s", err, f.accepted(), recorded, out)
		}
		if !strings.Contains(recorded, "sleep 5\n") || !strings.Contains(recorded, "sleep 15\n") {
			t.Fatalf("retries must back off:\n%s", recorded)
		}
	})
	t.Run("always refused", func(t *testing.T) {
		second := trustedRun("ci-queue-gate.yml", 6, failedGroupJob(61, "publish-"+strings.Repeat("b", 40), "Reconcile one authenticated head"))
		f := &rerunAfterResetFixture{t: t, dir: t.TempDir(), now: now, reset: now + 600, recovers: true, runs: []waitRun{run, second}, failures: map[string]int{"51": 99}}
		recorded, out, err := f.run()
		if err == nil || f.posts(recorded) != 4+1 || !reflect.DeepEqual(f.accepted(), []string{"61"}) {
			t.Fatalf("a refusal is retried a bounded number of times, named, and does not hide the next job: %v %v\n%s\n%s", err, f.accepted(), recorded, out)
		}
		if !strings.Contains(out, "Job 51 was not rerun") || strings.Contains(out, "secret response text") {
			t.Fatalf("the refused job must be named without GitHub's text:\n%s", out)
		}
	})
}

func TestQueueRetryWaitFailsVisiblyWhenItCannotHelp(t *testing.T) {
	now := int64(1795475603)
	run := trustedRun("ci-queue-gate.yml", 5, failedGroupJob(51, "publish-"+waitHead, "Reconcile one authenticated head"), failedGroupJob(52, "publish-"+strings.Repeat("b", 40), "Reconcile one authenticated head"))
	t.Run("budget is healthy", func(t *testing.T) {
		f := &rerunAfterResetFixture{t: t, dir: t.TempDir(), now: now, reset: now + 600, recovers: true, runs: []waitRun{run}}
		if err := os.WriteFile(filepath.Join(f.dir, "slept"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		recorded, out, err := f.run()
		if err != nil || strings.Contains(recorded, "sleep ") || len(f.accepted()) != 0 {
			t.Fatalf("a healthy budget needs no wait and no rerun: %v\n%s\n%s", err, recorded, out)
		}
	})
	t.Run("reset beyond one window", func(t *testing.T) {
		f := &rerunAfterResetFixture{t: t, dir: t.TempDir(), now: now, reset: now + 7200, recovers: true, runs: []waitRun{run}}
		recorded, out, err := f.run()
		if err == nil || strings.Contains(recorded, "sleep ") || len(f.accepted()) != 0 || !strings.Contains(out, "beyond one window") {
			t.Fatalf("an absurd reset must fail without sleeping: %v\n%s\n%s", err, recorded, out)
		}
	})
	t.Run("still spent after the reset", func(t *testing.T) {
		f := &rerunAfterResetFixture{t: t, dir: t.TempDir(), now: now, reset: now + 600, recovers: false, runs: []waitRun{run}}
		recorded, out, err := f.run()
		if err == nil || len(f.accepted()) != 0 || !strings.Contains(out, "still has only") {
			t.Fatalf("a still-spent window must fail instead of rerunning into it: %v\n%s\n%s", err, recorded, out)
		}
	})
}
