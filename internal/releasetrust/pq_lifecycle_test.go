package releasetrust_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/gofrs/flock"
)

// host is a disposable installation: the classical floor file the shipped
// installers write, the PQ floor beside it, and the same verification lock.
// It is test code that sequences only the exported verifiers; it is not a
// proposal for where a real host stores the PQ floor (NOT BUILT, E-17).
type host struct {
	dir   string
	crash string // "", "before-persist" or "between-floors"
}

var errCrash = errors.New("simulated crash")

func (h host) classical() string { return filepath.Join(h.dir, "release-trust.json") }
func (h host) pqPath() string    { return releasetrust.PQFloorPath(h.classical()) }

func (h host) read(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return raw
}

func atomicWrite(path string, raw []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".floor-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// apply is one update attempt: authenticate everything first, persist last.
// requirePQ=false is the frozen legacy installer behavior.
func (h host) apply(t *testing.T, fx *testfixture.PQFixture, requirePQ bool) error {
	t.Helper()
	lock := flock.New(releasetrust.VerificationLockPath(h.classical()))
	if ok, err := lock.TryLock(); err != nil || !ok {
		t.Fatalf("lock: %v %v", ok, err)
	}
	defer lock.Unlock()
	var classical releasetrust.SnapshotFloor
	if raw := h.read(t, h.classical()); raw != nil {
		var err error
		if classical, err = releasetrust.DecodeVerificationFloor(raw, releasetrust.SnapshotFloor{}); err != nil {
			return err
		}
	}
	snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, classical)
	if err != nil {
		return err
	}
	release, err := releasetrust.VerifyStable(snapshot, fx.Stable)
	if err != nil {
		return err
	}
	var pending []byte
	if requirePQ {
		var pqFloor releasetrust.PQFloor
		if raw := h.read(t, h.pqPath()); raw != nil {
			if pqFloor, err = releasetrust.DecodePQFloor(raw); err != nil {
				return err
			}
		}
		// An authenticated keyset or snapshot is remembered even when a later stage
		// refuses this update, so an older keyset cannot be replayed to revive a key.
		remember := func(floor releasetrust.PQFloor) error {
			if floor == (releasetrust.PQFloor{}) {
				return nil
			}
			remembered, encodeErr := releasetrust.EncodePQFloor(floor)
			if encodeErr != nil {
				return nil
			}
			return atomicWrite(h.pqPath(), remembered)
		}
		keyset, next, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, pqFloor)
		if err != nil {
			if writeErr := remember(next); writeErr != nil {
				return writeErr
			}
			return err
		}
		if next, err = releasetrust.VerifyPQRelease(fx.Pin, keyset, snapshot, release, fx.Stable.Manifest, fx.ManifestSet, next); err != nil {
			if writeErr := remember(next); writeErr != nil {
				return writeErr
			}
			return err
		}
		if pending, err = releasetrust.EncodePQFloor(next); err != nil {
			return err
		}
	}
	if h.crash == "before-persist" {
		return errCrash
	}
	if pending != nil {
		if err := atomicWrite(h.pqPath(), pending); err != nil {
			return err
		}
	}
	if h.crash == "between-floors" {
		return errCrash
	}
	raw, err := json.Marshal(snapshot.Floor())
	if err != nil {
		return err
	}
	return atomicWrite(h.classical(), append(raw, '\n'))
}

func (h host) snapshotState(t *testing.T) [2][]byte {
	return [2][]byte{h.read(t, h.classical()), h.read(t, h.pqPath())}
}

func TestLifecycleLegacyBridgePQAndSubsequentUpdate(t *testing.T) {
	h := host{dir: t.TempDir()}
	fx := testfixture.NewPQStableWorkflow(t)

	// 1. Frozen legacy: a v1-only update persists the classical floor and no PQ claim exists.
	if err := h.apply(t, fx, false); err != nil {
		t.Fatal(err)
	}
	if h.read(t, h.pqPath()) != nil {
		t.Fatal("legacy flow created PQ state")
	}
	if err := (releasetrust.PQFloor{}).AuthenticityClaim(fx.Pin); err == nil {
		t.Fatal("legacy host holds a PQ claim")
	}
	legacy := h.snapshotState(t)

	// 2. Bridge: the host now holds a pin delivered by a classically authenticated
	// binary (the E-11 window). The first PQ verification persists the claim.
	if err := h.apply(t, fx, true); err != nil {
		t.Fatal(err)
	}
	floor, err := releasetrust.DecodePQFloor(h.read(t, h.pqPath()))
	if err != nil || floor.AuthenticityClaim(fx.Pin) != nil {
		t.Fatalf("claim after bridge: %v", err)
	}
	if !bytes.Equal(legacy[0], h.read(t, h.classical())) {
		t.Fatal("PQ verification rewrote the unchanged classical floor")
	}

	// 3. Subsequent update at a higher sequence advances both floors.
	fx.Reissue(t, 3)
	if err := h.apply(t, fx, true); err != nil {
		t.Fatal(err)
	}
	floor, _ = releasetrust.DecodePQFloor(h.read(t, h.pqPath()))
	if floor.MetadataSequence != 3 || floor.CatalogSequence != 3 {
		t.Fatalf("floor %+v", floor)
	}
}

