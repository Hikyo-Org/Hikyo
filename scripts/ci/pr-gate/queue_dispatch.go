package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const queueWorkerFilename = "ci-queue-worker.yml"
const queueHeadBatchSize = 64

type headWork struct {
	Head string `json:"head"`
	Kind string `json:"kind"`
}

func parseHeadBatch(data string) ([]headWork, error) {
	d := json.NewDecoder(bytes.NewBufferString(data))
	d.DisallowUnknownFields()
	var batch []headWork
	if err := d.Decode(&batch); err != nil {
		return nil, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF || batch == nil || len(batch) == 0 || len(batch) > queueHeadBatchSize {
		return nil, errors.New("worker input must be one bounded nonempty head array")
	}
	seen := make(map[string]bool)
	for _, w := range batch {
		if !shaPattern.MatchString(w.Head) || seen[w.Head] || w.Kind != "admission" && w.Kind != "group" && w.Kind != "ambiguous" {
			return nil, errors.New("invalid, duplicate or unknown worker head/role")
		}
		seen[w.Head] = true
	}
	return batch, nil
}

func (q *queueController) discover(ctx context.Context, name string, e event) ([]headWork, error) {
	if err := q.init(ctx); err != nil {
		return nil, err
	}
	if e.Repository.ID != q.repo.ID || !strings.EqualFold(e.Repository.FullName, q.repoName) || q.expectedHead != "" || name != "push" && name != "schedule" && name != "workflow_dispatch" {
		return nil, errors.New("read-only discovery requires a trusted batch/manual event without a writer lease")
	}
	if name == "push" && e.Ref != "refs/heads/"+q.repo.DefaultBranch {
		return nil, errors.New("discovery push is not the protected default branch")
	}
	var pulls []pull
	if name == "workflow_dispatch" {
		n, err := strconv.Atoi(e.Inputs.PR)
		if err != nil || n <= 0 {
			return nil, errors.New("manual discovery requires a PR number")
		}
		p, err := q.readPR(ctx, n)
		if err != nil {
			return nil, err
		}
		if p.State == "open" {
			pulls = []pull{p}
		}
	} else {
		var err error
		pulls, err = q.openPRs(ctx)
		if err != nil {
			return nil, err
		}
	}
	policy, err := q.policy(ctx)
	if err != nil {
		return nil, err
	}
	if !policy.Enabled {
		return nil, nil
	}
	live, err := q.liveQueue(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := materializedHeads(live, q.repo.DefaultBranch)
	if err != nil {
		return nil, err
	}
	byHead := make(map[string]string)
	for _, p := range pulls {
		byHead[p.Head.SHA] = "admission"
	}
	for _, head := range groups {
		if byHead[head] == "admission" {
			byHead[head] = "ambiguous"
		} else {
			byHead[head] = "group"
		}
	}
	if len(byHead) > 4000 {
		return nil, errors.New("complete discovery exceeds its independent PR/group bounds")
	}
	work := make([]headWork, 0, len(byHead))
	for head, kind := range byHead {
		work = append(work, headWork{Head: head, Kind: kind})
	}
	sort.Slice(work, func(i, j int) bool { return work[i].Head < work[j].Head })
	return work, nil
}

func dispatchResponseBody(data []byte) ([]byte, error) {
	lineEnd := bytes.IndexByte(data, '\n')
	if lineEnd < 0 {
		return nil, errors.New("missing workflow dispatch HTTP status")
	}
	status := strings.Fields(string(data[:lineEnd]))
	if len(status) < 2 || !regexp.MustCompile(`^HTTP/(1\.[01]|2(?:\.0)?|3(?:\.0)?)$`).MatchString(status[0]) || status[1] != "200" {
		return nil, errors.New("workflow dispatch did not return its documented HTTP 200 receipt")
	}
	if _, body, found := bytes.Cut(data, []byte("\r\n\r\n")); found {
		return body, nil
	}
	if _, body, found := bytes.Cut(data, []byte("\n\n")); found {
		return body, nil
	}
	return nil, errors.New("missing workflow dispatch response body")
}

type workerReceipt struct {
	ID   int64  `json:"workflow_run_id"`
	URL  string `json:"run_url"`
	HTML string `json:"html_url"`
}

func (q *queueController) dispatchWorkers(ctx context.Context, name string, e event) error {
	work, err := q.discover(ctx, name, e)
	if err != nil {
		return err
	} // Complete snapshots before the first dispatch.
	seenRuns := make(map[int64]bool)
	if len(work) == 0 {
		return nil
	}
	count := (len(work) + queueHeadBatchSize - 1) / queueHeadBatchSize
	first := 0
	if !q.dryRun {
		slot, err := q.dispatchSlot(ctx, name)
		if err != nil {
			return err
		}
		first = int(slot % int64(count))
	}
	order := make([]int, 0, count)
	if name == "workflow_dispatch" {
		n, _ := strconv.Atoi(e.Inputs.PR)
		p, err := q.readPR(ctx, n)
		if err != nil {
			return err
		}
		if p.State == "open" {
			for i, w := range work {
				if w.Head == p.Head.SHA {
					order = append(order, i/queueHeadBatchSize)
					break
				}
			}
		}
	}
	for i := 0; i < count; i++ {
		index := (first + i) % count
		if len(order) == 0 || index != order[0] {
			order = append(order, index)
		}
	}
	var failures []error
	for _, index := range order {
		offset := index * queueHeadBatchSize
		batch := work[offset:min(offset+queueHeadBatchSize, len(work))]
		data, err := json.Marshal(batch)
		if err != nil {
			return err
		}
		var receipt workerReceipt
		payload := map[string]any{"ref": q.repo.DefaultBranch, "inputs": map[string]string{"work": string(data)}}
		if q.dryRun {
			if _, err := fmt.Fprintf(q.output, "DRY RUN head batch %s\n", data); err != nil {
				return err
			}
			continue
		}
		request, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = q.api.write(request, "POST", q.endpoint("actions/workflows/"+queueWorkerFilename+"/dispatches"), payload, &receipt)
		cancel()
		if err != nil {
			failures = append(failures, fmt.Errorf("worker batch %s not acknowledged (ambiguous mutation is not retried): %w", data, err))
			continue
		}
		if receipt.ID <= 0 || seenRuns[receipt.ID] || receipt.URL != fmt.Sprintf("https://api.github.com/repos/%s/actions/runs/%d", q.repo.FullName, receipt.ID) || receipt.HTML != fmt.Sprintf("https://github.com/%s/actions/runs/%d", q.repo.FullName, receipt.ID) {
			failures = append(failures, fmt.Errorf("worker batch %s has an invalid/ambiguous receipt; no retry", data))
			continue
		}
		seenRuns[receipt.ID] = true
		if _, err := fmt.Fprintf(q.output, "Dispatched head batch: worker_run=%d heads=%s\n", receipt.ID, data); err != nil {
			return err
		}
	}
	return errors.Join(failures...)
}

func (q *queueController) dispatchSlot(ctx context.Context, name string) (int64, error) {
	id, err := strconv.ParseInt(os.Getenv("GITHUB_RUN_ID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("dispatch needs its authenticated current run identity")
	}
	attempt, err := strconv.ParseInt(os.Getenv("GITHUB_RUN_ATTEMPT"), 10, 64)
	if err != nil || attempt <= 0 {
		return 0, errors.New("dispatch needs its authenticated current attempt")
	}
	var workflow workflow
	if err := q.api.read(ctx, q.endpoint("actions/workflows/ci-queue-gate.yml"), &workflow); err != nil {
		return 0, err
	}
	var r run
	if err := q.api.read(ctx, q.endpoint(fmt.Sprintf("actions/runs/%d", id)), &r); err != nil {
		return 0, err
	}
	if workflow.ID <= 0 || workflow.Path != ".github/workflows/ci-queue-gate.yml" || workflow.Name != "queue-gate" || r.ID != id || r.Attempt != attempt || r.WorkflowID != workflow.ID || r.Path != workflow.Path || r.Name != workflow.Name || r.Event != name || r.HeadBranch != q.repo.DefaultBranch || !shaPattern.MatchString(r.HeadSHA) || r.Repository.ID != q.repo.ID || r.HeadRepository.ID != q.repo.ID {
		return 0, errors.New("dispatch run is not the current trusted default-branch producer")
	}
	created, err := time.Parse(time.RFC3339, r.CreatedAt)
	if err != nil || created.After(time.Now()) || created.Unix() <= 0 {
		return 0, errors.New("invalid authenticated dispatch timestamp")
	}
	return created.Unix() / 900, nil
}
