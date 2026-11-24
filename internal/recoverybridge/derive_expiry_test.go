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
