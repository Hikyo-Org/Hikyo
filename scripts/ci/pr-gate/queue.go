package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const queueCheckName = "ci-queue-required"
const queueWorkflowPath = ".github/workflows/ci-merge-group.yml"
const queueRulesetID = 20539346
const feedbackWorkflowPath = ".github/workflows/ci-feedback.yml"

// GitHub can skip a job before expanding its matrix/name expression, and may
// normalize its parentheses. Match only observed canonical spellings of this
// ignored job, never an arbitrary successful/skipped run.
const feedbackNameExpression = "${{ github.event.action == 'edited' && github.event.changes.base.ref.from == '' && 'metadata (ignored)' || 'feedback' }} (${{ matrix.lane }})"

// The Actions API also returns this parenthesized serialization before matrix expansion.
const normalizedFeedbackNameExpression = normalizedSkippedFeedbackName
const skippedFeedbackName = "github.event.action == 'edited' && github.event.changes.base.ref.from == '' && 'metadata (ignored)' || 'feedback'"
const normalizedSkippedFeedbackName = "${{ (((github.event.action == 'edited') && (github.event.changes.base.ref.from == '') && 'metadata (ignored)') || 'feedback') }} (${{ matrix.lane }})"

func newBinding(repoID int64, p pull, r *run) binding {
	return (snapshot{PR: p, Run: r}).binding(repoID)
}

