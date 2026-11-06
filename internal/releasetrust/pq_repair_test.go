package releasetrust_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

// A VerifiedPQKeyset belongs to the pin it verified under. Ordering: verify the
// keyset under generation 1, replace the recovery key, re-sign the SAME keyset
// bytes at generation 2 (so the floor's sequence and digest match), then offer
// the stale generation-1 object to the release stage.
func TestStaleKeysetObjectIsRefusedUnderANewGeneration(t *testing.T) {
	fx := testfixture.NewPQStableWorkflow(t)
	snapshot, stale, first, release := verified(t, fx)
	fx.ReplaceRecovery(t)
	fx.Keyset.Sequence = 1
	fx.Resign(t)
	fresh, floor, err := releasetrust.VerifyPQKeyset(fx.Pin, first, fx.Sets.Keyset, fx.Sets.KeysetSet, fx.KeyFiles)
	if err != nil {
		t.Fatal(err)
	}
	if floor.KeysetSequence != first.KeysetSequence || floor.KeysetSHA256 != first.KeysetSHA256 || floor.KeysetPinSHA256 != fx.Pin.Digest() {
		t.Fatalf("the ordering no longer reproduces the same keyset bytes under a new pin: %+v", floor)
	}
	_, err = releasetrust.VerifyPQRelease(fx.Pin, stale, snapshot, release, fx.Stable.Manifest, fx.ManifestSet, floor)
	class(t, err, releasetrust.ErrPQScope)
	payload := []byte("synthetic platform payload for 1.0.0")
	err = releasetrust.VerifyPQArtifact(fx.Pin, stale, release, "hikyo_linux_arm64.tar.gz", bytes.NewReader(payload), fx.ArtifactSet(t, "hikyo_linux_arm64.tar.gz", payload))
	class(t, err, releasetrust.ErrPQScope)
	// The keyset verified under the current pin works.
	if _, err := releasetrust.VerifyPQRelease(fx.Pin, fresh, snapshot, release, fx.Stable.Manifest, fx.ManifestSet, floor); err != nil {
		t.Fatal(err)
	}
}

// The PQ call must receive the material the snapshot was verified with, because
// policy sets are valid across releases.
func TestPQSnapshotRequiresTheSnapshotsOwnPolicyMaterial(t *testing.T) {
	t.Run("stable policy bytes that the snapshot never authenticated", func(t *testing.T) {
		fx := testfixture.NewPQStableWorkflow(t)
		snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
		if err != nil {
			t.Fatal(err)
		}
		material := fx.Material
		material.StablePolicy = append(bytes.Clone(material.StablePolicy), ' ')
		sets := fx.Sets
		rec := fx.Recovery(t, releasetrust.PQKindStablePolicy)
		sets.StablePolicySet = fx.Set(t, rec, rec, fx.StablePolicyBinding(t), material.StablePolicy) // a valid recovery set over those bytes
		_, _, err = releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, material, sets, releasetrust.PQFloor{})
		class(t, err, releasetrust.ErrPQScope)
	})
	t.Run("a stable policy where the snapshot had none", func(t *testing.T) {
		fx := testfixture.NewPQLegacy(t)
		snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
		if err != nil {
			t.Fatal(err)
		}
		other := testfixture.NewPQStableWorkflow(t)
		material := fx.Material
		material.StablePolicy = other.Material.StablePolicy
		_, _, err = releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, material, fx.Sets, releasetrust.PQFloor{})
		class(t, err, releasetrust.ErrPQScope)
	})
	t.Run("a nightly policy missing from the catalog inventory", func(t *testing.T) {
		fx := testfixture.NewPQLegacy(t) // the snapshot saw no nightly policy
		snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
		if err != nil {
			t.Fatal(err)
		}
		donor := testfixture.NewPQLegacy(t)
		donor.AddNightlyPolicy(t)
		material := fx.Material
		material.NightlyPolicy = donor.Material.NightlyPolicy
		sets := fx.Sets
		rec := fx.Recovery(t, releasetrust.PQKindNightlyPolicy)
		sets.NightlyPolicySet = fx.Set(t, rec, rec, fx.NightlyPolicyBinding(), material.NightlyPolicy)
		_, _, err = releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, material, sets, releasetrust.PQFloor{})
		class(t, err, releasetrust.ErrPQScope)
	})
	t.Run("a nightly policy the snapshot saw but the PQ call omitted", func(t *testing.T) {
		fx := testfixture.NewPQLegacy(t)
		fx.AddNightlyPolicy(t)
		snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
		if err != nil {
			t.Fatal(err)
		}
		material := fx.Material
		material.NightlyPolicy = nil
		_, _, err = releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, material, fx.Sets, releasetrust.PQFloor{})
		class(t, err, releasetrust.ErrPQScope)
	})
}

