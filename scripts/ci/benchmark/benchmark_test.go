package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWalltimeBudget(t *testing.T) {
	for _, test := range []struct {
		name            string
		used            usage
		event           string
		credit, allowed bool
	}{
		{"daily fits", usage{automatic: 512, manual: 72}, "schedule", false, true},
		{"daily capacity exhausted", usage{automatic: 513}, "schedule", false, false},
		{"manual preserves daily capacity", usage{automatic: 528, manual: 56}, "workflow_dispatch", false, true},
		{"manual allowance exhausted", usage{manual: 57}, "workflow_dispatch", false, false},
		{"credit investigation fits", usage{automatic: 528, manual: 212}, "workflow_dispatch", true, true},
		{"credit investigation exceeds five dollars", usage{manual: 213}, "workflow_dispatch", true, false},
		{"credit does not stop daily", usage{automatic: 512, manual: 228}, "schedule", false, true},
		{"unknown event", usage{}, "push", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, _ := admit(test.used, test.event, test.credit)
			if got != test.allowed {
				t.Fatalf("allowed=%t, want %t", got, test.allowed)
			}
		})
	}
}

func TestJobAccounting(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	start, end := now.Add(-3*time.Minute-5*time.Second), now
	base := job{ID: 1, Labels: []string{macroLabel}, Status: "completed", Conclusion: "success", RunnerName: "macro", StartedAt: &start, CompletedAt: &end}
	for _, test := range []struct {
		name    string
		change  func(*job)
		minutes int
		bad     bool
	}{
		{"whole job rounded plus margin", func(*job) {}, 5, false},
		{"failed job still costs", func(j *job) { j.Conclusion = "failure" }, 5, false},
		{"unassigned queue cancellation", func(j *job) { j.RunnerName = ""; j.Conclusion = "cancelled"; j.StartedAt = nil }, 0, false},
		{"assigned cancellation counts", func(j *job) { j.Conclusion = "cancelled" }, 5, false},
		{"in progress blocks another admission", func(j *job) { j.Status = "in_progress" }, 0, true},
		{"missing evidence fails closed", func(j *job) { j.CompletedAt = nil }, 0, true},
		{"unapproved Macro label", func(j *job) { j.Labels = []string{"codspeed-macro-x64-other"} }, 0, true},
		{"missing Macro label", func(j *job) { j.Labels = nil; j.Name = "Requested PR walltime" }, 0, true},
		{"hosted smoke is free", func(j *job) { j.Labels = []string{"ubuntu-latest"} }, 0, false},
		{"old job excluded", func(j *job) {
			s, e := start.Add(-33*24*time.Hour), end.Add(-33*24*time.Hour)
			j.StartedAt = &s
			j.CompletedAt = &e
		}, 0, false},
		{"boundary crossing charged in full", func(j *job) {
			s, e := start.Add(-32*24*time.Hour), end.Add(-32*24*time.Hour+time.Minute)
			j.StartedAt = &s
			j.CompletedAt = &e
		}, 6, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			j := base
			test.change(&j)
			got, err := jobMinutes(j, now.Add(-32*24*time.Hour))
			if (err != nil) != test.bad || got != test.minutes {
				t.Fatalf("minutes=%d error=%v; want %d bad=%t", got, err, test.minutes, test.bad)
			}
		})
	}
}

