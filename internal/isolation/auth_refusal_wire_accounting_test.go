package isolation

import (
	"context"
	"sync"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/jackc/pgx/v5"
)

// These wire requests are serial. Observe the actual runtime result without
// changing its context, deadline, datastore writer or returned error.
type refusalWireRecorder struct {
	signals *service.RefusalSignals
	results []error
}

func (r *refusalWireRecorder) Record(ctx context.Context, sample audit.RefusalSample) error {
	err := r.signals.Record(ctx, sample)
	r.results = append(r.results, err)
	return err
}

// Capture staging and sealed counts together after unacknowledged PostgreSQL
// commits settle behind the existing singleton writer fence.
func refusalWireAccounting(t *testing.T, db *store.DB) (audit.RefusalWindow, int64) {
	t.Helper()
	window, sealed := refusalAccountingSnapshot(t, db)
	total := sealed
	for _, count := range window.Counts {
		total += count
	}
	return window, total
}

// holdRefusalWireWriter makes the next wire accounting attempt meet a real
// unavailable writer while leaving authentication and its response unchanged.
func holdRefusalWireWriter(t *testing.T, db *store.DB) func() {
	t.Helper()
	var rollback func()
	if db.Engine() == store.EngineSQLite {
		held, err := db.BeginSQLite(t.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		rollback = func() { _ = held.Rollback() }
	} else {
		held, err := db.BeginPostgres(t.Context(), pgx.TxOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := held.Exec(t.Context(), "SELECT id FROM auth_refusal_window WHERE id=1 FOR UPDATE"); err != nil {
			_ = held.Rollback(t.Context())
			t.Fatal(err)
		}
		rollback = func() { _ = held.Rollback(t.Context()) }
	}
	release := sync.OnceFunc(rollback)
	t.Cleanup(release)
	return release
}
