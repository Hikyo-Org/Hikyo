// Package checksuites finds the github-actions check suites that hold the check
// runs a CI owner needs, for commits on which REST cannot list runs by commit.
//
// REST lists a commit's check runs from only its newest 1000 check suites, of
// every app. Past that, a listing by commit is silently partial, so an owner
// reads the suites that hold its runs one by one. GraphQL finds those suites,
// filtered server-side to the github-actions app so no foreign app is traversed.
package checksuites

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// AppID is the github-actions app that owns every synthetic check context.
const AppID = 15368

// Margin is the suite count above which a listing by commit cannot be trusted.
// It sits under REST's cap of 1000 so suites added between counting and listing
// cannot push a commit over it unnoticed.
const Margin = 900

// MaxSuites bounds the github-actions suites one scan reads (50 pages of 100).
const MaxSuites = 5000

// MaxSelected bounds the suites one scan may select; more is left to an operator.
const MaxSelected = 100

const pageSize = 100

// Source reads one repository through GraphQL.
type Source struct {
	Owner, Name string
	RepoID      int64
	// Read runs query and decodes its JSON response into out.
	Read func(ctx context.Context, query string, out any) error
	// Diagnose turns the errors a GraphQL response reports into an error. Nil
	// reports the first error's message.
	Diagnose func(errs []jsontext.Value) error
}

// Selector picks the suites to read. The zero value selects suites holding a
// latest run that has not finished, which is how retirement discovery finds
// unfinished synthetic contexts. A Name selects suites holding any run of that
// check, finished or not, for owners that must see every record of a context.
type Selector struct{ Name string }

type pageInfo struct {
	More *bool   `json:"hasNextPage"`
	End  *string `json:"endCursor"`
}

type total struct {
	Total *int `json:"totalCount"`
}

func (s Source) diagnose(errs []jsontext.Value) error {
	if s.Diagnose != nil {
		return s.Diagnose(errs)
	}
	var first struct {
		Message string `json:"message"`
	}
	if len(errs) == 0 || json.Unmarshal(errs[0], &first) != nil || strings.TrimSpace(first.Message) == "" {
		return errors.New("GraphQL errors: missing or malformed server error detail")
	}
	message := first.Message
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if token := os.Getenv(key); token != "" {
			message = strings.ReplaceAll(message, token, "[REDACTED]")
		}
	}
	if runes := []rune(message); len(runes) > 200 {
		message = string(runes[:200]) + "...[truncated]"
	}
	// %q keeps server text from writing extra log lines or workflow commands.
	return fmt.Errorf("GraphQL error: %q", message)
}

func (s Source) object(head, fields string) string {
	return fmt.Sprintf(`query{repository(owner:%q,name:%q){databaseId object(oid:%q){... on Commit{oid %s}}}}`, s.Owner, s.Name, head, fields)
}

