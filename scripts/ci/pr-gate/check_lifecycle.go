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
type lifecycleReceipt struct {
	Version    int        `json:"version"`
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
	Canary struct {
		Name       string `json:"name"`
		ID         int64  `json:"id"`
		Head       string `json:"head_sha"`
		AppID      int64  `json:"app_id"`
		AppSlug    string `json:"app_slug"`
		ExternalID string `json:"external_id"`
	} `json:"canary"`
	Initial         lifecycleState     `json:"initial"`
	Reset           lifecycleState     `json:"reset"`
	ResetResponse   *lifecycleResponse `json:"reset_response,omitempty"`
	Cleanup         lifecycleState     `json:"cleanup"`
	ResetAccepted   bool               `json:"reset_accepted"`
	CleanupVerified bool               `json:"cleanup_verified"`
	Error           string             `json:"error,omitempty"`
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
	receipt.Version = 1
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
	initial := outcome{Status: "completed", Conclusion: "success", Title: "Non-required lifecycle canary", Summary: "API lifecycle diagnostic only. This is not validation or merge proof."}
	payload, payloadErr := c.checkPayload(b, initial)
	if payloadErr != nil {
		return payloadErr
	}
	payload["head_sha"] = sha
	var created check
	if err = c.api.write(work, "POST", c.endpoint("check-runs"), payload, &created); err != nil {
		return fmt.Errorf("canary creation not acknowledged; unknown IDs are never guessed or retried: %w", err)
	}
	if !c.lifecycleIdentity(created, 0, b) {
		return errors.New("canary creation returned foreign or ambiguous identity; no guessed-ID cleanup")
	}
	receipt.Canary.Name = created.Name
	receipt.Canary.ID = created.ID
	receipt.Canary.Head = created.HeadSHA
	receipt.Canary.AppID = created.App.ID
	receipt.Canary.AppSlug = created.App.Slug
	receipt.Canary.ExternalID = created.ExternalID
	path := c.endpoint(fmt.Sprintf("check-runs/%d", created.ID))
	// Cleanup has its own short context, never the cancelled work context. It
	// remains inside the original job clock, leaving 30s for artifact/runner exit.
	defer func() {
		end := time.Now().Add(30 * time.Second)
		if absolute := started.Add(150 * time.Second); absolute.Before(end) {
			end = absolute
		}
		cleanup, done := context.WithDeadline(context.WithoutCancel(ctx), end)
		defer done()
		var current check
		cleanupErr := c.api.read(cleanup, path, &current)
		if cleanupErr == nil && !c.lifecycleIdentity(current, created.ID, b) {
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
			if cleanupErr == nil && (!c.lifecycleIdentity(current, created.ID, b) || current.Status != "completed" || current.Conclusion != "neutral") {
				cleanupErr = errors.New("canary terminal neutral cleanup not confirmed")
			}
		}
		if cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("canary cleanup failed: %w", cleanupErr))
			return
		}
		receipt.Cleanup = lifecycleResult(current)
		receipt.CleanupVerified = true
	}()
	if created.Status != "completed" || created.Conclusion != "success" {
		return errors.New("initial canary success state not confirmed")
	}
	receipt.Initial = lifecycleResult(created)
	queued := pending("Exact production queued reset payload. No prior success supplies authority.")
	payload, payloadErr = c.checkPayload(b, queued)
	if payloadErr != nil {
		return payloadErr
	}
	var reset check
	if err = c.api.write(work, "PATCH", path, payload, &reset); err != nil {
		return fmt.Errorf("canary reset refused without mutation retry: %w", err)
	}
	// Retain the acknowledged response before validation. Cleanup cannot recover
	// rejected state afterward, and an observation never establishes acceptance.
	receipt.Reset = lifecycleResult(reset)
	receipt.ResetResponse = &lifecycleResponse{lifecycleState: receipt.Reset, ID: reset.ID, Name: reset.Name, Head: reset.HeadSHA, AppID: reset.App.ID, AppSlug: reset.App.Slug}
	if !c.lifecycleIdentity(reset, created.ID, b) || reset.Status != "queued" || reset.Conclusion != "" {
		return errors.New("production queued reset response retained success or contradicted identity")
	}
	if err = c.api.read(work, path, &reset); err != nil {
		return err
	}
	if !c.lifecycleIdentity(reset, created.ID, b) || reset.Status != "queued" || reset.Conclusion != "" {
		return errors.New("production queued reset readback not confirmed")
	}
	if err = c.probeMain(work, sha); err != nil {
		return err
	}
	finalPolicy, policyErr := policyReader.policy(work)
	if policyErr != nil || finalPolicy != policy {
		return errors.Join(errors.New("canary protection changed before lifecycle acceptance"), policyErr)
	}
	receipt.Reset = lifecycleResult(reset)
	receipt.ResetAccepted = true
	return nil
}
