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
	"time"

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
	writeMutate  func(check) check
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
			for i, current := range f.checks {
				if req.Method == "PATCH" && path == fmt.Sprintf("check-runs/%d", current.ID) {
					if value, ok := body["status"].(string); ok {
						current.Status = value
					}
					if value, ok := body["conclusion"].(string); ok {
						current.Conclusion = value
					}
					if value, ok := body["details_url"].(string); ok {
						current.DetailsURL = value
					}
					if f.writeMutate != nil {
						current = f.writeMutate(current)
					}
					f.checks[i] = current
					if err := json.NewEncoder(w).Encode(current); err != nil {
						t.Error(err)
					}
					return
				}
			}
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
		case path == "check-runs/777":
			if len(f.checks) == 0 {
				http.NotFound(w, req)
				return
			}
			result = f.checks[0]
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

func TestGitHubCanonicalCheckURLUpdatesExactExistingReport(t *testing.T) {
	for _, status := range []string{"queued", "in_progress", "completed"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t)
			f.existing(status)
			// Actions rewrites details_url to the check's own run URL.
			f.checks[0].DetailsURL = "https://github.com/owner/repo/runs/777"
			if err := f.c.publish(context.Background(), f.e); err != nil {
				t.Fatal(err)
			}
			if status == "completed" {
				if len(f.writes) != 0 {
					t.Fatal("terminal report was rewritten")
				}
			} else if len(f.writes) != 1 || f.writeMethods[0] != "PATCH" || f.writePaths[0] != "check-runs/777" {
				t.Fatalf("existing report not updated: %+v", f.writes)
			}
		})
	}
}

func TestCanonicalCheckURLCannotRedirectReportOwnership(t *testing.T) {
	for _, link := range []string{
		"https://github.com/owner/repo/runs/778",
		"https://github.com/other/repo/runs/777",
		"https://evil.test/owner/repo/runs/777",
		"https://github.com/owner/repo/runs/777?redirect=other",
		"http://github.com/owner/repo/runs/777",
	} {
		t.Run(link, func(t *testing.T) {
			f := newFixture(t)
			f.existing("in_progress")
			f.checks[0].DetailsURL = link
			if err := f.c.publish(context.Background(), f.e); err == nil || len(f.writes) != 0 {
				t.Fatalf("accepted unauthenticated check URL %q", link)
			}
		})
	}
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
	if title := output["title"].(string); title != "PR #7: 1 failed, 0 done (first failure: validation / go)" {
		t.Errorf("title: %q", title)
	}
	if text := output["text"].(string); !strings.Contains(text, "/actions/runs/50/job/99#step:4:1") {
		t.Errorf("text lacks the failing step link: %q", text)
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
		strings.Repeat("<", 400),
	} {
		t.Run(malicious, func(t *testing.T) {
			f.jobs[0].Name = malicious
			f.jobs[0].Steps[0].Name = malicious
			got := render(f.r, 7, f.parents[0]["sha"], f.prHead, runURL, f.jobs, false)
			if strings.ContainsAny(got.title, "<>\r\n") || len([]rune(got.title)) >= 200 {
				t.Fatalf("unsafe or oversized title (%d runes): %q", len([]rune(got.title)), got.title)
			}
			// Parse each body as Markdown: source escaping alone would not prove
			// that a malicious label cannot become a live link or HTML element.
			for _, part := range []struct {
				name, body, baseline string
				links                []string
			}{
				{"summary", got.summary, baseline.summary, []string{runURL, "https://github.com/owner/repo/actions/runs/50#artifacts", "https://github.com/owner/repo/actions/runs/50/job/99", "https://github.com/owner/repo/actions/runs/50/job/99#step:4:1"}},
				{"text", got.text, baseline.text, []string{"https://github.com/owner/repo/actions/runs/50/job/99", "https://github.com/owner/repo/actions/runs/50/job/99#step:4:1"}},
			} {
				if strings.Count(part.body, "\n") != strings.Count(part.baseline, "\n") || strings.Contains(part.body, "\r") {
					t.Fatalf("label injected new %s lines", part.name)
				}
				html := string(blackfriday.Run([]byte(part.body)))
				for _, forbidden := range []string{`href="https://attacker.example`, "<script", "<img", "<strong>", "<em>", "<code>code</code>"} {
					if strings.Contains(html, forbidden) {
						t.Errorf("label introduced active markup %q in %s: %s", forbidden, part.name, html)
					}
				}
				if strings.Count(html, "<a ") != len(part.links) {
					t.Fatalf("expected only trusted links in %s: %s", part.name, html)
				}
				for _, trusted := range part.links {
					if !strings.Contains(html, `href="`+trusted+`"`) {
						t.Errorf("trusted report link lost from %s: %s", part.name, trusted)
					}
				}
			}
		})
	}
}

