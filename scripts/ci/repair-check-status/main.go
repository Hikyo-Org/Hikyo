// repair-check-status normalizes one already successful native job's status.
// It cannot create a verdict, run validation, or repair an unfinished job.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strconv"
	"time"
)

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type api interface {
	read(context.Context, string, any) error
	patch(context.Context, string, any, any) error
}

type githubAPI struct{ remaining int }

func (a *githubAPI) command(ctx context.Context, payload any, args ...string) ([]byte, error) {
	if a.remaining <= 0 {
		return nil, errors.New("check-status repair API budget exhausted")
	}
	a.remaining--
	cmd := exec.CommandContext(ctx, "gh", append([]string{"api", "--hostname", "github.com"}, args...)...)
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		cmd.Stdin = bytes.NewReader(data)
	}
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("check-status repair API request failed: %w; no mutation retry", err)
	}
	if len(data) > 8<<20 {
		return nil, errors.New("check-status repair response exceeds bound")
	}
	return data, nil
}

func (a *githubAPI) read(ctx context.Context, path string, out any) error {
	data, err := a.command(ctx, nil, path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (a *githubAPI) patch(ctx context.Context, path string, payload, out any) error {
	data, err := a.command(ctx, payload, "--method", "PATCH", path, "--input", "-")
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

type repository struct {
	ID      int64  `json:"id"`
	Name    string `json:"full_name"`
	Default string `json:"default_branch"`
}
type check struct {
	Document   map[string]any `json:"-"`
	ID         int64          `json:"id"`
	Node       string         `json:"node_id"`
	Name       string         `json:"name"`
	Head       string         `json:"head_sha"`
	External   string         `json:"external_id"`
	URL        string         `json:"url"`
	HTML       string         `json:"html_url"`
	Details    string         `json:"details_url"`
	Status     string         `json:"status"`
	Conclusion string         `json:"conclusion"`
	Started    string         `json:"started_at"`
	Completed  string         `json:"completed_at"`
	Output     any            `json:"output"`
	Suite      struct {
		ID int64 `json:"id"`
	} `json:"check_suite"`
	App struct {
		ID   int64  `json:"id"`
		Slug string `json:"slug"`
	} `json:"app"`
}

// Retain the complete response document so unexpected metadata changes cannot
// hide behind a projection of the fields used for identity authentication.
func (c *check) UnmarshalJSON(data []byte) error {
	type fields check
	var decoded fields
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	*c = check(decoded)
	c.Document = document
	return nil
}

type job struct {
	ID         int64  `json:"id"`
	Node       string `json:"node_id"`
	Name       string `json:"name"`
	Run        int64  `json:"run_id"`
	Attempt    int    `json:"run_attempt"`
	Workflow   string `json:"workflow_name"`
	Head       string `json:"head_sha"`
	URL        string `json:"url"`
	HTML       string `json:"html_url"`
	CheckURL   string `json:"check_run_url"`
	RunURL     string `json:"run_url"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Started    string `json:"started_at"`
	Completed  string `json:"completed_at"`
	Steps      []struct {
		Number     int    `json:"number"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	} `json:"steps"`
}
type run struct {
	ID         int64      `json:"id"`
	Attempt    int        `json:"run_attempt"`
	WorkflowID int64      `json:"workflow_id"`
	Name       string     `json:"name"`
	Path       string     `json:"path"`
	Event      string     `json:"event"`
	Head       string     `json:"head_sha"`
	Status     string     `json:"status"`
	Conclusion string     `json:"conclusion"`
	Suite      int64      `json:"check_suite_id"`
	SuiteURL   string     `json:"check_suite_url"`
	Repo       repository `json:"repository"`
	HeadRepo   repository `json:"head_repository"`
}
type snapshot struct {
	Main  string
	Check check
	Job   job
	Run   run
}
type repairer struct {
	api  api
	repo repository
}

func (r *repairer) endpoint(path string) string { return "repos/" + r.repo.Name + "/" + path }

func (r *repairer) snapshot(ctx context.Context, id int64) (snapshot, error) {
	var s snapshot
	if id <= 0 || r.repo.ID <= 0 || !repoPattern.MatchString(r.repo.Name) || r.repo.Default != "main" {
		return s, errors.New("repair requires an authenticated repository, main branch and exact check ID")
	}
	if err := r.api.read(ctx, r.endpoint(fmt.Sprintf("check-runs/%d", id)), &s.Check); err != nil {
		return s, err
	}
	c := s.Check
	apiURL := "https://api.github.com/" + r.endpoint("")
	if c.ID != id || c.Node == "" || c.Name != "validation / changes" || !shaPattern.MatchString(c.Head) || !uuidPattern.MatchString(c.External) || c.App.ID != 15368 || c.App.Slug != "github-actions" || c.Suite.ID <= 0 || c.URL != apiURL+fmt.Sprintf("check-runs/%d", id) || (c.Status != "in_progress" && c.Status != "completed") || c.Conclusion != "success" {
		return s, errors.New("check is not an authenticated already-successful native changes job")
	}
	if err := r.api.read(ctx, r.endpoint(fmt.Sprintf("actions/jobs/%d", id)), &s.Job); err != nil {
		return s, err
	}
	j := s.Job
	html := fmt.Sprintf("https://github.com/%s/actions/runs/%d/job/%d", r.repo.Name, j.Run, id)
	if j.ID != id || j.Node != c.Node || j.Name != c.Name || j.Run <= 0 || j.Attempt <= 0 || j.Workflow != "trusted-ci" || j.Head != c.Head || j.URL != apiURL+fmt.Sprintf("actions/jobs/%d", id) || j.CheckURL != c.URL || j.RunURL != apiURL+fmt.Sprintf("actions/runs/%d", j.Run) || j.HTML != html || c.HTML != html || c.Details != html || (j.Status != "in_progress" && j.Status != "completed") || j.Conclusion != "success" || j.Started != c.Started || j.Completed != c.Completed {
		return s, errors.New("native job identity or existing verdict disagrees with check")
	}
	start, startErr := time.Parse(time.RFC3339, c.Started)
	end, endErr := time.Parse(time.RFC3339, c.Completed)
	if startErr != nil || endErr != nil || start.IsZero() || end.IsZero() || end.Before(start) || len(j.Steps) == 0 {
		return s, errors.New("job lacks authenticated completion timestamps or steps")
	}
	seen := map[int]bool{}
	for _, step := range j.Steps {
		if step.Number <= 0 || seen[step.Number] || step.Status != "completed" || (step.Conclusion != "success" && step.Conclusion != "skipped") {
			return s, errors.New("job contains unfinished, ambiguous or failed steps")
		}
		seen[step.Number] = true
	}
	if err := r.api.read(ctx, r.endpoint(fmt.Sprintf("actions/runs/%d", j.Run)), &s.Run); err != nil {
		return s, err
	}
	v := s.Run
	if v.ID != j.Run || v.Attempt != j.Attempt || v.WorkflowID <= 0 || v.Name != j.Workflow || v.Path != ".github/workflows/ci-control.yml" || v.Event != "pull_request_target" || v.Head != c.Head || v.Status != "completed" || v.Conclusion != "success" || v.Suite != c.Suite.ID || v.SuiteURL != apiURL+fmt.Sprintf("check-suites/%d", c.Suite.ID) || v.Repo.ID != r.repo.ID || v.Repo.Name != r.repo.Name || v.HeadRepo.ID != r.repo.ID || v.HeadRepo.Name != r.repo.Name {
		return s, errors.New("owning run is not the authenticated successful historical workflow attempt")
	}
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	if err := r.api.read(ctx, r.endpoint("git/ref/heads/main"), &ref); err != nil {
		return s, err
	}
	if ref.Ref != "refs/heads/main" || ref.Object.Type != "commit" || !shaPattern.MatchString(ref.Object.SHA) {
		return s, errors.New("current main identity is incomplete")
	}
	s.Main = ref.Object.SHA
	var comparison struct {
		Status string `json:"status"`
		Base   struct {
			SHA string `json:"sha"`
		} `json:"base_commit"`
		MergeBase struct {
			SHA string `json:"sha"`
		} `json:"merge_base_commit"`
	}
	if err := r.api.read(ctx, r.endpoint("compare/"+c.Head+"..."+s.Main+"?per_page=1"), &comparison); err != nil {
		return s, err
	}
	if (comparison.Status != "ahead" && comparison.Status != "identical") || comparison.Base.SHA != c.Head || comparison.MergeBase.SHA != c.Head {
		return s, errors.New("check head is not proven current-main ancestry")
	}
	return s, nil
}

func (r *repairer) repair(ctx context.Context, id int64, dry bool) error {
	before, err := r.snapshot(ctx, id)
	if err != nil {
		return err
	}
	if before.Check.Status == "completed" {
		fmt.Printf("Check %d already completed; existing success preserved.\n", id)
		return nil
	}
	fresh, err := r.snapshot(ctx, id)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(before, fresh) {
		return errors.New("repair authority changed before mutation; retry only a new independent inspection")
	}
	if dry {
		fmt.Printf("DRY RUN check %d: native job/run already successful, main ancestry authenticated; status-only normalization eligible.\n", id)
		return nil
	}
	path := r.endpoint(fmt.Sprintf("check-runs/%d", id))
	expected := before.Check
	expected.Status = "completed"
	expected.Document = make(map[string]any, len(before.Check.Document))
	for key, value := range before.Check.Document {
		expected.Document[key] = value
	}
	expected.Document["status"] = "completed"
	var written check
	// The Checks API requires conclusion when status is completed. Resend only
	// the independently authenticated existing verdict and original timestamp.
	payload := map[string]string{"status": "completed", "conclusion": before.Check.Conclusion, "completed_at": before.Check.Completed}
	if err := r.api.patch(ctx, path, payload, &written); err != nil {
		return err
	}
	if !reflect.DeepEqual(written, expected) {
		return errors.New("status-only repair response changed identity or existing verdict/output")
	}
	var readback check
	if err := r.api.read(ctx, path, &readback); err != nil {
		return err
	}
	if !reflect.DeepEqual(readback, expected) {
		return errors.New("status-only repair readback changed identity or existing verdict/output")
	}
	fmt.Printf("Check %d status completed; original success, timestamps, provenance and output verified unchanged.\n", id)
	return nil
}

func execute() error {
	id := flag.Int64("check-id", 0, "exact native check ID to inspect")
	dry := flag.Bool("dry-run", false, "authenticate eligibility without changing status")
	flag.Parse()
	repo := os.Getenv("GITHUB_REPOSITORY")
	if flag.NArg() != 0 || !repoPattern.MatchString(repo) || os.Getenv("GH_TOKEN") == "" {
		return errors.New("authenticated repository and exact check ID required")
	}
	if !*dry && (os.Getenv("GITHUB_EVENT_NAME") != "workflow_dispatch" || os.Getenv("GITHUB_REF") != "refs/heads/main" || os.Getenv("GITHUB_WORKFLOW_REF") != repo+"/.github/workflows/ci-check-status-repair.yml@refs/heads/main") {
		return errors.New("mutation requires the trusted main manual status-repair workflow")
	}
	ctx, stop := context.WithTimeout(context.Background(), 90*time.Second)
	defer stop()
	a := &githubAPI{remaining: 20}
	var repository repository
	if err := a.read(ctx, "repos/"+repo, &repository); err != nil {
		return err
	}
	if repository.Name != repo || repository.ID <= 0 || (!*dry && os.Getenv("GITHUB_REPOSITORY_ID") == "") || (os.Getenv("GITHUB_REPOSITORY_ID") != "" && os.Getenv("GITHUB_REPOSITORY_ID") != strconv.FormatInt(repository.ID, 10)) {
		return errors.New("repository identity mismatch")
	}
	return (&repairer{api: a, repo: repository}).repair(ctx, *id, *dry)
}

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Native check status repair refused:", err)
		os.Exit(1)
	}
}
