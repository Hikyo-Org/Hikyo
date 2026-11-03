package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func devFilesPage(f *queueFixture, page int) string {
	return fmt.Sprintf("pulls/%d/files?per_page=100&page=%d", f.pr.Number, page)
}

func newDevFixture(t *testing.T) *queueFixture {
	t.Helper()
	f := newQueueFixture(t)
	f.c.checkName = devCheckName
	f.pr.Base.Ref = "dev"
	f.refresh()
	f.api.responses[f.c.endpoint("git/ref/heads/dev")] = map[string]any{"ref": "refs/heads/dev", "object": map[string]string{"type": "commit", "sha": f.pr.Base.SHA}}
	main := strings.Repeat("e", 40)
	f.api.responses[f.c.endpoint("git/ref/heads/main")] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": main}}
	f.rules = f.rules[:len(f.rules)-1]
	f.rules[3] = encodedRule("pull_request", map[string]any{"required_review_thread_resolution": true, "dismiss_stale_reviews_on_push": true, "allowed_merge_methods": []string{"merge"}})
	f.rules[4] = encodedRule("required_status_checks", map[string]any{"strict_required_status_checks_policy": false, "do_not_enforce_on_create": false, "required_status_checks": []map[string]any{{"context": devCheckName, "integration_id": actionsAppID}}})
	f.owner = map[string]any{"id": 31, "name": devRulesetName, "target": "branch", "enforcement": "active", "bypass_actors": []any{}, "updated_at": "2026-11-01T00:00:00Z", "conditions": map[string]any{"ref_name": map[string]any{"include": []string{"refs/heads/dev"}, "exclude": []string{}}}, "rules": f.rules}
	f.api.responses[f.c.endpoint("rulesets?per_page=100&page=1")] = []map[string]any{{"id": 31, "name": devRulesetName}}
	f.api.responses[f.c.endpoint("rulesets/31")] = f.owner
	f.api.responses[f.c.endpoint("rules/branches/dev")] = f.rules
	for _, revision := range []string{main, f.pr.Base.SHA, f.pr.Head.SHA} {
		f.api.responses[f.c.endpoint("contents/"+devGateWorkflowPath+"?ref="+revision)] = map[string]string{"type": "file", "path": devGateWorkflowPath, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("trusted dev publisher"))}
	}
	f.api.responses[f.c.endpoint(devFilesPage(f, 1))] = []map[string]string{{"filename": "internal/app/app.go"}}
	f.feedbackRun.Pulls = []pull{f.pr}
	f.feedbackRefresh()
	r := f.feedbackRun
	f.api.responses[f.c.endpoint(fmt.Sprintf("actions/runs/%d/attempts/%d/jobs?per_page=100&page=1", r.ID, r.Attempt))] = map[string]any{"total_count": 3, "jobs": []job{
		{ID: 201, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: "feedback (go)", Status: "completed", Conclusion: "success"},
		{ID: 202, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: "feedback (frontend)", Status: "completed", Conclusion: "success"},
		{ID: 203, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: "feedback (tooling)", Status: "completed", Conclusion: "success"},
	}}
	return f
}

func TestDevAdmissionReusesCanonicalProofWithoutQueueAuthority(t *testing.T) {
	f := newDevFixture(t)
	dcoCalls := 0
	f.q.dco = func(_ context.Context, p pull) (int, error) {
		dcoCalls++
		if p.Base.Ref != "dev" || f.c.repo.DefaultBranch != "main" {
			t.Fatal("integration target replaced the trusted default branch")
		}
		return 0, nil
	}
	if err := f.q.admit(context.Background(), f.pr.Number); err != nil {
		t.Fatal(err)
	}
	if f.verifyCalls != 2 || dcoCalls != 2 || len(f.api.checks) != 1 || f.api.checks[0].Conclusion != "success" || f.api.checks[0].Name != devCheckName {
		t.Fatalf("dev did not compose independent current authority/DCO checks: policy=%d DCO=%d checks=%+v", f.verifyCalls, dcoCalls, f.api.checks)
	}
	for path := range f.api.reads {
		if strings.HasPrefix(path, "graphql:") || strings.Contains(path, "/actions/workflows/ci-merge-group.yml") || strings.Contains(path, "/rulesets/20539346") {
			t.Fatalf("dev admission borrowed full queue authority: %s", path)
		}
	}
	bound, err := decodeBinding(f.api.checks[0].ExternalID)
	if err != nil || bound != newBinding(f.c.repo.ID, f.pr, &f.feedbackRun) {
		t.Fatal("dev result lost exact source, base, run or attempt binding")
	}
}

