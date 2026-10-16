// retire-runs cancels only authenticated, no-longer-referenced merge-group
// validation. It never publishes checks, reruns validation, or deletes history.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const workflowPath = ".github/workflows/ci-merge-group.yml"
const maxPages = 20
const maxCancels = 10

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type api interface {
	read(context.Context, string, any) error
	cancel(context.Context, int64) error
	ref(context.Context, string) (string, error)
}

type githubAPI struct {
	repo      string
	remaining int
}

func (a *githubAPI) command(ctx context.Context, args ...string) ([]byte, error) {
	if a.remaining == 0 {
		return nil, errors.New("retirement API request budget exhausted")
	}
	a.remaining--
	cmd := exec.CommandContext(ctx, "gh", args...)
	// Never print stderr: it can contain authenticated API payloads.
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("retirement API request failed: %w", err)
	}
	if len(out) > 8<<20 {
		return nil, errors.New("retirement API response exceeds bound")
	}
	return out, nil
}

func (a *githubAPI) read(ctx context.Context, path string, target any) error {
	args := []string{"api", "--hostname", "github.com", path}
	if strings.HasPrefix(path, "graphql:") {
		args = []string{"api", "--hostname", "github.com", "graphql", "-f", "query=" + strings.TrimPrefix(path, "graphql:")}
	}
	raw, err := a.command(ctx, args...)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func (a *githubAPI) cancel(ctx context.Context, id int64) error {
	_, err := a.command(ctx, "api", "--hostname", "github.com", "--method", "POST", fmt.Sprintf("repos/%s/actions/runs/%d/cancel", a.repo, id))
	return err
}

// An exact ref's 404 is evidence of absence. Every other failure, including
// malformed responses, rate limits and permissions, refuses cancellation.
func (a *githubAPI) ref(ctx context.Context, branch string) (string, error) {
	out, err := a.command(ctx, "api", "--hostname", "github.com", "--include", fmt.Sprintf("repos/%s/git/ref/heads/%s", a.repo, url.PathEscape(branch)))
	if err != nil {
		if strings.HasPrefix(string(out), "HTTP/2.0 404 ") || strings.HasPrefix(string(out), "HTTP/1.1 404 ") {
			return "", nil
		}
		return "", err
	}
	_, body, ok := strings.Cut(string(out), "\r\n\r\n")
	if !ok {
		_, body, ok = strings.Cut(string(out), "\n\n")
	}
	if !ok {
		return "", errors.New("missing ref response headers")
	}
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	if err := json.Unmarshal([]byte(body), &ref); err != nil {
		return "", err
	}
	if ref.Ref != "refs/heads/"+branch || ref.Object.Type != "commit" || !shaPattern.MatchString(ref.Object.SHA) {
		return "", errors.New("invalid authenticated merge-group ref")
	}
	return ref.Object.SHA, nil
}

type run struct {
	ID         int64   `json:"id"`
	Attempt    int     `json:"run_attempt"`
	WorkflowID int64   `json:"workflow_id"`
	Name       string  `json:"name"`
	Path       string  `json:"path"`
	Event      string  `json:"event"`
	Head       string  `json:"head_sha"`
	Branch     string  `json:"head_branch"`
	Status     string  `json:"status"`
	Conclusion *string `json:"conclusion"`
	Repository struct {
		ID   int64  `json:"id"`
		Name string `json:"full_name"`
	} `json:"repository"`
	HeadRepository struct {
		ID   int64  `json:"id"`
		Name string `json:"full_name"`
	} `json:"head_repository"`
}

type sweeper struct {
	api        api
	repo       string
	repoID     int64
	branch     string
	workflowID int64
	dryRun     bool
	wait       func(context.Context) error
}

func active(status string) bool {
	switch status {
	case "queued", "in_progress", "waiting", "pending", "requested":
		return true
	}
	return false
}

func (s *sweeper) owned(r run) bool {
	return r.ID > 0 && r.Attempt > 0 && r.WorkflowID == s.workflowID && r.Name == "trusted-merge-ci" && r.Path == workflowPath && r.Event == "merge_group" && shaPattern.MatchString(r.Head) && strings.HasPrefix(r.Branch, "gh-readonly-queue/"+s.branch+"/") && r.Repository.ID == s.repoID && r.Repository.Name == s.repo && r.HeadRepository.ID == s.repoID && r.HeadRepository.Name == s.repo
}

func (s *sweeper) inventory(ctx context.Context) ([]run, error) {
	var result []run
	seen := map[int64]bool{}
	for _, status := range []string{"queued", "in_progress", "waiting", "pending", "requested"} {
		count := -1
		found := 0
		complete := false
		for page := 1; page <= maxPages; page++ {
			var response struct {
				Count *int  `json:"total_count"`
				Runs  []run `json:"workflow_runs"`
			}
			path := fmt.Sprintf("repos/%s/actions/workflows/%d/runs?event=merge_group&status=%s&per_page=100&page=%d", s.repo, s.workflowID, status, page)
			if err := s.api.read(ctx, path, &response); err != nil {
				return nil, err
			}
			if response.Count == nil || response.Runs == nil || *response.Count < 0 || *response.Count > 1000 || len(response.Runs) > 100 {
				return nil, errors.New("incomplete active-run inventory")
			}
			if count == -1 {
				count = *response.Count
			}
			if count != *response.Count {
				return nil, errors.New("active-run inventory changed during pagination")
			}
			for _, r := range response.Runs {
				if !s.owned(r) || r.Status != status || r.Conclusion != nil || seen[r.ID] {
					return nil, errors.New("ambiguous active-run identity")
				}
				seen[r.ID] = true
				result = append(result, r)
			}
			found += len(response.Runs)
			if found == count {
				complete = true
				break
			}
			if len(response.Runs) != 100 || found > count {
				return nil, errors.New("truncated active-run inventory")
			}
		}
		if !complete {
			return nil, errors.New("active-run inventory page limit reached")
		}
	}
	return result, nil
}

type pageInfo struct {
	More   *bool   `json:"hasNextPage"`
	Cursor *string `json:"endCursor"`
}
type pull struct {
	Number int    `json:"number"`
	Head   string `json:"headRefOid"`
	Base   string `json:"baseRefName"`
}
type entry struct {
	ID       string `json:"id"`
	Position int    `json:"position"`
	State    string `json:"state"`
	Head     struct {
		OID string `json:"oid"`
	} `json:"headCommit"`
	Base struct {
		OID string `json:"oid"`
	} `json:"baseCommit"`
	Pull pull `json:"pullRequest"`
}

func nextPage(info pageInfo, count int, seen map[string]bool) (string, bool, error) {
	if info.More == nil {
		return "", false, errors.New("missing role pagination metadata")
	}
	if !*info.More {
		return "", true, nil
	}
	if count == 0 || info.Cursor == nil || *info.Cursor == "" || len(*info.Cursor) > 1024 || seen[*info.Cursor] {
		return "", false, errors.New("missing or repeated role cursor")
	}
	seen[*info.Cursor] = true
	return *info.Cursor, false, nil
}

// Protect every open PR head and queue head/base, including shared SHA roles.
// A missing queue, partial GraphQL data, or incomplete page refuses all writes.
func (s *sweeper) roles(ctx context.Context) (map[string]bool, error) {
	parts := strings.Split(s.repo, "/")
	roles := map[string]bool{}
	pullCursor, queueCursor, mainHead, queueID := "", "", "", ""
	pullsDone, queueDone := false, false
	pullCursors, queueCursors := map[string]bool{}, map[string]bool{}
	seenPulls, seenQueuePulls := map[int]bool{}, map[int]bool{}
	seenEntries, positions := map[string]bool{}, map[int]bool{}
	for page := 0; page < maxPages; page++ {
		pullArgs, queueArgs := "first:100", "first:100"
		if pullCursor != "" {
			pullArgs += fmt.Sprintf(",after:%q", pullCursor)
		}
		if queueCursor != "" {
			queueArgs += fmt.Sprintf(",after:%q", queueCursor)
		}
		query := fmt.Sprintf(`query{repository(owner:%q,name:%q){databaseId defaultBranchRef{name target{... on Commit{oid}}} pullRequests(states:OPEN,%s){nodes{number headRefOid baseRefName} pageInfo{hasNextPage endCursor}} mergeQueue(branch:%q){id entries(%s){nodes{id position state headCommit{oid} baseCommit{oid} pullRequest{number headRefOid baseRefName}} pageInfo{hasNextPage endCursor}}}}}`, parts[0], parts[1], pullArgs, s.branch, queueArgs)
		var response struct {
			Errors []json.RawMessage `json:"errors"`
			Data   struct {
				Repository *struct {
					ID      int64 `json:"databaseId"`
					Default *struct {
						Name   string `json:"name"`
						Target struct {
							OID string `json:"oid"`
						} `json:"target"`
					} `json:"defaultBranchRef"`
					Pulls struct {
						Nodes []pull   `json:"nodes"`
						Page  pageInfo `json:"pageInfo"`
					} `json:"pullRequests"`
					Queue *struct {
						ID      string `json:"id"`
						Entries struct {
							Nodes []entry  `json:"nodes"`
							Page  pageInfo `json:"pageInfo"`
						} `json:"entries"`
					} `json:"mergeQueue"`
				} `json:"repository"`
			} `json:"data"`
		}
		if err := s.api.read(ctx, "graphql:"+query, &response); err != nil {
			return nil, err
		}
		r := response.Data.Repository
		if len(response.Errors) != 0 || r == nil || r.ID != s.repoID || r.Default == nil || r.Default.Name != s.branch || !shaPattern.MatchString(r.Default.Target.OID) || r.Queue == nil || r.Queue.ID == "" {
			return nil, errors.New("missing authenticated current roles")
		}
		if page == 0 {
			mainHead, queueID = r.Default.Target.OID, r.Queue.ID
		}
		if mainHead != r.Default.Target.OID || queueID != r.Queue.ID {
			return nil, errors.New("role authority changed during pagination")
		}
		roles[mainHead] = true
		if !pullsDone {
			if r.Pulls.Nodes == nil || len(r.Pulls.Nodes) > 100 {
				return nil, errors.New("missing open PR inventory")
			}
			for _, p := range r.Pulls.Nodes {
				if p.Number <= 0 || p.Base == "" || !shaPattern.MatchString(p.Head) || seenPulls[p.Number] {
					return nil, errors.New("invalid open PR identity")
				}
				seenPulls[p.Number] = true
				roles[p.Head] = true
			}
			var err error
			pullCursor, pullsDone, err = nextPage(r.Pulls.Page, len(r.Pulls.Nodes), pullCursors)
			if err != nil {
				return nil, err
			}
		}
		if !queueDone {
			if r.Queue.Entries.Nodes == nil || len(r.Queue.Entries.Nodes) > 100 {
				return nil, errors.New("missing queue inventory")
			}
			for _, e := range r.Queue.Entries.Nodes {
				if e.ID == "" || e.Position <= 0 || e.Pull.Number <= 0 || e.Pull.Base != s.branch || !shaPattern.MatchString(e.Pull.Head) || seenEntries[e.ID] || seenQueuePulls[e.Pull.Number] || positions[e.Position] {
					return nil, errors.New("invalid queue role identity")
				}
				seenEntries[e.ID], seenQueuePulls[e.Pull.Number], positions[e.Position] = true, true, true
				roles[e.Pull.Head] = true
				switch e.State {
				case "QUEUED", "AWAITING_CHECKS", "MERGEABLE", "UNMERGEABLE", "LOCKED":
				default:
					return nil, errors.New("invalid queue state")
				}
				if e.State == "QUEUED" && e.Head.OID == "" {
					if e.Base.OID != "" {
						if !shaPattern.MatchString(e.Base.OID) {
							return nil, errors.New("invalid unmaterialized queue base")
						}
						roles[e.Base.OID] = true
					}
					continue
				}
				if !shaPattern.MatchString(e.Head.OID) || !shaPattern.MatchString(e.Base.OID) {
					return nil, errors.New("invalid materialized queue role")
				}
				roles[e.Head.OID], roles[e.Base.OID] = true, true
			}
			var err error
			queueCursor, queueDone, err = nextPage(r.Queue.Entries.Page, len(r.Queue.Entries.Nodes), queueCursors)
			if err != nil {
				return nil, err
			}
		}
		if pullsDone && queueDone {
			for position := 1; position <= len(positions); position++ {
				if !positions[position] {
					return nil, errors.New("incomplete queue positions")
				}
			}
			return roles, nil
		}
	}
	return nil, errors.New("role inventory page limit reached")
}

func sameAttempt(a, b run) bool {
	return a.ID == b.ID && a.Attempt == b.Attempt && a.Head == b.Head && a.Branch == b.Branch
}

func (s *sweeper) sweep(ctx context.Context) error {
	runs, err := s.inventory(ctx)
	if err != nil {
		return err
	}
	if len(runs) == 0 && !s.dryRun {
		fmt.Println("No active merge-group validation runs")
		return nil
	}
	roles, err := s.roles(ctx)
	if err != nil {
		return err
	}
	if s.dryRun {
		fmt.Printf("Dry run: authenticated %d active runs and %d live SHA roles\n", len(runs), len(roles))
	}
	cancelled := 0
	for _, original := range runs {
		if roles[original.Head] {
			continue
		}
		ref, err := s.api.ref(ctx, original.Branch)
		if err != nil {
			return err
		}
		if ref == original.Head {
			continue
		}
		var current run
		path := fmt.Sprintf("repos/%s/actions/runs/%d", s.repo, original.ID)
		if err := s.api.read(ctx, path, &current); err != nil {
			return err
		}
		if !s.owned(current) || !sameAttempt(current, original) {
			return errors.New("run ownership or attempt changed before cancellation")
		}
		if current.Status == "completed" && current.Conclusion != nil && *current.Conclusion != "" {
			continue
		}
		if !active(current.Status) {
			return errors.New("unknown exact-run status before cancellation")
		}
		if current.Conclusion != nil {
			return errors.New("nonterminal run already has a conclusion")
		}
		// Immediately renew the complete roles and exact ref before mutation.
		fresh, err := s.roles(ctx)
		if err != nil {
			return err
		}
		if fresh[current.Head] {
			continue
		}
		ref, err = s.api.ref(ctx, current.Branch)
		if err != nil {
			return err
		}
		if ref == current.Head {
			continue
		}
		if cancelled == maxCancels {
			fmt.Println("Retirement cancellation limit reached; next asynchronous sweep will continue")
			return nil
		}
		var last run
		if err := s.api.read(ctx, path, &last); err != nil {
			return err
		}
		if !s.owned(last) || !sameAttempt(last, current) {
			return errors.New("run attempt changed immediately before cancellation")
		}
		if last.Status == "completed" && last.Conclusion != nil && *last.Conclusion != "" {
			continue
		}
		if !active(last.Status) {
			return errors.New("unknown final run status before cancellation")
		}
		if last.Conclusion != nil {
			return errors.New("nonterminal run acquired a conclusion before cancellation")
		}
		if s.dryRun {
			fmt.Printf("Would retire obsolete merge-group run %d attempt %d\n", last.ID, last.Attempt)
			continue
		}
		if err := s.api.cancel(ctx, current.ID); err != nil {
			return err
		}
		cancelled++
		terminal := false
		for attempt := 0; attempt < 4; attempt++ {
			var observed run
			if err := s.api.read(ctx, path, &observed); err != nil {
				return err
			}
			if !s.owned(observed) || !sameAttempt(observed, current) {
				return errors.New("cancelled run identity changed on readback")
			}
			if observed.Status == "completed" && observed.Conclusion != nil && *observed.Conclusion != "" {
				terminal = true
				fmt.Printf("Retired obsolete merge-group run %d attempt %d: %s\n", observed.ID, observed.Attempt, *observed.Conclusion)
				break
			}
			if !active(observed.Status) {
				return errors.New("unknown cancellation readback state")
			}
			if attempt < 3 {
				if err := s.wait(ctx); err != nil {
					return err
				}
			}
		}
		if !terminal {
			return fmt.Errorf("cancellation requested for run %d but terminal state is not yet verified", current.ID)
		}
	}
	fmt.Printf("Merge-group retirement complete: %d cancellation requests\n", cancelled)
	return nil
}

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Merge-group retirement refused:", err)
		os.Exit(1)
	}
}