func (q *queueController) contents(ctx context.Context, path, revision string) ([]byte, error) {
	var file struct {
		Type     string `json:"type"`
		Path     string `json:"path"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := q.api.read(ctx, q.endpoint("contents/"+path+"?ref="+revision), &file); err != nil {
		return nil, err
	}
	if file.Type != "file" || file.Path != path || file.Encoding != "base64" {
		return nil, errors.New("invalid immutable queue workflow contents")
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	if err != nil || len(data) == 0 {
		return nil, errors.New("invalid immutable queue workflow bytes")
	}
	return data, nil
}

type queueController struct {
	*controller
	dco        queueDCOVerifier
	targeted   bool
	repairOnly bool
	admissions map[int]*admissionTrace
}
type rule struct {
	Type       string          `json:"type"`
	Parameters json.RawMessage `json:"parameters"`
}
type queueParameters struct {
	Build    int    `json:"max_entries_to_build"`
	Minimum  int    `json:"min_entries_to_merge"`
	Maximum  int    `json:"max_entries_to_merge"`
	Wait     int    `json:"min_entries_to_merge_wait_minutes"`
	Timeout  int    `json:"check_response_timeout_minutes"`
	Strategy string `json:"grouping_strategy"`
	Method   string `json:"merge_method"`
}
type statusParameters struct {
	Strict *bool `json:"strict_required_status_checks_policy"`
	Checks []struct {
		Context string `json:"context"`
	} `json:"required_status_checks"`
}
type liveQueuePolicy struct {
	Enabled bool
	Digest  [sha256.Size]byte
}

func (q *queueController) policy(ctx context.Context) (liveQueuePolicy, error) {
	var owner struct {
		ID          int               `json:"id"`
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
	var effective []rule
	if err := parallelReads(ctx,
		func(ctx context.Context) error {
			return q.api.read(ctx, q.endpoint(fmt.Sprintf("rulesets/%d", queueRulesetID)), &owner)
		},
		func(ctx context.Context) error {
			return q.api.read(ctx, q.endpoint("rules/branches/"+url.PathEscape(q.repo.DefaultBranch)), &effective)
		},
	); err != nil {
		return liveQueuePolicy{}, err
	}
	// GitHub redacts bypass_actors without ruleset write access. Unknown is not
	// empty: the operator proves zero bypass during activation/readback. A
	// bypass actor already bypasses both the legacy check and mandatory queue;
	// this runtime contract protects actors subject to the effective rules.
	if _, err := fmt.Fprintf(q.output, "Queue policy capability: bypass actors observable=%t; owning/effective rule metadata read.\n", owner.Bypass != nil); err != nil {
		return liveQueuePolicy{}, err
	}
	if owner.ID != queueRulesetID || owner.Target != "branch" || owner.Enforcement != "active" || len(owner.Bypass) != 0 || len(owner.Conditions.Ref.Include) != 1 || owner.Conditions.Ref.Include[0] != "refs/heads/"+q.repo.DefaultBranch || len(owner.Conditions.Ref.Exclude) != 0 {
		return liveQueuePolicy{}, errors.New("invalid owning merge-queue protection")
	}
	if effective == nil {
		return liveQueuePolicy{}, errors.New("missing effective branch protection")
	}
	enabled := false
	for _, r := range owner.Rules {
		if r.Type == "merge_queue" {
			enabled = true
		}
	}
	if enabled {
		for _, rules := range [][]rule{owner.Rules, effective} {
			seen := make(map[string]int)
			for _, r := range rules {
				seen[r.Type]++
				switch r.Type {
				case "merge_queue":
					var p queueParameters
					if err := decode(r.Parameters, &p); err != nil {
						return liveQueuePolicy{}, err
					}
					if p.Build != 1 || p.Minimum != 1 || p.Maximum != 1 || p.Wait != 0 || p.Timeout < 60 || p.Timeout > 360 || p.Strategy != "ALLGREEN" || p.Method != "MERGE" {
						return liveQueuePolicy{}, errors.New("unsafe merge-queue parameters")
					}
				case "required_status_checks":
					var p statusParameters
					if err := decode(r.Parameters, &p); err != nil {
						return liveQueuePolicy{}, err
					}
					if p.Strict == nil || *p.Strict || len(p.Checks) != 1 || p.Checks[0].Context != queueCheckName {
						return liveQueuePolicy{}, errors.New("queue admission is not protected by its distinct required context")
					}
				case "pull_request":
					var p struct {
						Threads *bool `json:"required_review_thread_resolution"`
						Stale   *bool `json:"dismiss_stale_reviews_on_push"`
					}
					if err := decode(r.Parameters, &p); err != nil {
						return liveQueuePolicy{}, err
					}
					if p.Threads == nil || !*p.Threads || p.Stale == nil || !*p.Stale {
						return liveQueuePolicy{}, errors.New("queue protection lost review safeguards")
					}
				}
			}
			for _, kind := range []string{"merge_queue", "required_status_checks", "required_signatures", "pull_request", "deletion", "non_fast_forward"} {
				if seen[kind] != 1 {
					return liveQueuePolicy{}, fmt.Errorf("ambiguous or missing effective %s protection", kind)
				}
			}
		}
	} else {
		for _, r := range effective {
			if r.Type == "merge_queue" {
				return liveQueuePolicy{}, errors.New("queue policy does not belong to its pinned no-bypass ruleset")
			}
		}
	}
	// Preserve every owning/effective rule parameter in the final publication
	// comparison, including protections not interpreted by this controller.
	encoded, err := json.Marshal(struct {
		Owner     any
		Effective []rule
	}{owner, effective})
	if err != nil {
		return liveQueuePolicy{}, err
	}
	return liveQueuePolicy{Enabled: enabled, Digest: sha256.Sum256(encoded)}, nil
}

func (q *queueController) execute(ctx context.Context, name string, e event) error {
	if q.checkName != queueCheckName {
		return errors.New("queue publisher cannot write the legacy full-proof context")
	}
	if q.repairOnly && name != "workflow_dispatch" {
		return errors.New("group-only queue repair requires authenticated manual dispatch")
	}
	if q.targeted && (name != "pull_request_target" && name != "workflow_dispatch" && (name != "workflow_run" || e.Run.Event != "pull_request")) {
		return errors.New("targeted admission cannot execute a group or batch repair")
	}
	q.repo = repository{}
	if err := q.init(ctx); err != nil {
		return q.initializedFailure(ctx, e.Repository.ID, e.Repository.FullName, err)
	}
	if e.Repository.ID != q.repo.ID || !strings.EqualFold(e.Repository.FullName, q.repoName) {
		return errors.New("queue event repository identity mismatch")
	}
	return q.reassessHeld(ctx, q.rereadsGroup(name == "workflow_run" && e.Run.Event == "merge_group"), func(ctx context.Context) error { return q.executeProof(ctx, name, e) })
}

// The one test for waiting on GitHub metadata, shared by the queue-gate group
// callback and the group head worker (isGroup). Targeted admission, group-only
// repair, schedule and every other worker role stay bounded and reassess once.
func (q *queueController) rereadsGroup(isGroup bool) bool {
	return isGroup && !q.targeted && !q.repairOnly
}

func (q *queueController) executeProof(ctx context.Context, name string, e event) error {
	if q.repairOnly {
		if name != "workflow_dispatch" {
			return errors.New("group-only queue repair requires authenticated manual dispatch")
		}
		return q.repairGroups(ctx)
	}
	if q.targeted && (name != "pull_request_target" && name != "workflow_dispatch" && (name != "workflow_run" || e.Run.Event != "pull_request")) {
		return errors.New("targeted admission cannot execute a group or batch repair")
	}
	if name == "workflow_run" && e.Run.Event == "merge_group" {
		return q.group(ctx, e.Run)
	}
	var numbers []int
	switch name {
	case "pull_request_target":
		if e.Number <= 0 || e.Pull.Number != e.Number {
			return errors.New("invalid queue PR event")
		}
		numbers = []int{e.Number}
	case "workflow_dispatch":
		n, err := strconv.Atoi(e.Inputs.PR)
		if err != nil || n <= 0 {
			return errors.New("queue dispatch requires PR number")
		}
		numbers = []int{n}
	case "workflow_run":
		if e.Run.Event != "pull_request" || e.Run.Path != feedbackWorkflowPath || e.Run.Repository.ID != q.repo.ID || !shaPattern.MatchString(e.Run.HeadSHA) {
			return errors.New("invalid feedback callback")
		}
		// Routing is only a hint. Admission independently finds current exact-head
		// useful feedback and live PR authority, never trusting event associations.
		pulls, err := q.openPRs(ctx)
		if err != nil {
			return err
		}
		for _, p := range pulls {
			if p.Head.SHA == e.Run.HeadSHA {
				numbers = append(numbers, p.Number)
			}
		}
	case "schedule", "push":
		if name == "push" && e.Ref != "refs/heads/"+q.repo.DefaultBranch {
			return nil
		}
		pulls, err := q.openPRs(ctx)
		if err != nil {
			return err
		}
		for _, p := range pulls {
			numbers = append(numbers, p.Number)
		}
	default:
		return errors.New("unsupported queue publisher event")
	}
	var failures []error
	if name == "workflow_run" && len(numbers) == 0 {
		return q.retiredHead(ctx)
	}
	for _, n := range numbers {
		if err := q.admit(ctx, n); err != nil {
			if !onlyStaleHeadWork(err) {
				failures = append(failures, fmt.Errorf("queue PR #%d: %w", n, err))
			}
		}
	}
	if !q.targeted && (name == "schedule" || name == "push" || name == "workflow_dispatch") {
		if err := q.repairGroups(ctx); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (q *queueController) admit(ctx context.Context, number int) error {
	return q.admitCaptured(ctx, number, nil)
}

func (q *queueController) admitCaptured(ctx context.Context, number int, captured *admissionSnapshot) error {
	return q.reassess(ctx, func(ctx context.Context) error { return q.admitProof(ctx, number, captured) })
}

func (q *queueController) admitProof(ctx context.Context, number int, captured *admissionSnapshot) error {
	dcoVerifier := q.dco
	if dcoVerifier == nil {
		var close func()
		dcoVerifier, close = admissionDCOVerifier(q.repoName)
		defer close()
	}
	p, err := q.readPR(ctx, number)
	if err != nil {
		return err
	}
	if p.State != "open" {
		return q.retiredHead(ctx)
	}
	if p.Head.SHA != q.expectedHead {
		return errors.Join(&staleHeadWork{expected: q.expectedHead}, q.retiredHead(ctx))
	}
	if captured != nil {
		initial, exists := captured.roles.AllPulls[number]
		// REST Base.SHA may lag main; readPR above resolves the live branch.
		// Every other captured PR/actor/reference identity must still match.
		if !exists || !sameRolePull(initial, p) {
			return &staleHeadWork{expected: q.expectedHead}
		}
	}
	var trace *admissionTrace
	if q.targeted {
		if q.admissions == nil {
			q.admissions = make(map[int]*admissionTrace)
		}
		trace = &admissionTrace{PR: p}
		q.admissions[number] = trace
	}
	b := newBinding(q.repo.ID, p, nil)
	result := pending("Merge queue is not active; the legacy full-candidate check remains required.")
	var policy liveQueuePolicy
	if captured != nil {
		policy = captured.policy
	} else {
		policy, err = q.policy(ctx)
		if err != nil {
			return err
		}
	}
	var earlyFeedback *run
	if trace != nil && policy.Enabled {
		trace.Policy = &policy
		earlyFeedback, err = q.feedback(ctx, p)
		if err != nil {
			return err
		}
		trace.Run = earlyFeedback
	}
	var initialRoles headRoles
	if policy.Enabled {
		if captured != nil {
			initialRoles = captured.roles
		} else {
			initialRoles, err = q.roles(ctx, p.Head.SHA)
			if err != nil {
				return err
			}
			current, exists := initialRoles.AllPulls[number]
			if !exists || !sameRolePull(current, p) {
				return &staleHeadWork{expected: q.expectedHead}
			}
		}
		if initialRoles.Group && len(initialRoles.Pulls) > 0 {
			return q.roleCollision(ctx, initialRoles)
		}
	}
	var selectedRun *run
	if policy.Enabled {
		if p.Base.Ref != q.repo.DefaultBranch {
			result = refused("Only the protected default branch can enter this merge queue.")
		} else {
			decision, err := q.verify(ctx, p.Number, p.Head.SHA)
			if err != nil {
				return err
			}
			switch decision.Code {
			case 75:
				result = pending("Trusted exact-head admission policy is pending.")
			case 1:
				result = refused("Trusted exact-head admission policy refused this PR.")
			case 0:
				if p.Head.Repo.ID != q.repo.ID {
					vouched, err := q.currentVouch(ctx, p)
					if err != nil {
						return err
					}
					if !vouched {
						result = refused("Fork admission is not currently vouched.")
						break
					}
				}
				collision := false
				for _, other := range initialRoles.Pulls {
					if other.Number != p.Number && other.Head.SHA == p.Head.SHA {
						collision = true
						break
					}
				}
				if collision {
					result = refused("This head belongs to multiple open PRs; commit-level admission is ambiguous.")
					break
				}
				r := earlyFeedback
				if trace == nil {
					r, err = q.feedback(ctx, p)
				}
				if err != nil {
					return err
				}
				selectedRun = r
				if r == nil {
					result = pending("No useful exact-head fast feedback exists yet.")
					break
				}
				b = newBinding(q.repo.ID, p, r)
				if err := q.changedBinding(ctx, p, b); err != nil {
					return err
				}
				if r.Status != "completed" || r.Conclusion == "action_required" {
					result = pending("Exact-head fast feedback is still running or awaiting GitHub approval.")
					break
				}
				if r.Conclusion != "success" {
					result = refused("Exact-head fast feedback failed; queue admission remains blocked.")
					break
				}
				code, err := canonicalDCOResult(ctx, p, dcoVerifier)
				if err != nil {
					return err
				}
				if code != 0 {
					result = refused("Canonical DCO sign-off is missing from this PR range.")
					break
				}
				result = outcome{Status: "completed", Conclusion: "success", Title: "Ready for protected merge queue", Summary: "Admission only. The mandatory queue must validate its current-base candidate against the trusted change plan before merging."}
			default:
				return errors.New("invalid queue policy verifier outcome")
			}
		}
	}
	// This scan also establishes a blocking barrier if a foreign green appears
	// before a later PR read fails. Its recovery side effect is required even
	// though the final publication independently repeats the ownership scan.
	own, err := q.ownedChecks(ctx, p)
	if err != nil {
		return err
	}
	final, err := q.readPR(ctx, number)
	if err != nil {
		return err
	}
	if final.State != "open" {
		return q.retiredHead(ctx)
	}
	finalPolicy, err := q.policy(ctx)
	if err != nil {
		return err
	}
	if final.Head.SHA != p.Head.SHA || final.Base.SHA != p.Base.SHA || final.Base.Ref != p.Base.Ref || finalPolicy != policy {
		return q.queueMovement(ctx, number)
	}
	if result.Conclusion == "success" {
		decision, err := q.verify(ctx, p.Number, p.Head.SHA)
		if err != nil {
			return err
		}
		if decision.Code != 0 {
			return q.queueMovement(ctx, number)
		}
		if p.Head.Repo.ID != q.repo.ID {
			vouched, err := q.currentVouch(ctx, p)
			if err != nil {
				return err
			}
			if !vouched {
				return q.queueMovement(ctx, number)
			}
		}
		code, err := canonicalDCOResult(ctx, final, dcoVerifier)
		if err != nil {
			return err
		}
		if code != 0 {
			result = refused("Canonical DCO sign-off was refused during final admission verification.")
		}
		r, err := q.feedback(ctx, final)
		if err != nil {
			return err
		}
		if r == nil || selectedRun == nil || r.ID != selectedRun.ID || r.Attempt != selectedRun.Attempt || r.Status != "completed" || r.Conclusion != "success" {
			return q.queueMovement(ctx, number)
		}
	}
	if result.Conclusion == "success" {
		roles, err := q.roles(ctx, p.Head.SHA)
		if err != nil {
			return err
		}
		if roles.Group && len(roles.Pulls) > 0 {
			return q.roleCollision(ctx, roles)
		}
		present := false
		for _, current := range roles.Pulls {
			if current.Number == p.Number {
				if !sameRolePull(current, final) {
					return &staleHeadWork{expected: q.expectedHead}
				}
				present = true
			}
		}
		if !present {
			return &staleHeadWork{expected: q.expectedHead}
		}
		if len(roles.Pulls) != 1 {
			result = refused("This head now belongs to multiple open PRs; final role admission is ambiguous.")
		}
	}
	// A native or foreign same-name check can arrive during the final source
	// reads. Repeat the identity barrier immediately before any terminal write.
	own, err = q.ownedChecks(ctx, final)
	if err != nil {
		return err
	}
	var selected *check
	for i := range own {
		if selected == nil || own[i].ID > selected.ID {
			selected = &own[i]
		}
	}
	if trace != nil && selectedRun != nil {
		created, parseErr := time.Parse(time.RFC3339, selectedRun.CreatedAt)
		if parseErr == nil && !created.After(time.Now()) {
			fmt.Fprintf(q.output, "Admission timing: PR #%d head=%s feedback_run=%d attempt=%d source_event_elapsed_seconds=%.0f (diagnostic age at this reconciliation, includes any authority wait; not a push timestamp or first-eligibility proof).\n", number, p.Head.SHA, selectedRun.ID, selectedRun.Attempt, time.Since(created).Seconds())
		} else {
			fmt.Fprintln(q.output, "Admission timing: authenticated useful source creation time unavailable; no event-to-eligibility claim.")
		}
	}
	return q.publish(ctx, p, b, selected, result)
}
func (q *queueController) queueMovement(ctx context.Context, number int) error {
	return q.blockMovement(ctx, number, "queue admission final comparison")
}
func (q *queueController) sharedHead(ctx context.Context, p pull) (bool, error) {
	all, err := q.openPRs(ctx)
	if err != nil {
		return false, err
	}
	for _, other := range all {
		if other.Number != p.Number && other.Head.SHA == p.Head.SHA {
			return true, nil
		}
	}
	return false, nil
}

func (q *queueController) feedback(ctx context.Context, p pull) (*run, error) {
	var w workflow
	if err := q.api.read(ctx, q.endpoint("actions/workflows/ci-feedback.yml"), &w); err != nil {
		return nil, err
	}
	if w.ID <= 0 || w.Path != feedbackWorkflowPath || w.Name != "ci-feedback" {
		return nil, errors.New("invalid feedback workflow identity")
	}
	for page := 1; page <= 10; page++ {
		var list struct {
			Runs  []run `json:"workflow_runs"`
			Total int   `json:"total_count"`
		}
		path := q.endpoint(fmt.Sprintf("actions/workflows/ci-feedback.yml/runs?event=pull_request&head_sha=%s&per_page=100&page=%d", p.Head.SHA, page))
		if err := q.api.read(ctx, path, &list); err != nil {
			return nil, err
		}
		if list.Runs == nil || list.Total < 0 || list.Total > 1000 {
			return nil, errors.New("invalid feedback run list")
		}
		sort.Slice(list.Runs, func(i, j int) bool { return list.Runs[i].ID > list.Runs[j].ID })
		for _, r := range list.Runs {
			if r.ID <= 0 || r.Attempt <= 0 || r.WorkflowID != w.ID || r.Path != feedbackWorkflowPath || r.Event != "pull_request" || r.Repository.ID != q.repo.ID || !strings.EqualFold(r.Repository.FullName, q.repoName) || r.HeadSHA != p.Head.SHA || r.HeadRepository.ID != p.Head.Repo.ID || !strings.EqualFold(r.HeadRepository.FullName, p.Head.Repo.FullName) {
				return nil, errors.New("invalid exact-head feedback identity")
			}
			if len(r.Pulls) == 0 {
				if err := q.sourceBoundForkFeedback(ctx, r, p); err != nil {
					return nil, err
				}
			} else if len(r.Pulls) != 1 || r.Pulls[0].Number != p.Number || r.Pulls[0].Head.SHA != p.Head.SHA || r.Pulls[0].Base.Ref != p.Base.Ref || r.Pulls[0].Head.Repo.ID != p.Head.Repo.ID || r.Pulls[0].Base.Repo.ID != p.Base.Repo.ID {
				return nil, errors.New("feedback has contradictory or ambiguous PR association")
			}
			switch r.Status {
			case "queued", "in_progress", "completed", "waiting", "pending", "requested":
			default:
				return nil, errors.New("invalid feedback run status")
			}
			if r.Status == "completed" && (r.Conclusion == "action_required" || r.Conclusion == "failure" || r.Conclusion == "cancelled" || r.Conclusion == "timed_out" || r.Conclusion == "startup_failure") {
				return &r, nil
			}
			jobs, err := q.jobs(ctx, r)
			if err != nil {
				return nil, err
			}
			// GitHub can complete a skipped workflow before creating its matrix
			// placeholder job. Until that job proves this was an ignored metadata
			// event, keep admission pending and do not reuse an older success.
			if r.Status == "completed" && r.Conclusion == "skipped" && len(jobs) == 0 {
				_, err := fmt.Fprintf(q.output, "PR #%d head=%s feedback_run=%d attempt=%d: waiting for GitHub skipped jobs to materialize; admission remains pending.\n", p.Number, r.HeadSHA, r.ID, r.Attempt)
				// A failed diagnostic write is an operational error. The caller
				// still verifies an authenticated blocking barrier before return.
				return nil, err
			}
			if len(jobs) == 1 && (jobs[0].Name == skippedFeedbackName || jobs[0].Name == feedbackNameExpression || jobs[0].Name == normalizedFeedbackNameExpression) && jobs[0].Status == "completed" && jobs[0].Conclusion == "skipped" {
				continue
			}
			admitted, ignored := 0, 0
			lanes := make(map[string]int)
			for _, j := range jobs {
				lanes[j.Name]++
				if j.Name == "metadata (ignored) (go)" || j.Name == "metadata (ignored) (frontend)" {
					if j.Status != "completed" || j.Conclusion != "skipped" {
						return nil, errors.New("invalid ignored metadata feedback")
					}
					ignored++
				}
				if j.Name == "feedback (go)" || j.Name == "feedback (frontend)" {
					admitted++
					if j.HeadSHA != r.HeadSHA {
						return nil, errors.New("feedback job source mismatch")
					}
					if r.Status == "completed" && (j.Status != "completed" || j.Conclusion != "success") {
						r.Conclusion = "failure"
					}
				}
			}
			if ignored == 2 && admitted == 0 && lanes["metadata (ignored) (go)"] == 1 && lanes["metadata (ignored) (frontend)"] == 1 {
				continue
			}
			if r.Status == "completed" && (admitted != 2 || lanes["feedback (go)"] != 1 || lanes["feedback (frontend)"] != 1) {
				return nil, errors.New("missing or duplicated useful feedback lane")
			}
			return &r, nil
		}
		if len(list.Runs) < 100 {
			return nil, nil
		}
	}
	return nil, errors.New("feedback pagination limit reached")
}

func (q *queueController) sourceBoundForkFeedback(ctx context.Context, r run, p pull) error {
	// GitHub omits associations for some fork runs. Reuse exact source-head
	// feedback, not a claim about which historical PR triggered it. Current PR
	// uniqueness/refs, independent authority/vouch/DCO and the full queue-group
	// proof remain mandatory. Never fall back from contradictory associations.
	if p.Head.Repo.ID == q.repo.ID || r.HeadBranch == "" || r.HeadBranch != p.Head.Ref || (p.MergeCommitSHA != "" && !shaPattern.MatchString(p.MergeCommitSHA)) {
		return errors.New("fork feedback lacks a usable current source binding")
	}
	// Exact refs and immutable parents bind this source even while GitHub
	// recomputes its mergeability field. Missing source proof is an error.
	if p.MergeCommitSHA == "" || p.Mergeable != nil && !*p.Mergeable {
		return errors.New("fork feedback lacks a usable current source binding")
	}
	shared, err := q.sharedHead(ctx, p)
	if err != nil {
		return err
	}
	if shared {
		return errors.New("fork feedback source belongs to multiple open PRs")
	}
	for _, kind := range []string{"head", "merge"} {
		expected := p.Head.SHA
		if kind == "merge" {
			expected = p.MergeCommitSHA
		}
		var ref struct {
			Ref    string `json:"ref"`
			Object struct {
				Type string `json:"type"`
				SHA  string `json:"sha"`
			} `json:"object"`
		}
		path := fmt.Sprintf("pull/%d/%s", p.Number, kind)
		if err := q.api.read(ctx, q.endpoint("git/ref/"+path), &ref); err != nil {
			return err
		}
		if ref.Ref != "refs/"+path || ref.Object.Type != "commit" || ref.Object.SHA != expected {
			return errors.New("fork feedback current PR ref mismatch")
		}
	}
	var commit struct {
		SHA     string `json:"sha"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if err := q.api.read(ctx, q.endpoint("git/commits/"+p.MergeCommitSHA), &commit); err != nil {
		return err
	}
	if commit.SHA != p.MergeCommitSHA || len(commit.Parents) != 2 || commit.Parents[0].SHA != p.Base.SHA || commit.Parents[1].SHA != p.Head.SHA {
		return errors.New("fork feedback current integration identity mismatch")
	}
	return nil
}

// Queue metadata is API data only. Entries are not artifacts and cannot
// provide executable instructions to the privileged controller.
type queueEntry struct {
	ID       string `json:"id"`
	Position int    `json:"position"`
	State    string `json:"state"`
	Head     struct {
		OID string `json:"oid"`
	} `json:"headCommit"`
	Base struct {
		OID string `json:"oid"`
	} `json:"baseCommit"`
	PR struct {
		Number int    `json:"number"`
		Head   string `json:"headRefOid"`
		Base   string `json:"baseRefName"`
	} `json:"pullRequest"`
}
type queueSnapshot struct {
	ID      string
	Entries []queueEntry
	Base    string
	Digest  [sha256.Size]byte
}

type queueData struct {
	ID      string `json:"id"`
	Entries struct {
		Nodes []queueEntry `json:"nodes"`
		Page  struct {
			More *bool   `json:"hasNextPage"`
			End  *string `json:"endCursor"`
		} `json:"pageInfo"`
	} `json:"entries"`
}

func graphQLDiagnostic(raw []json.RawMessage) error {
	var first struct {
		Message    string `json:"message"`
		Type       string `json:"type"`
		Path       []any  `json:"path"`
		Extensions struct {
			Code string `json:"code"`
		} `json:"extensions"`
	}
	if len(raw) == 0 || decode(raw[0], &first) != nil || strings.TrimSpace(first.Message) == "" {
		return errors.New("merge queue GraphQL errors: missing or malformed server error detail")
	}
	// Render only known diagnostic fields, never the response or arbitrary
	// extensions. Quote controls, bound output, and redact known auth values.
	field := func(value string, limit int) string {
		for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
			if token := os.Getenv(key); token != "" {
				value = strings.ReplaceAll(value, token, "[REDACTED]")
			}
		}
		runes := []rune(value)
		if len(runes) > limit {
			return string(runes[:limit]) + "...[truncated]"
		}
		return value
	}
	path := "<absent>"
	validPath := true
	for _, component := range first.Path {
		switch component.(type) {
		case string, float64:
		default:
			validPath = false
		}
	}
	if validPath && first.Path != nil {
		encoded, _ := json.Marshal(first.Path)
		path = string(encoded)
	} else if !validPath {
		path = "<invalid>"
	}
	return fmt.Errorf("merge queue GraphQL errors (%d): message=%q type=%q code=%q path=%q", len(raw), field(first.Message, 256), field(first.Type, 64), field(first.Extensions.Code, 64), field(path, 256))
}