func TestDevAdmissionRequiresEveryFeedbackLane(t *testing.T) {
	for _, mutation := range []string{"missing tooling", "duplicated tooling", "wrong tooling head", "tooling failure", "tooling skipped", "frontend failure"} {
		t.Run(mutation, func(t *testing.T) {
			f := newDevFixture(t)
			path := f.c.endpoint(fmt.Sprintf("actions/runs/%d/attempts/%d/jobs?per_page=100&page=1", f.feedbackRun.ID, f.feedbackRun.Attempt))
			data := f.api.responses[path].(map[string]any)
			jobs := data["jobs"].([]job)
			switch mutation {
			case "missing tooling":
				jobs = jobs[:2]
			case "duplicated tooling":
				duplicate := jobs[2]
				duplicate.ID++
				jobs = append(jobs, duplicate)
			case "wrong tooling head":
				jobs[2].HeadSHA = strings.Repeat("f", 40)
			case "tooling failure":
				jobs[2].Conclusion = "failure"
			case "tooling skipped":
				jobs[2].Conclusion = "skipped"
			case "frontend failure":
				jobs[1].Conclusion = "failure"
			}
			f.api.responses[path] = map[string]any{"total_count": len(jobs), "jobs": jobs}
			_ = f.q.admit(context.Background(), f.pr.Number)
			f.assertNoSuccess(t)
		})
	}
}

func TestDevAdmissionRejectsChangedOrWeakenedAuthority(t *testing.T) {
	for _, mutation := range []string{"bootstrap", "queue", "main context", "main target", "missing signatures", "publisher head", "publisher base", "default branch", "native collision", "same head main PR", "DCO", "workflow authority"} {
		t.Run(mutation, func(t *testing.T) {
			f := newDevFixture(t)
			switch mutation {
			case "bootstrap":
				f.rules[4] = encodedRule("required_status_checks", map[string]any{"strict_required_status_checks_policy": false, "do_not_enforce_on_create": true, "required_status_checks": []map[string]string{{"context": devCheckName}}})
			case "queue":
				f.rules = append(f.rules, encodedRule("merge_queue", queueParameters{}))
			case "main context":
				f.rules[4] = encodedRule("required_status_checks", map[string]any{"strict_required_status_checks_policy": false, "do_not_enforce_on_create": false, "required_status_checks": []map[string]string{{"context": queueCheckName}}})
			case "main target":
				f.pr.Base.Ref = "main"
				f.api.responses[f.c.endpoint("pulls/7")] = f.pr
			case "missing signatures":
				f.rules = append(f.rules[:2], f.rules[3:]...)
			case "publisher head", "publisher base":
				revision := f.pr.Head.SHA
				if mutation == "publisher base" {
					revision = f.pr.Base.SHA
				}
				f.api.responses[f.c.endpoint("contents/"+devGateWorkflowPath+"?ref="+revision)] = map[string]string{"type": "file", "path": devGateWorkflowPath, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("candidate-controlled publisher"))}
			case "default branch":
				f.c.repo.DefaultBranch = "dev"
			case "native collision":
				v := check{ID: 99, Name: devCheckName, HeadSHA: f.pr.Head.SHA, Status: "completed", Conclusion: "success"}
				v.App.ID, v.App.Slug = actionsAppID, "github-actions"
				f.api.checks = []check{v}
			case "same head main PR":
				other := f.pr
				other.Number, other.Base.Ref = 8, "main"
				f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{f.pr, other}
			case "DCO":
				f.q.dco = func(context.Context, pull) (int, error) { return 1, nil }
			case "workflow authority":
				f.verifyCode = 1
			}
			f.owner["rules"] = f.rules
			f.api.responses[f.c.endpoint("rules/branches/dev")] = f.rules
			_ = f.q.admit(context.Background(), f.pr.Number)
			f.assertNoSuccess(t)
		})
	}
}

