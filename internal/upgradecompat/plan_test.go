package upgradecompat_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

var commit = strings.Repeat("a", 40)

func manifest(engine releaseidentity.Engine, count int) releaseidentity.MigrationManifest {
	m := releaseidentity.MigrationManifest{Engine: engine, Entries: []releaseidentity.Migration{}}
	for i := range count {
		m.Entries = append(m.Entries, releaseidentity.Migration{Version: uint64(i + 1), SHA256: releaseidentity.Hash([]byte(fmt.Sprint(i)))})
	}
	return m
}

func add(t *testing.T, f *testfixture.Fixture, seq int, m releaseidentity.MigrationManifest, sources ...upgradecompat.SourceEdge) testfixture.SignedRelease {
	t.Helper()
	if sources == nil {
		sources = []upgradecompat.SourceEdge{}
	}
	d := upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: releaseidentity.StableV1, Version: fmt.Sprintf("1.%d.0", seq), Sequence: uint64(seq), Commit: commit, Engines: []upgradecompat.EngineDeclaration{{Migrations: m, SchemaSHA256: catalog(m), Sources: sources}}}
	return f.AddStable(t, d.Version, int64(seq), commit, testfixture.JSON(t, d))
}

func catalog(m releaseidentity.MigrationManifest) releaseidentity.Digest {
	d, _ := m.Digest()
	return d
}

func edge(r testfixture.SignedRelease, m releaseidentity.MigrationManifest) upgradecompat.SourceEdge {
	return upgradecompat.SourceEdge{Source: releaseidentity.Source{Release: r.Identity}, Migrations: m, SchemaSHA256: catalog(m), Mode: upgradecompat.Maintenance}
}

func nodes(t *testing.T, snapshot releasetrust.Snapshot, releases ...testfixture.SignedRelease) []upgradecompat.VerifiedNode {
	t.Helper()
	result := []upgradecompat.VerifiedNode{}
	for _, raw := range releases {
		release, err := releasetrust.VerifyStable(snapshot, raw.Material)
		if err != nil {
			t.Fatal(err)
		}
		node, err := upgradecompat.Bind(release, raw.Material.Compatibility)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, node)
	}
	return result
}

func TestSignedRoutesAreDeterministicAndImmutable(t *testing.T) {
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) {
			f := testfixture.New(t)
			m := manifest(engine, 1)
			a := add(t, f, 1, m)
			b := add(t, f, 2, m, edge(a, m))
			c := add(t, f, 3, m, edge(a, m))
			d := add(t, f, 4, manifest(engine, 2), edge(b, m), edge(c, m))
			snapshot := f.Snapshot(t)
			ns := nodes(t, snapshot, a, b, c, d)
			source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: a.Identity}, Migrations: m, SchemaSHA256: catalog(m)}
			plan, err := upgradecompat.PlanRoute(snapshot, source, d.Identity, ns, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Steps()) != 2 || plan.Steps()[0].Target != b.Identity {
				t.Fatal("fewest hops/ascending tie not applied")
			}
			slices.Reverse(ns)
			reordered, err := upgradecompat.PlanRoute(snapshot, source, d.Identity, ns, nil)
			if err != nil || reordered.Digest() != plan.Digest() {
				t.Fatal("input order changes route", err)
			}
			steps := plan.Steps()
			steps[0].SourceMigrations.Entries[0].SHA256 = releaseidentity.Hash([]byte("mutated"))
			steps[0].Artifacts[0].SHA256 = "mutated"
			if plan.Steps()[0].SourceMigrations.Entries[0].SHA256 != m.Entries[0].SHA256 || plan.Steps()[0].Artifacts[0].SHA256 == "mutated" {
				t.Fatal("mutable plan authority exposed")
			}
			restart, err := upgradecompat.PlanRoute(snapshot, source, a.Identity, ns, nil)
			if err != nil || len(restart.Steps()) != 0 {
				t.Fatal("same release restart refused", err)
			}
			if _, err := upgradecompat.PlanRoute(snapshot, source, a.Identity, ns, []releasetrust.VerifiedBridge{{}}); err == nil {
				t.Fatal("same release ignored invalid bridge")
			}
			source.Migrations.Entries[0].SHA256 = releaseidentity.Hash([]byte("changed old migration"))
			if _, err := upgradecompat.PlanRoute(snapshot, source, d.Identity, ns, nil); err == nil {
				t.Fatal("changed installed history accepted")
			}
		})
	}
}

