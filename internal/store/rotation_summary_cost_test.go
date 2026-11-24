package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// countingRotationQueries counts the reads a rotation read makes. Every method the
// reads do not name is the embedded nil interface: calling it fails the test.
type countingRotationQueries struct {
	rotationStoreQueries
	calls map[string]int
}

func (c *countingRotationQueries) get(_ context.Context, _ domain.Scope, id string) (RotationRecord, error) {
	c.calls["get"]++
	return RotationRecord{ID: id, KeyID: "key_" + id}, nil
}

func (c *countingRotationQueries) list(_ context.Context, _ domain.Scope, q RotationListQuery) ([]RotationRecord, error) {
	c.calls["list"]++
	rows := make([]RotationRecord, q.Limit)
	for i := range rows {
		rows[i] = RotationRecord{ID: fmt.Sprintf("rot_%03d", i), KeyID: fmt.Sprintf("key_%03d", i)}
	}
	return rows, nil
}

func (c *countingRotationQueries) credentialExpiry(context.Context, domain.Scope, string) (string, error) {
	c.calls["credentialExpiry"]++
	return "", nil
}

func (c *countingRotationQueries) credentialExpiries(context.Context, domain.Scope, []string) (map[string]string, error) {
	c.calls["credentialExpiries"]++
	return map[string]string{}, nil
}

func (c *countingRotationQueries) lastRuns(_ context.Context, _ domain.Scope, ids []string) (map[string]RotationLastRun, error) {
	c.calls["lastRuns"]++
	return map[string]RotationLastRun{ids[0]: {ID: "run_x", Phase: "done"}}, nil
}

func (c *countingRotationQueries) owedDropCounts(_ context.Context, _ domain.Scope, ids []string) (map[string][2]int64, error) {
	c.calls["owedDropCounts"]++
	return map[string][2]int64{ids[0]: {2, 1}}, nil
}

// The run summary costs two reads for a page however long the page is (never one per
// rotation), and the reads the executor's gates, the existence checks and a mutation
// make (Get, a page of List) take no part in it: they carry no summary at all.
func TestRotationSummaryCostsTwoReadsPerPageAndGetListPayNothing(t *testing.T) {
	ctx := context.Background()
	chain := domain.Scope{Org: "org_a", Project: "prj_a", Env: "env_a"}
	for _, n := range []int{1, 7, 100} {
		counting := &countingRotationQueries{calls: map[string]int{}}
		r := rotationQueries{queries: counting}

		page, err := r.page(ctx, chain, RotationListQuery{Limit: n})
		if err != nil || len(page) != n {
			t.Fatalf("page of %d: %d rows, %v", n, len(page), err)
		}
		if _, err := r.get(ctx, chain, "rot_001"); err != nil {
			t.Fatal(err)
		}
		for _, row := range page {
			if row.LastRun != nil || row.OwedDropRuns != 0 || row.OwedDropRunsVersionsChanged != 0 {
				t.Fatalf("a listed rotation carries a summary before Summarize asked: %+v", row)
			}
		}
		if counting.calls["lastRuns"] != 0 || counting.calls["owedDropCounts"] != 0 {
			t.Fatalf("get and page read the run summary: %v", counting.calls)
		}

		if err := r.summarize(ctx, chain, page); err != nil {
			t.Fatal(err)
		}
		if counting.calls["lastRuns"] != 1 || counting.calls["owedDropCounts"] != 1 {
			t.Fatalf("a page of %d rotations cost %d last-run and %d owed-count reads, want one each: %v", n, counting.calls["lastRuns"], counting.calls["owedDropCounts"], counting.calls)
		}
		if page[0].LastRun == nil || page[0].OwedDropRuns != 2 || page[0].OwedDropRunsVersionsChanged != 1 {
			t.Fatalf("first row not summarized: %+v", page[0])
		}
		if n > 1 && (page[1].LastRun != nil || page[1].OwedDropRuns != 0) {
			t.Fatalf("second row has a summary it was not given: %+v", page[1])
		}
	}
	empty := &countingRotationQueries{calls: map[string]int{}}
	if err := (rotationQueries{queries: empty}).summarize(ctx, chain, nil); err != nil || len(empty.calls) != 0 {
		t.Fatalf("an empty page read %v, %v", empty.calls, err)
	}
}
