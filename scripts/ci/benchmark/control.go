package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const botID = 41898282 // GitHub's github-actions[bot], not a display name.
const marker = "<!-- hikyo-benchmark "
const checkbox = "- [x] Run benchmark"
const creditCheckbox = "- [x] Use confirmed credit"

type pull struct {
	Number int    `json:"number"`
	State  string `json:"state"`
	Head   struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		SHA string `json:"sha"`
	} `json:"base"`
}

type comment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User struct {
		ID int64 `json:"id"`
	} `json:"user"`
}

type controlEvent struct {
	Action string `json:"action"`
	Number int    `json:"number"`
	Pull   pull   `json:"pull_request"`
	Issue  struct {
		Number int `json:"number"`
		Pull   *struct {
			URL string `json:"url"`
		} `json:"pull_request"`
	} `json:"issue"`
	Comment comment `json:"comment"`
	Sender  struct {
		Login string `json:"login"`
	} `json:"sender"`
	Changes struct {
		Body *struct {
			From string `json:"from"`
		} `json:"body"`
	} `json:"changes"`
	WorkflowRun benchmarkRun `json:"workflow_run"`
}

type request struct {
	head    string
	run     int64
	attempt int
}

var requestPattern = regexp.MustCompile(`^<!-- hikyo-benchmark head=([0-9a-f]{40}) run=([0-9]+) attempt=([0-9]+) -->\n`)

func parseRequest(body string) (request, error) {
	parts := requestPattern.FindStringSubmatch(body)
	if parts == nil {
		return request{}, fmt.Errorf("invalid benchmark comment identity")
	}
	runID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return request{}, err
	}
	attempt, err := strconv.Atoi(parts[3])
	if err != nil {
		return request{}, err
	}
	return request{head: parts[1], run: runID, attempt: attempt}, nil
}

func checked(body, line string) bool {
	for _, candidate := range strings.Split(body, "\n") {
		if candidate == line {
			return true
		}
	}
	return false
}