func (q *queueController) readQueueSnapshot(ctx context.Context) (*queueData, error) {
	parts := strings.Split(q.repoName, "/")
	live := &queueData{}
	seenCursors := make(map[string]bool)
	seenEntries := make(map[string]bool)
	seenPRs := make(map[int]bool)
	seenPositions := make(map[int]bool)
	after := ""
	// Bound work without treating a truncated queue as an authoritative prefix.
	// Every group proof and repair uses the same complete snapshot reader.
	for page := 1; page <= 20; page++ {
		args := "first:100"
		if after != "" {
			args += fmt.Sprintf(",after:%q", after)
		}
		query := fmt.Sprintf(`query{repository(owner:%q,name:%q){mergeQueue(branch:%q){id entries(%s){nodes{id position state headCommit{oid} baseCommit{oid} pullRequest{number headRefOid baseRefName}} pageInfo{hasNextPage endCursor}}}}}`, parts[0], parts[1], q.repo.DefaultBranch, args)
		var response struct {
			Errors []json.RawMessage `json:"errors"`
			Data   struct {
				Repo struct {
					Queue *queueData `json:"mergeQueue"`
				} `json:"repository"`
			} `json:"data"`
		}
		if err := q.api.read(ctx, "graphql:"+query, &response); err != nil {
			return nil, fmt.Errorf("reading merge queue page %d: %w", page, err)
		}
		if len(response.Errors) != 0 {
			return nil, fmt.Errorf("reading merge queue page %d: %w", page, graphQLDiagnostic(response.Errors))
		}
		part := response.Data.Repo.Queue
		if part == nil || part.ID == "" || part.Entries.Nodes == nil || part.Entries.Page.More == nil || len(part.Entries.Nodes) > 100 {
			return nil, errors.New("missing or incomplete authenticated queue membership")
		}
		if page == 1 {
			live.ID = part.ID
		} else if live.ID != part.ID {
			return nil, &queueSnapshotChanged{reason: "merge queue identity changed between pages"}
		}
		pageEntries, pagePRs, pagePositions := make(map[string]bool), make(map[int]bool), make(map[int]bool)
		for _, entry := range part.Entries.Nodes {
			if entry.ID == "" || entry.PR.Number <= 0 || entry.Position <= 0 || pageEntries[entry.ID] || pagePRs[entry.PR.Number] || pagePositions[entry.Position] {
				return nil, errors.New("duplicate or invalid paginated queue entry")
			}
			pageEntries[entry.ID], pagePRs[entry.PR.Number], pagePositions[entry.Position] = true, true, true
		}
		// A malformed page stays fatal even if an earlier entry also repeated
		// across pages. Only movement between otherwise valid pages is retried.
		for _, entry := range part.Entries.Nodes {
			if seenEntries[entry.ID] || seenPRs[entry.PR.Number] || seenPositions[entry.Position] {
				return nil, &queueSnapshotChanged{reason: fmt.Sprintf("queue entry repeated across pages: page=%d position=%d PR=%d", page, entry.Position, entry.PR.Number)}
			}
			seenEntries[entry.ID], seenPRs[entry.PR.Number], seenPositions[entry.Position] = true, true, true
		}
		live.Entries.Nodes = append(live.Entries.Nodes, part.Entries.Nodes...)
		if !*part.Entries.Page.More {
			if live.Entries.Nodes == nil {
				live.Entries.Nodes = []queueEntry{}
			}
			for position := 1; position <= len(live.Entries.Nodes); position++ {
				if !seenPositions[position] {
					return nil, incompleteQueuePositions(live, page, position)
				}
			}
			return live, nil
		}
		if len(part.Entries.Nodes) == 0 || part.Entries.Page.End == nil || *part.Entries.Page.End == "" || len(*part.Entries.Page.End) > 1024 || seenCursors[*part.Entries.Page.End] {
			return nil, errors.New("missing or repeated merge queue cursor")
		}
		after = *part.Entries.Page.End
		seenCursors[after] = true
	}
	return nil, errors.New("merge queue pagination limit reached")
}
func (q *queueController) queue(ctx context.Context, head string) (queueSnapshot, error) {
	live, err := q.liveQueue(ctx)
	if err != nil {
		return queueSnapshot{}, err
	}
	return q.snapshotQueue(live, head)
}

