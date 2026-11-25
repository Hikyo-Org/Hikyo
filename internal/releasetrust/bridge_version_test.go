package releasetrust_test

import (
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

func TestSignedBridgeCannotReturnNightlyToItsStableBase(t *testing.T) {
	for _, tc := range []struct {
		version string
		allowed bool
	}{
		{"1.0.9", false}, {"1.1.0", false}, {"1.1.0+rebuilt", false}, {"1.1.0-rc.1", false},
		{"1.1.1", true}, {"1.2.0", true},
	} {
		t.Run(tc.version, func(t *testing.T) {
			f := testfixture.New(t)
			digest := releaseidentity.Hash([]byte("fixture"))
			source := releaseidentity.Identity{
				Profile: releaseidentity.NightlyV1, Version: "1.1.0-nightly.20260911.1.gaaaaaaaa",
				Sequence: 10, Commit: strings.Repeat("a", 40), CompatibilitySHA256: digest, ManifestSHA256: digest,
			}
			target := source
			target.Profile, target.Version, target.Sequence = releaseidentity.StableV1, tc.version, 11
			migrations := releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{}}
			material := f.AddBridge(t, releasetrust.BridgeStatement{
				Schema: releasetrust.BridgeSchemaV2, NotAfter: "2099-01-01T00:00:00Z", Source: source, Target: target,
				SourcePolicySHA256: digest, TargetPolicySHA256: digest,
				SourceMigrations: migrations, TargetMigrations: migrations,
				SourceSchemaSHA256: digest, TargetSchemaSHA256: digest, Mode: "maintenance",
			})
			_, err := releasetrust.VerifyBridge(f.Snapshot(t), material)
			if (err == nil) != tc.allowed {
				t.Fatalf("VerifyBridge = %v; want allowed=%v", err, tc.allowed)
			}
			if !tc.allowed && !strings.Contains(err.Error(), "stable target must be newer") {
				t.Fatalf("unexpected refusal: %v", err)
			}
		})
	}
}
