package benchmark

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPerformancePaths(t *testing.T) {
	for _, test := range []struct {
		path     string
		relevant bool
	}{
		{"internal/service/values.go", true}, {"internal/crypto/crypto_test.go", true},
		{"internal/buildcompat/development.json", true}, {"internal/importer/testdata/k8s-single.yaml", true},
		{"internal/store/queries/items.sql", true}, {"internal/store/migrations/001.sql", true},
		{"go.mod", true}, {"go.sum", true}, {"api/openapi.yaml", true},
		{"web/src/routes/Matrix.tsx", true}, {"web/src/main.css", true},
		{"web/pnpm-lock.yaml", true}, {"web/vite.config.ts", true},
		{"clients/ts/src/generated/client.gen.ts", true}, {"clients/ts/pnpm-lock.yaml", true}, {".nvmrc", true},
		{"web/src/routes/Matrix.stories.tsx", false}, {"web/src/routes/Matrix.test.tsx", false},
		{"web/README.md", false}, {"docs/site/src/index.astro", false},
		{".github/workflows/pr-benchmark.yml", true}, {".github/workflows/ci.yml", false},
		{"scripts/ci/benchmark/budget.go", true}, {"README.md", false},
		{"internal/service/README.md", false}, {"LICENSE", false},
	} {
		t.Run(test.path, func(t *testing.T) {
			if got := performancePath(test.path); got != test.relevant {
				t.Fatalf("relevant=%t, want %t", got, test.relevant)
			}
		})
	}
}

func TestAutomaticRequestRejectsMismatchedProviderIdentity(t *testing.T) {
	r := benchmarkRun{ID: 10, Head: strings.Repeat("a", 40), Title: "pr-benchmark #7", Event: "pull_request", Status: "completed", Conclusion: "success", Attempt: 1}
	live := r
	live.Head = strings.Repeat("b", 40)
	log := fakeGH(t, map[string]string{"repos/o/r/actions/runs/10": encode(t, live)})
	if _, _, requested, err := automaticRequest("o/r", controlEvent{WorkflowRun: r}); err == nil || requested {
		t.Fatalf("mismatched provider identity: requested=%t error=%v", requested, err)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(calls), "--method POST") {
		t.Fatal("mismatched run must not trigger measurement")
	}
}

func TestAutomaticSkipPreservesNewHeadsAndMaintainerEdits(t *testing.T) {
	for _, changedHead := range []bool{false, true} {
		t.Run(fmt.Sprintf("changedHead=%t", changedHead), func(t *testing.T) {
			head := strings.Repeat("a", 40)
			pr := pull{Number: 7, State: "open"}
			pr.Head.SHA = head
			latest := pr
			if changedHead {
				latest.Head.SHA = strings.Repeat("b", 40)
			}
			c := testComment(t, head, request{head: head})
			edited := c
			edited.Body = strings.Replace(c.Body, "- [ ] Run benchmark", checkbox, 1)
			log := fakeGH(t, map[string]string{
				"repos/o/r/pulls/7":                        encode(t, latest),
				"repos/o/r/issues/7/comments?per_page=100": encode(t, [][]comment{{edited}}),
			})
			if err := skipAutomatic("o/r", pr, c, "budget exhausted"); err != nil {
				t.Fatal(err)
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(calls), "--method PATCH") {
				t.Fatal("skip must preserve a newer head or maintainer request")
			}
		})
	}
}

func TestAutomaticSkipPreservesAlreadyPendingManualClaim(t *testing.T) {
	head := strings.Repeat("a", 40)
	pr := pull{Number: 7, State: "open"}
	pr.Head.SHA = head
	c := testComment(t, head, request{head: head})
	c.Body = strings.Replace(c.Body, "- [ ] Run benchmark", checkbox, 1)
	log := fakeGH(t, map[string]string{
		"repos/o/r/pulls/7":                        encode(t, pr),
		"repos/o/r/issues/7/comments?per_page=100": encode(t, [][]comment{{c}}),
	})
	if err := skipAutomatic("o/r", pr, c, "budget exhausted"); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(calls), "--method PATCH") {
		t.Fatal("a matching pending manual claim must survive automatic skip")
	}
}

