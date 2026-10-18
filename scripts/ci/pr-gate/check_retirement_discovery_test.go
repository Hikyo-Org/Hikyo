package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type retirementDiscoveryAPI struct{ *fakeAPI }

func (a retirementDiscoveryAPI) read(ctx context.Context, path string, result any) error {
	if strings.HasPrefix(path, "graphql:") && strings.Contains(path, "checkSuite") && strings.Contains(path, "app") {
		return errors.New("organization IP allow list blocks foreign checkSuite.app traversal")
	}
	if strings.Contains(path, "/check-runs?") && !strings.Contains(path, "check_name=") {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.reads[path]++
		response, ok := a.responses[path]
		if !ok {
			return fmt.Errorf("unexpected retirement check read %s", path)
		}
		if err, ok := response.(error); ok {
			return err
		}
		data, err := json.Marshal(response)
		if err != nil {
			return err
		}
		return decode(data, result)
	}
	return a.fakeAPI.read(ctx, path, result)
}

func retirementDiscoveryFixture(t *testing.T) (*queueFixture, string) {
	t.Helper()
	f := newQueueFixture(t)
	f.c.expectedHead = ""
	f.c.api = retirementDiscoveryAPI{f.api}
	t.Setenv("GITHUB_REF", "refs/heads/main")
	return f, f.pr.Base.SHA
}

