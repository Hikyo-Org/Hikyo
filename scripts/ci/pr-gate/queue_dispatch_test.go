package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type dispatchAPI struct {
	*fakeAPI
	batches    [][]headWork
	failAt     int
	badReceipt bool
}

func (a *dispatchAPI) write(ctx context.Context, method, path string, payload, out any) error {
	if method != "POST" || path != "repos/Hikyo-Org/Hikyo/actions/workflows/ci-queue-worker.yml/dispatches" {
		return errors.New("discovery tried an unexpected mutation")
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 5*time.Second {
		return errors.New("dispatch is not individually bounded")
	}
	data, _ := json.Marshal(payload)
	var p struct {
		Ref    string            `json:"ref"`
		Inputs map[string]string `json:"inputs"`
	}
	if err := decode(data, &p); err != nil {
		return err
	}
	if p.Ref != "main" || len(p.Inputs) != 1 {
		return errors.New("dispatch reference or inputs escaped the trusted contract")
	}
	batch, err := parseHeadBatch(p.Inputs["work"])
	if err != nil {
		return err
	}
	a.batches = append(a.batches, batch)
	if len(a.batches) == a.failAt {
		return errors.New("lost dispatch response")
	}
	id := int64(5000 + len(a.batches))
	r := workerReceipt{ID: id, URL: fmt.Sprintf("https://api.github.com/repos/Hikyo-Org/Hikyo/actions/runs/%d", id), HTML: fmt.Sprintf("https://github.com/Hikyo-Org/Hikyo/actions/runs/%d", id)}
	if a.badReceipt {
		r.URL = "https://api.github.com/repos/other/repo/actions/runs/1"
	}
	data, _ = json.Marshal(r)
	return decode(data, out)
}

func dispatchFixture(t *testing.T, prCount, groupCount int) (*queueFixture, *dispatchAPI, event) {
	t.Helper()
	f := newQueueFixture(t)
	f.c.expectedHead = ""
	a := &dispatchAPI{fakeAPI: f.api}
	f.c.api = a
	pulls := make([]pull, prCount)
	for i := range pulls {
		p := f.pr
		p.Number = i + 1
		p.Head.SHA = fmt.Sprintf("%040x", i+1)
		pulls[i] = p
	}
	for page := 1; ; page++ {
		lo := (page - 1) * 100
		hi := min(lo+100, len(pulls))
		f.api.responses[f.c.endpoint(fmt.Sprintf("pulls?state=open&per_page=100&page=%d", page))] = pulls[lo:hi]
		if hi-lo < 100 {
			break
		}
	}
	entries := make([]queueEntry, groupCount)
	for i := range entries {
		e := f.entry
		e.ID = fmt.Sprintf("ENTRY%d", i+1)
		e.Position = i + 1
		e.PR.Number = 3000 + i
		e.PR.Head = fmt.Sprintf("%040x", 6000+i)
		e.Head.OID = fmt.Sprintf("%040x", 3000+i)
		entries[i] = e
	}
	for page := 1; ; page++ {
		lo := (page - 1) * 100
		hi := min(lo+100, len(entries))
		more := hi < len(entries)
		query := f.query
		if page > 1 {
			query = strings.Replace(query, "entries(first:100)", fmt.Sprintf(`entries(first:100,after:"cursor%d")`, lo), 1)
		}
		f.api.responses[query] = queuePageResponse("QUEUE_MAIN", entries[lo:hi], more, fmt.Sprintf("cursor%d", hi))
		if !more {
			break
		}
	}
	t.Setenv("GITHUB_RUN_ID", "555")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	f.api.responses[f.c.endpoint("actions/workflows/ci-queue-gate.yml")] = workflow{ID: 30, Path: ".github/workflows/ci-queue-gate.yml", Name: "queue-gate"}
	f.api.responses[f.c.endpoint("actions/runs/555")] = run{ID: 555, Attempt: 1, WorkflowID: 30, Path: ".github/workflows/ci-queue-gate.yml", Name: "queue-gate", Event: "schedule", HeadSHA: f.pr.Base.SHA, HeadBranch: "main", Repository: f.event.Repository, HeadRepository: f.event.Repository, CreatedAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)}
	e := f.event
	e.Ref = "refs/heads/main"
	return f, a, e
}

func TestCompleteDiscoveryFansOutBeyondMatrixLimitWithoutLoss(t *testing.T) {
	for _, size := range []int{101, 257, 2000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			f, a, e := dispatchFixture(t, size, size)
			if err := f.q.dispatchWorkers(context.Background(), "schedule", e); err != nil {
				t.Fatal(err)
			}
			seen := make(map[string]bool)
			for _, batch := range a.batches {
				if len(batch) > 64 {
					t.Fatal("undocumented matrix-size bypass")
				}
				for _, w := range batch {
					if seen[w.Head] {
						t.Fatal("head dispatched twice")
					}
					seen[w.Head] = true
				}
			}
			if len(seen) != size*2 || len(a.batches) != (size*2+63)/64 {
				t.Fatalf("lost work: heads=%d batches=%d", len(seen), len(a.batches))
			}
			if len(f.api.writes) != 0 {
				t.Fatal("read-only discovery published a check")
			}
		})
	}
}

