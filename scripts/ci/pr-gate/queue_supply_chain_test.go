package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

// supplyChainJobs returns the selection and fixtures jobs of run r. An empty
// conclusion leaves the job in progress.
func supplyChainJobs(r run, selection, fixtures string) []job {
	state := func(conclusion string) string {
		if conclusion == "" {
			return "in_progress"
		}
		return "completed"
	}
	return []job{
		{ID: 204, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: supplyChainSelectName, Status: state(selection), Conclusion: selection},
		{ID: 205, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: supplyChainFixturesName, Status: state(fixtures), Conclusion: fixtures},
	}
}

func setDevJobs(f *queueFixture, jobs []job) {
	r := f.feedbackRun
	lanes := []job{
		{ID: 201, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: "feedback (go)", Status: "completed", Conclusion: "success"},
		{ID: 202, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: "feedback (frontend)", Status: "completed", Conclusion: "success"},
		{ID: 203, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: "feedback (tooling)", Status: "completed", Conclusion: "success"},
	}
	all := append(lanes, jobs...)
	f.api.responses[f.c.endpoint(fmt.Sprintf("actions/runs/%d/attempts/%d/jobs?per_page=100&page=1", r.ID, r.Attempt))] = map[string]any{"total_count": len(all), "jobs": all}
}

const testSupplyChainList = "# list\nhooks/pre-push\nfixtures/\n"

func encodedFile(path, data string) map[string]string {
	return map[string]string{"type": "file", "path": path, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(data))}
}

// setSupplyChainBase publishes the trusted input list at the base commit.
func setSupplyChainBase(f *queueFixture, list string) {
	f.api.responses[f.c.endpoint("contents/"+supplyChainListPath+"?ref="+f.pr.Base.SHA)] = encodedFile(supplyChainListPath, list)
}

func totalReads(f *queueFixture) (n int) {
	for _, c := range f.api.reads {
		n += c
	}
	return n
}

func admitted(f *queueFixture) bool {
	for _, w := range f.api.writes {
		if w["conclusion"] == "success" {
			return true
		}
	}
	return false
}

func TestDevAdmissionAllowsOnlyTheTwoSupplyChainOutcomes(t *testing.T) {
	for name, jobs := range map[string][]job{
		"fixtures ran and passed":                      supplyChainJobs(run{ID: 101, Attempt: 1}, "success", "success"),
		"selection chose nothing and fixtures skipped": supplyChainJobs(run{ID: 101, Attempt: 1}, "success", "skipped"),
	} {
		t.Run(name, func(t *testing.T) {
			f := newDevFixture(t)
			setSupplyChainBase(f, testSupplyChainList)
			for i := range jobs {
				jobs[i].HeadSHA = f.feedbackRun.HeadSHA
			}
			setDevJobs(f, jobs)
			if err := f.q.admit(context.Background(), f.pr.Number); err != nil {
				t.Fatal(err)
			}
			if !admitted(f) || f.api.checks[0].Name != devCheckName {
				t.Fatalf("an allowed supply-chain outcome was not admitted: %+v", f.api.writes)
			}
		})
	}
}

func TestDevAdmissionBlocksEveryOtherSupplyChainOutcome(t *testing.T) {
	two := func(selection, fixtures string) func(*queueFixture) []job {
		return func(f *queueFixture) []job { return supplyChainJobs(f.feedbackRun, selection, fixtures) }
	}
	for name, build := range map[string]func(*queueFixture) []job{
		"fixtures failed":                        two("success", "failure"),
		"fixtures cancelled":                     two("success", "cancelled"),
		"fixtures timed out":                     two("success", "timed_out"),
		"skipped after selection failed":         two("failure", "skipped"),
		"skipped after selection cancelled":      two("cancelled", "skipped"),
		"skipped after selection skipped":        two("skipped", "skipped"),
		"fixtures passed after selection failed": two("failure", "success"),
		"selection failed, fixtures failed":      two("failure", "failure"),
		"missing selection": func(f *queueFixture) []job {
			return supplyChainJobs(f.feedbackRun, "success", "skipped")[1:]
		},
		"missing selection with fixtures passed": func(f *queueFixture) []job {
			return supplyChainJobs(f.feedbackRun, "success", "success")[1:]
		},
		"missing fixtures": func(f *queueFixture) []job {
			return supplyChainJobs(f.feedbackRun, "success", "skipped")[:1]
		},
		"both missing": func(*queueFixture) []job { return nil },
		"duplicate selection": func(f *queueFixture) []job {
			jobs := supplyChainJobs(f.feedbackRun, "success", "success")
			duplicate := jobs[0]
			duplicate.ID = 206
			return append(jobs, duplicate)
		},
		"duplicate fixtures": func(f *queueFixture) []job {
			jobs := supplyChainJobs(f.feedbackRun, "success", "success")
			duplicate := jobs[1]
			duplicate.ID = 206
			return append(jobs, duplicate)
		},
		"duplicate fixtures, one failed": func(f *queueFixture) []job {
			jobs := supplyChainJobs(f.feedbackRun, "success", "success")
			duplicate := jobs[1]
			duplicate.ID, duplicate.Conclusion = 206, "failure"
			return append(jobs, duplicate)
		},
		"fixtures from another head": func(f *queueFixture) []job {
			jobs := supplyChainJobs(f.feedbackRun, "success", "success")
			jobs[1].HeadSHA = strings.Repeat("f", 40)
			return jobs
		},
		"fixtures still running":  two("success", ""),
		"selection still running": two("", "skipped"),
	} {
		t.Run(name, func(t *testing.T) {
			f := newDevFixture(t)
			setDevJobs(f, build(f))
			_ = f.q.admit(context.Background(), f.pr.Number)
			f.assertNoSuccess(t)
			if admitted(f) {
				t.Fatal("dev admitted an unsafe supply-chain outcome")
			}
		})
	}
}