func TestDevAdmissionRechecksLiveBaseAndApprovals(t *testing.T) {
	for _, mutation := range []string{"dev advanced", "approval withdrawn", "DCO withdrawn", "policy moved"} {
		t.Run(mutation, func(t *testing.T) {
			f := newDevFixture(t)
			if mutation == "dev advanced" {
				f.api.readHook = func(path string, count int) {
					if path == f.c.endpoint("git/ref/heads/dev") && count == 2 {
						f.api.responses[path] = map[string]any{"ref": "refs/heads/dev", "object": map[string]string{"type": "commit", "sha": strings.Repeat("f", 40)}}
					}
				}
			}
			if mutation == "approval withdrawn" {
				calls := 0
				f.c.verify = func(context.Context, int, string) (policyDecision, error) {
					calls++
					if calls == 2 {
						return policyDecision{Code: 1}, nil
					}
					return policyDecision{}, nil
				}
			}
			if mutation == "DCO withdrawn" {
				calls := 0
				f.q.dco = func(context.Context, pull) (int, error) {
					calls++
					if calls == 2 {
						return 1, nil
					}
					return 0, nil
				}
			}
			if mutation == "policy moved" {
				f.api.readHook = func(path string, count int) {
					if path == f.c.endpoint("rulesets/31") && count == 2 {
						f.owner["updated_at"] = "2026-11-01T00:01:00Z"
					}
				}
			}
			_ = f.q.admit(context.Background(), f.pr.Number)
			f.assertNoSuccess(t)
		})
	}
}

// The hosted path: a moved admission fails the job, so ci-queue-retry reruns it.
func TestDevMovementFailsSoTheRetryReassesses(t *testing.T) {
	t.Setenv("GITHUB_REF", "refs/heads/main")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1") // The barrier would give way to attempt 3's terminal refusal.
	moves := map[string]func(*queueFixture){
		"none": func(*queueFixture) {},
		"dev advanced": func(f *queueFixture) {
			verify := f.c.verify
			f.c.verify = func(ctx context.Context, n int, head string) (policyDecision, error) {
				f.api.mu.Lock()
				defer f.api.mu.Unlock()
				f.api.responses[f.c.endpoint("git/ref/heads/dev")] = map[string]any{"ref": "refs/heads/dev", "object": map[string]string{"type": "commit", "sha": strings.Repeat("f", 40)}}
				return verify(ctx, n, head)
			}
		},
		"final verifier refused": func(f *queueFixture) {
			calls := 0
			f.c.verify = func(context.Context, int, string) (policyDecision, error) {
				calls++
				if calls == 2 {
					return policyDecision{Code: 1}, nil
				}
				return policyDecision{}, nil
			}
		},
		"policy moved": func(f *queueFixture) {
			f.api.readHook = func(path string, count int) {
				if path == f.c.endpoint("rulesets/31") && count == 2 {
					f.owner["updated_at"] = "2026-11-01T00:01:00Z"
				}
			}
		},
	}
	for name, move := range moves {
		t.Run(name, func(t *testing.T) {
			f := newDevFixture(t)
			f.c.repo = f.pr.Base.Repo
			f.c.repo.DefaultBranch = "main"
			move(f)
			budget := admissionBudget{Started: time.Now(), Deadline: time.Now().Add(time.Minute)}
			err := f.q.withAdmissionBudget(context.Background(), budget, func(work context.Context) error {
				return f.q.executeDev(work, "workflow_dispatch", event{Repository: f.c.repo}, f.pr.Number)
			})
			latest := newestQueueCheck(f.api.checks)
			if name == "none" {
				if err != nil || latest == nil || latest.Conclusion != "success" {
					t.Fatalf("unmoved dev: err=%v, latest=%+v", err, latest)
				}
				return
			}
			if err == nil || onlyStaleHeadWork(err) || !strings.Contains(err.Error(), "admission state moved") {
				t.Fatalf("a movement must fail the job so the retry runs: %v", err)
			}
			f.assertNoSuccess(t)
			if latest == nil || latest.Status == "completed" {
				t.Fatalf("movement left no pending barrier: %+v", latest)
			}
		})
	}
}