func TestIncompleteDiscoveryRefusesBeforeAnyDispatch(t *testing.T) {
	for _, kind := range []string{"PR over limit", "duplicate PR", "incomplete queue", "queue API failure", "malformed queue state"} {
		t.Run(kind, func(t *testing.T) {
			f, a, e := dispatchFixture(t, 101, 101)
			switch kind {
			case "PR over limit":
				f, a, e = dispatchFixture(t, 2001, 0)
			case "duplicate PR":
				f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=2")] = []pull{f.pr}
			case "incomplete queue":
				f.api.responses[f.query] = queuePageResponse("QUEUE_MAIN", []queueEntry{f.entry}, true, nil)
			case "queue API failure":
				f.api.responses[f.query] = errors.New("queue unavailable")
			case "malformed queue state":
				f.entry.State = "UNKNOWN"
				f.api.responses[f.query] = queuePageResponse("QUEUE_MAIN", []queueEntry{f.entry}, false, nil)
			}
			if err := f.q.dispatchWorkers(context.Background(), "schedule", e); err == nil || len(a.batches) != 0 {
				t.Fatal("incomplete metadata caused partial dispatch")
			}
		})
	}
}

func TestDispatchFailureDoesNotDropLaterBatchesOrRetryLostMutation(t *testing.T) {
	f, a, e := dispatchFixture(t, 257, 0)
	a.failAt = 2
	err := f.q.dispatchWorkers(context.Background(), "schedule", e)
	if err == nil || !strings.Contains(err.Error(), "not acknowledged") {
		t.Fatalf("partial dispatch not explicit: %v", err)
	}
	if len(a.batches) != 5 {
		t.Fatalf("prefix failure dropped later heads: %d batches", len(a.batches))
	}
	seen := make(map[string]bool)
	for _, batch := range a.batches {
		for _, w := range batch {
			if seen[w.Head] {
				t.Fatal("ambiguous mutation retried")
			}
			seen[w.Head] = true
		}
	}
	if len(seen) != 257 {
		t.Fatal("not every head was attempted exactly once")
	}
	f, a, e = dispatchFixture(t, 101, 0)
	a.badReceipt = true
	if err := f.q.dispatchWorkers(context.Background(), "schedule", e); err == nil || len(a.batches) != 2 {
		t.Fatal("invalid receipt was accepted or truncated discovery")
	}
}

func TestDispatchRotationUsesAuthenticatedServerSlot(t *testing.T) {
	f, a, e := dispatchFixture(t, 257, 0)
	r := f.api.responses[f.c.endpoint("actions/runs/555")].(run)
	slot := time.Now().Unix()/900 - 2
	for i := int64(0); i < 2; i++ {
		r.CreatedAt = time.Unix((slot+i)*900, 0).UTC().Format(time.RFC3339)
		f.api.responses[f.c.endpoint("actions/runs/555")] = r
		a.batches = nil
		if err := f.q.dispatchWorkers(context.Background(), "schedule", e); err != nil {
			t.Fatal(err)
		}
		first := ((slot+i)%5)*64 + 1
		if a.batches[0][0].Head != fmt.Sprintf("%040x", first) {
			t.Fatal("scheduled repair restarted the same sorted prefix")
		}
	}
	r.Attempt = 2
	f.api.responses[f.c.endpoint("actions/runs/555")] = r
	a.batches = nil
	if err := f.q.dispatchWorkers(context.Background(), "schedule", e); err == nil || len(a.batches) != 0 {
		t.Fatal("wrong current run attempt dispatched")
	}
}

func TestWorkerInputAndHTTPReceiptRejectMalformedContracts(t *testing.T) {
	head := strings.Repeat("a", 40)
	valid := fmt.Sprintf(`[{"head":%q,"kind":"admission"}]`, head)
	if _, err := parseHeadBatch(valid); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"null", "[]", valid + valid, fmt.Sprintf(`[{"head":%q,"kind":"group","command":"evil"}]`, head), fmt.Sprintf(`[{"head":%q,"kind":"evil"}]`, head), fmt.Sprintf(`[{"head":%q,"kind":"group"},{"head":%q,"kind":"admission"}]`, head, head)} {
		if _, err := parseHeadBatch(raw); err == nil {
			t.Fatalf("malformed/ambiguous worker inputs accepted: %s", raw)
		}
	}
	for _, prefix := range []string{"HTTP/2.0 200 OK\r\nheader: value\r\n\r\n", "HTTP/1.1 200 OK\n\n"} {
		body, err := dispatchResponseBody([]byte(prefix + `{"workflow_run_id":1}`))
		if err != nil || string(body) != `{"workflow_run_id":1}` {
			t.Fatal("documented receipt refused")
		}
	}
	for _, raw := range []string{"HTTP/2.0 204 No Content\n\n", "HTTP/2.0 201 Created\n\n{}", "HTTP/wrong 200 OK\n\n{}", "{}", "HTTP/2.0 200 OK\n"} {
		if _, err := dispatchResponseBody([]byte(raw)); err == nil {
			t.Fatal("unknown receipt protocol accepted")
		}
	}
}

