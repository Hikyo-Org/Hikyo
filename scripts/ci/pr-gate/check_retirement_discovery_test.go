package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func retirementDiscoveryFixture(t *testing.T) (*queueFixture, string) {
	t.Helper()
	f := newQueueFixture(t)
	f.c.expectedHead = ""
	t.Setenv("GITHUB_REF", "refs/heads/main")
	return f, f.pr.Base.SHA
}
func retirementHistoryRecord(f *queueFixture, head, name, status string) map[string]any {
	p := f.pr
	p.Head.SHA = head
	return map[string]any{"__typename": "CheckRun", "name": name, "status": status, "externalId": newBinding(f.c.repo.ID, p, nil).externalID(), "checkSuite": map[string]any{"app": map[string]any{"databaseId": actionsAppID, "slug": "github-actions"}}}
}
func retirementHistoryNode(head string, checks []map[string]any) map[string]any {
	return map[string]any{"oid": head, "statusCheckRollup": map[string]any{"state": "PENDING", "contexts": map[string]any{"totalCount": len(checks), "nodes": checks, "pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil}}}}
}
func retirementHistoryResponse(f *queueFixture, head string, nodes []map[string]any, more bool, cursor string) map[string]any {
	for start := 0; start < len(nodes); start += retirementContextBatchSize {
		batch := nodes[start:min(start+retirementContextBatchSize, len(nodes))]
		heads := []string{}
		repo := map[string]any{"databaseId": f.c.repo.ID}
		for _, node := range batch {
			if node["statusCheckRollup"] == nil {
				continue
			}
			repo[fmt.Sprintf("h%d", len(heads))] = node
			heads = append(heads, node["oid"].(string))
		}
		if len(heads) > 0 {
			f.api.responses["graphql:"+f.c.retirementContextBatchQuery(heads)] = map[string]any{"data": map[string]any{"repository": repo}}
		}
	}
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
	foreign["checkSuite"] = map[string]any{"app": map[string]any{"databaseId": 42, "slug": "other"}}
	second := []map[string]any{foreign, retirementHistoryRecord(f, old, "ordinary job", "QUEUED"), retirementHistoryRecord(f, old, "ci-required-next", "QUEUED"), {"__typename": "StatusContext"}}
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, []map[string]any{retirementHistoryNode(head, first)}, true, "next")
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "next", 100)] = retirementHistoryResponse(f, head, []map[string]any{retirementHistoryNode(old, second), {"oid": strings.Repeat("d", 40), "statusCheckRollup": nil}}, false, "")
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
	for _, problem := range []string{"API", "GraphQL", "null repo", "foreign repo", "foreign head", "wrong first head", "null history", "empty history", "duplicate commit", "missing cursor", "repeated cursor", "missing context count", "partial contexts", "contexts overflow", "unknown context", "unknown status", "foreign binding", "missing app"} {
		t.Run(problem, func(t *testing.T) {
			f, head := retirementDiscoveryFixture(t)
			check := retirementHistoryRecord(f, head, queueCheckName, "QUEUED")
			node := retirementHistoryNode(head, []map[string]any{check})
			page := retirementHistoryResponse(f, head, []map[string]any{node}, false, "")
			repo := page["data"].(map[string]any)["repository"].(map[string]any)
			object := repo["object"].(map[string]any)
			history := object["history"].(map[string]any)
			contexts := node["statusCheckRollup"].(map[string]any)["contexts"].(map[string]any)
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
				other := retirementHistoryNode(strings.Repeat("e", 40), []map[string]any{})
				f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "same", 100)] = retirementHistoryResponse(f, head, []map[string]any{other}, true, "same")
			case "missing context count":
				delete(contexts, "totalCount")
			case "partial contexts":
				contexts["totalCount"] = 2
			case "contexts overflow":
				contexts["pageInfo"] = map[string]any{"hasNextPage": true, "endCursor": "more"}
			case "unknown context":
				check["__typename"] = "Unknown"
			case "unknown status":
				check["status"] = "UNKNOWN"
			case "foreign binding":
				check["externalId"] = newBinding(11, f.pr, nil).externalID()
			case "missing app":
				check["checkSuite"] = nil
			}
			f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = response
			got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
			if problem == "missing app" {
				if err != nil || len(got.Queue) != 0 {
					t.Fatalf("unowned app became a target: %+v/%v", got, err)
				}
			} else if err == nil || len(got.Queue) != 0 || len(got.Observation) != 0 {
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
					nodes = append(nodes, retirementHistoryNode(oid, checks))
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

func TestRetirementDiscoveryPaginatesEffectiveContexts(t *testing.T) {
	for _, fault := range []string{"none", "missing page", "changing total", "foreign commit", "repeated cursor"} {
		t.Run(fault, func(t *testing.T) {
			f, head := retirementDiscoveryFixture(t)
			checks := make([]map[string]any, 100)
			for i := range checks {
				checks[i] = map[string]any{"__typename": "StatusContext"}
			}
			node := retirementHistoryNode(head, checks)
			contexts := node["statusCheckRollup"].(map[string]any)["contexts"].(map[string]any)
			contexts["totalCount"] = 101
			contexts["pageInfo"] = map[string]any{"hasNextPage": true, "endCursor": "context-next"}
			f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, []map[string]any{node}, false, "")
			next := retirementHistoryNode(head, []map[string]any{retirementHistoryRecord(f, head, queueCheckName, "QUEUED")})
			nextContexts := next["statusCheckRollup"].(map[string]any)["contexts"].(map[string]any)
			nextContexts["totalCount"] = 101
			switch fault {
			case "changing total":
				nextContexts["totalCount"] = 102
			case "foreign commit":
				next["oid"] = strings.Repeat("e", 40)
			case "repeated cursor":
				nextContexts["totalCount"] = 102
				contexts["totalCount"] = 102
				nextContexts["pageInfo"] = map[string]any{"hasNextPage": true, "endCursor": "context-next"}
			}
			var response any = map[string]any{"data": map[string]any{"repository": map[string]any{"databaseId": f.c.repo.ID, "object": next}}}
			if fault == "missing page" {
				response = errors.New("second context page unavailable")
			}
			f.api.responses["graphql:"+f.c.retirementContextsQuery(head, "context-next")] = response
			got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
			if fault == "none" {
				if err != nil || !reflect.DeepEqual(got.Queue, []string{head}) {
					t.Fatalf("second-page pending missed: %+v/%v", got, err)
				}
			} else if err == nil || len(got.Queue) != 0 {
				t.Fatalf("partial context pages authorized output: %+v/%v", got, err)
			}
		})
	}
}

func TestRetirementDiscoverySkipsSuccessfulSummariesWithoutLoadingContexts(t *testing.T) {
	f, head := retirementDiscoveryFixture(t)
	node := retirementHistoryNode(head, []map[string]any{})
	node["statusCheckRollup"].(map[string]any)["state"] = "SUCCESS"
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, []map[string]any{node}, false, "")
	got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
	if err != nil || len(got.Queue) != 0 || len(got.Observation) != 0 {
		t.Fatalf("green summary discovery: %+v/%v", got, err)
	}
	for query, count := range f.api.reads {
		if strings.Contains(query, "h0:object") && count != 0 {
			t.Fatal("successful summary loaded expensive contexts")
		}
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
	record["externalId"] = fmt.Sprintf("hikyo-queue-report:v1:%d:7:%d:%d:%s:%s", f.c.repo.ID, run, attempt, strings.Repeat("d", 40), strings.Repeat("e", 40))
	return record
}
func TestReporterDiscoveryReusesSameHistoryAndContextReads(t *testing.T) {
	f, head := retirementDiscoveryFixture(t)
	checks := []map[string]any{retirementHistoryRecord(f, head, queueCheckName, "QUEUED"), reporterDiscoveryRecord(f, head, 901, 2), reporterDiscoveryRecord(f, head, 900, 1), reporterDiscoveryRecord(f, head, 901, 2)}
	completed := reporterDiscoveryRecord(f, head, 902, 1)
	completed["status"] = "COMPLETED"
	checks = append(checks, completed)
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, []map[string]any{retirementHistoryNode(head, checks)}, false, "")
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
			parts := strings.Split(record["externalId"].(string), ":")
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
				record["checkSuite"] = map[string]any{"app": map[string]any{"databaseId": 42, "slug": "other"}}
			case "missing app":
				record["checkSuite"] = nil
			case "completed malformed":
				record["status"] = "COMPLETED"
				parts[4] = "bad"
			}
			record["externalId"] = strings.Join(parts, ":")
			f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, []map[string]any{retirementHistoryNode(head, []map[string]any{record})}, false, "")
			got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
			ignored := fault == "foreign app" || fault == "missing app" || fault == "completed malformed"
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
func TestReporterDiscoveryFindsPendingBeyondFirstContextPage(t *testing.T) {
	f, head := retirementDiscoveryFixture(t)
	checks := make([]map[string]any, 100)
	for i := range checks {
		checks[i] = map[string]any{"__typename": "StatusContext"}
	}
	node := retirementHistoryNode(head, checks)
	contexts := node["statusCheckRollup"].(map[string]any)["contexts"].(map[string]any)
	contexts["totalCount"] = 101
	contexts["pageInfo"] = map[string]any{"hasNextPage": true, "endCursor": "reports"}
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "", 100)] = retirementHistoryResponse(f, head, []map[string]any{node}, false, "")
	next := retirementHistoryNode(head, []map[string]any{reporterDiscoveryRecord(f, head, 900, 1)})
	next["statusCheckRollup"].(map[string]any)["contexts"].(map[string]any)["totalCount"] = 101
	f.api.responses["graphql:"+f.c.retirementContextsQuery(head, "reports")] = map[string]any{"data": map[string]any{"repository": map[string]any{"databaseId": f.c.repo.ID, "object": next}}}
	got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
	if err != nil || !reflect.DeepEqual(got.ReporterTargets, []reporterRetirementTarget{{Head: head, Run: 900, Attempt: 1}}) {
		t.Fatalf("second-page reporter missed: %+v/%v", got, err)
	}
}
func TestReporterDiscoveryRejectsAmbiguousHeadsAndMatrixOverflow(t *testing.T) {
	for _, fault := range []string{"ambiguous heads", "too many attempts"} {
		t.Run(fault, func(t *testing.T) {
			f, head := retirementDiscoveryFixture(t)
			nodes := []map[string]any{retirementHistoryNode(head, []map[string]any{reporterDiscoveryRecord(f, head, 900, 1)})}
			if fault == "ambiguous heads" {
				other := strings.Repeat("c", 40)
				nodes = append(nodes, retirementHistoryNode(other, []map[string]any{reporterDiscoveryRecord(f, other, 900, 1)}))
			} else {
				first := []map[string]any{}
				for i := 0; i < 100; i++ {
					first = append(first, reporterDiscoveryRecord(f, head, int64(900+i), 1))
				}
				nodes[0] = retirementHistoryNode(head, first)
				other := strings.Repeat("c", 40)
				nodes = append(nodes, retirementHistoryNode(other, []map[string]any{reporterDiscoveryRecord(f, other, 1000, 1)}))
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
				node = retirementHistoryNode(oid, []map[string]any{retirementHistoryRecord(f, oid, queueCheckName, "QUEUED")})
			}
			if scanned+i == retirementHistoryWindow-1 {
				last = oid
				node = retirementHistoryNode(oid, []map[string]any{retirementHistoryRecord(f, oid, "ci-required-next", "QUEUED")})
			}
			nodes = append(nodes, node)
		}
		next := fmt.Sprintf("window-page-%d", page+1)
		f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, after, first)] = retirementHistoryResponse(f, head, nodes, true, next)
		after = next
		scanned += count
	}
	older := strings.Repeat("e", 40)
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, after, 100)] = retirementHistoryResponse(f, head, []map[string]any{retirementHistoryNode(older, []map[string]any{retirementHistoryRecord(f, older, queueCheckName, "QUEUED")})}, false, "")
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
	for _, fault := range []string{"history API", "history GraphQL", "invalid final commit", "unknown final rollup", "missing cursor", "repeated cursor", "incomplete final contexts", "too many final commits"} {
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
			case "incomplete final contexts":
				final["statusCheckRollup"].(map[string]any)["contexts"].(map[string]any)["totalCount"] = 2
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
