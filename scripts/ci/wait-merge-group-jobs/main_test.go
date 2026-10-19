package main

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func fixture() (source, []job) {
	s := source{repo: "owner/repo", head: strings.Repeat("a", 40), run: 50, attempt: 2}
	return s, []job{
		{ID: 1, Run: 50, Attempt: 2, Head: s.head, Name: "validation / ci-required", Status: "completed", Conclusion: "success"},
		{ID: 2, Run: 50, Attempt: 2, Head: s.head, Name: "merge-group-proof", Status: "completed", Conclusion: "success"},
	}
}

func jobPage(jobs []job) page {
	n := len(jobs)
	return page{Total: &n, Jobs: jobs}
}

func TestWaitForActualJobMetadataTransition(t *testing.T) {
	s, jobs := fixture()
	jobs[1].Status, jobs[1].Conclusion = "in_progress", ""
	var out bytes.Buffer
	reads, pauses := 0, 0
	fetch := func(context.Context, int) (page, error) {
		reads++
		return jobPage(jobs), nil
	}
	pause := func(context.Context) error {
		pauses++
		if strings.Contains(out.String(), "Both required") {
			t.Fatal("claimed success before terminal proof")
		}
		jobs[1].Status, jobs[1].Conclusion = "completed", "success"
		return nil
	}
	if err := wait(t.Context(), s, fetch, pause, &out); err != nil {
		t.Fatal(err)
	}
	if reads != 2 || pauses != 1 || !strings.Contains(out.String(), "Waiting for GitHub job metadata: merge-group-proof") || !strings.Contains(out.String(), "Both required proof jobs are complete") {
		t.Fatalf("unexpected readiness transition: reads=%d pauses=%d output=%q", reads, pauses, out.String())
	}
}

func TestRequiredJobsRejectForeignOrFailedProof(t *testing.T) {
	for _, scenario := range []string{"run", "attempt", "head", "zero-id", "duplicate-id", "duplicate-proof", "failure", "skipped", "cancelled", "unknown", "pending-conclusion", "missing-conclusion"} {
		t.Run(scenario, func(t *testing.T) {
			s, jobs := fixture()
			switch scenario {
			case "run":
				jobs[1].Run++
			case "attempt":
				jobs[1].Attempt--
			case "head":
				jobs[1].Head = strings.Repeat("b", 40)
			case "zero-id":
				jobs[1].ID = 0
			case "duplicate-id":
				jobs[1].ID = jobs[0].ID
			case "duplicate-proof":
				jobs = append(jobs, jobs[1])
				jobs[2].ID = 3
			case "failure", "skipped", "cancelled":
				jobs[1].Conclusion = scenario
			case "unknown":
				jobs[1].Status = "unknown"
			case "pending-conclusion":
				jobs[1].Status = "in_progress"
			case "missing-conclusion":
				jobs[1].Conclusion = ""
			}
			if _, err := requiredJobs(s, jobs); err == nil {
				t.Fatal("accepted invalid proof")
			}
		})
	}
}

func TestRequiredJobsKeepMissingAndNonterminalProofPending(t *testing.T) {
	for _, status := range []string{"missing", "queued", "in_progress", "waiting", "pending", "requested"} {
		t.Run(status, func(t *testing.T) {
			s, jobs := fixture()
			if status == "missing" {
				jobs = jobs[:1]
			} else {
				jobs[1].Status, jobs[1].Conclusion = status, ""
			}
			pending, err := requiredJobs(s, jobs)
			if err != nil || len(pending) != 1 || pending[0] != "merge-group-proof" {
				t.Fatalf("lost incomplete proof: %v %v", pending, err)
			}
		})
	}
}

