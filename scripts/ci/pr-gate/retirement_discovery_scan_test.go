package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type retirementScanTestAPI struct {
	api
	before func(context.Context, string) error
	after  func(string, error)
}

func (a retirementScanTestAPI) read(ctx context.Context, path string, out any) error {
	if strings.Contains(path, "/check-runs?") {
		if a.before != nil {
			if err := a.before(ctx, path); err != nil {
				return err
			}
		}
	}
	err := a.api.read(ctx, path, out)
	if a.after != nil {
		a.after(path, err)
	}
	return err
}

type retirementScanResult struct {
	targets retirementTargets
	err     error
}

func retirementConcurrentFixture(t *testing.T, count int) (*queueFixture, []string) {
	t.Helper()
	f, head := retirementDiscoveryFixture(t)
	heads := []string{head}
	nodes := []map[string]any{retirementHistoryNode(f, head, []map[string]any{retirementHistoryRecord(f, head, queueCheckName, "QUEUED")})}
	for i := 1; i < count; i++ {
		oid := fmt.Sprintf("%040x", 1000+i)
		heads = append(heads, oid)
		checks := []map[string]any{retirementHistoryRecord(f, oid, queueCheckName, "QUEUED"), retirementHistoryRecord(f, oid, "ci-required-next", "QUEUED"), reporterDiscoveryRecord(f, oid, int64(1000-i), 1)}
		node := retirementHistoryNode(f, oid, checks)
		node["statusCheckRollup"].(map[string]any)["state"] = []string{"ERROR", "EXPECTED", "FAILURE", "PENDING"}[i%4]
		nodes = append(nodes, node)
	}
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, nodes, false, "")
	return f, heads
}

func awaitRetirementResult(t *testing.T, done <-chan retirementScanResult) retirementScanResult {
	t.Helper()
	select {
	case got := <-done:
		return got
	case <-time.After(3 * time.Second):
		t.Fatal("retirement discovery did not cancel and drain its workers")
		return retirementScanResult{}
	}
}