func TestMainBaseMovementStaysPendingWithoutFailing(t *testing.T) {
	f := newQueueFixture(t)
	path := f.c.endpoint("git/ref/heads/" + f.c.admissionBranch())
	f.api.readHook = func(got string, count int) {
		if got == path && count == 2 {
			f.api.responses[path] = map[string]any{"ref": "refs/heads/" + f.c.admissionBranch(), "object": map[string]string{"type": "commit", "sha": strings.Repeat("f", 40)}}
		}
	}
	if err := f.q.admit(context.Background(), f.pr.Number); err != nil {
		t.Fatalf("a main push reassesses every PR, so this must not fail: %v", err)
	}
	f.assertNoSuccess(t)
	if latest := f.api.writes[len(f.api.writes)-1]; latest["status"] != "queued" {
		t.Fatalf("moved main left no pending barrier: %+v", latest)
	}
}

func TestDevForkVouchRemainsOwnedByMain(t *testing.T) {
	f := newDevFixture(t)
	f.pr.Head.Repo.ID, f.pr.Head.Repo.FullName = 11, "contributor/Hikyo"
	f.api.responses[f.c.endpoint("pulls/7")] = f.pr
	f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{f.pr}
	f.feedbackRun.HeadRepository = f.pr.Head.Repo
	f.feedbackRun.Pulls = []pull{f.pr}
	path := f.c.endpoint(fmt.Sprintf("actions/workflows/ci-feedback.yml/runs?event=pull_request&head_sha=%s&per_page=100&page=1", f.pr.Head.SHA))
	f.api.responses[path] = map[string]any{"total_count": 1, "workflow_runs": []run{f.feedbackRun}}
	if err := f.q.admit(context.Background(), f.pr.Number); err != nil {
		t.Fatal(err)
	}
	if f.api.reads[f.c.endpoint("contents/.github/VOUCHED.td?ref=main")] != 2 || f.c.repo.DefaultBranch != "main" || f.api.checks[len(f.api.checks)-1].Conclusion != "success" {
		t.Fatal("dev admission failed to repeat trusted main vouch authority")
	}
}

func TestDevPublisherRepairRequiresExactMainBackmerge(t *testing.T) {
	for _, route := range []string{"main", "feature", "fork"} {
		t.Run(route, func(t *testing.T) {
			f := newDevFixture(t)
			main := strings.Repeat("e", 40)
			f.api.responses[f.c.endpoint("contents/"+devGateWorkflowPath+"?ref="+f.pr.Base.SHA)] = map[string]string{"type": "file", "path": devGateWorkflowPath, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("older main publisher"))}
			if route != "feature" {
				f.pr.Head.SHA = main
			}
			if route == "fork" {
				f.pr.Head.Repo.ID, f.pr.Head.Repo.FullName = 11, "contributor/Hikyo"
			}
			err := f.q.devPublisherContract(context.Background(), f.pr)
			if (err == nil) != (route == "main") {
				t.Fatalf("publisher repair %s: %v", route, err)
			}
		})
	}
}