func TestPerformanceChangesChecksAllPagesAndBothRenamePaths(t *testing.T) {
	fakeGH(t, map[string]string{
		"repos/o/r/pulls/7/files?per_page=100": `[[{"filename":"README.md"}],[{"filename":"docs/removed.txt","previous_filename":"internal/service/values.go"}]]`,
	})
	if got, err := performanceChanges("o/r", pull{Number: 7, ChangedFiles: 2}); err != nil || !got {
		t.Fatalf("relevant=%t error=%v", got, err)
	}
	if _, err := performanceChanges("o/r", pull{Number: 7, ChangedFiles: 3}); err == nil {
		t.Fatal("truncated files must fail closed")
	}
	if _, err := performanceChanges("o/r", pull{Number: 7, ChangedFiles: 3001}); err == nil {
		t.Fatal("provider file cap must fail closed")
	}
}

func TestAutomaticBudgetProjection(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		u       usage
		f       forecast
		allowed bool
	}{
		{"cold start reserves full daily jobs", usage{}, forecast{now: now, observedSince: now}, true},
		{"measured daily and recent PR pace fits", usage{automatic: 100, manual: 57}, forecast{now: now, observedSince: now.Add(-7 * 24 * time.Hour), dailyMinutes: 100, dailyJobs: 10, recentRequestedMinutes: 57}, true},
		{"projected pace exceeds allowance", usage{automatic: 100, manual: 58}, forecast{now: now, observedSince: now.Add(-7 * 24 * time.Hour), dailyMinutes: 100, dailyJobs: 10, recentRequestedMinutes: 58}, false},
		{"low projection cannot hide exhausted actual budget", usage{manual: 585}, forecast{now: now, observedSince: now}, false},
		{"complete reservation fits exact cap", usage{manual: 584}, forecast{now: now, observedSince: now}, true},
		{"early burst uses short observation period", usage{manual: 20}, forecast{now: now, observedSince: now.Add(-time.Hour), dailyMinutes: 5, dailyJobs: 1, recentRequestedMinutes: 20}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, reason := admitAutomatic(test.u, test.f)
			if got != test.allowed {
				t.Fatalf("allowed=%t want=%t: %s", got, test.allowed, reason)
			}
		})
	}
}

func TestForecastCountsFailedAttemptsAndExcludesOldRequestedPace(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	start := now.Add(-3 * time.Minute)
	f := forecast{now: now, observedSince: now}
	f.add(job{StartedAt: &start, CompletedAt: &now, Conclusion: "failure"}, 4, true)
	f.add(job{StartedAt: &start, CompletedAt: &now, Conclusion: "cancelled"}, 4, false)
	oldStart, oldEnd := now.Add(-8*24*time.Hour-time.Minute), now.Add(-8*24*time.Hour)
	f.add(job{StartedAt: &oldStart, CompletedAt: &oldEnd}, 2, false)
	// Four minutes daily for 32 days, recent requests of four minutes per
	// seven days, plus the next full 16-minute reservation: ceil(146.29)+16.
	if got := f.projectedMinutes(); got != 163 {
		t.Fatalf("projected=%d, want 163", got)
	}
}