// A process-level fake exercises the installed gh argument contract, paginated
// response shape, and API failures without credentials or external writes.
func fakeGH(t *testing.T, responses map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	var cases strings.Builder
	index := 0
	for endpoint, body := range responses {
		file := filepath.Join(dir, fmt.Sprintf("response-%d", index))
		index++
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&cases, "'%s') cat '%s' ;;\n", endpoint, file)
	}
	log := filepath.Join(dir, "calls")
	script := "#!/bin/sh\nset -eu\nendpoint=\nbody=\nmethod=GET\nprevious=\nfor arg in \"$@\"; do\n case \"$arg\" in repos/*) endpoint=$arg;; body=*) body=${arg#body=};; esac\n if [ \"$previous\" = --method ]; then method=$arg; fi\n previous=$arg\ndone\nprintf '%s\\n' \"$*\" >>'" + log + "'\nif [ \"$method\" = PATCH ]; then printf '%s' \"$body\" >'" + filepath.Join(dir, "latest-body") + "'; fi\ncase \"$endpoint\" in\n" + cases.String() + "*) echo \"unexpected endpoint: $endpoint\" >&2; exit 1;;\nesac\n"
	// Let GET comments observe the trusted controller's last PATCH. Other
	// endpoint responses remain immutable, including run-owned identities.
	script = strings.ReplaceAll(script, "cat '", "respond '")
	script = strings.Replace(script, "case \"$endpoint\" in\n", "respond() {\n if [ \"$method\" = GET ] && [ \"$endpoint\" = 'repos/o/r/issues/7/comments?per_page=100' ] && [ -f '"+filepath.Join(dir, "latest-body")+"' ]; then\n  jq --arg body \"$(cat '"+filepath.Join(dir, "latest-body")+"')\" '.[0][0].body=$body' \"$1\"\n else cat \"$1\"; fi\n}\ncase \"$endpoint\" in\n", 1)
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func encode(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestUsageIncludesBothWorkflowsAndAllAttempts(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	start, end := now.Add(-3*time.Minute), now
	j := job{ID: 11, Labels: []string{macroLabel}, Status: "completed", Conclusion: "failure", RunnerName: "macro", StartedAt: &start, CompletedAt: &end}
	j2 := j
	j2.ID = 12
	j2.Conclusion = "success"
	log := fakeGH(t, map[string]string{
		"repos/o/r/actions/workflows/codspeed.yml/runs?per_page=100": `[{"workflow_runs":[{"id":1,"event":"schedule","status":"completed","updated_at":"2026-10-08T12:00:00Z"}]}]`,
		// An old run rerun recently remains included through updated_at.
		"repos/o/r/actions/workflows/pr-benchmark.yml/runs?per_page=100": `[{"workflow_runs":[]},{"workflow_runs":[{"id":2,"event":"pull_request","status":"completed","updated_at":"2026-10-08T12:00:00Z"}]}]`,
		"repos/o/r/actions/runs/1/jobs?filter=all&per_page=100":          `[{"jobs":[]}]`,
		"repos/o/r/actions/runs/2/jobs?filter=all&per_page=100":          encode(t, []map[string][]job{{"jobs": {j}}, {"jobs": {j, j2}}}),
	})
	got, err := collect("o/r", now, 0)
	if err != nil || got != (usage{manual: 8}) {
		t.Fatalf("usage=%+v error=%v", got, err)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(calls), "--paginate --slurp") || !strings.Contains(string(calls), "filter=all") {
		t.Fatal("gh must paginate and include all attempts")
	}
}

func TestUsageAPIFailureStopsAdmission(t *testing.T) {
	fakeGH(t, map[string]string{"repos/o/r/actions/workflows/codspeed.yml/runs?per_page=100": `[{"workflow_runs":[]}]`})
	if _, err := collect("o/r", time.Now(), 0); err == nil {
		t.Fatal("missing PR workflow usage must fail closed")
	}
}

func TestMainRetryCountsEarlierAttemptBeforeReservingCurrentJob(t *testing.T) {
	for _, status := range []string{"queued", "in_progress"} {
		t.Run(status, func(t *testing.T) { testMainRetryReservation(t, status) })
	}
}

func testMainRetryReservation(t *testing.T, status string) {
	t.Helper()
	now := time.Now().UTC()
	start := now.Add(-3 * time.Minute)
	prior := job{ID: 11, Name: "Track main performance", Labels: []string{macroLabel}, Status: "completed", Conclusion: "failure", RunnerName: "macro", StartedAt: &start, CompletedAt: &now}
	active := prior
	active.ID = 12
	active.Status = status
	active.Conclusion = ""
	active.CompletedAt = nil
	fakeGH(t, map[string]string{
		"repos/o/r/actions/workflows/codspeed.yml/runs?per_page=100":     fmt.Sprintf(`[{"workflow_runs":[{"id":1,"event":"schedule","status":"in_progress","updated_at":%q}]}]`, now.Format(time.RFC3339)),
		"repos/o/r/actions/workflows/pr-benchmark.yml/runs?per_page=100": `[{"workflow_runs":[]}]`,
		"repos/o/r/actions/runs/1/jobs?filter=all&per_page=100":          encode(t, []map[string][]job{{"jobs": {prior, active}}}),
	})
	got, err := collect("o/r", now, 1)
	if err != nil || got != (usage{automatic: 4}) {
		t.Fatalf("usage=%+v error=%v", got, err)
	}
	if _, err := collect("o/r", now, 0); err == nil {
		t.Fatal("other admissions must block while this Macro attempt runs")
	}
}

func TestPreparePostsOneCheckboxAndPreservesAnExistingHead(t *testing.T) {
	head := strings.Repeat("a", 40)
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing=%t", existing), func(t *testing.T) {
			comments := [][]comment{{}}
			if existing {
				comments[0] = []comment{testComment(t, head, request{head: head, run: 10, attempt: 2})}
			}
			log := fakeGH(t, map[string]string{
				"repos/o/r/pulls/7":                        `{"number":7,"state":"open","head":{"sha":"` + head + `"}}`,
				"repos/o/r/issues/7/comments?per_page=100": encode(t, comments),
				"repos/o/r/issues/7/comments":              `{}`,
			})
			if err := prepare("o/r", controlEvent{Number: 7}); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			posted := strings.Contains(string(raw), "--method POST")
			if posted == existing {
				t.Fatalf("posted=%t existing=%t", posted, existing)
			}
			if posted && !strings.Contains(string(raw), "- [ ] Run benchmark") {
				t.Fatal("missing benchmark control")
			}
		})
	}
}

