package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

const queueSnapshotAttempts = 3

// Movement invalidates the entire observation, never just the current page.
// It cannot authorize a verdict or relax any identity/completeness check.
type queueSnapshotChanged struct{ reason string }

func (e *queueSnapshotChanged) Error() string { return e.reason }

func (q *queueController) liveQueue(ctx context.Context) (*queueData, error) {
	var last error
	for attempt := 1; attempt <= queueSnapshotAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(last, err)
		}
		live, err := q.readQueueSnapshot(ctx)
		var moved *queueSnapshotChanged
		if !errors.As(err, &moved) {
			return live, err
		}
		last = err
		if _, writeErr := fmt.Fprintf(q.output, "Merge queue snapshot moved (attempt %d/%d): %v\n", attempt, queueSnapshotAttempts, err); writeErr != nil {
			return nil, errors.Join(err, writeErr)
		}
		if attempt == queueSnapshotAttempts {
			break
		}
		// Bound both attempts and backoff within the caller's existing deadline.
		// This retries only a read snapshot, never authority or a mutation.
		timer := time.NewTimer(time.Duration(attempt) * 100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errors.Join(last, ctx.Err())
		case <-timer.C:
		}
	}
	return nil, last
}

func incompleteQueuePositions(live *queueData, page, missing int) error {
	limit := min(len(live.Entries.Nodes), 16)
	positions, prs := make([]int, 0, limit), make([]int, 0, limit)
	for _, entry := range live.Entries.Nodes[:limit] {
		positions = append(positions, entry.Position)
		prs = append(prs, entry.PR.Number)
	}
	// Hash the opaque server ID rather than logging arbitrary response strings.
	id := sha256.Sum256([]byte(live.ID))
	return &queueSnapshotChanged{reason: fmt.Sprintf("incomplete paginated queue positions: queue=%x page=%d entries=%d missing_position=%d positions=%v PRs=%v truncated=%t", id[:8], page, len(live.Entries.Nodes), missing, positions, prs, limit < len(live.Entries.Nodes))}
}
