package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const devCheckName = "ci-dev-required"
const devRulesetName = "dev requires trusted fast feedback"
const devGateWorkflowPath = ".github/workflows/ci-dev-gate.yml"

// Admission target and trusted authority are separate. Vouching, publisher code
// and workflow authority keep using the real default branch, which stays main.
func (c *controller) admissionBranch() string {
	if c.checkName == devCheckName {
		return "dev"
	}
	return c.repo.DefaultBranch
}

func runDevCommand(number int, head string, dryRun bool) error {
	data, err := os.ReadFile(os.Getenv("GITHUB_EVENT_PATH"))
	if err != nil {
		return err
	}
	var e event
	if err := decode(data, &e); err != nil {
		return err
	}
	c := &controller{api: controllerAPI(true, false), verify: verifyPolicy, repoName: os.Getenv("GH_REPO"), checkName: devCheckName, expectedHead: head, dryRun: dryRun, output: os.Stdout, diagnostics: os.Stderr}
	q := &queueController{controller: c, targeted: true}
	budget, err := parseAdmissionBudget(os.Getenv("PR_GATE_STARTED_AT"), os.Getenv("PR_GATE_DEADLINE"), time.Now())
	if err != nil {
		return err
	}
	return q.withAdmissionBudget(context.Background(), budget, func(ctx context.Context) error {
		return q.executeDev(ctx, os.Getenv("GITHUB_EVENT_NAME"), e, number)
	})
}

func (q *queueController) executeDev(ctx context.Context, name string, e event, number int) error {
	if q.checkName != devCheckName || number <= 0 || !shaPattern.MatchString(q.expectedHead) {
		return errors.New("dev admission requires its distinct context, PR and held head")
	}
	if err := q.initRepository(ctx); err != nil {
		return q.initializedFailure(ctx, e.Repository.ID, e.Repository.FullName, err)
	}
	if q.repo.DefaultBranch != "main" || e.Repository.ID != q.repo.ID || !strings.EqualFold(e.Repository.FullName, q.repoName) || e.Repository.DefaultBranch != q.repo.DefaultBranch {
		return errors.New("dev publisher requires the authenticated main default branch")
	}
	switch name {
	case "pull_request_target":
		previous := e.Action == "synchronize" && e.Before == q.expectedHead && e.Before != e.Pull.Head.SHA
		if e.Number != number || e.Pull.Number != number || e.Pull.Base.Ref != "dev" || e.Pull.Head.SHA != q.expectedHead && !previous {
			return errors.New("dev PR event differs from the held routing identity")
		}
	case "workflow_run":
		if e.Run.Event != "pull_request" || e.Run.Path != feedbackWorkflowPath || e.Run.Repository.ID != q.repo.ID || e.Run.HeadSHA != q.expectedHead {
			return errors.New("dev callback is not exact-head PR feedback")
		}
	case "push":
		if e.Ref != "refs/heads/main" && e.Ref != "refs/heads/dev" {
			return errors.New("dev policy repair requires a protected branch push")
		}
	case "workflow_dispatch", "schedule":
		if os.Getenv("GITHUB_REF") != "refs/heads/main" {
			return errors.New("dev policy repair must execute from trusted main")
		}
	default:
		return errors.New("unsupported dev admission event")
	}
	return q.reassess(ctx, func(ctx context.Context) error {
		p, err := q.readPR(ctx, number)
		if err != nil {
			return err
		}
		if p.State != "open" || p.Head.SHA != q.expectedHead || p.Base.Ref != "dev" {
			return errors.Join(&staleHeadWork{expected: q.expectedHead}, q.reconciliation.retireHead(ctx, false))
		}
		return q.admit(ctx, number)
	})
}

func (q *queueController) devRoles(ctx context.Context, head string) (headRoles, error) {
	pulls, err := q.openPRs(ctx)
	if err != nil {
		return headRoles{}, err
	}
	roles := headRoles{AllPulls: make(map[int]pull, len(pulls))}
	for _, p := range pulls {
		roles.AllPulls[p.Number] = p
		if p.Head.SHA == head {
			// Include every base: one commit cannot borrow dev admission from
			// a different simultaneously open PR or a later retarget.
			roles.Pulls = append(roles.Pulls, p)
		}
	}
	return roles, nil
}

type devPolicyOwner struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	Target      string            `json:"target"`
	Enforcement string            `json:"enforcement"`
	UpdatedAt   string            `json:"updated_at"`
	Bypass      []json.RawMessage `json:"bypass_actors"`
	Conditions  struct {
		Ref struct {
			Include []string `json:"include"`
			Exclude []string `json:"exclude"`
		} `json:"ref_name"`
	} `json:"conditions"`
	Rules []rule `json:"rules"`
}

