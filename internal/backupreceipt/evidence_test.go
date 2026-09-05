package backupreceipt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

type signedEvidenceFixture struct {
	signer     *testfixture.Fixture
	pin        PinnedOperator
	plan       upgradecompat.Plan
	ciphertext *Ciphertext
	material   EvidenceMaterial
	live       LiveSource
	legacy     LegacyInspection
	proposal   LegacyProposal
	now        time.Time
}

func newSignedEvidenceFixture(t *testing.T, legacy bool) signedEvidenceFixture {
	t.Helper()
	f := testfixture.New(t)
	schema := releaseidentity.Hash([]byte("actual domain catalog"))
	migrations := releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{{Version: 1, SHA256: releaseidentity.Hash([]byte("CREATE TABLE fixture(value TEXT);"))}}}
	sourceDeclaration := upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: releaseidentity.StableV1, Version: "1.0.0", Sequence: 1, Commit: strings.Repeat("a", 40), Engines: []upgradecompat.EngineDeclaration{{Migrations: migrations, SchemaSHA256: schema, Sources: []upgradecompat.SourceEdge{}}}}
	sourceRelease := f.AddStable(t, "1.0.0", 1, sourceDeclaration.Commit, testfixture.JSON(t, sourceDeclaration))
	source := releaseidentity.Source{Release: sourceRelease.Identity}
	if legacy {
		source = releaseidentity.Source{Genesis: releaseidentity.LegacyGenesisV1}
		schema = releaseidentity.Hash([]byte("actual legacy catalog"))
	}
	targetDeclaration := upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: releaseidentity.StableV1, Version: "1.1.0", Sequence: 2, Commit: strings.Repeat("b", 40), Engines: []upgradecompat.EngineDeclaration{{Migrations: migrations, SchemaSHA256: schema, Sources: []upgradecompat.SourceEdge{{Source: source, Migrations: migrations, SchemaSHA256: schema, Mode: upgradecompat.Maintenance}}}}}
	targetRelease := f.AddStable(t, "1.1.0", 2, targetDeclaration.Commit, testfixture.JSON(t, targetDeclaration))
	snapshot := f.Snapshot(t)
	nodes := []upgradecompat.VerifiedNode{}
	for _, release := range []testfixture.SignedRelease{sourceRelease, targetRelease} {
		verified, err := releasetrust.VerifyStable(snapshot, release.Material)
		if err != nil {
			t.Fatal(err)
		}
		node, err := upgradecompat.Bind(verified, release.Material.Compatibility)
		if err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, node)
	}
	plan, err := upgradecompat.PlanRoute(snapshot, upgradecompat.InstalledSource{Identity: source, Migrations: migrations, SchemaSHA256: schema}, targetRelease.Identity, nodes, nil)
	if err != nil {
		t.Fatal(err)
	}
	receipt := fixtureReceipt()
	receipt.Snapshot.SourceIdentity = source
	receipt.Snapshot.SourceSchemaSHA256 = schema
	receipt.Snapshot.MigrationSHA256, err = migrations.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if legacy {
		receipt.Snapshot.Authority = LegacyProposalAuthority
		receipt.Snapshot.SourceGeneration = 0
		receipt.Snapshot.RouteGeneration = 1
	}
	original := filepath.Join(t.TempDir(), "archive.age")
	if err := os.WriteFile(original, []byte("exact opaque encrypted artifact"), 0600); err != nil {
		t.Fatal(err)
	}
	pinned, err := PinCiphertext(context.Background(), original, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pinned.Close(); err != nil {
			t.Error(err)
		}
	})
	receipt.CiphertextSHA256 = pinned.Digest()
	receipt.CiphertextBytes = pinned.Size()
	pin, err := PinOperator(receipt.Snapshot.InstanceID, f.PrimaryPublic)
	if err != nil {
		t.Fatal(err)
	}
	rawReceipt := testfixture.JSON(t, receipt)
	a := fixtureAttestation()
	a.Authority = receipt.Snapshot.Authority
	a.SourceGeneration = receipt.Snapshot.SourceGeneration
	a.RouteGeneration = receipt.Snapshot.RouteGeneration
	a.ReceiptSHA256 = releaseidentity.Hash(rawReceipt)
	a.RouteSHA256 = plan.Digest()
	a.TargetIdentity = plan.Target()
	a.OperatorKeyID = pin.KeyID()
	rawAttestation := testfixture.JSON(t, a)
	return signedEvidenceFixture{signer: f, pin: pin, plan: plan, ciphertext: pinned, material: EvidenceMaterial{Receipt: rawReceipt, Attestation: rawAttestation, Signature: testfixture.Sign(t, f.PrimarySigner, rawAttestation)}, now: a.IssuedAt.Add(time.Minute), live: LiveSource{InstanceID: a.InstanceID, Engine: releaseidentity.SQLite, Source: source, SourceSchemaSHA256: schema, MigrationSHA256: receipt.Snapshot.MigrationSHA256, RestoreEpoch: a.RestoreEpoch, RecoveryIncarnation: a.RecoveryIncarnation, Generation: 1}, legacy: LegacyInspection{InstanceID: a.InstanceID, Engine: releaseidentity.SQLite, SchemaSHA256: schema, MigrationSHA256: receipt.Snapshot.MigrationSHA256, RestoreEpoch: a.RestoreEpoch}, proposal: LegacyProposal{RecoveryIncarnation: a.RecoveryIncarnation}}
}

