package upgradebundle

import (
	"errors"
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

// A bridge the list revoked and its re-signed replacement share a source and
// target pair while both are inside not_after. Planning must use the list to decide,
// not fail every plan with a duplicate-pair error.
func TestRevokedBridgeAndItsReplacementCoexist(t *testing.T) {
	f := testfixture.New(t)
	commit := strings.Repeat("a", 40)
	var releases []testfixture.SignedRelease
	var declarations []upgradecompat.Declaration
	for sequence, count := range []int{2, 4} {
		d := upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: releaseidentity.StableV1, Version: fmt.Sprintf("1.%d.0", sequence+1), Sequence: uint64(sequence + 1), Commit: commit}
		for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
			m := releaseidentity.MigrationManifest{Engine: engine, Entries: []releaseidentity.Migration{}}
			for i := range count {
				m.Entries = append(m.Entries, releaseidentity.Migration{Version: uint64(i + 1), SHA256: releaseidentity.Hash([]byte(fmt.Sprint("x", i)))})
			}
			digest, _ := m.Digest()
			d.Engines = append(d.Engines, upgradecompat.EngineDeclaration{Migrations: m, SchemaSHA256: digest, Sources: []upgradecompat.SourceEdge{}})
		}
		releases = append(releases, f.AddStable(t, d.Version, int64(sequence+1), commit, testfixture.JSON(t, d)))
		declarations = append(declarations, d)
	}
	snapshot := f.Snapshot(t)
	evidence := func(i int) recoverybridge.Evidence {
		verified, err := releasetrust.VerifyStable(snapshot, releases[i].Material)
		if err != nil {
			t.Fatal(err)
		}
		return recoverybridge.Evidence{Release: verified, Declaration: declarations[i]}
	}
	now := time.Now().UTC().Truncate(time.Second)
	var statements []recoverybridge.Statement
	for _, expiry := range []time.Duration{24 * time.Hour, 48 * time.Hour} {
		derived, err := recoverybridge.Derive(evidence(0), evidence(1), now, now.Add(expiry))
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range derived {
			if s.Engine == releaseidentity.SQLite {
				statements = append(statements, s)
			}
		}
	}
	f.Catalog.Bridges = []releaseidentity.Digest{statements[0].Digest, statements[1].Digest}
	f.Catalog.Sequence++
	snapshot = f.Snapshot(t)
	bundle := Bundle{snapshot: snapshot}
	for _, signed := range releases {
		verified, err := releasetrust.VerifyStable(snapshot, signed.Material)
		if err != nil {
			t.Fatal(err)
		}
		node, err := upgradecompat.Bind(verified, signed.Material.Compatibility)
		if err != nil {
			t.Fatal(err)
		}
		bundle.nodes = append(bundle.nodes, node)
	}
	for _, s := range statements {
		verified, err := releasetrust.VerifyBridge(snapshot, releasetrust.BridgeMaterial{Statement: s.Bytes, Signature: testfixture.Sign(t, f.RecoverySigner, s.Bytes)})
		if err != nil {
			t.Fatal(err)
		}
		bundle.bridges = append(bundle.bridges, verified)
	}
	var sqlite upgradecompat.EngineDeclaration
	for _, e := range declarations[0].Engines {
		if e.Migrations.Engine == releaseidentity.SQLite {
			sqlite = e
		}
	}
	source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: releases[0].Identity}, Migrations: sqlite.Migrations, SchemaSHA256: sqlite.SchemaSHA256}
	target := releases[1].Identity

	if _, err := bundle.Plan(source, target); err == nil || !strings.Contains(err.Error(), "duplicate recovery bridge pair") {
		t.Fatalf("two live bridges on one pair, nothing revoked, is a catalog error: %v", err)
	}
	revokedOld := bundle.WithBridgeRevocations(func() ([]releaseidentity.Digest, error) { return []releaseidentity.Digest{statements[0].Digest}, nil })
	plan, err := revokedOld.Plan(source, target)
	if err != nil || len(plan.BridgeDigests()) != 1 || plan.BridgeDigests()[0] != statements[1].Digest {
		t.Fatalf("with the old statement revoked the replacement must plan: %v %v", plan.BridgeDigests(), err)
	}
	// The admitted route resumes and rolls back although both statements are in the
	// bundle; only a revocation of the bridge it uses refuses the resume.
	journaled := plan.Digest()
	if _, err := revokedOld.PlanAdmitted(source, target, journaled); err != nil {
		t.Fatalf("resume of the admitted route: %v", err)
	}
	revokedNew := bundle.WithBridgeRevocations(func() ([]releaseidentity.Digest, error) { return []releaseidentity.Digest{statements[1].Digest}, nil })
	if _, err := revokedNew.PlanAdmitted(source, target, journaled); err == nil || !strings.Contains(err.Error(), "recovery bridge revoked") {
		t.Fatalf("resume over a revoked bridge must be refused: %v", err)
	}
	if plan, err := revokedNew.PlanRollback(source, target, journaled); err != nil || plan.Digest() != journaled {
		t.Fatalf("rollback is never blocked by revocation: %v", err)
	}
	failing := bundle.WithBridgeRevocations(func() ([]releaseidentity.Digest, error) { return nil, errors.New("status 404") })
	if _, err := failing.Plan(source, target); err == nil || !strings.Contains(err.Error(), "recovery bridge revocation check") {
		t.Fatalf("an unreadable list must refuse: %v", err)
	}
}
