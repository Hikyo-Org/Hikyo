package vaultkv

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

type lostCASResponseKV struct {
	*fakeKV
	path string
}

func (f *lostCASResponseKV) WriteCAS(ctx context.Context, mount, path, value string, cas int64) (int64, error) {
	if path != f.path {
		return f.fakeKV.WriteCAS(ctx, mount, path, value, cas)
	}
	f.externalWrite(path, "external-writer")
	// The provider refuses Hikyo's CAS, but the response is lost. The pending
	// metadata now names the external writer's version, exactly the attack.
	if _, err := f.fakeKV.WriteCAS(ctx, mount, path, value, cas); !IsCASMismatch(err) {
		return 0, errors.New("fixture did not lose its CAS")
	}
	return 0, errors.New("connection reset before CAS refusal received")
}

func TestLostCASResponseNeverCapturesExternalPendingVersion(t *testing.T) {
	kv := newFakeKV()
	journal := newFakeJournal()
	target := testTarget(t, kv)
	module := &Module{API: kv}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1]}, journal); err != nil {
		t.Fatal(err)
	}
	path := "apps/pay/DATABASE_URL"
	module.API = &lostCASResponseKV{fakeKV: kv, path: path}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1], Ledger: journal.ledger()}, journal); !errors.Is(err, adapter.ErrOperatorReview) {
		t.Fatalf("lost CAS response = %v; want operator review", err)
	}
	current := kv.paths[path].current
	markers := maps.Clone(kv.paths[path].custom)
	if markers[PendingKey] != "2" || journal.states["secret:DATABASE_URL"] != adapter.Dispatched {
		t.Fatal("fixture lacks ambiguous version-two custody")
	}
	module.API = kv
	for _, teardown := range []bool{false, true} {
		kv.calls = nil
		req := adapter.SyncRequest{Target: target, Manifest: manifest[:1], Ledger: journal.ledger(), Teardown: teardown}
		if _, err := module.Sync(t.Context(), req, journal); !errors.Is(err, adapter.ErrOperatorReview) {
			t.Fatalf("future sync/teardown review = %v", err)
		}
		if value, ok := kv.value(path); !ok || value != "external-writer" || kv.paths[path].current != current || !maps.Equal(markers, kv.paths[path].custom) || len(kv.mutations()) != 0 {
			t.Fatal("review refusal changed external value, version, metadata, or other provider state")
		}
		if journal.states["secret:DATABASE_URL"] != adapter.Dispatched {
			t.Fatal("review refusal released uncertain custody")
		}
	}
}

func TestVersionBoundAdoptionRefusesStaleOrMissingWitness(t *testing.T) {
	for _, witness := range []*int64{nil, new(int64), ptrVersion(1), ptrVersion(3)} {
		state := pathState{kind: pathLanded, version: 2, pending: true}
		if writable(true, adapter.Owned, true, witness, state) {
			t.Fatalf("stale/empty witness %v authorized version two", witness)
		}
	}
	if !writable(true, adapter.Owned, true, ptrVersion(2), pathState{kind: pathLanded, version: 2, pending: true}) {
		t.Fatal("fresh exact witness did not authorize a new CAS")
	}
	if writable(true, adapter.Owned, true, ptrVersion(2), pathState{kind: pathForeign, version: 2}) {
		t.Fatal("version witness captured another target's marker")
	}
}

func ptrVersion(version int64) *int64 { return &version }