func TestLifecycleInterruptedUpdateConvergesAndNeverResets(t *testing.T) {
	for _, crash := range []string{"before-persist", "between-floors"} {
		t.Run(crash, func(t *testing.T) {
			h := host{dir: t.TempDir()}
			fx := testfixture.NewPQStableWorkflow(t)
			if err := h.apply(t, fx, true); err != nil {
				t.Fatal(err)
			}
			before := h.snapshotState(t)
			fx.Reissue(t, 3)
			interrupted := host{dir: h.dir, crash: crash}
			if err := interrupted.apply(t, fx, true); !errors.Is(err, errCrash) {
				t.Fatalf("got %v", err)
			}
			after := h.snapshotState(t)
			if crash == "before-persist" && (!bytes.Equal(before[0], after[0]) || !bytes.Equal(before[1], after[1])) {
				t.Fatal("an update that never persisted changed state")
			}
			if crash == "between-floors" && (!bytes.Equal(before[0], after[0]) || bytes.Equal(before[1], after[1])) {
				t.Fatal("expected only the PQ floor to have advanced")
			}
			// Retrying the same update converges: no floor ever moves backwards.
			if err := h.apply(t, fx, true); err != nil {
				t.Fatal(err)
			}
			pq, _ := releasetrust.DecodePQFloor(h.read(t, h.pqPath()))
			if pq.MetadataSequence != 3 {
				t.Fatalf("floor %+v", pq)
			}
		})
	}
	// This proves a half-written file is refused, not that every damaged or
	// missing file is: a missing file is a zero floor by design and a restored
	// or hand-edited file can state anything (E-17).
	t.Run("a torn PQ floor refuses and is never silently reset", func(t *testing.T) {
		h := host{dir: t.TempDir()}
		fx := testfixture.NewPQStableWorkflow(t)
		if err := h.apply(t, fx, true); err != nil {
			t.Fatal(err)
		}
		good := h.read(t, h.pqPath())
		if err := os.WriteFile(h.pqPath(), good[:len(good)/2], 0o600); err != nil {
			t.Fatal(err)
		}
		torn := h.snapshotState(t)
		if err := h.apply(t, fx, true); err == nil {
			t.Fatal("a torn PQ floor was treated as no floor")
		}
		if after := h.snapshotState(t); !bytes.Equal(torn[0], after[0]) || !bytes.Equal(torn[1], after[1]) {
			t.Fatal("refusal changed state")
		}
	})
}

