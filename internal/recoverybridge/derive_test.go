package recoverybridge_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/recoverybridge"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

var commit = strings.Repeat("a", 40)

// window returns a derivation instant and an expiry ten days later, whole seconds.
func window() (time.Time, time.Time) {
	now := time.Now().UTC().Truncate(time.Second)
	return now, now.Add(10 * 24 * time.Hour)
}

func derive(from, to recoverybridge.Evidence) ([]recoverybridge.Statement, error) {
	now, expiry := window()
	return recoverybridge.Derive(from, to, now, expiry)
}

func manifest(engine releaseidentity.Engine, count int, salt string) releaseidentity.MigrationManifest {
	m := releaseidentity.MigrationManifest{Engine: engine, Entries: []releaseidentity.Migration{}}
	for i := range count {
		m.Entries = append(m.Entries, releaseidentity.Migration{Version: uint64(i + 1), SHA256: releaseidentity.Hash([]byte(fmt.Sprint(salt, i)))})
	}
	return m
}

func schema(m releaseidentity.MigrationManifest) releaseidentity.Digest {
	d, _ := m.Digest()
	return d
}

// release adds a signed stable release declaring both engines with count
// migrations each and the given per-engine source edges.
func release(t *testing.T, f *testfixture.Fixture, sequence, count int, salt string, edges map[releaseidentity.Engine][]upgradecompat.SourceEdge) (testfixture.SignedRelease, upgradecompat.Declaration) {
	t.Helper()
	d := upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: releaseidentity.StableV1, Version: fmt.Sprintf("1.%d.0", sequence), Sequence: uint64(sequence), Commit: commit}
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		m := manifest(engine, count, salt)
		sources := edges[engine]
		if sources == nil {
			sources = []upgradecompat.SourceEdge{}
		}
		d.Engines = append(d.Engines, upgradecompat.EngineDeclaration{Migrations: m, SchemaSHA256: schema(m), Sources: sources})
	}
	return f.AddStable(t, d.Version, int64(sequence), commit, testfixture.JSON(t, d)), d
}

func evidence(t *testing.T, snapshot releasetrust.Snapshot, signed testfixture.SignedRelease, d upgradecompat.Declaration) recoverybridge.Evidence {
	t.Helper()
	verified, err := releasetrust.VerifyStable(snapshot, signed.Material)
	if err != nil {
		t.Fatal(err)
	}
	return recoverybridge.Evidence{Release: verified, Declaration: d}
}

func TestDeriveBindsExactEvidenceAndRoutesOnBothEngines(t *testing.T) {
	f := testfixture.New(t)
	a, da := release(t, f, 1, 2, "x", nil)
	b, db := release(t, f, 2, 4, "x", nil)
	snapshot := f.Snapshot(t)
	from, to := evidence(t, snapshot, a, da), evidence(t, snapshot, b, db)
	statements, err := derive(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 2 {
		t.Fatalf("want one statement per engine, got %d", len(statements))
	}
	var bridges []releasetrust.BridgeMaterial
	for _, s := range statements {
		f.Catalog.Bridges = append(f.Catalog.Bridges, s.Digest)
		bridges = append(bridges, releasetrust.BridgeMaterial{Statement: s.Bytes, Signature: testfixture.Sign(t, f.RecoverySigner, s.Bytes)})
	}
	f.Catalog.Sequence++
	snapshot = f.Snapshot(t)
	nodes := []upgradecompat.VerifiedNode{}
	for _, signed := range []testfixture.SignedRelease{a, b} {
		verified, err := releasetrust.VerifyStable(snapshot, signed.Material)
		if err != nil {
			t.Fatal(err)
		}
		node, err := upgradecompat.Bind(verified, signed.Material.Compatibility)
		if err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, node)
	}
	verifiedBridges := []releasetrust.VerifiedBridge{}
	for _, material := range bridges {
		bridge, err := releasetrust.VerifyBridge(snapshot, material)
		if err != nil {
			t.Fatal(err)
		}
		verifiedBridges = append(verifiedBridges, bridge)
	}
	for i, s := range statements {
		t.Run(string(s.Engine), func(t *testing.T) {
			var declaration upgradecompat.EngineDeclaration
			for _, e := range da.Engines {
				if e.Migrations.Engine == s.Engine {
					declaration = e
				}
			}
			source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: a.Identity}, Migrations: declaration.Migrations, SchemaSHA256: declaration.SchemaSHA256}
			plan, err := upgradecompat.PlanRoute(snapshot, source, b.Identity, nodes, verifiedBridges)
			if err != nil {
				t.Fatal(err)
			}
			if steps := plan.Steps(); len(steps) != 1 || steps[0].BridgeSHA256 != statements[i].Digest || steps[0].Mode != upgradecompat.Maintenance || !plan.RequiresOperatorAttestation() {
				t.Fatalf("want one attested maintenance bridge step, got %+v", steps)
			}
			// A database whose schema is not the signed source gets no route.
			source.SchemaSHA256 = releaseidentity.Hash([]byte("other"))
			if _, err := upgradecompat.PlanRoute(snapshot, source, b.Identity, nodes, verifiedBridges); err == nil {
				t.Fatal("bridge applied to a different source schema")
			}
		})
	}
	// No bridge, no route: the planner never invents one.
	source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: a.Identity}, Migrations: da.Engines[0].Migrations, SchemaSHA256: da.Engines[0].SchemaSHA256}
	if _, err := upgradecompat.PlanRoute(snapshot, source, b.Identity, nodes, nil); err == nil {
		t.Fatal("route exists without the bridge proofs")
	}
}

