// Command pr-gate publishes a trusted, asynchronous PR validation check.
// It must run from a trusted default-branch checkout, never from PR code.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

const workflowPath = ".github/workflows/ci-fork.yml"
const externalPrefix = "hikyo-pr-gate:v1:"
const actionsAppID = 15368

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type repository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
}
type reference struct {
	SHA  string     `json:"sha"`
	Ref  string     `json:"ref"`
	Repo repository `json:"repo"`
}
type pull struct {
	Number         int       `json:"number"`
	State          string    `json:"state"`
	Head           reference `json:"head"`
	Base           reference `json:"base"`
	MergeCommitSHA string    `json:"merge_commit_sha"`
	Mergeable      *bool     `json:"mergeable"`
	User           struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	} `json:"user"`
}
type workflow struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}
type run struct {
	ID             int64      `json:"id"`
	Attempt        int64      `json:"run_attempt"`
	WorkflowID     int64      `json:"workflow_id"`
	Path           string     `json:"path"`
	Name           string     `json:"name"`
	DisplayTitle   string     `json:"display_title"`
	Event          string     `json:"event"`
	HeadSHA        string     `json:"head_sha"`
	Status         string     `json:"status"`
	Conclusion     string     `json:"conclusion"`
	Repository     repository `json:"repository"`
	HeadRepository repository `json:"head_repository"`
	Pulls          []pull     `json:"pull_requests"`
	References     []struct {
		Path string `json:"path"`
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
	} `json:"referenced_workflows"`
}
type job struct {
	ID         int64  `json:"id"`
	RunID      int64  `json:"run_id"`
	Attempt    int64  `json:"run_attempt"`
	HeadSHA    string `json:"head_sha"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}
type check struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	HeadSHA    string `json:"head_sha"`
	ExternalID string `json:"external_id"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	App        struct {
		ID   int64  `json:"id"`
		Slug string `json:"slug"`
	} `json:"app"`
	Output struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	} `json:"output"`
}
type binding struct {
	RepoID  int64  `json:"repo"`
	PR      int    `json:"pr"`
	Head    string `json:"head"`
	BaseRef string `json:"base_ref"`
	Base    string `json:"base"`
	Run     int64  `json:"run"`
	Attempt int64  `json:"attempt"`
}

func (b binding) externalID() string {
	data, _ := json.Marshal(b)
	return externalPrefix + base64.RawURLEncoding.EncodeToString(data)
}
func decodeBinding(value string) (binding, error) {
	var b binding
	if !strings.HasPrefix(value, externalPrefix) {
		return b, errors.New("not a controller check")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, externalPrefix))
	if err != nil {
		return b, errors.New("invalid controller check binding")
	}
	if err := decode(data, &b); err != nil {
		return b, err
	}
	if b.RepoID <= 0 || b.PR <= 0 || !shaPattern.MatchString(b.Head) || !shaPattern.MatchString(b.Base) || b.BaseRef == "" || b.Run < 0 || b.Attempt < 0 || (b.Run == 0) != (b.Attempt == 0) {
		return b, errors.New("invalid controller check binding")
	}
	if b.externalID() != value {
		return b, errors.New("noncanonical controller check binding")
	}
	return b, nil
}

type event struct {
	Repository repository `json:"repository"`
	Number     int        `json:"number"`
	Pull       pull       `json:"pull_request"`
	Run        run        `json:"workflow_run"`
	Inputs     struct {
		PR string `json:"pr_number"`
	} `json:"inputs"`
}
type api interface {
	read(context.Context, string, any) error
	write(context.Context, string, string, any, any) error
}
type ghAPI struct{}

func decode(data []byte, result any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(result); err != nil {
		return fmt.Errorf("invalid GitHub JSON: %w", err)
	}
	var trailing any
	if err := d.Decode(&trailing); err != io.EOF {
		return errors.New("GitHub response contains extra JSON or malformed trailing data")
	}
	return nil
}
func (ghAPI) read(ctx context.Context, path string, result any) error {
	data, err := exec.CommandContext(ctx, "gh", "api", path).Output()
	if err != nil {
		return fmt.Errorf("GitHub read failed for %s", path)
	}
	return decode(data, result)
}
func (ghAPI) write(ctx context.Context, method, path string, payload, result any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "gh", "api", "--method", method, path, "--input", "-")
	cmd.Stdin = bytes.NewReader(data)
	response, err := cmd.Output()
	// A lost response may follow a successful mutation. Never retry writes.
	if err != nil {
		return fmt.Errorf("GitHub %s failed for %s; no mutation retry attempted", method, path)
	}
	return decode(response, result)
}

type verifier func(context.Context, int, string) (int, error)

