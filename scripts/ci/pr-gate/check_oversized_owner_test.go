package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
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
	skew int64
	// mu guards reads: the controller reads repository roles concurrently.
	mu    sync.Mutex
	reads []string
	// listed, when set, replaces what a holding suite lists.
	listed map[int64][]check
	// foreign is what a listing by commit shows of other apps, whose suites the
	// per-suite reads never open.
	foreign []check
	// grown, when set, becomes the suite total once a listing by commit is read,
	// as if suites were added during that read.
	grown int
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
	a.mu.Lock()
	a.reads = append(a.reads, path)
	a.mu.Unlock()
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
		a.mu.Lock()
		response := a.suiteResponse(nil, false, "")
		a.mu.Unlock()
		return queueHistoryJSON(response, result)
	case strings.Contains(path, "/commits/"+head+"/check-runs?"):
		a.mu.Lock()
		if a.grown > 0 {
			a.suites = a.grown
		}
		a.mu.Unlock()
		// REST lists the commit's newest 1000 suites only, which exclude the holders.
		if a.foreign != nil {
			return queueHistoryJSON(map[string]any{"total_count": len(a.foreign), "check_runs": a.foreign}, result)
		}
		return queueHistoryJSON(empty, result)
	case suiteListing.MatchString(path):
		id, _ := strconv.ParseInt(suiteListing.FindStringSubmatch(path)[1], 10, 64)
		if !slices.Contains(a.holding, id) {
			return queueHistoryJSON(empty, result)
		}
		if runs, ok := a.listed[id]; ok {
			return queueHistoryJSON(map[string]any{"total_count": len(runs), "check_runs": runs}, result)
		}
		return a.api.read(ctx, suiteListing.ReplaceAllString(path, "/commits/"+head+"/check-runs?"), result)
	}
	return a.api.read(ctx, path, result)
}