// A failed fixtures job fails the whole run in GitHub, so the conclusion check
// already blocks it. The job-level check must hold when the run still reports
// success, which is what a dropped or renamed job produces.
func TestDevAdmissionJudgesSupplyChainJobsWhenTheRunSucceeded(t *testing.T) {
	f := newDevFixture(t)
	f.feedbackRun.Conclusion = "success"
	f.feedbackRefresh()
	setDevJobs(f, supplyChainJobs(f.feedbackRun, "success", "failure"))
	_ = f.q.admit(context.Background(), f.pr.Number)
	if admitted(f) {
		t.Fatal("a failed fixtures job was admitted under a successful run conclusion")
	}
}

// Metadata-only edits skip every job. The two supply-chain jobs must neither
// break the ignored-run logic nor let a non-skipped job hide inside it.
func TestDevIgnoresMetadataRunWithSkippedSupplyChainJobs(t *testing.T) {
	const recordedName = "${{ (((github.event.action == 'edited') && (github.event.changes.base.ref.from == '') && 'metadata (ignored)') || 'feedback') }} (${{ matrix.lane }})"
	for name, tc := range map[string]struct {
		change func(*[]job)
		pass   bool
	}{
		"placeholder and both supply jobs skipped": {func(*[]job) {}, true},
		"supply jobs not yet materialized":         {func(j *[]job) { *j = (*j)[:1] }, true},
		"selection ran":                            {func(j *[]job) { (*j)[1].Conclusion = "success" }, false},
		"fixtures ran":                             {func(j *[]job) { (*j)[2].Conclusion = "failure" }, false},
		"duplicate selection": {func(j *[]job) {
			duplicate := (*j)[1]
			duplicate.ID = 399
			*j = append(*j, duplicate)
		}, false},
	} {
		t.Run(name, func(t *testing.T) {
			f := newDevFixture(t)
			metadata := f.feedbackRun
			metadata.ID, metadata.Conclusion = 103, "skipped"
			f.api.responses[f.c.endpoint(fmt.Sprintf("actions/workflows/ci-feedback.yml/runs?event=pull_request&head_sha=%s&per_page=100&page=1", f.pr.Head.SHA))] = map[string]any{"total_count": 2, "workflow_runs": []run{metadata, f.feedbackRun}}
			jobs := []job{
				{ID: 301, RunID: 103, Attempt: 1, HeadSHA: metadata.HeadSHA, Name: recordedName, Status: "completed", Conclusion: "skipped"},
				{ID: 302, RunID: 103, Attempt: 1, HeadSHA: metadata.HeadSHA, Name: supplyChainSelectName, Status: "completed", Conclusion: "skipped"},
				{ID: 303, RunID: 103, Attempt: 1, HeadSHA: metadata.HeadSHA, Name: supplyChainFixturesName, Status: "completed", Conclusion: "skipped"},
			}
			tc.change(&jobs)
			f.api.responses[f.c.endpoint("actions/runs/103/attempts/1/jobs?per_page=100&page=1")] = map[string]any{"total_count": len(jobs), "jobs": jobs}
			setSupplyChainBase(f, testSupplyChainList)
			setDevJobs(f, supplyChainJobs(f.feedbackRun, "success", "skipped"))
			err := f.q.admit(context.Background(), f.pr.Number)
			if tc.pass {
				if err != nil || !admitted(f) {
					t.Fatalf("metadata-only run broke admission of the useful run: %v", err)
				}
			} else if admitted(f) {
				t.Fatal("a malformed metadata run was ignored and an older success reused")
			}
		})
	}
}

