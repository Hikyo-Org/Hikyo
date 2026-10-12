package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"
)

const admissionSeconds = 60
const admissionReserve = 15 * time.Second

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
}

func (q *queueController) executeTargeted(ctx context.Context, name string, e event, b admissionBudget) error {
	return q.withAdmissionBudget(ctx, b, func(work context.Context) error { return q.execute(work, name, e) })
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
	if err == nil && workErr == nil {
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

func (q *queueController) refuseAdmission(ctx context.Context, t *admissionTrace) error {
	p, err := q.readPR(ctx, t.PR.Number)
	if err != nil {
		return err
	}
	// A timeout belongs to the captured invocation, not a newly pushed head,
	// retargeted PR or advanced main. Its existing pending barrier stays safe.
	if p.State != "open" || p.Head != t.PR.Head || p.Base != t.PR.Base {
		return errors.New("current PR moved during admission failure publication; refusing a different tuple")
	}
	policy, err := q.policy(ctx)
	if err != nil {
		return err
	}
	if t.Policy == nil || policy != *t.Policy {
		return errors.New("current protection moved or was not captured before admission failure")
	}
	r, err := q.feedback(ctx, p)
	if err != nil {
		return err
	}
	if (r == nil) != (t.Run == nil) || r != nil && !sameSourceEvent(*r, *t.Run) {
		return errors.New("useful feedback attempt moved during admission failure publication")
	}
	own, err := q.ownedChecks(ctx, p)
	if err != nil {
		return err
	}
	final, err := q.readPR(ctx, p.Number)
	if err != nil {
		return err
	}
	if final.State != "open" || final.Head != p.Head || final.Base != p.Base {
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
	own, err = q.ownedChecks(ctx, final)
	if err != nil {
		return err
	}
	var selected *check
	for i := range own {
		if selected == nil || own[i].ID > selected.ID {
			selected = &own[i]
		}
	}
	return q.publish(ctx, final, newBinding(q.repo.ID, final, r), selected,
		refused("The targeted publisher failed within its setup-inclusive sixty-second budget. All selected feedback and full queue validation remain required; reassess after correcting the error."))
}
