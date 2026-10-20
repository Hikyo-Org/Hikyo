// Command queue-report publishes informational merge queue diagnostics on the
// originating PR head. Run only from trusted default-branch tooling.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
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
	Ref        string     `json:"ref"`
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
	Conclusion string `json:"conclusion"`
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
	var data []byte
	if body != nil {
		var err error
		if data, err = json.Marshal(body); err != nil {
			return err
		}
	}
	endpoint := c.base + "/repos/" + c.repo
	if path != "" {
		endpoint += "/" + path
	}
	// Only idempotent reads are retried; check-run writes stay single-shot.
	attempts := 1
	if method == http.MethodGet {
		attempts = maxReadAttempts
	}
	for attempt := 1; ; attempt++ {
		retryAfter, retryable, err := c.attempt(ctx, method, path, endpoint, data, result)
		if err == nil || !retryable || attempt == attempts {
			return err
		}
		wait, ok := retryDelay(ctx, attempt, retryAfter)
		if !ok {
			return err
		}
		logRetry(attempt, wait, err)
		if retryWait(ctx, wait) != nil {
			return err
		}
	}
}

func (c client) attempt(ctx context.Context, method, path, endpoint string, data []byte, result any) (retryAfter time.Duration, retryable bool, err error) {
	var payload io.Reader
	if data != nil {
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return 0, false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, transientTransportError(ctx, err), err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return retryAfterSeconds(resp), retryableStatus(resp.StatusCode), fmt.Errorf("GitHub %s %s: HTTP %d", method, path, resp.StatusCode)
	}
	if result == nil {
		return 0, false, nil
	}
	// Decode buffers a whole value before unmarshalling, so a truncated body
	// leaves result untouched and the read can be repeated.
	err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(result)
	return 0, err != nil && transientTransportError(ctx, err), err
}

func (c client) read(ctx context.Context, path string, result any) error {
	return c.request(ctx, http.MethodGet, path, nil, result)
}

func (c client) publish(ctx context.Context, e event) error {
	return c.publishReport(ctx, e, nil)
}

func (c client) publishReport(ctx context.Context, e event, repair *reportWork) error {
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
	if repair != nil {
		if repair.Run != r.ID || repair.Attempt != r.Attempt || repair.Head != head {
			return errors.New("report repair routing differs from authenticated source membership")
		}
		// Discovery is only a routing hint. Repair never creates missing reports,
		// never copies another source's verdict and never retires an active run.
		if existing == nil || existing.Status == "completed" || r.Status != "completed" {
			return nil
		}
		if existing.Conclusion != "" || (existing.Status != "queued" && existing.Status != "in_progress") {
			return errors.New("report repair record has an unsupported nonterminal state")
		}
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
	out := render(r, prNumber, base, head, runURL, jobs, pr.Head.SHA != head)
	body := map[string]any{
		"name": name, "details_url": runURL, "external_id": external,
		"status": status, "output": map[string]string{"title": out.title, "summary": out.summary, "text": out.text},
	}
	if conclusion != "" {
		body["conclusion"] = conclusion
	}
	if existing != nil {
		if repair == nil {
			// Only the exact check ID authenticated above may be changed.
			return c.request(ctx, http.MethodPatch, fmt.Sprintf("check-runs/%d", existing.ID), body, nil)
		}
		// Hold the callback's existing run/attempt lease and renew source and record
		// immediately before the exact-ID PATCH. Terminal records stay immutable.
		var fresh run
		if err := c.read(ctx, runPath, &fresh); err != nil {
			return err
		}
		if fresh != r {
			return errors.New("report repair source changed before publication")
		}
		var current check
		path := fmt.Sprintf("check-runs/%d", existing.ID)
		if err := c.read(ctx, path, &current); err != nil {
			return err
		}
		// Completion changes status and conclusion, never the authenticated owner.
		identity := current
		identity.Status, identity.Conclusion = existing.Status, existing.Conclusion
		if identity != *existing {
			return errors.New("report repair record identity changed before publication")
		}
		if current.Status == "completed" {
			return nil
		}
		if current != *existing {
			return errors.New("report repair record changed before publication")
		}
		var written check
		if err := c.request(ctx, http.MethodPatch, path, body, &written); err != nil {
			return err
		}
		expected := *existing
		expected.Status, expected.Conclusion = status, conclusion
		canonical := fmt.Sprintf("https://github.com/%s/runs/%d", c.repo, existing.ID)
		if written.DetailsURL != expected.DetailsURL && written.DetailsURL != canonical && written.DetailsURL != runURL {
			return errors.New("report repair response changed source link ownership")
		}
		expected.DetailsURL = written.DetailsURL
		if written != expected {
			return errors.New("report repair response did not confirm exact completed record")
		}
		var observed check
		if err := c.read(ctx, path, &observed); err != nil {
			return err
		}
		if observed != expected {
			return errors.New("report repair terminal readback mismatch")
		}
		return nil
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
			// GitHub Actions can replace the submitted details URL with the
			// check's canonical run URL. Bind that form to this exact check ID;
			// the app, source binding, head and name must still all match.
			canonical := fmt.Sprintf("https://github.com/%s/runs/%d", c.repo, candidate.ID)
			if candidate.ID <= 0 || candidate.Name != name || candidate.HeadSHA != head || candidate.App.ID != actionsAppID || (candidate.DetailsURL != details && candidate.DetailsURL != canonical) || found != nil {
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

// now stamps "Updated" in the summary; tests replace it.
var now = time.Now

const reportCap = 50000

type rendered struct{ title, summary, text string }

// Job groups in display order: failures first, then unfinished, passed, skipped.
// groupFailed also holds cancelled jobs so they list together, in API order.
const (
	groupFailed = iota
	groupActive
	groupPassed
	groupSkipped
)

// jobGroup treats only success and neutral as passed: a completed job with
// action_required, stale or an empty conclusion must never read as green.
func jobGroup(j job) int {
	switch {
	case j.Status != "completed":
		return groupActive
	case j.Conclusion == "success" || j.Conclusion == "neutral":
		return groupPassed
	case j.Conclusion == "skipped":
		return groupSkipped
	}
	return groupFailed
}

// titleName keeps a hostile or very long job name from crowding the title.
// GitHub shows output.title as plain text, so Markdown escaping would print
// literally; drop control characters and angle brackets, collapse whitespace.
func titleName(s string) string {
	s = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '<' || r == '>' {
			return ' '
		}
		return r
	}, s)), " ")
	if r := []rune(s); len(r) > 40 {
		s = string(r[:40]) + "..."
	}
	return s
}

