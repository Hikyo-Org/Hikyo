package vaultkv

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

func TestDeliveredPathWithoutOwnershipMarkersRefusesSyncAndPrune(t *testing.T) {
	for _, removeAll := range []bool{false, true} {
		for _, externalWrite := range []bool{false, true} {
			name := "marker-only"
			if removeAll {
				name = "all-markers"
			}
			if externalWrite {
				name += "/advanced-version"
			} else {
				name += "/same-version"
			}
			t.Run(name, func(t *testing.T) {
				kv := newFakeKV()
				journal := newFakeJournal()
				target := testTarget(t, kv)
				module := &Module{API: kv}
				if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1]}, journal); err != nil {
					t.Fatal(err)
				}
				path := "apps/pay/DATABASE_URL"
				delete(kv.paths[path].custom, MarkerKey)
				if removeAll {
					kv.paths[path].custom = map[string]string{}
				}
				if externalWrite {
					kv.externalWrite(path, "external-value")
				}
				before := kv.paths[path].current
				kv.calls = nil
				result, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1], Ledger: journal.ledger()}, journal)
				if !errors.Is(err, adapter.ErrConflict) || len(result.Conflicts) != 1 {
					t.Fatalf("missing marker sync = %+v, %v; want conflict", result, err)
				}
				for _, call := range kv.mutations() {
					if strings.Contains(call, "DATABASE_URL") {
						t.Fatalf("markerless path mutated: %s", call)
					}
				}
				kv.calls = nil
				result, err = module.Sync(t.Context(), adapter.SyncRequest{Target: target, Ledger: journal.ledger(), Teardown: true}, journal)
				if err != nil {
					t.Fatal(err)
				}
				if slices.Contains(kv.calls, "soft-delete:"+path) || kv.paths[path].current != before || kv.paths[path].deleted[before] {
					t.Fatal("markerless path pruned or overwritten")
				}
				if journal.states["secret:DATABASE_URL"] != "" || len(result.Warnings) != 1 {
					t.Fatalf("unsafe custody not released with warning: %+v, %+v", journal.states, result)
				}
			})
		}
	}
}
