package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// This state is local to one held-head reconciliation, not an authority cache.
// Only the existing complete source/authority/final-identity proof can mark a
// terminal verdict verified. Every other exit independently verifies blocking.
type queueReassessment struct {
	latest           *check
	uncertainCreate  bool
	verifiedTerminal bool
}

func newestQueueCheck(own []check) *check {
	var latest *check
	for i := range own {
		if latest == nil || own[i].ID > latest.ID {
			copy := own[i]
			latest = &copy
		}
	}
	return latest
}

func (q *queueController) reassess(ctx context.Context, work func(context.Context) error) (err error) {
	if q.queueRefresh != nil {
		return work(ctx)
	}
	if !shaPattern.MatchString(q.expectedHead) {
		return errors.New("queue reconciliation requires a held head")
	}
	if q.repo.ID == 0 {
		if err := q.init(ctx); err != nil {
			return err
		}
	}
	state := &queueReassessment{}
	q.queueRefresh = state
	defer func() { q.queueRefresh = nil }()
	own, readErr := q.listOwnedChecks(ctx, q.expectedHead)
	state.latest = newestQueueCheck(own)
	if readErr == nil {
		err = work(ctx)
	} else {
		err = readErr
	}
	if q.dryRun || err == nil && state.verifiedTerminal {
		return nil
	}
	// Cancellation may have interrupted mutable authority or an uncertain write.
	// A separate short reserve is bounded by the invocation's original deadline.
	end := time.Now().Add(5 * time.Second)
	if !q.queueDeadline.IsZero() && q.queueDeadline.Before(end) {
		end = q.queueDeadline
	}
	if q.queueDeadline.IsZero() {
		if outer, ok := ctx.Deadline(); ok && outer.Before(end) {
			end = outer
		}
	}
	reserve, stop := context.WithDeadline(context.WithoutCancel(ctx), end)
	defer stop()
	return errors.Join(err, q.ensureBlocking(reserve, state, err))
}

func (q *queueController) ensureBlocking(ctx context.Context, state *queueReassessment, cause error) error {
	own, scanErr := q.listOwnedChecks(ctx, q.expectedHead)
	latest := newestQueueCheck(own)
	if scanErr == nil && blockingQueueCheck(latest) {
		return nil
	}
	p := pull{Number: 1, State: "open", Head: reference{SHA: q.expectedHead, Repo: q.repo}, Base: reference{SHA: q.expectedHead, Ref: q.repo.DefaultBranch, Repo: q.repo}}
	b := newBinding(q.repo.ID, p, nil)
	if latest != nil {
		b, _ = decodeBinding(latest.ExternalID)
		p.Number, p.Base.SHA = b.PR, b.Base
	}
	// A lost POST receipt is not retried. A complete fresh scan can establish its
	// actual blocking record; otherwise only a captured, still-newest owned ID
	// may receive a genuine failure. Foreign/truncated history stays an error.
	if state.uncertainCreate {
		if scanErr != nil || latest == nil {
			return errors.Join(scanErr, errors.New("uncertain queued creation; no effective blocking identity confirmed"))
		}
		return q.failOwnedBarrier(ctx, p, b, latest, cause)
	}
	result := pending("Current held-head proof is incomplete; admission and merging remain blocked.")
	written, writeErr := q.publishQueueRecord(ctx, p, b, latest, result)
	if writeErr != nil {
		// Never retry the uncertain CREATE. Only the complete inventory captured
		// before it may authorize a failure on that known owned record.
		if latest != nil && scanErr == nil {
			return errors.Join(writeErr, q.failOwnedBarrier(ctx, p, b, latest, writeErr))
		}
		return errors.Join(scanErr, writeErr)
	}
	fresh, finalErr := q.listOwnedChecks(ctx, q.expectedHead)
	current := newestQueueCheck(fresh)
	if finalErr != nil || written == nil || current == nil || current.ID != written.ID || !blockingQueueCheck(current) {
		return errors.Join(scanErr, finalErr, errors.New("newest owned blocking barrier could not be confirmed"))
	}
	return scanErr
}

func (q *queueController) failOwnedBarrier(ctx context.Context, p pull, b binding, selected *check, cause error) error {
	own, err := q.listOwnedChecks(ctx, q.expectedHead)
	latest := newestQueueCheck(own)
	if err != nil || latest == nil || latest.ID != selected.ID || !q.authenticatedQueueCheck(latest, q.expectedHead) {
		return errors.Join(err, errors.New("failure fallback cannot authenticate the effective owned record"))
	}
	if blockingQueueCheck(latest) {
		return nil
	}
	if cause == nil {
		cause = errors.New("queued blocking barrier could not be established")
	}
	result := refused("Current proof could not be safely established; the owned verdict is blocking.")
	payload, err := q.checkPayload(b, result)
	if err != nil {
		return err
	}
	var written check
	if err := q.api.write(ctx, "PATCH", q.endpoint(fmt.Sprintf("check-runs/%d", latest.ID)), payload, &written); err != nil {
		return err
	}
	if !q.authenticatedQueueCheck(&written, q.expectedHead) || written.ID != latest.ID || written.ExternalID != b.externalID() || !blockingQueueCheck(&written) {
		return errors.New("failure fallback response did not establish an owned blocking verdict")
	}
	own, err = q.listOwnedChecks(ctx, q.expectedHead)
	newest := newestQueueCheck(own)
	if err != nil || newest == nil || newest.ID != written.ID || !blockingQueueCheck(newest) {
		return errors.Join(err, errors.New("failure fallback is not the effective blocking record"))
	}
	return nil
}

// A changed authenticated source binding is an event transition, rather than a
// periodic refresh. Establish its pending generation promptly, before proof.
func (q *queueController) changedBinding(ctx context.Context, p pull, b binding) error {
	if q.queueRefresh == nil || q.queueRefresh.latest == nil || q.queueRefresh.latest.ExternalID == b.externalID() || blockingQueueCheck(q.queueRefresh.latest) {
		return nil
	}
	_, err := q.publishQueueRecord(ctx, p, b, q.queueRefresh.latest, pending("A new current source binding requires fresh validation."))
	return err
}

// Initialization cannot authorize success. If repository authentication succeeded
// but the subsequent workflow read failed, a held-head blocking reserve remains
// possible. A failed repository read never supplies guessed ownership.
func (q *queueController) initializedFailure(ctx context.Context, id int64, name string, cause error) error {
	if q.checkName != queueCheckName || q.repo.ID <= 0 || q.repo.ID != id || !strings.EqualFold(q.repo.FullName, q.repoName) || !strings.EqualFold(name, q.repoName) || !shaPattern.MatchString(q.expectedHead) {
		return cause
	}
	return q.reassess(ctx, func(context.Context) error { return cause })
}