func verifyPolicy(ctx context.Context, pr int, head string) (int, error) {
	cmd := exec.CommandContext(ctx, "sh", "scripts/ci/check-fork-validation.sh")
	// Override only the controller-owned settings, retaining gh authentication.
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "PR_NUMBER=") && !strings.HasPrefix(value, "HEAD_SHA=") && !strings.HasPrefix(value, "PR_GATE_ONCE=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "PR_NUMBER="+strconv.Itoa(pr), "HEAD_SHA="+head, "PR_GATE_ONCE=1")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && (exit.ExitCode() == 1 || exit.ExitCode() == 75) {
			return exit.ExitCode(), nil
		}
		return 0, errors.New("trusted one-shot policy verifier could not execute")
	}
	return 0, nil
}

type controller struct {
	api       api
	verify    verifier
	repoName  string
	repo      repository
	workflow  workflow
	checkName string
	dryRun    bool
	output    io.Writer
}

func (c *controller) endpoint(path string) string { return "repos/" + c.repoName + "/" + path }
func validWorkflow(w workflow) bool {
	return w.ID > 0 && w.Path == workflowPath && (w.Name == "fork-ci" || w.Name == "pr-validation")
}
func (c *controller) init(ctx context.Context) error {
	if !repoPattern.MatchString(c.repoName) {
		return errors.New("invalid GH_REPO")
	}
	if c.checkName != "ci-required" && c.checkName != "ci-required-next" {
		return errors.New("PR_GATE_CHECK_NAME must be ci-required or ci-required-next")
	}
	if err := c.api.read(ctx, "repos/"+c.repoName, &c.repo); err != nil {
		return err
	}
	if c.repo.ID <= 0 || !strings.EqualFold(c.repo.FullName, c.repoName) {
		return errors.New("repository identity mismatch")
	}
	if err := c.api.read(ctx, c.endpoint("actions/workflows/ci-fork.yml"), &c.workflow); err != nil {
		return err
	}
	if !validWorkflow(c.workflow) {
		return errors.New("validation workflow identity mismatch")
	}
	return nil
}
func validReference(r reference) bool {
	return shaPattern.MatchString(r.SHA) && r.Ref != "" && r.Repo.ID > 0 && repoPattern.MatchString(r.Repo.FullName)
}
func (c *controller) readPR(ctx context.Context, number int) (pull, error) {
	var p pull
	if number <= 0 {
		return p, errors.New("invalid PR number")
	}
	if err := c.api.read(ctx, c.endpoint(fmt.Sprintf("pulls/%d", number)), &p); err != nil {
		return p, err
	}
	if p.Number != number || (p.State != "open" && p.State != "closed") || !validReference(p.Head) || !validReference(p.Base) || p.Base.Repo.ID != c.repo.ID || !strings.EqualFold(p.Base.Repo.FullName, c.repoName) {
		return p, errors.New("invalid current PR metadata")
	}
	return p, nil
}
func (c *controller) openPRs(ctx context.Context) ([]pull, error) {
	var pulls []pull
	for page := 1; page <= 2; page++ {
		var entries []pull
		if err := c.api.read(ctx, c.endpoint(fmt.Sprintf("pulls?state=open&per_page=100&page=%d", page)), &entries); err != nil {
			return nil, err
		}
		if entries == nil {
			return nil, errors.New("invalid open PR list")
		}
		for _, p := range entries {
			if p.Number <= 0 || p.State != "open" || !validReference(p.Head) || !validReference(p.Base) || p.Base.Repo.ID != c.repo.ID {
				return nil, errors.New("invalid open PR metadata")
			}
		}
		pulls = append(pulls, entries...)
		if len(pulls) > 100 {
			return nil, errors.New("more than 100 open PRs; refusing incomplete collision/repair scan")
		}
		if len(entries) < 100 {
			return pulls, nil
		}
	}
	return pulls, nil
}
func associated(r run, p pull, exactBase bool) bool {
	if len(r.Pulls) == 0 {
		// GitHub omits pull_requests for some fork runs. A GitHub-recorded
		// reusable workflow merge ref and exact current merge SHA bind that PR
		// without accepting its PR-controlled title. Acceptance additionally
		// verifies the immutable merge commit and all author/workflow policy.
		if r.HeadSHA != p.Head.SHA || r.HeadRepository.ID != p.Head.Repo.ID || !shaPattern.MatchString(p.MergeCommitSHA) {
			return false
		}
		count := 0
		for _, ref := range r.References {
			parts := strings.Split(ref.Path, "@")
			if len(parts) == 2 && strings.EqualFold(parts[0], p.Base.Repo.FullName+"/.github/workflows/ci.yml") && parts[1] == ref.SHA && ref.SHA == p.MergeCommitSHA && ref.Ref == fmt.Sprintf("refs/pull/%d/merge", p.Number) {
				count++
			}
		}
		return count == 1
	}
	count := 0
	for _, a := range r.Pulls {
		if a.Number == p.Number && a.Head.SHA == p.Head.SHA && a.Head.Repo.ID == p.Head.Repo.ID && a.Base.Repo.ID == p.Base.Repo.ID {
			if !exactBase || (a.Base.SHA == p.Base.SHA && a.Base.Ref == p.Base.Ref) {
				count++
			}
		}
	}
	return count == 1
}
func (c *controller) validRun(r run) bool {
	if r.ID <= 0 || r.Attempt <= 0 || r.WorkflowID != c.workflow.ID || r.Path != workflowPath || r.Event != "pull_request" || r.Repository.ID != c.repo.ID || !strings.EqualFold(r.Repository.FullName, c.repoName) || !shaPattern.MatchString(r.HeadSHA) || r.HeadRepository.ID <= 0 || r.Pulls == nil {
		return false
	}
	switch r.Status {
	case "queued", "in_progress", "completed", "waiting", "pending", "requested":
	default:
		return false
	}
	if r.Status == "completed" && r.Conclusion == "" {
		return false
	}
	if r.Status != "completed" && r.Conclusion != "" {
		return false
	}
	return true
}
func (c *controller) latest(ctx context.Context, p pull) (*run, error) {
	var latest *run
	seen := make(map[int64]bool)
	for page := 1; page <= 10; page++ {
		var list struct {
			Total int   `json:"total_count"`
			Runs  []run `json:"workflow_runs"`
		}
		path := fmt.Sprintf("actions/workflows/ci-fork.yml/runs?event=pull_request&head_sha=%s&per_page=100&page=%d", p.Head.SHA, page)
		if err := c.api.read(ctx, c.endpoint(path), &list); err != nil {
			return nil, err
		}
		if list.Runs == nil || list.Total < 0 || list.Total > 1000 {
			return nil, errors.New("invalid or incomplete validation run list")
		}
		for _, r := range list.Runs {
			if !c.validRun(r) || r.HeadSHA != p.Head.SHA || seen[r.ID] {
				return nil, errors.New("invalid or duplicate validation run metadata")
			}
			seen[r.ID] = true
			if r.HeadRepository.ID != p.Head.Repo.ID || !associated(r, p, false) || (r.DisplayTitle != fmt.Sprintf("fork-ci #%d", p.Number) && r.DisplayTitle != fmt.Sprintf("pr-validation #%d", p.Number)) {
				continue
			}
			if latest == nil || r.ID > latest.ID {
				copy := r
				latest = &copy
			}
		}
		if len(list.Runs) < 100 {
			if len(seen) != list.Total {
				return nil, errors.New("incomplete validation run list")
			}
			if latest != nil {
				var live run
				if err := c.api.read(ctx, c.endpoint(fmt.Sprintf("actions/runs/%d", latest.ID)), &live); err != nil {
					return nil, err
				}
				if !c.validRun(live) || !sameSourceEvent(*latest, live) || !associated(live, p, false) {
					return nil, errors.New("source run changed during selection")
				}
				latest = &live
			}
			return latest, nil
		}
	}
	return nil, errors.New("validation run pagination limit reached")
}
func (c *controller) jobs(ctx context.Context, r run) ([]job, error) {
	return c.listJobs(ctx, r, false)
}
func (c *controller) listJobs(ctx context.Context, r run, latest bool) ([]job, error) {
	var all []job
	seen := make(map[int64]bool)
	for page := 1; page <= 10; page++ {
		var list struct {
			Total int   `json:"total_count"`
			Jobs  []job `json:"jobs"`
		}
		path := fmt.Sprintf("actions/runs/%d/attempts/%d/jobs?per_page=100&page=%d", r.ID, r.Attempt, page)
		if latest {
			path = fmt.Sprintf("actions/runs/%d/jobs?filter=latest&per_page=100&page=%d", r.ID, page)
		}
		if err := c.api.read(ctx, c.endpoint(path), &list); err != nil {
			return nil, err
		}
		if list.Jobs == nil || list.Total < 0 || list.Total > 1000 {
			return nil, errors.New("invalid validation job list")
		}
		for _, j := range list.Jobs {
			if j.ID <= 0 || j.RunID != r.ID || j.Attempt <= 0 || j.Attempt > r.Attempt || (!latest && j.Attempt != r.Attempt) || j.Name == "" || seen[j.ID] {
				return nil, errors.New("invalid exact-attempt validation job")
			}
			seen[j.ID] = true
			all = append(all, j)
		}
		if len(list.Jobs) < 100 {
			if len(all) != list.Total {
				return nil, errors.New("incomplete validation job list")
			}
			return all, nil
		}
	}
	return nil, errors.New("validation job pagination limit reached")
}