// R-15: the record combines the newest keyset with stages verified under older
// ones, so the claim helper stays true after a revocation until a stage is
// verified again. It says the pin and stages were verified, not that today's
// release signer is still valid. This pins that behavior; it is a limit, not a
// guarantee.
func TestClaimHelperSurvivesARevocationUntilStagesAreVerifiedAgain(t *testing.T) {
	fx := testfixture.NewPQStableWorkflow(t)
	snapshot, keyset, floor, release := verified(t, fx)
	floor, err := releasetrust.VerifyPQRelease(fx.Pin, keyset, snapshot, release, fx.Stable.Manifest, fx.ManifestSet, floor)
	if err != nil || floor.AuthenticityClaim(fx.Pin) != nil {
		t.Fatalf("setup: %v", err)
	}
	fx.Keyset.Sequence = 2
	fx.Keyset.Keys[0].Revoked = true
	fx.Reissue(t, 4)
	_, afterKeyset, err := releasetrust.VerifyPQKeyset(fx.Pin, floor, fx.Sets.Keyset, fx.Sets.KeysetSet, fx.KeyFiles)
	if err != nil || afterKeyset.KeysetSequence != 2 {
		t.Fatalf("keyset-only path: %v", err)
	}
	if err := afterKeyset.AuthenticityClaim(fx.Pin); err != nil {
		t.Fatalf("documented residual changed: %v", err)
	}
	snapshot, err = releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	_, refused, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, floor)
	class(t, err, releasetrust.ErrPQScope)
	if err := refused.AuthenticityClaim(fx.Pin); err != nil {
		t.Fatalf("documented residual changed: %v", err)
	}
}

// A revoked entry still counts toward coverage, so a replacement over the same
// range is ambiguous until the revoked entry is removed.
func TestRevokedKeyMustBeRemovedNotJoinedByAReplacement(t *testing.T) {
	fx := testfixture.NewPQStableWorkflow(t)
	revoked := fx.Keyset.Keys[0]
	revoked.Revoked = true
	fx.ReplaceReleaseKey(t)
	replacement := fx.Keyset.Keys
	fx.Keyset.Keys = append([]releasetrust.PQReleaseKey{revoked}, replacement...)
	fx.Reissue(t, 3)
	snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, releasetrust.PQFloor{})
	class(t, err, releasetrust.ErrPQScope)
	if !strings.Contains(err.Error(), "ambiguously") {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
	fx.Keyset.Keys = replacement
	fx.Keyset.Sequence++
	fx.Reissue(t, 4)
	snapshot, err = releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, releasetrust.PQFloor{}); err != nil {
		t.Fatalf("removing the revoked entry did not restore verification: %v", err)
	}
}

func TestEncodePQFloorRefusesTheEmptyRecordDecodeRefuses(t *testing.T) {
	if _, err := releasetrust.EncodePQFloor(releasetrust.PQFloor{}); !errors.Is(err, releasetrust.ErrPQMalformed) {
		t.Fatalf("got %v", err)
	}
}

func TestArtifactReadErrorsStayInTheClosedSet(t *testing.T) {
	fx := testfixture.NewPQStableWorkflow(t)
	_, keyset, _, release := verified(t, fx)
	payload := []byte("synthetic platform payload for 1.0.0")
	set := fx.ArtifactSet(t, "hikyo_linux_arm64.tar.gz", payload)
	for name, reader := range map[string]interface {
		Read([]byte) (int, error)
	}{"failing reader": iotest.ErrReader(errors.New("disk fault"))} {
		err := releasetrust.VerifyPQArtifact(fx.Pin, keyset, release, "hikyo_linux_arm64.tar.gz", reader, set)
		if !errors.Is(err, releasetrust.ErrPQMalformed) {
			t.Fatalf("%s: got %v", name, err)
		}
	}
	err := releasetrust.VerifyPQArtifact(fx.Pin, keyset, release, "hikyo_linux_arm64.tar.gz", nil, set)
	class(t, err, releasetrust.ErrPQMalformed)
}