func testComment(t *testing.T, head string, r request) comment {
	t.Helper()
	c := comment{ID: 20, Body: commentBody(pull{Number: 7}, r, "Not measured.")}
	c.User.ID = botID
	return c
}

func TestPRRequestBindsHeadRunAndAttempt(t *testing.T) {
	head := strings.Repeat("a", 40)
	for _, test := range []struct {
		name      string
		requested request
		attempt   string
		allowed   bool
	}{
		{"authorized", request{head: head, run: 10, attempt: 2}, "2", true},
		{"stale head", request{head: strings.Repeat("b", 40), run: 10, attempt: 2}, "2", false},
		{"different run", request{head: head, run: 11, attempt: 2}, "2", false},
		{"unapproved rerun", request{head: head, run: 10, attempt: 2}, "3", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := testComment(t, head, test.requested)
			fakeGH(t, map[string]string{
				"repos/o/r/pulls/7":                        `{"number":7,"state":"open","head":{"sha":"` + head + `"}}`,
				"repos/o/r/issues/7/comments?per_page=100": encode(t, [][]comment{{c}}),
			})
			t.Setenv("GITHUB_EVENT_NAME", "pull_request")
			t.Setenv("GITHUB_RUN_ID", "10")
			t.Setenv("GITHUB_RUN_ATTEMPT", test.attempt)
			output := filepath.Join(t.TempDir(), "output")
			if err := os.WriteFile(output, nil, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GITHUB_OUTPUT", output)
			e := controlEvent{}
			e.Pull.Number = 7
			e.Pull.Head.SHA = head
			if err := verifyRequest("o/r", e); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != fmt.Sprintf("allowed=%t\n", test.allowed) {
				t.Fatalf("output %q", raw)
			}
		})
	}
}

func TestUnauthorizedCheckboxIsNotARequest(t *testing.T) {
	head := strings.Repeat("a", 40)
	c := testComment(t, head, request{head: head})
	c.Body = strings.Replace(c.Body, "- [ ] Run benchmark", checkbox, 1)
	e := controlEvent{Action: "edited", Comment: c}
	e.Issue.Number = 7
	e.Issue.Pull = &struct {
		URL string `json:"url"`
	}{URL: "https://api.github.com/repos/o/r/pulls/7"}
	e.Sender.Login = "reader"
	e.Changes.Body = &struct {
		From string `json:"from"`
	}{From: strings.Replace(c.Body, checkbox, "- [ ] Run benchmark", 1)}
	fakeGH(t, map[string]string{"repos/o/r/collaborators/reader/permission": `{"permission":"read"}`})
	if _, _, requested, err := authorizedRequest("o/r", e); err == nil || requested {
		t.Fatal("reader must not authorize spending")
	}
	// Editing an already-checked box, or a human-authored copied marker, cannot
	// trigger another request and should not even need a permission lookup.
	e.Changes.Body.From = c.Body
	if _, _, requested, err := authorizedRequest("o/r", e); err != nil || requested {
		t.Fatalf("existing checked box: %t %v", requested, err)
	}
	e.Changes.Body.From = "- [ ] Run benchmark"
	e.Comment.User.ID = 123
	if _, _, requested, err := authorizedRequest("o/r", e); err != nil || requested {
		t.Fatalf("forged bot marker: %t %v", requested, err)
	}
}

func TestChangedWorkflowCannotGetPaidAdmission(t *testing.T) {
	head := strings.Repeat("a", 40)
	pr := pull{Number: 7}
	pr.Head.SHA = head
	fakeGH(t, map[string]string{
		"repos/o/r/contents/.github/workflows/pr-benchmark.yml?ref=main":    `{"sha":"trusted"}`,
		"repos/o/r/contents/.github/workflows/pr-benchmark.yml?ref=" + head: `{"sha":"changed"}`,
	})
	if err := checkWorkflow("o/r", pr); err == nil {
		t.Fatal("changed workflow must not receive Macro admission")
	}
}

