package releasetrust_test

import (
	"bytes"
	"crypto/mldsa"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

// verified runs the unchanged v1 verification first, then the PQ requirement.
func verified(t *testing.T, fx *testfixture.PQFixture) (releasetrust.Snapshot, releasetrust.VerifiedPQKeyset, releasetrust.PQFloor, releasetrust.VerifiedRelease) {
	t.Helper()
	snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	keyset, floor, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, releasetrust.PQFloor{})
	if err != nil {
		t.Fatal(err)
	}
	release, err := releasetrust.VerifyStable(snapshot, fx.Stable)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, keyset, floor, release
}

func TestPQHappyPathPersistsAClaimOnlyAfterEveryStage(t *testing.T) {
	fx := testfixture.NewPQStableWorkflow(t)
	snapshot, keyset, floor, release := verified(t, fx)
	if err := floor.AuthenticityClaim(fx.Pin); err == nil {
		t.Fatal("claim made before a release was verified")
	}
	floor, err := releasetrust.VerifyPQRelease(fx.Pin, keyset, snapshot, release, fx.Stable.Manifest, fx.ManifestSet, floor)
	if err != nil {
		t.Fatal(err)
	}
	if err := floor.AuthenticityClaim(fx.Pin); err != nil {
		t.Fatal(err)
	}
	payload := []byte("synthetic platform payload for 1.0.0")
	if err := releasetrust.VerifyPQArtifact(fx.Pin, keyset, release, "hikyo_linux_arm64.tar.gz", bytes.NewReader(payload), fx.ArtifactSet(t, "hikyo_linux_arm64.tar.gz", payload)); err != nil {
		t.Fatal(err)
	}
	if err := (releasetrust.PQFloor{}).AuthenticityClaim(fx.Pin); err == nil {
		t.Fatal("empty floor supports a claim")
	}
}

func class(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want class %v", err, want)
	}
}

