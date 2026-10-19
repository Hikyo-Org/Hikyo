package selfupdate

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
)

// verificationState is the historical on-disk encoding, retained as a migration fixture.
type verificationState struct {
	CatalogSequence        int64   `json:"catalog_sequence,omitempty"`
	CatalogSHA256          string  `json:"catalog_sha256,omitempty"`
	TrustSequence          int64   `json:"trust_sequence"`
	HighestReleaseSequence *int64  `json:"highest_release_sequence"`
	HighestRelease         *string `json:"highest_release"`
	MetadataSHA256         string  `json:"metadata_sha256"`
}

func TestLegacyStableAuthenticatesCatalogBeforeAdvancingState(t *testing.T) {
	for _, mutation := range []string{"zero legacy floor", "catalog legacy floor", "canonical floor", "catalog rollback", "catalog equivocation", "metadata rollback", "metadata equivocation", "highest rollback", "bad catalog signature", "unbound catalog", "missing catalog", "bad catalog signature canonical", "unbound catalog canonical", "missing catalog canonical"} {
		t.Run(mutation, func(t *testing.T) {
			f := testfixture.New(t)
			stable := f.AddStable(t, "1.0.0", 1, strings.Repeat("a", 40), []byte("{}"))
			f.Metadata.Sequence, f.Catalog.Sequence = 2, 3
			m := f.Material(t)
			installer, status, _, responses := installerFixtureForVersion(t, "1.0.0", false, "")
			status.Channel, status.Immutable = updatecheck.ChannelStable, true
			installer.config = Config{StateDir: t.TempDir(), TrustRootBase64: base64.StdEncoding.EncodeToString(f.Pinned.Root), RecoveryKeyBase64: base64.StdEncoding.EncodeToString(f.Pinned.RecoveryPublicKey)}
			const archiveName = "hikyo_linux_arm64.tar.gz"
			archive := stable.Payloads[archiveName]
			const base = "https://github.com/Hikyo-Org/Hikyo/releases/download/v1.0.0/"
			for name, raw := range map[string][]byte{"release-manifest.json": stable.Material.Manifest, "release-manifest.sigstore.json": stable.Material.ManifestSignature, "release-candidate.json": stable.Material.Candidate, archiveName + ".sigstore.json": testfixture.Sign(t, f.PrimarySigner, archive)} {
				addAssetResponse(&status, responses, base, name, raw)
			}
			statePath := filepath.Join(installer.config.StateDir, "release-trust.json")
			known := verificationState{TrustSequence: 1, MetadataSHA256: digestHex([]byte("older metadata")), HighestRelease: stringPointer("1.0.0"), HighestReleaseSequence: int64Pointer(1)}
			if mutation != "zero legacy floor" {
				known.CatalogSequence, known.CatalogSHA256 = 3, digestHex(m.Catalog)
			}
			switch strings.TrimSuffix(mutation, " canonical") {
			case "catalog rollback":
				known.CatalogSequence++
			case "catalog equivocation":
				known.CatalogSHA256 = strings.Repeat("a", 64)
			case "metadata rollback":
				known.TrustSequence = 3
			case "metadata equivocation":
				known.TrustSequence = 2
			case "highest rollback":
				known.HighestRelease, known.HighestReleaseSequence = stringPointer("1.1.0"), int64Pointer(2)
			case "bad catalog signature":
				m.CatalogSignature = testfixture.Sign(t, f.PrimarySigner, m.Catalog)
			case "unbound catalog":
				f.Catalog.StableMetadataSHA256 = releaseidentity.Hash([]byte("other metadata"))
				m.Catalog = testfixture.JSON(t, f.Catalog)
				m.CatalogSignature = testfixture.Sign(t, f.RecoverySigner, m.Catalog)
			case "missing catalog":
				m.Catalog = nil
			}
			before := testfixture.JSON(t, known)
			if mutation == "canonical floor" || strings.HasSuffix(mutation, " canonical") {
				before = testfixture.JSON(t, releasetrust.SnapshotFloor{MetadataSequence: known.TrustSequence, MetadataSHA256: releaseidentity.Digest(known.MetadataSHA256), CatalogSequence: known.CatalogSequence, CatalogSHA256: releaseidentity.Digest(known.CatalogSHA256), HighestReleaseSequence: 1})
			}
			if err := os.WriteFile(statePath, before, 0600); err != nil {
				t.Fatal(err)
			}
			for name, raw := range map[string][]byte{"metadata.json": m.Metadata, "metadata.sigstore.json": m.MetadataSignature, "catalog.json": m.Catalog, "catalog.sigstore.json": m.CatalogSignature, "primary.pub": f.PrimaryPublic} {
				responses[trustURL(name)] = raw
			}
			err := installer.verifyStable(t.Context(), status, archiveName, archive)
			after, readErr := os.ReadFile(statePath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			success := mutation == "zero legacy floor" || mutation == "catalog legacy floor" || mutation == "canonical floor"
			if success {
				if err != nil {
					t.Fatal(err)
				}
				floor, err := releasetrust.DecodeVerificationFloor(after, releasetrust.SnapshotFloor{})
				if err != nil || floor.MetadataSequence != 2 || floor.CatalogSequence != 3 || floor.MetadataSHA256 != releaseidentity.Hash(m.Metadata) || floor.CatalogSHA256 != releaseidentity.Hash(m.Catalog) || floor.HighestReleaseSequence != 1 {
					t.Fatalf("did not persist authenticated floors: %#v, %v", floor, err)
				}
			} else if err == nil || !bytes.Equal(before, after) {
				t.Fatalf("invalid evidence must fail without changing state: err=%v, before=%s, after=%s", err, before, after)
			}
		})
	}
}