func TestLifecycleFailuresLeaveOnlyTheExpectedFloor(t *testing.T) {
	for _, c := range []struct {
		name string
		want error
		// release is true when the refusal comes after the snapshot stage, which
		// authenticated the new metadata and catalog and so advances the PQ floor.
		release bool
		edit    func(t *testing.T, fx *testfixture.PQFixture)
	}{
		{"bad ML-DSA signature on metadata", releasetrust.ErrPQSignature, false, func(t *testing.T, fx *testfixture.PQFixture) {
			var set releasetrust.PQSignatureSet
			if err := json.Unmarshal(fx.Sets.MetadataSet, &set); err != nil {
				t.Fatal(err)
			}
			sig, _ := base64.StdEncoding.DecodeString(set.Signatures[0].Signature)
			sig[17] ^= 1
			set.Signatures[0].Signature = base64.StdEncoding.EncodeToString(sig)
			fx.Sets.MetadataSet = testfixture.JSON(t, set)
		}},
		{"stripped PQ sidecar", releasetrust.ErrPQMissing, false, func(t *testing.T, fx *testfixture.PQFixture) { fx.Sets.CatalogSet = nil }},
		{"stripped manifest sidecar", releasetrust.ErrPQMissing, true, func(t *testing.T, fx *testfixture.PQFixture) { fx.ManifestSet = nil }},
		{"random signature bytes", releasetrust.ErrPQSignature, true, func(t *testing.T, fx *testfixture.PQFixture) {
			var set releasetrust.PQSignatureSet
			if err := json.Unmarshal(fx.ManifestSet, &set); err != nil {
				t.Fatal(err)
			}
			junk := make([]byte, 3309)
			_, _ = rand.Read(junk)
			set.Signatures[0].Signature = base64.StdEncoding.EncodeToString(junk)
			fx.ManifestSet = testfixture.JSON(t, set)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := host{dir: t.TempDir()}
			fx := testfixture.NewPQStableWorkflow(t)
			if err := h.apply(t, fx, true); err != nil {
				t.Fatal(err)
			}
			before := h.snapshotState(t)
			fx.Reissue(t, 3)
			c.edit(t, fx)
			err := h.apply(t, fx, true)
			if err == nil {
				t.Fatal("accepted a failing update")
			}
			class(t, err, c.want)
			after := h.snapshotState(t)
			if !bytes.Equal(before[0], after[0]) {
				t.Fatal("a refused update changed the classical floor")
			}
			if !c.release {
				if !bytes.Equal(before[1], after[1]) {
					t.Fatal("a refused update changed persisted state")
				}
				return
			}
			was, _ := releasetrust.DecodePQFloor(before[1])
			now, err := releasetrust.DecodePQFloor(after[1])
			if err != nil || now.MetadataSequence != 3 || now.ReleaseSequence != was.ReleaseSequence || now.KeysetSequence != was.KeysetSequence {
				t.Fatalf("a release-stage refusal must keep exactly the snapshot-stage floor: %+v %v", now, err)
			}
		})
	}
	t.Run("rollback to an older authenticated snapshot", func(t *testing.T) {
		h := host{dir: t.TempDir()}
		fx2 := testfixture.NewPQStableWorkflow(t)
		fx2.Reissue(t, 3)
		if err := h.apply(t, fx2, true); err != nil {
			t.Fatal(err)
		}
		// The same host, served the older (also validly signed) sequence 2 evidence.
		fx2.Reissue(t, 2)
		before := h.snapshotState(t)
		err := h.apply(t, fx2, true)
		if err == nil {
			t.Fatal("rolled back to a lower sequence")
		}
		// The unchanged classical floor refuses first; the PQ floor refuses the same
		// rollback on its own when the classical state is gone.
		if err := os.Remove(h.classical()); err != nil {
			t.Fatal(err)
		}
		err = h.apply(t, fx2, true)
		class(t, err, releasetrust.ErrPQRollback)
		if err := atomicWrite(h.classical(), before[0]); err != nil {
			t.Fatal(err)
		}
		if after := h.snapshotState(t); !bytes.Equal(before[0], after[0]) || !bytes.Equal(before[1], after[1]) {
			t.Fatal("rollback attempt changed state")
		}
		// And the classical-only installer path is refused by the classical floor alone.
		if err := h.apply(t, fx2, false); err == nil {
			t.Fatal("classical floor allowed a rollback")
		}
	})
}

func TestLifecycleSignerRotationRevocationAndOfflineRecovery(t *testing.T) {
	h := host{dir: t.TempDir()}
	fx := testfixture.NewPQStableWorkflow(t)
	if err := h.apply(t, fx, true); err != nil {
		t.Fatal(err)
	}
	firstKeyset, firstKeysetSet := fx.Sets.Keyset, fx.Sets.KeysetSet
	t.Run("release signer rotation", func(t *testing.T) {
		fx.ReplaceReleaseKey(t)
		fx.Reissue(t, 3)
		if err := h.apply(t, fx, true); err != nil {
			t.Fatal(err)
		}
		pq, _ := releasetrust.DecodePQFloor(h.read(t, h.pqPath()))
		if pq.KeysetSequence != 2 {
			t.Fatalf("floor %+v", pq)
		}
	})
	t.Run("the older keyset cannot be replayed to revive the replaced key", func(t *testing.T) {
		stale := *fx
		stale.Sets.Keyset, stale.Sets.KeysetSet = firstKeyset, firstKeysetSet // validly signed, now superseded
		before := h.snapshotState(t)
		err := h.apply(t, &stale, true)
		if err == nil {
			t.Fatal("older evidence accepted")
		}
		class(t, err, releasetrust.ErrPQRollback)
		if after := h.snapshotState(t); !bytes.Equal(before[1], after[1]) {
			t.Fatal("state changed")
		}
	})
	t.Run("revocation stops all PQ verification, is remembered, and cannot be replayed away", func(t *testing.T) {
		notRevoked, notRevokedSet := fx.Sets.Keyset, fx.Sets.KeysetSet // authentic keyset 2, key still valid
		fx.Keyset.Sequence = 3
		fx.Keyset.Keys[0].Revoked = true
		fx.Reissue(t, 4)
		before := h.snapshotState(t)
		err := h.apply(t, fx, true)
		if err == nil {
			t.Fatal("a revoked release key still verified")
		}
		class(t, err, releasetrust.ErrPQScope)
		after := h.snapshotState(t)
		if !bytes.Equal(before[0], after[0]) {
			t.Fatal("a refused update changed the classical floor")
		}
		remembered, err := releasetrust.DecodePQFloor(after[1])
		if err != nil || remembered.KeysetSequence != 3 {
			t.Fatalf("the refusing keyset was not remembered: %+v %v", remembered, err)
		}
		// Replay: the older keyset (key valid) with fresh metadata that the revoked key signed.
		replay := *fx
		replay.Sets.Keyset, replay.Sets.KeysetSet = notRevoked, notRevokedSet
		again := h.snapshotState(t)
		err = h.apply(t, &replay, true)
		class(t, err, releasetrust.ErrPQRollback)
		if now := h.snapshotState(t); !bytes.Equal(again[0], now[0]) || !bytes.Equal(again[1], now[1]) {
			t.Fatal("the replay changed state")
		}
		// The legacy installer keeps working: PQ is not required on any shipping path.
		if err := h.apply(t, fx, false); err != nil {
			t.Fatalf("classical-only path regressed: %v", err)
		}
	})
	t.Run("offline recovery of a lost ML-DSA recovery key", func(t *testing.T) {
		// No in-band transition exists: the operator installs a new root from an
		// independently verified source (generation 2). This is E-11, not proof.
		oldPin := fx.Pin
		fx.ReplaceReleaseKey(t) // a revoked key is replaced, never un-revoked
		fx.ReplaceRecovery(t)
		fx.Reissue(t, 5)
		if err := h.apply(t, fx, true); err != nil {
			t.Fatal(err)
		}
		pq, _ := releasetrust.DecodePQFloor(h.read(t, h.pqPath()))
		if pq.Generation != 2 || pq.AuthenticityClaim(fx.Pin) != nil {
			t.Fatalf("floor %+v", pq)
		}
		// The retired pin can no longer verify: rollback to a verifier below the generation floor.
		old := *fx
		old.Pin = oldPin
		if err := h.apply(t, &old, true); err == nil {
			t.Fatal("generation rollback accepted")
		}
	})
}

