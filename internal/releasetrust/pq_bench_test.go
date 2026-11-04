package releasetrust_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
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

// TestPQSizesArePinned pins the size facts the baseline cites. Everything that
// does not depend on an ECDSA signature is exact: an ML-DSA signature is always
// 3,309 B and the fixture's names and digests have fixed lengths. An ECDSA P-256
// ASN.1 signature is 8 to 72 B (observed mostly 70, sometimes 69), so the
// recovery-role set varies with it; the test checks the whole DER range instead
// of the values it happened to see.
func TestPQSizesArePinned(t *testing.T) {
	fx := testfixture.NewPQStableWorkflow(t)
	for name, c := range map[string]struct{ got, want int }{
		"release-role signature set": {len(fx.ManifestSet), 4592},
		"ML-DSA-65 public key PEM":   {len(fx.KeyFiles["recovery-mldsa-1.pub"]), 2726},
		"P-256 public key PEM":       {len(fx.Pinned.RecoveryPublicKey), 178},
	} {
		if c.got != c.want {
			t.Errorf("%s: %d bytes, want %d", name, c.got, c.want)
		}
	}
	var set releasetrust.PQSignatureSet
	if err := json.Unmarshal(fx.Sets.KeysetSet, &set); err != nil {
		t.Fatal(err)
	}
	var der int
	for _, signature := range set.Signatures {
		raw, err := base64.StdEncoding.DecodeString(signature.Signature)
		if err != nil {
			t.Fatal(err)
		}
		switch signature.Algorithm {
		case releasetrust.AlgorithmMLDSA65:
			if len(raw) != 3309 {
				t.Errorf("ML-DSA-65 signature is %d bytes", len(raw))
			}
		case releasetrust.AlgorithmECDSAP256:
			der = len(raw)
		}
	}
	if der < 8 || der > 72 {
		t.Fatalf("ECDSA DER signature of %d bytes is outside 8 to 72", der)
	}
	// 4,735 B is the recovery-role set without its ECDSA signature text.
	if want := 4735 + 4*((der+2)/3); len(fx.Sets.KeysetSet) != want {
		t.Errorf("recovery-role set is %d bytes, want %d for a %d-byte DER signature", len(fx.Sets.KeysetSet), want, der)
	}
	t.Logf("root v2 %d B; keyset %d B; recovery-role set %d B (ECDSA DER %d B); metadata set %d B; policy set %d B; legacy metadata %d B; manifest %d B",
		len(fx.RootV2), len(fx.Sets.Keyset), len(fx.Sets.KeysetSet), der, len(fx.Sets.MetadataSet), len(fx.Sets.StablePolicySet), len(fx.Material.Metadata), len(fx.Stable.Manifest))
}
