package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/russross/blackfriday/v2"
)

type fixture struct {
	c            client
	e            event
	r            run
	prHead       string
	parents      []map[string]string
	checks       []check
	jobs         []job
	writes       []map[string]any
	writeMethods []string
	writePaths   []string
	requests     []string
	mutate       func(string, any) any
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	repo := repository{ID: 123, FullName: "owner/repo", DefaultBranch: "main"}
	f := &fixture{prHead: strings.Repeat("c", 40), checks: []check{}}
	f.r = run{ID: 50, Attempt: 1, WorkflowID: 17, Name: "trusted-merge-ci", Path: sourcePath, Event: "merge_group", HeadSHA: strings.Repeat("d", 40), HeadBranch: "gh-readonly-queue/main/pr-7-" + strings.Repeat("b", 40), Status: "completed", Conclusion: "failure", Repository: repo, HeadRepository: repo}
	f.e = event{Action: "completed", Repository: repo, Run: f.r}
	f.parents = []map[string]string{{"sha": strings.Repeat("b", 40)}, {"sha": f.prHead}}
	f.jobs = []job{{ID: 99, RunID: f.r.ID, Attempt: 1, HeadSHA: f.r.HeadSHA, Name: "validation / go", Status: "completed", Conclusion: "failure"}}
	f.jobs[0].Steps = append(f.jobs[0].Steps, struct {
		Name       string `json:"name"`
		Number     int    `json:"number"`
		Conclusion string `json:"conclusion"`
	}{"Run tests", 4, "failure"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer test" {
			t.Error("missing API authentication")
		}
		if req.URL.Path == "/repos/owner/repo/" {
			t.Error("repository endpoint must not have a trailing slash")
			http.NotFound(w, req)
			return
		}
		path := strings.TrimPrefix(strings.TrimPrefix(req.URL.Path, "/repos/owner/repo"), "/")
		f.requests = append(f.requests, path)
		if req.Method != http.MethodGet {
			var body map[string]any
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			f.writes = append(f.writes, body)
			f.writeMethods = append(f.writeMethods, req.Method)
			f.writePaths = append(f.writePaths, path)
			w.WriteHeader(http.StatusCreated)
			return
		}
		var result any
		switch {
		case path == "":
			result = repo
		case path == "actions/workflows/ci-merge-group.yml":
			result = map[string]any{"id": 17, "path": sourcePath, "name": "trusted-merge-ci"}
		case path == fmt.Sprintf("actions/runs/%d/attempts/%d", f.r.ID, f.r.Attempt):
			result = f.r
		case path == "git/commits/"+f.r.HeadSHA:
			result = map[string]any{"sha": f.r.HeadSHA, "parents": f.parents}
		case strings.HasPrefix(path, "commits/"):
			checks := []check{}
			for _, c := range f.checks {
				if c.Name == req.URL.Query().Get("check_name") {
					checks = append(checks, c)
				}
			}
			result = map[string]any{"check_runs": checks, "total_count": len(checks)}
		case path == "pulls/7":
			result = map[string]any{"number": 7, "head": map[string]string{"sha": f.prHead}, "base": map[string]any{"ref": "main", "repo": repo}}
		case path == fmt.Sprintf("actions/runs/%d/attempts/%d/jobs", f.r.ID, f.r.Attempt):
			result = map[string]any{"jobs": f.jobs, "total_count": len(f.jobs)}
		default:
			t.Errorf("unexpected request %s", req.URL)
			http.Error(w, "not found", 404)
			return
		}
		if f.mutate != nil {
			result = f.mutate(path, result)
		}
		if err := json.NewEncoder(w).Encode(result); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	f.c = client{http: server.Client(), base: server.URL, token: "test", repo: repo.FullName}
	return f
}

func (f *fixture) existing(status string) {
	c := check{ID: 777, Name: reportName(f.r), HeadSHA: f.parents[1]["sha"], ExternalID: fmt.Sprintf("hikyo-queue-report:v1:123:7:50:%d:%s:%s", f.r.Attempt, f.r.HeadSHA, f.parents[0]["sha"]), DetailsURL: fmt.Sprintf("https://github.com/owner/repo/actions/runs/50/attempts/%d", f.r.Attempt), Status: status}
	c.App.ID = actionsAppID
	f.checks = []check{c}
}

func TestFailureReportSurvivesDequeueWithoutPullRequestsArray(t *testing.T) {
	f := newFixture(t)
	if err := f.c.publish(context.Background(), f.e); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 1 || f.writeMethods[0] != "POST" {
		t.Fatalf("writes: %+v", f.writes)
	}
	body := f.writes[0]
	if body["head_sha"] != f.prHead || body["conclusion"] != "failure" || body["name"] != "Merge queue validation (run 50, attempt 1)" {
		t.Fatalf("wrong check: %+v", body)
	}
	output := body["output"].(map[string]any)
	for _, fragment := range []string{"PR #7", f.r.HeadSHA, f.parents[0]["sha"], f.prHead, "Run tests", "/actions/runs/50/job/99#step:4:1", "#artifacts", "not reusable merge approval"} {
		if !strings.Contains(output["summary"].(string), fragment) {
			t.Errorf("missing %q in summary", fragment)
		}
	}
	for _, path := range f.requests {
		if strings.Contains(path, "git/ref") || strings.Contains(path, "merge_queue") {
			t.Errorf("unexpected live queue dependency: %s", path)
		}
	}
}

func TestSourceAndRoutingFailClosed(t *testing.T) {
	tests := []struct {
		name  string
		alter func(*fixture)
	}{
		{"wrong event repository", func(f *fixture) { f.e.Repository.ID++ }},
		{"unsupported callback action", func(f *fixture) { f.e.Action = "pull_request_target" }},
		{"wrong source repository", func(f *fixture) { f.r.Repository.ID++ }},
		{"foreign head repository", func(f *fixture) { f.r.HeadRepository.ID++ }},
		{"spoofed workflow", func(f *fixture) { f.r.WorkflowID++ }},
		{"wrong path", func(f *fixture) { f.r.Path = ".github/workflows/evil.yml" }},
		{"wrong event", func(f *fixture) { f.r.Event = "pull_request" }},
		{"malformed branch", func(f *fixture) {
			f.r.HeadBranch = "gh-readonly-queue/main/pr-7-unknown"
			f.e.Run.HeadBranch = f.r.HeadBranch
		}},
		{"wrong SHA callback", func(f *fixture) { f.e.Run.HeadSHA = strings.Repeat("a", 40) }},
		{"wrong branch callback", func(f *fixture) { f.e.Run.HeadBranch = "other" }},
		{"wrong base parent", func(f *fixture) { f.parents[0]["sha"] = strings.Repeat("a", 40) }},
		{"not a two-parent merge", func(f *fixture) { f.parents = f.parents[:1] }},
		{"unsupported topology", func(f *fixture) { f.parents = append(f.parents, map[string]string{"sha": strings.Repeat("e", 40)}) }},
		{"head not captured", func(f *fixture) { f.prHead = strings.Repeat("e", 40) }},
		{"job from different attempt", func(f *fixture) { f.jobs[0].Attempt++ }},
		{"job from different head", func(f *fixture) { f.jobs[0].HeadSHA = strings.Repeat("a", 40) }},
		{"duplicate job", func(f *fixture) { f.jobs = append(f.jobs, f.jobs[0]) }},
		{"unknown conclusion", func(f *fixture) { f.r.Conclusion = "unexpected" }},
		{"wrong check app", func(f *fixture) { f.existing("in_progress"); f.checks[0].App.ID++ }},
		{"duplicate check binding", func(f *fixture) { f.existing("in_progress"); f.checks = append(f.checks, f.checks[0]) }},
		{"wrong check target", func(f *fixture) { f.existing("in_progress"); f.checks[0].HeadSHA = strings.Repeat("a", 40) }},
		{"wrong check link", func(f *fixture) { f.existing("in_progress"); f.checks[0].DetailsURL = "https://evil.test" }},
		{"wrong merge identity", func(f *fixture) {
			f.mutate = func(path string, result any) any {
				if strings.HasPrefix(path, "git/commits/") {
					result.(map[string]any)["sha"] = strings.Repeat("a", 40)
				}
				return result
			}
		}},
		{"retargeted PR", func(f *fixture) {
			f.mutate = func(path string, result any) any {
				if path == "pulls/7" {
					result.(map[string]any)["base"].(map[string]any)["ref"] = "other"
				}
				return result
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.alter(f)
			if err := f.c.publish(context.Background(), f.e); err == nil {
				t.Fatal("accepted invalid source")
			}
			if len(f.writes) != 0 {
				t.Fatalf("unexpected writes: %+v", f.writes)
			}
		})
	}
}

func TestLateFailureUpdatesOnlyCapturedOldHead(t *testing.T) {
	f := newFixture(t)
	f.existing("in_progress")
	f.prHead = strings.Repeat("e", 40)
	if err := f.c.publish(context.Background(), f.e); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 1 || f.writeMethods[0] != "PATCH" || f.writePaths[0] != "check-runs/777" {
		t.Fatalf("writes: %+v", f.writes)
	}
	if _, ok := f.writes[0]["head_sha"]; ok {
		t.Fatal("update attempts to change head")
	}
	if f.writes[0]["conclusion"] != "failure" {
		t.Fatal("late failure lost")
	}
	if !strings.Contains(f.writes[0]["output"].(map[string]any)["summary"].(string), "newer head") {
		t.Fatal("superseded head not disclosed")
	}
}

func TestTerminalReportCannotBeReopenedByDelayedCallback(t *testing.T) {
	f := newFixture(t)
	f.existing("completed")
	f.e.Action = "requested"
	f.r.Status = "in_progress"
	f.r.Conclusion = ""
	if err := f.c.publish(context.Background(), f.e); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 0 {
		t.Fatal("terminal report mutated")
	}
}

func TestRerunCreatesSeparateAttemptCheck(t *testing.T) {
	f := newFixture(t)
	f.existing("completed")
	f.r.Attempt = 2
	f.e.Run = f.r
	f.jobs[0].Attempt = 2
	f.r.Conclusion = "success"
	if err := f.c.publish(context.Background(), f.e); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 1 || f.writeMethods[0] != "POST" {
		t.Fatal("attempt 1 check reused")
	}
	if !strings.Contains(f.writes[0]["external_id"].(string), ":50:2:") {
		t.Fatal("attempt identity missing")
	}
}

func TestLateOldAttemptCannotObscureNewerAttemptContext(t *testing.T) {
	f := newFixture(t)
	f.r.Attempt = 2
	f.existing("completed")
	newer := f.checks[0]
	f.r.Attempt = 1
	f.e.Run = f.r
	if err := f.c.publish(context.Background(), f.e); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 1 || f.writeMethods[0] != "POST" || f.writes[0]["name"] == newer.Name || f.writes[0]["name"] != "Merge queue validation (run 50, attempt 1)" || f.writes[0]["conclusion"] != "failure" {
		t.Fatalf("late callback must preserve an independently named historical result: %+v", f.writes)
	}
	if f.checks[0] != newer {
		t.Fatal("newer terminal check was modified")
	}
}

func TestReportNamesDistinguishSeparateQueueRuns(t *testing.T) {
	if reportName(run{ID: 50, Attempt: 1}) == reportName(run{ID: 51, Attempt: 1}) {
		t.Fatal("separate queue builds share a check context")
	}
}

func TestLiveAttemptStateWinsOverDelayedRequestedEvent(t *testing.T) {
	f := newFixture(t)
	f.e.Action = "requested"
	f.e.Run.Status = "queued"
	f.e.Run.Conclusion = ""
	f.existing("in_progress")
	if err := f.c.publish(context.Background(), f.e); err != nil {
		t.Fatal(err)
	}
	if f.writes[0]["status"] != "completed" || f.writes[0]["conclusion"] != "failure" {
		t.Fatal("stale event state used")
	}
}

func TestCheckPaginationFindsExistingOnLaterPage(t *testing.T) {
	f := newFixture(t)
	f.existing("in_progress")
	want := f.checks[0]
	pages := 0
	f.mutate = func(path string, result any) any {
		if strings.HasPrefix(path, "commits/") {
			pages++
			if pages == 1 {
				checks := make([]check, 100)
				for i := range checks {
					checks[i] = want
					checks[i].ID = int64(i + 1)
					checks[i].ExternalID = "other"
				}
				return map[string]any{"check_runs": checks, "total_count": 101}
			}
			return map[string]any{"check_runs": []check{want}, "total_count": 101}
		}
		return result
	}
	if err := f.c.publish(context.Background(), f.e); err != nil {
		t.Fatal(err)
	}
	if pages != 2 || len(f.writes) != 1 || f.writeMethods[0] != "PATCH" {
		t.Fatal("existing paginated check not found")
	}
}

func TestPaginationOverflowDoesNotPublishPartialEvidence(t *testing.T) {
	f := newFixture(t)
	f.mutate = func(path string, result any) any {
		if strings.HasPrefix(path, "commits/") {
			return map[string]any{"check_runs": make([]check, 100), "total_count": 1001}
		}
		return result
	}
	if err := f.c.publish(context.Background(), f.e); err == nil || !strings.Contains(err.Error(), "pagination") {
		t.Fatalf("error: %v", err)
	}
	if len(f.writes) != 0 {
		t.Fatal("published incomplete evidence")
	}
}

func TestPartialAPIRepliesCannotPublish(t *testing.T) {
	for _, endpoint := range []string{"commits/", "actions/runs/50/attempts/1/jobs"} {
		for _, kind := range []string{"absent array", "null array", "absent count", "truncated", "duplicate"} {
			t.Run(endpoint+"/"+kind, func(t *testing.T) {
				f := newFixture(t)
				f.existing("in_progress")
				f.mutate = func(path string, result any) any {
					if !strings.HasPrefix(path, endpoint) {
						return result
					}
					r := result.(map[string]any)
					key := "jobs"
					if strings.HasPrefix(endpoint, "commits/") {
						key = "check_runs"
					}
					switch kind {
					case "absent array":
						delete(r, key)
					case "null array":
						r[key] = nil
					case "absent count":
						delete(r, "total_count")
					case "truncated":
						r["total_count"] = 2
					case "duplicate":
						r["total_count"] = 2
						if key == "jobs" {
							r[key] = []job{f.jobs[0], f.jobs[0]}
						} else {
							r[key] = []check{f.checks[0], f.checks[0]}
						}
					}
					return r
				}
				if err := f.c.publish(context.Background(), f.e); err == nil {
					t.Fatal("accepted partial API reply")
				}
				if len(f.writes) != 0 {
					t.Fatal("published incomplete evidence")
				}
			})
		}
	}
}

func TestPendingAndAllTerminalStates(t *testing.T) {
	for _, status := range []string{"queued", "requested", "waiting", "pending", "in_progress"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t)
			f.r.Status = status
			f.r.Conclusion = ""
			if err := f.c.publish(context.Background(), f.e); err != nil {
				t.Fatal(err)
			}
			if _, ok := f.writes[0]["conclusion"]; ok {
				t.Fatal("pending result has conclusion")
			}
		})
	}
	for _, conclusion := range []string{"success", "failure", "neutral", "cancelled", "timed_out", "action_required", "skipped", "stale"} {
		t.Run(conclusion, func(t *testing.T) {
			f := newFixture(t)
			f.r.Conclusion = conclusion
			if err := f.c.publish(context.Background(), f.e); err != nil {
				t.Fatal(err)
			}
			if f.writes[0]["conclusion"] != conclusion {
				t.Fatal("conclusion lost")
			}
		})
	}
}

