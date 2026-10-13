package main

import (
	"context"
	"errors"
	"fmt"
)

func (c *controller) authenticatedQueueCheck(v *check, head string) bool {
	if v == nil || v.ID <= 0 || v.Name != c.checkName || v.HeadSHA != head || v.App.ID != actionsAppID || v.App.Slug != "github-actions" {
		return false
	}
	b, err := decodeBinding(v.ExternalID)
	return err == nil && b.RepoID == c.repo.ID && b.Head == head
}

func (c *controller) recognizedTruncatedQueueCheck(v *check, head string) bool {
	return v != nil && v.ID > 0 && v.Name == c.checkName && v.HeadSHA == head && v.App.ID == actionsAppID && v.App.Slug == "github-actions" && truncatedLegacyBinding(v.ExternalID, c.repo.ID, head)
}

func blockingQueueCheck(v *check) bool {
	if v == nil {
		return false
	}
	if v.Status != "completed" {
		return v.Conclusion == "" && v.CompletedAt == "" && (v.Status == "queued" || v.Status == "in_progress" || v.Status == "waiting" || v.Status == "pending" || v.Status == "requested")
	}
	switch v.Conclusion {
	case "failure", "cancelled", "timed_out", "action_required", "startup_failure":
		return true
	}
	return false
}

// Callers must complete current source/authority/identity verification before
// requesting a terminal verdict. Equality is publication deduplication only.
// A terminal verdict is never reset: transitions get a new queued record.
func (c *controller) publishQueueRecord(ctx context.Context, p pull, b binding, selected *check, result outcome) (*check, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if (c.checkName != queueCheckName && c.checkName != lifecycleName) || c.repo.ID <= 0 || !shaPattern.MatchString(c.expectedHead) {
		return nil, errors.New("queue publication requires an authenticated repository and held head")
	}
	if p.Head.SHA != c.expectedHead || b.Head != c.expectedHead || selected != nil && selected.HeadSHA != c.expectedHead {
		return nil, &staleHeadWork{expected: c.expectedHead}
	}
	if b.RepoID != c.repo.ID || !shaPattern.MatchString(b.Head) || !shaPattern.MatchString(b.Base) {
		return nil, errors.New("invalid queue publication binding")
	}
	decoded, err := decodeBinding(b.externalID())
	if err != nil || decoded != b {
		return nil, errors.New("noncanonical queue publication binding")
	}
	if c.recognizedTruncatedQueueCheck(selected, b.Head) {
		// Prefix authentication is deliberately insufficient for PATCH/no-op.
		// Reauthenticate complete history before superseding its newest record
		// with a fully canonical current binding, including pending recovery.
		own, err := c.listOwnedChecks(ctx, b.Head)
		if err != nil {
			return nil, err
		}
		latest := newestQueueCheck(own)
		if latest == nil || latest.ID != selected.ID || latest.ExternalID != selected.ExternalID || !c.recognizedTruncatedQueueCheck(latest, b.Head) {
			return nil, errors.New("legacy migration selection changed before creation")
		}
		selected = nil
	}
	if selected != nil && !c.authenticatedQueueCheck(selected, b.Head) {
		return nil, errors.New("foreign queue publication record")
	}
	if result.Status != "queued" && result.Status != "completed" || result.Status == "queued" && result.Conclusion != "" {
		return nil, errors.New("unsupported queue publication outcome")
	}
	if c.dryRun {
		_, err := fmt.Fprintf(c.output, "DRY RUN PR #%d: %s %s (%s), %s\n", p.Number, c.checkName, result.Status, result.Conclusion, result.Summary)
		return selected, err
	}
	if selected != nil && selected.ExternalID == b.externalID() && selected.Status == result.Status && selected.Conclusion == result.Conclusion && selected.Output.Title == result.Title && selected.Output.Summary == result.Summary && (result.Status == "completed" || blockingQueueCheck(selected)) {
		copy := *selected
		return &copy, nil
	}
	if selected == nil || selected.Status == "completed" {
		barrier := result
		if result.Status == "completed" {
			barrier = pending("Finalizing a freshly verified queue verdict.")
		}
		payload, err := c.checkPayload(b, barrier)
		if err != nil {
			return nil, err
		}
		payload["head_sha"] = b.Head
		var written check
		if err := c.api.write(ctx, "POST", c.endpoint("check-runs"), payload, &written); err != nil {
			if c.queueRefresh != nil {
				c.queueRefresh.uncertainCreate = true
			}
			return nil, err
		}
		if !c.authenticatedQueueCheck(&written, b.Head) || written.ExternalID != b.externalID() {
			return nil, errors.New("queued creation returned foreign identity; no guessed-ID recovery")
		}
		selected = &written
		if c.queueRefresh != nil {
			c.queueRefresh.latest = selected
		}
		if written.Status != "queued" || written.Conclusion != "" || written.CompletedAt != "" {
			return selected, errors.New("queued creation did not return a blocking record")
		}
		if c.queueRefresh != nil {
			c.queueRefresh.latest = selected
		}
		if result.Status == "queued" {
			return selected, nil
		}
	}
	// Rebinding is permitted only while the independently authenticated record
	// is nonterminal. No prior terminal proof is mutated or carried forward.
	if selected.Status == "completed" || !blockingQueueCheck(selected) {
		return selected, errors.New("only an authenticated nonterminal queue record can be updated")
	}
	payload, err := c.checkPayload(b, result)
	if err != nil {
		return selected, err
	}
	var written check
	if err := c.api.write(ctx, "PATCH", c.endpoint(fmt.Sprintf("check-runs/%d", selected.ID)), payload, &written); err != nil {
		return selected, err
	}
	if !c.authenticatedQueueCheck(&written, b.Head) || written.ID != selected.ID || written.ExternalID != b.externalID() {
		return selected, errors.New("queue update returned foreign identity; no mutation retry")
	}
	if written.Status != result.Status || written.Conclusion != result.Conclusion || result.Status == "queued" && written.CompletedAt != "" {
		return &written, errors.New("queue update contradicted the requested result")
	}
	if c.queueRefresh != nil {
		c.queueRefresh.latest = &written
	}
	return &written, nil
}