func TestEvidenceAuthenticatesExactBytesAndCurrentAuthority(t *testing.T) {
	f := newSignedEvidenceFixture(t, false)
	verify := func(material EvidenceMaterial, live LiveSource, pin PinnedOperator, plan upgradecompat.Plan, now time.Time) (VerifiedEvidence, error) {
		return VerifyEvidence(context.Background(), pin, plan, f.ciphertext, material, live, now)
	}
	evidence, err := verify(f.material, f.live, f.pin, f.plan, f.now)
	if err != nil || !evidence.Valid() || evidence.Digest().Validate() != nil {
		t.Fatal("genuine signature-backed evidence refused", err)
	}
	copy := evidence.Receipt()
	copy.Snapshot.RecipientFingerprints[0] = "changed"
	if evidence.Receipt().Snapshot.RecipientFingerprints[0] == "changed" {
		t.Fatal("verified evidence exposed mutable inventory")
	}
	for name, change := range map[string]func(*LiveSource){
		"restored source incarnation": func(s *LiveSource) { s.RecoveryIncarnation = Nonce(strings.Repeat("9", 64)) },
		"credential epoch":            func(s *LiveSource) { s.RestoreEpoch++ },
		"generation":                  func(s *LiveSource) { s.Generation++ },
		"instance":                    func(s *LiveSource) { s.InstanceID = "ins_" + strings.Repeat("9", 32) },
		"engine":                      func(s *LiveSource) { s.Engine = releaseidentity.Postgres },
		"migration":                   func(s *LiveSource) { s.MigrationSHA256 = releaseidentity.Hash([]byte("changed")) },
		"release":                     func(s *LiveSource) { s.Source.Release.Commit = strings.Repeat("e", 40) },
	} {
		t.Run(name, func(t *testing.T) {
			live := f.live
			change(&live)
			if _, err := verify(f.material, live, f.pin, f.plan, f.now); err == nil {
				t.Fatal("stale or foreign source accepted")
			}
		})
	}
	a, _ := ParseAttestation(f.material.Attestation)
	for _, now := range []time.Time{a.IssuedAt.Add(-time.Second), a.ExpiresAt, a.ExpiresAt.Add(time.Second)} {
		if _, err := verify(f.material, f.live, f.pin, f.plan, now); err == nil {
			t.Fatal("outside validity accepted")
		}
	}
	other := testfixture.New(t)
	retiredPin, err := PinOperator(f.live.InstanceID, other.PrimaryPublic)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verify(f.material, f.live, retiredPin, f.plan, f.now); err == nil {
		t.Fatal("archived signer accepted after pin rotation")
	}
	if _, err := verify(f.material, f.live, f.pin, upgradecompat.Plan{}, f.now); err == nil {
		t.Fatal("caller-created empty plan accepted")
	}
	changed := f.material
	changed.Receipt = append(append([]byte{}, changed.Receipt...), '\n')
	if _, err := verify(changed, f.live, f.pin, f.plan, f.now); err == nil {
		t.Fatal("receipt bytes changed without attestation signature")
	}
	changed = f.material
	changed.Signature = testfixture.Sign(t, other.PrimarySigner, changed.Attestation)
	if _, err := verify(changed, f.live, f.pin, f.plan, f.now); err == nil {
		t.Fatal("foreign signature accepted")
	}
	for name, mutate := range map[string]func(*Attestation){
		"route":  func(a *Attestation) { a.RouteSHA256 = releaseidentity.Hash([]byte("other route")) },
		"target": func(a *Attestation) { a.TargetIdentity.Commit = strings.Repeat("e", 40) },
		"bridge": func(a *Attestation) {
			a.BridgeSHA256 = []releaseidentity.Digest{releaseidentity.Hash([]byte("other bridge"))}
		},
		"incarnation": func(a *Attestation) { a.RecoveryIncarnation = Nonce(strings.Repeat("9", 64)) },
	} {
		t.Run("signed wrong "+name, func(t *testing.T) {
			a, _ := ParseAttestation(f.material.Attestation)
			mutate(&a)
			changed := f.material
			changed.Attestation = testfixture.JSON(t, a)
			changed.Signature = testfixture.Sign(t, f.signer.PrimarySigner, changed.Attestation)
			if _, err := verify(changed, f.live, f.pin, f.plan, f.now); err == nil {
				t.Fatal("valid signature promoted incorrect claim")
			}
		})
	}
}