func TestRetirementDiscoveryFindsOwnedChecksWhenForeignAppGraphQLIsBlocked(t *testing.T) {
	f, head := retirementDiscoveryFixture(t)
	owned := retirementHistoryRecord(f, head, queueCheckName, "QUEUED")
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, []map[string]any{retirementHistoryNode(f, head, []map[string]any{owned})}, false, "")
	f.api.responses[f.c.endpoint(fmt.Sprintf("commits/%s/check-runs?filter=latest&per_page=100&page=1", head))] = map[string]any{
		"total_count": 2,
		"check_runs": []map[string]any{
			{"id": 10, "name": "foreign native check", "head_sha": head, "status": "queued", "app": map[string]any{"id": 42, "slug": "other"}},
			{"id": 11, "name": queueCheckName, "head_sha": head, "status": "queued", "external_id": owned["external_id"], "app": map[string]any{"id": actionsAppID, "slug": "github-actions"}},
		},
	}
	got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
	if err != nil || !reflect.DeepEqual(got.Queue, []string{head}) {
		t.Fatalf("foreign app visibility prevented owned retirement discovery: %+v/%v", got, err)
	}
	if len(f.api.writes) != 0 {
		t.Fatal("discovery mutated checks")
	}
}
func retirementHistoryRecord(f *queueFixture, head, name, status string) map[string]any {
	p := f.pr
	p.Head.SHA = head
	f.api.nextID++
	return map[string]any{"id": f.api.nextID, "head_sha": head, "name": name, "status": strings.ToLower(status), "external_id": newBinding(f.c.repo.ID, p, nil).externalID(), "app": map[string]any{"id": actionsAppID, "slug": "github-actions"}}
}
func retirementChecksPath(f *queueFixture, head string, page int) string {
	return f.c.endpoint(fmt.Sprintf("commits/%s/check-runs?filter=latest&per_page=100&page=%d", head, page))
}
func retirementHistoryNode(f *queueFixture, head string, checks []map[string]any) map[string]any {
	f.api.responses[retirementChecksPath(f, head, 1)] = map[string]any{"total_count": len(checks), "check_runs": checks}
	return map[string]any{"oid": head, "statusCheckRollup": map[string]any{"state": "PENDING"}}
}
func retirementHistoryResponse(f *queueFixture, head string, nodes []map[string]any, more bool, cursor string) map[string]any {
	return map[string]any{"data": map[string]any{"repository": map[string]any{"databaseId": f.c.repo.ID, "object": map[string]any{"oid": head, "history": map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": more, "endCursor": cursor}}}}}}
}
func TestRetirementDiscoveryFindsMissedCallbacksAcrossMainHistory(t *testing.T) {
	f, head := retirementDiscoveryFixture(t)
	old := strings.Repeat("c", 40)
	first := []map[string]any{
		retirementHistoryRecord(f, head, queueCheckName, "COMPLETED"),
		retirementHistoryRecord(f, head, queueCheckName, "QUEUED"),
		retirementHistoryRecord(f, head, "ci-required-next", "IN_PROGRESS"),
	}
	// Native and foreign-app checks never turn into cancellation targets.
	foreign := retirementHistoryRecord(f, old, queueCheckName, "QUEUED")
	foreign["app"] = map[string]any{"id": 42, "slug": "other"}
	second := []map[string]any{foreign, retirementHistoryRecord(f, old, "ordinary job", "QUEUED"), retirementHistoryRecord(f, old, "ci-required-next", "QUEUED")}
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, []map[string]any{retirementHistoryNode(f, head, first)}, true, "next")
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "next", 100)] = retirementHistoryResponse(f, head, []map[string]any{retirementHistoryNode(f, old, second), {"oid": strings.Repeat("d", 40), "statusCheckRollup": nil}}, false, "")
	got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
	want := retirementTargets{HistoryScope: retirementHistoryScope{Snapshot: head, MaxCommits: retirementHistoryWindow, ScannedCommits: 3, CompleteAncestry: true}, Queue: []string{head}, Observation: []string{head, old}, ObservationBatches: [][]string{{head, old}}, ReporterTargets: []reporterRetirementTarget{}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("discovery got %+v/%v want %+v", got, err, want)
	}
	if len(f.api.writes) != 0 {
		t.Fatal("read-only discovery mutated checks")
	}
}
func TestRetirementDiscoveryRejectsIncompleteOrAmbiguousHistory(t *testing.T) {
	for _, problem := range []string{"API", "GraphQL", "null repo", "foreign repo", "foreign head", "wrong first head", "null history", "empty history", "duplicate commit", "missing cursor", "repeated cursor", "checks API", "missing check count", "negative check count", "partial checks", "checks overflow", "null checks", "null check", "missing check ID", "duplicate check", "foreign check head", "missing check name", "unknown status", "unknown native status", "foreign binding", "missing app", "missing app ID", "missing app slug"} {
		t.Run(problem, func(t *testing.T) {
			f, head := retirementDiscoveryFixture(t)
			check := retirementHistoryRecord(f, head, queueCheckName, "QUEUED")
			node := retirementHistoryNode(f, head, []map[string]any{check})
			page := retirementHistoryResponse(f, head, []map[string]any{node}, false, "")
			repo := page["data"].(map[string]any)["repository"].(map[string]any)
			object := repo["object"].(map[string]any)
			history := object["history"].(map[string]any)
			checks := f.api.responses[retirementChecksPath(f, head, 1)].(map[string]any)
			var response any = page
			switch problem {
			case "API":
				response = errors.New("history unavailable")
			case "GraphQL":
				page["errors"] = []map[string]string{{"message": "incomplete server result"}}
			case "null repo":
				page["data"] = map[string]any{"repository": nil}
			case "foreign repo":
				repo["databaseId"] = 11
			case "foreign head":
				object["oid"] = strings.Repeat("e", 40)
			case "wrong first head":
				node["oid"] = strings.Repeat("e", 40)
			case "null history":
				history["nodes"] = nil
			case "empty history":
				history["nodes"] = []map[string]any{}
			case "duplicate commit":
				history["nodes"] = []map[string]any{node, node}
			case "missing cursor":
				history["pageInfo"] = map[string]any{"hasNextPage": true}
			case "repeated cursor":
				history["pageInfo"] = map[string]any{"hasNextPage": true, "endCursor": "same"}
				other := retirementHistoryNode(f, strings.Repeat("e", 40), []map[string]any{})
				f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "same", 100)] = retirementHistoryResponse(f, head, []map[string]any{other}, true, "same")
			case "checks API":
				f.api.responses[retirementChecksPath(f, head, 1)] = errors.New("check listing unavailable")
			case "missing check count":
				delete(checks, "total_count")
			case "negative check count":
				checks["total_count"] = -1
			case "partial checks":
				checks["total_count"] = 2
			case "checks overflow":
				checks["total_count"] = 1001
			case "null checks":
				checks["check_runs"] = nil
			case "null check":
				checks["check_runs"] = []map[string]any{nil}
			case "missing check ID":
				delete(check, "id")
			case "duplicate check":
				checks["total_count"] = 2
				checks["check_runs"] = []map[string]any{check, check}
			case "foreign check head":
				check["head_sha"] = strings.Repeat("e", 40)
			case "missing check name":
				delete(check, "name")
			case "unknown status":
				check["status"] = "UNKNOWN"
			case "unknown native status":
				check["name"], check["status"] = "native job", "UNKNOWN"
			case "foreign binding":
				check["external_id"] = newBinding(11, f.pr, nil).externalID()
			case "missing app":
				check["app"] = nil
			case "missing app ID":
				delete(check["app"].(map[string]any), "id")
			case "missing app slug":
				delete(check["app"].(map[string]any), "slug")
			}
			f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = response
			got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
			if err == nil || len(got.Queue) != 0 || len(got.Observation) != 0 || len(got.ReporterTargets) != 0 {
				t.Fatalf("incomplete history authorized partial targets: %+v/%v", got, err)
			}
			if len(f.api.writes) != 0 {
				t.Fatal("failed discovery performed mutation")
			}
		})
	}
}
func TestRetirementDiscoveryBoundsHistoryAndMatrixWithoutPartialOutput(t *testing.T) {
	for _, bound := range []string{"history", "heads"} {
		t.Run(bound, func(t *testing.T) {
			f, head := retirementDiscoveryFixture(t)
			pages := retirementHistoryPages
			if bound == "heads" {
				pages = 2
			}
			for page := 0; page < pages; page++ {
				nodes := []map[string]any{}
				count := 1
				if bound == "heads" {
					count = 100
					if page == 1 {
						count = 1
					}
				}
				for i := 0; i < count; i++ {
					oid := fmt.Sprintf("%040x", 100*page+i+1)
					if page == 0 && i == 0 {
						oid = head
					}
					checks := []map[string]any{}
					if bound == "heads" {
						checks = append(checks, retirementHistoryRecord(f, oid, queueCheckName, "QUEUED"))
					}
					nodes = append(nodes, retirementHistoryNode(f, oid, checks))
				}
				after := ""
				if page > 0 {
					after = fmt.Sprint(page)
				}
				f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, after, 100)] = retirementHistoryResponse(f, head, nodes, true, fmt.Sprint(page+1))
			}
			got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
			if err == nil || !strings.Contains(err.Error(), "operator") || len(got.Queue) != 0 {
				t.Fatalf("bound silently dropped targets: %+v/%v", got, err)
			}
		})
	}
}

