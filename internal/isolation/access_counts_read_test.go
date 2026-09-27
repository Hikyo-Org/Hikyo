package isolation

import (
	"context"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"testing"
	"time"
)

func TestAccessOperationalCountsUseReadTransaction(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		// Holding SQLite's sole writer proves the scrape uses the independent WAL
		// reader pool. A write transaction here would wait until the deadline.
		if db.Engine() == store.EngineSQLite {
			writer, err := db.BeginSQLite(t.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Rollback()
		}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		svc := &service.Access{DB: db}
		open, active, err := svc.OperationalCounts(ctx)
		if err != nil || open != 0 || active != 0 {
			t.Fatalf("counts during writer: open=%d active=%d error=%v", open, active, err)
		}
	})
}
