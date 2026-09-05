package releasetrust_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

func TestHistoricalStableIsSeparateFromLatestAndCurrentRevocation(t *testing.T) {
	f := testfixture.New(t)
	a := f.AddStable(t, "1.0.0", 1, strings.Repeat("a", 40), []byte("first declaration"))
	b := f.AddStable(t, "1.1.0", 2, strings.Repeat("b", 40), []byte("second declaration"))
	snapshot := f.Snapshot(t)
	old, err := releasetrust.VerifyStable(snapshot, a.Material)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := releasetrust.VerifyStable(snapshot, b.Material)
	if err != nil {
		t.Fatal(err)
	}
	if releasetrust.RequireLatestStable(snapshot, old) == nil {
		t.Fatal("historical verifier relaxed installer latest policy")
	}
	if err := releasetrust.RequireLatestStable(snapshot, latest); err != nil {
		t.Fatal(err)
	}
	for name, payload := range a.Payloads {
		if err := old.VerifyArtifact(name, bytes.NewReader(payload)); err != nil {
			t.Fatal(err)
		}
		if old.VerifyArtifact(name, strings.NewReader("tampered")) == nil {
			t.Fatal("artifact substitution accepted")
		}
	}
	inventory := old.Artifacts()
	inventory[0].SHA256 = "mutated"
	if old.Artifacts()[0].SHA256 == "mutated" {
		t.Fatal("mutable verified inventory")
	}
	f.Metadata.PrimaryKeys[0].Revoked = true
	f.Metadata.Sequence++
	f.Catalog.Sequence++
	revoked := f.Snapshot(t)
	if _, err := releasetrust.VerifyStable(revoked, a.Material); err == nil {
		t.Fatal("current revocation ignored")
	}
	if releasetrust.RequireLatestStable(revoked, latest) == nil {
		t.Fatal("stale cached authority accepted")
	}
}

func TestSnapshotRejectsRollbackEquivocationAndMutatedEvidence(t *testing.T) {
	f := testfixture.New(t)
	f.AddStable(t, "1.0.0", 1, strings.Repeat("a", 40), []byte("declaration"))
	firstMaterial := f.Material(t)
	first := f.Snapshot(t)
	f.Metadata.Sequence++
	f.Catalog.Sequence++
	second, err := releasetrust.VerifySnapshot(f.Pinned, f.Material(t), first.Floor())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := releasetrust.VerifySnapshot(f.Pinned, firstMaterial, second.Floor()); err == nil {
		t.Fatal("metadata rollback accepted")
	}
	f.Metadata.Event.Type = "rotation"
	if _, err := releasetrust.VerifySnapshot(f.Pinned, f.Material(t), second.Floor()); err == nil {
		t.Fatal("same-sequence equivocation accepted")
	}
	for _, name := range []string{"metadata", "catalog", "key", "invalid floor", "negative highest"} {
		t.Run(name, func(t *testing.T) {
			material := f.Material(t)
			floor := releasetrust.SnapshotFloor{}
			switch name {
			case "metadata":
				material.Metadata = append(material.Metadata, ' ')
			case "catalog":
				material.Catalog = append(material.Catalog, ' ')
			case "key":
				material.PrimaryKeys["test-primary"] = append(material.PrimaryKeys["test-primary"], ' ')
			case "invalid floor":
				floor.MetadataSequence = 1
			case "negative highest":
				floor.HighestReleaseSequence = -1
			}
			if _, err := releasetrust.VerifySnapshot(f.Pinned, material, floor); err == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
	material := f.Material(t)
	snapshot, err := releasetrust.VerifySnapshot(f.Pinned, material, releasetrust.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	identity := snapshot.Digest()
	material.PrimaryKeys["test-primary"][0] = 'X'
	material.Metadata[0] = 'X'
	material.Catalog[0] = 'X'
	f.Pinned.RecoveryPublicKey[0] = 'X'
	if snapshot.Digest() != identity || !snapshot.Valid() {
		t.Fatal("mutable snapshot")
	}
}

func TestCanonicalOperatorKeyID(t *testing.T) {
	f := testfixture.New(t)
	a, err := releasetrust.OperatorKeyID(f.PrimaryPublic)
	if err != nil {
		t.Fatal(err)
	}
	b, err := releasetrust.OperatorKeyID(append(bytes.Clone(f.PrimaryPublic), '\n'))
	if err != nil {
		t.Fatal(err)
	}
	if a != b || a == releaseidentity.Hash(f.PrimaryPublic) {
		t.Fatal("operator pin uses PEM packaging instead of canonical DER")
	}
}

func TestSignedReleaseCandidatesKeepStableTrustProfile(t *testing.T) {
	f := testfixture.New(t)
	raw := f.AddStable(t, "1.0.0-rc.1", 1, strings.Repeat("a", 40), []byte("release candidate declaration"))
	verified, err := releasetrust.VerifyStable(f.Snapshot(t), raw.Material)
	if err != nil || verified.Identity().Profile != releaseidentity.StableV1 {
		t.Fatal("offline-signed RC lost stable trust profile", err)
	}
}