func (q *queueController) snapshotQueue(live *queueData, head string) (queueSnapshot, error) {
	sort.Slice(live.Entries.Nodes, func(i, j int) bool { return live.Entries.Nodes[i].Position < live.Entries.Nodes[j].Position })
	snap := queueSnapshot{ID: live.ID}
	seen := make(map[string]bool)
	seenPR := make(map[int]bool)
	found := false
	matching := 0
	for _, entry := range live.Entries.Nodes {
		if entry.Head.OID == head {
			matching++
		}
	}
	if matching > 1 {
		return queueSnapshot{}, errors.New("ambiguous authenticated group-head membership")
	}
	for i, entry := range live.Entries.Nodes {
		if entry.State != "QUEUED" && entry.State != "AWAITING_CHECKS" && entry.State != "MERGEABLE" && entry.State != "UNMERGEABLE" && entry.State != "LOCKED" {
			return queueSnapshot{}, errors.New("invalid authenticated queue entry state")
		}
		// An absent group may have a queued successor with no group commit yet.
		// Still validate all membership identities before reporting absence. A
		// successor cannot supply the missing commits of an active group prefix.
		waiting := matching == 0 && entry.State == "QUEUED" && entry.Head.OID == "" && (entry.Base.OID == "" || shaPattern.MatchString(entry.Base.OID))
		if entry.ID == "" || seen[entry.ID] || seenPR[entry.PR.Number] || entry.Position != i+1 || entry.PR.Number <= 0 || entry.PR.Base != q.repo.DefaultBranch || !shaPattern.MatchString(entry.PR.Head) || !waiting && (!shaPattern.MatchString(entry.Head.OID) || !shaPattern.MatchString(entry.Base.OID)) {
			return queueSnapshot{}, fmt.Errorf("invalid ordered merge queue entry: position=%d expected=%d PR=%d state=%q id_present=%t duplicate_id=%t duplicate_pr=%t base_ref_matches=%t pr_head_valid=%t group_head_valid=%t base_commit_valid=%t", entry.Position, i+1, entry.PR.Number, entry.State, entry.ID != "", seen[entry.ID], seenPR[entry.PR.Number], entry.PR.Base == q.repo.DefaultBranch, shaPattern.MatchString(entry.PR.Head), shaPattern.MatchString(entry.Head.OID), shaPattern.MatchString(entry.Base.OID))
		}
		seen[entry.ID] = true
		seenPR[entry.PR.Number] = true
		snap.Entries = append(snap.Entries, entry)
		if i == 0 {
			snap.Base = entry.Base.OID
		}
		if entry.Head.OID == head {
			found = true
			break
		}
	}
	if !found {
		return queueSnapshot{}, &staleQueueGroup{reason: "tested group is absent from the live queue"}
	}
	encoded, err := json.Marshal(struct {
		ID      string
		Entries []queueEntry
	}{snap.ID, snap.Entries})
	if err != nil {
		return snap, err
	}
	snap.Digest = sha256.Sum256(encoded)
	return snap, nil
}

