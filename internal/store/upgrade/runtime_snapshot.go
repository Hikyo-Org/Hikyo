package upgrade

import "context"

// ReadPostgresSnapshot reads only the durable upgrade control record.
func ReadPostgresSnapshot(ctx context.Context, q PGSnapshot) (State, error) {
	return scanState(q.QueryRow(ctx, snapshotSQL))
}
