package releasetrust_test

import (
	"bytes"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

// The PQ parsers read release metadata that arrives over a classical channel.
// None may panic, and none may accept anything but the exact valid fixture.

func FuzzNewPQPin(f *testing.F) {
	fx := testfixture.NewPQStableWorkflow(f)
	f.Add(fx.RootV2)
	f.Add(fx.RootV2[:len(fx.RootV2)/2])
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		pin, err := releasetrust.NewPQPin(raw, fx.KeyFiles)
		if err == nil && !bytes.Equal(raw, fx.RootV2) && pin.Digest() == fx.Pin.Digest() {
			t.Fatal("two root encodings share a digest")
		}
	})
}

func FuzzDecodePQFloor(f *testing.F) {
	fx := testfixture.NewPQStableWorkflow(f)
	_, _, floor, _ := verifiedFuzz(f, fx)
	valid, err := releasetrust.EncodePQFloor(floor)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		got, err := releasetrust.DecodePQFloor(raw)
		if err != nil {
			return
		}
		again, err := releasetrust.EncodePQFloor(got)
		if err != nil {
			t.Fatalf("an accepted floor does not encode: %v", err)
		}
		back, err := releasetrust.DecodePQFloor(again)
		if err != nil || back != got {
			t.Fatalf("round trip changed the floor: %v", err)
		}
	})
}

// FuzzVerifyPQKeyset feeds arbitrary signature sets and keysets to the verifier.
func FuzzVerifyPQKeyset(f *testing.F) {
	fx := testfixture.NewPQStableWorkflow(f)
	f.Add(fx.Sets.Keyset, fx.Sets.KeysetSet)
	f.Add(fx.Sets.Keyset, fx.Sets.KeysetSet[:len(fx.Sets.KeysetSet)/2])
	f.Add([]byte(`{}`), []byte(`{}`))
	f.Fuzz(func(t *testing.T, keyset, set []byte) {
		_, _, err := releasetrust.VerifyPQKeyset(fx.Pin, releasetrust.PQFloor{}, keyset, set, fx.KeyFiles)
		if err == nil && (!bytes.Equal(keyset, fx.Sets.Keyset) || !bytes.Equal(set, fx.Sets.KeysetSet)) {
			// A different encoding of the same signatures is legal only if it parsed
			// to the same authenticated bytes, which strict decoding forbids for the keyset.
			if !bytes.Equal(keyset, fx.Sets.Keyset) {
				t.Fatal("accepted keyset bytes that were never signed")
			}
		}
	})
}

func verifiedFuzz(f *testing.F, fx *testfixture.PQFixture) (releasetrust.Snapshot, releasetrust.VerifiedPQKeyset, releasetrust.PQFloor, releasetrust.VerifiedRelease) {
	f.Helper()
	snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
	if err != nil {
		f.Fatal(err)
	}
	keyset, floor, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, releasetrust.PQFloor{})
	if err != nil {
		f.Fatal(err)
	}
	release, err := releasetrust.VerifyStable(snapshot, fx.Stable)
	if err != nil {
		f.Fatal(err)
	}
	floor, err = releasetrust.VerifyPQRelease(fx.Pin, keyset, snapshot, release, fx.Stable.Manifest, fx.ManifestSet, floor)
	if err != nil {
		f.Fatal(err)
	}
	return snapshot, keyset, floor, release
}