func TestAutomaticRequestRunsOnlyRelevantCurrentHeads(t *testing.T) {
	for _, test := range []struct {
		name, file                                         string
		draft, stale, claimed, budgetExceeded              bool
		pendingManual, lateManual, fork, missingRepository bool
		absentComment, staleComment                        bool
		superseded                                         string
		wantRun                                            bool
	}{
		{name: "runtime change", file: "internal/service/values.go", wantRun: true},
		{name: "web change", file: "web/src/routes/Matrix.tsx", wantRun: true},
		{name: "docs only", file: "README.md"},
		{name: "draft", file: "go.mod", draft: true},
		{name: "superseded head", file: "go.mod", stale: true},
		{name: "already requested", file: "go.mod", claimed: true},
		{name: "projected usage exceeded", file: "go.mod", budgetExceeded: true},
		{name: "pending manual claim", file: "go.mod", pendingManual: true},
		{name: "manual claim during budget collection", file: "go.mod", lateManual: true},
		{name: "fork requires manual maintainer admission", file: "go.mod", fork: true},
		{name: "missing head repository", file: "go.mod", missingRepository: true},
		{name: "newer completed discovery", file: "go.mod", superseded: "completed"},
		{name: "newer running discovery", file: "go.mod", superseded: "in_progress"},
		{name: "automatic creates absent comment", file: "go.mod", absentComment: true, wantRun: true},
		{name: "automatic refreshes stale comment", file: "go.mod", staleComment: true, wantRun: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			head := strings.Repeat("a", 40)
			pr := pull{Number: 7, State: "open", Draft: test.draft, ChangedFiles: 1}
			pr.Head.SHA = head
			pr.Head.Repo.FullName = "o/r"
			if test.fork {
				pr.Head.Repo.FullName = "outside/r"
			}
			if test.missingRepository {
				pr.Head.Repo.FullName = ""
			}
			if test.stale {
				pr.Head.SHA = strings.Repeat("b", 40)
			}
			identity := request{head: head}
			if test.claimed {
				identity.run, identity.attempt = 10, 2
			}
			c := testComment(t, head, identity)
			if test.pendingManual {
				c.Body = strings.Replace(c.Body, "- [ ] Run benchmark", checkbox, 1)
			}
			if test.staleComment {
				oldHead := strings.Repeat("b", 40)
				c = testComment(t, oldHead, request{head: oldHead, run: 8, attempt: 2})
			}
			r := benchmarkRun{ID: 10, Head: head, Title: "pr-benchmark #7", Event: "pull_request", Status: "completed", Conclusion: "success", Attempt: 1}
			now := time.Now().UTC()
			start := now.Add(-time.Minute)
			measured := job{ID: 12, Labels: []string{macroLabel}, Status: "completed", Conclusion: "success", RunnerName: "macro", StartedAt: &start, CompletedAt: &now}
			responses := map[string]string{
				"repos/o/r/actions/runs/10":                                                                               encode(t, r),
				"repos/o/r/pulls/7":                                                                                       encode(t, pr),
				"repos/o/r/pulls/7/files?per_page=100":                                                                    encode(t, [][]changedFile{{{Filename: test.file}}}),
				"repos/o/r/issues/7/comments?per_page=100":                                                                encode(t, [][]comment{{c}}),
				"repos/o/r/issues/comments/20":                                                                            `{}`,
				"repos/o/r/contents/.github/workflows/pr-benchmark.yml?ref=main":                                          `{"sha":"trusted"}`,
				"repos/o/r/contents/.github/workflows/pr-benchmark.yml?ref=" + head:                                       `{"sha":"trusted"}`,
				"repos/o/r/contents/.github/workflows/matrix-performance.yml?ref=main":                                    `{"sha":"trusted"}`,
				"repos/o/r/contents/.github/workflows/matrix-performance.yml?ref=" + head:                                 `{"sha":"trusted"}`,
				"repos/o/r/actions/workflows/pr-benchmark.yml/runs?event=pull_request&head_sha=" + head + "&per_page=100": encode(t, []map[string][]benchmarkRun{{"workflow_runs": {r}}}),
				"repos/o/r/actions/workflows/codspeed.yml/runs?per_page=100":                                              `[{"workflow_runs":[]}]`,
				"repos/o/r/actions/workflows/pr-benchmark.yml/runs?per_page=100":                                          fmt.Sprintf(`[{"workflow_runs":[{"id":10,"run_attempt":1,"event":"pull_request","status":"completed","updated_at":%q}]}]`, now.Format(time.RFC3339)),
				"repos/o/r/actions/runs/10/rerun":                                                                         `{}`,
				"repos/o/r/actions/runs/10/attempts/2/jobs?per_page=100":                                                  encode(t, []map[string][]job{{"jobs": {measured}}}),
			}
			if test.budgetExceeded {
				responses["repos/o/r/actions/workflows/pr-benchmark.yml/runs?per_page=100"] = fmt.Sprintf(`[{"workflow_runs":[{"id":2,"run_attempt":2,"event":"pull_request","status":"completed","updated_at":%q}]}]`, now.Format(time.RFC3339))
				// With no daily calibration, reserve 512 daily minutes. A
				// three-minute request extrapolated over one day adds 96.
				measuredStart := now.Add(-2 * time.Minute)
				paid := measured
				paid.StartedAt = &measuredStart
				responses["repos/o/r/actions/runs/2/jobs?filter=all&per_page=100"] = encode(t, []map[string][]job{{"jobs": {paid}}})
			}
			if test.superseded != "" {
				newer := r
				newer.ID, newer.Status = 11, test.superseded
				responses["repos/o/r/actions/workflows/pr-benchmark.yml/runs?event=pull_request&head_sha="+head+"&per_page=100"] = encode(t, []map[string][]benchmarkRun{{"workflow_runs": {r, newer}}})
			}
			if test.absentComment {
				responses["repos/o/r/issues/7/comments?per_page=100"] = `[[]]`
				responses["repos/o/r/issues/7/comments"] = `{}`
			}
			log := fakeGH(t, responses)
			// The fake provider advances the run only after the controller writes
			// its authorized attempt. This exercises discovery -> paid rerun -> report.
			gh := filepath.Join(filepath.Dir(log), "gh")
			script, err := os.ReadFile(gh)
			if err != nil {
				t.Fatal(err)
			}
			transition := fmt.Sprintf("respond() {\n if [ \"$method\" = GET ] && [ \"$endpoint\" = 'repos/o/r/actions/runs/10' ] && [ -f '%s' ] && grep -q 'run=10 attempt=2' '%s'; then jq '.run_attempt=2' \"$1\"; return; fi\n", filepath.Join(filepath.Dir(log), "latest-body"), filepath.Join(filepath.Dir(log), "latest-body"))
			if test.lateManual {
				transition += fmt.Sprintf(" if [ \"$endpoint\" = 'repos/o/r/issues/7/comments?per_page=100' ] && grep -q 'workflows/codspeed.yml/runs' '%s'; then jq '.[0][0].body |= sub(\"- \\\\[ \\\\] Run benchmark\"; \"- [x] Run benchmark\")' \"$1\"; return; fi\n", log)
			}
			if test.absentComment {
				bodyFile := filepath.Join(filepath.Dir(log), "latest-body")
				transition += fmt.Sprintf(" if [ \"$method\" = POST ] && [ \"$endpoint\" = 'repos/o/r/issues/7/comments' ]; then printf '%%s' \"$body\" >'%s'; fi\n if [ \"$method\" = GET ] && [ \"$endpoint\" = 'repos/o/r/issues/7/comments?per_page=100' ] && [ -f '%s' ]; then jq -n --rawfile body '%s' '[[{id:20,user:{id:41898282},body:$body}]]'; return; fi\n", bodyFile, bodyFile, bodyFile)
			}
			if err := os.WriteFile(gh, []byte(strings.Replace(string(script), "respond() {\n", transition, 1)), 0700); err != nil {
				t.Fatal(err)
			}
			if err := runBenchmark("o/r", controlEvent{WorkflowRun: r}, true); err != nil {
				t.Fatal(err)
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			gotRun := strings.Contains(string(calls), "--method POST repos/o/r/actions/runs/10/rerun")
			if gotRun != test.wantRun {
				t.Fatalf("ran=%t want=%t\n%s", gotRun, test.wantRun, calls)
			}
			if gotRun && !strings.Contains(string(calls), "Benchmark **success**") {
				t.Fatal("missing completed measurement report")
			}
			if !test.wantRun && strings.Contains(string(calls), "--method POST") {
				t.Fatal("ineligible PR must not trigger a run")
			}
			if (test.pendingManual || test.lateManual || test.superseded != "") && strings.Contains(string(calls), "--method PATCH") {
				t.Fatal("pending manual claims and superseded discovery must not change status")
			}
			if test.absentComment && strings.Count(string(calls), "--method POST repos/o/r/issues/7/comments ") != 1 {
				t.Fatal("automatic preparation and its recheck must create exactly one comment")
			}
		})
	}
}
