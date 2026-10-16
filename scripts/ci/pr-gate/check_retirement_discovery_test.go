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
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "")] = retirementHistoryResponse(f, head, []map[string]any{retirementHistoryNode(head, first)}, true, "next")
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "next")] = retirementHistoryResponse(f, head, []map[string]any{retirementHistoryNode(old, second), {"oid": strings.Repeat("d", 40), "statusCheckRollup": nil}}, false, "")
	got, err := f.c.discoverRetirements(context.Background(), "schedule", f.event)
	want := retirementTargets{Queue: []string{head}, Observation: []string{head, old}, ObservationBatches: [][]string{{head, old}}}
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
				f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "same")] = retirementHistoryResponse(f, head, []map[string]any{other}, true, "same")
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
			f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "")] = response
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
				f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, after)] = retirementHistoryResponse(f, head, nodes, true, fmt.Sprint(page+1))
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
			f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "")] = retirementHistoryResponse(f, head, []map[string]any{node}, false, "")
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
	f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "")] = retirementHistoryResponse(f, head, []map[string]any{node}, false, "")
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
			f.api.responses["graphql:"+f.c.retirementHistoryQuery(head, "")] = retirementHistoryResponse(f, head, []map[string]any{node}, false, "")
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
