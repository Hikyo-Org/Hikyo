package main

import "context"

// initialReads holds the first-pass authority reads of one admission. They are
// observations only: admitProof judges them in its original order, and the
// final phase repeats every authority read before any success is published.
type initialReads struct {
	feedback    *run
	feedbackErr error
	roles       headRoles
	rolesErr    error
	route       string
	routeErr    error
	contractErr error
}

// startInitialReads runs the independent first-pass reads of a live policy at
// once and returns when all are done. A read is started only when the serial
// code would also have reached it: the route and contract checks belong to the
// gated base branch. The policy verifier is deliberately not among them: it is
// the longest read, and the serial code never spent it on a head whose feedback
// or role inventory had already failed or collided.
func (q *queueController) startInitialReads(ctx context.Context, p pull, readRoles, readFeedback bool) *initialReads {
	r := &initialReads{}
	var reads []func(context.Context)
	if readFeedback {
		reads = append(reads, func(ctx context.Context) { r.feedback, r.feedbackErr = q.feedback(ctx, p) })
	}
	if readRoles {
		reads = append(reads, func(ctx context.Context) { r.roles, r.rolesErr = q.roles(ctx, p.Head.SHA) })
	}
	if p.Base.Ref == q.admissionBranch() {
		reads = append(reads,
			func(ctx context.Context) { r.route, r.routeErr = q.mainRouteRefusal(ctx, p) },
			func(ctx context.Context) { r.contractErr = q.devPublisherContract(ctx, p) },
		)
	}
	settle(ctx, reads...)
	return r
}
