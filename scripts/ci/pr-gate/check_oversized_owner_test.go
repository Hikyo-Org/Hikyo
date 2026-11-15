package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/scripts/ci/checksuites"
)

// oversizedHead models a head beyond REST's cap of 1000 check suites: listing
// runs by commit shows none of the context's records, and only the suites that
// hold them list them.
type oversizedHead struct {
	api
	f       *queueFixture
	suites  int
	holding []int64
	// countErr and scanErr fail the suite count and the suite scan.
	countErr, scanErr error
	// skew answers suite queries for another repository.
	skew  int64
	reads []string
}

var (
	suiteCursor  = regexp.MustCompile(`after:"p(\d+)"`)
	suiteListing = regexp.MustCompile(`/check-suites/(\d+)/check-runs\?`)
)

func installOversizedHead(f *queueFixture, suites int, holding ...int64) *oversizedHead {
	a := &oversizedHead{api: f.c.api, f: f, suites: suites, holding: holding}
	f.c.api = a
	return a
}

func (a *oversizedHead) suiteResponse(nodes []map[string]any, more bool, cursor string) map[string]any {
	object := map[string]any{"oid": a.f.pr.Head.SHA, "checkSuites": map[string]any{"totalCount": a.suites}}
	if nodes != nil {
		object["every"] = map[string]any{"totalCount": a.suites}
		object["checkSuites"] = map[string]any{"totalCount": a.suites, "nodes": nodes, "pageInfo": map[string]any{"hasNextPage": more, "endCursor": cursor}}
	}
	return map[string]any{"data": map[string]any{"repository": map[string]any{"databaseId": a.f.c.repo.ID + a.skew, "object": object}}}
}

func (a *oversizedHead) read(ctx context.Context, path string, result any) error {
	a.reads = append(a.reads, path)
	head := a.f.pr.Head.SHA
	empty := map[string]any{"total_count": 0, "check_runs": []check{}}
	switch {
	case strings.HasPrefix(path, "graphql:") && strings.Contains(path, "checkSuites("):
		if a.scanErr != nil {
			return a.scanErr
		}
		if !strings.Contains(path, fmt.Sprintf("checkName:%q", a.f.c.checkName)) {
			return fmt.Errorf("suite scan does not select by the context's name: %s", path)
		}
		page := 0
		if m := suiteCursor.FindStringSubmatch(path); m != nil {
			page, _ = strconv.Atoi(m[1])
		}
		nodes := []map[string]any{}
		for id := int64(page*100 + 1); id <= int64(min(page*100+100, a.suites)); id++ {
			named := 0
			if slices.Contains(a.holding, id) {
				named = 1
			}
			nodes = append(nodes, map[string]any{"databaseId": id, "named": map[string]any{"totalCount": named}})
		}
		return queueHistoryJSON(a.suiteResponse(nodes, (page+1)*100 < a.suites, fmt.Sprintf("p%d", page+1)), result)
	case strings.HasPrefix(path, "graphql:") && strings.Contains(path, "checkSuites{totalCount}") && !strings.Contains(path, "checkSuites("):
		if a.countErr != nil {
			return a.countErr
		}
		return queueHistoryJSON(a.suiteResponse(nil, false, ""), result)
	case strings.Contains(path, "/commits/"+head+"/check-runs?"):
		// REST lists the commit's newest 1000 suites only, which exclude the holders.
		return queueHistoryJSON(empty, result)
	case suiteListing.MatchString(path):
		id, _ := strconv.ParseInt(suiteListing.FindStringSubmatch(path)[1], 10, 64)
		if !slices.Contains(a.holding, id) {
			return queueHistoryJSON(empty, result)
		}
		return a.api.read(ctx, suiteListing.ReplaceAllString(path, "/commits/"+head+"/check-runs?"), result)
	}
	return a.api.read(ctx, path, result)
}

func (a *oversizedHead) readsOf(substring string) int {
	n := 0
	for _, path := range a.reads {
		if strings.Contains(path, substring) {
			n++
		}
	}
	return n
}

