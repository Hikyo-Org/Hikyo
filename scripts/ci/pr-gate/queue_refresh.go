package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// queueReconciliation owns observation, publication and settlement for one
// held-head pass. Proof producers deliver freshly verified results; they never
// carry publication flags or reuse an earlier pass's authority. Every unsettled
// exit independently verifies blocking under the original publication reserve.
type queueReconciliation struct {
	q                *queueController
	latest           *check
	uncertainCreate  bool
	verifiedTerminal bool
	verifiedRetired  bool
	finishedGroup    *finishedGroupProof
	// Set only by a re-reading group invocation whose window ended with GitHub
	// metadata still incomplete or unreadable. It selects the terminal wording.
	rereadExpired bool
	// The first finished-group identity a re-reading invocation observed. Later
	// passes may only confirm it; a different one is movement, never new proof.
	pinned *finishedGroupProof
}

type finishedGroupProof struct {
	run      run
	policy   liveQueuePolicy
	snapshot queueSnapshot
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

func (q *queueController) reassess(ctx context.Context, work func(context.Context) error) error {
	return q.reassessHeld(ctx, false, work)
}

// Group-kind invocations (the group callback job and the group head worker)
// run once per completed merge-group run with a 10-minute budget, and nothing
// retriggers them after that run completes. They may therefore repeat the whole
// reassessment while GitHub job metadata lags or a read fails transiently.
// Every other invocation reassesses exactly once and never waits.
func (q *queueController) reassessHeld(ctx context.Context, rereads bool, work func(context.Context) error) error {
	if q.reconciliation != nil {
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
	if !rereads || q.dryRun {
		_, err := q.reassessOnce(ctx, work, nil)
		return err
	}
	return q.rereadGroup(ctx, work)
}

// The window leaves the 10-minute group job room for setup (about 30s), the
// last pass and the terminal write; a job timeout would strand the record
// pending until the queue's own 60-minute timeout. Wall-clock caps: passes,
// waits and retries stop at the fence; the terminal write gets its own reserve
// that may run until groupRereadHardStop after the loop began.
const groupRereadWindow = 6 * time.Minute
const groupRereadInterval = 20 * time.Second

// A full pass costs dozens of requests against about 1000 an hour, so waits
// between full passes double from the interval up to this cap while reads keep
// failing. Polling the jobs listing alone keeps the fixed interval.
const groupRereadMaxBackoff = 2 * time.Minute

// Variables rather than constants so tests can shrink them.
var (
	groupRereadFence    = 7*time.Minute + 30*time.Second
	groupRereadReserve  = time.Minute
	groupRereadHardStop = 9 * time.Minute
)

var groupRereadNow = time.Now
var groupRereadWait = waitForReadRetry

type groupRereads struct {
	parent  context.Context
	started time.Time // window clock, replaceable in tests
	fence   time.Time // wall-clock caps: context deadlines run on time.Now
	hardEnd time.Time
	pinned  *finishedGroupProof
	streak  int           // consecutive full passes deferred by a failed read
	delay   time.Duration // the wait the loop was last asked to make
}

func newGroupRereads(parent context.Context) *groupRereads {
	wall := time.Now()
	return &groupRereads{parent: parent, started: groupRereadNow(), fence: wall.Add(groupRereadFence), hardEnd: wall.Add(groupRereadHardStop)}
}

// open reports whether a wait of next, and so the step after it, still starts
// inside the window. It is the only test for waiting: once false, the pass in
// hand is the final decision.
func (g *groupRereads) open(next time.Duration) bool {
	return groupRereadNow().Sub(g.started)+next < groupRereadWindow
}

// passDelay is the wait before the next full pass: the interval, doubled once
// per consecutive deferral, capped.
func (g *groupRereads) passDelay() time.Duration {
	delay := groupRereadInterval
	for i := 0; i < g.streak && delay < groupRereadMaxBackoff; i++ {
		delay *= 2
	}
	return min(delay, groupRereadMaxBackoff)
}

// What a looping pass asks of the loop.
type rereadNext int

const (
	rereadStop     rereadNext = iota // the outcome is final
	rereadPass                       // wait, then reassess from scratch
	rereadPollJobs                   // barrier confirmed; wait on the jobs listing alone
)

func (q *queueController) rereadGroup(ctx context.Context, work func(context.Context) error) error {
	loop := newGroupRereads(ctx)
	pass, cancel := context.WithDeadline(ctx, loop.fence)
	defer cancel()
	for {
		next, err := q.reassessOnce(pass, work, loop)
		if next == rereadStop {
			return err
		}
		if _, writeErr := fmt.Fprintf(q.output, "Merge group proof metadata is not final; re-reading in %s: %v\n", loop.delay, err); writeErr != nil {
			return errors.Join(err, writeErr)
		}
		var waitErr error
		if next == rereadPollJobs {
			waitErr = q.pollProofJobs(pass, loop, err)
		} else {
			waitErr = groupRereadWait(pass, loop.delay)
		}
		if waitErr != nil {
			return errors.Join(err, waitErr)
		}
	}
}

// While proof jobs lag, repeating the full reassessment every interval costs
// dozens of requests each time against a budget of about 1000 an hour. Poll the
// run's jobs listing alone, with all of its identity checks, until both proof
// jobs are final or the window has no room left. The caller then runs exactly
// one full pass, which re-verifies everything before any verdict.
func (q *queueController) pollProofJobs(ctx context.Context, loop *groupRereads, waiting error) error {
	var incomplete *groupProofIncomplete
	if !errors.As(waiting, &incomplete) {
		if !loop.open(groupRereadInterval) {
			return nil
		}
		return groupRereadWait(ctx, groupRereadInterval) // never spin
	}
	for loop.open(groupRereadInterval) {
		if err := groupRereadWait(ctx, groupRereadInterval); err != nil {
			return err
		}
		// The closing pass re-reads the listing itself, so this read only pays
		// off when another wait can follow it.
		if !loop.open(groupRereadInterval) {
			return nil
		}
		jobs, err := q.jobs(ctx, incomplete.run)
		if err != nil {
			if transientGitHubRead(err) && !rateLimitedGroupRead(err) {
				continue
			}
			return nil // let the full pass classify it
		}
		if len(summarizeProofJobs(jobs, incomplete.run).unsettled) == 0 {
			return nil
		}
	}
	return nil
}

// A looping pass leaves a nonterminal barrier and asks to wait; any other
// outcome is final. Nothing terminal is written while the window is open.
func (q *queueController) reassessOnce(ctx context.Context, work func(context.Context) error, rereads *groupRereads) (next rereadNext, err error) {
	session := &queueReconciliation{q: q}
	q.reconciliation = session
	defer func() { q.reconciliation = nil }()
	return session.reconcile(ctx, work, rereads)
}

func (s *queueReconciliation) reconcile(ctx context.Context, work func(context.Context) error, rereads *groupRereads) (next rereadNext, err error) {
	q := s.q
	if rereads != nil {
		s.pinned = rereads.pinned
	}
	own, readErr := q.listOwnedChecks(ctx, q.expectedHead)
	s.latest = newestQueueCheck(own)
	if readErr == nil {
		err = work(ctx)
	} else {
		err = readErr
	}
	if q.dryRun {
		return rereadStop, err
	}
	if rereads != nil && rereads.pinned == nil {
		rereads.pinned = s.finishedGroup
	}
	if err == nil && s.verifiedTerminal || s.verifiedRetired && (err == nil || onlyStaleHeadWork(err)) {
		return rereadStop, err
	}
	// Cancellation may have interrupted mutable authority or an uncertain write.
	// A separate short reserve is bounded by the invocation's original deadline.
	// A looping invocation derives a longer one from its parent, never from the
	// fenced pass context: the terminal refusal takes about nine sequential calls.
	var reserve context.Context
	var stop context.CancelFunc
	if rereads != nil {
		reserve, stop = q.reserveFor(rereads.parent, groupRereadReserve, rereads.hardEnd)
	} else {
		reserve, stop = q.reserveContext(ctx)
	}
	defer stop()
	if rereads != nil && !s.uncertainCreate && rereads.parent.Err() == nil && !s.groupSettled() {
		// Only the loop's own fence can have ended the pass context here.
		fenced := ctx.Err() != nil
		if fenced && expirableGroupRead(err) || !fenced && deferrableGroupRead(err) {
			var incomplete *groupProofIncomplete
			lagging := errors.As(err, &incomplete)
			next := rereads.passDelay()
			if lagging {
				next = groupRereadInterval
			}
			// A rate limit is not cured by waiting out the window: expire at once.
			if !fenced && !rateLimitedGroupRead(err) && rereads.open(next) {
				// An existing blocking record is left untouched; a missing one is still
				// created so the wait never leaves a stale non-blocking verdict effective.
				blockErr := s.ensureBlocking(reserve, err)
				if blockErr != nil && !deferrableGroupRead(blockErr) {
					return rereadStop, errors.Join(err, blockErr)
				}
				// Without a confirmed barrier the next step must be a full pass,
				// which establishes one again.
				polling := blockErr == nil && lagging
				next = rereads.passDelay()
				if polling {
					next = groupRereadInterval
				}
				// ensureBlocking takes several sequential calls: decide again on the
				// clock it left. A closed window expires on this pass's own evidence.
				if !rateLimitedGroupRead(blockErr) && rereads.open(next) {
					rereads.delay = next
					if polling {
						rereads.streak = 0 // a pass that read enough to see lag is healthy
						return rereadPollJobs, err
					}
					rereads.streak++
					return rereadPass, errors.Join(err, blockErr)
				}
			}
			s.rereadExpired = true
			if s.finishedGroup == nil {
				// The final pass failed before re-proving the group. The identity an
				// earlier pass proved is still re-verified against live state before
				// any write.
				s.finishedGroup = s.pinned
			}
		}
	}
	return rereadStop, errors.Join(err, s.ensureBlocking(reserve, err))
}

// A completed owned record that already carries this run's identity (or is the
// expiry refusal written onto the run-less held-head record) means another
// invocation has issued the verdict. Waiting would only spend requests, so such
// a pass behaves as a single reassessment. The pending barrier a running group
// establishes has no run, so an expiry refusal written onto it has Run 0: a
// same-head re-run after one therefore takes the single-pass path, which is
// acceptable because the refusal tells the operator to re-queue (a new head).
func (s *queueReconciliation) groupSettled() bool {
	q := s.q
	proved := s.finishedGroup
	if proved == nil {
		proved = s.pinned
	}
	if proved == nil || s.latest == nil || s.latest.Status != "completed" || !q.authenticatedQueueCheck(s.latest, q.expectedHead) {
		return false
	}
	held, err := decodeBinding(s.latest.ExternalID)
	if err != nil {
		return false
	}
	// A run-less completed record settles only as the expiry refusal; a
	// retirement-cancelled barrier or an unsafe-proof refusal is run-less too.
	return held.Run == proved.run.ID && held.Attempt == proved.run.Attempt || held.Run == 0 && s.latest.Output.Title == metadataExpiredRefusal.Title
}

// Cancellation may have interrupted mutable authority or an uncertain write.
// A separate short reserve is bounded by the invocation's original deadline.
func (q *queueController) reserveContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return q.reserveFor(ctx, 5*time.Second, time.Time{})
}

// reserveFor derives from ctx without its cancellation. A nonzero hardEnd caps
// the budget.
func (q *queueController) reserveFor(ctx context.Context, budget time.Duration, hardEnd time.Time) (context.Context, context.CancelFunc) {
	end := time.Now().Add(budget)
	if !hardEnd.IsZero() && hardEnd.Before(end) {
		end = hardEnd
	}
	if !q.queueDeadline.IsZero() && q.queueDeadline.Before(end) {
		end = q.queueDeadline
	}
	if q.queueDeadline.IsZero() {
		if outer, ok := ctx.Deadline(); ok && outer.Before(end) {
			end = outer
		}
	}
	return context.WithDeadline(context.WithoutCancel(ctx), end)
}

// deferrableGroupRead reports whether err consists only of causes that waiting
// can cure: incomplete GitHub job metadata or a transient read failure. Context
// cancellation from a sibling read that failed that way is tolerated, never alone.
func deferrableGroupRead(err error) bool {
	curable, other, _ := classifyGroupRead(err, false)
	return curable && !other
}

// rateLimitedGroupRead reports whether any cause in err is an HTTP 429.
func rateLimitedGroupRead(err error) bool {
	_, _, limited := classifyGroupRead(err, false)
	return limited
}

// expirableGroupRead is the same test once the loop's own fence has ended the
// pass: its deadline error says nothing about GitHub, so it is tolerated too,
// even alone.
func expirableGroupRead(err error) bool {
	_, other, _ := classifyGroupRead(err, true)
	return err != nil && !other
}

func classifyGroupRead(err error, fenced bool) (curable, other, limited bool) {
	// Cancellation is never evidence. The deadline is only once the fence fired.
	neutral := func(err error) bool {
		return errors.Is(err, context.Canceled) || fenced && errors.Is(err, context.DeadlineExceeded)
	}
	switch e := err.(type) {
	case nil:
		return false, false, false
	case *groupProofIncomplete:
		return true, false, false
	case *githubReadError:
		if transientGitHubRead(e) {
			return true, false, e.status == http.StatusTooManyRequests
		}
		return false, e.rejected || e.status != 0 || !neutral(e.cause), false
	}
	if many, ok := err.(interface{ Unwrap() []error }); ok {
		parts := many.Unwrap()
		if len(parts) == 0 {
			return false, true, false
		}
		for _, part := range parts {
			c, o, l := classifyGroupRead(part, fenced)
			curable, other, limited = curable || c, other || o, limited || l
		}
		return curable, other, limited
	}
	if one, ok := err.(interface{ Unwrap() error }); ok {
		return classifyGroupRead(one.Unwrap(), fenced)
	}
	return false, err != context.Canceled && !(fenced && err == context.DeadlineExceeded), false
}

func (s *queueReconciliation) ensureBlocking(ctx context.Context, cause error) error {
	q := s.q
	own, scanErr := q.listOwnedChecks(ctx, q.expectedHead)
	if scanErr != nil {
		return scanErr
	} // Unknown history cannot authorize another creation.
	latest := newestQueueCheck(own)
	if blockingQueueCheck(latest) {
		if s.terminalGroupCause(cause) && latest.Status != "completed" && q.authenticatedQueueCheck(latest, q.expectedHead) {
			b, err := decodeBinding(latest.ExternalID)
			if err != nil {
				return err
			}
			p := pull{Number: b.PR, Head: reference{SHA: b.Head}}
			return s.finishGroupFailure(ctx, p, b, latest, cause, s.groupFailure())
		}
		return nil
	}
	p := pull{Number: 1, State: "open", Head: reference{SHA: q.expectedHead, Repo: q.repo}, Base: reference{SHA: q.expectedHead, Ref: q.repo.DefaultBranch, Repo: q.repo}}
	b := newBinding(q.repo.ID, p, nil)
	if latest != nil {
		existing, decodeErr := decodeBinding(latest.ExternalID)
		if decodeErr == nil {
			b = existing
			p.Number, p.Base.SHA = b.PR, b.Base
		} else if q.recognizedTruncatedQueueCheck(latest, q.expectedHead) {
			// Complete history authenticated its limited legacy identity, but
			// lost base/run fields cannot authorize an update. Use only the
			// canonical held-head Run0 placeholder for a distinct CREATE.
			latest = nil
		} else {
			return decodeErr
		}
	}
	// A lost POST receipt is not retried. A complete fresh scan can establish its
	// actual blocking record; otherwise only a captured, still-newest owned ID
	// may receive a genuine failure. Foreign/truncated history stays an error.
	if s.uncertainCreate {
		if scanErr != nil || latest == nil {
			return errors.Join(scanErr, errors.New("uncertain queued creation; no effective blocking identity confirmed"))
		}
		return s.failOwnedBarrier(ctx, p, b, latest, cause, unsafeProofRefusal)
	}
	result := pending("Current held-head proof is incomplete; admission and merging remain blocked.")
	written, writeErr := s.publishRecord(ctx, p, b, latest, result)
	if writeErr != nil {
		// Never retry the uncertain CREATE. Only the complete inventory captured
		// before it may authorize a failure on that known owned record.
		if latest != nil && scanErr == nil {
			return errors.Join(writeErr, s.failOwnedBarrier(ctx, p, b, latest, writeErr, unsafeProofRefusal))
		}
		return errors.Join(scanErr, writeErr)
	}
	fresh, finalErr := q.listOwnedChecks(ctx, q.expectedHead)
	current := newestQueueCheck(fresh)
	if finalErr != nil || written == nil || current == nil || current.ID != written.ID || !blockingQueueCheck(current) {
		return errors.Join(scanErr, finalErr, errors.New("newest owned blocking barrier could not be confirmed"))
	}
	if s.terminalGroupCause(cause) {
		return s.finishGroupFailure(ctx, p, b, current, cause, s.groupFailure())
	}
	return scanErr
}

var unsafeProofRefusal = refused("Current proof could not be safely established; the owned verdict is blocking.")

// A finished group's failure is terminal because nothing retriggers it. The
// exceptions are causes only waiting can cure: they stay pending unless a
// re-reading invocation exhausted its window, which publishes a distinct refusal.
func (s *queueReconciliation) terminalGroupCause(cause error) bool {
	return cause != nil && s.finishedGroup != nil && (s.rereadExpired || !deferrableGroupRead(cause))
}

// observeGroup records the finished-group identity a pass is about to verdict. A
// re-reading pass that finds a different one keeps the pinned proof, so the only
// possible outcome is the existing identity-moved refusal to write.
func (s *queueReconciliation) observeGroup(proved *finishedGroupProof) error {
	if s.pinned != nil && !sameGroupProof(*s.pinned, *proved) {
		s.finishedGroup = s.pinned
		return &queueSnapshotChanged{reason: "merge group identity moved between re-reads"}
	}
	s.finishedGroup = proved
	return nil
}

func sameGroupProof(proved, live finishedGroupProof) bool {
	return proved.policy == live.policy && sameSourceEvent(proved.run, live.run) && proved.run.HeadBranch == live.run.HeadBranch && proved.run.Status == live.run.Status && proved.run.Conclusion == live.run.Conclusion && proved.snapshot.Digest == live.snapshot.Digest
}

// The refusal a re-reading invocation publishes when it gives up; groupSettled
// recognises it by Title.
var metadataExpiredRefusal = outcome{Status: "completed", Conclusion: "failure", Title: "Merge group proof metadata unavailable", Summary: "GitHub job metadata stayed incomplete or unreadable after a successful merge-group run, through bounded re-reads or a rate limit; re-queue the PR."}

func (s *queueReconciliation) groupFailure() outcome {
	if s.rereadExpired {
		return metadataExpiredRefusal
	}
	return unsafeProofRefusal
}

func (s *queueReconciliation) finishGroupFailure(ctx context.Context, p pull, b binding, selected *check, cause error, result outcome) error {
	q := s.q
	policy, err := q.policy(ctx)
	if err != nil {
		return err
	}
	live, err := q.latestGroup(ctx, q.expectedHead)
	if err != nil {
		return err
	}
	snapshot, err := q.queue(ctx, q.expectedHead)
	if err != nil {
		return err
	}
	if live == nil || live.Status != "completed" || !sameGroupProof(*s.finishedGroup, finishedGroupProof{run: *live, policy: policy, snapshot: snapshot}) {
		return errors.New("completed group identity moved before failure publication")
	}
	return s.failOwnedBarrier(ctx, p, b, selected, cause, result)
}

func (s *queueReconciliation) failOwnedBarrier(ctx context.Context, p pull, b binding, selected *check, cause error, result outcome) error {
	q := s.q
	own, err := q.listOwnedChecks(ctx, q.expectedHead)
	latest := newestQueueCheck(own)
	if err != nil || latest == nil || latest.ID != selected.ID || !q.authenticatedQueueCheck(latest, q.expectedHead) {
		return errors.Join(err, errors.New("failure fallback cannot authenticate the effective owned record"))
	}
	if blockingQueueCheck(latest) && (latest.Status == "completed" || s.finishedGroup == nil) {
		return nil
	}
	if cause == nil {
		cause = errors.New("queued blocking barrier could not be established")
	}
	payload, err := q.checkPayload(b, result)
	if err != nil {
		return err
	}
	var written check
	if err := q.api.write(ctx, "PATCH", q.endpoint(fmt.Sprintf("check-runs/%d", latest.ID)), payload, &written); err != nil {
		return err
	}
	if !q.authenticatedQueueCheck(&written, q.expectedHead) || written.ID != latest.ID || written.ExternalID != b.externalID() || written.Status != "completed" || written.Conclusion != "failure" {
		return errors.New("failure fallback response did not establish an owned blocking verdict")
	}
	own, err = q.listOwnedChecks(ctx, q.expectedHead)
	newest := newestQueueCheck(own)
	if err != nil || newest == nil || newest.ID != written.ID || newest.Status != "completed" || newest.Conclusion != "failure" {
		return errors.Join(err, errors.New("failure fallback is not the effective blocking record"))
	}
	return nil
}

// A changed authenticated source binding is an event transition, rather than a
// periodic refresh. Establish its pending generation promptly, before proof.
func (s *queueReconciliation) changedBinding(ctx context.Context, p pull, b binding) error {
	q := s.q
	if s.latest == nil || s.latest.ExternalID == b.externalID() || blockingQueueCheck(s.latest) && q.authenticatedQueueCheck(s.latest, b.Head) {
		return nil
	}
	_, err := s.publishRecord(ctx, p, b, s.latest, pending("A new current source binding requires fresh validation."))
	return err
}

// Closed PRs and stale callbacks are benign only when this exact commit has no
// other live role. The disposition belongs to this reconciliation, never a
// cached property of a SHA or a PR-close event.
func (s *queueReconciliation) retireHead(ctx context.Context, allowLandedGroup bool) error {
	q := s.q
	s.verifiedRetired = false
	roles, err := q.roles(ctx, q.expectedHead)
	if err != nil {
		return err
	}
	inactive, err := q.inactiveRetirementRoles(ctx, q.expectedHead, roles, allowLandedGroup)
	if err != nil {
		return err
	}
	if inactive {
		retired, err := q.retireChecksState(ctx, q.expectedHead, allowLandedGroup)
		if err != nil {
			return err
		}
		s.verifiedRetired = retired
	}
	return nil
}

// Callbacks and group workers both renew complete retirement proof when queue
// movement invalidates their role read. An active or uncertain head stays blocked.
func (s *queueReconciliation) retireMovedGroup(ctx context.Context, err error) error {
	q := s.q
	var moved *queueSnapshotChanged
	if !errors.As(err, &moved) {
		return err
	}
	if retireErr := s.retireHead(ctx, true); retireErr != nil {
		return errors.Join(err, retireErr)
	}
	if s.verifiedRetired {
		_, writeErr := fmt.Fprintln(q.output, "Merge group retired after queue movement:", moved.reason)
		return writeErr
	}
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
