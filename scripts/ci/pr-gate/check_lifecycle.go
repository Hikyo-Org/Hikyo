package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const lifecycleName = "ci-check-lifecycle-probe"
const lifecycleWorkflowName = "check-lifecycle-probe"
const lifecyclePath = ".github/workflows/ci-check-lifecycle-probe.yml"

type lifecycleState struct {
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	ExternalID  string `json:"external_id"`
	CompletedAt string `json:"completed_at"`
}
type lifecycleResponse struct {
	lifecycleState
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Head    string `json:"head_sha"`
	AppID   int64  `json:"app_id"`
	AppSlug string `json:"app_slug"`
}

const lifecycleProtocol = "immutable-verdict"

type lifecycleCounts struct {
	Creates int `json:"creates"`
	Patches int `json:"patches"`
}

type lifecycleReceipt struct {
	Version    int        `json:"version"`
	Protocol   string     `json:"protocol"`
	Repository repository `json:"repository"`
	Source     struct {
		SHA        string `json:"sha"`
		Path       string `json:"workflow_path"`
		WorkflowID int64  `json:"workflow_id"`
		RunID      int64  `json:"run_id"`
		Attempt    int64  `json:"attempt"`
		Event      string `json:"event"`
		Branch     string `json:"branch"`
	} `json:"source"`
	Canaries []lifecycleResponse `json:"canaries"`
	Stages   struct {
		Initial   lifecycleResponse `json:"initial"`
		Barrier   lifecycleResponse `json:"barrier"`
		Reuse     lifecycleResponse `json:"reuse"`
		Finalized lifecycleResponse `json:"finalized"`
		Noop      lifecycleResponse `json:"noop"`
	} `json:"stages"`
	EffectivePending lifecycleResponse `json:"effective_pending"`
	HistoryBefore    int               `json:"history_before"`
	HistoryAfter     int               `json:"history_after"`
	Operations       struct {
		Initial   lifecycleCounts `json:"initial"`
		Barrier   lifecycleCounts `json:"barrier"`
		Reuse     lifecycleCounts `json:"reuse"`
		Finalized lifecycleCounts `json:"finalized"`
		Noop      lifecycleCounts `json:"noop"`
		Cleanup   lifecycleCounts `json:"cleanup"`
	} `json:"operations"`
	Cleanups          []lifecycleResponse `json:"cleanups"`
	LifecycleAccepted bool                `json:"lifecycle_accepted"`
	CleanupVerified   bool                `json:"cleanup_verified"`
	Error             string              `json:"error,omitempty"`
}

type lifecycleCountingAPI struct {
	api
	Counts lifecycleCounts
}

func (a *lifecycleCountingAPI) write(ctx context.Context, method, path string, payload, result any) error {
	switch method {
	case "POST":
		a.Counts.Creates++
	case "PATCH":
		a.Counts.Patches++
	default:
		return errors.New("unsupported lifecycle mutation")
	}
	return a.api.write(ctx, method, path, payload, result)
}
func lifecycleObservation(v check) lifecycleResponse {
	return lifecycleResponse{lifecycleState: lifecycleResult(v), ID: v.ID, Name: v.Name, Head: v.HeadSHA, AppID: v.App.ID, AppSlug: v.App.Slug}
}
func (c *controller) lifecyclePending(v check, id int64, b binding) bool {
	return c.lifecycleIdentity(v, id, b) && v.Status == "queued" && v.Conclusion == "" && v.CompletedAt == ""
}
func (c *controller) lifecycleEffectivePending(ctx context.Context, b binding, id int64) (check, error) {
	own, err := c.listOwnedChecks(ctx, b.Head)
	if err != nil {
		return check{}, err
	}
	var latest *check
	for i := range own {
		if latest == nil || own[i].ID > latest.ID {
			latest = &own[i]
		}
	}
	if latest == nil || !c.lifecyclePending(*latest, id, b) {
		return check{}, errors.New("new queued canary is not the newest authenticated production record")
	}
	var response struct {
		Total  *int    `json:"total_count"`
		Checks []check `json:"check_runs"`
	}
	path := fmt.Sprintf("commits/%s/check-runs?check_name=%s&filter=latest&per_page=100&page=1", b.Head, lifecycleName)
	if err := c.api.read(ctx, c.endpoint(path), &response); err != nil {
		return check{}, err
	}
	if response.Total == nil || *response.Total != 1 || len(response.Checks) != 1 || !c.lifecyclePending(response.Checks[0], id, b) {
		return check{}, errors.New("GitHub latest canary context did not confirm the fresh queued record")
	}
	return response.Checks[0], nil
}