func TestRetirementDiscoveryPaginatesEffectiveChecks(t *testing.T) {
	for _, fault := range []string{"none", "missing page", "changing total", "foreign commit", "duplicate identity", "empty final page", "too many records"} {
		t.Run(fault, func(t *testing.T) {
			f, head := retirementDiscoveryFixture(t)
			checks := make([]map[string]any, 100)
			for i := range checks {
				checks[i] = retirementHistoryRecord(f, head, fmt.Sprintf("native job %d", i), "COMPLETED")
			}
			node := retirementHistoryNode(f, head, checks)
			f.api.responses[retirementChecksPath(f, head, 1)].(map[string]any)["total_count"] = 101
			f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, []map[string]any{node}, false, "")
			pending := retirementHistoryRecord(f, head, queueCheckName, "QUEUED")
			next := map[string]any{"total_count": 101, "check_runs": []map[string]any{pending}}
			var response any = next
			switch fault {
			case "changing total":
				next["total_count"] = 102
			case "foreign commit":
				pending["head_sha"] = strings.Repeat("e", 40)
			case "duplicate identity":
				pending["id"] = checks[0]["id"]
			case "empty final page":
				next["check_runs"] = []map[string]any{}
			case "too many records":
				next["check_runs"] = []map[string]any{pending, retirementHistoryRecord(f, head, "ci-required-next", "QUEUED")}
			case "missing page":
				response = errors.New("second check page unavailable")
			}
			f.api.responses[retirementChecksPath(f, head, 2)] = response
			got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
			if fault == "none" {
				if err != nil || !reflect.DeepEqual(got.Queue, []string{head}) {
					t.Fatalf("second-page pending missed: %+v/%v", got, err)
				}
			} else if err == nil || len(got.Queue) != 0 {
				t.Fatalf("partial check pages authorized output: %+v/%v", got, err)
			}
			if len(f.api.writes) != 0 {
				t.Fatal("paginated discovery performed mutation")
			}
		})
	}
}