func TestRetirementDiscoveryConcurrentInventoriesStayBoundedAndSorted(t *testing.T) {
	f, heads := retirementConcurrentFixture(t, 9)
	var active, peak atomic.Int64
	started := make(chan struct{}, len(heads))
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.c.api = retirementScanTestAPI{api: f.c.api, before: func(ctx context.Context, _ string) error {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		started <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	done := make(chan retirementScanResult, 1)
	go func() {
		targets, err := f.c.discoverRetirements(ctx, "schedule", f.event)
		done <- retirementScanResult{targets, err}
	}()
	for range 4 {
		select {
		case <-started:
		case <-time.After(time.Second):
			cancel()
			awaitRetirementResult(t, done)
			t.Fatal("four candidate inventories did not overlap")
		}
	}
	close(release)
	got := awaitRetirementResult(t, done)
	if got.err != nil || peak.Load() != 4 || active.Load() != 0 {
		t.Fatalf("bounded inventory: peak=%d active=%d error=%v", peak.Load(), active.Load(), got.err)
	}
	sort.Strings(heads)
	observations := append([]string{}, heads[:len(heads)-1]...)
	reporters := []reporterRetirementTarget{}
	for i := 1; i < len(heads); i++ {
		reporters = append(reporters, reporterRetirementTarget{Head: fmt.Sprintf("%040x", 1000+i), Run: int64(1000 - i), Attempt: 1})
	}
	sort.Slice(reporters, func(i, j int) bool { return reporters[i].Run < reporters[j].Run })
	want := retirementTargets{HistoryScope: retirementHistoryScope{Snapshot: f.pr.Base.SHA, MaxCommits: 1000, ScannedCommits: len(heads), CompleteAncestry: true}, Queue: heads, Observation: observations, ObservationBatches: [][]string{observations}, ReporterTargets: reporters}
	if !reflect.DeepEqual(got.targets, want) {
		t.Fatalf("completion order changed sorted targets: %+v", got.targets)
	}
	if len(f.api.writes) != 0 {
		t.Fatal("concurrent discovery mutated checks")
	}
}

func TestRetirementDiscoveryCancelsAndDrainsSiblingInventories(t *testing.T) {
	for _, tc := range []struct {
		name     string
		external bool
		failure  error
	}{
		{name: "request failure", failure: errors.New("inventory request failed")},
		{name: "HTTP 403", failure: &githubReadError{path: "fixture", status: 403}},
		{name: "HTTP 429", failure: &githubReadError{path: "fixture", status: 429}},
		{name: "external cancellation", external: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := retirementConcurrentFixture(t, 12)
			var diagnostic bytes.Buffer
			f.c.diagnostics = &diagnostic
			var active, attempted atomic.Int64
			allStarted := make(chan struct{})
			failure := tc.failure
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f.c.api = retirementScanTestAPI{api: f.c.api, before: func(ctx context.Context, _ string) error {
				active.Add(1)
				defer active.Add(-1)
				n := attempted.Add(1)
				if n == 4 {
					close(allStarted)
				}
				if n == 1 && !tc.external {
					select {
					case <-allStarted:
						return failure
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				<-ctx.Done()
				return ctx.Err()
			}}
			done := make(chan retirementScanResult, 1)
			go func() {
				targets, err := f.c.discoverRetirements(ctx, "schedule", f.event)
				done <- retirementScanResult{targets, err}
			}()
			select {
			case <-allStarted:
			case <-time.After(time.Second):
				cancel()
				awaitRetirementResult(t, done)
				t.Fatal("four cancellation siblings did not start")
			}
			if tc.external {
				cancel()
			}
			got := awaitRetirementResult(t, done)
			want := failure
			if tc.external {
				want = context.Canceled
			}
			if !errors.Is(got.err, want) || active.Load() != 0 || attempted.Load() != 4 || !reflect.DeepEqual(got.targets, retirementTargets{}) {
				t.Fatalf("cancellation lost cause, leaked workers, or exposed partial targets: active=%d attempted=%d targets=%+v error=%v", active.Load(), attempted.Load(), got.targets, got.err)
			}
			var record struct {
				Status    string `json:"status"`
				Attempted int    `json:"candidate_heads_attempted"`
				Completed int    `json:"candidate_heads_completed"`
				Requests  int    `json:"rest_requests"`
				Pages     int    `json:"rest_pages"`
			}
			status := "failure"
			if tc.external {
				status = "canceled"
			}
			if json.Unmarshal(diagnostic.Bytes(), &record) != nil || record.Status != status || record.Attempted != 4 || record.Completed != 0 || record.Requests != 4 || record.Pages != 0 {
				t.Fatalf("cancellation metrics lost failed requests: %s", diagnostic.String())
			}
		})
	}
}

func TestRetirementDiscoveryFailureDiscardsSuccessfulSiblingsAndEarlierPages(t *testing.T) {
	f, heads := retirementConcurrentFixture(t, 3)
	first := retirementHistoryNode(f, heads[0], []map[string]any{retirementHistoryRecord(f, heads[0], queueCheckName, "QUEUED")})
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(heads[0], "", 100)] = retirementHistoryResponse(f, heads[0], []map[string]any{first}, true, "next")
	checks := make([]map[string]any, 100)
	for i := range checks {
		checks[i] = retirementHistoryRecord(f, heads[2], fmt.Sprintf("native job %d", i), "COMPLETED")
	}
	second := retirementHistoryNode(f, heads[1], []map[string]any{retirementHistoryRecord(f, heads[1], queueCheckName, "QUEUED")})
	third := retirementHistoryNode(f, heads[2], checks)
	f.api.responses[retirementChecksPath(f, heads[2], 1)].(map[string]any)["total_count"] = 101
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(heads[0], "next", 100)] = retirementHistoryResponse(f, heads[0], []map[string]any{second, third}, false, "")
	failure := errors.New("second inventory page unavailable")
	f.api.responses[retirementChecksPath(f, heads[2], 2)] = failure
	got, err := f.c.discoverRetirements(t.Context(), "schedule", f.event)
	if !errors.Is(err, failure) || !reflect.DeepEqual(got, retirementTargets{}) {
		t.Fatalf("partial success survived an incomplete inventory: %+v/%v", got, err)
	}
}

func TestRetirementDiscoveryReportsUnrelatedCandidateCostsSeparately(t *testing.T) {
	f, heads := retirementConcurrentFixture(t, 5)
	var diagnostic, output bytes.Buffer
	f.c.diagnostics, f.c.output = &diagnostic, &output
	for _, head := range heads[1:] {
		f.api.responses[retirementChecksPath(f, head, 1)] = map[string]any{"total_count": 1, "check_runs": []map[string]any{retirementHistoryRecord(f, head, "unrelated native failure", "COMPLETED")}}
	}
	got, err := f.c.discoverRetirements(t.Context(), "schedule", f.event)
	var record struct {
		Snapshot            string `json:"snapshot"`
		Commits             int    `json:"commits_examined"`
		CandidatesAttempted int    `json:"candidate_heads_attempted"`
		CandidatesCompleted int    `json:"candidate_heads_completed"`
		Requests            int    `json:"rest_requests"`
		Pages               int    `json:"rest_pages"`
		ElapsedMS           int64  `json:"elapsed_ms"`
		Status              string `json:"status"`
		Workers             int    `json:"worker_limit"`
	}
	if err != nil || json.Unmarshal(diagnostic.Bytes(), &record) != nil || record.Snapshot != heads[0] || record.Commits != 5 || record.CandidatesAttempted != 5 || record.CandidatesCompleted != 5 || record.Requests != 5 || record.Pages != 5 || record.Status != "success" || record.Workers != 4 || record.ElapsedMS < 0 || output.Len() != 0 || len(got.Queue) != 1 {
		t.Fatalf("scan cost hidden or mixed with output: targets=%+v diagnostic=%s output=%q error=%v", got, diagnostic.String(), output.String(), err)
	}
}

func TestRetirementDiscoveryReportsFailedSecondPageWithoutPartialTargets(t *testing.T) {
	f, heads := retirementConcurrentFixture(t, 2)
	var diagnostic bytes.Buffer
	f.c.diagnostics = &diagnostic
	checks := make([]map[string]any, 100)
	for i := range checks {
		checks[i] = retirementHistoryRecord(f, heads[1], fmt.Sprintf("unrelated native job %d", i), "COMPLETED")
	}
	f.api.responses[retirementChecksPath(f, heads[1], 1)] = map[string]any{"total_count": 101, "check_runs": checks}
	failure := errors.New("private server response must not appear in diagnostics")
	f.api.responses[retirementChecksPath(f, heads[1], 2)] = failure
	successful := make(chan struct{})
	f.c.api = retirementScanTestAPI{api: f.c.api, before: func(ctx context.Context, path string) error {
		if path == retirementChecksPath(f, heads[1], 1) {
			select {
			case <-successful:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}, after: func(path string, err error) {
		if path == retirementChecksPath(f, heads[0], 1) && err == nil {
			close(successful)
		}
	}}
	got, err := f.c.discoverRetirements(t.Context(), "schedule", f.event)
	var record struct {
		Completed int    `json:"candidate_heads_completed"`
		Attempted int    `json:"candidate_heads_attempted"`
		Requests  int    `json:"rest_requests"`
		Pages     int    `json:"rest_pages"`
		Status    string `json:"status"`
	}
	if !errors.Is(err, failure) || !reflect.DeepEqual(got, retirementTargets{}) || json.Unmarshal(diagnostic.Bytes(), &record) != nil || record.Completed != 1 || record.Attempted != 2 || record.Requests != 3 || record.Pages != 2 || record.Status != "failure" || strings.Contains(diagnostic.String(), failure.Error()) {
		t.Fatalf("failed pagination hid costs, cause, or partial work: targets=%+v diagnostic=%s error=%v", got, diagnostic.String(), err)
	}
}

func TestRetirementDiscoveryReportsDeadlineBeforeInventories(t *testing.T) {
	f, _ := retirementConcurrentFixture(t, 1)
	var diagnostic bytes.Buffer
	f.c.diagnostics = &diagnostic
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	got, err := f.c.discoverRetirements(ctx, "schedule", f.event)
	var record struct {
		Status   string `json:"status"`
		Requests int    `json:"rest_requests"`
	}
	if !errors.Is(err, context.DeadlineExceeded) || !reflect.DeepEqual(got, retirementTargets{}) || json.Unmarshal(diagnostic.Bytes(), &record) != nil || record.Status != "deadline_exceeded" || record.Requests != 0 {
		t.Fatalf("deadline lost its diagnostic identity: targets=%+v diagnostic=%s error=%v", got, diagnostic.String(), err)
	}
}

type retirementDiagnosticFailure struct{ short bool }

func (w retirementDiagnosticFailure) Write(data []byte) (int, error) {
	if w.short {
		return len(data) - 1, nil
	}
	return 0, io.ErrClosedPipe
}

func TestRetirementDiscoveryDiagnosticFailureDiscardsTargets(t *testing.T) {
	for _, short := range []bool{false, true} {
		t.Run(fmt.Sprintf("short=%t", short), func(t *testing.T) {
			f, _ := retirementConcurrentFixture(t, 1)
			f.c.diagnostics = retirementDiagnosticFailure{short: short}
			got, err := f.c.discoverRetirements(t.Context(), "schedule", f.event)
			if err == nil || !reflect.DeepEqual(got, retirementTargets{}) {
				t.Fatalf("diagnostic failure silently produced targets: %+v/%v", got, err)
			}
		})
	}
}
