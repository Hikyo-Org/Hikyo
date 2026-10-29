package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

func retirementHead(name string, e event) (string, error) {
	var head string
	switch name {
	case "pull_request_target":
		if e.Number <= 0 || e.Pull.Number != e.Number || !validReference(e.Pull.Head) || !validReference(e.Pull.Base) || e.Pull.Base.Repo.ID != e.Repository.ID || !strings.EqualFold(e.Pull.Base.Repo.FullName, e.Repository.FullName) {
			return "", errors.New("invalid retirement PR event identity")
		}
		switch e.Action {
		case "synchronize":
			head = e.Before
			if head == e.Pull.Head.SHA || e.Pull.State != "open" {
				return "", errors.New("retirement synchronize event did not advance an open PR head")
			}
		case "closed":
			if e.Pull.State != "closed" {
				return "", errors.New("retirement close event is not closed")
			}
			head = e.Pull.Head.SHA
		default:
			return "", errors.New("retirement supports only synchronize and closed PR events")
		}
	case "workflow_dispatch":
		if e.Ref != "refs/heads/"+e.Repository.DefaultBranch || e.Inputs.PR != "" || e.Inputs.Work != "" {
			return "", errors.New("retirement requires exact-head trusted main dispatch")
		}
		head = e.Inputs.Head
		if head == "" {
			head = os.Getenv("PR_GATE_RETIREMENT_HEAD")
		}
	case "push":
		if e.Ref != "refs/heads/"+e.Repository.DefaultBranch {
			return "", errors.New("retirement push must target the protected default branch")
		}
		head = e.After
	case "schedule":
		if os.Getenv("GITHUB_REF") != "refs/heads/"+e.Repository.DefaultBranch {
			return "", errors.New("scheduled retirement must execute on the protected default branch")
		}
		// Scheduled matrix heads are discovery hints only. Ownership and fresh
		// complete live roles authorize cancellation, never discovery itself.
		head = os.Getenv("PR_GATE_RETIREMENT_HEAD")
	default:
		return "", errors.New("unsupported retirement event")
	}
	if !shaPattern.MatchString(head) {
		return "", errors.New("retirement event requires an exact commit SHA")
	}
	return head, nil
}

func (c *controller) retireEvent(ctx context.Context, name string, e event) error {
	if err := c.init(ctx); err != nil {
		return err
	}
	if e.Repository.ID != c.repo.ID || !strings.EqualFold(e.Repository.FullName, c.repoName) || e.Repository.DefaultBranch != c.repo.DefaultBranch {
		return errors.New("retirement event repository identity mismatch")
	}
	head, err := retirementHead(name, e)
	if err != nil {
		return err
	}
	if name == "push" && c.expectedHead == e.Before && shaPattern.MatchString(e.Before) && e.Before != strings.Repeat("0", 40) {
		head = e.Before
	}
	if head != c.expectedHead {
		return errors.New("retirement event head does not match the held writer lease")
	}

	return c.retireChecks(ctx, head)
}

// retireChecks is called only under the existing writer lease for this exact
// context/head. It never supplies proof and never rewrites a terminal verdict.
func (c *controller) retireChecks(ctx context.Context, head string) error {
	_, err := c.retireChecksState(ctx, head, false)
	return err
}

