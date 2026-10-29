package vaultkv

import (
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

func TestUndesiredReservationDoesNotReadProviderMetadata(t *testing.T) {
	for _, teardown := range []bool{false, true} {
		name := "converge"
		if teardown {
			name = "teardown"
		}
		t.Run(name, func(t *testing.T) {
			kv := newFakeKV()
			journal := newFakeJournal()
			journal.states["secret:UNSENT"] = adapter.Reserved
			denied := "read-metadata:apps/pay/UNSENT"
			kv.failOn[denied] = &ResponseError{Status: 403}
			module := &Module{API: kv}
			_, err := module.Sync(t.Context(), adapter.SyncRequest{Target: testTarget(t, kv), Ledger: journal.ledger(), Teardown: teardown}, journal)
			if err != nil {
				t.Fatalf("release unsent reservation: %v", err)
			}
			if slices.Contains(kv.calls, denied) || journal.releases["secret:UNSENT"] != 1 {
				t.Fatalf("reservation required provider access or was not released: calls=%v releases=%v", kv.calls, journal.releases)
			}
			if _, retained := journal.states["secret:UNSENT"]; retained {
				t.Fatal("unsent reservation retained")
			}
		})
	}
}

func TestDesiredReservationStillChecksPendingMetadata(t *testing.T) {
	kv := newFakeKV()
	journal := newFakeJournal()
	journal.states["secret:DATABASE_URL"] = adapter.Reserved
	kv.externalWrite("apps/pay/DATABASE_URL", "foreign")
	kv.paths["apps/pay/DATABASE_URL"].custom[PendingKey] = "2"
	module := &Module{API: kv}
	_, err := module.Sync(t.Context(), adapter.SyncRequest{Target: testTarget(t, kv), Ledger: journal.ledger(), Manifest: []adapter.ManifestEntry{{CanonicalName: "DATABASE_URL", Classification: adapter.SecretClassification, Value: "replacement"}}}, journal)
	if !errors.Is(err, adapter.ErrOperatorReview) || len(kv.mutations()) != 0 || len(journal.releases) != 0 {
		t.Fatalf("desired pending reservation escaped hold: err=%v mutations=%v releases=%v", err, kv.mutations(), journal.releases)
	}
}

func TestLegacyPendingMetadataStopsWholeTargetBeforeAnyMutation(t *testing.T) {
	for _, variant := range []string{"prepared", "advanced", "malformed", "missing-marker", "foreign-marker"} {
		t.Run(variant, func(t *testing.T) {
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
					switch variant {
					case "advanced":
						kv.paths["apps/pay/LOG_LEVEL"].current = 3
					case "malformed":
						kv.paths["apps/pay/LOG_LEVEL"].custom[PendingKey] = "unknown"
					case "missing-marker":
						delete(kv.paths["apps/pay/LOG_LEVEL"].custom, MarkerKey)
					case "foreign-marker":
						kv.paths["apps/pay/LOG_LEVEL"].custom[MarkerKey] = "other-target"
					}
					journal.states["secret:UNSENT"] = adapter.Reserved
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
		})
	}
}