func TestLegacyProposalNeverMasqueradesAsLiveLedger(t *testing.T) {
	f := newSignedEvidenceFixture(t, true)
	evidence, err := VerifyLegacyEvidence(context.Background(), f.pin, f.plan, f.ciphertext, f.material, f.legacy, f.proposal, f.now)
	if err != nil || !evidence.Valid() || evidence.Statement().Authority != LegacyProposalAuthority {
		t.Fatal("explicit legacy proposal refused", err)
	}
	if _, err := VerifyEvidence(context.Background(), f.pin, f.plan, f.ciphertext, f.material, f.live, f.now); err == nil {
		t.Fatal("proposed authority treated as persisted ledger")
	}
	changed := f.legacy
	changed.SchemaSHA256 = releaseidentity.Hash([]byte("other catalog"))
	if _, err := VerifyLegacyEvidence(context.Background(), f.pin, f.plan, f.ciphertext, f.material, changed, f.proposal, f.now); err == nil {
		t.Fatal("uninspected legacy schema accepted")
	}
	changedProposal := f.proposal
	changedProposal.RecoveryIncarnation = Nonce(strings.Repeat("9", 64))
	if _, err := VerifyLegacyEvidence(context.Background(), f.pin, f.plan, f.ciphertext, f.material, f.legacy, changedProposal, f.now); err == nil {
		t.Fatal("changed proposal accepted")
	}
}

func TestRotationRequiresPriorSignerAndExplicitBreakGlassMode(t *testing.T) {
	f := newSignedEvidenceFixture(t, false)
	next := testfixture.New(t)
	nextID, err := releasetrust.OperatorKeyID(next.PrimaryPublic)
	if err != nil {
		t.Fatal(err)
	}
	r := Rotation{Format: RotationFormat, Mode: PriorKeyRotation, InstanceID: f.live.InstanceID, RecoveryIncarnation: f.live.RecoveryIncarnation, RestoreEpoch: f.live.RestoreEpoch, MaxKnownCredentialEpoch: f.live.RestoreEpoch, NextEpoch: f.live.RestoreEpoch + 1, CurrentKeyID: f.pin.KeyID(), NewKeyID: nextID, IssuedAt: f.now}
	raw := testfixture.JSON(t, r)
	transition, err := VerifyKeyTransition(f.pin, next.PrimaryPublic, raw, testfixture.Sign(t, f.signer.PrimarySigner, raw), RotationSource{Live: f.live, MaxKnownCredentialEpoch: f.live.RestoreEpoch}, f.now)
	if err != nil || !transition.Valid() || transition.RequiresLocalRecovery() || transition.NextOperator().KeyID() != nextID {
		t.Fatal("prior-key rotation refused", err)
	}
	if _, err := VerifyKeyTransition(f.pin, next.PrimaryPublic, raw, testfixture.Sign(t, next.PrimarySigner, raw), RotationSource{Live: f.live, MaxKnownCredentialEpoch: f.live.RestoreEpoch}, f.now); err == nil {
		t.Fatal("new key authorized its own ordinary rotation")
	}
	r.Mode = LocalBreakGlass
	raw = testfixture.JSON(t, r)
	transition, err = VerifyKeyTransition(f.pin, next.PrimaryPublic, raw, testfixture.Sign(t, next.PrimarySigner, raw), RotationSource{Live: f.live, MaxKnownCredentialEpoch: f.live.RestoreEpoch}, f.now)
	if err != nil || !transition.Valid() || !transition.RequiresLocalRecovery() {
		t.Fatal("break-glass transition lost required local custody", err)
	}
	stale := f.live
	stale.RestoreEpoch++
	if _, err := VerifyKeyTransition(f.pin, next.PrimaryPublic, raw, testfixture.Sign(t, next.PrimarySigner, raw), RotationSource{Live: stale, MaxKnownCredentialEpoch: stale.RestoreEpoch}, f.now); err == nil {
		t.Fatal("stale epoch transition accepted")
	}
}