func (c *controller) reusedVouch(ctx context.Context, r run) (bool, error) {
	if r.Attempt <= 1 {
		return false, nil
	}
	// A partial rerun can retain successful admission dependencies. Accept only
	// the vouch GitHub still lists in this run's current execution, never a job
	// found by searching arbitrary historical attempts. The aggregate remains
	// exact-attempt and current default-branch admission is checked separately.
	jobs, err := c.listJobs(ctx, r, true)
	if err != nil {
		return false, err
	}
	count, successful := 0, false
	for _, j := range jobs {
		if j.Name == "vouch" {
			count++
			successful = j.Attempt < r.Attempt && j.HeadSHA == r.HeadSHA && j.Status == "completed" && j.Conclusion == "success"
		}
	}
	return count == 1 && successful, nil
}
func (c *controller) ownedChecks(ctx context.Context, p pull) ([]check, error) {
	var own []check
	listed := 0
	seen := make(map[int64]bool)
	for page := 1; page <= 10; page++ {
		var list struct {
			Total  int     `json:"total_count"`
			Checks []check `json:"check_runs"`
		}
		if err := c.api.read(ctx, c.endpoint(fmt.Sprintf("commits/%s/check-runs?check_name=%s&filter=latest&per_page=100&page=%d", p.Head.SHA, c.checkName, page)), &list); err != nil {
			return nil, err
		}
		if list.Checks == nil || list.Total < 0 || list.Total > 1000 {
			return nil, errors.New("invalid check list")
		}
		for _, v := range list.Checks {
			if v.ID <= 0 || seen[v.ID] {
				return nil, errors.New("invalid or duplicate current check identity")
			}
			seen[v.ID] = true
			listed++
			if v.Name != c.checkName {
				continue
			}
			if !strings.HasPrefix(v.ExternalID, externalPrefix) {
				if v.App.ID == actionsAppID || v.App.Slug == "github-actions" {
					return nil, errors.New("ambiguous same-name GitHub Actions check outside the controller")
				}
				continue
			}
			if v.ID <= 0 || v.App.ID != actionsAppID || v.App.Slug != "github-actions" || v.HeadSHA != p.Head.SHA {
				return nil, errors.New("untrusted controller check identity")
			}
			b, err := decodeBinding(v.ExternalID)
			if err != nil {
				return nil, err
			}
			if b.RepoID != c.repo.ID || b.Head != p.Head.SHA {
				return nil, errors.New("controller check repository/head mismatch")
			}
			// Check contexts belong to a commit. Even a closed PR's controller
			// success must be superseded before evaluating a new PR at that SHA.
			own = append(own, v)
		}
		if listed == list.Total {
			return own, nil
		}
		if len(list.Checks) < 100 || listed > list.Total {
			return nil, errors.New("incomplete current check listing")
		}
	}
	return nil, errors.New("check pagination limit reached")
}