func (q *queueController) group(ctx context.Context, hint run) error {
	if q.repo.ID == 0 {
		if err := q.init(ctx); err != nil {
			return err
		}
	}
	if hint.ID <= 0 || hint.Attempt <= 0 || hint.Event != "merge_group" || hint.Path != queueWorkflowPath || hint.Repository.ID != q.repo.ID || !shaPattern.MatchString(hint.HeadSHA) || !strings.HasPrefix(hint.HeadBranch, "gh-readonly-queue/"+q.repo.DefaultBranch+"/") {
		return errors.New("invalid merge group callback")
	}
	if hint.HeadSHA != q.expectedHead {
		return &staleHeadWork{expected: q.expectedHead}
	}
	return q.reassess(ctx, func(ctx context.Context) error { return q.groupProof(ctx, hint) })
}

func (q *queueController) groupProof(ctx context.Context, hint run) (err error) {
	defer func() {
		var stale *staleQueueGroup
		if errors.As(err, &stale) {
			_, writeErr := fmt.Fprintln(q.output, "Merge group pending:", stale.reason)
			err = errors.Join(writeErr, q.retiredGroup(ctx))
			return
		}
		err = q.retireMovedGroup(ctx, err)
	}()

	// A valid callback establishes held-head identity. Final proof adds queue
	// membership; any incomplete or uncertain exit verifies a blocking verdict.
	p := pull{Number: 1, State: "open", Head: reference{SHA: hint.HeadSHA, Repo: q.repo}, Base: reference{SHA: hint.HeadSHA, Ref: hint.HeadBranch, Repo: q.repo}}
	b := newBinding(q.repo.ID, p, nil)
	policy, err := q.policy(ctx)
	if err != nil {
		return err
	}
	if !policy.Enabled {
		return nil
	}
	roles, err := q.roles(ctx, hint.HeadSHA)
	if err != nil {
		return err
	}
	if roles.Group && len(roles.Pulls) > 0 {
		return q.roleCollision(ctx, roles)
	}
	if !roles.Group && len(roles.Pulls) == 0 {
		return q.retiredHead(ctx)
	}
	current, err := q.latestGroup(ctx, hint.HeadSHA)
	if err != nil {
		return err
	}
	if current == nil {
		return errors.New("no authenticated merge-group source exists")
	}
	r := *current
	var w workflow
	if err := q.api.read(ctx, q.endpoint("actions/workflows/ci-merge-group.yml"), &w); err != nil {
		return err
	}
	if w.ID != r.WorkflowID || w.Path != queueWorkflowPath || w.Name != "trusted-merge-ci" {
		return errors.New("merge group workflow identity mismatch")
	}
	if r.Status != "completed" {
		return nil
	}
	snap, err := q.queue(ctx, r.HeadSHA)
	if err != nil {
		return err
	}
	if q.queueRefresh != nil {
		if err := q.queueRefresh.proveGroup(&finishedGroupProof{run: r, policy: policy, snapshot: snap}); err != nil {
			return err
		}
	}
	p.Number = snap.Entries[len(snap.Entries)-1].PR.Number
	members, authorized, err := q.groupMembers(ctx, snap)
	_, authorityPending := err.(*groupAuthorityPending)
	if err != nil && !authorityPending {
		return err
	}
	p.Base.SHA = members[0].Base.SHA
	b = newBinding(q.repo.ID, p, &r)
	b.BaseRef = sha256.Sum256(append(snap.Digest[:], []byte(r.HeadBranch)...))
	if err := q.changedBinding(ctx, p, b); err != nil {
		return err
	}
	if err := q.proveCurrentBase(ctx, p.Base.SHA, r.HeadSHA); err != nil {
		return err
	}
	if err := q.compatibleDefinition(ctx, snap.Base, p.Base.SHA); err != nil {
		return err
	}
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	if err := q.api.read(ctx, q.endpoint("git/ref/heads/"+url.PathEscape(r.HeadBranch)), &ref); err != nil {
		var failure *githubReadError
		if errors.As(err, &failure) && failure.status == 404 {
			return &staleQueueGroup{reason: "tested merge group ref was deleted"}
		}
		return err
	}
	if ref.Ref != "refs/heads/"+r.HeadBranch || ref.Object.Type != "commit" || ref.Object.SHA != r.HeadSHA {
		return errors.New("merge group ref was deleted or rebuilt")
	}
	if err := q.groupSource(ctx, r, p.Base.SHA, authorized); err != nil {
		return err
	}
	jobs, err := q.jobs(ctx, r)
	if err != nil {
		return err
	}
	summary := summarizeProofJobs(jobs, r)
	if len(summary.unsettled) > 0 {
		return &groupProofIncomplete{jobs: summary.unsettled, run: r}
	}
	counts, ok := summary.counts, summary.ok
	if counts["validation / ci-required"] != 1 || counts["merge-group-proof"] != 1 {
		return errors.New("missing or duplicated full merge-group proof")
	}
	result := refused("Required merge-group validation failed; queue merge remains blocked.")
	if ok && r.Conclusion == "success" && authorityPending {
		result = refused("Validation finished but exact-head group member authority could not be established; retry after resolving the policy failure.")
	} else if ok && r.Conclusion == "success" {
		result = outcome{Status: "completed", Conclusion: "success", Title: "Merge group validated", Summary: fmt.Sprintf("All domains required by the trusted plan passed on %s; authenticated queue membership, current base, source run %d attempt %d and authority verified.", r.HeadSHA, r.ID, r.Attempt)}
	}
	finalPolicy, err := q.policy(ctx)
	if err != nil {
		return err
	}
	final, err := q.queue(ctx, r.HeadSHA)
	if err != nil {
		return err
	}
	if finalPolicy != policy {
		return errors.New("merge queue policy changed before publication")
	}
	if final.Digest != snap.Digest {
		return &queueSnapshotChanged{reason: "merge queue changed before publication"}
	}
	finalMembers, finalAuthorized, err := q.groupMembers(ctx, final)
	_, finalAuthorityPending := err.(*groupAuthorityPending)
	if err != nil && !finalAuthorityPending {
		return err
	}
	if finalAuthorityPending && result.Conclusion == "success" {
		result = refused("Validation finished but exact-head group member authority could not be established; retry after resolving the policy failure.")
	}
	if len(members) != len(finalMembers) {
		return errors.New("merge-group members changed")
	}
	for i := range members {
		if members[i].Head.SHA != finalMembers[i].Head.SHA || members[i].Base.SHA != finalMembers[i].Base.SHA {
			return errors.New("merge-group member identity changed")
		}
	}
	if err := q.groupSource(ctx, r, finalMembers[0].Base.SHA, finalAuthorized); err != nil {
		return err
	}
	if err := q.api.read(ctx, q.endpoint("git/ref/heads/"+url.PathEscape(r.HeadBranch)), &ref); err != nil {
		var failure *githubReadError
		if errors.As(err, &failure) && failure.status == 404 {
			return &staleQueueGroup{reason: "tested merge group ref was deleted before publication"}
		}
		return err
	}
	if ref.Ref != "refs/heads/"+r.HeadBranch || ref.Object.Type != "commit" || ref.Object.SHA != r.HeadSHA {
		return errors.New("merge group ref changed before publication")
	}
	if err := q.proveCurrentBase(ctx, finalMembers[0].Base.SHA, r.HeadSHA); err != nil {
		return err
	}
	if err := q.compatibleDefinition(ctx, final.Base, finalMembers[0].Base.SHA); err != nil {
		return err
	}
	last, err := q.latestGroup(ctx, r.HeadSHA)
	if err != nil {
		return err
	}
	if last == nil || !sameSourceEvent(r, *last) || last.HeadBranch != r.HeadBranch || last.Status != r.Status || last.Conclusion != r.Conclusion {
		return errors.New("merge group attempt changed before publication")
	}
	roles, err = q.roles(ctx, r.HeadSHA)
	if err != nil {
		return err
	}
	if roles.Group && len(roles.Pulls) > 0 {
		return q.roleCollision(ctx, roles)
	}
	if roles.Snapshot == nil || roles.Snapshot.Digest != final.Digest {
		return &staleQueueGroup{reason: "final role snapshot no longer contains the exact tested group membership"}
	}
	for _, member := range finalMembers {
		current, present := roles.AllPulls[member.Number]
		if !present || !sameRolePull(current, member) {
			return &staleQueueGroup{reason: "current member changed in final role snapshot"}
		}
	}
	own, err := q.groupChecks(ctx, p)
	if err != nil {
		return err
	}
	return q.publish(ctx, p, b, newestQueueCheck(own), result)
}

