package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// BenchmarkAuditBulkDisclosure300 isolates the bulk-disclosure audit writer:
// synthetic, already-rendered secret cells, real authorization and durable SQLite
// commits. It excludes decryption, network, reauthentication and fixture reset.
func BenchmarkAuditBulkDisclosure300(b *testing.B) {
	f := newWorkflowBenchmark(b, 10)
	// The untimed durability check uses the separately authorized audit reader.
	if _, err := (&Grants{DB: f.values.DB}).Create(b.Context(), f.actor, GrantSpec{
		Target: f.actor.principal, Capability: domain.CapAuditRead, Scope: f.scope,
	}); err != nil {
		b.Fatal(err)
	}
	reset := resetWorkflow(b, &f)
	cells := make([]ValueCell, 300)
	for i := range cells {
		cells[i] = ValueCell{KeyID: fmt.Sprintf("synthetic_key_%d", i), Name: fmt.Sprintf("SECRET_%d", i), Classification: "secret", Set: true}
	}
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		reset()
		b.StartTimer()
		err := tx.Write(b.Context(), f.values.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			caller, p, err := authorize(ctx, az, f.actor, authz.OpValueExportReveal, f.scope, time.Now())
			if err != nil {
				return err
			}
			return auditDisclosures(ctx, r.Audit(), p, caller.Principal, cells, "export")
		})
		b.StopTimer()
		if err != nil {
			b.Fatal(err)
		}
		page, err := (&Audits{DB: f.values.DB}).Query(b.Context(), f.actor.principal, f.scope, store.AuditFilter{Type: string(audit.EventValueRevealed), Limit: 1000})
		if err != nil || len(page.Events) != 300 {
			b.Fatalf("disclosure rows=%d, want 300: %v", len(page.Events), err)
		}
		b.StartTimer()
	}
}