type snapshot struct {
	PR        pull
	Run       *run
	Collision bool
}

func (c *controller) snapshot(ctx context.Context, number int) (snapshot, error) {
	var s snapshot
	p, err := c.readPR(ctx, number)
	if err != nil {
		return s, err
	}
	s.PR = p
	if p.State != "open" {
		return s, nil
	}
	all, err := c.openPRs(ctx)
	if err != nil {
		return s, err
	}
	found := false
	for _, other := range all {
		if other.Number == p.Number {
			found = true
			if other.Head != p.Head || other.Base != p.Base {
				return s, errors.New("PR changed during collision scan")
			}
		}
		if other.Number != p.Number && other.Head.SHA == p.Head.SHA {
			s.Collision = true
		}
	}
	if !found {
		return s, errors.New("open PR missing from collision scan")
	}
	s.Run, err = c.latest(ctx, p)
	return s, err
}
func (s snapshot) binding(repoID int64) binding {
	b := binding{RepoID: repoID, PR: s.PR.Number, Head: s.PR.Head.SHA, BaseRef: s.PR.Base.Ref, Base: s.PR.Base.SHA}
	if s.Run != nil {
		b.Run, b.Attempt = s.Run.ID, s.Run.Attempt
	}
	return b
}

type outcome struct{ Status, Conclusion, Title, Summary string }