func TestLifecycleReleaseRefusalRemembersAuthenticatedKeyset(t *testing.T) {
	h := host{dir: t.TempDir()}
	fx := testfixture.NewPQStableWorkflow(t)
	if err := h.apply(t, fx, true); err != nil {
		t.Fatal(err)
	}
	// Keyset 1 with key A signing at sequence 5, as an attacker holding A would offer it.
	fx.Reissue(t, 5)
	old := *fx
	// Keyset 2 drops key A. Metadata and catalog verify under key B, but the
	// manifest set is stripped, so the release stage refuses after the keyset
	// and snapshot stages have authenticated keyset 2.
	fx.ReplaceReleaseKey(t)
	fx.Reissue(t, 3)
	fx.ManifestSet = nil
	err := h.apply(t, fx, true)
	class(t, err, releasetrust.ErrPQMissing)
	remembered, err := releasetrust.DecodePQFloor(h.read(t, h.pqPath()))
	if err != nil || remembered.KeysetSequence != 2 {
		t.Fatalf("the refusing keyset was not remembered: %+v %v", remembered, err)
	}
	before := h.snapshotState(t)
	class(t, h.apply(t, &old, true), releasetrust.ErrPQRollback)
	if after := h.snapshotState(t); !bytes.Equal(before[0], after[0]) || !bytes.Equal(before[1], after[1]) {
		t.Fatal("the replay changed state")
	}
}

// A snapshot that skipped the PQ snapshot stage is refused by the release stage.
func TestPQReleaseRequiresTheSnapshotStageFloor(t *testing.T) {
	fx := testfixture.NewPQStableWorkflow(t)
	h := host{dir: t.TempDir()}
	if err := h.apply(t, fx, true); err != nil {
		t.Fatal(err)
	}
	floor, err := releasetrust.DecodePQFloor(h.read(t, h.pqPath()))
	if err != nil {
		t.Fatal(err)
	}
	keyset, _, err := releasetrust.VerifyPQKeyset(fx.Pin, floor, fx.Sets.Keyset, fx.Sets.KeysetSet, fx.KeyFiles)
	if err != nil {
		t.Fatal(err)
	}
	// A newer snapshot that only passed the classical stage.
	fx.Reissue(t, 4)
	snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	release, err := releasetrust.VerifyStable(snapshot, fx.Stable)
	if err != nil {
		t.Fatal(err)
	}
	next, err := releasetrust.VerifyPQRelease(fx.Pin, keyset, snapshot, release, fx.Stable.Manifest, fx.ManifestSet, floor)
	class(t, err, releasetrust.ErrPQScope)
	if next != floor {
		t.Fatal("a refusal must return the input floor")
	}
}
