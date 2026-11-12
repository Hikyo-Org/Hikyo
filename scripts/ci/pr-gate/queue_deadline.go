package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"time"
)

const admissionSeconds = 60
const admissionReserve = 15 * time.Second

// How far a verdict's write may run past the work clock. The fail-safe and the refusal
// share the rest of the reserve, and together took about six seconds on a live run.
const admissionWriteGrace = 3 * time.Second

// A failed targeted run is rerun by ci-queue-retry.yml while its run attempt is
// below this. Each attempt is a new job with its own sixty-second budget; the
// workflow's `run_attempt < 3` is the only loop bound, so keep both in step.
const admissionMaxAttempts = 3

// admissionRetryRemains reports whether a rerun will follow this attempt. An
// unreadable attempt counts as the last one: the terminal refusal is the safe
// default when nothing is promised to reassess the head.
func admissionRetryRemains() bool {
	attempt, err := strconv.Atoi(os.Getenv("GITHUB_RUN_ATTEMPT"))
	return err == nil && attempt >= 1 && attempt < admissionMaxAttempts
}

type admissionBudget struct {
	Started, Deadline time.Time
}

func parseAdmissionBudget(start, end string, now time.Time) (admissionBudget, error) {
	s, err := strconv.ParseInt(start, 10, 64)
	if err != nil || s <= 0 || strconv.FormatInt(s, 10) != start {
		return admissionBudget{}, errors.New("invalid authenticated publisher start timestamp")
	}
	d, err := strconv.ParseInt(end, 10, 64)
	if err != nil || d <= 0 || d != s+admissionSeconds || strconv.FormatInt(d, 10) != end {
		return admissionBudget{}, errors.New("publisher deadline must retain the original sixty-second budget")
	}
	if s > now.Unix() || now.Unix() >= d {
		return admissionBudget{}, errors.New("publisher start is future or its setup-inclusive budget is exhausted")
	}
	return admissionBudget{Started: time.Unix(s, 0), Deadline: time.Unix(d, 0)}, nil
}

type admissionTrace struct {
	PR     pull
	Policy *liveQueuePolicy
	Run    *run
	// PolicyRead and FeedbackRead record that the invocation's read returned.
	// Policy stays nil for a policy that returned disabled, and Run for feedback
	// that returned none, so a nil alone does not say the read never finished. A
	// trace the budget cut off before a read has nothing to compare with, and a
	// difference there is not movement.
	PolicyRead, FeedbackRead bool
}

func (q *queueController) executeTargeted(ctx context.Context, name string, e event, b admissionBudget) error {
	return q.withAdmissionBudget(ctx, b, func(work context.Context) error { return q.execute(work, name, e) })
}

// The proof is finished, so the work clock must not cut the write that publishes it.
// GitHub can apply a write whose reply is lost; the run then counted as unfinished and
// replaced its own pass with a barrier.
func (q *queueController) verdictWriteContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if !q.targeted || q.queueDeadline.IsZero() || ctx.Err() != nil {
		return ctx, func() {}
	}
	return context.WithDeadline(context.WithoutCancel(ctx), q.queueDeadline.Add(admissionWriteGrace-admissionReserve))
}

func (q *queueController) withAdmissionBudget(ctx context.Context, b admissionBudget, invoke func(context.Context) error) error {
	q.targeted = true
	oldDeadline := q.queueDeadline
	q.queueDeadline = b.Deadline
	defer func() { q.queueDeadline = oldDeadline }()
	q.admissions = make(map[int]*admissionTrace)
	work, cancel := context.WithDeadline(ctx, b.Deadline.Add(-admissionReserve))
	err := invoke(work)
	workErr := work.Err()
	cancel()
	if onlyStaleHeadWork(err) && workErr == nil {
		return nil
	}
	// A pass that finished and published its verdict is final even when the work
	// clock ran out a moment later; refusing now would overwrite that fresh verdict.
	if err == nil {
		return nil
	}
	// This is a new bounded context, never the cancelled validation context. It
	// does not extend the shared deadline and is shared by all routed PRs.
	reserveEnd := time.Now().Add(admissionReserve)
	if b.Deadline.Before(reserveEnd) {
		reserveEnd = b.Deadline
	}
	reserve, stop := context.WithDeadline(context.WithoutCancel(ctx), reserveEnd)
	defer stop()
	var failures []error
	if err != nil {
		failures = append(failures, err)
	}
	if workErr != nil {
		failures = append(failures, fmt.Errorf("targeted admission work budget exhausted: %w", workErr))
	}
	numbers := make([]int, 0, len(q.admissions))
	for n := range q.admissions {
		numbers = append(numbers, n)
	}
	slices.Sort(numbers)
	for _, n := range numbers {
		if err := reserve.Err(); err != nil {
			failures = append(failures, fmt.Errorf("admission publication reserve exhausted: %w", err))
			break
		}
		if err := q.refuseAdmission(reserve, q.admissions[n]); err != nil {
			failures = append(failures, fmt.Errorf("admission failure could not be published for PR #%d: %w", n, err))
		}
	}
	return errors.Join(failures...)
}