func TestRetirementDiscoverySkipsSuccessfulSummariesWithoutLoadingChecks(t *testing.T) {
	f, head := retirementDiscoveryFixture(t)
	node := retirementHistoryNode(f, head, []map[string]any{})
	node["statusCheckRollup"].(map[string]any)["state"] = "SUCCESS"
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, []map[string]any{node}, false, "")
	got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
	if err != nil || len(got.Queue) != 0 || len(got.Observation) != 0 {
		t.Fatalf("green summary discovery: %+v/%v", got, err)
	}
	for query, count := range f.api.reads {
		if strings.Contains(query, "/check-runs?") && count != 0 {
			t.Fatal("successful summary loaded expensive checks")
		}
	}
}

func TestRetirementDiscoveryRequiresExactActionsAppIdentity(t *testing.T) {
	for _, mismatch := range []string{"ID", "slug"} {
		t.Run(mismatch, func(t *testing.T) {
			f, head := retirementDiscoveryFixture(t)
			check := retirementHistoryRecord(f, head, queueCheckName, "QUEUED")
			app := check["app"].(map[string]any)
			if mismatch == "ID" {
				app["id"] = 42
			} else {
				app["slug"] = "other"
			}
			f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, []map[string]any{retirementHistoryNode(f, head, []map[string]any{check})}, false, "")
			got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
			if err != nil || len(got.Queue) != 0 || len(got.Observation) != 0 || len(got.ReporterTargets) != 0 {
				t.Fatalf("inexact app identity authorized discovery: %+v/%v", got, err)
			}
			if len(f.api.writes) != 0 {
				t.Fatal("foreign app discovery mutated checks")
			}
		})
	}
}

func TestRetirementDiscoveryFindsPendingAtEffectiveCheckBudgetBoundary(t *testing.T) {
	f, head := retirementDiscoveryFixture(t)
	checks := make([]map[string]any, 1000)
	for i := range checks {
		checks[i] = retirementHistoryRecord(f, head, fmt.Sprintf("native job %d", i), "COMPLETED")
	}
	checks[len(checks)-1] = retirementHistoryRecord(f, head, queueCheckName, "QUEUED")
	node := retirementHistoryNode(f, head, checks[:100])
	for page := 1; page <= 10; page++ {
		f.api.responses[retirementChecksPath(f, head, page)] = map[string]any{"total_count": 1000, "check_runs": checks[(page-1)*100 : page*100]}
	}
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, []map[string]any{node}, false, "")
	got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
	if err != nil || !reflect.DeepEqual(got.Queue, []string{head}) {
		t.Fatalf("last permitted check page was omitted: %+v/%v", got, err)
	}
	if f.api.reads[retirementChecksPath(f, head, 11)] != 0 || len(f.api.writes) != 0 {
		t.Fatal("discovery exceeded its read budget or mutated checks")
	}
}