func execute() error {
	dryRun := flag.Bool("dry-run", false, "authenticate and report eligible runs without cancelling")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unexpected retirement argument")
	}
	repo, branch := os.Getenv("GITHUB_REPOSITORY"), os.Getenv("DEFAULT_BRANCH")
	event := os.Getenv("GITHUB_EVENT_NAME")
	if !repositoryPattern.MatchString(repo) || branch == "" || os.Getenv("GITHUB_REF") != "refs/heads/"+branch || os.Getenv("GITHUB_WORKFLOW_REF") != repo+"/.github/workflows/ci-run-retirement.yml@refs/heads/"+branch || (event != "push" && event != "schedule" && event != "workflow_dispatch") || os.Getenv("GH_TOKEN") == "" {
		return errors.New("trusted default-branch workflow context required")
	}
	ctx, stop := context.WithTimeout(context.Background(), 120*time.Second)
	defer stop()
	a := &githubAPI{repo: repo, remaining: 180}
	var repository struct {
		ID      int64  `json:"id"`
		Name    string `json:"full_name"`
		Default string `json:"default_branch"`
	}
	if err := a.read(ctx, "repos/"+repo, &repository); err != nil {
		return err
	}
	if repository.ID <= 0 || repository.Name != repo || repository.Default != branch || fmt.Sprint(repository.ID) != os.Getenv("GITHUB_REPOSITORY_ID") {
		return errors.New("repository identity does not match trusted workflow")
	}
	var workflow struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Path  string `json:"path"`
		State string `json:"state"`
	}
	if err := a.read(ctx, "repos/"+repo+"/actions/workflows/ci-merge-group.yml", &workflow); err != nil {
		return err
	}
	if workflow.ID <= 0 || workflow.Name != "trusted-merge-ci" || workflow.Path != workflowPath || workflow.State != "active" {
		return errors.New("merge-group workflow identity mismatch")
	}
	s := sweeper{api: a, repo: repo, repoID: repository.ID, branch: branch, workflowID: workflow.ID, dryRun: *dryRun, wait: func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			return nil
		}
	}}
	return s.sweep(ctx)
}