func TestWithdrawalNeedsExactCurrentRecoveryBridge(t *testing.T) {
	f := testfixture.New(t)
	m := manifest(releaseidentity.SQLite, 1)
	a := add(t, f, 1, m)
	b := add(t, f, 2, m, edge(a, m))
	oldSnapshot := f.Snapshot(t)
	oldNodes := nodes(t, oldSnapshot, a, b)
	source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: a.Identity}, Migrations: m, SchemaSHA256: catalog(m)}
	f.Metadata.Releases = f.Metadata.Releases[1:]
	f.Metadata.Sequence++
	f.Catalog.Sequence++
	snapshot := f.Snapshot(t)
	targetNodes := nodes(t, snapshot, b)
	if _, err := upgradecompat.PlanRoute(snapshot, source, b.Identity, oldNodes, nil); err == nil {
		t.Fatal("cached proof ignored withdrawal")
	}
	if _, err := upgradecompat.PlanRoute(snapshot, source, b.Identity, targetNodes, nil); err == nil {
		t.Fatal("ordinary edge revived withdrawn source")
	}
	policy := releaseidentity.Hash(f.Pinned.Root)
	raw := f.AddBridge(t, releasetrust.BridgeStatement{Schema: "hikyo.dev/recovery-bridge/v1", Source: a.Identity, Target: b.Identity, SourcePolicySHA256: policy, TargetPolicySHA256: policy, SourceMigrations: m, TargetMigrations: m, SourceSchemaSHA256: catalog(m), TargetSchemaSHA256: catalog(m), Mode: "maintenance"})
	snapshot = f.Snapshot(t)
	targetNodes = nodes(t, snapshot, b)
	bridge, err := releasetrust.VerifyBridge(snapshot, raw)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := upgradecompat.PlanRoute(snapshot, source, b.Identity, targetNodes, []releasetrust.VerifiedBridge{bridge})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.BridgeDigests()) != 1 || plan.Steps()[0].Mode != upgradecompat.Maintenance {
		t.Fatal("bridge receipt binding missing")
	}
	if _, err := upgradecompat.PlanRoute(snapshot, source, b.Identity, nil, []releasetrust.VerifiedBridge{bridge}); err == nil {
		t.Fatal("bridge authorized target without target proof")
	}
	statement := bridge.Statement()
	statement.SourceMigrations.Entries[0].SHA256 = releaseidentity.Hash([]byte("change"))
	if bridge.Statement().SourceMigrations.Entries[0].SHA256 != m.Entries[0].SHA256 {
		t.Fatal("mutable bridge")
	}
}

func TestGenesisAndGraphBounds(t *testing.T) {
	f := testfixture.New(t)
	empty := manifest(releaseidentity.SQLite, 0)
	m := manifest(releaseidentity.SQLite, 1)
	genesis := releaseidentity.Source{Genesis: releaseidentity.FreshGenesisV1}
	schema := releaseidentity.Hash([]byte("inspected empty schema"))
	a := add(t, f, 1, m, upgradecompat.SourceEdge{Source: genesis, Migrations: empty, SchemaSHA256: schema, Mode: upgradecompat.Maintenance})
	snapshot := f.Snapshot(t)
	ns := nodes(t, snapshot, a)
	source := upgradecompat.InstalledSource{Identity: genesis, Migrations: empty, SchemaSHA256: schema}
	if _, err := upgradecompat.PlanRoute(snapshot, source, a.Identity, ns, nil); err != nil {
		t.Fatal(err)
	}
	source.SchemaSHA256 = releaseidentity.Hash([]byte("other schema"))
	if _, err := upgradecompat.PlanRoute(snapshot, source, a.Identity, ns, nil); err == nil {
		t.Fatal("genesis schema substitution accepted")
	}
	if _, err := upgradecompat.PlanRoute(snapshot, source, a.Identity, make([]upgradecompat.VerifiedNode, 257), nil); err == nil {
		t.Fatal("node bound ignored")
	}
	if _, err := upgradecompat.PlanRoute(snapshot, source, a.Identity, ns, make([]releasetrust.VerifiedBridge, 1025)); err == nil {
		t.Fatal("edge bound ignored")
	}
	if _, err := upgradecompat.PlanRoute(snapshot, source, a.Identity, append(ns, ns...), nil); err == nil {
		t.Fatal("duplicate node accepted")
	}
}

