package releasetrust_test

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

// A refusal caused by a revocation must be remembered. Without the returned
// floor the same replay is accepted, which is the defect this guards.
func TestRefusedRevocationIsRememberedAndKeysetReplayIsRefused(t *testing.T) {
	fx := testfixture.NewPQStableWorkflow(t)
	snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	_, first, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, releasetrust.PQFloor{})
	if err != nil {
		t.Fatal(err)
	}
	keyset1, set1 := fx.Sets.Keyset, fx.Sets.KeysetSet

	fx.Keyset.Sequence = 2
	fx.Keyset.Keys[0].Revoked = true
	fx.Reissue(t, 4)
	snapshot, err = releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	_, refused, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, first)
	class(t, err, releasetrust.ErrPQScope)
	if refused.KeysetSequence != 2 || first.Advance(refused) != nil {
		t.Fatalf("refusal did not return the advanced keyset floor: %+v", refused)
	}

	// Replay keyset 1 (key valid) with fresh metadata the revoked key signed.
	replay := fx.Sets
	replay.Keyset, replay.KeysetSet = keyset1, set1
	if _, _, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, replay, first); err != nil {
		t.Fatalf("control: without persisting the refusal the replay is accepted: %v", err)
	}
	_, _, err = releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, replay, refused)
	class(t, err, releasetrust.ErrPQRollback)

	// A failure before the keyset verified returns no floor.
	bad := fx.Sets
	bad.KeysetSet = nil
	_, none, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, bad, first)
	class(t, err, releasetrust.ErrPQMissing)
	if none != (releasetrust.PQFloor{}) {
		t.Fatal("a failure before the keyset verified returned a floor")
	}
}

// Stage records carry the pin they were made under, so a new generation cannot
// inherit a claim from the old one.
func TestAuthenticityClaimNeedsEveryStageUnderTheCurrentPin(t *testing.T) {
	fx := testfixture.NewPQStableWorkflow(t)
	snapshot, keyset, floor, release := verified(t, fx)
	floor, err := releasetrust.VerifyPQRelease(fx.Pin, keyset, snapshot, release, fx.Stable.Manifest, fx.ManifestSet, floor)
	if err != nil {
		t.Fatal(err)
	}
	if err := floor.AuthenticityClaim(fx.Pin); err != nil {
		t.Fatal(err)
	}
	oldPin := fx.Pin
	fx.ReplaceRecovery(t) // generation 2, keyset re-signed
	if fx.Pin.Generation() != 2 {
		t.Fatal("expected a new generation")
	}
	_, next, err := releasetrust.VerifyPQKeyset(fx.Pin, floor, fx.Sets.Keyset, fx.Sets.KeysetSet, fx.KeyFiles)
	if err != nil {
		t.Fatal(err)
	}
	if next.AuthenticityClaim(fx.Pin) == nil {
		t.Fatal("a claim followed a generation change after the keyset stage alone")
	}
	if floor.AuthenticityClaim(oldPin) != nil {
		t.Fatal("the old claim should still hold for the old pin")
	}
	// Every stage under the new pin restores it.
	snapshot, err = releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	keyset, next, err = releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, floor)
	if err != nil {
		t.Fatal(err)
	}
	if next.AuthenticityClaim(fx.Pin) == nil {
		t.Fatal("a claim without a release under the new pin")
	}
	release, err = releasetrust.VerifyStable(snapshot, fx.Stable)
	if err != nil {
		t.Fatal(err)
	}
	next, err = releasetrust.VerifyPQRelease(fx.Pin, keyset, snapshot, release, fx.Stable.Manifest, fx.ManifestSet, next)
	if err != nil || next.AuthenticityClaim(fx.Pin) != nil {
		t.Fatalf("claim under the new pin: %v", err)
	}
}

func TestDecodePQFloorRefusesAnEmptyRecord(t *testing.T) {
	for _, raw := range []string{`{}`, ``, ` `, `null`} {
		if _, err := releasetrust.DecodePQFloor([]byte(raw)); err == nil {
			t.Fatalf("decoded %q as a floor", raw)
		}
	}
}

// Recovery keys sign a policy once. A later release with unchanged policy bytes
// must verify under the old policy set, with metadata and catalog re-signed only
// by the release role.
func TestPolicySetsSurviveANewRelease(t *testing.T) {
	fx := testfixture.NewPQStableWorkflow(t)
	policySet := fx.Sets.StablePolicySet
	fx.SecondRelease(t)
	snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	sets := fx.Sets
	sets.StablePolicySet = policySet // signed before the second release existed
	if _, floor, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, sets, releasetrust.PQFloor{}); err != nil || floor.MetadataSequence != fx.Metadata.Sequence {
		t.Fatalf("a new release invalidated the recovery-signed policy set: %v", err)
	}
	// And the policy still cannot be signed by the release role.
	rel := fx.ReleaseMembers(t, releasetrust.PQKindStablePolicy)
	sets.StablePolicySet = fx.Set(t, rel, rel, fx.StablePolicyBinding(t), fx.Material.StablePolicy)
	_, _, err = releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, sets, releasetrust.PQFloor{})
	class(t, err, releasetrust.ErrPQMissing)
}

func TestNightlyPolicyIsRecoveryAuthorized(t *testing.T) {
	build := func(t *testing.T) (*testfixture.PQFixture, releasetrust.Snapshot) {
		fx := testfixture.NewPQLegacy(t)
		fx.AddNightlyPolicy(t)
		snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
		if err != nil {
			t.Fatal(err)
		}
		return fx, snapshot
	}
	t.Run("accepted", func(t *testing.T) {
		fx, snapshot := build(t)
		if _, _, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, releasetrust.PQFloor{}); err != nil {
			t.Fatal(err)
		}
	})
	for _, c := range []struct {
		name string
		want error
		set  func(t *testing.T, fx *testfixture.PQFixture) []byte
	}{
		{"stripped", releasetrust.ErrPQMissing, func(*testing.T, *testfixture.PQFixture) []byte { return nil }},
		{"release role", releasetrust.ErrPQMissing, func(t *testing.T, fx *testfixture.PQFixture) []byte {
			rel := fx.ReleaseMembers(t, releasetrust.PQKindNightlyPolicy)
			return fx.Set(t, rel, rel, fx.NightlyPolicyBinding(), fx.Material.NightlyPolicy)
		}},
		{"ML-DSA only", releasetrust.ErrPQMissing, func(t *testing.T, fx *testfixture.PQFixture) []byte {
			rec := fx.Recovery(t, releasetrust.PQKindNightlyPolicy)
			return fx.Set(t, rec, rec[1:], fx.NightlyPolicyBinding(), fx.Material.NightlyPolicy)
		}},
		{"other bytes", releasetrust.ErrPQSignature, func(t *testing.T, fx *testfixture.PQFixture) []byte {
			rec := fx.Recovery(t, releasetrust.PQKindNightlyPolicy)
			return fx.Set(t, rec, rec, fx.NightlyPolicyBinding(), []byte("other policy"))
		}},
		{"stable channel", releasetrust.ErrPQSignature, func(t *testing.T, fx *testfixture.PQFixture) []byte {
			rec := fx.Recovery(t, releasetrust.PQKindNightlyPolicy)
			b := fx.NightlyPolicyBinding()
			b.Channel = "stable/v1"
			return fx.Set(t, rec, rec, b, fx.Material.NightlyPolicy)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			fx, snapshot := build(t)
			sets := fx.Sets
			sets.NightlyPolicySet = c.set(t, fx)
			_, _, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, sets, releasetrust.PQFloor{})
			class(t, err, c.want)
		})
	}
}