func pending(summary string) outcome {
	return outcome{Status: "queued", Title: "PR validation pending", Summary: summary}
}
func refused(summary string) outcome {
	return outcome{Status: "completed", Conclusion: "failure", Title: "PR validation refused", Summary: summary}
}
func (c *controller) evaluate(ctx context.Context, s snapshot) (outcome, error) {
	code, err := c.verify(ctx, s.PR.Number, s.PR.Head.SHA)
	if err != nil {
		return outcome{}, err
	}
	if code != 0 && code != 1 && code != 75 {
		return outcome{}, errors.New("invalid one-shot verifier result")
	}
	if s.Collision {
		return refused("Another open PR shares this head. A commit-level check cannot safely authorize distinct PR policy and integration candidates."), nil
	}
	if code == 1 {
		return refused("The trusted exact-head policy verifier refused this PR. Inspect the controller workflow log for the policy or validation failure."), nil
	}
	if s.Run == nil {
		return pending("No API-associated PR validation run exists yet. The completion callback or scheduled repair will reconcile it."), nil
	}
	if !associated(*s.Run, s.PR, true) {
		return pending("The latest validation run targets a different base revision. Fresh validation of the current integration candidate is required."), nil
	}
	r := *s.Run
	if r.Status != "completed" || r.Conclusion == "action_required" {
		return pending(fmt.Sprintf("Validation run %d attempt %d is %s. No runner is held waiting for completion.", r.ID, r.Attempt, r.Status)), nil
	}
	jobs, err := c.jobs(ctx, r)
	if err != nil {
		return outcome{}, err
	}
	aggregates, vouches := 0, 0
	aggregateOK, vouchOK := false, false
	for _, j := range jobs {
		if j.Name == "validation / ci-required" {
			aggregates++
			aggregateOK = j.Status == "completed" && j.Conclusion == "success"
		}
		if j.Name == "vouch" {
			vouches++
			vouchOK = j.Status == "completed" && j.Conclusion == "success"
		}
	}
	if aggregates != 1 {
		return refused("Validation must contain exactly one validation / ci-required aggregate for the current attempt."), nil
	}
	if r.Conclusion != "success" || !aggregateOK {
		return refused(fmt.Sprintf("Validation run %d attempt %d failed or did not produce a successful aggregate.", r.ID, r.Attempt)), nil
	}
	if s.PR.Head.Repo.ID != c.repo.ID {
		if vouches == 0 {
			vouchOK, err = c.reusedVouch(ctx, r)
			if err != nil {
				return outcome{}, err
			}
		} else if vouches != 1 {
			vouchOK = false
		}
		if !vouchOK {
			return refused("A fork requires exactly one successful author-vouch job in the current attempt or proved reused by GitHub's current execution."), nil
		}
	}
	if s.PR.Head.Repo.ID != c.repo.ID {
		vouched, err := c.currentVouch(ctx, s.PR)
		if err != nil {
			return outcome{}, err
		}
		if !vouched {
			return refused("The fork author is no longer vouched by current default-branch policy or current collaborator permission."), nil
		}
	}
	proved, err := c.proveIntegration(ctx, r, s.PR)
	if err != nil {
		return outcome{}, err
	}
	if !proved {
		return pending("The validation run does not prove the current base/head integration candidate. Fresh validation is required."), nil
	}
	if code == 75 {
		return pending("Trusted policy verification is still pending; validation success alone cannot authorize the merge."), nil
	}
	return outcome{Status: "completed", Conclusion: "success", Title: "PR validation passed", Summary: fmt.Sprintf("Trusted policy and validation run %d attempt %d passed for PR #%d head %s against %s at %s.", r.ID, r.Attempt, s.PR.Number, s.PR.Head.SHA, s.PR.Base.Ref, s.PR.Base.SHA)}, nil
}