// groupProofIncomplete means the completed successful run's proof jobs are not
// yet listed as finished. It never authorizes a verdict; callers may only wait.
// run is the identity-checked run whose jobs lag, for a jobs-only poll.
type groupProofIncomplete struct {
	jobs []string
	run  run
}

func (e *groupProofIncomplete) Error() string {
	return "merge-group proof jobs are not yet final in GitHub job metadata: " + strings.Join(e.jobs, ", ")
}

type proofJobSummary struct {
	counts    map[string]int
	ok        bool     // every listed proof job succeeded on this head
	unsettled []string // set only for lag: waiting could still change the verdict
}

// summarizeProofJobs is the one definition of "lagging". A successful run whose
// proof jobs are still listed as unfinished or are not listed yet is GitHub
// metadata lag. Only a failed, mismatched or duplicated proof job is a verdict
// that waiting cannot change.
func summarizeProofJobs(jobs []job, r run) proofJobSummary {
	s := proofJobSummary{counts: make(map[string]int), ok: true}
	definite := false
	var unsettled []string
	for _, j := range jobs {
		if j.Name == "validation / ci-required" || j.Name == "merge-group-proof" {
			s.counts[j.Name]++
			good := j.Status == "completed" && j.Conclusion == "success" && j.HeadSHA == r.HeadSHA
			s.ok = s.ok && good
			switch {
			case good:
			case r.Conclusion == "success" && lagging(j, r.HeadSHA):
				unsettled = append(unsettled, j.Name+" ("+j.Status+")")
			default:
				definite = true
			}
		}
	}
	if s.counts["validation / ci-required"] <= 1 && s.counts["merge-group-proof"] <= 1 && !definite && r.Conclusion == "success" {
		for _, name := range []string{"validation / ci-required", "merge-group-proof"} {
			if s.counts[name] == 0 {
				unsettled = append(unsettled, name+" (missing)")
			}
		}
		s.unsettled = unsettled
	}
	return s
}

