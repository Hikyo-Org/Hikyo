package isolation

import (
	"context"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
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
