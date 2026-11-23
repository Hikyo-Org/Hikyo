package checksuites

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// source answers every query with a commit holding `suites` suites, the one
// numbered `holder` holding runs named "report".
func source(suites int, holder int64, queries *[]string) Source {
	return Source{
		Owner: "o", Name: "n", RepoID: 1,
		Diagnose: func([]jsontext.Value) error { return errors.New("diagnosed") },
		Read: func(_ context.Context, query string, out any) error {
			*queries = append(*queries, query)
			object := map[string]any{"oid": "abc", "checkSuites": map[string]any{"totalCount": suites}}
			if strings.Contains(query, "checkSuites(") {
				page := 0
				if _, after, ok := strings.Cut(query, `after:"p`); ok {
					digits, _, _ := strings.Cut(after, `"`)
					page, _ = strconv.Atoi(digits)
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

// Suites added while a commit listing is read can push an older suite out of
// REST's newest 1000, so the read is refused when the count has grown past the
// margin, and a per-suite read needs no recount.
func TestRecheckRefusesACommitListingAfterTheSuitesGrew(t *testing.T) {
	commit := []string{"commits/abc/check-runs"}
	for _, tc := range []struct {
		suites  int
		wantErr bool
	}{
		{suites: Margin},
		{suites: Margin + 1, wantErr: true},
	} {
		var queries []string
		err := Recheck(context.Background(), source(tc.suites, 7, &queries), "abc", commit)
		if (err != nil) != tc.wantErr || len(queries) != 1 {
			t.Fatalf("%d suites: err = %v after %d queries, want error %t after one count", tc.suites, err, len(queries), tc.wantErr)
		}
	}
	var queries []string
	if err := Recheck(context.Background(), source(Margin+1, 7, &queries), "abc", []string{"check-suites/7/check-runs"}); err != nil || len(queries) != 0 {
		t.Fatalf("per-suite listing: err = %v after %d queries, want no recount", err, len(queries))
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
		data, err := json.Marshal(response)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, out)
	}
	if _, err := Find(context.Background(), s, "abc", Selector{Name: "report"}); err == nil {
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

// A context spread over more suites than MaxSelected is left to an operator, not
// listed partially.
func TestFindRefusesMoreSelectedSuitesThanMaxSelected(t *testing.T) {
	var queries []string
	for _, tc := range []struct {
		suites  int
		refused bool
	}{{MaxSelected, false}, {MaxSelected + 1, true}} {
		s := source(tc.suites, 0, &queries)
		read := s.Read
		s.Read = func(ctx context.Context, query string, out any) error {
			var response map[string]any
			if err := read(ctx, query, &response); err != nil {
				return err
			}
			if strings.Contains(query, "checkSuites(") {
				nodes := response["data"].(map[string]any)["repository"].(map[string]any)["object"].(map[string]any)["checkSuites"].(map[string]any)["nodes"].([]any)
				for _, node := range nodes {
					node.(map[string]any)["named"] = map[string]any{"totalCount": 1}
				}
			}
			data, err := json.Marshal(response)
			if err != nil {
				return err
			}
			return json.Unmarshal(data, out)
		}
		got, err := Find(context.Background(), s, "abc", Selector{Name: "report"})
		if (err != nil) != tc.refused || (err == nil && len(got) != tc.suites) {
			t.Fatalf("%d suites holding the name: %d selected, err=%v, want refused=%v", tc.suites, len(got), err, tc.refused)
		}
	}
}

// The default diagnosis quotes server text so it cannot add log lines, bounds it
// and redacts the token.
func TestDefaultDiagnoseQuotesBoundsAndRedacts(t *testing.T) {
	t.Setenv("GH_TOKEN", "secret-token")
	message := "line\n::error::forged secret-token " + strings.Repeat("x", 300)
	raw, err := json.Marshal([]map[string]string{{"message": message}})
	if err != nil {
		t.Fatal(err)
	}
	var errs []jsontext.Value
	if err := json.Unmarshal(raw, &errs); err != nil {
		t.Fatal(err)
	}
	got := Source{}.diagnose(errs).Error()
	if strings.ContainsAny(got, "\n") || strings.Contains(got, "secret-token") || !strings.Contains(got, "[REDACTED]") || !strings.Contains(got, "...[truncated]") || len(got) > 300 {
		t.Fatalf("diagnosis = %q", got)
	}
}
