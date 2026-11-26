package recoverybridge_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/recoverybridge"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

type routeFixture struct {
	snapshot releasetrust.Snapshot
	nodes    []upgradecompat.VerifiedNode
	bridges  []releasetrust.VerifiedBridge
	source   upgradecompat.InstalledSource
	target   releaseidentity.Identity
	digests  map[string]releaseidentity.Digest
}

func sqliteOf(d upgradecompat.Declaration) upgradecompat.EngineDeclaration {
	for _, e := range d.Engines {
		if e.Migrations.Engine == releaseidentity.SQLite {
			return e
		}
	}
	panic("no sqlite declaration")
}

func ordinaryEdge(from testfixture.SignedRelease, d upgradecompat.Declaration) upgradecompat.SourceEdge {
	e := sqliteOf(d)
	return upgradecompat.SourceEdge{Source: releaseidentity.Source{Release: from.Identity}, Migrations: e.Migrations.Clone(), SchemaSHA256: e.SchemaSHA256, Mode: upgradecompat.Maintenance}
}

// bridgesBetween signs sqlite bridge statements from a to b with the given validity.
func bridgesBetween(t *testing.T, f *testfixture.Fixture, a, b testfixture.SignedRelease, da, db upgradecompat.Declaration, issued, expiry time.Time) recoverybridge.Statement {
	t.Helper()
	snapshot := f.Snapshot(t)
	statements, err := recoverybridge.Derive(evidence(t, snapshot, a, da), evidence(t, snapshot, b, db), issued, expiry)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range statements {
		if s.Engine == releaseidentity.SQLite {
			return s
		}
	}
	t.Fatal("no sqlite statement")
	return recoverybridge.Statement{}
}

func assemble(t *testing.T, f *testfixture.Fixture, releases []testfixture.SignedRelease, statements []recoverybridge.Statement, source upgradecompat.InstalledSource, target releaseidentity.Identity) routeFixture {
	t.Helper()
	f.Catalog.Bridges = []releaseidentity.Digest{}
	for _, s := range statements {
		f.Catalog.Bridges = append(f.Catalog.Bridges, s.Digest)
	}
	f.Catalog.Sequence++
	out := routeFixture{snapshot: f.Snapshot(t), source: source, target: target, digests: map[string]releaseidentity.Digest{}}
	for _, signed := range releases {
		verified, err := releasetrust.VerifyStable(out.snapshot, signed.Material)
		if err != nil {
			t.Fatal(err)
		}
		node, err := upgradecompat.Bind(verified, signed.Material.Compatibility)
		if err != nil {
			t.Fatal(err)
		}
		out.nodes = append(out.nodes, node)
	}
	for _, s := range statements {
		verified, err := releasetrust.VerifyBridge(out.snapshot, releasetrust.BridgeMaterial{Statement: s.Bytes, Signature: testfixture.Sign(t, f.RecoverySigner, s.Bytes)})
		if err != nil {
			t.Fatal(err)
		}
		out.bridges = append(out.bridges, verified)
	}
	return out
}

func (r routeFixture) at(now time.Time, revoked ...releaseidentity.Digest) (upgradecompat.Plan, error) {
	return upgradecompat.PlanRouteAt(r.snapshot, r.source, r.target, r.nodes, r.bridges, now, revoked)
}

func (r routeFixture) match(want releaseidentity.Digest, revoked ...releaseidentity.Digest) (upgradecompat.Plan, error) {
	return upgradecompat.PlanRouteMatching(r.snapshot, r.source, r.target, r.nodes, r.bridges, revoked, want)
}

// P01: a lapsed bridge X (A to C, sequence 2) and a live bridge Y (A to D, sequence 3),
// with T declaring both C and D. Admission takes Y then D to T. A zero-time search
// would take X then C to T, so re-running it and comparing digests would refuse the
// legitimate resume or rollback of the admitted route.
func TestJournaledRouteSurvivesALapsedBridgeThatSortsFirst(t *testing.T) {
	f := testfixture.New(t)
	a, da := release(t, f, 1, 2, "x", nil)
	c, dc := release(t, f, 2, 3, "x", nil)
	d, dd := release(t, f, 3, 4, "x", nil)
	tt, _ := release(t, f, 4, 5, "x", map[releaseidentity.Engine][]upgradecompat.SourceEdge{releaseidentity.SQLite: {ordinaryEdge(c, dc), ordinaryEdge(d, dd)}})
	base := time.Now().UTC().Truncate(time.Second)
	x := bridgesBetween(t, f, a, c, da, dc, base.Add(-3*time.Hour), base.Add(-time.Hour))
	y := bridgesBetween(t, f, a, d, da, dd, base, base.Add(10*24*time.Hour))
	ea := sqliteOf(da)
	source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: a.Identity}, Migrations: ea.Migrations, SchemaSHA256: ea.SchemaSHA256}
	r := assemble(t, f, []testfixture.SignedRelease{a, c, d, tt}, []recoverybridge.Statement{x, y}, source, tt.Identity)

	admitted, err := r.at(base)
	if err != nil || len(admitted.BridgeDigests()) != 1 || admitted.BridgeDigests()[0] != y.Digest {
		t.Fatalf("admission must take the live bridge Y: %v %v", admitted.BridgeDigests(), err)
	}
	if fresh, err := r.at(base.Add(-2 * time.Hour)); err != nil || fresh.Digest() == admitted.Digest() {
		t.Fatalf("the zero-time search must pick the other route, or this test does not reproduce P01: %v", err)
	}
	for name, revoked := range map[string][]releaseidentity.Digest{"rollback": nil, "resume": {x.Digest}} {
		plan, err := r.match(admitted.Digest(), revoked...)
		if err != nil || plan.Digest() != admitted.Digest() {
			t.Fatalf("%s must accept the journaled route: %v", name, err)
		}
	}
	// A resume of a route over a bridge that has since been revoked is refused.
	if _, err := r.match(admitted.Digest(), y.Digest); !errors.Is(err, upgradecompat.ErrJournaledRoute) {
		t.Fatalf("a revoked bridge on the journaled route must refuse the resume: %v", err)
	}
	// A digest that no authenticated path has is refused.
	if _, err := r.match(releaseidentity.Hash([]byte("not a route"))); !errors.Is(err, upgradecompat.ErrJournaledRoute) {
		t.Fatalf("an unknown route digest must be refused: %v", err)
	}
}