func TestPQSnapshotRefusesEveryTamperedAuthorityBoundary(t *testing.T) {
	type tamper struct {
		name string
		want error
		edit func(t *testing.T, fx *testfixture.PQFixture, sets *releasetrust.PQSnapshotSets)
	}
	recoverySet := func(t *testing.T, fx *testfixture.PQFixture, kind string, members func([]testfixture.PQMember) []testfixture.PQMember, binding releasetrust.PQBinding, payload []byte) []byte {
		all := fx.Recovery(t, kind)
		return fx.Set(t, all, members(all), binding, payload)
	}
	policy := func(fx *testfixture.PQFixture) releaseidentity.Digest {
		return releaseidentity.Hash(fx.Material.StablePolicy)
	}
	cases := []tamper{
		{"missing keyset set", releasetrust.ErrPQMissing, func(t *testing.T, fx *testfixture.PQFixture, s *releasetrust.PQSnapshotSets) { s.KeysetSet = nil }},
		{"stripped ML-DSA recovery signature", releasetrust.ErrPQMissing, func(t *testing.T, fx *testfixture.PQFixture, s *releasetrust.PQSnapshotSets) {
			s.KeysetSet = recoverySet(t, fx, releasetrust.PQKindKeyset, func(m []testfixture.PQMember) []testfixture.PQMember { return m[:1] }, releasetrust.PQBinding{Kind: releasetrust.PQKindKeyset, Channel: "stable/v1", Subject: "pq-release-keys.json", Sequence: 1, Policy: fx.Pin.Digest()}, s.Keyset)
		}},
		{"stripped classical recovery signature", releasetrust.ErrPQMissing, func(t *testing.T, fx *testfixture.PQFixture, s *releasetrust.PQSnapshotSets) {
			s.KeysetSet = recoverySet(t, fx, releasetrust.PQKindKeyset, func(m []testfixture.PQMember) []testfixture.PQMember { return m[1:] }, releasetrust.PQBinding{Kind: releasetrust.PQKindKeyset, Channel: "stable/v1", Subject: "pq-release-keys.json", Sequence: 1, Policy: fx.Pin.Digest()}, s.Keyset)
		}},
		{"suite downgraded to classical only", releasetrust.ErrPQSignature, func(t *testing.T, fx *testfixture.PQFixture, s *releasetrust.PQSnapshotSets) {
			all := fx.Recovery(t, releasetrust.PQKindKeyset)
			s.KeysetSet = fx.Set(t, all[:1], all, releasetrust.PQBinding{Kind: releasetrust.PQKindKeyset, Channel: "stable/v1", Subject: "pq-release-keys.json", Sequence: 1, Policy: fx.Pin.Digest()}, s.Keyset)
		}},
		{"release key signs the keyset", releasetrust.ErrPQMissing, func(t *testing.T, fx *testfixture.PQFixture, s *releasetrust.PQSnapshotSets) {
			rel := fx.ReleaseMembers(t, releasetrust.PQKindKeyset)
			s.KeysetSet = fx.Set(t, rel, rel, releasetrust.PQBinding{Kind: releasetrust.PQKindKeyset, Channel: "stable/v1", Subject: "pq-release-keys.json", Sequence: 1, Policy: fx.Pin.Digest()}, s.Keyset)
		}},
		{"recovery key signs the metadata in the release role", releasetrust.ErrPQScope, func(t *testing.T, fx *testfixture.PQFixture, s *releasetrust.PQSnapshotSets) {
			s.MetadataSet = recoverySet(t, fx, releasetrust.PQKindMetadata, func(m []testfixture.PQMember) []testfixture.PQMember { return m }, fx.TrustBinding(releasetrust.PQKindMetadata, "metadata.json", 2, policy(fx)), fx.Material.Metadata)
		}},
		{"keyset bytes altered after signing", releasetrust.ErrPQSignature, func(t *testing.T, fx *testfixture.PQFixture, s *releasetrust.PQSnapshotSets) {
			s.Keyset = append(bytes.TrimSpace(s.Keyset), ' ')
		}},
		{"metadata set signed for other bytes", releasetrust.ErrPQSignature, func(t *testing.T, fx *testfixture.PQFixture, s *releasetrust.PQSnapshotSets) {
			s.MetadataSet = fx.Set(t, fx.ReleaseMembers(t, releasetrust.PQKindMetadata), fx.ReleaseMembers(t, releasetrust.PQKindMetadata), fx.TrustBinding(releasetrust.PQKindMetadata, "metadata.json", 2, policy(fx)), []byte("other"))
		}},
		{"metadata set bound to another sequence", releasetrust.ErrPQSignature, func(t *testing.T, fx *testfixture.PQFixture, s *releasetrust.PQSnapshotSets) {
			s.MetadataSet = fx.Set(t, fx.ReleaseMembers(t, releasetrust.PQKindMetadata), fx.ReleaseMembers(t, releasetrust.PQKindMetadata), fx.TrustBinding(releasetrust.PQKindMetadata, "metadata.json", 3, policy(fx)), fx.Material.Metadata)
		}},
		{"metadata set bound to another policy", releasetrust.ErrPQSignature, func(t *testing.T, fx *testfixture.PQFixture, s *releasetrust.PQSnapshotSets) {
			s.MetadataSet = fx.Set(t, fx.ReleaseMembers(t, releasetrust.PQKindMetadata), fx.ReleaseMembers(t, releasetrust.PQKindMetadata), fx.TrustBinding(releasetrust.PQKindMetadata, "metadata.json", 2, releaseidentity.Hash([]byte("other"))), fx.Material.Metadata)
		}},
		{"catalog set replayed as metadata set", releasetrust.ErrPQSignature, func(t *testing.T, fx *testfixture.PQFixture, s *releasetrust.PQSnapshotSets) {
			s.MetadataSet = s.CatalogSet
		}},
		{"stable policy lacks recovery signatures", releasetrust.ErrPQMissing, func(t *testing.T, fx *testfixture.PQFixture, s *releasetrust.PQSnapshotSets) {
			rel := fx.ReleaseMembers(t, releasetrust.PQKindStablePolicy)
			s.StablePolicySet = fx.Set(t, rel, rel, fx.TrustBinding(releasetrust.PQKindStablePolicy, "stable-policy.json", 1, releaseidentity.Hash(fx.Pinned.Root)), fx.Material.StablePolicy)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fx := testfixture.NewPQStableWorkflow(t)
			snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
			if err != nil {
				t.Fatal(err)
			}
			sets := fx.Sets
			c.edit(t, fx, &sets)
			_, _, err = releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, sets, releasetrust.PQFloor{})
			if err == nil {
				t.Fatal("accepted tampered PQ evidence")
			}
			class(t, err, c.want)
		})
	}
}