func TestDispatchProvenanceRefusesBeforeMutation(t *testing.T) {
	for _, name := range []string{"workflow path", "workflow id", "run id", "event", "branch", "repository", "future time", "malformed time"} {
		t.Run(name, func(t *testing.T) {
			f, a, e := dispatchFixture(t, 1, 0)
			r := f.api.responses[f.c.endpoint("actions/runs/555")].(run)
			switch name {
			case "workflow path":
				r.Path = ".github/workflows/untrusted.yml"
			case "workflow id":
				r.WorkflowID = 99
			case "run id":
				r.ID = 556
			case "event":
				r.Event = "pull_request"
			case "branch":
				r.HeadBranch = "feature"
			case "repository":
				r.HeadRepository.ID = 99
			case "future time":
				r.CreatedAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
			case "malformed time":
				r.CreatedAt = "invalid"
			}
			f.api.responses[f.c.endpoint("actions/runs/555")] = r
			if err := f.q.dispatchWorkers(context.Background(), "schedule", e); err == nil || len(a.batches) != 0 {
				t.Fatal("unauthenticated producer metadata dispatched")
			}
		})
	}
}

func TestManualDispatchPrioritizesTargetWithoutDroppingGroups(t *testing.T) {
	f, a, e := dispatchFixture(t, 0, 257)
	p := f.pr
	p.Number = 41
	p.Head.SHA = fmt.Sprintf("%040x", 41)
	f.api.responses[f.c.endpoint("pulls/41")] = p
	e.Inputs.PR = "41"
	r := f.api.responses[f.c.endpoint("actions/runs/555")].(run)
	r.Event = "workflow_dispatch"
	slot := time.Now().Unix()/900 - 2
	for slot%5 == 0 {
		slot--
	}
	r.CreatedAt = time.Unix(slot*900, 0).UTC().Format(time.RFC3339)
	f.api.responses[f.c.endpoint("actions/runs/555")] = r
	if err := f.q.dispatchWorkers(context.Background(), "workflow_dispatch", e); err != nil {
		t.Fatal(err)
	}
	if a.batches[0][0].Head != p.Head.SHA {
		t.Fatal("manual target waited behind group batches")
	}
	count := 0
	for _, batch := range a.batches {
		count += len(batch)
	}
	if count != 258 {
		t.Fatal("manual target priority dropped group repair")
	}
}

func TestGHDispatchTransportPinsVersionAndRequires200Receipt(t *testing.T) {
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "payload.json")
	responsePath := filepath.Join(dir, "response")
	shim := `#!/bin/sh
set -eu
test "$#" = 9
test "$1" = api
test "$2" = --method
test "$3" = POST
test "$4" = repos/Hikyo-Org/Hikyo/actions/workflows/ci-queue-worker.yml/dispatches
test "$5" = --input
test "$6" = -
test "$7" = -H
test "$8" = 'X-GitHub-Api-Version: 2026-03-10'
test "$9" = --include
cat > "$CAPTURED_PAYLOAD"
cat "$FAKE_GH_RESPONSE"
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(shim), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CAPTURED_PAYLOAD", payloadPath)
	t.Setenv("FAKE_GH_RESPONSE", responsePath)
	payload := map[string]any{"ref": "main", "inputs": map[string]string{"work": fmt.Sprintf(`[{"head":%q,"kind":"admission"}]`, strings.Repeat("a", 40))}}
	for _, raw := range []string{"HTTP/2.0 200 OK\r\ncontent-type: application/json\r\n\r\n" + `{"workflow_run_id":1,"run_url":"api","html_url":"html"}`, "HTTP/2.0 204 No Content\r\n\r\n", "HTTP/2.0 200 OK\r\n\r\n{}{}"} {
		if err := os.WriteFile(responsePath, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		var receipt workerReceipt
		err := (ghAPI{}).write(context.Background(), "POST", "repos/Hikyo-Org/Hikyo/actions/workflows/ci-queue-worker.yml/dispatches", payload, &receipt)
		valid := strings.Contains(raw, "workflow_run_id")
		if valid && (err != nil || receipt.ID != 1) || !valid && err == nil {
			t.Fatalf("unexpected receipt acceptance: %v", err)
		}
		data, readErr := os.ReadFile(payloadPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var sent struct {
			Ref    string            `json:"ref"`
			Inputs map[string]string `json:"inputs"`
		}
		if err := decode(data, &sent); err != nil || sent.Ref != "main" {
			t.Fatal("payload escaped structured arguments")
		}
	}
}