func TestBridgeThenOrdinaryEdgeIsTwoHops(t *testing.T) {
	f := testfixture.New(t)
	a, da := release(t, f, 1, 2, "x", nil)
	b, db := release(t, f, 2, 4, "x", nil)
	edges := map[releaseidentity.Engine][]upgradecompat.SourceEdge{}
	for _, e := range db.Engines {
		edges[e.Migrations.Engine] = []upgradecompat.SourceEdge{{Source: releaseidentity.Source{Release: b.Identity}, Migrations: e.Migrations, SchemaSHA256: e.SchemaSHA256, Mode: upgradecompat.Maintenance}}
	}
	c, _ := release(t, f, 3, 5, "x", edges)
	snapshot := f.Snapshot(t)
	statements, err := derive(evidence(t, snapshot, a, da), evidence(t, snapshot, b, db))
	if err != nil {
		t.Fatal(err)
	}
	// Both engines carry the same shape, so one engine proves the chain.
	s := statements[0]
	f.Catalog.Bridges, f.Catalog.Sequence = append(f.Catalog.Bridges, s.Digest), f.Catalog.Sequence+1
	snapshot = f.Snapshot(t)
	bridge, err := releasetrust.VerifyBridge(snapshot, releasetrust.BridgeMaterial{Statement: s.Bytes, Signature: testfixture.Sign(t, f.RecoverySigner, s.Bytes)})
	if err != nil {
		t.Fatal(err)
	}
	nodes := []upgradecompat.VerifiedNode{}
	for _, signed := range []testfixture.SignedRelease{a, b, c} {
		verified, err := releasetrust.VerifyStable(snapshot, signed.Material)
		if err != nil {
			t.Fatal(err)
		}
		node, err := upgradecompat.Bind(verified, signed.Material.Compatibility)
		if err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, node)
	}
	var declaration upgradecompat.EngineDeclaration
	for _, e := range da.Engines {
		if e.Migrations.Engine == s.Engine {
			declaration = e
		}
	}
	source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: a.Identity}, Migrations: declaration.Migrations, SchemaSHA256: declaration.SchemaSHA256}
	plan, err := upgradecompat.PlanRoute(snapshot, source, c.Identity, nodes, []releasetrust.VerifiedBridge{bridge})
	if err != nil {
		t.Fatal(err)
	}
	steps := plan.Steps()
	if len(steps) != 2 || steps[0].BridgeSHA256 != s.Digest || steps[0].Target != b.Identity || steps[1].BridgeSHA256 != "" || steps[1].Target != c.Identity {
		t.Fatalf("want bridge then ordinary edge, got %+v", steps)
	}
}

func TestDeriveRefusesUnsoundPairs(t *testing.T) {
	f := testfixture.New(t)
	a, da := release(t, f, 1, 3, "x", nil)
	b, db := release(t, f, 2, 5, "x", nil)
	rewritten, drw := release(t, f, 3, 5, "y", nil) // same count, different migration hashes
	same, dsame := release(t, f, 4, 3, "x", nil)    // a newer release with the very same migrations
	other := testfixture.New(t)
	foreign, dforeign := release(t, other, 2, 5, "x", nil)
	snapshot := f.Snapshot(t)
	from, to := evidence(t, snapshot, a, da), evidence(t, snapshot, b, db)
	oneEngine := to
	oneEngine.Declaration.Engines = oneEngine.Declaration.Engines[:1]
	wrongVersion := to
	wrongVersion.Declaration.Version = "9.9.9"
	cases := map[string]struct {
		from, to recoverybridge.Evidence
		want     string
	}{
		"target not newer":           {to, from, "newer stable release"},
		"same release":               {from, from, "newer stable release"},
		"rewritten history":          {from, evidence(t, snapshot, rewritten, drw), "strict prefix"},
		"no new migrations":          {from, evidence(t, snapshot, same, dsame), "strict prefix"},
		"different engine sets":      {from, oneEngine, "different engines"},
		"declaration of another":     {from, wrongVersion, "does not describe"},
		"evidence from another root": {from, evidenceFrom(t, other, foreign, dforeign), "one trust snapshot"},
		"unverified release":         {from, recoverybridge.Evidence{Declaration: db}, "verified releases"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := derive(c.from, c.to); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want %q, got %v", c.want, err)
			}
		})
	}
}

func evidenceFrom(t *testing.T, f *testfixture.Fixture, signed testfixture.SignedRelease, d upgradecompat.Declaration) recoverybridge.Evidence {
	t.Helper()
	return evidence(t, f.Snapshot(t), signed, d)
}