func TestPQSignatureSetEnvelopeIsClosed(t *testing.T) {
	good := func(fx *testfixture.PQFixture) releasetrust.PQSignatureSet {
		var set releasetrust.PQSignatureSet
		if err := json.Unmarshal(fx.Sets.KeysetSet, &set); err != nil {
			t.Fatal(err)
		}
		return set
	}
	for _, c := range []struct {
		name string
		want error
		edit func(releasetrust.PQSignatureSet) any
	}{
		{"unknown algorithm", releasetrust.ErrPQUnsupported, func(s releasetrust.PQSignatureSet) any { s.Signatures[0].Algorithm = "mldsa44"; return s }},
		{"ML-DSA-87 label", releasetrust.ErrPQUnsupported, func(s releasetrust.PQSignatureSet) any { s.Signatures[1].Algorithm = "mldsa87"; return s }},
		{"duplicate algorithm", releasetrust.ErrPQMalformed, func(s releasetrust.PQSignatureSet) any { s.Signatures[1] = s.Signatures[0]; return s }},
		{"unknown schema", releasetrust.ErrPQUnsupported, func(s releasetrust.PQSignatureSet) any { s.Schema = "hikyo.dev/pq-signature-set/v2"; return s }},
		{"extra member", releasetrust.ErrPQMalformed, func(s releasetrust.PQSignatureSet) any {
			return map[string]any{"schema": s.Schema, "signatures": s.Signatures, "extra": true}
		}},
		{"too many signatures", releasetrust.ErrPQBound, func(s releasetrust.PQSignatureSet) any {
			s.Signatures = append(s.Signatures, s.Signatures...)
			s.Signatures = append(s.Signatures, s.Signatures[0])
			return s
		}},
		{"substituted key id", releasetrust.ErrPQScope, func(s releasetrust.PQSignatureSet) any { s.Signatures[1].KeyID = s.Signatures[0].KeyID; return s }},
		{"bad base64", releasetrust.ErrPQMalformed, func(s releasetrust.PQSignatureSet) any { s.Signatures[1].Signature = "!!"; return s }},
		{"short ML-DSA signature", releasetrust.ErrPQBound, func(s releasetrust.PQSignatureSet) any { s.Signatures[1].Signature = "AAAA"; return s }},
	} {
		t.Run(c.name, func(t *testing.T) {
			fx := testfixture.NewPQStableWorkflow(t)
			snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
			if err != nil {
				t.Fatal(err)
			}
			sets := fx.Sets
			sets.KeysetSet = testfixture.JSON(t, c.edit(good(fx)))
			_, _, err = releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, sets, releasetrust.PQFloor{})
			class(t, err, c.want)
		})
	}
	t.Run("oversized set", func(t *testing.T) {
		fx := testfixture.NewPQStableWorkflow(t)
		snapshot, _ := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
		sets := fx.Sets
		sets.KeysetSet = bytes.Repeat([]byte(" "), 64<<10+1)
		_, _, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, sets, releasetrust.PQFloor{})
		class(t, err, releasetrust.ErrPQBound)
	})
}