// fixedClock pins the Updated stamp and returns its expected UTC rendering.
func fixedClock(t *testing.T) string {
	t.Helper()
	previous := now
	now = func() time.Time { return time.Date(2026, 10, 19, 14, 5, 9, 0, time.FixedZone("test", 2*3600)) }
	t.Cleanup(func() { now = previous })
	return "2026-10-19T12:05:09Z"
}

func testJob(id int64, name, status, conclusion string) job {
	return job{ID: id, RunID: 50, Attempt: 1, HeadSHA: strings.Repeat("d", 40), Name: name, Status: status, Conclusion: conclusion}
}

func withStep(j job, name string, number int, conclusion string) job {
	j.Steps = append(j.Steps, struct {
		Name       string `json:"name"`
		Number     int    `json:"number"`
		Conclusion string `json:"conclusion"`
	}{name, number, conclusion})
	return j
}

func renderJobs(r run, jobs ...job) rendered {
	r.HeadSHA = strings.Repeat("d", 40)
	return render(r, 995, strings.Repeat("b", 40), strings.Repeat("c", 40), "https://github.com/owner/repo/actions/runs/50/attempts/1", jobs, false)
}

func TestTitleLeadsWithProgressAndFirstFailure(t *testing.T) {
	inProgress := run{ID: 50, Attempt: 1, Status: "in_progress"}
	succeeded := run{ID: 50, Attempt: 1, Status: "completed", Conclusion: "success"}
	failedRun := run{ID: 50, Attempt: 1, Status: "completed", Conclusion: "failure"}
	cancelledRun := run{ID: 50, Attempt: 1, Status: "completed", Conclusion: "cancelled"}
	var partial []job
	for i := 0; i < 12; i++ {
		partial = append(partial, testJob(int64(100+i), fmt.Sprintf("web (desktop, %d)", i), "in_progress", ""))
	}
	for i := 0; i < 5; i++ {
		partial = append(partial, testJob(int64(200+i), fmt.Sprintf("lint %d", i), "completed", "success"))
	}
	tests := []struct {
		name string
		run  run
		jobs []job
		want string
	}{
		{"no jobs yet", run{ID: 50, Attempt: 1, Status: "queued"}, nil, "PR #995: no jobs reported yet"},
		{"partial reporting never claims a total", inProgress, partial, "PR #995: 17 reported, 0 failed, 12 running"},
		{"queued jobs are counted", inProgress, []job{testJob(1, "a", "completed", "success"), testJob(2, "b", "queued", ""), testJob(3, "c", "waiting", "")}, "PR #995: 3 reported, 0 failed, 0 running, 2 queued"},
		{"reported jobs done but run still going", inProgress, []job{testJob(1, "a", "completed", "success")}, "PR #995: 1 reported, 0 failed, 0 running"},
		{"failure names the first failed job in API order", inProgress, []job{
			testJob(1, "plan", "completed", "success"),
			testJob(2, "web (desktop, 3)", "completed", "failure"),
			testJob(3, "api", "completed", "timed_out"),
			testJob(4, "docs", "completed", "skipped"),
			testJob(5, "e2e", "in_progress", ""),
			testJob(6, "bench", "queued", ""),
		}, "PR #995: 2 failed, 2 done, 1 running, 1 queued (first failure: web (desktop, 3))"},
		{"cancelled-only run is not reported as a failure", cancelledRun, []job{
			testJob(1, "plan", "completed", "success"),
			testJob(2, "web", "completed", "cancelled"),
			testJob(3, "api", "completed", "cancelled"),
		}, "PR #995: 2 cancelled, 1 done"},
		{"cancelled-only run keeps unfinished counts", inProgress, []job{
			testJob(1, "plan", "completed", "success"),
			testJob(2, "web", "completed", "cancelled"),
			testJob(3, "e2e", "in_progress", ""),
			testJob(4, "bench", "queued", ""),
		}, "PR #995: 1 cancelled, 1 done, 1 running, 1 queued"},
		{"first failure skips an earlier cancelled job", failedRun, []job{
			testJob(1, "plan", "completed", "success"),
			testJob(2, "web", "completed", "cancelled"),
			testJob(3, "api", "completed", "failure"),
			testJob(4, "e2e", "completed", "cancelled"),
			testJob(5, "docs", "completed", "skipped"),
		}, "PR #995: 1 failed, 2 cancelled, 2 done (first failure: api)"},
		{"failure plus cancelled keeps unfinished counts", inProgress, []job{
			testJob(1, "web", "completed", "cancelled"),
			testJob(2, "api", "completed", "timed_out"),
			testJob(3, "e2e", "in_progress", ""),
			testJob(4, "bench", "queued", ""),
		}, "PR #995: 1 failed, 1 cancelled, 0 done, 1 running, 1 queued (first failure: api)"},
		{"action_required job is a failure", failedRun, []job{testJob(1, "a", "completed", "success"), testJob(2, "deploy", "completed", "action_required")}, "PR #995: 1 failed, 1 done (first failure: deploy)"},
		{"stale job is a failure", failedRun, []job{testJob(1, "a", "completed", "success"), testJob(2, "old", "completed", "stale")}, "PR #995: 1 failed, 1 done (first failure: old)"},
		{"completed job without a conclusion is a failure", failedRun, []job{testJob(1, "a", "completed", "success"), testJob(2, "odd", "completed", "")}, "PR #995: 1 failed, 1 done (first failure: odd)"},
		{"action_required job never reads as all passed", succeeded, []job{testJob(1, "a", "completed", "success"), testJob(2, "deploy", "completed", "action_required")}, "PR #995: 1 failed, 1 done (first failure: deploy)"},
		{"neutral counts as passed", succeeded, []job{testJob(1, "a", "completed", "success"), testJob(2, "b", "completed", "neutral")}, "PR #995: all 2 jobs passed"},
		{"completed run without jobs reports the run conclusion", failedRun, nil, "PR #995: run failure, no jobs reported"},
		{"completed cancelled run without jobs", cancelledRun, nil, "PR #995: run cancelled, no jobs reported"},
		{"all passed", succeeded, []job{testJob(1, "a", "completed", "success"), testJob(2, "b", "completed", "success")}, "PR #995: all 2 jobs passed"},
		{"all passed with one job is singular", succeeded, []job{testJob(1, "a", "completed", "success")}, "PR #995: 1 job passed"},
		{"all passed with skipped jobs", succeeded, []job{testJob(1, "a", "completed", "success"), testJob(2, "b", "completed", "skipped")}, "PR #995: 1 passed, 1 skipped, 0 failed"},
		{"failed run without a failed job is not reported as passed", failedRun, []job{testJob(1, "a", "completed", "success")}, "PR #995: run failure, 1 reported, 0 failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderJobs(tt.run, tt.jobs...).title; got != tt.want {
				t.Fatalf("title %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTitleBoundsHostileFirstFailureName(t *testing.T) {
	hostile := "<b>\n" + strings.Repeat("x_", 300)
	got := renderJobs(run{ID: 50, Attempt: 1, Status: "in_progress"}, testJob(1, hostile, "completed", "failure")).title
	if len([]rune(got)) >= 200 || strings.ContainsAny(got, "<>\r\n") || !strings.Contains(got, "(first failure: b x_x_") {
		t.Fatalf("title not sanitized and bounded (%d runes): %q", len([]rune(got)), got)
	}
}

func TestTitleFallsBackToJobIDWhenNameSanitizesToNothing(t *testing.T) {
	for _, name := range []string{"", "<>", " \n\t\x00<<>> ", "\u200b\u2060"} {
		got := renderJobs(run{ID: 50, Attempt: 1, Status: "in_progress"}, testJob(42, name, "completed", "failure")).title
		if want := "PR #995: 1 failed, 0 done (first failure: job 42)"; got != want {
			t.Errorf("name %q: title %q, want %q", name, got, want)
		}
	}
}

func TestTitleKeepsPlainJobNamesVerbatim(t *testing.T) {
	got := renderJobs(run{ID: 50, Attempt: 1, Status: "in_progress"}, testJob(1, "test_core", "completed", "timed_out")).title
	if want := "PR #995: 1 failed, 0 done (first failure: test_core)"; got != want {
		t.Fatalf("title %q, want %q", got, want)
	}
}

func TestTextListsFailedFirstThenUnfinishedPassedSkipped(t *testing.T) {
	got := renderJobs(run{ID: 50, Attempt: 1, Status: "in_progress"},
		testJob(1, "pass-a", "completed", "success"),
		testJob(2, "skip-b", "completed", "skipped"),
		testJob(3, "run-c", "in_progress", ""),
		testJob(4, "fail-d", "completed", "failure"),
		testJob(5, "queue-e", "queued", ""),
		testJob(6, "cancel-f", "completed", "cancelled"),
		testJob(7, "pass-g", "completed", "neutral"),
		testJob(8, "fail-h", "completed", "timed_out"),
	).text
	last := -1
	for _, id := range []int{4, 6, 8, 3, 5, 1, 7, 2} {
		at := strings.Index(got, fmt.Sprintf("/job/%d)", id))
		if at < 0 || at < last {
			t.Fatalf("job %d out of order or missing in:\n%s", id, got)
		}
		last = at
	}
	for _, line := range []string{
		"[fail-d](https://github.com/owner/repo/actions/runs/50/job/4): completed failure\n",
		"[queue-e](https://github.com/owner/repo/actions/runs/50/job/5): queued\n",
		"[skip-b](https://github.com/owner/repo/actions/runs/50/job/2): completed skipped\n",
	} {
		if !strings.Contains(got, line) {
			t.Errorf("missing %q in text:\n%s", line, got)
		}
	}
}

func TestSummaryLeadsWithRunLinkThenCountsThenUpdated(t *testing.T) {
	stamp := fixedClock(t)
	failing := withStep(withStep(testJob(2, "web (desktop, 3)", "completed", "failure"), "Run tests", 6, "failure"), "Set up job", 1, "success")
	got := renderJobs(run{ID: 50, Attempt: 1, Status: "in_progress"}, testJob(1, "plan", "completed", "success"), failing, testJob(3, "e2e", "in_progress", "")).summary
	blocks := strings.Split(got, "\n\n")
	if want := "[Run 50, attempt 1](https://github.com/owner/repo/actions/runs/50/attempts/1): in\\_progress"; blocks[0] != want {
		t.Fatalf("run link is not first: %q", blocks[0])
	}
	if want := "3 jobs reported so far (more may appear as their dependencies finish): 1 failed or cancelled, 1 running, 0 queued, 1 passed, 0 skipped."; blocks[1] != want {
		t.Fatalf("counts line %q, want %q", blocks[1], want)
	}
	if blocks[2] != "Updated: "+stamp {
		t.Fatalf("updated line %q, want stamp %q", blocks[2], stamp)
	}
	for _, want := range []string{
		"### Failed or cancelled jobs",
		"- [web (desktop, 3)](https://github.com/owner/repo/actions/runs/50/job/2): completed failure\n  - [Run tests](https://github.com/owner/repo/actions/runs/50/job/2#step:6:1): failure\n",
		"not reusable merge approval",
		"- PR head: `" + strings.Repeat("c", 40) + "`",
		"- Tested base: `" + strings.Repeat("b", 40) + "`",
		"- Tested merge: `" + strings.Repeat("d", 40) + "`",
		"#artifacts",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"plan", "e2e", "Set up job"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("summary lists non-failing %q:\n%s", unwanted, got)
		}
	}
}

func TestCompletedRunCountsAreFinal(t *testing.T) {
	got := renderJobs(run{ID: 50, Attempt: 1, Status: "completed", Conclusion: "success"}, testJob(1, "plan", "completed", "success")).summary
	if !strings.Contains(got, "\n\n1 job: 0 failed or cancelled, 0 running, 0 queued, 1 passed, 0 skipped.\n\n") || strings.Contains(got, "so far") {
		t.Errorf("completed run counts hedged or not singular:\n%s", got)
	}
}

func TestSummaryUsesSingularJobWhileRunning(t *testing.T) {
	got := renderJobs(run{ID: 50, Attempt: 1, Status: "in_progress"}, testJob(1, "plan", "completed", "success")).summary
	if want := "\n\n1 job reported so far (more may appear as their dependencies finish): 0 failed or cancelled, 0 running, 0 queued, 1 passed, 0 skipped.\n\n"; !strings.Contains(got, want) {
		t.Errorf("summary lacks singular running counts:\n%s", got)
	}
}

func TestSummaryListsCancelledJobsWithFailures(t *testing.T) {
	got := renderJobs(run{ID: 50, Attempt: 1, Status: "completed", Conclusion: "cancelled"},
		testJob(1, "plan", "completed", "success"),
		testJob(2, "web", "completed", "cancelled"),
		testJob(3, "api", "completed", "action_required"),
	).summary
	for _, want := range []string{
		"2 failed or cancelled",
		"### Failed or cancelled jobs",
		"- [web](https://github.com/owner/repo/actions/runs/50/job/2): completed cancelled\n",
		"- [api](https://github.com/owner/repo/actions/runs/50/job/3): completed action\\_required\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}
}

func TestSummaryStatesWhenNothingFailed(t *testing.T) {
	fixedClock(t)
	got := renderJobs(run{ID: 50, Attempt: 1, Status: "in_progress"}, testJob(1, "plan", "completed", "success")).summary
	if !strings.Contains(got, "\nNo failed or cancelled jobs reported.\n") || strings.Contains(got, "### Failed or cancelled jobs") {
		t.Errorf("nothing-failed state not stated in one line:\n%s", got)
	}
}

func TestSummaryOfRunWithoutJobsDoesNotClaimNoFailures(t *testing.T) {
	fixedClock(t)
	for _, r := range []run{{ID: 50, Attempt: 1, Status: "in_progress"}, {ID: 50, Attempt: 1, Status: "completed", Conclusion: "failure"}} {
		got := renderJobs(r).summary
		if strings.Contains(got, "No failed or cancelled jobs reported.") || strings.Contains(got, "### Failed or cancelled jobs") || strings.Contains(got, "The full job list follows") {
			t.Errorf("zero-job summary makes a per-job claim:\n%s", got)
		}
	}
}

func TestSummaryAndTextStayUnderCapWithManyJobs(t *testing.T) {
	var jobs []job
	for i := 0; i < 1000; i++ {
		name := fmt.Sprintf("%04d %s", i, strings.Repeat("n", 150))
		jobs = append(jobs, withStep(testJob(int64(1000+i), name, "completed", "failure"), strings.Repeat("s", 150), 3, "failure"))
	}
	got := renderJobs(run{ID: 50, Attempt: 1, Status: "completed", Conclusion: "failure"}, jobs...)
	const omitted = "Additional jobs omitted; open the source run for the complete list."
	for name, body := range map[string]string{"summary": got.summary, "text": got.text} {
		// The cap is checked before each entry, so one entry may overshoot it.
		if len(body) > reportCap+1000 {
			t.Errorf("%s is %d bytes", name, len(body))
		}
		if !strings.Contains(body, omitted) {
			t.Errorf("%s does not say jobs were omitted", name)
		}
	}
	if !strings.Contains(got.summary, "not reusable merge approval") || !strings.Contains(got.summary, "#artifacts") {
		t.Error("summary footer lost after the cap")
	}
	if !strings.Contains(got.text, "/job/1000)") || strings.Contains(got.text, "/job/1999)") {
		t.Error("text cap did not truncate the tail")
	}
}
