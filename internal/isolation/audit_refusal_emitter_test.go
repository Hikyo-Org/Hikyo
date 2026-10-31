package isolation

import (
	"context"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/jackc/pgx/v5"
)

// Emitter inventory uses durable fixture staging, not a promise that every
// runtime accounting attempt succeeds inside its bounded accounting budget.
func TestAuditRefusalEmitterWaitsForStagingWriter(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		var release func()
		if db.Engine() == store.EngineSQLite {
			held, err := db.BeginSQLite(t.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			release = func() { _ = held.Rollback() }
		} else {
			held, err := db.BeginPostgres(t.Context(), pgx.TxOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := held.Exec(t.Context(), "SELECT id FROM auth_refusal_window WHERE id=1 FOR UPDATE"); err != nil {
				_ = held.Rollback(t.Context())
				t.Fatal(err)
			}
			release = func() { _ = held.Rollback(t.Context()) }
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			timer := time.NewTimer(2 * service.RefusalDeadline)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
			}
			release()
		}()
		t.Cleanup(func() { cancel(); <-done })
		runRefusalAuditLifecycle(t, db)
		if got := sealedRefusalCount(t, db); got != 1 {
			t.Fatalf("emitter lost or duplicated the fixture sample: %d", got)
		}
	})
}