func TestMatchedCheckFollowedByUnmatchedEntriesKeepsExactPatchTarget(t *testing.T) {
	f := newFixture(t)
	f.existing("in_progress")
	for _, id := range []int64{888, 999} {
		unmatched := f.checks[0]
		unmatched.ID = id
		unmatched.ExternalID = fmt.Sprintf("different-binding-%d", id)
		unmatched.Status = "completed"
		f.checks = append(f.checks, unmatched)
	}
	if err := f.c.publish(context.Background(), f.e); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 1 || f.writeMethods[0] != http.MethodPatch || f.writePaths[0] != "check-runs/777" {
		t.Fatalf("matching check was replaced by a later loop entry: methods=%v paths=%v", f.writeMethods, f.writePaths)
	}
}

type rejectNetworkTransport struct {
	requests int
}

func (transport *rejectNetworkTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.requests++
	return nil, errors.New("test forbids network access")
}

func TestExecuteRejectsInvalidRepositoryBeforeAnyHTTPRequest(t *testing.T) {
	f := newFixture(t)
	data, err := json.Marshal(f.e)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_EVENT_NAME", "workflow_run")
	t.Setenv("GITHUB_EVENT_PATH", path)
	t.Setenv("GH_TOKEN", "test-token")
	transport := &rejectNetworkTransport{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	for _, repo := range []string{"", "owner", "/repo", "owner/", "owner/repo/extra", "owner/repo?query=1", "owner/repo#fragment", "https://github.com/owner/repo", "owner/repo\n", "owner name/repo"} {
		t.Run(repo, func(t *testing.T) {
			t.Setenv("GH_REPO", repo)
			if err := execute(); err == nil || err.Error() != "invalid repository or source run identity" {
				t.Fatalf("invalid GH_REPO %q did not fail at input validation: %v", repo, err)
			}
			if transport.requests != 0 || len(f.requests) != 0 || len(f.writes) != 0 {
				t.Fatal("invalid repository reached an HTTP request or write")
			}
		})
	}
}

func TestMaliciousLabelsCannotInjectReportLinksHTMLOrLines(t *testing.T) {
	f := newFixture(t)
	runURL := "https://github.com/owner/repo/actions/runs/50/attempts/1"
	baseline := render(f.r, 7, f.parents[0]["sha"], f.prHead, runURL, f.jobs, false)
	for _, malicious := range []string{
		"](https://attacker.example) [forged](https://attacker.example)",
		`\](https://attacker.example) ![image](https://attacker.example/image)`,
		"<script>alert(1)</script><img src=x onerror=alert(1)>",
		"`code` **bold** _emphasis_\r\n- [injected](https://attacker.example)",
		"&#91;forged&#93;(https://attacker.example) &lt;script&gt;alert(1)&lt;/script&gt;",
	} {
		t.Run(malicious, func(t *testing.T) {
			f.jobs[0].Name = malicious
			f.jobs[0].Steps[0].Name = malicious
			summary := render(f.r, 7, f.parents[0]["sha"], f.prHead, runURL, f.jobs, false)
			if strings.Count(summary, "\n") != strings.Count(baseline, "\n") || strings.Contains(summary, "\r") {
				t.Fatal("label injected new report lines")
			}
			// Parse the report as Markdown: source escaping alone would not prove
			// that a malicious label cannot become a live link or HTML element.
			html := string(blackfriday.Run([]byte(summary)))
			for _, forbidden := range []string{`href="https://attacker.example`, "<script", "<img", "<strong>", "<em>", "<code>code</code>"} {
				if strings.Contains(html, forbidden) {
					t.Errorf("label introduced active markup %q: %s", forbidden, html)
				}
			}
			if strings.Count(html, "<a ") != 4 {
				t.Fatalf("expected only run, artifact, job and step links: %s", html)
			}
			for _, trusted := range []string{runURL, "https://github.com/owner/repo/actions/runs/50#artifacts", "https://github.com/owner/repo/actions/runs/50/job/99", "https://github.com/owner/repo/actions/runs/50/job/99#step:4:1"} {
				if !strings.Contains(html, `href="`+trusted+`"`) {
					t.Errorf("trusted report link lost: %s", trusted)
				}
			}
		})
	}
}
