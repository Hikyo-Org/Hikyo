package selfupdate

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
)

// This proves two narrow things about the shipped stable installer's
// verifyStable: it ignores PQ sidecars published beside a release and leaves a
// sibling PQ floor untouched, and a build stamped with a v2 root refuses before
// writing state. The root is stamped at build time, so no shipped client is ever
// handed a v2 root: the second case describes a build that does not exist, not
// a refusal real old clients will perform. Real old clients keep accepting
// classical-only releases (R-11).
func TestLegacyStableInstallerIsUnchangedByPQSidecarsAndRefusesThePQRoot(t *testing.T) {
	pq := testfixture.NewPQStableWorkflow(t)
	f, m, stable := pq.Fixture, pq.Material, pq.Stable
	sign := pq.SignKeyless
	const archiveName = "hikyo_linux_arm64.tar.gz"
	const base = "https://github.com/Hikyo-Org/Hikyo/releases/download/v1.0.0/"
	archive := []byte("synthetic platform payload for 1.0.0")
	setup := func(t *testing.T, root []byte) (*Installer, updatecheck.Status, string) {
		installer, status, _, responses := installerFixtureForVersion(t, "1.0.0", false, "")
		status.Channel, status.Immutable = updatecheck.ChannelStable, true
		installer.config = Config{StateDir: t.TempDir(), TrustRootBase64: base64.StdEncoding.EncodeToString(root), RecoveryKeyBase64: base64.StdEncoding.EncodeToString(f.Pinned.RecoveryPublicKey)}
		for name, raw := range map[string][]byte{"metadata.json": m.Metadata, "metadata.sigstore.json": m.MetadataSignature, "catalog.json": m.Catalog, "catalog.sigstore.json": m.CatalogSignature, "stable-policy.json": m.StablePolicy, "stable-policy.sigstore.json": m.StablePolicySignature, "stable-trusted-root.json": m.StableTrustedRoot, "primary.pub": f.PrimaryPublic,
			"pq-release-keys.json": pq.Sets.Keyset, "pq-release-keys.pq.json": pq.Sets.KeysetSet, "metadata.pq.json": pq.Sets.MetadataSet, "catalog.pq.json": pq.Sets.CatalogSet} {
			responses[trustURL(name)] = raw
		}
		for name, raw := range map[string][]byte{"metadata.json": m.Metadata, "metadata.sigstore.json": m.MetadataSignature, "catalog.json": m.Catalog, "catalog.sigstore.json": m.CatalogSignature, "release-manifest.json": stable.Manifest, "release-manifest.sigstore.json": stable.ManifestSignature, "release-candidate.json": stable.Candidate, "upgrade-compatibility.json": stable.Compatibility, "build-provenance.json": stable.Provenance, "build-provenance.json.sigstore.json": stable.ProvenanceSignature, archiveName + ".sigstore.json": sign(archive, f.Metadata.SourceCommit, "v1.0.0"),
			"release-manifest.pq.json": pq.ManifestSet} {
			addAssetResponse(&status, responses, base, name, raw)
		}
		return installer, status, filepath.Join(installer.config.StateDir, "release-trust.json")
	}

	t.Run("frozen legacy flow succeeds and writes only the classical floor", func(t *testing.T) {
		installer, status, statePath := setup(t, f.Pinned.Root)
		pqPath := releasetrust.PQFloorPath(statePath)
		sentinel := []byte("{\"format\":1}\n")
		if err := os.WriteFile(pqPath, sentinel, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := installer.verifyStable(t.Context(), status, archiveName, archive); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		floor, err := releasetrust.DecodeVerificationFloor(raw, releasetrust.SnapshotFloor{})
		if err != nil || floor.MetadataSequence != f.Metadata.Sequence {
			t.Fatalf("classical floor %+v %v", floor, err)
		}
		after, err := os.ReadFile(pqPath)
		if err != nil || !bytes.Equal(after, sentinel) {
			t.Fatalf("legacy installer touched the PQ floor: %v", err)
		}
	})
	t.Run("a hypothetical build stamped with the PQ root refuses and changes nothing", func(t *testing.T) {
		installer, status, statePath := setup(t, pq.RootV2)
		if err := installer.verifyStable(t.Context(), status, archiveName, archive); err == nil {
			t.Fatal("a v1 installer accepted a PQ root it cannot enforce")
		}
		if _, err := os.Stat(statePath); !os.IsNotExist(err) {
			t.Fatalf("refused update changed the classical floor: %v", err)
		}
	})
}