func titleJobName(j job) string {
	if name := titleName(j.Name); name != "" {
		return name
	}
	return fmt.Sprintf("job %d", j.ID)
}

type tally struct{ total, failed, cancelled, running, queued, passed, skipped int }

func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return singular
	}
	return pluralForm
}

func unfinishedTail(t tally) string {
	var tail string
	if t.running > 0 {
		tail += fmt.Sprintf(", %d running", t.running)
	}
	if t.queued > 0 {
		tail += fmt.Sprintf(", %d queued", t.queued)
	}
	return tail
}

func jobTitle(r run, pr int, t tally, firstFailed string) string {
	switch {
	case t.total == 0 && r.Status == "completed":
		return fmt.Sprintf("PR #%d: run %s, no jobs reported", pr, titleName(r.Conclusion))
	case t.total == 0:
		return fmt.Sprintf("PR #%d: no jobs reported yet", pr)
	case t.failed > 0:
		title := fmt.Sprintf("PR #%d: %d failed", pr, t.failed)
		if t.cancelled > 0 {
			title += fmt.Sprintf(", %d cancelled", t.cancelled)
		}
		title += fmt.Sprintf(", %d done", t.passed+t.skipped) + unfinishedTail(t)
		return title + fmt.Sprintf(" (first failure: %s)", firstFailed)
	case t.cancelled > 0:
		return fmt.Sprintf("PR #%d: %d cancelled, %d done", pr, t.cancelled, t.passed+t.skipped) + unfinishedTail(t)
	case r.Status == "completed" && r.Conclusion != "success":
		return fmt.Sprintf("PR #%d: run %s, %d reported, 0 failed", pr, titleName(r.Conclusion), t.total)
	case t.running > 0 || t.queued > 0:
		// Run and job snapshots can disagree; keep unfinished work visible.
		break
	case r.Status == "completed" && t.skipped == 0 && t.total == 1:
		return fmt.Sprintf("PR #%d: 1 job passed", pr)
	case r.Status == "completed" && t.skipped == 0:
		return fmt.Sprintf("PR #%d: all %d jobs passed", pr, t.total)
	case r.Status == "completed":
		return fmt.Sprintf("PR #%d: %d passed, %d skipped, 0 failed", pr, t.passed, t.skipped)
	}
	title := fmt.Sprintf("PR #%d: %d reported, 0 failed, %d running", pr, t.total, t.running)
	if t.queued > 0 {
		title += fmt.Sprintf(", %d queued", t.queued)
	}
	return title
}

func writeJobs(b *strings.Builder, runURL string, jobs []job) {
	for _, j := range jobs {
		if b.Len() > reportCap {
			b.WriteString("\nAdditional jobs omitted; open the source run for the complete list.\n")
			return
		}
		jobURL := fmt.Sprintf("%s/job/%d", strings.Split(runURL, "/attempts/")[0], j.ID)
		fmt.Fprintf(b, "- [%s](%s): %s\n", label(j.Name), jobURL, strings.TrimSpace(label(j.Status)+" "+label(j.Conclusion)))
		for _, step := range j.Steps {
			if step.Conclusion != "failure" && step.Conclusion != "timed_out" && step.Conclusion != "cancelled" {
				continue
			}
			if b.Len() > reportCap {
				break
			}
			fmt.Fprintf(b, "  - [%s](%s#step:%d:1): %s\n", label(step.Name), jobURL, step.Number, label(step.Conclusion))
		}
	}
}