func (c *controller) probeMain(ctx context.Context, sha string) error {
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"object"`
	}
	if err := c.api.read(ctx, c.endpoint("git/ref/heads/"+c.repo.DefaultBranch), &ref); err != nil {
		return err
	}
	if ref.Ref != "refs/heads/"+c.repo.DefaultBranch || ref.Object.Type != "commit" || ref.Object.SHA != sha {
		return errors.New("lifecycle probe source is not the exact current main")
	}
	return nil
}

func (c *controller) lifecycleIdentity(v check, id int64, b binding) bool {
	return v.ID > 0 && (id == 0 || v.ID == id) && v.Name == lifecycleName && v.HeadSHA == b.Head && v.ExternalID == b.externalID() && v.App.ID == actionsAppID && v.App.Slug == "github-actions"
}

func lifecycleResult(v check) lifecycleState {
	return lifecycleState{Status: v.Status, Conclusion: v.Conclusion, ExternalID: v.ExternalID, CompletedAt: v.CompletedAt}
}

func (c *controller) checkLifecycle(ctx context.Context, name string, e event, receipt *lifecycleReceipt) (err error) {
	// Read-only bootstrap is bounded before any mutation. The authenticated job
	// clock then includes checkout/compilation instead of starting a fresh budget.
	bootstrap, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err = c.init(bootstrap); err != nil {
		return err
	}
	c.checkName = lifecycleName
	if name != "workflow_dispatch" || e.Ref != "refs/heads/"+c.repo.DefaultBranch || e.Repository.ID != c.repo.ID || !strings.EqualFold(e.Repository.FullName, c.repoName) || e.Inputs.PR != "" || e.Inputs.Work != "" {
		return errors.New("lifecycle canary requires input-free trusted main dispatch")
	}
	id, parseErr := strconv.ParseInt(os.Getenv("GITHUB_RUN_ID"), 10, 64)
	attempt, attemptErr := strconv.ParseInt(os.Getenv("GITHUB_RUN_ATTEMPT"), 10, 64)
	sha := os.Getenv("GITHUB_SHA")
	if parseErr != nil || attemptErr != nil || id <= 0 || attempt <= 0 || !shaPattern.MatchString(sha) {
		return errors.New("missing lifecycle probe run/source identity")
	}
	var w workflow
	if err = c.api.read(bootstrap, c.endpoint("actions/workflows/ci-check-lifecycle-probe.yml"), &w); err != nil {
		return err
	}
	var r run
	if err = c.api.read(bootstrap, c.endpoint(fmt.Sprintf("actions/runs/%d", id)), &r); err != nil {
		return err
	}
	if w.ID <= 0 || w.Path != lifecyclePath || w.Name != lifecycleWorkflowName || r.ID != id || r.Attempt != attempt || r.WorkflowID != w.ID || r.Path != lifecyclePath || r.Event != name || r.HeadSHA != sha || r.HeadBranch != c.repo.DefaultBranch || r.Repository.ID != c.repo.ID || !strings.EqualFold(r.Repository.FullName, c.repoName) || r.HeadRepository.ID != c.repo.ID || !strings.EqualFold(r.HeadRepository.FullName, c.repoName) || r.Status != "in_progress" {
		return errors.New("untrusted lifecycle probe source/run/attempt")
	}
	var jobs struct {
		Total int `json:"total_count"`
		Jobs  []struct {
			ID      int64  `json:"id"`
			RunID   int64  `json:"run_id"`
			Attempt int64  `json:"run_attempt"`
			Name    string `json:"name"`
			Head    string `json:"head_sha"`
			Status  string `json:"status"`
			Started string `json:"started_at"`
		} `json:"jobs"`
	}
	if err = c.api.read(bootstrap, c.endpoint(fmt.Sprintf("actions/runs/%d/attempts/%d/jobs?per_page=100&page=1", id, attempt)), &jobs); err != nil {
		return err
	}
	if jobs.Jobs == nil || jobs.Total != len(jobs.Jobs) || jobs.Total > 100 {
		return errors.New("incomplete lifecycle job clock")
	}
	matched := 0
	var started time.Time
	for _, j := range jobs.Jobs {
		if j.Name != "lifecycle-probe" {
			continue
		}
		matched++
		started, err = time.Parse(time.RFC3339, j.Started)
		if err != nil || j.ID <= 0 || j.RunID != id || j.Attempt != attempt || j.Head != sha || j.Status != "in_progress" || started.IsZero() || started.After(time.Now()) {
			return errors.New("invalid authenticated lifecycle job clock")
		}
	}
	if matched != 1 || !time.Now().Before(started.Add(120*time.Second)) {
		return errors.New("lifecycle work budget exhausted or ambiguous; no canary mutation")
	}
	work, stop := context.WithDeadline(ctx, started.Add(120*time.Second))
	defer stop()
	if err = c.probeMain(work, sha); err != nil {
		return err
	}
	policyReader := queueController{controller: c}
	policy, policyErr := policyReader.policy(work)
	if policyErr != nil || !policy.Enabled {
		return errors.Join(errors.New("canary requires the exact active owning/effective queue policy; its name must not be required"), policyErr)
	}
	// This diagnostic shares the production publisher, but is never a required
	// context. Its source and original job clock have already been authenticated.
	c.expectedHead = sha
	receipt.Version = 2
	receipt.Protocol = lifecycleProtocol
	receipt.Repository = c.repo
	receipt.Source.SHA = sha
	receipt.Source.Path = w.Path
	receipt.Source.WorkflowID = w.ID
	receipt.Source.RunID = id
	receipt.Source.Attempt = attempt
	receipt.Source.Event = name
	receipt.Source.Branch = c.repo.DefaultBranch
	p := pull{Number: 1, State: "open", Head: reference{SHA: sha, Repo: c.repo}, Base: reference{SHA: sha, Ref: c.repo.DefaultBranch, Repo: c.repo}}
	b := (snapshot{PR: p, Run: &r}).binding(c.repo.ID)

	originalAPI := c.api
	counted := &lifecycleCountingAPI{api: originalAPI}
	c.api = counted
	defer func() { c.api = originalAPI }()
	known := make(map[int64]check)
	var order []int64
	track := func(v *check) {
		if v == nil || !c.lifecycleIdentity(*v, 0, b) {
			return
		}
		if _, present := known[v.ID]; !present {
			order = append(order, v.ID)
			receipt.Canaries = append(receipt.Canaries, lifecycleObservation(*v))
		}
		known[v.ID] = *v
	}
	stagesComplete := false
	// Every acknowledged owned ID is cleaned even when a later stage fails.
	// One shared independent reserve stays within the original job clock.
	defer func() {
		end := time.Now().Add(30 * time.Second)
		if absolute := started.Add(150 * time.Second); absolute.Before(end) {
			end = absolute
		}
		cleanup, done := context.WithDeadline(context.WithoutCancel(ctx), end)
		defer done()
		var failures []error
		for _, ownedID := range order {
			path := c.endpoint(fmt.Sprintf("check-runs/%d", ownedID))
			var current check
			cleanupErr := c.api.read(cleanup, path, &current)
			if cleanupErr == nil && !c.lifecycleIdentity(current, ownedID, b) {
				cleanupErr = errors.New("canary cleanup identity became foreign; refusing its mutation")
			}
			if cleanupErr == nil {
				neutral := outcome{Status: "completed", Conclusion: "neutral", Title: "Non-required lifecycle canary cleaned up", Summary: "Diagnostic finished. This check supplies no merge authority."}
				var body map[string]any
				body, cleanupErr = c.checkPayload(b, neutral)
				if cleanupErr == nil {
					cleanupErr = c.api.write(cleanup, "PATCH", path, body, &current)
				}
				if cleanupErr == nil {
					cleanupErr = c.api.read(cleanup, path, &current)
				}
				if cleanupErr == nil && (!c.lifecycleIdentity(current, ownedID, b) || current.Status != "completed" || current.Conclusion != "neutral" || current.CompletedAt == "") {
					cleanupErr = errors.New("canary terminal neutral cleanup not confirmed")
				}
			}
			if cleanupErr != nil {
				failures = append(failures, fmt.Errorf("canary %d cleanup failed: %w", ownedID, cleanupErr))
				continue
			}
			receipt.Cleanups = append(receipt.Cleanups, lifecycleObservation(current))
		}
		receipt.Operations.Cleanup = counted.Counts
		receipt.CleanupVerified = len(order) > 0 && len(receipt.Cleanups) == len(order) && len(failures) == 0
		err = errors.Join(err, errors.Join(failures...))
		receipt.LifecycleAccepted = stagesComplete && receipt.CleanupVerified && err == nil
	}()

	before, listErr := c.listOwnedChecks(work, sha)
	if listErr != nil {
		return listErr
	}
	receipt.HistoryBefore = len(before)
	publication := &queueReconciliation{q: &queueController{controller: c}}
	initial := outcome{Status: "completed", Conclusion: "success", Title: "Non-required lifecycle canary", Summary: "API lifecycle diagnostic only. This is not validation or merge proof."}
	created, publishErr := publication.publishRecord(work, p, b, nil, initial)
	track(created)
	if publishErr != nil {
		return fmt.Errorf("initial canary publication failed without mutation retry: %w", publishErr)
	}
	if created == nil || !c.lifecycleIdentity(*created, 0, b) || created.Status != "completed" || created.Conclusion != "success" || created.CompletedAt == "" {
		return errors.New("initial canary success state not confirmed")
	}
	receipt.Stages.Initial = lifecycleObservation(*created)
	receipt.Operations.Initial = counted.Counts
	if counted.Counts != (lifecycleCounts{Creates: 1, Patches: 1}) {
		return errors.New("initial canary did not use queued creation and finalization")
	}

	pendingResult := pending("Reassessment requires a fresh queued record; no prior terminal green supplies current proof.")
	barrier, publishErr := publication.publishRecord(work, p, b, created, pendingResult)
	track(barrier)
	if barrier != nil {
		receipt.Stages.Barrier = lifecycleObservation(*barrier)
	}
	receipt.Operations.Barrier = counted.Counts
	if publishErr != nil {
		return fmt.Errorf("fresh canary barrier refused without mutation retry: %w", publishErr)
	}
	if barrier == nil || barrier.ID == created.ID || !c.lifecyclePending(*barrier, barrier.ID, b) || counted.Counts != (lifecycleCounts{Creates: 2, Patches: 1}) {
		return errors.New("terminal green was not superseded by one distinct queued record")
	}
	var unchanged check
	if err = c.api.read(work, c.endpoint(fmt.Sprintf("check-runs/%d", created.ID)), &unchanged); err != nil {
		return err
	}
	if !c.lifecycleIdentity(unchanged, created.ID, b) || lifecycleResult(unchanged) != lifecycleResult(*created) {
		return errors.New("old terminal green was mutated during fresh barrier creation")
	}
	latest, latestErr := c.lifecycleEffectivePending(work, b, barrier.ID)
	if latestErr != nil {
		return latestErr
	}
	receipt.EffectivePending = lifecycleObservation(latest)

	pendingResult.Summary = "The same authenticated pending record is being updated without another creation."
	reused, publishErr := publication.publishRecord(work, p, b, barrier, pendingResult)
	track(reused)
	if reused != nil {
		receipt.Stages.Reuse = lifecycleObservation(*reused)
	}
	receipt.Operations.Reuse = counted.Counts
	if publishErr != nil {
		return publishErr
	}
	if reused == nil || !c.lifecyclePending(*reused, barrier.ID, b) || counted.Counts != (lifecycleCounts{Creates: 2, Patches: 2}) {
		return errors.New("pending canary did not reuse the exact owned ID")
	}
	if _, err = c.lifecycleEffectivePending(work, b, barrier.ID); err != nil {
		return err
	}

	finalized, publishErr := publication.publishRecord(work, p, b, reused, initial)
	track(finalized)
	if finalized != nil {
		receipt.Stages.Finalized = lifecycleObservation(*finalized)
	}
	receipt.Operations.Finalized = counted.Counts
	if publishErr != nil {
		return publishErr
	}
	if finalized == nil || !c.lifecycleIdentity(*finalized, barrier.ID, b) || finalized.Status != "completed" || finalized.Conclusion != "success" || finalized.CompletedAt == "" || counted.Counts != (lifecycleCounts{Creates: 2, Patches: 3}) {
		return errors.New("fresh canary finalization did not update only the pending ID")
	}
	var confirmed check
	if err = c.api.read(work, c.endpoint(fmt.Sprintf("check-runs/%d", barrier.ID)), &confirmed); err != nil {
		return err
	}
	if !c.lifecycleIdentity(confirmed, barrier.ID, b) || lifecycleResult(confirmed) != lifecycleResult(*finalized) || confirmed.Output.Title != initial.Title || confirmed.Output.Summary != initial.Summary {
		return errors.New("final canary success readback not confirmed")
	}
	noop, publishErr := publication.publishRecord(work, p, b, &confirmed, initial)
	track(noop)
	if noop != nil {
		receipt.Stages.Noop = lifecycleObservation(*noop)
	}
	receipt.Operations.Noop = counted.Counts
	if publishErr != nil {
		return publishErr
	}
	if noop == nil || !c.lifecycleIdentity(*noop, barrier.ID, b) || lifecycleResult(*noop) != lifecycleResult(confirmed) || counted.Counts != (lifecycleCounts{Creates: 2, Patches: 3}) {
		return errors.New("identical terminal canary was not a physical no-op")
	}
	after, listErr := c.listOwnedChecks(work, sha)
	if listErr != nil {
		return listErr
	}
	receipt.HistoryAfter = len(after)
	if receipt.HistoryAfter != receipt.HistoryBefore+2 || len(order) != 2 {
		return errors.New("canary history changed outside its two authenticated creations")
	}
	if err = c.probeMain(work, sha); err != nil {
		return err
	}
	finalPolicy, policyErr := policyReader.policy(work)
	if policyErr != nil || finalPolicy != policy {
		return errors.Join(errors.New("canary protection changed before lifecycle acceptance"), policyErr)
	}
	stagesComplete = true
	return nil
}
