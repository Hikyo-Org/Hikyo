package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Keep work counts separate from uninstrumented latency/allocation samples.
// Presence-only means a read-only caller's secret projection: config still
// decrypts and crosses. It is not a third wire projection enum.
func TestDeliveryPollWork(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "poll.db")}
			if engine == store.EnginePostgres {
				cfg = selfConfigPostgres(t)
			}
			f := newDeliveryPollFixture(t, cfg, 20, 1280)
			cases := deliveryPollCases(t, f)
			for _, c := range cases {
				if got := countDeliveryRows(t, f, c); got != c.opened {
					t.Fatalf("%s opens=%d, want %d", c.name, got, c.opened)
				}
			}
			before, after := deliveryPollCursors(t, f, cases)
			for i, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					if got := countDeliveryRows(t, f, c); got != c.opened {
						t.Fatalf("post-publication opens=%d, want %d", got, c.opened)
					}
					for _, current := range []bool{true, false} {
						cursor := before[i]
						if current {
							cursor = after[i]
						}
						fetches, disclosures := deliveryPollAuditCounts(t, f)
						out, err := f.svc.Fetch(t.Context(), c.bearer, f.scope, cursor, c.options)
						checkDeliveryPoll(t, out, err, c, current)
						if out.Cursor != after[i] {
							t.Fatal("poll returned a different post-publication cursor")
						}
						gotFetches, gotDisclosures := deliveryPollAuditCounts(t, f)
						wantDisclosures := c.opened
						if current {
							wantDisclosures = 0
						}
						if gotFetches-fetches != 1 || gotDisclosures-disclosures != wantDisclosures {
							t.Fatalf("current=%v audit deltas: fetch=%d disclosure=%d, want 1/%d", current, gotFetches-fetches, gotDisclosures-disclosures, wantDisclosures)
						}
					}
					// A current cursor must still validate parameters and the
					// credential's liveness at the actual delivery transaction.
					invalidOptions := c.options
					invalidOptions.Parameters = map[string]string{"UNDECLARED": "synthetic"}
					if _, err := f.svc.Fetch(t.Context(), c.bearer, f.scope, after[i], invalidOptions); !errors.Is(err, domain.ErrInvalid) {
						t.Fatalf("current cursor with undeclared parameter: %v", err)
					}
					if c.expiresAt.IsZero() {
						t.Fatal("fixture credential must have finite expiry")
					}
					f.svc.Now = func() time.Time { return c.expiresAt.Add(time.Second) }
					_, err := f.svc.Fetch(t.Context(), c.bearer, f.scope, after[i], c.options)
					f.svc.Now = nil
					if !errors.Is(err, domain.ErrUnauthenticated) {
						t.Fatalf("current cursor after credential expiry: %v", err)
					}
					t.Logf("included=%d snapshot AEAD opens=%d per row build (both cursor dispositions)", c.included, c.opened)
				})
			}
		})
	}
}

func deliveryPollAuditCounts(t testing.TB, f deliveryPollFixture) (int, int) {
	t.Helper()
	var fetches, disclosures int
	err := tx.Read(t.Context(), f.svc.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		fetches, disclosures = 0, 0
		_, p, err := authorize(ctx, az, f.admin, authz.OpAuditQueryEnv, f.scope, f.svc.now())
		if err != nil {
			return err
		}
		filter := store.AuditFilter{Limit: store.AuditMaxPageSize}
		for {
			rows, err := r.Audit().PageTenant(ctx, p, filter)
			if err != nil {
				return err
			}
			for _, row := range rows {
				switch row.Type {
				case audit.EventDeliveryFetched:
					fetches++
				case audit.EventDisclosure:
					disclosures++
				}
				filter.AfterSeq = row.Seq
			}
			if len(rows) < filter.Limit {
				return nil
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return fetches, disclosures
}
