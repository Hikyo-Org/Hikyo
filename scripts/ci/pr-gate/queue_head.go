package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Discovery and event SHAs select only a concurrency lease. Current source
// movement must never turn that lease into authority to write another head.
type staleHeadWork struct{ expected string }

func (e *staleHeadWork) Error() string {
	return fmt.Sprintf("Queue work is stale for held head %s; no check for a different head was written", e.expected)
}

func onlyStaleHeadWork(err error) bool {
	if err == nil {
		return false
	}
	if many, ok := err.(interface{ Unwrap() []error }); ok {
		parts := many.Unwrap()
		if len(parts) == 0 {
			return false
		}
		for _, part := range parts {
			if !onlyStaleHeadWork(part) {
				return false
			}
		}
		return true
	}
	if one, ok := err.(interface{ Unwrap() error }); ok {
		return onlyStaleHeadWork(one.Unwrap())
	}
	_, ok := err.(*staleHeadWork)
	return ok
}

type headRoles struct {
	Pulls    []pull
	Group    bool
	Snapshot *queueSnapshot
	AllPulls map[int]pull
}

// A worker may reuse only the initial snapshot it read in this reconciliation.
// Final authority, source and role reads remain independent; nothing is cached
// across invocations or supplied by dispatch routing hints.
type admissionSnapshot struct {
	policy liveQueuePolicy
	roles  headRoles
}

// REST Base.SHA can lag behind live main; ref/repository and exact source
// identity must still match the proof when a later complete list is observed.
func sameRolePull(current, proved pull) bool {
	return current.State == "open" && current.Number == proved.Number && current.User == proved.User && current.Head == proved.Head && current.Base.Ref == proved.Base.Ref && current.Base.Repo == proved.Base.Repo
}

func materializedHeads(live *queueData, branch string) ([]string, error) {
	seen := make(map[string]bool)
	for _, e := range live.Entries.Nodes {
		if e.PR.Base != branch || !shaPattern.MatchString(e.PR.Head) {
			return nil, errors.New("invalid queue discovery PR identity")
		}
		switch e.State {
		case "QUEUED", "AWAITING_CHECKS", "MERGEABLE", "UNMERGEABLE", "LOCKED":
		default:
			return nil, errors.New("invalid queue discovery state")
		}
		if e.Head.OID == "" && e.State == "QUEUED" {
			continue
		}
		if !shaPattern.MatchString(e.Head.OID) || !shaPattern.MatchString(e.Base.OID) || seen[e.Head.OID] {
			return nil, errors.New("invalid or ambiguous materialized group head")
		}
		seen[e.Head.OID] = true
	}
	heads := make([]string, 0, len(seen))
	for head := range seen {
		heads = append(heads, head)
	}
	sort.Strings(heads)
	return heads, nil
}

func (q *queueController) roles(ctx context.Context, head string) (headRoles, error) {
	var roles headRoles
	var pulls []pull
	var live *queueData
	if err := parallelReads(ctx,
		func(ctx context.Context) (err error) {
			pulls, err = q.openPRs(ctx)
			return err
		},
		func(ctx context.Context) (err error) {
			live, err = q.liveQueue(ctx)
			return err
		},
	); err != nil {
		return roles, err
	}
	heads, err := materializedHeads(live, q.repo.DefaultBranch)
	if err != nil {
		return roles, err
	}
	roles.AllPulls = make(map[int]pull, len(pulls))
	for _, p := range pulls {
		roles.AllPulls[p.Number] = p
		if p.Head.SHA == head {
			roles.Pulls = append(roles.Pulls, p)
		}
	}
	for _, group := range heads {
		if group == head {
			roles.Group = true
			snapshot, err := q.snapshotQueue(live, head)
			if err != nil {
				return roles, err
			}
			roles.Snapshot = &snapshot
		}
	}
	return roles, nil
}

func (q *queueController) roleCollision(ctx context.Context, roles headRoles) error {
	if !roles.Group || len(roles.Pulls) == 0 {
		return errors.New("no authenticated cross-role collision")
	}
	p, err := q.readPR(ctx, roles.Pulls[0].Number)
	if err != nil {
		return err
	}
	if p.State != "open" || p.Head.SHA != q.expectedHead {
		return &staleHeadWork{expected: q.expectedHead}
	}
	return q.publish(ctx, p, newBinding(q.repo.ID, p, nil), nil, refused("This SHA is both an open PR head and a materialized queue-group head. Cross-role check reuse is ambiguous; admission and merging remain blocked."))
}

func (q *queueController) runHead(ctx context.Context, name string, e event, kind string) error {
	q.repo = repository{}
	if err := q.init(ctx); err != nil {
		batch, parseErr := parseHeadBatch(e.Inputs.Work)
		if name != "workflow_dispatch" || parseErr != nil {
			return err
		}
		for _, work := range batch {
			if work.Head == q.expectedHead && work.Kind == kind {
				return q.initializedFailure(ctx, e.Repository.ID, e.Repository.FullName, err)
			}
		}
		return err
	}
	if name != "workflow_dispatch" || e.Repository.ID != q.repo.ID || !strings.EqualFold(e.Repository.FullName, q.repoName) || !shaPattern.MatchString(q.expectedHead) {
		return errors.New("head worker requires authenticated manual dispatch and an exact lease head")
	}
	batch, err := parseHeadBatch(e.Inputs.Work)
	if err != nil {
		return err
	}
	matched := false
	for _, work := range batch {
		if work.Head == q.expectedHead && work.Kind == kind {
			matched = true
		}
	}
	if !matched {
		return errors.New("worker head and role do not match its typed discovery record")
	}
	return q.reassess(ctx, func(ctx context.Context) error { return q.runHeadProof(ctx, name, e, kind) })
}

func (q *queueController) runHeadProof(ctx context.Context, name string, e event, kind string) error {
	policy, err := q.policy(ctx)
	if err != nil {
		return err
	}
	if !policy.Enabled {
		return nil
	}
	roles, err := q.roles(ctx, q.expectedHead)
	if err != nil {
		return err
	}
	if roles.Group && len(roles.Pulls) > 0 {
		return q.roleCollision(ctx, roles)
	}
	if kind == "admission" && !roles.Group && len(roles.Pulls) > 0 {
		captured := admissionSnapshot{policy: policy, roles: roles}
		var failures []error
		for _, p := range roles.Pulls {
			if err := q.admitCaptured(ctx, p.Number, &captured); err != nil && !onlyStaleHeadWork(err) {
				failures = append(failures, err)
			}
		}
		return errors.Join(failures...)
	}
	if kind == "group" && roles.Group && len(roles.Pulls) == 0 {
		r, err := q.latestGroup(ctx, q.expectedHead)
		if err != nil {
			return err
		}
		if r == nil {
			return nil
		} // No source exists; never invent a group proof.
		return q.group(ctx, *r)
	}
	return &staleHeadWork{expected: q.expectedHead}
}