func render(r run, pr int, base, head, runURL string, jobs []job, superseded bool) rendered {
	// Stable sort on group only: API order is kept within each group, so the
	// first failure is the first non-cancelled job the API listed as failed.
	ordered := slices.Clone(jobs)
	slices.SortStableFunc(ordered, func(a, b job) int { return jobGroup(a) - jobGroup(b) })
	t := tally{total: len(jobs)}
	firstFailed := ""
	for _, j := range ordered {
		switch jobGroup(j) {
		case groupFailed:
			if j.Conclusion == "cancelled" {
				t.cancelled++
			} else {
				if t.failed == 0 {
					firstFailed = titleJobName(j)
				}
				t.failed++
			}
		case groupActive:
			if j.Status == "in_progress" {
				t.running++
			} else {
				t.queued++
			}
		case groupPassed:
			t.passed++
		case groupSkipped:
			t.skipped++
		}
	}
	failed := ordered[:t.failed+t.cancelled]
	out := rendered{title: jobTitle(r, pr, t, firstFailed)}

	noJobs := "No jobs have been reported yet."
	if r.Status == "completed" {
		noJobs = "The run reported no jobs."
	}
	var b strings.Builder
	repoPath := strings.Split(strings.TrimPrefix(runURL, "https://github.com/"), "/actions/")[0]
	fmt.Fprintf(&b, "[Run %d, attempt %d](%s): %s\n\n", r.ID, r.Attempt, runURL, strings.TrimSpace(label(r.Status)+" "+label(r.Conclusion)))
	if len(jobs) == 0 {
		b.WriteString(noJobs + "\n\n")
	} else {
		scope := plural(t.total, "job", "jobs")
		if r.Status != "completed" {
			scope += " reported so far (more may appear as their dependencies finish)"
		}
		fmt.Fprintf(&b, "%d %s: %d failed or cancelled, %d running, %d queued, %d passed, %d skipped.\n\n", t.total, scope, len(failed), t.running, t.queued, t.passed, t.skipped)
	}
	fmt.Fprintf(&b, "Updated: %s\n\n", now().UTC().Format(time.RFC3339))
	if len(failed) == 0 {
		if len(jobs) > 0 {
			b.WriteString("No failed or cancelled jobs reported.\n\n")
		}
	} else {
		b.WriteString("### Failed or cancelled jobs\n\n")
		writeJobs(&b, runURL, failed)
		b.WriteString("\n")
	}
	if len(jobs) > 0 {
		b.WriteString("The full job list follows this summary.\n\n")
	}
	fmt.Fprintf(&b, "Informational queue result for PR #%d. This describes only the tested base and merge commit below; it is not reusable merge approval.\n\n", pr)
	if superseded {
		b.WriteString("The PR has a newer head. This report is retained on the original tested head.\n\n")
	}
	fmt.Fprintf(&b, "- PR head: `%s`\n- Tested base: `%s`\n- Tested merge: `%s`\n- [Run artifacts](https://github.com/%s/actions/runs/%d#artifacts) (shared across attempts)\n", head, base, r.HeadSHA, repoPath, r.ID)
	out.summary = b.String()

	b.Reset()
	b.WriteString("Job results, failed or cancelled first, then running and queued, then passed, then skipped (skipped jobs were not executed; selection reasons are in the planning job):\n\n")
	if len(jobs) == 0 {
		b.WriteString(noJobs + "\n")
	}
	writeJobs(&b, runURL, ordered)
	out.text = b.String()
	return out
}

func main() {
	if err := execute(os.Args[1:]...); err != nil {
		fmt.Fprintln(os.Stderr, "queue-report:", err)
		os.Exit(1)
	}
}
func execute(args ...string) error {
	flags := flag.NewFlagSet("queue-report", flag.ContinueOnError)
	repairJSON := flags.String("repair-work", "", "replay one existing report's authenticated completed source under its run/attempt lease")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected reporter arguments")
	}
	name := os.Getenv("GITHUB_EVENT_NAME")
	if (*repairJSON == "" && name != "workflow_run") || (*repairJSON != "" && name != "schedule" && name != "workflow_dispatch") {
		return errors.New("reporter requires a workflow callback or trusted scheduled/manual repair")
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
	if *repairJSON != "" {
		work, err := decodeReportWork(*repairJSON)
		if err != nil {
			return err
		}
		if e.Repository.DefaultBranch == "" || os.Getenv("GITHUB_REF") != "refs/heads/"+e.Repository.DefaultBranch || os.Getenv("GITHUB_WORKFLOW_REF") != repo+"/.github/workflows/ci-check-retirement.yml@refs/heads/"+e.Repository.DefaultBranch || (name == "workflow_dispatch" && e.Ref != "refs/heads/"+e.Repository.DefaultBranch) {
			return errors.New("reporter repair requires trusted default-branch retirement execution")
		}
		return c.replay(ctx, e.Repository, work)
	}
	return c.publish(ctx, e)
}