func validateDevRules(rules []rule) error {
	if rules == nil {
		return errors.New("missing dev protection rules")
	}
	seen := make(map[string]int)
	for _, r := range rules {
		seen[r.Type]++
		switch r.Type {
		case "merge_queue":
			return errors.New("dev fast admission cannot be coupled to a merge queue")
		case "required_status_checks":
			var p struct {
				Strict *bool `json:"strict_required_status_checks_policy"`
				Create *bool `json:"do_not_enforce_on_create"`
				Checks []struct {
					Context string `json:"context"`
				} `json:"required_status_checks"`
			}
			if err := decode(r.Parameters, &p); err != nil {
				return err
			}
			if p.Strict == nil || *p.Strict || p.Create == nil || *p.Create || len(p.Checks) != 1 || p.Checks[0].Context != devCheckName {
				return errors.New("dev requires its distinct non-queue fast admission check")
			}
		case "pull_request":
			var p struct {
				Threads *bool    `json:"required_review_thread_resolution"`
				Stale   *bool    `json:"dismiss_stale_reviews_on_push"`
				Methods []string `json:"allowed_merge_methods"`
			}
			if err := decode(r.Parameters, &p); err != nil {
				return err
			}
			if p.Threads == nil || !*p.Threads || p.Stale == nil || !*p.Stale {
				return errors.New("dev protection lost review safeguards")
			}
			// Squash and rebase rewrite signed-off commits, which promotion's DCO
			// range check would refuse with no way to repair protected history.
			if len(p.Methods) != 1 || p.Methods[0] != "merge" {
				return errors.New("dev must preserve signed-off commits through merge commits")
			}
		}
	}
	for _, kind := range []string{"required_status_checks", "required_signatures", "pull_request", "deletion", "non_fast_forward"} {
		if seen[kind] != 1 {
			return fmt.Errorf("ambiguous or missing dev %s protection", kind)
		}
	}
	return nil
}