func TestSkippedMeasurementCannotReportSuccess(t *testing.T) {
	r := benchmarkRun{ID: 10, Attempt: 2}
	fakeGH(t, map[string]string{"repos/o/r/actions/runs/10/attempts/2/jobs?per_page=100": `[{"jobs":[{"id":1,"labels":["codspeed-macro-arm64-graviton-ubuntu-22-04"],"status":"completed","conclusion":"skipped","runner_name":""}]}]`})
	if err := completedMeasurement("o/r", r); err == nil {
		t.Fatal("skipped Macro job is not a benchmark success")
	}
}

func TestCheckboxRunsAndReportsOnlyTheAuthorizedPullRequest(t *testing.T) {
	head := strings.Repeat("a", 40)
	c := testComment(t, head, request{head: head})
	c.Body = strings.Replace(c.Body, "- [ ] Run benchmark", checkbox, 1)
	e := controlEvent{Action: "edited", Comment: c}
	e.Issue.Number = 7
	e.Issue.Pull = &struct {
		URL string `json:"url"`
	}{URL: "https://api.github.com/repos/o/r/pulls/7"}
	e.Sender.Login = "maintainer"
	e.Changes.Body = &struct {
		From string `json:"from"`
	}{From: strings.Replace(c.Body, checkbox, "- [ ] Run benchmark", 1)}
	r := benchmarkRun{ID: 10, Head: head, Title: "pr-benchmark #7", Event: "pull_request", Status: "completed", Conclusion: "success", Attempt: 1}
	completed := r
	completed.Attempt = 2
	wrongPR := r
	wrongPR.ID = 20
	wrongPR.Title = "pr-benchmark #8"
	skip := job{ID: 11, Labels: []string{macroLabel}, Status: "completed", Conclusion: "skipped"}
	measured := skip
	measured.ID = 12
	measured.Conclusion = "success"
	measured.RunnerName = "macro"
	log := fakeGH(t, map[string]string{
		"repos/o/r/collaborators/maintainer/permission":                     `{"permission":"write"}`,
		"repos/o/r/pulls/7":                                                 `{"number":7,"state":"open","head":{"sha":"` + head + `"}}`,
		"repos/o/r/issues/7/comments?per_page=100":                          encode(t, [][]comment{{c}}),
		"repos/o/r/issues/comments/20":                                      `{}`,
		"repos/o/r/contents/.github/workflows/pr-benchmark.yml?ref=main":    `{"sha":"trusted"}`,
		"repos/o/r/contents/.github/workflows/pr-benchmark.yml?ref=" + head: `{"sha":"trusted"}`,
		"repos/o/r/actions/workflows/pr-benchmark.yml/runs?event=pull_request&head_sha=" + head + "&per_page=100": encode(t, []map[string][]benchmarkRun{{"workflow_runs": {wrongPR, r}}}),
		"repos/o/r/actions/workflows/codspeed.yml/runs?per_page=100":                                              `[{"workflow_runs":[]}]`,
		"repos/o/r/actions/workflows/pr-benchmark.yml/runs?per_page=100":                                          fmt.Sprintf(`[{"workflow_runs":[{"id":10,"event":"pull_request","status":"completed","updated_at":%q}]}]`, time.Now().UTC().Format(time.RFC3339)),
		"repos/o/r/actions/runs/10/jobs?filter=all&per_page=100":                                                  encode(t, []map[string][]job{{"jobs": {skip}}}),
		"repos/o/r/actions/runs/10/rerun":                                                                         `{}`,
		"repos/o/r/actions/runs/10":                                                                               encode(t, completed),
		"repos/o/r/actions/runs/10/attempts/2/jobs?per_page=100":                                                  encode(t, []map[string][]job{{"jobs": {measured}}}),
	})
	if err := requestBenchmark("o/r", e); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	calls := string(raw)
	if !strings.Contains(calls, "--method POST repos/o/r/actions/runs/10/rerun") || strings.Contains(calls, "runs/20/rerun") {
		t.Fatalf("incorrect rerun:\n%s", calls)
	}
	if !strings.Contains(calls, "head="+head+" run=10 attempt=2") || !strings.Contains(calls, "Benchmark **success**") || !strings.Contains(calls, "/attempts/2") {
		t.Fatalf("missing bound request or result:\n%s", calls)
	}
}