// Every error reaches these records, so they name no cause. They say what
// reassesses the head, which is what the gate workflows react to: ci-dev-gate.yml
// and ci-queue-gate.yml run on a PR event, a feedback run, a push to main and a
// schedule, and take a pr_number dispatch. A push to dev reassesses nothing unless
// it changed the dev publisher (scripts/ci/discover-dev-prs.sh), so it is not named.
func (q *queueController) reassessedBy(number int) string {
	gate := "gate-queue"
	if q.checkName == devCheckName {
		gate = "gate-dev"
	}
	return fmt.Sprintf("It is reassessed by a new push to this PR, a feedback result, a push to main, the schedule, or manually: gh workflow run %s -f pr_number=%d.", gate, number)
}

const unfinished = "The publisher did not finish: it failed or ran out of its budget. Merging stays blocked."

func (q *queueController) pendingText(number int) outcome {
	return pending(unfinished + " A failed run is rerun only while attempts remain. " + q.reassessedBy(number))
}

func (q *queueController) refusalText(number int) outcome {
	required := "Feedback and full queue validation remain required."
	if q.checkName == devCheckName {
		required = "Fast feedback, DCO and policy remain required."
	}
	return refused(unfinished + " " + required + " The linked run names the error; if it persists, fix it before reassessing. " + q.reassessedBy(number))
}

func (q *queueController) refuseAdmission(ctx context.Context, t *admissionTrace) error {
	p, err := q.readPR(ctx, t.PR.Number)
	if err != nil {
		return err
	}
	// A timeout belongs to the captured invocation, not a newly pushed head,
	// retargeted PR or advanced main. Its existing pending barrier stays safe.
	if p.State != "open" || p.Head != t.PR.Head || !p.sameBase(t.PR) {
		return errors.New("current PR moved during admission failure publication; refusing a different tuple")
	}
	// PRs to dev or a stacked base hold no queue role. A pending record here is retired
	// by the next pass; retirement skips completed records, so a refusal would stay red.
	if q.checkName == queueCheckName && p.Base.Ref != q.repo.DefaultBranch {
		return errors.New("a PR outside the default branch holds no queue role; no refusal is published, and its pending record stays until the next pass retires it")
	}
	policy, err := q.policy(ctx)
	if err != nil {
		return err
	}
	// A read that never returned is not movement, unless the queue is off now: the
	// schedule, a push to main and a dispatch do nothing then, so the refusal text
	// would be wrong. The record already on the head stays.
	if (t.PolicyRead || !policy.Enabled) && (t.Policy == nil || policy != *t.Policy) {
		return errors.New("current protection moved or was not captured before admission failure")
	}
	r, err := q.feedback(ctx, p)
	if err != nil {
		return err
	}
	if t.FeedbackRead && ((r == nil) != (t.Run == nil) || r != nil && !sameSourceEvent(*r, *t.Run)) {
		return errors.New("useful feedback attempt moved during admission failure publication")
	}
	final, err := q.readPR(ctx, p.Number)
	if err != nil {
		return err
	}
	if final.State != "open" || final.Head != p.Head || !final.sameBase(p) {
		return errors.New("current PR moved in the admission publication reserve")
	}
	finalPolicy, err := q.policy(ctx)
	if err != nil {
		return err
	}
	if finalPolicy != policy {
		return errors.New("protection moved in the admission publication reserve")
	}
	finalRun, err := q.feedback(ctx, final)
	if err != nil {
		return err
	}
	if (finalRun == nil) != (r == nil) || finalRun != nil && !sameSourceEvent(*finalRun, *r) {
		return errors.New("feedback attempt moved in the admission publication reserve")
	}
	own, err := q.ownedChecks(ctx, final)
	if err != nil {
		return err
	}
	var selected *check
	for i := range own {
		if selected == nil || own[i].ID > selected.ID {
			selected = &own[i]
		}
	}
	result := q.refusalText(final.Number)
	if admissionRetryRemains() {
		// Still blocking, but not a verdict: a rerun follows while attempts remain.
		result = q.pendingText(final.Number)
	}
	return q.publish(ctx, final, newBinding(q.repo.ID, final, r), selected, result)
}