func TestRetirementDiscoveryAuthenticatesReadOnlySchedule(t *testing.T) {
	for _, change := range []string{"event", "lease", "repository", "default branch", "source ref"} {
		t.Run(change, func(t *testing.T) {
			f, _ := retirementDiscoveryFixture(t)
			e := f.event
			name := "schedule"
			switch change {
			case "event":
				name = "push"
			case "lease":
				f.c.expectedHead = f.pr.Head.SHA
			case "repository":
				e.Repository.ID++
			case "default branch":
				e.Repository.DefaultBranch = "other"
			case "source ref":
				t.Setenv("GITHUB_REF", "refs/heads/other")
			}
			if _, err := f.c.discoverRetirements(context.Background(), name, e); err == nil {
				t.Fatal("untrusted discovery source accepted")
			}
			if len(f.api.writes) != 0 {
				t.Fatal("discovery acquired mutation authority")
			}
		})
	}
}

func TestObservationRetirementBatchesBoundGlobalLeaseWork(t *testing.T) {
	for _, count := range []int{0, 1, 10, 11, 75, 100} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			heads := []string{}
			for i := 0; i < count; i++ {
				heads = append(heads, fmt.Sprintf("%040x", i+1))
			}
			batches := retirementObservationBatches(heads)
			if len(batches) != (count+9)/10 {
				t.Fatalf("unexpected batch count: %d", len(batches))
			}
			flat := []string{}
			for _, batch := range batches {
				if len(batch) == 0 || len(batch) > 10 {
					t.Fatal("global-lease batch escaped bound")
				}
				flat = append(flat, batch...)
			}
			if !reflect.DeepEqual(flat, heads) {
				t.Fatal("batching lost or reordered exact heads")
			}
		})
	}
}
func TestRetirementDiscoverySupportsOnlyEmptyHeadMainDispatch(t *testing.T) {
	for _, change := range []string{"none", "target head", "PR input", "work input", "wrong ref"} {
		t.Run(change, func(t *testing.T) {
			f, head := retirementDiscoveryFixture(t)
			e := f.event
			e.Ref = "refs/heads/main"
			e.Inputs.PR = ""
			switch change {
			case "target head":
				e.Inputs.Head = head
			case "PR input":
				e.Inputs.PR = "7"
			case "work input":
				e.Inputs.Work = "[]"
			case "wrong ref":
				e.Ref = "refs/heads/other"
			}
			node := map[string]any{"oid": head, "statusCheckRollup": nil}
			f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, []map[string]any{node}, false, "")
			got, err := f.c.discoverRetirements(context.Background(), "workflow_dispatch", e)
			if (err != nil) != (change != "none") {
				t.Fatalf("manual discovery: %+v/%v", got, err)
			}
			if len(f.api.writes) != 0 {
				t.Fatal("manual discovery mutated checks")
			}
		})
	}
}

