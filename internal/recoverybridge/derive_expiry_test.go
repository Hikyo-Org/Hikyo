package recoverybridge_test

import (
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/recoverybridge"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func TestExpiredOrRevokedBridgeDropsOutOfThePlan(t *testing.T) {
	f := testfixture.New(t)
	a, da := release(t, f, 1, 2, "x", nil)
	b, db := release(t, f, 2, 4, "x", nil)
	snapshot := f.Snapshot(t)
	now, expiry := window()
	statements, err := recoverybridge.Derive(evidence(t, snapshot, a, da), evidence(t, snapshot, b, db), now, expiry)
	if err != nil {
		t.Fatal(err)
	}
	s := statements[0]
	f.Catalog.Bridges, f.Catalog.Sequence = []releaseidentity.Digest{s.Digest}, f.Catalog.Sequence+1
	snapshot = f.Snapshot(t)
	bridge, err := releasetrust.VerifyBridge(snapshot, releasetrust.BridgeMaterial{Statement: s.Bytes, Signature: testfixture.Sign(t, f.RecoverySigner, s.Bytes)})
	if err != nil {
		t.Fatal(err)
	}
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
	var declaration upgradecompat.EngineDeclaration
	for _, e := range da.Engines {
		if e.Migrations.Engine == s.Engine {
			declaration = e
		}
	}
	source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: a.Identity}, Migrations: declaration.Migrations, SchemaSHA256: declaration.SchemaSHA256}
	plan := func(at time.Time, revoked ...releaseidentity.Digest) error {
		_, err := upgradecompat.PlanRouteAt(snapshot, source, b.Identity, nodes, []releasetrust.VerifiedBridge{bridge}, at, revoked)
		return err
	}
	if err := plan(expiry.Add(-time.Second)); err != nil {
		t.Fatalf("a live bridge plans: %v", err)
	}
	if err := plan(expiry); err == nil {
		t.Fatal("a bridge at its not_after must not plan")
	}
	if err := plan(expiry.Add(time.Hour)); err == nil {
		t.Fatal("an expired bridge must not plan")
	}
	if err := plan(expiry.Add(-time.Second), s.Digest); err == nil {
		t.Fatal("a revoked bridge must not plan")
	}
	if err := plan(expiry.Add(-time.Second), releaseidentity.Hash([]byte("another"))); err != nil {
		t.Fatalf("revoking an unrelated digest changes nothing: %v", err)
	}
}

func TestDeriveRefusesAnUnboundedOrPastExpiry(t *testing.T) {
	f := testfixture.New(t)
	a, da := release(t, f, 1, 2, "x", nil)
	b, db := release(t, f, 2, 4, "x", nil)
	snapshot := f.Snapshot(t)
	from, to := evidence(t, snapshot, a, da), evidence(t, snapshot, b, db)
	now := time.Now().UTC().Truncate(time.Second)
	for name, expiry := range map[string]time.Time{
		"in the past":        now.Add(-time.Second),
		"at now":             now,
		"over thirty days":   now.Add(recoverybridge.MaxWindow + time.Second),
		"with fractional ns": now.Add(time.Hour + time.Nanosecond),
	} {
		if _, err := recoverybridge.Derive(from, to, now, expiry); err == nil {
			t.Fatalf("%s: expiry accepted", name)
		}
	}
	if _, err := recoverybridge.Derive(from, to, now, now.Add(recoverybridge.MaxWindow)); err != nil {
		t.Fatalf("exactly thirty days is allowed: %v", err)
	}
}

// A lapsed bridge and its re-signed replacement share a source and target pair.
// The lapsed one must leave the route graph before the duplicate-pair check, or one
// expired bridge in the bundle breaks every plan, including unrelated ones.
func TestLapsedBridgeDoesNotBreakItsResignedReplacementOrUnrelatedPlans(t *testing.T) {
	f := testfixture.New(t)
	a, da := release(t, f, 1, 2, "x", nil)
	b, db := release(t, f, 2, 4, "x", nil)
	snapshot := f.Snapshot(t)
	now := time.Now().UTC().Truncate(time.Second)
	lapsed, err := recoverybridge.Derive(evidence(t, snapshot, a, da), evidence(t, snapshot, b, db), now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := recoverybridge.Derive(evidence(t, snapshot, a, da), evidence(t, snapshot, b, db), now, now.Add(20*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	all := append(append([]recoverybridge.Statement{}, lapsed...), fresh...)
	f.Catalog.Bridges = nil
	for _, s := range all {
		f.Catalog.Bridges = append(f.Catalog.Bridges, s.Digest)
	}
	f.Catalog.Sequence++
	snapshot = f.Snapshot(t)
	var bridges []releasetrust.VerifiedBridge
	for _, s := range all {
		verified, err := releasetrust.VerifyBridge(snapshot, releasetrust.BridgeMaterial{Statement: s.Bytes, Signature: testfixture.Sign(t, f.RecoverySigner, s.Bytes)})
		if err != nil {
			t.Fatal(err)
		}
		bridges = append(bridges, verified)
	}
	var nodes []upgradecompat.VerifiedNode
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
	sourceOf := func(signed testfixture.SignedRelease, declaration upgradecompat.Declaration) upgradecompat.InstalledSource {
		for _, e := range declaration.Engines {
			if e.Migrations.Engine == releaseidentity.SQLite {
				return upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: signed.Identity}, Migrations: e.Migrations, SchemaSHA256: e.SchemaSHA256}
			}
		}
		t.Fatal("no sqlite declaration")
		return upgradecompat.InstalledSource{}
	}
	later := now.Add(2 * time.Hour)
	plan, err := upgradecompat.PlanRouteAt(snapshot, sourceOf(a, da), b.Identity, nodes, bridges, later, nil)
	if err != nil || len(plan.BridgeDigests()) != 1 {
		t.Fatalf("the re-signed bridge plans after the old one lapsed: %v %v", plan.BridgeDigests(), err)
	}
	if _, err := upgradecompat.PlanRouteAt(snapshot, sourceOf(b, db), b.Identity, nodes, bridges, later, nil); err != nil {
		t.Fatalf("an unrelated plan survives a lapsed bridge in the bundle: %v", err)
	}
	// Revoking every bridge leaves no bridged route, but not a broken bundle.
	revoked := []releaseidentity.Digest{}
	for _, s := range all {
		revoked = append(revoked, s.Digest)
	}
	if _, err := upgradecompat.PlanRouteAt(snapshot, sourceOf(b, db), b.Identity, nodes, bridges, later, revoked); err != nil {
		t.Fatalf("an unrelated plan survives revoked bridges: %v", err)
	}
	if _, err := upgradecompat.PlanRouteAt(snapshot, sourceOf(a, da), b.Identity, nodes, bridges, later, revoked); err == nil {
		t.Fatal("with every bridge revoked the bridged route is gone")
	}
}