func TestDevPolicyPathsLandThroughMainFirst(t *testing.T) {
	const path = ".github/workflows/ci.yml"
	content := func(data string) map[string]string {
		return map[string]string{"type": "file", "path": path, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(data))}
	}
	main := strings.Repeat("e", 40)
	for name, tc := range map[string]struct {
		files  []map[string]string
		head   string
		refuse bool
	}{
		"unrelated file":       {[]map[string]string{{"filename": "internal/app/app.go"}}, "", false},
		"changed definition":   {[]map[string]string{{"filename": path}}, "feature bytes", true},
		"backmerged from main": {[]map[string]string{{"filename": path}}, "main bytes", false},
		"renamed definition":   {[]map[string]string{{"filename": "other.yml", "previous_filename": path}}, "", true},
		"removed definition":   {[]map[string]string{{"filename": path, "status": "removed"}}, "", true},
		"empty file list":      {[]map[string]string{}, "", false},
	} {
		t.Run(name, func(t *testing.T) {
			f := newDevFixture(t)
			f.api.responses[f.c.endpoint(devFilesPage(f, 1))] = tc.files
			f.api.responses[f.c.endpoint("contents/"+path+"?ref="+main)] = content("main bytes")
			if tc.head != "" {
				f.api.responses[f.c.endpoint("contents/"+path+"?ref="+f.pr.Head.SHA)] = content(tc.head)
			}
			refusal, err := f.q.devPolicyRefusal(context.Background(), f.pr)
			if err != nil || (refusal != "") != tc.refuse {
				t.Fatalf("refusal %q/%v, want refuse=%t", refusal, err, tc.refuse)
			}
		})
	}
	// A wide backmerge is read to its last page. GitHub's own listing cap refuses.
	full := make([]map[string]string, 100)
	for i := range full {
		full[i] = map[string]string{"filename": fmt.Sprintf("file-%d", i)}
	}
	wide := newDevFixture(t)
	for page := 1; page <= pullFilePages; page++ {
		wide.api.responses[wide.c.endpoint(devFilesPage(wide, page))] = full
	}
	if refusal, err := wide.q.devPolicyRefusal(context.Background(), wide.pr); err != nil || refusal == "" {
		t.Fatalf("unlistable PR was not refused: %q/%v", refusal, err)
	}
	wide.api.responses[wide.c.endpoint(devFilesPage(wide, 7))] = []map[string]string{{"filename": path}}
	wide.api.responses[wide.c.endpoint("contents/"+path+"?ref="+main)] = content("main bytes")
	wide.api.responses[wide.c.endpoint("contents/"+path+"?ref="+wide.pr.Head.SHA)] = content("main bytes")
	if refusal, err := wide.q.devPolicyRefusal(context.Background(), wide.pr); err != nil || refusal != "" {
		t.Fatalf("wide backmerge was refused: %q/%v", refusal, err)
	}
	// Admission must publish this refusal instead of only failing the job.
	f := newDevFixture(t)
	f.q.dco = func(context.Context, pull) (int, error) { return 0, nil }
	f.api.responses[f.c.endpoint(devFilesPage(f, 1))] = []map[string]string{{"filename": path}}
	f.api.responses[f.c.endpoint("contents/"+path+"?ref="+main)] = content("main bytes")
	f.api.responses[f.c.endpoint("contents/"+path+"?ref="+f.pr.Head.SHA)] = content("feature bytes")
	if err := f.q.admit(context.Background(), f.pr.Number); err != nil {
		t.Fatal(err)
	}
	if len(f.api.checks) != 1 || f.api.checks[0].Conclusion != "failure" {
		t.Fatalf("policy-path refusal was not published: %+v", f.api.checks)
	}
}