// P02: a bridge controls its pair for as long as it is verified. Once it lapses or is
// revoked the pair has no route at all: the ordinary edge it overrode must not come
// back, or the maintenance mode and attestation the root demanded could be bypassed.
// The lapsed bridge and its re-signed replacement share the pair, which must not be an
// error when matching a journaled route, and the journal can never accept the
// overridden ordinary edge.
func TestALapsedOrRevokedBridgeKeepsControllingItsPair(t *testing.T) {
	f := testfixture.New(t)
	a, da := release(t, f, 1, 2, "x", nil)
	c, dc := release(t, f, 2, 3, "x", map[releaseidentity.Engine][]upgradecompat.SourceEdge{releaseidentity.SQLite: {ordinaryEdge(a, da)}})
	base := time.Now().UTC().Truncate(time.Second)
	lapsed := bridgesBetween(t, f, a, c, da, dc, base.Add(-3*time.Hour), base.Add(-time.Hour))
	replacement := bridgesBetween(t, f, a, c, da, dc, base, base.Add(10*24*time.Hour))
	ea := sqliteOf(da)
	source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: a.Identity}, Migrations: ea.Migrations, SchemaSHA256: ea.SchemaSHA256}
	r := assemble(t, f, []testfixture.SignedRelease{a, c}, []recoverybridge.Statement{lapsed, replacement}, source, c.Identity)

	// Both bridges out: no route, and in particular not the overridden ordinary edge.
	if plan, err := r.at(base, replacement.Digest); err == nil {
		t.Fatalf("with every bridge on the pair lapsed or revoked the ordinary edge came back: %v", plan.BridgeDigests())
	}
	// The lapsed statement and its re-signed replacement plan at admission as before.
	admitted, err := r.at(base)
	if err != nil || len(admitted.BridgeDigests()) != 1 || admitted.BridgeDigests()[0] != replacement.Digest || !admitted.RequiresOperatorAttestation() {
		t.Fatalf("the replacement plans after the old bridge lapsed: %v %v", admitted.BridgeDigests(), err)
	}
	if plan, err := r.match(admitted.Digest()); err != nil || plan.Digest() != admitted.Digest() {
		t.Fatalf("the journaled bridge route must be accepted: %v", err)
	}
	// The ordinary route the bridge overrides can never be a journaled route.
	ordinary := assemble(t, f, []testfixture.SignedRelease{a, c}, nil, source, c.Identity)
	overridden, err := ordinary.at(base)
	if err != nil || len(overridden.BridgeDigests()) != 0 {
		t.Fatalf("without bridges the ordinary edge is the route, or this test does not reproduce the bypass: %v", err)
	}
	if _, err := r.match(overridden.Digest()); !errors.Is(err, upgradecompat.ErrJournaledRoute) {
		t.Fatalf("a journaled digest through the overridden ordinary edge must be refused while the bridge is verified: %v", err)
	}
}

// An empty journaled digest is not "any route": without the validity check it would run
// a fresh admission search and accept whatever it finds, and the zero-step route (source
// equals target) must match only its own digest.
func TestJournaledRouteRefusesAnEmptyOrForeignDigest(t *testing.T) {
	f := testfixture.New(t)
	a, da := release(t, f, 1, 2, "x", nil)
	c, dc := release(t, f, 2, 3, "x", nil)
	base := time.Now().UTC().Truncate(time.Second)
	x := bridgesBetween(t, f, a, c, da, dc, base, base.Add(10*24*time.Hour))
	ea := sqliteOf(da)
	source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: a.Identity}, Migrations: ea.Migrations, SchemaSHA256: ea.SchemaSHA256}
	r := assemble(t, f, []testfixture.SignedRelease{a, c}, []recoverybridge.Statement{x}, source, c.Identity)
	admitted, err := r.at(base)
	if err != nil || admitted.Digest() == "" {
		t.Fatalf("the route to match must admit: %v", err)
	}
	for name, want := range map[string]releaseidentity.Digest{"empty": "", "malformed": "not-a-digest"} {
		if _, err := r.match(want); !errors.Is(err, upgradecompat.ErrJournaledRoute) {
			t.Fatalf("a %s journaled digest must be refused, not searched afresh: %v", name, err)
		}
	}
	// Source equals target: only the empty route's own digest matches.
	same := r
	same.target = a.Identity
	empty, err := same.at(base)
	if err != nil || len(empty.Steps()) != 0 {
		t.Fatalf("source equal to target plans no steps: %v", err)
	}
	if plan, err := same.match(empty.Digest()); err != nil || plan.Digest() != empty.Digest() {
		t.Fatalf("the zero-step route matches its own digest: %v", err)
	}
	if _, err := same.match(admitted.Digest()); !errors.Is(err, upgradecompat.ErrJournaledRoute) {
		t.Fatalf("a journaled route with steps does not match the zero-step route: %v", err)
	}
}