// Trustdown parsing follows mitchellh/vouch's file.nu and lib.nu at
// d66fa29a64600490892131ad87597c30c91fcac4: platformless entries default to
// github; matching is case-insensitive; the first match decides. Unsupported
// malformed handles are rejected instead of silently authorizing a user.
func trustdownVouched(contents, login string) (bool, error) {
	type entry struct {
		user, platform string
		deny           bool
	}
	var entries []entry
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		deny := strings.HasPrefix(line, "-")
		if deny {
			line = strings.TrimPrefix(line, "-")
		}
		handle, _, _ := strings.Cut(line, " ")
		parts := strings.Split(strings.ToLower(handle), ":")
		platform, user := "github", parts[0]
		if len(parts) == 2 {
			platform, user = parts[0], parts[1]
		}
		if len(parts) > 2 || platform == "" || user == "" || strings.ContainsAny(handle, "\t\r\x00") || strings.HasPrefix(user, "-") {
			return false, errors.New("unsupported malformed Trustdown entry")
		}
		entries = append(entries, entry{user: user, platform: platform, deny: deny})
	}
	for _, e := range entries {
		if e.platform == "github" && e.user == strings.ToLower(login) {
			return !e.deny, nil
		}
	}
	return false, nil
}
func (c *controller) currentVouch(ctx context.Context, p pull) (bool, error) {
	login := p.User.Login
	if p.User.ID <= 0 || !regexp.MustCompile(`^[A-Za-z0-9-]+(\[bot\])?$`).MatchString(login) {
		return false, errors.New("invalid fork author identity")
	}
	if strings.HasSuffix(login, "[bot]") {
		return true, nil
	}
	var permission struct {
		Permission string `json:"permission"`
		User       struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	if err := c.api.read(ctx, c.endpoint("collaborators/"+login+"/permission"), &permission); err != nil {
		return false, err
	}
	if permission.User.ID != p.User.ID || permission.Permission == "" {
		return false, errors.New("invalid current collaborator permission response")
	}
	if permission.Permission == "admin" || permission.Permission == "write" {
		return true, nil
	}
	if c.repo.DefaultBranch == "" {
		return false, errors.New("missing trusted default branch for current vouch policy")
	}
	var file struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		Path     string `json:"path"`
	}
	if err := c.api.read(ctx, c.endpoint("contents/.github/VOUCHED.td?ref="+url.QueryEscape(c.repo.DefaultBranch)), &file); err != nil {
		return false, err
	}
	if file.Type != "file" || file.Encoding != "base64" || file.Path != ".github/VOUCHED.td" {
		return false, errors.New("invalid default-branch vouch file metadata")
	}
	contents, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	if err != nil {
		return false, errors.New("invalid default-branch vouch file contents")
	}
	return trustdownVouched(string(contents), login)
}
func (c *controller) proveIntegration(ctx context.Context, r run, p pull) (bool, error) {
	// API PR associations can change after retargets. GitHub records the actual
	// reusable workflow merge commit. Compare it with GitHub's current merge
	// candidate, including stacked PRs whose first parent is a synthetic merge.
	var mergeSHA string
	count := 0
	for _, ref := range r.References {
		parts := strings.Split(ref.Path, "@")
		if len(parts) != 2 || !strings.EqualFold(parts[0], c.repoName+"/.github/workflows/ci.yml") {
			continue
		}
		count++
		if ref.Ref != fmt.Sprintf("refs/pull/%d/merge", p.Number) || !shaPattern.MatchString(ref.SHA) || parts[1] != ref.SHA {
			return false, errors.New("invalid reusable validation workflow source")
		}
		mergeSHA = ref.SHA
	}
	if count == 0 {
		return false, nil
	}
	if count != 1 {
		return false, errors.New("ambiguous reusable validation workflow sources")
	}
	if p.Mergeable == nil || !*p.Mergeable || !shaPattern.MatchString(p.MergeCommitSHA) || mergeSHA != p.MergeCommitSHA {
		return false, nil
	}
	var commit struct {
		SHA     string `json:"sha"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if err := c.api.read(ctx, c.endpoint("git/commits/"+mergeSHA), &commit); err != nil {
		return false, err
	}
	if commit.SHA != mergeSHA || commit.Parents == nil {
		return false, errors.New("invalid immutable integration commit")
	}
	return len(commit.Parents) == 2 && shaPattern.MatchString(commit.Parents[0].SHA) && commit.Parents[1].SHA == p.Head.SHA, nil
}
func sameSnapshot(a, b snapshot, repoID int64) bool {
	if a.PR.State != b.PR.State || a.PR.Head != b.PR.Head || a.PR.Base != b.PR.Base || a.PR.MergeCommitSHA != b.PR.MergeCommitSHA || a.Collision != b.Collision || a.binding(repoID) != b.binding(repoID) {
		return false
	}
	if a.Run == nil {
		return b.Run == nil
	}
	return b.Run != nil && a.Run.Status == b.Run.Status && a.Run.Conclusion == b.Run.Conclusion && associated(*b.Run, b.PR, true) == associated(*a.Run, a.PR, true)
}
func (c *controller) reconcile(ctx context.Context, number int, callback *run) error {
	// Revoke our prior success before querying mutable source/policy APIs. If
	// those later reads fail, an earlier result must not keep authorizing merge.
	if err := c.invalidateSuccess(ctx, number); err != nil {
		return err
	}
	s, err := c.snapshot(ctx, number)
	if err != nil {
		return err
	}
	if s.PR.State != "open" {
		return nil
	}
	if callback != nil && (s.Run == nil || s.Run.ID != callback.ID || s.Run.Attempt != callback.Attempt || s.PR.Head.SHA != callback.HeadSHA) {
		own, err := c.ownedChecks(ctx, s.PR)
		if err != nil {
			return err
		}
		if len(own) > 0 {
			return c.reconcile(ctx, number, nil)
		}
		return nil
	}
	own, err := c.ownedChecks(ctx, s.PR)
	if err != nil {
		return err
	}
	result, err := c.evaluate(ctx, s)
	if err != nil {
		return err
	}
	// Repeat authorization just before publication, including current maintainer
	// approval. Both reads and checks are intentionally bounded and never sleep.
	if result.Conclusion == "success" {
		code, err := c.verify(ctx, number, s.PR.Head.SHA)
		if err != nil {
			return err
		}
		switch code {
		case 0:
		case 1:
			result = refused("Trusted policy was refused during the final authorization check.")
		case 75:
			result = pending("Trusted policy is pending during the final authorization check.")
		default:
			return errors.New("invalid final one-shot verifier result")
		}
		if result.Conclusion == "success" && s.PR.Head.Repo.ID != c.repo.ID {
			vouched, err := c.currentVouch(ctx, s.PR)
			if err != nil {
				return err
			}
			if !vouched {
				result = refused("Fork author admission was revoked before publication.")
			}
		}
	}
	final, err := c.snapshot(ctx, number)
	if err != nil {
		return err
	}
	if !sameSnapshot(s, final, c.repo.ID) {
		return errors.New("PR, base, source run or attempt changed before publication")
	}
	if result.Conclusion == "success" {
		proved, err := c.proveIntegration(ctx, *final.Run, final.PR)
		if err != nil {
			return err
		}
		if !proved {
			return errors.New("integration candidate changed before success publication")
		}
	}
	own, err = c.ownedChecks(ctx, final.PR)
	if err != nil {
		return err
	}
	var selected *check
	for _, v := range own {
		if selected == nil || v.ID > selected.ID {
			copy := v
			selected = &copy
		}
	}
	externalID := final.binding(c.repo.ID).externalID()
	if selected != nil && selected.ExternalID == externalID && selected.Status == result.Status && selected.Conclusion == result.Conclusion && selected.Output.Summary == result.Summary {
		return nil
	}
	return c.publish(ctx, final.PR, final.binding(c.repo.ID), selected, result)
}
func (c *controller) publish(ctx context.Context, p pull, b binding, selected *check, result outcome) error {
	externalID := b.externalID()
	// GitHub's Checks schema does not accept null conclusions/completed_at.
	// Reruns supersede completed checks by creating a fresh queued check.
	if selected != nil && ((selected.Status == "completed" && result.Status != "completed") || selected.ExternalID != externalID) {
		old, err := decodeBinding(selected.ExternalID)
		placeholder := err == nil && selected.Status != "completed" && old.Run == 0 && old.RepoID == b.RepoID && old.PR == b.PR && old.Head == b.Head && old.Base == b.Base && old.BaseRef == b.BaseRef
		if !placeholder {
			selected = nil
		}
	}
	payload := map[string]any{"name": c.checkName, "external_id": externalID, "status": result.Status, "output": map[string]string{"title": result.Title, "summary": result.Summary}}
	if id := os.Getenv("GITHUB_RUN_ID"); id != "" {
		runID, err := strconv.ParseInt(id, 10, 64)
		if err != nil || runID <= 0 {
			return errors.New("invalid controller GITHUB_RUN_ID")
		}
		payload["details_url"] = fmt.Sprintf("https://github.com/%s/actions/runs/%d", c.repoName, runID)
	} else if b.Run > 0 {
		payload["details_url"] = fmt.Sprintf("https://github.com/%s/actions/runs/%d", c.repoName, b.Run)
	}
	if result.Status == "completed" {
		payload["conclusion"] = result.Conclusion
	}
	method, path := "POST", c.endpoint("check-runs")
	if selected != nil {
		method, path = "PATCH", c.endpoint(fmt.Sprintf("check-runs/%d", selected.ID))
	} else {
		payload["head_sha"] = p.Head.SHA
	}
	if c.dryRun {
		_, err := fmt.Fprintf(c.output, "DRY RUN PR #%d: %s %s (%s), %s\n", p.Number, c.checkName, result.Status, result.Conclusion, result.Summary)
		return err
	}
	var written check
	if err := c.api.write(ctx, method, path, payload, &written); err != nil {
		return err
	}
	if written.ID <= 0 || written.App.ID != actionsAppID || written.App.Slug != "github-actions" || written.ExternalID != externalID || written.HeadSHA != p.Head.SHA || written.Name != c.checkName || written.Status != result.Status || written.Conclusion != result.Conclusion {
		return errors.New("GitHub returned an unexpected check identity/result; no mutation retry attempted")
	}
	return nil
}
func (c *controller) invalidateSuccess(ctx context.Context, number int) error {
	p, err := c.readPR(ctx, number)
	if err != nil {
		return err
	}
	if p.State != "open" {
		return nil
	}
	own, err := c.ownedChecks(ctx, p)
	if err != nil {
		return err
	}
	var latest *check
	for _, v := range own {
		if latest == nil || v.ID > latest.ID {
			copy := v
			latest = &copy
		}
	}
	if latest == nil || (latest.Status == "completed" && latest.Conclusion == "success") {
		current, err := c.readPR(ctx, number)
		if err != nil {
			return err
		}
		if current.State != "open" || current.Head != p.Head || current.Base != p.Base {
			return errors.New("PR changed before pending barrier")
		}
		b := (snapshot{PR: p}).binding(c.repo.ID)
		if err := c.publish(ctx, p, b, latest, pending("Rechecking current policy, validation attempt and integration candidate.")); err != nil {
			return err
		}
	}
	return nil
}
func sameSourceEvent(a, b run) bool {
	return a.ID == b.ID && a.Attempt == b.Attempt && a.WorkflowID == b.WorkflowID && a.Path == b.Path && a.Event == b.Event && a.HeadSHA == b.HeadSHA && a.Repository.ID == b.Repository.ID && a.HeadRepository.ID == b.HeadRepository.ID
}
func (c *controller) execute(ctx context.Context, eventName string, e event) error {
	if err := c.init(ctx); err != nil {
		return err
	}
	if e.Repository.ID != c.repo.ID || !strings.EqualFold(e.Repository.FullName, c.repoName) {
		return errors.New("event repository identity mismatch")
	}
	switch eventName {
	case "pull_request_target":
		if e.Number <= 0 || e.Pull.Number != e.Number {
			return errors.New("invalid pull_request_target event")
		}
		p, err := c.readPR(ctx, e.Number)
		if err != nil {
			return err
		}
		if p.Head.SHA != e.Pull.Head.SHA || p.Base.Ref != e.Pull.Base.Ref || p.Head.Repo.ID != e.Pull.Head.Repo.ID || p.Base.Repo.ID != e.Pull.Base.Repo.ID {
			return nil
		}
		return c.reconcile(ctx, e.Number, nil)
	case "workflow_dispatch":
		number, err := strconv.Atoi(e.Inputs.PR)
		if err != nil || number <= 0 {
			return errors.New("workflow_dispatch requires inputs.pr_number")
		}
		return c.reconcile(ctx, number, nil)
	case "workflow_run":
		if !c.validRun(e.Run) {
			return errors.New("invalid callback source metadata")
		}
		if strings.HasPrefix(e.Run.DisplayTitle, "pr-metadata #") {
			return nil
		}
		// Event associations are hints for revocation only. They never authorize
		// success; the live source and current PR are checked below.
		hints := e.Run.Pulls
		if len(hints) == 0 {
			all, err := c.openPRs(ctx)
			if err != nil {
				return err
			}
			for _, p := range all {
				if p.Head.SHA == e.Run.HeadSHA && p.Head.Repo.ID == e.Run.HeadRepository.ID {
					hints = append(hints, p)
				}
			}
		}
		for _, a := range hints {
			if a.Number <= 0 {
				return errors.New("invalid callback PR hint")
			}
			p, err := c.readPR(ctx, a.Number)
			if err != nil {
				return err
			}
			if p.Head.SHA != e.Run.HeadSHA || p.Head.Repo.ID != e.Run.HeadRepository.ID {
				continue
			}
			if err := c.invalidateSuccess(ctx, a.Number); err != nil {
				return err
			}
		}
		var live run
		if err := c.api.read(ctx, c.endpoint(fmt.Sprintf("actions/runs/%d", e.Run.ID)), &live); err != nil {
			return err
		}
		if !c.validRun(live) {
			return errors.New("invalid live callback source")
		}
		if live.Attempt != e.Run.Attempt {
			return c.repairHints(ctx, hints)
		}
		if !sameSourceEvent(e.Run, live) {
			return errors.New("callback differs from live source identity")
		}
		if len(live.Pulls) == 0 {
			// Routing by head alone authorizes only reevaluation. A terminal pass
			// still needs associated's immutable reusable merge-ref fallback.
			for _, p := range hints {
				if err := c.reconcile(ctx, p.Number, nil); err != nil {
					return err
				}
			}
			return nil
		}
		seen := make(map[int]bool)
		for _, a := range live.Pulls {
			if a.Number <= 0 || seen[a.Number] {
				return errors.New("invalid or ambiguous callback PR association")
			}
			seen[a.Number] = true
			p, err := c.readPR(ctx, a.Number)
			if err != nil {
				return err
			}
			if p.State != "open" || p.Head.SHA != live.HeadSHA {
				if err := c.repairHints(ctx, []pull{a}); err != nil {
					return err
				}
				continue
			}
			if !associated(live, p, true) || live.HeadRepository.ID != p.Head.Repo.ID {
				if err := c.repairHints(ctx, []pull{a}); err != nil {
					return err
				}
				continue
			}
			if err := c.reconcile(ctx, a.Number, &live); err != nil {
				return err
			}
		}
		return nil
	case "schedule":
		pulls, err := c.openPRs(ctx)
		if err != nil {
			return err
		}
		var failures []error
		// Repair existing terminal checks too: a missed rerun/retarget event must
		// not leave an earlier success authorizing a newer integration candidate.
		for _, p := range pulls {
			if err := c.reconcile(ctx, p.Number, nil); err != nil {
				failures = append(failures, fmt.Errorf("PR #%d: %w", p.Number, err))
			}
		}
		return errors.Join(failures...)
	default:
		return errors.New("unsupported trusted controller event")
	}
}
func (c *controller) repairHints(ctx context.Context, hints []pull) error {
	for _, hint := range hints {
		p, err := c.readPR(ctx, hint.Number)
		if err != nil {
			return err
		}
		if p.State != "open" {
			continue
		}
		own, err := c.ownedChecks(ctx, p)
		if err != nil {
			return err
		}
		if len(own) > 0 {
			if err := c.reconcile(ctx, p.Number, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
func main() {
	dryRun := flag.Bool("dry-run", false, "read policy and live metadata without publishing checks")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected positional arguments")
		os.Exit(1)
	}
	checkName := os.Getenv("PR_GATE_CHECK_NAME")
	if checkName == "" {
		checkName = "ci-required"
	}
	data, err := os.ReadFile(os.Getenv("GITHUB_EVENT_PATH"))
	var e event
	if err == nil {
		err = decode(data, &e)
	}
	if err == nil {
		c := controller{api: ghAPI{}, verify: verifyPolicy, repoName: os.Getenv("GH_REPO"), checkName: checkName, dryRun: *dryRun, output: os.Stdout}
		err = c.execute(context.Background(), os.Getenv("GITHUB_EVENT_NAME"), e)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "trusted PR gate:", err)
		os.Exit(1)
	}
}