func reporterDiscoveryRecord(f *queueFixture, head string, run, attempt int64) map[string]any {
	name := fmt.Sprintf("Merge queue validation (run %d, attempt %d)", run, attempt)
	record := retirementHistoryRecord(f, head, name, "IN_PROGRESS")
	record["external_id"] = fmt.Sprintf("hikyo-queue-report:v1:%d:7:%d:%d:%s:%s", f.c.repo.ID, run, attempt, strings.Repeat("d", 40), strings.Repeat("e", 40))
	return record
}
func TestReporterDiscoveryReusesSameHistoryAndCheckReads(t *testing.T) {
	f, head := retirementDiscoveryFixture(t)
	checks := []map[string]any{retirementHistoryRecord(f, head, queueCheckName, "QUEUED"), reporterDiscoveryRecord(f, head, 901, 2), reporterDiscoveryRecord(f, head, 900, 1), reporterDiscoveryRecord(f, head, 901, 2)}
	completed := reporterDiscoveryRecord(f, head, 902, 1)
	completed["status"] = "completed"
	checks = append(checks, completed)
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, []map[string]any{retirementHistoryNode(f, head, checks)}, false, "")
	got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
	want := []reporterRetirementTarget{{Head: head, Run: 900, Attempt: 1}, {Head: head, Run: 901, Attempt: 2}}
	if err != nil || !reflect.DeepEqual(got.ReporterTargets, want) || !reflect.DeepEqual(got.Queue, []string{head}) {
		t.Fatalf("reporter discovery: %+v/%v", got, err)
	}
	requests := 0
	for _, n := range f.api.reads {
		requests += n
	}
	if requests != 5 || len(f.api.writes) != 0 {
		t.Fatalf("reporter hints added reads or mutation: reads=%d writes=%d", requests, len(f.api.writes))
	}
}
func TestReporterDiscoveryRejectsMalformedAndForeignHints(t *testing.T) {
	for _, fault := range []string{"version", "missing fields", "foreign repository", "zero PR", "negative run", "zero attempt", "leading zero", "overflow", "group SHA", "base SHA", "name run mismatch", "unrecognized name", "foreign app", "missing app", "completed malformed"} {
		t.Run(fault, func(t *testing.T) {
			f, head := retirementDiscoveryFixture(t)
			record := reporterDiscoveryRecord(f, head, 900, 1)
			parts := strings.Split(record["external_id"].(string), ":")
			switch fault {
			case "version":
				parts[1] = "v2"
			case "missing fields":
				parts = parts[:7]
			case "foreign repository":
				parts[2] = "11"
			case "zero PR":
				parts[3] = "0"
			case "negative run":
				parts[4] = "-900"
			case "zero attempt":
				parts[5] = "0"
			case "leading zero":
				parts[4] = "0900"
			case "overflow":
				parts[4] = "9223372036854775808"
			case "group SHA":
				parts[6] = "bad"
			case "base SHA":
				parts[7] = strings.Repeat("E", 40)
			case "name run mismatch":
				record["name"] = "Merge queue validation (run 901, attempt 1)"
			case "unrecognized name":
				record["name"] = "another context"
			case "foreign app":
				record["app"] = map[string]any{"id": 42, "slug": "other"}
			case "missing app":
				record["app"] = nil
			case "completed malformed":
				record["status"] = "completed"
				parts[4] = "bad"
			}
			record["external_id"] = strings.Join(parts, ":")
			f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, []map[string]any{retirementHistoryNode(f, head, []map[string]any{record})}, false, "")
			got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
			ignored := fault == "foreign app" || fault == "completed malformed"
			if ignored {
				if err != nil || len(got.ReporterTargets) != 0 {
					t.Fatalf("unowned/terminal reporter became work: %+v/%v", got, err)
				}
			} else if err == nil || len(got.ReporterTargets) != 0 || len(got.Queue) != 0 {
				t.Fatalf("malformed hint authorized partial output: %+v/%v", got, err)
			}
			if len(f.api.writes) != 0 {
				t.Fatal("reporter discovery acquired writer authority")
			}
		})
	}
}
func TestReporterDiscoveryFindsPendingBeyondFirstCheckPage(t *testing.T) {
	f, head := retirementDiscoveryFixture(t)
	checks := make([]map[string]any, 100)
	for i := range checks {
		checks[i] = retirementHistoryRecord(f, head, fmt.Sprintf("native job %d", i), "COMPLETED")
	}
	node := retirementHistoryNode(f, head, checks)
	f.api.responses[retirementChecksPath(f, head, 1)].(map[string]any)["total_count"] = 101
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, []map[string]any{node}, false, "")
	f.api.responses[retirementChecksPath(f, head, 2)] = map[string]any{"total_count": 101, "check_runs": []map[string]any{reporterDiscoveryRecord(f, head, 900, 1)}}
	got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
	if err != nil || !reflect.DeepEqual(got.ReporterTargets, []reporterRetirementTarget{{Head: head, Run: 900, Attempt: 1}}) {
		t.Fatalf("second-page reporter missed: %+v/%v", got, err)
	}
	if len(f.api.writes) != 0 {
		t.Fatal("reporter discovery performed mutation")
	}
}
func TestReporterDiscoveryRejectsAmbiguousHeadsAndMatrixOverflow(t *testing.T) {
	for _, fault := range []string{"ambiguous heads", "too many attempts"} {
		t.Run(fault, func(t *testing.T) {
			f, head := retirementDiscoveryFixture(t)
			nodes := []map[string]any{retirementHistoryNode(f, head, []map[string]any{reporterDiscoveryRecord(f, head, 900, 1)})}
			if fault == "ambiguous heads" {
				other := strings.Repeat("c", 40)
				nodes = append(nodes, retirementHistoryNode(f, other, []map[string]any{reporterDiscoveryRecord(f, other, 900, 1)}))
			} else {
				first := []map[string]any{}
				for i := 0; i < 100; i++ {
					first = append(first, reporterDiscoveryRecord(f, head, int64(900+i), 1))
				}
				nodes[0] = retirementHistoryNode(f, head, first)
				other := strings.Repeat("c", 40)
				nodes = append(nodes, retirementHistoryNode(f, other, []map[string]any{reporterDiscoveryRecord(f, other, 1000, 1)}))
			}
			f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, nodes, false, "")
			got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
			if err == nil || len(got.ReporterTargets) != 0 {
				t.Fatalf("reporter matrix bound failed: %+v/%v", got, err)
			}
			if len(f.api.writes) != 0 {
				t.Fatal("bounded discovery mutated checks")
			}
		})
	}
}