func readAPI(path string, target any) error {
	command := exec.Command("gh", "api", path)
	command.Stderr = os.Stderr
	raw, err := command.Output()
	if err != nil {
		return fmt.Errorf("GitHub read %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("GitHub decode %s: %w", path, err)
	}
	return nil
}

func writeAPI(method, path, body string) error {
	args := []string{"api", "--method", method, path}
	if body != "" {
		args = append(args, "--field", "body="+body)
	}
	command := exec.Command("gh", args...)
	command.Stderr = os.Stderr
	if _, err := command.Output(); err != nil {
		return fmt.Errorf("GitHub write %s: %w", path, err)
	}
	return nil
}

func currentPull(repository string, number int) (pull, error) {
	var pr pull
	if number <= 0 {
		return pr, fmt.Errorf("invalid PR number")
	}
	if err := readAPI(fmt.Sprintf("repos/%s/pulls/%d", repository, number), &pr); err != nil {
		return pr, err
	}
	if pr.Number != number || (pr.State != "open" && pr.State != "closed") || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(pr.Head.SHA) {
		return pr, fmt.Errorf("invalid live PR identity")
	}
	return pr, nil
}

func botComment(repository string, number int) (comment, error) {
	var pages [][]comment
	if err := api(fmt.Sprintf("repos/%s/issues/%d/comments?per_page=100", repository, number), &pages); err != nil {
		return comment{}, err
	}
	if len(pages) == 0 {
		return comment{}, fmt.Errorf("GitHub returned no comment-history pages")
	}
	var found comment
	for _, page := range pages {
		if page == nil {
			return comment{}, fmt.Errorf("GitHub returned incomplete comment history")
		}
		for _, c := range page {
			if c.User.ID == botID && strings.HasPrefix(c.Body, marker) {
				if found.ID != 0 {
					return comment{}, fmt.Errorf("multiple benchmark bot comments on PR %d", number)
				}
				found = c
			}
		}
	}
	return found, nil
}

func commentBody(pr pull, r request, status string) string {
	return fmt.Sprintf("<!-- hikyo-benchmark head=%s run=%d attempt=%d -->\n### Performance: PR #%d\n\nHead `%s`.\n\n%s\n\n- [ ] Run benchmark\n- [ ] Use confirmed credit\n\nA maintainer can tick **Run benchmark** to run the Go suite with CodSpeed walltime reporting and the large-matrix browser check for this head. Go comparisons use the available daily main baseline on the same Graviton hardware. Browser checks run separately on GitHub-hosted Chromium and retain timing samples as run artifacts. This is optional and does not replace required CI. One walltime request runs at a time.\n\nNormal requests share the 600-minute allowance with the daily main run. Tick **Use confirmed credit** first to permit up to 156 extra minutes (about $5) over the last 32 days; account credit must be confirmed by an administrator.\n", r.head, r.run, r.attempt, pr.Number, r.head[:12], status)
}

func updateComment(repository string, c comment, body string) error {
	if c.ID <= 0 || c.User.ID != botID {
		return fmt.Errorf("refusing to edit a non-bot benchmark comment")
	}
	return writeAPI("PATCH", fmt.Sprintf("repos/%s/issues/comments/%d", repository, c.ID), body)
}

func prepare(repository string, e controlEvent) error {
	pr, err := currentPull(repository, e.Number)
	if err != nil {
		return err
	}
	if pr.State != "open" {
		return nil
	}
	c, err := botComment(repository, pr.Number)
	if err != nil {
		return err
	}
	if c.ID != 0 {
		r, err := parseRequest(c.Body)
		if err == nil && r.head == pr.Head.SHA {
			return nil
		}
	}
	body := commentBody(pr, request{head: pr.Head.SHA}, "_Not run for this head._")
	if c.ID == 0 {
		return writeAPI("POST", fmt.Sprintf("repos/%s/issues/%d/comments", repository, pr.Number), body)
	}
	return updateComment(repository, c, body)
}

func authorizedRequest(repository string, e controlEvent) (pull, comment, bool, error) {
	if e.Action != "edited" || e.Issue.Pull == nil || e.Comment.User.ID != botID || e.Changes.Body == nil ||
		checked(e.Changes.Body.From, checkbox) || !checked(e.Comment.Body, checkbox) {
		return pull{}, comment{}, false, nil
	}
	if !regexp.MustCompile(`^[A-Za-z0-9-]+$`).MatchString(e.Sender.Login) {
		return pull{}, comment{}, false, fmt.Errorf("invalid request actor")
	}
	var permission struct {
		Permission string `json:"permission"`
	}
	if err := readAPI("repos/"+repository+"/collaborators/"+e.Sender.Login+"/permission", &permission); err != nil {
		return pull{}, comment{}, false, err
	}
	if permission.Permission != "admin" && permission.Permission != "maintain" && permission.Permission != "write" {
		return pull{}, comment{}, false, fmt.Errorf("benchmark requests require maintainer permission")
	}
	pr, err := currentPull(repository, e.Issue.Number)
	if err != nil {
		return pr, comment{}, false, err
	}
	c, err := botComment(repository, pr.Number)
	if err != nil {
		return pr, c, false, err
	}
	if c.ID != e.Comment.ID || c.Body != e.Comment.Body || pr.State != "open" {
		return pr, c, false, fmt.Errorf("benchmark request changed or PR closed while queued")
	}
	r, err := parseRequest(c.Body)
	if err != nil || r.head != pr.Head.SHA {
		return pr, c, false, fmt.Errorf("benchmark request is for a stale PR head")
	}
	return pr, c, true, nil
}

type benchmarkRun struct {
	ID         int64  `json:"id"`
	Head       string `json:"head_sha"`
	Title      string `json:"display_title"`
	Event      string `json:"event"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Attempt    int    `json:"run_attempt"`
}

func completedMeasurement(repository string, r benchmarkRun) error {
	var pages []struct {
		Jobs []job `json:"jobs"`
	}
	if err := api(fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100", repository, r.ID, r.Attempt), &pages); err != nil {
		return err
	}
	for _, page := range pages {
		for _, j := range page.Jobs {
			for _, label := range j.Labels {
				if label == macroLabel && j.Status == "completed" && j.Conclusion == "success" && j.RunnerName != "" {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("the workflow did not complete an assigned walltime measurement")
}

func report(repository string, r benchmarkRun) error {
	if r.ID <= 0 || r.Attempt <= 0 || r.Event != "pull_request" || r.Status != "completed" {
		return fmt.Errorf("invalid benchmark completion identity")
	}
	parts := regexp.MustCompile(`^pr-benchmark #([1-9][0-9]*)$`).FindStringSubmatch(r.Title)
	if parts == nil {
		return fmt.Errorf("invalid PR benchmark run title")
	}
	number, err := strconv.Atoi(parts[1])
	if err != nil {
		return err
	}
	pr, err := currentPull(repository, number)
	if err != nil {
		return err
	}
	if pr.State != "open" || pr.Head.SHA != r.Head {
		return nil
	}
	c, err := botComment(repository, number)
	if err != nil {
		return err
	}
	if c.ID == 0 {
		return nil
	}
	identity, err := parseRequest(c.Body)
	if err != nil {
		return err
	}
	if identity != (request{head: r.Head, run: r.ID, attempt: r.Attempt}) {
		return nil
	}
	conclusion := r.Conclusion
	var measurementError error
	if conclusion == "success" {
		if err := completedMeasurement(repository, r); err != nil {
			measurementError = err
			conclusion = "not measured"
		}
	}
	link := fmt.Sprintf("https://github.com/%s/actions/runs/%d/attempts/%d", repository, r.ID, r.Attempt)
	if err := updateComment(repository, c, commentBody(pr, identity, "Benchmark **"+conclusion+"**. [Results and CodSpeed upload]("+link+").")); err != nil {
		return err
	}
	return measurementError
}

func findRun(repository string, pr pull) (benchmarkRun, error) {
	var pages []struct {
		Runs []benchmarkRun `json:"workflow_runs"`
	}
	path := fmt.Sprintf("repos/%s/actions/workflows/pr-benchmark.yml/runs?event=pull_request&head_sha=%s&per_page=100", repository, pr.Head.SHA)
	if err := api(path, &pages); err != nil {
		return benchmarkRun{}, err
	}
	var found benchmarkRun
	for _, page := range pages {
		for _, r := range page.Runs {
			if r.Event == "pull_request" && r.Head == pr.Head.SHA && r.Title == fmt.Sprintf("pr-benchmark #%d", pr.Number) && r.ID > found.ID {
				found = r
			}
		}
	}
	if found.ID == 0 || found.Attempt < 1 {
		return found, fmt.Errorf("no exact-head PR benchmark workflow exists yet; wait for its initial check")
	}
	if found.Status != "completed" {
		return found, fmt.Errorf("the exact-head PR benchmark workflow is still running")
	}
	return found, nil
}

func checkWorkflow(repository string, pr pull) error {
	// A PR cannot redefine the job that receives runner admission. The workflow
	// must be byte-identical to main, even if the rest of the PR changes CI.
	for _, workflow := range []string{"pr-benchmark.yml", "matrix-performance.yml"} {
		var trusted, candidate struct {
			SHA string `json:"sha"`
		}
		path := "repos/" + repository + "/contents/.github/workflows/" + workflow + "?ref="
		if err := readAPI(path+"main", &trusted); err != nil {
			return err
		}
		if err := readAPI(path+pr.Head.SHA, &candidate); err != nil {
			return err
		}
		if trusted.SHA == "" || trusted.SHA != candidate.SHA {
			return fmt.Errorf("PR benchmark workflow %s must match main; merge workflow changes before using paid PR benchmarks", workflow)
		}
	}
	return nil
}

func requestBenchmark(repository string, e controlEvent) error {
	pr, c, requested, err := authorizedRequest(repository, e)
	if err != nil || !requested {
		return err
	}
	decline := func(reason error) error {
		latest, err := currentPull(repository, pr.Number)
		if err != nil {
			return err
		}
		if latest.State != "open" || latest.Head.SHA != pr.Head.SHA {
			return reason
		}
		current, err := botComment(repository, pr.Number)
		if err != nil {
			return err
		}
		identity, err := parseRequest(current.Body)
		if err != nil || current.ID != c.ID || identity.head != pr.Head.SHA {
			return reason
		}
		body := commentBody(pr, request{head: pr.Head.SHA}, "Benchmark declined: "+reason.Error())
		if err := updateComment(repository, current, body); err != nil {
			return err
		}
		return reason
	}
	if err := checkWorkflow(repository, pr); err != nil {
		return decline(err)
	}
	run, err := findRun(repository, pr)
	if err != nil {
		return decline(err)
	}
	credit := checked(c.Body, creditCheckbox)
	if credit && os.Getenv("CODSPEED_CREDIT_CONFIRMED") != "true" {
		return decline(fmt.Errorf("CodSpeed credit has not been confirmed for this account"))
	}
	u, err := collect(repository, time.Now().UTC(), 0)
	if err != nil {
		return decline(err)
	}
	allowed, reason := admit(u, "workflow_dispatch", credit)
	if !allowed {
		return decline(fmt.Errorf("%s (daily %d min; requested %d min)", reason, u.automatic, u.manual))
	}
	r := request{head: pr.Head.SHA, run: run.ID, attempt: run.Attempt + 1}
	link := fmt.Sprintf("https://github.com/%s/actions/runs/%d", repository, run.ID)
	// Budget collection may take several API pages. Recheck the live request
	// immediately before changing the comment or triggering the old PR run.
	if _, _, stillRequested, err := authorizedRequest(repository, e); err != nil || !stillRequested {
		if err != nil {
			return err
		}
		return fmt.Errorf("benchmark request was withdrawn")
	}
	if err := updateComment(repository, c, commentBody(pr, r, "Benchmark queued. [Follow the run]("+link+").")); err != nil {
		return err
	}
	if err := writeAPI("POST", fmt.Sprintf("repos/%s/actions/runs/%d/rerun", repository, run.ID), ""); err != nil {
		return decline(err)
	}
	// Hold the same workflow concurrency lease as daily main until the PR
	// measurement finishes. This avoids a reservation-to-run visibility race.
	deadline := time.Now().Add(25 * time.Minute)
	for time.Now().Before(deadline) {
		var current benchmarkRun
		if err := readAPI(fmt.Sprintf("repos/%s/actions/runs/%d", repository, run.ID), &current); err != nil {
			return err
		}
		if current.ID != run.ID || current.Head != pr.Head.SHA {
			return fmt.Errorf("benchmark run identity changed")
		}
		if current.Attempt == r.attempt && current.Status == "completed" {
			if err := report(repository, current); err != nil {
				return err
			}
			if current.Conclusion != "success" {
				return fmt.Errorf("PR benchmark %s", current.Conclusion)
			}
			if err := completedMeasurement(repository, current); err != nil {
				return err
			}
			return nil
		}
		if current.Attempt > r.attempt {
			return fmt.Errorf("benchmark was rerun outside its authorized attempt")
		}
		time.Sleep(10 * time.Second)
	}
	if err := writeAPI("POST", fmt.Sprintf("repos/%s/actions/runs/%d/cancel", repository, run.ID), ""); err != nil {
		return err
	}
	return decline(fmt.Errorf("benchmark exceeded the queue/run window and cancellation was requested"))
}

func verifyRequest(repository string, e controlEvent) error {
	if os.Getenv("GITHUB_EVENT_NAME") != "pull_request" {
		return fmt.Errorf("benchmark execution requires pull_request context")
	}
	runID, err := strconv.ParseInt(os.Getenv("GITHUB_RUN_ID"), 10, 64)
	if err != nil || runID <= 0 {
		return fmt.Errorf("invalid run ID")
	}
	attempt, err := strconv.Atoi(os.Getenv("GITHUB_RUN_ATTEMPT"))
	if err != nil || attempt <= 0 {
		return fmt.Errorf("invalid run attempt")
	}
	pr, err := currentPull(repository, e.Pull.Number)
	if err != nil {
		return err
	}
	c, err := botComment(repository, pr.Number)
	if err != nil {
		return err
	}
	allowed := false
	if c.ID != 0 {
		r, err := parseRequest(c.Body)
		if err != nil {
			return err
		}
		allowed = pr.State == "open" && pr.Head.SHA == e.Pull.Head.SHA && r.head == pr.Head.SHA && r.run == runID && r.attempt == attempt
	}
	if path := os.Getenv("GITHUB_OUTPUT"); path != "" {
		if err := appendFile(path, fmt.Sprintf("allowed=%t\n", allowed)); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("GITHUB_OUTPUT is required")
	}
	if !allowed {
		fmt.Println("No authorized exact-head benchmark request for this attempt.")
	}
	return nil
}

func Control(mode string) error {
	repository := os.Getenv("GITHUB_REPOSITORY")
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(repository) {
		return fmt.Errorf("invalid repository")
	}
	raw, err := os.ReadFile(os.Getenv("GITHUB_EVENT_PATH"))
	if err != nil {
		return err
	}
	var e controlEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return err
	}
	switch mode {
	case "prepare":
		if os.Getenv("GITHUB_EVENT_NAME") != "pull_request_target" {
			return fmt.Errorf("comment preparation requires trusted PR metadata context")
		}
		return prepare(repository, e)
	case "request":
		if os.Getenv("GITHUB_EVENT_NAME") != "issue_comment" {
			return fmt.Errorf("benchmark request requires comment context")
		}
		return requestBenchmark(repository, e)
	case "verify":
		return verifyRequest(repository, e)
	case "report":
		if os.Getenv("GITHUB_EVENT_NAME") != "workflow_run" {
			return fmt.Errorf("benchmark reporting requires trusted workflow_run context")
		}
		return report(repository, e.WorkflowRun)
	default:
		return fmt.Errorf("unknown benchmark control mode %q", mode)
	}
}
