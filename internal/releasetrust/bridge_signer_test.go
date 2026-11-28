package releasetrust_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

// R2 (owner decision on #1422): recovery-1 is never extended to bridges. A bridge signed by
// a denied key verifies and counts, but is marked retired for every routable schema so it
// never routes; the inert expiry-free legacy v1 statements are not marked.
func TestRoutableBridgeUnderTheRetiredRecoveryKeyVerifiesButNeverRoutes(t *testing.T) {
	legacy := func(schema string) releasetrust.BridgeStatement {
		digest := releaseidentity.Hash([]byte("fixture"))
		target := releaseidentity.Identity{Profile: releaseidentity.NightlyV1, Version: "1.1.0-nightly.20260101.1.gabcdef12", Sequence: 2, Commit: strings.Repeat("c", 40), CompatibilitySHA256: digest, ManifestSHA256: digest}
		migrations := releaseidentity.MigrationManifest{Engine: "sqlite", Entries: []releaseidentity.Migration{{Version: 1, SHA256: digest}}}
		return releasetrust.BridgeStatement{Schema: schema, SourceGenesis: releaseidentity.LegacyGenesisV1, Mode: "maintenance", Target: target, TargetPolicySHA256: digest,
			SourceMigrations: migrations, TargetMigrations: migrations, SourceSchemaSHA256: digest, TargetSchemaSHA256: digest,
			NotAfter: time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second).Format(time.RFC3339)}
	}
	for name, c := range map[string]struct {
		statement releasetrust.BridgeStatement
		retired   bool
	}{
		"recovery bridge v2":       {v2Statement(releasetrust.BridgeSchemaV2, time.Now().UTC().Add(24*time.Hour).Truncate(time.Second).Format(time.RFC3339)), true},
		"legacy nightly bridge v2": {legacy(releasetrust.LegacyNightlyBridgeSchemaV2), true},
		"legacy nightly bridge v1": {legacy("hikyo.dev/legacy-nightly-bridge/v1"), false},
	} {
		t.Run(name, func(t *testing.T) {
			f := testfixture.New(t)
			material := f.AddBridge(t, c.statement)
			snapshot := f.Snapshot(t)
			if bridge, err := releasetrust.VerifyBridge(snapshot, material); err != nil || bridge.RetiredSigner() {
				t.Fatalf("a bridge signed by a key that is not denied is routable: %v", err)
			}
			sum := sha256.Sum256(f.Pinned.RecoveryPublicKey)
			defer releasetrust.DenyBridgeSignerForTest(hex.EncodeToString(sum[:]))()
			bridge, err := releasetrust.VerifyBridge(snapshot, material)
			if err != nil || !bridge.Valid() || bridge.RetiredSigner() != c.retired {
				t.Fatalf("under a denied key the bridge still verifies; retired = %v, want %v: %v", bridge.RetiredSigner(), c.retired, err)
			}
		})
	}
}

// The denied digest is the committed recovery-1 key, not a stale or mistyped value.
func TestDeniedBridgeSignerIsTheCommittedRecovery1Key(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "release", "trust", "recovery-1.pub"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if !slices.Contains(releasetrust.BridgeDeniedSigners(), hex.EncodeToString(sum[:])) {
		t.Fatal("recovery-1 must be denied as a bridge signer")
	}
}
