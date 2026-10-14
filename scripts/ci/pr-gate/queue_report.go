package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const queueReportContext = "merge-queue-validation"

// This is advisory visibility on the PR head, never queue admission or merge
// proof. Immutable merge parents retain attribution after GitHub evicts a group.
func (c *controller) reportQueue(ctx context.Context, name string, e event) error {
	if name != "workflow_run" || e.Run.ID <= 0 || e.Run.Attempt <= 0 {
		return errors.New("queue reporting requires an authenticated workflow callback")
	}
	if err := c.init(ctx); err != nil {
		return err
	}
	if e.Repository.ID != c.repo.ID || !strings.EqualFold(e.Repository.FullName, c.repoName) {
		return errors.New("queue report repository mismatch")
	}
	var w workflow
	if err := c.api.read(ctx, c.endpoint("actions/workflows/ci-merge-group.yml"), &w); err != nil {
		return err
	}
	var r run
	if err := c.api.read(ctx, c.endpoint(fmt.Sprintf("actions/runs/%d", e.Run.ID)), &r); err != nil {
		return err
	}
	if w.ID <= 0 || w.Path != queueWorkflowPath || w.Name != "trusted-merge-ci" || r.ID != e.Run.ID || r.WorkflowID != w.ID || r.Path != w.Path || r.Event != "merge_group" || r.Repository.ID != c.repo.ID || r.HeadRepository.ID != c.repo.ID || !shaPattern.MatchString(r.HeadSHA) {
		return errors.New("queue report workflow identity mismatch")
	}
	if r.Attempt != e.Run.Attempt {
		return nil // A rerun supersedes callbacks for earlier attempts.
	}
	prefix := "gh-readonly-queue/" + c.repo.DefaultBranch + "/pr-"
	tail, ok := strings.CutPrefix(r.HeadBranch, prefix)
	if !ok {
		return errors.New("queue report branch mismatch")
	}
	number, base, ok := strings.Cut(tail, "-")
	n, err := strconv.Atoi(number)
	if !ok || err != nil || n <= 0 || strconv.Itoa(n) != number || !shaPattern.MatchString(base) {
		return errors.New("queue report branch identity is malformed")
	}
	p, err := c.readPRMetadata(ctx, n)
	if err != nil {
		return err
	}
	if p.State != "open" || p.Base.Ref != c.repo.DefaultBranch {
		return nil
	}
	var commit struct {
		SHA     string `json:"sha"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if err := c.api.read(ctx, c.endpoint("git/commits/"+r.HeadSHA), &commit); err != nil {
		return err
	}
	if commit.SHA != r.HeadSHA || len(commit.Parents) != 2 || commit.Parents[0].SHA != base {
		return errors.New("queue report merge-parent identity mismatch")
	}
	if commit.Parents[1].SHA != p.Head.SHA {
		return nil // Never put a superseded head's result on the current PR.
	}
	current, err := c.currentQueueReport(ctx, r, prefix+number+"-")
	if err != nil || !current {
		return err
	}
	state, description := "pending", "Full merge-candidate validation is running"
	switch r.Status {
	case "queued", "in_progress", "waiting", "pending", "requested":
	case "completed":
		state, description = "failure", "Full merge-candidate validation failed; open Details for the jobs"
		if r.Conclusion == "success" {
			state, description = "success", "Full merge-candidate workflow passed; protected queue proof is separate"
		} else if r.Conclusion == "cancelled" {
			description = "Full merge-candidate validation was cancelled"
		}
	default:
		return errors.New("queue report run status is invalid")
	}
	// Recheck the source head and newest callback immediately before publication.
	final, err := c.readPRMetadata(ctx, n)
	if err != nil {
		return err
	}
	if final.State != "open" || final.Head != p.Head || final.Base.Ref != p.Base.Ref {
		return nil
	}
	current, err = c.currentQueueReport(ctx, r, prefix+number+"-")
	if err != nil || !current {
		return err
	}
	if c.dryRun {
		_, err = fmt.Fprintf(c.output, "DRY RUN PR #%d: %s %s (run %d attempt %d)\n", n, queueReportContext, state, r.ID, r.Attempt)
		return err
	}
	payload := map[string]string{"state": state, "context": queueReportContext, "description": description, "target_url": fmt.Sprintf("https://github.com/%s/actions/runs/%d/attempts/%d", c.repoName, r.ID, r.Attempt)}
	var receipt struct {
		ID      int64  `json:"id"`
		State   string `json:"state"`
		Context string `json:"context"`
	}
	if err := c.api.write(ctx, "POST", c.endpoint("statuses/"+p.Head.SHA), payload, &receipt); err != nil {
		return err
	}
	if receipt.ID <= 0 || receipt.State != state || receipt.Context != queueReportContext {
		return errors.New("queue report publication receipt mismatch")
	}
	return nil
}

func (c *controller) currentQueueReport(ctx context.Context, r run, branchPrefix string) (bool, error) {
	var latest run
	if err := c.api.read(ctx, c.endpoint(fmt.Sprintf("actions/runs/%d", r.ID)), &latest); err != nil {
		return false, err
	}
	if latest.Attempt != r.Attempt || latest.Status != r.Status || latest.Conclusion != r.Conclusion || latest.HeadSHA != r.HeadSHA || latest.HeadBranch != r.HeadBranch {
		return false, nil
	}
	// A new queue group may have another base SHA. Look across this PR's group
	// branches rather than letting a late old-group callback hide its result.
	for page := 1; page <= 20; page++ {
		var list struct {
			Runs []run `json:"workflow_runs"`
		}
		path := fmt.Sprintf("actions/workflows/ci-merge-group.yml/runs?event=merge_group&per_page=100&page=%d", page)
		if err := c.api.read(ctx, c.endpoint(path), &list); err != nil {
			return false, err
		}
		found := false
		for _, candidate := range list.Runs {
			if strings.HasPrefix(candidate.HeadBranch, branchPrefix) && candidate.ID > r.ID {
				return false, nil
			}
			found = found || candidate.ID == r.ID
		}
		if found {
			return true, nil
		}
		if len(list.Runs) < 100 {
			return false, errors.New("queue report source is absent from workflow history")
		}
	}
	return false, errors.New("queue report workflow history exceeded pagination limit")
}