// lagging reports a job of this exact head that GitHub still lists as unfinished.
func lagging(j job, head string) bool {
	if j.HeadSHA != head || j.Conclusion != "" {
		return false
	}
	switch j.Status {
	case "queued", "in_progress", "waiting", "pending", "requested":
		return true
	}
	return false
}

func (q *queueController) groupChecks(ctx context.Context, p pull) ([]check, error) {
	var all struct {
		Checks []check `json:"check_runs"`
		Total  int     `json:"total_count"`
	}
	path := q.endpoint(fmt.Sprintf("commits/%s/check-runs?check_name=%s&filter=all&per_page=100&page=1", p.Head.SHA, q.checkName))
	if err := q.api.read(ctx, path, &all); err != nil {
		return nil, err
	}
	if all.Checks == nil || all.Total < 0 || all.Total > 100 || len(all.Checks) != all.Total {
		return nil, errors.New("incomplete group check identities")
	}
	seen := make(map[int64]bool)
	for _, v := range all.Checks {
		decoded, err := decodeBinding(v.ExternalID)
		if err != nil || v.ID <= 0 || seen[v.ID] || v.HeadSHA != p.Head.SHA || v.App.ID != actionsAppID || v.App.Slug != "github-actions" || decoded.RepoID != q.repo.ID || decoded.Head != p.Head.SHA || v.Name != q.checkName {
			return nil, errors.New("foreign merge-group required-check identity")
		}
		seen[v.ID] = true
	}
	return all.Checks, nil
}

// A pending canonical policy can accompany complete member identity, but never
// supplies workflow-change authority or authorizes a successful group verdict.
type groupAuthorityPending struct{}

func (*groupAuthorityPending) Error() string { return "queue member exact-head authority is pending" }

