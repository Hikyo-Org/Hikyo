package main

import "context"

// initialReads holds the first-pass authority reads of one admission. They are
// observations only: admitProof judges them in its original order, and the final
// phase repeats the feedback and role reads before any success is published.
type initialReads struct {
	feedback    *run
	feedbackErr error
	roles       headRoles
	rolesErr    error
	route       string
	routeErr    error
	contractErr error
}

// runInitialReads runs the independent first-pass reads of a live policy at once
// and returns when all are done. Some are issued where the serial code would have
// stopped first, such as the route check beside a role collision; admitProof drops
// those results, though such a read that hangs can still spend the budget. The
// policy verifier is deliberately not among them: it is the longest read, and the
// serial code never spent it on a failed read, a stale head or a group collision.
func (q *queueController) runInitialReads(ctx context.Context, p pull, readRoles, readFeedback bool) *initialReads {
	r := &initialReads{}
	var reads []func(context.Context)
	if readFeedback {
		reads = append(reads, func(ctx context.Context) { r.feedback, r.feedbackErr = q.feedback(ctx, p) })
	}
	if readRoles {
		reads = append(reads, func(ctx context.Context) { r.roles, r.rolesErr = q.roles(ctx, p.Head.SHA) })
	}
	if p.admission().Ref == q.admissionBranch() {
		reads = append(reads,
			func(ctx context.Context) { r.route, r.routeErr = q.mainRouteRefusal(ctx, p) },
			func(ctx context.Context) { r.contractErr = q.devPublisherContract(ctx, p) },
		)
	}
	settle(ctx, reads...)
	return r
}