func TestWaitHonorsCancellationAndReadFailures(t *testing.T) {
	s, jobs := fixture()
	jobs[1].Status, jobs[1].Conclusion = "in_progress", ""
	ctx, cancel := context.WithCancel(t.Context())
	err := wait(ctx, s, func(context.Context, int) (page, error) { return jobPage(jobs), nil }, func(context.Context) error { cancel(); return ctx.Err() }, &bytes.Buffer{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	sentinel := errors.New("API unavailable")
	err = wait(t.Context(), s, func(context.Context, int) (page, error) { return page{}, sentinel }, func(context.Context) error { t.Fatal("paused after API failure"); return nil }, &bytes.Buffer{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("lost API error: %v", err)
	}
}

func TestWaitRetriesOnlyBoundedServerReads(t *testing.T) {
	for _, status := range []int{502, 503, 401, 403} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			s, jobs := fixture()
			reads, pauses := 0, 0
			err := wait(t.Context(), s, func(context.Context, int) (page, error) {
				reads++
				if status == 502 && reads == 2 {
					return jobPage(jobs), nil
				}
				return page{}, &apiReadError{status: status, cause: errors.New("unavailable")}
			}, func(context.Context) error { pauses++; return nil }, &bytes.Buffer{})
			if status == 502 {
				if err != nil || reads != 2 || pauses != 1 {
					t.Fatalf("transient read did not recover: %d %d %v", reads, pauses, err)
				}
			} else if status == 503 {
				if err == nil || reads != 3 || pauses != 2 {
					t.Fatalf("server retries were not bounded: %d %d %v", reads, pauses, err)
				}
			} else if err == nil || reads != 1 || pauses != 0 {
				t.Fatalf("authorization failure was retried: %d %d %v", reads, pauses, err)
			}
		})
	}
}

func TestCommandReadErrorExposesOnlyStatus(t *testing.T) {
	err := commandReadError(&exec.ExitError{Stderr: []byte("gh: private-response-sentinel (HTTP 502)")})
	if !strings.Contains(err.Error(), "HTTP 502") || strings.Contains(err.Error(), "private-response-sentinel") {
		t.Fatalf("unexpected diagnostic: %v", err)
	}
}

func TestReadJobsConservesCompletePagination(t *testing.T) {
	_, seed := fixture()
	jobs := make([]job, 102)
	for i := range jobs {
		jobs[i] = seed[0]
		jobs[i].ID = int64(i + 1)
	}
	total := len(jobs)
	reads := 0
	got, err := readJobs(t.Context(), func(_ context.Context, n int) (page, error) {
		reads++
		start := (n - 1) * 100
		return page{Total: &total, Jobs: jobs[start:min(start+100, total)]}, nil
	})
	if err != nil || len(got) != total || reads != 2 {
		t.Fatalf("pagination lost jobs: %d %d %v", len(got), reads, err)
	}
	for _, scenario := range []string{"nil-total", "nil-jobs", "negative", "over-bound", "partial", "changed"} {
		t.Run(scenario, func(t *testing.T) {
			_, err := readJobs(t.Context(), func(_ context.Context, n int) (page, error) {
				p := page{Total: &total, Jobs: jobs[(n-1)*100 : min(n*100, total)]}
				switch scenario {
				case "nil-total":
					p.Total = nil
				case "nil-jobs":
					p.Jobs = nil
				case "negative":
					v := -1
					p.Total = &v
				case "over-bound":
					v := 1001
					p.Total = &v
				case "partial":
					p.Jobs = p.Jobs[:1]
				case "changed":
					if n == 2 {
						v := total - 1
						p.Total = &v
					}
				}
				return p, nil
			})
			if err == nil {
				t.Fatal("accepted incomplete pagination")
			}
		})
	}
}

func TestEnvironmentRequiresExactMergeGroupIdentity(t *testing.T) {
	values := map[string]string{"GITHUB_EVENT_NAME": "merge_group", "GITHUB_REPOSITORY": "owner/repo", "GITHUB_SHA": strings.Repeat("a", 40), "GITHUB_RUN_ID": "50", "GITHUB_RUN_ATTEMPT": "2"}
	if _, err := environment(func(k string) string { return values[k] }); err != nil {
		t.Fatal(err)
	}
	for key := range values {
		t.Run(key, func(t *testing.T) {
			if _, err := environment(func(k string) string {
				if k == key {
					return "invalid"
				}
				return values[k]
			}); err == nil {
				t.Fatal("accepted invalid environment")
			}
		})
	}
}
