package releasetrust_test

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

// The committed production trust tree lists the two legacy nightly bridges as
// expiry-free v1 statements. Every one of them must verify, so a snapshot and every
// bundle built from it loads, and every one must be expired at any time, so none
// can ever be a route. Only public material is read; nothing here signs.
func TestProductionTrustTreeLoadsWithItsExpiryFreeLegacyBridgesInert(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	trust := filepath.Join(filepath.Dir(file), "..", "..", "release", "trust")
	read := func(parts ...string) []byte {
		raw, err := os.ReadFile(filepath.Join(append([]string{trust}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	material := releasetrust.SnapshotMaterial{
		PrimaryKeys: map[string][]byte{"primary-1": read("primary-1.pub")},
		Metadata:    read("metadata.json"), MetadataSignature: read("metadata.sigstore.json"),
		Catalog: read("catalog.json"), CatalogSignature: read("catalog.sigstore.json"),
		StablePolicy: read("stable-policy.json"), StablePolicySignature: read("stable-policy.sigstore.json"), StableTrustedRoot: read("stable-trusted-root.json"),
	}
	snapshot, err := releasetrust.VerifySnapshot(releasetrust.PinnedTrust{Root: read("root.json"), RecoveryPublicKey: read("recovery-1.pub")}, material, releaseidentity.SnapshotFloor{})
	if err != nil {
		t.Fatalf("the committed production snapshot must load: %v", err)
	}
	digests := snapshot.BridgeDigests()
	entries, err := os.ReadDir(filepath.Join(trust, "bridges"))
	if err != nil || len(digests) == 0 || len(entries) != len(digests) {
		t.Fatalf("catalogued bridges %d, committed statements %d: %v", len(digests), len(entries), err)
	}
	for _, digest := range digests {
		if !slices.ContainsFunc(entries, func(e os.DirEntry) bool { return e.Name() == string(digest) }) {
			t.Fatalf("catalogued bridge %s has no committed statement", digest)
		}
		bridge, err := releasetrust.VerifyBridge(snapshot, releasetrust.BridgeMaterial{Statement: read("bridges", string(digest), "statement.json"), Signature: read("bridges", string(digest), "statement.sigstore.json")})
		if err != nil {
			t.Fatalf("catalogued bridge %s must verify so the bundle loads: %v", digest, err)
		}
		for _, at := range []time.Time{time.Unix(0, 0), time.Now(), time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)} {
			if !bridge.Expired(at) || bridge.Statement().NotAfter != "" {
				t.Fatalf("expiry-free bridge %s must be expired at %s", digest, at)
			}
		}
	}
}