func (a *oversizedHead) readsOf(substring string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
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

// Suites added while the commit listing is read can push an older suite out of
// REST's newest 1000, so both owners of the queue verdict count again after the
// read and refuse.
func TestOwnersRefuseACommitListingWhenSuitesGrowDuringTheRead(t *testing.T) {
	for _, owner := range []string{"controller", "queue verdict"} {
		t.Run(owner, func(t *testing.T) {
			f, _ := retirementFixture(t, queueCheckName)
			head := installOversizedHead(f, checksuites.Margin)
			head.grown = checksuites.Margin + 1
			var err error
			if owner == "controller" {
				_, err = f.c.listOwnedChecks(context.Background(), f.c.expectedHead)
			} else {
				_, err = f.q.groupChecks(context.Background(), f.pr)
			}
			if err == nil || !strings.Contains(err.Error(), "grew past") {
				t.Fatalf("error = %v, want a refusal after the recount", err)
			}
		})
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
		// Past the margin one listing by commit remains, to see other apps' records.
		{suites: checksuites.Margin + 1, byCommit: 1},
		{suites: 2005, holding: []int64{1856}, byCommit: 1, bySuite: 1},
		// The same record from two suites cannot be told apart from a replayed read.
		{suites: 2005, holding: []int64{7, 1856}, byCommit: 1, bySuite: 2, wantErr: true},
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

// The queue verdict reads at most 100 members across every suite, and one run
// listed by two suites is refused, as a listing by commit would have.
func TestGroupChecksBoundsAndDeduplicatesAcrossSuites(t *testing.T) {
	runs := func(f *queueFixture, first int64, n int) []check {
		out := make([]check, n)
		for i := range out {
			out[i] = f.api.checks[0]
			out[i].ID = first + int64(i)
		}
		return out
	}
	for _, tc := range []struct {
		name    string
		a, b    int
		overlap bool
		wantErr bool
	}{
		{name: "at the cap", a: 50, b: 50},
		{name: "over the cap across suites", a: 60, b: 60, wantErr: true},
		{name: "same run in two suites", a: 1, b: 1, overlap: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := retirementFixture(t, queueCheckName)
			head := installOversizedHead(f, 2005, 7, 1856)
			second := int64(2000)
			if tc.overlap {
				second = 1000
			}
			head.listed = map[int64][]check{7: runs(f, 1000, tc.a), 1856: runs(f, second, tc.b)}
			group, err := f.q.groupChecks(context.Background(), f.pr)
			if (err != nil) != tc.wantErr || (err == nil && len(group) != tc.a+tc.b) {
				t.Fatalf("group = %d checks, err=%v", len(group), err)
			}
		})
	}
}

// Past the margin the owners open only github-actions suites, so a same-name
// record of another app is seen through the listing by commit and refused, as
// that listing alone did before.
func TestOwnedChecksOfHeadBeyondRESTSuiteCapRefuseForeignAppRecords(t *testing.T) {
	foreignOf := func(f *queueFixture, external string, appID int64, slug string) []check {
		forged := f.api.checks[0]
		forged.ID += 1000
		forged.ExternalID = external
		forged.App.ID, forged.App.Slug = appID, slug
		return []check{forged}
	}
	for _, tc := range []struct {
		name    string
		prefix  bool
		owners  string
		appID   int64
		slug    string
		wantErr bool
	}{
		{name: "foreign record with a controller identity, owner listing", prefix: true, owners: "owned", wantErr: true},
		{name: "foreign record with a controller identity, queue verdict", prefix: true, owners: "group", wantErr: true},
		{name: "foreign record with any identity, queue verdict", owners: "group", wantErr: true},
		{name: "another app id under the actions slug, queue verdict", owners: "group", appID: 99, slug: "github-actions", wantErr: true},
		{name: "the actions app id under another slug, queue verdict", owners: "group", appID: actionsAppID, slug: "other-app", wantErr: true},
		// A listing by commit refused these as ambiguous same-name Actions checks.
		{name: "another app id under the actions slug, owner listing", owners: "owned", appID: 99, slug: "github-actions", wantErr: true},
		{name: "the actions app id under another slug, owner listing", owners: "owned", appID: actionsAppID, slug: "other-app", wantErr: true},
		// Unchanged from a listing by commit: owners ignore it unless it claims to be theirs.
		{name: "foreign record with another identity, owner listing", owners: "owned"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := retirementFixture(t, queueCheckName)
			head := installOversizedHead(f, 2005, 1856)
			external := "unrelated"
			if tc.prefix {
				external = f.api.checks[0].ExternalID
			}
			appID, slug := tc.appID, tc.slug
			if slug == "" {
				appID, slug = 99, "other-app"
			}
			head.foreign = foreignOf(f, external, appID, slug)
			var err error
			if tc.owners == "owned" {
				_, err = f.c.listOwnedChecks(context.Background(), f.c.expectedHead)
			} else {
				_, err = f.q.groupChecks(context.Background(), f.pr)
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want refusal=%v", err, tc.wantErr)
			}
		})
	}
}

// The suite inventory comes from GraphQL, which can lag a run REST already lists.
// The listing by commit shows runs of the newest suites, so a github-actions
// record it shows that the suites did not is refused instead of taken for
// absent, with or without the controller prefix.
func TestOwnedChecksRefuseRecordTheSuiteInventoryHasNotIndexed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		holding []int64
		dup     bool
		bare    bool
		wantErr bool
	}{
		{name: "indexed in a holding suite", holding: []int64{1856}},
		{name: "not indexed, no controller prefix", bare: true, wantErr: true},
		{name: "not indexed yet", wantErr: true},
		{name: "commit listing repeats a run", holding: []int64{1856}, dup: true, wantErr: true},
	} {
		for _, owners := range []string{"owned", "group"} {
			t.Run(tc.name+", "+owners, func(t *testing.T) {
				f, _ := retirementFixture(t, queueCheckName)
				head := installOversizedHead(f, 2005, tc.holding...)
				head.foreign = []check{f.api.checks[0]}
				if tc.bare {
					head.foreign[0].ExternalID = "someone-else"
				}
				if tc.dup {
					head.foreign = append(head.foreign, f.api.checks[0])
				}
				var err error
				if owners == "owned" {
					_, err = f.c.listOwnedChecks(context.Background(), f.c.expectedHead)
				} else {
					_, err = f.q.groupChecks(context.Background(), f.pr)
				}
				if (err != nil) != tc.wantErr {
					t.Fatalf("err = %v, want refusal=%v", err, tc.wantErr)
				}
			})
		}
	}
}
