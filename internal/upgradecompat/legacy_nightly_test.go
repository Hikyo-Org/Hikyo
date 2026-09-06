package upgradecompat_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func TestLegacyNightlyRequiresExactRecoveryBridge(t *testing.T) {
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) {
			m := manifest(engine, 1)
			source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Genesis: releaseidentity.LegacyGenesisV1}, Migrations: m, SchemaSHA256: catalog(m)}
			d := upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: releaseidentity.NightlyV1, Version: "1.1.0-nightly.1", Sequence: 2, Commit: commit, Engines: []upgradecompat.EngineDeclaration{{Migrations: m, SchemaSHA256: catalog(m), Sources: []upgradecompat.SourceEdge{{Source: source.Identity, Migrations: m, SchemaSHA256: catalog(m), Mode: upgradecompat.Maintenance}}}}}
			f, material, _ := testfixture.Nightly(t, testfixture.JSON(t, d), false)
			payloads := map[string][]byte{}
			for name, reader := range material.Artifacts {
				raw, err := io.ReadAll(reader)
				if err != nil {
					t.Fatal(err)
				}
				payloads[name] = raw
			}
			nodeFor := func(snapshot releasetrust.Snapshot) upgradecompat.VerifiedNode {
				t.Helper()
				for name, raw := range payloads {
					material.Artifacts[name] = bytes.NewReader(raw)
				}
				release, err := releasetrust.VerifyNightly(snapshot, material)
				if err != nil {
					t.Fatal(err)
				}
				node, err := upgradecompat.Bind(release, material.Compatibility)
				if err != nil {
					t.Fatal(err)
				}
				return node
			}
			snapshot := f.Snapshot(t)
			node := nodeFor(snapshot)
			if _, err := upgradecompat.PlanRoute(snapshot, source, node.Identity(), []upgradecompat.VerifiedNode{node}, nil); err == nil {
				t.Fatal("ordinary signed declaration admitted unsigned legacy database")
			}
			statement := releasetrust.BridgeStatement{Schema: "hikyo.dev/legacy-nightly-bridge/v1", SourceGenesis: releaseidentity.LegacyGenesisV1, Target: node.Identity(), TargetPolicySHA256: releaseidentity.Hash(material.Policy), SourceMigrations: m, TargetMigrations: m, SourceSchemaSHA256: catalog(m), TargetSchemaSHA256: catalog(m), Mode: "maintenance"}
			for _, mutation := range []string{"none", "fresh", "invented identity", "invented policy", "stable target", "changed source", "wrong target policy", "forged signature", "withdrawn"} {
				t.Run(mutation, func(t *testing.T) {
					changed := statement
					switch mutation {
					case "fresh":
						changed.SourceGenesis = releaseidentity.FreshGenesisV1
					case "invented identity":
						changed.Source = node.Identity()
					case "invented policy":
						changed.SourcePolicySHA256 = releaseidentity.Hash([]byte("invented"))
					case "stable target":
						changed.Target.Profile, changed.Target.Version = releaseidentity.StableV1, "1.1.0"
					case "changed source":
						changed.SourceSchemaSHA256 = releaseidentity.Hash([]byte("other schema"))
					case "wrong target policy":
						changed.TargetPolicySHA256 = releaseidentity.Hash([]byte("other policy"))
					}
					f.Catalog.Bridges = []releaseidentity.Digest{}
					raw := f.AddBridge(t, changed)
					if mutation == "forged signature" {
						raw.Signature = testfixture.Sign(t, f.PrimarySigner, raw.Statement)
					}
					if mutation == "withdrawn" {
						f.Catalog.Bridges = []releaseidentity.Digest{}
					}
					snapshot := f.Snapshot(t)
					bridge, err := releasetrust.VerifyBridge(snapshot, raw)
					if err == nil {
						node := nodeFor(snapshot)
						var plan upgradecompat.Plan
						plan, err = upgradecompat.PlanRoute(snapshot, source, node.Identity(), []upgradecompat.VerifiedNode{node}, []releasetrust.VerifiedBridge{bridge})
						if err == nil && (!plan.RequiresOperatorAttestation() || len(plan.BridgeDigests()) != 1) {
							t.Fatal("bridge omitted independent local proof")
						}
					}
					if (err == nil) != (mutation == "none") {
						t.Fatalf("bridge result: %v", err)
					}
				})
			}
		})
	}
}