func (q *queueController) devPolicy(ctx context.Context) (liveQueuePolicy, error) {
	if q.repo.DefaultBranch != "main" {
		return liveQueuePolicy{}, errors.New("dev authority must remain on main")
	}
	seen := make(map[int64]bool)
	var id int64
	for page := 1; ; page++ {
		var list []devPolicyOwner
		if err := q.api.read(ctx, q.endpoint(fmt.Sprintf("rulesets?per_page=100&page=%d", page)), &list); err != nil {
			return liveQueuePolicy{}, err
		}
		if list == nil || len(list) > 100 || page > 20 {
			return liveQueuePolicy{}, errors.New("incomplete dev ruleset discovery")
		}
		for _, item := range list {
			if item.ID <= 0 || seen[item.ID] || item.Name == "" {
				return liveQueuePolicy{}, errors.New("invalid dev ruleset discovery identity")
			}
			seen[item.ID] = true
			if item.Name == devRulesetName {
				if id != 0 {
					return liveQueuePolicy{}, errors.New("ambiguous owning dev ruleset")
				}
				id = item.ID
			}
		}
		if len(list) < 100 {
			break
		}
	}
	if id == 0 {
		return liveQueuePolicy{}, errors.New("dev protection has not been activated")
	}
	var owner devPolicyOwner
	var effective []rule
	if err := parallelReads(ctx,
		func(ctx context.Context) error {
			return q.api.read(ctx, q.endpoint(fmt.Sprintf("rulesets/%d", id)), &owner)
		},
		func(ctx context.Context) error { return q.api.read(ctx, q.endpoint("rules/branches/dev"), &effective) },
	); err != nil {
		return liveQueuePolicy{}, err
	}
	if owner.ID != id || owner.Name != devRulesetName || owner.Target != "branch" || owner.Enforcement != "active" || len(owner.Bypass) != 0 || len(owner.Conditions.Ref.Include) != 1 || owner.Conditions.Ref.Include[0] != "refs/heads/dev" || len(owner.Conditions.Ref.Exclude) != 0 {
		return liveQueuePolicy{}, errors.New("invalid owning dev protection")
	}
	if err := validateDevRules(owner.Rules); err != nil {
		return liveQueuePolicy{}, err
	}
	if err := validateDevRules(effective); err != nil {
		return liveQueuePolicy{}, err
	}
	var mainRef struct {
		Ref    string `json:"ref"`
		Object struct {
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"object"`
	}
	if err := q.api.read(ctx, q.endpoint("git/ref/heads/main"), &mainRef); err != nil {
		return liveQueuePolicy{}, err
	}
	if mainRef.Ref != "refs/heads/main" || mainRef.Object.Type != "commit" || !shaPattern.MatchString(mainRef.Object.SHA) {
		return liveQueuePolicy{}, errors.New("invalid trusted main identity for dev policy")
	}
	// A privileged base-branch publisher must not be editable through the fast
	// lane. CI policy changes land on main first, then flow back into dev.
	trusted, err := q.contents(ctx, devGateWorkflowPath, mainRef.Object.SHA)
	if err != nil {
		return liveQueuePolicy{}, err
	}
	body, err := json.Marshal(struct {
		Owner     devPolicyOwner
		Effective []rule
		Authority string
		Publisher [sha256.Size]byte
	}{owner, effective, mainRef.Object.SHA, sha256.Sum256(trusted)})
	if err != nil {
		return liveQueuePolicy{}, err
	}
	return liveQueuePolicy{Enabled: true, Digest: sha256.Sum256(body)}, nil
}

// Source proof requires these bytes to match approved main (source-proof's
// policyPaths plus its dev producer). A change landing on dev first would stall
// every later dev proof until a promotion that itself starts from dev proof.
var devPolicyPaths = map[string]bool{
	".github/workflows/ci-dev.yml": true, ".github/workflows/ci.yml": true,
	".github/workflows/floor-bench.yml": true,
	"scripts/ci/ci-job-registry.json":   true, "scripts/ci/check-required-jobs.sh": true,
	"scripts/ci/classify-changed-paths.sh": true, "scripts/ci/plan-merge-group.sh": true,
	"scripts/ci/analysis-shards-go/main.go": true, "scripts/ci/test-race-packages.sh": true,
	"scripts/ci/test-core-packages.sh": true, "scripts/release/prepare-image-root.sh": true,
}

// GitHub lists at most 3000 files for a pull request.
const pullFilePages = 30

type pullFile struct {
	Name     string `json:"filename"`
	Previous string `json:"previous_filename"`
	Status   string `json:"status"`
}

// A PR may carry a policy path only when it already matches main, as a backmerge
// does. The PR's file list starts at the merge base, so feature branches can lag a
// CI change on main.
func (q *queueController) devPolicyRefusal(ctx context.Context, p pull) (string, error) {
	var files []pullFile
	complete := false
	for page := 1; page <= pullFilePages && !complete; page++ {
		var list []pullFile
		if err := q.api.read(ctx, q.endpoint(fmt.Sprintf("pulls/%d/files?per_page=100&page=%d", p.Number, page)), &list); err != nil {
			return "", err
		}
		files = append(files, list...)
		complete = len(list) < 100
	}
	if !complete {
		return "This PR changes too many files for GitHub to list, so CI definition changes cannot be ruled out.", nil
	}
	var mainRef struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := q.api.read(ctx, q.endpoint("git/ref/heads/main"), &mainRef); err != nil {
		return "", err
	}
	if !shaPattern.MatchString(mainRef.Object.SHA) {
		return "", errors.New("invalid trusted main identity for dev policy paths")
	}
	refusal := func(path string) string {
		return "CI and publisher definitions land through main first, then a backmerge: " + path
	}
	for _, file := range files {
		if devPolicyPaths[file.Previous] {
			return refusal(file.Previous), nil
		}
		if !devPolicyPaths[file.Name] {
			continue
		}
		if file.Status == "removed" {
			return refusal(file.Name), nil
		}
		trusted, err := q.contents(ctx, file.Name, mainRef.Object.SHA)
		if err != nil {
			return "", err
		}
		candidate, err := q.contents(ctx, file.Name, p.Head.SHA)
		if err != nil {
			return "", err
		}
		if !bytes.Equal(trusted, candidate) {
			return refusal(file.Name), nil
		}
	}
	return "", nil
}

func (q *queueController) devPublisherContract(ctx context.Context, p pull) error {
	if q.checkName != devCheckName {
		return nil
	}
	var mainRef struct {
		Ref    string `json:"ref"`
		Object struct {
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"object"`
	}
	if err := q.api.read(ctx, q.endpoint("git/ref/heads/main"), &mainRef); err != nil {
		return err
	}
	if mainRef.Ref != "refs/heads/main" || mainRef.Object.Type != "commit" || !shaPattern.MatchString(mainRef.Object.SHA) {
		return errors.New("invalid trusted publisher authority")
	}
	trusted, err := q.contents(ctx, devGateWorkflowPath, mainRef.Object.SHA)
	if err != nil {
		return err
	}
	for _, revision := range []string{p.Base.SHA, p.Head.SHA} {
		candidate, err := q.contents(ctx, devGateWorkflowPath, revision)
		if err != nil {
			return err
		}
		if !bytes.Equal(trusted, candidate) {
			// A main-side publisher repair must be able to flow back into
			// dev. Only the exact authenticated main head can restore an
			// older base definition; the proposed head must still match main.
			if revision == p.Base.SHA && p.Head.SHA == mainRef.Object.SHA && p.Head.Repo.ID == q.repo.ID && strings.EqualFold(p.Head.Repo.FullName, q.repoName) {
				continue
			}
			return errors.New("dev publisher changes must land through the full main promotion gate first")
		}
	}
	return nil
}
