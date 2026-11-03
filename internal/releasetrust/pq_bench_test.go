package releasetrust_test

import (
	"bytes"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

// These are measurements for the owner's DEC-17 baseline, not thresholds.

func BenchmarkPQVerifyKeyset(b *testing.B) {
	fx := testfixture.NewPQStableWorkflow(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := releasetrust.VerifyPQKeyset(fx.Pin, releasetrust.PQFloor{}, fx.Sets.Keyset, fx.Sets.KeysetSet, fx.KeyFiles); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLegacyVerifySnapshot(b *testing.B) {
	fx := testfixture.NewPQStableWorkflow(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPQVerifySnapshot(b *testing.B) {
	fx := testfixture.NewPQStableWorkflow(b)
	snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, releasetrust.PQFloor{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPQVerifyRelease(b *testing.B) {
	fx := testfixture.NewPQStableWorkflow(b)
	snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
	if err != nil {
		b.Fatal(err)
	}
	keyset, floor, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, releasetrust.PQFloor{})
	if err != nil {
		b.Fatal(err)
	}
	release, err := releasetrust.VerifyStable(snapshot, fx.Stable)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := releasetrust.VerifyPQRelease(fx.Pin, keyset, snapshot, release, fx.Stable.Manifest, fx.ManifestSet, floor); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPQVerifyArtifactSignature(b *testing.B) {
	fx := testfixture.NewPQStableWorkflow(b)
	snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
	if err != nil {
		b.Fatal(err)
	}
	keyset, _, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, releasetrust.PQFloor{})
	if err != nil {
		b.Fatal(err)
	}
	release, err := releasetrust.VerifyStable(snapshot, fx.Stable)
	if err != nil {
		b.Fatal(err)
	}
	payload := []byte("synthetic platform payload for 1.0.0")
	set := fx.ArtifactSet(b, "hikyo_linux_arm64.tar.gz", payload)
	b.ReportAllocs()
	for b.Loop() {
		if err := releasetrust.VerifyPQArtifact(fx.Pin, keyset, release, "hikyo_linux_arm64.tar.gz", bytes.NewReader(payload), set); err != nil {
			b.Fatal(err)
		}
	}
}

// TestPQSizesAreRecorded pins the frozen size facts the baseline cites.
func TestPQSizesAreRecorded(t *testing.T) {
	fx := testfixture.NewPQStableWorkflow(t)
	t.Logf("root v2 %d B; keyset %d B; keyset set %d B; metadata set %d B; catalog set %d B; manifest set %d B; policy set %d B",
		len(fx.RootV2), len(fx.Sets.Keyset), len(fx.Sets.KeysetSet), len(fx.Sets.MetadataSet), len(fx.Sets.CatalogSet), len(fx.ManifestSet), len(fx.Sets.StablePolicySet))
	t.Logf("ML-DSA-65 public key PEM %d B; recovery P-256 PEM %d B; legacy metadata %d B; legacy catalog %d B; manifest %d B",
		len(fx.KeyFiles["recovery-mldsa-1.pub"]), len(fx.Pinned.RecoveryPublicKey), len(fx.Material.Metadata), len(fx.Material.Catalog), len(fx.Stable.Manifest))
	if len(fx.ManifestSet) < 3309*4/3 || len(fx.ManifestSet) > 8192 {
		t.Fatalf("manifest set size %d outside the expected envelope", len(fx.ManifestSet))
	}
}