func TestPQReleaseAndArtifactBindings(t *testing.T) {
	payload := []byte("synthetic platform payload for 1.0.0")
	name := "hikyo_linux_arm64.tar.gz"
	for _, c := range []struct {
		name string
		want error
		run  func(t *testing.T, fx *testfixture.PQFixture, snapshot releasetrust.Snapshot, keyset releasetrust.VerifiedPQKeyset, release releasetrust.VerifiedRelease, floor releasetrust.PQFloor) error
	}{
		{"manifest set stripped", releasetrust.ErrPQMissing, func(t *testing.T, fx *testfixture.PQFixture, s releasetrust.Snapshot, k releasetrust.VerifiedPQKeyset, r releasetrust.VerifiedRelease, f releasetrust.PQFloor) error {
			_, err := releasetrust.VerifyPQRelease(fx.Pin, k, s, r, fx.Stable.Manifest, nil, f)
			return err
		}},
		{"manifest signed by the recovery role", releasetrust.ErrPQScope, func(t *testing.T, fx *testfixture.PQFixture, s releasetrust.Snapshot, k releasetrust.VerifiedPQKeyset, r releasetrust.VerifiedRelease, f releasetrust.PQFloor) error {
			rec := fx.Recovery(t, releasetrust.PQKindReleaseManifest)
			set := fx.Set(t, rec, rec, fx.ReleaseBinding(releasetrust.PQKindReleaseManifest, "release-manifest.json", 1), fx.Stable.Manifest)
			_, err := releasetrust.VerifyPQRelease(fx.Pin, k, s, r, fx.Stable.Manifest, set, f)
			return err
		}},
		{"manifest bound to nightly channel", releasetrust.ErrPQSignature, func(t *testing.T, fx *testfixture.PQFixture, s releasetrust.Snapshot, k releasetrust.VerifiedPQKeyset, r releasetrust.VerifiedRelease, f releasetrust.PQFloor) error {
			b := fx.ReleaseBinding(releasetrust.PQKindReleaseManifest, "release-manifest.json", 1)
			b.Channel = string(releaseidentity.NightlyV1)
			rel := fx.ReleaseMembers(t, releasetrust.PQKindReleaseManifest)
			_, err := releasetrust.VerifyPQRelease(fx.Pin, k, s, r, fx.Stable.Manifest, fx.Set(t, rel, rel, b, fx.Stable.Manifest), f)
			return err
		}},
		{"manifest bound to another version", releasetrust.ErrPQSignature, func(t *testing.T, fx *testfixture.PQFixture, s releasetrust.Snapshot, k releasetrust.VerifiedPQKeyset, r releasetrust.VerifiedRelease, f releasetrust.PQFloor) error {
			b := fx.ReleaseBinding(releasetrust.PQKindReleaseManifest, "release-manifest.json", 1)
			b.Version = "1.0.1"
			rel := fx.ReleaseMembers(t, releasetrust.PQKindReleaseManifest)
			_, err := releasetrust.VerifyPQRelease(fx.Pin, k, s, r, fx.Stable.Manifest, fx.Set(t, rel, rel, b, fx.Stable.Manifest), f)
			return err
		}},
		{"manifest bound to another source", releasetrust.ErrPQSignature, func(t *testing.T, fx *testfixture.PQFixture, s releasetrust.Snapshot, k releasetrust.VerifiedPQKeyset, r releasetrust.VerifiedRelease, f releasetrust.PQFloor) error {
			b := fx.ReleaseBinding(releasetrust.PQKindReleaseManifest, "release-manifest.json", 1)
			b.Source = strings.Repeat("b", 40)
			rel := fx.ReleaseMembers(t, releasetrust.PQKindReleaseManifest)
			_, err := releasetrust.VerifyPQRelease(fx.Pin, k, s, r, fx.Stable.Manifest, fx.Set(t, rel, rel, b, fx.Stable.Manifest), f)
			return err
		}},
		{"manifest bytes substituted", releasetrust.ErrPQScope, func(t *testing.T, fx *testfixture.PQFixture, s releasetrust.Snapshot, k releasetrust.VerifiedPQKeyset, r releasetrust.VerifiedRelease, f releasetrust.PQFloor) error {
			_, err := releasetrust.VerifyPQRelease(fx.Pin, k, s, r, append(bytes.Clone(fx.Stable.Manifest), ' '), fx.ManifestSet, f)
			return err
		}},
		{"release sequence rolled back", releasetrust.ErrPQRollback, func(t *testing.T, fx *testfixture.PQFixture, s releasetrust.Snapshot, k releasetrust.VerifiedPQKeyset, r releasetrust.VerifiedRelease, f releasetrust.PQFloor) error {
			f.ReleaseSequence = 2
			_, err := releasetrust.VerifyPQRelease(fx.Pin, k, s, r, fx.Stable.Manifest, fx.ManifestSet, f)
			return err
		}},
		{"artifact bytes substituted", releasetrust.ErrPQScope, func(t *testing.T, fx *testfixture.PQFixture, s releasetrust.Snapshot, k releasetrust.VerifiedPQKeyset, r releasetrust.VerifiedRelease, f releasetrust.PQFloor) error {
			return releasetrust.VerifyPQArtifact(fx.Pin, k, r, name, strings.NewReader("substituted"), fx.ArtifactSet(t, name, payload))
		}},
		{"truncated download", releasetrust.ErrPQScope, func(t *testing.T, fx *testfixture.PQFixture, s releasetrust.Snapshot, k releasetrust.VerifiedPQKeyset, r releasetrust.VerifiedRelease, f releasetrust.PQFloor) error {
			return releasetrust.VerifyPQArtifact(fx.Pin, k, r, name, bytes.NewReader(payload[:len(payload)-1]), fx.ArtifactSet(t, name, payload))
		}},
		{"artifact set for another name", releasetrust.ErrPQSignature, func(t *testing.T, fx *testfixture.PQFixture, s releasetrust.Snapshot, k releasetrust.VerifiedPQKeyset, r releasetrust.VerifiedRelease, f releasetrust.PQFloor) error {
			return releasetrust.VerifyPQArtifact(fx.Pin, k, r, name, bytes.NewReader(payload), fx.ArtifactSet(t, "hikyo_linux_amd64.tar.gz", payload))
		}},
		{"artifact absent from inventory", releasetrust.ErrPQScope, func(t *testing.T, fx *testfixture.PQFixture, s releasetrust.Snapshot, k releasetrust.VerifiedPQKeyset, r releasetrust.VerifiedRelease, f releasetrust.PQFloor) error {
			return releasetrust.VerifyPQArtifact(fx.Pin, k, r, "extra.bin", bytes.NewReader(payload), fx.ArtifactSet(t, "extra.bin", payload))
		}},
		{"artifact signature stripped", releasetrust.ErrPQMissing, func(t *testing.T, fx *testfixture.PQFixture, s releasetrust.Snapshot, k releasetrust.VerifiedPQKeyset, r releasetrust.VerifiedRelease, f releasetrust.PQFloor) error {
			return releasetrust.VerifyPQArtifact(fx.Pin, k, r, name, bytes.NewReader(payload), nil)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			fx := testfixture.NewPQStableWorkflow(t)
			snapshot, keyset, floor, release := verified(t, fx)
			err := c.run(t, fx, snapshot, keyset, release, floor)
			if err == nil {
				t.Fatal("accepted tampered release evidence")
			}
			class(t, err, c.want)
		})
	}
}

