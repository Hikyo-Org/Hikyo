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
// a denied key is refused for every routable schema, while the inert expiry-free legacy v1
// statements stay loadable so the committed catalog still verifies.
func TestRoutableBridgeSignedByTheRetiredRecoveryKeyIsRefused(t *testing.T) {
	legacy := func(schema string) releasetrust.BridgeStatement {
		digest := releaseidentity.Hash([]byte("fixture"))
		target := releaseidentity.Identity{Profile: releaseidentity.NightlyV1, Version: "1.1.0-nightly.20260101.1.gabcdef12", Sequence: 2, Commit: strings.Repeat("c", 40), CompatibilitySHA256: digest, ManifestSHA256: digest}
		migrations := releaseidentity.MigrationManifest{Engine: "sqlite", Entries: []releaseidentity.Migration{{Version: 1, SHA256: digest}}}
		return releasetrust.BridgeStatement{Schema: schema, SourceGenesis: releaseidentity.LegacyGenesisV1, Mode: "maintenance", Target: target, TargetPolicySHA256: digest,
			SourceMigrations: migrations, TargetMigrations: migrations, SourceSchemaSHA256: digest, TargetSchemaSHA256: digest,
			NotAfter: time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second).Format(time.RFC3339)}
	}
	for name, statement := range map[string]releasetrust.BridgeStatement{
		"recovery bridge v2":       v2Statement(releasetrust.BridgeSchemaV2, time.Now().UTC().Add(24*time.Hour).Truncate(time.Second).Format(time.RFC3339)),
		"legacy nightly bridge v2": legacy(releasetrust.LegacyNightlyBridgeSchemaV2),
	} {
		t.Run(name, func(t *testing.T) {
			f := testfixture.New(t)
			material := f.AddBridge(t, statement)
			snapshot := f.Snapshot(t)
			if _, err := releasetrust.VerifyBridge(snapshot, material); err != nil {
				t.Fatalf("a bridge signed by a key that is not denied verifies: %v", err)
			}
			sum := sha256.Sum256(f.Pinned.RecoveryPublicKey)
			undo := releasetrust.DenyBridgeSigner(hex.EncodeToString(sum[:]))
			defer undo()
			if _, err := releasetrust.VerifyBridge(snapshot, material); err == nil || !strings.Contains(err.Error(), "retired recovery-1") {
				t.Fatalf("a routable bridge signed by a denied recovery key must be refused: %v", err)
			}
		})
	}
	t.Run("legacy v1 stays loadable", func(t *testing.T) {
		f := testfixture.New(t)
		material := f.AddBridge(t, legacy("hikyo.dev/legacy-nightly-bridge/v1"))
		snapshot := f.Snapshot(t)
		sum := sha256.Sum256(f.Pinned.RecoveryPublicKey)
		defer releasetrust.DenyBridgeSigner(hex.EncodeToString(sum[:]))()
		if bridge, err := releasetrust.VerifyBridge(snapshot, material); err != nil || !bridge.Expired(time.Now()) {
			t.Fatalf("the inert v1 statement still loads, always expired: %v", err)
		}
	})
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