// Targeted retirement on a head with more than 1000 suites used to cancel nothing
// yet report success, because REST's listing by commit never showed the record.
func TestRetirementCancelsOwnedChecksOnHeadBeyondRESTSuiteCap(t *testing.T) {
	for _, name := range []string{queueCheckName, "ci-required-next"} {
		t.Run(name, func(t *testing.T) {
			f, _ := retirementFixture(t, name)
			head := installOversizedHead(f, 2005, 1856)
			if err := f.c.retireChecks(context.Background(), f.c.expectedHead); err != nil {
				t.Fatal(err)
			}
			if len(f.api.writes) != 1 || f.api.checks[0].Status != "completed" || f.api.checks[0].Conclusion != "cancelled" {
				t.Fatalf("hidden unfinished record survived retirement: writes=%d checks=%+v", len(f.api.writes), f.api.checks)
			}
			if head.readsOf("/check-suites/1856/check-runs?") == 0 || head.readsOf("/check-suites/") != head.readsOf("/check-suites/1856/") {
				t.Fatalf("reads %v, want only the holding suite listed", head.reads)
			}
		})
	}
}

// Both owners of the queue verdict list through the same seam, so a head beyond
// the cap shows its records to publication, not only to retirement.
func TestQueueVerdictOwnersSeeChecksOnHeadBeyondRESTSuiteCap(t *testing.T) {
	f, _ := retirementFixture(t, queueCheckName)
	installOversizedHead(f, 1147, 1100)
	own, err := f.c.listOwnedChecks(context.Background(), f.c.expectedHead)
	if err != nil || len(own) != 1 || own[0].ID != f.api.checks[0].ID {
		t.Fatalf("owned checks = %+v/%v, want the hidden record", own, err)
	}
	group, err := f.q.groupChecks(context.Background(), f.pr)
	if err != nil || len(group) != 1 || group[0].ID != f.api.checks[0].ID {
		t.Fatalf("group checks = %+v/%v, want the hidden record", group, err)
	}
}

// A commit within the margin keeps its one listing by commit; past it, only the
// suites holding the context are listed.
func TestOwnedChecksListBySuiteOnlyBeyondSuiteMargin(t *testing.T) {
	for _, tc := range []struct {
		suites            int
		holding           []int64
		byCommit, bySuite int
		wantErr           bool
	}{
		{suites: 1, byCommit: 1},
		{suites: checksuites.Margin, byCommit: 1},
		{suites: checksuites.Margin + 1},
		{suites: 2005, holding: []int64{1856}, bySuite: 1},
		// The same record from two suites cannot be told apart from a replayed read.
		{suites: 2005, holding: []int64{7, 1856}, bySuite: 2, wantErr: true},
	} {
		t.Run(fmt.Sprintf("%d suites holding %v", tc.suites, tc.holding), func(t *testing.T) {
			f, _ := retirementFixture(t, queueCheckName)
			head := installOversizedHead(f, tc.suites, tc.holding...)
			_, err := f.c.listOwnedChecks(context.Background(), f.c.expectedHead)
			if (err != nil) != tc.wantErr || head.readsOf("/commits/") != tc.byCommit || head.readsOf("/check-suites/") != tc.bySuite {
				t.Fatalf("err=%v reads=%v", err, head.reads)
			}
		})
	}
}

// Anything short of a complete suite inventory refuses, never lists partially.
func TestOwnedChecksOfHeadBeyondRESTSuiteCapFailClosed(t *testing.T) {
	for _, fault := range []string{"count unavailable", "scan unavailable", "foreign repository"} {
		t.Run(fault, func(t *testing.T) {
			f, _ := retirementFixture(t, queueCheckName)
			head := installOversizedHead(f, 2005, 1856)
			switch fault {
			case "count unavailable":
				head.countErr = errors.New("count unavailable")
			case "scan unavailable":
				head.scanErr = errors.New("scan unavailable")
			case "foreign repository":
				head.skew = 1
			}
			if err := f.c.retireChecks(context.Background(), f.c.expectedHead); err == nil || len(f.api.writes) != 0 {
				t.Fatalf("%s: err=%v writes=%d, want a refusal without mutation", fault, err, len(f.api.writes))
			}
		})
	}
}
