// Command queue-report publishes informational merge queue diagnostics on the
// originating PR head. Run only from trusted default-branch tooling.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const checkName = "Merge queue validation"
const sourcePath = ".github/workflows/ci-merge-group.yml"
const actionsAppID = 15368
const maxPages = 10

func reportName(r run) string {
	return fmt.Sprintf("%s (run %d, attempt %d)", checkName, r.ID, r.Attempt)
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type repository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
}
type run struct {
	ID             int64      `json:"id"`
	Attempt        int        `json:"run_attempt"`
	WorkflowID     int64      `json:"workflow_id"`
	Name           string     `json:"name"`
	Path           string     `json:"path"`
	Event          string     `json:"event"`
	HeadSHA        string     `json:"head_sha"`
	HeadBranch     string     `json:"head_branch"`
	Status         string     `json:"status"`
	Conclusion     string     `json:"conclusion"`
	Repository     repository `json:"repository"`
	HeadRepository repository `json:"head_repository"`
}
type event struct {
	Action     string     `json:"action"`
	Repository repository `json:"repository"`
	Run        run        `json:"workflow_run"`
}
type check struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	HeadSHA    string `json:"head_sha"`
	ExternalID string `json:"external_id"`
	DetailsURL string `json:"details_url"`
	Status     string `json:"status"`
	App        struct {
		ID int64 `json:"id"`
	} `json:"app"`
}
type job struct {
	ID         int64  `json:"id"`
	RunID      int64  `json:"run_id"`
	Attempt    int    `json:"run_attempt"`
	HeadSHA    string `json:"head_sha"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Steps      []struct {
		Name       string `json:"name"`
		Number     int    `json:"number"`
		Conclusion string `json:"conclusion"`
	} `json:"steps"`
}

type client struct {
	http              *http.Client
	base, token, repo string
}

func (c client) request(ctx context.Context, method, path string, body, result any) error {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
	}
	endpoint := c.base + "/repos/" + c.repo
	if path != "" {
		endpoint += "/" + path
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GitHub %s %s: HTTP %d", method, path, resp.StatusCode)
	}
	if result == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(result)
}
func (c client) read(ctx context.Context, path string, result any) error {
	return c.request(ctx, http.MethodGet, path, nil, result)
}

func (c client) publish(ctx context.Context, e event) error {
	if e.Action != "requested" && e.Action != "in_progress" && e.Action != "completed" {
		return errors.New("unsupported workflow_run action")
	}
	if !repoPattern.MatchString(c.repo) || e.Run.ID <= 0 || e.Run.Attempt <= 0 {
		return errors.New("invalid repository or source run identity")
	}
	var repo repository
	if err := c.read(ctx, "", &repo); err != nil {
		return err
	}
	if repo.ID <= 0 || repo.ID != e.Repository.ID || repo.ID != e.Run.Repository.ID || !strings.EqualFold(repo.FullName, c.repo) || !strings.EqualFold(e.Repository.FullName, c.repo) || repo.DefaultBranch == "" {
		return errors.New("callback repository mismatch")
	}
	var workflow struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := c.read(ctx, "actions/workflows/ci-merge-group.yml", &workflow); err != nil {
		return err
	}
	var r run
	runPath := fmt.Sprintf("actions/runs/%d/attempts/%d", e.Run.ID, e.Run.Attempt)
	if err := c.read(ctx, runPath, &r); err != nil {
		return err
	}
	if workflow.ID <= 0 || workflow.Name != "trusted-merge-ci" || workflow.Path != sourcePath || r.WorkflowID != workflow.ID || r.Path != sourcePath || r.Name != workflow.Name || r.Event != "merge_group" || r.ID != e.Run.ID || r.Attempt != e.Run.Attempt || r.Repository.ID != repo.ID || r.HeadRepository.ID != repo.ID || !strings.EqualFold(r.Repository.FullName, c.repo) || !strings.EqualFold(r.HeadRepository.FullName, c.repo) || !shaPattern.MatchString(r.HeadSHA) || r.HeadSHA != e.Run.HeadSHA || r.HeadBranch != e.Run.HeadBranch {
		return errors.New("source workflow run identity mismatch")
	}
	// GitHub's single-member queue branch records its immutable tested base.
	// Authenticate that base and the PR head against the actual merge parents.
	branch := regexp.MustCompile(`^gh-readonly-queue/` + regexp.QuoteMeta(repo.DefaultBranch) + `/pr-([1-9][0-9]*)-([0-9a-f]{40})$`).FindStringSubmatch(r.HeadBranch)
	if branch == nil {
		return errors.New("unsupported merge queue branch identity")
	}
	prNumber, err := strconv.Atoi(branch[1])
	if err != nil {
		return err
	}
	base := branch[2]
	var commit struct {
		SHA     string `json:"sha"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if err := c.read(ctx, "git/commits/"+r.HeadSHA, &commit); err != nil {
		return err
	}
	if commit.SHA != r.HeadSHA || len(commit.Parents) != 2 || commit.Parents[0].SHA != base || !shaPattern.MatchString(commit.Parents[1].SHA) || commit.Parents[1].SHA == base {
		return errors.New("unsupported merge topology: expected a single PR with the recorded base and head as parents")
	}
	head := commit.Parents[1].SHA
	runURL := fmt.Sprintf("https://github.com/%s/actions/runs/%d/attempts/%d", c.repo, r.ID, r.Attempt)
	external := fmt.Sprintf("hikyo-queue-report:v1:%d:%d:%d:%d:%s:%s", repo.ID, prNumber, r.ID, r.Attempt, r.HeadSHA, base)
	name := reportName(r)
	existing, err := c.existing(ctx, head, name, external, runURL)
	if err != nil {
		return err
	}
	var pr struct {
		Number int `json:"number"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref  string     `json:"ref"`
			Repo repository `json:"repo"`
		} `json:"base"`
	}
	if err := c.read(ctx, fmt.Sprintf("pulls/%d", prNumber), &pr); err != nil {
		return err
	}
	if pr.Number != prNumber || pr.Base.Repo.ID != repo.ID || pr.Base.Ref != repo.DefaultBranch {
		return errors.New("originating PR identity mismatch")
	}
	// A previous authenticated publication is the immutable binding for a late
	// callback after a force push. Never create a report on the new PR head.
	if pr.Head.SHA != head && existing == nil {
		return errors.New("PR head changed before membership was captured; no report can be safely created")
	}
	if existing != nil && existing.Status == "completed" {
		return nil
	}
	jobs, err := c.jobs(ctx, r)
	if err != nil {
		return err
	}
	status, conclusion, err := checkState(r)
	if err != nil {
		return err
	}
	summary := render(r, prNumber, base, head, runURL, jobs, pr.Head.SHA != head)
	body := map[string]any{
		"name": name, "details_url": runURL, "external_id": external,
		"status": status, "output": map[string]string{"title": fmt.Sprintf("PR #%d: queue run %d, attempt %d (%s)", prNumber, r.ID, r.Attempt, status), "summary": summary},
	}
	if conclusion != "" {
		body["conclusion"] = conclusion
	}
	if existing != nil {
		// Only the exact check ID authenticated above may be changed.
		return c.request(ctx, http.MethodPatch, fmt.Sprintf("check-runs/%d", existing.ID), body, nil)
	}
	body["head_sha"] = head
	return c.request(ctx, http.MethodPost, "check-runs", body, nil)
}

func (c client) existing(ctx context.Context, head, name, external, details string) (*check, error) {
	var found *check
	total, seenCount := -1, 0
	seen := map[int64]bool{}
	for page := 1; page <= maxPages; page++ {
		var result struct {
			Checks []check `json:"check_runs"`
			Total  *int    `json:"total_count"`
		}
		path := fmt.Sprintf("commits/%s/check-runs?check_name=%s&filter=all&per_page=100&page=%d", head, url.QueryEscape(name), page)
		if err := c.read(ctx, path, &result); err != nil {
			return nil, err
		}
		if result.Checks == nil || !validPage(result.Total, &total, seenCount, len(result.Checks)) {
			return nil, errors.New("incomplete or inconsistent queue report pagination")
		}
		for _, candidate := range result.Checks {
			if candidate.ID <= 0 || candidate.Name != name || candidate.HeadSHA != head || candidate.App.ID <= 0 || seen[candidate.ID] {
				return nil, errors.New("invalid or duplicate queue report identity")
			}
			seen[candidate.ID] = true
			if candidate.ExternalID != external {
				continue
			}
			if candidate.ID <= 0 || candidate.Name != name || candidate.HeadSHA != head || candidate.App.ID != actionsAppID || candidate.DetailsURL != details || found != nil {
				return nil, errors.New("ambiguous or unauthenticated existing queue report")
			}
			found = &candidate
		}
		seenCount += len(result.Checks)
		if seenCount == total {
			return found, nil
		}
	}
	return nil, errors.New("queue report pagination limit reached")
}

func (c client) jobs(ctx context.Context, r run) ([]job, error) {
	var jobs []job
	seen := map[int64]bool{}
	total := -1
	for page := 1; page <= maxPages; page++ {
		var result struct {
			Jobs  []job `json:"jobs"`
			Total *int  `json:"total_count"`
		}
		path := fmt.Sprintf("actions/runs/%d/attempts/%d/jobs?per_page=100&page=%d", r.ID, r.Attempt, page)
		if err := c.read(ctx, path, &result); err != nil {
			return nil, err
		}
		if result.Jobs == nil || !validPage(result.Total, &total, len(jobs), len(result.Jobs)) {
			return nil, errors.New("incomplete or inconsistent job pagination")
		}
		for _, j := range result.Jobs {
			if j.ID <= 0 || j.RunID != r.ID || j.Attempt != r.Attempt || j.HeadSHA != r.HeadSHA || seen[j.ID] {
				return nil, errors.New("job source identity mismatch")
			}
			seen[j.ID] = true
			jobs = append(jobs, j)
		}
		if len(jobs) == total {
			return jobs, nil
		}
	}
	return nil, errors.New("job pagination limit reached")
}

func validPage(count *int, total *int, consumed, length int) bool {
	if count == nil || *count < 0 || *count > maxPages*100 {
		return false
	}
	if *total == -1 {
		*total = *count
	}
	return *count == *total && consumed <= *total && length == min(100, *total-consumed)
}

func checkState(r run) (string, string, error) {
	if r.Status != "completed" {
		switch r.Status {
		case "queued", "requested", "waiting", "pending":
			return "queued", "", nil
		case "in_progress":
			return "in_progress", "", nil
		}
		return "", "", errors.New("unsupported source run status")
	}
	switch r.Conclusion {
	case "success", "failure", "neutral", "cancelled", "timed_out", "action_required", "skipped", "stale":
		return "completed", r.Conclusion, nil
	}
	return "", "", errors.New("unsupported source run conclusion")
}

func label(s string) string {
	if len(s) > 250 {
		s = string([]rune(s)[:min(200, len([]rune(s)))]) + "..."
	}
	return strings.NewReplacer("\\", "\\\\", "`", "\\`", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "\n", " ", "\r", " ", "*", "\\*", "_", "\\_").Replace(s)
}
func render(r run, pr int, base, head, runURL string, jobs []job, superseded bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Informational queue result for PR #%d. This describes only the tested base and merge commit below; it is not reusable merge approval.\n\n", pr)
	if superseded {
		b.WriteString("The PR has a newer head. This report is retained on the original tested head.\n\n")
	}
	fmt.Fprintf(&b, "- PR head: `%s`\n- Tested base: `%s`\n- Tested merge: `%s`\n- [Run %d, attempt %d](%s): %s %s\n- [Run artifacts](https://github.com/%s/actions/runs/%d#artifacts) (shared across attempts)\n\n", head, base, r.HeadSHA, r.ID, r.Attempt, runURL, label(r.Status), label(r.Conclusion), strings.Split(strings.TrimPrefix(runURL, "https://github.com/"), "/actions/")[0], r.ID)
	b.WriteString("Job results (skipped jobs were not executed; selection reasons are in the planning job):\n\n")
	if len(jobs) == 0 {
		b.WriteString("No jobs have been reported yet.\n")
	}
	for _, j := range jobs {
		if b.Len() > 50000 {
			b.WriteString("\nAdditional jobs omitted; open the source run for the complete list.\n")
			break
		}
		jobURL := fmt.Sprintf("%s/job/%d", strings.Split(runURL, "/attempts/")[0], j.ID)
		fmt.Fprintf(&b, "- [%s](%s): %s %s\n", label(j.Name), jobURL, label(j.Status), label(j.Conclusion))
		for _, step := range j.Steps {
			if step.Conclusion != "failure" && step.Conclusion != "timed_out" && step.Conclusion != "cancelled" {
				continue
			}
			if b.Len() > 50000 {
				break
			}
			fmt.Fprintf(&b, "  - [%s](%s#step:%d:1): %s\n", label(step.Name), jobURL, step.Number, label(step.Conclusion))
		}
	}
	return b.String()
}

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, "queue-report:", err)
		os.Exit(1)
	}
}
func execute() error {
	if os.Getenv("GITHUB_EVENT_NAME") != "workflow_run" {
		return errors.New("requires workflow_run event")
	}
	data, err := os.ReadFile(os.Getenv("GITHUB_EVENT_PATH"))
	if err != nil {
		return err
	}
	var e event
	if err := json.Unmarshal(data, &e); err != nil {
		return err
	}
	token, repo := os.Getenv("GH_TOKEN"), os.Getenv("GH_REPO")
	if token == "" {
		return errors.New("GH_TOKEN is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	c := client{http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("unexpected GitHub API redirect") }}, base: "https://api.github.com", token: token, repo: repo}
	return c.publish(ctx, e)
}
