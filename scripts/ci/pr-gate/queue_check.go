package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

func reusableQueueCheck(v *check, b binding) bool {
	if v == nil || v.ID <= 0 || v.Name != queueCheckName || v.App.ID != actionsAppID || v.App.Slug != "github-actions" || v.HeadSHA != b.Head {
		return false
	}
	old, err := decodeBinding(v.ExternalID)
	return err == nil && old.RepoID == b.RepoID && old.Head == b.Head
}

func (c *controller) checkPayload(b binding, result outcome) (map[string]any, error) {
	payload := map[string]any{"name": c.checkName, "external_id": b.externalID(), "status": result.Status, "output": map[string]string{"title": result.Title, "summary": result.Summary}}
	if id := os.Getenv("GITHUB_RUN_ID"); id != "" {
		runID, err := strconv.ParseInt(id, 10, 64)
		if err != nil || runID <= 0 {
			return nil, errors.New("invalid controller GITHUB_RUN_ID")
		}
		payload["details_url"] = fmt.Sprintf("https://github.com/%s/actions/runs/%d", c.repoName, runID)
	} else if b.Run > 0 {
		payload["details_url"] = fmt.Sprintf("https://github.com/%s/actions/runs/%d", c.repoName, b.Run)
	}
	if result.Status == "completed" {
		payload["conclusion"] = result.Conclusion
	}
	return payload, nil
}

// A rejected or contradictory reset may leave the old owned green intact.
// Do not retry that uncertain PATCH. Publish one distinct blocking barrier,
// with the same held-head guard, then retain the original failure.
func (c *controller) blockResetFailure(ctx context.Context, p pull, b binding, selected *check, result outcome, cause error) error {
	if c.checkName != queueCheckName || selected == nil || selected.Status != "completed" || selected.Conclusion != "success" || result.Status == "completed" {
		return cause
	}
	end := time.Now().Add(5 * time.Second)
	if !c.queueDeadline.IsZero() && c.queueDeadline.Before(end) {
		end = c.queueDeadline
	}
	if c.queueDeadline.IsZero() {
		if outer, ok := ctx.Deadline(); ok && outer.Before(end) {
			end = outer
		}
	}
	reserve, stop := context.WithDeadline(context.WithoutCancel(ctx), end)
	defer stop()
	err := c.publish(reserve, p, b, nil, pending("Owned check reset could not be confirmed; current proof remains blocked."))
	return errors.Join(cause, err)
}