func TestMainRouteSendsFeaturePRsToDev(t *testing.T) {
	const policy = ".github/workflows/ci.yml"
	for name, tc := range map[string]struct {
		head, login string
		fork, noDev bool
		files       []map[string]string
		refuse      bool
	}{
		"feature":                 {head: "feat/x", refuse: true},
		"feature before dev":      {head: "feat/x", noDev: true},
		"release":                 {head: "release/1.1.0"},
		"nightly promotion":       {head: "release/nightly-20261103-abcdef0"},
		"hotfix":                  {head: "hotfix/x"},
		"fork named like release": {head: "release/1.1.0", fork: true, refuse: true},
		"fork named like hotfix":  {head: "hotfix/x", fork: true, refuse: true},
		"dev itself":              {head: "dev", refuse: true},
		"dependabot":              {head: "dependabot/go_modules/x", login: "dependabot[bot]"},
		"impostor dependabot":     {head: "dependabot/go_modules/x", login: "dependabot", refuse: true},
		"CI definition":           {head: "feat/ci", files: []map[string]string{{"filename": policy}}},
		"renamed CI definition":   {head: "feat/ci", files: []map[string]string{{"filename": "other.yml", "previous_filename": policy}}},
		"dev gate workflow":       {head: "feat/ci", files: []map[string]string{{"filename": devGateWorkflowPath}}},
		"other workflow":          {head: "feat/ci", files: []map[string]string{{"filename": ".github/workflows/fuzz-report.yml"}}, refuse: true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newQueueFixture(t)
			f.pr.Head.Ref, f.pr.User.Login = tc.head, tc.login
			if tc.fork {
				f.pr.Head.Repo.ID = f.c.repo.ID + 1
			}
			if !tc.noDev {
				f.api.responses[f.c.endpoint("git/ref/heads/dev")] = map[string]any{"ref": "refs/heads/dev"}
				files := tc.files
				if files == nil {
					files = []map[string]string{{"filename": "internal/app/app.go"}}
				}
				f.api.responses[f.c.endpoint(devFilesPage(f, 1))] = files
			}
			refusal, err := f.q.mainRouteRefusal(context.Background(), f.pr)
			if err != nil || (refusal != "") != tc.refuse {
				t.Fatalf("refusal %q/%v, want refuse=%t", refusal, err, tc.refuse)
			}
		})
	}
	// Dev refuses a PR it cannot list, so routing leaves it to main's own checks.
	full := make([]map[string]string, 100)
	for i := range full {
		full[i] = map[string]string{"filename": fmt.Sprintf("file-%d", i)}
	}
	wide := newQueueFixture(t)
	wide.pr.Head.Ref = "feat/wide"
	wide.api.responses[wide.c.endpoint("git/ref/heads/dev")] = map[string]any{"ref": "refs/heads/dev"}
	for page := 1; page <= pullFilePages; page++ {
		wide.api.responses[wide.c.endpoint(devFilesPage(wide, page))] = full
	}
	if refusal, err := wide.q.mainRouteRefusal(context.Background(), wide.pr); err != nil || refusal != "" {
		t.Fatalf("routing refused an unlistable PR: %q/%v", refusal, err)
	}
	// Only a missing dev turns routing off; other read failures must not admit.
	f := newQueueFixture(t)
	f.pr.Head.Ref = "feat/x"
	f.q.api = failedQueueRead{api: f.q.api, contains: "git/ref/heads/dev"}
	if _, err := f.q.mainRouteRefusal(context.Background(), f.pr); err == nil {
		t.Fatal("dev read failure was treated as dev being absent")
	}
	// Admission must publish the refusal instead of only failing the job.
	f = newQueueFixture(t)
	f.pr.Head.Ref = "feat/x"
	f.refresh()
	f.api.responses[f.c.endpoint("git/ref/heads/dev")] = map[string]any{"ref": "refs/heads/dev"}
	f.api.responses[f.c.endpoint(devFilesPage(f, 1))] = []map[string]string{{"filename": "internal/app/app.go"}}
	if err := f.admit(); err != nil {
		t.Fatal(err)
	}
	latest := newestQueueCheck(f.api.checks)
	if latest == nil || latest.Conclusion != "failure" {
		t.Fatalf("route refusal was not published: %+v", latest)
	}
}

// Source proof is package main too, so compare the two lists through its source.
func TestDevPolicyPathsCoverSourceProof(t *testing.T) {
	raw, err := os.ReadFile("../source-proof/resolve.go")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)var policyPaths = \[\]string\{(.*?)\n\}`).FindSubmatch(raw)
	if block == nil {
		t.Fatal("source-proof policy paths moved")
	}
	want := map[string]bool{".github/workflows/ci-dev.yml": true}
	for _, match := range regexp.MustCompile(`"([^"]+)"`).FindAllSubmatch(block[1], -1) {
		want[string(match[1])] = true
	}
	if !reflect.DeepEqual(want, devPolicyPaths) {
		t.Fatalf("dev policy paths differ from source proof:\n got %v\nwant %v", devPolicyPaths, want)
	}
}

func TestQueueGateLeavesDevPullRequestsAlone(t *testing.T) {
	f := newQueueFixture(t)
	f.pr.Base.Ref = "dev"
	f.refresh()
	if err := f.q.admit(context.Background(), f.pr.Number); err != nil {
		t.Fatal(err)
	}
	if len(f.api.checks) != 0 {
		t.Fatalf("main queue gate published on a dev PR: %+v", f.api.checks)
	}
}