func retirementDiscoveryWindowFixture(t *testing.T, shortFirst bool) (*queueFixture, string, string, string) {
	t.Helper()
	f, head := retirementDiscoveryFixture(t)
	after, last := "", ""
	for scanned, page := 0, 0; scanned < retirementHistoryWindow; page++ {
		first := min(100, retirementHistoryWindow-scanned)
		count := first
		if shortFirst && page == 0 {
			count--
		}
		nodes := []map[string]any{}
		for i := 0; i < count; i++ {
			oid := fmt.Sprintf("%040x", 10000+scanned+i)
			node := map[string]any{"oid": oid, "statusCheckRollup": nil}
			if scanned+i == 0 {
				oid = head
				node = retirementHistoryNode(f, oid, []map[string]any{retirementHistoryRecord(f, oid, queueCheckName, "QUEUED")})
			}
			if scanned+i == retirementHistoryWindow-1 {
				last = oid
				node = retirementHistoryNode(f, oid, []map[string]any{retirementHistoryRecord(f, oid, "ci-required-next", "QUEUED")})
			}
			nodes = append(nodes, node)
		}
		next := fmt.Sprintf("window-page-%d", page+1)
		f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, after, first)] = retirementHistoryResponse(f, head, nodes, true, next)
		after = next
		scanned += count
	}
	older := strings.Repeat("e", 40)
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, after, 100)] = retirementHistoryResponse(f, head, []map[string]any{retirementHistoryNode(f, older, []map[string]any{retirementHistoryRecord(f, older, queueCheckName, "QUEUED")})}, false, "")
	return f, head, last, after
}

