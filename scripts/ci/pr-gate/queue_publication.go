package main

import (
	"context"
	"errors"
	"fmt"
	"time"
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

// completeAdmission and completeGroup accept only this pass's freshly verified
// source/authority/final-identity result. The owner repeats the appropriate
// complete history authentication and selects the effective generation itself.
// Equality below is publication deduplication, never cached proof.
func (s *queueReconciliation) completeAdmission(ctx context.Context, p pull, b binding, result outcome) error {
	own, err := s.q.ownedChecks(ctx, p)
	if err != nil {
		return err
	}
	write, stop := s.q.verdictWriteContext(ctx)
	defer stop()
	written, err := s.publishRecord(write, p, b, newestQueueCheck(own), result)
	if err == nil && result.Status == "completed" {
		s.verifiedTerminal = true
	}
	if err != nil || result.Conclusion != "success" || s.q.checkName != devCheckName {
		return err
	}
	return s.reapplyRevocation(ctx, write, p, b, written)
}

// revocationSkew is how far a revocation record's start may trail this pass's start
// and still count as older. Both are GitHub's clocks, but one is a job start and the
// other an API write, so a second or two of disagreement must not make the reconcile
// that follows the revocation (it starts after the revoke job ends, tens of seconds
// later) look older than the revocation. A stale pass reads its inputs before the
// event that revoked the head, and the revocation follows that event by a discovery
// job, a checkout and a build, so it is never within this margin of a stale pass's start.
const revocationSkew = 3 * time.Second

// revocationNewerThanPass reports whether v is a revocation written after this pass
// began. An unreadable start fails the pass instead of ending it quietly, so the retry
// runs it again and the head is not left revoked with nothing queued.
func (q *queueController) revocationNewerThanPass(v *check) (bool, error) {
	if v == nil || v.Output.Title != revocationTitle || q.queueDeadline.IsZero() {
		return false, nil
	}
	created, err := time.Parse(time.RFC3339, v.StartedAt)
	if err != nil {
		return false, errors.New("revocation record has no readable start time")
	}
	return created.After(q.queueDeadline.Add(-admissionSeconds*time.Second + revocationSkew)), nil
}

// reapplyRevocation closes the one race the in-place guard cannot see: this pass listed
// the owned checks before a revocation was written, so it created its own success record
// with a higher ID than the revocation. After a success write the pass lists the head's
// checks again, with the suite-aware listing and bounded waits every other confirmation
// uses, and acts only on a listing that shows the exact success it wrote as completed.
// A listing that cannot show it (absent, older, other status, or an error) is not a clean
// answer, so the pass fails closed: a blocking record goes over the success it cannot
// confirm. If a revocation newer than the pass sits below the success, a blocking record
// that keeps the revocation's start replaces it, so the reconcile queued after the
// revocation is still not refused. Reads run under the work clock, writes under the write grace.
func (s *queueReconciliation) reapplyRevocation(ctx, write context.Context, p pull, b binding, written *check) error {
	if written == nil {
		return errors.New("the success write returned no record to confirm")
	}
	own, err := s.q.ownedAfterWrite(ctx, written, &s.confirmSpent)
	newest := newestQueueCheck(own)
	if err != nil || newest == nil || newest.ID < written.ID || newest.ID == written.ID && (newest.Status != written.Status || newest.Conclusion != written.Conclusion) {
		unconfirmed := errors.Join(err, errors.New("the success write could not be confirmed against a listing that shows it; a blocking record replaces it"))
		if _, werr := s.publishRecord(write, p, b, written, pending("A published success could not be confirmed; admission remains blocked until the next reconciliation.")); werr != nil {
			return errors.Join(unconfirmed, werr)
		}
		return unconfirmed
	}
	if newest.ID > written.ID {
		return nil // a newer record, written by another pass or a revocation, is on top
	}
	var first *check
	for i := range own {
		newer, err := s.q.revocationNewerThanPass(&own[i])
		if err != nil {
			return err
		}
		if newer && own[i].ID < written.ID && (first == nil || own[i].StartedAt < first.StartedAt) {
			first = &own[i]
		}
	}
	if first == nil {
		return nil
	}
	again := revoked("The dev publisher or main changed while an older pass was finishing; this head is blocked until its exact-head reconciliation finishes.")
	again.StartedAt = first.StartedAt
	if _, err := s.publishRecord(write, p, b, written, again); err != nil {
		return err
	}
	return &staleHeadWork{expected: s.q.expectedHead}
}

func (s *queueReconciliation) completeGroup(ctx context.Context, p pull, b binding, result outcome) error {
	own, err := s.q.groupChecks(ctx, p)
	if err != nil {
		return err
	}
	return s.publishVerified(ctx, p, b, newestQueueCheck(own), result)
}

func (s *queueReconciliation) publishVerified(ctx context.Context, p pull, b binding, selected *check, result outcome) error {
	_, err := s.publishRecord(ctx, p, b, selected, result)
	if err == nil && result.Status == "completed" {
		s.verifiedTerminal = true
	}
	return err
}

// A terminal verdict is never reset: transitions get a new queued record.
// All publication observations, including uncertain receipts, stay with the
// owner that settles the pass.
func (s *queueReconciliation) publishRecord(ctx context.Context, p pull, b binding, selected *check, result outcome) (*check, error) {
	c := s.q.controller
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if (c.checkName != queueCheckName && c.checkName != devCheckName) || c.repo.ID <= 0 || !shaPattern.MatchString(c.expectedHead) {
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
	if result.Title != revocationTitle {
		// No older pass rewrites a revocation record in place: a pending or refusal result
		// would lose its title, and a success would turn the head green on stale inputs.
		if newer, err := s.q.revocationNewerThanPass(selected); err != nil {
			return nil, err
		} else if newer {
			return nil, &staleHeadWork{expected: c.expectedHead}
		}
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
			s.uncertainCreate = true
			return nil, err
		}
		if !c.authenticatedQueueCheck(&written, b.Head) || written.ExternalID != b.externalID() {
			return nil, errors.New("queued creation returned foreign identity; no guessed-ID recovery")
		}
		selected = &written
		s.latest = selected
		if written.Status != "queued" || written.Conclusion != "" || written.CompletedAt != "" {
			return selected, errors.New("queued creation did not return a blocking record")
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
	s.latest = &written
	return &written, nil
}