func TestHopBoundAndMissingIntermediateFailClosed(t *testing.T) {
	f := testfixture.New(t)
	m := manifest(releaseidentity.Postgres, 1)
	releases := []testfixture.SignedRelease{add(t, f, 1, m)}
	for seq := 2; seq <= 34; seq++ {
		releases = append(releases, add(t, f, seq, m, edge(releases[len(releases)-1], m)))
	}
	snapshot := f.Snapshot(t)
	ns := nodes(t, snapshot, releases...)
	source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: releases[0].Identity}, Migrations: m, SchemaSHA256: catalog(m)}
	if p, err := upgradecompat.PlanRoute(snapshot, source, releases[32].Identity, ns, nil); err != nil || len(p.Steps()) != 32 {
		t.Fatal("32-hop supported route failed", err)
	}
	if _, err := upgradecompat.PlanRoute(snapshot, source, releases[33].Identity, ns, nil); err == nil {
		t.Fatal("33-hop route accepted")
	}
	ns = append(ns[:5], ns[6:]...)
	if _, err := upgradecompat.PlanRoute(snapshot, source, releases[32].Identity, ns, nil); err == nil {
		t.Fatal("missing intermediate accepted")
	}
}

func TestBridgeCannotBeOmittedToBypassMaintenancePrecedence(t *testing.T) {
	f := testfixture.New(t)
	m := manifest(releaseidentity.SQLite, 1)
	a := add(t, f, 1, m)
	ordinary := edge(a, m)
	ordinary.Mode = upgradecompat.Restart
	b := add(t, f, 2, m, ordinary)
	policy := releaseidentity.Hash(f.Pinned.Root)
	raw := f.AddBridge(t, releasetrust.BridgeStatement{Schema: "hikyo.dev/recovery-bridge/v1", Source: a.Identity, Target: b.Identity, SourcePolicySHA256: policy, TargetPolicySHA256: policy, SourceMigrations: m, TargetMigrations: m, SourceSchemaSHA256: catalog(m), TargetSchemaSHA256: catalog(m), Mode: "maintenance"})
	snapshot := f.Snapshot(t)
	ns := nodes(t, snapshot, a, b)
	source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: a.Identity}, Migrations: m, SchemaSHA256: catalog(m)}
	if _, err := upgradecompat.PlanRoute(snapshot, source, b.Identity, ns, nil); err == nil {
		t.Fatal("omitting bridge bypassed maintenance precedence")
	}
	bridge, err := releasetrust.VerifyBridge(snapshot, raw)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := upgradecompat.PlanRoute(snapshot, source, b.Identity, ns, []releasetrust.VerifiedBridge{bridge})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps()) != 1 || plan.Steps()[0].Mode != upgradecompat.Maintenance || !plan.RequiresOperatorAttestation() || plan.BridgeDigests()[0] != bridge.Digest() {
		t.Fatal("ordinary edge took precedence over root bridge")
	}
	listed := snapshot.BridgeDigests()
	listed[0] = releaseidentity.Hash([]byte("mutated"))
	if snapshot.BridgeDigests()[0] != bridge.Digest() {
		t.Fatal("mutable catalog inventory")
	}
	f.Catalog.Bridges = []releaseidentity.Digest{}
	f.Catalog.Sequence++
	withdrawn := f.Snapshot(t)
	if _, err := releasetrust.VerifyBridge(withdrawn, raw); err == nil {
		t.Fatal("withdrawn bridge still authenticated")
	}
}