func (c *controller) retireChecksState(ctx context.Context, head string, allowLandedGroup bool) (bool, error) {
	if c.checkName != queueCheckName && c.checkName != "ci-required-next" {
		return false, errors.New("retirement requires a production queue or observation context")
	}
	if !shaPattern.MatchString(head) || c.repo.ID <= 0 || c.checkName == queueCheckName && c.expectedHead != head {
		return false, errors.New("retirement requires an authenticated repository and exact held head")
	}
	checks, err := c.listOwnedChecks(ctx, head)
	if err != nil {
		return false, err
	}
	for _, v := range checks {
		if v.Status == "completed" {
			continue
		}
		if !c.authenticatedQueueCheck(&v, head) || !blockingQueueCheck(&v) {
			return false, errors.New("retirement cannot authenticate a complete nonterminal record")
		}
	}
	q := queueController{controller: c}
	for _, selected := range checks {
		if selected.Status == "completed" {
			continue
		}
		// Reread roles and the selected record immediately before every PATCH.
		// This also detects reopening/reuse between paginated inventory and work.
		var current check
		path := c.endpoint(fmt.Sprintf("check-runs/%d", selected.ID))
		if err := c.api.read(ctx, path, &current); err != nil {
			return false, err
		}
		if current != selected {
			return false, errors.New("retirement record changed after inventory; no mutation attempted")
		}
		roles, err := q.roles(ctx, head)
		if err != nil {
			return false, err
		}
		inactive, err := q.inactiveRetirementRoles(ctx, head, roles, allowLandedGroup)
		if err != nil {
			return false, err
		}
		if !inactive {
			_, err := fmt.Fprintf(c.output, "Retirement retained %s on %s: commit still has a live PR/group role.\n", c.checkName, head)
			return false, err
		}
		if c.dryRun {
			_, err := fmt.Fprintf(c.output, "DRY RUN retire %s %s check %d as cancelled\n", head, c.checkName, selected.ID)
			if err != nil {
				return false, err
			}
			continue
		}
		b, err := decodeBinding(selected.ExternalID)
		if err != nil {
			return false, err
		}
		result := outcome{Status: "completed", Conclusion: "cancelled", Title: "Retired commit", Summary: "This commit has no current open PR or unmerged merge-group role. Retiring this unfinished synthetic record without granting merge proof."}
		payload, err := c.checkPayload(b, result)
		if err != nil {
			return false, err
		}
		// Preserve canonical legacy bindings and the original publication link.
		payload["external_id"] = selected.ExternalID
		delete(payload, "details_url")
		var written check
		if err := c.api.write(ctx, "PATCH", path, payload, &written); err != nil {
			return false, err // An uncertain mutation is never retried.
		}
		if !c.authenticatedQueueCheck(&written, head) || written.ID != selected.ID || written.ExternalID != selected.ExternalID || written.Status != "completed" || written.Conclusion != "cancelled" {
			return false, errors.New("retirement response did not confirm the exact owned cancelled record")
		}
		var fresh check
		if err := c.api.read(ctx, path, &fresh); err != nil {
			return false, err
		}
		if fresh != written {
			return false, errors.New("retirement readback disagrees with cancelled record")
		}
		if _, err := fmt.Fprintf(c.output, "Retired %s %s check %d: completed/cancelled, readback verified.\n", head, c.checkName, selected.ID); err != nil {
			return false, err
		}
	}
	// Inventory can contain only terminal records (or be empty). Even that
	// no-op must not mark the invocation retired after a PR reopens during
	// inventory, or after the final PATCH/readback.
	roles, err := q.roles(ctx, head)
	if err != nil {
		return false, err
	}
	return q.inactiveRetirementRoles(ctx, head, roles, allowLandedGroup)
}

// Only the stale-group callback path may treat a queue role as already landed.
// A fresh exact current-main ref establishes that this group SHA cannot still
// be an unmerged candidate. Every open PR head role remains protected, and the
// same main/role proof is repeated before each mutation and final disposition.
func (q *queueController) inactiveRetirementRoles(ctx context.Context, head string, roles headRoles, allowLandedGroup bool) (bool, error) {
	if len(roles.Pulls) != 0 {
		return false, nil
	}
	if !roles.Group {
		return true, nil
	}
	if !allowLandedGroup {
		return false, nil
	}
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	if err := q.api.read(ctx, q.endpoint("git/ref/heads/"+q.repo.DefaultBranch), &ref); err != nil {
		return false, err
	}
	if ref.Ref != "refs/heads/"+q.repo.DefaultBranch || ref.Object.Type != "commit" || !shaPattern.MatchString(ref.Object.SHA) {
		return false, errors.New("stale-group retirement cannot authenticate current main")
	}
	return ref.Object.SHA == head, nil
}
