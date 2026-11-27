package main

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

// backmergeFixture is the deadlock of #1601: main's publisher moved on, dev's copy
// (the PR's admission base) is the older one, and the head is a merge commit whose copy
// is headCopy. status and mergeBase are GitHub's answer to compare main...head; an empty
// status serves no answer.
func backmergeFixture(t *testing.T, headCopy, status, mergeBase string) (*queueFixture, string) {
	t.Helper()
	f := newDevFixture(t)
	main := strings.Repeat("e", 40)
	put := func(revision, content string) {
		f.api.responses[f.c.endpoint("contents/"+devGateWorkflowPath+"?ref="+revision)] = map[string]string{"type": "file", "path": devGateWorkflowPath, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))}
	}
	put(f.pr.Base.SHA, "older main publisher")
	put(f.pr.Head.SHA, headCopy)
	if status != "" {
		f.api.responses[f.c.endpoint(compareRange(main, f.pr.Head.SHA))] = map[string]any{"status": status, "base_commit": map[string]string{"sha": main}, "merge_base_commit": map[string]string{"sha": mergeBase}}
	}
	return f, main
}

func compareReads(f *queueFixture, main string) int {
	return f.api.reads[f.c.endpoint(compareRange(main, f.pr.Head.SHA))]
}

func TestDevPublisherAdmitsBackmergeContainingCurrentMain(t *testing.T) {
	main := strings.Repeat("e", 40)
	for _, status := range []string{"ahead", "identical"} {
		f, _ := backmergeFixture(t, "trusted dev publisher", status, main)
		if err := f.q.devPublisherContract(context.Background(), f.pr); err != nil {
			t.Fatalf("%s: a head containing main's current head with main's copy must be admitted: %v", status, err)
		}
		if got := compareReads(f, main); got != 1 {
			t.Fatalf("%s: ancestry read %d times, want 1", status, got)
		}
	}
}

func TestDevPublisherBackmergeRefusals(t *testing.T) {
	main, older := strings.Repeat("e", 40), strings.Repeat("d", 40)
	for name, tc := range map[string]struct {
		copy, status, mergeBase string
		compareRead             bool
	}{
		"modified copy":                    {"candidate-controlled publisher", "ahead", main, false},
		"does not contain main":            {"trusted dev publisher", "behind", main, true},
		"contains only an older main":      {"trusted dev publisher", "diverged", older, true},
		"ahead but merge base is not main": {"trusted dev publisher", "ahead", older, true},
	} {
		t.Run(name, func(t *testing.T) {
			f, main := backmergeFixture(t, tc.copy, tc.status, tc.mergeBase)
			if err := f.q.devPublisherContract(context.Background(), f.pr); !errors.Is(err, errDevPublisherDiffers) {
				t.Fatalf("want a publisher verdict, got %v", err)
			}
			if got := compareReads(f, main) > 0; got != tc.compareRead {
				t.Fatalf("ancestry read = %v, want %v (spent only where it can matter)", got, tc.compareRead)
			}
		})
	}
}

func TestDevPublisherBackmergeAncestryReadFailsClosed(t *testing.T) {
	main := strings.Repeat("e", 40)
	for name, tc := range map[string]struct{ status, mergeBase string }{
		"unexpected status":    {"weird", main},
		"malformed merge base": {"ahead", "x"},
	} {
		t.Run(name, func(t *testing.T) {
			f, _ := backmergeFixture(t, "trusted dev publisher", tc.status, tc.mergeBase)
			err := f.q.devPublisherContract(context.Background(), f.pr)
			if err == nil || errors.Is(err, errDevPublisherDiffers) {
				t.Fatalf("a bad ancestry answer must be a read error, not admission or a verdict: %v", err)
			}
		})
	}
	t.Run("read error", func(t *testing.T) {
		f, _ := backmergeFixture(t, "trusted dev publisher", "", "")
		f.q.api = failedQueueRead{api: f.q.api, contains: "compare/"}
		err := f.q.devPublisherContract(context.Background(), f.pr)
		if err == nil || errors.Is(err, errDevPublisherDiffers) {
			t.Fatalf("a failed ancestry read must fail closed as a read error: %v", err)
		}
	})
}

func TestDevPublisherBackmergeRequiresSameRepository(t *testing.T) {
	f, main := backmergeFixture(t, "trusted dev publisher", "ahead", strings.Repeat("e", 40))
	f.pr.Head.Repo.ID, f.pr.Head.Repo.FullName = 11, "contributor/Hikyo"
	if err := f.q.devPublisherContract(context.Background(), f.pr); !errors.Is(err, errDevPublisherDiffers) {
		t.Fatalf("a fork head must be refused, got %v", err)
	}
	if compareReads(f, main) != 0 {
		t.Fatal("a fork head must not spend the ancestry read")
	}
}

// What the sizing pins for a pass that takes the exception: one read more than the 45 of
// TestDevAdmissionReadsThePullRequestTwice on dev, and one request more than the 95 of the attempt-3 bypass.
const (
	backmergePassReads            = 46
	attempt3BackmergePassRequests = 96
)

func backmergeAdmission(t *testing.T) *queueFixture {
	t.Helper()
	f, _ := backmergeFixture(t, "trusted dev publisher", "ahead", strings.Repeat("e", 40))
	f.c.repo = f.pr.Base.Repo
	f.c.repo.DefaultBranch = "main"
	return f
}

func TestDevBackmergeAdmissionCostIsPinnedForTheSizing(t *testing.T) {
	t.Setenv("GITHUB_REF", "refs/heads/main")
	f := backmergeAdmission(t)
	if err := f.q.executeDev(context.Background(), "workflow_dispatch", event{Repository: f.c.repo}, f.pr.Number); err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, n := range f.api.reads {
		total += n
	}
	if total != backmergePassReads {
		t.Fatalf("a backmerge admission spent %d reads, the sizing pins %d", total, backmergePassReads)
	}
	requireDevSuccess(t, f)
}

func TestDevBackmergeAttempt3UnconfirmedPassCostIsPinnedForTheSizing(t *testing.T) {
	t.Setenv("GITHUB_REF", "refs/heads/main")
	t.Setenv("GITHUB_RUN_ATTEMPT", "3")
	recordReadWaits(t)
	f := backmergeAdmission(t)
	counter := &countingAPI{inner: &laggingListAPI{inner: f.api, lag: 1 << 20}}
	f.c.api = counter
	budget := admissionBudget{Started: time.Now(), Deadline: time.Now().Add(time.Minute)}
	err := f.q.withAdmissionBudget(context.Background(), budget, func(work context.Context) error {
		return f.q.executeDev(work, "workflow_dispatch", event{Repository: f.c.repo}, f.pr.Number)
	})
	if err == nil {
		t.Fatal("the pass must fail")
	}
	if got := counter.reads.Load() + counter.writes.Load(); got != attempt3BackmergePassRequests {
		t.Fatalf("a backmerge pass with a lagging confirmation spends %d requests, the sizing pins %d", got, attempt3BackmergePassRequests)
	}
}