// Count returns how many check suites of every app head holds, the figure REST's
// cap applies to.
func Count(ctx context.Context, s Source, head string) (int, error) {
	var response struct {
		Errors []jsontext.Value `json:"errors"`
		Data   struct {
			Repo *struct {
				ID     int64 `json:"databaseId"`
				Object *struct {
					OID    string `json:"oid"`
					Suites *total `json:"checkSuites"`
				} `json:"object"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := s.Read(ctx, s.object(head, "checkSuites{totalCount}"), &response); err != nil {
		return 0, fmt.Errorf("check suite count for %s: %w", head, err)
	}
	if len(response.Errors) != 0 {
		return 0, s.diagnose(response.Errors)
	}
	repo := response.Data.Repo
	if repo == nil || repo.ID != s.RepoID || repo.Object == nil || repo.Object.OID != head || repo.Object.Suites == nil || repo.Object.Suites.Total == nil || *repo.Object.Suites.Total < 0 {
		return 0, errors.New("incomplete or foreign check suite count")
	}
	return *repo.Object.Suites.Total, nil
}

// SuitesQuery is the query Find sends for one page of suites after cursor.
func (s Source) SuitesQuery(head, after string, sel Selector) string {
	args := fmt.Sprintf("first:%d", pageSize)
	if after != "" {
		args += fmt.Sprintf(",after:%q", after)
	}
	// Counting runs rather than naming unfinished states keeps a future status
	// visible to the REST read, which refuses unknown ones.
	runs := `all:checkRuns(first:1,filterBy:{checkType:LATEST}){totalCount} done:checkRuns(first:1,filterBy:{checkType:LATEST,statuses:[COMPLETED]}){totalCount}`
	if sel.Name != "" {
		runs = fmt.Sprintf(`named:checkRuns(first:1,filterBy:{checkType:ALL,checkName:%q}){totalCount}`, sel.Name)
	}
	return s.object(head, fmt.Sprintf(`every:checkSuites{totalCount} checkSuites(%s,filterBy:{appId:%d}){totalCount nodes{databaseId %s} pageInfo{hasNextPage endCursor}}`, args, AppID, runs))
}

// Find returns the ids of head's github-actions suites that sel selects.
// A scan that cannot be complete fails instead of returning a partial set.
func Find(ctx context.Context, s Source, head string, sel Selector) ([]int64, error) {
	selected := []int64{}
	seen := make(map[int64]bool)
	seenCursors := make(map[string]bool)
	count, after := -1, ""
	for page := 1; page <= MaxSuites/pageSize; page++ {
		var response struct {
			Errors []jsontext.Value `json:"errors"`
			Data   struct {
				Repo *struct {
					ID     int64 `json:"databaseId"`
					Object *struct {
						OID    string `json:"oid"`
						Every  *total `json:"every"`
						Suites *struct {
							total
							Nodes []*struct {
								ID    int64  `json:"databaseId"`
								All   *total `json:"all"`
								Done  *total `json:"done"`
								Named *total `json:"named"`
							} `json:"nodes"`
							Page pageInfo `json:"pageInfo"`
						} `json:"checkSuites"`
					} `json:"object"`
				} `json:"repository"`
			} `json:"data"`
		}
		if err := s.Read(ctx, s.SuitesQuery(head, after, sel), &response); err != nil {
			return nil, fmt.Errorf("check suites for %s page %d: %w", head, page, err)
		}
		if len(response.Errors) != 0 {
			return nil, s.diagnose(response.Errors)
		}
		repo := response.Data.Repo
		if repo == nil || repo.ID != s.RepoID || repo.Object == nil || repo.Object.OID != head || repo.Object.Suites == nil {
			return nil, errors.New("incomplete or foreign check suite snapshot")
		}
		part := repo.Object.Suites
		if part.Total == nil || *part.Total < 0 || count != -1 && count != *part.Total || part.Nodes == nil || len(part.Nodes) > pageSize || part.Page.More == nil {
			return nil, errors.New("incomplete or changing check suite inventory")
		}
		count = *part.Total
		// REST's newest-1000-suite cap counts every app's suites, not only these.
		if repo.Object.Every == nil || repo.Object.Every.Total == nil || *repo.Object.Every.Total < count {
			return nil, errors.New("incomplete check suite count")
		}
		if count > MaxSuites {
			return nil, fmt.Errorf("checks on %s span more than %d github-actions check suites; operator review required", head, MaxSuites)
		}
		for _, suite := range part.Nodes {
			if suite == nil || suite.ID <= 0 || seen[suite.ID] {
				return nil, errors.New("incomplete or duplicate check suite identity")
			}
			seen[suite.ID] = true
			hit, err := sel.holds(suite.All, suite.Done, suite.Named)
			if err != nil {
				return nil, err
			}
			if !hit {
				continue
			}
			selected = append(selected, suite.ID)
			if len(selected) > MaxSelected {
				return nil, fmt.Errorf("checks on %s are in more than %d check suites; operator review required", head, MaxSelected)
			}
		}
		if !*part.Page.More {
			if len(seen) != count {
				return nil, errors.New("incomplete check suite pagination")
			}
			return selected, nil
		}
		if len(seen) >= count {
			return nil, errors.New("incomplete check suite pagination")
		}
		if len(part.Nodes) != pageSize {
			return nil, errors.New("short non-final check suite page")
		}
		if part.Page.End == nil || *part.Page.End == "" || len(*part.Page.End) > 1024 || seenCursors[*part.Page.End] {
			return nil, errors.New("missing or repeated check suite cursor")
		}
		after = *part.Page.End
		seenCursors[after] = true
	}
	return nil, fmt.Errorf("checks on %s span more than %d github-actions check suites; operator review required", head, MaxSuites)
}

// holds reports whether a suite with these run counts is selected, refusing
// counts that are missing or inconsistent.
func (sel Selector) holds(all, done, named *total) (bool, error) {
	if sel.Name != "" {
		if named == nil || named.Total == nil || *named.Total < 0 {
			return false, errors.New("incomplete check suite run counts")
		}
		return *named.Total > 0, nil
	}
	if all == nil || all.Total == nil || done == nil || done.Total == nil || *done.Total < 0 || *all.Total < *done.Total {
		return false, errors.New("incomplete or inconsistent check suite run counts")
	}
	return *all.Total > *done.Total, nil
}

// Recheck refuses a commit listing that was read while head's suites grew past
// Margin: REST lists only the newest 1000 suites, so the read could have missed
// an older suite holding the check. Call it after reading every listing.
func Recheck(ctx context.Context, s Source, head string, listings []string) error {
	if len(listings) != 1 || !strings.HasPrefix(listings[0], "commits/") {
		return nil
	}
	count, err := Count(ctx, s, head)
	if err != nil {
		return err
	}
	if count > Margin {
		return fmt.Errorf("check suites on %s grew past %d during the read; retry", head, Margin)
	}
	return nil
}

// Listings returns the check-runs REST listings that together hold every run of
// the named check on head, each followed by the query string the caller adds:
// the commit's own listing while REST sees every suite, else one listing per
// github-actions suite holding the check.
func Listings(ctx context.Context, s Source, head, name string) ([]string, error) {
	count, err := Count(ctx, s, head)
	if err != nil {
		return nil, err
	}
	if count <= Margin {
		return []string{fmt.Sprintf("commits/%s/check-runs", head)}, nil
	}
	suites, err := Find(ctx, s, head, Selector{Name: name})
	if err != nil {
		return nil, err
	}
	slices.Sort(suites)
	listings := make([]string, 0, len(suites))
	for _, suite := range suites {
		listings = append(listings, fmt.Sprintf("check-suites/%d/check-runs", suite))
	}
	return listings, nil
}