func TestSplitSupplyChainJobsLeavesLanesAlone(t *testing.T) {
	all := []job{{Name: "feedback (go)"}, {Name: supplyChainSelectName}, {Name: "feedback (tooling)"}, {Name: supplyChainFixturesName}, {Name: "other"}}
	lanes, supply := splitSupplyChainJobs(all)
	if len(lanes) != 3 || len(supply) != 2 || supply[0].Name != supplyChainSelectName || supply[1].Name != supplyChainFixturesName {
		t.Fatalf("lanes=%v supply=%v", lanes, supply)
	}
}

// The selection job runs the PR's own selector, so it can say "nothing" for a PR
// that changes a fixture input. The gate recomputes the selection from the base
// list and the PR's changed files, and refuses a skipped fixtures job then.
func TestDevAdmissionRecomputesSelectionFromTheBaseList(t *testing.T) {
	const ci = ".github/workflows/ci.yml"
	job := func(step string) string {
		return "jobs:\n  other:\n    steps:\n      - run: other\n  supply-chain-checks:\n    steps:\n      - run: " + step + "\n  later:\n    steps:\n      - run: later\n"
	}
	main := strings.Repeat("e", 40)
	for name, tc := range map[string]struct {
		files     []map[string]string
		baseCI    string
		headCI    string
		admit     bool
		extraRead int
	}{
		"nothing listed changed":      {[]map[string]string{{"filename": "internal/app/app.go"}}, "", "", true, 0},
		"listed file changed":         {[]map[string]string{{"filename": "hooks/pre-push"}}, "", "", false, 0},
		"file under listed directory": {[]map[string]string{{"filename": "fixtures/deep/a.sh"}}, "", "", false, 0},
		"listed file renamed away":    {[]map[string]string{{"filename": "elsewhere", "previous_filename": "hooks/pre-push"}}, "", "", false, 0},
		"the PR rewrote the selector": {[]map[string]string{{"filename": supplyChainSelectorPath}}, "", "", false, 0},
		"the PR rewrote the list":     {[]map[string]string{{"filename": supplyChainListPath}}, "", "", false, 0},
		"the PR rewrote ci-feedback":  {[]map[string]string{{"filename": supplyChainFeedbackPath}}, "", "", false, 0},
		"job steps changed in ci.yml": {[]map[string]string{{"filename": ci}}, job("a"), job("b"), false, 2},
		"other job changed in ci.yml": {[]map[string]string{{"filename": ci}}, job("a"), job("a") + "  extra:\n    steps: []\n", true, 2},
	} {
		t.Run(name, func(t *testing.T) {
			f := newDevFixture(t)
			f.q.dco = func(context.Context, pull) (int, error) { return 0, nil }
			setSupplyChainBase(f, testSupplyChainList)
			f.api.responses[f.c.endpoint(devFilesPage(f, 1))] = tc.files
			if tc.baseCI != "" {
				f.api.responses[f.c.endpoint("contents/"+ci+"?ref="+f.pr.Base.SHA)] = encodedFile(ci, tc.baseCI)
				f.api.responses[f.c.endpoint("contents/"+ci+"?ref="+f.pr.Head.SHA)] = encodedFile(ci, tc.headCI)
				f.api.responses[f.c.endpoint("contents/"+ci+"?ref="+main)] = encodedFile(ci, tc.headCI)
			}
			// The selection job reports success and the fixtures were skipped.
			setDevJobs(f, supplyChainJobs(f.feedbackRun, "success", "skipped"))
			_ = f.q.admit(context.Background(), f.pr.Number)
			if admitted(f) != tc.admit {
				t.Fatalf("admitted=%t, want %t: %+v", admitted(f), tc.admit, f.api.writes)
			}
			if !tc.admit {
				f.assertNoSuccess(t)
			}
		})
	}
}

// Fixtures that ran are never recomputed, so the common case adds no read; a
// skipped job adds the single read of the base list.
func TestDevSupplyChainRecomputationReadBudget(t *testing.T) {
	count := func(fixtures string) int {
		f := newDevFixture(t)
		f.q.dco = func(context.Context, pull) (int, error) { return 0, nil }
		setSupplyChainBase(f, testSupplyChainList)
		setDevJobs(f, supplyChainJobs(f.feedbackRun, "success", fixtures))
		if err := f.q.admit(context.Background(), f.pr.Number); err != nil || !admitted(f) {
			t.Fatalf("admit %s: %v", fixtures, err)
		}
		return totalReads(f)
	}
	if ran, skipped := count("success"), count("skipped"); skipped-ran != 1 {
		t.Fatalf("a skipped fixtures job added %d reads, want 1", skipped-ran)
	}
}

func TestSupplyChainJobBlockMatchesTheSelectorScript(t *testing.T) {
	workflow := "jobs:\n  a:\n    x: 1\n  supply-chain-checks:\n    steps:\n      - run: one\n\n    env: {}\n  b:\n    y: 2\n"
	if got, want := supplyChainJobBlock([]byte(workflow)), "    steps:\n      - run: one\n\n    env: {}"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
