package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Bound the latency/rate tradeoff of parallel read-only inventories. Any failed
// request, including HTTP 403/429, cancels the page without retries or backoff.
const retirementDiscoveryWorkers = 4

type retirementDiscoveryScan struct {
	started             time.Time
	snapshot            string
	commitsExamined     int
	historyPages        int
	candidatesAttempted atomic.Int64
	candidatesCompleted atomic.Int64
	restRequests        atomic.Int64
	restPages           atomic.Int64
}

func newRetirementDiscoveryScan() *retirementDiscoveryScan {
	return &retirementDiscoveryScan{started: time.Now()}
}

// Emit only bounded scan metadata after all candidate workers have joined.
// Diagnostics never contain API response bodies or share the target JSON stream.
func (s *retirementDiscoveryScan) report(out io.Writer, scanErr error) error {
	if out == nil {
		return nil
	}
	status := "success"
	if errors.Is(scanErr, context.DeadlineExceeded) {
		status = "deadline_exceeded"
	} else if errors.Is(scanErr, context.Canceled) {
		status = "canceled"
	} else if scanErr != nil {
		status = "failure"
	}
	record := struct {
		Event               string `json:"event"`
		Snapshot            string `json:"snapshot"`
		CommitsExamined     int    `json:"commits_examined"`
		HistoryPages        int    `json:"history_pages"`
		CandidatesAttempted int64  `json:"candidate_heads_attempted"`
		CandidatesCompleted int64  `json:"candidate_heads_completed"`
		RESTRequests        int64  `json:"rest_requests"`
		RESTPages           int64  `json:"rest_pages"`
		ElapsedMS           int64  `json:"elapsed_ms"`
		Status              string `json:"status"`
		WorkerLimit         int    `json:"worker_limit"`
	}{
		Event: "retirement_discovery_scan", Snapshot: s.snapshot,
		CommitsExamined: s.commitsExamined, HistoryPages: s.historyPages,
		CandidatesAttempted: s.candidatesAttempted.Load(), CandidatesCompleted: s.candidatesCompleted.Load(),
		RESTRequests: s.restRequests.Load(), RESTPages: s.restPages.Load(),
		ElapsedMS: time.Since(s.started).Milliseconds(), Status: status, WorkerLimit: retirementDiscoveryWorkers,
	}
	body, err := json.Marshal(record)
	if err != nil {
		return errors.New("retirement discovery diagnostics could not be encoded")
	}
	body = append(body, '\n')
	if n, err := out.Write(body); err != nil || n != len(body) {
		return errors.New("retirement discovery diagnostics could not be written")
	}
	return nil
}

// Count every inventory request, including unrelated/native contexts and failed
// reads. RESTPages counts received JSON pages; inventory validation still runs
// independently and no partial inventory authorizes a retirement hint.
type retirementDiscoveryReadAPI struct {
	api  api
	scan *retirementDiscoveryScan
}

func (a retirementDiscoveryReadAPI) read(ctx context.Context, path string, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	inventory := strings.Contains(path, "/check-runs?")
	if inventory {
		a.scan.restRequests.Add(1)
	}
	// Each counted request must be exactly one attempt, whatever API was wired.
	reader := a.api
	if retrying, ok := reader.(retryingAPI); ok {
		reader = retrying.api
	}
	err := reader.read(ctx, path, out)
	if inventory && err == nil {
		a.scan.restPages.Add(1)
	}
	return err
}

func (retirementDiscoveryReadAPI) write(context.Context, string, string, any, any) error {
	return errors.New("retirement discovery cannot mutate GitHub state")
}

type retirementCandidateInventory struct {
	head        string
	queue       bool
	observation bool
	reporters   []reporterRetirementTarget
	err         error
}

// Authenticate a history page before calling this helper. Each worker loads one
// complete head inventory at a time; pagination/ownership remain unchanged.
// Workers cancel siblings on failure and always join before results or metrics
// are read. The caller aggregates results in history order, never completion order.
func (c *controller) retirementCandidateInventories(ctx context.Context, heads []string, scan *retirementDiscoveryScan) ([]retirementCandidateInventory, error) {
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int, len(heads))
	for i := range heads {
		jobs <- i
	}
	close(jobs)
	results := make([]retirementCandidateInventory, len(heads))
	var workers sync.WaitGroup
	for range min(retirementDiscoveryWorkers, len(heads)) {
		workers.Go(func() {
			for i := range jobs {
				if work.Err() != nil {
					return
				}
				scan.candidatesAttempted.Add(1)
				q, o, reporters, err := c.retirementPendingChecks(work, heads[i])
				results[i] = retirementCandidateInventory{head: heads[i], queue: q, observation: o, reporters: reporters, err: err}
				if err != nil {
					cancel()
					return
				}
				scan.candidatesCompleted.Add(1)
			}
		})
	}
	workers.Wait()
	var failures []error
	var canceled []error
	for _, result := range results {
		if result.err != nil {
			if errors.Is(result.err, context.Canceled) {
				canceled = append(canceled, result.err)
			} else {
				failures = append(failures, result.err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		failures = append(failures, err)
	}
	if len(failures) == 0 {
		failures = canceled
	}
	if err := errors.Join(failures...); err != nil {
		return nil, err
	}
	return results, nil
}