func TestPQRevokedAndRotatedReleaseKeys(t *testing.T) {
	t.Run("revoked key cannot sign even for its old sequence", func(t *testing.T) {
		fx := testfixture.NewPQStableWorkflow(t)
		fx.Keyset.Sequence = 2
		fx.Keyset.Keys[0].Revoked = true
		fx.Resign(t)
		snapshot, _ := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
		keyset, floor, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, releasetrust.PQFloor{})
		if err == nil {
			// The keyset itself is valid; the metadata set was signed by the revoked key.
			t.Fatalf("revoked release key accepted: %v %v", keyset, floor)
		}
		class(t, err, releasetrust.ErrPQScope)
	})
	t.Run("two keys covering one sequence are ambiguous", func(t *testing.T) {
		fx := testfixture.NewPQStableWorkflow(t)
		fx.Keyset.Sequence = 2
		second, err := mldsa.GenerateKey(mldsa.MLDSA65())
		if err != nil {
			t.Fatal(err)
		}
		secondPEM := spkiPEM(t, second.PublicKey())
		extra := fx.Keyset.Keys[0]
		extra.ID, extra.PublicKey, extra.SHA256 = "release-mldsa-2", "release-mldsa-2.pub", string(releaseidentity.Hash(secondPEM))
		fx.KeyFiles["release-mldsa-2.pub"] = secondPEM
		fx.Keyset.Keys = append(fx.Keyset.Keys, extra)
		fx.Resign(t)
		snapshot, _ := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
		_, _, err = releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, releasetrust.PQFloor{})
		class(t, err, releasetrust.ErrPQScope)
		if !strings.Contains(err.Error(), "ambiguously") {
			t.Fatalf("refused for the wrong reason: %v", err)
		}
	})
	t.Run("keyset sequence rollback and equivocation", func(t *testing.T) {
		fx := testfixture.NewPQStableWorkflow(t)
		snapshot, _ := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
		_, floor, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, releasetrust.PQFloor{})
		if err != nil {
			t.Fatal(err)
		}
		rolled := floor
		rolled.KeysetSequence = 2
		_, _, err = releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, rolled)
		class(t, err, releasetrust.ErrPQRollback)
		equivocated := floor
		equivocated.KeysetSHA256 = releaseidentity.Hash([]byte("other keyset"))
		_, _, err = releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, equivocated)
		class(t, err, releasetrust.ErrPQRollback)
	})
}
