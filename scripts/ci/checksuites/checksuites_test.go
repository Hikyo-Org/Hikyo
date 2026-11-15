package checksuites

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// source answers every query with a commit holding `suites` suites, the one
// numbered `holder` holding runs named "report".
func source(suites int, holder int64, queries *[]string) Source {
	return Source{
		Owner: "o", Name: "n", RepoID: 1,
		Diagnose: func([]json.RawMessage) error { return errors.New("diagnosed") },
		Read: func(_ context.Context, query string, out any) error {
			*queries = append(*queries, query)
			object := map[string]any{"oid": "abc", "checkSuites": map[string]any{"totalCount": suites}}
			if strings.Contains(query, "checkSuites(") {
				page := 0
				if _, after, ok := strings.Cut(query, `after:"p`); ok {
					fmt.Sscanf(after, "%d", &page)
				}
				nodes := []map[string]any{}
				for id := int64(page*100 + 1); id <= int64(min(suites, page*100+100)); id++ {
					named := 0
					if id == holder {
						named = 1
					}
					nodes = append(nodes, map[string]any{"databaseId": id, "named": map[string]any{"totalCount": named}})
				}
				object["every"] = map[string]any{"totalCount": suites}
				object["checkSuites"] = map[string]any{"totalCount": suites, "nodes": nodes, "pageInfo": map[string]any{"hasNextPage": (page+1)*100 < suites, "endCursor": fmt.Sprintf("p%d", page+1)}}
			}
			data, err := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{"databaseId": 1, "object": object}}})
			if err != nil {
				return err
			}
			return json.Unmarshal(data, out)
		},
	}
}

func TestListingsReadBySuiteOnlyAboveTheMargin(t *testing.T) {
	for _, tc := range []struct {
		suites int
		want   []string
	}{
		{suites: 1, want: []string{"commits/abc/check-runs"}},
		{suites: Margin, want: []string{"commits/abc/check-runs"}},
	} {
		var queries []string
		got, err := Listings(context.Background(), source(tc.suites, 7, &queries), "abc", "report")
		if err != nil || fmt.Sprint(got) != fmt.Sprint(tc.want) || len(queries) != 1 {
			t.Fatalf("%d suites: %v/%v after %d queries, want %v after one", tc.suites, got, err, len(queries), tc.want)
		}
	}
	// Past the margin only the suite that holds the check is listed, and the scan
	// selects by the check's name so it cannot name another context's suites.
	var queries []string
	got, err := Listings(context.Background(), source(Margin+1, 7, &queries), "abc", "report")
	if err != nil || fmt.Sprint(got) != "[check-suites/7/check-runs]" || !strings.Contains(queries[len(queries)-1], `checkName:"report"`) {
		t.Fatalf("listings = %v/%v", got, err)
	}
}

func TestFindRefusesMissingNamedRunCounts(t *testing.T) {
	var queries []string
	s := source(3, 1, &queries)
	read := s.Read
	s.Read = func(ctx context.Context, query string, out any) error {
		var response map[string]any
		if err := read(ctx, query, &response); err != nil {
			return err
		}
		if strings.Contains(query, "checkSuites(") {
			nodes := response["data"].(map[string]any)["repository"].(map[string]any)["object"].(map[string]any)["checkSuites"].(map[string]any)["nodes"].([]any)
			delete(nodes[1].(map[string]any), "named")
		}
		data, _ := json.Marshal(response)
		return json.Unmarshal(data, out)
	}
	if _, _, err := Find(context.Background(), s, "abc", Selector{Name: "report"}); err == nil {
		t.Fatal("a suite without its run count was treated as holding nothing")
	}
}

func TestCountRefusesForeignRepository(t *testing.T) {
	var queries []string
	s := source(5, 0, &queries)
	s.RepoID = 2
	if _, err := Count(context.Background(), s, "abc"); err == nil {
		t.Fatal("a count for another repository was accepted")
	}
}
