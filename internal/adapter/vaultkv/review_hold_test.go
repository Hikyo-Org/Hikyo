package vaultkv

import (
	"errors"
	"maps"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

func TestLegacyPendingMetadataStopsWholeTargetBeforeAnyMutation(t *testing.T) {
	for _, teardown := range []bool{false, true} {
		name := "sync"
		if teardown {
			name = "teardown"
		}
		t.Run(name, func(t *testing.T) {
			kv := newFakeKV()
			journal := newFakeJournal()
			target := testTarget(t, kv)
			module := &Module{API: kv}
			if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest}, journal); err != nil {
				t.Fatal(err)
			}
			// This later name is Owned, rather than Dispatched. Earlier names
			// and the sentinel must remain untouched while its pending writer
			// is unresolved, including when the whole target is being removed.
			kv.paths["apps/pay/LOG_LEVEL"].custom[PendingKey] = "2"
			before := maps.Clone(journal.states)
			outcomes := len(journal.outcomes)
			kv.calls = nil
			req := adapter.SyncRequest{Target: target, Manifest: manifest, Ledger: journal.ledger(), Teardown: teardown}
			if teardown {
				req.Manifest = nil
			}
			if _, err := module.Sync(t.Context(), req, journal); !errors.Is(err, adapter.ErrOperatorReview) {
				t.Fatalf("legacy pending hold = %v", err)
			}
			if len(kv.mutations()) != 0 || !maps.Equal(before, journal.states) || len(journal.outcomes) != outcomes {
				t.Fatalf("held target changed before review: provider=%v custody=%v", kv.mutations(), journal.states)
			}
		})
	}
}