func (q *queueController) groupMembers(ctx context.Context, s queueSnapshot) ([]pull, bool, error) {
	var members []pull
	authorized, authorityPending := false, false
	for _, entry := range s.Entries {
		p, err := q.readPR(ctx, entry.PR.Number)
		if err != nil {
			return nil, false, err
		}
		if p.State != "open" || p.Head.SHA != entry.PR.Head || p.Base.Ref != q.repo.DefaultBranch || p.Base.SHA == q.expectedHead || (len(members) > 0 && p.Base.SHA != members[0].Base.SHA) {
			return nil, false, &staleQueueGroup{reason: "queue member or current base differs from tested group"}
		}
		decision, err := q.verify(ctx, p.Number, p.Head.SHA)
		if err != nil {
			return nil, false, err
		}
		switch decision.Code {
		case 0:
			authorized = authorized || decision.WorkflowChangeAuthorized
		case 75:
			authorityPending = true
		default:
			return nil, false, errors.New("queue member exact-head authority is not approved")
		}
		if p.Head.Repo.ID != q.repo.ID {
			vouched, err := q.currentVouch(ctx, p)
			if err != nil {
				return nil, false, err
			}
			if !vouched {
				return nil, false, errors.New("queue member fork vouch was revoked")
			}
		}
		code, err := q.dcoResult(ctx, p)
		if err != nil {
			return nil, false, err
		}
		if code != 0 {
			return nil, false, errors.New("queue member canonical DCO sign-off is missing")
		}
		members = append(members, p)
	}
	if authorityPending {
		return members, authorized, &groupAuthorityPending{}
	}
	return members, authorized, nil
}
func (q *queueController) groupSource(ctx context.Context, r run, base string, authorized bool) error {
	count := 0
	for _, ref := range r.References {
		parts := strings.Split(ref.Path, "@")
		if len(parts) == 2 && strings.EqualFold(parts[0], q.repoName+"/.github/workflows/ci.yml") {
			count++
			if ref.SHA != r.HeadSHA || parts[1] != r.HeadSHA || ref.Ref != "refs/heads/"+r.HeadBranch {
				return errors.New("full merge-group workflow source mismatch")
			}
		}
	}
	if count != 1 {
		return errors.New("ambiguous merge-group reusable source")
	}
	for _, path := range []string{queueWorkflowPath, ".github/workflows/ci.yml"} {
		a, err := q.contents(ctx, path, base)
		if err != nil {
			return err
		}
		b, err := q.contents(ctx, path, r.HeadSHA)
		if err != nil {
			return err
		}
		if string(a) != string(b) && !authorized {
			return errors.New("merge group changes unapproved validation contract")
		}
	}
	return nil
}

func (q *queueController) latestGroup(ctx context.Context, head string) (*run, error) {
	var list struct {
		Runs  []run `json:"workflow_runs"`
		Total int   `json:"total_count"`
	}
	if err := q.api.read(ctx, q.endpoint("actions/workflows/ci-merge-group.yml/runs?event=merge_group&head_sha="+head+"&per_page=100&page=1"), &list); err != nil {
		return nil, err
	}
	if list.Runs == nil || list.Total < 0 || list.Total > 100 || len(list.Runs) != list.Total {
		return nil, errors.New("incomplete current merge-group runs")
	}
	var latest *run
	for _, r := range list.Runs {
		if r.ID <= 0 || r.Attempt <= 0 || r.Event != "merge_group" || r.Path != queueWorkflowPath || r.HeadSHA != head || r.Repository.ID != q.repo.ID || !strings.EqualFold(r.Repository.FullName, q.repoName) || !strings.HasPrefix(r.HeadBranch, "gh-readonly-queue/"+q.repo.DefaultBranch+"/") {
			return nil, errors.New("invalid current merge-group source")
		}
		if latest == nil || r.ID > latest.ID {
			copy := r
			latest = &copy
		}
	}
	if latest == nil {
		return nil, nil
	}
	var live run
	if err := q.api.read(ctx, q.endpoint(fmt.Sprintf("actions/runs/%d", latest.ID)), &live); err != nil {
		return nil, err
	}
	if live.ID != latest.ID || live.Attempt < latest.Attempt || live.HeadSHA != latest.HeadSHA || live.HeadBranch != latest.HeadBranch || live.Event != latest.Event || live.Path != latest.Path || live.Repository.ID != latest.Repository.ID || live.WorkflowID != latest.WorkflowID {
		return nil, errors.New("current merge-group run identity changed")
	}
	return &live, nil
}
func (q *queueController) proveCurrentBase(ctx context.Context, base, head string) error {
	var compare struct {
		Status string `json:"status"`
		Base   struct {
			SHA string `json:"sha"`
		} `json:"base_commit"`
		MergeBase struct {
			SHA string `json:"sha"`
		} `json:"merge_base_commit"`
	}
	if err := q.api.read(ctx, q.endpoint("compare/"+base+"..."+head), &compare); err != nil {
		return err
	}
	if compare.Base.SHA != base || !shaPattern.MatchString(compare.MergeBase.SHA) || (compare.Status != "ahead" && compare.Status != "identical" && compare.Status != "behind" && compare.Status != "diverged") {
		return errors.New("invalid authenticated current-main ancestry response")
	}
	if compare.MergeBase.SHA != base || (compare.Status != "ahead" && compare.Status != "identical") {
		return &staleQueueGroup{reason: "tested group does not contain the authenticated current main"}
	}
	return nil
}
func (q *queueController) compatibleDefinition(ctx context.Context, original, current string) error {
	if original == current {
		return nil
	}
	// The reusable workflow executes the group version, compared with current
	// main by groupSource. Planning/aggregate tools come from the original base.
	for _, path := range []string{"scripts/ci/classify-changed-paths.sh", "scripts/ci/ci-job-registry.json", "scripts/ci/check-required-jobs.sh", "scripts/ci/plan-merge-group.sh", "scripts/ci/analysis-shards-go/main.go", "scripts/ci/test-race-packages.sh"} {
		a, err := q.contents(ctx, path, original)
		if err != nil {
			return err
		}
		b, err := q.contents(ctx, path, current)
		if err != nil {
			return err
		}
		if string(a) != string(b) {
			return errors.New("current-main validation definition changed; a fresh merge group is required")
		}
	}
	return nil
}

// Only authenticated obsolete group observations are expected pending. API,
// schema, authority, collision and write failures keep their error identity.
type staleQueueGroup struct{ reason string }

func (e *staleQueueGroup) Error() string { return e.reason }
func (q *queueController) repairGroups(ctx context.Context) error {
	policy, err := q.policy(ctx)
	if err != nil {
		return err
	}
	if !policy.Enabled {
		return nil
	}
	live, err := q.liveQueue(ctx)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	var failures []error
	for _, entry := range live.Entries.Nodes {
		if entry.Head.OID == "" && entry.State == "QUEUED" {
			if _, err := fmt.Fprintf(q.output, "PR #%d queue entry %q (position %d, QUEUED): waiting for GitHub to create a merge-group commit; no group proof can be published yet.\n", entry.PR.Number, entry.ID, entry.Position); err != nil {
				return err
			}
			continue
		}
		if !shaPattern.MatchString(entry.Head.OID) || seen[entry.Head.OID] {
			return errors.New("invalid group repair identity")
		}
		seen[entry.Head.OID] = true
		// Complete queue identity still validates above. A held-head writer
		// cannot repair another head; discovery routes that work separately.
		if entry.Head.OID != q.expectedHead {
			continue
		}
		r, err := q.latestGroup(ctx, entry.Head.OID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if r == nil {
			continue
		}
		if err := q.group(ctx, *r); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (q *queueController) dcoResult(ctx context.Context, p pull) (int, error) {
	verifier := q.dco
	if verifier == nil {
		verifier = func(ctx context.Context, p pull) (int, error) { return verifyQueueDCO(ctx, q.repoName, p) }
	}
	return canonicalDCOResult(ctx, p, verifier)
}
func canonicalDCOResult(ctx context.Context, p pull, verifier queueDCOVerifier) (int, error) {
	code, err := verifier(ctx, p)
	if err != nil {
		return 0, err
	}
	if code != 0 && code != 1 {
		return 0, errors.New("invalid canonical DCO verifier result")
	}
	return code, nil
}