func TestRetirementDiscoveryCompletesDeclaredRecentWindowWhenOlderHistoryRemains(t *testing.T) {
	for _, shortFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("short first page %t", shortFirst), func(t *testing.T) {
			f, head, last, cursor := retirementDiscoveryWindowFixture(t, shortFirst)
			got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
			want := retirementHistoryScope{Snapshot: head, MaxCommits: 1000, ScannedCommits: 1000, WindowLimited: true, NextCursor: cursor}
			if err != nil || got.HistoryScope != want || !reflect.DeepEqual(got.Queue, []string{head}) || !reflect.DeepEqual(got.Observation, []string{last}) {
				t.Fatalf("bounded newest-window discovery: %+v/%v", got, err)
			}
			if f.api.reads["graphql:"+f.c.retirementHistoryQuery(head, cursor, 100)] != 0 {
				t.Fatal("discovery read older history outside its declared window")
			}
			historyReads := 0
			for query, count := range f.api.reads {
				if strings.Contains(query, "history(") {
					historyReads += count
				}
			}
			wantReads := 10
			if shortFirst {
				wantReads++
			}
			if historyReads != wantReads || len(f.api.writes) != 0 {
				t.Fatalf("window expanded API cost or authority: reads=%d writes=%d", historyReads, len(f.api.writes))
			}
		})
	}
}

func TestRetirementDiscoveryWindowBoundaryNeverHidesActualErrors(t *testing.T) {
	for _, fault := range []string{"history API", "history GraphQL", "invalid final commit", "unknown final rollup", "missing cursor", "repeated cursor", "incomplete final checks", "too many final commits"} {
		t.Run(fault, func(t *testing.T) {
			f, head, _, _ := retirementDiscoveryWindowFixture(t, false)
			query := "graphql:" + f.c.retirementHistoryQuery(head, "window-page-9", 100)
			response := f.api.responses[query].(map[string]any)
			history := response["data"].(map[string]any)["repository"].(map[string]any)["object"].(map[string]any)["history"].(map[string]any)
			nodes := history["nodes"].([]map[string]any)
			final := nodes[len(nodes)-1]
			switch fault {
			case "history API":
				f.api.responses[query] = errors.New("final window page unavailable")
			case "history GraphQL":
				response["errors"] = []map[string]string{{"message": "incomplete final page"}}
			case "invalid final commit":
				final["oid"] = "invalid"
			case "unknown final rollup":
				final["statusCheckRollup"].(map[string]any)["state"] = "UNKNOWN"
			case "missing cursor":
				history["pageInfo"] = map[string]any{"hasNextPage": true}
			case "repeated cursor":
				history["pageInfo"] = map[string]any{"hasNextPage": true, "endCursor": "window-page-9"}
			case "incomplete final checks":
				f.api.responses[retirementChecksPath(f, final["oid"].(string), 1)].(map[string]any)["total_count"] = 2
			case "too many final commits":
				history["nodes"] = append(nodes, map[string]any{"oid": strings.Repeat("e", 40), "statusCheckRollup": nil})
			}
			got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
			if err == nil || len(got.Queue) != 0 || len(got.Observation) != 0 || len(got.ReporterTargets) != 0 || got.HistoryScope != (retirementHistoryScope{}) {
				t.Fatalf("window boundary hid incomplete evidence: %+v/%v", got, err)
			}
		})
	}
}

func TestRetirementDiscoveryReportsCompleteAncestryAtExactWindowSize(t *testing.T) {
	f, head, _, _ := retirementDiscoveryWindowFixture(t, false)
	query := "graphql:" + f.c.retirementHistoryQuery(head, "window-page-9", 100)
	response := f.api.responses[query].(map[string]any)
	history := response["data"].(map[string]any)["repository"].(map[string]any)["object"].(map[string]any)["history"].(map[string]any)
	history["pageInfo"] = map[string]any{"hasNextPage": false, "endCursor": nil}
	got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
	want := retirementHistoryScope{Snapshot: head, MaxCommits: 1000, ScannedCommits: 1000, CompleteAncestry: true}
	if err != nil || got.HistoryScope != want {
		t.Fatalf("exact-window complete ancestry was misreported: %+v/%v", got, err)
	}
}
